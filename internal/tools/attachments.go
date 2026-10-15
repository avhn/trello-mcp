package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// attachmentFind rechecks card ownership after reading metadata and omits upload URLs.
func (s *Service) attachmentFind(ctx context.Context, a arguments) (any, error) {
	id := a.str("cardId")
	board, err := s.access.ObjectBoard(ctx, "cards", id, a.str("boardId"))
	if err != nil {
		return nil, err
	}
	var items []view.Attachment
	q := url.Values{"fields": {"id,name,fileName,mimeType,bytes,date,isUpload,url"}}
	if err = s.access.Client.Get(ctx, "/cards/"+id+"/attachments", q, &items); err != nil {
		return nil, err
	}
	if _, err = s.access.ObjectBoard(ctx, "cards", id, board); err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].IsUpload {
			items[i].URL = ""
		}
	}
	return view.Collection(items, ""), nil
}

// safeFileName discards path components and reduces names to a bounded portable alphabet.
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	var out strings.Builder
	for _, r := range name {
		if out.Len() >= 120 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
	}
	clean := strings.Trim(out.String(), ". ")
	if clean == "" {
		return "attachment"
	}
	return clean
}

func (s *Service) attachmentAdd(ctx context.Context, a arguments) (any, error) {
	u, err := url.Parse(a.str("url"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("url must be an HTTP(S) URL without credentials")
	}
	if _, err = s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPost, "/cards/"+a.str("cardId")+"/attachments", a.payload("url", "name"))
}

