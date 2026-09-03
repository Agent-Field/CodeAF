package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// workfold is render-time structure. Nothing here is journaled: replaying the
// same entries derives the same fold, while a person's expansion dies with the
// window that owns it.
type workfold struct {
	// key is THE CHIP'S OWN NAME — what [deck.workOpen] is keyed by, what the
	// chip's row carries, and what a click and `ctrl+e` name when they open it.
	//
	// It is not the turn, and the difference is what makes a room's chips
	// separable. Out in the conversation one turn holds at most one chip, so the
	// turn number named it and nothing was lost. A room's page is ONE turn
	// holding a chip per settled phase (see [derivePhaseFolds]), and a key that
	// was the turn would have made every chip on the page one control: opening
	// the second would open the first, the fourth and the tenth. The
	// conversation keeps the turn as its key, so nothing about it changed.
	key                 int
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

// deckFolds is the chips ONE PAGE draws, resolved through the lens's fold style
// (lens.go's [folders]).
//
// ── THE LAW THIS USED TO STATE, AND THE RULING THAT REVERSED IT ─────────────
//
// ~~A ROOM FOLDS NOTHING. The chip is an affordance of the conversation and it
// earns its place there. A turn out in the thread is a question somebody asked
// and the answer they were given, and the machinery between the two is work
// they delegated precisely so they would not have to watch it — so it
// collapses, and the page reads back as the exchange it was. A room is the
// opposite errand. It is the page somebody opened BECAUSE they want to read the
// machinery, and a node's whole life is one long turn with a report at the end
// of it — so the same rule swallowed the entire page the instant the node
// stopped running, leaving `▸ worked · 10 tool calls · ctrl+e` and the report
// under it. It is stated as an absence of folds rather than as a fold that
// opens itself, because [deck.workOpen] is the READER'S own answer and a
// default that had to be inverted for one kind of page would give that map two
// meanings.~~
//
//	> RULED BY THE OWNER, 2026-09-01 (issue #252, ruling 1): FOLD THE PAST.
//	> "The task page folds settled work into phase chips by default; the
//	> machinery stays one keypress away (ctrl+e / scroll-up). This reverses
//	> the written law at workfold.go:26-52 … and the three tests pinning it."
//
// THE ARGUMENT THE RULING ENCODES, because the struck text above is a good
// argument for the wrong page. It optimises the RARE visit — the audit — at the
// cost of the common one. A person goes to a task to steer and to check, not to
// read a transcript, and a page built on the premise that every call must be
// read is the industry's linear machinery scroll reproduced one level down. The
// reversal was possible without giving up the audit because the old law's own
// objection — a chip per TURN swallows a page that is one turn — is answered by
// folding per PHASE instead ([derivePhaseFolds]): the machinery collapses, every
// paragraph the node wrote stays standing, and the live frontier never folds at
// all. And [deck.workOpen] keeps its one meaning: the chips still default shut
// and the reader's expansion is still the reader's.
//
// THE DOORS, all three, because DISCOVERABILITY BEFORE PURITY: `ctrl+e` opens
// the newest chip ([app.toggleLatestWorkfold]), a click opens any of them
// (app.go's [app.press]), and a scroll up at the top of a room opens the one
// nearest the top (room.go's [app.roomUnfoldAtTop]) — which is what keeps the
// disclosure ladder from dead-ending (docs/THREAD-UX.md).
func (a *app) deckFolds(d deck) map[int]workfold {
	fold, ok := folders[d.lens.foldPast]
	if !ok {
		return nil
	}
	return fold(d)
}

// ── A PHASE IS SETTLED WORK WITH A SETTLED PARAGRAPH AFTER IT ───────────────
//
// derivePhaseFolds is the room's chips. A PHASE is the settled work — thought,
// calls, compaction, the surface's own notes — that precedes a settled block of
// the node's prose. The chip hides that machinery and leaves the prose standing,
// which is exactly what the conversation's chip does with an answer; the only
// thing that changed is what counts as the end of one, because a node writes a
// paragraph every few steps and asks a question once.
//
// THE LIVE FRONTIER NEVER FOLDS. Everything after the last settled paragraph is
// what the node is doing NOW, and the person watching now is the one reader for
// whom the machinery is the content — so it is left whole, at the room's own
// whole-screenful tool tail (lens.go's [lens.toolTail]). It falls out of the
// walk rather than being tested for: a run with no settled paragraph after it
// never closes, so no chip is ever minted over it.
//
// WHAT NEVER FOLDS, AND WHY EACH ONE. A run carrying any of these keeps every
// row it has, exactly as the conversation's `blocked` runs do:
//
//   - THE PERSON'S OWN WORDS — the brief, and every correction they typed into
//     running work. A FOLD MAY NEVER HIDE THE PERSON'S WORDS ([groupBreaks]),
//     and an elbow ends a run for the same reason it ends one out in the thread.
//     The brief keeps its own three-lines-and-a-door instead (brieffold.go).
//   - A FAILED CALL. ONLY FAILURE SPEAKS on this surface, so the one row that
//     was allowed to raise its voice may not then be filed away by a chip.
//   - AN ASK — a consent question, a task proposal, a standing card. It is a
//     thing the work could not decide alone, and the record of what the person
//     answered is the only account of where the next hour came from.
//   - A CALL STILL IN FLIGHT, which is not settled work and therefore not part
//     of a settled phase at all.
//   - A SEAM, for [deriveWorkfolds]'s reason: a fold that swallowed one would be
//     claiming the page above it is the same unbroken page.
//
// THE FINAL REPORT STANDS BY CONSTRUCTION. It is the last settled paragraph, so
// it is the block the last chip stops at rather than a case anything tests for.
//
// Nothing here is journaled and nothing is summarised: a chip states counted
// facts about the rows it covers, and a phrase that paraphrased the work would
// be a second account of it that can drift from the work
// ([app.workfoldLabel]).
func derivePhaseFolds(es []entry) map[int]workfold {
	out := make(map[int]workfold)
	phase := phaseRun{start: -1}
	for i := range es {
		e := &es[i]
		switch {
		case groupBreaks(e) || phaseKeeps(e):
			// The run is abandoned, not emitted: whatever it held, this row is
			// something a chip may not cover, and a chip that stopped short of it
			// would be a fold whose reason nobody can see.
			phase = phaseRun{start: -1, key: phase.key}
		case e.kind == entryAssistant && e.settled && strings.TrimSpace(e.text) != "":
			if f, ok := phase.close(es, i); ok {
				out[f.start] = f
			}
			phase = phaseRun{start: -1, key: phase.key}
		default:
			phase.open(i)
		}
	}
	return out
}

// phaseRun is the run of rows [derivePhaseFolds] is currently inside: where it
// began, and how many chips have been minted before it. It is a type rather
// than four locals so that the walk above reads as three cases and no
// bookkeeping.
type phaseRun struct {
	start, key int
}

func (p *phaseRun) open(i int) {
	if p.start < 0 {
		p.start = i
	}
}

// close mints the chip for a run that just reached a settled paragraph, and
// reports whether there was a run to mint one for. An empty run — a paragraph
// straight after a paragraph — is no phase at all and gets no chip, which is the
// emptiness law said about a fold.
func (p *phaseRun) close(es []entry, answer int) (workfold, bool) {
	if p.start < 0 || p.start >= answer {
		return workfold{}, false
	}
	f := workfold{turn: es[p.start].turn, answer: answer}
	// A RUN THAT COUNTED NOTHING IS NOT A PHASE. [countWork] steps over the rows
	// a fold may not measure, so a run of nothing but dividers leaves no start
	// behind — and a chip over no work is a chip that hides nothing and offers a
	// door onto it.
	if countWork(es, p.start, answer, &f); f.start < 0 {
		return workfold{}, false
	}
	p.key++
	f.key = p.key
	return f, true
}

// phaseKeeps reports whether this row is one a chip may never cover. The list is
// the argument in [derivePhaseFolds]'s comment, said once, as a table of
// predicates rather than a condition spelled into the walk.
func phaseKeeps(e *entry) bool {
	switch e.kind {
	case entryTask, entryStanding, entryConnect, entrySeam, entryHarness, entryDone:
		return true
	case entryTool:
		return e.status != toolOK
	}
	return false
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
			// THE CONVERSATION KEYS ITS CHIPS BY THE TURN, which is what
			// [deck.workOpen], [app.stamps] and every gesture out here already
			// name (see [workfold.key]). One turn, one chip: nothing to separate.
			f := workfold{key: es[lo].turn, turn: es[lo].turn, start: -1, answer: end, stopped: stopped}
			if countWork(es, lo, end, &f); f.start >= 0 {
				out[f.start] = f
			}
		}
		lo = hi
	}
	return out
}

