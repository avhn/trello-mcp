package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/avhn/trello-mcp/internal/trello"
)

func TestInstructions(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("instructions fetched upstream data")
		w.WriteHeader(500)
	}))
	defer fake.Close()
	for _, tc := range []struct {
		name, workspaces, upload string
		readOnly                 bool
		want                     []string
	}{
		{"unrestricted", "", "", false, []string{"Workspaces: unrestricted", "read-only: false", "path uploads: false", "downloads: true"}},
		{"restricted", "team,w2", "/staging", false, []string{"Workspaces: restricted", "path uploads: true"}},
		{"read-only", "team", "/staging", true, []string{"read-only: true", "path uploads: false", "downloads: false"}},
		{"long", strings.Repeat("workspace", 100), "/" + strings.Repeat("staging", 100), false, []string{"Workspaces: restricted"}},
		{"scrubbed", "test-token", "/test-key", false, []string{"Workspaces: restricted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var allowed []string
			if tc.workspaces != "" {
				allowed = strings.Split(tc.workspaces, ",")
			}
			s := testService(t, fake, allowed)
			s.config.ReadOnly, s.config.UploadDir = tc.readOnly, tc.upload
			text := s.Instructions()
			for _, want := range append(tc.want, `server_health detail "policy"`) {
				if !fileTransfersSupported {
					if want == "downloads: true" {
						want = "downloads: false"
					}
					if want == "path uploads: true" {
						want = "path uploads: false"
					}
				}
				if !strings.Contains(text, want) {
					t.Errorf("instructions %q missing %q", text, want)
				}
			}
			if utf8.RuneCountInString(text) > 150 || strings.ContainsAny(text, "\r\n") {
				t.Errorf("instructions not a short paragraph (%d chars): %s", utf8.RuneCountInString(text), text)
			}
			for _, hidden := range []string{"test-key", "test-token", "b1", "w1"} {
				if strings.Contains(text, hidden) {
					t.Errorf("instructions leaked %q: %s", hidden, text)
				}
			}
		})
	}
}

