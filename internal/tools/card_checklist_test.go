package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCardCreateWithChecklist(t *testing.T) {
	type write struct {
		path, body, response string
		status               int
	}
	card := write{"/cards", `{"idList":"l1","name":"Test"}`, `{"id":"new1","name":"Test","url":"https://trello.com/c/new1","private":"drop"}`, 200}
	checklist := write{"/checklists", `{"idCard":"new1","name":"Checklist"}`, `{"id":"clnew1","name":"Checklist","private":"drop"}`, 200}
	first := write{"/checklists/clnew1/checkItems", `{"name":"SND01","pos":"bottom"}`, `{"id":"inew1"}`, 200}
	second := write{"/checklists/clnew1/checkItems", `{"name":"SND02","pos":"bottom"}`, `{"id":"inew2"}`, 200}
	const projection = `"id":"new1","name":"Test","url":"https://trello.com/c/new1"`
	const listProjection = `"checklist":{"id":"clnew1","name":"Checklist","items":2}`
	const args = `{"listId":"l1","name":"Test","checklist":{"items":["SND01","SND02"]}}`
	const batchArgs = `{"listId":"l1","cards":[{"name":"Test","checklist":{"items":["SND01","SND02"]}},{"name":"Test","checklist":{"name":"Checks","items":["SND01"]}}]}`
	custom := checklist
	custom.body = `{"idCard":"new1","name":"Checks"}`
	custom.response = `{"id":"clnew1","name":"Checks"}`
	card2 := card
	card2.response = `{"id":"new2","name":"Test"}`
	checklist2 := custom
	checklist2.body = `{"idCard":"new2","name":"Checks"}`
	checklist2.response = `{"id":"clnew2","name":"Checks"}`
	item2 := first
	item2.path = "/checklists/clnew2/checkItems"
	listFailure := checklist
	listFailure.status, listFailure.response = 500, "test-key private upstream error"
	itemFailure := second
	itemFailure.status, itemFailure.response = 500, "test-token private upstream error"
	badListID := checklist
	badListID.response = `{"id":"../untrusted"}`
	badItemID := second
	badItemID.response = `{}`
	badCard := card
	badCard.status, badCard.response = 500, "private upstream error"
	for _, tc := range []struct {
		name, tool, args, want string
		failed                 bool
		writes                 []write
	}{
		{"single default", "card_create", args, `{` + projection + `,` + listProjection + `}`, false, []write{card, checklist, first, second}},
		{"single custom", "card_create", strings.Replace(args, `"items":`, `"name":"Checks","items":`, 1), `{` + projection + `,"checklist":{"id":"clnew1","name":"Checks","items":2}}`, false, []write{card, custom, first, second}},
		{"batch", "card_create_batch", batchArgs, `{"items":[{"index":0,"id":"new1",` + listProjection + `},{"index":1,"id":"new2","checklist":{"id":"clnew2","name":"Checks","items":1}}]}`, false, []write{card, checklist, first, second, card2, checklist2, item2}},
		{"checklist failure", "card_create", args, `{` + projection + `,"checklist":{"name":"Checklist","items":0},"error":"checklist: trello API: HTTP 500; write may have succeeded; inspect before retrying"}`, true, []write{card, listFailure}},
		{"first item failure", "card_create", args, `{` + projection + `,"checklist":{"id":"clnew1","name":"Checklist","items":0},"error":"checklist.items[0]: trello API: HTTP 500; write may have succeeded; inspect before retrying"}`, true, []write{card, checklist, {first.path, first.body, itemFailure.response, 500}}},
		{"later item failure", "card_create", args, `{` + projection + `,"checklist":{"id":"clnew1","name":"Checklist","items":1},"error":"checklist.items[1]: trello API: HTTP 500; write may have succeeded; inspect before retrying"}`, true, []write{card, checklist, first, itemFailure}},
		{"batch continues after item failure", "card_create_batch", batchArgs, `{"items":[{"index":0,"id":"new1","checklist":{"id":"clnew1","name":"Checklist","items":1},"error":"checklist.items[1]: trello API: HTTP 500; write may have succeeded; inspect before retrying"},{"index":1,"id":"new2","checklist":{"id":"clnew2","name":"Checks","items":1}}]}`, false, []write{card, checklist, first, itemFailure, card2, checklist2, item2}},
		{"batch continues after checklist failure", "card_create_batch", batchArgs, `{"items":[{"index":0,"id":"new1","checklist":{"name":"Checklist","items":0},"error":"checklist: trello API: HTTP 500; write may have succeeded; inspect before retrying"},{"index":1,"id":"new2","checklist":{"id":"clnew2","name":"Checks","items":1}}]}`, false, []write{card, listFailure, card2, checklist2, item2}},
		{"unverifiable checklist id", "card_create", args, `{` + projection + `,"checklist":{"name":"Checklist","items":0},"error":"checklist: mutation result id unverifiable; mutation may have succeeded; write may have succeeded; inspect before retrying"}`, true, []write{card, badListID}},
		{"unverifiable item id", "card_create", args, `{` + projection + `,"checklist":{"id":"clnew1","name":"Checklist","items":1},"error":"checklist.items[1]: mutation result id unverifiable; mutation may have succeeded; write may have succeeded; inspect before retrying"}`, true, []write{card, checklist, first, badItemID}},
		{"card failure skips checklist", "card_create", args, `{"error":"trello API: HTTP 500"}`, true, []write{badCard}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			reads, writes, cards := 0, 0, 0
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					reads++
					switch r.URL.Path {
					case "/lists/l1":
						_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
					case "/boards/b1":
						_, _ = w.Write([]byte(boardJSON))
					case "/organizations/w1":
						_, _ = w.Write([]byte(workspaceJSON))
					default:
						t.Errorf("unexpected ownership read %s", r.URL.Path)
						w.WriteHeader(500)
					}
					return
				}
				if writes >= len(tc.writes) {
					t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				expected := tc.writes[writes]
				writes++
				if r.Method != http.MethodPost || r.URL.Path != expected.path {
					t.Errorf("write %d: got %s %s; want POST %s", writes, r.Method, r.URL.Path, expected.path)
				}
				if r.URL.Path == "/cards" {
					cards++
				}
				wantReads := 3
				if tc.tool == "card_create_batch" {
					wantReads += 3 * cards
				}
				if reads != wantReads {
					t.Errorf("authorization reads: got %d; want %d before write %d", reads, wantReads, writes)
				}
				var got, want any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if err := json.Unmarshal([]byte(expected.body), &want); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body got %#v; want %#v", got, want)
				}
				w.WriteHeader(expected.status)
				_, _ = w.Write([]byte(expected.response))
			}))
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, []string{"w1"}), tc.tool, tc.args)
			if text != tc.want || failed != tc.failed {
				t.Fatalf("got %s (failed=%v); want %s (failed=%v)", text, failed, tc.want, tc.failed)
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != len(tc.writes) {
				t.Errorf("got %d writes; want %d", writes, len(tc.writes))
			}
		})
	}
}

