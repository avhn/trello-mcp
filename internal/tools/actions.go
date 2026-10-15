package tools

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/avhn/trello-mcp/internal/view"
)

// actions shares field selection and cursor handling without erasing domain authorization.
func (s *Service) actions(ctx context.Context, path, filter, boardID string, a arguments) (any, error) {
	q := url.Values{"fields": {"id,type,date,data,idMemberCreator"}, "memberCreator": {"true"}, "memberCreator_fields": {"id,username"}, "filter": {filter}, "limit": {strconv.Itoa(a.limit)}}
	for _, key := range []string{"since", "before"} {
		if value := a.str(key); value != "" {
			q.Set(key, value)
		}
	}
	var actions []view.Action
	if err := s.access.Client.Get(ctx, path, q, &actions); err != nil {
		return nil, err
	}
	if len(actions) > a.limit {
		return nil, fmt.Errorf("trello action page exceeds requested limit")
	}
	next := ""
	if len(actions) == a.limit {
		next = actions[len(actions)-1].ID
	}
	return view.Collection(view.ProjectActions(actions, boardID), next), nil
}

// boardActivity checks ownership before requesting board actions with since/before intact.
func (s *Service) boardActivity(ctx context.Context, a arguments) (any, error) {
	id, err := s.boardID(a)
	if err != nil {
		return nil, err
	}
	if _, err = s.access.Board(ctx, id, false); err != nil {
		return nil, err
	}
	filter := a.str("filter")
	if filter == "" {
		filter = "all"
	}
	return s.actions(ctx, "/boards/"+id+"/actions", filter, id, a)
}

// cardActions rechecks the resolved owner after fetching actions, including any retries.
func (s *Service) cardActions(ctx context.Context, filter string, a arguments) (any, error) {
	id := a.str("cardId")
	board, err := s.access.ObjectBoard(ctx, "cards", id, a.str("boardId"))
	if err != nil {
		return nil, err
	}
	out, err := s.actions(ctx, "/cards/"+id+"/actions", filter, board, a)
	if err != nil {
		return nil, err
	}
	if _, err = s.access.ObjectBoard(ctx, "cards", id, board); err != nil {
		return nil, err
	}
	return out, nil
}

// cardHistory preserves the caller's action-type filter and last-action-id cursor.
func (s *Service) cardHistory(ctx context.Context, a arguments) (any, error) {
	filter := a.str("filter")
	if filter == "" {
		filter = "all"
	}
	return s.cardActions(ctx, filter, a)
}

// commentFind always restricts the action endpoint to comments.
func (s *Service) commentFind(ctx context.Context, a arguments) (any, error) {
	return s.cardActions(ctx, "commentCard", a)
}