func TestPolicyHealth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		allowed    []string
		profile    []string
		readOnly   bool
		fail       bool
		enabled    int
		hidden     int
		workspaces string
	}{
		{"unrestricted", nil, []string{"all"}, false, false, 48, 0, `[]`},
		{"resolved", []string{"allowed", "w2", "missing"}, []string{"all"}, false, false, 48, 0, `[{"selector":"allowed","id":"w1","name":"allowed"},{"selector":"w2","id":"w2","name":"second"},{"selector":"missing"}]`},
		{"read-only", []string{"w1"}, []string{"all"}, true, false, 19, 29, `[{"selector":"w1","id":"w1","name":"allowed"}]`},
		{"profile", nil, []string{"card", "server_health"}, true, false, 4, 9, `[]`},
		{"read-oriented discovery", nil, strings.Split(discoveryProfile, ","), false, false, 10, 0, `[]`},
		{"resolution-failed", []string{"w1"}, []string{"all"}, false, true, 48, 0, `[{"selector":"w1"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/members/me/organizations" || r.URL.Query().Get("fields") != "id,name" {
					t.Errorf("unexpected policy lookup: %s", r.URL)
				}
				if tc.fail {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = w.Write([]byte(`[{"id":"w1","name":"allowed"},{"id":"w2","name":"second"},{"id":"w3","name":"never-expose"}]`))
			}))
			defer fake.Close()
			s := testService(t, fake, tc.allowed)
			s.config.Tools, s.config.ReadOnly = tc.profile, tc.readOnly
			s.config.UploadDir, s.config.DownloadDir = "/staging", "/downloads"
			var wg sync.WaitGroup
			for range 5 {
				wg.Go(func() {
					text, failed := callTool(t, s, "server_health", `{"detail":"policy"}`)
					if failed {
						t.Errorf("policy failed: %s", text)
						return
					}
					var got struct {
						Restricted        bool            `json:"restricted"`
						AllowedWorkspaces json.RawMessage `json:"allowedWorkspaces"`
						ReadOnly          bool            `json:"readOnly"`
						ToolProfile       []string        `json:"toolProfile"`
						EnabledToolCount  int             `json:"enabledToolCount"`
						HiddenWriteTools  int             `json:"hiddenWriteTools"`
						Uploads           transferPolicy  `json:"uploads"`
						Downloads         transferPolicy  `json:"downloads"`
					}
					if err := json.Unmarshal([]byte(text), &got); err != nil {
						t.Error(err)
						return
					}
					if got.Restricted != (len(tc.allowed) > 0) || got.ReadOnly != tc.readOnly || got.EnabledToolCount != tc.enabled || got.HiddenWriteTools != tc.hidden || string(got.AllowedWorkspaces) != tc.workspaces || strings.Join(got.ToolProfile, ",") != strings.Join(tc.profile, ",") {
						t.Errorf("wrong policy: %s", text)
					}
					transfer := fileTransfersSupported && !tc.readOnly && strings.Join(tc.profile, ",") == "all"
					if got.Uploads != (transferPolicy{transfer, "/staging"}) || got.Downloads != (transferPolicy{transfer, "/downloads"}) {
						t.Errorf("wrong transfers: %s", text)
					}
					for _, hidden := range []string{"never-expose", "test-key", "test-token", "b1"} {
						if strings.Contains(text, hidden) {
							t.Errorf("policy leaked %q: %s", hidden, text)
						}
					}
				})
			}
			wg.Wait()
			want := int32(1)
			if len(tc.allowed) == 0 {
				want = 0
			}
			if requests.Load() != want {
				t.Errorf("policy made %d requests, want %d", requests.Load(), want)
			}
		})
	}
}

func TestPolicyDenialMessages(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args, path, response, policy, reason string
	}{
		{"workspace", "workspace_use", `{"workspaceId":"w2"}`, "/organizations/w2", `{"id":"w2","name":"private-workspace"}`, "TRELLO_ALLOWED_WORKSPACES", "workspace w2 is outside the allowed workspaces (allowed: allowed)"},
		{"board", "board_get", `{}`, "/boards/b1", `{"id":"b1","idOrganization":"w2","name":"private-board"}`, "TRELLO_ALLOWED_WORKSPACES", "board b1 is outside the allowed workspaces (allowed: allowed)"},
		{"workspace-ownership", "workspace_use", `{"workspaceId":"w2"}`, "/organizations/w2", `{}`, "ownership verification", "workspace w2 ownership could not be verified, so policy refused"},
		{"board-ownership", "board_get", `{}`, "/boards/b1", `{}`, "ownership verification", "board b1 ownership could not be verified, so policy refused"},
		{"board-workspace", "board_get", `{}`, "/boards/b1", `{"id":"b1"}`, "ownership verification", "workspace for board b1 ownership could not be verified"},
		{"object-ownership", "card_get", `{"cardId":"c1"}`, "/cards/c1", `{}`, "ownership verification", "board for object c1 ownership could not be verified"},
		{"board-constraint", "card_get", `{"cardId":"c1","boardId":"b2"}`, "/cards/c1", `{"idBoard":"b1"}`, "boardId constraint", "object c1 belongs to board b1, not the supplied boardId b2"},
		{"uploads-disabled", "attachment_upload", `{"cardId":"c1","path":"file.txt"}`, "", "", "TRELLO_UPLOAD_DIR", "path uploads are disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tc.path:
					_, _ = w.Write([]byte(tc.response))
				case "/cards/c1":
					_, _ = w.Write([]byte(`{"idBoard":"b1"}`))
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/organizations/w1":
					_, _ = w.Write([]byte(workspaceJSON))
				case "/organizations/w2":
					_, _ = w.Write([]byte(`{"id":"w2","name":"private-workspace"}`))
				default:
					t.Errorf("unexpected route: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			text, failed := callTool(t, testService(t, fake, []string{"allowed"}), tc.tool, tc.args)
			var got struct{ Error string }
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatal(err)
			}
			if !failed || !strings.HasPrefix(got.Error, "access denied by "+tc.policy+": ") || !strings.Contains(got.Error, tc.reason) || !strings.Contains(got.Error, "not a token/permission error") || !strings.Contains(got.Error, `server_health with detail "policy"`) {
				t.Fatalf("wrong denial: %s", text)
			}
			if strings.Contains(text, "private-board") || strings.Contains(text, "private-workspace") {
				t.Fatalf("denial leaked names: %s", text)
			}
		})
	}
}

func TestReadOnlyHandlerRefusal(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("read-only refusal contacted Trello")
		w.WriteHeader(500)
	}))
	defer fake.Close()
	s := testService(t, fake, nil)
	s.config.ReadOnly = true
	for _, d := range s.definitions() {
		if !d.write {
			continue
		}
		text, failed := callTool(t, s, d.name, writeArguments[d.name])
		if !failed || !strings.Contains(text, "access denied by TRELLO_READ_ONLY") || !strings.Contains(text, "not a token/permission error") {
			t.Errorf("missing read-only refusal for %s: %s", d.name, text)
		}
	}
}

func TestToolUpstreamTokenError(t *testing.T) {
	for _, status := range []int{401, 403} {
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("test-key test-token private-board"))
		}))
		s := testService(t, fake, []string{"allowed"})
		text, failed := callTool(t, s, "board_get", `{}`)
		fake.Close()
		if !failed || !strings.Contains(text, "trello API: HTTP") || !strings.Contains(text, "token/permission issue, not server policy") || strings.Contains(text, "test-token") || strings.Contains(text, "private-board") {
			t.Errorf("wrong upstream error: %s", text)
		}
	}
}

func TestPolicyScrubbing(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"w1","name":"test-token"}]`))
	}))
	defer fake.Close()
	s := testService(t, fake, []string{"test-token"})
	s.config.UploadDir, s.config.DownloadDir = "/test-key", "/test-token"
	text, failed := callTool(t, s, "server_health", `{"detail":"policy"}`)
	if failed || strings.Contains(text, "test-key") || strings.Contains(text, "test-token") || !strings.Contains(text, "[redacted]") {
		t.Fatalf("unsafe policy: %s", text)
	}
	err := trello.PolicyDenied("test", "test-key test-token")
	if got := s.access.Client.Scrub(err.Error()); strings.Contains(got, "test-key") || strings.Contains(got, "test-token") {
		t.Errorf("unsafe denial: %s", got)
	}
}

