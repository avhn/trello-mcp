package tools

import (
	"context"
	"net/url"

	"github.com/avhn/trello-mcp/internal/view"
)

// boardCatalog authorizes a board before accessing any of its metadata collections.
func (s *Service) boardCatalog(ctx context.Context, a arguments, collection, fields string, out any) error {
	id, err := s.boardID(a)
	if err != nil {
		return err
	}
	if _, err = s.access.Board(ctx, id, false); err != nil {
		return err
	}
	q := url.Values{"fields": {fields}}
	if collection == "labels" {
		q.Set("limit", "1000")
	}
	return s.access.Client.Get(ctx, "/boards/"+id+"/"+collection, q, out)
}

// memberFind exposes names, not personal profile details.
func (s *Service) memberFind(ctx context.Context, a arguments) (any, error) {
	var all []view.Member
	if err := s.boardCatalog(ctx, a, "members", "id,username,fullName", &all); err != nil {
		return nil, err
	}
	items := make([]view.Member, 0, len(all))
	for _, m := range all {
		if matches(m.Username, a.str("nameFilter")) || matches(m.FullName, a.str("nameFilter")) {
			items = append(items, m)
		}
	}
	return view.Collection(items, ""), nil
}

// labelFind retains ids, names and colors for readable card queries and future writes.
func (s *Service) labelFind(ctx context.Context, a arguments) (any, error) {
	var all []view.Label
	if err := s.boardCatalog(ctx, a, "labels", "id,name,color", &all); err != nil {
		return nil, err
	}
	items := make([]view.Label, 0, len(all))
	for _, label := range all {
		if matches(label.Name, a.str("nameFilter")) {
			items = append(items, label)
		}
	}
	return view.Collection(items, ""), nil
}

// customfieldFind retains dropdown option ids and each scalar field's type.
func (s *Service) customfieldFind(ctx context.Context, a arguments) (any, error) {
	var fields []view.CustomField
	if err := s.boardCatalog(ctx, a, "customFields", "id,name,type,options", &fields); err != nil {
		return nil, err
	}
	return view.Collection(fields, ""), nil
}
