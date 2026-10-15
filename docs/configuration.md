# Configuration

The detailed reference for variables, credential precedence and storage, platform paths,
permissions, troubleshooting and the security model. Install and client registration are in
[setup](setup.md); tool behavior is in [tools](tools.md).

## Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `TRELLO_API_KEY` | saved credentials | Trello API key; must be set together with `TRELLO_TOKEN` |
| `TRELLO_TOKEN` | saved credentials | Trello API token; an environment pair overrides the file |
| `TRELLO_BOARD_ID` | saved selection | Initial board; changing it clears the old saved workspace |
| `TRELLO_ALLOWED_WORKSPACES` | unrestricted | Comma-separated workspace ids or canonical names, not display names; additional server policy, not token permissions |
| `TRELLO_TOOLS` | `all` | Comma-separated tool names or prefix groups: `workspace`, `board`, `list`, `card`, `comment`, `checklist`, `member`, `label`, `customfield`, `attachment`, `server` |
| `TRELLO_READ_ONLY` | `0` (or saved scope `read`) | Opt-in: `1` hides all 29 mutation/transfer tools, including local-file downloads; 19 tools remain. `0` cannot override a saved read-only token |
| `TRELLO_DOWNLOAD_DIR` | `os.TempDir()/trello-mcp` | Absolute download directory; created as 0700, existing directories must be private and owned by the current user |
| `TRELLO_UPLOAD_DIR` | unset (path uploads disabled) | Absolute root for local-file uploads; only regular files inside this directory may be uploaded, without following symlinks |
| `TRELLO_MCP_CREDENTIALS` | `~/.secrets/.trello-mcp.secret` | Absolute dotenv credential file path; `auth --file PATH` overrides it for that command |
| Shared fallback | `~/.secrets.env` | Read only the three Trello keys if no earlier source exists; auth never modifies it |
| `TRELLO_TOKEN_SCOPE` (file only) | unknown, writable | `read` forces read-only; `read,write` permits writes subject to server policy |
| `XDG_CONFIG_HOME` | platform configuration directory | Absolute root for `trello-mcp/state.json` only; does not affect credentials |
| `HTTPS_PROXY` | unset | Standard Go HTTP proxy configuration |

