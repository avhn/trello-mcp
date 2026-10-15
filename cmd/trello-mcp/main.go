// Command trello-mcp serves token-efficient Trello tools over standard input/output.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/tools"
	"github.com/avhn/trello-mcp/internal/trello"
)

// main keeps stdout exclusively available for MCP protocol messages.
func main() {
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
	server := mcp.NewServer(&mcp.Implementation{Name: "trello-mcp", Version: tools.Version}, &mcp.ServerOptions{Logger: logger})
	service := tools.New(c, trello.New(c.APIKey, c.Token), store)
	if err = service.Register(server); err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
