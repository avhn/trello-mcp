package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCustomFieldTypes covers each scalar, dropdown option identity and clearing every type.
func TestCustomFieldTypes(t *testing.T) {
	for _, tc := range []struct{ kind, arg, body string }{
		{"text", `"value":{"text":"hello"}`, `{"value":{"text":"hello"}}`},
		{"number", `"value":{"number":"-12.50"}`, `{"value":{"number":"-12.50"}}`},
		{"date", `"value":{"date":"2026-10-20T09:00:00Z"}`, `{"value":{"date":"2026-10-20T09:00:00Z"}}`},
		{"checkbox", `"value":{"checked":"false"}`, `{"value":{"checked":"false"}}`},
		{"list", `"idValue":"o1"`, `{"idValue":"o1"}`},
	} {
		for _, clear := range []bool{false, true} {
			name := tc.kind
			if clear {
				name += "/clear"
			}
			t.Run(name, func(t *testing.T) {
				var writes atomic.Int32
				arg, body := tc.arg, tc.body
				if clear {
					arg, body = `"value":""`, `{"value":""}`
					if tc.kind == "list" {
						arg = `"idValue":""`
					}
				}
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "PUT" {
						writes.Add(1)
						if r.URL.Path != "/cards/c1/customField/f1/item" {
							t.Errorf("bad route %s", r.URL.Path)
						}
						var got, want any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Error(err)
						}
						if err := json.Unmarshal([]byte(body), &want); err != nil {
							t.Error(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("body: got %#v; want %#v", got, want)
						}
						_, _ = w.Write([]byte(`{}`))
						return
					}
					switch r.URL.Path {
					case "/cards/c1":
						_, _ = w.Write([]byte(cardJSON))
					case "/boards/b1":
						_, _ = w.Write([]byte(boardJSON))
					case "/organizations/w1":
						_, _ = w.Write([]byte(workspaceJSON))
					case "/customFields/f1":
						_, _ = w.Write([]byte(`{"id":"f1","idModel":"b1","modelType":"board","name":"Field","type":"` + tc.kind + `","options":[{"id":"o1","value":{"text":"Ready"}}]}`))
					default:
						t.Errorf("unexpected route %s", r.URL.Path)
						w.WriteHeader(500)
					}
				}))
				defer fake.Close()
				text, failed := callTool(t, testService(t, fake, []string{"w1"}), "customfield_set", `{"cardId":"c1","customFieldId":"f1",`+arg+`}`)
				if failed || text != `{"ok":true}` || writes.Load() != 1 {
					t.Fatalf("got %s, failed=%v, writes=%d", text, failed, writes.Load())
				}
			})
		}
	}
}

func TestCustomFieldRejectsMismatch(t *testing.T) {
	for _, tc := range []struct{ kind, arg string }{
		{"list", `"idValue":"foreign"`}, {"list", `"value":{"text":"Ready"}`},
		{"text", `"idValue":"o1"`}, {"text", `"value":{"number":"1"}`},
		{"number", `"value":{"number":"NaN"}`}, {"number", `"value":{"number":"\"1\""}`},
		{"checkbox", `"value":{"checked":false}`}, {"checkbox", `"value":{"checked":"yes"}`},
		{"date", `"value":{"date":"tomorrow"}`}, {"unknown", `"value":""`},
		{"text", `"value":{"text":"one","text":"two"}`}, {"text", `"value":null`},
		{"text", `"value":{}`}, {"text", `"value":{"text":"x","number":"1"}`},
		{"text", `"value":"","idValue":""`},
	} {
		t.Run(tc.kind+"/"+tc.arg, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("invalid value mutated Trello")
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/cards/c1":
					_, _ = w.Write([]byte(cardJSON))
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/customFields/f1":
					_, _ = w.Write([]byte(`{"id":"f1","idModel":"b1","modelType":"board","type":"` + tc.kind + `"}`))
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, nil), "customfield_set", `{"cardId":"c1","customFieldId":"f1",`+tc.arg+`}`)
			if !failed || !strings.Contains(text, `"error"`) {
				t.Fatalf("accepted invalid custom-field value: %s", text)
			}
		})
	}
}
