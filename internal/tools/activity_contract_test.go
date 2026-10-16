package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/avhn/trello-mcp/internal/view"
)

// TestDocumentedActivityProjection executes the synthetic example, not prose keywords.
func TestDocumentedActivityProjection(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "tools.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := "<!-- activity-example:start -->", "<!-- activity-example:end -->"
	if strings.Count(text, start) != 1 || strings.Count(text, end) != 1 {
		t.Fatal("activity example markers missing or ambiguous")
	}
	_, example, _ := strings.Cut(text, start)
	example, _, found := strings.Cut(example, end)
	if !found || strings.Count(example, "```json\n") != 1 {
		t.Fatal("activity example must contain one JSON block")
	}
	_, example, _ = strings.Cut(example, "```json\n")
	example, _, found = strings.Cut(example, "\n```")
	if !found {
		t.Fatal("activity example JSON fence is unclosed")
	}
	var contract struct {
		BoardID string          `json:"boardId"`
		Input   []view.Action   `json:"input"`
		Output  json.RawMessage `json:"output"`
	}
	dec := json.NewDecoder(strings.NewReader(example))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&contract); err != nil {
		t.Fatal(err)
	}
	if contract.BoardID == "" || len(contract.Input) < 3 {
		t.Fatal("example needs a resolved board, ID-only references and repeated named entities within a page")
	}
	projected := view.ProjectActions(contract.Input, contract.BoardID)
	actual, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contract.Output, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("documented output differs from ProjectActions\ngot: %s\nwant: %s", actual, contract.Output)
	}
	// The first reference must show that upstream names can be unknown, not inherited.
	initial := contract.Input[0].Data
	if initial.Card == nil || initial.Card.ID == "" || initial.Card.Name != "" || initial.List == nil || initial.List.ID == "" || initial.List.Name != "" {
		t.Fatal("example must start with ID-only card and list references")
	}
	supplied, repeated := false, false
	for i, action := range contract.Input {
		if action.Data.Card != nil && action.Data.Card.ID == initial.Card.ID && action.Data.Card.Name != "" && action.Data.List != nil && action.Data.List.ID == initial.List.ID && action.Data.List.Name != "" {
			if supplied && projected[i].Card == "" && projected[i].List == "" {
				repeated = true
			}
			supplied = true
		}
		// Every nonempty suffix is a new page, even after projecting the full example.
		page := view.ProjectActions(contract.Input[i:], contract.BoardID)
		first := view.ProjectAction(action, contract.BoardID)
		if !reflect.DeepEqual(page[0], first) {
			t.Fatalf("suffix %d reused a previous page's names: got %+v, want %+v", i, page[0], first)
		}
	}
	if !supplied || !repeated {
		t.Fatal("example must supply and then repeat names after its ID-only references")
	}
}
