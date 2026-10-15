package tools

import (
	"context"
	"net/url"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

const checklistFields = "id,name,idBoard,idCard"
const itemFields = "id,name,state,pos,due,idMember"

// checklistItems authorizes the checklist's owning board, not a caller-provided board claim.
func (s *Service) checklistItems(ctx context.Context, a arguments) (any, error) {
	id := a.str("checklistId")
	board, err := s.access.ObjectBoard(ctx, "checklists", id, a.str("boardId"))
	if err != nil {
		return nil, err
	}
	var list view.Checklist
	q := url.Values{"fields": {checklistFields}, "checkItems": {"all"}, "checkItem_fields": {itemFields}}
	if err = s.access.Client.Get(ctx, "/checklists/"+id, q, &list); err != nil {
		return nil, err
	}
	if list.ID != id || list.BoardID != board {
		return nil, trello.OwnershipDenied("checklist")
	}
	return view.Collection(list.Items, ""), nil
}

type checklistChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type checklistMiss struct {
	Error     string            `json:"error"`
	Name      string            `json:"name"`
	Available []checklistChoice `json:"available"`
	Total     int               `json:"total"`
	Truncated bool              `json:"truncated"`
}

func (checklistMiss) toolFailed() bool { return true }

// checklistGet finds an exact name, defaulting to Acceptance Criteria, on an authorized card.
func (s *Service) checklistGet(ctx context.Context, a arguments) (any, error) {
	id := a.str("cardId")
	board, err := s.access.ObjectBoard(ctx, "cards", id, a.str("boardId"))
	if err != nil {
		return nil, err
	}
	var lists []view.Checklist
	q := url.Values{"fields": {checklistFields}, "checkItems": {"all"}, "checkItem_fields": {itemFields}}
	if err = s.access.Client.Get(ctx, "/cards/"+id+"/checklists", q, &lists); err != nil {
		return nil, err
	}
	name := a.str("name")
	if name == "" {
		name = "Acceptance Criteria"
	}
	// Validate every sibling before selecting a match or disclosing alternatives.
	seen := map[string]bool{}
	for _, list := range lists {
		if !trello.ValidID(list.ID) || seen[list.ID] || list.BoardID != board || list.CardID != id {
			return nil, trello.OwnershipDenied("checklist")
		}
		seen[list.ID] = true
	}
	for _, list := range lists {
		if list.Name == name {
			if list.Items == nil {
				list.Items = []view.Item{}
			}
			return list, nil
		}
	}
	choices := make([]checklistChoice, 0, min(len(lists), 20))
	for _, list := range lists[:min(len(lists), 20)] {
		choices = append(choices, checklistChoice{ID: list.ID, Name: list.Name})
	}
	return checklistMiss{Error: "checklist not found; choose an available exact name", Name: name, Available: choices, Total: len(lists), Truncated: len(lists) > len(choices)}, nil
}
