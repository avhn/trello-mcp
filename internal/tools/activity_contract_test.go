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
	if contract.BoardID == "" || len(contract.Input) < 2 {
		t.Fatal("example needs a resolved board and repeated entities within a page")
	}
	actual, err := json.Marshal(view.ProjectActions(contract.Input, contract.BoardID))
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
	// Paging must reset name inheritance, even after projecting the complete example.
	page := view.ProjectActions(contract.Input[1:], contract.BoardID)
	first := view.ProjectAction(contract.Input[1], contract.BoardID)
	if !reflect.DeepEqual(page[0], first) {
		t.Fatalf("first occurrence on a fresh page is incomplete: %+v", page[0])
	}
}
