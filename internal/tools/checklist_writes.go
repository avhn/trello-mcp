package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
		return list, trello.OwnershipDenied("checklist")
	}
	if _, err = s.access.ObjectBoard(ctx, "cards", list.CardID, board); err != nil {
		return list, s.access.ScopeError(err, "checklist "+id)
	}
	return list, nil
}

// createCardChecklist only targets the card just created, never a caller-supplied parent.
// Stop on the first failure and retain confirmed progress; ambiguous writes need inspection.
func (s *Service) createCardChecklist(ctx context.Context, card string, a arguments) (*createdChecklist, error) {
	out := &createdChecklist{Name: a.str("name")}
	result, err := s.mutate(ctx, http.MethodPost, "/checklists", map[string]any{"idCard": card, "name": out.Name})
	if err != nil {
		return out, fmt.Errorf("checklist: %w; write may have succeeded; inspect before retrying", err)
	}
	out.ID = result.(writeResult).ID
	for i, name := range a.items {
		_, err = s.mutate(ctx, http.MethodPost, "/checklists/"+out.ID+"/checkItems", map[string]any{"name": name, "pos": "bottom"})
		if err != nil {
			return out, fmt.Errorf("checklist.items[%d]: %w; write may have succeeded; inspect before retrying", i, err)
		}
		out.Items++
	}
	return out, nil
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
	return s.mutateChecklistItem(ctx, http.MethodPost, "/checklists/"+list.ID+"/checkItems", a, list, body)
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
	return list, trello.OwnershipDenied("checklist item")
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
	return s.mutateChecklistItem(ctx, http.MethodPut, "/cards/"+list.CardID+"/checkItem/"+a.str("itemId"), a, list, body)
}

func (s *Service) mutateChecklistItem(ctx context.Context, method, path string, a arguments, list view.Checklist, body map[string]any) (any, error) {
	var fields []mutationField
	if a.has("due") {
		fields = append(fields, mutationField{input: "due", wire: "due", want: a.str("due"), date: true})
	}
	if a.has("memberId") {
		fields = append(fields, mutationField{input: "memberId", wire: "idMember", want: a.str("memberId")})
	}
	if len(fields) == 0 {
		return s.mutate(ctx, method, path, body)
	}
	target := mutationTarget{id: a.str("itemId"), board: list.BoardID, card: list.CardID, checklist: list.ID}
	fallback := func(id string, missing []mutationField) (map[string]json.RawMessage, error) {
		// Re-resolve both actual parents. The scoped single-item endpoint and its
		// checklist identity must still prove membership after the mutation.
		if _, err := s.access.ObjectBoard(ctx, "checklists", list.ID, list.BoardID); err != nil {
			return nil, err
		}
		if _, err := s.access.ObjectBoard(ctx, "cards", list.CardID, list.BoardID); err != nil {
			return nil, err
		}
		selection := []string{"id", "idChecklist"}
		for _, field := range missing {
			selection = append(selection, field.wire)
		}
		var response map[string]json.RawMessage
		err := s.access.Client.Get(ctx, "/cards/"+list.CardID+"/checkItem/"+id, url.Values{"fields": {strings.Join(selection, ",")}}, &response)
		var checklist string
		if err == nil && (json.Unmarshal(response["idChecklist"], &checklist) != nil || checklist != list.ID) {
			err = trello.OwnershipDenied("checklist item")
		}
		return response, err
	}
	result, err := s.mutateVerified(ctx, method, path, body, target, fields, fallback)
	if partial, ok := result.(mutationPartialResult); ok {
		partial.Error = "requested checklist fields not confirmed as applied: " + strings.Join(partial.Unconfirmed, ", ") + "; inspect before retrying. Check advanced checklist availability and permissions."
		result = partial
	}
	return result, err
}

func (s *Service) checklistItemDelete(ctx context.Context, a arguments) (any, error) {
	list, err := s.checklistItemOwner(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.mutateOK(ctx, http.MethodDelete, "/checklists/"+list.ID+"/checkItems/"+a.str("itemId"), nil)
}
