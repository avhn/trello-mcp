package tools

import (
	"context"
	"fmt"
	"net/url"

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
		return nil, fmt.Errorf("checklist ownership changed or unverifiable")
	}
	return view.Collection(list.Items, ""), nil
}

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
	for _, list := range lists {
		if list.BoardID != board || list.CardID != id {
			return nil, fmt.Errorf("checklist ownership unverifiable")
		}
		if list.Name == name {
			if list.Items == nil {
				list.Items = []view.Item{}
			}
			return list, nil
		}
	}
	return nil, fmt.Errorf("checklist not found")
}
