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
  `checksums.txt`. The registry job runs right after the GitHub release and verifies
  the public bytes itself; the human download of the public assets, check of all ten
  hashes and the generated identifier's exact bytes, and validation of downloaded
  metadata is a post-publication audit, not a gate. Other directory submissions are
  not part of the release workflow.
- Always pass the absolute path of the downloaded generated `server.json` to
  `mcp-publisher publish`; there is deliberately no `server.json` in the repository root.
- Generated registry metadata lists only `trello-mcp_desktop.mcpb` (macOS universal and
  Windows amd64). The Linux MCPBs are owner-untested beta downloads: inspect them and
  include them in the unified checksums, but keep them out of registry packages until
  the owner promotes them with test evidence. This is a support policy, not a registry
  format limit.
- Releases with a vX.Y.Z tag (major >= 1) publish `io.github.avhn/trello-mcp` to the
  official registry automatically after the GitHub release via `registry.yml`.
  This uses GitHub OIDC, no stored secrets, the checksum-pinned publisher, public-byte
  verification, idempotent publication and a Registry API name/version/packages check.
  The owner-authorized rerun is `gh workflow run registry.yml -f tag=vX.Y.Z`; it runs
  from main and fails safely unless main's template version still equals the tag.
  Deprecation stays a human step: `mcp-publisher status --status deprecated
  --message "Use X.Y.Z" io.github.avhn/trello-mcp VERSION`, then verify the Registry API.
  Never inspect token files, add registry secrets or PATs, or submit pre-1.0 releases.
