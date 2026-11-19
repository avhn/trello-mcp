//go:build windows

package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/avhn/trello-mcp/internal/trello"
)

// openUploadRoot pins the configured root after refusing links and junctions at its path.
func openUploadRoot(root string) (*os.Root, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || isReparsePoint(rootInfo) {
		return nil, trello.PolicyDenied("upload file safety", "open TRELLO_UPLOAD_DIR without symlinks or junctions; stage a regular file under a real TRELLO_UPLOAD_DIR")
	}
	r, err := openRoot(root)
	if err != nil {
		return nil, trello.PolicyDenied("upload file safety", fmt.Sprintf("open TRELLO_UPLOAD_DIR: %s; stage a regular file under TRELLO_UPLOAD_DIR", err))
	}
	if opened, statErr := r.Stat("."); statErr != nil || !os.SameFile(rootInfo, opened) {
		_ = r.Close()
		return nil, trello.PolicyDenied("upload file safety", "TRELLO_UPLOAD_DIR changed while opening; retry with a stable directory")
	}
	return r, nil
}

// inspectUpload resolves the entry through the pinned root, so lookups cannot leave it
// and the returned identity comes from a handle rather than a replaceable pathname.
func inspectUpload(root, rel string) (os.FileInfo, error) {
	r, err := openUploadRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	info, err := r.Lstat(rel)
	if err != nil {
		return nil, fmt.Errorf("inspect upload file: %w", err)
	}
	return info, nil
}

// openUpload reopens the root, refuses a final link and requires the opened handle to be
// the regular file inspected by the caller.
func openUpload(root, path string, info os.FileInfo) (*os.File, error) {
	path = filepath.Clean(path)
	if !filepath.IsLocal(path) || path == "." {
		return nil, trello.PolicyDenied("TRELLO_UPLOAD_DIR", "upload path must name a file inside TRELLO_UPLOAD_DIR; use a file under the configured root")
	}
	r, err := openUploadRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	entry, err := r.Lstat(path)
	if err != nil || !entry.Mode().IsRegular() || isReparsePoint(entry) || !os.SameFile(info, entry) {
		return nil, trello.PolicyDenied("upload file safety", "upload must be a regular file, not a symlink or junction; stage a regular file under TRELLO_UPLOAD_DIR")
	}
	f, err := r.Open(path)
	if err != nil {
		return nil, trello.PolicyDenied("upload file safety", fmt.Sprintf("open upload file inside TRELLO_UPLOAD_DIR: %s; stage a regular file under TRELLO_UPLOAD_DIR", err))
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || isReparsePoint(opened) || !os.SameFile(entry, opened) {
		_ = f.Close()
		return nil, trello.PolicyDenied("upload file safety", "upload file changed while opening; retry with a stable regular file under TRELLO_UPLOAD_DIR")
	}
	if !singleLink(f) {
		_ = f.Close()
		return nil, hardLinkDenied()
	}
	return f, nil
}
