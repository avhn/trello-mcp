package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/trello"
	"github.com/avhn/trello-mcp/internal/view"
)

// TestBoardOwnershipRechecked rejects a board moved out of the allowlist between calls.
func TestBoardOwnershipRechecked(t *testing.T) {
	var boards atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/boards/b1":
			workspace := "w1"
			if boards.Add(1) > 1 {
				workspace = "w2"
			}
			_, _ = w.Write([]byte(`{"id":"b1","idOrganization":"` + workspace + `"}`))
		case "/organizations/w1":
			_, _ = w.Write([]byte(workspaceJSON))
		case "/organizations/w2":
			_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, []string{"w1"})
	a := arguments{strings: map[string]string{}}
	if _, err := s.boardGet(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.boardGet(context.Background(), a); !errors.Is(err, trello.ErrDenied) {
		t.Errorf("stale ownership grant accepted: %v", err)
	}
}

// TestCardOwnershipChanges rejects a different owner in a data-bearing response.
func TestCardOwnershipChanges(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cards/c1":
			if r.URL.Query().Get("fields") == "idBoard" {
				_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
			} else {
				_, _ = w.Write([]byte(`{"id":"c1","idBoard":"b2","name":"must not escape"}`))
			}
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	out, err := s.cardGet(context.Background(), arguments{strings: map[string]string{"cardId": "c1"}})
	if err == nil || out != nil {
		t.Fatalf("changed ownership returned content: %v, %v", out, err)
	}
}

// TestCardActionOwnershipChanges rejects moves during action reads, including 429 recovery.
func TestCardActionOwnershipChanges(t *testing.T) {
	for _, tool := range []string{"card_history", "comment_find"} {
		for _, retry := range []bool{false, true} {
			name := tool
			if retry {
				name += "/retry"
			}
			t.Run(name, func(t *testing.T) {
				var moved atomic.Bool
				var cards, actions atomic.Int32
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/cards/c1":
						cards.Add(1)
						board := "b1"
						if moved.Load() {
							board = "b2"
						}
						_, _ = w.Write([]byte(`{"idBoard":"` + board + `"}`))
					case "/boards/b1":
						_, _ = w.Write([]byte(boardJSON))
					case "/boards/b2":
						_, _ = w.Write([]byte(`{"id":"b2","idOrganization":"w2"}`))
					case "/organizations/w1":
						_, _ = w.Write([]byte(workspaceJSON))
					case "/organizations/w2":
						_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
					case "/cards/c1/actions":
						current := actions.Add(1)
						moved.Store(true)
						if retry && current == 1 {
							w.Header().Set("Retry-After", "0")
							w.WriteHeader(http.StatusTooManyRequests)
							return
						}
						_, _ = w.Write([]byte(`[` + strings.ReplaceAll(actionJSON, `"b1"`, `"b2"`) + `]`))
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
						w.WriteHeader(500)
					}
				}))
				defer fake.Close()
				s := testService(t, fake, []string{"w1"})
				a := arguments{strings: map[string]string{"cardId": "c1"}, limit: 20}
				call := s.cardHistory
				if tool == "comment_find" {
					call = s.commentFind
				}
				out, err := call(context.Background(), a)
				if err == nil || out != nil {
					t.Fatalf("changed ownership returned actions: %v, %v", out, err)
				}
				wantActions := int32(1)
				if retry {
					wantActions++
				}
				if cards.Load() != 2 || actions.Load() != wantActions {
					t.Errorf("requests: cards=%d, actions=%d; want 2, %d", cards.Load(), actions.Load(), wantActions)
				}
			})
		}
	}
}

// TestCardListOwnership rejects cards whose list differs from the resolved list endpoint.
func TestCardListOwnership(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lists/l1":
			_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		case "/lists/l1/cards":
			_, _ = w.Write([]byte(`[{"id":"c1","idBoard":"b1","idList":"l2","name":"moved"}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	out, err := s.cardFind(context.Background(), arguments{strings: map[string]string{"listId": "l1"}, limit: 1})
	if err == nil || err.Error() != "card list unverifiable" || out != nil {
		t.Fatalf("unverifiable list returned content: %v, %v", out, err)
	}
}

// TestAssignedCardAllowlist filters denied cards before computing the page and cursor.
func TestAssignedCardAllowlist(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/members/me/cards":
			_, _ = w.Write([]byte(`[{"id":"private","name":"Denied card","idBoard":"b2"},` + cardJSON + `]`))
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		case "/boards/b2":
			_, _ = w.Write([]byte(`{"id":"b2","idOrganization":"w2"}`))
		case "/organizations/w1":
			_, _ = w.Write([]byte(workspaceJSON))
		case "/organizations/w2":
			_, _ = w.Write([]byte(`{"id":"w2","name":"denied"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	s := testService(t, fake, []string{"w1"})
	out, err := s.cardFind(context.Background(), arguments{strings: map[string]string{}, mine: true, limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, err := view.Encode(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"items":[`+cardJSON+`]}` || strings.Contains(got, "private") {
		t.Errorf("denied card affected page: %s", got)
	}
}
