package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func toolDefinition(t *testing.T, name string) definition {
	t.Helper()
	for _, d := range (&Service{}).definitions() {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("tool %s missing", name)
	return definition{}
}

func TestEveryToolArgumentKeys(t *testing.T) {
	for _, d := range (&Service{}).definitions() {
		t.Run(d.name, func(t *testing.T) {
			_, err := validate(json.RawMessage(`{"unexpected":true}`), d)
			want := `unknown argument "unexpected" in arguments; allowed: ` + strings.Join(strings.Fields(d.params), ", ")
			if err == nil || err.Error() != want {
				t.Fatalf("unknown key: got %v; want %s", err, want)
			}
			key := strings.Fields(d.params)[0]
			value := `"x"`
			if strings.HasSuffix(key, "Id") {
				value = `"b1"`
			}
			if key == "detail" {
				value = `"basic"`
			}
			_, err = validate(json.RawMessage(fmt.Sprintf(`{%q:%s,%q:%s}`, key, value, key, value)), d)
			want = fmt.Sprintf("duplicate argument %q in arguments", key)
			if err == nil || err.Error() != want {
				t.Fatalf("duplicate key: got %v; want %s", err, want)
			}
		})
	}
}

func TestArgumentErrorDetails(t *testing.T) {
	for _, tc := range []struct{ tool, raw, want string }{
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"Test","items":["SND01"]}]}`, `unknown argument "items" in cards[0]; allowed: name, desc, due, start, pos, idMembers, idLabels, checklist`},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"One"},{"name":"Two","name":"Again"}]}`, `duplicate argument "name" in cards[1]`},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":false}]}`, `cards[0].name must be a string`},
		{"card_create_batch", `{"listId":"l1","cards":[false]}`, `cards[0] must be an object`},
		{"card_create_batch", `{"listId":"l1","cards":[null]}`, `cards[0] must be an object`},
		{"card_create_batch", `{"listId":"l1","cards":{}}`, `cards must be an array of objects`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{"items":["x"],"extra":true}}`, `unknown argument "extra" in checklist; allowed: name, items`},
		{"card_create_batch", `{"listId":"l1","cards":[{"name":"Test","checklist":{"items":["x"],"extra":true}}]}`, `unknown argument "extra" in cards[0].checklist; allowed: name, items`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{"items":["x"],"items":["y"]}}`, `duplicate argument "items" in checklist`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":[]}`, `checklist must be an object`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":null}`, `checklist must be an object`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{"items":[42]}}`, `checklist.items[0] must be a string`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{"items":[null]}}`, `checklist.items[0] must be a string`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{"items":"x"}}`, `checklist.items must be an array of strings`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{"name":false,"items":["x"]}}`, `checklist.name must be a string`},
		{"card_create", `{"listId":"l1","name":"Test","checklist":{}}`, `checklist.items is required`},
		{"card_get", `{"cardId":null}`, `cardId must be a string`},
		{"card_get", `{"cardId":"QprgpMhG"}`, `cardId "QprgpMhG" is a card short link (from a trello.com/c/QprgpMhG URL), not a card id; call card_find with the board's boardId (or nameFilter, or mine:true) and retry with the id of the item whose url contains /c/QprgpMhG/`},
		{"comment_create", `{"cardId":"ATi872Hl","text":"x"}`, `cardId "ATi872Hl" is a card short link (from a trello.com/c/ATi872Hl URL), not a card id; call card_find with the board's boardId (or nameFilter, or mine:true) and retry with the id of the item whose url contains /c/ATi872Hl/`},
		{"card_find", `{"limit":"1"}`, `limit must be an integer`},
		{"card_find", `{"limit":20.5}`, `limit must be an integer`},
		{"card_find", `{"limit":1e400}`, `limit must be an integer`},
		{"card_find", `{"limit":1e30}`, `limit must be between 1 and 1000`},
		{"card_find", `{"mine":1}`, `mine must be a boolean`},
		{"list_create", `{"name":"Test","pos":true}`, `pos must be a string or number`},
		{"list_update", `{"listId":"l1","closed":"false"}`, `closed must be a boolean`},
		{"checklist_item_create", `{"checklistId":"cl1","name":"Test","checked":"true"}`, `checked must be a boolean`},
		{"card_create", `{"listId":"l1","name":"Test","idMembers":"m1"}`, `idMembers must be an array of strings`},
		{"card_create", `{"listId":"l1","name":"Test","idLabels":[true]}`, `idLabels[0] must be a string`},
		{"card_get", `{"cardId":"c1","extraFields":true}`, `extraFields must be an array of strings`},
		{"customfield_set", `{"cardId":"c1","customFieldId":"f1","value":42}`, `value must be a typed scalar object or an empty string`},
		{"customfield_set", `{"cardId":"c1","customFieldId":"f1","value":{"wat":"x"}}`, `unknown argument "wat" in value; allowed: text, number, date, checked`},
		{"customfield_set", `{"cardId":"c1","customFieldId":"f1","value":{"text":"x","text":"y"}}`, `duplicate argument "text" in value`},
		{"customfield_set", `{"cardId":"c1","customFieldId":"f1","value":{"checked":true}}`, `value.checked must be a string`},
	} {
		t.Run(tc.tool+"/"+tc.want, func(t *testing.T) {
			_, err := validate(json.RawMessage(tc.raw), toolDefinition(t, tc.tool))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
}

func TestCardChecklistBounds(t *testing.T) {
	for _, tool := range []string{"card_create", "card_create_batch"} {
		for _, tc := range []struct {
			name, checklist string
			valid           bool
		}{
			{"one", `{"items":["SND01"]}`, true},
			{"hundred", `{"items":[` + strings.Repeat(`"SND01",`, 99) + `"SND100"]}`, true},
			{"many large names", `{"items":[` + strings.Repeat(`"`+strings.Repeat("x", 4096)+`",`, 99) + `"last"]}`, true},
			{"empty", `{"items":[]}`, false},
			{"too many", `{"items":[` + strings.Repeat(`"x",`, 100) + `"x"]}`, false},
			{"null", `{"items":null}`, false},
			{"blank item", `{"items":["  "]}`, false},
			{"oversized item", `{"items":["` + strings.Repeat("x", localTextLimit+1) + `"]}`, false},
			{"blank name", `{"name":" ","items":["x"]}`, false},
			{"oversized name", `{"name":"` + strings.Repeat("x", localTextLimit+1) + `","items":["x"]}`, false},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				card := `"name":"Test","checklist":` + tc.checklist
				raw := `{"listId":"l1",` + card + `}`
				if tool == "card_create_batch" {
					raw = `{"listId":"l1","cards":[{` + card + `}]}`
				}
				a, err := validate(json.RawMessage(raw), toolDefinition(t, tool))
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%v, err=%v", tc.valid, err)
				}
				if tc.valid {
					if tool == "card_create_batch" {
						a = a.cards[0]
					}
					if a.checklist.str("name") != "Checklist" {
						t.Fatalf("missing default name: %+v", a.checklist)
					}
				}
			})
		}
	}
}

