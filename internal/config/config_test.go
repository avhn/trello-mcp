package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avhn/trello-mcp/internal/credentials"
)

// TestParse isolates environment parsing, including fail-closed comma lists and boolean values.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		fail   bool
	}{
		{"defaults", nil, false}, {"profiles", map[string]string{"TRELLO_TOOLS": "card, board_get", "TRELLO_READ_ONLY": "1", "TRELLO_ALLOWED_WORKSPACES": "w1, allowed"}, false}, {"missing key", map[string]string{"TRELLO_API_KEY": ""}, true}, {"missing token", map[string]string{"TRELLO_TOKEN": ""}, true}, {"empty allowlist entry", map[string]string{"TRELLO_ALLOWED_WORKSPACES": "w1,"}, true}, {"empty tools entry", map[string]string{"TRELLO_TOOLS": ",card"}, true}, {"bad boolean", map[string]string{"TRELLO_READ_ONLY": "true"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token"}
			for k, v := range tc.values {
				values[k] = v
			}
			dir := t.TempDir()
			c, err := Parse(func(k string) string { return values[k] }, func() (string, error) { return dir, nil })
			if (err != nil) != tc.fail {
				t.Fatalf("got %v, fail=%v", err, tc.fail)
			}
			if tc.fail {
				return
			}
			if c.StatePath != filepath.Join(dir, "trello-mcp", "state.json") {
				t.Error("wrong XDG state path")
			}
			if tc.name == "profiles" && (!c.ReadOnly || len(c.Tools) != 2 || c.Tools[1] != "board_get" || len(c.AllowedWorkspaces) != 2) {
				t.Errorf("bad parsed profile: tools=%v, allowed=%v", c.Tools, c.AllowedWorkspaces)
			}
		})
	}
}

// TestXDGOverride honors an explicit portable config root, including on macOS.
func TestXDGOverride(t *testing.T) {
	for _, root := range []string{t.TempDir(), "relative"} {
		env := func(key string) string {
			switch key {
			case "TRELLO_API_KEY", "TRELLO_TOKEN":
				return "synthetic"
			case "XDG_CONFIG_HOME":
				return root
			default:
				return ""
			}
		}
		c, err := Parse(env, func() (string, error) { return "", fmt.Errorf("platform fallback must not run") })
		if !filepath.IsAbs(root) {
			if err == nil {
				t.Error("relative XDG_CONFIG_HOME accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.StatePath != filepath.Join(root, "trello-mcp", "state.json") {
			t.Error("XDG override ignored")
		}
	}
}

// TestConfigDirectory honors XDG on every platform without consulting real environment settings.
func TestConfigDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, xdg string
		fail      bool
	}{
		{"explicit", t.TempDir(), false},
		{"relative", "relative/config", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "XDG_CONFIG_HOME": tc.xdg}
			c, err := Parse(func(k string) string { return values[k] }, func() (string, error) {
				t.Fatal("explicit XDG path must not use the platform fallback")
				return "", nil
			})
			if (err != nil) != tc.fail {
				t.Fatalf("got %v, fail=%v", err, tc.fail)
			}
			if !tc.fail && c.StatePath != filepath.Join(tc.xdg, "trello-mcp", "state.json") {
				t.Errorf("unexpected state path %s", c.StatePath)
			}
		})
	}
}

// TestDownloadDirectory validates paths without reading credentials from the environment.
func TestDownloadDirectory(t *testing.T) {
	for _, dir := range []string{"", t.TempDir(), "relative/downloads"} {
		values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "TRELLO_DOWNLOAD_DIR": dir}
		c, err := Parse(func(k string) string { return values[k] }, func() (string, error) { return t.TempDir(), nil })
		if dir != "" && !filepath.IsAbs(dir) {
			if err == nil {
				t.Error("relative download directory accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := dir
		if want == "" {
			want = filepath.Join(os.TempDir(), "trello-mcp")
		}
		if c.DownloadDir != want {
			t.Errorf("download directory got %s; want %s", c.DownloadDir, want)
		}
	}
}

// TestUploadDirectory leaves path uploads disabled by default and requires an absolute root.
func TestUploadDirectory(t *testing.T) {
	for _, dir := range []string{"", t.TempDir(), "relative/uploads"} {
		values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "TRELLO_UPLOAD_DIR": dir}
		c, err := Parse(func(k string) string { return values[k] }, func() (string, error) { return t.TempDir(), nil })
		if dir != "" && !filepath.IsAbs(dir) {
			if err == nil || !strings.Contains(err.Error(), "TRELLO_UPLOAD_DIR must be an absolute path") {
				t.Errorf("relative upload directory: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.UploadDir != dir {
			t.Errorf("upload directory got %s; want %s", c.UploadDir, dir)
		}
	}
}

// TestCredentialSources keeps environment pairs authoritative and saved read scope fail-closed.
func TestCredentialSources(t *testing.T) {
	for _, tc := range []struct {
		name, scope, key, token, readOnly, source string
		missingFile, fail, wantReadOnly           bool
	}{
		{name: "missing", missingFile: true, source: "none"},
		{name: "saved read", scope: "read", source: "file", wantReadOnly: true},
		{name: "saved read cannot opt out", scope: "read", readOnly: "0", source: "file", wantReadOnly: true},
		{name: "saved write", scope: "read,write", source: "file"},
		{name: "env overrides read file", scope: "read", key: "env-key", token: "env-token", source: "env"},
		{name: "partial env never falls back", scope: "read", key: "env-key", fail: true},
		{name: "whitespace env", scope: "read", key: " ", token: "env-token", fail: true},
		{name: "env read only", key: "env-key", token: "env-token", readOnly: "1", missingFile: true, source: "env", wantReadOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "trello-mcp", "credentials.json")
			if !tc.missingFile {
				if err := credentials.Save(path, credentials.File{APIKey: "file-key", Token: "file-token", Scope: tc.scope}); err != nil {
					t.Fatal(err)
				}
			}
			env := func(k string) string {
				return map[string]string{"TRELLO_API_KEY": tc.key, "TRELLO_TOKEN": tc.token, "TRELLO_READ_ONLY": tc.readOnly}[k]
			}
			c, err := Parse(env, func() (string, error) { return dir, nil })
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if tc.fail {
				return
			}
			if c.CredentialSource != tc.source || c.ReadOnly != tc.wantReadOnly || c.HasCredentials() != (tc.source != "none") {
				t.Error("incorrect source, read-only mode or availability")
			}
			if tc.source == "env" && (c.APIKey != tc.key || c.Token != tc.token || c.CredentialScope != "") {
				t.Error("environment precedence failed")
			}
		})
	}
}

func TestEnvironmentSkipsUnsafeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trello-mcp", "credentials.json")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "TRELLO_API_KEY" || k == "TRELLO_TOKEN" {
			return "synthetic"
		}
		return ""
	}
	if _, err := Parse(env, func() (string, error) { return dir, nil }); err != nil {
		t.Fatalf("environment pair should bypass the file: %v", err)
	}
}
