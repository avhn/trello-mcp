package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// matches applies case-insensitive substring matching before collection limits.
func matches(name, filter string) bool {
	return strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

// boardID uses local state only as a default; callers must still authorize the resolved id.
func (s *Service) boardID(a arguments) (string, error) {
	id := a.str("boardId")
	if id == "" {
		id = s.state.Get().BoardID
	}
	if id == "" {
		return "", fmt.Errorf("boardId or an active board is required")
	}
	return id, nil
}

// workspaceFind filters both allowlist and optional name before projection.
func (s *Service) workspaceFind(ctx context.Context, a arguments) (any, error) {
	all, err := s.access.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]view.Workspace, 0, len(all))
	for _, w := range all {
		if matches(w.Name, a.str("nameFilter")) || matches(w.DisplayName, a.str("nameFilter")) {
			items = append(items, w)
		}
	}
	return view.Collection(items, ""), nil
}

// workspaceUse verifies access before persisting and clears a potentially unrelated active board.
func (s *Service) workspaceUse(ctx context.Context, a arguments) (any, error) {
	w, err := s.access.Workspace(ctx, a.str("workspaceId"))
	if err != nil {
		return nil, err
	}
	if err = s.state.Update(ctx, state.Selection{WorkspaceID: w.ID}); err != nil {
		return nil, err
	}
	return w, nil
}

// boardFind uses account boards rather than workspace memberships to include guest boards.
func (s *Service) boardFind(ctx context.Context, a arguments) (any, error) {
	workspace := a.str("workspaceId")
	if workspace != "" {
		if _, err := s.access.Workspace(ctx, workspace); err != nil {
			return nil, err
		}
	}
	path := "/members/me/boards"
	if workspace != "" {
		path = "/organizations/" + workspace + "/boards"
	}
	var boards []view.Board
	if err := s.access.Client.Get(ctx, path, url.Values{"fields": {"id,name,url,idOrganization,closed"}, "filter": {"open"}}, &boards); err != nil {
		return nil, err
	}
	items := make([]view.Board, 0, len(boards))
	grants := map[string]bool{}
	for _, b := range boards {
		if workspace != "" && b.WorkspaceID != workspace {
			continue
		}
		if len(s.config.AllowedWorkspaces) > 0 {
			if b.WorkspaceID == "" {
				continue
			}
			grant, known := grants[b.WorkspaceID]
			if !known {
				_, err := s.access.Workspace(ctx, b.WorkspaceID)
				if err != nil && !errors.Is(err, trello.ErrDenied) {
					return nil, s.access.ScopeError(err, "board collection")
				}
				grant = err == nil
				grants[b.WorkspaceID] = grant
			}
			if !grant {
				continue
			}
		}
		if matches(b.Name, a.str("nameFilter")) {
			b.Desc = ""
			items = append(items, b)
		}
	}
	return view.Collection(items, ""), nil
}

// boardUse derives the active workspace from the authorized board, never from caller claims.
func (s *Service) boardUse(ctx context.Context, a arguments) (any, error) {
	b, err := s.access.Board(ctx, a.str("boardId"), false)
	if err != nil {
		return nil, err
	}
	if err = s.state.Update(ctx, state.Selection{BoardID: b.ID, WorkspaceID: b.WorkspaceID}); err != nil {
		return nil, err
	}
	return b, nil
}

// boardGet reads a curated summary or full board projection.
func (s *Service) boardGet(ctx context.Context, a arguments) (any, error) {
	id, err := s.boardID(a)
	if err != nil {
		return nil, err
	}
	return s.access.Board(ctx, id, a.full())
}

// listFind authorizes its board before retrieving a narrow collection of open lists.
func (s *Service) listFind(ctx context.Context, a arguments) (any, error) {
	id, err := s.boardID(a)
	if err != nil {
		return nil, err
	}
	if _, err = s.access.Board(ctx, id, false); err != nil {
		return nil, err
	}
	var all []view.List
	if err = s.access.Client.Get(ctx, "/boards/"+id+"/lists", url.Values{"fields": {"id,name,idBoard,closed,pos"}, "filter": {"open"}}, &all); err != nil {
		return nil, err
	}
	items := make([]view.List, 0, len(all))
	for _, l := range all {
		if l.BoardID != id {
			return nil, trello.OwnershipDenied("list board")
		}
		if matches(l.Name, a.str("nameFilter")) {
			items = append(items, l)
		}
	}
	return view.Collection(items, ""), nil
}
