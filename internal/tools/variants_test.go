package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
				if q.Get("fields") != cardFields {
					t.Error("wrong card fields")
				}
			}, "cards")
			defer fake.Close()
			service := testService(t, fake, []string{"w1"})
			for _, tc := range []struct{ cursor, want string }{{"", `{"items":[` + cardJSON + `],"next":"c1"}`}, {"c1", `{"items":[` + strings.ReplaceAll(strings.ReplaceAll(cardJSON, `"id":"c1"`, `"id":"c2"`), "Fix login", "Fix logout") + `]}`}, {"missing", ""}} {
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
				if tool == "card_history" {
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
				if tc.full && (q.Get("checklists") != "all" || q.Get("member_fields") != "id,username,fullName") {
					t.Errorf("missing full projection parameters: %v", q)
				}
				if len(tc.extras) > 0 && (q.Get("stickers") != "true" || q.Get("sticker_fields") != "id,image,top,left" || q.Get("pluginData") != "true" || q.Get("customFieldItems") != "true" || !strings.Contains(q.Get("fields"), "idMembersVoted")) {
					t.Errorf("missing extra parameters: %v", q)
				}
				_, _ = w.Write([]byte(`{"id":"c1","name":"Card","idBoard":"b1","desc":"description","start":"2026-10-15","checklists":[],"members":[{"id":"m1"}],"stickers":[{"id":"s1","image":"star","extra":"drop"}],"pluginData":[{"id":"p1","idPlugin":"p","value":"value","extra":"drop"}],"idMembersVoted":["m1"],"customFieldItems":[{"idCustomField":"f1","idValue":"o1"}],"badges":{}}`))
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
			want := `{"id":"c1","name":"Card","idBoard":"b1","desc":"description"`
			if tc.full {
				want += `,"start":"2026-10-15","members":[{"id":"m1"}]`
			}
			if len(tc.extras) > 0 {
				want += `,"stickers":[{"id":"s1","image":"star"}],"pluginData":[{"id":"p1","idPlugin":"p","value":"value"}],"idMembersVoted":["m1"],"customFieldItems":[{"idCustomField":"f1","idValue":"o1"}]`
			}
			want += "}"
			if tc.markdown {
				want = `{"markdown":"# Card\n\ndescription"}`
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
