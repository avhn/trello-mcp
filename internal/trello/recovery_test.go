package trello

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestOverlappingCredentials ensures redaction never exposes part of another credential.
func TestOverlappingCredentials(t *testing.T) {
	for _, tc := range []struct{ key, token, message, want string }{
		{"prefix", "prefix-suffix", "prefix-suffix prefix", "[redacted] [redacted]"},
		{"prefix-suffix", "prefix", "prefix-suffix prefix", "[redacted] [redacted]"},
		{"redact", "redacted-value", "redacted-value redact", "[redacted] [redacted]"},
		{"a b", "a b/c", url.QueryEscape("a b/c") + " a b", "[redacted] [redacted]"},
	} {
		c := New(tc.key, tc.token)
		if got := c.Scrub(tc.message); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

// TestConcurrentQuota checks reservations are atomic, including the member-route budget.
func TestConcurrentQuota(t *testing.T) {
	for _, index := range []int{0, 1, 2} {
		c := New("synthetic-key", "synthetic-token")
		c.windows[index].max = 8
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		var successes atomic.Int32
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				if err := c.acquire(ctx, true); err == nil {
					successes.Add(1)
				} else if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("unexpected quota error: %v", err)
				}
			})
		}
		wg.Wait()
		cancel()
		if got := successes.Load(); got != 8 {
			t.Errorf("window %d admitted %d requests, want 8", index, got)
		}
	}
}

// TestSharedRecovery checks Retry-After also blocks other calls using these credentials.
func TestSharedRecovery(t *testing.T) {
	c := New("synthetic-key", "synthetic-token")
	c.cooldown(time.Second, true)
	original := c.windows[0].blocked
	c.cooldown(0, false)
	for i, w := range c.windows {
		if w.blocked.Before(original) {
			t.Errorf("window %d lost its recovery deadline", i)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := c.acquire(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("non-member call bypassed recovery: %v", err)
	}
}

// TestLongRetryAfterSharedRecovery retains cooldowns even when automatic retries are refused.
func TestLongRetryAfterSharedRecovery(t *testing.T) {
	for _, value := range []string{"60", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)} {
		t.Run(value, func(t *testing.T) {
			var requests atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Retry-After", value)
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer fake.Close()
			c := NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client())
			var out any
			if err := c.Get(context.Background(), "/members/me/boards", nil, &out); err == nil || err.Error() != "trello retry delay exceeds 30 seconds" {
				t.Fatalf("long Retry-After not refused: %v", err)
			}
			for i, w := range c.windows {
				if !w.blocked.After(time.Now().Add(30 * time.Second)) {
					t.Errorf("window %d lost the long cooldown", i)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if err := c.Get(ctx, "/boards/b1", nil, &out); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("next call bypassed recovery: %v", err)
			}
			if requests.Load() != 1 || c.Stats().Retries != 0 {
				t.Errorf("unexpected counters: requests=%d, stats=%+v", requests.Load(), c.Stats())
			}
		})
	}
}

// TestInterleavedRateHeaders cannot shorten a cooldown established by another response.
func TestInterleavedRateHeaders(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/members/short" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			for _, name := range []string{"Api-Key", "Api-Token", "Member"} {
				w.Header().Set("X-Rate-Limit-"+name+"-Remaining", "0")
				w.Header().Set("X-Rate-Limit-"+name+"-Interval-Ms", "1")
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer fake.Close()
	defer unblock()
	c := NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var out any
		done <- c.Get(ctx, "/members/short", nil, &out)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("short-window request did not start")
	}
	var out any
	if err := c.Get(ctx, "/members/long", nil, &out); err == nil || err.Error() != "trello retry delay exceeds 30 seconds" {
		t.Fatalf("long Retry-After not refused: %v", err)
	}
	c.mu.Lock()
	deadlines := [3]time.Time{c.windows[0].blocked, c.windows[1].blocked, c.windows[2].blocked}
	c.mu.Unlock()
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for i, w := range c.windows {
		if !w.blocked.Equal(deadlines[i]) {
			t.Errorf("window %d cooldown changed: got %v, want %v", i, w.blocked, deadlines[i])
		}
	}
}

// Test429Cancellation verifies HTTP recovery uses shared budgets and remains cancellable.
func Test429Cancellation(t *testing.T) {
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer fake.Close()
	c := NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var out any
		done <- c.Get(ctx, "/members/me/boards", nil, &out)
	}()
	// Wait for the handler to enter recovery without depending on transport timing.
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for c.Stats().Retries != 1 {
		select {
		case err := <-done:
			t.Fatalf("request ended before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("request did not enter recovery")
		case <-tick.C:
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("recovery ignored cancellation: %v", err)
	}
	if requests.Load() != 1 || c.Stats().Retries != 1 {
		t.Errorf("unexpected counters: requests=%d, stats=%+v", requests.Load(), c.Stats())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, w := range c.windows {
		if !w.blocked.After(time.Now()) {
			t.Errorf("429 did not cool down window %d", i)
		}
	}
}
