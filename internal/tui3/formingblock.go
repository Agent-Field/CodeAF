package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE FORMING BLOCK: what a task looks like in the seconds between a
// proposal's yes and the task existing.
//
// A person answers yes on a proposal card, and the task it names does not
// appear in the same instant — the engine admits it, and its first update is
// what puts a row on the roster. The block is what stands at the transcript tail
// meanwhile, so the yes is answered by something moving rather than by nothing:
//
//	▏ task
//	▏ adapter index
//	▏ ⠙ shaping the brief… · 3s
//
// The head is the card's own word for a task not yet named, the second row is
// the card's own name for the work — plain, because nobody typed it — and the
// third is the braille spinner and the count-up every genuinely in-flight row on
// this surface wears. The first update for the task's id takes the block away
// in the same frame the task's own row lands (taskcommand.go's
// [app.settleProposalWait]).
//
// ── SEVERAL AT ONCE ARE ONE BLOCK ──
//
// Two proposals can be forming at the same time, and three blocks of three rows
// stacked at the tail of a transcript is a wall. So more than one is ONE block
// with a head that counts them and one compact row each:
//
//	▏ tasks · 2 forming
//	▏ ⠙ adapter index · 3s
//	▏ ⠙ nil-map crash · 1s
//
// WITH EXACTLY ONE TASK FORMING, NONE OF THAT DRAWS. No head that counts and no
// roster row: that is the emptiness law read at the shape of a block rather than
// at a number — machinery for the plural case does not appear in the singular.
//
// THE PREVIEW AND THE WINDOW ARE GONE, and with them the arrow keys, the press
// and the pointer that chose which preview to draw. They showed the brief as a
// typed `/task`'s shaper wrote it, and that road no longer waits on the shaper
// at all: the task starts at once and the brief is written beside its worker
// (#936). The proposal road never had a stream behind it, so the block is only
// ever a name, a phase and a clock — nothing on it can be opened or walked.

// formingHeadWord is the plural block's head: `tasks · 3 forming`. The singular
// head is [taskFormingName]'s own word and is spelled where it always was.
const formingHeadWord = "tasks"

// formingStateWord is the state the head counts in. It is the card's own word
// ([taskFormingWord] without its ellipsis), because a person reading `forming…`
// on a card and `3 forming` on a block is reading about the same thing. The
// spelling is longer than the word it holds because [formingWord] is already a
// tool row's whole sentence one file over (toolview.go), and one name for two
// things is the drift this codebase spends its comments preventing.
const formingStateWord = "forming"

// formingRail is the block's whole border: one hairline and one space, on every
// row.
const formingRail = "▏ "

// preflightRows draws the forming block at the transcript tail. Its rows belong
// to no entry and answer no press: the block is what stands where a task is
// about to be, and there is nothing on it to open.
func (a *app) preflightRows(width int) []row {
	if !a.waiting() || width < 3 {
		return nil
	}
	if len(a.waits) == 1 {
		return a.formingOneRows(width)
	}
	return a.formingManyRows(width)
}

// formingOneRows is the singular block: the head, the task's name, the phase.
func (a *app) formingOneRows(width int) []row {
	p := &a.waits[0]
	room := width - 2
	out := []row{{text: a.pal.dim(formingRail + taskFormingName), entry: -1}}
	if p.name != "" {
		out = append(out, row{text: a.pal.dim(formingRail + fit(p.name, room)), entry: -1})
	}
	return append(out, row{text: a.pal.dim(formingRail + a.formingMark() + " " + fit(a.formingPhase(p), room-2)), entry: -1})
}

// formingManyRows is the block when more than one task is forming: one head that
// counts, and one row each.
func (a *app) formingManyRows(width int) []row {
	room := width - 2
	head := formingHeadWord + railSep + itoa(len(a.waits)) + " " + formingStateWord
	out := []row{{text: a.pal.dim(formingRail + fit(head, room)), entry: -1}}
	mark := a.formingMark()
	for i := range a.waits {
		p := &a.waits[i]
		line := mark + " " + fit(p.name, room-2)
		if word := countUpWord(a.now().Sub(p.at)); word != "" {
			line += railSep + word
		}
		out = append(out, row{text: a.pal.dim(formingRail + fit(line, room)), entry: -1})
	}
	return out
}

// formingPhase is the status row's sentence: what is happening, and for how
// long. The count-up is [countUpWord], which floors under a second by the
// emptiness law.
func (a *app) formingPhase(p *preflight) string {
	line := p.note
	if word := countUpWord(a.now().Sub(p.at)); word != "" {
		line += railSep + word
	}
	return line
}

// formingMark is the live mark every row of the block wears.
//
// The linear tier's objection to a spinner is the one it makes on a tool line: a
// claim repeated thirty times a second is heard thirty times a second by a
// surface being read aloud. A still mark makes it once.
func (a *app) formingMark() string {
	if a.linear {
		return glyphRunASCII
	}
	return tokens.Spinner(a.paints / spinnerStep)
}
