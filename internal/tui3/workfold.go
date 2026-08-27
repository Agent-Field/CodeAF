package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// workfold is render-time structure. Nothing here is journaled: replaying the
// same entries derives the same fold, while a person's expansion dies with the
// window that owns it.
type workfold struct {
	turn, start, answer int
	tools               int
	thought             time.Duration
	took                time.Duration
	// stopped says this chip covers A TURN THE PERSON STOPPED (hierarchy.go's
	// [entry.cut]) rather than one that finished. Such a turn has no answer to
	// leave standing under the chip, so [workfold.answer] is one past the turn's
	// last block and the chip is the whole of what is left — which is the fold
	// saying the same thing the missing flush paragraph says.
	stopped bool
}

// deckFolds is the chips ONE PAGE draws, and it is where the room's exemption
// lives: A ROOM FOLDS NOTHING.
//
// The chip is an affordance of the conversation and it earns its place there. A
// turn out in the thread is a question somebody asked and the answer they were
// given, and the machinery between the two is work they delegated precisely so
// they would not have to watch it — so it collapses, and the page reads back as
// the exchange it was.
//
// A room is the opposite errand. It is the page somebody opened BECAUSE they
// want to read the machinery, and a node's whole life is one long turn with a
// report at the end of it — so the same rule swallowed the entire page the
// instant the node stopped running, leaving `▸ worked · 10 tool calls · ctrl+e`
// and the report under it. That is the complaint this answers: a person who
// walked into a task to watch it work was shown its result and nothing else.
//
// It is stated as an absence of folds rather than as a fold that opens itself,
// because [deck.workOpen] is the READER'S own answer and a default that had to
// be inverted for one kind of page would give that map two meanings. With no
// chip there is nothing to open, and `ctrl+e` falls through to the thinking
// block, which is the key's other meaning and the one a room advertises.
func (a *app) deckFolds(d deck) map[int]workfold {
	if d.showsWork {
		return nil
	}
	return deriveWorkfolds(d.entries, d.runningTurn)
}

