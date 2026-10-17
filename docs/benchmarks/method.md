# Benchmark method (2026-10-17)

Public-ready description of how the README comparison was measured. Private ids are replaced with `BOARD_ID`, `LIST_ID`, `CARD_ID`, `WORKSPACE_ID`. Results: [`docs/benchmarks/2026-10-17.json`](./2026-10-17.json) in the repo.

## Setup

- One private work board (144 cards), one Trello account, one day (2026-10-17).
- trello-mcp: repository build at commit `cb40466` (v1.0.3 code), stdio.
- Node: `npx -y @delorenj/mcp-server-trello@1.8.1`, stdio.
- Both local servers got the same Trello API key and token through the environment; the harness never reads credentials.
- Official: Atlassian hosted server `https://mcp.trello.com/v1`, connected to Claude Code over OAuth (2 authorized workspaces).

## Calls

| Scenario | trello-mcp | Official | Node 1.8.1 |
| --- | --- | --- | --- |
| Boards | `board_find {}` | `trelloReadBoard {action: list}` | `list_boards {}` |
| 16 cards in one list | `card_find {listId}` (summary, default) and `{listId, detail: full}` | `trelloReadCard {action: list_by_list, listId}` | `get_cards_by_list_id {boardId, listId}` |
| 50 board actions | `board_activity {boardId, limit: 50}` | `trelloReadBoard {action: list_activity, boardId, limit: 50}` | `get_recent_activity {boardId, limit: 50}` |
| One card's details | `card_get {cardId, detail: full}` | `trelloReadCard {action: get, cardIdOrUrl}` | `get_card {cardId}` |
| Comments (2 on that card) | `comment_find {cardId, limit: 20}` | `trelloReadCard {action: list_comments, cardIdOrUrl, limit: 20}` | `get_card_comments {cardId, limit: 20}` |
| Tool definitions | `tools/list` | not available without the OAuth token | `tools/list` |

Official ids are the ARI forms the server returns, e.g. `ari:cloud:trello::list/workspace/WORKSPACE_ID/LIST_ID`.

Exact local plans: [`harness/plan-trello-mcp.json`](./harness/plan-trello-mcp.json), [`harness/plan-node.json`](./harness/plan-node.json).

## Counting

- Local servers: [`harness/mcpc.py`](./harness/mcpc.py) speaks MCP over stdio (initialize, then `tools/call` per step). The measured text is the concatenation of every `text` part in the result `content`, exactly as returned. Count = Python `len()` of that string (Unicode code points). `tools/list` is counted as its result serialized as compact JSON.
- Official: driven from a headless Claude Code session (`claude -p ... --output-format stream-json`), allowed only the official read tools. The measured text is the `tool_result` content text from the stream, counted the same way. The 50-action result exceeded Claude Code's inline limit, so its full text came from the persisted tool-result file (`.[0].text`).
- Boards are reported per returned board because the sets differ: trello-mcp 16 (includes a guest board in another organization), official 15 (2 authorized workspaces), Node 29 (includes 13 closed boards). Totals: 2,399 / 9,330 / 251,785 characters.
- Tool definitions in Claude tokens: one Claude Code session, `/context` per-tool rows summed (rows over 1k display-rounded to 0.1k). trello-mcp 48 tools 8,107; official 15 tools 19,664. Node could not be loaded into `/context` in that setup, so only its `tools/list` characters are given (33,766 vs 20,583).

## Limits

- One board, one day, one account; the board is private so nobody else can rerun on the same data.
- Characters, not tokens. Different servers return different fields, so the responses are not equally complete.
- Official results reflect its hosted version on that date.
- For one card's details the official server returned less (2,662 vs 2,825).
- To reproduce on your own board: run `mcpc.py OUT_DIR plan.json -- <server command>` with your own ids and credentials in the environment. Use an OUT_DIR outside the repository; it holds raw board responses and the server's stderr, which may include credentials or private data. Never commit it. Set `MCPC_TIMEOUT` to the per-RPC deadline in seconds (default: 120).
