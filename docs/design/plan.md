# Plan: trello-mcp, a token-efficient Trello MCP server in Go

Version 1. Author: Claude coordinator. Status: draft for one GPT plan review.

## Goal

A new public project `github.com/avhn/trello-mcp` (local checkout `~/trello-mcp`)
that covers every capability of `@delorenj/mcp-server-trello@1.8.1` (57 tools)
with far smaller tool responses and a smaller tool-definition footprint. Tool
names are new (user decision). The README opens with the measured improvement
table against the Node server and a short statement of why this server exists
(multi-workspace use; wording of the Atlassian comparison only after it is
verified against official sources).

## Evidence this plan rests on (static audit of 1.8.1, estimates chars/4)

- Responses are raw Trello objects, pretty-printed, almost no `fields=`:
  30 cards ~11.7k tokens vs ~2.9k trimmed; card with 20 comments ~8.8k vs ~1.3k;
  20 comments ~7.4k vs ~1.2k; 50 actions ~18.5k vs ~2.9k; 20 boards ~6.7k vs ~0.5k.
- `get_card` always fetches fields=all plus attachments, checklists, members,
  stickers, pluginData, 100 comment actions.
- `download_attachment` returns base64 inline (1 MB file ~350k tokens).
- 57 tools, ~7.3k tokens of definitions; no server-side way to hide tools.
- Env: TRELLO_API_KEY, TRELLO_TOKEN, TRELLO_BOARD_ID, TRELLO_ALLOWED_WORKSPACES,
  HTTPS_PROXY; active board/workspace persisted to ~/.trello-mcp/config.json.

## Stack and layout

- Go module `github.com/avhn/trello-mcp`, `go 1.26.0` floor (toolchain 1.27).
- MCP: official `github.com/modelcontextprotocol/go-sdk` v1.8.0, stdio transport only.
- No other runtime deps beyond stdlib (net/http, encoding/json, log/slog to stderr).

```text
cmd/trello-mcp/main.go        entry point only
internal/config/              typed Config from env, fail fast on missing key/token
internal/trello/              HTTP client: auth query params, rate limiter, retries, typed API calls
internal/view/                projection types + compact encoder (the token-efficiency core)
internal/tools/               one file per domain, registers tools on the SDK server
internal/state/               active board/workspace, persisted to $XDG_CONFIG_HOME/trello-mcp/state.json
internal/bench/               size comparison test that produces the README table
testdata/trello/              synthetic Trello API responses (no real board data)
docs/tools.md                 old-name to new-name mapping
```

## Token-efficiency design (applies to every tool)

1. Ask Trello for less: every GET sends explicit `fields=`, `card_fields=`,
   `member_fields=`, `memberCreator_fields=`, `action_fields=`, `limit=`.
2. Project into small typed structs with `omitempty`; drop badges, prefs,
   limits, descData, cover, emoji, pluginData, labelNames etc.
3. Compact JSON (no indentation) as a single TextContent block. No
   `structuredContent` and no output schemas (the go-sdk typed handler would
   emit both text and structured content, doubling the payload, and output
   schemas add static tokens). Handlers use the raw tool API or a wrapper that
   returns only TextContent.
4. `detail` enum (`summary` default, `full`) on card_get, card_find, board_get
   for callers that need more; `full` is still a curated set, never fields=all.
5. Long text: card_find truncates desc to 200 chars with a `descTruncated`
   flag; card_get returns full desc.
6. Writes return only `{id, name?, url?}` or `{ok:true}`; batch create returns
   ids plus per-item errors.
7. Collections have default and max limits (card_find 100/1000, comment_find
   20/100, board_activity 20/1000, card_history 20/1000) and return a
   `next` cursor (Trello `before` action id or last card id) when truncated.
8. Attachments: downloads are written to a file under `TRELLO_DOWNLOAD_DIR`
   (default os.TempDir()/trello-mcp, 0700) and the tool returns path, size,
   mimeType; never inline base64. Uploads accept a local file path or base64.
9. Static footprint: short descriptions, no repeated prose; shared params
   (boardId) documented once as "defaults to the active board". Optional
   `TRELLO_TOOLS` (comma list of groups or tool names, default `all`) and
   `TRELLO_READ_ONLY=1` to register fewer tools.

## Tool map (57 old tools -> 47 new; merges are parameter variants only)

| New | Old |
| --- | --- |
| workspace_find, workspace_use | list_workspaces, set_active_workspace |
| board_find(workspaceId?) | list_boards, list_boards_in_workspace |
| board_use, board_get, board_create | set_active_board, get_active_board_info, create_board |
| board_activity | get_recent_activity |
| list_find, list_create, list_archive, list_watch | get_lists, add_list_to_board, archive_list, watch_list |
| list_update(name?, pos?) | update_list, update_list_position |
| card_find(listId? / boardId? / mine?, nameFilter?) | get_cards_by_list_id, get_my_cards |
| card_get, card_create, card_create_batch, card_update | get_card, add_card_to_list, add_cards_to_list, update_card_details |
| card_archive, card_watch, card_move, card_copy, card_history | archive_card, watch_card, move_card, copy_card, get_card_history |
| comment_find, comment_create, comment_update, comment_delete | get_card_comments, add_comment, update_comment, delete_comment |
| checklist_create, checklist_items, checklist_copy | create_checklist, get_checklist_items, copy_checklist |
| checklist_get(name, default "Acceptance Criteria") | get_checklist_by_name, get_acceptance_criteria |
| checklist_item_create, checklist_item_find, checklist_item_update, checklist_item_delete | add_checklist_item, find_checklist_items_by_description, update_checklist_item, delete_checklist_item |
| member_find, card_member_add, card_member_remove | get_board_members, assign_member_to_card, remove_member_from_card |
| label_find, label_create, label_update, label_delete | get_board_labels, create_label, update_label, delete_label |
| customfield_find, customfield_set | get_board_custom_fields, update_card_custom_field |
| attachment_add(url) | attach_image_to_card, attach_file_to_card |
| attachment_upload(path or base64) | attach_data_to_card, attach_image_data_to_card |
| attachment_download (to file) | download_attachment |
| server_health(detail?) | get_health, get_health_detailed, get_health_metadata, get_health_performance, perform_system_repair (repair dropped: it only re-selects an active board) |

