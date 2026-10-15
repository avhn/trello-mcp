package state

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// TestPersistence verifies override precedence, private permissions and concurrent atomic updates.
func TestPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trello-mcp", "state.json")
	s, err := Open(ctx, path, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Get() != (Selection{}) {
		t.Fatal("nonempty fresh state")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Update(ctx, Selection{BoardID: "b" + strconv.Itoa(i), WorkspaceID: "w1"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	reopened, err := Open(ctx, path, "")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Get() != s.Get() {
		t.Error("persisted selection differs")
	}
	overridden, err := Open(ctx, path, "env-board")
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Get().BoardID != "env-board" {
		t.Error("environment did not override saved board")
	}
	for path, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("got mode %o, want %o", info.Mode().Perm(), mode)
		}
	}
}

// TestUnsafeState rejects corrupt and symlinked state without replacing an existing selection.
func TestUnsafeState(t *testing.T) {
	ctx := context.Background()
	for _, data := range []string{"not json", `{"unknown":true}`, `{"boardId":"b1"} trailing`} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(ctx, path, ""); err == nil {
			t.Errorf("accepted corrupt state %q", data)
		}
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, link, ""); err == nil {
		t.Error("accepted symlink state")
	}
	s := &Store{path: link, selection: Selection{BoardID: "original"}}
	if err := s.Update(ctx, Selection{BoardID: "new"}); err == nil {
		t.Error("overwrote symlink state")
	}
	if s.Get().BoardID != "original" {
		t.Error("failed persistence changed selection")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Update(cancelled, Selection{}); err == nil {
		t.Error("cancelled update succeeded")
	}
}

// TestOverrideWorkspace avoids retaining the previous board's workspace after an override.
func TestOverrideWorkspace(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	saved := Selection{BoardID: "b1", WorkspaceID: "w1"}
	s, err := Open(ctx, path, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(ctx, saved); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		board string
		want  Selection
	}{{"", saved}, {"b1", saved}, {"b2", Selection{BoardID: "b2"}}} {
		reopened, err := Open(ctx, path, tc.board)
		if err != nil {
			t.Fatal(err)
		}
		if reopened.Get() != tc.want {
			t.Errorf("override %q: got %+v, want %+v", tc.board, reopened.Get(), tc.want)
		}
	}
}

func TestMemoryOnlyState(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, "", "synthetic-board")
	if err != nil || s.Get() != (Selection{BoardID: "synthetic-board"}) {
		t.Fatalf("open memory-only state: %v", err)
	}
	next := Selection{BoardID: "other-board", WorkspaceID: "synthetic-workspace"}
	if err := s.Update(ctx, next); err != nil || s.Get() != next {
		t.Fatalf("update memory-only state: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Update(cancelled, Selection{}); err == nil || s.Get() != next {
		t.Fatal("cancelled update changed memory-only state")
	}
}
