#!/usr/bin/env python3
"""Synthetic release assembly regressions; no credentials or external calls."""

import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import struct
import tarfile
import tempfile
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

    def test_workflow_keeps_preview_out_of_normal_release(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        publication = workflow.split('gh release create "$RELEASE_TAG"', 1)[1]
        self.assertIn('dist/*.tar.gz dist/*.zip dist/checksums.txt', publication)
        self.assertNotIn('.mcpb', publication)
        self.assertNotIn('server.json', publication)
        self.assertNotIn(' generate ', workflow)
        self.assertIn('name: mcpb-bundles', workflow)
        self.assertIn('needs: [checks, native, desktop, desktop-windows]', workflow)
        self.assertIn('notes --output notes.md', workflow)
        self.assertIn('--notes-file notes.md', publication)

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
        self.assertEqual(package["transport"], {"type": "stdio"})
        text = release.checksums(self.directory, "registry", verify=True)
        self.assertEqual(len(text.splitlines()), 10)
        self.assertNotIn("  checksums.txt", text)

    def test_generated_invalid_package_semantics(self):
        self.all_assets()
        value = release.generate(self.metadata, self.directory)
        for key, invalid in (("identifier", "https://example.com/wrong.mcpb"), ("version", "99.0.0"),
                             ("fileSha256", "A" * 64), ("registryType", "npm")):
            altered = copy.deepcopy(value)
            altered["packages"][0][key] = invalid
            with self.subTest(key=key), self.assertRaises(ValueError):
                release.validate_generated(altered, self.metadata, self.directory)
        altered = copy.deepcopy(value)
        altered["packages"].append(copy.deepcopy(altered["packages"][0]))
        with self.assertRaises(ValueError):
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
