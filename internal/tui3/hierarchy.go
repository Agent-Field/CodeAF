package tui3

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// The hierarchy separates conversation from operational work on every deck.
// User messages, confirmed tool-free responses and explicitly addressed [update]
// messages stay visible. The audience marker is parsed by the shared session
// layer; it is a protocol declaration, not a guess based on prose keywords.
// Addressed updates can stream before the response boundary confirms them.
//
// Tool preambles and reasoning belong to the work disclosure. Unclassified
// streaming prose remains compact until its response role is known. A completed
// response remains a reply when later work starts. Interrupting that later work
// preserves the completed reply and marks only the unfinished continuation cut.
//
// Replies use full markdown at the body margin. Opened narration retains
// markdown structure in quieter ink in the work column; raw markdown markers do
// not substitute for rendering. Captions take the introducing narration, while
// the remaining text and all intervening entries remain behind the step door.
//
// These decisions are computed over the deck, so chat, manager, task and nested
// transcript pages use the same classification. Explicit audience metadata is
// persisted for replay alongside structural response boundaries.

// stampHierarchy writes THE ANSWER HIERARCHY onto the blocks of one deck, before
// any of them is asked for its rows.
//
// It is a pass rather than a question each renderer asks because the answer is a
// fact about the LIST — "is there more work after this block in this turn" — and
// [app.renderEntry] is handed one block at a time. Deriving it per block would
// mean walking the list once per block; deriving it once per layout costs one
// walk and leaves the renderer with a field to read.
//
// THE STALE FLAG IS THE POINT OF THE COMPARISON. A block whose tier changed is
// holding rows it drew in the other tier, and [app.entryRows] hands those back
// unless something says otherwise — so the flip and the invalidation are one
// statement, exactly as [feed.closeLive] writes the settle. A block whose tier did
// NOT change is left alone, which is nearly every block on every frame: this pass
// is free unless something actually moved.
func stampHierarchy(es []entry, folds map[int]workfold) {
	for i := range es {
		if es[i].kind != entryAssistant {
			continue
		}
		if want := workEntry(es, folds, i); es[i].demoted != want {
			es[i].demoted, es[i].stale = want, true
		}
	}
}

// stampCaptions lifts one proven narration line into each step heading.
//
// THE ANSWER IS NEVER A CAPTION. [stampHierarchy] has already proved which
// blocks precede more work, and this pass only marks heads from that set.
func stampCaptions(es []entry, captions []caption) {
	heads := make(map[int]int, len(captions))
	for _, c := range captions {
		if c.source != captionSaid || c.head < 0 || c.head >= len(es) {
			continue
		}
		_, cut := captionSpan(es[c.head].text)
		heads[c.head] = cut
	}
	for i := range es {
		wantCut, wantHead := heads[i]
		if es[i].capHead != wantHead || es[i].capCut != wantCut {
			es[i].capHead, es[i].capCut, es[i].stale = wantHead, wantCut, true
		}
	}
}

// workingProse renders disclosed narration as markdown structure in quiet ink.
// Strip the markdown renderer's styling before applying the narration palette
// so nested foreground resets cannot accidentally promote part of the block.
// Layout uses the work column width; the shared row pass applies its indent.
func (a *app) workingProse(text string, width int) []string {
	rows := a.renderMarkdown(text, max(1, width-workIndentCols(width)))
	for i, line := range rows {
		// A ROW WITH NOTHING ON IT IS LEFT ALONE, for [app.liveTail]'s reason:
		// [trimBlanks] decides what to drop by asking whether a row is blank, and a
		// run of spaces wrapped in an escape stops answering yes.
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows[i] = a.pal.narr(ansi.Strip(line))
	}
	return trimBlanks(rows)
}

// answerBreath reports whether the block at i is A PROMOTED ANSWER UNDER WORK,
// which is the one blank row this file adds to THE SPACING LAW.
//
// It exists because the gap the reader needs is not the gap the old rules
// produced. "One blank after a cluster" covers the commonest turn — tools, then
// the answer — but a turn that thought and then answered, and a turn whose whole
// machinery collapsed into one chip, both put the answer hard against the row
// above it. The rule people actually read is about the ANSWER and not about what
// happened to precede it: a turn that did work gets one row of silence before the
// thing it was working towards, however that work was drawn.
//
// It is asked in the same `if` as the other four rules rather than beside them,
// because a gap asked for twice is still one gap and [app.deckRows]'s emitter is
// not idempotent — two calls are two blank rows.
//
// [entry.demoted] is read rather than re-derived: [stampHierarchy] has already
// run over this deck, and a second derivation is a second rule.
func answerBreath(es []entry, i int) bool {
	e := es[i]
	if e.kind != entryAssistant || e.demoted || strings.TrimSpace(e.text) == "" {
		return false
	}
	// Backwards to THE PERSON'S LAST MESSAGE, which is where this answer's own
	// work begins — the same boundary [workEntry] walks forward to, because "the
	// work behind this answer" and "the answer this work led to" have to be two
	// readings of one span. A divider is stepped over: it is a line about the
	// session rather than a step in it.
	for at := i - 1; at >= 0 && es[at].turn == e.turn; at-- {
		if entryWithdrawn(&es[at]) {
			// A correction the turn never gave the model draws nothing, so it is
			// stepped over here for the divider's reason: a block with no rows
			// cannot be the work this answer came out of (steerelbow.go).
			continue
		}
		switch es[at].kind {
		// AND A CORRECTION IS ONE OF THE PERSON'S MESSAGES. The walk is looking
		// for work between this answer and the last thing they said, and a
		// sentence they typed into the turn is the last thing they said.
		case entryUser, entrySteer:
			return false
		case entryDivider:
		default:
			return true
		}
	}
	return false
}

// cutTurn marks one turn as STOPPED BY THE PERSON ([entry.cut]).
//
// It is written onto the blocks rather than held as a number on the surface for
// one reason: the mark has to outlive the state that produced it. [app.state]
// leaves stateInterrupted the moment the next message is sent and [app.turn]
// moves with it, so a rule that asked "was the session interrupted" would promote
// the stopped turn's last paragraph as soon as the person asked anything else.
// A fact about a turn belongs on that turn's blocks.
//
// THE PERSON'S OWN MESSAGE IS NOT MARKED. It is what the turn was answering and
// it was said in full; only the work and the prose the turn managed to produce
// were cut short. Marking it would also put it inside the chip that
// [deriveWorkfolds] builds for a stopped turn, and the question is the one thing
// on this surface a fold may never hide.
//
// It is called TWICE for a single interrupt — at the keypress and again when the
// stream finally closes ([app.settle]) — because events already in flight land
// between the two, and a block appended after the mark would be the one block of
// the turn still claiming to be an answer.
func (a *app) cutTurn(turn int) {
	changed := false
	// A completed response already said its words to the person. Only the
	// activity resumed after its boundary is being interrupted now.
	from := 0
	for i := range a.entries {
		if a.entries[i].turn == turn && confirmedAnswer(&a.entries[i]) {
			from = i + 1
		}
	}
	for i := from; i < len(a.entries); i++ {
		e := &a.entries[i]
		// AND A CORRECTION IS NOT MARKED EITHER, for the person's own message's
		// reason said again: it is a thing they said in full, and only the work
		// and the prose the turn managed to produce were cut short (steerelbow.go).
		if e.turn != turn || e.kind == entryUser || e.kind == entrySteer || e.cut {
			continue
		}
		e.cut, e.stale, changed = true, true, true
	}
	if changed {
		a.touch()
	}
}
