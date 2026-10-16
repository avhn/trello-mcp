// Package credentials stores local Trello credentials without exposing their contents in errors.
package credentials

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Hint is shared by initialize and health when credentials are not configured.
const Hint = "Trello credentials not configured. For CLI installs, the human must run the installer's quoted absolute-path auth command in a private terminal (not through an agent). For Claude Desktop extensions, the human must enter credentials in extension settings. Then use the trello server's Reconnect action in /mcp or fully quit/restart the MCP client; restart the Desktop extension or fully quit/reopen Desktop. Never share keys, tokens or authorization URLs in chat or agent-captured commands. Only server_health is exposed until a fresh configured server starts."

// File is the private on-disk credential record. Never log or format it.
type File struct {
	APIKey string
	Token  string
	Scope  string
}

// Path resolves the dedicated save target, never the shared top-level store.
func Path(env func(string) string, homeDir func() (string, error)) (string, error) {
	if path := env("TRELLO_MCP_CREDENTIALS"); path != "" {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("TRELLO_MCP_CREDENTIALS must be an absolute path")
		}
		return path, nil
	}
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory")
	}
	return filepath.Join(home, ".secrets", ".trello-mcp.secret"), nil
}

// Directory honors an explicit XDG root for noncredential state on every platform.
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

// Source identifies a file and whether it is the read-only shared fallback.
// The dedicated default directory requires owner-only access on Unix.
type Source struct {
	Path             string
	Shared           bool
	privateDirectory bool
}

// Paths lists the override, dedicated default and shared top-level store, in priority order.
// Environment credentials skip file resolution; Resolve validates their complete pair.
func Paths(env func(string) string, homeDir func() (string, error)) ([]Source, error) {
	if env("TRELLO_API_KEY") != "" || env("TRELLO_TOKEN") != "" {
		return nil, nil
	}
	home, err := homeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory")
	}
	target, err := Path(env, func() (string, error) { return home, nil })
	if err != nil {
		return nil, err
	}
	shared := filepath.Join(home, ".secrets.env")
	dedicated := filepath.Join(home, ".secrets", ".trello-mcp.secret")
	paths := []Source{{Path: target, Shared: SamePath(target, shared), privateDirectory: SamePath(filepath.Dir(target), filepath.Dir(dedicated))}}
	if !SamePath(target, dedicated) {
		paths = append(paths, Source{Path: dedicated, privateDirectory: true})
	}
	if !SamePath(target, shared) {
		paths = append(paths, Source{Path: shared, Shared: true})
	}
	return paths, nil
}

