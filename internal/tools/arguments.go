package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avhn/trello-mcp/internal/trello"
)

// arguments contains only validated, schema-declared values.
type arguments struct {
	strings   map[string]string
	limit     int
	mine      bool
	extras    []string
	values    map[string]any
	cards     []arguments
	checklist *arguments
	items     []string
}

// str returns an optional string argument with its empty default.
func (a arguments) str(name string) string { return a.strings[name] }

// full distinguishes curated full projections from the default summary.
func (a arguments) full() bool { return a.str("detail") == "full" }

// includeDescriptions preserves previews unless explicitly disabled.
func (a arguments) includeDescriptions() bool {
	include, set := a.values["includeDesc"].(bool)
	return !set || include
}

const (
	// Trello Object Definitions documents card desc as "Up to 16384 chars".
	// Its Unicode counting unit is unspecified; our bound counts code points.
	cardDescriptionLimit = 16384
	// Trello documents no limit for the other human text fields below. This
	// provisional local ceiling is not an observed or documented Trello maximum.
	localTextLimit = 16384
)

// textLimit names semantic contexts for runtime text bounds. Zero
// leaves specialized protocol strings under their existing byte/type guards.
func textLimit(tool, key string) int {
	switch key {
	case "desc":
		switch tool {
		case "card_create", "card_update", "batch_item":
			return cardDescriptionLimit
		case "board_create":
			return localTextLimit
		}
	case "text":
		switch tool {
		case "comment_create", "comment_update", "field_value":
			return localTextLimit
		}
	case "nameFilter":
		return localTextLimit
	case "items":
		if tool == "card_checklist" {
			return localTextLimit
		}
	case "name":
		switch tool {
		case "board_create", "list_create", "list_update", "card_create", "card_update", "card_copy", "batch_item",
			"checklist_get", "checklist_create", "checklist_copy", "card_checklist", "checklist_item_create", "checklist_item_update",
			"label_create", "label_update", "attachment_add", "attachment_upload":
			return localTextLimit
		}
	}
	return 0
}

// argumentBytes keeps aggregate JSON bytes independent of text code points.
func argumentBytes(d definition) int {
	if d.name == "attachment_upload" {
		return 14 << 20
	}
	for _, key := range strings.Fields(d.params) {
		if textLimit(d.name, key) > 0 || key == "cards" || key == "checklist" || key == "value" {
			return 1 << 20
		}
	}
	return 16384
}

// maxLimit selects collection-specific defaults and hard ceilings.
func maxLimit(name string) (int, int) {
	switch name {
	case "card_find":
		return 100, 1000
	case "comment_find":
		return 20, 100
	default:
		return 20, 1000
	}
}

const actionFilterPattern = `^(all|[A-Za-z][A-Za-z0-9_]*(,[A-Za-z][A-Za-z0-9_]*)*)$`

var actionFilter = regexp.MustCompile(actionFilterPattern)

// validate rejects unknown fields, nulls, duplicate keys, bad types and unsafe path components.
func validate(raw json.RawMessage, d definition) (arguments, error) {
	return validateAt(raw, d, "arguments")
}

// argumentPath names nested values without adding noise to top-level errors.
func argumentPath(location, key string) string {
	if location == "arguments" {
		return key
	}
	return location + "." + key
}

func argumentType(key string, d definition) string {
	switch key {
	case "checked":
		if d.name == "field_value" {
			return "a string"
		}
		fallthrough
	case "closed", "subscribed", "dueComplete", "defaultLists", "mine", "includeDesc":
		return "a boolean"
	case "pos":
		return "a string or number"
	case "limit":
		return "an integer"
	case "idMembers", "idLabels", "extraFields", "items":
		return "an array of strings"
	case "cards":
		return "an array of objects"
	case "checklist":
		return "an object"
	case "value":
		return "a typed scalar object or an empty string"
	default:
		return "a string"
	}
}

// stringArray rejects nulls and reports the exact index of non-string elements.
func stringArray(raw json.RawMessage, path string) ([]string, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, fmt.Errorf("%s must be an array of strings", path)
	}
	out := make([]string, len(items))
	for i, item := range items {
		if bytes.Equal(item, []byte("null")) || json.Unmarshal(item, &out[i]) != nil {
			return nil, fmt.Errorf("%s[%d] must be a string", path, i)
		}
	}
	return out, nil
}

