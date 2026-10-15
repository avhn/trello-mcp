// Package auth implements interactive credential setup outside the MCP transport.
package auth

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"

	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/trello"
)

// runner isolates terminal, environment, browser and HTTP dependencies for synthetic tests.
type runner struct {
	in          io.Reader
	out         io.Writer
	env         func(string) string
	configDir   func() (string, error)
	client      func(string, string) *trello.Client
	browser     func(string) error
	hiddenToken func(context.Context) (string, error)
}

// Run handles auth, auth status and auth logout without starting an MCP server.
func Run(ctx context.Context, args []string, in *os.File, out io.Writer) error {
	r := runner{in: in, out: out, env: os.Getenv, configDir: os.UserConfigDir, client: trello.New, browser: openBrowser}
	if term.IsTerminal(int(in.Fd())) {
		r.hiddenToken = func(ctx context.Context) (string, error) {
			fd := int(in.Fd())
			state, err := term.GetState(fd)
			if err != nil {
				return "", fmt.Errorf("read terminal state failed; nothing saved")
			}
			// Restore on cancellation as well as normal completion. The read goroutine
			// also restores the original state if it finishes after cancellation.
			defer func() { _ = term.Restore(fd, state) }()
			return readInput(ctx, func() (string, error) {
				defer func() { _ = term.Restore(fd, state) }()
				data, err := term.ReadPassword(fd)
				return string(data), err
			})
		}
	}
	return r.run(ctx, args)
}

func (r runner) run(ctx context.Context, args []string) error {
	dir, err := credentials.Directory(r.env, r.configDir)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "credentials.json")
	if len(args) > 0 && (args[0] == "status" || args[0] == "logout") {
		if len(args) != 1 {
			return fmt.Errorf("usage: trello-mcp auth [status|logout]")
		}
		if args[0] == "status" {
			return r.status(ctx, path)
		}
		if err = credentials.Logout(path); err != nil {
			return err
		}
		_, err = fmt.Fprintln(r.out, "Saved Trello credentials removed. Environment credentials, if set, still take precedence.")
		return err
	}
	return r.connect(ctx, args, path)
}

func (r runner) connect(ctx context.Context, args []string, path string) error {
	flags := flag.NewFlagSet("auth", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	readOnly := flags.Bool("read-only", false, "request read-only access")
	write := flags.Bool("write", false, "request read and write access")
	expiration := flags.String("expiration", "never", "1day, 30days or never")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || (*readOnly && *write) {
		return fmt.Errorf("usage: trello-mcp auth [--read-only|--write] [--expiration 1day|30days|never]")
	}
	if *expiration != "1day" && *expiration != "30days" && *expiration != "never" {
		return fmt.Errorf("expiration must be 1day, 30days or never")
	}
	if _, err := fmt.Fprintln(r.out, "Create a Trello API key manually at https://trello.com/power-ups/admin, then enter it here."); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(r.in, 8192)
	key, err := r.prompt(ctx, reader, "API key: ")
	if err != nil {
		return err
	}
	if key == "" || len(key) > 4096 {
		return fmt.Errorf("API key must be nonempty and at most 4096 bytes; nothing saved")
	}
	if !*readOnly && !*write {
		answer, promptErr := r.prompt(ctx, reader, "Allow write access? [Y/n] ")
		if promptErr != nil {
			return promptErr
		}
		switch strings.ToLower(answer) {
		case "", "y", "yes":
			*write = true
		case "n", "no":
		default:
			return fmt.Errorf("answer y or n; nothing saved")
		}
	}
	scope := "read"
	if *write {
		scope = "read,write"
	}
	q := url.Values{"expiration": {*expiration}, "name": {"trello-mcp"}, "scope": {scope}, "response_type": {"token"}, "key": {key}}
	authorize := "https://trello.com/1/authorize?" + q.Encode()
	// The required authorization link contains the key. Neither credential is printed separately.
	if _, err = fmt.Fprintln(r.out, "Open this authorization URL and approve access:\n"+authorize); err != nil {
		return err
	}
	if r.browser != nil {
		_ = r.browser(authorize)
	}
	var token string
	if r.hiddenToken != nil {
		if _, err = fmt.Fprint(r.out, "Token (hidden): "); err != nil {
			return err
		}
		token, err = r.hiddenToken(ctx)
		if _, printErr := fmt.Fprintln(r.out); printErr != nil {
			return printErr
		}
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("auth cancelled: %w", ctx.Err())
			}
			return fmt.Errorf("read token failed; nothing saved")
		}
		token = strings.TrimSpace(token)
	} else {
		token, err = r.prompt(ctx, reader, "Token: ")
		if err != nil {
			return err
		}
	}
	if key == "" || token == "" || len(key) > 4096 || len(token) > 4096 {
		return fmt.Errorf("API key and token must be nonempty and at most 4096 bytes; nothing saved")
	}
	client := r.client(key, token)
	username, granted, err := validate(ctx, client, token)
	if err != nil {
		return err
	}
	if scope == "read" && granted != "read" {
		return fmt.Errorf("token permits writes despite the read-only request; use the token from this authorization link; nothing saved")
	}
	if err = credentials.Save(path, credentials.File{APIKey: key, Token: token, Scope: granted, SavedAt: time.Now().UTC()}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Connected as %s. Granted scope: %s. Credentials saved. Restart your MCP client to load them.\n", username, granted)
	return err
}

