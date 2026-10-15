# Tool coverage

The MIT-licensed `@delorenj/mcp-server-trello` 1.8.1 is the coverage reference.
With credentials configured, all 48 tools are registered by default. Parameter
variants are merged; the reference's repair operation is replaced by explicit
local selection.

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
| `attachment_find` | No reference equivalent; discover card attachment ids |
| `attachment_add` | `attach_image_to_card`, `attach_file_to_card` |
| `attachment_upload` | `attach_data_to_card`, `attach_image_data_to_card` |
| `attachment_download` | `download_attachment` |
| `server_health` | `get_health`, `get_health_detailed`, `get_health_metadata`, `get_health_performance` |

## Credentials

Credential storage, precedence, permissions and troubleshooting are documented once, in
[configuration](configuration.md). Install and client registration are in [setup](setup.md).
Credentials never appear in tool arguments, tool results or client snippets.

## Profiles and common parameters

`TRELLO_TOOLS` accepts comma-separated tool names and prefix groups: `workspace`,
`board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`,
`attachment`, `server`, or `all`. Selectors form a union, not an intersection.
For example, `card` includes both card-member tools. Unknown selectors fail
startup. Write access is the default for `trello-mcp auth`. Read-only is opt-in via
`trello-mcp auth --read-only` or `TRELLO_READ_ONLY=1`; either then removes all 29
mutation/transfer tools, even explicitly selected ones. The 19 reads and
local-selection tools remain eligible. A saved token with `scope=read` implies
read-only mode even if `TRELLO_READ_ONLY=0`.

The named read-oriented discovery profile has exactly ten tools. `board_use` still changes
the local default board, so the profile is not wholly read-only:

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

With no credentials the server still initializes and registers only `server_health`,
regardless of the selected profile. Initialization instructions and every health mode
tell the human to run `trello-mcp auth` in a private terminal, then restart or reconnect the
client. Never request keys, tokens or authorization links in chat. See
[configuration](configuration.md) for the exact message and precedence.

- `boardId` is described per tool. Board collection tools default to the active board;
  `board_use` requires it; object-scoped tools accept it only as an optional expected
  owning-board constraint, never as authorization. `card_find` with `listId` checks that
  list's board and otherwise uses the active board unless `mine` is true.
- Object ids are required for object-scoped calls: `cardId`, `listId`,
  `checklistId`, `commentId`, `itemId`, `labelId`, `customFieldId`, `memberId`,
  `attachmentId`, as appropriate. Item mutations require both `checklistId` and
  `itemId`; comment mutations require both `cardId` and `commentId`.
- `pos` accepts `top`, `bottom`, or a positive number. Dates use RFC 3339;
  empty `due` or `start` clears a date, and empty label `color` clears a color. Optional
  booleans preserve explicit false.
- Every result is one compact JSON text block, without structured content or an
  output schema. Writes return `{id,name?,url?}` or `{ok:true}`, never raw objects;
  card creation can also include checklist progress and an error.
- `card_find` (default `detail: "summary"`) returns IDs, ownership, labels, members, dates,
  status and a description preview of at most 200 characters with `descTruncated`, but no `url`.
  `detail: "full"` adds `url`; `card_get` keeps `url` at both detail levels. This summary change is
  intentional before 1.0. Filters, authorization, IDs and `next` cursors are identical in both modes.
- `card_find` `before` is a card ID returned earlier in the same filtered snapshot, never a date.
  Dates and malformed IDs are rejected before any request; syntactically valid IDs are checked against
  the freshly fetched filtered snapshot and rejected if absent. `mine: true` selects account-wide assigned cards and
  cannot be combined with `boardId` or `listId`. `state` accepts `complete` or `incomplete`.
- Unknown arguments name the key, object location and allowed keys. Duplicate
  arguments name the key and location; type errors name the field and expected type.

## Discovering access policy

The MCP initialize response includes a short `instructions` summary generated from startup
configuration: whether workspaces are restricted or unrestricted, read-only mode, path uploads and
downloads, and a pointer to policy health. Selectors, credentials and saved active ids are never included.

Call `server_health` with `detail: "policy"` for compact text-only JSON containing:

- `restricted` and `allowedWorkspaces`: entries contain the configured `selector`
  as given, plus resolved `id` and canonical `name` when available. Only allowed
  workspace names are returned. One cached best-effort request to
  `/members/me/organizations` with explicit `fields=id,name` resolves selectors;
  lookup failures leave them unresolved and do not fail health. This diagnostic
  cache is never used to grant access.
