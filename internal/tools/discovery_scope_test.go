package tools

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDiscoveryDescriptions advertises open-only discovery and local-only health.
func TestDiscoveryDescriptions(t *testing.T) {
	want := map[string]string{
		"board_find":    "Find all accessible open boards, including guest boards.",
		"card_find":     "Find open cards; filter names before snapshot paging; includeDesc:false omits summary previews.",
		"server_health": "Local config health, not Trello auth; verify with workspace_find. Policy shows access restrictions.",
	}
	for _, d := range (&Service{}).definitions() {
		if description, ok := want[d.name]; ok {
			if d.description != description {
				t.Errorf("%s description: %q", d.name, d.description)
			}
			delete(want, d.name)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}

// TestDiscoveryOpenFilters covers account/workspace boards and board/list/mine cards.
// The synthetic upstream omits archived objects when the requested filter is open.
func TestDiscoveryOpenFilters(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args, path, fields string
	}{
		{"account-boards", "board_find", `{}`, "/members/me/boards", "id,name,url,idOrganization,closed"},
		{"workspace-boards", "board_find", `{"workspaceId":"w1"}`, "/organizations/w1/boards", "id,name,url,idOrganization,closed"},
		{"board-cards", "card_find", `{"boardId":"b1"}`, "/boards/b1/cards", cardSummaryFields},
		{"list-cards", "card_find", `{"listId":"l1"}`, "/lists/l1/cards", cardSummaryFields},
		{"mine-cards", "card_find", `{"mine":true}`, "/members/me/cards", cardSummaryFields},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var discoveries atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				var body string
				switch r.URL.Path {
				case tc.path:
					discoveries.Add(1)
					q := r.URL.Query()
					q.Del("key")
					q.Del("token")
					if !reflect.DeepEqual(q, query("fields", tc.fields, "filter", "open")) {
						t.Errorf("discovery query: %v", q)
						body = `[{"id":"archived","closed":true}]`
					} else {
						body = `[]`
					}
				case "/boards/b1":
					body = boardJSON
				case "/organizations/w1":
					body = workspaceJSON
				case "/lists/l1":
					body = `{"idBoard":"b1"}`
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			defer fake.Close()
			s := testService(t, fake, []string{"w1"})
			if text, failed := callTool(t, s, tc.tool, tc.args); failed || text != `{"items":[]}` || discoveries.Load() != 1 {
				t.Errorf("open discovery: %s, failed=%t, requests=%d", text, failed, discoveries.Load())
			}
		})
	}
}

// TestArchivedCardKnownID requires actual-parent authorization even when discovery omits a card.
func TestArchivedCardKnownID(t *testing.T) {
	const archived = `{"id":"archived","name":"Archived card","idBoard":"b1","idList":"l1","closed":true}`
	for _, tc := range []struct {
		name, args string
		allowed    []string
		failed     bool
	}{
		{"allowed", `{"cardId":"archived"}`, []string{"w1"}, false},
		{"outside-allowlist", `{"cardId":"archived"}`, []string{"other"}, true},
		{"supplied-board-cannot-override-parent", `{"cardId":"archived","boardId":"b2"}`, []string{"w1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Discovery first omits the archived card. Known-ID access must then resolve
			// its actual board and workspace before fetching the card projection.
			expected := []requestSpec{
				{"/members/me/cards", query("fields", cardSummaryFields, "filter", "open"), `[]`},
				{"/cards/archived", query("fields", "idBoard"), `{"idBoard":"b1"}`},
				{"/boards/b1", query("fields", "id,name,url,idOrganization,closed"), boardJSON},
				{"/organizations/w1", query("fields", "id,name,displayName"), workspaceJSON},
			}
			if !tc.failed {
				expected = append(expected, requestSpec{"/cards/archived", query("fields", cardFields), archived})
			}
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(requests.Add(1)) - 1
				if i >= len(expected) {
					t.Errorf("unauthorized or unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				want := expected[i]
				q := r.URL.Query()
				q.Del("key")
				q.Del("token")
				if r.Method != http.MethodGet || r.URL.Path != want.path || !reflect.DeepEqual(q, want.query) {
					t.Errorf("request %d: %s %s %v, want %s %v", i, r.Method, r.URL.Path, q, want.path, want.query)
				}
				_, _ = w.Write([]byte(want.fixture))
			}))
			defer fake.Close()
			s := testService(t, fake, tc.allowed)
			if text, failed := callTool(t, s, "card_find", `{"mine":true}`); failed || text != `{"items":[]}` {
				t.Fatalf("archived card appeared in discovery: %s, failed=%t", text, failed)
			}
			text, failed := callTool(t, s, "card_get", tc.args)
			if failed != tc.failed || int(requests.Load()) != len(expected) {
				t.Fatalf("known-ID access: %s, failed=%t, requests=%d", text, failed, requests.Load())
			}
			if !failed && (!strings.Contains(text, `"id":"archived"`) || !strings.Contains(text, `"closed":true`)) {
				t.Errorf("archived card not returned: %s", text)
			}
			if failed && strings.Contains(text, "Archived card") {
				t.Error("denied known-ID access exposed card contents")
			}
		})
	}
}
