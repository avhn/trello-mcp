package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/trello"
)

func TestCustomFieldResolutionError(t *testing.T) {
	missing := &trello.HTTPError{StatusCode: http.StatusNotFound}
	wrapped := customfieldResolutionError(fmt.Errorf("safe ownership failure: %w", missing))
	var status *trello.HTTPError
	if !errors.As(wrapped, &status) || status != missing || !strings.Contains(wrapped.Error(), "custom field unavailable or not accessible") {
		t.Fatalf("404 context or typed cause lost: %v", wrapped)
	}
	for _, original := range []error{
		nil,
		&trello.HTTPError{StatusCode: http.StatusUnauthorized},
		&trello.HTTPError{StatusCode: http.StatusForbidden},
		&trello.HTTPError{StatusCode: http.StatusTooManyRequests},
		trello.OwnershipDenied("custom field"),
		errors.New("HTTP 404 is not a typed status"),
		&url.Error{Op: "Get", URL: "https://example.invalid/private", Err: errors.New("synthetic network failure")},
	} {
		if got := customfieldResolutionError(original); got != original {
			t.Errorf("non-404 error changed: %v, want %v", got, original)
		}
	}
}

// TestCustomFieldErrors covers both typed causes and the compact MCP error envelope.
func TestCustomFieldErrors(t *testing.T) {
	const fieldDefinition = `{"id":"f1","idModel":"b1","modelType":"board","type":"list","options":[{"id":"o1","value":{"text":"Ready"}}]}`
	type scenario struct {
		name, stage, resolver, definition, option, want string
		status, writes, resolutions, definitions        int
		contextual, denied                              bool
	}
	cases := make([]scenario, 0, 34)
	cases = append(cases, []scenario{
		{name: "resolver 404", stage: "resolver", status: 404, resolutions: 1, contextual: true},
		{name: "definition 404", stage: "definition", status: 404, resolutions: 1, definitions: 1, contextual: true},
		{name: "nested parent 404", stage: "parent", status: 404, resolutions: 1, contextual: true},
		{name: "initial card 404", stage: "card", status: 404},
		{name: "final PUT 404", stage: "put", status: 404, resolutions: 1, definitions: 1, writes: 1},
		{name: "denied workspace", resolver: `{"idModel":"b-private","modelType":"board"}`, resolutions: 1, denied: true, want: "access denied by TRELLO_ALLOWED_WORKSPACES"},
		{name: "other board", resolver: `{"idModel":"b2","modelType":"board"}`, resolutions: 1, want: "boardId constraint"},
		{name: "malformed ownership", resolver: `{"idModel":"../bad","modelType":"board"}`, resolutions: 1, want: "ownership verification"},
		{name: "wrong model type", resolver: `{"idModel":"b1","modelType":"card"}`, resolutions: 1, want: "ownership verification"},
		{name: "foreign definition", definition: `{"id":"f1","idModel":"b2","modelType":"board","type":"list"}`, resolutions: 1, definitions: 1, want: "ownership verification"},
		{name: "wrong definition identity", definition: `{"id":"other","idModel":"b1","modelType":"board","type":"list"}`, resolutions: 1, definitions: 1, want: "ownership verification"},
		{name: "bad definition JSON", definition: `private-body test-key test-token`, resolutions: 1, definitions: 1, want: "invalid Trello JSON response"},
		{name: "unsupported definition", definition: `{"id":"f1","idModel":"b1","modelType":"board","type":"unknown"}`, resolutions: 1, definitions: 1, want: "unsupported custom-field type"},
		{name: "invalid option", option: "foreign", resolutions: 1, definitions: 1, want: "custom-field option"},
	}...)
	for _, stage := range []string{"card", "resolver", "parent", "definition", "put"} {
		for _, status := range []int{401, 403, 429, 500} {
			tc := scenario{name: fmt.Sprintf("%s %d", stage, status), stage: stage, status: status}
			if stage != "card" {
				tc.resolutions = 1
			}
			if stage == "definition" || stage == "put" {
				tc.definitions = 1
			}
			if stage == "put" {
				tc.writes = 1
			}
			cases = append(cases, tc)
		}
	}
	for _, tc := range cases {
		for _, route := range []string{"direct", "MCP"} {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				var cardReads, resolutions, definitions, writes atomic.Int32
				fail := func(w http.ResponseWriter, stage string) bool {
					if stage != tc.stage {
						return false
					}
					// Fail 429 immediately without sleeping or exhausting the GET retry budget.
					if tc.status == http.StatusTooManyRequests {
						w.Header().Set("Retry-After", "31")
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`private-body https://example.invalid/private test-key test-token`))
					return true
				}
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPut && r.URL.Path == "/cards/c1/customField/f1/item" {
						writes.Add(1)
						if !fail(w, "put") {
							_, _ = w.Write([]byte(`{}`))
						}
						return
					}
					if r.Method != http.MethodGet {
						t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					switch r.URL.Path {
					case "/cards/c1":
						cardReads.Add(1)
						if !fail(w, "card") {
							_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
						}
					case "/boards/b1", "/boards/b2":
						id := strings.TrimPrefix(r.URL.Path, "/boards/")
						_, _ = fmt.Fprintf(w, `{"id":%q,"name":"Synthetic","idOrganization":"w1"}`, id)
					case "/boards/b-private":
						if !fail(w, "parent") {
							_, _ = w.Write([]byte(`{"id":"b-private","idOrganization":"w-private"}`))
						}
					case "/organizations/w1":
						_, _ = w.Write([]byte(workspaceJSON))
					case "/organizations/w-private":
						_, _ = w.Write([]byte(`{"id":"w-private","name":"private-workspace"}`))
					case "/customFields/f1":
						if cardReads.Load() != 1 {
							t.Error("custom-field lookup preceded card authorization")
						}
						if r.URL.Query().Get("fields") == "idModel,modelType" {
							resolutions.Add(1)
							if !fail(w, "resolver") {
								parent := tc.resolver
								if tc.stage == "parent" {
									parent = `{"idModel":"b-private","modelType":"board"}`
								}
								if parent == "" {
									parent = `{"idModel":"b1","modelType":"board"}`
								}
								_, _ = w.Write([]byte(parent))
							}
						} else {
							definitions.Add(1)
							if r.URL.Query().Get("fields") != "id,name,type,options,idModel,modelType" || resolutions.Load() != 1 {
								t.Error("definition fetched without scoped ownership or bounded fields")
							}
							if !fail(w, "definition") {
								body := tc.definition
								if body == "" {
									body = fieldDefinition
								}
								_, _ = w.Write([]byte(body))
							}
						}
					default:
						t.Errorf("unexpected custom-field route %s", r.URL.Path)
						w.WriteHeader(http.StatusInternalServerError)
					}
				}))
				defer fake.Close()
				s := testService(t, fake, []string{"w1"})
				option := tc.option
				if option == "" {
					option = "o1"
				}
				raw := fmt.Sprintf(`{"cardId":"c1","customFieldId":"f1","idValue":%q}`, option)
				var message string
				if route == "direct" {
					var def definition
					for _, d := range s.definitions() {
						if d.name == "customfield_set" {
							def = d
						}
					}
					args, err := validate(json.RawMessage(raw), def)
					if err != nil {
						t.Fatal(err)
					}
					_, err = s.customfieldSet(context.Background(), args)
					if err == nil {
						t.Fatal("custom-field failure acknowledged as success")
					}
					var status *trello.HTTPError
					if errors.As(err, &status) != (tc.status != 0) || (status != nil && status.StatusCode != tc.status) {
						t.Fatalf("HTTP classification lost: %v", err)
					}
					if errors.Is(err, trello.ErrDenied) != tc.denied {
						t.Fatalf("workspace policy classification changed: %v", err)
					}
					message = err.Error()
				} else {
					text, failed := callTool(t, s, "customfield_set", raw)
					var out struct {
						Error string `json:"error"`
					}
					if err := json.Unmarshal([]byte(text), &out); err != nil || !failed || out.Error == "" {
						t.Fatalf("not an MCP tool error: %s, isError=%v, err=%v", text, failed, err)
					}
					message = out.Error
				}
				if tc.status != 0 {
					cause := &trello.HTTPError{StatusCode: tc.status}
					want := cause.Error()
					if tc.stage == "parent" {
						want = s.access.ScopeError(cause, "customField f1").Error()
					} else if tc.status == http.StatusTooManyRequests {
						want = "trello API: HTTP 429; rate limit exceeded; retry delay exceeds 30 seconds; retry later"
					}
					if tc.contextual {
						want = "custom field unavailable or not accessible; refresh customfield_find on the card's authorized board and verify access: " + want
					}
					if message != want {
						t.Fatalf("safe status diagnostic changed: %q, want %q", message, want)
					}
				}
				if strings.Contains(message, "custom field unavailable or not accessible") != tc.contextual {
					t.Fatalf("404 context applied at the wrong stage: %s", message)
				}
				if tc.contextual && !strings.Contains(message, "refresh customfield_find on the card's authorized board and verify access:") {
					t.Fatalf("missing scoped recovery guidance: %s", message)
				}
				if tc.want != "" && !strings.Contains(message, tc.want) {
					t.Fatalf("classification changed: %s, want %s", message, tc.want)
				}
				for _, forbidden := range []string{"private-body", "example.invalid/private", "test-key", "test-token", "b-private", "w-private", "private-workspace", fake.URL} {
					if strings.Contains(message, forbidden) {
						t.Errorf("private response, URL, credential or resolved denied parent leaked: %s", message)
					}
				}
				if cardReads.Load() != 1 || resolutions.Load() != int32(tc.resolutions) || definitions.Load() != int32(tc.definitions) || writes.Load() != int32(tc.writes) {
					t.Fatalf("requests card=%d resolver=%d definition=%d PUT=%d, want 1/%d/%d/%d", cardReads.Load(), resolutions.Load(), definitions.Load(), writes.Load(), tc.resolutions, tc.definitions, tc.writes)
				}
			})
		}
	}
}
