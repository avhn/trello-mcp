//go:build unix

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avhn/trello-mcp/internal/trello"
	"golang.org/x/sys/unix"
)

// openUpload pins the configured root and each child directory without following links.
func openUpload(root, path string, info os.FileInfo) (*os.File, error) {
	path = filepath.Clean(path)
	if !filepath.IsLocal(path) || path == "." {
		return nil, trello.PolicyDenied("TRELLO_UPLOAD_DIR", "upload path must name a file inside TRELLO_UPLOAD_DIR; use a file under the configured root")
	}
	dir, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, trello.PolicyDenied("upload file safety", fmt.Sprintf("open TRELLO_UPLOAD_DIR without symlinks: %s; stage a regular file without symlinks under TRELLO_UPLOAD_DIR", err))
	}
	defer func() { _ = unix.Close(dir) }()
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(dir, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return nil, trello.PolicyDenied("upload file safety", fmt.Sprintf("open upload directory without symlinks: %s; stage a regular file without symlinks under TRELLO_UPLOAD_DIR", openErr))
		}
		_ = unix.Close(dir)
		dir = next
	}
	// Nonblocking open prevents a substituted FIFO from hanging before fstat can reject it.
	fd, err := unix.Openat(dir, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, trello.PolicyDenied("upload file safety", fmt.Sprintf("open upload file without symlinks: %s; stage a regular file without symlinks under TRELLO_UPLOAD_DIR", err))
	}
	f := os.NewFile(uintptr(fd), path)
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, trello.PolicyDenied("upload file safety", "upload file changed while opening; retry with a stable regular file under TRELLO_UPLOAD_DIR")
	}
	if err = unix.SetNonblock(fd, false); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("set upload file blocking mode: %w", err)
	}
	return f, nil
}
