#!/usr/bin/env python3
"""Synthetic release assembly regressions; no credentials or external calls."""

import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shlex
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import textwrap
import unittest
from unittest import mock
import warnings
import zipfile

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("release_manifest", ROOT / "scripts" / "release_manifest.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="release-test-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.metadata = release.source_metadata()
        self.version = self.metadata["version"]

    def bundle(self, name="trello-mcp_desktop.mcpb", change=None, entries=None):
        manifest = json.loads((ROOT / "packaging/mcpb/manifest.template.json").read_text())
        manifest["version"] = self.version
        manifest["compatibility"] = {"platforms": release.BUNDLES[name]}
        if name == "trello-mcp_desktop.mcpb":
            manifest["server"]["mcp_config"]["platform_overrides"] = {
                "win32": {"command": "${__dirname}/server/trello-mcp.exe"}}
            mac = bytearray(48)
            mac[:8] = struct.pack(">II", 0xCAFEBABE, 2)
            struct.pack_into(">I", mac, 8, 0x01000007)
            struct.pack_into(">I", mac, 28, 0x0100000C)
            windows = bytearray(70)
            windows[:2] = b"MZ"
            struct.pack_into("<I", windows, 60, 64)
            windows[64:] = b"PE\x00\x00\x64\x86"
            files = {"server/trello-mcp": bytes(mac), "server/trello-mcp.exe": bytes(windows)}
        else:
            manifest["server"]["mcp_config"].pop("platform_overrides", None)
            linux = bytearray(64)
            linux[:6] = b"\x7fELF\x02\x01"
            struct.pack_into("<H", linux, 18, 62 if "amd64" in name else 183)
            files = {"server/trello-mcp": bytes(linux)}
        if change:
            change(manifest)
        files.update({"manifest.json": json.dumps(manifest).encode(), "LICENSE": b"synthetic license",
                      "icon.png": b"synthetic icon"})
        path = self.directory / name
        with zipfile.ZipFile(path, "w") as archive:
            for filename, value in files.items():
                info = zipfile.ZipInfo(filename)
                info.create_system = 3
                info.external_attr = (stat.S_IFREG | (0o755 if filename.startswith("server/") else 0o644)) << 16
                archive.writestr(info, value)
            for info, value in entries or []:
                archive.writestr(info, value)
        return path

    def all_assets(self):
        for name in release.CLI_ASSETS:
            (self.directory / name).write_bytes(("synthetic archive " + name).encode())
        for name in release.BUNDLES:
            self.bundle(name)

    def test_source_stays_metadata_only(self):
        self.assertNotIn("packages", self.metadata)
        release.source_metadata(tag="v" + self.version)
        with self.assertRaisesRegex(ValueError, "tag/version"):
            release.source_metadata(tag="v99.0.0")

    def release_workflow(self):
        return (ROOT / ".github/workflows/release.yml").read_text().split('\n  release:\n', 1)[1]

    def run_blocks(self, workflow):
        return [textwrap.dedent(block) for block in
                re.findall(r'^        run: \|\n((?:          [^\n]*\n|\n)+)', workflow, re.MULTILINE)]

    def test_workflow_publishes_normal_release_asset_set_without_registry_submission(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        job = self.release_workflow()
        publication = job.split('gh release create "$RELEASE_TAG"', 1)[1]
        assets = re.findall(r'"\$DIST/([^"\n]+)"', publication)
        expected = list(release.expected_assets('registry')) + ['checksums.txt']
        self.assertCountEqual(assets, expected)
        self.assertEqual(len(assets), 11)
        needs = re.search(r'needs: \[([^\]]+)\]', job).group(1).split(', ')
        self.assertTrue({'checks', 'native', 'desktop', 'desktop-windows', 'windows-installer'} <= set(needs))
        self.assertIn('notes --output "$GITHUB_WORKSPACE/notes.md"', job)
        self.assertIn('--notes-file "$GITHUB_WORKSPACE/notes.md"', publication)
        self.assertIn('--verify-tag', publication)
        self.assertLess(job.index(' generate --directory '), job.index(' publisher-validate --input '))
        self.assertLess(job.index(' publisher-validate --input '), job.index('gh release create'))
        self.assertIn('checksums --directory "$DIST" --kind registry --verify', job)
        self.assertNotIn('continue-on-error:', job)
        self.assertNotIn('if:', job)
        for name in ('ci.yml', 'release.yml'):
            text = (ROOT / '.github/workflows' / name).read_text()
            self.assertNotRegex(text, r'\bmcp-publisher\s+(?:login|publish)\b')
            self.assertNotRegex(text, r'(?i)secrets\.[^ }]*(?:registry|publisher)')
        self.assertNotIn('preview', workflow.lower())

    def test_workflow_paths_resolve_from_workspace_and_initialize_dist_per_step(self):
        job = self.release_workflow()
        declaration = 'DIST="${GITHUB_WORKSPACE:?GITHUB_WORKSPACE must be set}/dist"'
        self.assertEqual(job.count('path: ${{ github.workspace }}/dist'), 2)
        self.assertNotIn('path: preview', job)
        self.assertNotIn('$PWD', job)
        self.assertNotIn('/home/avhn/', job)
        for block in self.run_blocks(job):
            subprocess.run(['bash', '-n'], input=block, text=True, capture_output=True, check=True)
            if '$DIST' in block:
                self.assertEqual(block.splitlines()[0], declaration)
            for line in block.splitlines():
                if 'release_manifest.py' in line:
                    self.assertIn('python3 -I "$GITHUB_WORKSPACE/scripts/release_manifest.py"', line)
        workflow = (ROOT / '.github/workflows/release.yml').read_text()
        for path in re.findall(r'^          path: (.+)$', workflow, re.MULTILINE):
            self.assertTrue(path.startswith('${{ github.workspace }}/'), path)
        for name in ('ci.yml', 'release.yml'):
            text = (ROOT / '.github/workflows' / name).read_text()
            self.assertNotIn('/home/avhn/', text)
            for engine in ('pwsh', 'powershell'):
                self.assertIn('shell: ' + engine, text)
                for test in ('install_test.ps1', 'setup_commands_test.ps1'):
                    prefix = engine + ' -NoProfile '
                    if engine == 'powershell':
                        prefix += '-ExecutionPolicy Bypass '
                    command = prefix + '-File "$env:GITHUB_WORKSPACE/tests/' + test + '"'
                    self.assertIn(command + '\n          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }', text)

    def test_exact_workflow_generation_command_in_unrelated_checkout_with_spaces(self):
        block, = [block for block in self.run_blocks(self.release_workflow()) if ' generate --directory ' in block]
        command, = [line for line in block.splitlines() if ' generate --directory ' in line]
        workspace = self.directory / 'unrelated checkout with spaces'
        scripts = workspace / 'scripts'
        scripts.mkdir(parents=True)
        (scripts / 'release_manifest.py').write_text('import json, sys\nprint(json.dumps(sys.argv[1:]))\n')
        elsewhere = self.directory / 'other working directory'
        elsewhere.mkdir()
        env = dict(os.environ, GITHUB_WORKSPACE=str(workspace), RELEASE_TAG='v' + self.version)
        result = subprocess.run(['bash', '-eu', '-c', block.splitlines()[0] + '\n' + command],
                                cwd=elsewhere, env=env, check=True, text=True, capture_output=True)
        self.assertEqual(json.loads(result.stdout),
                         ['--tag', 'v' + self.version, 'generate', '--directory', str(workspace / 'dist')])
        env.pop('GITHUB_WORKSPACE')
        result = subprocess.run(['bash', '-eu', '-c', block.splitlines()[0] + '\n' + command],
                                cwd=elsewhere, env=env, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('GITHUB_WORKSPACE must be set', result.stderr)

    def test_workflow_generated_validation_failure_prevents_publication(self):
        blocks = self.run_blocks(self.release_workflow())
        validation, = [block for block in blocks if ' publisher-validate --input ' in block]
        publication, = [block for block in blocks if 'gh release create ' in block]
        workspace = self.directory / 'synthetic checkout with spaces'
        scripts = workspace / 'scripts'
        scripts.mkdir(parents=True)
        (scripts / 'release_manifest.py').write_text('raise SystemExit(1)\n')
        marker = self.directory / 'unexpected publication'
        gh = scripts / 'gh'
        gh.write_text('#!/bin/sh\nprintf published > ' + shlex.quote(str(marker)) + '\n')
        gh.chmod(0o755)
        env = dict(os.environ, GITHUB_WORKSPACE=str(workspace), RELEASE_TAG='v' + self.version,
                   PATH=str(scripts) + os.pathsep + os.defpath)
        result = subprocess.run(['bash', '-eu', '-c', validation + '\n' + publication],
                                cwd=self.directory, env=env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertFalse(marker.exists())

    def test_malformed_manifest_json(self):
        for data in (b'{', b'[]', b'{"version":"1.0.0","version":"2.0.0"}'):
            path = self.bundle()
            with zipfile.ZipFile(path) as archive:
                entries = [(info, archive.read(info)) for info in archive.infolist()]
            with zipfile.ZipFile(path, 'w') as archive:
                for info, content in entries:
                    archive.writestr(info, data if info.filename == 'manifest.json' else content)
            with self.subTest(data=data), self.assertRaises(ValueError):
                release.inspect_bundle(path, self.version)

    def test_source_rejects_packages_and_duplicate_json(self):
        path = self.directory / "source.json"
        value = dict(self.metadata, packages=[])
        path.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, "metadata-only"):
            release.source_metadata(path)
        with self.assertRaisesRegex(ValueError, "duplicate JSON"):
            release.read_json('{"version":"1.0.0","version":"2.0.0"}')

    def test_all_bundle_layouts(self):
        for name in release.BUNDLES:
            with self.subTest(name=name):
                release.inspect_bundle(self.bundle(name), self.version)

    def test_extract_approved_files_only(self):
        path = self.bundle()
        output = self.directory / "empty"
        output.mkdir()
        release.inspect_bundle(path, self.version, output)
        self.assertTrue((output / "server/trello-mcp.exe").is_file())
        with self.assertRaisesRegex(ValueError, "empty directory"):
            release.inspect_bundle(path, self.version, output)

    def test_wrong_version_and_platform(self):
        for change in (lambda value: value.update(version="99.0.0"),
                       lambda value: value.update(compatibility={"platforms": ["linux"]})):
            with self.subTest(change=change), self.assertRaises(ValueError):
                release.inspect_bundle(self.bundle(change=change), self.version)

    def test_invalid_manifest_and_command(self):
        for change in (lambda value: value.update(manifest_version="0.1"),
                       lambda value: value["server"]["mcp_config"].update(command="sh"),
                       lambda value: value["server"].update(type="node")):
            with self.subTest(change=change), self.assertRaises(ValueError):
                release.inspect_bundle(self.bundle(change=change), self.version)

    def test_credential_defaults_and_mapping(self):
        for change in (lambda value: value["user_config"]["token"].update(default="synthetic"),
                       lambda value: value["user_config"]["api_key"].update(sensitive=False),
                       lambda value: value["server"]["mcp_config"]["env"].update(TRELLO_TOKEN="literal")):
            with self.subTest(change=change), self.assertRaises(ValueError):
                release.inspect_bundle(self.bundle(change=change), self.version)

    def test_zip_traversal_absolute_and_unexpected_files(self):
        for name in ("../escape", "/absolute", "server/settings.json"):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "layout"):
                release.inspect_bundle(self.bundle(entries=[(name, b"bad")]), self.version)

    def test_zip_duplicates(self):
        with warnings.catch_warnings():
            warnings.simplefilter("ignore", UserWarning)
            path = self.bundle(entries=[("LICENSE", b"duplicate")])
        with self.assertRaisesRegex(ValueError, "duplicate ZIP"):
            release.inspect_bundle(path, self.version)

    def test_zip_modes_and_symlinks(self):
        for mode in (stat.S_IFLNK | 0o777, stat.S_IFREG | 0o644):
            path = self.bundle()
            with zipfile.ZipFile(path) as archive:
                files = [(info, archive.read(info)) for info in archive.infolist()]
            with zipfile.ZipFile(path, "w") as archive:
                for info, data in files:
                    if info.filename == "server/trello-mcp":
                        info.external_attr = mode << 16
                    archive.writestr(info, data)
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                release.inspect_bundle(path, self.version)

    def test_binary_header_tampering(self):
        for name in release.BUNDLES:
            path = self.bundle(name)
            with zipfile.ZipFile(path) as archive:
                files = [(info, archive.read(info)) for info in archive.infolist()]
            with zipfile.ZipFile(path, "w") as archive:
                for info, data in files:
                    archive.writestr(info, b"not native" if info.filename == "server/trello-mcp" else data)
            with self.subTest(name=name), self.assertRaises(ValueError):
                release.inspect_bundle(path, self.version)

    def test_generated_exact_common_package_and_all_checksums(self):
        self.all_assets()
        value = release.generate(self.metadata, self.directory)
        package, = value["packages"]
        self.assertEqual(package["registryType"], "mcpb")
        self.assertEqual(package["identifier"], f"{release.REPOSITORY}/releases/download/v{self.version}/trello-mcp_desktop.mcpb")
        self.assertEqual(package["fileSha256"], hashlib.sha256((self.directory / "trello-mcp_desktop.mcpb").read_bytes()).hexdigest())
        self.assertEqual(package["version"], self.version)
        self.assertEqual(value["version"], self.version)
        self.assertRegex(package["fileSha256"], r'^[0-9a-f]{64}$')
        self.assertEqual(package["transport"], {"type": "stdio"})
        self.assertEqual(json.loads((self.directory / 'server.json').read_text()), value)
        text = release.checksums(self.directory, "registry", verify=True)
        lines = text.splitlines()
        names = [line.split('  ', 1)[1] for line in lines]
        self.assertEqual(names, sorted(release.expected_assets('registry')))
        self.assertEqual(len(lines), len(set(lines)))
        self.assertEqual(len(lines), 10)
        self.assertNotIn("  checksums.txt", text)
        for line in lines:
            checksum, name = line.split('  ', 1)
            self.assertEqual(checksum, hashlib.sha256((self.directory / name).read_bytes()).hexdigest())

    def test_generated_invalid_package_semantics(self):
        self.all_assets()
        value = release.generate(self.metadata, self.directory)
        for key, invalid in (("identifier", "https://example.com/wrong.mcpb"),
                             ("identifier", f"{release.REPOSITORY}/releases/download/desktop-v{self.version}/trello-mcp_desktop.mcpb"),
                             ("version", "99.0.0"), ("fileSha256", "A" * 64), ("registryType", "npm")):
            altered = copy.deepcopy(value)
            altered["packages"][0][key] = invalid
            with self.subTest(key=key), self.assertRaises(ValueError):
                release.validate_generated(altered, self.metadata, self.directory)
        altered = copy.deepcopy(value)
        altered["packages"].append(copy.deepcopy(altered["packages"][0]))
        with self.assertRaises(ValueError):
            release.validate_generated(altered, self.metadata, self.directory)

    def test_generated_excludes_otherwise_valid_linux_packages(self):
        self.all_assets()
        value = release.generate(self.metadata, self.directory)
        for name in release.BUNDLES:
            if name == "trello-mcp_desktop.mcpb":
                continue
            release.inspect_bundle(self.directory / name, self.version)
            package = {"registryType": "mcpb",
                       "identifier": f"{release.REPOSITORY}/releases/download/v{self.version}/{name}",
                       "version": self.version,
                       "fileSha256": release.digest(self.directory / name),
                       "transport": {"type": "stdio"}}
            for packages in ([package], value["packages"] + [package]):
                altered = copy.deepcopy(value)
                altered["packages"] = packages
                with self.subTest(name=name, count=len(packages)), self.assertRaisesRegex(ValueError, "invalid generated package"):
                    release.validate_generated(altered, self.metadata, self.directory)

    def test_generated_stale_bundle_hash(self):
        self.all_assets()
        value = release.generate(self.metadata, self.directory)
        with (self.directory / "trello-mcp_desktop.mcpb").open("ab") as stream:
            stream.write(b"tampering")
        with self.assertRaisesRegex(ValueError, "stale bundle hash"):
            release.validate_generated(value, self.metadata, self.directory)

    def test_missing_unexpected_and_duplicate_artifacts(self):
        self.all_assets()
        (self.directory / release.CLI_ASSETS[0]).unlink()
        with self.assertRaisesRegex(ValueError, "exactly six"):
            release.generate(self.metadata, self.directory)
        (self.directory / release.CLI_ASSETS[0]).write_bytes(b"restored")
        (self.directory / "trello-mcp_desktop-copy.mcpb").write_bytes(b"duplicate")
        with self.assertRaises(ValueError):
            release.generate(self.metadata, self.directory)

    def test_cli_preview_checksum_separation_and_tamper(self):
        for kind, names in (("cli", release.CLI_ASSETS), ("mcpb", release.BUNDLES)):
            directory = self.directory / kind
            directory.mkdir()
            for name in names:
                (directory / name).write_bytes(b"synthetic bytes")
            text = release.checksums(directory, kind)
            self.assertEqual(len(text.splitlines()), len(names))
            release.checksums(directory, kind, verify=True)
            first = next(iter(names))
            (directory / first).write_bytes(b"tampered")
            with self.assertRaisesRegex(ValueError, "stale"):
                release.checksums(directory, kind, verify=True)

    def test_checksum_rejects_symlink_and_duplicate_line(self):
        for name in release.CLI_ASSETS:
            (self.directory / name).write_bytes(b"archive")
        release.checksums(self.directory, "cli")
        checksum_path = self.directory / "checksums.txt"
        checksum_path.write_text(checksum_path.read_text() * 2)
        with self.assertRaisesRegex(ValueError, "stale"):
            release.checksums(self.directory, "cli", verify=True)
        path = self.directory / release.CLI_ASSETS[0]
        path.unlink()
        path.symlink_to(self.directory / release.CLI_ASSETS[1])
        with self.assertRaisesRegex(ValueError, "nonregular"):
            release.checksums(self.directory, "cli")

    @mock.patch.object(release, 'publisher_validate')
    def test_generated_input_is_locally_verified_before_publisher_validation(self, publisher):
        self.all_assets()
        release.generate(self.metadata, self.directory)
        path = self.directory / 'server.json'
        argv = ['release_manifest.py', '--tag', 'v' + self.version, 'publisher-validate', '--input', str(path)]
        with mock.patch.object(sys, 'argv', argv):
            release.main()
        publisher.assert_called_once_with(path)
        publisher.reset_mock()
        (self.directory / release.CLI_ASSETS[0]).write_bytes(b'tampered final archive')
        with mock.patch.object(sys, 'argv', argv), self.assertRaisesRegex(ValueError, 'stale'):
            release.main()
        publisher.assert_not_called()
        release.checksums(self.directory, 'registry')
        value = json.loads(path.read_text())
        value['packages'][0]['identifier'] = value['packages'][0]['identifier'].replace('/v', '/desktop-v')
        path.write_text(json.dumps(value))
        with mock.patch.object(sys, 'argv', argv), self.assertRaisesRegex(ValueError, 'invalid generated'):
            release.main()
        publisher.assert_not_called()

    @mock.patch.object(release, 'publisher_validate')
    def test_default_publisher_validation_keeps_metadata_only_source_support(self, publisher):
        with mock.patch.object(sys, 'argv', ['release_manifest.py', 'publisher-validate']):
            release.main()
        publisher.assert_called_once_with(ROOT / 'server.json')

    def test_registry_checksums_reject_missing_asset_or_metadata_tamper(self):
        self.all_assets()
        release.generate(self.metadata, self.directory)
        asset = self.directory / release.CLI_ASSETS[0]
        original = asset.read_bytes()
        asset.unlink()
        with self.assertRaisesRegex(ValueError, 'missing'):
            release.checksums(self.directory, 'registry', verify=True)
        asset.write_bytes(original)
        path = self.directory / 'server.json'
        path.write_text(path.read_text() + ' ')
        with self.assertRaisesRegex(ValueError, 'stale'):
            release.checksums(self.directory, 'registry', verify=True)

    @mock.patch.object(release.subprocess, "check_output")
    def test_tag_notes_preserve_prose_and_git_extraction(self, output):
        prose = "Owner subject\n\n## Changes\nDesktop preview is pending owner testing.\n"
        output.side_effect = ["tag\n", prose]
        self.assertEqual(release.tag_notes("v" + self.version), prose)
        self.assertIn("--format=%(contents:subject)%0a%0a%(contents:body)", output.call_args.args[0])

    @mock.patch.object(release.subprocess, "check_output")
    def test_tag_notes_reject_missing_prose_and_placeholders(self, output):
        for prose in ("Subject\n\nOnly one line\n", "Subject\n\n## Changes\nPENDING verified value\n",
                      "Subject\n\n## Changes\nTODO replace\n", "Subject\n\n## Changes\n{{value}}\n"):
            output.side_effect = ["tag\n", prose]
            with self.subTest(prose=prose), self.assertRaises(ValueError):
                release.tag_notes("v" + self.version)
        output.side_effect = ["commit\n"]
        with self.assertRaisesRegex(ValueError, "annotated"):
            release.tag_notes("v" + self.version)

    @mock.patch.object(release.platform, "machine", return_value="x86_64")
    @mock.patch.object(release.platform, "system", return_value="Linux")
    @mock.patch.object(release.subprocess, "run")
    @mock.patch.object(release, "digest")
    def test_publisher_pinned_checksum_and_isolated_validation(self, digest, run, _system, _machine):
        digest.return_value = "a06c9096dcb9727c13555b6be26c7effa707b01f06a4c561ba7a3635443cf2cc"
        calls = []

        def execute(args, **kwargs):
            calls.append((args, kwargs))
            if args[0] == "curl":
                path = Path(args[args.index("--output") + 1])
                with tarfile.open(path, "w:gz") as archive:
                    for name in ("LICENSE", "README.md", "mcp-publisher"):
                        info = tarfile.TarInfo(name)
                        info.size = 4
                        archive.addfile(info, io.BytesIO(b"test"))
            else:
                env = kwargs["env"]
                self.assertNotEqual(env["HOME"], os.environ.get("HOME"))
                for variable in ("HOME", "XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA", "USERPROFILE"):
                    self.assertEqual(list(Path(env[variable]).iterdir()), [])
        run.side_effect = execute
        release.publisher_validate(ROOT / "server.json")
        self.assertIn("/v1.8.1/", calls[0][0][-1])
        self.assertEqual(calls[1][0][1:], ["--help"])
        self.assertEqual(calls[2][0][1:], ["validate", str(ROOT / "server.json")])
        digest.return_value = "wrong"
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            release.publisher_validate(ROOT / "server.json")
        self.assertEqual(len(calls), 4)
        digest.return_value = "a06c9096dcb9727c13555b6be26c7effa707b01f06a4c561ba7a3635443cf2cc"

        def remote_failure(args, **kwargs):
            execute(args, **kwargs)
            if len(args) > 1 and args[1] == "validate":
                raise release.subprocess.CalledProcessError(1, args)
        run.side_effect = remote_failure
        with self.assertRaises(release.subprocess.CalledProcessError):
            release.publisher_validate(ROOT / "server.json")


if __name__ == "__main__":
    unittest.main()
