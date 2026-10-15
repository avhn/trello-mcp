package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/view"
)

type verificationFixture struct {
	response, fallback string
	status             int
	fallbackStatus     int
	denyFallback       bool
	writes, dataReads  atomic.Int32
	parentReads        atomic.Int32
	queries            chan url.Values
	dataQueries        chan url.Values
}

// server separates ownership requests from the single permitted verification data GET.
func (f *verificationFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	f.queries = make(chan url.Values, 2)
	f.dataQueries = make(chan url.Values, 2)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Del("key")
		q.Del("token")
		if r.Method != http.MethodGet {
			f.writes.Add(1)
			f.queries <- q
			if r.Header.Get("Content-Type") != "" || r.ContentLength > 0 {
				t.Error("verified mutation must use query parameters only")
			}
			if f.status != 0 {
				w.WriteHeader(f.status)
			}
			_, _ = w.Write([]byte(f.response))
			return
		}
		fields := q.Get("fields")
		switch {
		case r.URL.Path == "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		case r.URL.Path == "/organizations/w1":
			_, _ = w.Write([]byte(`{"id":"w1","name":"synthetic"}`))
		case r.URL.Path == "/boards/b1/members":
			_, _ = w.Write([]byte(`[{"id":"m1"}]`))
		case r.URL.Path == "/checklists/cl1" && fields == "idBoard,idCard":
			f.parentReads.Add(1)
			if f.writes.Load() > 0 && f.denyFallback {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"idBoard":"b1","idCard":"c1"}`))
		case r.URL.Path == "/checklists/cl1" && fields == checklistFields:
			_, _ = w.Write([]byte(`{"id":"cl1","idBoard":"b1","idCard":"c1","checkItems":[{"id":"i1","name":"Item","state":"incomplete"}]}`))
		case (r.URL.Path == "/cards/c1" || r.URL.Path == "/labels/label1" || r.URL.Path == "/cards/new1" || r.URL.Path == "/labels/new1") && fields == "idBoard":
			f.parentReads.Add(1)
			if f.writes.Load() > 0 && f.denyFallback {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
		case r.URL.Path == "/lists/l1":
			_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
		case r.URL.Path == "/cards/c1/checkItem/i1" || r.URL.Path == "/cards/c1" || r.URL.Path == "/labels/label1" || r.URL.Path == "/cards/new1" || r.URL.Path == "/labels/new1":
			f.dataReads.Add(1)
			f.dataQueries <- q
			if q.Get("fields") == "" || strings.Contains(fields, "all") {
				t.Error("unbounded verification selection")
			}
			if f.fallbackStatus != 0 {
				w.WriteHeader(f.fallbackStatus)
			}
			_, _ = w.Write([]byte(f.fallback))
		default:
			t.Errorf("unexpected verification route %s with fields %s", r.URL.Path, fields)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func TestClearMutationVerification(t *testing.T) {
	for _, target := range []struct{ tool, args, id, field string }{
		{"card_update", `{"cardId":"c1","due":""}`, "c1", "due"},
		{"card_update", `{"cardId":"c1","start":""}`, "c1", "start"},
		{"label_update", `{"labelId":"label1","color":""}`, "label1", "color"},
	} {
		for _, tc := range []struct {
			name, value, fallback string
			failed                bool
			dataReads             int32
			deny                  bool
		}{
			{"explicit null", "null", "", false, 0, false},
			{"empty string", `""`, "", false, 0, false},
			{"retained value", `"retained"`, "", true, 0, false},
			{"malformed field", `{}`, "", true, 0, false},
			{"missing fallback cleared", "", "null", false, 1, false},
			{"missing fallback retained", "", `"retained"`, true, 1, false},
			{"missing fallback missing", "", "", true, 1, false},
			{"missing fallback denied", "", "null", true, 0, true},
		} {
			t.Run(target.field+"/"+tc.name, func(t *testing.T) {
				response := map[string]any{"id": target.id, "idBoard": "b1", "name": "Curated", "private": "drop"}
				fallback := map[string]any{"id": target.id, "idBoard": "b1"}
				if tc.value != "" {
					response[target.field] = json.RawMessage(tc.value)
				}
				if tc.fallback != "" {
					fallback[target.field] = json.RawMessage(tc.fallback)
				}
				raw, _ := json.Marshal(response)
				readback, _ := json.Marshal(fallback)
				fixture := verificationFixture{response: string(raw), fallback: string(readback), denyFallback: tc.deny}
				fake := fixture.server(t)
				defer fake.Close()
				text, failed := callTool(t, testService(t, fake, []string{"w1"}), target.tool, target.args)
				if failed != tc.failed || strings.Contains(text, "private") {
					t.Fatalf("got %s (failed=%v), want failed=%v", text, failed, tc.failed)
				}
				var out struct {
					ID          string   `json:"id"`
					Unconfirmed []string `json:"unconfirmed"`
				}
				if err := json.Unmarshal([]byte(text), &out); err != nil || out.ID != target.id {
					t.Fatalf("confirmed identity lost: %s", text)
				}
				if tc.failed && (!reflect.DeepEqual(out.Unconfirmed, []string{target.field}) || !strings.Contains(text, "inspect before retrying")) {
					t.Fatalf("partial result %s", text)
				}
				if fixture.writes.Load() != 1 || fixture.dataReads.Load() != tc.dataReads {
					t.Fatalf("requests: writes=%d data GETs=%d", fixture.writes.Load(), fixture.dataReads.Load())
				}
				if got := (<-fixture.queries).Get(target.field); got != "null" {
					t.Errorf("wire %s=%q, want null", target.field, got)
				}
				if tc.dataReads > 0 {
					if got := <-fixture.dataQueries; !reflect.DeepEqual(got, query("fields", "id,idBoard,"+target.field)) {
						t.Errorf("verification selection %v", got)
					}
				}
			})
		}
	}
}

func TestClearQueryPreservesMixedUpdates(t *testing.T) {
	fixture := verificationFixture{response: `{"id":"c1","due":null,"start":null}`}
	fake := fixture.server(t)
	defer fake.Close()
	text, failed := callTool(t, testService(t, fake, nil), "card_update", `{"cardId":"c1","name":"New","desc":"","due":"","start":"","dueComplete":false,"closed":false,"pos":2.5,"idLabels":[],"idMembers":[]}`)
	if failed || text != `{"id":"c1"}` {
		t.Fatalf("result %s", text)
	}
	want := query("name", "New", "desc", "", "due", "null", "start", "null", "dueComplete", "false", "closed", "false", "pos", "2.5", "idLabels", "", "idMembers", "")
	if got := <-fixture.queries; !reflect.DeepEqual(got, want) {
		t.Fatalf("query %v, want %v", got, want)
	}
	q, err := mutationQuery(map[string]any{"ids": []string{"m1", "m2"}, "n": 1.25, "pos": json.Number("123.5"), "empty": ""})
	if err != nil || q.Get("ids") != "m1,m2" || q.Get("n") != "1.25" || q.Get("pos") != "123.5" || !q.Has("empty") {
		t.Fatalf("query encoding %v, %v", q, err)
	}
	if _, err = mutationQuery(map[string]any{"invalid": map[string]any{}}); err == nil {
		t.Fatal("unsupported query value accepted")
	}
}

func TestMutationResponseIdentityAndOwnership(t *testing.T) {
	for _, response := range []string{
		`null`, `{}`, `{"id":null}`, `{"id":""}`, `{"id":"../bad"}`, `{"id":"other"}`, `{"id":42}`, `[]`, `broken`,
		`{"id":"c1","idBoard":"foreign","due":null}`,
		`{"id":"c1","idBoard":null,"due":null}`,
		`{"id":"c1","idCard":"foreign","due":null}`,
	} {
		t.Run(response, func(t *testing.T) {
			fixture := verificationFixture{response: response}
			fake := fixture.server(t)
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, nil), "card_update", `{"cardId":"c1","due":""}`)
			if !failed || !strings.Contains(text, "mutation may have applied") || !strings.Contains(text, "inspect before retrying") {
				t.Fatalf("unsafe acknowledgment %s", text)
			}
			if fixture.writes.Load() != 1 || fixture.dataReads.Load() != 0 {
				t.Fatal("ambiguous response retried or read back")
			}
		})
	}
}

func TestVerificationFallbackFailure(t *testing.T) {
	for _, tc := range []struct {
		name, fallback string
		status         int
	}{
		{"http error", `private test-key test-token`, http.StatusForbidden},
		{"invalid JSON", `broken`, 0},
		{"null", `null`, 0},
		{"wrong identity", `{"id":"foreign","due":null}`, 0},
		{"wrong board", `{"id":"c1","idBoard":"foreign","due":null}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := verificationFixture{response: `{"id":"c1"}`, fallback: tc.fallback, fallbackStatus: tc.status}
			fake := fixture.server(t)
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, nil), "card_update", `{"cardId":"c1","due":""}`)
			if !failed || !strings.Contains(text, `"id":"c1"`) || !strings.Contains(text, `"unconfirmed":["due"]`) || strings.Contains(text, "private") || strings.Contains(text, "test-key") || strings.Contains(text, "test-token") {
				t.Fatalf("partial progress/error redaction %s", text)
			}
			if fixture.writes.Load() != 1 || fixture.dataReads.Load() != 1 {
				t.Fatal("unbounded verification or duplicate mutation")
			}
		})
	}
}