func TestCardChecklistSchema(t *testing.T) {
	for _, tool := range []string{"card_create", "card_create_batch"} {
		s := schema(toolDefinition(t, tool))
		props := s["properties"].(map[string]any)
		if tool == "card_create_batch" {
			props = props["cards"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		}
		checklist := props["checklist"].(map[string]any)
		props = checklist["properties"].(map[string]any)
		items := props["items"].(map[string]any)
		item := items["items"].(map[string]any)
		if _, present := item["maxLength"]; present || item["type"] != "string" || item["minLength"] != 1 {
			t.Errorf("%s: incorrect compact item schema: %v", tool, item)
		}
		if checklist["additionalProperties"] != false || props["name"].(map[string]any)["default"] != "Checklist" || items["minItems"] != 1 || items["maxItems"] != 100 {
			t.Errorf("%s: incomplete checklist schema: %v", tool, checklist)
		}
	}
}

func TestContextualSchemaDescriptions(t *testing.T) {
	active := map[string]bool{"board_get": true, "board_activity": true, "list_find": true, "member_find": true, "label_find": true, "customfield_find": true, "list_create": true, "label_create": true}
	objectScoped := 0
	for _, d := range (&Service{}).definitions() {
		props := schema(d)["properties"].(map[string]any)
		if p, ok := props["boardId"]; ok {
			if slices.Contains(strings.Fields(d.required), "boardId") != (d.name == "board_use") {
				t.Errorf("%s: boardId requiredness changed", d.name)
			}
			desc := p.(map[string]any)["description"].(string)
			switch {
			case d.name == "board_use":
				if desc != "Required active-board selection." || d.required != "boardId" {
					t.Errorf("selection schema: %v", p)
				}
			case d.name == "card_find":
				if desc != "With listId: expected owner; otherwise active board, unless mine." {
					t.Errorf("card discovery schema: %v", p)
				}
			case active[d.name]:
				if desc != "Defaults to active board." {
					t.Errorf("%s: wrong active-board default: %v", d.name, p)
				}
			default:
				objectScoped++
				if desc != "Expected board; actual board verified." {
					t.Errorf("%s: wrong object constraint: %v", d.name, p)
				}
			}
		}
	}
	if objectScoped != 33 {
		t.Errorf("got %d object-scoped board descriptions, want 33", objectScoped)
	}
	props := schema(toolDefinition(t, "card_find"))["properties"].(map[string]any)
	if !strings.Contains(props["mine"].(map[string]any)["description"].(string), "excludes boardId/listId") || !strings.Contains(props["before"].(map[string]any)["description"].(string), "same filtered snapshot, not a date") {
		t.Fatalf("card discovery exclusivity/cursor hints missing: %v", props)
	}
	for _, tc := range []struct {
		name, description, params, required string
		write                               bool
		defaults                            map[string]any
	}{
		{"board_activity", "Page board actions, newest first; omitted card/list names reuse page-local names, else unknown.", "boardId limit since before filter", "", false, map[string]any{"limit": 20, "filter": "all"}},
		{"card_history", "Page filtered card actions; omitted card/list names reuse page-local names, else unknown.", "boardId cardId limit before since filter", "cardId", false, map[string]any{"limit": 20, "filter": "all"}},
		{"comment_find", "Page card comments; omitted card/list names reuse page-local names, else unknown.", "boardId cardId limit before since", "cardId", false, map[string]any{"limit": 20}},
		{"list_find", "Find open board lists.", "boardId nameFilter", "", false, map[string]any{}},
		{"card_find", "Find open cards; filter names before snapshot paging; includeDesc:false omits summary previews.", "boardId listId mine nameFilter detail limit before includeDesc", "", false, map[string]any{"limit": 100, "includeDesc": true}},
		{"card_get", "Get a card; full adds dates/checklists/members; view:markdown omits metadata; extraFields optional.", "boardId cardId detail view extraFields", "cardId", false, map[string]any{}},
		{"checklist_item_create", "Add a checklist item; due/memberId may be unconfirmed (advanced checklists).", "boardId checklistId name pos checked due memberId", "checklistId name", true, map[string]any{}},
		{"checklist_item_update", "Update an item verified within its checklist; due/memberId may be unconfirmed.", "boardId checklistId itemId name state pos due memberId", "checklistId itemId", true, map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := toolDefinition(t, tc.name)
			if d.description != tc.description || d.params != tc.params || d.required != tc.required || d.write != tc.write {
				t.Fatalf("description or frozen definition changed: %+v", d)
			}
			s := schema(d)
			props := s["properties"].(map[string]any)
			keys := make([]string, 0, len(props))
			defaults := map[string]any{}
			for key, value := range props {
				keys = append(keys, key)
				if def, ok := value.(map[string]any)["default"]; ok {
					defaults[key] = def
				}
			}
			wantKeys := strings.Fields(tc.params)
			slices.Sort(keys)
			slices.Sort(wantKeys)
			required, _ := s["required"].([]string)
			if !reflect.DeepEqual(keys, wantKeys) || !slices.Equal(required, strings.Fields(tc.required)) || !reflect.DeepEqual(defaults, tc.defaults) {
				t.Fatalf("frozen schema keys/required/defaults changed: %v", s)
			}
		})
	}
	if _, present := schema(toolDefinition(t, "board_create"))["properties"].(map[string]any)["boardId"]; present {
		t.Error("board_create must not accept boardId")
	}
}

