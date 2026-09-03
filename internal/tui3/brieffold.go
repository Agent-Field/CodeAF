package tui3

// THE INSTRUCTION A TASK WAS GIVEN, FOLDED.
//
// A node's page opens on the words it was sent to do: the first line of its
// record is the person's own message ([roomReplay], replay.go), and it is
// drawn the way every message on this surface is drawn — whole. That is right
// for the sentence somebody typed after `/task` and wrong for everything else
// that arrives there: a brief the planner wrote out in full, a paragraph pasted
// out of an issue, a spec with its acceptance criteria under it. Sixty rendered
// lines of assignment filled the page, and a person who walked into a room to
// watch work happen had to scroll past their own instructions to reach the first
// tool call.
//
// So the block folds. THREE LINES AND A DOOR — enough to recognise what the work
// was asked for, and one dim line saying how much more there is and which key
// opens it. It is home's fold said on a transcript: [bandFoldMark] and
// [bandFoldWord] are CALLED here rather than copied, because two spellings of
// one fold is two things to keep in step, and a person who has learned
// `▸ …3 more tasks` on the home column has already learned this line.
//
// IT IS THE ONLY MESSAGE ON THIS SURFACE THAT FOLDS, and that is a law rather
// than a shortage of ambition. workfold.go states the other half of it: the one
// thing on this surface a fold may never hide is the person's own words. An
// instruction is the one message that is not read as part of a conversation — it
// is the terms of reference at the head of a page about something else, it sits
// at the top where nothing pushes it away, and a fold that keeps its opening on
// screen and says exactly what it is holding back is not hiding anything. Every
// other message — theirs out in the conversation, theirs steered into a running
// node — is drawn whole, always, and there is no key that folds it.
//
// AND NOTHING ABOUT THE FOLD IS KEPT. An expansion dies with the page that owns
// it, which is workfold.go's rule for the same kind of state and for its reason:
// replaying the journal derives the same fold, and a person's look at the small
// print is not a fact about the work. It is per-page by construction — the state
// lives on the block, and a room builds its blocks fresh every time it opens.

// briefFoldLines is how much of the instruction a folded page keeps on screen.
//
// THREE, BECAUSE THIS SURFACE ALREADY FOLDS AT THREE. Home's list-shaped bands
// keep three rows above their fold line ([app.bandFold]'s callers), so a folded
// instruction is a four-row block wearing the exact footprint a folded band
// wears one screen over. DESIGN-LANGUAGE.md's ladder is explicit that A NEW
// DISTANCE NEEDS A NEW RELATIONSHIP; this is the same relationship — some of a
// thing, and a door onto the rest — so it reuses the number rather than minting
// a fourth one.
//
// AND THREE IS THE LEAST THAT STILL READS AS PROSE. One line is a title, which
// the room's header is already carrying; two is a sentence cut in half. What the
// visible lines buy is a person recognising the assignment without opening
// anything, and recognising prose takes a paragraph's worth of it.
const briefFoldLines = 3

// briefFoldWhat is the noun the fold line counts in.
//
// THE UNIT IS A DISPLAY LINE because that is the unit the reader can see. A door
// counting sentences, words or bytes would be a door whose number does not match
// the rows it is holding back — and on a brief written in Japanese it would not
// even be close, since one of those glyphs takes two cells. Everything here is
// measured through [wrap], which measures in cells.
const briefFoldWhat = "lines"

// briefFoldKey is the key the fold line names. It is `ctrl+o`'s own meaning —
// SHOW ME THE REST OF THIS (input.go) — spent on the one block in a room that is
// showing less than it has.
//
// The line says it for the reason the worked chip says `· ctrl+e` (workfold.go):
// a block has no legend under it, so a fold that wants to be reachable without a
// mouse has to name its key where the fold is.
const briefFoldKey = "ctrl+o"

