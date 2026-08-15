package tui3

import (
	"strings"
	"time"
)

// THE THINKING BLOCK.
//
// Some models put their working on the wire (session.EventReasoning). It is
// worth showing — a model that is reasoning about the wrong file is worth
// catching before it edits one — and it is worth showing DIFFERENTLY, because
// it is not the answer:
//
//	⠿ the file is probably under internal/, and the caller…
//	⠿ thought for 6s · ctrl+e          ← the moment the first word of the reply
//	                                     (or the first tool call) arrives
//
// While the reasoning streams it is the lower tier of everything on screen —
// dim, italic, behind its own marker — and it sits ABOVE the answer in progress
// because that is the order it happened in. The moment the turn says anything
// that is not reasoning, the block COLLAPSES to one row carrying the one fact
// worth keeping at a glance: how long the model spent. ctrl+e, or a click, opens
// it again.
//
// ── DISPLAY-ONLY, AND WHAT THAT COSTS ──
//
// Reasoning is never journaled (internal/session says why: re-sending a model
// its own working as if it had said it out loud is a different conversation),
// so the collapsed row survives in THIS surface's entries for as long as the
// window is open and is absent from every replay — a resumed session shows the
// answers it gave, not the thinking behind them. That asymmetry is deliberate
// and it is the honest one: the alternative is a surface that invents a record
// the session does not have.

// thoughtWindow caps an expanded block. Two hundred rows is a long think and a
// short scroll; past it the block says how much it is holding back rather than
// turning the transcript into a reasoning log.
const thoughtWindow = 200

// appendThought grows the turn's reasoning block, opening one on the first
// delta.
//
// Like a text delta it does NOT ask for a repaint per chunk — it marks the block
// stale and lets the frame clock decide when a flood becomes a frame. The one
// exception is the block's first delta, which appends an ENTRY: a structural
// change the layout has to see.
func (a *app) appendThought(text string) {
	if text == "" {
		return
	}
	if a.think < 0 || a.think >= len(a.entries) || a.entries[a.think].kind != entryThinking {
		// The reply in progress is closed first, so the block lands above the
		// answer rather than splitting a paragraph that is still being written.
		a.closeLive()
		now := time.Now()
		a.entries = append(a.entries, entry{
			kind: entryThinking, turn: a.turn, began: now, ended: now,
		})
		a.think = len(a.entries) - 1
		a.follow()
		a.touch()
	}
	e := &a.entries[a.think]
	e.text += text
	e.ended = time.Now()
	e.stale = true
	a.follow()
}

// collapseThought closes the streaming block. It is called by the event pump for
// the turn's first non-reasoning event, and again when the turn settles — a turn
// that streamed nothing else still has to leave a closed block behind.
func (a *app) collapseThought() {
	if a.think < 0 {
		return
	}
	if a.think < len(a.entries) && a.entries[a.think].kind == entryThinking {
		e := &a.entries[a.think]
		e.settled, e.stale = true, true
	}
	a.think = -1
	a.touch()
}

// toggleThought opens or closes one collapsed block.
func (a *app) toggleThought(i int) bool {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryThinking {
		return false
	}
	e := &a.entries[i]
	if !e.settled {
		return false // it is streaming; there is nothing to expand yet
	}
	e.open, e.stale = !e.open, true
	a.touch()
	return true
}

// toggleLatestThought is ctrl+e: the most recent block, opened or closed.
func (a *app) toggleLatestThought() bool {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryThinking {
			return a.toggleThought(i)
		}
	}
	return false
}

// thoughtRows draws one block in whichever of its three states it is in.
func (a *app) thoughtRows(e *entry, width int) []string {
	if !e.settled {
		return a.thoughtBody(e, width, glyphThought+" ")
	}
	head := a.pal.dim(fit(glyphThought+" "+thoughtLabel(e), width))
	if !e.open {
		return []string{head}
	}
	return append([]string{head}, a.thoughtBody(e, width, "  ")...)
}

// thoughtLabel is the collapsed row's sentence. It names the key that opens it,
// for the reason the fold line does: something hidden without a way back is
// something deleted.
func thoughtLabel(e *entry) string {
	return "thought for " + itoa(thoughtSeconds(e)) + "s · ctrl+e"
}

// thoughtSeconds is the time between the FIRST and the LAST reasoning delta —
// how long the model spent thinking, not how long the turn took.
func thoughtSeconds(e *entry) int {
	span := e.ended.Sub(e.began)
	if span <= 0 {
		return 0
	}
	return int(span.Round(time.Second) / time.Second)
}

// thoughtBody is the words: wrapped, dim, italic where the terminal can say so,
// every row behind the same two-cell lead. It is capped, and it says by how much.
func (a *app) thoughtBody(e *entry, width int, lead string) []string {
	text := strings.TrimSpace(e.text)
	if text == "" {
		return nil
	}
	body := wrap(text, width-2)
	more := 0
	if len(body) > thoughtWindow {
		more, body = len(body)-thoughtWindow, body[:thoughtWindow]
	}
	out := make([]string, 0, len(body)+1)
	for i, line := range body {
		mark := "  "
		if i == 0 {
			mark = lead
		}
		out = append(out, a.pal.dim(mark+a.pal.italic(line)))
	}
	if more > 0 {
		out = append(out, a.pal.dim("  "+glyphMore+" "+itoa(more)+" more"))
	}
	return out
}