func TestClearSetReadStateful(t *testing.T) {
	for _, field := range []string{"due", "start", "color"} {
		t.Run(field, func(t *testing.T) {
			value := ""
			writes, dataReads := 0, 0
			kind, id, tool, key, set := "cards", "c1", "card_update", "cardId", "2027-02-03T12:00:00Z"
			if field == "color" {
				kind, id, tool, key, set = "labels", "label1", "label_update", "labelId", "red"
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/boards/b1" {
					_, _ = w.Write([]byte(boardJSON))
					return
				}
				if r.URL.Path != "/"+kind+"/"+id {
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				if r.Method == http.MethodGet && r.URL.Query().Get("fields") == "idBoard" {
					_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
					return
				}
				if r.Method == http.MethodGet {
					dataReads++
				} else {
					writes++
					if v, present := r.URL.Query()[field]; present {
						value = v[0]
						if value == "null" {
							value = ""
						}
					} else {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						// Model the live JSON-null no-op, not a successful clear.
						if v, ok := body[field].(string); ok {
							value = v
						}
					}
				}
				response := map[string]any{"id": id, "idBoard": "b1", field: nil}
				if value != "" {
					response[field] = value
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer fake.Close()
			s := testService(t, fake, nil)
			text, failed := callTool(t, s, tool, fmt.Sprintf(`{%q:%q,%q:%q}`, key, id, field, set))
			if failed {
				t.Fatal(text)
			}
			if err := s.access.Client.Write(context.Background(), http.MethodPut, "/"+kind+"/"+id, map[string]any{field: nil}, nil); err != nil {
				t.Fatal(err)
			}
			var persisted map[string]any
			if err := s.access.Client.Get(context.Background(), "/"+kind+"/"+id, query("fields", "id,"+field), &persisted); err != nil || persisted[field] != set {
				t.Fatalf("JSON-null no-op not modeled: %v %v", persisted, err)
			}
			text, failed = callTool(t, s, tool, fmt.Sprintf(`{%q:%q,%q:""}`, key, id, field))
			if failed || text != fmt.Sprintf(`{"id":%q}`, id) {
				t.Fatalf("clear %s", text)
			}
			if dataReads != 1 {
				t.Fatal("populated clear incurred verification GET")
			}
			if err := s.access.Client.Get(context.Background(), "/"+kind+"/"+id, query("fields", "id,"+field), &persisted); err != nil || persisted[field] != nil {
				t.Fatalf("clear did not persist: %v %v", persisted, err)
			}
			if writes != 3 || dataReads != 2 {
				t.Fatalf("requests writes=%d data=%d", writes, dataReads)
			}
		})
	}
}

func TestChecklistMetadataVerification(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, mode := range []string{"due", "member", "both"} {
			for _, variant := range []string{"complete", "timezone", "null", "mismatch", "missing", "unavailable", "fallback error", "fallback foreign", "denied"} {
				t.Run(operation+"/"+mode+"/"+variant, func(t *testing.T) {
					args := map[string]any{"checklistId": "cl1"}
					if operation == "create" {
						args["name"] = "Item"
						args["checked"] = false
					} else {
						args["itemId"] = "i1"
						args["state"] = "incomplete"
					}
					response := map[string]any{"id": "i1", "idChecklist": "cl1", "unsolicited": "drop"}
					fallback := map[string]any{"id": "i1", "idChecklist": "cl1"}
					wantNames := []string{}
					wire := map[string]string{}
					if mode != "member" {
						args["due"] = "2027-02-03T13:00:00+01:00"
						wire["due"] = "2027-02-03T12:00:00.000Z"
						wantNames = append(wantNames, "due")
					}
					if mode != "due" {
						args["memberId"] = "m1"
						wire["idMember"] = "m1"
						wantNames = append(wantNames, "memberId")
					}
					for key, value := range wire {
						response[key] = value
						fallback[key] = value
						switch variant {
						case "null":
							response[key] = nil
						case "mismatch":
							response[key] = "retained"
						case "missing", "unavailable", "fallback error", "fallback foreign", "denied":
							delete(response, key)
						}
						if variant == "unavailable" {
							delete(fallback, key)
						}
					}
					if variant == "fallback foreign" {
						fallback["idChecklist"] = "foreign"
					}
					raw, _ := json.Marshal(response)
					readback, _ := json.Marshal(fallback)
					fixture := verificationFixture{response: string(raw), fallback: string(readback), denyFallback: variant == "denied"}
					if variant == "fallback error" {
						fixture.fallbackStatus = http.StatusForbidden
					}
					fake := fixture.server(t)
					defer fake.Close()
					rawArgs, _ := json.Marshal(args)
					text, failed := callTool(t, testService(t, fake, []string{"w1"}), "checklist_item_"+operation, string(rawArgs))
					wantFailure := variant != "complete" && variant != "timezone" && variant != "missing"
					if failed != wantFailure || strings.Contains(text, "unsolicited") {
						t.Fatalf("result %s failed=%v want=%v", text, failed, wantFailure)
					}
					var out mutationPartialResult
					if err := json.Unmarshal([]byte(text), &out); err != nil || out.ID != "i1" {
						t.Fatalf("confirmed item lost: %s", text)
					}
					if wantFailure && (!reflect.DeepEqual(out.Unconfirmed, wantNames) || !strings.Contains(out.Error, "inspect before retrying")) {
						t.Fatalf("partial %s", text)
					}
					wantReads := int32(0)
					if variant == "missing" || variant == "unavailable" || variant == "fallback error" || variant == "fallback foreign" {
						wantReads = 1
					}
					if fixture.writes.Load() != 1 || fixture.dataReads.Load() != wantReads {
						t.Fatalf("writes=%d data GETs=%d want=%d", fixture.writes.Load(), fixture.dataReads.Load(), wantReads)
					}
					q := <-fixture.queries
					if mode != "member" && q.Get("due") != args["due"] {
						t.Errorf("due wire %v", q)
					}
					if mode != "due" && q.Get("idMember") != "m1" {
						t.Errorf("member wire %v", q)
					}
					if operation == "create" && q.Get("checked") != "false" {
						t.Error("checked false lost")
					}
				})
			}
		}
	}
}

func TestVerificationDoesNotMaskPopulatedMismatch(t *testing.T) {
	fixture := verificationFixture{response: `{"id":"i1","idChecklist":"cl1","due":null}`, fallback: `{"id":"i1","idChecklist":"cl1","due":"2027-02-03T12:00:00Z","idMember":"m1"}`}
	fake := fixture.server(t)
	defer fake.Close()
	text, failed := callTool(t, testService(t, fake, nil), "checklist_item_create", `{"checklistId":"cl1","name":"Item","due":"2027-02-03T12:00:00Z","memberId":"m1"}`)
	if !failed || !strings.Contains(text, `"unconfirmed":["due"]`) || fixture.writes.Load() != 1 || fixture.dataReads.Load() != 1 {
		t.Fatalf("present mismatch overwritten by fallback: %s", text)
	}
	if got := <-fixture.dataQueries; !reflect.DeepEqual(got, query("fields", "id,idChecklist,idMember")) {
		t.Fatalf("fallback must select only missing metadata: %v", got)
	}
}

func TestClearCreateRetainsIdentityWithoutRetry(t *testing.T) {
	for _, tc := range []struct{ tool, args, response string }{
		{"card_create", `{"listId":"l1","name":"Card","due":"","checklist":{"name":"Checks","items":["One"]}}`, `{"id":"new1","due":"retained"}`},
		{"label_create", `{"name":"Label","color":""}`, `{"id":"new1","color":"red"}`},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			fixture := verificationFixture{response: tc.response}
			fake := fixture.server(t)
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, nil), tc.tool, tc.args)
			if !failed || !strings.Contains(text, `"id":"new1"`) || !strings.Contains(text, `"unconfirmed"`) || fixture.writes.Load() != 1 || fixture.dataReads.Load() != 0 {
				t.Fatalf("partial create %s", text)
			}
		})
	}
}

