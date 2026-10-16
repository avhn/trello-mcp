# Agent guidance

- `cmd/trello-mcp` wires the stdio server. Domain code lives in `internal/config`,
  `internal/trello`, `internal/state`, `internal/view` and `internal/tools`.
- `testdata/trello` contains synthetic fixtures only. Never use real account data
  or credentials in tests. The allowlist is enforced at object ownership checks,
  not by trusting a supplied board id.
- Use raw MCP handlers, compact text-only JSON and curated projections. No output
  schemas, structured content, `fields=all`, or inline attachment bodies.
- Format with gofmt. Quick checks: `gofmt -l .`, `go build ./...`, `go vet ./...`,
  `go test ./...`. CI runs golangci-lint v2 and race tests on Linux and macOS.
- Refresh measurements with `go test ./internal/bench -v -update`.
- Commits are signed and conventional: `type(scope): lowercase summary`, no
  trailing period, body lines at most 100 characters, no co-author trailers.
- No em or en dashes, no emojis. Keep changes small and report failed checks.

## Releasing

- For each normal vX.Y.Z release, inspect the final MCPBs and generate metadata from
  those exact bytes with `python3 -I scripts/release_manifest.py --tag vX.Y.Z generate
  --directory /absolute/path/to/dist`. Keep the metadata-only template
  `packaging/registry/server.template.json`. Verify the unified checksums and use
  the checksum-pinned publisher via `publisher-validate
  --input /absolute/path/to/dist/server.json` before approved GitHub publication.
  The normal v-tag has six CLI archives, three MCPBs, generated `server.json` and
  `checksums.txt`. Independently download the public assets, verify all ten hashes
  and the generated identifier's exact bytes, and validate downloaded metadata.
  Stop there: no registry login, publication or other directory submissions.
- Always pass the absolute path of the downloaded generated `server.json` to
  `mcp-publisher publish`; there is deliberately no `server.json` in the repository root.
- Generated registry metadata lists only `trello-mcp_desktop.mcpb` (macOS universal and
  Windows amd64). The Linux MCPBs are owner-untested beta downloads: inspect them and
  include them in the unified checksums, but keep them out of registry packages until
  the owner promotes them with test evidence. This is a support policy, not a registry
  format limit.
- Registry publication happens only for a release the owner separately authorizes
  (v1.0.0 published without packages by mistake; v1.0.1 corrects it). Recheck the
  official format and publisher pin, then verify the downloaded generated
  `server.json` against the public bytes with `publisher-validate --input
  /absolute/path/to/downloaded/server.json`. A human runs `mcp-publisher login
  github` privately for `io.github.avhn/trello-mcp`, then `mcp-publisher publish
  /absolute/path/to/downloaded/server.json`. Verify the exact name, version and
  package through the Registry API. To retire a version, the human runs
  `mcp-publisher status --status deprecated --message "Use X.Y.Z"
  io.github.avhn/trello-mcp VERSION` and the result is checked the same way. Do
  not inspect token files, add automated registry auth secrets or retroactively
  submit pre-1.0 releases. This procedure grants no publication permission.