func TestCardChecklistTargetListAllowlist(t *testing.T) {
	for _, tool := range []string{"card_create", "card_create_batch"} {
		t.Run(tool, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unauthorized mutation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/lists/l1":
					_, _ = w.Write([]byte(`{"idBoard":"b2"}`))
				case "/boards/b2":
					_, _ = w.Write([]byte(`{"id":"b2","idOrganization":"w2"}`))
				case "/organizations/w2":
					_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			card := `"name":"Test","checklist":{"items":["SND01"]}`
			raw := `{"boardId":"b1","listId":"l1",` + card + `}`
			if tool == "card_create_batch" {
				raw = `{"boardId":"b1","listId":"l1","cards":[{` + card + `}]}`
			}
			text, failed := callTool(t, testService(t, fake, []string{"w1"}), tool, raw)
			if !failed || !strings.Contains(text, "access denied by TRELLO_ALLOWED_WORKSPACES") || strings.Contains(text, "b2") || strings.Contains(text, "w2") {
				t.Fatalf("target list not checked against allowlist: %s", text)
			}
		})
	}
}

func TestBatchChecklistValidatedBeforeWrites(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request before whole-batch validation: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer fake.Close()
	for _, items := range []string{`[]`, `[" "]`, `[null]`, `[` + strings.Repeat(`"x",`, 100) + `"x"]`} {
		raw := fmt.Sprintf(`{"listId":"l1","cards":[{"name":"First","checklist":{"items":["SND01"]}},{"name":"Second","checklist":{"items":%s}}]}`, items)
		text, failed := callTool(t, testService(t, fake, []string{"w1"}), "card_create_batch", raw)
		if !failed || !strings.Contains(text, "cards[1].checklist.items") {
			t.Fatalf("invalid later checklist not rejected before writes: %s", text)
		}
	}
}