// deriveWorkfolds finds completed turns with machinery followed by a real
// trailing answer. A question, failure, cancellation, or tools-only tail has
// no eligible trailing answer and therefore cannot disappear into a chip.
//
// AND TURNS THE PERSON STOPPED, which are the one kind that folds with NOTHING
// left standing under the chip (hierarchy.go). A stopped turn reached no answer,
// so there is no block to promote and no block to leave out of the fold: the
// machinery and the half-sentence it got to are all working material, and the
// chip says so in its own words ([app.workfoldLabel]).
func deriveWorkfolds(es []entry, runningTurn int) map[int]workfold {
	out := make(map[int]workfold)
	for lo := 0; lo < len(es); {
		// A GROUP IS THE BLOCKS BETWEEN TWO OF THE PERSON'S MESSAGES, which is
		// the turn number and ONE THING MORE: a message sent into a turn that is
		// already streaming does not open a new turn (app.go's
		// [app.submittingShown] — steering is not a second turn), so a turn number
		// alone can span two questions. The chip hides the machinery BETWEEN a
		// question and its answer, so a run that had a second question in the
		// middle of it would fold that question away — and the one thing on this
		// surface a fold may never hide is the person's own words.
		hi := lo + 1
		for hi < len(es) && es[hi].turn == es[lo].turn && !groupBreaks(&es[hi]) {
			hi++
		}
		answer := -1
		blocked, stopped := false, false
		for i := lo; i < hi; i++ {
			if es[i].kind == entryAssistant && strings.TrimSpace(es[i].text) != "" {
				answer = i
			}
			if es[i].cut {
				stopped = true
			}
			if es[i].kind == entryTask || es[i].kind == entryConnect || es[i].kind == entryStanding ||
				(es[i].kind == entryNote && strings.HasPrefix(es[i].text, "cancel")) {
				blocked = true
			}
			// A SEAM IS NEVER FOLDED AWAY. A chip hides the machinery between a
			// question and its answer, and the run of blocks it hides is chosen by
			// position — so a seam that happened to sit inside one would vanish
			// with it, and the fold would be quietly claiming that the
			// conversation above it is the same unbroken conversation. The whole
			// group keeps its rows instead (replay.go's [entrySeam]).
			if es[i].kind == entrySeam {
				blocked = true
			}
		}
		// THE END OF WHAT THE CHIP SWALLOWS. An ordinary fold stops at the answer
		// and leaves it standing; a stopped turn's fold runs to the end of the
		// group, because there is nothing in it that was said TO the person.
		end := answer
		eligible := answer >= 0 && es[answer].settled
		if stopped {
			end, eligible = hi, true
			// EXCEPT THE SURFACE'S OWN NEWS AT THE TAIL. An interrupt writes lines
			// of its own under the turn it stopped — that it was interrupted, what
			// the session dropped from the queue — and those are the only rows on a
			// stopped turn that are addressed TO the person. Folding them would be
			// the surface telling somebody their message was dropped and hiding the
			// sentence in the same breath.
			for end > lo && es[end-1].kind == entryNote {
				end--
			}
		}
		if eligible && !blocked && es[lo].turn != runningTurn {
			f := workfold{turn: es[lo].turn, start: -1, answer: end, stopped: stopped}
			var began, ended time.Time
			for i := lo; i < end; i++ {
				e := &es[i]
				work := e.kind != entryUser && e.kind != entryDivider && e.kind != entrySteer
				if !work {
					continue
				}
				if f.start < 0 {
					f.start = i
				}
				if e.kind == entryTool {
					f.tools++
				}
				if e.kind == entryThinking {
					f.thought += e.ended.Sub(e.began)
				}
				if !e.began.IsZero() && (began.IsZero() || e.began.Before(began)) {
					began = e.began
				}
				if e.ended.After(ended) {
					ended = e.ended
				}
			}
			if f.start >= 0 {
				if !began.IsZero() && ended.After(began) {
					f.took = ended.Sub(began)
				}
				out[f.start] = f
			}
		}
		lo = hi
	}
	return out
}

func workIndent(width int) string {
	if layoutTier(width) == tierPhone {
		return ""
	}
	return strings.Repeat(" ", spacingConversationLead)
}

// workIndentCols is what the indent law costs, in columns. It is asked at
// LAYOUT and again at the pass that applies it (render.go's [app.deckRows]), and
// it is one function because those two must never be able to disagree: a block
// laid out at the full width and then shoved two cells right is a block two
// cells wider than the column it is drawn in, and the two cells it overhangs are
// cut off by [app.railJoin] — which is where a tool row's spinner went.
func workIndentCols(width int) int { return ansi.StringWidth(workIndent(width)) }

// workEntry reports whether the entry at i is WORK — the half of [rowIsWork]
// that can be answered before a single row has been built, so the width a block
// is laid out at and the indent it is later given are decided by one rule.
func workEntry(es []entry, folds map[int]workfold, i int) bool {
	if i < 0 || i >= len(es) {
		return false
	}
	e := es[i]
	if e.kind == entryThinking || e.kind == entryTool || e.kind == entryCompact || e.kind == entryNote {
		return true
	}
	if e.kind != entryAssistant {
		return false
	}
	// AN INTERRUPTED TURN PROMOTES NOTHING (hierarchy.go). It is asked first and
	// asked of the BLOCK rather than of the fold because a room derives no folds
	// at all (A ROOM FOLDS NOTHING, above) and the law is not the conversation's:
	// a turn that was stopped reached no answer on any page that draws it.
	if e.cut {
		return true
	}
	for _, f := range folds {
		if f.turn == e.turn {
			return i < f.answer
		}
	}
	// During a live turn there is no completed fold yet, so the same question is
	// asked of the list directly: IS THERE MORE WORK AFTER THIS BLOCK BEFORE THE
	// PERSON SPEAKS AGAIN. The walk stops at the next of their messages for
	// [deriveWorkfolds]'s reason above — a steer does not open a turn, and prose
	// answering the question before it was never narration for the one after it —
	// and it steps over a divider, which is a line about the session rather than
	// a step in it.
	for at := i + 1; at < len(es) && es[at].turn == e.turn; at++ {
		if groupBreaks(&es[at]) {
			return false
		}
		if es[at].kind != entryDivider && !entryWithdrawn(&es[at]) {
			return true
		}
	}
	return false
}

