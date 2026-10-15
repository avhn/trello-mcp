package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/avhn/trello-mcp/internal/trello"
)

// arguments contains only validated, schema-declared values.
type arguments struct {
	strings map[string]string
	limit   int
	mine    bool
	extras  []string
}

// str returns an optional string argument with its empty default.
func (a arguments) str(name string) string { return a.strings[name] }

// full distinguishes curated full projections from the default summary.
func (a arguments) full() bool { return a.str("detail") == "full" }

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

var actionFilter = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(,[A-Za-z][A-Za-z0-9]*)*$`)

// validate rejects unknown fields, nulls, duplicate keys, bad types and unsafe path components.
func validate(raw json.RawMessage, d definition) (arguments, error) {
	a := arguments{strings: map[string]string{}}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if len(raw) > 16384 {
		return a, fmt.Errorf("arguments exceed 16 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return a, fmt.Errorf("arguments must be an object")
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
		if !ok || !allowed[key] || seen[key] {
			return a, fmt.Errorf("unknown or duplicate argument")
		}
		seen[key] = true
		var value json.RawMessage
		if err = dec.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return a, fmt.Errorf("invalid %s", key)
		}
		switch key {
		case "limit":
			if err = json.Unmarshal(value, &a.limit); err != nil {
				return a, fmt.Errorf("limit must be an integer")
			}
			_, max := maxLimit(d.name)
			if a.limit < 1 || a.limit > max {
				return a, fmt.Errorf("limit must be between 1 and %d", max)
			}
		case "mine":
			if err = json.Unmarshal(value, &a.mine); err != nil {
				return a, fmt.Errorf("mine must be a boolean")
			}
		case "extraFields":
			if err = json.Unmarshal(value, &a.extras); err != nil || len(a.extras) > 4 {
				return a, fmt.Errorf("extraFields must contain at most four names")
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
			if err = json.Unmarshal(value, &v); err != nil || len(v) > 4096 {
				return a, fmt.Errorf("invalid %s", key)
			}
			a.strings[key] = v
			if strings.HasSuffix(key, "Id") && !trello.ValidID(v) {
				return a, fmt.Errorf("invalid %s", key)
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
		if a.str(key) == "" {
			return a, fmt.Errorf("%s is required", key)
		}
	}
	if a.limit == 0 {
		a.limit, _ = maxLimit(d.name)
	}
	if detail := a.str("detail"); seen["detail"] {
		valid := detail == "summary" || detail == "full"
		if d.name == "server_health" {
			valid = detail == "basic" || detail == "detailed" || detail == "metadata" || detail == "performance"
		}
		if !valid {
			return a, fmt.Errorf("invalid detail")
		}
	}
	if v := a.str("view"); seen["view"] && v != "json" && v != "markdown" {
		return a, fmt.Errorf("view must be json or markdown")
	}
	if v := a.str("filter"); v != "" && !actionFilter.MatchString(v) {
		return a, fmt.Errorf("invalid action-type filter")
	}
	for _, key := range []string{"before", "since"} {
		v := a.str(key)
		if v != "" {
			_, err = time.Parse(time.RFC3339, v)
			if !trello.ValidID(v) && err != nil {
				return a, fmt.Errorf("invalid %s cursor", key)
			}
		}
	}
	if d.name == "card_find" && a.mine && (a.str("listId") != "" || a.str("boardId") != "") {
		return a, fmt.Errorf("mine cannot be combined with listId or boardId")
	}
	return a, nil
}

// schema publishes concise bounds; validate is the authoritative runtime trust boundary.
func schema(d definition) map[string]any {
	properties := map[string]any{}
	for _, key := range strings.Fields(d.params) {
		p := map[string]any{"type": "string"}
		switch key {
		case "boardId":
			p["description"] = "Defaults to the active board."
		case "limit":
			def, max := maxLimit(d.name)
			p = map[string]any{"type": "integer", "minimum": 1, "maximum": max, "default": def}
		case "mine":
			p = map[string]any{"type": "boolean"}
		case "extraFields":
			p = map[string]any{"type": "array", "maxItems": 4, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": []string{"stickers", "pluginData", "votes", "customFieldItems"}}}
		case "detail":
			values := []string{"summary", "full"}
			if d.name == "server_health" {
				values = []string{"basic", "detailed", "metadata", "performance"}
			}
			p["enum"] = values
		case "view":
			p["enum"] = []string{"json", "markdown"}
		case "name":
			p["default"] = "Acceptance Criteria"
		}
		properties[key] = p
	}
	out := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if d.required != "" {
		out["required"] = strings.Fields(d.required)
	}
	return out
}
