package tools

import (
	"encoding/json"
	"fmt"
	"strings"
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
		{"card_find", `{"limit":"1"}`, `limit must be an integer`},
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
	for _, d := range (&Service{}).definitions() {
		props := schema(d)["properties"].(map[string]any)
		if p, ok := props["boardId"]; ok {
			desc := p.(map[string]any)["description"].(string)
			switch {
			case d.name == "board_use":
				if !strings.Contains(desc, "Required active-board selection") || d.required != "boardId" {
					t.Errorf("selection schema: %v", p)
				}
			case d.name == "card_find":
				for _, word := range []string{"listId", "expected owner", "active board", "mine"} {
					if !strings.Contains(desc, word) {
						t.Errorf("card discovery schema missing %s: %v", word, p)
					}
				}
			case active[d.name]:
				if desc != "Defaults to active board." {
					t.Errorf("%s: wrong active-board default: %v", d.name, p)
				}
			default:
				if !strings.Contains(desc, "Optional expected owner; actual board verified.") || strings.Contains(desc, "active board") {
					t.Errorf("%s: wrong object constraint: %v", d.name, p)
				}
			}
		}
	}
	props := schema(toolDefinition(t, "card_find"))["properties"].(map[string]any)
	if !strings.Contains(props["mine"].(map[string]any)["description"].(string), "excludes boardId/listId") || !strings.Contains(props["before"].(map[string]any)["description"].(string), "same filtered snapshot, not a date") {
		t.Fatalf("card discovery exclusivity/cursor hints missing: %v", props)
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
