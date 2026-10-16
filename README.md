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
Set up trello-mcp by following https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md; ask me to run its printed absolute-path auth command myself in a private terminal, then reconnect. Never capture authentication output.
```

## Quick start

1. Install the binary (above) and note its absolute path: POSIX defaults to `~/.local/bin`, Windows
   PowerShell to `%LOCALAPPDATA%\Programs\trello-mcp`. The
   installer prints quoted absolute-path commands for auth, status, version and registration; use those.
2. Register it with your client. Claude Code, user scope:

   ```sh
   claude mcp add --scope user --transport stdio trello -- "/absolute/path/to/trello-mcp"
   ```

   Replace the quoted placeholder with the concrete installed path; prefer the printed registration.
   Claude Desktop, Codex CLI and desktop, ChatGPT desktop, OpenCode, Cursor and VS Code snippets,
   with exact file locations and reload steps, are in [docs/setup.md](docs/setup.md).
3. Run the printed absolute-path `trello-mcp auth` yourself in a private terminal. It validates your Trello API key and token and saves
   them to `~/.secrets/.trello-mcp.secret`; write access is the default and `--read-only` opts out.
   Never run it where an agent captures the output or paste keys, tokens or authorization links into chat.
4. Use the trello server's Reconnect action in `/mcp` or restart the client, then call `server_health`
   for local presence/source, metadata for version and policy for restrictions. Then call `workspace_find {}`
   for actual Trello access: health `ok` and `credentialSource` do not validate auth. Empty success still
   proves the read worked. Until a fresh configured server starts only `server_health` is exposed, and
   every health detail then returns just the setup response; version and policy fields need credentials.

Claude Desktop bundles support macOS and Windows amd64 (owner-tested at v0.6.0; Windows on Arm is not
claimed); Linux Desktop remains beta.
Download from [normal releases](https://github.com/avhn/trello-mcp/releases/latest), verify the checksum and
install/update manually. Setup needs no terminal (only the checksum check does); create the key/token in a browser and use
extension settings, not CLI auth. They are not directory listed or one-click. Removal uses Desktop controls,
not CLI logout; settings deletion/migration and token revocation are not guaranteed. See
[docs/setup.md](docs/setup.md#claude-desktop-extension).

Update, rollback, uninstall and revocation are in [docs/setup.md](docs/setup.md#update-roll-back-and-uninstall).
Variables, credential precedence and permissions are in [docs/configuration.md](docs/configuration.md).

## Usage

Ask in plain language: "List my boards", "Show cards assigned to me", "Move card X to Done and tick the
checklist" or "Summarize this week's activity on the board". Useful controls:

- `TRELLO_READ_ONLY=1` (or `true`) hides all 29 mutation and transfer tools but leaves token permissions
  unchanged; `trello-mcp auth --read-only` saves a read-only token. Writes stay enabled by default.
- `TRELLO_ALLOWED_WORKSPACES=team,5f0c1a2b3c4d5e6f7a8b9c0d` adds a server-side allowlist of workspace ids or
  canonical names (not display names), checked against each object's actual owner and not a supplied board id.
- Select tools with `TRELLO_TOOLS`; groups use the prefix before the first underscore, such as `card`.

### Read-oriented profile

Ten tools for discovery and reading; `board_use` still changes the local default board, so it is not wholly read-only.
Place it in your client's environment setting (per-client placement is in [docs/setup.md](docs/setup.md#optional-read-oriented-profile)):

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

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
`{id,name?,url?}` or `{ok:true}`. For requested card fields subject to zero-value omission, an absent key
means empty, false or unset; activity name reuse and redacted upload URLs are not empty signals. When a
single mutation may have applied but a requested field is unconfirmed, the result lists `unconfirmed`
fields with `isError`; batches put `error` and `unconfirmed` on entries, not the envelope. Inspect before
retrying. Date and label-color clears are
sent as query parameters and verified from the response. Activity is flat, supports an upstream `filter`
(default `all`) and `since`/`before` paging, and repeats card and list names only when they change within a page.
`card_find` summaries omit `url`; use `detail: "full"` or `card_get` when you need it. Pass
`includeDesc: false` to `card_find` to skip description previews when triaging or paging. Human text fields
carry a 16,384 code point limit; only the card description limit is documented by Trello, the rest are
labeled local ceilings in [docs/tools.md](docs/tools.md#text-limits).

Through v1.0.0, the stability intent covers tool names, `TRELLO_TOOLS` selection syntax, local-health
semantics and the v0.7.0 configured basic/detailed fields. Values, documentation, packaging, descriptions
and Linux beta status can change; incompatible changes require explicit review in the v1.0.0 audit.
The [compatibility and versioning policy](docs/tools.md#compatibility-and-versioning) is binding from v1.0.0.

## How it compares

Selective comparison, checked 2026-10-16. Two sourced alternatives, not an exhaustive survey or a
ranking of capabilities or token efficiency. The benchmark below does not execute either alternative.

| | This project | [Atlassian official](https://github.com/atlassian/trello-mcp-server) | [delorenj 1.8.1](https://github.com/delorenj/mcp-server-trello/blob/v1.8.1/src/index.ts) |
| --- | --- | --- | --- |
| Transport | Local stdio | Hosted remote HTTP at `mcp.trello.com/v1` | Local stdio |
| Auth | User-managed API key and token | OAuth 2.0 | API key and token |
| Runtime | Native Go binary | Hosted; no local runtime | JavaScript entrypoint; package declares Bun >=1.0.0 |
| Scope | Accessible open boards, including guest boards; optional workspace allowlist | Workspace-scoped OAuth; current multi-workspace coverage unverified | Every board the token can see |
| Tool inventory (not parity) | 48 by default, [10 in a profile](#read-oriented-profile) | Capability categories published, no tool count | 57 direct registrations in pinned source |
| Response shaping | Curated compact text JSON | Not evaluated | Pretty JSON with [card-list previews and size controls](https://github.com/delorenj/mcp-server-trello/blob/v1.8.1/src/card-list-preview.ts) |

Checked 2026-10-16: Atlassian's [support guide](https://support.atlassian.com/trello/docs/connect-trello-to-ai-assistants-with-trello-mcp/)
describes comments, activity, custom fields and URL attachments, while its [repository](https://github.com/atlassian/trello-mcp-server)
lists them as coming soon. The sources also differ on multi-workspace support. Current coverage was not
independently verified; see both official sources. Here `board_find` lists accessible open boards,
including guest boards, subject to the workspace allowlist.

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
| tools/list | 20801 chars (~5200 tokens) | 4374 chars (~1094 tokens) |
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

## Security

- Credentials live in a private file (`~/.secrets/.trello-mcp.secret`, 0600 in a 0700 directory) or in a
  complete `TRELLO_API_KEY` plus `TRELLO_TOKEN` environment pair, never in client snippets or tool output.
- Your token's permissions are the ceiling; the workspace allowlist and read-only mode only restrict further.
- Every scoped read and write resolves the real owning board. Moves, copies, comments, checklist items,
  labels, custom fields, members and attachments are checked against their actual parents; supplied
  board ids are constraints, not authorization. Unverifiable ownership fails closed.
- Server-policy denials are distinct from upstream failures and never reveal resolved ids or names;
  `server_health` with `detail: "policy"` explains restrictions, not validated authentication.
- Path uploads are off unless `TRELLO_UPLOAD_DIR` is set and are confined to regular files below it;
  downloads use a private directory, OAuth headers, no redirects and a 10 MiB cap.
- Requests have deadlines, credentials are scrubbed from errors, and 429 retries are bounded.
- Windows refuses local path uploads and attachment downloads even with directories set. Base64 uploads
  and URL attachments remain alternatives subject to write/profile policy.
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

Normal releases include native CLI binaries, Claude Desktop bundles (`.mcpb`, pinned CLI 2.1.2) and
checksums. macOS and Windows amd64 bundles are supported; Linux Desktop is beta. Install and update bundles
manually using the setup guide. They are not directory listed and there is no one-click Desktop install.

## License

[GPL-3.0-or-later](LICENSE), copyright Mert Dede. Releases up to v0.5.0 were published under MIT. This is an
independent Go implementation and covers the tool surface of
[delorenj/mcp-server-trello](https://github.com/delorenj/mcp-server-trello) 1.8.1 (MIT); that server is not a
runtime dependency. Trello is a trademark of Atlassian. trello-mcp is an independent project, not affiliated
with or endorsed by Atlassian.
