package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
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
	binary := filepath.Join(credentialTestDir(t), "trello-mcp")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	const tag = "v1.2.3-test"
	build := exec.CommandContext(ctx, "go", "build", "-ldflags", "-X main.version="+tag, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}

	checkHelp(t, ctx, binary)
	checkCredentialRecovery(t, ctx, binary)
	checkInstructions(t, ctx, binary)
	checkCredentialStartup(t, ctx, binary)
	checkEnvironmentOnlyStartup(t, ctx, binary)
	checkAuthCancellation(t, ctx, binary)

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
		"HOME=" + credentialTestDir(t), "USERPROFILE=" + credentialTestDir(t), "XDG_CONFIG_HOME=" + credentialTestDir(t),
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
		{"unrestricted", nil, []string{"Workspaces: unrestricted", "read-only: false"}},
		{"restricted", []string{"TRELLO_ALLOWED_WORKSPACES=team,w2"}, []string{"Workspaces: restricted", "read-only: false"}},
		{"read-only", []string{"TRELLO_ALLOWED_WORKSPACES=team", "TRELLO_READ_ONLY=1"}, []string{"Workspaces: restricted", "read-only: true", "downloads: false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, binary)
			command.Env = append([]string{"TRELLO_API_KEY=synthetic-key", "TRELLO_TOKEN=synthetic-token", "TRELLO_BOARD_ID=saved-board", "HOME=" + credentialTestDir(t), "USERPROFILE=" + credentialTestDir(t), "XDG_CONFIG_HOME=" + credentialTestDir(t)}, tc.settings...)
			client := mcp.NewClient(&mcp.Implementation{Name: "policy-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = session.Close() }()
			instructions := session.InitializeResult().Instructions
			for _, want := range append(tc.want, `server_health detail "policy"`) {
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
			dir := credentialTestDir(t)
			if tc.scope != "" {
				if err := credentials.Save(filepath.Join(dir, ".secrets", ".trello-mcp.secret"), credentials.File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: tc.scope}); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.CommandContext(ctx, binary)
			command.Env = []string{"XDG_CONFIG_HOME=" + dir, "HOME=" + dir, "USERPROFILE=" + dir}
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
			} else if tc.scope == "read" && !strings.Contains(session.InitializeResult().Instructions, "read-only: true") {
				t.Error("saved read scope missing from initialize policy summary")
			}
			logout := exec.CommandContext(ctx, binary, "auth", "logout")
			logout.Env = command.Env
			if output, logoutErr := logout.CombinedOutput(); logoutErr != nil {
				t.Fatalf("auth logout: %v\n%s", logoutErr, output)
			}
			status := exec.CommandContext(ctx, binary, "auth", "status")
			status.Env = command.Env
			if output, statusErr := status.CombinedOutput(); statusErr != nil || !strings.Contains(string(output), "No credentials in this shell or ") {
				t.Fatalf("auth status after logout: %v\n%s", statusErr, output)
			}
		})
	}
}

// checkAuthCancellation verifies the CLI exits instead of retaining a blocked stdin reader.
func checkAuthCancellation(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return // os.Process.Signal does not support Interrupt on Windows.
	}
	command := exec.CommandContext(ctx, binary, "auth")
	command.Env = []string{"HOME=" + credentialTestDir(t), "USERPROFILE=" + credentialTestDir(t), "XDG_CONFIG_HOME=" + credentialTestDir(t)}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill() }()
	ready := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		var output strings.Builder
		for !strings.HasSuffix(output.String(), "API key: ") {
			b, err := reader.ReadByte()
			if err != nil {
				ready <- err
				return
			}
			output.WriteByte(b)
		}
		ready <- nil
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("auth prompt: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("auth prompt did not appear")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 130 {
			t.Fatalf("auth interrupt = %v, want exit 130", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("auth did not exit with blocked stdin")
	}
}

// credentialTestDir resolves system temporary-directory aliases (such as /var on macOS).
// The credential boundary deliberately refuses symlinks in parent directories.
func credentialTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Keep synthetic homes safe independently of temporary-directory defaults.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func checkEnvironmentOnlyStartup(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	for _, xdg := range []string{"", "relative/config"} {
		t.Run("env-only-"+xdg, func(t *testing.T) {
			command := exec.CommandContext(ctx, binary)
			command.Env = []string{"TRELLO_API_KEY=synthetic-key", "TRELLO_TOKEN=synthetic-token", "TRELLO_MCP_CREDENTIALS=relative/credentials", "XDG_CONFIG_HOME=" + xdg}
			client := mcp.NewClient(&mcp.Implementation{Name: "env-only-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
			if err != nil {
				t.Fatalf("startup without home and with invalid credential path: %v", err)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// checkHelp uses invalid configuration and noninteractive stdin to catch startup side effects.
func checkHelp(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	for _, args := range [][]string{
		{"--help"}, {"-h"}, {"auth", "--help"}, {"auth", "-h"},
		{"auth", "status", "--help"}, {"auth", "status", "-h"},
		{"auth", "logout", "--help"}, {"auth", "logout", "-h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := credentialTestDir(t)
			command := exec.CommandContext(ctx, binary, args...)
			command.Env = []string{
				"HOME=" + dir, "USERPROFILE=" + dir, "XDG_CONFIG_HOME=relative/config",
				"TRELLO_MCP_CREDENTIALS=relative/credentials", "TRELLO_READ_ONLY=invalid",
			}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("help: %v\n%s", err, output)
			}
			for _, want := range []string{"Usage:", "private terminal", "server_health", "restart", "https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md"} {
				if !strings.Contains(string(output), want) {
					t.Errorf("help missing %q: %s", want, output)
				}
			}
			for _, unwanted := range []string{"API key: ", "Token: ", "Token (hidden): ", "https://trello.com/1/authorize?"} {
				if strings.Contains(string(output), unwanted) {
					t.Errorf("help started authentication: %s", output)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("help modified synthetic home: %v", err)
			}
		})
	}
	for _, args := range [][]string{{"unexpected"}, {"--unknown"}, {"auth", "unknown"}, {"auth", "--unknown"}, {"auth", "status", "--write"}, {"auth", "logout", "--read-only"}} {
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = []string{"TRELLO_MCP_CREDENTIALS=relative/credentials"}
		if output, err := command.CombinedOutput(); err == nil {
			t.Errorf("unexpected success for %v: %s", args, output)
		} else if strings.Contains(string(output), "must be an absolute path") {
			t.Errorf("arguments reached credential resolution for %v: %s", args, output)
		}
	}
}

// checkCredentialRecovery proves saved synthetic credentials require a fresh server.
func checkCredentialRecovery(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	dir := credentialTestDir(t)
	connect := func() *mcp.ClientSession {
		command := exec.CommandContext(ctx, binary)
		command.Env = []string{"HOME=" + dir, "USERPROFILE=" + dir, "XDG_CONFIG_HOME=" + dir}
		client := mcp.NewClient(&mcp.Implementation{Name: "recovery-test", Version: "1"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	unconfigured := connect()
	if unconfigured.InitializeResult().Instructions != credentials.Hint {
		t.Fatal("unconfigured server missing human auth handoff")
	}
	if err := credentials.Save(filepath.Join(dir, ".secrets", ".trello-mcp.secret"), credentials.File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: "read,write"}); err != nil {
		t.Fatal(err)
	}
	listed, err := unconfigured.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "server_health" {
		t.Fatalf("running unconfigured server must stay health-only: %v", err)
	}
	configured := connect()
	listed, err = configured.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 48 {
		t.Fatalf("fresh configured server must expose 48 tools: %v", err)
	}
}
