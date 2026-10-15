// Package tools validates MCP arguments and implements the Trello tool domains.
package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// Service owns domain dependencies and the shared authorization boundary.
type Service struct {
	access           *trello.Access
	state            *state.Store
	config           config.Config
	started          time.Time
	policyOnce       sync.Once
	policyWorkspaces []view.Workspace
}

// toolFailed marks partial mutation results without expanding their text-only output.
type toolFailed interface {
	toolFailed() bool
}

// definition binds one concise schema to a domain method. Write is separate from local selection changes.
type definition struct {
	name, description, params, required string
	write                               bool
	call                                func(context.Context, arguments) (any, error)
}

// New creates a service without reading environment variables or making network calls.
func New(c config.Config, client *trello.Client, store *state.Store) *Service {
	return &Service{access: &trello.Access{Client: client, Allowed: c.AllowedWorkspaces}, state: store, config: c, started: time.Now()}
}

// definitions keeps groups derived from names so new domains can slot into registration filters.
func (s *Service) definitions() []definition {
	return append([]definition{
		{name: "workspace_find", description: "Find accessible workspaces.", params: "nameFilter", call: s.workspaceFind},
		{name: "workspace_use", description: "Set active workspace; clear active board.", params: "workspaceId", required: "workspaceId", call: s.workspaceUse},
		{name: "board_find", description: "Find all accessible boards, including guest boards.", params: "workspaceId nameFilter", call: s.boardFind},
		{name: "board_use", description: "Set active board.", params: "boardId", required: "boardId", call: s.boardUse},
		{name: "board_get", description: "Get board details.", params: "boardId detail", call: s.boardGet},
		{name: "board_activity", description: "Page board actions, newest first; omitted card/list names inherit per page.", params: "boardId limit since before filter", call: s.boardActivity},
		{name: "list_find", description: "Find board lists.", params: "boardId nameFilter", call: s.listFind},
		{name: "card_find", description: "Find cards; filter names before paging a snapshot.", params: "boardId listId mine nameFilter detail limit before", call: s.cardFind},
		{name: "card_get", description: "Get a card; optional curated extras or markdown.", params: "boardId cardId detail view extraFields", required: "cardId", call: s.cardGet},
		{name: "card_history", description: "Page filtered card actions; omitted card/list names inherit per page.", params: "boardId cardId limit before since filter", required: "cardId", call: s.cardHistory},
		{name: "attachment_find", description: "Find card attachment metadata and link URLs.", params: "boardId cardId", required: "cardId", call: s.attachmentFind},
		{name: "comment_find", description: "Page card comments; omitted card/list names inherit per page.", params: "boardId cardId limit before since", required: "cardId", call: s.commentFind},
		{name: "checklist_items", description: "Get checklist items by checklist id.", params: "boardId checklistId", required: "checklistId", call: s.checklistItems},
		{name: "checklist_get", description: "Find a card checklist by exact name.", params: "boardId cardId name", required: "cardId", call: s.checklistGet},
		{name: "member_find", description: "Find board members.", params: "boardId nameFilter", call: s.memberFind},
		{name: "label_find", description: "Find board labels.", params: "boardId nameFilter", call: s.labelFind},
		{name: "customfield_find", description: "Get custom-field types and dropdown option ids.", params: "boardId", call: s.customfieldFind},
		{name: "server_health", description: "Health: basic, detailed, metadata, performance or policy (access restrictions).", params: "detail", call: s.serverHealth},
	}, s.deliveryDefinitions()...)
}

// Register validates selectors before installing raw handlers with text-only results.
func (s *Service) Register(server *mcp.Server) error {
	defs := s.definitions()
	valid := map[string]bool{"all": true}
	for _, d := range defs {
		valid[d.name] = true
		valid[strings.SplitN(d.name, "_", 2)[0]] = true
	}
	for _, name := range s.config.Tools {
		if !valid[name] {
			return fmt.Errorf("unknown TRELLO_TOOLS selector %q", name)
		}
	}
	for _, d := range defs {
		if (!s.config.HasCredentials() && d.name != "server_health") || (s.config.ReadOnly && d.write) || (s.config.HasCredentials() && !s.selected(d)) {
			continue
		}
		var annotations *mcp.ToolAnnotations
		if !d.write && d.name != "workspace_use" && d.name != "board_use" {
			annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
		}
		server.AddTool(&mcp.Tool{Name: d.name, Description: d.description, InputSchema: schema(d), Annotations: annotations}, s.handler(d))
	}
	return nil
}

// handler keeps all errors inside compact text content, never protocol or structured output.
func (s *Service) handler(d definition) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := validate(req.Params.Arguments, d)
		var out any
		if !s.config.HasCredentials() && d.name != "server_health" {
			err = fmt.Errorf("%s", credentials.Hint)
		} else if s.config.ReadOnly && d.write {
			err = trello.PolicyDenied("TRELLO_READ_ONLY", "write tools are hidden; use read tools or ask the operator to change the configuration")
		}
		if err == nil {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			out, err = d.call(ctx, args)
		}
		failed := err != nil
		if result, ok := out.(toolFailed); ok && result.toolFailed() {
			failed = true
		}
		if err != nil {
			if !s.config.HasCredentials() && d.name == "server_health" {
				err = fmt.Errorf("%w. %s", err, credentials.Hint)
			}
			out = struct {
				Error string `json:"error"`
			}{s.access.Client.Scrub(err.Error())}
		}
		text, encodeErr := view.Encode(out)
		if encodeErr != nil {
			failed = true
			text = `{"error":"encode response failed"}`
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: failed}, nil
	}
}