// SamePath compares existing filesystem identities without following final symlinks.
// Missing paths use cleaned spellings, case-insensitive on Windows and macOS.
func SamePath(a, b string) bool {
	left, leftErr := os.Lstat(a)
	right, rightErr := os.Lstat(b)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(left, right)
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Resolve uses the environment pair, then files in priority order. It never merges files.
// The returned path identifies the selected file without exposing credential values.
func Resolve(env func(string) string, paths ...Source) (File, string, string, error) {
	key, token := env("TRELLO_API_KEY"), env("TRELLO_TOKEN")
	if key != "" || token != "" {
		if key == "" || token == "" {
			return File{}, "env", "", fmt.Errorf("TRELLO_API_KEY and TRELLO_TOKEN must be set together")
		}
		if !ValidValue(key) || !ValidValue(token) {
			return File{}, "env", "", fmt.Errorf("invalid environment credentials")
		}
		return File{APIKey: key, Token: token}, "env", "", nil
	}
	for _, source := range paths {
		path := source.Path
		c, err := loadSource(source)
		if errors.Is(err, os.ErrNotExist) || (err == nil && c == (File{})) {
			continue
		}
		if err != nil {
			return File{}, "file", path, fmt.Errorf("credentials file %s: %w", path, err)
		}
		return c, "file", path, nil
	}
	return File{}, "none", "", nil
}

// openDir pins the credential directory and verifies its ownership and permissions.
func openDir(path string, create bool) (*os.Root, error) {
	return openDirectory(path, create, directoryPolicy(Source{Path: path}))
}

type directoryRule int

const (
	customDirectory directoryRule = iota
	privateDirectory
	sharedDirectory
)

// directoryPolicy keeps the default store private, permits nonwritable custom
// parents, and preserves the shared home store's Windows DACL exception.
func directoryPolicy(source Source) directoryRule {
	if source.Shared {
		return sharedDirectory
	}
	if source.privateDirectory {
		return privateDirectory
	}
	if home, err := os.UserHomeDir(); err == nil && SamePath(filepath.Dir(source.Path), filepath.Join(home, ".secrets")) {
		return privateDirectory
	}
	return customDirectory
}

func openDirectory(path string, create bool, rule directoryRule) (*os.Root, error) {
	dir := filepath.Dir(path)
	if err := noSymlinkParents(dir); err != nil {
		return nil, err
	}
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
	err = checkDirectory(directory, pinned, rule, create)
	_ = directory.Close()
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

// noSymlinkParents rejects symlinks in every existing directory component before creation or access.
func noSymlinkParents(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve credentials directory")
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect credentials parent directory")
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("credentials parent must be a directory, not a symlink; choose a real directory with TRELLO_MCP_CREDENTIALS")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
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

const maxSize = 16384

// readFile checks ownership and permissions before reading a bounded regular file.
func readFile(root *os.Root, name string) ([]byte, error) {
	info, err := regular(root, name)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, readFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("open credentials file")
	}
	defer func() { _ = file.Close() }()
	pinned, err := file.Stat()
	if err != nil || !os.SameFile(info, pinned) || !pinned.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials file changed while opening")
	}
	if err = checkPrivateFile(file, pinned, false); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil || len(data) > maxSize {
		return nil, fmt.Errorf("read credentials file: invalid size or unreadable file; rerun the installer's absolute-path auth command")
	}
	return data, nil
}

// Load accepts unquoted dotenv credentials, without interpreting unrelated lines.
func Load(path string) (File, error) { return load(path, false) }

func load(path string, shared bool) (File, error) {
	return loadSource(Source{Path: path, Shared: shared})
}

func loadSource(source Source) (File, error) {
	path, shared := source.Path, source.Shared
	// The optional shared store must not impose its directory policy when absent.
	// Lstat leaves existing symlinks for the normal strict checks below.
	if shared {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return File{}, os.ErrNotExist
		} else if err != nil {
			return File{}, fmt.Errorf("inspect credentials file")
		}
	}
	root, err := openDirectory(path, false, directoryPolicy(source))
	if err != nil {
		return File{}, err
	}
	defer func() { _ = root.Close() }()
	data, err := readFile(root, filepath.Base(path))
	if err != nil {
		return File{}, err
	}
	var c File
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if shared && !managed(key) {
			continue
		}
		if !ok || (managed(strings.TrimSpace(key)) && !managed(key)) {
			return File{}, fmt.Errorf("invalid credentials file; use unquoted KEY=VALUE lines and rerun the installer's absolute-path auth command")
		}
		if !managed(key) {
			continue
		}
		if seen[key] || !ValidValue(value) {
			return File{}, fmt.Errorf("invalid credentials file; use unquoted KEY=VALUE lines and rerun the installer's absolute-path auth command")
		}
		seen[key] = true
		switch key {
		case "TRELLO_API_KEY":
			c.APIKey = value
		case "TRELLO_TOKEN":
			c.Token = value
		case "TRELLO_TOKEN_SCOPE":
			c.Scope = value
		}
	}
	if shared && (c.APIKey == "" || c.Token == "") {
		return File{}, nil
	}
	if c == (File{}) {
		return c, nil
	}
	if !valid(c) {
		return File{}, fmt.Errorf("invalid credentials file; API key and token must be set together with optional scope read or read,write")
	}
	return c, nil
}