// briefFoldHidden is how many rendered lines the door is holding back, and zero
// for every block that has no door: an ordinary message, and an instruction
// short enough to be drawn whole. It is the one question this file answers, and
// everything else here is built on it.
//
// IT IS MEASURED THE WAY THE BLOCK IS PAINTED and never estimated: the same
// [wrap] at the same width the person's words are given ([app.renderEntry]'s
// entryUser case), so the count on the door is the count of rows behind it at
// this width in this terminal. That is also the whole of the wide-rune rule —
// [wrap] breaks on display width, so a brief of double-width glyphs folds at the
// place on screen a reader would point to, not at a rune count that happens to
// match on Latin text.
//
// The paragraph is wrapped a second time here, having already been wrapped to be
// drawn. That is deliberate and it is cheap: it runs for ONE block of a page
// whose rows are cached whole ([app.roomRows] rebuilds only on a resize or a
// change), and the alternative — carrying the count out of the renderer on the
// block — would be a second field that can disagree with the paint.
func briefFoldHidden(e *entry, width int) int {
	if e == nil || !e.brief {
		return 0
	}
	if n := len(wrap(e.text, width-userLeadCols)) - briefFoldLines; n > 0 {
		return n
	}
	return 0
}

// briefFoldCut is the instruction's visible opening: the first [briefFoldLines]
// of it while it is folded, and all of it once somebody has opened it.
//
// IT IS GATED ON THE SAME FIELD [briefFoldHidden] IS GATED ON, and that is the
// whole of this function's law rather than a tidiness. The two are one fold seen
// from its two ends — this one takes the lines away, that one counts what was
// taken and is what makes the door and the key exist ([app.deckRows] draws the
// door only for a non-zero count, and [app.toggleBriefFold] skips every block
// that returns zero) — so a cut that fired where the count did not is a fold
// WITH NO DOOR, NO KEY AND NO ELLIPSIS. That is exactly what happened to an
// ordinary message out in the conversation: it was cut at three rows mid-
// sentence, and the rest of what somebody typed was unreachable with nothing on
// screen saying it had been taken. The header above states the law it broke —
// the one thing on this surface a fold may never hide is the person's own words.
func briefFoldCut(e *entry, body []string) []string {
	if e == nil || !e.brief || e.full || len(body) <= briefFoldLines {
		return body
	}
	return body[:briefFoldLines]
}

// briefFoldLine is the door itself: the mark, how much is behind it, and the key
// that opens it. It hangs in the column the message's own continuation lines
// hang in, so the door reads as the foot of the block rather than as a line of
// this surface's own.
func (a *app) briefFoldLine(hidden int, folded bool, width int) string {
	line := userLead + bandFoldMark(a.pal, folded) + " " +
		bandFoldWord(hidden, briefFoldWhat, folded) + railSep + briefFoldKey
	return a.pal.dim(fit(line, width))
}

// toggleBriefFold opens the instruction on the page a person is standing on, or
// folds it back, and answers whether there was one to act on.
//
// A PAGE WITH NO DOOR ON IT ANSWERS FALSE, which is what lets the key keep every
// other meaning it has: a room whose instruction is one line has nothing folded,
// so `ctrl+o` falls through to the fold it has always opened out in the
// conversation (input.go).
func (a *app) toggleBriefFold() bool {
	d := a.bodyDeck()
	width := a.bodyWidth()
	for i := range d.entries {
		e := &d.entries[i]
		if briefFoldHidden(e, width) == 0 {
			continue
		}
		a.setBriefFold(e, !e.full)
		return true
	}
	return false
}

// toggleBriefFoldAt is the same act reached by pressing the door, and it is
// keyed by the block the door belongs to. The guard is [app.openTool]'s: the
// index names a place in whichever list the body is drawing, and a room's blocks
// are not the conversation's (render.go's [app.bodyDeck]).
func (a *app) toggleBriefFoldAt(i int) {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || !es[i].brief {
		return
	}
	a.setBriefFold(&es[i], !es[i].full)
}

// setBriefFold writes the state and tells everything that cached a row built
// from it to build again: the block keeps its own rows until they are stale
// ([app.entryRows]) and the page keeps its whole list until the room is dirty
// ([app.roomRows]), so a fold that touched neither would flip a flag nobody
// draws.
func (a *app) setBriefFold(e *entry, full bool) {
	e.full, e.stale = full, true
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}
