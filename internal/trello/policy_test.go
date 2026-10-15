package trello

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestTokenErrorsAcrossRequestPaths(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("synthetic-key synthetic-token private-name"))
			}))
			defer fake.Close()
			client := NewClient(fake.URL, "synthetic-key", "synthetic-token", fake.Client())
			for name, call := range map[string]func(context.Context) error{
				"get": func(ctx context.Context) error {
					return client.Get(ctx, "/boards/b1", nil, nil)
				},
				"write": func(ctx context.Context) error {
					return client.Write(ctx, http.MethodPut, "/cards/c1", nil, nil)
				},
				"upload": func(ctx context.Context) error {
					return client.Upload(ctx, "c1", "", "file.txt", "text/plain", []byte("synthetic"), nil)
				},
				"download": func(ctx context.Context) error {
					_, _, err := client.Download(ctx, "c1", "at1", "file.txt", io.Discard)
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					err := call(context.Background())
					want := "trello API: HTTP " + strconv.Itoa(status) + "; token/permission issue, not server policy"
					if err == nil || !strings.HasPrefix(err.Error(), want) {
						t.Fatalf("wrong token error: %v", err)
					}
					for _, hidden := range []string{"synthetic-key", "synthetic-token", "private-name"} {
						if strings.Contains(err.Error(), hidden) {
							t.Errorf("upstream diagnostic leaked %q: %v", hidden, err)
						}
					}
				})
			}
		})
	}
}
