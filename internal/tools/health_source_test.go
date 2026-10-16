package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
						if text.Text != `{"ok":true,"credentialSource":"`+source+`","access":"unverified; call workspace_find"}` {
							t.Errorf("basic health %q: %s", detail, text.Text)
						}
					case "detailed":
						want := `{"ok":true,"credentialSource":"` + source + `","access":"unverified; call workspace_find","readOnly":false,"restricted":false,"activeBoard":true,"activeWorkspace":true}`
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

// TestHealthAccessObservation keeps local configuration separate from each upstream read.
func TestHealthAccessObservation(t *testing.T) {
	for _, tc := range []struct {
		name, response, want string
		status               int
		allowed              []string
	}{
		{"unauthorized", `test-key test-token upstream-private-body`, `"error":`, http.StatusUnauthorized, nil},
		{"accessible", `[` + workspaceJSON + `]`, `{"items":[` + workspaceJSON + `]}`, http.StatusOK, nil},
		{"empty", `[]`, `{"items":[]}`, http.StatusOK, nil},
		{"filtered-empty", `[` + workspaceJSON + `]`, `{"items":[]}`, http.StatusOK, []string{"other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/members/me/organizations" || r.URL.Query().Get("fields") != "id,name,displayName" {
					t.Errorf("unexpected access verification: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer fake.Close()
			s := testService(t, fake, tc.allowed)
			selection := s.state.Get()
			wantHealth := `{"ok":true,"credentialSource":"env","access":"unverified; call workspace_find"}`
			if text, failed := callTool(t, s, "server_health", `{}`); failed || text != wantHealth || requests.Load() != 0 {
				t.Fatalf("local health: %s, failed=%t, requests=%d", text, failed, requests.Load())
			}
			// Repeat the read to ensure access is observed afresh, never cached by health.
			for i := int32(1); i <= 2; i++ {
				text, failed := callTool(t, s, "workspace_find", `{}`)
				if failed != (tc.status != http.StatusOK) || !strings.Contains(text, tc.want) || requests.Load() != i {
					t.Fatalf("workspace_find: %s, failed=%t, requests=%d", text, failed, requests.Load())
				}
				for _, forbidden := range []string{"test-key", "test-token", "upstream-private-body"} {
					if strings.Contains(text, forbidden) {
						t.Errorf("workspace_find exposed %q", forbidden)
					}
				}
				if text, failed = callTool(t, s, "server_health", `{}`); failed || text != wantHealth || requests.Load() != i {
					t.Errorf("health changed after upstream observation: %s, failed=%t", text, failed)
				}
				text, failed = callTool(t, s, "server_health", `{"detail":"detailed"}`)
				if failed || !strings.Contains(text, `"access":"unverified; call workspace_find"`) || requests.Load() != i {
					t.Errorf("detailed health changed after upstream observation: %s, failed=%t", text, failed)
				}
				if s.state.Get() != selection {
					t.Error("access verification changed local selection")
				}
			}
		})
	}
}

// TestHealthPolicyIsBestEffort ensures cached label lookup is not authentication.
func TestHealthPolicyIsBestEffort(t *testing.T) {
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()
	s := testService(t, fake, []string{"w1"})
	selection := s.state.Get()
	for i := 0; i < 2; i++ {
		text, failed := callTool(t, s, "server_health", `{"detail":"policy"}`)
		var body map[string]any
		if err := json.Unmarshal([]byte(text), &body); err != nil {
			t.Fatal(err)
		}
		if failed || body["restricted"] != true || body["access"] != nil || requests.Load() != 1 {
			t.Fatalf("best-effort policy: %s, failed=%t, requests=%d", text, failed, requests.Load())
		}
	}
	if text, failed := callTool(t, s, "workspace_find", `{}`); !failed || !strings.Contains(text, `"error":`) || requests.Load() != 2 {
		t.Errorf("policy success masked unauthorized read: %s, failed=%t, requests=%d", text, failed, requests.Load())
	}
	if s.state.Get() != selection {
		t.Error("policy or failed access verification changed local selection")
	}
}
