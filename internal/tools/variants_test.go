package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/view"
)

// fixtureServer serves synthetic variants, with an assertion hook for data-bearing requests.
func fixtureServer(t *testing.T, check func(*http.Request), cardsFixture string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture := ""
		switch r.URL.Path {
		case "/boards/b1":
			fixture = "board"
		case "/organizations/w1":
			fixture = "workspace"
		case "/cards/c1":
			fixture = "card"
		case "/lists/l1":
			fixture = "list"
		case "/boards/b1/cards", "/lists/l1/cards", "/members/me/cards":
			fixture = cardsFixture
			check(r)
		case "/cards/c1/actions", "/boards/b1/actions":
			fixture = "actions"
			check(r)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trello", fixture+".json"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(data)
	}))
}

// TestCardPaging verifies local snapshot cursors and name filtering before limits for every endpoint.
func TestCardPaging(t *testing.T) {
	for _, selector := range []string{"board", "list", "mine"} {
		t.Run(selector, func(t *testing.T) {
			fake := fixtureServer(t, func(r *http.Request) {
				q := r.URL.Query()
				if q.Has("before") || q.Has("limit") {
					t.Error("unsupported card cursor sent upstream")
				}
				if q.Get("fields") != cardSummaryFields {
					t.Error("wrong card fields")
				}
			}, "cards")
			defer fake.Close()
			service := testService(t, fake, []string{"w1"})
			for _, tc := range []struct{ cursor, want string }{{"", `{"items":[` + cardSummaryJSON + `],"next":"c1"}`}, {"c1", `{"items":[` + strings.ReplaceAll(strings.ReplaceAll(cardSummaryJSON, `"id":"c1"`, `"id":"c2"`), "Fix login", "Fix logout") + `]}`}, {"missing", ""}} {
				a := arguments{strings: map[string]string{"nameFilter": "fix", "before": tc.cursor}, limit: 1}
				switch selector {
				case "list":
					a.strings["listId"] = "l1"
				case "mine":
					a.mine = true
				}
				output, err := service.cardFind(context.Background(), a)
				if tc.want == "" {
					if err == nil {
						t.Error("missing cursor accepted")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				got, err := view.Encode(output)
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Errorf("got %s, want %s", got, tc.want)
				}
			}
		})
	}
}

// TestActionCursors checks since/before and action-type filters without changing their values.
func TestActionCursors(t *testing.T) {
	for _, tool := range []string{"board_activity", "card_history", "comment_find"} {
		t.Run(tool, func(t *testing.T) {
			fake := fixtureServer(t, func(r *http.Request) {
				q := r.URL.Query()
				if q.Get("before") != "a2" || q.Get("since") != "2026-10-01T00:00:00Z" || q.Get("limit") != "1" {
					t.Errorf("cursor query %v", q)
				}
				filter := "all"
				if tool == "card_history" || tool == "board_activity" {
					filter = "updateCard,commentCard"
				}
				if tool == "comment_find" {
					filter = "commentCard"
				}
				if q.Get("filter") != filter {
					t.Errorf("filter %s", q.Get("filter"))
				}
			}, "")
			defer fake.Close()
			service := testService(t, fake, nil)
			a := arguments{strings: map[string]string{"cardId": "c1", "before": "a2", "since": "2026-10-01T00:00:00Z", "filter": "updateCard,commentCard"}, limit: 1}
			var output any
			var err error
			switch tool {
			case "board_activity":
				output, err = service.boardActivity(context.Background(), a)
			case "card_history":
				output, err = service.cardHistory(context.Background(), a)
			case "comment_find":
				output, err = service.commentFind(context.Background(), a)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := view.Encode(output)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"items":[` + projectedActionJSON + `],"next":"a1"}`
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

// TestCardFullExtrasAndMarkdown verifies bounded opt-in expansions and full description retention.
func TestCardFullExtrasAndMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		full     bool
		extras   []string
		markdown bool
	}{
		{name: "summary"}, {name: "full", full: true}, {name: "extras", full: true, extras: []string{"stickers", "pluginData", "votes", "customFieldItems"}}, {name: "markdown", full: true, markdown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/boards/b1" {
					_, _ = w.Write([]byte(`{"id":"b1","idOrganization":"w1"}`))
					return
				}
				if r.URL.Path != "/cards/c1" {
					t.Error("unexpected request")
					w.WriteHeader(500)
					return
				}
				if r.URL.Query().Get("fields") == "idBoard" {
					_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
					return
				}
				q := r.URL.Query()
				if !strings.Contains(","+q.Get("fields")+",", ",url,") {
					t.Error("card_get stopped requesting URL")
				}
				if tc.full && (q.Get("checklists") != "all" || q.Get("member_fields") != "id,username,fullName") {
					t.Errorf("missing full projection parameters: %v", q)
				}
				if len(tc.extras) > 0 && (q.Get("stickers") != "true" || q.Get("sticker_fields") != "id,image,top,left" || q.Get("pluginData") != "true" || q.Get("customFieldItems") != "true" || !strings.Contains(q.Get("fields"), "idMembersVoted")) {
					t.Errorf("missing extra parameters: %v", q)
				}
				_, _ = w.Write([]byte(`{"id":"c1","name":"Card","url":"https://example.invalid/c1","idBoard":"b1","desc":"description","start":"2026-10-15","checklists":[],"members":[{"id":"m1"}],"stickers":[{"id":"s1","image":"star","extra":"drop"}],"pluginData":[{"id":"p1","idPlugin":"p","value":"value","extra":"drop"}],"idMembersVoted":["m1"],"customFieldItems":[{"idCustomField":"f1","idValue":"o1"}],"badges":{}}`))
			}))
			defer fake.Close()
			service := testService(t, fake, nil)
			a := arguments{strings: map[string]string{"cardId": "c1"}, extras: tc.extras}
			if tc.full {
				a.strings["detail"] = "full"
			}
			if tc.markdown {
				a.strings["view"] = "markdown"
			}
			output, err := service.cardGet(context.Background(), a)
			if err != nil {
				t.Fatal(err)
			}
			got, err := view.Encode(output)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"id":"c1","name":"Card","url":"https://example.invalid/c1","idBoard":"b1","desc":"description"`
			if tc.full {
				want += `,"start":"2026-10-15","members":[{"id":"m1"}]`
			}
			if len(tc.extras) > 0 {
				want += `,"stickers":[{"id":"s1","image":"star"}],"pluginData":[{"id":"p1","idPlugin":"p","value":"value"}],"idMembersVoted":["m1"],"customFieldItems":[{"idCustomField":"f1","idValue":"o1"}]`
			}
			want += "}"
			if tc.markdown {
				want = `{"markdown":"# Card\n\ndescription\n\nhttps://example.invalid/c1"}`
			}
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

// TestBoardFindGuestAndAllowlist filters denied workspace boards without requiring account membership.
func TestBoardFindGuestAndAllowlist(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/members/me/boards":
			_, _ = w.Write([]byte(`[{"id":"b1","name":"Guest board","idOrganization":"w1"},{"id":"b2","name":"Denied","idOrganization":"w2"}]`))
		case "/organizations/w1":
			_, _ = w.Write([]byte(workspaceJSON))
		case "/organizations/w2":
			_, _ = w.Write([]byte(`{"id":"w2","name":"other"}`))
		default:
			t.Error("must use account board enumeration")
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	service := testService(t, fake, []string{"allowed"})
	selection := service.state.Get()
	selection.WorkspaceID = ""
	if err := service.state.Update(context.Background(), selection); err != nil {
		t.Fatal(err)
	}
	result, err := service.boardFind(context.Background(), arguments{strings: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"items":[{"id":"b1","name":"Guest board","idOrganization":"w1"}]}` {
		t.Errorf("got %s", data)
	}
}

func TestCardCursorRejectedBeforeHTTP(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("invalid card cursor or mine conflict reached HTTP")
		w.WriteHeader(500)
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	for _, raw := range []string{
		`{"before":"2026-10-15T12:00:00Z"}`, `{"before":"../bad"}`, `{"before":""}`, `{"before":"c1?key=x"}`,
		`{"mine":true,"boardId":"b1"}`, `{"mine":true,"listId":"l1"}`,
	} {
		result, err := s.handler(toolDefinition(t, "card_find"))(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(raw)}})
		if err != nil || !result.IsError {
			t.Errorf("invalid discovery arguments %s: result=%+v err=%v", raw, result, err)
		}
	}
	for _, cursor := range []string{"c1", "synthetic_card-2", "1234567890abcdef12345678"} {
		raw := fmt.Sprintf(`{"before":%q}`, cursor)
		if _, err := validate(json.RawMessage(raw), toolDefinition(t, "card_find")); err != nil {
			t.Errorf("valid synthetic/card id %q rejected: %v", cursor, err)
		}
	}
	for _, tool := range []string{"board_activity", "card_history", "comment_find"} {
		raw := `{"before":"2026-10-15T12:00:00Z","since":"2026-10-01T00:00:00Z"}`
		if tool != "board_activity" {
			raw = `{"cardId":"c1",` + raw[1:]
		}
		if _, err := validate(json.RawMessage(raw), toolDefinition(t, tool)); err != nil {
			t.Errorf("activity date cursor rejected for %s: %v", tool, err)
		}
	}
}

func TestCardDiscoverySummaryFullParity(t *testing.T) {
	for _, selector := range []string{"board", "list", "mine"} {
		t.Run(selector, func(t *testing.T) {
			desc := strings.Repeat("界", 201)
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/lists/l1":
					_, _ = w.Write([]byte(`{"id":"l1","idBoard":"b1"}`))
				case "/boards/b1/cards", "/lists/l1/cards", "/members/me/cards":
					fields := r.URL.Query().Get("fields")
					if fields != cardSummaryFields && fields != cardFields+",start,dateLastActivity" {
						t.Errorf("wrong discovery fields: %s", fields)
					}
					card := view.Card{ID: "c1", Name: "Fix login", URL: "https://example.invalid/c1", BoardID: "b1", ListID: "l1", Desc: desc, Due: "2026-10-20T09:00:00Z", DueComplete: true, Labels: []view.Label{{ID: "label1", Name: "Ready", Color: "green"}}, MemberIDs: []string{"m1"}, Start: "2026-10-15T12:00:00Z", LastActivity: "2026-10-15T12:00:00Z"}
					second := card
					second.ID = "c2"
					if err := json.NewEncoder(w).Encode([]view.Card{card, second}); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			s := testService(t, fake, nil)
			var baseline []view.Card
			for _, detail := range []string{"", "summary", "full"} {
				for _, cursor := range []string{"", "c1"} {
					a := arguments{strings: map[string]string{"detail": detail, "before": cursor, "nameFilter": "fix"}, limit: 1}
					if selector == "list" {
						a.strings["listId"] = "l1"
					}
					a.mine = selector == "mine"
					out, err := s.cardFind(context.Background(), a)
					if err != nil {
						t.Fatal(err)
					}
					text, err := view.Encode(out)
					if err != nil {
						t.Fatal(err)
					}
					var page struct {
						Items []view.Card `json:"items"`
						Next  string      `json:"next"`
					}
					if err := json.Unmarshal([]byte(text), &page); err != nil {
						t.Fatal(err)
					}
					if len(page.Items) != 1 {
						t.Fatalf("bad page: %s", text)
					}
					c := page.Items[0]
					id, next := "c1", "c1"
					if cursor != "" {
						id, next = "c2", ""
					}
					if c.ID != id || page.Next != next || c.Desc != strings.Repeat("界", 200) || !c.DescTruncated {
						t.Fatalf("changed ordering/cursor/preview: %s", text)
					}
					if detail == "full" {
						if c.URL == "" || c.Start == "" || c.LastActivity == "" {
							t.Fatalf("full discovery lost fields: %s", text)
						}
					} else if c.URL != "" || strings.Contains(text, `"url"`) || c.Start != "" || c.LastActivity != "" {
						t.Fatalf("summary retained unsolicited fields: %s", text)
					}
					c.URL, c.Start, c.LastActivity = "", "", ""
					index := 0
					if cursor != "" {
						index = 1
					}
					if detail == "" {
						baseline = append(baseline, c)
					} else if !reflect.DeepEqual(c, baseline[index]) {
						t.Fatalf("summary/full changed shared fields: got %+v want %+v", c, baseline[index])
					}
				}
			}
		})
	}
}

func TestFilteredBoardActivityDenial(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/boards/b1":
			_, _ = w.Write([]byte(`{"id":"b1","idOrganization":"denied"}`))
		case "/organizations/denied":
			_, _ = w.Write([]byte(`{"id":"denied","name":"denied"}`))
		default:
			t.Errorf("denied board activity fetched data: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, []string{"w1"})
	a, err := validate(json.RawMessage(`{"filter":"commentCard","limit":1}`), toolDefinition(t, "board_activity"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.boardActivity(context.Background(), a); err == nil {
		t.Fatal("filtered board activity bypassed authorization")
	}
}

func TestBoardActivityFilterForwarding(t *testing.T) {
	for _, filter := range []string{"", "all", "commentCard", "createCard,updateCard", "future_Action2"} {
		t.Run(filter, func(t *testing.T) {
			wantFilter := filter
			if wantFilter == "" {
				wantFilter = "all"
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/boards/b1/actions":
					q := r.URL.Query()
					if q.Get("filter") != wantFilter || q.Get("limit") != "1" || q.Get("before") != "a2" || q.Get("since") != "2026-10-01T00:00:00Z" {
						t.Errorf("upstream filter/page changed: %v", q)
					}
					_, _ = w.Write([]byte("[" + actionJSON + "]"))
				default:
					t.Errorf("unexpected activity request: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			s := testService(t, fake, nil)
			args := map[string]any{"limit": 1, "before": "a2", "since": "2026-10-01T00:00:00Z"}
			if filter != "" {
				args["filter"] = filter
			}
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			text, failed := callTool(t, s, "board_activity", string(raw))
			// Do not post-filter an upstream page, even if a synthetic upstream ignores its filter.
			if failed || text != `{"items":[`+projectedActionJSON+`],"next":"a1"}` {
				t.Fatalf("upstream page/cursor not preserved: %s", text)
			}
		})
	}
}
