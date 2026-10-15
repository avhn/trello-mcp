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