// workfoldLabel is the chip's line: WHAT HAPPENED, COUNTED, AND NOTHING ELSE.
//
// THE FACTS ARE STATED AND NEVER JUDGED. There is no ✓ and no "success" on a
// finished turn — only failure speaks on this surface, and a chip that congratulated
// itself would be spending the reader's attention on the one outcome they can
// already see, since the answer is sitting under it. A turn the person stopped
// says so in the person's own terms — "stopped by you", not "aborted", not
// "cancelled", not "incomplete" — because they are the one who did it and they
// know why; the line exists to say where the missing answer went, not to grade the
// turn.
func (a *app) workfoldLabel(f workfold) string {
	took := f.took
	if stamp, ok := a.stamps[f.turn]; ok {
		took = stamp.took
	}
	parts := []string{"▸ worked"}
	if f.stopped {
		parts[0] = "▸ stopped by you"
		if word := tookWord(took); word != "" {
			parts[0] += " at " + word
		}
	} else if word := tookWord(took); word != "" {
		parts[0] += " " + word
	}
	if word := tookWord(f.thought); word != "" {
		parts = append(parts, "thought "+word)
	}
	if f.tools > 0 {
		word := " tool call"
		if f.tools != 1 {
			word += "s"
		}
		parts = append(parts, itoa(f.tools)+word)
	}
	parts = append(parts, "ctrl+e")
	return strings.Join(parts, " · ")
}

// groupBreaks reports whether this block ENDS a fold group — whether it is one
// of the person's own messages.
//
// THE ONE THING ON THIS SURFACE A FOLD MAY NEVER HIDE IS THE PERSON'S OWN
// WORDS, and a turn can hold more than one of them: a message sent into a turn
// that is already streaming does not open a new turn (app.go's
// [app.submittingShown] — steering is not a second turn), so a turn number alone
// can span a question and every correction made to it. The chip hides the
// machinery BETWEEN a question and its answer, so a run with a correction in the
// middle of it would fold that correction away.
//
// A WITHDRAWN correction breaks nothing, because it draws nothing: a group
// ended at an invisible row would leave the work above it unfoldable for a
// reason nobody can see (steerelbow.go).
func groupBreaks(e *entry) bool {
	return e.kind == entryUser || (e.kind == entrySteer && e.steer != nil)
}

func rowIsWork(r row, es []entry, folds map[int]workfold) bool {
	if r.text == "" {
		return false
	}
	if r.hit == hitWorkFold || r.hit == hitFold || r.hit == hitTool || r.hit == hitMore {
		return true
	}
	return workEntry(es, folds, r.entry)
}

func (a *app) toggleLatestWorkfold() bool {
	d := a.bodyDeck()
	folds := a.deckFolds(d)
	latest := -1
	for _, f := range folds {
		if f.turn > latest {
			latest = f.turn
		}
	}
	if latest < 0 {
		return false
	}
	if d.workOpen == nil {
		if a.room != nil {
			a.room.workOpen = make(map[int]bool)
			d.workOpen = a.room.workOpen
		} else {
			a.workOpen = make(map[int]bool)
			d.workOpen = a.workOpen
		}
	}
	d.workOpen[latest] = !d.workOpen[latest]
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
	return true
}

func (a *app) toggleWorkfold(turn int) {
	d := a.bodyDeck()
	if d.workOpen == nil {
		if a.room != nil {
			a.room.workOpen = make(map[int]bool)
			d.workOpen = a.room.workOpen
		} else {
			a.workOpen = make(map[int]bool)
			d.workOpen = a.workOpen
		}
	}
	d.workOpen[turn] = !d.workOpen[turn]
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}
