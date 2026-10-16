package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/trello"
)

func TestStatusAndFileOverrides(t *testing.T) {
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
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
		before := requests.Load()
		if err := r.run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		if requests.Load()-before != 2 {
			t.Fatal("status must validate identity and read permission with two GETs")
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
	before := requests.Load()
	if err := r.run(context.Background(), []string{"status", "--file", paths[2]}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before {
		t.Fatal("missing credentials made an HTTP request")
	}
	want := fmt.Sprintf("No credentials in this shell or %s. MCP clients may pass them through their own config. To save CLI credentials, the human runs the installer's quoted absolute-path auth command in a private terminal, then reconnects or restarts clients.\n", paths[2])
	if out.String() != want || strings.Contains(out.String(), "`trello-mcp auth`") {
		t.Fatalf("missing status %q", out.String())
	}
}

func TestStatusValidationFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failAt      int
		status      int
		identity    string
		permissions string
	}{
		{"identity unauthorized", 1, 401, "synthetic-user", `{"permissions":[{"read":true}]}`},
		{"identity forbidden", 1, 403, "synthetic-user", `{"permissions":[{"read":true}]}`},
		{"grant unauthorized", 2, 401, "synthetic-user", `{"permissions":[{"read":true}]}`},
		{"grant forbidden", 2, 403, "synthetic-user", `{"permissions":[{"read":true}]}`},
		{"missing identity", 0, 200, "", `{"permissions":[{"read":true}]}`},
		{"missing read grant", 0, 200, "synthetic-user", `{"permissions":[{"write":true}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				request := int(requests.Add(1))
				if req.Method != http.MethodGet {
					t.Errorf("status sent %s", req.Method)
				}
				switch {
				case request == tc.failAt:
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, `{"message":"synthetic-key synthetic-token"}`)
				case req.URL.Path == "/members/me":
					_, _ = fmt.Fprintf(w, `{"username":%q}`, tc.identity)
				default:
					_, _ = io.WriteString(w, tc.permissions)
				}
			}))
			defer fake.Close()
			home := credentialTestDir(t)
			// Save creates this subdirectory with a protected DACL on Windows.
			path := filepath.Join(home, "status", "credentials.secret")
			if err := credentials.Save(path, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			r := runner{out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
			err = r.run(context.Background(), []string{"status", "--file", path})
			const recovery = "credential validation failed; check the token and its read permission. If renewal is needed, the human runs the installer's quoted absolute-path auth command with --file and the path above in a private terminal, then reconnects or restarts clients: "
			if err == nil || !strings.HasPrefix(err.Error(), recovery) {
				t.Fatalf("incomplete validation failure: %v", err)
			}
			for _, text := range []string{err.Error(), out.String()} {
				if strings.Contains(text, "synthetic-key") || strings.Contains(text, "synthetic-token") || strings.Contains(text, "`trello-mcp auth`") {
					t.Fatal("status leaked credentials or advised a bare auth command")
				}
			}
			var cause *trello.HTTPError
			if tc.failAt != 0 && (!errors.As(err, &cause) || cause.StatusCode != tc.status) {
				t.Fatalf("HTTP cause not preserved: %v", err)
			}
			wantRequests := 2
			if tc.failAt == 1 {
				wantRequests = 1
			}
			if int(requests.Load()) != wantRequests {
				t.Errorf("requests = %d, want %d", requests.Load(), wantRequests)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(after, original) {
				t.Fatal("status changed saved credentials")
			}
			if _, err := os.Stat(filepath.Join(home, ".secrets", ".trello-mcp.secret")); !os.IsNotExist(err) {
				t.Fatal("status created default credentials")
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

func TestLogoutWithoutSavedKeys(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprintf("exists=%t", exists), func(t *testing.T) {
			home := credentialTestDir(t)
			path := filepath.Join(home, ".secrets", "test.secret")
			const unrelated = "# retained\nOTHER=synthetic\n"
			if err := credentials.Save(path, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
				t.Fatal(err)
			}
			if exists {
				// Truncate the existing protected file without recreating its DACL.
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteString(unrelated)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("write unrelated fixture: %v, close: %v", err, closeErr)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			r := runner{out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }}
			if err := r.run(context.Background(), []string{"logout", "--file", path}); err != nil {
				t.Fatal(err)
			}
			if want := "No saved Trello credentials at " + path + ".\n"; out.String() != want {
				t.Fatalf("logout = %q, want %q", out.String(), want)
			}
			if exists {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != unrelated {
					t.Fatalf("unrelated file changed: %v", err)
				}
			}
		})
	}
}

func TestStatusValidationFailureFromEnv(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	env := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token"}
	var out bytes.Buffer
	r := runner{out: &out, env: func(key string) string { return env[key] }, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	err := r.run(context.Background(), []string{"status"})
	// Renewing a saved file cannot replace environment credentials, which win.
	const recovery = "credential validation failed; check the token and its read permission. If renewal is needed, the human updates or unsets TRELLO_API_KEY and TRELLO_TOKEN where they are set, then reconnects or restarts clients: "
	if err == nil || !strings.HasPrefix(err.Error(), recovery) {
		t.Fatalf("env recovery missing: %v", err)
	}
	if strings.Contains(err.Error()+out.String(), "synthetic-key") || strings.Contains(err.Error()+out.String(), "synthetic-token") {
		t.Fatal("status leaked credentials")
	}
	if !strings.Contains(out.String(), "Credential source: env") {
		t.Fatalf("source not reported: %q", out.String())
	}
}

func TestStatusValidationFailureFromSharedStore(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	staged := filepath.Join(home, "staging", "shared.secret")
	if err := credentials.Save(staged, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, filepath.Join(home, ".secrets.env")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := runner{out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	err := r.run(context.Background(), []string{"status"})
	// auth refuses to write the shared store, so recovery must not suggest --file for it.
	const recovery = "credential validation failed; check the token and its read permission. If renewal is needed, the human runs the installer's quoted absolute-path auth command without --file in a private terminal (the dedicated file outranks the shared store) or updates the shared store privately, then reconnects or restarts clients: "
	if err == nil || !strings.HasPrefix(err.Error(), recovery) {
		t.Fatalf("shared-store recovery missing: %v", err)
	}
	if !strings.Contains(out.String(), "Credential source: file ~/.secrets.env (shared store)") {
		t.Fatalf("source not reported: %q", out.String())
	}
}

func TestStatusValidationFailureFromSharedStoreFile(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	staged := filepath.Join(home, "staging", "shared.secret")
	if err := credentials.Save(staged, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, filepath.Join(home, ".secrets.env")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := runner{out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	err := r.run(context.Background(), []string{"status", "--file", filepath.Join(home, ".secrets.env")})
	// --file is not the environment override, so plain auth still outranks the shared store.
	const recovery = "credential validation failed; check the token and its read permission. If renewal is needed, the human runs the installer's quoted absolute-path auth command without --file in a private terminal (the dedicated file outranks the shared store) or updates the shared store privately, then reconnects or restarts clients: "
	if err == nil || !strings.HasPrefix(err.Error(), recovery) {
		t.Fatalf("shared-store recovery missing: %v", err)
	}
	if !strings.Contains(out.String(), "Credential source: file ~/.secrets.env (shared store)") {
		t.Fatalf("source not reported: %q", out.String())
	}
}

func TestStatusValidationFailureFromSharedStoreOverride(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()
	home := credentialTestDir(t)
	staged := filepath.Join(home, "staging", "shared.secret")
	if err := credentials.Save(staged, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, filepath.Join(home, ".secrets.env")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := runner{out: &out, env: func(k string) string {
		if k == "TRELLO_MCP_CREDENTIALS" {
			return filepath.Join(home, ".secrets.env")
		}
		return ""
	}, homeDir: func() (string, error) { return home, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
	err := r.run(context.Background(), []string{"status"})
	// The override ranks the shared store first, so auth without --file cannot replace it.
	const recovery = "credential validation failed; check the token and its read permission. If renewal is needed, the human updates the shared store privately, or points TRELLO_MCP_CREDENTIALS at a dedicated file, wherever it is set, and runs the installer's quoted absolute-path auth command in a private terminal, then reconnects or restarts clients: "
	if err == nil || !strings.HasPrefix(err.Error(), recovery) {
		t.Fatalf("shared-store recovery missing: %v", err)
	}
	if !strings.Contains(out.String(), "Credential source: file ~/.secrets.env (shared store)") {
		t.Fatalf("source not reported: %q", out.String())
	}
}
