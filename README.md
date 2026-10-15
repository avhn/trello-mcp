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
- Supported binaries: Linux amd64/arm64, macOS amd64/arm64, Windows amd64.
- Requires `curl`, `tar` (or `unzip` on Windows), and `sha256sum` or `shasum`.
- If a release or platform asset is unavailable, it falls back to `go install`
  at `@latest` when Go is available, even if a release version was requested.
- The installer prints setup instructions only. It never asks for credentials
  or registers an MCP server automatically.

Prefer installing from source? Requires Go 1.26 or newer; the module selects the
Go 1.27.1 toolchain:

```sh
go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest
```

Go installs to `GOBIN`, or normally `~/go/bin`, rather than `~/.local/bin`.

### 2. Get a Trello API key and token

Follow the [Trello API key and token guide](https://developer.atlassian.com/cloud/trello/guides/rest-api/api-introduction/).
Replace the placeholders below locally. Supply credentials through the client's
server environment, not arguments to `trello-mcp`. Never commit real credentials
in a client configuration or share them in chat.

### 3. Connect your MCP client

**Claude Code**

```sh
claude mcp add trello --transport stdio \
  --env TRELLO_API_KEY=YOUR_API_KEY \
  --env TRELLO_TOKEN=YOUR_TOKEN \
  --env TRELLO_READ_ONLY=1 \
  -- "$HOME/.local/bin/trello-mcp"
```

Adjust the executable path if you used Go, a custom directory, or Windows.
The command saves the supplied environment in Claude's local configuration;
substitute credentials only on your own machine, mindful of shell history.
The examples enable read-only mode. Remove `TRELLO_READ_ONLY` or set it to `0`
to expose writes and attachment transfers.

**Other MCP clients**

Merge this into a client configuration that supports `mcpServers`. The executable
must be on the client's PATH, or `command` must be its full installed path.

```json
{
  "mcpServers": {
    "trello": {
      "command": "trello-mcp",
      "env": {
        "TRELLO_API_KEY": "YOUR_API_KEY",
        "TRELLO_TOKEN": "YOUR_TOKEN",
        "TRELLO_READ_ONLY": "1"
      }
    }
  }
}
```

<details>
<summary>OpenCode configuration</summary>

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "trello": {
      "type": "local",
      "command": ["trello-mcp"],
      "environment": {
        "TRELLO_API_KEY": "YOUR_API_KEY",
        "TRELLO_TOKEN": "YOUR_TOKEN",
        "TRELLO_READ_ONLY": "1"
      },
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
  read-only mode. All 47 planned tools are available by default; read-only mode
  exposes 18 reads and local-selection tools.

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
| 20 comments | ~5.42k | ~2.62k | 51.6% |
| 50 board actions | ~11.95k | ~4.95k | 58.6% |
| Card with 20 comments | ~6.21k | ~2.76k | 55.6% |
| Tool definitions | 57 tools, ~7.3k (static audit) | 47 tools, ~4.99k | not comparable |

<!-- bench:end -->

- Response rows use the same synthetic fixtures on both sides: pretty-printed raw
  Trello-shaped JSON, as the Node server returns it, versus actual compact MCP
  text responses. This server's registered `tools/list` result is also measured.
- The reference tool-definition estimate (57 tools, ~7.3k tokens) comes from the
  plan's static 1.8.1 audit, not the offline benchmark. The 47 tools merge the
  reference's parameter variants; repair is intentionally replaced by local
  selection. The definition row is not a like-for-like measured comparison.
- The card-with-comments scenario sums `card_get(detail: "full")` and
  `comment_find(limit: 20)`. Comments are not fetched implicitly.
- Tool-call request sizes, protocol framing, and model-specific tokenization are
  excluded. See [Development](#development) to reproduce or refresh the table.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `TRELLO_API_KEY` | required | Trello API key |
| `TRELLO_TOKEN` | required | Trello API token |
| `TRELLO_BOARD_ID` | saved selection | Initial board; changing it clears the old saved workspace |
| `TRELLO_ALLOWED_WORKSPACES` | unrestricted | Comma-separated workspace ids or canonical names, not display names |
| `TRELLO_TOOLS` | `all` | Comma-separated tool names or prefix groups: `workspace`, `board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`, `attachment`, `server` |
| `TRELLO_READ_ONLY` | `0` | `1` hides all 29 mutation/transfer tools, including local-file downloads; 18 tools remain |
| `TRELLO_DOWNLOAD_DIR` | `os.TempDir()/trello-mcp` | Absolute download directory; created as 0700, existing directories must be private and owned by the current user |
| `TRELLO_UPLOAD_DIR` | unset (path uploads disabled) | Absolute root for local-file uploads; only regular files inside this directory may be uploaded, without following symlinks |
| `XDG_CONFIG_HOME` | platform configuration directory | Active selection is stored under `trello-mcp/state.json` |
| `HTTPS_PROXY` | unset | Standard Go HTTP proxy configuration |

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
| `card_create` | Create a card in a verified list, with optional labels and members. |
| `card_create_batch` | Preflight all ids, then create 1-100 cards with per-item results. |
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
| `attachment_add` | Attach an HTTP(S) URL; the server does not fetch it locally. |
| `attachment_upload` | Upload a local `path` or `base64` with `fileName`. |
| `attachment_download` | Save an uploaded attachment under the private download directory. |
| `server_health` | Get local basic, detailed, metadata or performance diagnostics. |

See [docs/tools.md](docs/tools.md) for parameters and the full reference name mapping.

### Responses and pagination

- Collections return `{"items":[...],"next":"cursor"}`; empty cursors are omitted.
- `card_find` defaults to 100 results (maximum 1000), comments to 20 (maximum 100),
  and activity/history to 20 (maximum 1000).
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
- Writes return only `{id,name?,url?}` or `{ok:true}`. Batch create returns an
  `items` array with zero-based `index`, `id` or `error`. It is not transactional;
  failures after preflight can leave successful earlier cards in place.
- Dates use RFC 3339. Empty `due`/`start` strings clear dates, and explicit false
  values are preserved. Positions accept `top`, `bottom`, or a positive number.

### Custom fields and attachments

- Discover `customFieldId`, type and dropdown option ids with `customfield_find`.
  `customfield_set` takes `cardId`, `customFieldId`, and exactly one of `value`
  or `idValue`. Scalars use `value: {"text":"..."}`, `{"number":"12.5"}`,
  `{"date":"2026-10-20T09:00:00Z"}`, or `{"checked":"false"}`. Dropdowns use
  `idValue: "option-id"`, not the display text. Clear a scalar with `value: ""`
  or a dropdown with `idValue: ""`.
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

- **Token permissions are the ceiling.** The workspace allowlist is an additional
  restriction, not a replacement for token permissions.
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
lint and race checks on Linux and macOS. Also run `shellcheck install.sh` and
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
five supported platforms. Release archives contain `trello-mcp` (or
`trello-mcp.exe`), with `checksums.txt` alongside them on the GitHub release.
Release binaries report their tag with `--version` and through MCP metadata.
No GoReleaser or local release credentials are needed; publishing uses the
workflow's `GITHUB_TOKEN`.

## License and credit

MIT, copyright Mert Dede. [delorenj/mcp-server-trello](https://github.com/delorenj/mcp-server-trello)
(MIT), version 1.8.1, is the reference for tool coverage. This is an independent Go
implementation; the reference server is not a runtime dependency.
