//go:build unix

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openUpload pins each directory without following links, then verifies the opened file.
func openUpload(path string, info os.FileInfo) (*os.File, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("upload path must be absolute")
	}
	dir, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dir) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
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
