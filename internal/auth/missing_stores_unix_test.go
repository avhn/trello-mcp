//go:build unix

package auth

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/tools"
)

func TestMissingStoresInWritableHome(t *testing.T) {
	home := credentialTestDir(t)
	if err := os.Chmod(home, 0775); err != nil {
		t.Fatal(err)
	}
	env := func(string) string { return "" }
	homeDir := func() (string, error) { return home, nil }
	var out bytes.Buffer
	r := runner{out: &out, env: env, homeDir: homeDir}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.run(ctx, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "No credentials in this shell or ") {
		t.Fatalf("unexpected status: %q", out.String())
	}
	c, err := config.Parse(env, homeDir, homeDir)
	if err != nil || c.CredentialSource != "none" || c.HasCredentials() {
		t.Fatalf("unconfigured startup: source %q, error %v", c.CredentialSource, err)
	}
	s := tools.New(c, nil, nil)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{Instructions: s.Instructions()})
	if err := s.Register(server); err != nil {
		t.Fatal(err)
	}
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
		t.Fatal("missing credential instructions")
	}
	listed, err := cs.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "server_health" {
		t.Fatalf("expected health-only registration: %v", err)
	}
}
