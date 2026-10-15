# trello-mcp

A token-efficient Trello MCP server in Go. Let your AI assistant manage boards,
cards, checklists and attachments through a local stdio connection.
No hosted intermediary. Read-only access is one configuration switch away.

## Quick start

### 1. Install

On Linux or macOS, or Windows with a POSIX shell such as Git Bash:

```sh
curl -fsSL https://raw.githubusercontent.com/avhn/trello-mcp/main/install.sh | sh
```

The installer downloads the latest release, verifies its SHA-256 checksum, and
installs to `~/.local/bin`. Add that directory to your shell and MCP client's
PATH, or use the full executable path. Windows uses `trello-mcp.exe`.

- Choose a release with `TRELLO_MCP_VERSION=v0.1.0` or a directory with
  `INSTALL_DIR=/your/bin`. For a piped install, set these on `sh`, not `curl`.
- Supported binaries: Linux amd64/arm64, macOS amd64/arm64, Windows amd64/arm64.
  The POSIX installer supports Windows amd64; use PowerShell for Windows ARM64.
- Requires `curl`, `tar` (or `unzip` on Windows), and `sha256sum` or `shasum`.
- If a release or platform asset is unavailable, it falls back to `go install`
  at `@latest` when Go is available, even if a release version was requested.
- The installer prints setup instructions only. It never asks for credentials
  or registers an MCP server automatically.

#### Windows (PowerShell or cmd)

In Windows PowerShell 5.1 or PowerShell 7:

```powershell
irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex
```

From cmd:

```cmd
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex"
```

The native installer downloads the latest Windows amd64 or ARM64 release,
verifies SHA-256, and installs `trello-mcp.exe` to
`%LOCALAPPDATA%\Programs\trello-mcp`. It adds the directory to your user PATH
(not the machine PATH), without duplicating an existing entry. Open a new
terminal afterward. No administrator privileges or Go installation are needed.
Set `$env:TRELLO_MCP_VERSION = 'v0.1.0'` to pin a release or
`$env:INSTALL_DIR = 'C:\your\bin'` to change the destination before running it.
Unlike the POSIX installer, it fails if the release is unavailable rather than
building from source.

It prints setup instructions for Claude Code, Claude Desktop
(`%APPDATA%\Claude\claude_desktop_config.json`), and Codex and ChatGPT desktop
(`~/.codex/config.toml`), using the full installed executable path. It never
asks for credentials or registers a server automatically.

**Windows limitations:** local path uploads and attachment downloads are refused
(fail closed), because their filesystem ownership and path checks require Unix.
Base64 uploads and URL attachments remain available when read-only mode is off.

Prefer installing from source? Requires Go 1.26 or newer; the module selects the
Go 1.27.1 toolchain:

```sh
go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest
```

Go installs to `GOBIN`, or normally `~/go/bin`, rather than `~/.local/bin`.

### 2. Connect your Trello account

Run this in a terminal, using the full installed path if it is not on PATH:

```sh
trello-mcp auth
```

Create an API key manually at <https://trello.com/power-ups/admin> when prompted.
The command prints a Trello authorization link and tries to open your browser.
Write access is the default (`Allow write access? [Y/n]`, Enter means yes).
Approve access, then paste the token at the hidden prompt. Non-terminal input
reads one line per prompt for scripting; the token is never echoed by the command.
The authorization link necessarily contains the API key, so keep it private too.

