package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/trello"
)

func TestAuthFlow(t *testing.T) {
	for _, tc := range []struct {
		name, input, scope string
		args               []string
	}{
		{"default write", "synthetic-key\n\nsynthetic-token\n", "read,write", nil},
		{"decline write", "synthetic-key\nn\nsynthetic-token\n", "read", nil},
		{"read flag", "synthetic-key\nsynthetic-token\n", "read", []string{"--read-only"}},
		{"write flag", "synthetic-key\nsynthetic-token", "read,write", []string{"--write", "--expiration", "30days"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				if req.Method != "GET" || req.URL.Query().Get("key") != "synthetic-key" || req.URL.Query().Get("token") != "synthetic-token" {
					t.Error("invalid validation request")
				}
				switch req.URL.Path {
				case "/members/me":
					if req.URL.Query().Get("fields") != "username,fullName" {
						t.Error("wrong member fields")
					}
					_, _ = io.WriteString(w, `{"username":"synthetic-user","fullName":"Synthetic User"}`)
				case "/tokens/synthetic-token":
					if req.URL.Query().Get("fields") != "permissions" {
						t.Error("wrong token fields")
					}
					_, _ = fmt.Fprintf(w, `{"permissions":[{"read":true,"write":%t}]}`, tc.scope == "read,write")
				default:
					t.Error("unexpected validation path")
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			dir := credentialTestDir(t)
			if runtime.GOOS != "windows" {
				t.Setenv("HOME", dir)
				if err := os.Mkdir(filepath.Join(dir, ".secrets"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(dir, ".secrets"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			// A real pipe exercises the non-TTY path, including buffered input and final EOF.
			in, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = in.Close() }()
			if _, err = io.WriteString(writer, tc.input); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			var authorize string
			r := runner{in: in, out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return dir, nil },
				client:  func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) },
				browser: func(address string) error { authorize = address; return errors.New("browser unavailable") }}
			if err = r.run(context.Background(), tc.args); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(filepath.Join(dir, ".secrets"))
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatal("auth did not repair 0755 ~/.secrets to 0700")
				}
			}
			u, err := url.Parse(authorize)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			expiration := "never"
			if tc.name == "write flag" {
				expiration = "30days"
			}
			if u.Scheme != "https" || u.Host != "trello.com" || u.Path != "/1/authorize" || q.Get("scope") != tc.scope || q.Get("expiration") != expiration || q.Get("name") != "trello-mcp" || q.Get("response_type") != "token" || q.Get("key") != "synthetic-key" {
				t.Error("wrong authorization URL")
			}
			for _, want := range []string{"https://trello.com/power-ups/admin", "synthetic-user", "Granted scope: " + tc.scope} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %s", want)
				}
			}
			if strings.Contains(out.String(), "synthetic-token") || strings.Count(out.String(), "synthetic-key") != 1 {
				t.Error("credentials echoed outside required authorization URL")
			}
			path := filepath.Join(dir, ".secrets", ".trello-mcp.secret")
			saved, err := credentials.Load(path)
			if err != nil || saved.Scope != tc.scope {
				t.Fatal("credentials were not saved correctly")
			}
			if requests != 2 {
				t.Error("setup must use two validation requests")
			}
			out.Reset()
			if err = r.run(context.Background(), []string{"status"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Credential source: file") || !strings.Contains(out.String(), tc.scope) || !strings.Contains(out.String(), "synthetic-user") || strings.Contains(out.String(), "synthetic-token") || strings.Contains(out.String(), "synthetic-key") {
				t.Error("unsafe or incomplete status")
			}
			r.env = func(k string) string {
				switch k {
				case "TRELLO_API_KEY":
					return "synthetic-key"
				case "TRELLO_TOKEN":
					return "synthetic-token"
				default:
					return ""
				}
			}
			out.Reset()
			if err = r.run(context.Background(), []string{"status"}); err != nil || !strings.Contains(out.String(), "Credential source: env") {
				t.Errorf("environment status failed: %v", err)
			}
			if err = r.run(context.Background(), []string{"logout"}); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Error("logout left credentials behind")
			}
			r.env = func(string) string { return "" }
			out.Reset()
			if err = r.run(context.Background(), []string{"status"}); err != nil || !strings.Contains(out.String(), "No credentials in this shell or "+path) {
				t.Errorf("missing status failed: %v", err)
			}
		})
	}
}

