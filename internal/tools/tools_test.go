package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/trello"
)

const boardJSON = `{"id":"b1","name":"Release","url":"https://trello.com/b/b1","idOrganization":"w1"}`
const workspaceJSON = `{"id":"w1","name":"allowed","displayName":"Allowed Workspace"}`
const cardJSON = `{"id":"c1","name":"Fix login","url":"https://trello.com/c/c1","idBoard":"b1","idList":"l1","desc":"Synthetic card description","due":"2026-10-20T09:00:00Z","labels":[{"id":"label1","name":"Ready","color":"green"}],"idMembers":["m1"]}`
const itemJSON = `{"id":"i1","name":"Works","state":"complete","pos":1}`
const actionJSON = `{"id":"a1","type":"commentCard","date":"2026-10-15T12:00:00Z","data":{"text":"Looks good","card":{"id":"c1","name":"Fix login"},"board":{"id":"b1","name":"Release"}},"memberCreator":{"id":"m1","username":"synthetic","fullName":"Synthetic Member"}}`

// requestSpec describes every expected HTTP request, including authentication and field selection.
type requestSpec struct {
	path    string
	query   url.Values
	fixture string
}

// query converts concise key/value pairs into the exact expected query map.
func query(pairs ...string) url.Values {
	q := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		q.Set(pairs[i], pairs[i+1])
	}
	return q
}

// boardRequests captures the common ownership and workspace verification path.
func boardRequests() []requestSpec {
	return []requestSpec{{"/boards/b1", query("fields", "id,name,url,idOrganization,closed"), "board"}, {"/organizations/w1", query("fields", "id,name,displayName"), "workspace"}}
}

// objectRequests prepends actual object resolution, rather than trusting boardId in arguments.
func objectRequests(kind, id, fixture string) []requestSpec {
	fields := "idBoard"
	if kind == "checklists" {
		fields = "idBoard,idCard"
	}
	return append([]requestSpec{{"/" + kind + "/" + id, query("fields", fields), fixture}}, boardRequests()...)
}

