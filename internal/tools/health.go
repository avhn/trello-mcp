package tools

import (
	"context"
	"runtime"
	"time"

	"github.com/avhn/trello-mcp/internal/credentials"
)

// serverHealth reports diagnostics; persisted identifiers and credentials are never exposed.
func (s *Service) serverHealth(ctx context.Context, a arguments) (any, error) {
	if !s.config.HasCredentials() {
		return struct {
			OK               bool   `json:"ok"`
			Message          string `json:"message"`
			CredentialSource string `json:"credentialSource"`
			EnabledToolCount int    `json:"enabledToolCount"`
		}{false, credentials.Hint, "none", 1}, nil
	}
	switch a.str("detail") {
	case "policy":
		return s.policy(ctx), nil
	case "detailed":
		selection := s.state.Get()
		return struct {
			OK               bool   `json:"ok"`
			CredentialSource string `json:"credentialSource"`
			Access           string `json:"access"`
			ReadOnly         bool   `json:"readOnly"`
			Restricted       bool   `json:"restricted"`
			ActiveBoard      bool   `json:"activeBoard"`
			ActiveWorkspace  bool   `json:"activeWorkspace"`
		}{true, s.config.CredentialSource, "unverified; call workspace_find", s.config.ReadOnly, len(s.config.AllowedWorkspaces) > 0, selection.BoardID != "", selection.WorkspaceID != ""}, nil
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
			OK               bool   `json:"ok"`
			CredentialSource string `json:"credentialSource"`
			Access           string `json:"access"`
		}{true, s.config.CredentialSource, "unverified; call workspace_find"}, nil
	}
}

// Version is the server implementation version, set by the command at startup.
var Version = "0.8.0"