func TestAuthFailureDoesNotSave(t *testing.T) {
	for _, status := range []int{401, 403, 200} {
		for _, failAt := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d/get%d", status, failAt), func(t *testing.T) {
				requests := 0
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					requests++
					if requests == failAt {
						w.WriteHeader(status)
						_, _ = io.WriteString(w, `{"username":"synthetic-user","permissions":[]}`)
						return
					}
					_, _ = io.WriteString(w, `{"username":"synthetic-user"}`)
				}))
				defer fake.Close()
				dir := credentialTestDir(t)
				// Save creates the parent with an owner-only DACL on Windows.
				path := filepath.Join(dir, ".secrets", "credentials.secret")
				var out bytes.Buffer
				r := runner{in: strings.NewReader("synthetic-key\nsynthetic-token\n"), out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return dir, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
				checkError := func(err error) {
					t.Helper()
					if err == nil {
						t.Fatal("missing validation error")
					}
					for _, hidden := range []string{"synthetic-key", "synthetic-token", "not configured", "Only server_health", "workspace_find"} {
						if strings.Contains(err.Error(), hidden) {
							t.Errorf("unexpected %q in validation error: %v", hidden, err)
						}
					}
					if status != 200 {
						var upstream *trello.HTTPError
						if !errors.As(err, &upstream) || upstream.StatusCode != status {
							t.Errorf("HTTP status lost: %v", err)
						}
					}
					if status == 401 {
						for _, want := range []string{"token rejected during auth validation", "HTTP 401", "API key and token match", "expiry or revocation", "read permission", "nothing saved"} {
							if !strings.Contains(err.Error(), want) {
								t.Errorf("missing %q: %v", want, err)
							}
						}
					} else if status == 403 && err.Error() != (&trello.HTTPError{StatusCode: status}).Error() && !strings.HasPrefix(err.Error(), "credential validation failed;") {
						t.Errorf("non-401 validation error changed: %v", err)
					}
				}
				checkError(r.run(context.Background(), []string{"--read-only", "--file", path}))
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed validation saved credentials")
				}
				if err := credentials.Save(path, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: "read"}); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				requests = 0
				out.Reset()
				err = r.run(context.Background(), []string{"status", "--file", path})
				checkError(err)
				if !strings.HasPrefix(err.Error(), "credential validation failed;") {
					t.Errorf("status context lost: %v", err)
				}
				if strings.Contains(out.String(), "synthetic-key") || strings.Contains(out.String(), "synthetic-token") {
					t.Error("status echoed credentials")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Error("failed status changed saved credentials")
				}
			})
		}
	}
}

func TestAuthFlagsAndEmptyInput(t *testing.T) {
	for _, args := range [][]string{{"--write", "--read-only"}, {"--expiration", "bad"}, {"status", "extra"}, {"unexpected"}, {"--read-only"}} {
		r := runner{in: strings.NewReader(""), out: io.Discard, env: func(string) string { return "" }, homeDir: func() (string, error) { return credentialTestDir(t), nil }}
		if err := r.run(context.Background(), args); err == nil {
			t.Errorf("invalid input accepted: %v", args)
		}
	}
}

func TestHiddenTokenAndGrantedScope(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		hiddenFail, write, fail bool
		args                    []string
	}{
		{name: "hidden prompt", args: []string{"--write", "--expiration", "1day"}},
		{name: "hidden error", hiddenFail: true, fail: true, args: []string{"--write"}},
		{name: "writable token refused for read-only", write: true, fail: true, args: []string{"--read-only"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/members/me" {
					_, _ = io.WriteString(w, `{"username":"synthetic-user"}`)
				} else {
					_, _ = fmt.Fprintf(w, `{"permissions":[{"read":true,"write":%t}]}`, tc.write)
				}
			}))
			defer fake.Close()
			dir := credentialTestDir(t)
			var out bytes.Buffer
			r := runner{in: strings.NewReader("synthetic-key\n"), out: &out, env: func(string) string { return "" }, homeDir: func() (string, error) { return dir, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) },
				hiddenToken: func(context.Context) (string, error) {
					if tc.hiddenFail {
						return "", errors.New("synthetic-token")
					}
					return "synthetic-token", nil
				}}
			err := r.run(context.Background(), tc.args)
			if (err != nil) != tc.fail || (err != nil && strings.Contains(err.Error(), "synthetic-token")) || strings.Contains(out.String(), "synthetic-token") || !strings.Contains(out.String(), "Token (hidden):") {
				t.Errorf("hidden input failed or leaked a token: %v", err)
			}
			path := filepath.Join(dir, ".secrets", ".trello-mcp.secret")
			if tc.fail {
				if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Error("failed auth saved credentials")
				}
			} else {
				saved, loadErr := credentials.Load(path)
				if loadErr != nil || saved.Scope != "read" {
					t.Error("saved scope must follow granted permissions, not requested write scope")
				}
			}
		})
	}
}

