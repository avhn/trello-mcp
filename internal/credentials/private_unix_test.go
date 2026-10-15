//go:build unix

package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCustomPrivatePermissions(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0644, 0640, 0604, 0620} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Errorf("mode %o accepted: %v", mode, err)
		}
	}
	// Auth must not read an unsafe file even when preserving its unrelated entries.
	if err := Save(path, synthetic()); err == nil {
		t.Fatal("save read a group/world accessible file")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0700, 0750, 0755, 0770, 0702} {
		if err := os.Chmod(filepath.Dir(path), mode); err != nil {
			t.Fatal(err)
		}
		for _, operation := range []func() error{
			func() error { _, err := Load(path); return err },
			func() error { return Save(path, synthetic()) },
			func() error { _, err := Logout(path); return err },
		} {
			err := operation()
			if mode&0022 != 0 {
				if err == nil || !strings.Contains(err.Error(), "chmod go-w") {
					t.Errorf("writable directory %o accepted: %v", mode, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			// Logout may remove the file; save it again for the next operation.
			if mode&0022 == 0 {
				if err := Save(path, synthetic()); err != nil {
					t.Fatal(err)
				}
			}
			info, err := os.Stat(filepath.Dir(path))
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("existing directory mode changed")
			}
		}
	}
}

type ownerInfo struct{ os.FileInfo }

func (ownerInfo) Sys() any { return &syscall.Stat_t{Uid: uint32(os.Geteuid()) + 1} }

func TestWrongOwner(t *testing.T) {
	info, err := os.Stat(credentialTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = checkDirectory(nil, ownerInfo{info}, privateDirectory, true); err == nil {
		t.Error("auth accepted a foreign owner before directory repair")
	}
	for _, directory := range []bool{false, true} {
		if err = checkPrivate(ownerInfo{info}, directory); err == nil || !strings.Contains(err.Error(), "owned by the current user") {
			t.Errorf("foreign owner accepted: %v", err)
		}
	}
}

func TestFIFORefusal(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), ".trello-mcp.secret")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("FIFO accepted")
	}
}

func TestSharedFilePermissions(t *testing.T) {
	home := credentialTestDir(t)
	path := sharedFixture(t, home, "TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n")
	for _, mode := range []os.FileMode{0640, 0604} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := Resolve(func(string) string { return "" }, Source{Path: path, Shared: true}); err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Fatal("unsafe shared file accepted")
		}
	}
}

func TestCustomSharedBasenameRequiresSafeDirectory(t *testing.T) {
	home := credentialTestDir(t)
	path := filepath.Join(home, "custom", ".secrets.env")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0777); err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		if key == "TRELLO_MCP_CREDENTIALS" {
			return path
		}
		return ""
	}
	paths, err := Paths(env, func() (string, error) { return home, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Resolve(env, paths...); err == nil || !strings.Contains(err.Error(), "chmod go-w") {
		t.Fatal("custom .secrets.env bypassed directory privacy")
	}
	if _, err := Load(path); err == nil {
		t.Fatal("load inferred shared-store privileges from a basename")
	}
}

func TestDefaultDirectoryPermissions(t *testing.T) {
	home := credentialTestDir(t)
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".secrets", ".trello-mcp.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0750, 0755, 0770, 0777} {
		if err := os.Chmod(filepath.Dir(path), mode); err != nil {
			t.Fatal(err)
		}
		for _, operation := range []func() error{
			func() error { _, err := Load(path); return err },
			func() error { _, err := Logout(path); return err },
		} {
			if err := operation(); err == nil || !strings.Contains(err.Error(), "chmod 700 ~/.secrets") {
				t.Errorf("default directory %o accepted: %v", mode, err)
			}
		}
		if err := Save(path, synthetic()); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Dir(path))
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("auth did not repair default directory mode %o", mode)
		}
	}
}

func TestPathsDefaultDirectoryPermissions(t *testing.T) {
	// Paths must honor its injected home even when it differs from os.UserHomeDir.
	home := credentialTestDir(t)
	path := filepath.Join(home, ".secrets", ".trello-mcp.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	env := func(string) string { return "" }
	paths, err := Paths(env, func() (string, error) { return home, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Resolve(env, paths...); err == nil || !strings.Contains(err.Error(), "chmod 700 ~/.secrets") {
		t.Fatalf("default source directory was not private: %v", err)
	}
}

func TestSharedHomeDirectoryPermissions(t *testing.T) {
	home := credentialTestDir(t)
	path := sharedFixture(t, home, "TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n")
	for _, mode := range []os.FileMode{0755, 0775} {
		if err := os.Chmod(home, mode); err != nil {
			t.Fatal(err)
		}
		_, err := load(path, true)
		if mode == 0755 && err != nil {
			t.Fatal(err)
		}
		if mode == 0775 && (err == nil || !strings.Contains(err.Error(), "chmod go-w")) {
			t.Fatalf("writable shared home directory accepted: %v", err)
		}
	}
}

func TestMissingStoresInWritableHome(t *testing.T) {
	home := credentialTestDir(t)
	if err := os.Chmod(home, 0775); err != nil {
		t.Fatal(err)
	}
	env := func(string) string { return "" }
	paths, err := Paths(env, func() (string, error) { return home, nil })
	if err != nil {
		t.Fatal(err)
	}
	// Both the default directory and the optional shared file are absent.
	if _, err := loadSource(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("missing default store: %v", err)
	}
	if _, source, _, err := Resolve(env, paths...); err != nil || source != "none" {
		t.Fatalf("missing stores: source %q, error %v", source, err)
	}
	sharedFixture(t, home, "TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n")
	if _, _, _, err := Resolve(env, paths...); err == nil || !strings.Contains(err.Error(), "chmod go-w") {
		t.Fatalf("existing shared file in writable home accepted: %v", err)
	}
}
