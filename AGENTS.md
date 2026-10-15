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