func TestBoundedPrompt(t *testing.T) {
	r := runner{in: strings.NewReader(strings.Repeat("synthetic", 2000)), out: io.Discard, env: func(string) string { return "" }, homeDir: func() (string, error) { return credentialTestDir(t), nil }}
	if err := r.run(context.Background(), []string{"--write"}); err == nil || !strings.Contains(err.Error(), "exceeded 8192 bytes") {
		t.Errorf("oversized piped input accepted: %v", err)
	}
}

// inputObserver signals when a prompt reaches the pipe that has no complete line.
type inputObserver struct {
	io.Reader
	started chan struct{}
}

func (r inputObserver) Read(p []byte) (int, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	return r.Reader.Read(p)
}

func TestAuthInputCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
		args         []string
		hidden       bool
	}{
		{name: "API key"},
		{name: "write confirmation", prefix: "synthetic-key\n"},
		{name: "piped token", prefix: "synthetic-key\n", args: []string{"--write"}},
		{name: "hidden token", prefix: "synthetic-key\n", args: []string{"--write"}, hidden: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = in.Close() }()
			defer func() { _ = writer.Close() }()
			started := make(chan struct{}, 1)
			blocked := inputObserver{Reader: in, started: started}
			dir := credentialTestDir(t)
			r := runner{
				in: io.MultiReader(strings.NewReader(tc.prefix), blocked), out: io.Discard,
				env: func(string) string { return "" }, homeDir: func() (string, error) { return dir, nil },
			}
			if tc.hidden {
				r.in = strings.NewReader(tc.prefix)
				r.hiddenToken = func(ctx context.Context) (string, error) {
					return readInput(ctx, func() (string, error) {
						data, err := io.ReadAll(blocked)
						return string(data), err
					})
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.run(ctx, tc.args) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("auth did not reach the blocked input")
			}
			cancel()
			select {
			case err = <-done:
				if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "auth cancelled") {
					t.Fatalf("wrong cancellation error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("auth did not cancel within one second")
			}
			if _, err = os.Stat(filepath.Join(dir, ".secrets", ".trello-mcp.secret")); !errors.Is(err, os.ErrNotExist) {
				t.Error("cancelled auth saved credentials")
			}
		})
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
	// Synthetic home directories must be safe even when the test runner creates
	// temporary directories with group write permission.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAuthHelpBeforeDependencies(t *testing.T) {
	for _, command := range []string{"", "status", "logout"} {
		for _, help := range []string{"--help", "-h"} {
			t.Run(command+"/"+help, func(t *testing.T) {
				var out bytes.Buffer
				// Unset input, environment, home, HTTP and browser dependencies must
				// never be accessed on the help path.
				r := runner{out: &out}
				args := []string{"--file", "relative/invalid", help}
				if command != "" {
					args = append([]string{command}, args...)
				}
				if err := r.run(context.Background(), args); err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"Usage: trello-mcp auth", "--file PATH", "auth, status, logout", "--read-only", "auth only", "--expiration", "private terminal", "restart", "server_health"} {
					if !strings.Contains(out.String(), want) {
						t.Errorf("help missing %q", want)
					}
				}
				if command != "" && !strings.HasPrefix(out.String(), "Usage: trello-mcp auth "+command+" [--file PATH]\n") {
					t.Error("subcommand usage includes inapplicable flags")
				}
			})
		}
	}
}
