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
//	⠿ thinking · 148 tok                  ← the header, while it streams
//	  the file is probably under internal/   35% — read already
//	  and the caller in cmd/ passes it       60%
//	  the path it was given, so start there  85% — the newest line
//	⠿ thought for 6s · 148 tok · ctrl+e   ← the moment the first word of the
//	                                         reply (or the first tool call) lands
//
// While the reasoning streams it is the lower tier of everything on screen —
// dim, italic, behind its own marker — and it sits ABOVE the answer in progress
// because that is the order it happened in. The moment the turn says anything
// that is not reasoning, the block COLLAPSES to one row carrying the two facts
// worth keeping at a glance: how long the model spent, and how much it wrote.
// ctrl+e, or a click, opens it again.
//
// ── WHY THREE LINES AND NOT ALL OF THEM ──
//
// The streaming block used to draw its whole text, which grew a wall: a model
// that reasons for thirty seconds pushes the answer it is about to write off
// the bottom of the screen, and a wall of dim italic that scrolls under the
// reader's eye is not readable at any speed. Three lines is the READING
// WINDOW — where the model is now, and the two steps it took to get there —
// and the fade (styles.go's [thoughtFade]) is what says which end is new
// without anybody having to work it out. The whole text is not lost: it is one
// ctrl+e away the moment the block settles.
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

// thoughtLive is the streaming window: the last three wrapped lines, and no
// more, however long the model goes on for.
const thoughtLive = 3

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
//
// IT DOES NOT CLOSE A BLOCK THE PERSON OPENED. That is the whole of the latch
// (see [entry.latched]): the automatic collapse is this surface's opinion about
// a block nobody has said anything about, and it stops being anybody's opinion
// the moment somebody presses ctrl+e. A block opened mid-stream stays open
// through the settle, through every later delta of the turn, and until the same
// person closes it again.
func (a *app) collapseThought() {
	if a.think < 0 {
		return
	}
	if a.think < len(a.entries) && a.entries[a.think].kind == entryThinking {
		e := &a.entries[a.think]
		e.settled, e.stale = true, true
		if !e.latched {
			e.open = false
		}
	}
	a.think = -1
	a.touch()
}

// toggleThought opens or closes one block, streaming or settled, and LATCHES
// what was chosen — in whichever list is on screen.
//
// It used to refuse while the block was streaming — "there is nothing to expand
// yet" — and that was wrong twice over. There is something to expand: the whole
// buffer is in [entry.text] and only the last three wrapped lines of it are on
// screen, so the reasoning a person wants to read is precisely the part the
// reveal window is holding back. And the moment they most want it is while it is
// still going, because a model reasoning about the wrong file is worth catching
// before it edits one — which is the argument the block's own header makes for
// existing at all.
//
// A NODE REASONS TOO, so this reads the deck rather than the conversation: a
// page that showed the working but would not open it is a block with its own key
// printed on it and nothing behind the key (render.go's [app.bodyDeck],
// room.go).
func (a *app) toggleThought(i int) bool {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || es[i].kind != entryThinking {
		return false
	}
	e := &es[i]
	e.open, e.latched, e.stale = !e.open, true, true
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
	return true
}

// toggleLatestThought is ctrl+e: the most recent block, opened or closed.
func (a *app) toggleLatestThought() bool {
	es := a.bodyDeck().entries
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].kind == entryThinking {
			return a.toggleThought(i)
		}
	}
	return false
}

// thoughtRows draws one block in whichever of its three states it is in. Under
// the pointer the marker brightens — the block is clickable, and this is the
// row saying so (hover.go).
func (a *app) thoughtRows(e *entry, width int, hovered bool) []string {
	if !e.settled {
		// The live header names the key too, for the reason the collapsed one
		// does: a window that holds three of thirty lines back has hidden
		// something, and something hidden without a way to it is something
		// deleted.
		label := " thinking · " + thoughtCount(e) + " · ctrl+e"
		head := a.pal.dim(fit(glyphThought+label, width))
		if hovered {
			head = a.pal.accent(glyphThought) + a.pal.dim(fit(label, width-1))
		}
		// AN OPENED BLOCK RESOLVES FROM THE BUFFER, NOT FROM THE WINDOW. The
		// three-line reveal is what this block shows a reader who has not asked;
		// a reader who has asked gets every word the model has written so far,
		// through the same renderer the settled block uses. Drawing the reveal
		// slice here — a wider window, or a taller one — would be the surface
		// answering "show me the thinking" with more of the same summary.
		if e.open {
			return append([]string{head}, a.thoughtBody(e, width)...)
		}
		return append([]string{head}, a.thoughtLiveRows(e, width)...)
	}
	head := a.pal.dim(fit(glyphThought+" "+thoughtLabel(e), width))
	if hovered {
		head = a.pal.accent(glyphThought) + a.pal.dim(fit(" "+thoughtLabel(e), width-1))
	}
	if !e.open {
		return []string{head}
	}
	return append([]string{head}, a.thoughtBody(e, width)...)
}

// thoughtLabel is the collapsed row's sentence. It names the key that opens it,
// for the reason the fold line does: something hidden without a way back is
// something deleted. The size sits between the two so the row reads as one
// sentence about the think and ends on the way back into it.
func thoughtLabel(e *entry) string {
	return "thought for " + itoa(thoughtSeconds(e)) + "s · " + thoughtCount(e) + " · ctrl+e"
}

// thoughtCount is how much the model wrote, ESTIMATED at four bytes to the
// token — the same rule of thumb every surface in this tree uses where the
// provider does not report reasoning tokens separately, and most do not.
//
// It is derived from the accumulated text rather than counted per delta on the
// way in, and that is not laziness: a provider that streams reasoning a word at
// a time delivers chunks shorter than four bytes, and a counter that divided
// each of them would report zero for the whole think.
func thoughtCount(e *entry) string {
	return itoa(len(e.text)/4) + " tok"
}

// thoughtLiveRows is THE WINDOW: the last [thoughtLive] wrapped lines of a
// think in progress, oldest first, each painted one stop further along the fade.
//
// The newest line always takes the LAST stop, so a think that has only written
// one line so far opens at the tier it will keep rather than starting faint and
// brightening — the gradient says "this is where you are", not "this is how
// much there is".
func (a *app) thoughtLiveRows(e *entry, width int) []string {
	body := trimBlanks(wrap(strings.TrimSpace(e.text), width-2))
	if len(body) == 0 {
		return nil
	}
	if len(body) > thoughtLive {
		body = body[len(body)-thoughtLive:]
	}
	first := len(thoughtFade) - len(body)
	out := make([]string, 0, len(body))
	for i, line := range body {
		out = append(out, a.pal.fade("  "+a.pal.italic(line), first+i))
	}
	return out
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

// thoughtBody is the EXPANDED block: every word the model wrote, wrapped, dim,
// italic where the terminal can say so, every row behind the same two-cell
// lead. It is capped, and it says by how much.
//
// It is deliberately unfaded. The fade is a live cue about which line is the
// newest, and in a block somebody opened on purpose there is no newest line —
// there is a document, and a document that dims toward its own top is one that
// has been made harder to read for a reason that no longer applies.
func (a *app) thoughtBody(e *entry, width int) []string {
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
	for _, line := range body {
		out = append(out, a.pal.dim("  "+a.pal.italic(line)))
	}
	if more > 0 {
		out = append(out, a.pal.dim("  "+glyphMore+" "+itoa(more)+" more"))
	}
	return out
}
