package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

const cardSummaryFields = "id,name,idBoard,idList,desc,due,dueComplete,closed,labels,idMembers"
const cardFields = "id,name,url,idBoard,idList,desc,due,dueComplete,closed,labels,idMembers"

// cardFind filters the complete bounded endpoint snapshot before local cursor paging.
// Trello board/list card collections do not document before/limit, so neither is sent upstream.
func (s *Service) cardFind(ctx context.Context, a arguments) (any, error) {
	path := ""
	board := ""
	var err error
	switch {
	case a.str("listId") != "":
		board, err = s.access.ObjectBoard(ctx, "lists", a.str("listId"), a.str("boardId"))
		path = "/lists/" + a.str("listId") + "/cards"
	case a.mine:
		path = "/members/me/cards"
	default:
		board, err = s.boardID(a)
		if err == nil {
			_, err = s.access.Board(ctx, board, false)
		}
		path = "/boards/" + board + "/cards"
	}
	if err != nil {
		return nil, err
	}
	fields := cardSummaryFields
	if a.full() {
		fields = cardFields + ",start,dateLastActivity"
	} else if !a.includeDescriptions() {
		fields = strings.ReplaceAll(fields, ",desc,", ",")
	}
	var all []view.Card
	if err = s.access.Client.Get(ctx, path, url.Values{"fields": {fields}, "filter": {"open"}}, &all); err != nil {
		return nil, err
	}
	filtered := make([]view.Card, 0, len(all))
	checked := map[string]bool{}
	grants := map[string]bool{}
	for _, c := range all {
		if !a.full() {
			c.URL = ""
		}
		c.Stickers = nil
		c.PluginData = nil
		c.Votes = nil
		c.CustomFields = nil
		c.Checklists = nil
		c.Members = nil
		if board != "" && c.BoardID != board {
			return nil, trello.OwnershipDenied("card board")
		}
		if list := a.str("listId"); list != "" && c.ListID != list {
			return nil, trello.OwnershipDenied("card list")
		}
		if a.mine {
			if !checked[c.BoardID] {
				_, err = s.access.Board(ctx, c.BoardID, false)
				if err != nil && !errors.Is(err, trello.ErrDenied) {
					return nil, s.access.ScopeError(err, "card collection")
				}
				checked[c.BoardID] = true
				grants[c.BoardID] = err == nil
			}
			if !grants[c.BoardID] {
				continue
			}
		}
		if matches(c.Name, a.str("nameFilter")) {
			c = view.ProjectCard(c, a.full(), true)
			// Ignore unsolicited descriptions and flags even if upstream expands them.
			if !a.includeDescriptions() {
				c.Desc = ""
				c.DescTruncated = false
			}
			filtered = append(filtered, c)
		}
	}
	start := 0
	if cursor := a.str("before"); cursor != "" {
		found := false
		for i, c := range filtered {
			if c.ID == cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("card cursor no longer exists in the filtered snapshot")
		}
	}
	end := start + a.limit
	if end > len(filtered) {
		end = len(filtered)
	}
	next := ""
	if end < len(filtered) {
		next = filtered[end-1].ID
	}
	return view.Collection(filtered[start:end], next), nil
}

// cardGet independently resolves card ownership, then requests only the chosen projection and extras.
func (s *Service) cardGet(ctx context.Context, a arguments) (any, error) {
	id := a.str("cardId")
	board, err := s.access.ObjectBoard(ctx, "cards", id, a.str("boardId"))
	if err != nil {
		return nil, err
	}
	fields := cardFields
	q := url.Values{}
	if a.full() {
		fields += ",start,dateLastActivity"
		q.Set("checklists", "all")
		q.Set("checklist_fields", "id,name,idBoard,idCard")
		q.Set("checkItem_fields", "id,name,state,pos,due,idMember")
		q.Set("members", "true")
		q.Set("member_fields", "id,username,fullName")
	}
	for _, extra := range a.extras {
		switch extra {
		case "votes":
			fields += ",idMembersVoted"
		case "stickers":
			q.Set("stickers", "true")
			q.Set("sticker_fields", "id,image,top,left")
		case "pluginData":
			q.Set("pluginData", "true")
		case "customFieldItems":
			q.Set("customFieldItems", "true")
		}
	}
	q.Set("fields", fields)
	var c view.Card
	if err = s.access.Client.Get(ctx, "/cards/"+id, q, &c); err != nil {
		return nil, err
	}
	if c.ID != id || c.BoardID != board {
		return nil, trello.OwnershipDenied("card")
	}
	c = view.ProjectCard(c, a.full(), false)
	// Ignore unsolicited expansions even if an upstream endpoint sends them by default.
	wanted := map[string]bool{}
	for _, extra := range a.extras {
		wanted[extra] = true
	}
	if !wanted["votes"] {
		c.Votes = nil
	}
	if !wanted["stickers"] {
		c.Stickers = nil
	}
	if !wanted["pluginData"] {
		c.PluginData = nil
	}
	if !wanted["customFieldItems"] {
		c.CustomFields = nil
	}
	if a.str("view") == "markdown" {
		return struct {
			Markdown string `json:"markdown"`
		}{view.Markdown(c)}, nil
	}
	return c, nil
}
