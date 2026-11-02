package syncsetup

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract A1 retires a displaced local session before taking back an expired hold.
func TestTakeBackRetiresARepliedSessionWithoutKeepingSharedWork(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	var firstAcquire atomic.Int64
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
				firstAcquire.CompareAndSwap(0, time.Now().UnixNano())
			}
			next.ServeHTTP(w, r)
		})
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	a := h.openA()
	mustNil(t, os.Remove(transcriptOf(h.cell)))
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"shared reply\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(model.Close)
	agent, err := session.New(session.Config{
		Workspace: h.work, Model: "stub/chat", APIKey: "stub-key", BaseURL: model.URL,
		SessionFile: transcriptOf(h.cell), Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, Seat: a.seat,
	})
	mustNil(t, err)
	t.Cleanup(func() { _ = agent.Close() })
	events, err := agent.Submit(ctx, "Remember the saved word.")
	mustNil(t, err)
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("scripted reply failed: %s", event.Text)
		}
	}
	h.durable(h.cell.ID, h.cell)
	before, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	mustNil(t, err)
	b := h.openOn(h.b, h.engB, first.Taken.Cell, workspaceOf(first.Taken.Cell.Root), nameB)
	b.mustSay("reply after the move")
	h.durable(h.cell.ID, first.Taken.Cell)
	mustNil(t, a.drive.CheckLease(ctx))
	if _, viewing := a.drive.Viewer(); !viewing {
		t.Fatal("displaced session still drives the chat")
	}
	retired := make(chan error, 1)
	go func() {
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				retired <- ctx.Err()
				return
			case <-tick.C:
				if agent.TakeoverAsked() {
					agent.InterruptFor(session.StopByTakeover)
					retired <- agent.Close()
					return
				}
			}
		}
	}()
	// The other holder disappears without releasing; only its expired lease remains.
	b.drive.cancel()
	<-b.drive.done
	h.quiet.Store(true)
	h.clock.advance(directory.LeaseTTL + time.Millisecond)
	continuer := h.continuerA()
	started := time.Now()
	firstAcquire.Store(0)
	continuer.opt.OnPhase = func(phase handoff.Phase) {
		if !phase.Ended.IsZero() {
			t.Logf("%s: %s", phase.Name, phase.Ended.Sub(phase.Started))
		}
	}
	back, err := continuer.Take(ctx, h.cell.ID)
	if first := firstAcquire.Load(); first == 0 {
		t.Error("take never reached acquire")
	} else if elapsed := time.Unix(0, first).Sub(started); elapsed > 2*time.Second {
		t.Errorf("first acquire took %s; want within 2s", elapsed)
	} else {
		t.Logf("first acquire: %s", elapsed)
	}
	mustNil(t, err)
	mustNil(t, <-retired)
	if session.InUse(transcriptOf(h.cell)) {
		t.Error("retired session still holds the journal")
	}
	if back.Taken.Kept != "" || back.KeptTurns != 0 {
		t.Errorf("shared work became a kept branch: %s (%d turns); shared journal bytes=%d", back.Taken.Kept, back.KeptTurns, len(before))
	}
	if branch, ok := branchRowOf(t, h.b, h.cell.ID); ok {
		t.Errorf("another computer lists a branch for shared work: %+v", branch)
	}
	raw, err := os.ReadFile(transcriptOf(back.Taken.Cell))
	mustNil(t, err)
	file := filepath.Join(workspaceOf(back.Taken.Cell.Root), fmt.Sprintf("turn-%s-1.txt", h.cell.ID[len(h.cell.ID)-4:]))
	if body, err := os.ReadFile(file); err != nil || string(body) != "reply after the move" {
		t.Errorf("latest file lost: %q, %v", body, err)
	}
	if !strings.Contains(string(raw), "reply after the move") {
		t.Fatal("arrived chat lost the newer reply")
	}
}

// Contract A1 keeps unpublished local files while the newer chat continues.
func TestTakeBackKeepsFilesSealedAfterDisplacement(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	h := bContinued(t)
	ctx := context.Background()
	mustNil(t, os.WriteFile(filepath.Join(h.work, "unsent.txt"), []byte("keep this file"), 0600))
	_, err := h.engine.Seal(ctx, h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	continuer := h.continuerA()
	started := time.Now()
	continuer.opt.OnPhase = func(p handoff.Phase) {
		if !p.Ended.IsZero() {
			t.Logf("%s: %s", p.Name, p.Ended.Sub(p.Started))
		} else {
			t.Logf("first acquire after %s", p.Started.Sub(started))
		}
	}
	back, err := continuer.Take(ctx, h.cell.ID)
	mustNil(t, err)
	if back.Taken.Kept == "" || back.KeptTurns != 1 {
		t.Fatalf("unsent file was not kept once: %+v", back)
	}
	view, err := h.a.Dir.Cell(ctx, back.Taken.Kept)
	mustNil(t, err)
	if got := h.fetchInto(view.Cell.Head)["unsent.txt"].body; got != "keep this file" {
		t.Fatalf("kept file = %q", got)
	}
}
