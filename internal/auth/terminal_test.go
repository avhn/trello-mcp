package auth

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/term"
)

type fakeTerminal struct {
	mu        sync.Mutex
	events    []string
	state     term.State
	stateErr  error
	echoErr   error
	onDisable func()
}

func (t *fakeTerminal) record(event string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *fakeTerminal) getState() (*term.State, error) {
	t.record("capture")
	return &t.state, t.stateErr
}

func (t *fakeTerminal) disableEcho() error {
	t.record("echo off")
	if t.onDisable != nil {
		t.onDisable()
	}
	return t.echoErr
}

func (t *fakeTerminal) restore(state *term.State) error {
	if state != &t.state {
		panic("restored a different terminal state")
	}
	t.record("restore")
	return nil
}

func TestHiddenInputRestorationOrdering(t *testing.T) {
	for _, name := range []string{"success", "read failure", "cancel before capture", "cancel before read", "cancel during read", "state failure", "echo failure"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			terminal := &fakeTerminal{}
			wantEvents := "capture,echo off,read,restore"
			switch name {
			case "cancel before capture":
				cancel()
				wantEvents = ""
			case "cancel before read":
				terminal.onDisable = cancel
				wantEvents = "capture,echo off,restore"
			case "state failure":
				terminal.stateErr = io.ErrUnexpectedEOF
				wantEvents = "capture"
			case "echo failure":
				terminal.echoErr = io.ErrUnexpectedEOF
				wantEvents = "capture,echo off,restore"
			}
			started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			done := make(chan error, 1)
			go func() {
				_, err := readHidden(ctx, terminal, func() (string, error) {
					defer close(finished)
					terminal.record("read")
					close(started)
					if name == "cancel during read" {
						<-release
					}
					if name == "read failure" {
						return "", io.ErrUnexpectedEOF
					}
					return "synthetic-token", nil
				})
				done <- err
			}()
			if name == "cancel during read" {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("reader did not start")
				}
				cancel()
			}
			select {
			case err := <-done:
				if strings.HasPrefix(name, "cancel") {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation lost: %v", err)
					}
				} else if (err == nil) != (name == "success") {
					t.Fatalf("unexpected result: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("hidden input did not return promptly")
			}
			terminal.mu.Lock()
			got := strings.Join(terminal.events, ",")
			terminal.mu.Unlock()
			if got != wantEvents {
				t.Fatalf("terminal order = %q, want %q", got, wantEvents)
			}
		})
	}
}

func TestReadInputAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readInput(ctx, func() (string, error) {
		t.Error("reader started after cancellation")
		return "", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