func validateAt(raw json.RawMessage, d definition, location string) (arguments, error) {
	a := arguments{strings: map[string]string{}, values: map[string]any{}}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if len(raw) > argumentBytes(d) {
		return a, fmt.Errorf("arguments exceed size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return a, fmt.Errorf("%s must be an object", location)
	}
	allowed := map[string]bool{}
	for _, k := range strings.Fields(d.params) {
		allowed[k] = true
	}
	seen := map[string]bool{}
	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return a, fmt.Errorf("invalid arguments")
		}
		key, ok := token.(string)
		if !ok {
			return a, fmt.Errorf("invalid argument key in %s", location)
		}
		if !allowed[key] {
			return a, fmt.Errorf("unknown argument %q in %s; allowed: %s", key, location, strings.Join(strings.Fields(d.params), ", "))
		}
		if seen[key] {
			return a, fmt.Errorf("duplicate argument %q in %s", key, location)
		}
		path := argumentPath(location, key)
		seen[key] = true
		var value json.RawMessage
		if err = dec.Decode(&value); err != nil {
			return a, fmt.Errorf("invalid JSON for %s", path)
		}
		if bytes.Equal(value, []byte("null")) {
			return a, fmt.Errorf("%s must be %s", path, argumentType(key, d))
		}
		switch key {
		case "checked":
			if d.name == "field_value" {
				var v string
				if json.Unmarshal(value, &v) != nil {
					return a, fmt.Errorf("%s must be a string", path)
				}
				a.strings[key], a.values[key] = v, v
				break
			}
			fallthrough
		case "closed", "subscribed", "dueComplete", "defaultLists", "includeDesc":
			var v bool
			if err = json.Unmarshal(value, &v); err != nil {
				return a, fmt.Errorf("%s must be a boolean", path)
			}
			a.values[key] = v
		case "pos":
			var v string
			if json.Unmarshal(value, &v) != nil {
				var n json.Number
				if json.Unmarshal(value, &n) != nil {
					return a, fmt.Errorf("%s must be a string or number", path)
				}
				v = n.String()
			}
			if n, e := strconv.ParseFloat(v, 64); v != "top" && v != "bottom" && (e != nil || !(n > 0 && n <= 1e20)) {
				return a, fmt.Errorf("%s must be top, bottom or a positive number", path)
			}
			a.values[key] = v
		case "idMembers", "idLabels":
			ids, e := stringArray(value, path)
			if e != nil {
				return a, e
			}
			if len(ids) > 100 {
				return a, fmt.Errorf("%s must contain at most 100 ids", path)
			}
			used := map[string]bool{}
			for _, id := range ids {
				if !trello.ValidID(id) || used[id] {
					return a, fmt.Errorf("invalid or duplicate id in %s", path)
				}
				used[id] = true
			}
			a.values[key] = ids
		case "value":
			v, e := fieldValue(value)
			if e != nil {
				return a, e
			}
			a.values[key] = v
		case "cards":
			var cards []json.RawMessage
			if json.Unmarshal(value, &cards) != nil {
				return a, fmt.Errorf("%s must be an array of objects", path)
			}
			if len(cards) < 1 || len(cards) > 100 {
				return a, fmt.Errorf("%s must contain 1 to 100 items", path)
			}
			for i, card := range cards {
				item, e := validateAt(card, definition{name: "batch_item", params: cardCreateParams, required: "name"}, fmt.Sprintf("%s[%d]", path, i))
				if e != nil {
					return a, e
				}
				a.cards = append(a.cards, item)
			}
		case "checklist":
			list, e := validateAt(value, definition{name: "card_checklist", params: "name items", required: "items"}, path)
			if e != nil {
				return a, e
			}
			if !list.has("name") {
				list.strings["name"] = "Checklist"
			}
			a.checklist = &list
		case "items":
			items, e := stringArray(value, path)
			if e != nil {
				return a, e
			}
			if len(items) < 1 || len(items) > 100 {
				return a, fmt.Errorf("%s must contain 1 to 100 strings", path)
			}
			for i, name := range items {
				max := textLimit(d.name, key)
				if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > max {
					return a, fmt.Errorf("%s[%d] must be a nonblank string of at most %d code points", path, i, max)
				}
			}
			a.items = items
		case "limit":
			if err = json.Unmarshal(value, &a.limit); err != nil {
				return a, fmt.Errorf("%s must be an integer", path)
			}
			_, max := maxLimit(d.name)
			if a.limit < 1 || a.limit > max {
				return a, fmt.Errorf("%s must be between 1 and %d", path, max)
			}
		case "mine":
			if err = json.Unmarshal(value, &a.mine); err != nil {
				return a, fmt.Errorf("%s must be a boolean", path)
			}
		case "extraFields":
			a.extras, err = stringArray(value, path)
			if err != nil {
				return a, err
			}
			if len(a.extras) > 4 {
				return a, fmt.Errorf("%s must contain at most four names", path)
			}
			used := map[string]bool{}
			for _, extra := range a.extras {
				if used[extra] || (extra != "stickers" && extra != "pluginData" && extra != "votes" && extra != "customFieldItems") {
					return a, fmt.Errorf("invalid extraFields")
				}
				used[extra] = true
			}
		default:
			var v string
			maxString := 4096
			if key == "base64" {
				maxString = 14 << 20
			}
			if err = json.Unmarshal(value, &v); err != nil {
				return a, fmt.Errorf("%s must be a string", path)
			}
			if max := textLimit(d.name, key); max > 0 {
				if utf8.RuneCountInString(v) > max {
					return a, fmt.Errorf("%s must be at most %d code points", path, max)
				}
			} else if len(v) > maxString {
				return a, fmt.Errorf("%s must be at most %d bytes", path, maxString)
			}
			if d.name == "card_checklist" && key == "name" && strings.TrimSpace(v) == "" {
				return a, fmt.Errorf("%s must not be blank", path)
			}
			a.strings[key] = v
			a.values[key] = v
			if (strings.HasSuffix(key, "Id") || key == "idBoard" || key == "idValue") && (key != "idValue" || v != "") && !trello.ValidID(v) {
				return a, fmt.Errorf("invalid %s", path)
			}
		}
	}
	if _, err = dec.Token(); err != nil {
		return a, fmt.Errorf("invalid arguments")
	}
	if _, err = dec.Token(); err != io.EOF {
		return a, fmt.Errorf("trailing arguments")
	}
	for _, key := range strings.Fields(d.required) {
		if !seen[key] || (key != "cards" && key != "items" && strings.TrimSpace(a.str(key)) == "") {
			return a, fmt.Errorf("%s is required", argumentPath(location, key))
		}
	}
	if a.limit == 0 {
		a.limit, _ = maxLimit(d.name)
	}
	if detail := a.str("detail"); seen["detail"] {
		valid := detail == "summary" || detail == "full"
		if d.name == "server_health" {
			valid = detail == "basic" || detail == "detailed" || detail == "metadata" || detail == "performance" || detail == "policy"
		}
		if !valid {
			return a, fmt.Errorf("invalid detail")
		}
	}
	if d.name == "card_find" && a.full() && !a.includeDescriptions() {
		return a, fmt.Errorf("includeDesc:false cannot be combined with detail:full")
	}
	if v := a.str("view"); seen["view"] && v != "json" && v != "markdown" {
		return a, fmt.Errorf("view must be json or markdown")
	}
	if v := a.str("filter"); seen["filter"] {
		if !actionFilter.MatchString(v) || (v != "all" && slices.Contains(strings.Split(v, ","), "all")) {
			return a, fmt.Errorf("filter must be all or comma-separated action identifiers without whitespace or all components")
		}
	}
	for _, key := range []string{"before", "since"} {
		v := a.str(key)
		if !seen[key] {
			continue
		}
		if d.name == "card_find" {
			if !trello.ValidID(v) {
				return a, fmt.Errorf("before must be a previously returned card id in the same filtered snapshot")
			}
			continue
		}
		if v == "" {
			continue
		}
		_, err = time.Parse(time.RFC3339, v)
		if !trello.ValidID(v) && err != nil {
			return a, fmt.Errorf("invalid %s cursor", key)
		}
	}
	if d.name == "card_find" && a.mine && (a.str("listId") != "" || a.str("boardId") != "") {
		return a, fmt.Errorf("mine cannot be combined with listId or boardId")
	}
	for _, key := range []string{"due", "start", "date"} {
		if v := a.str(key); v != "" {
			if _, e := time.Parse(time.RFC3339, v); e != nil {
				return a, fmt.Errorf("invalid %s date", argumentPath(location, key))
			}
		}
	}
	if v := a.str("state"); seen["state"] && v != "complete" && v != "incomplete" {
		return a, fmt.Errorf("state must be complete or incomplete")
	}
	return a, nil
}

