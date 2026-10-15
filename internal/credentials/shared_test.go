package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sharedFixture uses the platform's protected file creation even on Windows,
// without requiring an owner-only DACL on the synthetic home directory itself.
func sharedFixture(t *testing.T, home, data string) string {
	t.Helper()
	root, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	file, err := createPrivateFile(root, ".secrets.env")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".secrets.env")
}

func TestSharedFallback(t *testing.T) {
	home := credentialTestDir(t)
	data := "# shared store\nOTHER=\"ignored synthetic value\"\nUNRELATED=\x00\r\nnot a dotenv assignment\nTRELLO_API_KEY=shared-key\nTRELLO_TOKEN=shared-token\nTRELLO_TOKEN_SCOPE=read\n"
	shared := sharedFixture(t, home, data)
	values := map[string]string{}
	env := func(key string) string { return values[key] }
	paths, err := Paths(env, func() (string, error) { return home, nil })
	if err != nil || len(paths) != 2 || paths[1].Path != shared {
		t.Fatal("incorrect source order")
	}
	c, source, path, err := Resolve(env, paths...)
	if err != nil || source != "file" || path != shared || c != (File{APIKey: "shared-key", Token: "shared-token", Scope: "read"}) {
		t.Fatal("shared fallback failed or read unrelated fields")
	}
	if err := Save(paths[0].Path, synthetic()); err != nil {
		t.Fatal(err)
	}
	c, _, path, err = Resolve(env, paths...)
	if err != nil || path != paths[0].Path || c != synthetic() {
		t.Fatal("shared store overrode dedicated file")
	}
	values["TRELLO_MCP_CREDENTIALS"] = filepath.Join(home, "override", "custom.secret")
	override := File{APIKey: "override-key", Token: "override-token"}
	if err := Save(values["TRELLO_MCP_CREDENTIALS"], override); err != nil {
		t.Fatal(err)
	}
	paths, err = Paths(env, func() (string, error) { return home, nil })
	if err != nil || len(paths) != 3 {
		t.Fatal("override source list failed")
	}
	c, _, path, err = Resolve(env, paths...)
	if err != nil || c != override || path != paths[0].Path {
		t.Fatal("override did not beat default")
	}
	values["TRELLO_API_KEY"], values["TRELLO_TOKEN"] = "env-key", "env-token"
	c, source, _, err = Resolve(env, paths...)
	if err != nil || source != "env" || c.Token != "env-token" {
		t.Fatal("environment did not win")
	}
	if _, err := Logout(paths[0].Path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(shared)
	if err != nil || string(after) != data {
		t.Fatal("auth operation modified shared store")
	}
}

func TestSharedRequiresSameFilePair(t *testing.T) {
	for _, data := range []string{"OTHER=synthetic-value\n", "TRELLO_API_KEY=shared-key\n", "TRELLO_TOKEN=shared-token\n", "TRELLO_TOKEN_SCOPE=read\n"} {
		home := credentialTestDir(t)
		shared := sharedFixture(t, home, data)
		c, source, _, err := Resolve(func(string) string { return "" }, Source{Path: shared, Shared: true})
		if err != nil || source != "none" || c != (File{}) {
			t.Fatal("incomplete shared pair counted as credentials")
		}
	}
}

func TestUnsafeSharedFallbackSkippedWithEarlierSource(t *testing.T) {
	home := credentialTestDir(t)
	shared := sharedFixture(t, home, "TRELLO_API_KEY=shared-key\nTRELLO_TOKEN=shared-token\n")
	target := filepath.Join(home, "target.secret")
	if err := os.Rename(shared, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, shared); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	dedicated := filepath.Join(home, ".secrets", ".trello-mcp.secret")
	if err := Save(dedicated, synthetic()); err != nil {
		t.Fatal(err)
	}
	env := func(string) string { return "" }
	if _, _, _, err := Resolve(env, Source{Path: dedicated}, Source{Path: shared, Shared: true}); err != nil {
		t.Fatal("earlier source did not bypass unsafe fallback")
	}
	if _, err := Logout(dedicated); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Resolve(env, Source{Path: dedicated}, Source{Path: shared, Shared: true}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal("unsafe final fallback did not return clear error")
	}
}

func TestCustomSharedBasenameIsNotSharedStore(t *testing.T) {
	home := credentialTestDir(t)
	override := filepath.Join(home, "custom", ".secrets.env")
	env := func(key string) string {
		if key == "TRELLO_MCP_CREDENTIALS" {
			return override
		}
		return ""
	}
	paths, err := Paths(env, func() (string, error) { return home, nil })
	if err != nil || len(paths) != 3 || paths[0].Shared || !paths[2].Shared || paths[2].Path != filepath.Join(home, ".secrets.env") {
		t.Fatal("a custom basename inherited shared-store privileges")
	}
}

func TestSharedStoreIdentity(t *testing.T) {
	home := credentialTestDir(t)
	shared := sharedFixture(t, home, "unrelated non-dotenv line\nTRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n")
	alias := filepath.Join(home, "alias.secret")
	if err := os.Link(shared, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	env := func(key string) string {
		if key == "TRELLO_MCP_CREDENTIALS" {
			return alias
		}
		return ""
	}
	paths, err := Paths(env, func() (string, error) { return home, nil })
	if err != nil || !paths[0].Shared || len(paths) != 2 {
		t.Fatalf("shared alias not recognized: %v", err)
	}
	c, source, path, err := Resolve(env, paths...)
	if err != nil || source != "file" || path != alias || c.Token != "synthetic-token" {
		t.Fatalf("shared alias failed to load: %v", err)
	}
	link := filepath.Join(home, "symlink.secret")
	if err := os.Symlink(shared, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if SamePath(link, shared) {
		t.Fatal("symlink inherited shared identity")
	}
}

func TestSamePathFallback(t *testing.T) {
	home := credentialTestDir(t)
	path := filepath.Join(home, "missing.secret")
	if !SamePath(path, filepath.Join(home, "unused", "..", "missing.secret")) {
		t.Fatal("cleaned missing paths differ")
	}
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if SamePath(path, filepath.Join(home, "MISSING.SECRET")) != want {
		t.Fatal("incorrect platform case comparison")
	}
	other := filepath.Join(home, "other.secret")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if SamePath(path, other) {
		t.Fatal("distinct files share identity")
	}
}