- `readOnly`, `credentialScope` (when loaded from file), `toolProfile` (the
  `TRELLO_TOOLS` selectors), `enabledToolCount`, and
  `hiddenWriteTools` (selected tools hidden specifically by read-only mode).
- `uploads` and `downloads`, each with `enabled` and `dir`. Availability accounts
  for tool selection, read-only mode and platform support; `uploads` describes
  local path uploads, not base64. Paths report configuration, not filesystem
  readiness. Unconfigured upload directories are empty strings.

Custom profiles should include `server` or `server_health` to retain diagnostics.
The health `detail` enum is `basic`, `detailed`, `metadata`, `performance`, `policy`.

Policy denials use `{"error":"access denied by <policy>: <reason>. This is server
policy, not a token/permission error. ..."}` and include a corrective action plus
`server_health` with `detail "policy"`. Allowlist errors identify
`TRELLO_ALLOWED_WORKSPACES`, configured selectors and only the caller-supplied
object id, never resolved denied board/workspace ids or names. For example:
`card c1 is outside the allowed workspaces (allowed: team)`. Unverifiable
ownership explicitly fails closed without exposing resolved parent ids. Only
after the allowlist passes, a supplied `boardId` mismatch reports `object <id> belongs to board <actual>, not the supplied
boardId <expected>`. Read-only, confined path uploads and platform transfer
refusals follow the same format. Do not assume these failures mean a broken token.

Upstream HTTP failures instead use `trello API: HTTP <status>`; 401/403 explicitly
say `token/permission issue, not server policy`. Error bodies are never returned,
and credentials remain scrubbed from diagnostics.

## Activity and comments

`board_activity`, `card_history` and `comment_find` share a flat, type-specific projection:
`id`, `type`, `date`, then only relevant fields. `by` is the username (the member id when Trello has no
username); `cardId`, `listId`, `listBeforeId`, `listAfterId` and `boardId` are IDs, with `boardId` omitted
when it equals the queried board. Entity names (`card`, `list`, `listBefore`, `listAfter`) appear on the first
occurrence in a page and whenever the name changes; an omitted name means the last name for that ID earlier in
the same page. Every new page starts fresh, so its first occurrence is complete. `checklist`, `item` and `state`
describe progress; attachments keep id and name, never URLs. Comment `text` is unchanged.
`old` keeps previous scalar values, including false, zero, null and empty strings, with strings capped at
80 Unicode characters; `oldTruncated` lists shortened fields and `changed` lists fields whose non-scalar old
values were omitted. Other nested metadata and profile details are dropped.

Actions page with the last action ID as `before` and accept `since`; both take an action ID or an RFC 3339
date. `board_activity` and `card_history` accept `filter`, which defaults to `all`: either `all` alone, or one
or more comma-separated action identifiers made of letters, digits and underscores that start with a letter,
with no spaces, empty parts or `all` component. Examples are `commentCard`, `createCard`, `updateCard` and
`updateCheckItemStateOnCard`. The grammar is open-ended rather than a closed list, and filtering happens
upstream before paging, not after. A full page advertises a possible next page, which can be empty.

<!-- activity-example:start -->
Synthetic page input and its `ProjectActions` output (`boardId` is the resolved board):

```json
{
  "boardId": "b1",
  "input": [
    {"id":"a1","type":"commentCard","date":"2026-10-15T12:00:00Z","memberCreator":{"id":"m1","username":"synthetic"},"data":{"board":{"id":"b1"},"card":{"id":"c1","name":"Example"},"list":{"id":"l1","name":"Doing"},"text":"Keep this text.\nNext line."}},
    {"id":"a2","type":"updateCard","date":"2026-10-15T11:00:00Z","memberCreator":{"id":"m1","username":"synthetic"},"data":{"card":{"id":"c1","name":"Example"},"list":{"id":"l1","name":"Doing"},"old":{"name":"Draft","idLabels":["label1"]}}}
  ],
  "output": [
    {"id":"a1","type":"commentCard","date":"2026-10-15T12:00:00Z","by":"synthetic","cardId":"c1","card":"Example","text":"Keep this text.\nNext line.","listId":"l1","list":"Doing"},
    {"id":"a2","type":"updateCard","date":"2026-10-15T11:00:00Z","by":"synthetic","cardId":"c1","listId":"l1","old":{"name":"Draft"},"changed":["idLabels"]}
  ]
}
```
<!-- activity-example:end -->

## Mutation parameters