// fieldValue validates the typed Trello scalar object, or an empty string to clear.
func fieldValue(raw json.RawMessage) (any, error) {
	if bytes.Equal(raw, []byte(`""`)) {
		return "", nil
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, fmt.Errorf("value must be a typed scalar object or an empty string")
	}
	a, err := validateAt(raw, definition{name: "field_value", params: "text number date checked"}, "value")
	if err != nil {
		return nil, err
	}
	if len(a.strings) != 1 {
		return nil, fmt.Errorf("value must contain exactly one of text, number, date or checked")
	}
	if v, ok := a.strings["number"]; ok {
		if !json.Valid([]byte(v)) || strings.TrimSpace(v) != v || (v[0] != '-' && (v[0] < '0' || v[0] > '9')) {
			return nil, fmt.Errorf("invalid custom-field number")
		}
		var n json.Number
		if json.Unmarshal([]byte(v), &n) != nil || v == "null" || v == "" {
			return nil, fmt.Errorf("invalid custom-field number")
		}
	}
	if v, ok := a.strings["date"]; ok && v == "" {
		return nil, fmt.Errorf("date must be RFC 3339; use an empty value string to clear")
	}
	if v, ok := a.strings["checked"]; ok && v != "true" && v != "false" {
		return nil, fmt.Errorf("checked must be true or false")
	}
	return a.values, nil
}

