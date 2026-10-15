// Command trello-mcp serves token-efficient Trello tools over standard input/output.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/auth"
	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/tools"
	"github.com/avhn/trello-mcp/internal/trello"
)

// version is overridden by release builds using -X main.version.
var version string

// main keeps stdout available for MCP protocol messages unless --version is requested.
func main() {
	if version == "" {
		version = tools.Version
	}
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err := auth.Run(ctx, os.Args[2:], os.Stdin, os.Stdout)
		stop()
		if err != nil {
			fmt.Fprintln(os.Stderr, "trello-mcp auth:", err)
			if errors.Is(err, context.Canceled) {
				os.Exit(130)
			}
			os.Exit(1)
		}
		return
	}
	flags := flag.NewFlagSet("trello-mcp", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() { _, _ = fmt.Fprint(os.Stdout, rootHelp) }
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2)
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "trello-mcp: unexpected arguments; use --help")
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("trello-mcp %s\n", version)
		return
	}
	tools.Version = version
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, logger)
	stop()
	if err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

const rootHelp = `Usage: trello-mcp [--help|-h|--version]
       trello-mcp auth [status|logout] [flags]

With no arguments, serve MCP over standard input/output (stdio).
  auth         Connect Trello in the human's private terminal.
  auth status  Validate credentials and show their source without printing them.
  auth logout  Remove saved credentials, not environment credentials or Trello tokens.
  --version    Print the version and exit.
  --help, -h   Print this help and exit.
Use auth [status|logout] --help for applicable flags.

Credentials default to ~/.secrets/.trello-mcp.secret; TRELLO_MCP_CREDENTIALS
selects a dedicated file. Never share keys, tokens or authorization URLs in
chat or agent-captured commands. The human must run auth privately, then
restart/reconnect the MCP client. Until a configured server starts, only
server_health is exposed.

Writes are enabled by default; TRELLO_READ_ONLY=1 opts into read-only tools.
TRELLO_ALLOWED_WORKSPACES restricts workspace access; TRELLO_TOOLS selects
individual tools or prefix groups (comma-separated, default all).
Call server_health with detail "policy" to inspect the active policy.

Setup: https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md
`

// run wires dependencies after validating startup configuration and saved selection.
func run(ctx context.Context, logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	store, err := state.Open(ctx, c.StatePath, c.BoardID)
	if err != nil {
		return err
	}
	service := tools.New(c, trello.New(c.APIKey, c.Token), store)
	server := mcp.NewServer(&mcp.Implementation{Name: "trello-mcp", Version: tools.Version}, &mcp.ServerOptions{Logger: logger, Instructions: service.Instructions()})
	if err = service.Register(server); err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
