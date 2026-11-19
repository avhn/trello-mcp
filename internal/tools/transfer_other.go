//go:build !unix && !windows

package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/avhn/trello-mcp/internal/trello"
)

const fileTransfersSupported = false

func makePrivateDir(string) error {
	return trello.PolicyDenied("file transfer platform safety", "local file transfers unavailable on this platform; supply base64 or use a Unix or Windows host")
}

func checkDownloadPrivate(*os.Root, os.FileInfo) error {
	return trello.PolicyDenied("download platform safety", "downloads unavailable on this platform because directory ownership cannot be verified; use a Unix or Windows host")
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
