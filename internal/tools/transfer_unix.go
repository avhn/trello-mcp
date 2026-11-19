//go:build unix

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/avhn/trello-mcp/internal/trello"
)

const fileTransfersSupported = true

// makePrivateDir creates missing directories owner-only; existing ones are checked separately.
func makePrivateDir(dir string) error { return os.MkdirAll(dir, 0700) }

// checkDownloadPrivate requires owner-only permissions and current-user ownership.
func checkDownloadPrivate(_ *os.Root, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return trello.PolicyDenied("download directory safety", "download directory must be a private directory, not a symlink; choose a private TRELLO_DOWNLOAD_DIR")
	}
	return checkDownloadOwner(info)
}

func checkDownloadOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return trello.PolicyDenied("download directory safety", "download directory must be owned by the current user; choose a private TRELLO_DOWNLOAD_DIR")
	}
	return nil
}

// inspectUpload reports the staged entry without following a final symlink.
func inspectUpload(root, rel string) (os.FileInfo, error) {
	info, err := os.Lstat(filepath.Join(root, rel))
	if err != nil {
		return nil, fmt.Errorf("inspect upload file: %w", err)
	}
	return info, nil
}

// createDownloadFile creates an owner-only file exclusively inside the pinned root.
func createDownloadFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

// openRoot pins a directory; callers compare its identity with their earlier Lstat.
func openRoot(dir string) (*os.Root, error) { return os.OpenRoot(dir) }
