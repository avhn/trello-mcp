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
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/avhn/trello-mcp/internal/config"
	"github.com/avhn/trello-mcp/internal/state"
	"github.com/avhn/trello-mcp/internal/tools"
	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// update is deliberately opt-in; the environment alias supports a single whole-suite test invocation.
var update = flag.Bool("update", os.Getenv("TRELLO_BENCH_UPDATE") == "1", "update the README raw-reference measurements")

const discoveryProfile = "workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,checklist_get,checklist_items,server_health"

// estimatedTokens rounds the chars/4 estimate consistently for every benchmark table.
func estimatedTokens(chars int) string {
	return fmt.Sprintf("~%.0f", float64(chars)/4)
}

func TestEstimatedTokens(t *testing.T) {
	for _, tc := range []struct {
		chars int
		want  string
	}{{0, "~0"}, {3, "~1"}, {141, "~35"}, {24254, "~6064"}} {
		if got := estimatedTokens(tc.chars); got != tc.want {
			t.Errorf("estimatedTokens(%d) = %s, want %s", tc.chars, got, tc.want)
		}
	}
}

// TestSizes compares six matched synthetic responses, not executions of other servers.
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
		data := fixture(name)
		// Model upstream filtering before paging; never compare a filtered page with all actions.
		if r.URL.Path == "/boards/b1/actions" && r.URL.Query().Get("filter") != "all" {
			var actions []view.Action
			if err := json.Unmarshal(data, &actions); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			filter := strings.Split(r.URL.Query().Get("filter"), ",")
			selected := make([]view.Action, 0, len(actions))
			for _, action := range actions {
				if slices.Contains(filter, action.Type) {
					selected = append(selected, action)
				}
			}
			if err := json.NewEncoder(w).Encode(selected); err != nil {
				t.Error(err)
			}
			return
		}
		_, _ = w.Write(data)
	}))
	defer fake.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	launch := func(profile []string) *mcp.ClientSession {
		t.Helper()
		store, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.json"), "b1")
		if err != nil {
			t.Fatal(err)
		}
		service := tools.New(config.Config{APIKey: "synthetic-key", Token: "synthetic-token", Tools: profile}, trello.NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), store)
		server := mcp.NewServer(&mcp.Implementation{Name: "trello-mcp", Version: tools.Version}, &mcp.ServerOptions{Instructions: service.Instructions()})
		if err = service.Register(server); err != nil {
			t.Fatal(err)
		}
		a, b := mcp.NewInMemoryTransports()
		ss, err := server.Connect(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		client := mcp.NewClient(&mcp.Implementation{Name: "bench", Version: "1"}, nil)
		cs, err := client.Connect(ctx, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		if cs.InitializeResult().Instructions != service.Instructions() {
			t.Fatal("initialize instructions differ from service startup policy")
		}
		return cs
	}
	cs := launch([]string{"all"})
	profile := launch(strings.Split(discoveryProfile, ","))
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
	marshal := func(value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	summary := call("card_find", map[string]any{"listId": "l1"})
	full := call("card_find", map[string]any{"listId": "l1", "detail": "full"})
	boardActions := call("board_activity", map[string]any{"limit": 50})
	rows := []struct{ name, fixture, output string }{
		{"20 boards", "boards_20", call("board_find", map[string]any{})},
		{"30 cards in a list", "cards_30", summary},
		{"20 comments", "comments_20", call("comment_find", map[string]any{"cardId": "c1", "limit": 20})},
		{"50 board actions", "actions_50", boardActions},
		{"22 card actions", "history_22", call("card_history", map[string]any{"cardId": "c1", "limit": 22})},
		{"Card with 20 comments", "card_with_comments", call("card_get", map[string]any{"cardId": "c1", "detail": "full"}) + call("comment_find", map[string]any{"cardId": "c1", "limit": 20})},
	}
	var table strings.Builder
	table.WriteString("| Scenario | Raw Trello JSON | this server | reduction |\n| --- | --- | --- | --- |\n")
	t.Log("Scenario | raw chars | raw tokens (chars/4) | projected chars | projected tokens (chars/4)")
	for _, row := range rows {
		chars := utf8.RuneCountInString(row.output)
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, bytes.TrimSpace(fixture(row.fixture)), "", "  "); err != nil {
			t.Fatal(err)
		}
		raw := utf8.RuneCountInString(pretty.String())
		t.Logf("%s | %d | %s | %d | %s", row.name, raw, estimatedTokens(raw), chars, estimatedTokens(chars))
		if chars >= raw {
			t.Errorf("%s did not reduce response size", row.name)
		}
		fmt.Fprintf(&table, "| %s | %s | %s | %.1f%% |\n", row.name, estimatedTokens(raw), estimatedTokens(chars), 100*(1-float64(chars)/float64(raw)))
	}
	table.WriteString("\nSynthetic raw pretty JSON is a reproducible reference, not measured output from another MCP server. Tokens use chars/4.\n\n")
	table.WriteString("| Startup cost | Default (48 tools) | Read-oriented discovery (10 tools) |\n| --- | --- | --- |\n")
	cost := func(label, left, right string) {
		l, r := utf8.RuneCountInString(left), utf8.RuneCountInString(right)
		t.Logf("%s | default: %d chars, %s tokens | profile: %d chars, %s tokens", label, l, estimatedTokens(l), r, estimatedTokens(r))
		fmt.Fprintf(&table, "| %s | %d chars (%s tokens) | %d chars (%s tokens) |\n", label, l, estimatedTokens(l), r, estimatedTokens(r))
	}
	catalog := func(session *mcp.ClientSession, count int) string {
		t.Helper()
		definitions, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(definitions.Tools) != count {
			t.Fatalf("catalog: got %d tools, want %d", len(definitions.Tools), count)
		}
		if count == 10 {
			for _, tool := range definitions.Tools {
				if !slices.Contains(strings.Split(discoveryProfile, ","), tool.Name) {
					t.Errorf("unexpected discovery profile tool: %s", tool.Name)
				}
			}
		}
		return marshal(definitions)
	}
	cost("tools/list", catalog(cs, 48), catalog(profile, 10))
	cost("Initialize instructions", cs.InitializeResult().Instructions, profile.InitializeResult().Instructions)
	cost("Initialize result (including instructions)", marshal(cs.InitializeResult()), marshal(profile.InitializeResult()))
	table.WriteString("\n| Independent response | Characters | Tokens (chars/4) |\n| --- | --- | --- |\n")
	for _, row := range []struct{ name, output string }{
		{"card_find summary (30 cards)", summary},
		{"card_find full (30 cards)", full},
		{"board_activity all (50 actions)", boardActions},
		{"board_activity commentCard", call("board_activity", map[string]any{"filter": "commentCard", "limit": 50})},
		{"board_activity updateCard", call("board_activity", map[string]any{"filter": "updateCard", "limit": 50})},
	} {
		chars := utf8.RuneCountInString(row.output)
		t.Logf("%s | %d chars | %s tokens", row.name, chars, estimatedTokens(chars))
		fmt.Fprintf(&table, "| %s | %d | %s |\n", row.name, chars, estimatedTokens(chars))
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