| Tool(s) | Required input | Optional input beyond `boardId` |
| --- | --- | --- |
| `board_create` | `name` | `workspaceId` (active workspace default), `desc`, `defaultLists` |
| `list_create` | `name` | `pos` |
| `list_update` | `listId`, at least one update | `name`, `pos`, `idBoard` (destination), `closed` |
| `list_archive` / `list_watch` | `listId` | watch: `subscribed` (default true) |
| `card_create` | `listId`, `name` | `desc`, `due`, `start`, `pos`, `idMembers`, `idLabels`, `checklist` (`name` optional, `items` required) |
| `card_create_batch` | `listId`, `cards` | each card has `name` and the same optional creation fields, including `checklist` |
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

Batch create accepts 1-100 cards. All arguments and referenced ids are preflighted
before the first mutation, then ownership and references are checked again per
card. Results are `items` with zero-based `index`, `id` when created, optional
`checklist` progress and `error` on failure. Batch processing continues after a
per-card failure. This is not a transaction; cards and checklists are never rolled
back after a later failure.

Both creation tools accept `checklist: {"name":"Checklist","items":["SND01","SND02"]}`
on each card. The name defaults to `Checklist`; `items` requires 1-100 nonblank
strings, each at most 4096 bytes (the `checklist_item_create` name bound). Items
are created in order on the new checklist of the new card, without concurrency.
Successful single-card output adds `checklist: {id,name,items:N}` to the card
projection; batch entries include the same checklist summary.

On checklist failure, the result retains the card `id`, checklist `id` if
confirmed, confirmed `items` count and an `error` identifying the failed step.
Single-card partial failures set MCP `isError` while preserving that result;
batches retain their existing per-entry error semantics. Counts exclude an
ambiguous failed request, which might have succeeded upstream. Inspect the card
with `checklist_get` and its items with `checklist_item_find` before resuming with
`checklist_create` or `checklist_item_create`; do not repeat card creation.

```json
{"listId":"l1","cards":[{"name":"Sound test","checklist":{"items":["SND01","SND02"]}}]}
```

Example batch result:

```json
{"items":[{"index":0,"id":"c1","checklist":{"id":"cl1","name":"Checklist","items":2}}]}
```

`checklist_item_find` is read-only and accepts `checklistId`, optional `boardId`
and case-insensitive `nameFilter` (matching the item's description/name).

`checklist_get` matches an exact name, default `Acceptance Criteria`. On a miss it returns an error with the
searched `name`, up to 20 `available` `{id,name}` choices, `total` and `truncated`, and no items. Every
checklist on the card is ownership-checked before any name is disclosed. Item `due` and `idMember` appear in
`checklist_items`, `checklist_get`, `checklist_item_find` and full `card_get`.

## Mutation verification and partial results

Clearing `due`, `start` or a label `color` with an empty string is sent as Trello query parameters, because an
HTTP JSON null is silently ignored upstream. Checklist item `due` and `memberId` use query parameters too. The
response is checked first: requested values that come back populated are compared immediately with no extra
request, including a mismatch. A GET happens only for fields the response omits, for one bounded authorized read.
Mutations are not retried after ambiguous transport failures, invalid success responses or verification
mismatches. HTTP 429 responses get bounded retries (at most three). In live checks Trello ignored checklist item `due` and
`memberId` while still answering with a populated item, so those requests surface as a partial result rather
than success when the value does not come back; the cause is not diagnosed further.

When the object is confirmed but a requested field is not, the result is `{id, error, unconfirmed}` (plus the
usual `name` or `url`), where `unconfirmed` lists input names. The MCP result sets `isError`. The mutation
may have been applied, so inspect the object before repeating anything. A created card or item is never
recreated to repair a partial result.

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

`attachment_find` requires `cardId` and accepts an optional `boardId` ownership
constraint. It remains available in read-only mode. Card ownership is resolved
before fetching metadata and rechecked afterward. The result is `{"items":[...]}`
with `id`, `name`, `fileName`, `mimeType`, `bytes`, `date`, and `isUpload`. Only
non-upload (link) attachments include `url`; uploaded-file URLs require OAuth
and are omitted. No attachment bodies or previews are returned. Use the returned
`id` as `attachmentId` for `attachment_download`.

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
   uptime/request/retry counters. `policy` reports configured/effective access
   policy with one cached best-effort workspace-resolution request. No mode
   exposes saved ids or credentials. This is not an upstream readiness check.
10. Dropdown ids and all scalar types are preserved and tested, including writes
    and empty-value clearing for each type.