// countWork fills in WHAT A CHIP COUNTS over es[from:to] — where the work it
// covers begins, how many calls it made, how long it thought, and how long the
// whole of it took.
//
// It is one function because both fold styles state the same facts in the same
// grammar, and a chip that counted differently on two pages would be the same
// sentence meaning two things. THE PERSON'S OWN ROWS ARE NOT WORK and never
// start a chip: a question, a divider and an elbow are all things a fold stops
// at rather than things it measures.
func countWork(es []entry, from, to int, f *workfold) {
	f.start = -1
	var began, ended time.Time
	for i := from; i < to; i++ {
		e := &es[i]
		if e.kind == entryUser || e.kind == entryDivider || e.kind == entrySteer {
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
	if !began.IsZero() && ended.After(began) {
		f.took = ended.Sub(began)
	}
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
	// A BLOCK INSIDE A CHIP IS WORK AND THE BLOCK A CHIP STOPS AT IS THE ANSWER,
	// and both are asked BY POSITION rather than by turn. The turn was enough
	// while one turn held one chip; a room's page is one turn holding a chip per
	// settled phase ([derivePhaseFolds]), and a walk that stopped at the first
	// fold of this turn would be reading a map in whatever order Go handed it —
	// the second phase's narration answered by the first phase's boundary. The
	// two tests are disjoint by construction, so the answer does not depend on
	// that order; out in the conversation, where a turn has one chip, they give
	// exactly what the turn test gave.
	for _, f := range folds {
		if f.turn != e.turn {
			continue
		}
		if i >= f.start && i < f.answer {
			return true
		}
		if i == f.answer {
			return false
		}
	}
	// During a live turn there is no completed fold yet, so the same question is
	// asked of the list directly: IS THERE MORE WORK AFTER THIS BLOCK BEFORE THE
	// PERSON SPEAKS AGAIN. The walk stops at the next of their messages for
	// [deriveWorkfolds]'s reason above — a steer does not open a turn, and prose
	// answering the question before it was never narration for the one after it —
	// and it steps over a divider, which is a line about the session rather than
	// a step in it.
	//
	// AND IT STEPS OVER A NOTE, WHICH IS THE SURFACE TALKING AND NOT WORK THE
	// ANSWER WAS WAITING FOR (#178). The two notes a turn ends with — what it
	// changed and what it cost — are written at the boundary and land UNDER the
	// reply on purpose, so they are about the answer rather than after it
	// (app.go's EventTurnDone). A walk that counted them read "there is more after
	// this block" and demoted the answer itself, which is drawn plain: heading,
	// bold and whole table came back as the characters they were typed as, two
	// columns into the work column.
	//
	// IT ONLY EVER BIT WHERE THIS WALK IS THE CLASSIFIER, and that is the narrow
	// part worth writing down. A turn that derives a fold is answered by the loop
	// above instead — `i < f.answer`, and [deriveWorkfolds] already picks the
	// answer as the group's last assistant block, so a trailing note is outside
	// the chip and cannot reach it. The walk is reached when NO fold is derived:
	// a group that is `blocked` (a task, connect or standing card in the turn, a
	// `cancel…` note, a seam), a group with no work at all before its answer
	// (no thought, no call), and every room, which folds nothing at all. The
	// reproduction was the first of those — a turn carrying a task proposal, then
	// a markdown answer, then its own `⟲ … cached` line — measured against a real
	// model on this branch's parent.
	for at := i + 1; at < len(es) && es[at].turn == e.turn; at++ {
		if groupBreaks(&es[at]) {
			return false
		}
		if es[at].kind != entryDivider && es[at].kind != entryNote && !entryWithdrawn(&es[at]) {
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
func (a *app) workfoldLabel(d deck, f workfold) string {
	took := f.took
	// THE SESSION'S RECEIPTS ARE THE SESSION'S, and only a page that runs the
	// session's clock may read them (lens.go's [receiptsInline]). [app.stamps] is
	// keyed by the CONVERSATION's turn numbers; a room numbers its own turns from
	// one, so a chip in there that consulted the map would quote the time the
	// conversation's first turn took as the time this node's first phase took —
	// a figure about somebody else's work, said with confidence.
	if d.lens.receipts == receiptsInline {
		if stamp, ok := a.stamps[f.turn]; ok {
			took = stamp.took
		}
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
		// ONE SPELLING OF THIS NUMBER, and it is timestamps.go's
		// ([toolCallWord]) — the receipt six rows under this chip counts the same
		// calls and used to spell them differently.
		parts = append(parts, toolCallWord(f.tools))
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
	latest, found := 0, false
	for _, f := range a.deckFolds(d) {
		if !found || f.key > latest {
			latest, found = f.key, true
		}
	}
	if !found {
		return false
	}
	a.setWorkOpen(d, latest, !d.workOpen[latest])
	return true
}

// toggleWorkfold is a press on one chip: the click's door, and the one the
// keyboard's own gesture resolves to.
func (a *app) toggleWorkfold(key int) {
	d := a.bodyDeck()
	a.setWorkOpen(d, key, !d.workOpen[key])
}

// workFoldOpen reports whether one chip is SHOWING ITS WORK: because the reader
// opened it, or because `ui.work = open` opened every chip on the surface.
//
// It is one function because two places ask it — the pass that draws the rows
// and the scroll that looks for a chip still worth opening (room.go's
// [app.roomFoldDoor]) — and a gesture that disagreed with the screen about
// which chips were shut would spend itself on one that was already open.
func (a *app) workFoldOpen(d deck, key int) bool {
	return a.workMode == config.WorkOpen || d.workOpen[key]
}

// openWorkfold OPENS one chip and never closes it. It is the door a SCROLL takes
// (room.go's [app.roomUnfoldAtTop]): a person reading history upward is asking
// for more of it at every tick, and a gesture that closed the chip it had just
// opened would make the wheel a switch.
func (a *app) openWorkfold(key int) { a.setWorkOpen(a.bodyDeck(), key, true) }

// setWorkOpen writes the reader's answer about one chip, minting the deck's map
// where the page has not needed one yet.
//
// It is ONE function because the map lives on whichever list is being drawn and
// the three doors above must not each carry their own copy of that reasoning:
// the deck is a VIEW, so a map minted here has to be minted on the object the
// deck was taken from or the next frame reads a map nobody wrote to.
func (a *app) setWorkOpen(d deck, key int, open bool) {
	if d.workOpen == nil {
		d.workOpen = make(map[int]bool)
		if a.room != nil {
			a.room.workOpen = d.workOpen
		} else {
			a.workOpen = d.workOpen
		}
	}
	d.workOpen[key] = open
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}
