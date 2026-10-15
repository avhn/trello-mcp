# Tool coverage

The MIT-licensed `@delorenj/mcp-server-trello` 1.8.1 is the coverage reference.
All 47 planned tools are registered by default. Parameter variants are merged;
the reference's repair operation is replaced by explicit local selection.

| This server | Reference names |
| --- | --- |
| `workspace_find` | `list_workspaces` |
| `workspace_use` | `set_active_workspace` |
| `board_find` | `list_boards`, `list_boards_in_workspace` |
| `board_use` | `set_active_board` |
| `board_get` | `get_active_board_info` |
| `board_create` | `create_board` |
| `board_activity` | `get_recent_activity` |
| `list_find` | `get_lists` |
| `list_create` | `add_list_to_board` |
| `list_update` | `update_list`, `update_list_position` |
| `list_archive` | `archive_list` |
| `list_watch` | `watch_list` |
| `card_find` | `get_cards_by_list_id`, `get_my_cards` |
| `card_get` | `get_card` |
| `card_create` | `add_card_to_list` |
| `card_create_batch` | `add_cards_to_list` |
| `card_update` | `update_card_details` |
| `card_archive` | `archive_card` |
| `card_watch` | `watch_card` |
| `card_move` | `move_card` |
| `card_copy` | `copy_card` |
| `card_history` | `get_card_history` |
| `comment_find` | `get_card_comments` |
| `comment_create` | `add_comment` |
| `comment_update` | `update_comment` |
| `comment_delete` | `delete_comment` |
| `checklist_create` | `create_checklist` |
| `checklist_items` | `get_checklist_items` |
| `checklist_copy` | `copy_checklist` |
| `checklist_get` | `get_checklist_by_name`, `get_acceptance_criteria` |
| `checklist_item_create` | `add_checklist_item` |
| `checklist_item_find` | `find_checklist_items_by_description` |
| `checklist_item_update` | `update_checklist_item` |
| `checklist_item_delete` | `delete_checklist_item` |
| `member_find` | `get_board_members` |
| `card_member_add` | `assign_member_to_card` |
| `card_member_remove` | `remove_member_from_card` |
| `label_find` | `get_board_labels` |
| `label_create` | `create_label` |
| `label_update` | `update_label` |
| `label_delete` | `delete_label` |
| `customfield_find` | `get_board_custom_fields` |
| `customfield_set` | `update_card_custom_field` |
| `attachment_add` | `attach_image_to_card`, `attach_file_to_card` |
| `attachment_upload` | `attach_data_to_card`, `attach_image_data_to_card` |
| `attachment_download` | `download_attachment` |
| `server_health` | `get_health`, `get_health_detailed`, `get_health_metadata`, `get_health_performance` |

## Profiles and common parameters

`TRELLO_TOOLS` accepts comma-separated tool names and prefix groups: `workspace`,
`board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`,
`attachment`, `server`, or `all`. Selectors form a union, not an intersection.
For example, `card` includes both card-member tools. Unknown selectors fail
startup. `TRELLO_READ_ONLY=1` then removes all 29 mutation/transfer tools, even
explicitly selected ones. The 18 reads and local-selection tools remain eligible.
Downloads are filtered too because they write local files.

- `boardId` defaults to the active board on board-scoped calls. On object-scoped
  calls it is an optional ownership constraint, not a source of authorization.
- Object ids are required for object-scoped calls: `cardId`, `listId`,
  `checklistId`, `commentId`, `itemId`, `labelId`, `customFieldId`, `memberId`,
  `attachmentId`, as appropriate. Item mutations require both `checklistId` and
  `itemId`; comment mutations require both `cardId` and `commentId`.
- `pos` accepts `top`, `bottom`, or a positive number. Dates use RFC 3339;
  empty `due` or `start` clears a date. Optional booleans preserve explicit false.
- Every result is one compact JSON text block, without structured content or an
  output schema. Writes return `{id,name?,url?}` or `{ok:true}`, never raw objects.

## Mutation parameters

| Tool(s) | Required input | Optional input beyond `boardId` |
| --- | --- | --- |
| `board_create` | `name` | `workspaceId` (active workspace default), `desc`, `defaultLists` |
| `list_create` | `name` | `pos` |
| `list_update` | `listId`, at least one update | `name`, `pos`, `idBoard` (destination), `closed` |
| `list_archive` / `list_watch` | `listId` | watch: `subscribed` (default true) |
| `card_create` | `listId`, `name` | `desc`, `due`, `start`, `pos`, `idMembers`, `idLabels` |
| `card_create_batch` | `listId`, `cards` | each card has `name` and the same optional creation fields |
| `card_update` | `cardId`, at least one update | `name`, `desc`, `due`, `start`, `dueComplete`, `closed`, `pos`, `idMembers`, `idLabels` |
| `card_archive` / `card_watch` | `cardId` | watch: `subscribed` (default true) |
| `card_move` / `card_copy` | `cardId`, `listId` (destination) | `idBoard` (destination constraint), `pos`; copy: `name` |
| `comment_create` | `cardId`, `text` | none |
| `comment_update` / `comment_delete` | `cardId`, `commentId` | update also requires `text` |
| `checklist_create` | `cardId`, `name` | `pos` |
| `checklist_copy` | `checklistId` (source), `cardId` (destination) | `name`, `pos`; `boardId` constrains destination |
| `checklist_item_create` | `checklistId`, `name` | `pos`, `checked`, `due`, `memberId` |
| `checklist_item_update` | `checklistId`, `itemId`, at least one update | `name`, `state` (`complete`/`incomplete`), `pos`, `due`, `memberId` |
| `checklist_item_delete` | `checklistId`, `itemId` | none |
| `card_member_add` / `card_member_remove` | `cardId`, `memberId` | none |
| `label_create` | `name` | `color` |
| `label_update` | `labelId`, at least one update | `name`, `color` |
| `label_delete` | `labelId` | none |
| `customfield_set` | `cardId`, `customFieldId`, exactly one value input | `value` or `idValue`, as below |
| `attachment_add` | `cardId`, HTTP(S) `url` | display `name` |
| `attachment_upload` | `cardId`, exactly one of confined `path` or `base64` | `fileName` (required for base64), `mimeType`, display `name` |
| `attachment_download` | `cardId`, `attachmentId` | none |

