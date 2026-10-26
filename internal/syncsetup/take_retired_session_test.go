package syncsetup

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTakeBackRetiresARepliedSessionWithoutKeepingSharedWork(t *testing.T) {
	h := newTwoHomes(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
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
	back, err := h.continuerA().Take(ctx, h.cell.ID)
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
	if !strings.Contains(string(raw), "reply after the move") {
		t.Fatal("arrived chat lost the newer reply")
	}
}

func TestTakeBackKeepsFilesSealedAfterDisplacement(t *testing.T) {
	h := bContinued(t)
	ctx := context.Background()
	mustNil(t, os.WriteFile(filepath.Join(h.work, "unsent.txt"), []byte("keep this file"), 0600))
	_, err := h.engine.Seal(ctx, h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	back, err := h.continuerA().Take(ctx, h.cell.ID)
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
