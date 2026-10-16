# Set up trello-mcp

<!-- Restricted Markdown subset, mirrored by site/setup/index.html: headings (#, ##, ###),
paragraphs, flat bullet or numbered lists, links, inline code, tables and fenced code blocks.
No emphasis, nesting, quotes, images or raw HTML. Section markers are comment pairs:
section:NAME opens and /section:NAME closes; sections do not nest. -->

This is the one canonical setup document. A single fetch of it is enough to install, register,
verify, update, roll back and remove trello-mcp. Variables and credential details live in
[configuration](https://github.com/avhn/trello-mcp/blob/main/docs/configuration.md); tool behavior lives in
[tools](https://github.com/avhn/trello-mcp/blob/main/docs/tools.md).

<!-- section:agent -->
## For an AI agent

Follow these steps in order. Resolve every path on the machine you are setting up; do not copy a username or path from an example.

1. Install the binary (Install section) and resolve its absolute executable path.
2. Register it with the user's client (Clients section). Prefer user scope; use project scope only when asked.
3. Substitute the concrete absolute path into the installer's auth command, then stop and ask the human to run it themselves in a private terminal. Do not rely on PATH, shell variables, the working directory or a new shell. Browser consent and API-key creation are human-only.
4. Never run bare `trello-mcp auth` or the absolute-path form in a terminal you capture, and never ask for, read, print or paste an API key, token, authorization link or credential file.
5. Ask the human to use the individual trello Reconnect action in `/mcp` or fully restart the client. Until a fresh configured server starts, only `server_health` is exposed.
6. Verify local configuration with `server_health`, then actual Trello access with `workspace_find {}` (Verify section). CLI users may also use the printed absolute-path status command; it does not describe Desktop extension credentials.

The same request as one paste-to-agent line:

```text
Set up trello-mcp by following https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md; ask me to run its printed absolute-path auth command myself in a private terminal, then reconnect. Never capture authentication output.
```
<!-- /section:agent -->

<!-- section:install -->
## Install

Linux, macOS, or Windows with a POSIX shell such as Git Bash. The installer verifies the SHA-256 checksum and installs to `~/.local/bin` (change it with `INSTALL_DIR`):

```sh
curl -fsSL https://raw.githubusercontent.com/avhn/trello-mcp/main/install.sh | sh
```

Windows PowerShell 5.1 or PowerShell 7 installs `trello-mcp.exe` to `%LOCALAPPDATA%\Programs\trello-mcp` and adds only that directory to the user PATH:

```powershell
irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex
```

- Supported binaries: Linux, macOS and Windows on amd64 and arm64. The POSIX installer on Windows supports amd64 only; use PowerShell for Windows ARM64.
- The POSIX installer needs `curl`, `tar` (or `unzip` for Windows archives) and `sha256sum` or `shasum`.
- Both installers fail closed. A missing release, missing asset, unsupported platform or failed checksum stops the install; nothing is built from source and a pinned version never becomes latest.
- Both accept `--help` and `-h` (PowerShell also `-Help`), and reject unknown arguments, before doing any work.
`irm | iex` cannot pass arguments; for PowerShell help run:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1))) -Help
```
Neither installer reads credentials or edits client configuration. Building from source is a separate manual choice and needs Go 1.26 or newer. It installs to `GOBIN`, or the first `GOPATH` entry plus `bin`:

```sh
go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest
```

For source installs on Linux/macOS, resolve the executable from Go's install settings, not PATH:

```sh
trello_mcp="$(go env GOBIN)"; if [ -z "$trello_mcp" ]; then trello_mcp="$(go env GOPATH)"; trello_mcp="${trello_mcp%%:*}/bin"; fi; trello_mcp="$trello_mcp/trello-mcp"; printf '%s\n' "$trello_mcp"
"$trello_mcp" --version
"$trello_mcp" auth status
```

For Windows PowerShell source installs:

```powershell
$bin = go env GOBIN; if (-not $bin) { $gopath = (go env GOPATH) -split [IO.Path]::PathSeparator; $bin = Join-Path $gopath[0] 'bin' }; $trelloMcp = Join-Path $bin 'trello-mcp.exe'; $trelloMcp
& $trelloMcp --version
& $trelloMcp auth status
```

Substitute the printed absolute path into client configuration, not the shell variable or a different PATH executable. Source users run human-only auth privately with the same resolved executable, and use it for lifecycle commands below. Installer users do not need Go and should prefer the installer's printed commands.

The installer prints quoted, concrete absolute-path commands for auth, read-only auth, status, logout, version and Claude Code registration; PowerShell prefixes the auth, status, logout and version commands with `&`, passes the quoted path as an argument in the registration line, and doubles every single-quote character in the path (including typographic quotes). Hand them to the human as printed. Use the printed path if the command is not on PATH. In the snippets below, replace `/absolute/path/to/trello-mcp` with that path. In JSON and TOML double every backslash on Windows, for example `C:\\Users\\you\\AppData\\Local\\Programs\\trello-mcp\\trello-mcp.exe`.
<!-- /section:install -->

<!-- section:auth -->
## Connect Trello

This step belongs to the human, in a private terminal that no agent captures. Run the installer's absolute-path command so it does not depend on PATH:

```sh
"/absolute/path/to/trello-mcp" auth
```

- Create an API key at [Trello Power-Up administration](https://trello.com/power-ups/admin) when prompted, approve the Trello authorization link, then paste the token at the hidden prompt. The link contains the API key, so keep it private.
- Claude Desktop users who install the extension enter the key and token in the extension settings instead and do not run this command (see the Claude Desktop extension section).
- Write access is the default. Use the printed absolute-path auth command with `--read-only` for a read-only token.
- Credentials are validated and saved to `~/.secrets/.trello-mcp.secret` (`%USERPROFILE%\.secrets\.trello-mcp.secret` on Windows). Client snippets never need credentials.
- Use the individual trello Reconnect action in `/mcp`, or fully quit and restart the client, afterward. Until a fresh configured server starts it exposes only `server_health`.

Variables, precedence, permissions and troubleshooting are in [configuration](https://github.com/avhn/trello-mcp/blob/main/docs/configuration.md).
<!-- /section:auth -->

<!-- section:clients -->
## Clients

| Client | Platform and scope | Where | Reload |
| --- | --- | --- | --- |
| Claude Code | Linux, macOS, Windows; local, project, user | `claude mcp add` | `/mcp` |
| Claude Desktop | macOS, Windows, Linux beta; user | `claude_desktop_config.json` | Quit and reopen |
| Codex CLI and desktop | Linux, macOS, Windows; user, trusted project | `~/.codex/config.toml` | `/mcp`, or Restart |
| ChatGPT desktop | Local MCP only; user | `~/.codex/config.toml` | Restart |
| OpenCode | Linux, macOS, Windows; user, project | `opencode.json` | Restart |
| Cursor | Linux, macOS, Windows; user, project | `mcp.json` | MCP Logs panel |
| VS Code | Linux, macOS, Windows; personal, workspace | `~/.copilot/mcp-config.json` | MCP: List Servers |

Every snippet uses saved credentials and leaves writes enabled. Merge entries into existing files instead of overwriting them. ChatGPT in a browser cannot start a local stdio server.

### Claude Code

User scope makes the server available in all your projects. Replace the path first:

```sh
claude mcp add --scope user --transport stdio trello -- "/absolute/path/to/trello-mcp"
```

On Windows quote the real path, for example `"C:\Users\you\AppData\Local\Programs\trello-mcp\trello-mcp.exe"`. Use `--scope local` (this project only, the default) or `--scope project` (shared through `.mcp.json`) only when you want that narrower scope. Check with `claude mcp list` and `/mcp`; after authentication use the trello server's individual Reconnect action in `/mcp`, which refreshes its tool list; `/mcp reconnect all` retries failed or auth-needed servers, and a full restart also works.

### Claude Desktop

This is the CLI binary path; the packaged extension is described in the next section. Merge into the user configuration file, then quit and reopen Claude Desktop:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: Anthropic offers a [beta Linux build](https://support.claude.com/en/articles/10065433-installing-claude-desktop) for Ubuntu 22.04+ and Debian 12+ on x64 and arm64. Use Settings > Developer > Edit Config; no path is assumed.

```json
{
  "mcpServers": {
    "trello": {
      "command": "/absolute/path/to/trello-mcp"
    }
  }
}
```

### Codex CLI and desktop

Codex CLI, the Codex desktop app and the IDE extension read the same user file, `~/.codex/config.toml`. A trusted project may also use `.codex/config.toml`. Merge this table and restart Codex:

```toml
[mcp_servers.trello]
command = "/absolute/path/to/trello-mcp"
```

Or run:

```sh
codex mcp add trello -- "/absolute/path/to/trello-mcp"
```

Check with `codex mcp list` or `/mcp`.

### ChatGPT desktop

The ChatGPT desktop app shares `~/.codex/config.toml` with Codex for local MCP servers, so the Codex entry above is the only configuration. Restart the app after saving. Alternatively use the app's MCP settings: add a STDIO server with the same command.

### OpenCode

Use `~/.config/opencode/opencode.json` for all projects, or `opencode.json` in a project root. Merge this entry and restart OpenCode:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "trello": {
      "type": "local",
      "command": ["/absolute/path/to/trello-mcp"],
      "enabled": true
    }
  }
}
```

