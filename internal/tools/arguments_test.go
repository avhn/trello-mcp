package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
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
			{"maximum names", `{"items":[` + strings.Repeat(`"`+strings.Repeat("x", 4096)+`",`, 99) + `"last"]}`, true},
			{"empty", `{"items":[]}`, false},
			{"too many", `{"items":[` + strings.Repeat(`"x",`, 100) + `"x"]}`, false},
			{"null", `{"items":null}`, false},
			{"blank item", `{"items":["  "]}`, false},
			{"oversized item", `{"items":["` + strings.Repeat("x", 4097) + `"]}`, false},
			{"blank name", `{"name":" ","items":["x"]}`, false},
			{"oversized name", `{"name":"` + strings.Repeat("x", 4097) + `","items":["x"]}`, false},
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
		if checklist["additionalProperties"] != false || props["name"].(map[string]any)["default"] != "Checklist" || items["minItems"] != 1 || items["maxItems"] != 100 {
			t.Errorf("%s: incomplete checklist schema: %v", tool, checklist)
		}
	}
}
