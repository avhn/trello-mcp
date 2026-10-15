//go:build windows

package credentials

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const readFlags = os.O_RDONLY

func privacyError() error {
	return fmt.Errorf("credentials require a protected owner-only Windows DACL and ownership by the current user; remove access for other users in Properties > Security on the trello-mcp config directory and credentials.json, then rerun trello-mcp auth")
}

func privateDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, privacyError()
	}
	// Set the owner explicitly, including when an elevated token defaults to Administrators.
	return windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")")
}

func makePrivateDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return fmt.Errorf("create credentials parent directory")
	}
	sd, err := privateDescriptor()
	if err != nil {
		return privacyError()
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return fmt.Errorf("invalid credentials directory path")
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err = windows.CreateDirectory(path, &sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return privacyError()
	}
	return nil
}

func createPrivateFile(root *os.Root, name string) (*os.File, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, privacyError()
	}
	defer func() { _ = dir.Close() }()
	sd, err := privateDescriptor()
	if err != nil {
		return nil, privacyError()
	}
	relative, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, privacyError()
	}
	// Create relative to the pinned directory, not its potentially replaced pathname.
	oa := windows.OBJECT_ATTRIBUTES{
		Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: windows.Handle(dir.Fd()),
		ObjectName: relative, Attributes: windows.OBJ_DONT_REPARSE, SecurityDescriptor: sd,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_WRITE|windows.READ_CONTROL, &oa, &status, nil,
		windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return nil, privacyError()
	}
	return os.NewFile(uintptr(handle), name), nil
}

func checkPrivateFile(file *os.File, _ os.FileInfo, _ bool) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return privacyError()
	}
	return checkDescriptor(sd)
}

func checkDescriptor(sd *windows.SECURITY_DESCRIPTOR) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return privacyError()
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !windows.EqualSid(owner, user.User.Sid) {
		return privacyError()
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return privacyError()
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		return privacyError()
	}
	// Fail closed for unfamiliar ACE types as well as grants to any other principal.
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, i, &ace); err != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return privacyError()
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !windows.EqualSid(sid, user.User.Sid) || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			return privacyError()
		}
	}
	return nil
}