func managed(key string) bool {
	return key == "TRELLO_API_KEY" || key == "TRELLO_TOKEN" || key == "TRELLO_TOKEN_SCOPE"
}

// ValidValue reports whether a value is safe in the unquoted dotenv format.
func ValidValue(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	last := len(value) - 1
	return last == 0 || value[0] != value[last] || (value[0] != '\'' && value[0] != '"')
}

func valid(c File) bool {
	return ValidValue(c.APIKey) && ValidValue(c.Token) && (c.Scope == "" || c.Scope == "read" || c.Scope == "read,write")
}

// rewrite changes only managed lines, leaving all other bytes and their order intact.
func rewrite(data []byte, c *File) []byte {
	values := map[string]string{}
	if c != nil {
		values = map[string]string{"TRELLO_API_KEY": c.APIKey, "TRELLO_TOKEN": c.Token, "TRELLO_TOKEN_SCOPE": c.Scope}
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(string(data), "\n") {
		key, _, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
		if !ok || !managed(key) {
			out.WriteString(line)
			continue
		}
		if value := values[key]; value != "" {
			out.WriteString(key + "=" + value + "\n")
			delete(values, key)
		}
	}
	var missing strings.Builder
	for _, key := range []string{"TRELLO_API_KEY", "TRELLO_TOKEN", "TRELLO_TOKEN_SCOPE"} {
		if value := values[key]; value != "" {
			missing.WriteString(key + "=" + value + "\n")
		}
	}
	// Prepend missing keys when appending would alter an unrelated unterminated line.
	if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
		return []byte(missing.String() + out.String())
	}
	return []byte(out.String() + missing.String())
}

// replace uses an exclusive private temporary file and an atomic rename in the pinned directory.
func replace(root *os.Root, name string, data []byte) error {
	if len(data) > maxSize {
		return fmt.Errorf("credentials file exceeds size limit; nothing saved")
	}
	tmp := ".credentials-" + rand.Text()
	file, err := createPrivateFile(root, tmp)
	if err != nil {
		return fmt.Errorf("create credentials temporary file")
	}
	defer func() { _ = root.Remove(tmp) }()
	_, err = file.Write(data)
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

// Save preserves unrelated dotenv entries and comments under the shared write lock.
func Save(path string, c File) error {
	return save(path, c, writeLockTimeout)
}

func save(path string, c File, timeout time.Duration) error {
	if !valid(c) {
		return fmt.Errorf("invalid credentials; nothing saved")
	}
	root, err := openDir(path, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return withWriteLock(root, path, timeout, func() error {
		name := filepath.Base(path)
		data, err := readFile(root, name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return replace(root, name, rewrite(data, &c))
	})
}

// Logout removes only managed keys; comments, blank lines and unrelated entries remain.
// It reports whether any key was removed.
func Logout(path string) (bool, error) {
	return logout(path, writeLockTimeout)
}

func logout(path string, timeout time.Duration) (bool, error) {
	root, err := openDir(path, false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	var removed bool
	err = withWriteLock(root, path, timeout, func() error {
		name := filepath.Base(path)
		data, err := readFile(root, name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		removed, err = removeManaged(root, name, data)
		return err
	})
	return removed, err
}

// removeManaged checks again before deleting, so a changed file is rewritten
// from its latest contents rather than removing unrelated entries. The caller
// holds the write lock; writers bypassing that protocol cannot be serialized.
func removeManaged(root *os.Root, name string, original []byte) (bool, error) {
	current, err := readFile(root, name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	data := rewrite(current, nil)
	if bytes.Equal(data, current) {
		return false, nil
	}
	if len(data) != 0 || !bytes.Equal(current, original) {
		err = replace(root, name, data)
		return err == nil, err
	}
	if err = root.Remove(name); err != nil {
		return false, fmt.Errorf("delete credentials file")
	}
	return true, nil
}
