package trello

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/avhn/trello-mcp/internal/view"
)

// ErrDenied distinguishes filtered-out workspaces from unverifiable ownership or network failures.
var ErrDenied = errors.New("workspace access denied")

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidID restricts path components to identifiers, not caller-controlled URL fragments.
func ValidID(id string) bool { return identifier.MatchString(id) }

// Access centralizes workspace and object-level authorization for read and future write tools.
type Access struct {
	Client  *Client
	Allowed []string
}

// WorkspaceAllowed compares ids or canonical names, not ambiguous display names.
func (a *Access) WorkspaceAllowed(w view.Workspace) bool {
	if len(a.Allowed) == 0 {
		return true
	}
	for _, entry := range a.Allowed {
		if entry == w.ID || entry == w.Name {
			return true
		}
	}
	return false
}

// Workspaces fetches the account's workspaces and filters before exposing any records.
func (a *Access) Workspaces(ctx context.Context) ([]view.Workspace, error) {
	var all []view.Workspace
	if err := a.Client.Get(ctx, "/members/me/organizations", url.Values{"fields": {"id,name,displayName"}}, &all); err != nil {
		return nil, err
	}
	allowed := make([]view.Workspace, 0, len(all))
	for _, w := range all {
		if a.WorkspaceAllowed(w) {
			allowed = append(allowed, w)
		}
	}
	return allowed, nil
}

// Workspace resolves an explicit workspace and rejects unverifiable identifiers.
func (a *Access) Workspace(ctx context.Context, id string) (view.Workspace, error) {
	if !ValidID(id) {
		return view.Workspace{}, fmt.Errorf("invalid workspaceId")
	}
	var w view.Workspace
	if err := a.Client.Get(ctx, "/organizations/"+id, url.Values{"fields": {"id,name,displayName"}}, &w); err != nil {
		return w, err
	}
	if w.ID != id {
		return view.Workspace{}, fmt.Errorf("workspace ownership unverifiable")
	}
	if !a.WorkspaceAllowed(w) {
		return view.Workspace{}, ErrDenied
	}
	return w, nil
}

// Board re-resolves current ownership on each call so moved boards cannot reuse stale grants.
func (a *Access) Board(ctx context.Context, id string, full bool) (view.Board, error) {
	if !ValidID(id) {
		return view.Board{}, fmt.Errorf("invalid boardId")
	}
	fields := "id,name,url,idOrganization,closed"
	if full {
		fields += ",desc"
	}
	var b view.Board
	if err := a.Client.Get(ctx, "/boards/"+id, url.Values{"fields": {fields}}, &b); err != nil {
		return b, err
	}
	if b.ID != id {
		return view.Board{}, fmt.Errorf("board ownership unverifiable")
	}
	if !full {
		b.Desc = ""
	}
	if len(a.Allowed) > 0 {
		if b.WorkspaceID == "" {
			return view.Board{}, fmt.Errorf("board workspace unverifiable")
		}
		if _, err := a.Workspace(ctx, b.WorkspaceID); err != nil {
			return view.Board{}, err
		}
	}
	return b, nil
}

// ObjectBoard resolves an object's actual parent before accepting any supplied board constraint.
// Future comment/action, label and custom-field write handlers must use this boundary too.
func (a *Access) ObjectBoard(ctx context.Context, kind, id, expectedBoard string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid object id")
	}
	var parent struct {
		BoardID string `json:"idBoard"`
		CardID  string `json:"idCard"`
	}
	fields := "idBoard"
	switch kind {
	case "cards", "lists":
	case "checklists":
		fields = "idBoard,idCard"
	default:
		return "", fmt.Errorf("unsupported ownership resolver")
	}
	if err := a.Client.Get(ctx, "/"+kind+"/"+id, url.Values{"fields": {fields}}, &parent); err != nil {
		return "", err
	}
	if parent.BoardID == "" && kind == "checklists" && ValidID(parent.CardID) {
		return a.ObjectBoard(ctx, "cards", parent.CardID, expectedBoard)
	}
	if !ValidID(parent.BoardID) {
		return "", fmt.Errorf("object board unverifiable")
	}
	if expectedBoard != "" && expectedBoard != parent.BoardID {
		return "", fmt.Errorf("object does not belong to boardId")
	}
	if _, err := a.Board(ctx, parent.BoardID, false); err != nil {
		return "", err
	}
	return parent.BoardID, nil
}
