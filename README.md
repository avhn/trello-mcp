<h1 align="center">trello-mcp</h1>

<p align="center">
  Local Trello MCP server in Go. 48 tools, compact responses, guest boards.
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

Windows: `irm https://raw.githubusercontent.com/avhn/trello-mcp/main/install.ps1 | iex`. Checksum-verified.

Or give your agent:

```text
Set up trello-mcp by following https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md; ask me to run its printed absolute-path auth command myself in a private terminal, then reconnect. Never capture authentication output.
```

Then: run the printed `auth` command yourself in a private terminal, register, reconnect.
Clients and Claude Desktop: [docs/setup.md](docs/setup.md).

## Why

Fewer tokens per response and definition:

| | vs [Official](https://github.com/atlassian/trello-mcp-server) | vs [Node 1.8.1](https://github.com/delorenj/mcp-server-trello/blob/v1.8.1/src/index.ts) |
| --- | --- | --- |
| 30 cards | 69% fewer | 75% fewer |
| 50 board actions | 47% fewer | 77% fewer |
| 20 boards | 88% fewer | 94% fewer |
| Tool definitions | 53% fewer | 29% fewer |

| | trello-mcp | Official | Node 1.8.1 |
| --- | --- | --- | --- |
| Runs | Local binary | Hosted | Local, Bun/Node |
| Guest boards | Yes | No | Yes |
| Delete comments, labels, checklist items | Yes | No | Yes |
| Local file upload, download to disk (Unix) | Yes | No | Inline base64 |
| Per-object workspace allowlist | Yes | Per-workspace consent | Partial |
| Server-side read-only mode | Yes | Not evaluated | Not documented |

Choose official for zero install, web and mobile, Inbox, Planner or keyword search.

Sources: ours measured (below); official measured live 2026-10-15, definitions estimated; Node estimated from
pinned source. Tokens are chars/4, not a controlled benchmark. Guest boards verified 2026-10-16. Official
features per its [repository](https://github.com/atlassian/trello-mcp-server) (2026-09-28) and [guide](https://support.atlassian.com/trello/docs/connect-trello-to-ai-assistants-with-trello-mcp/).

## Read-oriented profile

Ten read tools ([placement](docs/setup.md#optional-read-oriented-profile); `board_use` sets the local default board):

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

## Benchmark

Synthetic fixtures, versus raw Trello JSON for the same objects.

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
| tools/list | 20694 chars (~5174 tokens) | 4400 chars (~1100 tokens) |
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

## Docs

[Setup](docs/setup.md) · [Configuration and security](docs/configuration.md) · [Tools](docs/tools.md)

## License

[GPL-3.0-or-later](LICENSE), copyright Mert Dede (MIT up to v0.5.0). Independent Go implementation of the
[delorenj/mcp-server-trello](https://github.com/delorenj/mcp-server-trello) 1.8.1 (MIT) tool surface, not a
runtime dependency. Trello is a trademark of Atlassian. trello-mcp is an independent project, not affiliated
with or endorsed by Atlassian.
