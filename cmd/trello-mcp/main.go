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
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
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
