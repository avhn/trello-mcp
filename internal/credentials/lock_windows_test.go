//go:build windows

package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWriteLockWindowsDeletePending(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), "store", "test.secret")
	root, err := openDir(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := root.Mkdir(writeLockName, 0700); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(filepath.Join(filepath.Dir(path), writeLockName))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.DELETE,
		windows.FILE_SHARE_DELETE|windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if handle != windows.InvalidHandle {
			_ = windows.CloseHandle(handle)
		}
	})
	// Classic FILE_DISPOSITION_INFO has one BOOLEAN byte, not a Win32 BOOL.
	disposition := struct{ DeleteFile byte }{DeleteFile: 1}
	if err := windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo,
		&disposition.DeleteFile, uint32(unsafe.Sizeof(disposition))); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir(writeLockName, 0700); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("delete-pending Mkdir did not yield permission denial: %v", err)
	}
	called := false
	timeout := 50 * time.Millisecond
	start := time.Now()
	err = withWriteLock(root, path, timeout, func() error {
		called = true
		return nil
	})
	elapsed := time.Since(start)
	if elapsed < timeout || elapsed > timeout+5*time.Second {
		t.Fatalf("permission retry elapsed %s, want [%s, %s]", elapsed, timeout, timeout+5*time.Second)
	}
	t.Logf("permission retry elapsed %s with timeout %s", elapsed, timeout)
	if err == nil || !strings.Contains(err.Error(), "check directory permissions") {
		t.Fatalf("missing permission guidance: %v", err)
	}
	for _, advice := range []string{"timed out", "PID", "manually remove", "never auto-stolen"} {
		if strings.Contains(err.Error(), advice) {
			t.Fatalf("permission denial reported stale-lock advice: %v", err)
		}
	}
	if called {
		t.Fatal("update ran despite failed acquisition")
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	handle = windows.InvalidHandle
	if err := withWriteLock(root, path, writeLockTimeout, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("update did not run after closing the pending-delete handle")
	}
	assertLockReleased(t, path)
}
