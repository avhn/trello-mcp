package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

var writeArguments = map[string]string{
	"board_create": `{"name":"New"}`, "list_create": `{"name":"New"}`,
	"list_update": `{"listId":"l1","closed":false}`, "list_archive": `{"listId":"l1"}`, "list_watch": `{"listId":"l1"}`,
	"card_create": `{"listId":"l1","name":"New"}`, "card_create_batch": `{"listId":"l1","cards":[{"name":"New"}]}`,
	"card_update": `{"cardId":"c1","name":"New"}`, "card_archive": `{"cardId":"c1"}`, "card_watch": `{"cardId":"c1"}`,
	"card_move": `{"cardId":"c1","listId":"l1"}`, "card_copy": `{"cardId":"c1","listId":"l1"}`,
	"comment_create": `{"cardId":"c1","text":"Hello"}`, "comment_update": `{"cardId":"c1","commentId":"a1","text":"Edit"}`, "comment_delete": `{"cardId":"c1","commentId":"a1"}`,
	"checklist_create": `{"cardId":"c1","name":"Checks"}`, "checklist_copy": `{"cardId":"c1","checklistId":"cl1"}`,
	"checklist_item_create": `{"checklistId":"cl1","name":"Works"}`, "checklist_item_update": `{"checklistId":"cl1","itemId":"i1","state":"complete"}`, "checklist_item_delete": `{"checklistId":"cl1","itemId":"i1"}`,
	"card_member_add": `{"cardId":"c1","memberId":"m1"}`, "card_member_remove": `{"cardId":"c1","memberId":"m1"}`,
	"label_create": `{"name":"Ready"}`, "label_update": `{"labelId":"label1","color":"green"}`, "label_delete": `{"labelId":"label1"}`,
	"customfield_set": `{"cardId":"c1","customFieldId":"f1","value":{"text":"Hello"}}`,
	"attachment_add":  `{"cardId":"c1","url":"https://example.invalid/file"}`, "attachment_upload": `{"cardId":"c1","base64":"aGk=","fileName":"hi.txt"}`, "attachment_download": `{"cardId":"c1","attachmentId":"at1"}`,
}

func TestEveryWriteRejectsDeniedOwner(t *testing.T) {
	for _, d := range (&Service{}).definitions() {
		if !d.write {
			continue
		}
		t.Run(d.name, func(t *testing.T) {
			raw, ok := writeArguments[d.name]
			if !ok {
				t.Fatal("missing write security case")
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("mutation before authorization")
					w.WriteHeader(500)
					return
				}
				switch {
				case strings.HasPrefix(r.URL.Path, "/organizations/"):
					_, _ = fmt.Fprintf(w, `{"id":%q,"name":"denied"}`, strings.TrimPrefix(r.URL.Path, "/organizations/"))
				case strings.HasPrefix(r.URL.Path, "/boards/"):
					_, _ = w.Write([]byte(`{"id":"b1","idOrganization":"w2"}`))
				default:
					_, _ = w.Write([]byte(`{"idBoard":"b1","idCard":"c1","idModel":"b1","modelType":"board"}`))
				}
			}))
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, []string{"allowed"}), d.name, raw)
			if !failed || !strings.Contains(text, "access denied by TRELLO_ALLOWED_WORKSPACES") {
				t.Fatalf("not denied at ownership boundary: %s", text)
			}
		})
	}
}

