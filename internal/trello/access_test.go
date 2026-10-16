package trello

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOwnershipDenialsHideResolvedScopes(t *testing.T) {
	for _, kind := range []string{"cards", "lists", "labels", "checklists", "checklist-fallback", "customFields", "actions", "boards"} {
		for _, expected := range []string{"", "supplied-board"} {
			for _, ownership := range []string{"denied", "missing-workspace", "missing-board", "invalid-workspace"} {
				t.Run(kind+"/"+expected+"/"+ownership, func(t *testing.T) {
					fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/boards/hidden-board", "/boards/supplied-id":
							if ownership == "missing-board" {
								_, _ = w.Write([]byte(`{}`))
								return
							}
							workspace := "hidden-workspace"
							if ownership == "invalid-workspace" {
								workspace = ""
							}
							_, _ = fmt.Fprintf(w, `{"id":%q,"idOrganization":%q}`, strings.TrimPrefix(r.URL.Path, "/boards/"), workspace)
						case "/organizations/hidden-workspace":
							if ownership == "missing-workspace" {
								_, _ = w.Write([]byte(`{}`))
								return
							}
							_, _ = w.Write([]byte(`{"id":"hidden-workspace","name":"private-name"}`))
						case "/cards/hidden-card":
							_, _ = w.Write([]byte(`{"idBoard":"hidden-board"}`))
						default:
							switch kind {
							case "actions":
								_, _ = w.Write([]byte(`{"data":{"board":{"id":"hidden-board"},"card":{"id":"hidden-card"}}}`))
							case "checklist-fallback":
								_, _ = w.Write([]byte(`{"idCard":"hidden-card"}`))
							case "customFields":
								_, _ = w.Write([]byte(`{"idModel":"hidden-board","modelType":"board"}`))
							default:
								_, _ = w.Write([]byte(`{"idBoard":"hidden-board"}`))
							}
						}
					}))
					defer fake.Close()
					a := &Access{Client: NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), Allowed: []string{"team"}}
					var err error
					resolver := kind
					if kind == "boards" {
						resolver = "board"
						_, err = a.Board(context.Background(), "supplied-id", false)
					} else {
						if kind == "checklist-fallback" {
							resolver = "checklists"
						}
						_, err = a.ObjectBoard(context.Background(), resolver, "supplied-id", expected)
					}
					if err == nil {
						t.Fatal("unverifiable or denied ownership accepted")
					}
					if ownership == "denied" {
						want := a.workspaceDenied(strings.TrimSuffix(resolver, "s") + " supplied-id").Error()
						if !errors.Is(err, ErrDenied) || err.Error() != want {
							t.Fatalf("got %v, want %s", err, want)
						}
					} else if !strings.Contains(err.Error(), "ownership verification") {
						t.Fatalf("wrong ownership refusal: %v", err)
					}
					for _, hidden := range []string{"hidden-board", "hidden-workspace", "hidden-card", "private-name", "supplied-board"} {
						if strings.Contains(err.Error(), hidden) {
							t.Errorf("denial leaked %q: %v", hidden, err)
						}
					}
				})
			}
		}
	}
}

