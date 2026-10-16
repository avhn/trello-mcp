#!/usr/bin/env python3
"""Build contract for release CI (run from trusted checkout with python3 -I).

Install tooling: npm ci --prefix packaging/mcpb --ignore-scripts
Required args: --version VERSION --target {desktop,linux-amd64,linux-arm64}
               --output-dir DIR
Desktop inputs: --darwin-amd64 FILE --darwin-arm64 FILE --windows-amd64 FILE
  Requires lipo on PATH; combines and verifies exactly arm64 + x86_64 slices.
Linux inputs: --linux-binary FILE (matching target architecture).
Outputs: DIR/trello-mcp_desktop.mcpb, DIR/trello-mcp_linux_amd64.mcpb or
  DIR/trello-mcp_linux_arm64.mcpb, respectively. Existing files are not replaced.
Version must match root server.json, without the optional leading v in the arg.
Binaries must use -X main.version=vVERSION (or VERSION). No builds or downloads
are performed by this script. Requires Node and locked @anthropic-ai/mcpb 2.1.2.

Each call stages only approved files, validates/packs with the pinned CLI,
checks ZIP names/modes/content, unpacks into a separate empty directory, then
checks the extracted payload. A matching host runs --version plus synthetic
MCP initialize, tools/list and server_health; foreign payloads print SKIP.
--require-smoke fails unless at least one payload matches the current host.
No real credentials, client settings or upstream requests are used.
For another native CI host after verified extraction, run just the smoke check:
  --version VERSION --smoke-binary /absolute/extracted/server/trello-mcp[.exe]
This mode needs neither Node nor lipo and never packs or publishes an artifact.

Local Linux-only desktop layout checks may supply just one --darwin-* input
and --test-darwin-single-arch. This emits trello-mcp_desktop_test-only.mcpb,
NOT a release asset. CI must never use this flag. Native Desktop installation,
Windows .exe handling and host settings interpolation remain human tests.
No signing, hashing of publishable metadata, or publishing occurs here.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import queue
import re
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import threading
import time
import zipfile

ROOT = Path(__file__).resolve().parents[1]
PACKAGING = ROOT / "packaging" / "mcpb"
CLI_PACKAGE = PACKAGING / "node_modules" / "@anthropic-ai" / "mcpb"
CLI_VERSION = "2.1.2"
TARGETS = ("desktop", "linux-amd64", "linux-arm64")


def run(args, **kwargs):
    """Never execute build tools with an archive or staging directory as cwd."""
    return subprocess.run([str(arg) for arg in args], cwd=ROOT, check=True,
                          timeout=120, **kwargs)


def cli(*args):
    return run(["node", CLI_PACKAGE / "dist" / "cli" / "cli.js", *args])


def check_cli():
    if not (CLI_PACKAGE / "package.json").is_file():
        raise ValueError("missing MCPB CLI; run npm ci --prefix packaging/mcpb --ignore-scripts")
    installed = json.loads((CLI_PACKAGE / "package.json").read_text())["version"]
    if installed != CLI_VERSION:
        raise ValueError(f"MCPB CLI must be {CLI_VERSION}, not {installed}")
    result = run(["node", CLI_PACKAGE / "dist" / "cli" / "cli.js", "--version"],
                 capture_output=True, text=True)
    if result.stdout.strip() != CLI_VERSION:
        raise ValueError("MCPB CLI reported an unexpected version")


def release_version(value):
    version = value.removeprefix("v")
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError("version must be a stable semantic version, optionally prefixed with v")
    metadata = json.loads((ROOT / "server.json").read_text())
    if metadata["version"] != version:
        raise ValueError("version does not match root server.json")
    return version


def manifest(version, target):
    value = json.loads((PACKAGING / "manifest.template.json").read_text())
    value["version"] = version
    if target == "desktop":
        value["compatibility"] = {"platforms": ["darwin", "win32"]}
        value["server"]["mcp_config"]["platform_overrides"] = {
            "win32": {"command": "${__dirname}/server/trello-mcp.exe"}}
    else:
        arch = target.removeprefix("linux-")
        value["compatibility"] = {"platforms": ["linux"]}
        value["display_name"] += f" (Linux {arch} beta)"
        value["description"] = f"Linux {arch} beta. " + value["description"]
        value["long_description"] = (
            f"Linux {arch} beta: manual, architecture-specific download. "
            "Requires a compatible Claude Desktop Linux beta host and secure storage.\n\n"
            + value["long_description"])
    return value


def regular_file(path):
    if path is None or not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError(f"payload must be a regular file, not a symlink: {path}")
    return path


def check_binary(path, system, arch):
    """Reject mislabeled native inputs before a release bundle can be assembled."""
    regular_file(path)
    with path.open("rb") as stream:
        header = stream.read(64)
        valid = False
        if system == "linux" and header[:6] == b"\x7fELF\x02\x01":
            valid = struct.unpack_from("<H", header, 18)[0] == {"amd64": 62, "arm64": 183}[arch]
        elif system == "darwin" and header[:4] == b"\xcf\xfa\xed\xfe":
            valid = struct.unpack_from("<I", header, 4)[0] == {
                "amd64": 0x01000007, "arm64": 0x0100000C}[arch]
        elif system == "win32" and header[:2] == b"MZ" and len(header) == 64:
            stream.seek(struct.unpack_from("<I", header, 60)[0])
            pe = stream.read(6)
            valid = pe == b"PE\x00\x00\x64\x86" and arch == "amd64"
    if not valid:
        raise ValueError(f"payload is not a {system}/{arch} native binary: {path}")


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def copy_payload(source, destination):
    regular_file(source)
    shutil.copyfile(source, destination)
    destination.chmod(0o755)


def stage_payloads(args, stage):
    server = stage / "server"
    server.mkdir()
    binary = server / "trello-mcp"
    if args.target != "desktop":
        arch = args.target.removeprefix("linux-")
        check_binary(args.linux_binary, "linux", arch)
        copy_payload(args.linux_binary, binary)
        return [(binary.name, "linux", arch)]
    check_binary(args.windows_amd64, "win32", "amd64")
    copy_payload(args.windows_amd64, server / "trello-mcp.exe")
    slices = [(args.darwin_amd64, "amd64"), (args.darwin_arm64, "arm64")]
    supplied = [(path, arch) for path, arch in slices if path is not None]
    for path, arch in supplied:
        check_binary(path, "darwin", arch)
    if args.test_darwin_single_arch:
        if len(supplied) != 1:
            raise ValueError("test-only desktop bundle requires exactly one Darwin slice")
        source, arch = supplied[0]
        copy_payload(source, binary)
        print("WARNING: single-arch Darwin payload, test-only bundle, not releasable", flush=True)
        return [(binary.name, "darwin", arch), ("trello-mcp.exe", "win32", "amd64")]
    if len(supplied) != 2:
        raise ValueError("desktop release requires both Darwin slices")
    if shutil.which("lipo") is None:
        raise ValueError("desktop release requires lipo on macOS; no single-arch fallback")
    run(["lipo", "-create", args.darwin_amd64, args.darwin_arm64, "-output", binary])
    architectures = run(["lipo", "-archs", binary], capture_output=True, text=True).stdout.split()
    if sorted(architectures) != ["arm64", "x86_64"]:
        raise ValueError(f"unexpected universal Mac slices: {architectures}")
    binary.chmod(0o755)
    return [(binary.name, "darwin", "universal"), ("trello-mcp.exe", "win32", "amd64")]


def inspect_zip(bundle, stage):
    """Check names and attributes before allowing the CLI to extract anything."""
    expected = {path.relative_to(stage).as_posix(): path
                for path in stage.rglob("*") if path.is_file()}
    seen = set()
    with zipfile.ZipFile(bundle) as archive:
        for entry in archive.infolist():
            name = entry.filename
            path = PurePosixPath(name)
            if (name in seen or path.is_absolute() or ".." in path.parts
                    or "\\" in name or ":" in name or str(path) != name):
                raise ValueError(f"unsafe or duplicate ZIP entry: {name}")
            seen.add(name)
            if name not in expected or entry.is_dir() or entry.flag_bits & 1:
                raise ValueError(f"unexpected ZIP entry: {name}")
            mode = entry.external_attr >> 16
            if stat.S_IFMT(mode) not in (0, stat.S_IFREG):
                raise ValueError(f"non-regular ZIP entry: {name}")
            wanted_mode = 0o755 if name.startswith("server/") else 0o644
            if entry.create_system != 3 or stat.S_IMODE(mode) != wanted_mode:
                raise ValueError(f"incorrect Unix ZIP mode for {name}: {oct(mode)}")
            if entry.file_size != expected[name].stat().st_size:
                raise ValueError(f"incorrect ZIP size: {name}")
            with archive.open(entry) as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            if digest != sha256(expected[name]):
                raise ValueError(f"ZIP content differs from stage: {name}")
        if seen != set(expected):
            raise ValueError("ZIP is missing approved files")


def inspect_unpacked(unpacked, stage):
    expected = {path.relative_to(stage) for path in stage.rglob("*")}
    actual = {path.relative_to(unpacked) for path in unpacked.rglob("*")}
    if expected != actual:
        raise ValueError("unpacked paths differ from stage")
    for relative in actual:
        path = unpacked / relative
        if path.is_symlink():
            raise ValueError("unpacked symlink")
        if path.is_file():
            if sha256(path) != sha256(stage / relative):
                raise ValueError(f"unpacked bytes differ: {relative}")
            if os.name != "nt" and stat.S_IMODE(path.stat().st_mode) != stat.S_IMODE((stage / relative).stat().st_mode):
                raise ValueError(f"unpacked mode differs: {relative}")


def mcp_exchange(binary, env, messages):
    """Wait for each response before stdin EOF; SDK handlers run asynchronously."""
    responses = []
    lines = queue.Queue()
    with tempfile.TemporaryFile() as errors, subprocess.Popen(
            [str(binary)], cwd=ROOT, env=env, stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=errors, text=True, encoding="utf-8") as process:
        def read_stdout():
            try:
                for line in process.stdout:
                    lines.put(line)
            finally:
                lines.put(None)

        reader = threading.Thread(target=read_stdout, daemon=True)
        reader.start()
        try:
            for message in messages:
                process.stdin.write(json.dumps(message) + "\n")
                process.stdin.flush()
                if "id" not in message:
                    continue
                deadline = time.monotonic() + 15
                while True:
                    try:
                        line = lines.get(timeout=max(0, deadline - time.monotonic()))
                    except queue.Empty as error:
                        raise ValueError("MCP smoke timed out waiting for a response") from error
                    if line is None:
                        raise ValueError("MCP process closed stdout before responding")
                    response = json.loads(line)
                    if not isinstance(response, dict):
                        raise ValueError("MCP smoke received a non-object response")
                    if "id" not in response:
                        continue
                    if response["id"] != message["id"] or "error" in response:
                        raise ValueError("MCP smoke returned an unexpected or failed response")
                    responses.append(response)
                    break
            process.stdin.close()
            if process.wait(timeout=15) != 0:
                raise ValueError("MCP process failed after smoke requests")
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)
            reader.join(timeout=5)
    return responses


def smoke(binary, version):
    """Run only local MCP methods, with a fresh home and synthetic environment."""
    with tempfile.TemporaryDirectory(prefix="trello-mcp-smoke-") as temporary:
        home = Path(temporary)
        env = {key: os.environ[key] for key in ("PATH", "SYSTEMROOT", "WINDIR") if key in os.environ}
        env.update({"HOME": str(home), "USERPROFILE": str(home),
                    "XDG_CONFIG_HOME": str(home / "config"), "APPDATA": str(home / "config"),
                    "TRELLO_API_KEY": "synthetic-api-key", "TRELLO_TOKEN": "synthetic-token",
                    "TRELLO_MCP_CREDENTIALS": str(home / "unused-credentials"),
                    "TRELLO_READ_ONLY": "false"})
        result = run([binary, "--version"], env=env, capture_output=True, text=True)
        if result.stdout.strip() not in (f"trello-mcp {version}", f"trello-mcp v{version}"):
            raise ValueError("extracted binary version differs from manifest")
        messages = [
            {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                "protocolVersion": "2025-03-26", "capabilities": {},
                "clientInfo": {"name": "synthetic-bundle-test", "version": "1"}}},
            {"jsonrpc": "2.0", "method": "notifications/initialized"},
            {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}},
            {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {
                "name": "server_health", "arguments": {}}},
        ]
        responses = mcp_exchange(binary, env, messages)
        by_id = {response["id"]: response for response in responses if "id" in response}
        if set(by_id) != {1, 2, 3} or any("error" in response for response in by_id.values()):
            raise ValueError("MCP smoke did not return three successful responses")
        implementation = by_id[1]["result"]["serverInfo"]
        if implementation["name"] != "trello-mcp" or implementation["version"].removeprefix("v") != version:
            raise ValueError("MCP initialize implementation mismatch")
        tools = by_id[2]["result"]["tools"]
        if len(tools) <= 1 or "server_health" not in {tool["name"] for tool in tools}:
            raise ValueError("MCP tools/list is missing configured tools")
        health = by_id[3]["result"]
        if health.get("isError") or health.get("structuredContent") is not None:
            raise ValueError("MCP health returned an error or structured content")
        content = health["content"]
        if len(content) != 1 or content[0]["type"] != "text":
            raise ValueError("MCP health did not return one text result")
        health_text = content[0]["text"]
        health_value = json.loads(health_text)
        if (health_value != {"ok": True, "credentialSource": "env",
                             "access": "unverified; call workspace_find"}
                or health_text != json.dumps(health_value, separators=(",", ":"))):
            raise ValueError("MCP health did not return compact local success with env source and unverified access")
        print("PASS extracted payload: --version, MCP initialize, tools/list, server_health", flush=True)


def smoke_payloads(unpacked, payloads, version):
    machine = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine().lower())
    system = {"Linux": "linux", "Darwin": "darwin", "Windows": "win32"}.get(platform.system())
    count = 0
    for name, target_system, arch in payloads:
        if system != target_system or arch not in (machine, "universal"):
            print(f"SKIP native smoke for {target_system}/{arch}: host is {system}/{machine}", flush=True)
            continue
        smoke(unpacked / "server" / name, version)
        count += 1
    return count


def build(args):
    version = release_version(args.version)
    check_cli()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    suffix = "desktop_test-only" if args.test_darwin_single_arch else args.target.replace("-", "_")
    output = args.output_dir / f"trello-mcp_{suffix}.mcpb"
    if output.exists() or output.is_symlink():
        raise ValueError(f"refusing to replace existing bundle: {output}")
    with tempfile.TemporaryDirectory(prefix="trello-mcp-stage-") as staging, \
            tempfile.TemporaryDirectory(prefix="trello-mcp-packed-") as packed, \
            tempfile.TemporaryDirectory(prefix="trello-mcp-unpacked-") as extracted:
        stage, unpacked = Path(staging), Path(extracted)
        payloads = stage_payloads(args, stage)
        (stage / "manifest.json").write_text(json.dumps(manifest(version, args.target), indent=2) + "\n")
        shutil.copyfile(ROOT / "LICENSE", stage / "LICENSE")
        shutil.copyfile(PACKAGING / "icon.png", stage / "icon.png")
        for name in ("manifest.json", "LICENSE", "icon.png"):
            (stage / name).chmod(0o644)
        cli("validate", stage / "manifest.json")
        bundle = Path(packed) / output.name
        cli("pack", stage, bundle)
        inspect_zip(bundle, stage)
        cli("unpack", bundle, unpacked)
        inspect_unpacked(unpacked, stage)
        cli("validate", unpacked / "manifest.json")
        if smoke_payloads(unpacked, payloads, version) == 0 and args.require_smoke:
            raise ValueError("--require-smoke requested but no payload matches this host")
        # Create exclusively so a concurrent build cannot replace an existing asset.
        with output.open("xb") as destination, bundle.open("rb") as source:
            shutil.copyfileobj(source, destination)
    print(f"PASS bundle: {output}", flush=True)
    return output


def parser():
    value = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    value.add_argument("--version", required=True)
    value.add_argument("--target", choices=TARGETS)
    value.add_argument("--output-dir", type=Path)
    value.add_argument("--smoke-binary", type=Path)
    for name in ("darwin-amd64", "darwin-arm64", "windows-amd64", "linux-binary"):
        value.add_argument("--" + name, type=Path)
    value.add_argument("--test-darwin-single-arch", action="store_true")
    value.add_argument("--require-smoke", action="store_true")
    return value


def main():
    args = parser().parse_args()
    if args.smoke_binary:
        if any((args.target, args.output_dir, args.linux_binary, args.darwin_amd64,
                args.darwin_arm64, args.windows_amd64, args.test_darwin_single_arch, args.require_smoke)):
            raise ValueError("--smoke-binary accepts only --version, not bundle arguments")
        binary = regular_file(args.smoke_binary.absolute())
        smoke(binary, release_version(args.version))
        return
    if args.target is None or args.output_dir is None:
        raise ValueError("bundle build requires --target and --output-dir")
    if args.target == "desktop":
        if not args.windows_amd64 or args.linux_binary:
            raise ValueError("desktop requires --windows-amd64 and forbids --linux-binary")
    elif not args.linux_binary or any((args.darwin_amd64, args.darwin_arm64, args.windows_amd64, args.test_darwin_single_arch)):
        raise ValueError("Linux target requires only --linux-binary, not desktop inputs")
    for name in ("output_dir", "darwin_amd64", "darwin_arm64", "windows_amd64", "linux_binary"):
        path = getattr(args, name)
        if path is not None:
            # absolute() preserves the final symlink so regular_file can reject it.
            setattr(args, name, path.absolute())
    build(args)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError, struct.error, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        print(f"FAIL MCPB build: {error}", file=sys.stderr)
        sys.exit(1)
