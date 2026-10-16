#!/usr/bin/env python3
"""Focused bundle tests: python3 -I tests/package_mcpb_test.py.

The pinned-CLI round trip requires npm ci --prefix packaging/mcpb --ignore-scripts.
Native executable smoke tests run in the builder on a matching host, not on the
synthetic binary headers used in this suite.
"""

import argparse
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import struct
import subprocess
import tempfile
import sys
import unittest
from unittest import mock
import zipfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("package_mcpb", ROOT / "scripts" / "package_mcpb.py")
packaging = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(packaging)


def native_header(system, arch):
    value = bytearray(128)
    if system == "linux":
        value[:6] = b"\x7fELF\x02\x01"
        struct.pack_into("<H", value, 18, {"amd64": 62, "arm64": 183}[arch])
    elif system == "darwin":
        value[:4] = b"\xcf\xfa\xed\xfe"
        struct.pack_into("<I", value, 4, {"amd64": 0x01000007, "arm64": 0x0100000C}[arch])
    else:
        value[:2] = b"MZ"
        struct.pack_into("<I", value, 60, 64)
        value[64:70] = b"PE\x00\x00\x64\x86"
    return bytes(value)


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="trello-mcp-package-test-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.stage = self.directory / "stage"
        self.stage.mkdir()
        (self.stage / "server").mkdir()
        for name, content in (("manifest.json", b"{}"), ("LICENSE", b"synthetic license"),
                              ("icon.png", b"<svg/>"), ("server/trello-mcp", b"synthetic binary")):
            path = self.stage / name
            path.write_bytes(content)
            path.chmod(0o755 if name.startswith("server/") else 0o644)

    def args(self, **changes):
        values = dict(target="linux-arm64", version=json.loads((ROOT / "server.json").read_text())["version"],
                      output_dir=self.directory / "out", linux_binary=None, darwin_amd64=None,
                      darwin_arm64=None, windows_amd64=None, test_darwin_single_arch=False,
                      require_smoke=False)
        values.update(changes)
        return argparse.Namespace(**values)

    def payload(self, system="linux", arch="arm64"):
        path = self.directory / f"{system}-{arch}"
        path.write_bytes(native_header(system, arch))
        return path

    def zip(self, *, extra=None, omit=None, mode=None, duplicate=False, altered=False):
        path = self.directory / "bundle.mcpb"
        with zipfile.ZipFile(path, "w") as archive:
            for source in self.stage.rglob("*"):
                if not source.is_file():
                    continue
                name = source.relative_to(self.stage).as_posix()
                if name == omit:
                    continue
                info = zipfile.ZipInfo(name)
                info.create_system = 3
                info.external_attr = (mode if mode is not None else source.stat().st_mode) << 16
                archive.writestr(info, b"X" * source.stat().st_size if altered else source.read_bytes())
            if extra is not None:
                info = zipfile.ZipInfo(extra)
                info.create_system = 3
                info.external_attr = (stat.S_IFREG | 0o644) << 16
                archive.writestr(info, "unexpected")
            if duplicate:
                with self.assertWarns(UserWarning):
                    archive.writestr("LICENSE", "duplicate")
        return path

    def test_manifest_identity_and_platforms(self):
        for target in packaging.TARGETS:
            with self.subTest(target=target):
                manifest = packaging.manifest("6.7.8", target)
                self.assertEqual(manifest["version"], "6.7.8")
                self.assertEqual(manifest["name"], "trello-mcp")
                self.assertEqual(manifest["manifest_version"], "0.3")
                self.assertEqual(manifest["server"]["type"], "binary")
                self.assertEqual(manifest["server"]["entry_point"], "server/trello-mcp")
                self.assertEqual(manifest["server"]["mcp_config"]["command"], "${__dirname}/server/trello-mcp")
                self.assertEqual([tool["name"] for tool in manifest["tools"]], ["server_health"])
                self.assertIs(manifest["tools_generated"], True)
                self.assertNotIn("runtimes", manifest["compatibility"])
                if target == "desktop":
                    self.assertEqual(manifest["compatibility"]["platforms"], ["darwin", "win32"])
                    self.assertEqual(manifest["server"]["mcp_config"]["platform_overrides"], {
                        "win32": {"command": "${__dirname}/server/trello-mcp.exe"}})
                else:
                    self.assertEqual(manifest["compatibility"]["platforms"], ["linux"])
                    self.assertNotIn("platform_overrides", manifest["server"]["mcp_config"])
                    self.assertIn("beta", manifest["display_name"])
                    self.assertIn(target.removeprefix("linux-"), manifest["display_name"])

    def test_supported_desktop_and_explicit_linux_beta_help(self):
        for target in packaging.TARGETS:
            with self.subTest(target=target):
                manifest = packaging.manifest("6.7.8", target)
                self.assertIn("macOS and Windows are supported.", manifest["long_description"])
                for field in ("display_name", "description", "long_description"):
                    self.assertNotIn("preview", manifest[field].lower())
                    self.assertNotIn("pending", manifest[field].lower())
                if target == "desktop":
                    self.assertEqual(manifest["display_name"], "trello-mcp")
                    for field in ("display_name", "description", "long_description"):
                        self.assertNotIn("beta", manifest[field].lower())
                else:
                    arch = target.removeprefix("linux-")
                    self.assertEqual(manifest["display_name"], f"trello-mcp (Linux {arch} beta)")
                    self.assertTrue(manifest["description"].startswith(f"Linux {arch} beta. "))
                    self.assertTrue(manifest["long_description"].startswith(
                        f"Linux {arch} beta: manual, architecture-specific download. "))
                    self.assertIn("compatible Claude Desktop Linux beta host and secure storage",
                                  manifest["long_description"])

    def test_windows_transfer_warnings_cannot_be_enabled_by_settings(self):
        for target in packaging.TARGETS:
            with self.subTest(target=target):
                manifest = packaging.manifest("6.7.8", target)
                for setting, operation in (("download_dir", "attachment downloads"),
                                           ("upload_dir", "local path uploads")):
                    description = manifest["user_config"][setting]["description"]
                    self.assertIn(f"Windows refuses {operation}", description)
                    self.assertIn("even when this directory is set", description)
                    self.assertIn("configuring it cannot enable them", description)
                    self.assertIn("Restart", description)
                help_text = manifest["long_description"]
                self.assertIn("Windows refuses local path uploads and attachment downloads", help_text)
                self.assertIn("even when directories are set; configuring directories cannot enable them", help_text)
                for text in (help_text, manifest["user_config"]["upload_dir"]["description"]):
                    self.assertIn("Base64 uploads and URL attachments remain alternatives", text)
                    self.assertIn("subject to write permissions and tool-profile policy", text)

    def test_settings_and_substitution_contract(self):
        manifest = packaging.manifest("6.7.8", "desktop")
        config = manifest["user_config"]
        for name in ("api_key", "token"):
            self.assertEqual(config[name]["type"], "string")
            self.assertIs(config[name]["sensitive"], True)
            self.assertIs(config[name]["required"], True)
            self.assertNotIn("default", config[name])
        self.assertIs(config["read_only"]["default"], False)
        self.assertEqual(config["allowed_workspaces"]["default"], "")
        for name in ("download_dir", "upload_dir"):
            self.assertEqual(config[name]["type"], "directory")
            self.assertEqual(config[name]["default"], "")
        env = manifest["server"]["mcp_config"]["env"]
        self.assertEqual(env, {"TRELLO_" + name.upper(): "${user_config." + name + "}" for name in config})
        # Synthetic interpolation is a contract test, not evidence about Desktop coercion.
        settings = {name: value.get("default", "synthetic-" + name) for name, value in config.items()}
        settings["read_only"] = True
        interpolated = {name: str(settings[template[14:-1]]).lower()
                        if isinstance(settings[template[14:-1]], bool)
                        else settings[template[14:-1]] for name, template in env.items()}
        self.assertEqual(interpolated["TRELLO_READ_ONLY"], "true")
        self.assertEqual(interpolated["TRELLO_UPLOAD_DIR"], "")
        self.assertEqual(interpolated["TRELLO_API_KEY"], "synthetic-api_key")

    def test_help_explains_browser_only_auth_scope_and_revocation(self):
        manifest = packaging.manifest("6.7.8", "desktop")
        help_text = manifest["long_description"]
        for required in ("https://trello.com/power-ups/admin", "key=YOUR_KEY&scope=read,write&expiration=30days",
                         "scope=read", "masked", "30 days", "Revoke", "does not revoke",
                         "not the token", "OS secure storage", "manual updates", "restart",
                         "Extension Developer > Install Extension"):
            self.assertIn(required, help_text)
        self.assertEqual(manifest["privacy_policies"], ["https://www.atlassian.com/legal/privacy-policy"])

    def test_help_verifies_desktop_access_without_cli_credentials(self):
        manifest = packaging.manifest("6.7.8", "desktop")
        self.assertEqual(manifest["tools"][0]["description"],
                         "Local config health, not Trello auth; verify with workspace_find. "
                         "Policy shows access restrictions.")
        help_text = manifest["long_description"]
        for required in ("basic checks local credential presence/source",
                         "metadata shows the installed version", "policy shows effective restrictions",
                         "Health does not verify Trello access", "workspace_find {} for a real Trello read",
                         "successful empty items result proves that upstream read worked",
                         "not that a workspace selector matches", "Authentication rejection differs",
                         "permission, network or rate-limit failures", "permitted existing upstream read such as board_find",
                         "health-only profile cannot verify access", "change the profile and reconnect",
                         "never bypass restrictions", "No CLI auth/status or credential copying is required",
                         "CLI status describes only that CLI invocation, not Desktop credentials",
                         "remove or disable the extension in Settings > Extensions",
                         "retained sensitive settings using supported controls",
                         "Do not use CLI logout as Desktop cleanup",
                         "removal does not promise deletion of OS secure-storage entries"):
            self.assertIn(required, help_text)
        self.assertLess(help_text.index("basic checks"), help_text.index("metadata shows"))
        self.assertLess(help_text.index("metadata shows"), help_text.index("policy shows"))
        self.assertLess(help_text.index("policy shows"), help_text.index("Then call workspace_find"))

    def test_native_headers_and_symlink_rejection(self):
        for system, arches in (("linux", ("amd64", "arm64")), ("darwin", ("amd64", "arm64")),
                               ("win32", ("amd64",))):
            for arch in arches:
                with self.subTest(system=system, arch=arch):
                    path = self.payload(system, arch)
                    packaging.check_binary(path, system, arch)
                    with self.assertRaises(ValueError):
                        packaging.check_binary(path, "linux" if system != "linux" else "darwin", arch)
        with self.assertRaises(ValueError):
            packaging.check_binary(self.payload("linux", "amd64"), "linux", "arm64")
        symlink = self.directory / "link"
        try:
            symlink.symlink_to(self.payload())
        except OSError as error:
            self.skipTest(f"symlink unavailable on this host: {error}")
        with self.assertRaises(ValueError):
            packaging.check_binary(symlink, "linux", "arm64")

    def test_zip_allowlist_bytes_and_modes(self):
        packaging.inspect_zip(self.zip(), self.stage)
        invalid = [dict(extra="../escaped"), dict(extra="/absolute"), dict(extra="server\\evil"),
                   dict(extra="C:/absolute"), dict(extra="server//evil"), dict(extra=".env"),
                   dict(extra="server/"), dict(omit="LICENSE"), dict(duplicate=True),
                   dict(mode=stat.S_IFLNK | 0o755), dict(mode=0o644), dict(altered=True)]
        for options in invalid:
            with self.subTest(options=options), self.assertRaises(ValueError):
                packaging.inspect_zip(self.zip(**options), self.stage)

    def test_unpacked_modes_bytes_and_extra_paths(self):
        import shutil
        unpacked = self.directory / "unpacked"
        shutil.copytree(self.stage, unpacked)
        packaging.inspect_unpacked(unpacked, self.stage)
        (unpacked / "unexpected").write_text("bad")
        with self.assertRaises(ValueError):
            packaging.inspect_unpacked(unpacked, self.stage)
        (unpacked / "unexpected").unlink()
        (unpacked / "LICENSE").write_text("tampered")
        with self.assertRaises(ValueError):
            packaging.inspect_unpacked(unpacked, self.stage)

    def test_version_must_match_root_metadata(self):
        version = self.args().version
        self.assertEqual(packaging.release_version("v" + version), version)
        for value in ("999.999.999", "01.2.3", "1.2.3-rc1", "1.2", "PENDING"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                packaging.release_version(value)

    def test_pinned_dependency_and_lock_agree(self):
        package = json.loads((packaging.PACKAGING / "package.json").read_text())
        lock = json.loads((packaging.PACKAGING / "package-lock.json").read_text())
        self.assertEqual(package["devDependencies"]["@anthropic-ai/mcpb"], "2.1.2")
        self.assertEqual(lock["packages"][""]["devDependencies"], package["devDependencies"])
        self.assertEqual(lock["packages"]["node_modules/@anthropic-ai/mcpb"]["version"], "2.1.2")
        with mock.patch.object(packaging, "CLI_VERSION", "invalid"), self.assertRaises(ValueError):
            packaging.check_cli()

    def test_desktop_release_never_falls_back_without_lipo(self):
        stage = self.directory / "desktop"
        stage.mkdir()
        args = self.args(target="desktop", darwin_amd64=self.payload("darwin", "amd64"),
                         darwin_arm64=self.payload("darwin", "arm64"),
                         windows_amd64=self.payload("win32", "amd64"))
        with mock.patch.object(packaging.shutil, "which", return_value=None), self.assertRaisesRegex(ValueError, "requires lipo"):
            packaging.stage_payloads(args, stage)

    def test_universal_build_checks_both_slices(self):
        args = self.args(target="desktop", darwin_amd64=self.payload("darwin", "amd64"),
                         darwin_arm64=self.payload("darwin", "arm64"),
                         windows_amd64=self.payload("win32", "amd64"))
        for slices, valid in (("x86_64 arm64\n", True), ("arm64\n", False), ("arm64 arm64 x86_64\n", False)):
            with self.subTest(slices=slices), tempfile.TemporaryDirectory() as temporary:
                def lipo(command, **kwargs):
                    if command[1] == "-create":
                        Path(command[-1]).write_bytes(b"synthetic universal payload")
                    return subprocess.CompletedProcess(command, 0, stdout=slices)
                with mock.patch.object(packaging.shutil, "which", return_value="lipo"), mock.patch.object(packaging, "run", side_effect=lipo):
                    if valid:
                        payloads = packaging.stage_payloads(args, Path(temporary))
                        self.assertIn(("trello-mcp", "darwin", "universal"), payloads)
                    else:
                        with self.assertRaisesRegex(ValueError, "unexpected universal"):
                            packaging.stage_payloads(args, Path(temporary))

    @unittest.skipIf(os.name == "nt", "synthetic shebang server is Unix-only; native bundle smoke runs on Windows")
    def test_mcp_exchange_waits_for_each_response(self):
        trusted = self.directory / "trusted-script"
        trusted.mkdir()
        server = trusted / "synthetic-server"
        server.write_text("#!" + sys.executable + "\n"
                          "import json, sys\n"
                          "for line in sys.stdin:\n"
                          "    message = json.loads(line)\n"
                          "    if 'id' in message:\n"
                          "        print(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {}}), flush=True)\n")
        server.chmod(0o755)
        messages = [{"jsonrpc": "2.0", "id": 1, "method": "initialize"},
                    {"jsonrpc": "2.0", "method": "notifications/initialized"},
                    {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
                    {"jsonrpc": "2.0", "id": 3, "method": "tools/call"}]
        responses = packaging.mcp_exchange(server, {"PATH": os.defpath}, messages)
        self.assertEqual([response["id"] for response in responses], [1, 2, 3])

    def test_smoke_uses_synthetic_env_and_checks_text_only_health(self):
        replies = [
            {"id": 1, "result": {"serverInfo": {"name": "trello-mcp", "version": "v6.7.8"}}},
            {"id": 2, "result": {"tools": [{"name": "server_health"}, {"name": "card_find"}]}},
            {"id": 3, "result": {"content": [{"type": "text", "text": '{"ok":true,"credentialSource":"env","access":"unverified; call workspace_find"}'}]}},
        ]
        host = {"HOME": "/synthetic-host-home", "TRELLO_API_KEY": "synthetic-host-key",
                "TRELLO_TOKEN": "synthetic-host-token", "TRELLO_TOOLS": "server_health"}
        with mock.patch.object(packaging.os, "environ", host), \
                mock.patch.object(packaging, "run", return_value=subprocess.CompletedProcess([], 0, stdout="trello-mcp v6.7.8\n")), \
                mock.patch.object(packaging, "mcp_exchange", return_value=replies) as exchange:
            packaging.smoke(self.stage / "server" / "trello-mcp", "6.7.8")
            _, env, messages = exchange.call_args.args
            self.assertNotEqual(env["HOME"], host["HOME"])
            self.assertEqual(env["TRELLO_API_KEY"], "synthetic-api-key")
            self.assertEqual(env["TRELLO_TOKEN"], "synthetic-token")
            self.assertEqual(env["TRELLO_READ_ONLY"], "false")
            self.assertNotIn("TRELLO_TOOLS", env)
            self.assertEqual([message["method"] for message in messages],
                             ["initialize", "notifications/initialized", "tools/list", "tools/call"])
            for content in ([{"type": "text", "text": '{"ok":false}'}],
                            [{"type": "text", "text": '{ "ok":true,"credentialSource":"env","access":"unverified; call workspace_find"}'}],
                            [{"type": "text", "text": '{"ok":true,"credentialSource":"env"}'}],
                            [{"type": "text", "text": '{"ok":true,"credentialSource":"env","access":"verified"}'}],
                            [{"type": "text", "text": '{"ok":true}'}],
                            [{"type": "image", "data": "synthetic"}]):
                with self.subTest(content=content):
                    replies[2]["result"]["content"] = content
                    with self.assertRaises(ValueError):
                        packaging.smoke(self.stage / "server" / "trello-mcp", "6.7.8")

    def test_standalone_native_smoke_cli_contract(self):
        binary = self.payload("linux", "amd64")
        version = self.args().version
        argv = ["package_mcpb.py", "--version", version, "--smoke-binary", str(binary)]
        with mock.patch.object(sys, "argv", argv), mock.patch.object(packaging, "smoke") as smoke:
            packaging.main()
            smoke.assert_called_once_with(binary, version)
        with mock.patch.object(sys, "argv", argv + ["--target", "desktop"]), self.assertRaisesRegex(ValueError, "accepts only"):
            packaging.main()
        with mock.patch.object(sys, "argv", ["package_mcpb.py", "--version", version]), self.assertRaisesRegex(ValueError, "requires --target"):
            packaging.main()

    def test_foreign_smoke_is_explicit_and_does_not_execute(self):
        with mock.patch.object(packaging.platform, "system", return_value="Linux"), \
                mock.patch.object(packaging.platform, "machine", return_value="x86_64"), \
                mock.patch.object(packaging, "smoke") as smoke, contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(packaging.smoke_payloads(self.stage, [("trello-mcp", "linux", "arm64")], "1.2.3"), 0)
            smoke.assert_not_called()
            self.assertIn("SKIP native smoke for linux/arm64", output.getvalue())

    def test_cli_round_trip_and_immutable_output(self):
        if not (packaging.CLI_PACKAGE / "package.json").is_file():
            self.skipTest("pinned MCPB CLI missing; run npm ci --prefix packaging/mcpb --ignore-scripts")
        args = self.args(linux_binary=self.payload())
        # The payload is only a synthetic header, never execute it on any host.
        with mock.patch.object(packaging, "smoke_payloads", return_value=0):
            output = packaging.build(args)
            self.assertEqual(output.name, "trello-mcp_linux_arm64.mcpb")
            with zipfile.ZipFile(output) as archive:
                self.assertEqual(set(archive.namelist()), {"manifest.json", "LICENSE", "icon.png", "server/trello-mcp"})
            with self.assertRaisesRegex(ValueError, "refusing to replace"):
                packaging.build(args)
        args.output_dir = self.directory / "require-smoke"
        args.require_smoke = True
        with mock.patch.object(packaging, "smoke_payloads", return_value=0), self.assertRaisesRegex(ValueError, "no payload matches"):
            packaging.build(args)
        self.assertFalse((args.output_dir / output.name).exists())

    def test_local_desktop_bundle_is_never_a_release_asset(self):
        if not (packaging.CLI_PACKAGE / "package.json").is_file():
            self.skipTest("pinned MCPB CLI missing")
        args = self.args(target="desktop", linux_binary=None, darwin_arm64=self.payload("darwin", "arm64"),
                         windows_amd64=self.payload("win32", "amd64"), test_darwin_single_arch=True)
        with mock.patch.object(packaging, "smoke_payloads", return_value=0):
            output = packaging.build(args)
        self.assertEqual(output.name, "trello-mcp_desktop_test-only.mcpb")
        self.assertFalse((args.output_dir / "trello-mcp_desktop.mcpb").exists())


if __name__ == "__main__":
    unittest.main()
