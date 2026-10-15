// Package trello implements bounded, authenticated Trello requests and ownership checks.
package trello

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 8 << 20

// Client holds credentials privately and applies all three Trello request budgets.
type Client struct {
	base     string
	key      string
	token    string
	http     *http.Client
	mu       sync.Mutex
	windows  [3]window
	requests uint64
	retries  uint64
}

// window tracks a rolling quota and a server-reported cooling-off deadline.
type window struct {
	max      int
	interval time.Duration
	times    []time.Time
	blocked  time.Time
}

// Stats contains only non-sensitive counters for the local health tool.
type Stats struct {
	Requests uint64 `json:"requests"`
	Retries  uint64 `json:"retries"`
}

// New constructs a production client. Redirects are refused to prevent credential forwarding.
func New(key, token string) *Client {
	return NewClient("https://api.trello.com/1", key, token, &http.Client{Timeout: 30 * time.Second})
}

// NewClient accepts a test endpoint and copies transport settings without permitting redirects.
func NewClient(base, key, token string, hc *http.Client) *Client {
	copyClient := *hc
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 {
		copyClient.Timeout = 30 * time.Second
	}
	return &Client{base: strings.TrimRight(base, "/"), key: key, token: token, http: &copyClient, windows: [3]window{{max: 300, interval: 10 * time.Second}, {max: 100, interval: 10 * time.Second}, {max: 100, interval: 900 * time.Second}}}
}

// Stats snapshots the request counters without exposing authentication or request URLs.
func (c *Client) Stats() Stats { c.mu.Lock(); defer c.mu.Unlock(); return Stats{c.requests, c.retries} }

// Scrub removes both literal and query-escaped credentials from diagnostic text.
func (c *Client) Scrub(message string) string {
	secrets := []string{c.key, c.token, url.QueryEscape(c.key), url.QueryEscape(c.token)}
	// Replace longest matches first, in one pass, so overlapping credentials
	// cannot leave a suffix or modify an already-inserted redaction marker.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, 2*len(secrets))
	for _, secret := range secrets {
		if secret != "" {
			pairs = append(pairs, secret, "[redacted]")
		}
	}
	return strings.NewReplacer(pairs...).Replace(message)
}

// pause waits without leaking a timer or ignoring request cancellation.
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// acquire reserves capacity in every applicable rolling window atomically.
func (c *Client) acquire(ctx context.Context, members bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now()
		wait := time.Duration(0)
		n := 2
		if members {
			n = 3
		}
		c.mu.Lock()
		for i := 0; i < n; i++ {
			w := &c.windows[i]
			drop := 0
			for drop < len(w.times) && !w.times[drop].Add(w.interval).After(now) {
				drop++
			}
			w.times = w.times[drop:]
			until := w.blocked
			if len(w.times) >= w.max && w.times[0].Add(w.interval).After(until) {
				until = w.times[0].Add(w.interval)
			}
			if d := until.Sub(now); d > wait {
				wait = d
			}
		}
		if wait <= 0 {
			for i := 0; i < n; i++ {
				c.windows[i].times = append(c.windows[i].times, now)
			}
			c.requests++
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()
		if err := pause(ctx, wait); err != nil {
			return err
		}
	}
}

// observe applies reported ceilings and exhausted budgets, never increasing local limits.
func (c *Client) observe(h http.Header, members bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := []string{"Api-Key", "Api-Token"}
	if members {
		names = append(names, "Member")
	}
	for i, name := range names {
		prefix := "X-Rate-Limit-" + name + "-"
		w := &c.windows[i]
		if max, err := strconv.Atoi(h.Get(prefix + "Max")); err == nil && max > 0 && max < w.max {
			w.max = max
		}
		interval := w.interval
		if ms, err := strconv.ParseInt(h.Get(prefix+"Interval-Ms"), 10, 64); err == nil && ms > 0 && ms <= 900000 {
			interval = time.Duration(ms) * time.Millisecond
		}
		if interval > w.interval {
			w.interval = interval
		}
		if remaining, err := strconv.Atoi(h.Get(prefix + "Remaining")); err == nil && remaining >= 0 && remaining <= w.max {
			now := time.Now()
			for len(w.times) < w.max-remaining {
				w.times = append(w.times, now)
			}
			if until := now.Add(interval); remaining == 0 && until.After(w.blocked) {
				w.blocked = until
			}
		}
	}
}

// cooldown shares 429 recovery with concurrent requests using these credentials.
func (c *Client) cooldown(delay time.Duration, members bool) {
	until := time.Now().Add(delay)
	n := 2
	if members {
		n = 3
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := 0; i < n; i++ {
		if until.After(c.windows[i].blocked) {
			c.windows[i].blocked = until
		}
	}
}

// retryDelay honors Retry-After seconds or dates; excessive delays fail instead of retrying early.
func retryDelay(h http.Header, attempt int) (time.Duration, error) {
	d := time.Second * time.Duration(1<<attempt)
	if raw := h.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds >= 0 {
			// Saturate excessive values so conversion cannot wrap the shared cooldown.
			if seconds >= float64((1<<63-1)/time.Second) {
				d = 1<<63 - 1
			} else {
				d = time.Duration(seconds * float64(time.Second))
			}
		} else if date, err := http.ParseTime(raw); err == nil {
			d = time.Until(date)
			if d < 0 {
				d = 0
			}
		}
	}
	if d > 30*time.Second {
		return d, fmt.Errorf("Trello retry delay exceeds 30 seconds")
	}
	return d, nil
}

// Get decodes one bounded response with a total deadline and at most three 429 retries.
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	members := strings.HasPrefix(path, "/members/")
	query := url.Values{}
	for k, v := range q {
		query[k] = append([]string(nil), v...)
	}
	query.Set("key", c.key)
	query.Set("token", c.token)
	for attempt := 0; attempt <= 3; attempt++ {
		if err := c.acquire(ctx, members); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+query.Encode(), nil)
		if err != nil {
			return fmt.Errorf("build Trello request: %s", c.Scrub(err.Error()))
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("Trello request: %s", c.Scrub(err.Error()))
		}
		c.observe(resp.Header, members)
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Trello response: %s", c.Scrub(readErr.Error()))
		}
		if closeErr != nil {
			return fmt.Errorf("close Trello response: %s", c.Scrub(closeErr.Error()))
		}
		if len(data) > maxResponseBytes {
			return fmt.Errorf("Trello response exceeds 8 MiB limit")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			delay, err := retryDelay(resp.Header, attempt)
			c.cooldown(delay, members)
			if err != nil {
				return err
			}
			if attempt < 3 {
				c.mu.Lock()
				c.retries++
				c.mu.Unlock()
				continue
			}
		}
		// Error bodies can contain credentials or private data, so never surface them.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("Trello HTTP %d", resp.StatusCode)
		}
		if err = json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("invalid Trello JSON response")
		}
		return nil
	}
	return fmt.Errorf("Trello retry budget exhausted")
}
