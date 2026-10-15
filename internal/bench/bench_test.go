// Package bench measures actual registered tool responses using synthetic Trello fixtures.
package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/tools"
	"github.com/avhn/trello-mcp/internal/trello"
)

// update is deliberately opt-in; the environment alias supports a single whole-suite test invocation.
var update = flag.Bool("update", os.Getenv("TRELLO_BENCH_UPDATE") == "1", "update the README token comparison table")

// TestSizes measures raw pretty JSON against text returned by real MCP tools/call handlers.
func TestSizes(t *testing.T) {
	root := filepath.Join("..", "..")
	fixture := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "testdata", "trello", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := ""
		switch r.URL.Path {
		case "/members/me/boards":
			name = "boards_20"
		case "/boards/b1":
			name = "board"
		case "/lists/l1":
			name = "list"
		case "/lists/l1/cards":
			name = "cards_30"
		case "/boards/b1/actions":
			name = "actions_50"
		case "/cards/c1":
			name = "card_with_comments"
		case "/cards/c1/actions":
			name = "history_22"
			if r.URL.Query().Get("filter") == "commentCard" {
				name = "comments_20"
			}
		default:
			t.Errorf("unexpected bench path %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(fixture(name))
	}))
	defer fake.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.json"), "b1")
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "bench", Version: "1"}, nil)
	service := tools.New(config.Config{APIKey: "synthetic-key", Token: "synthetic-token", Tools: []string{"all"}}, trello.NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), store)
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "bench", Version: "1"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	call := func(name string, args map[string]any) string {
		t.Helper()
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError || result.StructuredContent != nil || len(result.Content) != 1 {
			t.Fatalf("bad result: %+v", result)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatal("not text-only")
		}
		return text.Text
	}
	definitions, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct{ name, fixture, output string }{
		{"20 boards", "boards_20", call("board_find", map[string]any{})},
		{"30 cards in a list", "cards_30", call("card_find", map[string]any{"listId": "l1"})},
		{"20 comments", "comments_20", call("comment_find", map[string]any{"cardId": "c1", "limit": 20})},
		{"50 board actions", "actions_50", call("board_activity", map[string]any{"limit": 50})},
		{"22 card actions", "history_22", call("card_history", map[string]any{"cardId": "c1", "limit": 22})},
		{"Card with 20 comments", "card_with_comments", call("card_get", map[string]any{"cardId": "c1", "detail": "full"}) + call("comment_find", map[string]any{"cardId": "c1", "limit": 20})},
		{"Tool definitions", "", string(defs)},
	}
	var table strings.Builder
	table.WriteString("| Scenario | delorenj 1.8.1 style | this server | reduction |\n| --- | --- | --- | --- |\n")
	t.Log("Scenario | raw chars | raw tokens (chars/4) | projected chars | projected tokens (chars/4)")
	for _, row := range rows {
		chars := utf8.RuneCountInString(row.output)
		ours := fmt.Sprintf("~%.2fk", float64(chars)/4000)
		node, reduction := "57 tools, ~7.3k (static audit)", "not comparable"
		if row.fixture != "" {
			// Preserve fixture property order, as JSON.stringify(obj, null, 2) does.
			var pretty bytes.Buffer
			if err = json.Indent(&pretty, bytes.TrimSpace(fixture(row.fixture)), "", "  "); err != nil {
				t.Fatal(err)
			}
			raw := utf8.RuneCountInString(pretty.String())
			node = fmt.Sprintf("~%.2fk", float64(raw)/4000)
			reduction = fmt.Sprintf("%.1f%%", 100*(1-float64(chars)/float64(raw)))
			t.Logf("%s | %d | %.2f | %d | %.2f", row.name, raw, float64(raw)/4, chars, float64(chars)/4)
			if chars >= raw {
				t.Errorf("%s did not reduce response size", row.name)
			}
		} else {
			ours = fmt.Sprintf("%d tools, %s", len(definitions.Tools), ours)
			t.Logf("%s | measured registered tools/list: %d chars, %.2f tokens", row.name, chars, float64(chars)/4)
		}
		fmt.Fprintf(&table, "| %s | %s | %s | %s |\n", row.name, node, ours, reduction)
	}
	if *update {
		path := filepath.Join(root, "README.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		start, end := "<!-- bench:start -->", "<!-- bench:end -->"
		text := string(data)
		i, j := strings.Index(text, start), strings.Index(text, end)
		if i < 0 || j < i || strings.Count(text, start) != 1 || strings.Count(text, end) != 1 {
			t.Fatal("README bench markers missing or ambiguous")
		}
		text = text[:i+len(start)] + "\n\n" + table.String() + "\n" + text[j:]
		if err = os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
