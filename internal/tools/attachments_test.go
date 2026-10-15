package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avhn/trello-mcp/internal/trello"
)

func attachmentFake(t *testing.T, transfer http.HandlerFunc, fileName string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/download/") || r.Method == "POST" {
			transfer(w, r)
			return
		}
		if r.URL.Query().Get("key") != "test-key" || r.URL.Query().Get("token") != "test-token" {
			t.Error("missing metadata auth")
		}
		switch r.URL.Path {
		case "/cards/c1":
			if r.URL.Query().Get("fields") != "idBoard" {
				t.Error("unbounded card query")
			}
			_, _ = w.Write([]byte(cardJSON))
		case "/boards/b1":
			_, _ = w.Write([]byte(boardJSON))
		case "/organizations/w1":
			_, _ = w.Write([]byte(workspaceJSON))
		case "/cards/c1/attachments/at1":
			if r.URL.Query().Get("fields") != "id,fileName,isUpload,bytes" {
				t.Error("unbounded attachment query")
			}
			if fileName != "" {
				_, _ = fmt.Fprintf(w, `{"id":"at1","fileName":%q,"isUpload":true,"bytes":9}`, fileName)
				return
			}
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trello", "attachment.json"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write(data)
		default:
			t.Errorf("unexpected attachment route %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
}

func TestAttachmentUploadSources(t *testing.T) {
	for _, source := range []string{"base64", "absolute", "relative", "clean-absolute", "clean-relative", "dotfile"} {
		t.Run(source, func(t *testing.T) {
			var uploads atomic.Int32
			fake := attachmentFake(t, func(w http.ResponseWriter, r *http.Request) {
				uploads.Add(1)
				if r.Method != "POST" || r.URL.Path != "/cards/c1/attachments" {
					t.Error("bad upload route")
				}
				if r.URL.Query().Get("key") != "test-key" || r.URL.Query().Get("token") != "test-token" {
					t.Error("missing upload auth")
				}
				reader, err := r.MultipartReader()
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				namePart, err := reader.NextPart()
				if err != nil {
					t.Error(err)
					return
				}
				data, err := io.ReadAll(namePart)
				if err != nil {
					t.Error(err)
				}
				if namePart.FormName() != "name" || string(data) != "Display" {
					t.Errorf("name part %s %s", namePart.FormName(), data)
				}
				part, err := reader.NextPart()
				if err != nil {
					t.Error(err)
					return
				}
				data, err = io.ReadAll(part)
				if err != nil {
					t.Error(err)
				}
				if part.FormName() != "file" || part.FileName() != "upload.txt" || string(data) != "synthetic" || part.Header.Get("Content-Type") != "text/plain" {
					t.Errorf("unexpected file part: %v %s", part.Header, data)
				}
				_, _ = w.Write([]byte(`{"id":"at1","name":"Display","url":"https://trello.com/file","bytes":9,"previews":["must not escape"]}`))
			}, "")
			defer fake.Close()
			s := testService(t, fake, []string{"w1"})
			arg := `"base64":"c3ludGhldGlj"`
			if source != "base64" {
				s.config.UploadDir = t.TempDir()
				rel := filepath.Join("nested", "local.txt")
				if source == "dotfile" {
					rel = filepath.Join(".private", ".local.txt")
				}
				path := filepath.Join(s.config.UploadDir, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
					t.Fatal(err)
				}
				switch source {
				case "relative", "dotfile":
					path = rel
				case "clean-absolute":
					path = s.config.UploadDir + "/nested/../" + rel
				case "clean-relative":
					path = "nested/../" + rel
				}
				encoded, err := json.Marshal(path)
				if err != nil {
					t.Fatal(err)
				}
				arg = `"path":` + string(encoded)
			}
			text, failed := callTool(t, s, "attachment_upload", `{"cardId":"c1","fileName":"../upload.txt","mimeType":"text/plain","name":"Display",`+arg+`}`)
			if failed || text != `{"id":"at1","name":"Display","url":"https://trello.com/file"}` || uploads.Load() != 1 {
				t.Fatalf("upload: %s", text)
			}
		})
	}
}

func TestUploadConfinesPaths(t *testing.T) {
	for _, kind := range []string{"unset", "outside", "parent-escape", "nested-escape", "absolute-escape", "prefix-sibling", "file-symlink", "directory-symlink", "root-symlink", "root-symlink-trailing-slash"} {
		t.Run(kind, func(t *testing.T) {
			var uploads atomic.Int32
			fake := attachmentFake(t, func(w http.ResponseWriter, _ *http.Request) {
				uploads.Add(1)
				w.WriteHeader(500)
			}, "")
			defer fake.Close()
			s := testService(t, fake, nil)
			base := t.TempDir()
			root := filepath.Join(base, "uploads")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside.txt")
			if err := os.WriteFile(outside, []byte("synthetic outside data"), 0600); err != nil {
				t.Fatal(err)
			}
			s.config.UploadDir = root
			path := outside
			switch kind {
			case "unset":
				s.config.UploadDir = ""
			case "parent-escape":
				path = "../outside.txt"
			case "nested-escape":
				path = "nested/../../outside.txt"
			case "absolute-escape":
				path = root + "/../outside.txt"
			case "prefix-sibling":
				path = root + "-other/outside.txt"
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("synthetic outside data"), 0600); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				path = filepath.Join(root, "link.txt")
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "directory-symlink":
				link := filepath.Join(root, "link")
				if err := os.Symlink(base, link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, "outside.txt")
			case "root-symlink", "root-symlink-trailing-slash":
				link := filepath.Join(base, "root-link")
				if err := os.Symlink(base, link); err != nil {
					t.Fatal(err)
				}
				s.config.UploadDir = link
				if kind == "root-symlink-trailing-slash" {
					s.config.UploadDir += string(os.PathSeparator)
				}
				path = "outside.txt"
			}
			encoded, err := json.Marshal(path)
			if err != nil {
				t.Fatal(err)
			}
			text, failed := callTool(t, s, "attachment_upload", `{"cardId":"c1","path":`+string(encoded)+`}`)
			if !failed || uploads.Load() != 0 {
				t.Fatalf("unconfined upload proceeded: %s", text)
			}
			if (kind == "unset" || kind == "outside" || kind == "parent-escape" || kind == "nested-escape" || kind == "absolute-escape" || kind == "prefix-sibling") && !strings.Contains(text, "TRELLO_UPLOAD_DIR") {
				t.Fatalf("missing upload configuration guidance: %s", text)
			}
		})
	}
}

