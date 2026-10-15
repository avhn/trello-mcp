package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/credentials"
)

func TestReleaseVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "trello-mcp")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	const tag = "v1.2.3-test"
	build := exec.CommandContext(ctx, "go", "build", "-ldflags", "-X main.version="+tag, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}

	checkInstructions(t, ctx, binary)
	checkCredentialStartup(t, ctx, binary)

	version := exec.CommandContext(ctx, binary, "--version")
	version.Env = []string{}
	output, err := version.CombinedOutput()
	if err != nil {
		t.Fatalf("--version without credentials: %v\n%s", err, output)
	}
	if got, want := string(output), "trello-mcp "+tag+"\n"; got != want {
		t.Fatalf("--version = %q, want %q", got, want)
	}

	command := exec.CommandContext(ctx, binary)
	command.Env = []string{
		"TRELLO_API_KEY=synthetic-key",
		"TRELLO_TOKEN=synthetic-token",
		"XDG_CONFIG_HOME=" + t.TempDir(),
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "version-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if got := session.InitializeResult().ServerInfo.Version; got != tag {
		t.Fatalf("MCP version = %q, want %q", got, tag)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "server_health", Arguments: map[string]any{"detail": "metadata"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("metadata result: %+v", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("metadata content: %T", result.Content[0])
	}
	var metadata struct{ Version string }
	if err := json.NewDecoder(strings.NewReader(text.Text)).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Version != tag {
		t.Fatalf("metadata version = %q, want %q", metadata.Version, tag)
	}
}

// checkInstructions exercises the command's InitializeResult, not only the formatter.
func checkInstructions(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	for _, tc := range []struct {
		name           string
		settings, want []string
	}{
		{"unrestricted", nil, []string{"workspaces unrestricted", "Read-only: no"}},
		{"restricted", []string{"TRELLO_ALLOWED_WORKSPACES=team,w2"}, []string{"workspaces restricted to team,w2", "Read-only: no"}},
		{"read-only", []string{"TRELLO_ALLOWED_WORKSPACES=team", "TRELLO_READ_ONLY=1"}, []string{"workspaces restricted to team", "Read-only: yes (write tools hidden)", "Downloads: disabled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, binary)
			command.Env = append([]string{"TRELLO_API_KEY=synthetic-key", "TRELLO_TOKEN=synthetic-token", "TRELLO_BOARD_ID=saved-board", "XDG_CONFIG_HOME=" + t.TempDir()}, tc.settings...)
			client := mcp.NewClient(&mcp.Implementation{Name: "policy-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = session.Close() }()
			instructions := session.InitializeResult().Instructions
			for _, want := range append(tc.want, `server_health with detail "policy"`) {
				if !strings.Contains(instructions, want) {
					t.Errorf("instructions missing %q: %s", want, instructions)
				}
			}
			for _, hidden := range []string{"synthetic-key", "synthetic-token", "saved-board"} {
				if strings.Contains(instructions, hidden) {
					t.Errorf("instructions exposed %q: %s", hidden, instructions)
				}
			}
		})
	}
}

// checkCredentialStartup verifies the real stdio entry point with no environment credentials.
func checkCredentialStartup(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	for _, tc := range []struct {
		scope string
		count int
	}{{"", 1}, {"read", 19}, {"read,write", 48}} {
		t.Run("credentials-"+tc.scope, func(t *testing.T) {
			dir := t.TempDir()
			if tc.scope != "" {
				if err := credentials.Save(filepath.Join(dir, "trello-mcp", "credentials.json"), credentials.File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: tc.scope}); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.CommandContext(ctx, binary)
			command.Env = []string{"XDG_CONFIG_HOME=" + dir}
			client := mcp.NewClient(&mcp.Implementation{Name: "credentials-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = session.Close() }()
			listed, err := session.ListTools(ctx, nil)
			if err != nil || len(listed.Tools) != tc.count {
				t.Fatalf("tools/list count, want %d: %v", tc.count, err)
			}
			if tc.scope == "" {
				if session.InitializeResult().Instructions != credentials.Hint {
					t.Error("initialize missing setup hint")
				}
				health, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "server_health", Arguments: map[string]any{}})
				if callErr != nil || health.IsError || !strings.Contains(health.Content[0].(*mcp.TextContent).Text, credentials.Hint) {
					t.Errorf("health missing setup hint: %v", callErr)
				}
			} else if tc.scope == "read" && !strings.Contains(session.InitializeResult().Instructions, "saved token scope=read") {
				t.Error("saved read scope missing from initialize instructions")
			}
			logout := exec.CommandContext(ctx, binary, "auth", "logout")
			logout.Env = []string{"XDG_CONFIG_HOME=" + dir}
			if output, logoutErr := logout.CombinedOutput(); logoutErr != nil {
				t.Fatalf("auth logout: %v\n%s", logoutErr, output)
			}
			status := exec.CommandContext(ctx, binary, "auth", "status")
			status.Env = []string{"XDG_CONFIG_HOME=" + dir}
			if output, statusErr := status.CombinedOutput(); statusErr != nil || !strings.Contains(string(output), "Credential source: none") {
				t.Fatalf("auth status after logout: %v\n%s", statusErr, output)
			}
		})
	}
}
