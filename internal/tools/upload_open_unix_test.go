//go:build unix

package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestUploadRejectsAncestorSymlink(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(base, "private")
	if err = os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(private, "settings.json"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "reports")
	if err = os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	data, err := readUpload(base, filepath.Join(link, "settings.json"))
	if err == nil || len(data) != 0 {
		t.Fatalf("ancestor symlink accepted: %q, %v", data, err)
	}
}

func TestUploadRejectsFileSubstitution(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink", "regular"} {
		t.Run(kind, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(base, "upload.txt")
			if err = os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Keep the checked inode alive so its number cannot be reused by the replacement.
			original := filepath.Join(base, "original.txt")
			if err = os.Rename(path, original); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "symlink":
				err = os.Symlink(original, path)
			case "regular":
				err = os.WriteFile(path, []byte("replacement"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				f, openErr := openUpload(base, "upload.txt", info)
				if f != nil {
					_ = f.Close()
				}
				result <- openErr
			}()
			select {
			case err = <-result:
				if err == nil {
					t.Fatal("substituted file accepted")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("opening substituted file blocked for 2 seconds")
			}
		})
	}
}
