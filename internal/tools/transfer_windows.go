//go:build windows

package tools

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/avhn/trello-mcp/internal/trello"
	"golang.org/x/sys/windows"
)

const fileTransfersSupported = true

// Well-known principals that can always take ownership of local files.
const (
	sidLocalSystem    = "S-1-5-18"
	sidAdministrators = "S-1-5-32-544"
	sidCreatorOwner   = "S-1-3-0"
)

func privateDirError() error {
	return trello.PolicyDenied("download directory safety", "download directory must be owned by the current user and grant access only to that user, SYSTEM and Administrators; remove other users in Properties > Security or choose a private TRELLO_DOWNLOAD_DIR")
}

// makePrivateDir creates missing parents normally and the leaf with an explicit owner-only DACL.
// Existing directories are left unchanged; callers verify them before use.
func makePrivateDir(dir string) error {
	if _, err := os.Lstat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return privateDirError()
	}
	sd, err := ownerOnlyDescriptor(user, "OICI")
	if err != nil {
		return privateDirError()
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err = windows.CreateDirectory(path, &sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	return nil
}

// checkDownloadPrivate refuses reparse points and, once the root is pinned, checks the
// descriptor of the opened directory handle rather than a replaceable pathname.
func checkDownloadPrivate(root *os.Root, info os.FileInfo) error {
	if isReparsePoint(info) {
		return trello.PolicyDenied("download directory safety", "download directory must be a private directory, not a symlink or junction; choose a private TRELLO_DOWNLOAD_DIR")
	}
	if root == nil {
		return nil
	}
	dir, err := root.Open(".")
	if err != nil {
		return privateDirError()
	}
	defer func() { _ = dir.Close() }()
	sd, err := windows.GetSecurityInfo(windows.Handle(dir.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return privateDirError()
	}
	if !privateDescriptor(sd) {
		return privateDirError()
	}
	return nil
}

// privateDescriptor accepts only current-user ownership and allow ACEs for the user and
// principals that already control every local file. Unknown ACE types fail closed.
func privateDescriptor(sd *windows.SECURITY_DESCRIPTOR) bool {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !windows.EqualSid(owner, user.User.Sid) {
		return false
	}
	dacl, _, err := sd.DACL()
	// A nil DACL grants everyone full access.
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		return false
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, i, &ace); err != nil || ace == nil {
			return false
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if windows.EqualSid(sid, user.User.Sid) {
			continue
		}
		switch sid.String() {
		case sidLocalSystem, sidAdministrators, sidCreatorOwner:
			continue
		}
		return false
	}
	return true
}

func isReparsePoint(info os.FileInfo) bool {
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return true
	}
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attrs.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// ownerOnlyDescriptor sets the owner explicitly so an elevated token does not default it
// to Administrators, and grants access only to the current user.
func ownerOnlyDescriptor(user *windows.Tokenuser, inherit string) (*windows.SECURITY_DESCRIPTOR, error) {
	sid := user.User.Sid.String()
	return windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;" + inherit + ";FA;;;" + sid + ")")
}

// createDownloadFile creates the file exclusively relative to the pinned root handle with an
// explicit owner-only descriptor, independent of inherited ACEs or the token default DACL.
func createDownloadFile(root *os.Root, name string) (*os.File, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, privateDirError()
	}
	sd, err := ownerOnlyDescriptor(user, "")
	if err != nil {
		return nil, privateDirError()
	}
	relative, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := windows.OBJECT_ATTRIBUTES{
		Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: windows.Handle(dir.Fd()),
		ObjectName: relative, Attributes: windows.OBJ_DONT_REPARSE, SecurityDescriptor: sd,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_WRITE, &oa, &status, nil,
		windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	// An existing link is an occupied name: it is never followed, and the caller tries the next suffix.
	if errors.Is(err, windows.STATUS_OBJECT_NAME_COLLISION) || errors.Is(err, windows.STATUS_REPARSE_POINT_ENCOUNTERED) {
		return nil, &os.PathError{Op: "create", Path: name, Err: os.ErrExist}
	}
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: name, Err: err}
	}
	return os.NewFile(uintptr(handle), filepath.Join(root.Name(), name)), nil
}

// openRoot holds a no-reparse handle that denies delete sharing, so the leaf cannot be
// renamed, deleted or swapped for a link (including one to a network share) before
// os.OpenRoot resolves it. FILE_LIST_DIRECTORY matters: attribute-only handles are exempt
// from sharing checks. Ancestors are trusted configuration. Identities are handle-based.
func openRoot(dir string) (*os.Root, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	pin := os.NewFile(uintptr(h), dir)
	defer func() { _ = pin.Close() }()
	pinned, err := pin.Stat()
	if err != nil || !pinned.IsDir() || isReparsePoint(pinned) {
		return nil, trello.PolicyDenied("directory safety", "directory must be a real directory, not a symlink or junction")
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	if opened, statErr := r.Stat("."); statErr != nil || !os.SameFile(pinned, opened) {
		_ = r.Close()
		return nil, trello.PolicyDenied("directory safety", "directory changed while opening; retry with a stable directory")
	}
	return r, nil
}

// singleLink refuses hard-linked files, whose other names may lie outside the staging root.
func singleLink(f *os.File) bool {
	var info windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info) == nil && info.NumberOfLinks == 1
}
