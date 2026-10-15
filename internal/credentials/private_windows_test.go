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
	path := filepath.Join(t.TempDir(), "trello-mcp", "credentials.json")
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
				path := filepath.Join(t.TempDir(), "trello-mcp", "credentials.json")
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