// boardDescription classifies tool contexts explicitly, never by identifier suffix.
func boardDescription(name string) string {
	switch name {
	case "board_use":
		return "Required active-board selection."
	case "board_get", "board_activity", "list_find", "member_find", "label_find", "customfield_find", "list_create", "label_create":
		return "Defaults to active board."
	case "card_find":
		return "With listId: expected owner; otherwise active board, unless mine."
	default:
		return "Optional expected owner; actual board verified."
	}
}

// schema publishes concise bounds; validate is the authoritative runtime trust boundary.
func schema(d definition) map[string]any {
	properties := map[string]any{}
	for _, key := range strings.Fields(d.params) {
		p := map[string]any{"type": "string"}
		switch key {
		case "boardId":
			p["description"] = boardDescription(d.name)
		case "before", "since":
			p["description"] = "Action id or RFC3339."
			if d.name == "card_find" {
				p["description"] = "Prior card id from same filtered snapshot, not a date."
				p["pattern"] = `^[A-Za-z0-9_-]{1,64}$`
			}
		case "filter":
			p["default"] = "all"
			p["pattern"] = actionFilterPattern
			p["not"] = map[string]any{"pattern": `(^all,|,all,|,all$)`}
			p["description"] = "Upstream action types, e.g. commentCard; all alone or comma-separated names (no spaces)."
		case "state":
			p["enum"] = []string{"complete", "incomplete"}
		case "limit":
			def, max := maxLimit(d.name)
			p = map[string]any{"type": "integer", "minimum": 1, "maximum": max, "default": def}
		case "mine":
			p = map[string]any{"type": "boolean", "description": "My authorized cards; excludes boardId/listId."}
		case "includeDesc":
			p = map[string]any{"type": "boolean", "default": true}
		case "closed", "subscribed", "dueComplete", "checked", "defaultLists":
			p = map[string]any{"type": "boolean"}
		case "pos":
			p = map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number", "exclusiveMinimum": 0}}}
		case "idMembers", "idLabels":
			p = map[string]any{"type": "array", "maxItems": 100, "uniqueItems": true, "items": map[string]any{"type": "string"}}
		case "checklist":
			p = schema(definition{name: "card_checklist", params: "name items", required: "items"})
		case "items":
			p = map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "string", "minLength": 1}}
		case "cards":
			p = map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": schema(definition{name: "batch_item", params: cardCreateParams, required: "name"})}
		case "value":
			props := map[string]any{}
			for _, name := range []string{"text", "number", "date", "checked"} {
				props[name] = map[string]any{"type": "string"}
			}
			p = map[string]any{"anyOf": []any{map[string]any{"type": "string", "enum": []string{""}}, map[string]any{"type": "object", "properties": props, "additionalProperties": false, "minProperties": 1, "maxProperties": 1}}}
		case "extraFields":
			p = map[string]any{"type": "array", "maxItems": 4, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": []string{"stickers", "pluginData", "votes", "customFieldItems"}}}
		case "detail":
			values := []string{"summary", "full"}
			if d.name == "server_health" {
				values = []string{"basic", "detailed", "metadata", "performance", "policy"}
			}
			p["enum"] = values
		case "view":
			p["enum"] = []string{"json", "markdown"}
		case "name":
			switch d.name {
			case "checklist_get":
				p["default"] = "Acceptance Criteria"
			case "card_checklist":
				p["default"] = "Checklist"
			}
		}
		properties[key] = p
	}
	out := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if d.required != "" {
		out["required"] = strings.Fields(d.required)
	}
	return out
}
