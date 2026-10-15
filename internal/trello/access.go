package trello

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/avhn/trello-mcp/internal/view"
)

// ErrDenied distinguishes filtered-out workspaces from unverifiable ownership or network failures.
var ErrDenied = errors.New("access denied by TRELLO_ALLOWED_WORKSPACES")

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidID restricts path components to identifiers, not caller-controlled URL fragments.
func ValidID(id string) bool { return identifier.MatchString(id) }

// Access centralizes workspace and object-level authorization for all scoped tools.
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
		return view.Workspace{}, OwnershipDenied("workspace " + id)
	}
	if !a.WorkspaceAllowed(w) {
		return view.Workspace{}, a.workspaceDenied("workspace " + id)
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
		return view.Board{}, OwnershipDenied("board " + id)
	}
	if !full {
		b.Desc = ""
	}
	if len(a.Allowed) > 0 {
		if !ValidID(b.WorkspaceID) {
			return view.Board{}, OwnershipDenied("workspace for board " + id)
		}
		if _, err := a.Workspace(ctx, b.WorkspaceID); err != nil {
			return view.Board{}, a.ScopeError(err, "board "+id)
		}
	}
	return b, nil
}

// ObjectBoard resolves an object's actual parent before accepting any supplied board constraint.
func (a *Access) ObjectBoard(ctx context.Context, kind, id, expectedBoard string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid object id")
	}
	var parent struct {
		BoardID   string `json:"idBoard"`
		CardID    string `json:"idCard"`
		ModelID   string `json:"idModel"`
		ModelType string `json:"modelType"`
		Data      struct {
			Board view.Named `json:"board"`
			Card  view.Named `json:"card"`
		} `json:"data"`
	}
	fields := "idBoard"
	switch kind {
	case "cards", "lists", "labels":
	case "checklists":
		fields = "idBoard,idCard"
	case "customFields":
		fields = "idModel,modelType"
	case "actions":
		fields = "data"
	default:
		return "", fmt.Errorf("unsupported ownership resolver")
	}
	if err := a.Client.Get(ctx, "/"+kind+"/"+id, url.Values{"fields": {fields}}, &parent); err != nil {
		return "", err
	}
	if kind == "customFields" && parent.ModelType == "board" {
		parent.BoardID = parent.ModelID
	}
	if kind == "actions" {
		parent.BoardID = parent.Data.Board.ID
		if !ValidID(parent.Data.Card.ID) {
			return "", OwnershipDenied("card for action " + id)
		}
	}
	if parent.BoardID == "" && kind == "checklists" && ValidID(parent.CardID) {
		board, err := a.ObjectBoard(ctx, "cards", parent.CardID, "")
		if err != nil {
			return "", a.ScopeError(err, strings.TrimSuffix(kind, "s")+" "+id)
		}
		parent.BoardID = board
	}
	if !ValidID(parent.BoardID) {
		return "", OwnershipDenied("board for object " + id)
	}
	if _, err := a.Board(ctx, parent.BoardID, false); err != nil {
		return "", a.ScopeError(err, strings.TrimSuffix(kind, "s")+" "+id)
	}
	if kind == "actions" {
		if _, err := a.ObjectBoard(ctx, "cards", parent.Data.Card.ID, parent.BoardID); err != nil {
			return "", a.ScopeError(err, strings.TrimSuffix(kind, "s")+" "+id)
		}
	}
	if expectedBoard != "" && expectedBoard != parent.BoardID {
		return "", PolicyDenied("boardId constraint", fmt.Sprintf("object %s belongs to board %s, not the supplied boardId %s; supply its owning boardId or omit the constraint", id, parent.BoardID, expectedBoard))
	}
	return parent.BoardID, nil
}

// workspaceDenial keeps collection filtering distinct from unverifiable ownership.
type workspaceDenial struct{ error }

func (workspaceDenial) Unwrap() error { return ErrDenied }

func (a *Access) workspaceDenied(object string) error {
	return workspaceDenial{PolicyDenied("TRELLO_ALLOWED_WORKSPACES", object+" is outside the allowed workspaces (allowed: "+strings.Join(a.Allowed, ",")+")")}
}

// ScopeError keeps nested ownership failures limited to the caller's object, not resolved parents.
func (a *Access) ScopeError(err error, object string) error {
	if errors.Is(err, ErrDenied) {
		return a.workspaceDenied(object)
	}
	var ownership ownershipDenial
	if errors.As(err, &ownership) {
		return OwnershipDenied(object)
	}
	return fmt.Errorf("%s ownership could not be verified (temporary error); retry", object)
}
