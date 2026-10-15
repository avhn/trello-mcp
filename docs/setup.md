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
3. Stop and ask the human to run `trello-mcp auth` themselves in a private terminal. Browser consent and API-key creation are human-only.
4. Never run bare `trello-mcp auth` in a terminal you capture, and never ask for, read, print or paste an API key, token, authorization link or credential file.
5. Ask the human to restart or reconnect the client. Until a fresh configured server starts, only `server_health` is exposed.
6. Verify with `server_health` and `trello-mcp auth status` (Verify section). Both are secret-free.

The same request as one paste-to-agent line:

```text
Set up trello-mcp by following https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md; ask me to run trello-mcp auth myself in a private terminal, then reconnect. Never capture authentication output.
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
- `irm | iex` cannot pass arguments; for PowerShell help run `& ([scriptblock]::Create((irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1))) -Help`.
- Neither installer reads credentials or edits client configuration.
- Building from source is a separate manual choice and needs Go 1.26 or newer. It installs to `GOBIN`, normally `~/go/bin`:

```sh
go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest
```

Resolve the absolute executable path now, because desktop clients do not inherit your shell PATH:

```sh
command -v trello-mcp || { test -x "$HOME/.local/bin/trello-mcp" && echo "$HOME/.local/bin/trello-mcp"; }
```

```powershell
(Get-Command trello-mcp -ErrorAction SilentlyContinue).Source
```

Use the installer's printed path if the command is not on PATH. In the snippets below, replace `/absolute/path/to/trello-mcp` with that path. In JSON and TOML double every backslash on Windows, for example `C:\\Users\\you\\AppData\\Local\\Programs\\trello-mcp\\trello-mcp.exe`.
<!-- /section:install -->

<!-- section:auth -->
## Connect Trello

This step belongs to the human, in a private terminal that no agent captures:

```sh
trello-mcp auth
```

- Create an API key at https://trello.com/power-ups/admin when prompted, approve the Trello authorization link, then paste the token at the hidden prompt. The link contains the API key, so keep it private.
- Write access is the default. Use `trello-mcp auth --read-only` for a read-only token.
- Credentials are validated and saved to `~/.secrets/.trello-mcp.secret` (`%USERPROFILE%\.secrets\.trello-mcp.secret` on Windows). Client snippets never need credentials.
- Restart or reconnect the MCP client afterward. Until then the server exposes only `server_health`.

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
| VS Code | Linux, macOS, Windows; user profile, workspace | `mcp.json` | MCP: List Servers |

Every snippet uses saved credentials and leaves writes enabled. Merge entries into existing files instead of overwriting them. ChatGPT in a browser cannot start a local stdio server.

### Claude Code

User scope makes the server available in all your projects. Replace the path first:

```sh
claude mcp add --scope user --transport stdio trello -- "/absolute/path/to/trello-mcp"
```

On Windows quote the real path, for example `"C:\Users\you\AppData\Local\Programs\trello-mcp\trello-mcp.exe"`. Use `--scope local` (this project only, the default) or `--scope project` (shared through `.mcp.json`) only when you want that narrower scope. Check with `claude mcp list` and `/mcp`; after authentication run `/mcp reconnect all` or restart Claude Code.

### Claude Desktop

Merge into the user configuration file, then quit and reopen Claude Desktop:

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

Or run `codex mcp add trello -- "/absolute/path/to/trello-mcp"`. Check with `codex mcp list` or `/mcp`.

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

For one workspace, merge into `.vscode/mcp.json`. For your user profile, run the command palette entry MCP: Open User Configuration and merge the same entry there; the profile file location is not fixed across platforms.

```json
{
  "servers": {
    "trello": {
      "type": "stdio",
      "command": "/absolute/path/to/trello-mcp"
    }
  }
}
```

Use MCP: List Servers to start, stop, restart or show output.
<!-- /section:clients -->

<!-- section:profile -->
## Optional read-oriented profile

This profile registers ten tools: discovery, reading, local board selection and diagnostics. It omits every write tool. `board_use` still changes the local default board, so it is not wholly read-only. It is measured in the README benchmark.

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

Place the variable in the client's native environment setting, never in a shell export the client cannot see:

| Client | Setting |
| --- | --- |
| Claude Code | `--env TRELLO_TOOLS=...` before the server name |
| Claude Desktop, Cursor, VS Code | `"env": {"TRELLO_TOOLS": "..."}` beside `command` |
| Codex CLI, desktop and ChatGPT desktop | `[mcp_servers.trello.env]` table with `TRELLO_TOOLS = "..."` |
| OpenCode | `"environment": {"TRELLO_TOOLS": "..."}` beside `command` |

```sh
claude mcp add --scope user --transport stdio --env TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health trello -- "/absolute/path/to/trello-mcp"
```

```toml
[mcp_servers.trello]
command = "/absolute/path/to/trello-mcp"

[mcp_servers.trello.env]
TRELLO_TOOLS = "workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health"
```

For a token that cannot write at all, authenticate with `trello-mcp auth --read-only`, or set `TRELLO_READ_ONLY=1` to hide every write and transfer tool.
<!-- /section:profile -->

<!-- section:verify -->
## Verify

1. Ask the client to call `server_health`. With credentials it answers `ok`; without them it is the only tool and its message says to run `trello-mcp auth`.
2. Run `trello-mcp --version` to confirm the installed release.
3. Run `trello-mcp auth status`. It prints the credential source, username and granted scope, never a secret.
4. Call `server_health` with `detail` set to `policy` to see the effective workspace allowlist, read-only state and enabled tool count.
5. If a tool is hidden or refused, read the policy health before assuming the token is broken.
<!-- /section:verify -->

<!-- section:lifecycle -->
## Update, roll back and uninstall

### Update

1. Stop every client that runs trello-mcp.
2. Repeat the install method you used, with the same destination: the installer command above, or `go install ...@latest`.
3. Run `trello-mcp --version`.
4. Reconnect or restart the client.

### Roll back

Name an exact known release. A pinned version that is missing fails instead of falling back to latest:

```sh
curl -fsSL https://raw.githubusercontent.com/avhn/trello-mcp/main/install.sh | TRELLO_MCP_VERSION=v0.4.2 sh
```

```powershell
$env:TRELLO_MCP_VERSION = 'v0.4.2'; irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex
```

For a Go install use `go install github.com/avhn/trello-mcp/cmd/trello-mcp@v0.4.2`. Then check the version and reconnect.

### Uninstall

1. Optionally run `trello-mcp auth logout` first. It removes only the three Trello keys from the saved file, not environment credentials and not the token at Trello.
2. Remove the registration: `claude mcp remove trello --scope user`, or delete the `trello` entry from each client file above and restart that client.
3. Delete the exact binary you installed, for example `rm "$HOME/.local/bin/trello-mcp"`, or `trello-mcp.exe` in the Windows install directory.
4. On Windows remove only the PATH entry the installer added, in your user environment variables. Leave the machine PATH alone.
5. Optional cleanup: the selection file `trello-mcp/state.json` under your user configuration directory (`XDG_CONFIG_HOME` when set) and the download directory `trello-mcp` under your temporary directory, or `TRELLO_DOWNLOAD_DIR`.

### Revoke access

Logout and deleting files do not revoke authorization at Trello. Revoke the token in your Trello account's authorized applications. Credentials set through client environment variables must be removed from that client's configuration. Never delete the shared `~/.secrets` directory or `~/.secrets.env`; other tools use them.
<!-- /section:lifecycle -->
