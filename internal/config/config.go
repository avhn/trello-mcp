// Package config loads and validates the server's environment configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config contains startup settings. Credentials are never persisted or logged.
type Config struct {
	APIKey            string
	Token             string
	BoardID           string
	AllowedWorkspaces []string
	Tools             []string
	ReadOnly          bool
	StatePath         string
}

// Load reads the environment once and rejects missing credentials or malformed settings.
func Load() (Config, error) { return Parse(os.Getenv, os.UserConfigDir) }

// Parse permits isolated environment tests without modifying the process environment.
func Parse(env func(string) string, configDir func() (string, error)) (Config, error) {
	c := Config{APIKey: env("TRELLO_API_KEY"), Token: env("TRELLO_TOKEN"), BoardID: env("TRELLO_BOARD_ID")}
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.Token) == "" {
		return c, fmt.Errorf("TRELLO_API_KEY and TRELLO_TOKEN are required")
	}
	var err error
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
	dir := env("XDG_CONFIG_HOME")
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return c, fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
		}
	} else {
		dir, err = configDir()
		if err != nil {
			return c, fmt.Errorf("resolve configuration directory: %w", err)
		}
	}
	c.StatePath = filepath.Join(dir, "trello-mcp", "state.json")
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
