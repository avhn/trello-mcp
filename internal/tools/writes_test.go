package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callTool(t *testing.T, s *Service, name, raw string) (string, bool) {
	t.Helper()
	for _, d := range s.definitions() {
		if d.name != name {
			continue
		}
		result, err := s.handler(d)(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(raw)}})
		if err != nil {
			t.Fatal(err)
		}
		if result.StructuredContent != nil || len(result.Content) != 1 {
			t.Fatalf("not compact text-only: %+v", result)
		}
		return result.Content[0].(*mcp.TextContent).Text, result.IsError
	}
	t.Fatalf("tool %s missing", name)
	return "", true
}

// TestWriteTools asserts authorization ordering, exact bodies and curated output for every JSON mutation.
func TestWriteTools(t *testing.T) {
	card := objectRequests("cards", "c1", "card")
	list := objectRequests("lists", "l1", "list")
	label := objectRequests("labels", "label1", "label")
	checklist := objectRequests("checklists", "cl1", "checklist")
	loadedChecklist := append(append([]requestSpec{}, checklist...), requestSpec{"/checklists/cl1", query("fields", checklistFields, "checkItems", "all", "checkItem_fields", itemFields), "checklist"})
	loadedChecklist = append(loadedChecklist, card...)
	member := requestSpec{"/boards/b1/members", query("fields", "id,username,fullName"), "members"}
	comment := append(append([]requestSpec{}, card...), requestSpec{"/actions/a1", query("fields", "data"), "comment"})
	comment = append(comment, boardRequests()...)
	comment = append(comment, card...)
	comment = append(comment, requestSpec{"/actions/a1", query("fields", "id,type,data"), "comment"})
	batchPreflight := append(append([]requestSpec{}, list...), list...)
	batchPreflight = append(batchPreflight, list...)
	const compact = `{"id":"new1","name":"Created","url":"https://trello.com/c/new1"}`
	cases := []struct {
		name, args, method, path, body, want string
		reads                                []requestSpec
	}{
		{"board_create", `{"name":"New","desc":"Plan","defaultLists":false}`, "POST", "/boards", `{"name":"New","desc":"Plan","defaultLists":false,"idOrganization":"w1"}`, compact, []requestSpec{{"/organizations/w1", query("fields", "id,name,displayName"), "workspace"}}},
		{"list_create", `{"name":"New","pos":"top"}`, "POST", "/lists", `{"name":"New","pos":"top","idBoard":"b1"}`, compact, boardRequests()},
		{"list_update", `{"listId":"l1","idBoard":"b1","closed":false,"name":"New","pos":2}`, "PUT", "/lists/l1", `{"idBoard":"b1","closed":false,"name":"New","pos":"2"}`, compact, append(append([]requestSpec{}, list...), boardRequests()...)},
		{"list_archive", `{"listId":"l1"}`, "PUT", "/lists/l1/closed", `{"value":true}`, compact, list},
		{"list_watch", `{"listId":"l1","subscribed":false}`, "PUT", "/lists/l1", `{"subscribed":false}`, compact, list},
		{"card_create", `{"listId":"l1","name":"New","idMembers":["m1"],"idLabels":["label1"]}`, "POST", "/cards", `{"idList":"l1","name":"New","idMembers":["m1"],"idLabels":["label1"]}`, compact, append(append(append([]requestSpec{}, list...), label...), member)},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"One"},{"name":"Two"}]}`, "POST", "/cards", `{"idList":"l1","name":"One"}`, `{"items":[{"index":0,"id":"new1"},{"index":1,"id":"new1"}]}`, batchPreflight},
		{"card_update", `{"cardId":"c1","name":"New","desc":"","dueComplete":false,"idLabels":[],"idMembers":[]}`, "PUT", "/cards/c1", `{"name":"New","desc":"","dueComplete":false,"idLabels":[],"idMembers":[]}`, compact, card},
		{"card_archive", `{"cardId":"c1"}`, "PUT", "/cards/c1", `{"closed":true}`, compact, card},
		{"card_watch", `{"cardId":"c1"}`, "PUT", "/cards/c1", `{"subscribed":true}`, compact, card},
		{"card_move", `{"cardId":"c1","listId":"l1"}`, "PUT", "/cards/c1", `{"idBoard":"b1","idList":"l1"}`, compact, append(append([]requestSpec{}, card...), list...)},
		{"card_copy", `{"cardId":"c1","listId":"l1","name":"Copy"}`, "POST", "/cards", `{"idCardSource":"c1","idList":"l1","name":"Copy"}`, compact, append(append([]requestSpec{}, card...), list...)},
		{"comment_create", `{"cardId":"c1","text":"Hello"}`, "POST", "/cards/c1/actions/comments", `{"text":"Hello"}`, compact, card},
		{"comment_update", `{"cardId":"c1","commentId":"a1","text":"Edited"}`, "PUT", "/cards/c1/actions/a1/comments", `{"text":"Edited"}`, compact, comment},
		{"comment_delete", `{"cardId":"c1","commentId":"a1"}`, "DELETE", "/cards/c1/actions/a1/comments", `null`, `{"ok":true}`, comment},
		{"checklist_create", `{"cardId":"c1","name":"Checks"}`, "POST", "/checklists", `{"idCard":"c1","name":"Checks"}`, compact, card},
		{"checklist_copy", `{"cardId":"c1","checklistId":"cl1"}`, "POST", "/checklists", `{"idCard":"c1","idChecklistSource":"cl1"}`, compact, append(append([]requestSpec{}, checklist...), card...)},
		{"checklist_item_create", `{"checklistId":"cl1","name":"Works","checked":false}`, "POST", "/checklists/cl1/checkItems", `{"name":"Works","checked":false}`, compact, loadedChecklist},
		{"checklist_item_update", `{"checklistId":"cl1","itemId":"i1","state":"incomplete"}`, "PUT", "/cards/c1/checkItem/i1", `{"state":"incomplete"}`, compact, loadedChecklist},
		{"checklist_item_delete", `{"checklistId":"cl1","itemId":"i1"}`, "DELETE", "/checklists/cl1/checkItems/i1", `null`, `{"ok":true}`, loadedChecklist},
		{"card_member_add", `{"cardId":"c1","memberId":"m1"}`, "POST", "/cards/c1/idMembers", `{"value":"m1"}`, `{"ok":true}`, append(append([]requestSpec{}, card...), member)},
		{"card_member_remove", `{"cardId":"c1","memberId":"m1"}`, "DELETE", "/cards/c1/idMembers/m1", `null`, `{"ok":true}`, append(append([]requestSpec{}, card...), member)},
		{"label_create", `{"name":"Ready","color":"green"}`, "POST", "/labels", `{"name":"Ready","color":"green","idBoard":"b1"}`, compact, boardRequests()},
		{"label_update", `{"labelId":"label1","name":"","color":"red"}`, "PUT", "/labels/label1", `{"name":"","color":"red"}`, compact, label},
		{"label_delete", `{"labelId":"label1"}`, "DELETE", "/labels/label1", `null`, `{"ok":true}`, label},
		{"customfield_set", `{"cardId":"c1","customFieldId":"f2","value":{"text":"Hello"}}`, "PUT", "/cards/c1/customField/f2/item", `{"value":{"text":"Hello"}}`, `{"ok":true}`, append(append(append([]requestSpec{}, card...), requestSpec{"/customFields/f2", query("fields", "idModel,modelType"), "customfield"}), append(boardRequests(), requestSpec{"/customFields/f2", query("fields", "id,name,type,options,idModel,modelType"), "customfield"})...)},
		{"attachment_add", `{"cardId":"c1","url":"https://example.invalid/image.png","name":"Image"}`, "POST", "/cards/c1/attachments", `{"url":"https://example.invalid/image.png","name":"Image"}`, compact, card},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			reads, writes := 0, 0
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				q := r.URL.Query()
				if q.Get("key") != "test-key" || q.Get("token") != "test-token" {
					t.Error("missing authentication")
				}
				q.Del("key")
				q.Del("token")
				if r.Method == "GET" {
					if reads >= len(tc.reads) {
						t.Errorf("unexpected GET %s", r.URL.Path)
						w.WriteHeader(500)
						return
					}
					expected := tc.reads[reads]
					reads++
					if r.URL.Path != expected.path || !reflect.DeepEqual(q, expected.query) {
						t.Errorf("GET %d: got %s %v; want %s %v", reads, r.URL.Path, q, expected.path, expected.query)
					}
					data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trello", expected.fixture+".json"))
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					_, _ = w.Write(data)
					return
				}
				writes++
				if tc.name != "card_create_batch" && reads != len(tc.reads) {
					t.Error("mutation before complete authorization")
				}
				if r.Method != tc.method || r.URL.Path != tc.path || len(q) != 0 || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected mutation %s %s %v", r.Method, r.URL.Path, q)
				}
				var got any
				if tc.method != "DELETE" {
					if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Error(err)
					}
				}
				var want any
				body := tc.body
				if tc.name == "card_create_batch" && writes == 2 {
					body = strings.ReplaceAll(body, "One", "Two")
				}
				if err := json.Unmarshal([]byte(body), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body got %#v; want %#v", got, want)
				}
				data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trello", "mutation.json"))
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = w.Write(data)
			}))
			defer fake.Close()
			s := testService(t, fake, []string{"w1"})
			text, failed := callTool(t, s, tc.name, tc.args)
			if failed || text != tc.want {
				t.Fatalf("got %s (failed=%v); want %s", text, failed, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			wantWrites := 1
			if tc.name == "card_create_batch" {
				wantWrites = 2
			}
			if reads != len(tc.reads) || writes != wantWrites {
				t.Errorf("requests: %d reads, %d writes", reads, writes)
			}
		})
	}
}

func TestChecklistItemFind(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checklists/cl1":
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trello", "checklist.json"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write(data)
		case "/cards/c1":
			_, _ = w.Write([]byte(cardJSON))
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	for filter, want := range map[string]string{"WORK": `{"items":[` + itemJSON + `]}`, "not present": `{"items":[]}`} {
		text, failed := callTool(t, s, "checklist_item_find", `{"checklistId":"cl1","nameFilter":"`+filter+`"}`)
		if failed || text != want {
			t.Errorf("filter %s: got %s", filter, text)
		}
	}
}

func TestBatchPerItemFailure(t *testing.T) {
	var mu sync.Mutex
	writes := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "POST" {
			writes++
			if writes == 1 {
				w.WriteHeader(500)
				_, _ = w.Write([]byte("test-key private upstream content"))
				return
			}
			_, _ = w.Write([]byte(`{"id":"new2","name":"must not be included"}`))
			return
		}
		switch r.URL.Path {
		case "/lists/l1":
			_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	text, failed := callTool(t, testService(t, fake, nil), "card_create_batch", `{"listId":"l1","cards":[{"name":"One"},{"name":"Two"}]}`)
	if failed || text != `{"items":[{"index":0,"error":"trello API: HTTP 500"},{"index":1,"id":"new2"}]}` {
		t.Fatalf("batch result %s", text)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 2 {
		t.Errorf("writes %d, want 2", writes)
	}
}

func TestWriteArgumentValidation(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"card_create_batch", `{"listId":"l1","cards":[]}`},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"x","name":"y"}]}`},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"x","listId":"l2"}]}`},
		{"card_create", `{"listId":"l1","name":"x","idLabels":["../label"]}`},
		{"card_create", `{"listId":"l1","name":"x","idMembers":["m1","m1"]}`},
		{"list_update", `{"listId":"l1","idBoard":"../b2"}`},
		{"list_update", `{"listId":"l1","closed":"false"}`},
		{"list_create", `{"name":"x","pos":"NaN"}`},
		{"list_create", `{"name":"x","pos":-1}`},
		{"card_update", `{"cardId":"c1","due":"tomorrow"}`},
		{"checklist_item_update", `{"checklistId":"cl1","itemId":"i1","state":"done"}`},
		{"comment_create", `{"cardId":"c1","text":" "}`},
	} {
		t.Run(tc.name+tc.raw, func(t *testing.T) {
			for _, d := range (&Service{}).definitions() {
				if d.name == tc.name {
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
