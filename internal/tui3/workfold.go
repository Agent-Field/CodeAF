package tui3

import (
	"strings"
	"time"
)

// workfold is render-time structure. Nothing here is journaled: replaying the
// same entries derives the same fold, while a person's expansion dies with the
// window that owns it.
type workfold struct {
	turn, start, answer int
	tools               int
	thought             time.Duration
	took                time.Duration
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
func deriveWorkfolds(es []entry, runningTurn int) map[int]workfold {
	out := make(map[int]workfold)
	for lo := 0; lo < len(es); {
		hi := lo + 1
		for hi < len(es) && es[hi].turn == es[lo].turn {
			hi++
		}
		answer := -1
		blocked := false
		for i := lo; i < hi; i++ {
			if es[i].kind == entryAssistant && strings.TrimSpace(es[i].text) != "" {
				answer = i
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
		if answer >= 0 && es[answer].settled && !blocked && es[lo].turn != runningTurn {
			f := workfold{turn: es[lo].turn, start: -1, answer: answer}
			var began, ended time.Time
			for i := lo; i < answer; i++ {
				e := &es[i]
				work := e.kind != entryUser && e.kind != entryDivider
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
	return "  "
}

func (a *app) workfoldLabel(f workfold) string {
	took := f.took
	if stamp, ok := a.stamps[f.turn]; ok {
		took = stamp.took
	}
	parts := []string{"▸ worked"}
	if word := tookWord(took); word != "" {
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

func rowIsWork(r row, es []entry, folds map[int]workfold) bool {
	if r.text == "" {
		return false
	}
	if r.hit == hitWorkFold || r.hit == hitFold || r.hit == hitTool || r.hit == hitMore {
		return true
	}
	if r.entry < 0 || r.entry >= len(es) {
		return false
	}
	e := es[r.entry]
	if e.kind == entryThinking || e.kind == entryTool || e.kind == entryCompact || e.kind == entryNote {
		return true
	}
	if e.kind == entryAssistant {
		for _, f := range folds {
			if f.turn == e.turn {
				return r.entry < f.answer
			}
		}
		// During a live turn there is no completed fold yet. Its last assistant
		// block is the answer; any earlier one has been superseded by later work.
		for i := len(es) - 1; i > r.entry; i-- {
			if es[i].turn == e.turn && es[i].kind != entryDivider {
				return true
			}
		}
	}
	return false
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
