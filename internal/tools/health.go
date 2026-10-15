package tools

import (
	"context"
	"runtime"
	"time"
)

// serverHealth reports local diagnostics only; persisted identifiers and credentials are never exposed.
func (s *Service) serverHealth(_ context.Context, a arguments) (any, error) {
	switch a.str("detail") {
	case "detailed":
		selection := s.state.Get()
		return struct {
			OK              bool `json:"ok"`
			ReadOnly        bool `json:"readOnly"`
			Restricted      bool `json:"restricted"`
			ActiveBoard     bool `json:"activeBoard"`
			ActiveWorkspace bool `json:"activeWorkspace"`
		}{true, s.config.ReadOnly, len(s.config.AllowedWorkspaces) > 0, selection.BoardID != "", selection.WorkspaceID != ""}, nil
	case "metadata":
		return struct {
			Name      string `json:"name"`
			Version   string `json:"version"`
			Transport string `json:"transport"`
			Go        string `json:"go"`
		}{"trello-mcp", Version, "stdio", runtime.Version()}, nil
	case "performance":
		stats := s.access.Client.Stats()
		return struct {
			UptimeSeconds int64  `json:"uptimeSeconds"`
			Requests      uint64 `json:"requests"`
			Retries       uint64 `json:"retries"`
		}{int64(time.Since(s.started) / time.Second), stats.Requests, stats.Retries}, nil
	default:
		return struct {
			OK bool `json:"ok"`
		}{true}, nil
	}
}

// Version is the server implementation version advertised during MCP initialization.
const Version = "0.1.0"
