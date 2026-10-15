package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func synthetic() File {
	return File{APIKey: "synthetic-key", Token: "synthetic-token", Scope: "read", SavedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
}

func TestSaveLoadLogout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trello-mcp", "credentials.json")
	for range 2 {
		if err := Save(path, synthetic()); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got != synthetic() {
			t.Fatal("credentials did not round trip")
		}
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != want {
				t.Errorf("incorrect private permissions, want %o", want)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files remained")
	}
	for range 2 {
		if err = Logout(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("logout did not remove file: %v", err)
	}
}

func TestSymlinkRefusal(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "trello-mcp", "credentials.json")
			if err := Save(path, synthetic()); err != nil {
				t.Fatal(err)
			}
			from, to := path, filepath.Join(base, "target.json")
			if directory {
				from, to = filepath.Dir(path), filepath.Join(base, "target")
			}
			if err := os.Rename(from, to); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(to, from); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := Save(path, synthetic()); err == nil {
				t.Error("save accepted symlink")
			}
			if _, err := Load(path); err == nil {
				t.Error("load accepted symlink")
			}
			if err := Logout(path); err == nil {
				t.Error("logout accepted symlink")
			}
			if _, err := os.Stat(to); err != nil {
				t.Error("symlink target was modified")
			}
		})
	}
}

func TestUnsafeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trello-mcp", "credentials.json")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"null", "{}", `{"apiKey":"synthetic-key","token":"synthetic-token","scope":"invalid"}`, strings.Repeat("synthetic-token", 2000)} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), "synthetic-key") {
			t.Errorf("unsafe file did not produce a safe error: %v", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("directory accepted as file")
	}
	if err := Save(path, synthetic()); err == nil {
		t.Error("directory replaced as file")
	}
}