### Cursor

Use `~/.cursor/mcp.json` for all projects, or `.cursor/mcp.json` in a project. Merge this entry, then confirm the server in the MCP Logs output panel:

```json
{
  "mcpServers": {
    "trello": {
      "command": "/absolute/path/to/trello-mcp"
    }
  }
}
```

### VS Code

Prefer the personal file `~/.copilot/mcp-config.json` (`$COPILOT_HOME/mcp-config.json` when that variable is set); it keeps a machine-specific path out of repositories. Merge, do not overwrite:

```json
{"mcpServers":{"trello":{"command":"/absolute/path/to/trello-mcp"}}}
```

- Copilot CLI shares this file when it uses the same configuration home.
- A workspace-root `.mcp.json` uses the same shape for intentionally shared configuration. Claude Code also reads it, so if trello is already registered through `claude mcp add`, check for duplicates instead of adding a second entry.
- Existing `.vscode/mcp.json` and user-profile files (MCP: Open User Configuration) remain supported for compatibility, with the older `servers` root; keep working entries.

Use MCP: List Servers to start, stop, restart or show output.
<!-- /section:clients -->

<!-- section:desktop -->
## Claude Desktop extension

The packaged extension supports macOS and Windows amd64, owner-tested at v0.6.0. Download bundles from GitHub releases, verify checksums, and install/update manually. Registry discovery is separate from Desktop installation. Other clients use the CLI; browser authorization is human-only.

