// Package config loads and validates the server's environment configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avhn/trello-mcp/internal/credentials"
)

// Config contains startup settings. Credentials must never be logged.
type Config struct {
	APIKey            string
	Token             string
	CredentialSource  string
	CredentialScope   string
	BoardID           string
	AllowedWorkspaces []string
	Tools             []string
	ReadOnly          bool
	StatePath         string
	DownloadDir       string
	UploadDir         string
}

// HasCredentials reports whether the full credential pair is available.
func (c Config) HasCredentials() bool { return c.APIKey != "" && c.Token != "" }

// Load reads environment and optional saved credentials, rejecting malformed settings.
func Load() (Config, error) { return Parse(os.Getenv, os.UserConfigDir) }

// Parse permits isolated environment tests without modifying the process environment.
func Parse(env func(string) string, configDir func() (string, error)) (Config, error) {
	c := Config{BoardID: env("TRELLO_BOARD_ID")}
	dir, err := credentials.Directory(env, configDir)
	if err != nil {
		return c, err
	}
	saved, source, err := credentials.Resolve(env, filepath.Join(dir, "credentials.json"))
	if err != nil {
		return c, err
	}
	c.APIKey, c.Token = saved.APIKey, saved.Token
	c.CredentialSource, c.CredentialScope = source, saved.Scope
	c.ReadOnly = saved.Scope == "read"
	c.AllowedWorkspaces, err = split(env("TRELLO_ALLOWED_WORKSPACES"))
	if err != nil {
		return c, fmt.Errorf("TRELLO_ALLOWED_WORKSPACES: %w", err)
	}
	c.Tools, err = split(env("TRELLO_TOOLS"))
	if err != nil {
		return c, fmt.Errorf("TRELLO_TOOLS: %w", err)
	}
	if len(c.Tools) == 0 {
		c.Tools = []string{"all"}
	}
	switch env("TRELLO_READ_ONLY") {
	case "", "0":
	case "1":
		c.ReadOnly = true
	default:
		return c, fmt.Errorf("TRELLO_READ_ONLY must be 0 or 1")
	}
	c.StatePath = filepath.Join(dir, "state.json")
	c.DownloadDir = env("TRELLO_DOWNLOAD_DIR")
	if c.DownloadDir == "" {
		c.DownloadDir = filepath.Join(os.TempDir(), "trello-mcp")
	} else if !filepath.IsAbs(c.DownloadDir) {
		return c, fmt.Errorf("TRELLO_DOWNLOAD_DIR must be an absolute path")
	}
	c.UploadDir = env("TRELLO_UPLOAD_DIR")
	if c.UploadDir != "" && !filepath.IsAbs(c.UploadDir) {
		return c, fmt.Errorf("TRELLO_UPLOAD_DIR must be an absolute path")
	}
	return c, nil
}

// split rejects empty entries so a mistyped restriction cannot silently become unrestricted.
func split(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	values := strings.Split(s, ",")
	for i, v := range values {
		values[i] = strings.TrimSpace(v)
		if values[i] == "" {
			return nil, fmt.Errorf("empty comma-separated entry")
		}
	}
	return values, nil
}