func TestChecklistMissChoicesValidateEverySibling(t *testing.T) {
	for _, variant := range []string{"default", "exact", "empty", "truncated", "foreign after match", "foreign card", "invalid id", "duplicate id"} {
		t.Run(variant, func(t *testing.T) {
			lists := []view.Checklist{{ID: "cl1", Name: "Other", BoardID: "b1", CardID: "c1"}}
			args := `{"cardId":"c1"}`
			switch variant {
			case "exact":
				args = `{"cardId":"c1","name":"other"}`
			case "empty":
				lists = nil
			case "truncated":
				for i := 2; i <= 23; i++ {
					lists = append(lists, view.Checklist{ID: fmt.Sprintf("cl%d", i), Name: fmt.Sprintf("Choice %d", i), BoardID: "b1", CardID: "c1"})
				}
			case "foreign after match":
				lists[0].Name = "Acceptance Criteria"
				lists = append(lists, view.Checklist{ID: "cl2", Name: "DO NOT LEAK", BoardID: "foreign", CardID: "c1"})
			case "foreign card":
				lists[0].CardID = "foreign"
			case "invalid id":
				lists[0].ID = "../bad"
			case "duplicate id":
				lists = append(lists, lists[0])
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/cards/c1":
					_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/cards/c1/checklists":
					_ = json.NewEncoder(w).Encode(lists)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, nil), "checklist_get", args)
			if !failed || strings.Contains(text, "checkItems") || strings.Contains(text, "DO NOT LEAK") {
				t.Fatalf("miss response %s", text)
			}
			if strings.HasPrefix(variant, "foreign") || variant == "invalid id" || variant == "duplicate id" {
				if strings.Contains(text, "available") {
					t.Fatalf("unauthorized choices %s", text)
				}
				return
			}
			var out checklistMiss
			if err := json.Unmarshal([]byte(text), &out); err != nil {
				t.Fatal(err)
			}
			wantName := "Acceptance Criteria"
			if variant == "exact" {
				wantName = "other"
			}
			if out.Name != wantName || out.Total != len(lists) || len(out.Available) != min(len(lists), 20) || out.Truncated != (len(lists) > 20) {
				t.Fatalf("choices %s", text)
			}
			if variant == "empty" && !strings.Contains(text, `"available":[]`) {
				t.Fatal("empty choices must be an array")
			}
		})
	}
}

