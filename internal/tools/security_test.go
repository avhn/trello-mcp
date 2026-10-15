package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAllowlistDeniesEveryScopedTool checks that explicit and default selections cannot bypass ownership.
func TestAllowlistDeniesEveryScopedTool(t *testing.T) {
	cases := []struct{ name, args string }{
		{"workspace_use", `{"workspaceId":"w2"}`}, {"board_find", `{"workspaceId":"w2"}`}, {"board_use", `{"boardId":"b1"}`}, {"board_get", `{}`}, {"board_activity", `{}`}, {"list_find", `{}`}, {"card_find", `{"listId":"l1","boardId":"b1"}`}, {"card_get", `{"cardId":"c1","boardId":"b1"}`}, {"card_history", `{"cardId":"c1"}`}, {"attachment_find", `{"cardId":"c1","boardId":"b1"}`}, {"comment_find", `{"cardId":"c1"}`}, {"checklist_items", `{"checklistId":"cl1"}`}, {"checklist_get", `{"cardId":"c1"}`}, {"member_find", `{}`}, {"label_find", `{}`}, {"customfield_find", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/cards/c1", "/lists/l1", "/checklists/cl1":
					_, _ = w.Write([]byte(`{"idBoard":"b1","idCard":"c1"}`))
				case "/boards/b1":
					_, _ = w.Write([]byte(`{"id":"b1","idOrganization":"w2","name":"private name"}`))
				case "/organizations/w2":
					_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
				default:
					t.Errorf("data fetched before authorization: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			service := testService(t, fake, []string{"allowed"})
			before := service.state.Get()
			for _, d := range service.definitions() {
				if d.name == tc.name {
					result, err := service.handler(d)(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(tc.args)}})
					if err != nil {
						t.Fatal(err)
					}
					if !result.IsError || result.Content[0].(*mcp.TextContent).Text != `{"error":"workspace access denied"}` {
						t.Fatalf("authorization result: %+v", result)
					}
				}
			}
			if service.state.Get() != before {
				t.Error("denied call changed state")
			}
		})
	}
}

// TestOwnerMismatch rejects supplied-board spoofing and missing object ownership.
func TestOwnerMismatch(t *testing.T) {
	for _, payload := range []string{`{"idBoard":"b2"}`, `{}`} {
		t.Run(payload, func(t *testing.T) {
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/cards/c1" {
					t.Error("unexpected ownership request")
				}
				_, _ = w.Write([]byte(payload))
			}))
			defer fake.Close()
			service := testService(t, fake, nil)
			a := arguments{strings: map[string]string{"cardId": "c1", "boardId": "b1"}}
			if _, err := service.cardGet(context.Background(), a); err == nil {
				t.Error("unverifiable/mismatched ownership accepted")
			}
			if requests.Load() != 1 {
				t.Errorf("got %d requests", requests.Load())
			}
		})
	}
}

// TestLocalHealthModes never leaks stale denied identifiers and performs no Trello requests.
func TestLocalHealthModes(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("health made a network call")
		w.WriteHeader(500)
	}))
	defer fake.Close()
	service := testService(t, fake, []string{"different"})
	for _, mode := range []string{"basic", "detailed", "metadata", "performance"} {
		result, err := service.serverHealth(context.Background(), arguments{strings: map[string]string{"detail": mode}})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "b1") || strings.Contains(string(data), "w1") || strings.Contains(string(data), "test-key") {
			t.Errorf("health leaked local identifiers: %s", data)
		}
	}
}

// TestUnknownFilter fails registration rather than silently widening the exposed tools.
func TestUnknownFilter(t *testing.T) {
	service := &Service{}
	service.config.Tools = []string{"typo"}
	if err := service.Register(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)); err == nil {
		t.Error("unknown selector accepted")
	}
}
