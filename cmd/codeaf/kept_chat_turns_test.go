package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contracts G2.2–3 count one unpublished exchange across its intermediate saves.
func TestLocalContinuationKeepsOneUnpublishedTurnAcrossManySaves(t *testing.T) {
	for _, sealedAnswer := range []bool{false, true} {
		t.Run(fmt.Sprint("sealed answer ", sealedAnswer), func(t *testing.T) {
			h := newHostedMoveRig(t)
			d := h.publish(h.a, h.ea, h.c, "this computer")
			if err := d.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			journal := session.Place{Dir: h.c.Root}.Transcript()
			appendLine := func(line string) {
				t.Helper()
				f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := fmt.Fprintln(f, line)
				if err := f.Close(); writeErr != nil || err != nil {
					t.Fatalf("journal append: %v %v", writeErr, err)
				}
			}
			appendLine(`{"type":"message","role":"user","content":"one unpublished question"}`)
			appendLine(`{"type":"message","role":"assistant","content":"I will inspect it first."}`)
			for i := 0; i < 7; i++ {
				appendLine(`{"type":"message","role":"assistant","toolCalls":[{"id":"tool","type":"function"}]}`)
				appendLine(`{"type":"message","role":"tool","toolCallId":"tool","content":"tool result"}`)
				if _, err := h.ea.Seal(t.Context(), h.c, cellstore.TurnInfo{}); err != nil {
					t.Fatal(err)
				}
			}
			appendLine(`{"type":"message","role":"assistant","content":"one unpublished answer"}`)
			if sealedAnswer {
				if _, err := h.ea.Seal(t.Context(), h.c, cellstore.TurnInfo{}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := h.taker(h.a, h.ea, func(string) string { return h.c.Root }, nil).Take(t.Context(), h.c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kept == "" || got.KeptTurns != 1 {
				t.Errorf("one unpublished turn kept as %q with %d turns", got.Kept, got.KeptTurns)
			}
			if got.Resume.From != "" {
				t.Errorf("local continuation arrived from %q", got.Resume.From)
			}
		})
	}
}
