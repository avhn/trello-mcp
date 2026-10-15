<h1 align="center">trello-mcp</h1>

<p align="center">
  A local Trello MCP server in Go. Your AI assistant manages boards, cards, checklists and
  attachments over stdio, with compact curated responses, workspace allowlists and a read-only opt-in.
</p>

<p align="center">
  <a href="https://github.com/avhn/trello-mcp/releases"><img alt="Release" src="https://img.shields.io/github/v/release/avhn/trello-mcp"></a>
  <a href="https://github.com/avhn/trello-mcp/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/avhn/trello-mcp/ci.yml?branch=main&label=ci"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/avhn/trello-mcp"></a>
  <a href="docs/setup.md"><img alt="MCP: stdio" src="https://img.shields.io/badge/MCP-stdio-000000?logo=modelcontextprotocol&logoColor=white"></a>
</p>

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/avhn/trello-mcp/main/install.sh | sh
```

Windows PowerShell: `irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex`.
Both verify SHA-256 and fail closed instead of building from source.

Or give your agent this line and let it follow the canonical [setup guide](docs/setup.md):

```text
Set up trello-mcp by following https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md; ask me to run trello-mcp auth myself in a private terminal, then reconnect. Never capture authentication output.
```

## Quick start

1. Install the binary (above) and note its absolute path; it installs into `~/.local/bin` by default.
2. Register it with your client. Claude Code, user scope:

   ```sh
   claude mcp add --scope user --transport stdio trello -- "$HOME/.local/bin/trello-mcp"
   ```

   Claude Desktop, Codex CLI and desktop, ChatGPT desktop, OpenCode, Cursor and VS Code snippets,
   with exact file locations and reload steps, are in [docs/setup.md](docs/setup.md).
3. Run `trello-mcp auth` yourself in a private terminal. It validates your Trello API key and token and saves
   them to `~/.secrets/.trello-mcp.secret`; write access is the default and `--read-only` opts out.
   Never run it where an agent captures the output or paste keys, tokens or authorization links into chat.
4. Restart or reconnect the client, then call `server_health`. Until then only `server_health` is exposed.

Update, rollback, uninstall and revocation are in [docs/setup.md](docs/setup.md#update-roll-back-and-uninstall).
Variables, credential precedence and permissions are in [docs/configuration.md](docs/configuration.md).

## How it compares

A short, sourced comparison with two other Trello MCP servers. It describes each design; it is not a
capability or token-efficiency ranking, and the benchmark below does not execute either of them.

| | This project | [Atlassian official](https://github.com/atlassian/trello-mcp-server) | [delorenj 1.8.1](https://github.com/delorenj/mcp-server-trello/blob/v1.8.1/src/index.ts) |
| --- | --- | --- | --- |
| Transport | Local stdio | Hosted remote HTTP at `mcp.trello.com/v1` | Local stdio |
| Auth | User-managed API key and token | OAuth 2.0 | API key and token |
| Runtime | Native Go binary | Hosted; no local runtime | JavaScript entrypoint; package declares Bun >=1.0.0 |
| Scope | Every board the token can see; optional workspace allowlist | Workspaces approved on the OAuth consent screen | Every board the token can see |
| Tool inventory (not parity) | 48 by default, [10 in a profile](#read-oriented-profile) | Capability categories published, no tool count | 57 direct registrations in pinned source |
| Response shaping | Curated compact text JSON | Not evaluated | Pretty JSON with [card-list previews and size controls](https://github.com/delorenj/mcp-server-trello/blob/v1.8.1/src/card-list-preview.ts) |

Atlassian documents ([support page](https://support.atlassian.com/trello/docs/connect-trello-to-ai-assistants-with-trello-mcp/)) that its connector reaches only workspaces approved on its consent screen. That does not
establish whether guest-only boards are accessible, so no guest-board exclusion is claimed. Here `board_find`
lists every board your token can see, including guest boards, unless you narrow it with a workspace allowlist.

## Usage

Ask in plain language: "List my boards", "Show cards assigned to me", "Move card X to Done and tick the
checklist" or "Summarize this week's activity on the board". Useful controls:

- `TRELLO_READ_ONLY=1` or `trello-mcp auth --read-only` hides all 29 mutation and transfer tools.
- `TRELLO_ALLOWED_WORKSPACES=team,Another Team` adds a server-side allowlist, checked against each object's
  actual owner and not a supplied board id.
- Select tools with `TRELLO_TOOLS`; groups use the prefix before the first underscore, such as `card`.

### Read-oriented profile

Ten tools for discovery and reading; `board_use` still changes the local default board, so it is not wholly read-only.
Place it in your client's environment setting (per-client placement is in [docs/setup.md](docs/setup.md#optional-read-oriented-profile)):

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

## Benchmark

Characters / 4 on synthetic fixtures, compared with the raw Trello JSON for the same objects. Startup costs are
shown separately and are not compared across servers.

<!-- bench:start -->

| Scenario | Raw Trello JSON | this server | reduction |
| --- | --- | --- | --- |
| 20 boards | ~2166 | ~433 | 80.0% |
| 30 cards in a list | ~9266 | ~2968 | 68.0% |
| 20 comments | ~6739 | ~4311 | 36.0% |
| 50 board actions | ~11910 | ~4248 | 64.3% |
| 22 card actions | ~4888 | ~1368 | 72.0% |
| Card with 20 comments | ~7404 | ~4465 | 39.7% |

Synthetic raw pretty JSON is a reproducible reference, not measured output from another MCP server. Tokens use chars/4.

| Startup cost | Default (48 tools) | Read-oriented discovery (10 tools) |
| --- | --- | --- |
| tools/list | 20703 chars (~5176 tokens) | 4242 chars (~1060 tokens) |
| Initialize instructions | 140 chars (~35 tokens) | 141 chars (~35 tokens) |
| Initialize result (including instructions) | 304 chars (~76 tokens) | 305 chars (~76 tokens) |

| Independent response | Characters | Tokens (chars/4) |
| --- | --- | --- |
| card_find summary (30 cards) | 11873 | ~2968 |
| card_find full (30 cards) | 15023 | ~3756 |
| board_activity all (50 actions) | 16994 | ~4248 |
| board_activity commentCard | 5835 | ~1459 |
| board_activity updateCard | 5476 | ~1369 |

<!-- bench:end -->

- Both columns use the same fixtures; the right column is this server's actual compact text response.
- Card with comments sums `card_get(detail: "full")` and `comment_find(limit: 20)`; comments are not fetched implicitly.
- Activity fixtures model mixed updates, checklist progress and long comments; savings depend on the mix.
- Summary discovery omits each card `url`; full keeps it. Filtered activity rows are independent samples.
- Tool-call requests, protocol framing and model tokenizers are excluded. See [Development](#development).

## Tools

All 48 tools are registered by default; read-only mode exposes 19 reads and local-selection tools. Parameters
and the reference-name mapping are in [docs/tools.md](docs/tools.md).

- Workspaces, boards and lists (12): `workspace_find`, `workspace_use`, `board_find`, `board_use`, `board_get`,
  `board_activity`, `board_create`, `list_find`, `list_create`, `list_update`, `list_archive`, `list_watch`.
- Cards, comments and checklists (22): `card_find`, `card_get`, `card_history`, `card_create`,
  `card_create_batch`, `card_update`, `card_archive`, `card_watch`, `card_move`, `card_copy`, `comment_find`,
  `comment_create`, `comment_update`, `comment_delete`, `checklist_items`, `checklist_get`, `checklist_create`,
  `checklist_copy`, `checklist_item_create`, `checklist_item_find`, `checklist_item_update`,
  `checklist_item_delete`.
- Metadata and diagnostics (14): `member_find`, `card_member_add`, `card_member_remove`, `label_find`,
  `label_create`, `label_update`, `label_delete`, `customfield_find`, `customfield_set`, `attachment_find`,
  `attachment_add`, `attachment_upload`, `attachment_download`, `server_health`.

Responses are one compact JSON text block: curated fields, no output schemas, structured content, `fields=all`
or inline attachment bodies. Collections return `{"items":[...],"next":"cursor"}`. Writes return
`{id,name?,url?}` or `{ok:true}`. When a mutation may have applied but a requested field is unconfirmed, the
result lists `unconfirmed` fields with `isError`; inspect before retrying. Date and label-color clears are
sent as query parameters and verified from the response. Activity is flat, supports an upstream `filter`
(default `all`) and `since`/`before` paging, and repeats card and list names only when they change within a page.
`card_find` summaries omit `url`; use `detail: "full"` or `card_get` when you need it.

## Security

- Credentials live in a private file (`~/.secrets/.trello-mcp.secret`, 0600 in a 0700 directory) or in a
  complete `TRELLO_API_KEY` plus `TRELLO_TOKEN` environment pair, never in client snippets or tool output.
- Your token's permissions are the ceiling; the workspace allowlist and read-only mode only restrict further.
- Every scoped read and write resolves the real owning board. Moves, copies, comments, checklist items,
  labels, custom fields, members and attachments are checked against their actual parents; supplied
  board ids are constraints, not authorization. Unverifiable ownership fails closed.
- Server-policy denials are distinct from upstream failures and never reveal resolved ids or names;
  `server_health` with `detail: "policy"` explains the effective policy.
- Path uploads are off unless `TRELLO_UPLOAD_DIR` is set and are confined to regular files below it;
  downloads use a private directory, OAuth headers, no redirects and a 10 MiB cap.
- Requests have deadlines, credentials are scrubbed from errors, and 429 retries are bounded.
- Windows refuses local path uploads and attachment downloads because their ownership checks need Unix.
- Logout removes the saved keys but does not revoke the token at Trello; revoke it there separately.

The full model, refusals and troubleshooting are in [docs/configuration.md](docs/configuration.md).

## Documentation

- [docs/setup.md](docs/setup.md): install, seven clients, profile, verification, update, rollback, uninstall.
- [docs/configuration.md](docs/configuration.md): variables, precedence, permissions, troubleshooting, security.
- [docs/tools.md](docs/tools.md): tool behavior, parameters and synthetic examples.
- Site: <https://avhn.github.io/trello-mcp/>

## Development

Tests use only synthetic fixtures and `httptest`, never a Trello account:

```sh
gofmt -l . && go build ./... && go vet ./... && go test -race ./...
sh -n install.sh && sh tests/install_test.sh
python3 -I scripts/site/validate.py --self-test && python3 -I scripts/site/validate.py
go test ./internal/bench -v -update  # refresh the benchmark table above
```

CI runs lint and race checks on Linux and macOS, plus offline installer tests under PowerShell 7 and Windows
PowerShell 5.1. A signed `v*` tag runs the [release workflow](.github/workflows/release.yml): tests, six
`CGO_ENABLED=0` builds and `checksums.txt`; binaries report their tag with `--version`.
The tag message is the release description: `git tag -s --cleanup=verbatim vX.Y.Z -F notes.md`.

### Distribution

`server.json` is MCP Registry metadata (schema-valid, metadata only). Release binaries are the supported
install path; there is no one-click Claude Desktop package yet.

## License

MIT, copyright Mert Dede. This is an independent Go implementation and covers the tool surface of
[delorenj/mcp-server-trello](https://github.com/delorenj/mcp-server-trello) 1.8.1 (MIT); that server is not a
runtime dependency. Trello is a trademark of Atlassian. trello-mcp is an independent project, not affiliated
with or endorsed by Atlassian.
