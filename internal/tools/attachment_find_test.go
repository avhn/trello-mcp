package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/view"
)

func TestAttachmentFindOwnership(t *testing.T) {
	for _, mode := range []string{"denied", "constraint", "card_move", "board_move", "retry"} {
		t.Run(mode, func(t *testing.T) {
			var fetched atomic.Int32
			var cards atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/cards/c1":
					cards.Add(1)
					board := "b1"
					if mode == "card_move" && fetched.Load() > 0 {
						board = "b2"
					}
					_, _ = w.Write([]byte(`{"idBoard":"` + board + `"}`))
				case "/boards/b2":
					_, _ = w.Write([]byte(`{"id":"b2","idOrganization":"w2"}`))
				case "/boards/b1":
					workspace := "w1"
					if mode == "denied" || ((mode == "board_move" || mode == "retry") && fetched.Load() > 0) {
						workspace = "w2"
					}
					_, _ = w.Write([]byte(`{"id":"b1","idOrganization":"` + workspace + `"}`))
				case "/organizations/w1":
					_, _ = w.Write([]byte(workspaceJSON))
				case "/organizations/w2":
					_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
				case "/cards/c1/attachments":
					if fetched.Add(1) == 1 && mode == "retry" {
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					_, _ = w.Write([]byte(`[{"id":"private","name":"must not escape"}]`))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			s := testService(t, fake, []string{"w1"})
			a := arguments{strings: map[string]string{"cardId": "c1"}}
			if mode == "constraint" {
				a.strings["boardId"] = "b2"
			}
			out, err := s.attachmentFind(context.Background(), a)
			if err == nil || out != nil {
				t.Fatalf("unauthorized metadata returned: %v, %v", out, err)
			}
			if mode != "constraint" && (strings.Contains(err.Error(), "b1") || strings.Contains(err.Error(), "b2") || strings.Contains(err.Error(), "w2")) {
				t.Fatalf("denial leaked resolved scope: %v", err)
			}
			wantFetch, wantCards := int32(1), int32(2)
			switch mode {
			case "denied", "constraint":
				wantFetch, wantCards = 0, 1
			case "retry":
				wantFetch = 2
			}
			if fetched.Load() != wantFetch || cards.Load() != wantCards {
				t.Errorf("requests: attachments=%d cards=%d; want %d %d", fetched.Load(), cards.Load(), wantFetch, wantCards)
			}
		})
	}
}

func TestAttachmentFindEmpty(t *testing.T) {
	for _, body := range []string{"[]", "null"} {
		t.Run(body, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/cards/c1":
					_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/cards/c1/attachments":
					_, _ = w.Write([]byte(body))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			s := testService(t, fake, nil)
			out, err := s.attachmentFind(context.Background(), arguments{strings: map[string]string{"cardId": "c1"}})
			if err != nil {
				t.Fatal(err)
			}
			text, err := view.Encode(out)
			if err != nil || strings.TrimSpace(text) != `{"items":[]}` {
				t.Fatalf("empty collection: %s, %v", text, err)
			}
		})
	}
}