func TestChecklistMetadataReadProjections(t *testing.T) {
	const item = `{"id":"i1","name":"Item","state":"incomplete","due":"2027-02-03T12:00:00Z","idMember":"m1"}`
	const checklist = `{"id":"cl1","name":"Acceptance Criteria","idBoard":"b1","idCard":"c1","checkItems":[` + item + `]}`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		case "/checklists/cl1":
			_, _ = w.Write([]byte(checklist))
		case "/cards/c1/checklists":
			_, _ = w.Write([]byte(`[` + checklist + `]`))
		case "/cards/c1":
			if r.URL.Query().Get("fields") == "idBoard" {
				_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
				return
			}
			if r.URL.Query().Get("checkItem_fields") != itemFields {
				t.Error("metadata selection omitted")
			}
			_, _ = w.Write([]byte(`{"id":"c1","idBoard":"b1","checklists":[` + checklist + `]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	for _, tc := range []struct{ tool, args string }{
		{"checklist_items", `{"checklistId":"cl1"}`}, {"checklist_get", `{"cardId":"c1"}`}, {"checklist_item_find", `{"checklistId":"cl1"}`}, {"card_get", `{"cardId":"c1","detail":"full"}`},
	} {
		text, failed := callTool(t, s, tc.tool, tc.args)
		if failed || !strings.Contains(text, `"due":"2027-02-03T12:00:00Z"`) || !strings.Contains(text, `"idMember":"m1"`) {
			t.Fatalf("%s metadata lost: %s", tc.tool, text)
		}
	}
}

func TestChecklistMetadataIdentityAndRejection(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, tc := range []struct {
			name, response string
			status         int
		}{
			{"null", `null`, 0},
			{"missing id", `{"due":null}`, 0},
			{"invalid id", `{"id":"../bad","due":null}`, 0},
			{"foreign checklist", `{"id":"i1","idChecklist":"foreign","due":null}`, 0},
			{"foreign card", `{"id":"i1","idCard":"foreign","due":null}`, 0},
			{"foreign board", `{"id":"i1","idBoard":"foreign","due":null}`, 0},
			{"feature rejection", `upstream-private-marker test-key test-token`, http.StatusForbidden},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				fixture := verificationFixture{response: tc.response, status: tc.status}
				fake := fixture.server(t)
				defer fake.Close()
				args := `{"checklistId":"cl1","name":"Item","due":"2027-02-03T12:00:00Z"}`
				if operation == "update" {
					args = `{"checklistId":"cl1","itemId":"i1","due":"2027-02-03T12:00:00Z"}`
				}
				text, failed := callTool(t, testService(t, fake, nil), "checklist_item_"+operation, args)
				if !failed || !strings.Contains(text, "inspect before retrying") || strings.Contains(text, "subscription") || strings.Contains(text, "upstream-private-marker") || strings.Contains(text, "test-key") || strings.Contains(text, "test-token") {
					t.Fatalf("unsafe mutation result %s", text)
				}
				if fixture.writes.Load() != 1 || fixture.dataReads.Load() != 0 {
					t.Fatal("ambiguous/rejected metadata write retried")
				}
			})
		}
	}
}

func TestClearQueryAuthorizedReferences(t *testing.T) {
	fixture := verificationFixture{response: `{"id":"c1","due":null}`}
	fake := fixture.server(t)
	defer fake.Close()
	text, failed := callTool(t, testService(t, fake, []string{"w1"}), "card_update", `{"cardId":"c1","due":"","idLabels":["label1"],"idMembers":["m1"]}`)
	if failed || text != `{"id":"c1"}` {
		t.Fatalf("authorized mixed update %s", text)
	}
	want := query("due", "null", "idLabels", "label1", "idMembers", "m1")
	if got := <-fixture.queries; !reflect.DeepEqual(got, want) {
		t.Fatalf("reference query %v, want %v", got, want)
	}
	if fixture.writes.Load() != 1 || fixture.dataReads.Load() != 0 {
		t.Fatal("unexpected mutation or verification GET")
	}
}

func TestClearExpectedBoardDenialPreventsWrite(t *testing.T) {
	fixture := verificationFixture{response: `{"id":"c1","due":null}`}
	fake := fixture.server(t)
	defer fake.Close()
	text, failed := callTool(t, testService(t, fake, []string{"w1"}), "card_update", `{"cardId":"c1","boardId":"other","due":""}`)
	if !failed || !strings.Contains(text, "boardId constraint") || fixture.writes.Load() != 0 {
		t.Fatalf("expected-board denial failed: %s", text)
	}
}