Batch create accepts 1-100 cards. All referenced ids are preflighted before the
first mutation, then checked again per card. Results are `items` with zero-based
`index` and either `id` or `error`. This is not a transaction; a later runtime
failure does not roll back earlier successful cards. Do not blindly repeat an
ambiguous write after a transport failure, as it might already have succeeded.

`checklist_item_find` is read-only and accepts `checklistId`, optional `boardId`
and case-insensitive `nameFilter` (matching the item's description/name).

## Custom fields

`customfield_find` returns ids, names, types and dropdown options with their ids.
`customfield_set` verifies the card and field share a board and derives the type
from the current definition. Scalar inputs are typed string-valued objects:

```json
{"value":{"text":"Example"}}
{"value":{"number":"12.5"}}
{"value":{"date":"2026-10-20T09:00:00Z"}}
{"value":{"checked":"false"}}
```

List fields require `{"idValue":"option-id"}` using a returned option id, not
its display text. Options from another field are rejected. Clear scalars with
`{"value":""}` and dropdowns with `{"idValue":""}`. The latter is translated
into Trello's empty-value clear request. Unknown types and mismatched values
are rejected before mutation.

## Attachments

Uploads accept regular local files (not symlinks) or standard base64. Path
uploads are disabled by default. To enable them, set `TRELLO_UPLOAD_DIR` to an
absolute directory containing only files intended for upload. A `path` may be
relative to that root or absolute inside it after cleaning; paths escaping the
root, including `..` escapes, are refused. The root is opened without following
symlinks, and each child component is opened relative to a pinned directory
descriptor without following symlinks. Dot-files and dot-directories inside the
root are allowed. Path verification requires Unix (including Linux and macOS);
other platforms refuse path uploads. Base64 uploads still work without
`TRELLO_UPLOAD_DIR` and require `fileName`.

Filenames are sanitized and MIME types validated. Both directions have a 10 MiB
cap. URL attachments are passed to Trello; this process never fetches arbitrary URLs.

Downloads resolve the card and verify attachment membership through its
card-scoped metadata endpoint. The HTTP download uses OAuth Authorization,
attachment `fileName` rather than display name, and no query credentials. All
redirects are refused, so credentials cannot follow even a same-origin redirect.

`TRELLO_DOWNLOAD_DIR` defaults to `os.TempDir()/trello-mcp`. Missing directories
are created as 0700. Existing directories must be private, owned by the current
user and not symlinks. A pinned directory root and exclusive 0600 creation avoid
following or overwriting target symlinks. Names are
`attachmentId-sanitized_fileName`; collisions fail instead of overwriting.
Failed or oversized transfers are removed. Results contain only `path`, `size`,
`mimeType`, never inline file bodies. Directory ownership verification is
currently Unix-only (including Linux and macOS); other platforms fail closed.

## Review requirements

1. Every scoped read and write resolves the actual owning board. Object ids
   cannot bypass the workspace allowlist. Action/comment, label and custom-field
   mutations use ownership resolvers; checklist items, members and attachments
   are verified through their parents. Move/copy checks both ends. Unsupported
   or unverifiable ownership fails closed. The upstream API cannot make this
   check atomic with mutation against concurrent upstream moves.
2. `list_update` supports `idBoard`, checking both boards, and `closed: false`
   for reopening.
3. `card_get` includes opt-in markdown and bounded extras: stickers, plugin data,
   votes and custom-field values.
4. Board activity keeps `since`/`before`; card history keeps action-type filters.
   Action paging uses the last action id.
5. Names are filtered before card limits. Board/list/member card endpoints have
   no documented cursor support, so cards page a bounded local snapshot.
6. The client enforces key, token and conservative member-route quotas, observes
   rate headers and bounds 429 retries and waits. Limits are local to a process.
7. Attachment downloads use OAuth headers, attachment `fileName` and no redirects.
8. Download storage verifies directory ownership/type, rejects symlinks, creates
   exclusively, sanitizes names and enforces a size cap.
9. Health modes `basic`, `detailed`, `metadata`, `performance` are all local:
   `basic` returns `ok`; `detailed` adds profile and selection-presence booleans;
   `metadata` reports implementation/runtime/transport; `performance` reports
   uptime/request/retry counters. None exposes saved ids, credentials or upstream
   content. This is not an upstream readiness check.
10. Dropdown ids and all scalar types are preserved and tested, including writes
    and empty-value clearing for each type.
