package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/credentials"
)

// TestHealthCredentialSource keeps source diagnostics secret-free and text-only.
func TestHealthCredentialSource(t *testing.T) {
	for _, source := range []string{"env", "file", "none"} {
		t.Run(source, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("health contacted upstream")
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer fake.Close()
			s := testService(t, fake, nil)
			s.config.CredentialSource = source
			s.config.APIKey, s.config.Token = "synthetic-health-key", "synthetic-health-token"
			s.config.StatePath = "/synthetic/private/state.json"
			s.config.DownloadDir = "/synthetic/private/downloads"
			s.config.UploadDir = "/synthetic/private/uploads"
			if source == "none" {
				s.config.APIKey, s.config.Token = "", ""
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			if err := s.Register(server); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			a, b := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ss.Close() }()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, b, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cs.Close() }()
			for _, detail := range []string{"", "basic", "detailed", "metadata", "performance"} {
				args := map[string]any{}
				if detail != "" {
					args["detail"] = detail
				}
				result, callErr := cs.CallTool(ctx, &mcp.CallToolParams{Name: "server_health", Arguments: args})
				if callErr != nil || result.IsError || len(result.Content) != 1 || result.StructuredContent != nil {
					t.Fatalf("health %q: %v", detail, callErr)
				}
				text, ok := result.Content[0].(*mcp.TextContent)
				if !ok {
					t.Fatalf("health %q is not text", detail)
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(text.Text), &body); err != nil {
					t.Fatal(err)
				}
				if source == "none" {
					if body["ok"] != false || body["credentialSource"] != "none" || body["message"] != credentials.Hint || body["enabledToolCount"] != float64(1) || len(body) != 4 {
						t.Errorf("unconfigured health %q changed shape", detail)
					}
				} else {
					switch detail {
					case "", "basic":
						if text.Text != `{"ok":true,"credentialSource":"`+source+`"}` {
							t.Errorf("basic health %q: %s", detail, text.Text)
						}
					case "detailed":
						want := `{"ok":true,"credentialSource":"` + source + `","readOnly":false,"restricted":false,"activeBoard":true,"activeWorkspace":true}`
						if text.Text != want {
							t.Errorf("detailed health: %s", text.Text)
						}
					case "metadata":
						if len(body) != 4 || body["name"] != "trello-mcp" || body["version"] != Version || body["transport"] != "stdio" || body["go"] == "" {
							t.Error("metadata shape changed")
						}
					case "performance":
						if len(body) != 3 || body["uptimeSeconds"] == nil || body["requests"] != float64(0) || body["retries"] != float64(0) {
							t.Error("performance shape changed")
						}
					}
				}
				for _, forbidden := range []string{"synthetic-health-key", "synthetic-health-token", "/synthetic/private", `"credentialScope"`, `"client"`} {
					if strings.Contains(text.Text, forbidden) {
						t.Errorf("health %q exposed %q", detail, forbidden)
					}
				}
			}
		})
	}
}
