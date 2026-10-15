# trello-mcp

A token-efficient Trello MCP server in Go. Let your AI assistant browse boards,
read cards and checklists, and follow activity through a local stdio connection.
No hosted intermediary, no Trello write tools.

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
  read-only mode. The current phase has 17 read and local-selection tools.

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
| Tool definitions | 57 tools, ~7.3k (static audit) | 17 tools, ~1.56k | not comparable |

<!-- bench:end -->

- Response rows use the same synthetic fixtures on both sides: pretty-printed raw
  Trello-shaped JSON, as the Node server returns it, versus actual compact MCP
  text responses. This server's registered `tools/list` result is also measured.
- The reference tool-definition estimate (57 tools, ~7.3k tokens) comes from the
  plan's static 1.8.1 audit, not the offline benchmark. Phase 1 has fewer tools,
  so this row is not a coverage-equivalent comparison.
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
| `TRELLO_TOOLS` | `all` | Comma-separated tool names or groups: `workspace`, `board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`, `server` |
| `TRELLO_READ_ONLY` | `0` | `1` hides Trello write tools; all phase 1 tools remain available |
| `XDG_CONFIG_HOME` | platform configuration directory | Active selection is stored under `trello-mcp/state.json` |
| `HTTPS_PROXY` | unset | Standard Go HTTP proxy configuration |

### Active selection

- `workspace_use` and `board_use` update local defaults, even in read-only mode.
- Selecting a workspace clears the active board. Selecting a board sets its
  owning workspace.
- `board_find` enumerates all account boards, including guest boards, unless an
  explicit `workspaceId` narrows the search.
- Explicit `boardId` values work independently of the active selection.

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
| `list_find` | Find open lists on a board. |

### Cards, comments and checklists

| Tool | Description |
| --- | --- |
| `card_find` | Find board, list or assigned cards with name filtering and local paging. |
| `card_get` | Get a card with optional full detail, extras or markdown. |
| `card_history` | Page card actions with an action-type filter. |
| `comment_find` | Page comments on a card. |
| `checklist_items` | Get items from an authorized checklist. |
| `checklist_get` | Find a card checklist by exact name, default Acceptance Criteria. |

### Board metadata and diagnostics

| Tool | Description |
| --- | --- |
| `member_find` | Find board members by username or full name. |
| `label_find` | Find board labels by name. |
| `customfield_find` | Get field definitions, types and dropdown option ids. |
| `server_health` | Get local basic, detailed, metadata or performance diagnostics. |

See [docs/tools.md](docs/tools.md) for the name mapping to the reference server.
Trello writes and attachment transfers are planned for phase 2, not available now.

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
  Every HTTP response is capped at 8 MiB.

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
- **Ownership checks, not caller claims.** The allowlist applies to every scoped
  read, including card, list, and checklist ids. Ownership is resolved before
  contents are fetched; supplied board ids are constraints, never authorization.
  Unverifiable ownership fails closed.
- **Fresh authorization.** Ownership grants are not cached across calls, so a
  moved board is rechecked. Workspace listings are filtered, and health responses
  never expose saved ids.
- **Read-only Trello access.** All current tools are reads or local-selection
  changes. `TRELLO_READ_ONLY=1` filters Trello write tools; local defaults can
  still change. It does not replace a suitably scoped Trello token.
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
