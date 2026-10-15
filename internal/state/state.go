// Package state persists only active workspace and board identifiers.
package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Selection is a local default, never proof of authorization.
type Selection struct {
	BoardID     string `json:"boardId,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// Store serializes atomic updates for concurrent MCP sessions in this process.
type Store struct {
	mu        sync.Mutex
	path      string
	selection Selection
}

// Open loads an existing selection, failing closed on corrupt or unsafe state.
// An empty path keeps the selection in memory only.
func Open(ctx context.Context, path, boardOverride string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	if path == "" {
		s.selection = Selection{BoardID: boardOverride}
		return s, nil
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("state must be a regular file")
		}
		if info.Size() > 4096 {
			return nil, fmt.Errorf("state exceeds size limit")
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, fmt.Errorf("state must be a JSON object")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&s.selection); err != nil {
			return nil, fmt.Errorf("decode state: %w", err)
		}
		if err = dec.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("state contains trailing data")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if boardOverride != "" && boardOverride != s.selection.BoardID {
		// The saved workspace belongs to the previous board, not this override.
		s.selection = Selection{BoardID: boardOverride}
	}
	return s, nil
}

// Get returns a copy under the same lock used for persistence.
func (s *Store) Get() Selection { s.mu.Lock(); defer s.mu.Unlock(); return s.selection }

// Update persists a complete selection by private temporary file and atomic rename.
func (s *Store) Update(ctx context.Context, next Selection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.path == "" {
		s.selection = next
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state directory must not be a symlink")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return err
	}
	if info, err = os.Lstat(s.path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("state must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	data, err := json.Marshal(next)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.selection = next
	return nil
}
