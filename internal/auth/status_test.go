package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/trello"
)

func TestStatusAndFileOverrides(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/members/me" {
			_, _ = io.WriteString(w, `{"username":"synthetic-user"}`)
		} else {
			_, _ = io.WriteString(w, `{"permissions":[{"read":true,"write":true}]}`)
		}
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	paths := []string{filepath.Join(home, ".secrets", ".trello-mcp.secret"), filepath.Join(home, "override", "custom.secret"), filepath.Join(home, "flag", "chosen.secret")}
	for _, path := range paths {
		if err := credentials.Save(path, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{}
	var out bytes.Buffer
	r := runner{out: &out, env: func(key string) string { return env[key] }, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	for i, path := range paths {
		args := []string{"status"}
		if i >= 1 {
			env["TRELLO_MCP_CREDENTIALS"] = paths[1]
		}
		if i == 2 {
			args = append(args, "--file", path)
		}
		out.Reset()
		if err := r.run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		want := "Credential source: file " + path + "\nConnected as synthetic-user, scope read,write\n"
		if out.String() != want {
			t.Fatalf("status = %q, want %q", out.String(), want)
		}
	}
	env["TRELLO_API_KEY"], env["TRELLO_TOKEN"] = "synthetic-key", "synthetic-token"
	out.Reset()
	if err := r.run(context.Background(), []string{"status", "--file", paths[2]}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Credential source: env (TRELLO_API_KEY, TRELLO_TOKEN)\nConnected as synthetic-user, scope read,write\n" {
		t.Fatalf("unexpected env status %q", out.String())
	}
	delete(env, "TRELLO_API_KEY")
	delete(env, "TRELLO_TOKEN")
	// Logout targets the explicit file and retains all unrelated bytes.
	if err := os.WriteFile(paths[2], []byte("# retained\nOTHER=value\nTRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.run(context.Background(), []string{"logout", "--file", paths[2]}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths[2])
	if err != nil || string(data) != "# retained\nOTHER=value\n" {
		t.Fatal("logout discarded unrelated lines")
	}
	out.Reset()
	if err := r.run(context.Background(), []string{"status", "--file", paths[2]}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Credential source: file "+paths[0]+"\nConnected as synthetic-user, scope read,write\n" {
		t.Fatal("empty override did not fall back to default")
	}
	for _, path := range paths[:2] {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if err := r.run(context.Background(), []string{"status", "--file", paths[2]}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("No credentials in this shell or %s. MCP clients may pass them through their own config; run `trello-mcp auth` to save them for all clients.\n", paths[2])
	if out.String() != want {
		t.Fatalf("missing status %q", out.String())
	}
}

func TestStatusValidationFailure(t *testing.T) {
	for _, status := range []int{401, 403, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"permissions":[]}`)
			}))
			defer fake.Close()
			var out bytes.Buffer
			r := runner{out: &out, env: func(key string) string {
				if key == "TRELLO_API_KEY" || key == "TRELLO_TOKEN" {
					return "synthetic-value"
				}
				return ""
			}, homeDir: func() (string, error) { return credentialTestDir(t), nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
			err := r.run(context.Background(), []string{"status"})
			if err == nil || !strings.Contains(err.Error(), "credential validation failed; check the token and its read permission") || strings.Contains(err.Error(), "synthetic-value") || strings.Contains(out.String(), "synthetic-value") {
				t.Errorf("unsafe or incomplete validation failure: %v", err)
			}
		})
	}
}

func TestAuthFileFlag(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/members/me" {
			_, _ = io.WriteString(w, `{"username":"synthetic-user"}`)
		} else {
			_, _ = io.WriteString(w, `{"permissions":[{"read":true}]}`)
		}
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	path := filepath.Join(home, "chosen", "credentials.secret")
	r := runner{in: strings.NewReader("synthetic-key\nsynthetic-token\n"), out: io.Discard, env: func(key string) string {
		if key == "TRELLO_MCP_CREDENTIALS" {
			return "relative-but-overridden"
		}
		return ""
	}, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	if err := r.run(context.Background(), []string{"--file", path, "--read-only"}); err != nil {
		t.Fatal(err)
	}
	if c, err := credentials.Load(path); err != nil || c.Scope != "read" {
		t.Fatal("auth did not save selected file")
	}
	for _, args := range [][]string{{"--file", "relative"}, {"status", "--file", "relative"}, {"logout", "--file", "relative"}, {"status", "--write"}} {
		if err := r.run(context.Background(), args); err == nil {
			t.Errorf("invalid flags accepted: %v", args)
		}
	}
}

func TestSharedStoreStatusAndAuthIsolation(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/members/me" {
			_, _ = io.WriteString(w, `{"username":"synthetic-user"}`)
		} else {
			_, _ = io.WriteString(w, `{"permissions":[{"read":true}]}`)
		}
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	staged := filepath.Join(home, "staging", "shared.secret")
	if err := credentials.Save(staged, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: "read"}); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(home, ".secrets.env")
	if err := os.Rename(staged, shared); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := runner{in: strings.NewReader("synthetic-key\nsynthetic-token\n"), out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	if err := r.run(context.Background(), []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Credential source: file ~/.secrets.env (shared store)\nConnected as synthetic-user, scope read\n" {
		t.Fatalf("unexpected shared status %q", out.String())
	}
	if err := r.run(context.Background(), []string{"--read-only"}); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Load(filepath.Join(home, ".secrets", ".trello-mcp.secret")); err != nil {
		t.Fatal("auth did not save dedicated file")
	}
	if err := r.run(context.Background(), []string{"logout"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--file", shared}, {"logout", "--file", shared}} {
		if err := r.run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "never modifies the shared") {
			t.Fatal("auth accepted shared write target")
		}
	}
	after, err := os.ReadFile(shared)
	if err != nil || string(after) != string(original) {
		t.Fatal("auth changed shared store")
	}
}

func TestAuthRejectsUnsafeValuesBeforeHTTP(t *testing.T) {
	for _, input := range []string{"'quoted-key'\nsynthetic-token\n", " boundary-key\nsynthetic-token\n", "synthetic-key\n\"quoted-token\"\n", "synthetic-key\ntrailing-token \n", "synthetic-key\nnul\x00token\n"} {
		home := credentialTestDir(t)
		r := runner{in: strings.NewReader(input), out: io.Discard, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }, client: func(string, string) *trello.Client { t.Fatal("invalid credentials reached HTTP boundary"); return nil }}
		if err := r.run(context.Background(), []string{"--read-only"}); err == nil {
			t.Fatal("unsafe auth input accepted")
		}
	}
}

func TestStatusEnvironmentWithoutPaths(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/members/me" {
			_, _ = io.WriteString(w, `{"username":"synthetic-user"}`)
		} else {
			_, _ = io.WriteString(w, `{"permissions":[{"read":true}]}`)
		}
	}))
	defer fake.Close()
	for _, override := range []string{"", "relative/credentials"} {
		t.Run(override, func(t *testing.T) {
			var out bytes.Buffer
			r := runner{out: &out, env: func(key string) string {
				switch key {
				case "TRELLO_API_KEY", "TRELLO_TOKEN":
					return "synthetic-value"
				case "TRELLO_MCP_CREDENTIALS":
					return override
				case "XDG_CONFIG_HOME":
					return "relative/config"
				}
				return ""
			}, homeDir: func() (string, error) {
				t.Fatal("env status must not resolve a home directory")
				return "", nil
			}, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
			if err := r.run(context.Background(), []string{"status"}); err != nil {
				t.Fatal(err)
			}
			if out.String() != "Credential source: env (TRELLO_API_KEY, TRELLO_TOKEN)\nConnected as synthetic-user, scope read\n" {
				t.Fatalf("unexpected status %q", out.String())
			}
		})
	}
}

func TestSharedStoreHardlinkGuard(t *testing.T) {
	home := credentialTestDir(t)
	staged := filepath.Join(home, "staging", "shared.secret")
	if err := credentials.Save(staged, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(home, ".secrets.env")
	if err := os.Rename(staged, shared); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(home, "alias.secret")
	if err := os.Link(shared, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	r := runner{out: io.Discard, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }}
	for _, args := range [][]string{{"--file", alias}, {"logout", "--file", alias}} {
		if err := r.run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "never modifies the shared") {
			t.Fatalf("hard-linked shared target accepted: %v", err)
		}
	}
	data, err := os.ReadFile(shared)
	if err != nil || !strings.Contains(string(data), "TRELLO_TOKEN=synthetic-token") {
		t.Fatal("shared store changed")
	}
}