func TestAttachmentDownload(t *testing.T) {
	fake := attachmentFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/c1/attachments/at1/download/report final.txt" {
			t.Errorf("download used display name or URL: %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" || r.Header.Get("Authorization") != `OAuth oauth_consumer_key="test-key", oauth_token="test-token"` {
			t.Error("download must use OAuth header only")
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("synthetic"))
	}, "")
	defer fake.Close()
	s := testService(t, fake, []string{"w1"})
	s.config.DownloadDir = filepath.Join(t.TempDir(), "private")
	text, failed := callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
	if failed {
		t.Fatal(text)
	}
	path := filepath.Join(s.config.DownloadDir, "at1-report_final.txt")
	var got struct {
		Path     string
		Size     int
		MimeType string
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != path || got.Size != 9 || got.MimeType != "text/plain" || strings.Contains(text, "synthetic") {
		t.Errorf("download response: %s", text)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "synthetic" {
		t.Errorf("file body %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Error("download not private")
	}
	original := info
	info, err = os.Stat(s.config.DownloadDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Error("directory not private")
	}
	for attempt := 1; attempt <= 2; attempt++ {
		text, failed = callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
		if failed {
			t.Fatal(text)
		}
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(s.config.DownloadDir, fmt.Sprintf("at1-report_final-%d.txt", attempt))
		if got.Path != want || got.Size != 9 {
			t.Fatalf("repeat download response: %s", text)
		}
		data, err := os.ReadFile(want)
		if err != nil || string(data) != "synthetic" {
			t.Fatalf("repeat download body %q: %v", data, err)
		}
	}
	unchanged, err := os.Stat(path)
	if err != nil || !os.SameFile(original, unchanged) || !original.ModTime().Equal(unchanged.ModTime()) {
		t.Fatalf("inspect original: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "synthetic" {
		t.Fatalf("original file changed: %q, %v", data, err)
	}
}

func TestDownloadRejectsUnsafeStorage(t *testing.T) {
	for _, kind := range []string{"directory-symlink", "directory-symlink-trailing-slash", "not-directory", "public-directory", "file-symlink"} {
		t.Run(kind, func(t *testing.T) {
			var transfers atomic.Int32
			fake := attachmentFake(t, func(w http.ResponseWriter, _ *http.Request) { transfers.Add(1); _, _ = w.Write([]byte("unsafe")) }, "")
			defer fake.Close()
			s := testService(t, fake, nil)
			base := t.TempDir()
			dir := filepath.Join(base, "download")
			var err error
			switch kind {
			case "directory-symlink", "directory-symlink-trailing-slash":
				err = os.Symlink(base, dir)
				if kind == "directory-symlink-trailing-slash" {
					dir += string(os.PathSeparator)
				}
			case "not-directory":
				err = os.WriteFile(dir, []byte("keep"), 0600)
			case "public-directory":
				err = os.Mkdir(dir, 0755)
			case "file-symlink":
				err = os.Mkdir(dir, 0700)
				if err == nil {
					err = os.Symlink(filepath.Join(base, "outside"), filepath.Join(dir, "at1-report_final.txt"))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			s.config.DownloadDir = dir
			text, failed := callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
			if kind == "file-symlink" {
				if failed || transfers.Load() != 1 {
					t.Fatalf("did not skip existing symlink: %s", text)
				}
				if _, err := os.Stat(filepath.Join(base, "outside")); !os.IsNotExist(err) {
					t.Fatalf("symlink target touched: %v", err)
				}
				if info, err := os.Lstat(filepath.Join(dir, "at1-report_final.txt")); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink changed: %v", err)
				}
			} else if !failed || transfers.Load() != 0 {
				t.Fatalf("unsafe download proceeded: %s", text)
			}
		})
	}
}

func TestDownloadFailureCleansPartialFile(t *testing.T) {
	for _, kind := range []string{"redirect", "size", "chunked-size", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			var leaked atomic.Int32
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { leaked.Add(1); _, _ = w.Write([]byte("unsafe")) }))
			defer other.Close()
			fake := attachmentFake(t, func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "redirect":
					http.Redirect(w, r, other.URL, http.StatusFound)
				case "size":
					w.Header().Set("Content-Length", fmt.Sprint(trello.MaxAttachmentBytes+1))
				case "chunked-size":
					w.(http.Flusher).Flush()
					_, _ = io.Copy(w, io.LimitReader(strings.NewReader(strings.Repeat("x", trello.MaxAttachmentBytes+1)), trello.MaxAttachmentBytes+1))
				case "truncated":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte("short"))
				}
			}, "")
			defer fake.Close()
			s := testService(t, fake, nil)
			s.config.DownloadDir = filepath.Join(t.TempDir(), "download")
			if err := os.Mkdir(s.config.DownloadDir, 0700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(s.config.DownloadDir, "at1-report_final.txt")
			if err := os.WriteFile(original, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			text, failed := callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
			if !failed || leaked.Load() != 0 {
				t.Fatalf("unsafe transfer: %s", text)
			}
			entries, err := os.ReadDir(s.config.DownloadDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(original) {
				t.Error("partial file not removed or original removed")
			}
			data, err := os.ReadFile(original)
			if err != nil || string(data) != "keep" {
				t.Fatalf("original changed: %q, %v", data, err)
			}
		})
	}
}

func TestUploadRejectsUnsafeInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), path); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{
		``, `,"path":"relative"`, `,"path":` + string(encoded), `,"base64":"not base64","fileName":"file.txt"`,
		`,"base64":"c3ludGhldGlj"`, `,"path":"/missing","base64":""`,
		`,"base64":"c3ludGhldGlj","fileName":"file.txt","mimeType":"text/plain\r\nInjected: yes"`,
	} {
		fake := attachmentFake(t, func(w http.ResponseWriter, _ *http.Request) { t.Error("unsafe upload sent"); w.WriteHeader(500) }, "")
		s := testService(t, fake, nil)
		text, failed := callTool(t, s, "attachment_upload", `{"cardId":"c1"`+arg+`}`)
		fake.Close()
		if !failed {
			t.Fatalf("unsafe upload accepted: %s", text)
		}
	}
}

func TestSafeFileName(t *testing.T) {
	for input, want := range map[string]string{"../../etc/passwd": "passwd", `..\..\report.txt`: "report.txt", "..": "attachment", "a\nb.txt": "a_b.txt", "é.txt": "_.txt"} {
		if got := safeFileName(input); got != want {
			t.Errorf("%q: got %q; want %q", input, got, want)
		}
	}
}

func TestDownloadCollisionExhaustion(t *testing.T) {
	fake := attachmentFake(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("transfer attempted after collision exhaustion")
		w.WriteHeader(500)
	}, "")
	defer fake.Close()
	s := testService(t, fake, nil)
	s.config.DownloadDir = filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(s.config.DownloadDir, 0700); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt <= 100; attempt++ {
		name := "at1-report_final.txt"
		if attempt > 0 {
			name = fmt.Sprintf("at1-report_final-%d.txt", attempt)
		}
		if err := os.WriteFile(filepath.Join(s.config.DownloadDir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	text, failed := callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
	if !failed || !strings.Contains(text, "filename collisions exhausted suffixes 1-100") {
		t.Fatalf("expected collision exhaustion: %s", text)
	}
	entries, err := os.ReadDir(s.config.DownloadDir)
	if err != nil || len(entries) != 101 {
		t.Fatalf("existing files changed: count %d, %v", len(entries), err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(s.config.DownloadDir, entry.Name()))
		if err != nil || string(data) != "keep" {
			t.Fatalf("existing file changed: %s, %q, %v", entry.Name(), data, err)
		}
	}
}

func TestDownloadFileName(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{"../../report final.jpg", "report_final-100.jpg"},
		{"report", "report-100"},
		{strings.Repeat("a", 116) + ".jpg", strings.Repeat("a", 112) + "-100.jpg"},
		{"a." + strings.Repeat("x", 118), "a-100." + strings.Repeat("x", 114)},
	} {
		got := downloadFileName("at1", safeFileName(test.input), 100)
		if got != "at1-"+test.want || len(got) > len("at1-")+120 {
			t.Errorf("%q: got %q; want %q", test.input, got, "at1-"+test.want)
		}
	}
}