Selected tools form a union and unknown selectors fail startup. Groups use the prefix before the
first underscore, so `card` includes `card_member_add` and `card_member_remove`. Read-only filtering
applies even when write tools are selected explicitly. The copyable read-oriented profile is in
[setup](setup.md#optional-read-oriented-profile).

## Credential storage

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

The default on every platform is `~/.secrets/.trello-mcp.secret`, resolved with
`os.UserHomeDir()` (`%USERPROFILE%\.secrets\.trello-mcp.secret` on Windows).
Precedence is: environment `TRELLO_API_KEY` + `TRELLO_TOKEN`, then an absolute
`TRELLO_MCP_CREDENTIALS` file, then the default file, then `~/.secrets.env`.
Both environment values are required together; a partial pair is an error, not a fallback. MCP client
configs need only the full binary path, with no credential env block.

The dedicated plaintext file uses unquoted dotenv:

```dotenv
TRELLO_API_KEY=your-api-key
TRELLO_TOKEN=your-token
TRELLO_TOKEN_SCOPE=read,write
```

`TRELLO_TOKEN_SCOPE` is optional: absent means unknown and is treated as writable;
`read` forces read-only even with `TRELLO_READ_ONLY=0`. Status validates the
actual granted scope with Trello. Values must be nonempty and unquoted, without
NUL, CR, embedded newlines, boundary whitespace or surrounding quote pairs. A
final LF terminating the file is allowed. Comments beginning with `#`, blank
lines and unrelated keys are preserved byte-for-byte and in order on save.
Do not put secrets in committed files, shell history or chat.

### Permissions and locking

`trello-mcp auth` creates missing parents as 0700 and files as 0600 on Unix.
It also sets the default `~/.secrets` to 0700 after refusing symlinks and foreign
owners, matching `secret stash`. Reads and logout require no group/world bits
(0077) on that directory. Custom parents and the home parent of read-only
`~/.secrets.env` may be 0755, but must not be group/world writable (0022).
Custom parent modes are unchanged. Files must belong to the current user with
no group/world bits, normally 0600. Windows keeps its ownership and protected
owner-only DACL checks; the shared file's home parent is exempt from the directory
DACL check. Symlinked files/parent paths and nonregular files are refused.
Saves use a same-directory private temporary file, fsync and atomic rename.

Save and logout hold `<parent>/.write-lock` across the entire read-modify-write,
including deletion. This mkdir protocol matches `secret stash` for the default
store, with private token/PID files and token-checked release. Reads do not lock.
Writers retry every 100ms for up to 3s. Timeout errors name the lock: verify its
PID is stopped before manually removing it. Locks are never auto-stolen.
Logout rechecks content before deletion and rewrites a changed file instead.
Writers ignoring the lock protocol cannot be serialized.

### Auth commands

All auth commands accept an absolute file override, and each accepts `--help`
without side effects:

```sh
trello-mcp auth --file /absolute/path/account.secret
trello-mcp auth status --file /absolute/path/account.secret
trello-mcp auth logout --file /absolute/path/account.secret
```

`--file` overrides `TRELLO_MCP_CREDENTIALS` for that command. Set
`TRELLO_MCP_CREDENTIALS` in the server environment to use the same custom file in
MCP clients; the environment credential pair still wins. `--read-only` or `--write`
skips the scope question and `--expiration 1day|30days|never` selects the token
lifetime (default `never`); both apply to `auth` only. Run `auth` only in the human's
private terminal, never in a terminal an agent captures, and restart or reconnect
clients afterward. `auth status` prints one fact per line, never credentials:

```text
Credential source: file /home/alice/.secrets/.trello-mcp.secret
Connected as alice, scope read,write
```

For environment credentials the first line is exactly
`Credential source: env (TRELLO_API_KEY, TRELLO_TOKEN)`. With none found:

```text
No credentials in this shell or <path>. MCP clients may pass them through their own config; run `trello-mcp auth` to save them for all clients.
```

Validation failure starts with:

```text
credential validation failed; check the token and its read permission, then run `trello-mcp auth`:
```

The scrubbed upstream error follows that prefix.
Logout removes only the three Trello keys; unrelated lines and comments remain, and
the file is deleted only if no bytes remain. It does not revoke the token or
unset environment values.

### Manual setup without the browser flow

If you already have a key and token, create the file in a private editor with
the three dotenv lines above, using your own values, without quotes. Use the
actual token scope (`read` or `read,write`); do not mark a read-only token writable.
On Unix, create a missing directory with `mkdir -m 700 ~/.secrets`, then create
the empty file privately after `umask 077`, run
`chmod 600 ~/.secrets/.trello-mcp.secret`, then edit it. Use LF line endings.
Ensure an existing `~/.secrets` belongs to you and is intended to be private,
then run `chmod 700 ~/.secrets`; auth and `secret stash` also repair that default.
If using the `secret` tool, `secret stash FILE KEY`
prompts privately for each key; never place values in command arguments. On
Windows use a current-user-owned folder and a protected owner-only DACL in
Properties > Security for both directory and file. Then run `trello-mcp auth status`.

## Troubleshooting

The CLI prefixes errors with `trello-mcp auth:`; source resolution may also
prepend `credentials file <path>:`. Exact storage messages include:

- `credentials directory is group/world accessible; chmod 700 ~/.secrets or rerun trello-mcp auth`
- `credentials directory is group/world writable; remove group/world write permissions (chmod go-w) or choose a private directory with TRELLO_MCP_CREDENTIALS`
- `credentials file is group/world accessible; chmod 600 the credentials file or rerun trello-mcp auth`
- `credentials must be owned by the current user; rerun trello-mcp auth as that user`
- `credentials parent must be a directory, not a symlink; choose a real directory with TRELLO_MCP_CREDENTIALS`
- `credentials file must be a regular file, not a symlink`
- Windows: `credentials require a protected owner-only Windows DACL and ownership by the current user; remove access for other users in Properties > Security on the credentials directory and .trello-mcp.secret, then rerun trello-mcp auth`

Fix permissions only on a path you own and intend to keep private, or select a
new private parent with `TRELLO_MCP_CREDENTIALS`. Replace symlinks with real
paths, and rerun as the owner rather than bypassing ownership checks.

- No credentials found, or only `server_health` is exposed: the human runs `trello-mcp auth` in a private
  terminal, checks any override, then restarts or reconnects the client. The server stays health-only until
  a fresh configured server starts. A client may supply its own environment values not visible in your shell.
- Permission refused: use the exact corrective message above. On Unix auth
  repairs only the default `~/.secrets` to 0700, when it is a current-user-owned real directory; it does not
  chmod custom parents or read an unsafe file to preserve its lines.
- 401 token rejected: reconnect with `trello-mcp auth`; check token expiry,
  revocation and the matching API key. 403 can indicate insufficient permission.
- Wrong workspace denial: inspect `server_health` with `detail: "policy"`
  and correct `TRELLO_ALLOWED_WORKSPACES` or the object selection. Server policy
  is distinct from a rejected token.

## Discovering access policy

MCP initialization instructions are a short startup summary of three policy flags: whether
workspaces are `restricted` or `unrestricted`, read-only mode, and effective path-upload and download
availability, plus a pointer to policy health. They never include credentials, selectors or saved
active ids. If a tool is hidden or an operation is refused, call `server_health` with
`detail: "policy"` before assuming that the Trello token is broken. Include `server` or
`server_health` in custom `TRELLO_TOOLS` profiles to keep this diagnostic available.

Without credentials the server still initializes, registers only `server_health`, and includes this
message in initialization instructions and every health mode: "Trello credentials not configured. The
human must run `trello-mcp auth` in a private terminal, then restart/reconnect the MCP client. Never share
keys, tokens or authorization URLs in chat or agent-captured commands. Only server_health is exposed until
a fresh configured server starts." Invalid-token HTTP 401 errors include the same setup hint.

Policy health returns `restricted`, `allowedWorkspaces` (each configured
`selector`, plus resolved `id` and canonical `name` when available), `readOnly`,
`credentialScope` (when loaded from file), `toolProfile`, `enabledToolCount`,
`hiddenWriteTools`, and `uploads`/`downloads`
with `enabled` and `dir`. Counts follow the selected profile; `hiddenWriteTools`
counts selected tools removed by read-only mode. Transfer availability also
accounts for read-only mode, tool selection and platform support; uploads here
means local path uploads, not base64. Directory paths describe configuration,
not a filesystem readiness check.

Workspace resolution uses one cached, best-effort request to
`/members/me/organizations` with only `id,name`. A failed lookup leaves selectors
unresolved without failing health. This diagnostic cache never authorizes
operations, and names of workspaces outside the allowlist are not returned.

## Active selection

- `workspace_use` and `board_use` update local defaults, even in read-only mode.
- Selecting a workspace clears the active board. Selecting a board sets its
  owning workspace.
- `board_find` enumerates all account boards, including guest boards, unless an
  explicit `workspaceId` narrows the search.
- Explicit `boardId` values work independently of the active selection.
- `board_create` requires an explicit or active `workspaceId` and verifies it.

## Security model

- Private saved credentials. The plaintext dotenv file is `~/.secrets/.trello-mcp.secret`, with an
  absolute `TRELLO_MCP_CREDENTIALS` override. New directories are 0700, files 0600 on Unix. Auth repairs
  the default `~/.secrets` to 0700; custom parents may be 0755 but not group/world writable and are not
  chmodded. Windows uses protected owner-only DACLs. Symlinks, foreign owners and accessible files are
  refused. Saves use a private same-directory temporary file, fsync and atomic rename. Save and logout use
  the token-owned `.write-lock` protocol shared with `secret stash`. The environment pair overrides files.
  Saved `TRELLO_TOKEN_SCOPE=read` forces read-only even with `TRELLO_READ_ONLY=0`. Logout removes only the
  three keys, keeping unrelated lines; it does not revoke Trello authorization. Protect plaintext files and
  backups.
- Token permissions are the ceiling. The workspace allowlist is an additional
  restriction, not a replacement for token permissions. Unset means unrestricted;
  ids and canonical names match, not display names. Initialization instructions
  and `server_health(detail: "policy")` explain the effective server policy.
- Distinct denials. Policy failures return compact `{"error":"access denied
  by <policy>: <reason>. This is server policy, not a token/permission error. ..."}`
  with a corrective action and policy-health pointer. Allowlist errors include
  configured selectors and only the caller-supplied object id, never resolved
  denied board/workspace ids or names. For example: `card c1 is outside the
  allowed workspaces (allowed: team)`. Ownership failures explicitly refuse
  access without exposing resolved parent ids. Only after the allowlist passes
  does a `boardId` mismatch identify the actual and supplied board ids.
  Genuine upstream HTTP failures use `trello API: HTTP <status>`; 401/403 explicitly
  identify a token/permission issue. Do not change credentials to fix server policy.
- Ownership checks, not caller claims. Every scoped read and write resolves
  the actual owning board. Moves and copies check source and destination;
  comments, checklist items, attachments, labels, custom fields and member
  references are checked against their actual parents before mutation. Supplied
  board ids are constraints, never authorization. Unverifiable ownership fails
  closed. Trello has no atomic authorization-and-mutation API, so concurrent
  upstream moves cannot be prevented by a local check.
- Fresh authorization. Ownership grants are not cached across calls, so a
  moved board is rechecked. Workspace listings are filtered, and health responses
  never expose saved ids.
- Read-only profile. `TRELLO_READ_ONLY=1` hides every Trello mutation and
  attachment transfer, including local-file downloads. Local defaults can still
  change. This does not replace a suitably scoped Trello token.
- Confined local uploads. Path uploads are disabled unless `TRELLO_UPLOAD_DIR`
  is set. Only regular files inside that root can be read, using a pinned root
  descriptor and no-symlink component traversal on Unix; other platforms fail
  closed. Choose a dedicated staging directory, not your home or a credential
  directory. Dot-files are not excluded. Base64 uploads do not need this setting
  because the caller already supplies the bytes.
- Defensive HTTP handling. Requests have deadlines, redirects are refused,
  credentials are scrubbed from errors, and upstream error bodies are never
  returned.
- Windows limitations. Local path uploads and attachment downloads are refused (fail closed), because
  their filesystem ownership and path checks require Unix. Base64 uploads and URL attachments remain
  available when read-only mode is off.

## Rate limits and retries

- Rolling limits: 300 requests/10s per key, 100/10s per token, and a conservative
  100/900s for all member-prefixed requests. Rate headers can reduce the budget.
- HTTP 429 allows at most three retries, honoring `Retry-After` across concurrent
  calls. Delays above 30s fail instead of retrying early.
- Limits are per process. Other clients sharing credentials still consume the
  same upstream quota.
