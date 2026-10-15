//go:build unix

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openUpload pins the configured root and each child directory without following links.
func openUpload(root, path string, info os.FileInfo) (*os.File, error) {
	path = filepath.Clean(path)
	if !filepath.IsLocal(path) || path == "." {
		return nil, fmt.Errorf("upload path must name a file inside TRELLO_UPLOAD_DIR")
	}
	dir, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open TRELLO_UPLOAD_DIR without symlinks: %w", err)
	}
	defer func() { _ = unix.Close(dir) }()
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(dir, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return nil, fmt.Errorf("open upload directory without symlinks: %w", openErr)
		}
		_ = unix.Close(dir)
		dir = next
	}
	// Nonblocking open prevents a substituted FIFO from hanging before fstat can reject it.
	fd, err := unix.Openat(dir, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open upload file without symlinks: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, fmt.Errorf("upload file changed while opening")
	}
	if err = unix.SetNonblock(fd, false); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("set upload file blocking mode: %w", err)
	}
	return f, nil
}
