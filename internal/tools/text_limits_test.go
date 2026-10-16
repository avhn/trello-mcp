package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/avhn/trello-mcp/internal/view"
)

// resolvedToolSchema exercises the published schema, not a second limit table.
func resolvedToolSchema(t *testing.T, d definition) *jsonschema.Resolved {
	t.Helper()
	raw, err := json.Marshal(schema(d))
	if err != nil {
		t.Fatal(err)
	}
	var published jsonschema.Schema
	if err = json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	resolved, err := published.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func requiredTextArguments(d definition) map[string]any {
	args := map[string]any{}
	for _, key := range strings.Fields(d.required) {
		args[key] = "x1"
	}
	return args
}

// TestHumanTextBoundaries covers runtime code-point bounds in all human text
// contexts, including nested fields; compact schemas intentionally omit them.
func TestHumanTextBoundaries(t *testing.T) {
	type textCase struct {
		name string
		d    definition
		max  int
		args func(string) map[string]any
	}
	var cases []textCase
	for _, d := range (&Service{}).definitions() {
		for _, key := range strings.Fields(d.params) {
			if max := textLimit(d.name, key); max > 0 {
				cases = append(cases, textCase{d.name + "/" + key, d, max, func(value string) map[string]any {
					args := requiredTextArguments(d)
					args[key] = value
					return args
				}})
			}
		}
	}
	for _, tool := range []string{"card_create", "card_create_batch"} {
		for _, field := range []string{"name", "desc", "checklist.name", "checklist.items"} {
			d := toolDefinition(t, tool)
			cases = append(cases, textCase{tool + "/nested/" + field, d, localTextLimit, func(value string) map[string]any {
				card := map[string]any{"name": "Card"}
				switch field {
				case "name", "desc":
					card[field] = value
				case "checklist.name":
					card["checklist"] = map[string]any{"name": value, "items": []string{"Item"}}
				case "checklist.items":
					card["checklist"] = map[string]any{"items": []string{value}}
				}
				if tool == "card_create_batch" {
					return map[string]any{"listId": "l1", "cards": []any{card}}
				}
				card["listId"] = "l1"
				return card
			}})
		}
	}
	cases = append(cases, textCase{"customfield_set/value.text", toolDefinition(t, "customfield_set"), localTextLimit, func(value string) map[string]any {
		return map[string]any{"cardId": "c1", "customFieldId": "f1", "value": map[string]any{"text": value}}
	}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			published, err := json.Marshal(schema(tc.d))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(published), `"maxLength"`) {
				t.Fatal("compact schema must omit human text maxLength")
			}
			resolved := resolvedToolSchema(t, tc.d)
			for _, unit := range []string{"x", "界", "\U00010400"} {
				for _, n := range []int{tc.max - 1, tc.max, tc.max + 1} {
					args := tc.args(strings.Repeat(unit, n))
					raw, err := json.Marshal(args)
					if err != nil {
						t.Fatal(err)
					}
					_, runtimeErr := validate(raw, tc.d)
					schemaErr := resolved.Validate(args)
					valid := n <= tc.max
					if (runtimeErr == nil) != valid || schemaErr != nil {
						t.Fatalf("unit bytes=%d points=%d runtime valid=%v runtime=%v schema=%v", len(unit), n, valid, runtimeErr, schemaErr)
					}
					if !valid && !strings.Contains(runtimeErr.Error(), fmt.Sprintf("%d code points", tc.max)) {
						t.Fatalf("did not reach field validation: %v", runtimeErr)
					}
				}
			}
		})
	}
}

// Build literal JSON surrogate escapes, never a JSON encoder's UTF-8 output.
const supplementaryJSONPair = string(rune(92)) + "ud801" + string(rune(92)) + "udc00"

