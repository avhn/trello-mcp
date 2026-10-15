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
	homeDir     func() (string, error)
	client      func(string, string) *trello.Client
	browser     func(string) error
	hiddenToken func(context.Context) (string, error)
}

// Run handles auth, auth status and auth logout without starting an MCP server.
// It is CLI-only: the caller must exit on cancellation because a piped stdin
// reader can remain blocked until the process exits.
func Run(ctx context.Context, args []string, in *os.File, out io.Writer) error {
	r := runner{in: in, out: out, env: os.Getenv, homeDir: os.UserHomeDir, client: trello.New, browser: openBrowser}
	return r.run(ctx, args)
}

func (r runner) run(ctx context.Context, args []string) error {
	command := "auth"
	if len(args) > 0 && (args[0] == "status" || args[0] == "logout") {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("auth "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "absolute credential file path")
	readOnly := flags.Bool("read-only", false, "request read-only access")
	write := flags.Bool("write", false, "request read and write access")
	expiration := flags.String("expiration", "never", "1day, 30days or never")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return r.help(command)
	} else if err != nil || flags.NArg() != 0 || (*readOnly && *write) {
		return fmt.Errorf("usage: trello-mcp auth [status|logout] [--file PATH] [--read-only|--write] [--expiration 1day|30days|never]")
	}
	if command != "auth" {
		invalid := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "file" {
				invalid = true
			}
		})
		if invalid {
			return fmt.Errorf("usage: trello-mcp auth %s [--file PATH]", command)
		}
	}
	fileSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "file" {
			fileSet = true
		}
	})
	if fileSet && !filepath.IsAbs(*file) {
		return fmt.Errorf("--file must be an absolute path")
	}
	env := r.env
	if fileSet {
		env = func(key string) string {
			if key == "TRELLO_MCP_CREDENTIALS" {
				return *file
			}
			return r.env(key)
		}
	}
	if command == "status" {
		paths, err := credentials.Paths(env, r.homeDir)
		if err != nil {
			return err
		}
		return r.status(ctx, paths)
	}
	path, err := credentials.Path(env, r.homeDir)
	if err != nil {
		return err
	}
	home, homeErr := r.homeDir()
	if homeErr == nil && credentials.SamePath(path, filepath.Join(home, ".secrets.env")) {
		return fmt.Errorf("auth never modifies the shared ~/.secrets.env store; choose a dedicated file")
	}
	switch command {
	case "logout":
		removed, err := credentials.Logout(path)
		if err != nil {
			return err
		}
		if !removed {
			_, err = fmt.Fprintf(r.out, "No saved Trello credentials at %s.\n", path)
			return err
		}
		_, err = fmt.Fprintln(r.out, "Saved Trello credentials removed. Environment credentials, if set, still take precedence.")
		return err
	default:
		return r.connect(ctx, path, *readOnly, *write, *expiration)
	}
}

func (r runner) help(command string) error {
	usage := "trello-mcp auth [--file PATH] [--read-only|--write] [--expiration 1day|30days|never]"
	if command != "auth" {
		usage = "trello-mcp auth " + command + " [--file PATH]"
	}
	_, err := fmt.Fprintf(r.out, `Usage: %s

  --help, -h       Show help for auth, status or logout without side effects.
  --file PATH      Absolute dedicated credential file (auth, status, logout).
                   Default: ~/.secrets/.trello-mcp.secret, overridden by
                   TRELLO_MCP_CREDENTIALS.
  --read-only      Request read-only access (auth only).
  --write          Request read and write access (auth only).
                   Without either flag, auth asks, defaulting to write access.
  --expiration     1day, 30days or never; default never (auth only).

Auth belongs in the human's private terminal, never chat or agent-captured
commands. Never share keys, tokens or authorization URLs. After auth, restart
or reconnect the MCP client; only server_health is exposed until a fresh
configured server starts. Status validates credentials online without printing
them. Logout removes saved credentials, not environment credentials or
Trello-side token authorization; revoke tokens separately in Trello.

Setup: https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md
`, usage)
	return err
}

