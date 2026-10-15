# Tool coverage

The MIT-licensed `@delorenj/mcp-server-trello` 1.8.1 is the coverage reference.
Phase 1 implements these renamed read and local-selection capabilities.

| This server | Reference names |
| --- | --- |
| `workspace_find` | `list_workspaces` |
| `workspace_use` | `set_active_workspace` |
| `board_find` | `list_boards`, `list_boards_in_workspace` |
| `board_use` | `set_active_board` |
| `board_get` | `get_active_board_info` |
| `board_activity` | `get_recent_activity` |
| `list_find` | `get_lists` |
| `card_find` | `get_cards_by_list_id`, `get_my_cards` |
| `card_get` | `get_card` |
| `card_history` | `get_card_history` |
| `comment_find` | `get_card_comments` |
| `checklist_items` | `get_checklist_items` |
| `checklist_get` | `get_checklist_by_name`, `get_acceptance_criteria` |
| `member_find` | `get_board_members` |
| `label_find` | `get_board_labels` |
| `customfield_find` | `get_board_custom_fields` |
| `server_health` | `get_health`, `get_health_detailed`, `get_health_metadata`, `get_health_performance` |

## Review requirements and phase boundaries

The approved review findings are part of the contract, not optional suggestions.

1. Every scoped read checks the actual owning board. Object ids cannot bypass the
   workspace allowlist. Future action/comment, label and custom-field mutations
   must add ownership resolvers before registration; move/copy must check both
   boards. Unsupported resolver kinds fail closed.
2. `list_update` is phase 2. It must support `idBoard` with both boards checked,
   and `closed: false` for reopening.
3. `card_get` includes an opt-in markdown view and a bounded extra-field allowlist
   for stickers, plugin data, votes and custom-field values.
4. Board activity preserves `since` and `before`. Card history preserves its
   action-type filter. Action paging uses the last action id.
5. Card names are filtered before the limit. Board/list/member card endpoints
   have no documented cursor support, so cards page a bounded local snapshot.
6. The client enforces key, token and conservative member-route quotas, observes
   rate headers and bounds 429 retries and waits. Limits are local to a process.
7. Attachment transfer is phase 2. Downloads must use the OAuth Authorization
   header, use attachment `fileName` and never forward credentials on redirects.
8. Attachment transfer is phase 2. Verify directory ownership/type, refuse
   symlinks, create files exclusively, sanitize names and enforce a size cap.
9. Health supports `basic`, `detailed`, `metadata` and `performance`, all local and
   without saved ids, credentials or upstream content. It is not an upstream
   readiness check and does not claim Trello is reachable.
10. Custom-field definitions preserve dropdown option ids and all scalar types.
    Phase 2 writes must use `idValue` for lists, typed `value` otherwise, and an
    empty value to clear, with each type covered by tests.

Trello write tools, batch writes and attachment operations are intentionally not
registered in phase 1. The reference's repair tool is not retained; local
selection tools already cover its behavior.
