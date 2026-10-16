#!/usr/bin/env python3
"""Generate and validate normal-release metadata and bytes. Run with python3 -I.

The metadata-only template is packaging/registry/server.template.json. For each
approved normal vX.Y.Z tag, inspect final MCPBs, generate metadata and unified
checksums, verify them, and publisher-validate the generated server.json before
approved GitHub publication. The registry-publish command independently verifies
public release bytes and validates downloaded metadata before registry submission.

Inspect all three MCPBs and include all in the unified checksums. Registry
installation metadata lists only trello-mcp_desktop.mcpb (macOS universal and
Windows amd64). Linux MCPBs remain owner-untested beta downloads, excluded until
support is explicitly promoted with owner evidence. This is our discovery/support
policy, not a Registry format limitation.

Normal vX.Y.Z releases with major >= 1 publish automatically through registry.yml
using GitHub OIDC, no stored secrets, and the checksum-pinned publisher. Publication
is idempotent and verifies the exact name, version and packages through the official
Registry API. Always publish the absolute path of the downloaded server.json.
Deprecation remains a human step. Never inspect token files, add registry secrets
or PATs, or retroactively submit pre-1.0 releases.
See AGENTS.md.
"""

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
import platform
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.request import urlopen
import zipfile

ROOT = Path(__file__).resolve().parents[1]
CLI_ASSETS = tuple(f"trello-mcp_{system}_{arch}.{extension}"
                   for system, extension in (("linux", "tar.gz"), ("darwin", "tar.gz"), ("windows", "zip"))
                   for arch in ("amd64", "arm64"))
BUNDLES = {"trello-mcp_desktop.mcpb": ["darwin", "win32"],
           "trello-mcp_linux_amd64.mcpb": ["linux"],
           "trello-mcp_linux_arm64.mcpb": ["linux"]}
REPOSITORY = "https://github.com/avhn/trello-mcp"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"duplicate JSON key: {key}")
        result[key] = value
    return result


def read_json(value):
    result = json.loads(value, object_pairs_hook=unique_object)
    require(isinstance(result, dict), "JSON document must be an object")
    return result


def source_metadata(path=ROOT / "packaging/registry/server.template.json", tag=None):
    value = read_json(path.read_text())
    version = value.get("version", "")
    require(re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version), "invalid stable version")
    require(tag is None or tag == f"v{version}", "release tag/version mismatch")
    require("packages" not in value and "remotes" not in value, "source must be metadata-only")
    require(value.get("name") == "io.github.avhn/trello-mcp", "unexpected server identity")
    require(value.get("repository") == {"url": REPOSITORY, "source": "github"}, "unexpected repository")
    return value