// readUpload confines paths to the configured root and reads only a verified regular-file descriptor.
func readUpload(root, path string) ([]byte, error) {
	if root == "" {
		return nil, trello.PolicyDenied("TRELLO_UPLOAD_DIR", "path uploads are disabled; set TRELLO_UPLOAD_DIR to a dedicated staging directory to enable them, or supply base64")
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("TRELLO_UPLOAD_DIR must be an absolute path")
	}
	if !filepath.IsAbs(path) {
		if !filepath.IsLocal(path) {
			return nil, trello.PolicyDenied("TRELLO_UPLOAD_DIR", "upload path must stay inside TRELLO_UPLOAD_DIR; use a file under the configured root")
		}
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return nil, trello.PolicyDenied("TRELLO_UPLOAD_DIR", "upload path must name a file inside TRELLO_UPLOAD_DIR; use a file under the configured root")
	}
	info, err := os.Lstat(filepath.Join(root, rel))
	if err != nil {
		return nil, fmt.Errorf("inspect upload file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > trello.MaxAttachmentBytes {
		return nil, trello.PolicyDenied("upload file safety", "upload must be a regular file at most 10 MiB, not a symlink; stage a regular file under TRELLO_UPLOAD_DIR")
	}
	f, err := openUpload(root, rel, info)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, trello.MaxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > trello.MaxAttachmentBytes {
		return nil, fmt.Errorf("attachment exceeds 10 MiB limit")
	}
	return data, nil
}

func (s *Service) attachmentUpload(ctx context.Context, a arguments) (any, error) {
	if a.has("path") == a.has("base64") {
		return nil, fmt.Errorf("supply exactly one of path or base64")
	}
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	var data []byte
	var err error
	name := a.str("fileName")
	if a.has("path") {
		data, err = readUpload(s.config.UploadDir, a.str("path"))
		if name == "" {
			name = filepath.Base(a.str("path"))
		}
	} else {
		if name == "" {
			return nil, fmt.Errorf("fileName is required with base64")
		}
		data, err = base64.StdEncoding.Strict().DecodeString(a.str("base64"))
		if err != nil {
			return nil, fmt.Errorf("invalid base64 attachment")
		}
	}
	if err != nil {
		return nil, err
	}
	if len(data) > trello.MaxAttachmentBytes {
		return nil, fmt.Errorf("attachment exceeds 10 MiB limit")
	}
	mimeType := a.str("mimeType")
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if _, _, err = mime.ParseMediaType(mimeType); err != nil || strings.ContainsAny(mimeType, "\r\n") {
		return nil, fmt.Errorf("invalid mimeType")
	}
	var out writeResult
	if err = s.access.Client.Upload(ctx, a.str("cardId"), a.str("name"), safeFileName(name), mimeType, data, &out); err != nil {
		return nil, err
	}
	if !trello.ValidID(out.ID) {
		return nil, fmt.Errorf("upload result id unverifiable; upload may have succeeded")
	}
	return out, nil
}

// downloadRoot pins the checked directory inode. Exclusive creates below this root cannot follow links.
func downloadRoot(dir string) (*os.Root, error) {
	// A trailing separator can make Lstat follow a directory symlink on Unix.
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("download directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, trello.PolicyDenied("download directory safety", "download directory must be a private directory, not a symlink; choose a private TRELLO_DOWNLOAD_DIR")
	}
	if err = checkDownloadOwner(info); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm()&0077 != 0 {
		_ = root.Close()
		return nil, trello.PolicyDenied("download directory safety", "download directory changed while opening; retry with a stable private TRELLO_DOWNLOAD_DIR")
	}
	if err = checkDownloadOwner(opened); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

// downloadFileName keeps the sanitized component within its 120-byte cap, including the suffix.
func downloadFileName(id, clean string, attempt int) string {
	if attempt == 0 {
		return id + "-" + clean
	}
	suffix := fmt.Sprintf("-%d", attempt)
	ext := filepath.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	// Unusually long extensions must also leave room for a stem and the suffix.
	if len(ext) > 119-len(suffix) {
		ext = ext[:119-len(suffix)]
	}
	if len(stem) > 120-len(ext)-len(suffix) {
		stem = stem[:120-len(ext)-len(suffix)]
	}
	return id + "-" + stem + suffix + ext
}

func (s *Service) attachmentDownload(ctx context.Context, a arguments) (any, error) {
	if !fileTransfersSupported {
		return nil, trello.PolicyDenied("download platform safety", "downloads unavailable on this platform because directory ownership cannot be verified; use a Unix host")
	}
	card, id := a.str("cardId"), a.str("attachmentId")
	board, err := s.access.ObjectBoard(ctx, "cards", card, a.str("boardId"))
	if err != nil {
		return nil, err
	}
	var attachment struct {
		ID       string `json:"id"`
		FileName string `json:"fileName"`
		IsUpload bool   `json:"isUpload"`
		Bytes    int64  `json:"bytes"`
	}
	q := url.Values{"fields": {"id,fileName,isUpload,bytes"}}
	if err = s.access.Client.Get(ctx, "/cards/"+card+"/attachments/"+id, q, &attachment); err != nil {
		return nil, err
	}
	if attachment.ID != id || !attachment.IsUpload || attachment.FileName == "" || len(attachment.FileName) > 1024 {
		return nil, trello.OwnershipDenied("uploaded attachment or filename")
	}
	if attachment.Bytes > trello.MaxAttachmentBytes {
		return nil, fmt.Errorf("attachment exceeds 10 MiB limit")
	}
	// The card-scoped attachment endpoint proves attachment membership; recheck the card after metadata.
	if _, err = s.access.ObjectBoard(ctx, "cards", card, board); err != nil {
		return nil, err
	}
	dir := s.downloadDir()
	root, err := downloadRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	clean := safeFileName(attachment.FileName)
	var name string
	var f *os.File
	const maxSuffix = 100
	for attempt := 0; attempt <= maxSuffix; attempt++ {
		name = downloadFileName(id, clean, attempt)
		f, err = root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create download exclusively: %w", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("create download exclusively: filename collisions exhausted suffixes 1-%d: %w", maxSuffix, err)
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = root.Remove(name)
		}
	}()
	n, mimeType, err := s.access.Client.Download(ctx, card, id, attachment.FileName, f)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	complete = true
	return struct {
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		MimeType string `json:"mimeType"`
	}{filepath.Join(dir, name), n, mimeType}, nil
}
