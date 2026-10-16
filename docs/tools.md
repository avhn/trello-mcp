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
`trello-mcp auth --read-only` or `TRELLO_READ_ONLY=1` (`true` and `false` are also accepted); either then removes all 29
mutation/transfer tools, even explicitly selected ones. The 19 reads and
local-selection tools remain eligible. A saved token with `scope=read` implies
read-only mode even if `TRELLO_READ_ONLY=0` or `false`.

The named read-oriented discovery profile has exactly ten tools. `board_use` still changes
the local default board, so the profile is not wholly read-only:

```text
TRELLO_TOOLS=workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health
```

With no credentials the server still initializes and registers only `server_health`,
regardless of the selected profile. Initialization instructions and every health mode
tell a CLI human to run the installer's absolute-path auth command in a private terminal (or a Desktop
extension human to use extension settings), then use the individual Reconnect action or restart the
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
  status and a description preview of at most 200 code points with `descTruncated`, but no `url`.
  `detail: "full"` adds `url`, `start` and `dateLastActivity` but still limits collection descriptions to
  200 code points. `card_get` keeps `url` and untruncated descriptions at both levels; full adds
  checklists and members. Filters, authorization, IDs and `next` cursors are identical in both modes.
- `card_find` accepts `includeDesc`, a boolean defaulting to `true`. `includeDesc: false` drops `desc` and
  `descTruncated` from summaries for triage, listing or paging; it is rejected together with
  `detail: "full"`. Filters, ownership checks, IDs, retained values and `next` cursors do not change, and
  `card_get` and full detail are unaffected. On the synthetic 30-card benchmark fixture the response shrinks
  from 11,873 to 4,943 characters (58.4%); the saving on your boards depends on your descriptions, and the
  `tools/list` entry grows by 80 characters. Character counts are not tokenizer measurements.
- `card_find` `before` is a card ID returned earlier in the same filtered snapshot, never a date.
  Dates and malformed IDs are rejected before any request; syntactically valid IDs are checked against
  the freshly fetched filtered snapshot and rejected if absent. `mine: true` selects account-wide assigned cards and
  cannot be combined with `boardId` or `listId`. Board, list and mine modes discover open cards only,
  with no archive selector. An authorized `card_get` can still retrieve a known archived card ID from
  history; empty discovery does not imply deletion.
- Unknown arguments name the key, object location and allowed keys. Duplicate
  arguments name the key and location; type errors name the field and expected type.

## Read parameters

Every non-write tool. `boardId` is optional everywhere except `board_use`, which requires it: collection reads default to the active board and
object reads treat it as an expected-owner constraint, never authorization. Limits, defaults and enums here are
checked against the registration and runtime schema by the repository validator.

| Tool | Required | Optional | Defaults and accepted values |
| --- | --- | --- | --- |
| `workspace_find` | none | `nameFilter` | none |
| `workspace_use` | `workspaceId` | none | local selection; clears the active board |
| `board_find` | none | `workspaceId`, `nameFilter` | accessible open boards, including guest boards, unless `workspaceId` narrows |
| `board_use` | `boardId` | none | local selection |
| `board_get` | none | `boardId`, `detail` | `detail`: `summary` or `full` |
| `board_activity` | none | `boardId`, `limit`, `since`, `before`, `filter` | `limit`: default 20, max 1000; `filter`: default `all` |
| `list_find` | none | `boardId`, `nameFilter` | open lists only |
| `card_find` | none | `boardId`, `listId`, `mine`, `nameFilter`, `detail`, `limit`, `before`, `includeDesc` | `detail`: `summary` or `full`; `limit`: default 100, max 1000; `includeDesc`: default `true` |
| `card_get` | `cardId` | `boardId`, `detail`, `view`, `extraFields` | `detail`: `summary` or `full`; `view`: `json` or `markdown`; `extraFields`: up to four unique of `stickers`, `pluginData`, `votes` and `customFieldItems` |
| `card_history` | `cardId` | `boardId`, `limit`, `before`, `since`, `filter` | `limit`: default 20, max 1000; `filter`: default `all` |
| `attachment_find` | `cardId` | `boardId` | metadata only |
| `comment_find` | `cardId` | `boardId`, `limit`, `before`, `since` | `limit`: default 20, max 100 |
| `checklist_items` | `checklistId` | `boardId` | none |
| `checklist_get` | `cardId` | `boardId`, `name` | `name`: default `Acceptance Criteria` |
| `checklist_item_find` | `checklistId` | `boardId`, `nameFilter` | case-insensitive substring |
| `member_find` | none | `boardId`, `nameFilter` | none |
| `label_find` | none | `boardId`, `nameFilter` | none |
| `customfield_find` | none | `boardId` | none |
| `server_health` | none | `detail` | `detail`: `basic`, `detailed`, `metadata`, `performance` or `policy` |

