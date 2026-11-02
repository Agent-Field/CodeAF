package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// Contract K1 and K5 require the first Enter after watch displacement to ask before running a turn.
func TestHostedWatchDisplacedWindowAsksBeforeHeartbeat(t *testing.T) {
	h := newHostedMoveRig(t)
	sleeping := make(chan struct{}, 1)
	d, err := h.a.Drive(t.Context(), h.ea, h.c, syncsetup.DriveOptions{DeviceName: "original computer", Sleep: func(ctx context.Context, _ time.Duration) error {
		select {
		case sleeping <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}, OnRenewalChange: func() {
		select {
		case h.changes <- struct{}{}:
		default:
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	sealed, err := d.Store(h.ea).Seal(t.Context(), h.c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	d.Idle()
	waitForWatchFixture(t, "publication and send hold", func() bool {
		v, err := h.a.Dir.Cell(t.Context(), h.c.ID)
		_, fence, holdErr := h.a.SendLease(h.c.ID)
		return err == nil && v.Cell.Head == sealed.Turn.ID && holdErr == nil && fence != 0
	})
	<-sleeping
	client := h.host(d, h.c, h.work, h.ea)
	screen, typed := h.surface(client, h.taker(h.a, h.ea, func(string) string { return h.c.Root }, nil), false)
	if err := h.b.Rename(t.Context(), "taking computer"); err != nil {
		t.Fatal(err)
	}
	var homeResult homeWatchTake
	homeDone := make(chan struct{})
	go func() {
		defer close(homeDone)
		c, err := takeLiveChatFromHome(t, h)
		homeResult = homeWatchTake{c, err}
	}()
	t.Cleanup(func() { <-homeDone })
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	frames := time.NewTicker(time.Millisecond)
	defer frames.Stop()
	for !strings.Contains(screen.String(), "continued this chat; this window now only shows it") {
		select {
		case <-frames.C:
		case <-deadline.C:
			t.Fatal("watch displacement notice did not arrive")
		}
	}
	h.mu.Lock()
	h.requests = nil
	h.mu.Unlock()
	if _, err := io.WriteString(typed, "keep this question unsent\r"); err != nil {
		t.Fatal(err)
	}
	h.wait("continue card or unexpected completion", func() bool {
		return strings.Contains(screen.String(), "Continue this chat here?") || h.modelCalls.Load() != 0
	})
	if h.modelCalls.Load() != 0 || !strings.Contains(screen.String(), "Continue this chat here?") {
		t.Fatalf("Enter after the displaced notice ran %d model completions instead of asking", h.modelCalls.Load())
	}
	h.mu.Lock()
	requests := append([]string(nil), h.requests...)
	h.mu.Unlock()
	for _, r := range requests {
		if strings.HasSuffix(r, "/heartbeat") || strings.HasSuffix(r, "/publish") || strings.HasPrefix(r, "POST /v1/dir/cells/") {
			t.Errorf("displaced Enter mutated relay before any heartbeat tick: %s", r)
		}
	}
	select {
	case <-homeDone:
		if homeResult.err != nil {
			t.Fatal(homeResult.err)
		}
		next, err := h.b.Drive(t.Context(), h.eb, homeResult.cell, syncsetup.DriveOptions{DeviceName: "taking computer", Sleep: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = next.Close(context.Background()) })
	case <-deadline.C:
		t.Fatal("Home take did not arrive")
	}
}

type homeWatchTake struct {
	cell cell.Cell
	err  error
}

func takeLiveChatFromHome(t *testing.T, h *hostedMoveRig) (cell.Cell, error) {
	t.Helper()
	input, typed := io.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	result := make(chan copiedTakeResult, 1)
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: &quietAgent{}, Landing: true, Machines: surfaceMachines{h.b}, DeviceID: h.b.Device.ID(), Takeover: copiedSurfaceTaker{h.taker(h.b, h.eb, func(id string) string { return filepath.Join(h.bRoots, id) }, func(p handoff.Phase) {
			if !p.Ended.IsZero() {
				t.Logf("Home take %s: %s", p.Name, p.Ended.Sub(p.Started))
			}
		}), result}, Input: input, Output: screen, Width: 120, Height: 35, Open: func(work, file string) (tui3.Conversation, error) {
			return tui3.Conversation{Agent: &quietAgent{}, Workspace: work, SessionFile: file}, nil
		}})
	}()
	defer func() { cancel(); input.Close(); typed.Close(); <-done }()
	defer func() {
		if t.Failed() {
			t.Logf("taking Home: %s", screen.String())
		}
	}()
	<-screen.ready
	if _, err := io.WriteString(typed, "  "); err != nil {
		return cell.Cell{}, err
	}
	h.wait("running chat on Home", func() bool { return strings.Contains(screen.String(), "held on original computer") })
	if _, err := io.WriteString(typed, "\r"); err != nil {
		return cell.Cell{}, err
	}
	h.wait("Home continue card", func() bool { return strings.Contains(screen.String(), "Continue this chat here?") })
	if _, err := io.WriteString(typed, "1\r"); err != nil {
		return cell.Cell{}, err
	}
	select {
	case got := <-result:
		if got.err != nil {
			return cell.Cell{}, got.err
		}
		h.wait("Home arrival", func() bool { return strings.Contains(screen.String(), "Moved from original computer") })
		return cell.OpenAt(session.DirOf(got.taken.Transcript), h.c.ID)
	case <-time.After(2 * time.Minute):
		t.Fatal("Home take did not finish")
		return cell.Cell{}, nil
	}
}

func waitForWatchFixture(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("waiting for " + what)
		}
	}
}
