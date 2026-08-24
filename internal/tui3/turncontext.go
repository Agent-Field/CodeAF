package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// THE TURN SAYS WHAT IT IS PART OF.
//
// Nearly every sentence a person types on this surface is a message to a
// conversation, and that is so ordinary it needs no saying. A few are not. A
// line typed at a subharness being designed goes into that design's own thread,
// where it can rewrite the page — and the transcript drew it exactly as it draws
// an ordinary chat turn: the accent `›`, a think, an answer. "use models
// dynamically in the subharness", typed at a design and acted on by it, read back
// as a remark made to nobody in particular. Somebody scrolling their own history
// had no way to learn that those five words had built something.
//
// So:
//
//	THE LAW — A TURN THAT RUNS INSIDE A NAMED WORKING CONTEXT NAMES THAT CONTEXT
//	WHERE THE TURN STARTS, AND THE NAME IS READ OFF THE SESSION'S REAL STATE.
//
// Three things follow from it, and they are the whole of this file:
//
//   - THE NAME COMES FROM THE ENGINE AND IS DRAWN VERBATIM. It is the word the
//     node itself published for what it is (session's TaskNotice.Context —
//     "designing subharness flake-triage"), taken off the node the engine
//     actually routed the turn into. Nothing here recognizes a shape, matches a
//     kind, or reads the person's own words: a surface that inferred the context
//     from the text would be guessing about the one fact the person needs to be
//     certain of, and a surface holding a list of contexts would be a second
//     place for that list to drift from the engine's.
//   - IT IS FROZEN ON THE BLOCK AT SUBMIT TIME ([entry.context]). Where a
//     sentence went is a fact about the past; asking the live session would
//     re-label a whole history the moment somebody walked into a different room.
//   - AN ORDINARY TURN DRAWS NOTHING AT ALL. Not a dim placeholder, not an empty
//     clause — nothing (docs/DESIGN-LANGUAGE.md's emptiness law). The mark
//     appears because there is something to report, and its appearing is the
//     report.
//
// ── THE FORM, AND WHY IT IS THIS ONE ──
//
//	› use models dynamically in it · designing subharness flake-triage
//
// A DIM CLAUSE ON THE PERSON'S OWN LINE. Four decisions, each of them a rule this
// surface already keeps rather than a choice made here:
//
//   - IT HAS WORDS AND IT IS NOT A GLYPH. Every mark on this surface has a word
//     near it, because an unfamiliar glyph is indistinguishable from a decorative
//     one (docs/DESIGN-LANGUAGE.md's refusal of icon-only minimalism).
//   - IT DOES NOT SPEND THE ACCENT. The accent marks the one live or chosen thing
//     on a screen, and a fact about where a sentence went is not it — a passing
//     line never gets the budget (the payload rule says exactly this).
//   - IT IS A CLAUSE AND NOT A ROW. ` · ` is the spacing ladder's clause step:
//     parts of one sentence on one line. A row above every turn would be a row
//     taken off the transcript for a fact eighteen cells can carry, which is the
//     argument the composer's own room segment is built on (room.go).
//   - AND IT WRAPS RATHER THAN BEING CUT, onto the continuation lead the block
//     already uses, because a context truncated mid-word is the one place a
//     shortened sentence could name the wrong design.
//
// ── WHAT THIS DELIBERATELY DOES NOT DO ──
//
// It does not announce the context a second time on the same frame. A design's
// room already carries the node in its pinned header and in the segment in front
// of the composer's caret, and the conversation already draws the dim
// `⠿ harness · designing with <model> · <goal> — task 4` row when a design opens
// and its settle card when one lands (harness.go, taskdone.go). This is the one
// account of it that is part of the TRANSCRIPT — the thing that scrolls, exports
// and is read back tomorrow — and the chrome around it is left exactly as it was.

// turnContextSep is the clause that joins the mark to the sentence in front of
// it: the spacing ladder's own divider, spelled the way every other clause on
// this surface is spelled.
const turnContextSep = " · "

// turnContextLead is what the mark opens with when it did not fit on the line it
// belongs to. It is the continuation lead the person's own block already wraps
// onto, so a mark on its own row sits under the sentence rather than under the
// glyph.
const turnContextLead = "  "

// turnContextFloor is the least width worth drawing a mark in. Under it the
// block is already fighting for room to hold the person's own words, and a
// context clipped to five cells names nothing.
const turnContextFloor = 12

// turnContext is the named working context THIS surface's next turn will run
// inside, or "" — which is the conversation, and nearly every turn.
//
// IT ASKS THE SESSION'S REAL STATE AND NOTHING ELSE. The room open on this
// surface names the node the engine will route the words to (room.go's
// [app.steer] hands them to that same id), and the node carries the word the
// engine published for what it is. A room the surface has had no update about
// yet, and every ordinary node, answer with nothing.
func (a *app) turnContext() string {
	if a.room == nil {
		return ""
	}
	node := a.roomNode()
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.context)
}

// markRoomContext stamps a freshly-read room history with the context those
// lines were said into.
//
// THE JOURNAL RECORDS WHAT WAS SAID AND NEVER WHERE IT WENT, which is right — a
// transcript on disk is a transcript, not a record of this program's routing — so
// the mark is put on at the door instead, off the same node every live line in
// this room reads it from. Every one of these lines was typed into THIS node, so
// there is no per-line question to ask: one context, one room.
//
// A node this surface has had no update about, and every ordinary node, name
// nothing and nothing is marked.
func (a *app) markRoomContext(room *taskRoom) {
	if room == nil {
		return
	}
	node := a.tasks[room.id]
	if node == nil {
		return
	}
	word := strings.TrimSpace(node.context)
	if word == "" {
		return
	}
	for i := range room.entries {
		if room.entries[i].kind == entryUser {
			room.entries[i].context = word
		}
	}
}

// turnContextRows puts the mark on a block that has already been laid out and
// painted. It is the last thing done to the person's own block, so the mark sits
// after the sentence in the reading order as well as on the row.
//
// The rows arrive PAINTED, which is why the mark is appended rather than spliced:
// each paint closes with its own reset, so a dim run added after one starts from
// the terminal's default and ends there.
func (a *app) turnContextRows(rows []string, word string, width int) []string {
	word = strings.TrimSpace(word)
	if word == "" || len(rows) == 0 || width < turnContextFloor {
		return rows
	}
	out := append([]string(nil), rows...)
	last := len(out) - 1
	// The clause goes on the line the sentence ended on when there is room for
	// the whole of it; a mark half on one row and half on the next would be the
	// one thing this file is against, said twice.
	if ansi.StringWidth(out[last])+ansi.StringWidth(turnContextSep+word) <= width {
		out[last] += a.pal.dim(turnContextSep + word)
		return out
	}
	// Otherwise it takes rows of its own, wrapped into the block's own column.
	// The first of them opens with the divider so the mark still reads as a
	// clause of the sentence above it rather than as a new sentence.
	body := wrap(word, width-ansi.StringWidth(turnContextLead+turnContextSep))
	for i, line := range body {
		lead := turnContextLead + turnContextSep
		if i > 0 {
			lead = turnContextLead + strings.Repeat(" ", ansi.StringWidth(turnContextSep))
		}
		out = append(out, a.pal.dim(lead+line))
	}
	return out
}