// TestTools checks exact compact output and all upstream query parameters for every phase 1 tool.
func TestTools(t *testing.T) {
	actionQuery := func(filter string) url.Values {
		return query("fields", "id,type,date,data,idMemberCreator", "memberCreator", "true", "memberCreator_fields", "id,username,fullName", "filter", filter, "limit", "20")
	}
	cardActionRequests := func(filter string) []requestSpec {
		requests := append(objectRequests("cards", "c1", "card"), requestSpec{"/cards/c1/actions", actionQuery(filter), "actions"})
		return append(requests, objectRequests("cards", "c1", "card")...)
	}
	catalog := func(name, fields, fixture string) []requestSpec {
		q := query("fields", fields)
		if name == "labels" {
			q.Set("limit", "1000")
		}
		return append(boardRequests(), requestSpec{"/boards/b1/" + name, q, fixture})
	}
	cases := []struct {
		name, args, want string
		requests         []requestSpec
	}{
		{"workspace_find", `{}`, `{"items":[` + workspaceJSON + `]}`, []requestSpec{{"/members/me/organizations", query("fields", "id,name,displayName"), "workspaces"}}},
		{"workspace_use", `{"workspaceId":"w1"}`, workspaceJSON, []requestSpec{{"/organizations/w1", query("fields", "id,name,displayName"), "workspace"}}},
		{"board_find", `{}`, `{"items":[` + boardJSON + `]}`, []requestSpec{{"/members/me/boards", query("fields", "id,name,url,idOrganization,closed", "filter", "open"), "boards"}, {"/organizations/w1", query("fields", "id,name,displayName"), "workspace"}, {"/organizations/w2", query("fields", "id,name,displayName"), "workspace_other"}}},
		{"board_use", `{"boardId":"b1"}`, boardJSON, boardRequests()},
		{"board_get", `{}`, boardJSON, boardRequests()},
		{"board_activity", `{}`, `{"items":[` + actionJSON + `]}`, append(boardRequests(), requestSpec{"/boards/b1/actions", actionQuery("all"), "actions"})},
		{"list_find", `{}`, `{"items":[{"id":"l1","name":"Doing","idBoard":"b1","pos":1}]}`, append(boardRequests(), requestSpec{"/boards/b1/lists", query("fields", "id,name,idBoard,closed,pos", "filter", "open"), "lists"})},
		{"card_find", `{"listId":"l1","nameFilter":"login","limit":1}`, `{"items":[` + cardJSON + `]}`, append(objectRequests("lists", "l1", "list"), requestSpec{"/lists/l1/cards", query("fields", cardFields, "filter", "open"), "cards"})},
		{"card_get", `{"cardId":"c1"}`, cardJSON, append(objectRequests("cards", "c1", "card"), requestSpec{"/cards/c1", query("fields", cardFields), "card"})},
		{"card_history", `{"cardId":"c1"}`, `{"items":[` + actionJSON + `]}`, cardActionRequests("all")},
		{"comment_find", `{"cardId":"c1"}`, `{"items":[` + actionJSON + `]}`, cardActionRequests("commentCard")},
		{"checklist_items", `{"checklistId":"cl1"}`, `{"items":[` + itemJSON + `]}`, append(objectRequests("checklists", "cl1", "checklist"), requestSpec{"/checklists/cl1", query("fields", checklistFields, "checkItems", "all", "checkItem_fields", itemFields), "checklist"})},
		{"checklist_get", `{"cardId":"c1"}`, `{"id":"cl1","name":"Acceptance Criteria","idBoard":"b1","idCard":"c1","checkItems":[` + itemJSON + `]}`, append(objectRequests("cards", "c1", "card"), requestSpec{"/cards/c1/checklists", query("fields", checklistFields, "checkItems", "all", "checkItem_fields", itemFields), "checklists"})},
		{"member_find", `{}`, `{"items":[{"id":"m1","username":"synthetic","fullName":"Synthetic Member"}]}`, catalog("members", "id,username,fullName", "members")},
		{"label_find", `{}`, `{"items":[{"id":"label1","name":"Ready","color":"green"}]}`, catalog("labels", "id,name,color", "labels")},
		{"customfield_find", `{}`, `{"items":[{"id":"f1","name":"Stage","type":"list","options":[{"id":"o1","value":{"text":"Ready"},"color":"green","pos":1}]},{"id":"f2","name":"Text","type":"text"},{"id":"f3","name":"Number","type":"number"},{"id":"f4","name":"Date","type":"date"},{"id":"f5","name":"Checkbox","type":"checkbox"}]}`, catalog("customFields", "id,name,type,options", "customfields")},
		{"server_health", `{}`, `{"ok":true}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index := 0
			var mu sync.Mutex
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if index >= len(tc.requests) {
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				expected := tc.requests[index]
				index++
				q := r.URL.Query()
				if q.Get("key") != "test-key" || q.Get("token") != "test-token" {
					t.Error("authentication query missing")
				}
				q.Del("key")
				q.Del("token")
				if r.Method != "GET" || r.URL.Path != expected.path || !reflect.DeepEqual(q, expected.query) {
					t.Errorf("request %d: got %s %s %v; want GET %s %v", index, r.Method, r.URL.Path, q, expected.path, expected.query)
				}
				data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trello", expected.fixture+".json"))
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				_, _ = w.Write(data)
			}))
			defer fake.Close()
			service := testService(t, fake, []string{"w1"})
			var d definition
			for _, candidate := range service.definitions() {
				if candidate.name == tc.name {
					d = candidate
				}
			}
			result, err := service.handler(d)(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(tc.args)}})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError || result.StructuredContent != nil || len(result.Content) != 1 {
				t.Fatalf("unexpected result: %+v", result)
			}
			text, ok := result.Content[0].(*mcp.TextContent)
			if !ok || text.Text != tc.want {
				t.Fatalf("output got %#v, want %s", result.Content[0], tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if index != len(tc.requests) {
				t.Errorf("got %d requests, want %d", index, len(tc.requests))
			}
			if tc.name == "workspace_use" && service.state.Get().BoardID != "" {
				t.Error("workspace selection did not clear active board")
			}
		})
	}
}

// testService provides synthetic credentials and an isolated state file.
func testService(t *testing.T, fake *httptest.Server, allowed []string) *Service {
	t.Helper()
	ctx := context.Background()
	store, err := state.Open(ctx, filepath.Join(t.TempDir(), "trello-mcp", "state.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(ctx, state.Selection{BoardID: "b1", WorkspaceID: "w1"}); err != nil {
		t.Fatal(err)
	}
	return New(config.Config{AllowedWorkspaces: allowed, Tools: []string{"all"}}, trello.NewClient(fake.URL, "test-key", "test-token", fake.Client()), store)
}

// TestValidation ensures untrusted raw handlers reject malformed input without any HTTP calls.
func TestValidation(t *testing.T) {
	cases := []struct{ tool, raw string }{
		{"card_get", `{}`}, {"card_get", `{"cardId":"../bad"}`}, {"card_get", `{"cardId":"c1","boardId":"b1?key=x"}`}, {"card_find", `{"limit":0}`}, {"card_find", `{"limit":1001}`}, {"card_find", `{"limit":1.5}`}, {"card_find", `{"mine":"yes"}`}, {"card_find", `{"mine":true,"listId":"l1"}`}, {"board_get", `{"detail":"all"}`}, {"server_health", `{"detail":"full"}`}, {"card_get", `{"cardId":"c1","extraFields":["attachments"]}`}, {"card_get", `{"cardId":"c1","extraFields":["votes","votes"]}`}, {"card_get", `{"cardId":"c1","view":"html"}`}, {"board_activity", `{"before":"../bad"}`}, {"card_history", `{"cardId":"c1","filter":"updateCard&key=bad"}`}, {"server_health", `null`}, {"server_health", `[]`}, {"server_health", `{} {}`}, {"card_get", `{"cardId":null}`}, {"card_get", `{"cardId":"c1","cardId":"c2"}`}, {"board_get", `{"extra":true}`},
	}
	service := &Service{}
	for i, tc := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			for _, d := range service.definitions() {
				if d.name == tc.tool {
					if _, err := validate(json.RawMessage(tc.raw), d); err == nil {
						t.Errorf("accepted %s", tc.raw)
					}
					return
				}
			}
			t.Fatal("tool missing")
		})
	}
}

// TestRegistration verifies filters through the real MCP tools/list and tools/call protocol.
func TestRegistration(t *testing.T) {
	for _, tc := range []struct {
		selectors []string
		count     int
	}{{[]string{"all"}, 17}, {[]string{"card"}, 3}, {[]string{"board_get", "server"}, 2}} {
		t.Run(strings.Join(tc.selectors, ","), func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
			defer fake.Close()
			service := testService(t, fake, nil)
			service.config.Tools = tc.selectors
			service.config.ReadOnly = true
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			if err := service.Register(server); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a, b := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ss.Close() }()
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			cs, err := client.Connect(ctx, b, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cs.Close() }()
			result, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Tools) != tc.count {
				t.Fatalf("got %d tools, want %d", len(result.Tools), tc.count)
			}
			for _, tool := range result.Tools {
				if tool.OutputSchema != nil {
					t.Errorf("output schema on %s", tool.Name)
				}
			}
			if tc.count == 17 {
				response, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "server_health", Arguments: map[string]any{}})
				if err != nil {
					t.Fatal(err)
				}
				if response.StructuredContent != nil || response.Content[0].(*mcp.TextContent).Text != `{"ok":true}` {
					t.Errorf("not text-only: %+v", response)
				}
			}
		})
	}
}
