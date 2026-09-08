package tui3

import "strings"

// ── THE ANSWER HIERARCHY ────────────────────────────────────────────────────
//
// An agentic turn is prose, then tools, then prose, then tools, then prose. This
// surface used to paint every one of those prose blocks identically, so "let me
// check the config first" arrived in the same ink, at the same margin, with the
// same headings and the same weight as the sentence that actually answered the
// question. A person scrolling back through an hour of work read a wall of
// paragraphs with no way of telling which of them they had been waiting for.
//
// THE FIX IS STRUCTURAL AND NEEDS NO COOPERATION FROM THE MODEL. Four statements,
// each true by construction:
//
//  1. PROSE FOLLOWED BY MORE WORK IN THE SAME TURN WAS NEVER THE ANSWER. It was
//     narration — the surface saying what it was about to do — and the proof
//     arrives the moment the next tool call opens under it. Nothing has to be
//     guessed and nothing has to be parsed: the entry list's own shape says it.
//  2. THE TRAILING BLOCK IS PROVISIONALLY THE ANSWER while the turn runs, which
//     is exactly what the live tier already draws (styles.go's [hueLive]).
//  3. SETTLE IS THE CONFIRMATION. When the turn ends, whatever is last is the
//     answer and everything above it in that turn is working material.
//  4. AN INTERRUPTED TURN PROMOTES NOTHING ([entry.cut]). The turn ended without
//     reaching an answer, and the absence of a flush, full-ink block under the
//     work is the surface stating that plainly rather than pretending the last
//     half-sentence was a reply.
//
// ── WHAT THE TWO TIERS LOOK LIKE, AND WHY ───────────────────────────────────
//
// THE ANSWER is unchanged: flush to the margin, the body ink, full markdown. It
// is what this surface drew for every assistant block before the hierarchy
// existed, and it goes on being drawn that way, because the answer is the thing
// the reader came for and the rendering it already had is the right one.
//
// NARRATION DROPS INTO THE WORK COLUMN AT THE MUTED TIER, PLAIN. Three decisions,
// each of them a law rather than a taste:
//
//   - THE INDENT IS THE ONE THE MACHINERY ALREADY WEARS (workfold.go's
//     [workIndent]). Demoted prose is the surface doing things, which is what the
//     two-column gutter means on every other row of a turn — a thought, a call,
//     a call's output. It costs nothing to state and it says the whole thing:
//     flush is said TO you, indented is done FOR you. [workEntry] has classified
//     assistant blocks this way since the fold wave; this file is what finally
//     makes the ink agree with the column.
//
//   - THE TIER IS THE NARRATION RUNG, NOT MUTED AND NOT DIM. Dim is the lane
//     this surface says its OWN lines in — a note, a seam, a fold chip, a tool
//     row's figures — and narration is not the surface talking about itself; it
//     is the model's own prose, one rung back. It wore muted for a wave, and
//     muted was the wrong hue for a paragraph: muted is the ACCENT one step
//     back, right for a tool's name or a heading — a word or two of label —
//     and paragraphs of it turned a working turn into a field of blue prose.
//     So the working tier is the body's own hue family at the second voice's
//     loudness ([hueNarr]): quieter than the answer in lightness, and never a
//     different KIND of thing in hue.
//
//   - A DEMOTED BLOCK IS PLAIN, WITH NO MARKDOWN AT ALL. This is the honest
//     simplification, and MARKDOWN OWNS WEIGHT (render.go's user-entry comment)
//     is what forces it. Weight is the one channel a block cannot borrow without
//     lying: a bold lead-in or a `##` heading inside demoted narration would
//     render HEAVIER than the settled answer below it, and the hierarchy would be
//     inverted by the very block it was drawn on. Nor can the weight simply be
//     stripped — prose hands back rows with its own foregrounds already spliced
//     in, and a second colour wrapped around them tears open at the first inner
//     SGR 39 (render.go's [app.assistantRows] says why the promoted head is never
//     repainted). So the demoted block takes the ONE rendering that carries no
//     weight and no colour of its own: wrapped plain text, one tier, exactly the
//     shape the live tail already uses with a different lightness in it. Nothing
//     is lost that a person wanted — narration is two sentences and a verb — and
//     what the reader gets instead is a block that cannot shout.
//
// ── AND THE BREATH ABOVE THE ANSWER ─────────────────────────────────────────
//
// A promoted answer under a turn that did work opens with ONE blank row
// ([answerBreath]). It is emitted by [app.deckRows] with every other blank on
// this surface, because [app.layout] is THE SPACING LAW and a block that appended
// a row of its own would be a second one. A turn with no work in it is not given
// the breath and renders byte-identically to what it rendered before this file
// existed — pinned by TestATurnWithNoWorkIsUntouchedByTheHierarchy.
//
// ── ONE IDEOLOGY, EVERY CHAT SURFACE ────────────────────────────────────────
//
// Nothing here reads [app.entries]. The stamp runs over whatever deck is being
// laid out, so the conversation, a task's room and a node's transcript inside a
// run's page get the same hierarchy from the same rule — which is the guarantee
// [deck] exists to make. A ROOM FOLDS NOTHING (workfold.go) and still demotes
// its narration: the chip is an affordance of the conversation, the hierarchy is
// a property of the prose.

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

// workingProse is a demoted block's rows: the model's own words, wrapped plain,
// at the tier one rung back from the body.
//
// It is a method of its own for [app.liveTail]'s reason — a test can ask for the
// rows the renderer builds instead of spelling the paint out a second time — and
// it is deliberately that function with one value changed, because the two are
// the same idea at two ends of a block's life: the growing edge is the body ink
// one step UP, and the working tier is the body ink one step DOWN.
//
// THE WIDTH IS THE COLUMN'S AND NOT THE FRAME'S. These rows are shifted two cells
// right by the indent law after layout (render.go's [app.deckRows]), so a block
// wrapped to the whole frame would be two cells wider than the column it is drawn
// in — which is the overhang [workIndentCols] exists to let a block subtract, and
// the reason [app.toolLine] subtracts it too.
func (a *app) workingProse(text string, width int) []string {
	rows := wrap(text, width-workIndentCols(width))
	for i, line := range rows {
		// A ROW WITH NOTHING ON IT IS LEFT ALONE, for [app.liveTail]'s reason:
		// [trimBlanks] decides what to drop by asking whether a row is blank, and a
		// run of spaces wrapped in an escape stops answering yes.
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows[i] = a.pal.narr(line)
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
	for i := range a.entries {
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
