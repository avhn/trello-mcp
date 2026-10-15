package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
