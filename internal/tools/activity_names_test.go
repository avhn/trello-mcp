package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestActivityUnknownNamesNoLookup(t *testing.T) {
	const firstPage = `[
		{"id":"a3","type":"commentCard","date":"2026-10-15T12:00:00Z","data":{"card":{"id":"c1"},"list":{"id":"l1"},"text":"Keep this text.\nNext line."}},
		{"id":"a2","type":"commentCard","date":"2026-10-15T11:00:00Z","data":{"card":{"id":"c1","name":"Example"},"list":{"id":"l1","name":"Doing"}}},
		{"id":"a1","type":"commentCard","date":"2026-10-15T10:00:00Z","data":{"card":{"id":"c1","name":"Example"},"list":{"id":"l1","name":"Doing"}}}
	]`
	const secondPage = `[{"id":"a0","type":"commentCard","date":"2026-10-15T09:00:00Z","data":{"card":{"id":"c1"},"list":{"id":"l1"}}}]`
	want := []string{
		`{"items":[{"id":"a3","type":"commentCard","date":"2026-10-15T12:00:00Z","cardId":"c1","text":"Keep this text.\nNext line.","listId":"l1"},{"id":"a2","type":"commentCard","date":"2026-10-15T11:00:00Z","cardId":"c1","card":"Example","listId":"l1","list":"Doing"},{"id":"a1","type":"commentCard","date":"2026-10-15T10:00:00Z","cardId":"c1","listId":"l1"}],"next":"a1"}`,
		`{"items":[{"id":"a0","type":"commentCard","date":"2026-10-15T09:00:00Z","cardId":"c1","listId":"l1"}]}`,
	}
	for _, tool := range []string{"board_activity", "card_history", "comment_find"} {
		t.Run(tool, func(t *testing.T) {
			filter := "updateCard,commentCard"
			path := "/cards/c1/actions"
			switch tool {
			case "board_activity":
				path = "/boards/b1/actions"
			case "comment_find":
				filter = "commentCard"
			}
			var requests []requestSpec
			for page, fixture := range []string{"first", "second"} {
				if tool == "board_activity" {
					requests = append(requests, boardRequests()...)
				} else {
					requests = append(requests, objectRequests("cards", "c1", "card")...)
				}
				q := query("fields", "id,type,date,data,idMemberCreator", "memberCreator", "true", "memberCreator_fields", "id,username", "filter", filter, "limit", "3", "since", "2026-10-01T00:00:00Z")
				if page == 1 {
					q.Set("before", "a1")
				}
				requests = append(requests, requestSpec{path, q, fixture})
				if tool != "board_activity" {
					requests = append(requests, objectRequests("cards", "c1", "card")...)
				}
			}
			fixtures := map[string]string{"board": boardJSON, "workspace": workspaceJSON, "card": cardJSON, "first": firstPage, "second": secondPage}
			index := 0
			var mu sync.Mutex
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if index >= len(requests) {
					t.Errorf("unexpected name lookup or other request: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				expected := requests[index]
				index++
				q := r.URL.Query()
				if q.Get("key") != "test-key" || q.Get("token") != "test-token" {
					t.Error("authentication query missing")
				}
				q.Del("key")
				q.Del("token")
				if r.Method != http.MethodGet || r.URL.Path != expected.path || !reflect.DeepEqual(q, expected.query) {
					t.Errorf("request %d: got %s %s %v; want GET %s %v", index, r.Method, r.URL.Path, q, expected.path, expected.query)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(fixtures[expected.fixture]))
			}))
			defer fake.Close()
			s := testService(t, fake, []string{"w1"})
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			if err := s.Register(server); err != nil {
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
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, b, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cs.Close() }()
			args := map[string]any{"boardId": "b1", "limit": 3, "since": "2026-10-01T00:00:00Z"}
			if tool != "board_activity" {
				args["cardId"] = "c1"
			}
			if tool != "comment_find" {
				args["filter"] = filter
			}
			for page := range 2 {
				if page == 1 {
					args["before"] = "a1"
				}
				result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				if result.IsError || result.StructuredContent != nil || len(result.Content) != 1 {
					t.Fatalf("page %d: not a successful text-only result: %+v", page, result)
				}
				text, ok := result.Content[0].(*mcp.TextContent)
				if !ok || text.Text != want[page] {
					t.Fatalf("page %d: got %#v, want %s", page, result.Content[0], want[page])
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if index != len(requests) {
				t.Errorf("got %d requests, want %d including ownership rechecks", index, len(requests))
			}
		})
	}
}
