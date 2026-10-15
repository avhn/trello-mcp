package trello

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestHTTP covers compact decoding, status handling and the exact bounded retry budget.
func TestHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		status, failures, wantRequests int
		wantErr                        string
	}{
		{"success", 200, 0, 1, ""}, {"unauthorized", 401, 99, 1, "trello HTTP 401"}, {"429 then success", 429, 2, 3, ""}, {"retry exhausted", 429, 99, 4, "trello HTTP 429"}, {"invalid json", 200, 99, 1, "invalid Trello JSON response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current := requests.Add(1)
				if r.URL.Query().Get("key") != "key" || r.URL.Query().Get("token") != "token" || r.URL.Query().Get("fields") != "id" {
					t.Error("missing request query")
				}
				w.Header().Set("Retry-After", "0")
				if int(current) <= tc.failures {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte("key token invalid"))
					return
				}
				_, _ = w.Write([]byte(`{"id":"ok"}`))
			}))
			defer fake.Close()
			c := NewClient(fake.URL, "key", "token", fake.Client())
			var got struct {
				ID string `json:"id"`
			}
			q := url.Values{"fields": {"id"}}
			err := c.Get(context.Background(), "/boards/b1", q, &got)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("got %v, want %s", err, tc.wantErr)
				}
			} else if err != nil || got.ID != "ok" {
				t.Fatalf("got %+v, %v", got, err)
			}
			if got := int(requests.Load()); got != tc.wantRequests {
				t.Errorf("requests %d, want %d", got, tc.wantRequests)
			}
			if q.Has("key") || q.Has("token") {
				t.Error("caller query mutated")
			}
		})
	}
}

// TestRateWindows checks independent ceilings, rolling expiration, cancellation and response headers.
func TestRateWindows(t *testing.T) {
	c := New("key", "token")
	for i, w := range c.windows {
		wantMax := []int{300, 100, 100}[i]
		wantInterval := []time.Duration{10 * time.Second, 10 * time.Second, 900 * time.Second}[i]
		if w.max != wantMax || w.interval != wantInterval {
			t.Fatalf("window %d: %+v", i, w)
		}
	}
	if err := c.acquire(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(c.windows[2].times) != 0 {
		t.Error("non-member consumed member quota")
	}
	if err := c.acquire(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(c.windows[2].times) != 1 {
		t.Error("member quota not consumed")
	}
	for index := 0; index < 3; index++ {
		isolated := New("key", "token")
		isolated.windows[index].max = 1
		isolated.windows[index].times = []time.Time{time.Now()}
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		err := isolated.acquire(ctx, true)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("window %d did not block: %v", index, err)
		}
		isolated.windows[index].times = []time.Time{time.Now().Add(-1000 * time.Second)}
		if err = isolated.acquire(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
	c.observe(http.Header{"X-Rate-Limit-Api-Token-Max": {"80"}, "X-Rate-Limit-Api-Key-Remaining": {"0"}, "X-Rate-Limit-Api-Key-Interval-Ms": {"10000"}}, false)
	if c.windows[1].max != 80 || !c.windows[0].blocked.After(time.Now()) {
		t.Error("rate limit headers not applied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(c.acquire(ctx, true), context.Canceled) {
		t.Error("cancelled acquisition accepted")
	}
}

// roundTripFunc lets tests model a transport failure without contacting external services.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper for synthetic failures.
func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestScrubbing covers literal and URL-escaped credentials in transport errors.
func TestScrubbing(t *testing.T) {
	key, token := "key+/?", "token+/?"
	c := NewClient("https://example.invalid/1", key, token, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("failed %s %s %s", r.URL, key, token)
	})})
	var out any
	err := c.Get(context.Background(), "/boards/b1", nil, &out)
	if err == nil {
		t.Fatal("missing error")
	}
	for _, secret := range []string{key, token, url.QueryEscape(key), url.QueryEscape(token)} {
		if strings.Contains(err.Error(), secret) {
			t.Error("credential present in diagnostic")
		}
	}
}

// TestRedirectAndSizeBounds ensures credentials never follow redirects and oversized bodies fail.
func TestRedirectAndSizeBounds(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("redirect forwarded credentials")
		w.WriteHeader(500)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, destination.URL, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat(" ", maxResponseBytes+1)))
	}))
	defer source.Close()
	c := NewClient(source.URL, "key", "token", source.Client())
	for path, want := range map[string]string{"/redirect": "trello HTTP 302", "/big": "trello response exceeds 8 MiB limit"} {
		var out any
		err := c.Get(context.Background(), path, nil, &out)
		if err == nil || err.Error() != want {
			t.Errorf("%s got %v, want %s", path, err, want)
		}
	}
}

// TestRetryAfter verifies both supported formats and bounded refusal of long waits.
func TestRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		value string
		fail  bool
	}{{"0", false}, {"0.001", false}, {"60", true}, {"1e30", true}, {"invalid", false}, {time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), true}, {time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat), false}} {
		h := http.Header{}
		h.Set("Retry-After", tc.value)
		d, err := retryDelay(h, 0)
		if (err != nil) != tc.fail || d < 0 || (d > 30*time.Second) != tc.fail {
			t.Errorf("Retry-After %q: %s, %v", tc.value, d, err)
		}
	}
}