def digest(path):
    require(path.is_file() and not path.is_symlink(), f"missing or nonregular artifact: {path.name}")
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def inspect_bundle(path, version, extract=None):
    """Validate layout before optional extraction, without running archive code."""
    require(path.name in BUNDLES, "unknown bundle basename")
    digest(path)
    desktop = path.name == "trello-mcp_desktop.mcpb"
    expected = {"manifest.json", "LICENSE", "icon.png", "server/trello-mcp"}
    if desktop:
        expected.add("server/trello-mcp.exe")
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        require(len(names) == len(set(names)), "duplicate ZIP entry")
        require(set(names) == expected, "invalid bundle layout")
        for info in archive.infolist():
            mode = info.external_attr >> 16
            require(info.create_system == 3 and not info.flag_bits & 1, "invalid ZIP origin or encryption")
            require(stat.S_IFMT(mode) in (0, stat.S_IFREG), "nonregular ZIP entry")
            require(stat.S_IMODE(mode) == (0o755 if info.filename.startswith("server/") else 0o644),
                    "invalid ZIP permissions")
            require(info.file_size <= 128 * 1024 * 1024, "oversized ZIP entry")
        manifest = read_json(archive.read("manifest.json"))
        require(manifest.get("manifest_version") == "0.3", "invalid manifest format")
        require(manifest.get("name") == "trello-mcp" and manifest.get("version") == version,
                "bundle identity/version mismatch")
        require(manifest.get("compatibility", {}).get("platforms") == BUNDLES[path.name], "bundle OS mismatch")
        server = manifest.get("server", {})
        config = server.get("mcp_config", {})
        require(server.get("type") == "binary" and server.get("entry_point") == "server/trello-mcp",
                "invalid native entry point")
        require(config.get("command") == "${__dirname}/server/trello-mcp", "invalid native command")
        override = {"win32": {"command": "${__dirname}/server/trello-mcp.exe"}} if desktop else {}
        require(config.get("platform_overrides", {}) == override, "invalid platform override")
        settings = manifest.get("user_config", {})
        env = config.get("env", {})
        for key, variable in (("api_key", "TRELLO_API_KEY"), ("token", "TRELLO_TOKEN")):
            setting = settings.get(key, {})
            require(setting.get("sensitive") is True and setting.get("required") is True
                    and setting.get("type") == "string" and "default" not in setting,
                    "credentials must be required, sensitive and without defaults")
            require(env.get(variable) == "${user_config." + key + "}", "invalid credential mapping")
        payload = archive.read("server/trello-mcp")
        if desktop:
            require(len(payload) >= 48 and payload[:4] == b"\xca\xfe\xba\xbe", "missing universal Mach-O")
            count = struct.unpack_from(">I", payload, 4)[0]
            require(count == 2 and {struct.unpack_from(">I", payload, 8 + i * 20)[0] for i in range(2)}
                    == {0x01000007, 0x0100000C}, "invalid universal slices")
            windows = archive.read("server/trello-mcp.exe")
            require(len(windows) >= 64 and windows[:2] == b"MZ", "missing Windows executable")
            offset = struct.unpack_from("<I", windows, 60)[0]
            require(windows[offset:offset + 6] == b"PE\x00\x00\x64\x86", "invalid Windows architecture")
        else:
            machine = 62 if "amd64" in path.name else 183
            require(len(payload) >= 20 and payload[:6] == b"\x7fELF\x02\x01"
                    and struct.unpack_from("<H", payload, 18)[0] == machine, "invalid Linux architecture")
        if extract is not None:
            require(extract.is_dir() and not extract.is_symlink() and not any(extract.iterdir()),
                    "extraction requires a new empty directory")
            for info in archive.infolist():
                destination = extract / info.filename
                destination.parent.mkdir(parents=True, exist_ok=True)
                with destination.open("xb") as stream:
                    stream.write(archive.read(info))
                destination.chmod(stat.S_IMODE(info.external_attr >> 16))
    return manifest


def expected_assets(kind):
    return {"cli": CLI_ASSETS, "mcpb": tuple(BUNDLES),
            "registry": CLI_ASSETS + tuple(BUNDLES) + ("server.json",)}[kind]


def checksums(directory, kind, verify=False):
    names = expected_assets(kind)
    require({path.name for path in directory.iterdir()} == set(names) | ({"checksums.txt"} if (directory / "checksums.txt").exists() else set()),
            "missing, duplicate or unexpected artifacts")
    text = "".join(f"{digest(directory / name)}  {name}\n" for name in sorted(names))
    destination = directory / "checksums.txt"
    require(not destination.is_symlink() and (not destination.exists() or destination.is_file()),
            "checksums.txt must be a regular file")
    if verify:
        require(destination.read_text() == text, "stale or invalid checksums")
    else:
        destination.write_text(text)
    return text


def validate_generated(value, metadata, directory):
    expected = dict(metadata)
    expected["packages"] = [{"registryType": "mcpb",
                             "identifier": f"{REPOSITORY}/releases/download/v{metadata['version']}/trello-mcp_desktop.mcpb",
                             "version": metadata["version"],
                             "fileSha256": digest(directory / "trello-mcp_desktop.mcpb"),
                             "transport": {"type": "stdio"}}]
    require(value == expected, "invalid generated package metadata or stale bundle hash")
    for name in BUNDLES:
        inspect_bundle(directory / name, metadata["version"])


def generate(metadata, directory):
    require({path.name for path in directory.iterdir()} == set(CLI_ASSETS) | set(BUNDLES),
            "generation requires exactly six archives and three bundles")
    for name in CLI_ASSETS:
        digest(directory / name)
    value = dict(metadata)
    value["packages"] = [{"registryType": "mcpb",
                          "identifier": f"{REPOSITORY}/releases/download/v{metadata['version']}/trello-mcp_desktop.mcpb",
                          "version": metadata["version"],
                          "fileSha256": digest(directory / "trello-mcp_desktop.mcpb"),
                          "transport": {"type": "stdio"}}]
    validate_generated(value, metadata, directory)
    with (directory / "server.json").open("x") as stream:
        stream.write(json.dumps(value, indent=2) + "\n")
    checksums(directory, "registry")
    return value


