//go:build windows

package tools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func isPrivatePath(t *testing.T, path string, _ os.FileMode) bool {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return privateDescriptor(sd)
}

// makePublicDir grants Builtin Users access, like a 0755 directory on Unix.
func makePublicDir(dir string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FR;;;BU)")
	if err != nil {
		return err
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func TestWindowsPrivateDescriptor(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, tc := range []struct {
		sddl string
		ok   bool
	}{
		{"O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")", true},
		{"O:" + sid + "D:(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", true},
		{"O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(D;;FA;;;WD)", true},
		{"O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;;FR;;;BU)", false},
		{"O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICIIO;FA;;;AU)", false},
		{"O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;;FR;;;WD)", false},
		{"O:BAD:P(A;OICI;FA;;;" + sid + ")", false},
		{"O:" + sid + "D:P", false},
	} {
		sd, err := windows.SecurityDescriptorFromString(tc.sddl)
		if err != nil {
			t.Fatal(err)
		}
		if got := privateDescriptor(sd); got != tc.ok {
			t.Errorf("%s: private %t; want %t", tc.sddl, got, tc.ok)
		}
	}
}

func TestWindowsDefaultDirectoryIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := makePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if !isPrivatePath(t, dir, 0) {
		t.Fatal("created directory is not private")
	}
	// Existing directories are not modified.
	if err := makePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsDownloadRejectsJunction(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := makePrivateDir(target); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %v %s", err, out)
	}
	if root, err := downloadRoot(link); err == nil {
		_ = root.Close()
		t.Fatal("junction download directory accepted")
	}
	if err := os.WriteFile(filepath.Join(target, "staged.txt"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readUpload(link, "staged.txt", false); err == nil {
		t.Fatal("junction upload root accepted")
	}
}

// TestWindowsDownloadFileIgnoresInheritance keeps files owner-only when the directory
// carries no inheritable ACE, so the token default DACL would otherwise apply.
func TestWindowsDownloadFileIgnoresInheritance(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	// makePrivateDir sets the owner explicitly; elevated tokens would otherwise default it
	// to Administrators. The DACL is then replaced with a non-inheritable one.
	dir := filepath.Join(t.TempDir(), "flat")
	if err = makePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	root, err := downloadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	f, err := createDownloadFile(root, "at1-file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !isPrivatePath(t, filepath.Join(dir, "at1-file.txt"), 0) {
		t.Fatal("download file not owner-only")
	}
	if _, err = createDownloadFile(root, "at1-file.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing name not reported as ErrExist: %v", err)
	}
}

// TestWindowsRootPinBlocksRename proves the share mode used by openRoot excludes renames
// and deletes while the pin is held.
func TestWindowsRootPinBlocksRename(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "pinned")
	if err := makePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(path, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(dir, filepath.Join(base, "moved")); err == nil {
		t.Fatal("pinned directory renamed")
	}
	if err = os.Remove(dir); err == nil {
		t.Fatal("pinned directory removed")
	}
	_ = windows.CloseHandle(h)
	if err = os.Rename(dir, filepath.Join(base, "moved")); err != nil {
		t.Fatalf("rename after release: %v", err)
	}
}