// readInput lets cancellation interrupt a blocked pipe or terminal read. A buffered
// result allows the goroutine to finish even if cancellation wins; Run's caller
// owns the input and the auth command exits after restoring terminal state.
func readInput(ctx context.Context, read func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("auth cancelled: %w", err)
	}
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := read()
		done <- result{value, err}
	}()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("auth cancelled: %w", ctx.Err())
	case got := <-done:
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("auth cancelled: %w", err)
		}
		return got.value, got.err
	}
}

func (r runner) prompt(ctx context.Context, reader *bufio.Reader, label string) (string, error) {
	if _, err := fmt.Fprint(r.out, label); err != nil {
		return "", err
	}
	return readInput(ctx, func() (string, error) {
		line, err := reader.ReadSlice('\n')
		if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
			return "", fmt.Errorf("read auth input failed or exceeded 8192 bytes; nothing saved")
		}
		return strings.TrimSpace(string(line)), nil
	})
}

func (r runner) status(ctx context.Context, path string) error {
	c, source, err := credentials.Resolve(r.env, path)
	if err != nil {
		return err
	}
	scope := c.Scope
	if scope == "" {
		scope = "unknown"
	}
	if _, err = fmt.Fprintf(r.out, "Credential source: %s. Scope: %s.\n", source, scope); err != nil {
		return err
	}
	if source == "none" {
		_, err = fmt.Fprintln(r.out, credentials.Hint)
		return err
	}
	username, granted, err := validate(ctx, r.client(c.APIKey, c.Token), c.Token)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Connected as %s. Granted scope: %s.\n", username, granted)
	return err
}

// validate checks identity and actual permissions using two small, curated GETs.
func validate(ctx context.Context, client *trello.Client, token string) (string, string, error) {
	var member struct {
		Username string `json:"username"`
	}
	if err := client.Get(ctx, "/members/me", url.Values{"fields": {"username,fullName"}}, &member); err != nil {
		return "", "", err
	}
	var result struct {
		Permissions []struct {
			Read  bool `json:"read"`
			Write bool `json:"write"`
		} `json:"permissions"`
	}
	if err := client.Get(ctx, "/tokens/"+url.PathEscape(token), url.Values{"fields": {"permissions"}}, &result); err != nil {
		return "", "", err
	}
	read, write := false, false
	for _, p := range result.Permissions {
		read, write = read || p.Read, write || p.Write
	}
	if member.Username == "" || !read {
		return "", "", fmt.Errorf("trello identity or read permission could not be verified; nothing saved")
	}
	scope := "read"
	if write {
		scope = "read,write"
	}
	username := strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, client.Scrub(member.Username))
	return username, scope, nil
}

// openBrowser is best effort and never sends subprocess diagnostics to the terminal.
func openBrowser(address string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{address}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		command, args = "xdg-open", []string{address}
	}
	cmd := exec.Command(command, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("browser unavailable")
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