- Download from the [latest release](https://github.com/avhn/trello-mcp/releases/latest). Use `trello-mcp_desktop.mcpb` for macOS and Windows amd64; there is no CPU choice. Windows on Arm is not claimed.
- Linux Desktop is itself a beta. Bundles are owner-untested and excluded from Registry installation metadata. Use the CLI installer or pick `trello-mcp_linux_amd64.mcpb` or `trello-mcp_linux_arm64.mcpb` to match your CPU. Secure-storage support on Linux is unverified.
- Compare the file's SHA-256 with the `checksums.txt` on that release before installing (macOS `shasum -a 256 FILE`, Windows `Get-FileHash FILE`). Updates are manual: download the newer bundle and install it again.

Steps for a person at Claude Desktop:

1. In your browser open [Trello Power-Up administration](https://trello.com/power-ups/admin), create a personal Power-Up if needed, and generate a personal API key in its Trello Auth or API key area.
2. In your browser only, replace `YOUR_KEY` in the template below, open it, approve access and copy the token. For a read-only token use `scope=read`. The Power-Up page's own Token link may use different parameters.
3. In Claude Desktop choose Settings > Extensions > Advanced settings > Extension Developer > Install Extension, select the downloaded `.mcpb` file and approve it.
4. Paste the key and token only into the extension's masked API key and token fields. Optionally set allowed workspaces, read-only mode and the download and upload directories, then save.
5. Restart the extension. If Desktop does not pick the settings up, fully quit and reopen it. Follow Verify below: `server_health` reports local configuration and `credentialSource: "env"`, not validated Trello access; `workspace_find {}` performs the real read. No CLI auth or status command is needed.

```text
https://trello.com/1/authorize?key=YOUR_KEY&scope=read,write&expiration=30days&name=trello-mcp&response_type=token
```

- Never share the key, token or your personalized authorization URL in chat, logs or agent-captured commands. There is no shared application key and no server-printed URL.
- The URL template asks for read and write access that expires after 30 days. When it expires, repeat steps 2 and 4 and restart the extension. A token with expiration `never` must be revoked by hand; uninstalling the extension does not revoke any token. Revoke in your Trello account's authorized applications.
- The read-only toggle and the workspace allowlist restrict this server only, not the token's authority. Use a `scope=read` token for read-only access at Trello.
- Desktop keeps sensitive fields in OS secure storage, but the running server still receives them as environment variables, which take priority over saved credential files. Do not run CLI auth to store Desktop credentials. An environment token has unknown scope and is not assumed read-only.
- On Unix, an empty download directory keeps the temporary-directory default; an empty upload directory keeps path uploads off. Windows refuses local path uploads and attachment downloads even when directories are set; configuring directories cannot enable them. Base64 uploads and URL attachments remain alternatives subject to write/profile policy. Read-only accepts `true` or `false` here and `1` or `0` in a client environment.
- Update manually by verifying and installing the newer bundle. Its identity remains `trello-mcp`, but settings retention or migration is not guaranteed. Check settings using supported Desktop controls, restart the extension, then repeat Verify. Never inspect secure-storage databases or copy credentials to CLI files.
<!-- /section:desktop -->

<!-- section:profile -->
## Optional read-oriented profile

This profile registers ten tools: discovery, reading, local board selection and diagnostics. It omits every write tool. `board_use` still changes the local default board, so it is not wholly read-only. It is measured in the README benchmark.

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

Place the variable in the client's native environment setting, never in a shell export the client cannot see:

| Client | Setting |
| --- | --- |
| Claude Code | `--env TRELLO_TOOLS=... --transport stdio` before the server name |
| Claude Desktop, Cursor, VS Code | `"env": {"TRELLO_TOOLS": "..."}` beside `command` |
| Codex CLI, desktop and ChatGPT desktop | `[mcp_servers.trello.env]` table with `TRELLO_TOOLS = "..."` |
| OpenCode | `"environment": {"TRELLO_TOOLS": "..."}` beside `command` |

```sh
claude mcp add --scope user --env TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health --transport stdio trello -- "/absolute/path/to/trello-mcp"
```

```toml
[mcp_servers.trello]
command = "/absolute/path/to/trello-mcp"

[mcp_servers.trello.env]
TRELLO_TOOLS = "workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health"
```

`--env` is variadic: an option such as `--transport` must separate its values from the server name, or `trello` can be consumed as another environment value.

For a read-only token, use the printed auth command with --read-only. TRELLO_READ_ONLY=1 only hides this server's write and transfer tools; token permissions stay unchanged.
<!-- /section:profile -->

<!-- section:verify -->
## Verify

1. Credentials must be present before health metadata or policy fields exist; every unconfigured detail returns the same four-field setup response. Call `server_health {}` for local configuration presence and credential source. `ok: true` does not validate authentication. Basic and detailed modes report `credentialSource` as `env`, `file` or `none`, never a path or secret. Configured responses include `access: "unverified; call workspace_find"`.
2. Call `server_health` with `detail: "metadata"` for the installed version (configured servers only). CLI users may also use the printed absolute-path version and `trello-mcp auth status` commands; status describes that CLI invocation's credentials, not Desktop's environment.
3. Call `server_health` with `detail: "policy"` (configured servers only) for effective workspace restrictions, read-only state and tool availability. Its best-effort workspace labels do not validate authentication.
4. Call `workspace_find {}` to verify a real Trello read. Empty `items` still proves the read worked, not that a workspace selector matches. A configured server that gets 401 or 403 is not missing setup. A 401 can mean an expired or revoked token, a missing scope (especially write) or no access to that object, so it is not proof of an invalid token. If `workspace_find {}` succeeds, the token is valid for that read; check object access or write scope. A 403 points to object access, permissions or token scopes. Network and rate-limit errors differ. If renewal is needed, the human privately reruns the printed auth command (with `--file` for a client-set `TRELLO_MCP_CREDENTIALS` file; update or repoint one naming `~/.secrets.env`), updates Desktop settings, or updates client-set env credentials.
5. If a custom profile omits `workspace_find`, use a permitted existing upstream read such as `board_find`. A health-only profile cannot verify access: change the profile and reconnect if appropriate, never bypass restrictions. Desktop extension users need no CLI auth/status or credential copying.

Configured basic response (the source may instead be `file`):

```json
{"ok":true,"credentialSource":"env","access":"unverified; call workspace_find"}
```

Health does not become an enduring validation after a successful read; access verification is a separate observation each time.
<!-- /section:verify -->

<!-- section:lifecycle -->
## Update, roll back and uninstall

### Update

1. Stop every client that runs trello-mcp.
2. For CLI installs repeat your install method with the same destination: the installer command above, or the source install command. For Desktop extensions verify the newer bundle checksum, install it manually and check settings using supported controls.
3. CLI users run the printed absolute-path version command, for example `"/absolute/path/to/trello-mcp" --version`; source users use `"$trello_mcp" --version` or `& $trelloMcp --version`. Desktop users use health metadata, which needs configured credentials.
4. Use the individual trello Reconnect action, restart the extension or fully restart the client, then repeat Verify, including the actual upstream read.

Updating from v0.4.x? Read [upgrading from v0.4.x](https://github.com/avhn/trello-mcp/releases/tag/v0.5.0#upgrading-from-v04x) for the summary URL migration and the explicit versioned `go install` fallback when release assets are missing.

### Roll back

Name an exact known release. A pinned version that is missing fails instead of falling back to latest:

```sh
curl -fsSL https://raw.githubusercontent.com/avhn/trello-mcp/main/install.sh | TRELLO_MCP_VERSION=v0.4.2 sh
```

```powershell
& { $hadPin = Test-Path Env:TRELLO_MCP_VERSION; $oldPin = $env:TRELLO_MCP_VERSION; try { $env:TRELLO_MCP_VERSION = 'v0.4.2'; irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex } finally { if ($hadPin) { $env:TRELLO_MCP_VERSION = $oldPin } else { Remove-Item Env:TRELLO_MCP_VERSION -ErrorAction SilentlyContinue } } }
```

The PowerShell scriptblock restores an existing version pin even if installation throws, and removes only a temporary pin. For a Go install use:

```sh
go install github.com/avhn/trello-mcp/cmd/trello-mcp@v0.4.2
```

Then check the version with the resolved absolute path and reconnect. For Desktop, manually install a known earlier bundle after checking its release checksum; verify settings and access again, without assuming settings migrate backward.

### Uninstall

For a CLI install:

1. Optionally run the printed quoted absolute-path `Logout:` command first, or the resolved source command below. It removes only the three Trello keys from the saved file, not environment credentials and not the token at Trello.
2. Remove the registration using the command below, or delete the `trello` entry from each client file above and restart that client.
3. Delete the exact binary you installed, for example `rm "$HOME/.local/bin/trello-mcp"`, or `trello-mcp.exe` in the Windows install directory.
4. On Windows remove only the PATH entry the installer added, in your user environment variables. Leave the machine PATH alone.
5. Optional cleanup: the selection file `trello-mcp/state.json` under your user configuration directory (`XDG_CONFIG_HOME` when set) and the download directory `trello-mcp` under your temporary directory, or `TRELLO_DOWNLOAD_DIR`.

```sh
"/absolute/path/to/trello-mcp" auth logout
# Source install, using the resolved variable from Install:
"$trello_mcp" auth logout
```

```powershell
& 'C:\absolute\path\to\trello-mcp.exe' auth logout
# Source install, using the resolved variable from Install:
& $trelloMcp auth logout
```

```sh
claude mcp remove trello --scope user
```

For a Desktop extension, remove or disable it using supported controls in Settings > Extensions. Manage any retained sensitive settings there using supported controls and revoke Trello access separately. Uninstall does not promise deletion of secure-storage entries or revocation. Do not inspect or delete keychain/credential databases, and do not use CLI logout as Desktop cleanup.

### Revoke access

Logout and deleting files do not revoke authorization at Trello. Revoke the token in your Trello account's authorized applications. Credentials set through client environment variables must be removed from that client's configuration. Never delete the shared `~/.secrets` directory or `~/.secrets.env`; other tools use them.
<!-- /section:lifecycle -->
