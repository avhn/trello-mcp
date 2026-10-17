"""Stdio MCP client for the 3-server benchmark.

Usage: python3 -I mcpc.py OUT_DIR PLAN_JSON -- COMMAND...
PLAN_JSON is a list of {"name": label, "tool": tool, "args": {...}}; the special
tool "__tools_list__" records the tools/list result. Each response text is saved
to OUT_DIR/<label>.txt and a summary with character counts is printed as JSON.
MCPC_TIMEOUT sets the per-RPC deadline in seconds (default: 120).
Credentials must reach COMMAND through the environment; this client never reads them.
Use an OUT_DIR outside the repository; it holds raw board responses and the server's
stderr, which may include credentials or private data. Never commit it.
"""
import json
import math
import os
import queue
import re
import subprocess
import sys
import threading


def usage(message):
    print("Usage: python3 -I mcpc.py OUT_DIR PLAN_JSON -- COMMAND...", file=sys.stderr)
    print(f"mcpc.py: {message}", file=sys.stderr)
    raise SystemExit(2)


if len(sys.argv) < 5 or sys.argv[3] != "--" or not all(sys.argv[1:3]) or not sys.argv[4]:
    usage("expected OUT_DIR, PLAN_JSON, -- and a nonempty server command")
out_dir, plan_path = sys.argv[1], sys.argv[2]
command = sys.argv[4:]
if os.path.exists(out_dir) and not os.path.isdir(out_dir):
    usage("OUT_DIR must be a directory")
if not os.path.isfile(plan_path):
    usage("PLAN_JSON must be a file")
try:
    timeout = float(os.environ.get("MCPC_TIMEOUT", "120"))
    if not math.isfinite(timeout) or timeout <= 0:
        raise ValueError
except ValueError:
    usage("MCPC_TIMEOUT must be a positive finite number of seconds")
try:
    with open(plan_path, encoding="utf-8") as handle:
        plan = json.load(handle)
    if not isinstance(plan, list) or any(
        not isinstance(step, dict) or not isinstance(step.get("name"), str)
        or not re.fullmatch(r"[A-Za-z0-9_-]+", step["name"])
        or not isinstance(step.get("tool"), str)
        or (step["tool"] != "__tools_list__" and not isinstance(step.get("args"), dict))
        for step in plan
    ):
        usage("PLAN_JSON must be a list of tool calls named [A-Za-z0-9_-]+")
    os.umask(0o077)
    os.makedirs(out_dir, mode=0o700, exist_ok=True)
except (OSError, ValueError) as error:
    usage(str(error))
stderr_path = os.path.abspath(os.path.join(out_dir, "server.stderr"))
requests = queue.Queue()
responses = queue.Queue()
counter = 0


def exchange():
    try:
        for request in iter(requests.get, None):
            proc.stdin.write(json.dumps(request) + "\n")
            proc.stdin.flush()
            if "id" not in request:
                continue
            while True:
                line = proc.stdout.readline()
                if not line:
                    return
                try:
                    message = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(message, dict) and message.get("id") == request["id"]:
                    responses.put(message)
                    break
    except (OSError, UnicodeError):
        pass
    finally:
        responses.put(None)


def rpc(method, params=None):
    global counter
    counter += 1
    requests.put({"jsonrpc": "2.0", "id": counter, "method": method, "params": params or {}})
    try:
        message = responses.get(timeout=timeout)
    except queue.Empty:
        raise SystemExit(f"timeout during {method}; stderr: {stderr_path}") from None
    if message is None:
        raise SystemExit(f"server exited during {method}; stderr: {stderr_path}")
    return message


with open(stderr_path, "w", encoding="utf-8") as stderr_handle:
    proc = None
    worker = None
    try:
        proc = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=stderr_handle, text=True)
        worker = threading.Thread(target=exchange, daemon=True)
        worker.start()
        rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "bench3", "version": "0"}})
        requests.put({"jsonrpc": "2.0", "method": "notifications/initialized"})
        summary = []
        for step in plan:
            if step["tool"] == "__tools_list__":
                result = rpc("tools/list")["result"]
                text = json.dumps(result, separators=(",", ":"))
                summary.append({"name": step["name"], "chars": len(text), "tools": len(result.get("tools", []))})
            else:
                response = rpc("tools/call", {"name": step["tool"], "arguments": step["args"]})
                result = response.get("result") or {}
                text = "".join(c.get("text", "") for c in result.get("content", []) if c.get("type") == "text")
                if "error" in response:
                    text = json.dumps(response["error"])
                summary.append({"name": step["name"], "chars": len(text), "isError": bool(result.get("isError")) or "error" in response})
            with open(os.path.join(out_dir, step["name"] + ".txt"), "w", encoding="utf-8") as handle:
                handle.write(text)
        print(json.dumps(summary, indent=1))
    except OSError as error:
        raise SystemExit(f"server I/O failed: {error}; stderr: {stderr_path}") from None
    finally:
        if proc is not None:
            requests.put(None)
            # Stop the child before closing pipes: the worker may be blocked writing.
            if proc.poll() is None:
                proc.terminate()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
            if worker is not None:
                worker.join(timeout=1)
            if worker is None or not worker.is_alive():
                try:
                    proc.stdin.close()
                except OSError:
                    pass
                proc.stdout.close()