func TestActionFilterSchemaRuntimeParity(t *testing.T) {
	for _, tool := range []string{"board_activity", "card_history"} {
		d := toolDefinition(t, tool)
		encoded, err := json.Marshal(schema(d))
		if err != nil {
			t.Fatal(err)
		}
		var published jsonschema.Schema
		if err := json.Unmarshal(encoded, &published); err != nil {
			t.Fatal(err)
		}
		resolved, err := published.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			filter string
			valid  bool
		}{
			{"all", true}, {"commentCard", true}, {"createCard,updateCard,updateCheckItemStateOnCard", true}, {"future_Action2", true}, {"all_custom", true}, {"ALL,commentCard", true},
			{"", false}, {",commentCard", false}, {"commentCard,", false}, {"commentCard,,updateCard", false}, {"commentCard, updateCard", false}, {" commentCard", false}, {"commentCard\n", false}, {"all,commentCard", false}, {"commentCard,all", false}, {"commentCard,all,updateCard", false}, {"all,all", false}, {"1update", false}, {"_update", false}, {"comment-Card", false}, {"commentCard&key=bad", false},
		} {
			t.Run(tool+"/"+tc.filter, func(t *testing.T) {
				args := map[string]any{"filter": tc.filter}
				if tool == "card_history" {
					args["cardId"] = "c1"
				}
				raw, err := json.Marshal(args)
				if err != nil {
					t.Fatal(err)
				}
				_, runtimeErr := validate(raw, d)
				schemaErr := resolved.Validate(args)
				if (runtimeErr == nil) != tc.valid || (schemaErr == nil) != tc.valid {
					t.Fatalf("valid=%v runtime=%v schema=%v", tc.valid, runtimeErr, schemaErr)
				}
			})
		}
		if published.Properties["filter"].Default == nil || string(published.Properties["filter"].Default) != `"all"` {
			t.Error("filter default is not all")
		}
	}
}