// TestBothEndsChecked covers source and destination denial for each move/copy variant.
func TestBothEndsChecked(t *testing.T) {
	for _, tc := range []struct{ tool, args, sourcePath, destPath string }{
		{"list_update", `{"listId":"l1","idBoard":"b2"}`, "/lists/l1", "/boards/b2"},
		{"card_move", `{"cardId":"c1","listId":"l2"}`, "/cards/c1", "/lists/l2"},
		{"card_copy", `{"cardId":"c1","listId":"l2"}`, "/cards/c1", "/lists/l2"},
		{"checklist_copy", `{"checklistId":"cl1","cardId":"c2"}`, "/checklists/cl1", "/cards/c2"},
	} {
		for _, denied := range []string{"source", "destination"} {
			t.Run(tc.tool+"/"+denied, func(t *testing.T) {
				var writes atomic.Int32
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						writes.Add(1)
						w.WriteHeader(500)
						return
					}
					switch r.URL.Path {
					case tc.sourcePath:
						_, _ = w.Write([]byte(`{"idBoard":"b1","idCard":"c1"}`))
					case tc.destPath:
						if tc.destPath == "/boards/b2" {
							workspace := "w1"
							if denied == "destination" {
								workspace = "w2"
							}
							_, _ = fmt.Fprintf(w, `{"id":"b2","idOrganization":%q}`, workspace)
						} else {
							_, _ = w.Write([]byte(`{"idBoard":"b2"}`))
						}
					case "/boards/b1", "/boards/b2":
						id := strings.TrimPrefix(r.URL.Path, "/boards/")
						workspace := "w1"
						if (id == "b1" && denied == "source") || (id == "b2" && denied == "destination") {
							workspace = "w2"
						}
						_, _ = fmt.Fprintf(w, `{"id":%q,"idOrganization":%q}`, id, workspace)
					case "/organizations/w1":
						_, _ = w.Write([]byte(workspaceJSON))
					case "/organizations/w2":
						_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
					default:
						t.Errorf("unexpected route %s", r.URL.Path)
						w.WriteHeader(500)
					}
				}))
				defer fake.Close()
				text, failed := callTool(t, testService(t, fake, []string{"w1"}), tc.tool, tc.args)
				if !failed || !strings.Contains(text, "access denied by TRELLO_ALLOWED_WORKSPACES") || writes.Load() != 0 {
					t.Fatalf("unchecked %s: %s writes=%d", denied, text, writes.Load())
				}
			})
		}
	}
}

func TestReferencedObjectsFailClosed(t *testing.T) {
	for _, tc := range []struct{ tool, args, foreignPath, response string }{
		{"card_create", `{"listId":"l1","name":"New","idLabels":["label1"]}`, "/labels/label1", `{"idBoard":"b2"}`},
		{"card_update", `{"cardId":"c1","idMembers":["m2"]}`, "/boards/b1/members", `[]`},
		{"card_member_add", `{"cardId":"c1","memberId":"m2"}`, "/boards/b1/members", `[]`},
		{"customfield_set", `{"cardId":"c1","customFieldId":"f1","value":""}`, "/customFields/f1", `{"idModel":"b2","modelType":"board"}`},
		{"comment_delete", `{"cardId":"c1","commentId":"a1"}`, "/actions/a1", `{"id":"a1","type":"commentCard","data":{"board":{"id":"b1"},"card":{"id":"c2"}}}`},
		{"checklist_item_delete", `{"checklistId":"cl1","itemId":"foreign"}`, "/checklists/cl1", `{"id":"cl1","idBoard":"b1","idCard":"c1","checkItems":[{"id":"i1"}]}`},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"First"},{"name":"Denied","idLabels":["label1"]}]}`, "/labels/label1", `{}`},
		{"list_update", `{"listId":"l1","idBoard":"b2"}`, "/boards/b2", `{}`},
	} {
		t.Run(tc.tool+tc.foreignPath, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unverifiable object mutated")
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == tc.foreignPath {
					_, _ = w.Write([]byte(tc.response))
					return
				}
				switch r.URL.Path {
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/cards/c1", "/cards/c2", "/lists/l1", "/checklists/cl1":
					_, _ = w.Write([]byte(`{"idBoard":"b1","idCard":"c1"}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, nil), tc.tool, tc.args)
			if !failed {
				t.Fatalf("foreign or unverifiable reference accepted: %s", text)
			}
		})
	}
}
