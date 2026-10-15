package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/credentials"
)

func TestMissingCredentialsMCP(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("unconfigured server contacted Trello")
		w.WriteHeader(500)
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	var err error
	s.config, err = config.Parse(func(k string) string {
		if k == "TRELLO_TOOLS" {
			return "card" // Health must be available even in a custom profile.
		}
		return ""
	}, func() (string, error) { return t.TempDir(), nil })
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{Instructions: s.Instructions()})
	if err = s.Register(server); err != nil {
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
	if cs.InitializeResult().Instructions != credentials.Hint {
		t.Error("initialize did not report missing credentials")
	}
	listed, err := cs.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "server_health" {
		t.Fatalf("expected health-only registration: %v", err)
	}
	for _, detail := range []string{"basic", "detailed", "metadata", "performance", "policy"} {
		result, callErr := cs.CallTool(ctx, &mcp.CallToolParams{Name: "server_health", Arguments: map[string]any{"detail": detail}})
		if callErr != nil || result.IsError || len(result.Content) != 1 || result.StructuredContent != nil {
			t.Fatalf("health %s failed: %v", detail, callErr)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok || !strings.Contains(text.Text, credentials.Hint) || !strings.Contains(text.Text, `"enabledToolCount":1`) {
			t.Errorf("health %s missing setup hint or tool count", detail)
		}
	}
	text, failed := callTool(t, s, "server_health", `{"detail":"invalid"}`)
	if !failed || !strings.Contains(text, credentials.Hint) {
		t.Error("invalid health arguments lost setup hint")
	}
	_, failed = callTool(t, s, "card_get", `{"cardId":"c1"}`)
	if !failed {
		t.Error("direct handler bypassed missing credentials")
	}
}

func TestSavedReadScopePolicy(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("policy contacted upstream unexpectedly")
		w.WriteHeader(500)
	}))
	defer fake.Close()
	dir := t.TempDir()
	if err := credentials.Save(filepath.Join(dir, "trello-mcp", "credentials.json"), credentials.File{APIKey: "test-key", Token: "test-token", Scope: "read"}); err != nil {
		t.Fatal(err)
	}
	s := testService(t, fake, nil)
	var err error
	s.config, err = config.Parse(func(string) string { return "" }, func() (string, error) { return dir, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Instructions(), "saved token scope=read; write tools hidden") || s.enabled("card_create") {
		t.Error("saved read scope did not hide writes or explain why")
	}
	text, failed := callTool(t, s, "server_health", `{"detail":"policy"}`)
	for _, want := range []string{`"credentialScope":"read"`, `"readOnly":true`, `"enabledToolCount":19`, `"hiddenWriteTools":29`} {
		if failed || !strings.Contains(text, want) {
			t.Errorf("policy missing %s: %s", want, text)
		}
	}
}
