package trello

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadOAuthAndRetry(t *testing.T) {
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.URL.Path != "/cards/c1/attachments/at1/download/report final.txt" {
			t.Errorf("download URL %s", r.URL)
		}
		if r.Header.Get("Authorization") != `OAuth oauth_consumer_key="key%20%22%26", oauth_token="token%2F%3F"` {
			t.Errorf("OAuth escaping %s", r.Header.Get("Authorization"))
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("synthetic"))
	}))
	defer fake.Close()
	client := NewClient(fake.URL, `key "&`, "token/?", fake.Client())
	var out bytes.Buffer
	n, mimeType, err := client.Download(context.Background(), "c1", "at1", "report final.txt", &out)
	if err != nil || n != 9 || out.String() != "synthetic" || mimeType != "text/plain" {
		t.Fatalf("download result n=%d, mime=%s, error=%v", n, mimeType, err)
	}
	if stats := client.Stats(); stats.Requests != 2 || stats.Retries != 1 {
		t.Errorf("stats %+v", stats)
	}
}

func TestDownloadRejectsSameOriginRedirect(t *testing.T) {
	var forwarded atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			forwarded.Add(1)
			_, _ = w.Write([]byte("must not follow"))
			return
		}
		http.Redirect(w, r, "/next", http.StatusTemporaryRedirect)
	}))
	defer fake.Close()
	client := NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client())
	if _, _, err := client.Download(context.Background(), "c1", "at1", "file.txt", io.Discard); err == nil || !strings.Contains(err.Error(), "307") {
		t.Errorf("redirect accepted: %v", err)
	}
	if forwarded.Load() != 0 {
		t.Error("same-origin redirect followed")
	}
}

func TestMultipart429Replay(t *testing.T) {
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/cards/c1/attachments" {
			t.Errorf("upload route %s %s", r.Method, r.URL.Path)
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Error(err)
		}
		if string(data) != "synthetic" || part.FileName() != "file.txt" {
			t.Errorf("replayed upload %s %s", part.FileName(), data)
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`{"id":"at1"}`))
	}))
	defer fake.Close()
	client := NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client())
	var out struct {
		ID string `json:"id"`
	}
	if err := client.Upload(context.Background(), "c1", "", "file.txt", "text/plain", []byte("synthetic"), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "at1" || requests.Load() != 2 {
		t.Errorf("upload result %+v, requests=%d", out, requests.Load())
	}
}