// TestDescriptionRoundTrip verifies lossless read-edit-write through raw handlers,
// HTTP JSON encoding and full reads. Explicit surrogate pairs exceed 128 KiB.
func TestDescriptionRoundTrip(t *testing.T) {
	for _, tool := range []string{"card_create", "card_create_batch", "card_update"} {
		for _, tc := range []struct{ name, value, escaped string }{
			{"5320 bytes", strings.Repeat("x", 5320), ""},
			{"4097 ASCII", strings.Repeat("x", 4097), ""},
			{"2732 BMP", strings.Repeat("界", 2732), ""},
			{"ASCII boundary", strings.Repeat("x", cardDescriptionLimit), ""},
			{"BMP boundary", strings.Repeat("界", cardDescriptionLimit), ""},
			{"supplementary boundary", strings.Repeat("\U00010400", cardDescriptionLimit), ""},
			{"escaped boundary", strings.Repeat("\U00010400", cardDescriptionLimit), strings.Repeat(supplementaryJSONPair, cardDescriptionLimit)},
			{"clear", "", ""},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				stored := tc.value
				writes := 0
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method != http.MethodGet:
						writes++
						var body struct {
							Desc string `json:"desc"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						stored = body.Desc
						if stored != tc.value {
							t.Error("description changed on upstream write")
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "name": "Card", "idBoard": "b1", "desc": stored})
					case r.URL.Path == "/boards/b1":
						_, _ = w.Write([]byte(boardJSON))
					case r.URL.Path == "/lists/l1":
						_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
					case r.URL.Path == "/cards/c1":
						_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "name": "Card", "idBoard": "b1", "desc": stored})
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
						w.WriteHeader(500)
					}
				}))
				defer fake.Close()
				s := testService(t, fake, nil)
				read, failed := callTool(t, s, "card_get", `{"cardId":"c1","detail":"full"}`)
				var card view.Card
				if failed || json.Unmarshal([]byte(read), &card) != nil || card.Desc != tc.value {
					t.Fatal("full read changed description")
				}
				encoded, err := json.Marshal(card.Desc)
				if err != nil {
					t.Fatal(err)
				}
				if tc.escaped != "" {
					encoded = []byte(`"` + tc.escaped + `"`)
					if len(encoded) != 196610 {
						t.Fatalf("surrogate contents: %d bytes", len(encoded)-2)
					}
				}
				raw := descriptionArguments(tool, string(encoded))
				result, failed := callTool(t, s, tool, raw)
				if failed || writes != 1 {
					t.Fatalf("mutation failed=%v writes=%d response=%s", failed, writes, result)
				}
				read, failed = callTool(t, s, "card_get", `{"cardId":"c1","detail":"full"}`)
				if failed || json.Unmarshal([]byte(read), &card) != nil || card.Desc != tc.value {
					t.Fatal("read-edit-write changed description")
				}
			})
		}
	}
}

func descriptionArguments(tool, encoded string) string {
	switch tool {
	case "card_create":
		return `{"listId":"l1","name":"Card","desc":` + encoded + `}`
	case "card_create_batch":
		return `{"listId":"l1","cards":[{"name":"Card","desc":` + encoded + `}]}`
	default:
		return `{"cardId":"c1","desc":` + encoded + `}`
	}
}

func TestTextValidationBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	for _, tool := range []string{"card_create", "card_create_batch", "card_update"} {
		for _, value := range []string{`"` + strings.Repeat("x", cardDescriptionLimit+1) + `"`, `"` + strings.Repeat(supplementaryJSONPair, cardDescriptionLimit+1) + `"`} {
			text, failed := callTool(t, s, tool, descriptionArguments(tool, value))
			if !failed || !strings.Contains(text, "desc must be at most 16384 code points") {
				t.Fatalf("%s did not reach description validation: %s", tool, text)
			}
		}
	}
	for _, tc := range []struct{ tool, raw, path string }{
		{"comment_create", `{"cardId":"c1","text":"` + strings.Repeat("界", localTextLimit+1) + `"}`, "text"},
		{"card_update", `{"cardId":"c1","name":"` + strings.Repeat("界", localTextLimit+1) + `"}`, "name"},
		{"checklist_item_create", `{"checklistId":"cl1","name":"` + strings.Repeat("界", localTextLimit+1) + `"}`, "name"},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"Card","checklist":{"items":["` + strings.Repeat("界", localTextLimit+1) + `"]}}]}`, "cards[0].checklist.items[0]"},
	} {
		text, failed := callTool(t, s, tc.tool, tc.raw)
		if !failed || !strings.Contains(text, tc.path) || !strings.Contains(text, "code points") {
			t.Fatalf("%s: %s", tc.tool, text)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{`{"cardId":"c1","desc":null}`, "desc must be a string"},
		{`{"cardId":"c1","desc":"x","desc":"y"}`, "duplicate argument"},
		{`{"cardId":"c1","description":"x"}`, "unknown argument"},
		{`{"cardId":"c1","desc":"` + strings.Repeat("x", cardDescriptionLimit) + `"} {}`, "trailing arguments"},
	} {
		text, failed := callTool(t, s, "card_update", tc.raw)
		if !failed || !strings.Contains(text, tc.want) {
			t.Fatalf("malformed description: %s", text)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("validation made %d HTTP calls", calls.Load())
	}
}

func TestTextAggregateBudgets(t *testing.T) {
	for _, d := range (&Service{}).definitions() {
		for _, key := range strings.Fields(d.params) {
			if textLimit(d.name, key) > 0 || key == "value" || key == "cards" {
				want := 1 << 20
				if d.name == "attachment_upload" {
					want = 14 << 20
				}
				if got := argumentBytes(d); got != want {
					t.Errorf("%s: aggregate bytes=%d want=%d", d.name, got, want)
				}
			}
		}
	}
	// A valid field can reach the exact aggregate byte bound with JSON whitespace.
	d := toolDefinition(t, "card_update")
	base := `{"cardId":"c1","desc":"x"}`
	boundary := base + strings.Repeat(" ", argumentBytes(d)-len(base))
	if _, err := validate([]byte(boundary), d); err != nil {
		t.Fatalf("exact aggregate boundary rejected: %v", err)
	}
	if _, err := validate([]byte(boundary+" "), d); err == nil || err.Error() != "arguments exceed size limit" {
		t.Fatalf("aggregate boundary+1: %v", err)
	}
	// Each item is valid on its own, but 100 maximum-sized items do not fit.
	item := `"` + strings.Repeat("x", localTextLimit) + `"`
	raw := `{"listId":"l1","name":"Card","checklist":{"items":[` + strings.Repeat(item+",", 99) + item + `]}}`
	_, err := validate([]byte(raw), toolDefinition(t, "card_create"))
	if err == nil || err.Error() != "arguments exceed size limit" {
		t.Fatalf("aggregate guard: %v", err)
	}
	// Specialized inputs retain byte bounds, independently of human text.
	for _, key := range []string{"url", "path", "fileName", "mimeType", "base64", "cardId", "due", "filter", "state"} {
		if textLimit("attachment_upload", key) != 0 {
			t.Errorf("specialized %s treated as human text", key)
		}
	}
	_, err = validate([]byte(`{"cardId":"c1","url":"https://example.invalid/`+strings.Repeat("x", 4096)+`"}`), toolDefinition(t, "attachment_add"))
	if err == nil || !strings.Contains(err.Error(), "4096 bytes") {
		t.Fatalf("URL byte guard: %v", err)
	}
}