func (r runner) connect(ctx context.Context, path string, readOnly, write bool, expiration string) error {
	if in, ok := r.in.(*os.File); ok && r.hiddenToken == nil && term.IsTerminal(int(in.Fd())) {
		r.hiddenToken = func(ctx context.Context) (string, error) {
			return readHidden(ctx, fileTerminal{fd: int(in.Fd())}, func() (string, error) {
				line, err := bufio.NewReaderSize(in, 8192).ReadSlice('\n')
				return string(line), err
			})
		}
	}
	if expiration != "1day" && expiration != "30days" && expiration != "never" {
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
	if !credentials.ValidValue(key) || len(key) > 4096 {
		return fmt.Errorf("API key must be a valid unquoted value and at most 4096 bytes; nothing saved")
	}
	if !readOnly && !write {
		answer, promptErr := r.prompt(ctx, reader, "Allow write access? [Y/n] ")
		if promptErr != nil {
			return promptErr
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			write = true
		case "n", "no":
		default:
			return fmt.Errorf("answer y or n; nothing saved")
		}
	}
	scope := "read"
	if write {
		scope = "read,write"
	}
	q := url.Values{"expiration": {expiration}, "name": {"trello-mcp"}, "scope": {scope}, "response_type": {"token"}, "key": {key}}
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
		token = inputLine(token)
	} else {
		token, err = r.prompt(ctx, reader, "Token: ")
		if err != nil {
			return err
		}
	}
	if !credentials.ValidValue(key) || !credentials.ValidValue(token) || len(key) > 4096 || len(token) > 4096 {
		return fmt.Errorf("API key and token must be valid unquoted values and at most 4096 bytes; nothing saved")
	}
	client := r.client(key, token)
	username, granted, err := validate(ctx, client, token)
	if err != nil {
		return err
	}
	if scope == "read" && granted != "read" {
		return fmt.Errorf("token permits writes despite the read-only request; use the token from this authorization link; nothing saved")
	}
	if err = credentials.Save(path, credentials.File{APIKey: key, Token: token, Scope: granted}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Connected as %s. Granted scope: %s. Credentials saved. Restart your MCP client to load them.\n", username, granted)
	return err
}

// terminal isolates state changes so tests can verify cancellation ordering.
type terminal interface {
	getState() (*term.State, error)
	disableEcho() error
	restore(*term.State) error
}

type fileTerminal struct{ fd int }

func (t fileTerminal) getState() (*term.State, error)  { return term.GetState(t.fd) }
func (t fileTerminal) restore(state *term.State) error { return term.Restore(t.fd, state) }

// readHidden changes echo only in the calling goroutine, before starting a reader.
// The reader never changes terminal state, so restoration cannot race a late read.
func readHidden(ctx context.Context, terminal terminal, read func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("auth cancelled: %w", err)
	}
	state, err := terminal.getState()
	if err != nil {
		return "", fmt.Errorf("read terminal state failed; nothing saved")
	}
	defer func() { _ = terminal.restore(state) }()
	if err := terminal.disableEcho(); err != nil {
		return "", fmt.Errorf("disable terminal echo failed; nothing saved")
	}
	return readInput(ctx, read)
}

// readInput lets cancellation interrupt a blocked pipe or terminal read. A buffered
// result allows the goroutine to finish even if cancellation wins. It never changes
// terminal state. Run is CLI-only and its caller exits on cancellation, terminating
// any stdin reader that remains blocked.
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

// inputLine removes one input terminator, not whitespace belonging to credentials.
func inputLine(value string) string {
	if runtime.GOOS == "windows" && strings.HasSuffix(value, "\r\n") {
		return strings.TrimSuffix(value, "\r\n")
	}
	return strings.TrimSuffix(value, "\n")
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
		return inputLine(string(line)), nil
	})
}

func (r runner) status(ctx context.Context, paths []credentials.Source) error {
	c, source, path, err := credentials.Resolve(r.env, paths...)
	if err != nil {
		return err
	}
	if source == "none" {
		_, err = fmt.Fprintf(r.out, "No credentials in this shell or %s. MCP clients may pass them through their own config; run `trello-mcp auth` to save them for all clients.\n", paths[0].Path)
		return err
	}
	label := "file " + path
	if source == "env" {
		label = "env (TRELLO_API_KEY, TRELLO_TOKEN)"
	} else if home, err := r.homeDir(); err == nil && credentials.SamePath(path, filepath.Join(home, ".secrets.env")) {
		label = "file ~/.secrets.env (shared store)"
	}
	if _, err = fmt.Fprintln(r.out, "Credential source: "+label); err != nil {
		return err
	}
	username, granted, err := validate(ctx, r.client(c.APIKey, c.Token), c.Token)
	if err != nil {
		return fmt.Errorf("credential validation failed; check the token and its read permission, then run `trello-mcp auth`: %w", err)
	}
	_, err = fmt.Fprintf(r.out, "Connected as %s, scope %s\n", username, granted)
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