func TestResolvedParentFailuresHideScopes(t *testing.T) {
	for _, mode := range []string{"board-collection", "card-collection", "checklist-card"} {
		t.Run(mode, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/members/me/boards":
					_, _ = w.Write([]byte(`[{"id":"hidden-board","idOrganization":"hidden-workspace"}]`))
				case "/members/me/cards":
					_, _ = w.Write([]byte(`[{"id":"hidden-card","idBoard":"hidden-board"}]`))
				case "/checklists/cl1":
					_, _ = w.Write([]byte(`{"id":"cl1","idBoard":"b1","idCard":"hidden-card"}`))
				case "/cards/hidden-card":
					_, _ = w.Write([]byte(`{"idBoard":"hidden-board"}`))
				case "/boards/b1":
					_, _ = w.Write([]byte(boardJSON))
				case "/organizations/w1":
					_, _ = w.Write([]byte(workspaceJSON))
				case "/boards/hidden-board":
					if mode == "card-collection" {
						_, _ = w.Write([]byte(`{}`))
					} else {
						_, _ = w.Write([]byte(`{"id":"hidden-board","idOrganization":"hidden-workspace"}`))
					}
				case "/organizations/hidden-workspace":
					if mode == "board-collection" {
						_, _ = w.Write([]byte(`{}`))
					} else {
						_, _ = w.Write([]byte(`{"id":"hidden-workspace","name":"private-name"}`))
					}
				default:
					t.Errorf("unexpected route: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			s := testService(t, fake, []string{"allowed"})
			var err error
			switch mode {
			case "board-collection":
				_, err = s.boardFind(context.Background(), arguments{})
			case "card-collection":
				_, err = s.cardFind(context.Background(), arguments{mine: true})
			case "checklist-card":
				_, err = s.loadChecklist(context.Background(), arguments{strings: map[string]string{"checklistId": "cl1"}})
			}
			if err == nil {
				t.Fatal("unverifiable or denied parent accepted")
			}
			for _, hidden := range []string{"hidden-board", "hidden-workspace", "hidden-card", "private-name"} {
				if strings.Contains(err.Error(), hidden) {
					t.Errorf("resolved parent leaked %q: %v", hidden, err)
				}
			}
		})
	}
}