`card_find` cursors and action cursors differ. A `card_find` `before` is a card ID from the same filtered
snapshot and `mine` cannot be combined with `boardId` or `listId`. Trello JSON responses have an 8 MiB
upstream limit, before local filtering and snapshot paging. `limit` and `nameFilter` run after retrieval
and cannot shrink that upstream response. A narrower `listId` or summary `includeDesc: false` may help,
but does not guarantee recovery; omission is invalid with full detail. Smaller local pages are not a fix
for this limit, which is not an MCP output cap. Activity, history and comment `before` and
`since` take an action ID or an RFC 3339 date, and `filter` applies upstream before paging.

```json
{"cardId":"c1","view":"markdown"}
{"cardId":"c1","detail":"full","extraFields":["votes"]}
{"listId":"l1","includeDesc":false,"limit":50}
```

`card_get` with `view: "markdown"` returns only `{markdown:string}`, rendering name, description, URL,
due and checklist text. It does not render IDs, members, labels, start/activity or extras. Request JSON
for metadata or `extraFields`, even though a Markdown-plus-extras call can fetch those fields.

## Text limits

Human text is counted in Unicode code points, not bytes, and a value over its limit is rejected before any
request. Trello's own error is reported as an error, never as silent truncation. Tool schemas omit these
limits to keep the catalog small; the runtime check and its error message state them.

| Field | Local limit | Evidence |
| --- | --- | --- |
| Card `desc` (`card_create`, `card_update`, batch cards) | 16,384 code points | Trello's object definitions say "Up to 16384 chars"; how upstream counts Unicode is not defined |
| Board `desc` | 16,384 code points | Trello documents no limit; local ceiling |
| Comment `text`, card, list, board, checklist, checklist item, label and attachment names, `checklist.items` entries, custom-field text and `nameFilter` | 16,384 code points | Trello documents no limit; local ceiling, not an observed Trello maximum |

A ceiling marked local is this server's own safety bound. It may be lower or higher than what Trello accepts;
the card description row is the only one with a documented upstream figure. Live checks accepted 16,384
ASCII characters for card names/descriptions, comments and checklist item names; only descriptions also
have supplementary-character round-trip evidence. These observations are not upstream maxima or Unicode
evidence for the other fields. Request bodies have independent
aggregate caps: 1 MiB of JSON for tools that carry enlarged text, cards, checklists or custom-field values,
14 MiB for `attachment_upload` and 16 KiB for everything else. A maximal 16,384 code point value of
supplementary characters written as literal JSON surrogate escapes takes 196,608 bytes and fits, but the whole
request must too: 100 batch cards with maximal descriptions exceed 1 MiB, so split large batches.

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
`basic` and `detailed` also report `credentialSource` as `env`, `file` or `none`; it never contains a
path, key or token. An environment source includes credentials supplied by a Claude Desktop extension. Source means where
credentials came from, not validated authentication. Configured basic and detailed responses include
`access: "unverified; call workspace_find"`; `ok: true` means local configuration is present.

```json
{"ok":true,"credentialSource":"env","access":"unverified; call workspace_find"}
```

Use basic health for presence/source, metadata for the installed version, and policy for effective
restrictions, then call `workspace_find {}` for an actual Trello read. A successful empty result proves
the upstream read worked, not that a selector matches. Distinguish authentication, permission, network
and rate-limit failures using the scrubbed errors. Custom profiles can use a permitted upstream read
such as `board_find`; health-only profiles cannot verify access. Adjust the profile and reconnect if
appropriate, never bypass restrictions. Desktop users need no CLI auth/status or credential copying;
CLI status describes only that invocation. A successful read never upgrades health to enduring validation.

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
strings, each at most 16,384 code points (the local text limit above). Items
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
Windows and other unsupported platforms refuse path uploads even with a configured directory.
Configuring directories cannot enable Windows local path uploads or attachment downloads. Base64
uploads and URL attachments remain alternatives subject to write/profile policy. Base64 uploads still work without
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
   configured `basic` returns `ok`, `credentialSource` and the unverified `access` pointer;
   `detailed` adds read-only/restriction and selection-presence booleans;
   `metadata` reports implementation/runtime/transport; `performance` reports
   uptime/request/retry counters. `policy` reports configured/effective access
   policy with one cached best-effort workspace-resolution request. No mode
   exposes saved ids or credentials. This is not an upstream readiness check.
10. Dropdown ids and all scalar types are preserved and tested, including writes
    and empty-value clearing for each type.
