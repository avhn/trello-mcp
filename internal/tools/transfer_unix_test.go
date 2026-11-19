//go:build unix

package tools

import (
	"os"
	"syscall"
	"testing"
)

type ownerInfo struct {
	os.FileInfo
	uid uint32
}

func (i ownerInfo) Sys() any { return &syscall.Stat_t{Uid: i.uid} }

func TestDownloadOwner(t *testing.T) {
	uid := uint32(os.Geteuid())
	if err := checkDownloadOwner(ownerInfo{uid: uid}); err != nil {
		t.Fatal(err)
	}
	if err := checkDownloadOwner(ownerInfo{uid: uid + 1}); err == nil {
		t.Error("foreign owner accepted")
	}
}

func isPrivatePath(t *testing.T, path string, mode os.FileMode) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm() == mode
}

func makePublicDir(dir string) error { return os.Mkdir(dir, 0755) }
