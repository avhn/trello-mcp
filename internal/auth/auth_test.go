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
			dir := t.TempDir()
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
			r := runner{in: in, out: &out, env: func(string) string { return "" }, configDir: func() (string, error) { return dir, nil },
				client:  func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) },
				browser: func(address string) error { authorize = address; return errors.New("browser unavailable") }}
			if err = r.run(context.Background(), tc.args); err != nil {
				t.Fatal(err)
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
			path := filepath.Join(dir, "trello-mcp", "credentials.json")
			saved, err := credentials.Load(path)
			if err != nil || saved.Scope != tc.scope || saved.SavedAt.IsZero() {
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
			if err = r.run(context.Background(), []string{"status"}); err != nil || !strings.Contains(out.String(), "Credential source: none") || !strings.Contains(out.String(), credentials.Hint) {
				t.Errorf("missing status failed: %v", err)
			}
		})
	}
}

func TestAuthFailureDoesNotSave(t *testing.T) {
	for _, status := range []int{401, 403, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"username":"synthetic-user","permissions":[]}`)
			}))
			defer fake.Close()
			dir := t.TempDir()
			var out bytes.Buffer
			r := runner{in: strings.NewReader("synthetic-key\nsynthetic-token\n"), out: &out, env: func(string) string { return "" }, configDir: func() (string, error) { return dir, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) }}
			err := r.run(context.Background(), []string{"--read-only"})
			if err == nil || strings.Contains(err.Error(), "synthetic-key") || strings.Contains(err.Error(), "synthetic-token") {
				t.Errorf("unsafe or missing validation error: %v", err)
			}
			if status == 401 && !strings.Contains(err.Error(), credentials.Hint) {
				t.Error("missing setup hint")
			}
			if _, err = os.Stat(filepath.Join(dir, "trello-mcp", "credentials.json")); !errors.Is(err, os.ErrNotExist) {
				t.Error("failed validation saved credentials")
			}
		})
	}
}

func TestAuthFlagsAndEmptyInput(t *testing.T) {
	for _, args := range [][]string{{"--write", "--read-only"}, {"--expiration", "bad"}, {"status", "extra"}, {"unexpected"}, {"--read-only"}} {
		r := runner{in: strings.NewReader(""), out: io.Discard, env: func(string) string { return "" }, configDir: func() (string, error) { return t.TempDir(), nil }}
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
			dir := t.TempDir()
			var out bytes.Buffer
			r := runner{in: strings.NewReader("synthetic-key\n"), out: &out, env: func(string) string { return "" }, configDir: func() (string, error) { return dir, nil }, client: func(key, token string) *trello.Client { return trello.NewClient(fake.URL, key, token, fake.Client()) },
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
			path := filepath.Join(dir, "trello-mcp", "credentials.json")
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
	r := runner{in: strings.NewReader(strings.Repeat("synthetic", 2000)), out: io.Discard, env: func(string) string { return "" }, configDir: func() (string, error) { return t.TempDir(), nil }}
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
			dir := t.TempDir()
			r := runner{
				in: io.MultiReader(strings.NewReader(tc.prefix), blocked), out: io.Discard,
				env: func(string) string { return "" }, configDir: func() (string, error) { return dir, nil },
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
			if _, err = os.Stat(filepath.Join(dir, "trello-mcp", "credentials.json")); !errors.Is(err, os.ErrNotExist) {
				t.Error("cancelled auth saved credentials")
			}
		})
	}
}