def tag_notes(tag, repository=ROOT):
    require(re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag), "invalid release tag")
    kind = subprocess.check_output(["git", "cat-file", "-t", f"refs/tags/{tag}"], cwd=repository, text=True).strip()
    require(kind == "tag", "release requires an annotated signed tag message")
    # Preserve the existing signed-tag subject/body extraction, excluding signature.
    notes = subprocess.check_output(["git", "for-each-ref", "--format=%(contents:subject)%0a%0a%(contents:body)",
                                     f"refs/tags/{tag}"], cwd=repository, text=True)
    require(len([line for line in notes.splitlines() if line.strip()]) >= 3,
            f"tag {tag} needs a release description in its message")
    require(not re.search(r"\b(PENDING|TODO|TBD|FIXME)\b|\{\{.*?\}\}|(?i:<[^>]*(?:placeholder|verified value)[^>]*>)", notes),
            "unresolved release-text placeholder")
    return notes


@contextmanager
def verified_publisher():
    """Yield the pinned Linux CI binary and an isolated, temporary auth environment."""
    require(platform.system() == "Linux" and platform.machine().lower() in ("amd64", "x86_64"),
            "publisher validation requires Linux amd64")
    url = "https://github.com/modelcontextprotocol/registry/releases/download/v1.8.1/mcp-publisher_linux_amd64.tar.gz"
    checksum = "a06c9096dcb9727c13555b6be26c7effa707b01f06a4c561ba7a3635443cf2cc"
    with tempfile.TemporaryDirectory(prefix="publisher-download-") as download, \
            tempfile.TemporaryDirectory(prefix="publisher-extracted-") as extracted, \
            tempfile.TemporaryDirectory(prefix="publisher-home-") as home:
        archive_path = Path(download) / "publisher.tar.gz"
        subprocess.run(["curl", "--fail", "--location", "--silent", "--show-error", "--retry", "3",
                        "--output", str(archive_path), url], cwd=ROOT, check=True, timeout=120)
        require(digest(archive_path) == checksum, "publisher download checksum mismatch")
        binary = Path(extracted) / "mcp-publisher"
        with tarfile.open(archive_path, "r:gz") as archive:
            require(sorted(archive.getnames()) == ["LICENSE", "README.md", "mcp-publisher"], "invalid publisher archive")
            member = archive.getmember("mcp-publisher")
            require(member.isfile() and member.size <= 128 * 1024 * 1024, "invalid publisher binary")
            with archive.extractfile(member) as source, binary.open("xb") as destination:
                destination.write(source.read())
        binary.chmod(0o755)
        env = os.environ.copy()
        for variable in ("HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME",
                         "XDG_RUNTIME_DIR", "APPDATA", "LOCALAPPDATA", "USERPROFILE"):
            directory = Path(home) / variable.lower()
            directory.mkdir(mode=0o700)
            env[variable] = str(directory)
        subprocess.run([str(binary), "--help"], cwd=ROOT, env=env, check=True, timeout=30)
        yield binary, env


def publisher_validate(path, publisher=None):
    """Public validation only, optionally sharing an already verified publisher."""
    if publisher is None:
        with verified_publisher() as publisher:
            publisher_validate(path, publisher)
        return
    binary, env = publisher
    subprocess.run([str(binary), "validate", str(path.resolve())], cwd=ROOT, env=env, check=True, timeout=120)


def registry_record(version):
    url = f"https://registry.modelcontextprotocol.io/v0.1/servers/io.github.avhn%2Ftrello-mcp/versions/{version}"
    try:
        with urlopen(url, timeout=30) as response:
            require(response.status == 200, f"Registry API returned HTTP {response.status}")
            return read_json(response.read())
    except HTTPError as error:
        error.close()
        if error.code == 404:
            return None
        raise ValueError(f"Registry API returned HTTP {error.code}") from error
    except URLError as error:
        raise ValueError("Registry API request failed") from error


def require_registry_match(record, expected):
    server = record.get("server")
    require(isinstance(server, dict) and all(server.get(key) == expected[key]
            for key in ("name", "version", "packages")), "Registry name/version/packages mismatch")


