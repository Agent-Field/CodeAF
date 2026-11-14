package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type walkNoRestore struct {
	calls     atomic.Int32
	attempted chan struct{}
}

func (t *walkNoRestore) Take(context.Context, string) (tui3.Taken, error) {
	t.calls.Add(1)
	select {
	case t.attempted <- struct{}{}:
	default:
	}
	return tui3.Taken{}, errors.New("D1 attempted a restore for same-window work")
}
func (t *walkNoRestore) TakeSeen(ctx context.Context, id string, _ *directory.LeaseExpectation) (tui3.Taken, error) {
	return t.Take(ctx, id)
}
func (t *walkNoRestore) OpenAvailable(ctx context.Context, id string, _ *directory.LeaseExpectation) (tui3.Taken, error) {
	return t.Take(ctx, id)
}

func TestIdleRelease_C3_D1_NativeTerminalModelSendReacquiresInPlace(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	celltest.RequireEngine(t)
	h := newHostedMoveRig(t)
	if err := h.b.Rename(t.Context(), "laptop"); err != nil {
		t.Fatal(err)
	}
	if raw, err := exec.Command("git", "init", "--quiet", h.work).CombinedOutput(); err != nil {
		t.Fatalf("fixture repository: %s, %v", raw, err)
	}
	ignored := filepath.Join(h.work, "node_modules", "x", "index.js")
	if err := os.MkdirAll(filepath.Dir(ignored), 0700); err != nil {
		t.Fatal(err)
	}
	for file, text := range map[string]string{filepath.Join(h.work, ".gitignore"): "node_modules/\n", ignored: "local install"} {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	drive := h.publish(h.a, h.ea, h.c, "desktop")
	drive.SetIdleCheck(func() bool { return false })
	entered := make(chan struct{}, 4)
	var sending atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sending.Load() && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions") {
			view, err := h.a.Dir.Cell(r.Context(), h.c.ID)
			if err != nil || view.Cell.Lease.Device != h.a.Device.ID() || view.Cell.Lease.Expires <= view.Now || view.Cell.Lease.Fence < 2 {
				t.Errorf("D1 provider entered without a new owned fence: %+v, %v", view, err)
			}
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"walk answer\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(model.Close)
	seat, err := cellstore.SeatOver(executor.HostBound, h.c, h.work, nil, nil, func(cellstore.Engine) cellstore.Store { return drive.Store(h.ea) })
	if err != nil {
		t.Fatal(err)
	}
	workGate := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), driveWorkWithin)
		defer cancel()
		confirmed, err := drive.ConfirmWork(ctx)
		if err != nil {
			return err
		}
		if !confirmed {
			return cellstore.ErrReadOnly
		}
		return drive.Gate()
	}
	seat = executor.Gated(seat, drive.Gate, workGate)
	watch := &cellstore.SealWatch{OnTurnEnd: drive.Idle}
	watch.OnTail = func() { executor.Settle(t.Context(), seat); drive.Idle() }
	watch.Waiting()
	file := session.Place{Dir: h.c.Root}.Transcript()
	agent, err := session.New(session.Config{Workspace: h.work, Place: session.Place{Dir: h.c.Root, Workspace: h.work}, SessionFile: file,
		Model: "stub/chat", APIKey: "stub", BaseURL: model.URL, System: "SYSTEM", Seat: seat, Seals: watch, Renewals: moveRenewals{drive, h.changes}, ProfileDir: h.a.Home})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drive.SetIdleCheck(watch.IdleReady)
	agent.SettleWrites()
	if _, err = drive.Store(h.ea).Seal(t.Context(), h.c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	drive.Idle()
	h.wait("source to release", func() bool {
		v, err := h.a.Dir.Cell(t.Context(), h.c.ID)
		return err == nil && v.Cell.Lease.Expires == 0
	})
	noRestore := &walkNoRestore{attempted: make(chan struct{}, 1)}
	input, typed := io.Pipe()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: agent, Workspace: h.work, SessionFile: file, Machines: surfaceMachines{h.a}, Takeover: noRestore,
			Input: input, Output: screen, Width: 120, Height: 35})
	}()
	defer func() { cancel(); input.Close(); typed.Close(); <-done }()
	<-screen.ready
	sending.Store(true)
	if _, err = io.WriteString(typed, "ordinary model message in this window\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-noRestore.attempted:
		t.Fatal("D1 ordinary send took the restore door")
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("D1 no model call: %s", screen.String())
	}
	h.wait("model answer saved", func() bool { raw, _ := os.ReadFile(file); return strings.Contains(string(raw), "walk answer") })
	if noRestore.calls.Load() != 0 {
		t.Fatal("D1 restore ran")
	}
	if raw, err := os.ReadFile(ignored); err != nil || string(raw) != "local install" {
		t.Fatalf("D1 ignored install changed: %q, %v", raw, err)
	}
	for _, text := range []string{"Moved here", "Set this machine up like"} {
		if strings.Contains(screen.String(), text) {
			t.Fatalf("D1 showed %q", text)
		}
	}
}
