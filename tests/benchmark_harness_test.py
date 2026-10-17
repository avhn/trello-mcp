"""Offline harness regression checks: python3 -I tests/benchmark_harness_test.py."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import time
import unittest


HARNESS = Path(__file__).resolve().parents[1] / 'docs/benchmarks/harness/mcpc.py'
FAKE_SERVER = r'''
import json
import os
from pathlib import Path
import signal
import sys
import threading

mode, pid_path = sys.argv[1:]
Path(pid_path).write_text(str(os.getpid()), encoding='utf-8')
print('synthetic server stderr', file=sys.stderr, flush=True)
if mode == 'stubborn':
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
for line in sys.stdin:
    request = json.loads(line)
    if 'id' not in request:
        continue
    method = request['method']
    if ((method == 'initialize' and mode in ('silent', 'partial', 'chatter', 'stubborn'))
            or (method == 'tools/call' and mode == 'silent_call')):
        if mode == 'partial':
            sys.stdout.write('{"jsonrpc":')
            sys.stdout.flush()
        if mode == 'chatter':
            while True:
                print('{"jsonrpc":"2.0","method":"notifications/progress"}', flush=True)
                threading.Event().wait(0.001)
        threading.Event().wait()
    if method == 'tools/call' and mode == 'crash':
        sys.exit(7)
    response = {'jsonrpc': '2.0', 'id': request['id']}
    if method == 'tools/list':
        response['result'] = {'tools': [{'name': 'sample', 'description': 'café'}]}
    elif method == 'tools/call':
        tool = request['params']['name']
        if tool == 'rpc_error':
            response['error'] = {'code': -32603, 'message': 'synthetic failure'}
        elif tool == 'empty':
            response['result'] = {}
        else:
            response['result'] = {'content': [
                {'type': 'text', 'text': 'café'},
                {'type': 'image', 'data': 'ignored'},
                {'type': 'text', 'text': ' / 東京\n'},
            ], 'isError': tool == 'tool_error'}
    else:
        response['result'] = {}
    print(json.dumps(response), flush=True)
    if mode == 'unread_call' and method == 'tools/list':
        threading.Event().wait()
'''


class HarnessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='trello-benchmark-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.server = self.root / 'server.py'
        self.server.write_text(textwrap.dedent(FAKE_SERVER), encoding='utf-8')
        self.plan = self.root / 'plan.json'
        self.plan.write_text(json.dumps([
            {'name': 'tools', 'tool': '__tools_list__'},
            *({'name': tool, 'tool': tool, 'args': {}}
              for tool in ('sample', 'tool_error', 'rpc_error', 'empty')),
        ]), encoding='utf-8')
        self.counter = 0

    def run_harness(self, mode='normal', arguments=None, timeout='0.5'):
        self.counter += 1
        out_dir = self.root / f'out-{self.counter}'
        pid_path = self.root / f'pid-{self.counter}'
        if arguments is None:
            arguments = [str(out_dir), str(self.plan), '--', sys.executable, '-I',
                         str(self.server), mode, str(pid_path)]
        result = subprocess.run([sys.executable, '-I', str(HARNESS), *arguments],
                                capture_output=True, text=True, timeout=10,
                                env={'MCPC_TIMEOUT': timeout, 'PYTHONIOENCODING': 'utf-8'})
        return result, out_dir, pid_path

    def assert_reaped(self, pid_path):
        self.assertTrue(pid_path.is_file(), 'fake server must have started')
        if os.name == 'posix':
            with self.assertRaises(ProcessLookupError):
                os.kill(int(pid_path.read_text(encoding='utf-8')), 0)

    def test_usage_errors_before_creating_output(self):
        missing_out = self.root / 'unused'
        for arguments in ([], [str(missing_out), str(self.plan)],
                          [str(missing_out), str(self.plan), '--'],
                          [str(missing_out), str(self.plan), '--', '']):
            with self.subTest(arguments=arguments):
                result, _, _ = self.run_harness(arguments=arguments)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn('Usage:', result.stderr)
                self.assertNotIn('Traceback', result.stderr)
                self.assertFalse(missing_out.exists())

    def test_invalid_paths_and_plan(self):
        for out_dir, plan in ((self.plan, self.plan),
                              (self.root / 'unused', self.root / 'missing.json')):
            with self.subTest(out_dir=out_dir, plan=plan):
                result, _, _ = self.run_harness(arguments=[str(out_dir), str(plan), '--', 'unused'])
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertNotIn('Traceback', result.stderr)
        self.plan.write_text('not JSON', encoding='utf-8')
        result, out_dir, _ = self.run_harness()
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertFalse(out_dir.exists())
        for name in ('../escape', 'a/b', ''):
            with self.subTest(name=name):
                self.plan.write_text(json.dumps([{'name': name, 'tool': 'sample', 'args': {}}]), encoding='utf-8')
                result, out_dir, _ = self.run_harness()
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertFalse(out_dir.exists())

    def test_output_is_private(self):
        if os.name != 'posix':
            self.skipTest('POSIX permissions')
        result, out_dir, _ = self.run_harness()
        self.assertEqual(result.returncode, 0, result.stderr)
        for path in (out_dir, *out_dir.iterdir()):
            self.assertEqual(path.stat().st_mode & 0o077, 0, path)

    def test_invalid_deadline(self):
        for timeout in ('0', '-1', 'nan', 'inf', 'invalid'):
            with self.subTest(timeout=timeout):
                result, out_dir, _ = self.run_harness(timeout=timeout)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn('MCPC_TIMEOUT', result.stderr)
                self.assertFalse(out_dir.exists())

    def test_normal_counts_and_per_step_files(self):
        result, out_dir, pid_path = self.run_harness()
        self.assertEqual(result.returncode, 0, result.stderr)
        tools = json.dumps({'tools': [{'name': 'sample', 'description': 'café'}]}, separators=(',', ':'))
        text = 'café / 東京\n'
        error = json.dumps({'code': -32603, 'message': 'synthetic failure'})
        expected = {'tools': tools, 'sample': text, 'tool_error': text, 'rpc_error': error, 'empty': ''}
        self.assertEqual(json.loads(result.stdout), [
            {'name': 'tools', 'chars': len(tools), 'tools': 1},
            {'name': 'sample', 'chars': len(text), 'isError': False},
            {'name': 'tool_error', 'chars': len(text), 'isError': True},
            {'name': 'rpc_error', 'chars': len(error), 'isError': True},
            {'name': 'empty', 'chars': 0, 'isError': False},
        ])
        self.assertEqual({p.name for p in out_dir.iterdir()},
                         {'server.stderr', *(name + '.txt' for name in expected)})
        for name, content in expected.items():
            self.assertEqual((out_dir / f'{name}.txt').read_bytes(), content.encode('utf-8'))
        self.assert_reaped(pid_path)

    def test_deadline_includes_partial_lines_and_unrelated_messages(self):
        for mode, method in (('silent', 'initialize'), ('partial', 'initialize'),
                             ('chatter', 'initialize'), ('silent_call', 'tools/call')):
            with self.subTest(mode=mode):
                started = time.monotonic()
                result, out_dir, pid_path = self.run_harness(mode=mode)
                self.assertLess(time.monotonic() - started, 8)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f'timeout during {method}', result.stderr)
                self.assertIn(str(out_dir / 'server.stderr'), result.stderr)
                self.assertNotIn('Traceback', result.stderr)
                self.assert_reaped(pid_path)

    def test_deadline_includes_blocked_stdin_write(self):
        plan = json.loads(self.plan.read_text(encoding='utf-8'))
        plan[1]['args'] = {'payload': 'x' * (1024 * 1024)}
        self.plan.write_text(json.dumps(plan), encoding='utf-8')
        result, out_dir, pid_path = self.run_harness(mode='unread_call')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('timeout during tools/call', result.stderr)
        self.assertIn(str(out_dir / 'server.stderr'), result.stderr)
        self.assertNotIn('Traceback', result.stderr)
        self.assert_reaped(pid_path)

    @unittest.skipUnless(os.name == 'posix', 'SIGTERM ignore behavior requires POSIX')
    def test_kill_fallback_reaps_server(self):
        result, _, pid_path = self.run_harness(mode='stubborn')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('timeout during initialize', result.stderr)
        self.assert_reaped(pid_path)

    def test_crash_mid_run(self):
        result, out_dir, pid_path = self.run_harness(mode='crash')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('server exited during tools/call', result.stderr)
        self.assertIn(str(out_dir / 'server.stderr'), result.stderr)
        self.assertNotIn('Traceback', result.stderr)
        self.assertTrue((out_dir / 'tools.txt').is_file())
        self.assertFalse((out_dir / 'sample.txt').exists())
        self.assert_reaped(pid_path)


if __name__ == '__main__':
    unittest.main()
