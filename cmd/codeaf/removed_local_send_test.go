package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type removedLocalSource struct {
	sendSurfaceSource
	rows []chatlist.Row
	read chan struct{}
}

func (s removedLocalSource) Rows(context.Context) ([]chatlist.Row, error) { return s.rows, nil }
func (s removedLocalSource) RowsAt(context.Context) ([]chatlist.Row, uint64, error) {
	return s.rows, 0, nil
}
func (s removedLocalSource) RowsForChatSend(ctx context.Context, file string, offline, held bool) ([]chatlist.Row, uint64, bool, error) {
	rows, version, local, err := s.surfaceMachines.RowsForChatSend(ctx, file, offline, held)
	select {
	case s.read <- struct{}{}:
	default:
	}
	return rows, version, local, err
}

// Contract G3/J20 preserves local work after removal without reviving a displaced copy.
func TestRemovedHostedWindowSendsOnlyItsOwnLocalChats(t *testing.T) {
	for _, kind := range []string{"new", "held at removal", "displaced"} {
		t.Run(kind, func(t *testing.T) {
			h := newHostedMoveRig(t)
			if err := h.a.Rename(t.Context(), "this computer"); err != nil {
				t.Fatal(err)
			}
			if err := h.b.Rename(t.Context(), "other computer"); err != nil {
				t.Fatal(err)
			}
			var rows []chatlist.Row
			if kind != "new" {
				if err := cellstore.NotePublished(h.c, "shared"); err != nil {
					t.Fatal(err)
				}
				row := chatlist.Row{Cell: h.c.ID, Status: chatlist.Here, Mine: true, Fence: 7}
				if kind == "displaced" {
					row.Status, row.Mine, row.Device = chatlist.Running, false, "other computer"
				}
				rows = []chatlist.Row{row}
			}
			if err := h.b.Dir.Revoke(t.Context(), h.a.Device.ID()); err != nil {
				t.Fatal(err)
			}
			if _, err := h.a.Dir.List(t.Context()); !errors.Is(err, directory.ErrRevoked) {
				t.Fatalf("removal fixture: %v", err)
			}
			before := len(h.requests)
			source := removedLocalSource{sendSurfaceSource{surfaceMachines{h.a}}, rows, make(chan struct{}, 1)}
			agent := &recordingSendAgent{sent: make(chan string, 1)}
			input, typed := io.Pipe()
			defer input.Close()
			defer typed.Close()
			screen := &surfaceFrameWriter{ready: make(chan struct{})}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				done <- tui3.Run(ctx, tui3.Options{Agent: agent, SessionFile: session.Place{Dir: h.c.Root}.Transcript(), Workspace: h.work, Machines: source, Input: input, Output: screen, Width: 120, Height: 35})
			}()
			defer func() { cancel(); <-done }()
			<-screen.ready
			_, _ = io.WriteString(typed, "keep working locally\r")
			if kind == "displaced" {
				select {
				case <-source.read:
				case <-time.After(5 * time.Second):
					t.Fatal("no ownership reading")
				}
				select {
				case words := <-agent.sent:
					t.Fatalf("displaced copy sent %q", words)
				case <-time.After(100 * time.Millisecond):
				}
			} else {
				select {
				case words := <-agent.sent:
					if words != "keep working locally" {
						t.Fatalf("sent %q", words)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("own chat refused after removal: %s", screen.String())
				}
			}
			h.mu.Lock()
			after := len(h.requests)
			h.mu.Unlock()
			if after != before {
				t.Errorf("removed chat sent %d relay requests", after-before)
			}
		})
	}
}