func TestChecklistStateSchemaRuntimeParity(t *testing.T) {
	d := toolDefinition(t, "checklist_item_update")
	props := schema(d)["properties"].(map[string]any)
	states := props["state"].(map[string]any)["enum"].([]string)
	if strings.Join(states, ",") != "complete,incomplete" {
		t.Fatalf("incomplete state enum: %v", states)
	}
	for _, state := range []string{"complete", "incomplete", "", "done"} {
		raw := fmt.Sprintf(`{"checklistId":"cl1","itemId":"i1","state":%q}`, state)
		_, err := validate(json.RawMessage(raw), d)
		if (err == nil) != (state == "complete" || state == "incomplete") {
			t.Errorf("state %q: %v", state, err)
		}
	}
}

func TestPositionBoundaries(t *testing.T) {
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		t.Error("invalid position reached HTTP")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"top", `"top"`, true},
		{"bottom", `"bottom"`, true},
		{"small number", `1e-10`, true},
		{"small numeric string", `"1e-10"`, true},
		{"upper number", `1e20`, true},
		{"upper numeric string", `"1e20"`, true},
		{"too large number", `1e21`, false},
		{"too large numeric string", `"1e21"`, false},
		{"zero number", `0`, false},
		{"zero numeric string", `"0"`, false},
		{"negative number", `-1`, false},
		{"negative numeric string", `"-1"`, false},
		{"malformed string", `"not-a-position"`, false},
		{"empty string", `""`, false},
		{"NaN string", `"NaN"`, false},
		{"Inf string", `"Inf"`, false},
		{"negative Inf string", `"-Inf"`, false},
		{"overflow number", `1e309`, false},
		{"overflow numeric string", `"1e309"`, false},
		{"boolean", `true`, false},
		{"null", `null`, false},
		{"decimal string", `"16384.5"`, true},
		{"rounded to bound string", `"100000000000000000001"`, true},
		{"large integer", `1234567`, true},
		{"small exponent string", `"1e-5"`, true},
		{"fractional exponent number", `1.2345675e6`, true},
		{"underflow string", `"1e-400"`, false},
		{"hex string", `"0x1p4"`, false},
		{"digit separator string", `"1_000"`, false},
		{"signed string", `"+5"`, false},
		{"trailing dot string", `"5."`, false},
		{"leading dot string", `".5"`, false},
		{"leading zero string", `"05"`, false},
		{"padded string", `" 5"`, false},
	} {
		for _, tool := range []string{"list_create", "card_create_batch"} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				raw := `{"name":"Example","pos":` + tc.value + `}`
				path := "pos"
				if tool == "card_create_batch" {
					raw = `{"listId":"l1","cards":[` + raw + `]}`
					path = "cards[0].pos"
				}
				a, err := validate(json.RawMessage(raw), toolDefinition(t, tool))
				if tc.valid {
					if err != nil {
						t.Fatal(err)
					}
					if tool == "card_create_batch" {
						a = a.cards[0]
					}
					want := strings.Trim(tc.value, `"`)
					if canonical, ok := map[string]string{"1e-10": "0.0000000001", "1e20": "100000000000000000000", "100000000000000000001": "100000000000000000000", "1234567": "1234567", "1e-5": "0.00001", "1.2345675e6": "1234567.5"}[want]; ok {
						want = canonical
					}
					if got := a.values["pos"]; got != want {
						t.Fatalf("position changed: got %v, want %s", got, want)
					}
					return
				}
				want := path + " must be top, bottom or a number with 0 < pos <= 1e20"
				if tc.value == "true" || tc.value == "null" {
					want = path + " must be a string or number"
				}
				if err == nil || err.Error() != want {
					t.Fatalf("got %v, want %s", err, want)
				}
				text, failed := callTool(t, s, tool, raw)
				var result struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(text), &result); err != nil || !failed || result.Error != want {
					t.Fatalf("invalid position response: %s, failed=%v, err=%v", text, failed, err)
				}
			})
		}
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("invalid positions made %d HTTP requests", got)
	}
	pos := schema(toolDefinition(t, "list_create"))["properties"].(map[string]any)["pos"].(map[string]any)
	for _, variant := range pos["anyOf"].([]any) {
		if _, present := variant.(map[string]any)["maximum"]; present {
			t.Error("local position bound must not add a schema maximum")
		}
	}
}

func TestIntegralLimitForms(t *testing.T) {
	for _, raw := range []string{`20`, `20.0`, `2e1`} {
		a, err := validate(json.RawMessage(`{"limit":`+raw+`}`), toolDefinition(t, "card_find"))
		if err != nil || a.limit != 20 {
			t.Fatalf("limit %s: got %d, %v", raw, a.limit, err)
		}
	}
}
