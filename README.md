# trello-mcp

A token-efficient Trello MCP server in Go, running locally over stdio.

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

The response rows are reproduced from the same synthetic fixtures on both sides.
The benchmark compares pretty-printed raw Trello-shaped JSON, as the Node server
returns it, with actual compact MCP text responses. It also measures this server's
registered `tools/list` result. The reference definition estimate (57 tools,
~7.3k tokens) comes from the plan's static 1.8.1 audit, not the offline benchmark;
phase 1 has fewer tools, so that row is not a coverage-equivalent comparison.
The card-with-comments scenario sums `card_get(detail: "full")` and
`comment_find(limit: 20)`; comments are not fetched implicitly. Tool-call request
sizes, protocol framing and model-specific tokenization are excluded.

To print the local raw/projected character and token table and refresh this table:

```sh
go test ./internal/bench -v -update
```

## Why

- Works across all workspaces and guest boards visible to your API token, with an
  optional workspace allowlist. The [official documentation](https://support.atlassian.com/trello/docs/connect-trello-to-ai-assistants-with-trello-mcp/)
  says Atlassian's connector can access only workspaces explicitly authorized on
  its consent screen. It does not establish whether guest-only boards are
  accessible, so no guest-board exclusion is claimed here.
- Runs locally over stdio with an API key and token, with no hosted service in
  between.
- Responses project only the fields an agent needs, with compact JSON and no
  duplicate structured content or output schemas.
- Optional workspace allowlist and read-only mode reduce the exposed surface.

## Install

Requires Go 1.26 or newer; the module selects the Go 1.27.1 toolchain.

```sh
go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest
```

For a source checkout:

```sh
go build -o trello-mcp ./cmd/trello-mcp
```

## Configuration

Obtain a [Trello API key and token](https://developer.atlassian.com/cloud/trello/guides/rest-api/api-introduction/).
Supply credentials through the client environment, not command-line arguments.
Never commit a client configuration containing real credentials.

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

Phase 1 has 17 read and local-selection tools, not Trello write tools.
`workspace_use` and `board_use` update local defaults even in read-only mode.
Selecting a workspace clears the active board. Selecting a board sets its owning
workspace. `board_find` enumerates all account boards, including guest boards,
unless an explicit `workspaceId` narrows the search. Explicit `boardId` values
work independently of the active selection.

The allowlist applies to every scoped read, including ids for cards, lists and
checklists. Object ownership is resolved before fetching contents; supplied board
ids are constraints, never authorization. Unverifiable ownership fails closed.
Workspace listings are filtered and health responses never expose saved ids.
Ownership grants are not cached across calls, so a moved board is rechecked.
An allowlist is an additional restriction, not a replacement for token permissions.

Collections return `{"items":[...],"next":"cursor"}`; empty cursors are omitted.
`card_find` defaults to 100 results (maximum 1000), comments to 20 (maximum 100),
and activity/history to 20 (maximum 1000). Actions use Trello's last-action-id
`before` cursor and accept `since`; history also accepts a comma-separated action
`filter`. A full action page advertises a possible next page, which may be empty.

Board/list/member card endpoints do not document cursor pagination. `card_find`
fetches a bounded snapshot, applies case-insensitive `nameFilter` first, then
pages locally using the last card id as `before`. It never sends unsupported
`before` or `limit` parameters to those endpoints. A changed or missing cursor
returns an error rather than restarting. Snapshots can change between calls;
there is no stable snapshot guarantee. Every HTTP response is capped at 8 MiB.

`board_get`, `card_find` and `card_get` support `detail: "summary"` (default) or
`"full"`. Collection descriptions are capped at 200 Unicode characters with a
`descTruncated` flag. `card_get` keeps the full description and supports
`view: "markdown"`, returned as a JSON object containing `markdown`. Its bounded
`extraFields` array accepts only `stickers`, `pluginData`, `votes` and
`customFieldItems`. No tool requests `fields=all` or embeds attachment data.

Requests have deadlines, rolling limits of 300/10s per key and 100/10s per token,
and a conservative 100/900s limit for all member-prefixed requests. Rate headers
can reduce the available budget. HTTP 429 permits at most three retries,
honoring `Retry-After` across concurrent calls; delays above 30s fail instead of
retrying early. Redirects
are refused, credentials are scrubbed from errors, and upstream error bodies are
never returned. Limits are per process; other clients sharing the credentials
still consume the same upstream quota.

### Claude Code

Put this under `mcpServers` in your client configuration, substituting credentials
locally. The installed `trello-mcp` executable must be on the client's PATH.

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

### OpenCode

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

## Tools

`boardId` defaults to the active board for board-scoped tools. For object-scoped
tools it is an optional ownership constraint; the object's own board is resolved
independently. `mine: true` in `card_find` selects account-wide assigned cards and
cannot be combined with `boardId` or `listId`.

| Tool | Description |
| --- | --- |
| `workspace_find` | Find allowed workspaces by name. |
| `workspace_use` | Set the active workspace and clear the active board. |
| `board_find` | Find account boards or boards in a workspace. |
| `board_use` | Set an authorized board as the local default. |
| `board_get` | Get curated summary or full board details. |
| `board_activity` | Page board actions with since/before cursors. |
| `list_find` | Find open lists on a board. |
| `card_find` | Find board, list or assigned cards with name filtering and local paging. |
| `card_get` | Get a card with optional full detail, extras or markdown. |
| `card_history` | Page card actions with an action-type filter. |
| `comment_find` | Page comments on a card. |
| `checklist_items` | Get items from an authorized checklist. |
| `checklist_get` | Find a card checklist by exact name, default Acceptance Criteria. |
| `member_find` | Find board members by username or full name. |
| `label_find` | Find board labels by name. |
| `customfield_find` | Get field definitions, types and dropdown option ids. |
| `server_health` | Get local basic, detailed, metadata or performance diagnostics. |

The name mapping to the reference server is in [docs/tools.md](docs/tools.md).
Trello writes and attachment transfers are planned for phase 2.

## Development

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Tests use only synthetic fixtures and `httptest`, never a Trello account. CI adds
lint and race checks on Linux and macOS. The size benchmark can be included in a
single whole-suite update with `TRELLO_BENCH_UPDATE=1 go test ./...`.

## License and credit

MIT, copyright Mert Dede. [delorenj/mcp-server-trello](https://github.com/delorenj/mcp-server-trello)
(MIT), version 1.8.1, is the reference for tool coverage. This is an independent Go
implementation; the reference server is not a runtime dependency.
