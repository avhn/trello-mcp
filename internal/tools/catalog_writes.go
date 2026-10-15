package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

func (s *Service) cardMember(ctx context.Context, a arguments, remove bool) (any, error) {
	board, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId"))
	if err != nil {
		return nil, err
	}
	if err = s.members(ctx, board, []string{a.str("memberId")}); err != nil {
		return nil, err
	}
	path := "/cards/" + a.str("cardId") + "/idMembers"
	if remove {
		return s.mutateOK(ctx, http.MethodDelete, path+"/"+a.str("memberId"), nil)
	}
	return s.mutateOK(ctx, http.MethodPost, path, map[string]any{"value": a.str("memberId")})
}

func (s *Service) cardMemberAdd(ctx context.Context, a arguments) (any, error) {
	return s.cardMember(ctx, a, false)
}
func (s *Service) cardMemberRemove(ctx context.Context, a arguments) (any, error) {
	return s.cardMember(ctx, a, true)
}

func (s *Service) labelCreate(ctx context.Context, a arguments) (any, error) {
	board, err := s.authorizedBoard(ctx, a)
	if err != nil {
		return nil, err
	}
	body := a.payload("name", "color")
	if !a.has("color") {
		body["color"] = nil
	}
	body["idBoard"] = board
	return s.mutate(ctx, http.MethodPost, "/labels", body)
}

func (s *Service) labelUpdate(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "labels", a.str("labelId"), a.str("boardId")); err != nil {
		return nil, err
	}
	body := a.payload("name", "color")
	if len(body) == 0 {
		return nil, fmt.Errorf("at least one update field is required")
	}
	return s.mutate(ctx, http.MethodPut, "/labels/"+a.str("labelId"), body)
}

func (s *Service) labelDelete(ctx context.Context, a arguments) (any, error) {
	if _, err := s.access.ObjectBoard(ctx, "labels", a.str("labelId"), a.str("boardId")); err != nil {
		return nil, err
	}
	return s.mutateOK(ctx, http.MethodDelete, "/labels/"+a.str("labelId"), nil)
}

// customfieldSet derives the type and valid option ids from the authorized definition.
func (s *Service) customfieldSet(ctx context.Context, a arguments) (any, error) {
	board, err := s.access.ObjectBoard(ctx, "cards", a.str("cardId"), a.str("boardId"))
	if err != nil {
		return nil, err
	}
	id := a.str("customFieldId")
	if _, err = s.access.ObjectBoard(ctx, "customFields", id, board); err != nil {
		return nil, err
	}
	var field struct {
		view.CustomField
		ModelID   string `json:"idModel"`
		ModelType string `json:"modelType"`
	}
	if err = s.access.Client.Get(ctx, "/customFields/"+id, url.Values{"fields": {"id,name,type,options,idModel,modelType"}}, &field); err != nil {
		return nil, err
	}
	if field.ID != id || field.ModelID != board || field.ModelType != "board" {
		return nil, trello.OwnershipDenied("custom field")
	}
	if a.has("value") == a.has("idValue") {
		return nil, fmt.Errorf("supply exactly one of value or idValue")
	}
	body := map[string]any{}
	if field.Type == "list" {
		if !a.has("idValue") {
			return nil, fmt.Errorf("list fields require idValue from customfield_find")
		}
		option := a.str("idValue")
		if option == "" {
			body["value"] = ""
		} else {
			found := false
			for _, o := range field.Options {
				if o.ID == option {
					found = true
					break
				}
			}
			if !found {
				return nil, trello.OwnershipDenied("custom-field option")
			}
			body["idValue"] = option
		}
	} else {
		key := field.Type
		if key == "checkbox" {
			key = "checked"
		}
		if key != "text" && key != "number" && key != "date" && key != "checked" {
			return nil, fmt.Errorf("unsupported custom-field type")
		}
		if !a.has("value") {
			return nil, fmt.Errorf("scalar fields require typed value")
		}
		if v, ok := a.values["value"].(string); ok && v == "" {
			body["value"] = ""
		} else {
			value, ok := a.values["value"].(map[string]any)
			if !ok || len(value) != 1 || value[key] == nil {
				return nil, fmt.Errorf("value does not match custom-field type")
			}
			body["value"] = value
		}
	}
	return s.mutateOK(ctx, http.MethodPut, "/cards/"+a.str("cardId")+"/customField/"+id+"/item", body)
}