def registry_publish(tag, metadata):
    """Verify public bytes before idempotent OIDC publication of stable v1+ releases."""
    require(isinstance(tag, str) and re.fullmatch(r"v[1-9][0-9]*\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag),
            "registry publication requires a stable vX.Y.Z tag with major >= 1")
    require(tag == "v" + metadata["version"], "release tag/version mismatch")
    details = read_json(subprocess.check_output(
        ["gh", "release", "view", tag, "--repo", REPOSITORY, "--json", "tagName,isDraft,isPrerelease,assets"],
        cwd=ROOT, text=True, timeout=60))
    require(details.get("tagName") == tag and details.get("isDraft") is False
            and details.get("isPrerelease") is False, "release must exist and not be draft or prerelease")
    names = [asset["name"] for asset in details["assets"]]
    expected_names = set(expected_assets("registry")) | {"checksums.txt"}
    require(len(names) == len(expected_names) and set(names) == expected_names,
            "release requires exactly the 11 expected assets")
    with tempfile.TemporaryDirectory(prefix="registry-download-") as download, \
            tempfile.TemporaryDirectory(prefix="registry-regenerated-") as regenerated:
        directory = Path(download)
        subprocess.run(["gh", "release", "download", tag, "--repo", REPOSITORY, "--dir", str(directory)],
                       cwd=ROOT, check=True, timeout=180)
        checksums(directory, "registry", verify=True)
        for name in CLI_ASSETS + tuple(BUNDLES):
            shutil.copyfile(directory / name, Path(regenerated) / name)
        expected = generate(metadata, Path(regenerated))
        for name in ("server.json", "checksums.txt"):
            require((directory / name).read_bytes() == (Path(regenerated) / name).read_bytes(),
                    f"downloaded {name} differs from regenerated public bytes")
        path = (directory / "server.json").resolve()
        with verified_publisher() as publisher:
            publisher_validate(path, publisher)
            record = registry_record(metadata["version"])
            if record is not None:
                require_registry_match(record, expected)
                print(f"Registry {metadata['version']} already published")
                return
            binary, env = publisher
            subprocess.run([str(binary), "login", "github-oidc"], cwd=ROOT, env=env, check=True, timeout=120)
            subprocess.run([str(binary), "publish", str(path)], cwd=ROOT, env=env, check=True, timeout=180)
            for attempt in range(10):
                record = registry_record(metadata["version"])
                if record is not None:
                    require_registry_match(record, expected)
                    official = record.get("_meta", {}).get("io.modelcontextprotocol.registry/official", {})
                    if official.get("status") == "active":
                        print(f"Registry {metadata['version']} published and verified active")
                        return
                if attempt < 9:
                    time.sleep(6)
            raise ValueError("Registry publication did not become active after 10 checks")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--metadata", type=Path, default=ROOT / "packaging/registry/server.template.json")
    parser.add_argument("--tag")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("version")
    commands.add_parser("registry-publish")
    command = commands.add_parser("publisher-validate")
    command.add_argument("--input", type=Path, help="Generated metadata beside final assets and unified checksums; defaults to the metadata-only source")
    for name in ("checksums", "generate"):
        command = commands.add_parser(name)
        command.add_argument("--directory", required=True, type=Path)
        if name == "checksums":
            command.add_argument("--kind", choices=("cli", "mcpb", "registry"), required=True)
            command.add_argument("--verify", action="store_true")
    command = commands.add_parser("inspect-bundle")
    command.add_argument("bundle", type=Path)
    command.add_argument("--extract", type=Path)
    command = commands.add_parser("notes")
    command.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    metadata = source_metadata(args.metadata, args.tag)
    if args.command == "version":
        print(metadata["version"])
    elif args.command == "registry-publish":
        registry_publish(args.tag, metadata)
    elif args.command == "publisher-validate":
        if args.input is not None:
            validate_generated(read_json(args.input.read_text()), metadata, args.input.parent)
            checksums(args.input.parent, "registry", verify=True)
        publisher_validate(args.input or args.metadata)
    elif args.command == "checksums":
        checksums(args.directory, args.kind, args.verify)
    elif args.command == "generate":
        generate(metadata, args.directory)
    elif args.command == "inspect-bundle":
        inspect_bundle(args.bundle, metadata["version"], args.extract)
    else:
        require(args.tag is not None, "notes requires --tag")
        args.output.write_text(tag_notes(args.tag))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError, struct.error, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        print(f"FAIL release manifest: {error}", file=sys.stderr)
        sys.exit(1)
