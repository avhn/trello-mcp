package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/view"
)

func TestCardFindIncludeDescValidation(t *testing.T) {
	d := toolDefinition(t, "card_find")
	if _, present := schema(d)["not"]; present {
		t.Fatal("compact schema must omit the detail/includeDesc exclusion")
	}
	resolved := resolvedToolSchema(t, d)
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{}`, true}, {`{"includeDesc":true}`, true}, {`{"includeDesc":false}`, true},
		{`{"detail":"summary","includeDesc":false}`, true}, {`{"detail":"full","includeDesc":true}`, true},
		{`{"detail":"full","includeDesc":false}`, false}, {`{"includeDesc":"false"}`, false},
		{`{"includeDesc":0}`, false}, {`{"includeDesc":null}`, false},
	} {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.raw), &args); err != nil {
			t.Fatal(err)
		}
		_, runtimeErr := validate([]byte(tc.raw), d)
		schemaErr := resolved.Validate(args)
		schemaValid := tc.valid || tc.raw == `{"detail":"full","includeDesc":false}`
		if (runtimeErr == nil) != tc.valid || (schemaErr == nil) != schemaValid {
			t.Errorf("%s runtime valid=%v schema valid=%v runtime=%v schema=%v", tc.raw, tc.valid, schemaValid, runtimeErr, schemaErr)
		}
		if tc.raw == `{"detail":"full","includeDesc":false}` && (runtimeErr == nil || runtimeErr.Error() != "includeDesc:false cannot be combined with detail:full") {
			t.Errorf("missing runtime exclusion error: %v", runtimeErr)
		}
	}
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	for _, raw := range []string{`{"detail":"full","includeDesc":false}`, `{"includeDesc":null}`, `{"includeDesc":"false"}`, `{"includeDesc":true,"includeDesc":false}`} {
		if _, failed := callTool(t, s, "card_find", raw); !failed {
			t.Errorf("accepted %s", raw)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid includeDesc made %d HTTP calls", calls.Load())
	}
}

func TestCardFindDescriptionProjection(t *testing.T) {
	for _, selector := range []string{"board", "list", "mine"} {
		for _, detail := range []string{"", "summary"} {
			t.Run(selector+"/"+detail, func(t *testing.T) {
				wantDescriptions := true
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/boards/b1":
						_, _ = w.Write([]byte(boardJSON))
					case "/boards/denied":
						_, _ = w.Write([]byte(`{"id":"denied","idOrganization":"other"}`))
					case "/organizations/w1":
						_, _ = w.Write([]byte(`{"id":"w1","name":"synthetic"}`))
					case "/organizations/other":
						_, _ = w.Write([]byte(`{"id":"other","name":"other"}`))
					case "/lists/l1":
						_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
					case "/boards/b1/cards", "/lists/l1/cards", "/members/me/cards":
						fields := strings.Split(r.URL.Query().Get("fields"), ",")
						if slices.Contains(fields, "desc") != wantDescriptions || r.URL.Query().Has("before") || r.URL.Query().Has("limit") {
							t.Errorf("wrong upstream projection/paging: %s", r.URL.Query().Get("fields"))
						}
						cards := make([]view.Card, 0, 4)
						for i, name := range []string{"Skip", "Fix 界", "Fix second"} {
							cards = append(cards, view.Card{ID: fmt.Sprintf("c%d", i), Name: name, URL: "https://example.invalid/card", BoardID: "b1", ListID: "l1", Desc: strings.Repeat("\U00010400", 201), DescTruncated: true, Due: "2026-10-20T09:00:00Z", DueComplete: true, Closed: true, Labels: []view.Label{{ID: "label1", Name: "Ready", Color: "green"}}, MemberIDs: []string{"m1"}, Start: "unsolicited", LastActivity: "unsolicited", Votes: []string{"m1"}})
						}
						if selector == "mine" {
							cards = append(cards, view.Card{ID: "denied", Name: "Fix denied", BoardID: "denied", ListID: "elsewhere", Desc: "denied"})
						}
						// Deliberately return desc and descTruncated even when not requested.
						if err := json.NewEncoder(w).Encode(cards); err != nil {
							t.Error(err)
						}
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
						w.WriteHeader(500)
					}
				}))
				defer fake.Close()
				s := testService(t, fake, []string{"w1"})
				for _, cursor := range []string{"", "c1"} {
					args := map[string]any{"detail": detail, "nameFilter": "fix", "limit": 1}
					if detail == "" {
						delete(args, "detail")
					}
					if cursor != "" {
						args["before"] = cursor
					}
					if selector == "list" {
						args["listId"] = "l1"
					}
					if selector == "mine" {
						args["mine"] = true
					}
					var baseline map[string]any
					for _, mode := range []string{"omitted", "true", "false"} {
						wantDescriptions = mode != "false"
						delete(args, "includeDesc")
						if mode != "omitted" {
							args["includeDesc"] = wantDescriptions
						}
						raw, err := json.Marshal(args)
						if err != nil {
							t.Fatal(err)
						}
						text, failed := callTool(t, s, "card_find", string(raw))
						if failed {
							t.Fatal(text)
						}
						var page map[string]any
						if err = json.Unmarshal([]byte(text), &page); err != nil {
							t.Fatal(err)
						}
						items := page["items"].([]any)
						if len(items) != 1 {
							t.Fatalf("wrong filtered page size: %d", len(items))
						}
						card := items[0].(map[string]any)
						if wantDescriptions {
							if card["desc"] != strings.Repeat("\U00010400", 200) || card["descTruncated"] != true {
								t.Fatal("default Unicode preview/flag changed")
							}
						} else if _, present := card["desc"]; present || card["descTruncated"] != nil {
							t.Fatal("unsolicited description/flag leaked")
						}
						delete(card, "desc")
						delete(card, "descTruncated")
						if mode == "omitted" {
							baseline = page
						} else if !reflect.DeepEqual(page, baseline) {
							t.Fatalf("mode %s changed retained fields or cursors", mode)
						}
					}
				}
			})
		}
	}
}

func TestCardFindOmissionOwnership(t *testing.T) {
	for _, selector := range []string{"board", "list"} {
		for _, failure := range []string{"board", "list"} {
			if selector == "board" && failure == "list" {
				continue
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/lists/l1":
					_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
				default:
					card := view.Card{ID: "c1", Name: "Card", BoardID: "b1", ListID: "l1", Desc: "unsolicited"}
					if failure == "board" {
						card.BoardID = "other"
					} else {
						card.ListID = "other"
					}
					if err := json.NewEncoder(w).Encode([]view.Card{card}); err != nil {
						t.Error(err)
					}
				}
			}))
			s := testService(t, fake, nil)
			raw := `{"includeDesc":false}`
			if selector == "list" {
				raw = `{"listId":"l1","includeDesc":false}`
			}
			if text, failed := callTool(t, s, "card_find", raw); !failed || !strings.Contains(text, "ownership") {
				t.Errorf("%s/%s accepted mismatched ownership: %s", selector, failure, text)
			}
			fake.Close()
		}
	}
}
