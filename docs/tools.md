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

## Credentials and diagnostics

File locations and the unquoted `KEY=VALUE` format follow
[avhn/secret](https://github.com/avhn/secret), "Let coding agents use your
credentials without reading them". The bare name `trello-mcp` resolves to
`~/.secrets/.trello-mcp.secret` in that tool: `secret show trello-mcp` lists masked
names and `secret exec -f trello-mcp ...` can launch a configured consumer.
Values follow the same rules as `secret stash`. Using avhn/secret is optional;
trello-mcp reads the files itself.

`~/.secrets.env` is secret's shared top-level store and the final fallback.
Only `TRELLO_API_KEY`, `TRELLO_TOKEN` and optional `TRELLO_TOKEN_SCOPE` are
interpreted there; unrelated keys and values are ignored and never logged.
Both key and token must exist in the same file; sources are never merged.
Missing or credential-free earlier files fall through. Unsafe or malformed
earlier files fail closed. An unsafe shared file is never read when an earlier
source exists; otherwise its ownership, file privacy and symlink errors are
reported clearly. Shared files need the same private file mode/DACL; their home
parent is not required to have an owner-only Windows DACL.
Auth never writes to or removes keys from the shared store, even via `--file`.
`auth status` identifies it as `Credential source: file ~/.secrets.env (shared store)`.
After logout, credentials in the shared store can still be active.


Client configs need only the full binary path. Run `trello-mcp auth` outside MCP
to validate and save `~/.secrets/.trello-mcp.secret` on every platform
(Windows: `%USERPROFILE%\.secrets\.trello-mcp.secret`, using `os.UserHomeDir()`).
Precedence is the complete `TRELLO_API_KEY` + `TRELLO_TOKEN` environment pair,
then the absolute `TRELLO_MCP_CREDENTIALS` file, then the default file, then
`~/.secrets.env` (the shared top-level store). Partial
environment pairs fail. `auth`, `auth status` and `auth logout` accept
`--file /absolute/path/account.secret`; that flag overrides the file path for
only the command, not future MCP processes.

The file is plaintext, unquoted dotenv compatible with the `secret` tool:

```dotenv
TRELLO_API_KEY=your-api-key
TRELLO_TOKEN=your-token
TRELLO_TOKEN_SCOPE=read,write
```

Scope is optional: absent means unknown, treated as writable; `read` forces
read-only regardless of `TRELLO_READ_ONLY=0`. Values reject empty strings, NUL,
CR, embedded newlines, boundary whitespace and surrounding quote pairs. A final
LF is allowed. Save rewrites only these keys and preserves all other lines and
comments in order, using a same-directory private temp file, fsync and rename.

Auth creates a missing parent as 0700 and a file as 0600 on Unix, or protected
owner-only DACLs on Windows. Existing folders are never chmodded. Unix refuses
foreign owners, group/world-writable directories and group/world-accessible
files; Windows verifies current-user ownership and protected owner-only DACLs.
Symlinked files or parent paths and nonregular files are refused. For example:
`credentials file is group/world accessible; chmod 600 the credentials file or rerun trello-mcp auth`.
Directory refusal says
`credentials directory is group/world writable; remove group/world write permissions (chmod go-w) or choose a private directory with TRELLO_MCP_CREDENTIALS`.
See the [README credential troubleshooting](../README.md#credential-troubleshooting)
for exact ownership, symlink and Windows errors and safe fixes.

To configure without the browser flow, create a missing `~/.secrets` with
`mkdir -m 700 ~/.secrets`, privately edit the three lines with your existing key,
token and actual scope, and `chmod 600 ~/.secrets/.trello-mcp.secret` on Unix.
Do not change an existing shared folder automatically. `secret stash FILE KEY`
can prompt privately for each value, without putting it in command arguments.
Windows manual setup needs protected owner-only DACLs on folder and file.

`trello-mcp auth status` validates identity and granted scope with Trello and
prints `Credential source: env (TRELLO_API_KEY, TRELLO_TOKEN)` or
`Credential source: file <path>`, then `Connected as <username>, scope <granted scope>`.
With none found it explains that MCP clients may provide their own environment
and suggests auth to save for all clients. Validation failures explicitly ask
you to check the token and read permission; no secret values are printed.
`auth logout` removes only the three keys, deleting the file only when empty;
unrelated lines, comments, environment credentials and Trello authorization
remain. Restart clients after changes.

Troubleshooting: no credentials means run auth and restart; permission refusal
means repair only your intended private path or choose a new one; HTTP 401 means
check the key, token expiry/revocation and reconnect; wrong workspace denial is
server policy, so inspect `server_health` with `detail: "policy"` rather than
changing credentials.

## Profiles and common parameters

`TRELLO_TOOLS` accepts comma-separated tool names and prefix groups: `workspace`,
`board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`,
`attachment`, `server`, or `all`. Selectors form a union, not an intersection.
For example, `card` includes both card-member tools. Unknown selectors fail
startup. Write access is the default for `trello-mcp auth` (Enter at
`Allow write access? [Y/n]` means yes). Read-only is opt-in via
`trello-mcp auth --read-only` or `TRELLO_READ_ONLY=1`; either then removes all 29
mutation/transfer tools, even explicitly selected ones. The 19 reads and
local-selection tools remain eligible. Downloads are filtered too because they
write local files. A saved token with `scope=read` implies read-only mode even if
`TRELLO_READ_ONLY=0`.

With no credentials the server still initializes and registers only
`server_health`, regardless of the selected profile. Initialization instructions
and every health mode say: "Trello credentials not configured: run
`trello-mcp auth` to save ~/.secrets/.trello-mcp.secret, or set TRELLO_API_KEY and TRELLO_TOKEN." Run auth
outside the MCP transport, then restart the client. A complete environment pair
overrides the saved credentials file; partial pairs are rejected.

- `boardId` defaults to the active board on board-scoped calls. On object-scoped
  calls it is an optional ownership constraint, not a source of authorization.
- Object ids are required for object-scoped calls: `cardId`, `listId`,
  `checklistId`, `commentId`, `itemId`, `labelId`, `customFieldId`, `memberId`,
  `attachmentId`, as appropriate. Item mutations require both `checklistId` and
  `itemId`; comment mutations require both `cardId` and `commentId`.
- `pos` accepts `top`, `bottom`, or a positive number. Dates use RFC 3339;
  empty `due` or `start` clears a date. Optional booleans preserve explicit false.
- Every result is one compact JSON text block, without structured content or an
  output schema. Writes return `{id,name?,url?}` or `{ok:true}`, never raw objects;
  card creation can also include checklist progress and an error.
- Unknown arguments name the key, object location and allowed keys. Duplicate
  arguments name the key and location; type errors name the field and expected type.

## Discovering access policy

The MCP initialize response includes short `instructions` generated from startup
configuration: workspace restrictions (configured ids/canonical names or
unrestricted), read-only mode, path uploads and downloads. Credentials and saved
active ids are never included. Long selectors/paths are shortened there.

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

`board_activity`, `card_history` and `comment_find` share a compact projection:
`id`, `type`, `date`, `memberCreator: {id,username}`, and curated `data`.
Card/list/move/checklist/attachment references keep only id/name; checklist items
also keep state, labels keep color, and member references keep id/username.
Boards keep id only, omitted when equal to the queried/resolved board. Attachments
never include URLs or previews. `old` contains only supported changed scalar keys,
with previous strings capped at 200 Unicode characters (including `...`). Comment
text is unchanged. Other nested metadata is dropped, even for unknown action types.
Explicit request fields, limits, filters and since/before paging are retained.

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