## Multi-workspace and safety

- `TRELLO_ALLOWED_WORKSPACES` (comma list of ids or names) enforced on every
  board-scoped call by resolving the board's idOrganization (cached); calls
  outside the allowlist fail with a clear error. Unset means no restriction.
- Every board-scoped tool accepts an explicit `boardId`; the active board is
  only a default, so one session can work across workspaces without switching.
- Credentials only from env, sent as query params per Trello API; never logged;
  error messages scrub the key and token from URLs.
- Rate limiting: token bucket 100 req / 10 s per token, retry on 429 with
  backoff honoring Retry-After, max 3 retries; context timeouts on every call.
- HTTPS_PROXY honored via net/http default proxy.

## Testing

- `httptest` fake Trello serving `testdata/trello/*.json` (synthetic, shaped
  like the documented default responses); table-driven tests per tool assert
  the request query (fields, limits) and the exact compact output.
- `internal/bench`: for each scenario in the README table, render the Node
  server's equivalent (raw fixture, JSON.stringify(..., null, 2) equivalent via
  json.MarshalIndent) and this server's output; report chars and chars/4 tokens.
  Also counts tool-definition size from the registered tools/list. A `-update`
  flag rewrites the README table between markers, so numbers are reproducible.
- `gofmt`, `go vet`, `golangci-lint` (copy of the org v2 config), `go test -race ./...`.
- GitHub Actions: ubuntu and macos, same checks.

## README

1. Title, one line.
2. Improvement table (from internal/bench) with the method stated: synthetic
   fixtures, chars/4 estimate, compared with @delorenj/mcp-server-trello 1.8.1.
   Tool-definition footprint row included.
3. "Why": works across every Trello workspace the token can see, with an
   optional allowlist; the Atlassian comparison sentence is added only in the
   wording a docs check confirms, with a link to the source.
4. Install (`go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest`),
   configuration (env table), client config snippets (Claude Code, OpenCode),
   tool list, credit to the Node server (MIT) as the reference for coverage.
   No mention of private tooling or the internal standards catalog.

## Delivery and routing

1. Scaffold, config, trello client, view, state, read tools (boards, lists,
   cards, comments, activity, history), bench, README: `gpt-implementer`.
2. Write tools, checklists, labels, members, custom fields, attachments,
   health, tool profiles: `gpt-implementer`.
3. After each phase: one `gpt-code-reviewer` pass on the diff, then
   `gpt-terra` runs gofmt/vet/lint/test. User directive: GPT does the bulk,
   Claude only orchestrates, so the Claude diff review is skipped by request.
4. Local commits only (conventional, signed). Creating the GitHub repo and
   pushing is an outward action and waits for explicit user approval.
5. Switching the dotfiles launchers (opencode.jsonc, ~/.claude.json, the
   test-claude pin, secrets skill doc) to the new binary is a separate
   follow-up in the workspace repo, after the user tries it.

## Open risks

- Exact default field sets of Trello collection endpoints are not fully
  documented; fixtures are representative, so README numbers are labelled as
  synthetic estimates until a live comparison is run by the user.
- go-sdk raw-handler API surface for returning text-only results must be
  checked against v1.8.0 docs before phase 1.

## Review round 1 (gpt-plan-reviewer): REVISE, to fold in before approval

1. Blocker: allowlist must resolve the owning board of every object id (card, list,
   checklist, comment/action, label, custom field), both ends of move/copy; reject unverifiable.
2. list_update also needs idBoard (move, both boards checked) and closed:false (reopen).
3. card_get: keep an opt-in markdown view and a bounded extra-fields option
   (stickers, pluginData, votes, custom field values).
4. board_activity keeps since/before; card_history keeps action-type filter; page by last action id.
5. card_find: apply nameFilter before the limit; verify cursor support per endpoint.
6. Rate limits: per key 300/10s and per token 100/10s, /1/members/ 100/900s; read limit
   headers, bounded 429 recovery.
7. Attachment download uses the OAuth Authorization header, attachment fileName, no creds on redirects.
8. Download dir: verify ownership and type (no symlink), exclusive create, sanitized names, size cap.
9. server_health detail modes: basic, detailed, metadata, performance.
10. customfield_find returns dropdown option ids; customfield_set uses idValue for lists,
    typed value otherwise, empty to clear; test each type.
SDK confirmed: v1.8.0 Server.AddTool raw ToolHandler returns text-only CallToolResult;
StdioTransport; raw handlers need own argument validation.
