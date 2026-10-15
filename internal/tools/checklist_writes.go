package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// loadChecklist verifies both the board and card and preserves item membership for mutations.
func (s *Service) loadChecklist(ctx context.Context, a arguments) (view.Checklist, error) {
	id := a.str("checklistId")
	board, err := s.access.ObjectBoard(ctx, "checklists", id, a.str("boardId"))
	if err != nil {
		return view.Checklist{}, err
	}
	var list view.Checklist
	q := url.Values{"fields": {checklistFields}, "checkItems": {"all"}, "checkItem_fields": {itemFields}}
	if err = s.access.Client.Get(ctx, "/checklists/"+id, q, &list); err != nil {
		return list, err
	}
	if list.ID != id || list.BoardID != board || !trello.ValidID(list.CardID) {
		return list, fmt.Errorf("checklist ownership unverifiable")
	}
	if _, err = s.access.ObjectBoard(ctx, "cards", list.CardID, board); err != nil {
		return list, err
	}
	return list, nil
}

func (s *Service) checklistCreate(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	body := a.payload("name", "pos")
	body["idCard"] = a.str("cardId")
	return s.mutate(ctx, http.MethodPost, "/checklists", body)
}

func (s *Service) checklistCopy(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "checklists", a.str("checklistId"), ""); err != nil {
		return nil, err
	}
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	body := a.payload("name", "pos")
	body["idCard"], body["idChecklistSource"] = a.str("cardId"), a.str("checklistId")
	return s.mutate(ctx, http.MethodPost, "/checklists", body)
}

func (s *Service) checklistItemFind(ctx context.Context, a arguments) (any, error) {
	list, err := s.loadChecklist(ctx, a)
	if err != nil {
		return nil, err
	}
	items := make([]view.Item, 0, len(list.Items))
	for _, item := range list.Items {
		if matches(item.Name, a.str("nameFilter")) {
			items = append(items, item)
		}
	}
	return view.Collection(items, ""), nil
}

func (s *Service) checklistItemCreate(ctx context.Context, a arguments) (any, error) {
	list, err := s.loadChecklist(ctx, a)
	if err != nil {
		return nil, err
	}
	if id := a.str("memberId"); id != "" {
		if err = s.members(ctx, list.BoardID, []string{id}); err != nil {
			return nil, err
		}
	}
	body := a.payload("name", "pos", "checked", "due")
	if a.has("memberId") {
		body["idMember"] = a.str("memberId")
	}
	return s.mutate(ctx, http.MethodPost, "/checklists/"+list.ID+"/checkItems", body)
}

// checklistItemOwner proves item membership, rather than trusting an arbitrary item id on a card.
func (s *Service) checklistItemOwner(ctx context.Context, a arguments) (view.Checklist, error) {
	list, err := s.loadChecklist(ctx, a)
	if err != nil {
		return list, err
	}
	for _, item := range list.Items {
		if item.ID == a.str("itemId") {
			return list, nil
		}
	}
	return list, fmt.Errorf("item does not belong to checklist")
}

func (s *Service) checklistItemUpdate(ctx context.Context, a arguments) (any, error) {
	list, err := s.checklistItemOwner(ctx, a)
	if err != nil {
		return nil, err
	}
	if id := a.str("memberId"); id != "" {
		if err = s.members(ctx, list.BoardID, []string{id}); err != nil {
			return nil, err
		}
	}
	body := a.payload("name", "state", "pos", "due")
	if a.has("memberId") {
		body["idMember"] = a.str("memberId")
	}
	if err = nonemptyUpdate(body); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPut, "/cards/"+list.CardID+"/checkItem/"+a.str("itemId"), body)
}

func (s *Service) checklistItemDelete(ctx context.Context, a arguments) (any, error) {
	list, err := s.checklistItemOwner(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.mutateOK(ctx, http.MethodDelete, "/checklists/"+list.ID+"/checkItems/"+a.str("itemId"), nil)
}