Credentials are validated and saved in `~/.secrets/.trello-mcp.secret` on every
platform (`%USERPROFILE%\.secrets\.trello-mcp.secret` on Windows), not in
client snippets. See [Credential storage](#credential-storage) for permissions,
manual setup and overrides. Use `trello-mcp auth --read-only` for a read-only token, or
`--write` to skip the scope question. `--expiration 1day|30days|never` selects the
token lifetime (default `never`). `trello-mcp auth status` reports the credential
source, username and granted scope without secrets. `trello-mcp auth logout`
removes only the three Trello keys, preserving unrelated lines and comments
(deleting the file only if empty), but does not revoke the token at Trello or unset
environment variables. Restart your MCP client after connecting or logging out.

Alternatively, set **both** `TRELLO_API_KEY` and `TRELLO_TOKEN` in the server's
environment. That pair overrides saved credentials; a partial pair is an error.
Never commit credentials or share them in chat.

### 3. Use with your client

The snippets use your saved credentials and leave writes enabled. Read-only is
opt-in: use `trello-mcp auth --read-only`, or set `TRELLO_READ_ONLY=1` in the
server's environment to hide write and attachment-transfer tools.

**Claude Code**

```sh
claude mcp add trello --transport stdio -- "$HOME/.local/bin/trello-mcp"
```

Adjust the executable path if you used Go, a custom directory, or Windows.

**Claude Desktop**

Merge the following `mcpServers` object into your local configuration:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Claude Desktop has no official Linux build.

Replace `/absolute/path/to/trello-mcp` with the full installed executable path.
Desktop apps do not inherit your shell PATH. On Windows, use `trello-mcp.exe`
and escape backslashes in JSON, for example
`C:\\Users\\YOUR_USER\\AppData\\Local\\Programs\\trello-mcp\\trello-mcp.exe`.
The installers print the correct path and escaping. Restart Claude Desktop after
saving.

```json
{
  "mcpServers": {
    "trello": {
      "command": "/absolute/path/to/trello-mcp"
    }
  }
}
```

**Codex (CLI and desktop) and ChatGPT desktop**

The Codex CLI, Codex desktop and the ChatGPT desktop app share
`~/.codex/config.toml`. Merge the following into that local file and restart the
client; in ChatGPT desktop, check Settings, MCP servers. You can also add it there
by hand: Add server, type STDIO, and the same command. Use the full executable
path. Windows TOML basic strings need doubled backslashes, just like JSON above.

```toml
[mcp_servers.trello]
command = "/absolute/path/to/trello-mcp"
```

**Other MCP clients (generic MCP JSON)**

For clients accepting `mcpServers`, use the same JSON as the Claude Desktop
example, with the full executable path. Keep credentials out of committed files.
Client-specific formats may differ, as in the OpenCode example below.

<details>
<summary>OpenCode configuration</summary>

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

</details>

## Features and why

- **Account-wide discovery.** Works across all workspaces and guest boards visible
  to your API token, with an optional workspace allowlist. The
  [official documentation](https://support.atlassian.com/trello/docs/connect-trello-to-ai-assistants-with-trello-mcp/)
  says Atlassian's connector can access only workspaces explicitly authorized on
  its consent screen. It does not establish whether guest-only boards are
  accessible, so no guest-board exclusion is claimed here.
- **Local stdio server.** Runs with an API key and token, with no hosted service in
  between.
- **Small responses.** Curated fields, compact text-only JSON, no duplicate
  structured content or output schemas, and no inline attachment bodies.
- **Focused tool surface.** Select tools or groups, restrict workspaces, and enable
  read-only mode. All 48 tools are available by default; read-only mode
  exposes 19 reads and local-selection tools.

## Token cost

Estimated tokens, using characters / 4 on synthetic fixtures, compared with the
raw pretty-printed response style of `@delorenj/mcp-server-trello` 1.8.1. These are
reproducible offline size estimates, not live account measurements or tokenizer
counts.

<!-- bench:start -->

| Scenario | delorenj 1.8.1 style | this server | reduction |
| --- | --- | --- | --- |
| 20 boards | ~2.17k | ~0.43k | 80.0% |
| 30 cards in a list | ~9.27k | ~3.21k | 65.4% |
| 20 comments | ~6.74k | ~4.31k | 36.0% |
| 50 board actions | ~11.91k | ~4.25k | 64.3% |
| 22 card actions | ~4.89k | ~1.37k | 72.0% |
| Card with 20 comments | ~7.40k | ~4.46k | 39.7% |
| Tool definitions | 57 tools, ~7.3k (static audit) | 48 tools, ~5.23k | not comparable |

<!-- bench:end -->

- Response rows use the same synthetic fixtures on both sides: pretty-printed raw
  Trello-shaped JSON, as the Node server returns it, versus actual compact MCP
  text responses. This server's registered `tools/list` result is also measured.
- Action fixtures model mixed updates, checklist progress and long comments,
  without padding every card with descriptions or profile metadata. Savings
  depend on the action mix, especially the share of intact comment text.
- The reference tool-definition estimate (57 tools, ~7.3k tokens) comes from the
  plan's static 1.8.1 audit, not the offline benchmark. The 48 tools merge the
  reference's parameter variants; repair is intentionally replaced by local
  selection. The definition row is not a like-for-like measured comparison.
- The card-with-comments scenario sums `card_get(detail: "full")` and
  `comment_find(limit: 20)`. Comments are not fetched implicitly.
- Tool-call request sizes, protocol framing, and model-specific tokenization are
  excluded. See [Development](#development) to reproduce or refresh the table.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `TRELLO_API_KEY` | saved credentials | Trello API key; must be set together with `TRELLO_TOKEN` |
| `TRELLO_TOKEN` | saved credentials | Trello API token; an environment pair overrides the file |
| `TRELLO_BOARD_ID` | saved selection | Initial board; changing it clears the old saved workspace |
| `TRELLO_ALLOWED_WORKSPACES` | unrestricted | Comma-separated workspace ids or canonical names, not display names; additional server policy, not token permissions |
| `TRELLO_TOOLS` | `all` | Comma-separated tool names or prefix groups: `workspace`, `board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`, `attachment`, `server` |
| `TRELLO_READ_ONLY` | `0` (or saved scope `read`) | Opt-in: `1` hides all 29 mutation/transfer tools, including local-file downloads; 19 tools remain. `0` cannot override a saved read-only token |
| `TRELLO_DOWNLOAD_DIR` | `os.TempDir()/trello-mcp` | Absolute download directory; created as 0700, existing directories must be private and owned by the current user |
| `TRELLO_UPLOAD_DIR` | unset (path uploads disabled) | Absolute root for local-file uploads; only regular files inside this directory may be uploaded, without following symlinks |
| `TRELLO_MCP_CREDENTIALS` | `~/.secrets/.trello-mcp.secret` | Absolute dotenv credential file path; `auth --file PATH` overrides it for that command |
| Shared fallback | `~/.secrets.env` | Read only the three Trello keys if no earlier source exists; auth never modifies it |
| `TRELLO_TOKEN_SCOPE` (file only) | unknown, writable | `read` forces read-only; `read,write` permits writes subject to server policy |
| `XDG_CONFIG_HOME` | platform configuration directory | Absolute root for `trello-mcp/state.json` only; does not affect credentials |
| `HTTPS_PROXY` | unset | Standard Go HTTP proxy configuration |

### Credential storage

File locations and the unquoted `KEY=VALUE` format follow
[avhn/secret](https://github.com/avhn/secret), "Let coding agents use your
credentials without reading them". The bare name `trello-mcp` resolves to
`~/.secrets/.trello-mcp.secret` in that tool: `secret show trello-mcp` lists masked
names and `secret exec -f trello-mcp ...` can launch a configured consumer.
Values follow the same rules as `secret stash`. Using avhn/secret is optional;
trello-mcp reads the files itself.

`~/.secrets.env` is secret's shared top-level store and the final fallback.
Only `TRELLO_API_KEY`, `TRELLO_TOKEN` and optional `TRELLO_TOKEN_SCOPE` are
interpreted there; unrelated keys and values are ignored and never logged.
Both key and token must exist in the same file; sources are never merged.
Missing or credential-free earlier files fall through. Unsafe or malformed
earlier files fail closed. An unsafe shared file is never read when an earlier
source exists; otherwise its ownership, file privacy and symlink errors are
reported clearly. Shared files need the same private file mode/DACL; their home
parent is not required to have an owner-only Windows DACL.
Auth never writes to or removes keys from the shared store, even via `--file`.
`auth status` identifies it as `Credential source: file ~/.secrets.env (shared store)`.
After logout, credentials in the shared store can still be active.

The default on every platform is `~/.secrets/.trello-mcp.secret`, resolved with
`os.UserHomeDir()` (`%USERPROFILE%\.secrets\.trello-mcp.secret` on Windows).
Precedence is **environment `TRELLO_API_KEY` + `TRELLO_TOKEN` > absolute
`TRELLO_MCP_CREDENTIALS` file > default file > `~/.secrets.env`**. Both
environment values are required together; a partial pair is an error, not a fallback. MCP client
configs need only the full binary path, with no credential env block.

The dedicated plaintext file uses unquoted dotenv:

```dotenv
TRELLO_API_KEY=your-api-key
TRELLO_TOKEN=your-token
TRELLO_TOKEN_SCOPE=read,write
```

`TRELLO_TOKEN_SCOPE` is optional: absent means unknown and is treated as writable;
`read` forces read-only even with `TRELLO_READ_ONLY=0`. Status validates the
actual granted scope with Trello. Values must be nonempty and unquoted, without
NUL, CR, embedded newlines, boundary whitespace or surrounding quote pairs. A
final LF terminating the file is allowed. Comments beginning with `#`, blank
lines and unrelated keys are preserved byte-for-byte and in order on save.
Do not put secrets in committed files, shell history or chat.

`trello-mcp auth` creates a missing parent directory as 0700 and its file as 0600
on Unix. On Windows both use protected owner-only DACLs. An existing folder's
permissions are never changed: it must belong to the current user and must not
be group/world writable on Unix (0755 is acceptable). Files must belong to the
current user with no group/world access, normally 0600. Windows checks owner and
protected owner-only DACL on both. Symlinks in the file or parent path,
nonregular files and unsafe ownership/permissions are refused. Saves use a
same-directory private temporary file, fsync and atomic rename.

All auth commands accept an absolute file override:

```sh
trello-mcp auth --file /absolute/path/account.secret
trello-mcp auth status --file /absolute/path/account.secret
trello-mcp auth logout --file /absolute/path/account.secret
```

`--file` overrides `TRELLO_MCP_CREDENTIALS` for that command. Set
`TRELLO_MCP_CREDENTIALS` in the server environment to use the same custom file in
MCP clients; the environment credential pair still wins. `auth status` prints
one fact per line, never credentials:

```text
Credential source: file /home/alice/.secrets/.trello-mcp.secret
Connected as alice, scope read,write
```

For environment credentials the first line is exactly
`Credential source: env (TRELLO_API_KEY, TRELLO_TOKEN)`. With none found:

```text
No credentials in this shell or <path>. MCP clients may pass them through their own config; run `trello-mcp auth` to save them for all clients.
```

Validation failure starts with:

```text
credential validation failed; check the token and its read permission, then run `trello-mcp auth`:
```

The scrubbed upstream error follows that prefix.
Logout removes only these three keys; unrelated lines and comments remain, and
the file is deleted only if no bytes remain. It does not revoke the token or
unset environment values. Restart clients after credential changes.

#### Manual setup without the browser flow

If you already have a key and token, create the file in a private editor with
the three dotenv lines above, using your own values, without quotes. Use the
actual token scope (`read` or `read,write`); do not mark a read-only token writable.
On Unix, create a missing directory with `mkdir -m 700 ~/.secrets`, then create
the empty file privately after `umask 077`, run
`chmod 600 ~/.secrets/.trello-mcp.secret`, then edit it. Use LF line endings. Do not chmod an existing
shared folder automatically. If using the `secret` tool, `secret stash FILE KEY`
prompts privately for each key; never place values in command arguments. On
Windows use a current-user-owned folder and a protected owner-only DACL in
Properties > Security for both directory and file. Then run `trello-mcp auth status`.

#### Credential troubleshooting

The CLI prefixes errors with `trello-mcp auth:`; source resolution may also
prepend `credentials file <path>:`. Exact storage messages include:

- `credentials directory is group/world writable; remove group/world write permissions (chmod go-w) or choose a private directory with TRELLO_MCP_CREDENTIALS`
- `credentials file is group/world accessible; chmod 600 the credentials file or rerun trello-mcp auth`
- `credentials must be owned by the current user; rerun trello-mcp auth as that user`
- `credentials parent must be a directory, not a symlink; choose a real directory with TRELLO_MCP_CREDENTIALS`
- `credentials file must be a regular file, not a symlink`
- Windows: `credentials require a protected owner-only Windows DACL and ownership by the current user; remove access for other users in Properties > Security on the credentials directory and .trello-mcp.secret, then rerun trello-mcp auth`

Fix permissions only on a path you own and intend to keep private, or select a
new private parent with `TRELLO_MCP_CREDENTIALS`. Replace symlinks with real
paths, and rerun as the owner rather than bypassing ownership checks.

- **No credentials found / only `server_health`:** run `trello-mcp auth` to save
  the default file, check any override, and restart the client. A client may
  supply its own environment values not visible in your shell.
- **Permission refused:** use the exact corrective message above; auth will not
  silently chmod an existing folder or read an unsafe file to preserve its lines.
- **401 token rejected:** reconnect with `trello-mcp auth`; check token expiry,
  revocation and the matching API key. 403 can indicate insufficient permission.
- **Wrong workspace denial:** inspect `server_health` with `detail: "policy"`
  and correct `TRELLO_ALLOWED_WORKSPACES` or the object selection. Server policy
  is distinct from a rejected token.

### Discovering access policy

MCP initialization instructions summarize the configured workspace restriction,
read-only mode and effective file-transfer availability in one short paragraph.
They never include credentials or saved active ids. If a tool is hidden or an
operation is refused, call `server_health` with `detail: "policy"` before assuming
that the Trello token is broken. Include `server` or `server_health` in custom
`TRELLO_TOOLS` profiles to keep this diagnostic available.

Without credentials the server still initializes, registers only `server_health`,
and includes this message in initialization instructions and every health mode:
"Trello credentials not configured: run `trello-mcp auth` to save ~/.secrets/.trello-mcp.secret, or set
TRELLO_API_KEY and TRELLO_TOKEN." Invalid-token HTTP 401 errors include the same
setup hint.

Policy health returns `restricted`, `allowedWorkspaces` (each configured
`selector`, plus resolved `id` and canonical `name` when available), `readOnly`,
`credentialScope` (when loaded from file), `toolProfile`, `enabledToolCount`,
`hiddenWriteTools`, and `uploads`/`downloads`
with `enabled` and `dir`. Counts follow the selected profile; `hiddenWriteTools`
counts selected tools removed by read-only mode. Transfer availability also
accounts for read-only mode, tool selection and platform support; uploads here
means local path uploads, not base64. Directory paths describe configuration,
not a filesystem readiness check. Long selectors/paths may be shortened in
instructions; policy health retains their full configured values.

Workspace resolution uses one cached, best-effort request to
`/members/me/organizations` with only `id,name`. A failed lookup leaves selectors
unresolved without failing health. This diagnostic cache never authorizes
operations, and names of workspaces outside the allowlist are not returned.

### Active selection

- `workspace_use` and `board_use` update local defaults, even in read-only mode.
- Selecting a workspace clears the active board. Selecting a board sets its
  owning workspace.
- `board_find` enumerates all account boards, including guest boards, unless an
  explicit `workspaceId` narrows the search.
- Explicit `boardId` values work independently of the active selection.
- `board_create` requires an explicit or active `workspaceId` and verifies it.
- Tool selectors form a union; unknown selectors fail startup. Groups use the
  prefix before the first underscore, so `card` includes `card_member_add` and
  `card_member_remove`. For example, `TRELLO_TOOLS=card,comment,server_health`.
  Read-only filtering applies even when write tools are selected explicitly.

## Tools

`boardId` defaults to the active board for board-scoped tools. For object-scoped
tools it is an optional ownership constraint; the object's own board is resolved
independently. `mine: true` in `card_find` selects account-wide assigned cards and
cannot be combined with `boardId` or `listId`.

### Workspaces, boards and lists

| Tool | Description |
| --- | --- |
| `workspace_find` | Find allowed workspaces by name. |
| `workspace_use` | Set the active workspace and clear the active board. |
| `board_find` | Find account boards or boards in a workspace. |
| `board_use` | Set an authorized board as the local default. |
| `board_get` | Get curated summary or full board details. |
| `board_activity` | Page board actions with since/before cursors. |
| `board_create` | Create a board in an authorized workspace. |
| `list_find` | Find open lists on a board. |
| `list_create` | Create a list with optional position. |
| `list_update` | Rename, reorder, move with `idBoard`, or reopen with `closed: false`. |
| `list_archive` | Archive a list. |
| `list_watch` | Subscribe or unsubscribe with `subscribed` (default true). |

### Cards, comments and checklists

| Tool | Description |
| --- | --- |
| `card_find` | Find board, list or assigned cards with name filtering and local paging. |
| `card_get` | Get a card with optional full detail, extras or markdown. |
| `card_history` | Page card actions with an action-type filter. |
| `card_create` | Create a card in a verified list, with optional labels, members and checklist. |
| `card_create_batch` | Preflight ids, then create 1-100 cards with optional checklists and per-card results. |
| `card_update` | Update details, labels, members, dates, archive state or position. |
| `card_archive` | Archive a card. |
| `card_watch` | Subscribe or unsubscribe with `subscribed` (default true). |
| `card_move` | Move to `listId`, resolving and checking both boards. |
| `card_copy` | Copy to `listId`, resolving and checking both boards. |
| `comment_find` | Page comments on a card. |
| `comment_create` | Add a comment. |
| `comment_update` | Edit a verified card comment by `commentId`. |
| `comment_delete` | Delete a verified card comment. |
| `checklist_items` | Get items from an authorized checklist. |
| `checklist_get` | Find a card checklist by exact name, default Acceptance Criteria. |
| `checklist_create` | Create a card checklist. |
| `checklist_copy` | Copy `checklistId` to `cardId`, checking both boards. |
| `checklist_item_create` | Add an item, with optional position, assignee and due date. |
| `checklist_item_find` | Filter a checklist's items by `nameFilter`. |
| `checklist_item_update` | Update a verified item's name, state, position, assignee or due. |
| `checklist_item_delete` | Delete `itemId` after checking membership in `checklistId`. |

### Board metadata and diagnostics

| Tool | Description |
| --- | --- |
| `member_find` | Find board members by username or full name. |
| `card_member_add` | Assign a verified board member to a card. |
| `card_member_remove` | Remove a verified board member from a card. |
| `label_find` | Find board labels by name. |
| `label_create` | Create a board label. |
| `label_update` | Update a label's name or color. |
| `label_delete` | Delete an authorized label. |
| `customfield_find` | Get field definitions, types and dropdown option ids. |
| `customfield_set` | Set a typed value or dropdown option, or clear a value. |
| `attachment_find` | Find card attachment ids and metadata, with URLs for links only. |
| `attachment_add` | Attach an HTTP(S) URL; the server does not fetch it locally. |
| `attachment_upload` | Upload a local `path` or `base64` with `fileName`. |
| `attachment_download` | Save an uploaded attachment under the private download directory. |
| `server_health` | Get basic, detailed, metadata, performance or access-policy diagnostics (`detail: "policy"`). |

See [docs/tools.md](docs/tools.md) for parameters and the full reference name mapping.

Both `card_create` and each card in `card_create_batch` accept an optional
`checklist` object. Its `name` defaults to `Checklist`; required `items` contains
1-100 nonblank strings, each at most 4096 bytes. For example:

```json
{"listId":"l1","cards":[{"name":"Sound test","checklist":{"items":["SND01","SND02"]}}]}
```

The card is created first, then its checklist and items in order. Results include
`checklist: {id,name,items:N}`. A checklist failure preserves the card id,
checklist id if confirmed, confirmed item count and an error. Single-card partial
results set MCP `isError`; batches report errors per card and continue. Inspect
existing checklist items before resuming; never repeat card creation to repair
a partially created checklist.

### Responses and pagination

- Collections return `{"items":[...],"next":"cursor"}`; empty cursors are omitted.
- `card_find` defaults to 100 results (maximum 1000), comments to 20 (maximum 100),
  and activity/history to 20 (maximum 1000).
- Activity/history/comments use flat, type-specific summaries: `id,type,date`,
  `by` (username, falling back to member id), `cardId`, and relevant optional
  fields. Full `card` names appear on the first occurrence of each card in a
  page and whenever its name changes; an omitted name means the last name for
  that card in this page. Every page is self-contained, and card ids remain on
  every action. Lists and `listBefore`/`listAfter` moves use names only;
  `checklist,item,state` describe progress. Attachments keep id/name, never URLs.
  The queried board is omitted; other boards use `boardId`. `old` retains
  previous scalar values, including false, zero, null and empty strings, capped
  at 80 Unicode characters per string. `oldTruncated` lists shortened fields;
  `changed` lists fields whose non-scalar old values were omitted. Comment
  `text` is preserved. Nested Trello metadata and profile details are dropped.
- Actions use Trello's last-action-id `before` cursor and accept `since`. History
  also accepts a comma-separated action `filter`. A full action page advertises
  a possible next page, which may be empty.
- Board/list/member card endpoints do not document cursor pagination. `card_find`
  fetches a bounded snapshot, applies case-insensitive `nameFilter` first, then
  pages locally using the last card id as `before`. It never sends unsupported
  `before` or `limit` parameters to those endpoints.
- A changed or missing card cursor returns an error rather than restarting.
  Snapshots can change between calls; there is no stable snapshot guarantee.
  Every JSON API response is capped at 8 MiB; file transfers are capped at 10 MiB.
- Writes return `{id,name?,url?}` or `{ok:true}`; card creation can also include
  `checklist` progress and an `error`. Batch create returns an `items` array with
  zero-based `index`, `id` when created, optional checklist progress and errors.
  It is not transactional; failures do not roll back created cards or checklists.
- Argument errors identify unknown/duplicate keys and their object location.
  Unknown keys list allowed keys; type errors name the field and expected type.
- Dates use RFC 3339. Empty `due`/`start` strings clear dates, and explicit false
  values are preserved. Positions accept `top`, `bottom`, or a positive number.

### Custom fields and attachments

- Discover `customFieldId`, type and dropdown option ids with `customfield_find`.
  `customfield_set` takes `cardId`, `customFieldId`, and exactly one of `value`
  or `idValue`. Scalars use `value: {"text":"..."}`, `{"number":"12.5"}`,
  `{"date":"2026-10-20T09:00:00Z"}`, or `{"checked":"false"}`. Dropdowns use
  `idValue: "option-id"`, not the display text. Clear a scalar with `value: ""`
  or a dropdown with `idValue: ""`.
- Discover attachment ids with `attachment_find` using required `cardId` and an
  optional `boardId` constraint. It returns `items` with `id`, `name`, `fileName`,
  `mimeType`, `bytes`, `date`, and `isUpload`. Only link attachments include `url`;
  uploaded-file URLs require OAuth and are omitted. Available in read-only mode.
- Upload accepts exactly one local `path` or standard `base64` string. Path
  uploads are disabled by default: set `TRELLO_UPLOAD_DIR` to an absolute directory
  containing only files intended for upload. Paths may be relative to that root
  or absolute inside it after cleaning; paths escaping the root are refused.
  The root itself and every component below it must not be symlinks. Sources
  must be regular files; dot-files and dot-directories inside the root are allowed.
  Path verification requires Unix (including Linux and macOS); other platforms
  refuse path uploads. Base64 works without `TRELLO_UPLOAD_DIR` and requires
  `fileName`; `mimeType` and display `name` are optional. Uploads are capped at 10 MiB.
- Downloads use the attachment's `fileName`, not display name or supplied URLs,
  and OAuth headers without query credentials. Redirects are refused.
- Download directories must be private, owned by the current user and not
  symlinks. Files are created as 0600 with exclusive create inside a pinned
  directory root, using `attachmentId-sanitized_fileName`. Existing files and
  symlinks are never overwritten; failed/oversized downloads are removed.
  Responses contain only `path`, `size`, `mimeType`, never inline file bodies.
  Ownership verification currently requires a Unix platform (including Linux
  and macOS); other platforms refuse downloads rather than weaken this check.

### Detail levels

- `board_get`, `card_find`, and `card_get` support `detail: "summary"` (default)
  or `"full"`.
- Collection descriptions are capped at 200 Unicode characters with a
  `descTruncated` flag. `card_get` keeps the full description.
- `card_get` supports `view: "markdown"`, returned as a JSON object containing
  `markdown`. Its bounded `extraFields` array accepts only `stickers`,
  `pluginData`, `votes`, and `customFieldItems`.
- No tool requests `fields=all` or embeds attachment data.

## Security model

- **Private saved credentials.** See [Credential storage](#credential-storage).
  The plaintext dotenv file is `~/.secrets/.trello-mcp.secret`, with an absolute
  `TRELLO_MCP_CREDENTIALS` override. New directories are 0700, files 0600 on Unix;
  Windows uses protected owner-only DACLs. Existing folder permissions are never
  changed. Symlinks, foreign owners, writable directories and accessible files
  are refused. Saves use a private same-directory temporary file, fsync and
  atomic rename. The environment pair overrides files. Saved `TRELLO_TOKEN_SCOPE=read`
  forces read-only even with `TRELLO_READ_ONLY=0`. Logout removes only the three
  keys, keeping unrelated lines; it does not revoke Trello authorization.
  Protect plaintext files and backups.
- **Token permissions are the ceiling.** The workspace allowlist is an additional
  restriction, not a replacement for token permissions. Unset means unrestricted;
  ids and canonical names match, not display names. Initialization instructions
  and `server_health(detail: "policy")` explain the effective server policy.
- **Distinct denials.** Policy failures return compact `{"error":"access denied
  by <policy>: <reason>. This is server policy, not a token/permission error. ..."}`
  with a corrective action and policy-health pointer. Allowlist errors include
  configured selectors and only the caller-supplied object id, never resolved
  denied board/workspace ids or names. For example: `card c1 is outside the
  allowed workspaces (allowed: team)`. Ownership failures explicitly refuse
  access without exposing resolved parent ids. Only after the allowlist passes
  does a `boardId` mismatch identify the actual and supplied board ids.
  Genuine upstream HTTP failures use `trello API: HTTP <status>`; 401/403 explicitly
  identify a token/permission issue. Do not change credentials to fix server policy.
- **Ownership checks, not caller claims.** Every scoped read and write resolves
  the actual owning board. Moves and copies check source and destination;
  comments, checklist items, attachments, labels, custom fields and member
  references are checked against their actual parents before mutation. Supplied
  board ids are constraints, never authorization. Unverifiable ownership fails
  closed. Trello has no atomic authorization-and-mutation API, so concurrent
  upstream moves cannot be prevented by a local check.
- **Fresh authorization.** Ownership grants are not cached across calls, so a
  moved board is rechecked. Workspace listings are filtered, and health responses
  never expose saved ids.
- **Read-only profile.** `TRELLO_READ_ONLY=1` hides every Trello mutation and
  attachment transfer, including local-file downloads. Local defaults can still
  change. This does not replace a suitably scoped Trello token.
- **Confined local uploads.** Path uploads are disabled unless `TRELLO_UPLOAD_DIR`
  is set. Only regular files inside that root can be read, using a pinned root
  descriptor and no-symlink component traversal on Unix; other platforms fail
  closed. Choose a dedicated staging directory, not your home or a credential
  directory. Dot-files are not excluded. Base64 uploads do not need this setting
  because the caller already supplies the bytes.
- **Defensive HTTP handling.** Requests have deadlines, redirects are refused,
  credentials are scrubbed from errors, and upstream error bodies are never
  returned.

### Rate limits and retries

- Rolling limits: 300 requests/10s per key, 100/10s per token, and a conservative
  100/900s for all member-prefixed requests. Rate headers can reduce the budget.
- HTTP 429 allows at most three retries, honoring `Retry-After` across concurrent
  calls. Delays above 30s fail instead of retrying early.
- Limits are per process. Other clients sharing credentials still consume the
  same upstream quota.

## Development

Build a checkout and run the checks:

```sh
go build -o trello-mcp ./cmd/trello-mcp
./trello-mcp --version
gofmt -l .
go build ./...
go vet ./...
go test -race ./...
sh -n install.sh
sh tests/install_test.sh
```

Tests use only synthetic fixtures and `httptest`, never a Trello account. CI runs
lint and race checks on Linux and macOS, plus offline native installer tests on
Windows with PowerShell 7 and Windows PowerShell 5.1 (`pwsh -File
tests/install_test.ps1` or `powershell -File tests/install_test.ps1`). The
PowerShell tests mock user PATH access and use synthetic local release files via
`TRELLO_MCP_RELEASE_BASE_URL` (an absolute fixture directory containing tag
subdirectories, with `TRELLO_MCP_VERSION` set). Also run `shellcheck install.sh` and
`actionlint` if installed.

Print the local raw/projected character and estimated-token table:

```sh
go test ./internal/bench -v
```

Refresh the README comparison table without changing its markers:

```sh
go test ./internal/bench -v -update
```

For a whole-suite refresh, use `TRELLO_BENCH_UPDATE=1 go test ./...`.

### Releases

Push a signed version tag matching `v*` (for example, `v0.1.0`) to trigger
[the release workflow](.github/workflows/release.yml). It runs the race-tested
suite first, then cross-compiles with `CGO_ENABLED=0` and plain `go build` for the
six supported platforms. Release archives contain `trello-mcp` (or
`trello-mcp.exe`), with `checksums.txt` alongside them on the GitHub release.
Release binaries report their tag with `--version` and through MCP metadata.
No GoReleaser or local release credentials are needed; publishing uses the
workflow's `GITHUB_TOKEN`.

## License and credit

MIT, copyright Mert Dede. [delorenj/mcp-server-trello](https://github.com/delorenj/mcp-server-trello)
(MIT), version 1.8.1, is the reference for tool coverage. This is an independent Go
implementation; the reference server is not a runtime dependency.

Trello is a trademark of Atlassian. trello-mcp is an independent project, not affiliated with or endorsed by Atlassian.
