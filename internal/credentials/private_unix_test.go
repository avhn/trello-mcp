//go:build unix

package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPrivatePermissions(t *testing.T) {
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
			func() error { return Logout(path) },
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
