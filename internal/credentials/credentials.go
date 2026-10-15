// Package credentials stores local Trello credentials without exposing their contents in errors.
package credentials

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Hint is shared by initialize, health and upstream authentication errors.
const Hint = "Trello credentials not configured: run `trello-mcp auth` in a terminal, or set TRELLO_API_KEY and TRELLO_TOKEN."

// File is the private on-disk credential record. Never log or format it.
type File struct {
	APIKey  string    `json:"apiKey"`
	Token   string    `json:"token"`
	Scope   string    `json:"scope"`
	SavedAt time.Time `json:"savedAt"`
}

// Directory honors an explicit XDG root on every platform, including macOS.
func Directory(env func(string) string, configDir func() (string, error)) (string, error) {
	dir := env("XDG_CONFIG_HOME")
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
		}
	} else {
		var err error
		dir, err = configDir()
		if err != nil {
			return "", fmt.Errorf("resolve configuration directory")
		}
	}
	return filepath.Join(dir, "trello-mcp"), nil
}

// Resolve uses an environment pair in preference to the credential file.
func Resolve(env func(string) string, path string) (File, string, error) {
	key, token := env("TRELLO_API_KEY"), env("TRELLO_TOKEN")
	if key != "" || token != "" {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(token) == "" {
			return File{}, "env", fmt.Errorf("TRELLO_API_KEY and TRELLO_TOKEN must be set together")
		}
		return File{APIKey: key, Token: token}, "env", nil
	}
	c, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, "none", nil
	}
	return c, "file", err
}

// openDir pins a private application directory and refuses a symlink at that boundary.
func openDir(path string, create bool) (*os.Root, error) {
	dir := filepath.Dir(path)
	if create {
		if err := makePrivateDir(dir); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("inspect credentials directory")
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("credentials directory must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open credentials directory")
	}
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		_ = root.Close()
		return nil, fmt.Errorf("credentials directory changed while opening")
	}
	directory, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("open credentials directory for privacy verification")
	}
	err = checkPrivateFile(directory, pinned, true)
	_ = directory.Close()
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func regular(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("inspect credentials file")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials file must be a regular file, not a symlink")
	}
	return info, nil
}

// Load checks ownership and permissions before reading a bounded regular file.
func Load(path string) (File, error) {
	var c File
	root, err := openDir(path, false)
	if err != nil {
		return c, err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	info, err := regular(root, name)
	if err != nil {
		return c, err
	}
	file, err := root.OpenFile(name, readFlags, 0)
	if err != nil {
		return c, fmt.Errorf("open credentials file")
	}
	defer func() { _ = file.Close() }()
	pinned, err := file.Stat()
	if err != nil || !os.SameFile(info, pinned) || !pinned.Mode().IsRegular() {
		return c, fmt.Errorf("credentials file changed while opening")
	}
	if err = checkPrivateFile(file, pinned, false); err != nil {
		return c, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(data) > 16384 {
		return c, fmt.Errorf("read credentials file: invalid size or unreadable file; rerun trello-mcp auth")
	}
	if err = json.Unmarshal(data, &c); err != nil || !valid(c) {
		return File{}, fmt.Errorf("invalid credentials file; rerun trello-mcp auth")
	}
	return c, nil
}

func valid(c File) bool {
	return strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.Token) != "" && (c.Scope == "read" || c.Scope == "read,write")
}

// Save uses an exclusive private temporary file and an atomic rename within a pinned directory.
func Save(path string, c File) error {
	if !valid(c) {
		return fmt.Errorf("invalid credentials; nothing saved")
	}
	root, err := openDir(path, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	if _, err = regular(root, name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp := ".credentials-" + rand.Text()
	file, err := createPrivateFile(root, tmp)
	if err != nil {
		return fmt.Errorf("create credentials temporary file")
	}
	defer func() { _ = root.Remove(tmp) }()
	data, err := json.Marshal(c)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("write credentials file")
	}
	if _, err = regular(root, name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = root.Rename(tmp, name); err != nil {
		return fmt.Errorf("replace credentials file")
	}
	return nil
}

// Logout removes only the credential file, leaving local selections unchanged.
func Logout(path string) error {
	root, err := openDir(path, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	if _, err = regular(root, name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err = root.Remove(name); err != nil {
		return fmt.Errorf("delete credentials file")
	}
	return nil
}
