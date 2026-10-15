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
	for _, source := range []string{"base64", "path"} {
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
			arg := `"base64":"c3ludGhldGlj"`
			if source == "path" {
				path := filepath.Join(t.TempDir(), "local.txt")
				if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
					t.Fatal(err)
				}
				// System temporary directories can have symlink ancestors, such as /var on macOS.
				path, err := filepath.EvalSymlinks(path)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(path)
				if err != nil {
					t.Fatal(err)
				}
				arg = `"path":` + string(encoded)
			}
			text, failed := callTool(t, testService(t, fake, []string{"w1"}), "attachment_upload", `{"cardId":"c1","fileName":"../upload.txt","mimeType":"text/plain","name":"Display",`+arg+`}`)
			if failed || text != `{"id":"at1","name":"Display","url":"https://trello.com/file"}` || uploads.Load() != 1 {
				t.Fatalf("upload: %s", text)
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
	info, err = os.Stat(s.config.DownloadDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Error("directory not private")
	}
	text, failed = callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
	if !failed || !strings.Contains(text, "exclusively") {
		t.Fatalf("overwrote existing file: %s", text)
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
			if !failed || transfers.Load() != 0 {
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
			text, failed := callTool(t, s, "attachment_download", `{"cardId":"c1","attachmentId":"at1"}`)
			if !failed || leaked.Load() != 0 {
				t.Fatalf("unsafe transfer: %s", text)
			}
			entries, err := os.ReadDir(s.config.DownloadDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Error("partial file not removed")
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
