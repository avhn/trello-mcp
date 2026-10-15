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
	path := filepath.Join(t.TempDir(), "trello-mcp", "credentials.json")
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
	// Rerunning auth replaces a user-owned but overly permissive file safely.
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Errorf("public directory accepted: %v", err)
	}
}

type ownerInfo struct{ os.FileInfo }

func (ownerInfo) Sys() any { return &syscall.Stat_t{Uid: uint32(os.Geteuid()) + 1} }

func TestWrongOwner(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = checkPrivate(ownerInfo{info}, false); err == nil || !strings.Contains(err.Error(), "owned by the current user") {
		t.Errorf("foreign owner accepted: %v", err)
	}
}

func TestFIFORefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("FIFO accepted")
	}
}
