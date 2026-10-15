//go:build windows

package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPrivateCreation(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Dir(path), path} {
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		err = checkPrivateFile(file, nil, false)
		_ = file.Close()
		if err != nil {
			t.Fatalf("created object is not private: %v", err)
		}
	}
}

func TestWindowsRejectsOtherPrincipalsOnLoad(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, principal := range []string{"WD", "BU", "AU", "BA", "SY"} {
		for _, directory := range []bool{false, true} {
			t.Run(principal+map[bool]string{false: "/file", true: "/directory"}[directory], func(t *testing.T) {
				path := filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret")
				if err := Save(path, synthetic()); err != nil {
					t.Fatal(err)
				}
				target := path
				if directory {
					target = filepath.Dir(path)
				}
				sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;" + principal + ")")
				if err != nil {
					t.Fatal(err)
				}
				dacl, _, err := sd.DACL()
				if err != nil {
					t.Fatal(err)
				}
				if err = windows.SetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
					t.Fatal(err)
				}
				if _, err = Load(path); err == nil || !strings.Contains(err.Error(), "Properties > Security") {
					t.Fatalf("public ACL was not rejected actionably: %v", err)
				}
			})
		}
	}
}

func TestWindowsRejectsUnsafeDescriptors(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, sddl := range []string{
		"O:BAD:P(A;;FA;;;" + sid + ")",         // Wrong owner.
		"O:" + sid + "D:(A;;FA;;;" + sid + ")", // Inheritance is not protected.
		"O:" + sid + "D:PNO_ACCESS_CONTROL",    // Null DACL grants everyone access.
		"O:" + sid + "D:P",                     // No usable grants.
	} {
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		if err = checkDescriptor(sd); err == nil {
			t.Error("unsafe owner or DACL accepted")
		}
	}
}

func TestWindowsExistingDirectoryUnchanged(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret")
	dir := filepath.Dir(path)
	if err := makePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	descriptor := func() string {
		file, err := os.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		return sd.String()
	}
	before := descriptor()
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	if descriptor() != before {
		t.Fatal("auth changed an existing directory DACL or owner")
	}
}

func TestWindowsSharedFileDACL(t *testing.T) {
	home := credentialTestDir(t)
	path := sharedFixture(t, home, "TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n")
	if _, err := load(path, true); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	dedicated := filepath.Join(home, ".secrets", ".trello-mcp.secret")
	if err := Save(dedicated, synthetic()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Resolve(func(string) string { return "" }, Source{Path: dedicated}, Source{Path: path, Shared: true}); err != nil {
		t.Fatal("earlier credentials did not bypass shared DACL")
	}
	if _, _, _, err := Resolve(func(string) string { return "" }, Source{Path: path, Shared: true}); err == nil || !strings.Contains(err.Error(), "Properties > Security") {
		t.Fatal("unsafe shared DACL did not fail clearly")
	}
}
