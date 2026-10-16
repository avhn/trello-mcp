package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
		{"defaults", nil, false}, {"profiles", map[string]string{"TRELLO_TOOLS": "card, board_get", "TRELLO_READ_ONLY": "1", "TRELLO_ALLOWED_WORKSPACES": "w1, allowed"}, false}, {"missing key", map[string]string{"TRELLO_API_KEY": ""}, true}, {"missing token", map[string]string{"TRELLO_TOKEN": ""}, true}, {"empty allowlist entry", map[string]string{"TRELLO_ALLOWED_WORKSPACES": "w1,"}, true}, {"empty tools entry", map[string]string{"TRELLO_TOOLS": ",card"}, true}, {"bad boolean", map[string]string{"TRELLO_READ_ONLY": "TRUE"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token"}
			for k, v := range tc.values {
				values[k] = v
			}
			dir := credentialTestDir(t)
			c, err := parseTest(func(k string) string { return values[k] }, func() (string, error) { return dir, nil })
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
	for _, root := range []string{credentialTestDir(t), "relative"} {
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
		c, err := parseTest(env, func() (string, error) { return "", fmt.Errorf("platform fallback must not run") })
		if !filepath.IsAbs(root) {
			if err != nil || c.StatePath != "" {
				t.Error("env-only session did not fall back to in-memory state")
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
		{"explicit", credentialTestDir(t), false},
		{"relative", "relative/config", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "XDG_CONFIG_HOME": tc.xdg}
			c, err := parseTest(func(k string) string { return values[k] }, func() (string, error) {
				t.Fatal("explicit XDG path must not use the platform fallback")
				return "", nil
			})
			if (err != nil) != tc.fail {
				t.Fatalf("got %v, fail=%v", err, tc.fail)
			}
			if filepath.IsAbs(tc.xdg) && c.StatePath != filepath.Join(tc.xdg, "trello-mcp", "state.json") {
				t.Errorf("unexpected state path %s", c.StatePath)
			}
		})
	}
}

// TestDownloadDirectory validates paths without reading credentials from the environment.
func TestDownloadDirectory(t *testing.T) {
	for _, dir := range []string{"", credentialTestDir(t), "relative/downloads"} {
		values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "TRELLO_DOWNLOAD_DIR": dir}
		c, err := parseTest(func(k string) string { return values[k] }, func() (string, error) { return credentialTestDir(t), nil })
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
	for _, dir := range []string{"", credentialTestDir(t), "relative/uploads"} {
		values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "TRELLO_UPLOAD_DIR": dir}
		c, err := parseTest(func(k string) string { return values[k] }, func() (string, error) { return credentialTestDir(t), nil })
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
		{name: "saved read cannot opt out with false", scope: "read", readOnly: "false", source: "file", wantReadOnly: true},
		{name: "saved write", scope: "read,write", source: "file"},
		{name: "saved unknown scope is writable", source: "file"},
		{name: "env overrides read file", scope: "read", key: "env-key", token: "env-token", source: "env"},
		{name: "partial env never falls back", scope: "read", key: "env-key", fail: true},
		{name: "whitespace env", scope: "read", key: " ", token: "env-token", fail: true},
		{name: "env read only", key: "env-key", token: "env-token", readOnly: "1", missingFile: true, source: "env", wantReadOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := credentialTestDir(t)
			path := filepath.Join(dir, ".secrets", ".trello-mcp.secret")
			if !tc.missingFile {
				if err := credentials.Save(path, credentials.File{APIKey: "file-key", Token: "file-token", Scope: tc.scope}); err != nil {
					t.Fatal(err)
				}
			}
			env := func(k string) string {
				return map[string]string{"TRELLO_API_KEY": tc.key, "TRELLO_TOKEN": tc.token, "TRELLO_READ_ONLY": tc.readOnly}[k]
			}
			c, err := parseTest(env, func() (string, error) { return dir, nil })
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
	dir := credentialTestDir(t)
	path := filepath.Join(dir, ".secrets", ".trello-mcp.secret")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "TRELLO_API_KEY" || k == "TRELLO_TOKEN" {
			return "synthetic"
		}
		return ""
	}
	if _, err := parseTest(env, func() (string, error) { return dir, nil }); err != nil {
		t.Fatalf("environment pair should bypass the file: %v", err)
	}
}

// parseTest uses an isolated synthetic home, never the real user's credential path.
func parseTest(env func(string) string, configDir func() (string, error)) (Config, error) {
	return Parse(env, configDir, func() (string, error) {
		if path := env("XDG_CONFIG_HOME"); filepath.IsAbs(path) {
			return path, nil
		}
		return configDir()
	})
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

func TestSharedStoreReadOnly(t *testing.T) {
	home := credentialTestDir(t)
	staged := filepath.Join(home, "staging", "shared.secret")
	if err := credentials.Save(staged, credentials.File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: "read"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, filepath.Join(home, ".secrets.env")); err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		if key == "TRELLO_READ_ONLY" {
			return "0"
		}
		return ""
	}
	c, err := Parse(env, func() (string, error) { return credentialTestDir(t), nil }, func() (string, error) { return home, nil })
	if err != nil || !c.ReadOnly || c.CredentialScope != "read" || c.CredentialSource != "file" {
		t.Fatal("shared read scope did not force read-only")
	}
}

func TestEnvironmentWithoutCredentialPaths(t *testing.T) {
	for _, override := range []string{"", "relative/credentials"} {
		t.Run(override, func(t *testing.T) {
			values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "TRELLO_MCP_CREDENTIALS": override, "XDG_CONFIG_HOME": "relative/config"}
			c, err := Parse(func(key string) string { return values[key] }, func() (string, error) {
				t.Fatal("invalid explicit XDG path must not use platform fallback")
				return "", nil
			}, func() (string, error) {
				t.Fatal("environment credentials must not resolve a home directory")
				return "", nil
			})
			if err != nil || !c.HasCredentials() || c.CredentialSource != "env" || c.StatePath != "" {
				t.Fatalf("env-only configuration failed: %v", err)
			}
		})
	}
}

func TestLoadEnvironmentWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TRELLO_MCP_CREDENTIALS", "relative/credentials")
	t.Setenv("TRELLO_API_KEY", "synthetic-key")
	t.Setenv("TRELLO_TOKEN", "synthetic-token")
	c, err := Load()
	if err != nil || !c.HasCredentials() || c.CredentialSource != "env" {
		t.Fatalf("environment credentials without home failed: %v", err)
	}
}

func TestInvalidStateDirectoryWithoutEnvironment(t *testing.T) {
	home := credentialTestDir(t)
	_, err := Parse(func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return "relative/config"
		}
		return ""
	}, func() (string, error) { return home, nil }, func() (string, error) { return home, nil })
	if err == nil || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
		t.Fatalf("invalid state directory accepted without env pair: %v", err)
	}
}

// TestReadOnlyLiterals accepts only the documented CLI and Desktop forms.
func TestReadOnlyLiterals(t *testing.T) {
	for _, value := range []string{"", "0", "1", "false", "true", "TRUE", "False", "yes", "2", " true", "false ", "null"} {
		t.Run("value="+value, func(t *testing.T) {
			values := map[string]string{"TRELLO_API_KEY": "synthetic-key", "TRELLO_TOKEN": "synthetic-token", "TRELLO_READ_ONLY": value}
			c, err := Parse(func(key string) string { return values[key] }, func() (string, error) { return "", fmt.Errorf("unavailable config") }, func() (string, error) {
				t.Fatal("environment pair must not resolve credential paths")
				return "", nil
			})
			valid := value == "" || value == "0" || value == "1" || value == "false" || value == "true"
			if !valid {
				if err == nil || !strings.Contains(err.Error(), "TRELLO_READ_ONLY must be 0, 1, false or true") {
					t.Fatalf("invalid boolean was not rejected: %v", err)
				}
				return
			}
			if err != nil || c.ReadOnly != (value == "1" || value == "true") || c.CredentialScope != "" {
				t.Fatalf("incorrect environment boolean or inferred scope: %v", err)
			}
		})
	}
}

func TestPartialEnvironmentSkipsCredentialPaths(t *testing.T) {
	for _, key := range []string{"TRELLO_API_KEY", "TRELLO_TOKEN"} {
		t.Run(key, func(t *testing.T) {
			_, err := Parse(func(name string) string {
				if name == key {
					return "synthetic"
				}
				if name == "TRELLO_MCP_CREDENTIALS" {
					return "relative/inaccessible"
				}
				return ""
			}, func() (string, error) { t.Fatal("partial pair must fail before state resolution"); return "", nil }, func() (string, error) {
				t.Fatal("partial pair must not fall back to credential files")
				return "", nil
			})
			if err == nil || !strings.Contains(err.Error(), "must be set together") {
				t.Fatalf("partial pair: %v", err)
			}
		})
	}
}
