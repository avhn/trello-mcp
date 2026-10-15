package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// writeResult excludes all unsolicited fields in mutation responses.
type writeResult struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// cardCreateResult retains confirmed progress when a later checklist write fails.
type cardCreateResult struct {
	writeResult
	Checklist *createdChecklist `json:"checklist,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type createdChecklist struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Items int    `json:"items"`
}

type success struct {
	OK bool `json:"ok"`
}

func (a arguments) has(key string) bool { _, ok := a.values[key]; return ok }

// payload copies only explicitly permitted, present values, including false and empty strings.
func (a arguments) payload(keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if v, ok := a.values[key]; ok {
			if (key == "due" || key == "start" || key == "color") && v == "" {
				v = nil
			}
			out[key] = v
		}
	}
	return out
}

func (s *Service) mutate(ctx context.Context, method, path string, body any) (any, error) {
	var out writeResult
	if err := s.access.Client.Write(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	if !trello.ValidID(out.ID) {
		return nil, fmt.Errorf("mutation result id unverifiable; mutation may have succeeded")
	}
	return out, nil
}

func (s *Service) mutateOK(ctx context.Context, method, path string, body any) (any, error) {
	if err := s.access.Client.Write(ctx, method, path, body, nil); err != nil {
		return nil, err
	}
	return success{true}, nil
}

func (s *Service) authorizedBoard(ctx context.Context, a arguments) (string, error) {
	id, err := s.boardID(a)
	if err == nil {
		_, err = s.access.Board(ctx, id, false)
	}
	return id, err
}

// members verifies membership on the actual object board, never via a caller's board claim.
func (s *Service) members(ctx context.Context, board string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var members []view.Member
	if err := s.access.Client.Get(ctx, "/boards/"+board+"/members", url.Values{"fields": {"id,username,fullName"}}, &members); err != nil {
		return err
	}
	for _, id := range ids {
		found := false
		for _, member := range members {
			if member.ID == id {
				found = true
				break
			}
		}
		if !found {
			return trello.OwnershipDenied("board member")
		}
	}
	return nil
}

// cardReferences checks every referenced label and member before any card mutation.
func (s *Service) cardReferences(ctx context.Context, board string, a arguments) error {
	if ids, ok := a.values["idLabels"].([]string); ok {
		for _, id := range ids {
			if _, err := s.access.ObjectBoard(ctx, "labels", id, board); err != nil {
				return err
			}
		}
	}
	if ids, ok := a.values["idMembers"].([]string); ok {
		return s.members(ctx, board, ids)
	}
	return nil
}

func nonemptyUpdate(body map[string]any) error {
	if len(body) == 0 {
		return fmt.Errorf("at least one update field is required")
	}
	for _, key := range []string{"name", "text"} {
		if v, ok := body[key].(string); ok && v == "" {
			return fmt.Errorf("%s must not be empty", key)
		}
	}
	return nil
}

func (s *Service) boardCreate(ctx context.Context, a arguments) (any, error) {
	workspace := a.str("workspaceId")
	if workspace == "" {
		workspace = s.state.Get().WorkspaceID
	}
	if workspace == "" {
		return nil, fmt.Errorf("workspaceId or an active workspace is required")
	}
	if _, err := s.access.Workspace(ctx, workspace); err != nil {
		return nil, err
	}
	body := a.payload("name", "desc", "defaultLists")
	body["idOrganization"] = workspace
	return s.mutate(ctx, http.MethodPost, "/boards", body)
}

func (s *Service) listCreate(ctx context.Context, a arguments) (any, error) {
	board, err := s.authorizedBoard(ctx, a)
	if err != nil {
		return nil, err
	}
	body := a.payload("name", "pos")
	body["idBoard"] = board
	return s.mutate(ctx, http.MethodPost, "/lists", body)
}

func (s *Service) listUpdate(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("boardId")); err != nil {
		return nil, err
	}
	body := a.payload("name", "pos", "idBoard", "closed")
	if err := nonemptyUpdate(body); err != nil {
		return nil, err
	}
	if dest := a.str("idBoard"); dest != "" {
		if _, err := s.access.Board(ctx, dest, false); err != nil {
			return nil, err
		}
	}
	return s.mutate(ctx, http.MethodPut, "/lists/"+a.str("listId"), body)
}

func (s *Service) listArchive(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("boardId")); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPut, "/lists/"+a.str("listId")+"/closed", map[string]any{"value": true})
}

func (s *Service) listWatch(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("boardId")); err != nil {
		return nil, err
	}
	value := any(true)
	if a.has("subscribed") {
		value = a.values["subscribed"]
	}
	return s.mutate(ctx, http.MethodPut, "/lists/"+a.str("listId"), map[string]any{"subscribed": value})
}

func cardBody(a arguments, list string) map[string]any {
	body := a.payload("name", "desc", "due", "start", "pos", "idMembers", "idLabels")
	body["idList"] = list
	return body
}

func (s *Service) cardCreate(ctx context.Context, a arguments) (any, error) {
	board, err := s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("boardId"))
	if err != nil {
		return nil, err
	}
	if err = s.cardReferences(ctx, board, a); err != nil {
		return nil, err
	}
	return s.createCard(ctx, a, a.str("listId"))
}

// createCard uses only the authorized list and ids returned by these writes.
func (s *Service) createCard(ctx context.Context, a arguments, list string) (cardCreateResult, error) {
	result, err := s.mutate(ctx, http.MethodPost, "/cards", cardBody(a, list))
	if err != nil {
		return cardCreateResult{}, err
	}
	out := cardCreateResult{writeResult: result.(writeResult)}
	if a.checklist != nil {
		out.Checklist, err = s.createCardChecklist(ctx, out.ID, *a.checklist)
		if err != nil {
			out.Error = s.access.Client.Scrub(err.Error())
		}
	}
	return out, nil
}

func (s *Service) cardCreateBatch(ctx context.Context, a arguments) (any, error) {
	board, err := s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("boardId"))
	if err != nil {
		return nil, err
	}
	// Preflight the whole batch. A later denied reference must not leave earlier cards behind.
	for _, card := range a.cards {
		if err = s.cardReferences(ctx, board, card); err != nil {
			return nil, err
		}
	}
	type item struct {
		Index     int               `json:"index"`
		ID        string            `json:"id,omitempty"`
		Checklist *createdChecklist `json:"checklist,omitempty"`
		Error     string            `json:"error,omitempty"`
	}
	items := make([]item, 0, len(a.cards))
	for i, card := range a.cards {
		// Recheck mutable ownership before each request as well as the initial preflight.
		_, err = s.access.ObjectBoard(ctx, "lists", a.str("listId"), board)
		if err == nil {
			err = s.cardReferences(ctx, board, card)
		}
		var result cardCreateResult
		if err == nil {
			result, err = s.createCard(ctx, card, a.str("listId"))
		}
		entry := item{Index: i}
		if err != nil {
			entry.Error = s.access.Client.Scrub(err.Error())
		} else {
			entry.ID, entry.Checklist, entry.Error = result.ID, result.Checklist, result.Error
		}
		items = append(items, entry)
	}
	return view.Collection(items, ""), nil
}

func (s *Service) cardUpdate(ctx context.Context, a arguments) (any, error) {
	board, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId"))
	if err != nil {
		return nil, err
	}
	if err = s.cardReferences(ctx, board, a); err != nil {
		return nil, err
	}
	body := a.payload("name", "desc", "due", "start", "dueComplete", "closed", "pos", "idMembers", "idLabels")
	if err = nonemptyUpdate(body); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPut, "/cards/"+a.str("cardId"), body)
}

func (s *Service) cardArchive(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPut, "/cards/"+a.str("cardId"), map[string]any{"closed": true})
}

func (s *Service) cardWatch(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	value := any(true)
	if a.has("subscribed") {
		value = a.values["subscribed"]
	}
	return s.mutate(ctx, http.MethodPut, "/cards/"+a.str("cardId"), map[string]any{"subscribed": value})
}

// cardDestination checks the source card and resolves the list's actual destination board.
func (s *Service) cardDestination(ctx context.Context, a arguments) (string, error) {
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return "", err
	}
	return s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("idBoard"))
}

func (s *Service) cardMove(ctx context.Context, a arguments) (any, error) {
	board, err := s.cardDestination(ctx, a)
	if err != nil {
		return nil, err
	}
	body := a.payload("pos")
	body["idBoard"], body["idList"] = board, a.str("listId")
	return s.mutate(ctx, http.MethodPut, "/cards/"+a.str("cardId"), body)
}

func (s *Service) cardCopy(ctx context.Context, a arguments) (any, error) {
	if _, err := s.cardDestination(ctx, a); err != nil {
		return nil, err
	}
	body := a.payload("name", "pos")
	body["idList"], body["idCardSource"] = a.str("listId"), a.str("cardId")
	return s.mutate(ctx, http.MethodPost, "/cards", body)
}

func (s *Service) commentCreate(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId")); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPost, "/cards/"+a.str("cardId")+"/actions/comments", a.payload("text"))
}

func (s *Service) commentOwner(ctx context.Context, a arguments) error {
	board, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId"))
	if err != nil {
		return err
	}
	if _, err = s.access.ObjectBoard(ctx, "actions", a.str("commentId"), board); err != nil {
		return err
	}
	var action view.Action
	if err = s.access.Client.Get(ctx, "/actions/"+a.str("commentId"), url.Values{"fields": {"id,type,data"}}, &action); err != nil {
		return err
	}
	if action.ID != a.str("commentId") || action.Type != "commentCard" || action.Data.Card == nil || action.Data.Board == nil || action.Data.Card.ID != a.str("cardId") || action.Data.Board.ID != board {
		return trello.OwnershipDenied("comment")
	}
	return nil
}

func (s *Service) commentUpdate(ctx context.Context, a arguments) (any, error) {
	if err := s.commentOwner(ctx, a); err != nil {
		return nil, err
	}
	return s.mutate(ctx, http.MethodPut, "/cards/"+a.str("cardId")+"/actions/"+a.str("commentId")+"/comments", a.payload("text"))
}

func (s *Service) commentDelete(ctx context.Context, a arguments) (any, error) {
	if err := s.commentOwner(ctx, a); err != nil {
		return nil, err
	}
	return s.mutateOK(ctx, http.MethodDelete, "/cards/"+a.str("cardId")+"/actions/"+a.str("commentId")+"/comments", nil)
}