func TestAllowedObjectBoardConstraint(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cards/c1":
			_, _ = w.Write([]byte(`{"idBoard":"allowed-board"}`))
		case "/boards/allowed-board":
			_, _ = w.Write([]byte(`{"id":"allowed-board","idOrganization":"allowed-workspace"}`))
		case "/organizations/allowed-workspace":
			_, _ = w.Write([]byte(`{"id":"allowed-workspace","name":"team"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	a := &Access{Client: NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), Allowed: []string{"team"}}
	_, err := a.ObjectBoard(context.Background(), "cards", "c1", "wrong-board")
	if err == nil || !strings.Contains(err.Error(), "object c1 belongs to board allowed-board, not the supplied boardId wrong-board") {
		t.Fatalf("wrong constraint refusal: %v", err)
	}
}

func TestActionDeniesBoardBeforeCardConstraint(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/actions/a1":
			_, _ = w.Write([]byte(`{"data":{"board":{"id":"hidden-board"},"card":{"id":"hidden-card"}}}`))
		case "/boards/hidden-board":
			_, _ = w.Write([]byte(`{"id":"hidden-board","idOrganization":"hidden-workspace"}`))
		case "/organizations/hidden-workspace":
			_, _ = w.Write([]byte(`{"id":"hidden-workspace","name":"private-name"}`))
		default:
			t.Errorf("resolved card fetched before action board authorization: %s", r.URL.Path)
			_, _ = w.Write([]byte(`{"idBoard":"allowed-board"}`))
		}
	}))
	defer fake.Close()
	a := &Access{Client: NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), Allowed: []string{"team"}}
	_, err := a.ObjectBoard(context.Background(), "actions", "a1", "supplied-board")
	if err == nil || err.Error() != a.workspaceDenied("action a1").Error() {
		t.Fatalf("wrong action denial: %v", err)
	}
}

func TestScopeErrorDoesNotMisclassifyUpstreamFailures(t *testing.T) {
	a := &Access{Allowed: []string{"team"}}
	got := a.ScopeError(&HTTPError{StatusCode: http.StatusForbidden}, "card c1")
	if errors.Is(got, ErrDenied) || !strings.Contains(got.Error(), "token/permission issue, not server policy") {
		t.Fatalf("upstream failure misclassified or exposed: %v", got)
	}
	assertConfiguredAuthError(t, got, http.StatusForbidden)
}

func TestNestedTransportFailuresHideResolvedScopes(t *testing.T) {
	for _, failure := range []error{io.EOF, context.DeadlineExceeded} {
		for _, board := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/board=%t", failure, board), func(t *testing.T) {
				client := NewClient("https://example.invalid", "synthetic-key", "synthetic-token", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					var body string
					switch req.URL.Path {
					case "/cards/supplied-id":
						body = `{"idBoard":"hidden-board"}`
					case "/boards/hidden-board", "/boards/supplied-id":
						body = fmt.Sprintf(`{"id":%q,"idOrganization":"hidden-workspace"}`, strings.TrimPrefix(req.URL.Path, "/boards/"))
					case "/organizations/hidden-workspace":
						return nil, failure
					default:
						t.Errorf("unexpected request: %s", req.URL.Path)
						return nil, io.ErrUnexpectedEOF
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
				})})
				a := &Access{Client: client, Allowed: []string{"team"}}
				var err error
				if board {
					_, err = a.Board(context.Background(), "supplied-id", false)
				} else {
					_, err = a.ObjectBoard(context.Background(), "cards", "supplied-id", "")
				}
				want := "supplied-id ownership could not be verified (temporary error); retry"
				if errors.Is(failure, context.DeadlineExceeded) {
					want = "supplied-id ownership verification deadline exceeded"
				}
				if err == nil || errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), want) || !errors.Is(err, failure) {
					t.Fatalf("unexpected resolution failure: %v", err)
				}
				for _, hidden := range []string{"hidden-board", "hidden-workspace", "http", "/organizations", "policy", "synthetic-key", "synthetic-token"} {
					if strings.Contains(err.Error(), hidden) {
						t.Errorf("resolution failure exposed %q: %v", hidden, err)
					}
				}
			})
		}
	}
}

func TestNestedHTTPFailuresPreserveSafeDiagnosis(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests := 0
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/cards/supplied-id":
					_, _ = io.WriteString(w, `{"idBoard":"hidden-board"}`)
				case "/boards/hidden-board":
					_, _ = io.WriteString(w, `{"id":"hidden-board","idOrganization":"hidden-workspace"}`)
				case "/organizations/hidden-workspace":
					requests++
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, "hidden-board hidden-workspace https://private.invalid synthetic-token")
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer fake.Close()
			a := &Access{Client: NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), Allowed: []string{"team"}}
			_, err := a.ObjectBoard(context.Background(), "cards", "supplied-id", "")
			var upstream *HTTPError
			if !errors.As(err, &upstream) || upstream.StatusCode != status {
				t.Fatalf("HTTP status lost: %v", err)
			}
			if !strings.HasPrefix(err.Error(), "card supplied-id ownership could not be verified: ") {
				t.Fatalf("caller object lost: %v", err)
			}
			if status == http.StatusTooManyRequests {
				if requests != 4 || !strings.Contains(err.Error(), "rate limit") {
					t.Fatalf("retry exhaustion lost: requests=%d, %v", requests, err)
				}
			} else {
				assertConfiguredAuthError(t, err, status)
			}
			assertSafeScopeMessage(t, err)
		})
	}
}

func TestNested429CancellationPreservesCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recovering := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cards/supplied-id":
			_, _ = io.WriteString(w, `{"idBoard":"hidden-board"}`)
		case "/boards/hidden-board":
			_, _ = io.WriteString(w, `{"id":"hidden-board","idOrganization":"hidden-workspace"}`)
		case "/organizations/hidden-workspace":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			close(recovering)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer fake.Close()
	a := &Access{Client: NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client()), Allowed: []string{"team"}}
	done := make(chan error, 1)
	go func() {
		_, err := a.ObjectBoard(ctx, "cards", "supplied-id", "")
		done <- err
	}()
	select {
	case <-recovering:
	case <-time.After(time.Second):
		t.Fatal("nested lookup did not reach rate limiting")
	}
	// Retries is incremented after the 429 response is consumed, before acquire
	// waits for the shared cooldown. Cancel recovery, not the initial HTTP read.
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for a.Client.Stats().Retries == 0 {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("429 recovery did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "verification canceled") {
			t.Fatalf("cancellation diagnosis lost: %v", err)
		}
		assertSafeScopeMessage(t, err)
	case <-time.After(time.Second):
		t.Fatal("nested recovery ignored cancellation")
	}
}

func assertSafeScopeMessage(t *testing.T, err error) {
	t.Helper()
	// The MCP registry serializes Client.Scrub(err.Error()), never the cause chain.
	for _, message := range []string{err.Error(), fmt.Sprint(err), fmt.Sprintf("%+v", err)} {
		for _, hidden := range []string{"hidden-board", "hidden-workspace", "https://", "http://", "/organizations", "synthetic-key", "synthetic-token"} {
			if strings.Contains(message, hidden) {
				t.Errorf("scope error exposed %q: %s", hidden, message)
			}
		}
	}
}
