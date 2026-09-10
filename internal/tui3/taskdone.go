package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// WORK COMING HOME IS AN EVENT, AND IT GETS A CARD.
//
// What stood here was one dim line — "task Fix the nil-map crash done in 2m 10s
// · merged" — wedged into the transcript between two paragraphs, in the same
// grey the surface uses to mutter about its own housekeeping. That line is
// wrong in proportion to how much it is worth: it is the END of something a
// person delegated ten minutes ago, it is the only place the outcome is ever
// stated, and it was drawn quieter than the tool call that read a file.
//
//	✓ ◆ Fix nil-map crash · done · 4m12s · 3 files
//	  "the guard is in and the regression test passes" · started 14:02 · ctrl+o output
//
// TWO ROWS, AND THE SECOND IS THE ONE THAT PAYS. The head is what happened; the
// muted line under it is what came of it, in the node's own first sentence,
// with the two facts that let a person go back to it — when it started, and the
// key that opens what it actually did.
//
// It is the same card in three states, which is the whole of decision 4: a
// proposal collapsed is a title and a subtitle (task.go), a landed node
// collapsed is a title and an outcome, and BOTH open onto the full context
// behind the same two gestures. A person who has learned that a card has more
// inside it has learned it once.
//
// ── A BATCH IS ONE OBJECT ──
//
// Three nodes finishing within a second of each other used to write three
// lines, each repeating the word "task", and between them they said one thing:
// the batch is home. So a run of more than two lands as a ROLLUP — a header
// that counts them, a compact row each, and the outcome sentence on the most
// recent one only. The rest of the outcomes are one keystroke away on their own
// rows, which is where they were going to be read anyway.

// taskDone is one landed node, as the transcript keeps it.
//
// It is a snapshot rather than a pointer to the rail's node, and the reason is
// the rail: a node LEAVES the rail when its work comes home ([taskNode.
// resident]), and a card that read through to a live struct would be a record
// of history pointing at a thing designed to be forgotten.
type taskDone struct {
	id    uint64
	ident taskIdent
	// title and subtitle are the identity (taskident.go), frozen at landing.
	title, subtitle string
	// status is THE READING, taken once at landing from the node's own facts
	// through [session.ProjectTask] and never worked out again here.
	//
	// IT REPLACED THREE BOOLEANS AND FOUR PRIVATE WORDS. This card used to carry
	// `failed`, `unverified` and a delivery predicate of its own, and each of them
	// picked its own vocabulary out of a switch — so one landing was `failed` on
	// the card, `incomplete` on the record page and `stopped` on the roster. The
	// tier is the glyph, [session.TaskStatus.Word] is the word, and
	// [session.TaskStatus.Ask] is the question and its two answers
	// (docs/design/task-states/DESIGN.md).
	status session.TaskStatus
	// span is the node's own age at its final state. started is the record's start
	// or the live surface's established fallback, and landed is the record's
	// settling instant; the surface clock is the last resort only for a landing
	// this window actually watched. They are kept because "how long" and "when"
	// are different questions and the second one is what a person matches against
	// their own memory of the afternoon.
	span            time.Duration
	started, landed time.Time
	outcome, report string
	// result is WHAT THE WORK PRODUCED and report above is the LANDING'S OWN
	// STORY about it, and the whole hierarchy inside an open card rests on the
	// two being different things ([taskDone.answerAndAccount]).
	//
	// The engine keeps them apart already (internal/session's task_result.go) and
	// this card had been drawing only the report — so an accepted landing over a
	// refused merge put the merge refusal, the sentence saying somebody took it
	// as done, and the answer itself into one grey block in the order they were
	// composed, with the thing the person delegated the work FOR at the bottom.
	//
	// resultWhole is where the whole of a cut answer can be read, resultCut says
	// this is only the beginning of it, and resultHeld is the landing that turned
	// the work back: no body at all, and a pointer to where it is.
	result, resultWhole   string
	resultCut, resultHeld bool
	changed               []string
	added, removed        int
	branch, merge         string
	brief, acceptance     string
	// rung is which copy of the ground the work happened in and mode what was
	// promised about it, frozen off the node at landing with everything else on
	// this card (session's TaskNotice.Rung). They are here so the row naming the
	// branch can be labelled with the place that branch came home FROM, in the
	// engine's own words ([app.doneGroundLabel]) — a card that spelled its own
	// would be a second answer to a question the record card answers next week.
	rung session.GroundRung
	mode session.TaskMode
	// model is whose hands did the work, frozen with the rest of it. It is
	// inside the card rather than on its head for the reason the working copy is:
	// the head is what happened, and this is a fact somebody opens the card to
	// check.
	model string
	// cost is what the work came to in dollars, frozen at landing from the
	// node's own reconciled figure ([taskNode.spent]) rather than from the last
	// notice: the rail spent this node's whole life keeping the engine's
	// published price and the pilot's running sum in one number, and a card that
	// read the notice directly would print a figure the column beside it had
	// already corrected.
	//
	// ZERO IS NOT "IT COST NOTHING" — it is nobody published a price (session's
	// task_contract.go on CostUSD), so the row is absent rather than $0.00. It
	// sits beside the model for the same reason the model sits inside the card:
	// whose hands, and what the hands came to, are the two facts a person opens
	// a landed card to check.
	cost float64
	// open says the full context is showing, behind the same expand mechanic
	// every other card on this surface is behind.
	open bool
	// gut is how many columns of the READING GUTTER this card's rows already
	// carry (gutter.go), for the reason the proposal's own spans carry one
	// (task.go's [taskCard]).
	gut int
}

// The card's words.
//
// THE STATE WORDS ARE NOT HERE ANY MORE. `done`, `stopped`, `incomplete` and
// `your call` are [session.TaskStatus.Word], spelled once in internal/session's
// task_status.go and read off the reading — because a state with two spellings
// is two states to whoever is reading it, and this card used to hold four of its
// own (docs/design/task-states/DESIGN.md).
const (
	// doneWord is the record page's own reading of a landed row (taskstatus.go)
	// and no longer this card's head: the head takes its word from the reading.
	doneWord      = "done"
	doneOutputKey = "ctrl+o output"
	// doneStartWord IS THE WORD FOR WHEN THE WORK BEGAN, and it said `spawned`
	// until this wave. That is the machinery's own verb for starting a process,
	// which this house bans in anything a person reads — the same rule that took
	// `worktree` off [doneBranchLabel] eight lines below, on 2026-09-01. `started`
	// is what a person calls it, and the stamp beside it is the node's own start
	// or nothing at all ([taskNode.spawnedAt] reads a restored node's recorded
	// start and answers the zero time when its older record carried none, which
	// the emptiness law draws as nothing).
	doneStartWord = "started "
	// doneFileSuffix and doneFilesSuffix are the changed-file count. Singular
	// and plural are both spelled because "1 files" is the surface being sloppy
	// in the one row a person reads to decide whether to look.
	doneFileSuffix  = " file"
	doneFilesSuffix = " files"
	// doneRollupWord counts a batch. It is the only row on this surface that
	// says the word "tasks" in the plural, which is what makes it read as a
	// header rather than as another task.
	doneRollupWord = " tasks done"
	doneRollupMix  = " tasks landed"
	// The labels the expansion hangs its facts on. They are nouns and they are
	// aligned by nothing: a two-column table of six short facts is furniture
	// around a paragraph.
	doneChangedLabel = "changed · "
	// doneBranchLabel is the LAST RESORT for the row naming the branch the work
	// was left on, and it is used only when nothing knows what the working copy
	// was. The label that row usually wears is the ground ladder's word for the
	// rung that made the node's world ([app.doneGroundLabel]).
	//
	// IT SAID `worktree` UNTIL 2026-09-01, and that was wrong in both directions
	// at once: it is the machinery's own vocabulary, which this house bans in
	// anything a person reads, and it named a mechanism nobody had checked was in
	// use — a repository task is grounded in a fork whenever furrow can make one
	// (internal/session's groundladder.go), and the row was telling people their
	// work had been in a git worktree it never went near.
	doneBranchLabel = "branch · "
	doneAcceptLabel = "done when · "
	doneBriefLabel  = "brief · "
	doneSpanLabel   = "ran · "
	doneModelLabel  = "model · "
	// THE PRICE WEARS NO LABEL AND IT USED TO WEAR `cost · `. Whose hands, what
	// they came to and how long they took are one row now rather than three
	// ([app.doneFactsRow]), and on one row `$0.75` is the only thing on this
	// surface that can be a dollar figure — a noun in front of it is a cell spent
	// saying what the glyph already said, on the row that has to stay under one
	// line at sixty columns.
	//
	// doneMoreAt and doneMoreGone are the pointer under an answer this card was
	// handed only the beginning of — internal/session's own facts
	// (task_result.go's Result, ResultCut, ResultWhole) said in this surface's
	// words.
	//
	// THE SENTENCE ABOUT A HELD ANSWER IS GONE FROM HERE. It read `what it
	// produced was not taken as done`, which is a second account of the state and
	// is deleted as person-facing text: the card's own reason row already says
	// `the check did not pass it: <gaps>` in the engine's spelling, and all this
	// block owes a person after that is where the answer can be read.
	doneMoreAt   = "the whole of it is at "
	doneMoreGone = "the rest of it was not kept"
)

// doneWindow caps EVERY long field inside an open card — the answer, the tail of
// the landing's own account, what it was done when, and the brief. Twenty rows
// is about a screen; past it a person is reading a document in a transcript, and
// the room (room.go) is where a node's whole life is.
//
// The cap is per FIELD and not per card, which is what keeps a landing whose
// account ran long from pushing the answer off the bottom: each block is bounded
// where it is drawn, and the marker says which one was cut.
const doneWindow = 20

// ── landing ─────────────────────────────────────────────────────────────────

// landedCard is what a node writes into the conversation when it comes home. It
// replaces the one-line note this surface used to leave, and it takes a blank
// row on both sides for the reason that note did: a block about work that
// started rows ago, wedged against the next paragraph, reads as part of it
// (render.go's [app.layout] emits every gap on this surface).
func (a *app) landedCard(node *taskNode) {
	title := taskTitleOf(node.label, node.assignment, node.id)
	landed := node.ended
	if landed.IsZero() && !node.restored {
		landed = a.now()
	}
	card := &taskDone{
		id:          node.id,
		ident:       node.ident,
		title:       title,
		subtitle:    taskSubtitleOf(title, node.assignment),
		status:      session.ProjectTask(doneNodeFacts(node)),
		span:        node.elapsed,
		started:     node.spawnedAt(),
		landed:      landed,
		outcome:     strings.TrimSpace(firstLine(node.report)),
		report:      strings.TrimSpace(node.report),
		result:      strings.TrimSpace(node.produced),
		resultWhole: strings.TrimSpace(node.producedWhole),
		resultCut:   node.producedCut,
		resultHeld:  node.producedHeld,
		changed:     node.changed,
		branch:      node.branch,
		merge:       node.merge,
		rung:        node.rung,
		mode:        node.mode,
		brief:       node.brief,
		acceptance:  node.acceptance,
		model:       node.model,
		cost:        node.spent(),
	}
	if card.span == 0 && !node.began.IsZero() {
		card.span = a.now().Sub(node.began)
	}
	// A LANDING THAT SAID NOTHING ABOUT ITSELF SAYS NOTHING. The two glosses this
	// surface used to write into an empty outcome — `stopped`, `finished, but
	// needs your look` — were the card telling a person the state twice, in its own
	// second vocabulary, on the one row that exists to carry the node's own words.
	// The head already says the state and the reason row already says why; the
	// emptiness law does the rest.
	//
	// THE FLOOR HANDS A CARD BACK RATHER THAN WRITING A SECOND ONE. A turn that
	// ended with the model still holding a question publishes an ordinary update
	// whose only news is who is deciding (session's agent.go), so the card that is
	// already on the page picks up its chips instead of a duplicate landing below
	// it (see [app.handedBackCard]).
	if a.handedBackCard(card) {
		return
	}
	// A card lands in the middle of whatever the model was saying, exactly as a
	// note did: the streaming block is closed first so the card is a block of its
	// own rather than a paragraph inside the reply (app.go's [feed.note]).
	a.closeLive()
	a.entries = append(a.entries, entry{kind: entryDone, turn: a.turn, done: card})
	a.follow()
	a.touch()
}

// doneNodeFacts is one landing as [session.ProjectTask] takes it.
//
// IT IS A LANDING'S FACTS AND NOT A ROW'S, which is why it is here rather than
// beside the roster's own reading (taskstatus.go): a card asks four things of a
// node the column never needs — the landing's own report, whether the check held
// what it produced, which files clashed, and who is holding the decision right
// now — and every one of them is what turns `your call` into a question with two
// answers on it. The prerequisites go the other way: a node that has landed is
// waiting on nothing, so no titles are looked up.
func doneNodeFacts(node *taskNode) session.TaskFacts {
	return session.TaskFacts{
		State:      node.state,
		Ending:     node.ending,
		Life:       node.phase,
		Kind:       node.kind,
		Phase:      node.doing,
		Gap:        node.mending,
		Hold:       node.waiting,
		Paused:     node.paused,
		Stopped:    node.stopped,
		Merge:      node.merge,
		Branch:     node.branch,
		Report:     strings.TrimSpace(node.report),
		Held:       node.producedHeld,
		Conflicts:  node.conflicts,
		Shifted:    node.shifted,
		GroundHeld: node.groundHeld,
		Decider:    node.decider,
		Liveness:   session.TaskLivenessHeld,
	}
}

// handedBackCard is the floor putting one decision back in the person's hands,
// and it reports whether the card already on the page took the news.
//
// A TASK NEVER STAYS UNOWNED PAST THE END OF A TURN (the design's own law). When
// the model's turn ends with a question it never answered, the engine publishes
// an ordinary update whose only change is [session.TaskNotice.Decider] — the
// state, the branch, the report and the span are all exactly what they were. So
// this is not a landing: writing a second card for it would put two accounts of
// one piece of work in the transcript, the older of them still saying somebody
// else was deciding.
//
// AND IT IS EVERY RE-SETTLE OF A NODE THAT HAS NOT MOVED STATE, not only the
// hand-back. A landing the model accepted whose merge was then REFUSED comes
// back as a fresh notice for the same id in the same state, asking a different
// question — `your folder already has files the task wrote` where it said
// `nobody could check it` — and the card that stayed frozen in the first
// landing's shape was the reason a person read a reason that was not the one
// they were being asked about (#767). So the reading is replaced wherever the
// state has not moved; a node that actually settled lands its own card below,
// which is the design's own rule and the reason the head is never rewritten.
func (a *app) handedBackCard(fresh *taskDone) bool {
	card := a.doneCardFor(fresh.id)
	if card == nil || card.status.State != fresh.status.State {
		return false
	}
	card.status = fresh.status
	a.settleTouched(card)
	return true
}

// doneCardAt is the card one entry draws, or nil.
func (a *app) doneCardAt(i int) *taskDone {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryDone {
		return nil
	}
	return a.entries[i].done
}

// toggleDoneAt opens or closes one card's full context. It is the CLICK, and
// the whole card is the target for the reason the whole of a proposal is
// (task.go): a card is a paragraph, and asking somebody to hit its second row
// is asking them to aim.
func (a *app) toggleDoneAt(i int) {
	card := a.doneCardAt(i)
	if card == nil {
		return
	}
	card.open = !card.open
	a.touch()
}

// openDone is ctrl+o on a SELECTED card, and it reports whether it took the
// key.
//
// The key is the one the card itself names, which is why it is spent here: the
// muted line says "ctrl+o output", and a surface that printed a key and then
// did something else with it would have lied in the only place it explained
// itself. With nothing selected — the ordinary case — this answers false and
// the key stays the tool cluster's fold (input.go), which is what it has always
// been.
func (a *app) openDone(i int) bool {
	card := a.doneCardAt(i)
	if card == nil {
		return false
	}
	card.open = !card.open
	a.touch()
	return true
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// doneCluster lays out one contiguous run of landed cards and appends it to
// out. It is [app.clusterRows]'s shape for the same reason that function has
// it: whether a batch rolls up is a property of the RUN and not of any card in
// it.
func (a *app) doneCluster(d deck, out []row, from, to, width int) []row {
	if to-from > doneRollupFloor {
		return a.rollupRows(d, out, from, to, width)
	}
	for i := from; i < to; i++ {
		card := d.entries[i].done
		if card == nil {
			continue
		}
		for _, text := range a.doneRows(card, width, a.selected(i)) {
			out = append(out, row{text: text, entry: i, hit: hitDone})
		}
	}
	return out
}

// doneRollupFloor is how many cards a batch has to have before it becomes one
// object. Two full cards are four rows and read as two events, which is what
// they are; three are six rows saying the word "task" three times.
const doneRollupFloor = 2

// doneRows is one card whole: the head, the outcome line, and the full context
// when it is open.
func (a *app) doneRows(card *taskDone, width int, sel bool) []string {
	if card == nil || width < 8 {
		return nil
	}
	out := []string{a.doneHead(card, width, sel)}
	if line := a.doneUnder(card, width); line != "" {
		out = append(out, line)
	}
	return append(out, a.doneDetail(card, width)...)
}

// doneHead is the row a person reads at a glance:
//
//	✓ ◆ Fix nil-map crash · done · 4m12s · 3 files (+42 −7)
//	■ ▲ Mix audio · stopped · 2m03s · branch kept · task/mix
//	✕ ▲ Port the parser · incomplete · 4m02s · 1 file · branch kept · task/parser
//	? ● Port the parser · your call · 6m40s · 2 files · branch kept · task/parser
//
// THE GLYPH IS THE TIER AND NOTHING ELSE, which is the whole of the design a
// person is asked to learn: `?` is a question in front of them, `✓ ■ ✕` are
// three ways of being over, and none of those three needs anything (the doc's
// own table). Only the question takes the accent — an incomplete landing is dim
// unless something actually broke, because most of them are work that ran out of
// road rather than work that went wrong.
//
// The identity is the one cell that never changes, and everything after the
// title is dim: the title is what the row is about and the rest is what became
// of it.
func (a *app) doneHead(card *taskDone, width int, sel bool) string {
	lead := a.doneMark(card) + " " + a.taskMarkSel(card.ident, sel) + " "
	tail := a.doneTail(card)
	// THE TAIL GOES FIRST WHEN THE TERMINAL IS NARROW, which is the tool line's
	// own rule for the same reason (toolview.go): the name is the substance, and
	// an outcome hung off a title nobody can read is a fact about nothing.
	room := width - 4
	if space := room - ansi.StringWidth(tail); space >= doneTitleFloor {
		room = space
	} else {
		tail = ""
	}
	line := lead + a.pal.ink(fit(card.title, room))
	if tail != "" {
		line += a.pal.dim(tail)
	}
	return line
}

// doneTitleFloor is the fewest cells a name may be cut to before the outcome
// beside it is dropped instead. Eight is the tool line's own floor.
const doneTitleFloor = 8

// doneMark is the tier in one cell, painted.
//
// IT IS THE READING'S CELL AND NOT THE STATE'S. Four marks used to be worked out
// here from `failed`, an ending and a merge word, and the same node wore a
// different one on the roster: the reading answers it once for every surface now
// ([session.ProjectTask]).
func (a *app) doneMark(card *taskDone) string {
	mark := a.tierMark(card.status)
	if card.status.Tier == session.TaskTierYourCall {
		// THE ONE ACCENT ON THE CARD. A question in front of somebody is the only
		// thing on this surface that is waiting for them, and it is the same cell
		// every other question here wears (tokens.GNeedsHuman).
		return a.pal.accent(mark)
	}
	switch card.status.Presence {
	case session.TaskPresenceStopped:
		// A person's own stop is not a finding, so it is neither a tick nor a cross.
		return a.pal.dim(mark)
	case session.TaskPresenceIncomplete:
		// THE CROSS IS DIM UNLESS SOMETHING BROKE. Running out of steps, losing the
		// wire and a check that named gaps are all work that did not finish, and
		// colouring them as failures reports a fault nobody found
		// ([session.TaskStatus.Fault] is the one field that says otherwise).
		if card.status.Fault {
			return a.pal.bad(mark)
		}
		return a.pal.dim(mark)
	}
	return a.pal.muted(mark)
}

// doneTail is everything the head says after the name, in ONE FIXED ORDER: the
// tier word, the span, the files, the merge fact, the branch. Nothing else goes
// on the head — the reason and the answers are the rows under it.
//
// THE WORD IS THE READING'S AND NEVER THIS CARD'S ([session.TaskStatus.Word]).
// The reason travels on row 2 rather than here, which is why this is Word and
// not [session.TaskStatus.RowWord]: a head carrying `your call · conflicts with
// your branch: parser.go, parser_test.go` would push the span, the files and the
// branch off every terminal there is.
//
// A KEPT BRANCH IS NAMED HERE: a branch that did not come home is the one
// outcome a person still has to do something about, and the name of it is the
// only handle back to work that is not on screen.
func (a *app) doneTail(card *taskDone) string {
	tail := ""
	if word := strings.TrimSpace(card.status.Word); word != "" {
		tail = " · " + word
	}
	// ONE SEPARATOR MEANS ONE THING ON THIS ROW. The span used to be joined to
	// the state word with a bare space while every other fact on the same row was
	// joined with ` · `, so the card read `? □ Cut every list · your call 12m00s ·
	// 4 files · merged` — in which the state word, which is the reason the card is
	// asking for a hand at all, reads as part of a duration. The list on the
	// roster was fixed the same way and for the same reason (audit-tasks.md's row
	// 4).
	if word := taskSpanWord(card.span); card.span > 0 {
		tail += " · " + word
	}
	if files := doneFilesWord(len(card.changed), card.added, card.removed); files != "" {
		tail += " · " + files
	}
	// The expanded body names the branch once, beside its delivery details.
	if card.open {
		return tail
	}
	if fact := doneMergeFact(card); fact != "" {
		tail += " · " + fact
	}
	if card.status.ChangesUnlanded() {
		tail += " · " + card.status.Branch
	}
	return tail
}

// doneMergeFact is where the work ended up, as a FACT and never as a state:
// `merged`, `branch kept`, or nothing at all for work done in place.
//
// `stopped — branch kept` is gone. It fused a state and a source-control fact
// into one phrase, and the head already carries the state one cell to the left —
// so a stopped node whose branch is waiting now reads `stopped · branch kept`,
// which is two facts a person can act on separately.
func doneMergeFact(card *taskDone) string {
	switch card.status.Changes {
	case session.TaskChangesMerged:
		return mergeScreenWord(mergeWordMerged)
	case session.TaskChangesKept, session.TaskChangesConflicted:
		// AND ONLY WHERE THERE IS A BRANCH TO KEEP. A folder family refuses over
		// the person's own edit with no branch anywhere — git said no and there is
		// nothing to send anybody to — and `branch kept` about no branch is a
		// pointer to nothing (the emptiness law).
		if card.status.Branch != "" {
			return taskBranchKept
		}
	}
	return ""
}

// doneFilesWord is the diffstat, and it says only what it was told.
//
// The COUNT is a fact the engine publishes (session's TaskNotice.Changed). The
// LINES are not: nothing on the wire carries them today, so the parenthesis is
// drawn when they arrive and is silently absent until then. A surface that
// computed a diffstat of its own here — by shelling into the node's branch —
// would be answering a question about work that has already merged with numbers
// nobody else in this process has ever seen.
func doneFilesWord(files, added, removed int) string {
	if files <= 0 {
		return ""
	}
	word := itoa(files) + doneFilesSuffix
	if files == 1 {
		word = itoa(files) + doneFileSuffix
	}
	if added <= 0 && removed <= 0 {
		return word
	}
	return word + " (" + glyphAdd + itoa(added) + "/" + glyphDel + itoa(removed) + ")"
}

// doneUnder is the muted line: what the work came to, when it started, and the
// key that opens the rest.
//
//	"the guard is in and the regression test passes" · started 14:02 · ctrl+o output
//
// The outcome is QUOTED because it is the node's own sentence and not this
// surface's — the same reason the report's first line is kept verbatim in the
// failure case rather than rewritten (task.go's landed word did the same). It
// falls back to the subtitle for a node that landed with nothing to say, which
// is better than an empty pair of quotes claiming it said nothing.
//
// AN OPEN CARD DRAWS NO PREVIEW AT ALL. The quote is the first sentence of what
// is about to be printed one row lower in full — on a card somebody has just
// expanded it is the same words twice, and the second copy is the one wearing
// quotation marks it does not need. The start stamp goes with it and comes back
// on the facts row ([app.doneFactsRow]), so nothing is lost and the expansion
// opens on the thing the card was opened for.
func (a *app) doneUnder(card *taskDone, width int) string {
	// A QUESTION'S SECOND ROW IS ITS REASON, AND IT IS THE ONE ACCENT ON THIS
	// CARD besides its glyph: it is the half a person acts on, and the head one
	// row above has already said everything else in dim.
	//
	// IT IS DRAWN WHETHER OR NOT ANYTHING CAN BE ANSWERED. The absence law is
	// about CAPABILITIES — an answer with no door behind it is left off — and a
	// reason is not one: `your call` with nothing under it is the card with no
	// choices and no explanation, which is the defect the task-states wave exists
	// to close. The ANSWERS are the landing question's, on the block above the
	// box (tasksettle.go); what stands here is what is being asked.
	if card.status.Tier == session.TaskTierYourCall {
		if reason := strings.TrimSpace(card.status.Ask.Reason); reason != "" {
			return a.pal.ask("  " + fit(reason, width-4))
		}
		return ""
	}
	// AND AN INCOMPLETE LANDING'S SECOND ROW IS WHY, dim, in the engine's own
	// sentence ([session.TaskReasonOf] spells the table once). It stands INSTEAD
	// of the quoted report and never beside it: two accounts of one landing on one
	// row is the wall this card was split apart to stop being.
	if reason := strings.TrimSpace(card.status.Reason); reason != "" {
		return a.pal.dim("  " + doneReasonRow(reason, doneReasonKey(card), width-4))
	}
	if card.open && card.saysOutcome() {
		return ""
	}
	tail := ""
	if !card.started.IsZero() {
		tail += " · " + doneStartWord + card.started.Format("15:04")
	}
	if a.doneHasDetail(card) && !card.open {
		tail += " · " + doneOutputKey
	}
	said := firstNonEmpty(card.outcome, card.subtitle)
	if said == "" && tail == "" {
		return ""
	}
	// The quotes are put on only around something that survived the fit: a pair
	// of quotation marks with nothing between them is the surface reporting that
	// it had no room rather than that the node said nothing.
	if said = fit(said, width-4-ansi.StringWidth(tail)); said != "" {
		said = `"` + said + `"`
	} else {
		// AND A ROW THAT LEADS WITH A SEPARATOR IS A ROW REPORTING ITS OWN MISSING
		// HALF. A stop and a landing that arrived with nothing to say both reach
		// here with no sentence at all — since this surface stopped writing glosses
		// of its own into an empty outcome — and ` · started 14:02` opens with a
		// join to something that is not there.
		tail = strings.TrimPrefix(tail, railSep)
	}
	return a.pal.dim("  " + said + tail)
}

// doneReasonRow is a reason and the one key clause beside it, fitted so that
// THE KEY IS THE HALF THAT SURVIVES.
//
// NO SENTENCE ON THIS CARD MAY END IN `…` HIDING THE INSTRUCTION. A reason
// carries a list — the files that clash, the gaps a check named — and a list is
// the half a person can do without; the clause naming what to press is the half
// they cannot. So the room is spent on the key first and the reason is cut into
// whatever is left, which is the reverse of every other fit on this card and the
// reason this has a function of its own (docs/design/task-states/DESIGN.md).
//
// A row with no room for even the key drops the key rather than the reason: half
// a key is a press that does nothing.
func doneReasonRow(reason, key string, room int) string {
	if key == "" || ansi.StringWidth(key) >= room {
		return fit(reason, room)
	}
	if said := fit(reason, room-ansi.StringWidth(key)); said != "" {
		return said + key
	}
	return fit(reason, room)
}

// doneReasonKey is the one dim clause a reason row may carry after it, and today
// it is `ctrl+o output` and nothing else.
//
// THE OPTIONAL VERB IS ABSENT AND NOT DEAD. The design offers `[r] rerun from its
// branch` on an incomplete landing whose branch has work — and NOTHING IN THIS
// PROCESS RE-RUNS A FINISHED TASK. A record row is an account of work that
// happened, and starting the same brief again is `/task <brief>`, which is a new
// piece of work with a new id (place_tasks.go's own reading of the same missing
// seam). A capability that cannot work is absent, not broken, so the key is not
// named and the foot does not promise it.
//
// WHAT IS DRAWN INSTEAD IS THE KEY THAT DOES WORK. A reason row stands where the
// quoted report would have been, and the report is behind `ctrl+o` — so on the
// one kind of card that no longer names that key anywhere, the card names it
// once, dim, at the end of the row it displaced.
func doneReasonKey(card *taskDone) string {
	if card.open || !card.hasDetail() {
		return ""
	}
	return " · " + doneOutputKey
}

// doneGroundLabel labels the row naming the branch a landed node's work is on,
// with the words the ground ladder keeps for the rung that made that node's
// world (internal/session's groundladder.go).
//
// THE ROW ANSWERS "WHERE WAS THIS WORK LEFT", and the branch is only half of
// that answer: the same branch name means a checkout registered in the person's
// own repository on one rung and a branch fetched home out of a fork on
// another. The label is the half that says which, and it is the ENGINE'S phrase
// rather than this package's for the reason the settled card reads the same
// table (taskrecord.go's [taskCardGroundWord]): the two are read minutes apart
// about one node, and two wordings of one place is one place too many.
//
// THE FALLBACK NAMES THE BRANCH AND NOTHING ELSE. A node whose rung nobody
// recorded — a graph a test scripted, a checkpoint written before the ladder —
// still has a branch, and `branch · ` is true of it without claiming what the
// directory was.
func (a *app) doneGroundLabel(card *taskDone) string {
	if word := session.GroundWord(card.rung, card.mode); word != "" {
		return word + " · "
	}
	return doneBranchLabel
}

// doneHasDetail reports whether there is anything behind the card at all. A
// node that landed with no report, no files, no branch and no brief has already
// said everything it has to say, and offering a key that opens nothing is worse
// than offering none.
func (a *app) doneHasDetail(card *taskDone) bool { return card.hasDetail() }

// hasDetail is that question asked of the card alone, for the rows that are
// drawn without a surface in hand ([doneReasonKey]).
func (card *taskDone) hasDetail() bool {
	return card.report != "" || card.result != "" || card.resultHeld ||
		len(card.changed) > 0 || card.branch != "" ||
		card.brief != "" || card.acceptance != "" || card.model != "" ||
		card.cost > 0
}

// ── WHAT IT PRODUCED, AND WHAT THE LANDING SAID ABOUT IT ────────────────────
//
// THE DEFECT, FROM A REAL CARD. A ten-chapter story was written, its branch
// would not merge, somebody took it as done anyway, and the expansion drew ONE
// DIM WALL: the merge refusal, git's own error under it, the sentence recording
// the decision, the words "finished, but needs your look" from before that
// decision, and then the answer itself — still in its markdown source, `**` and
// all — in the same grey as the file count and the price. Five different kinds
// of thing, one hue, in composition order, with the work the person delegated at
// the bottom.
//
// It is one wall because the card read ONE FIELD. The report is the landing's
// own story and it is REWRITTEN by everything that happens to a node afterwards
// — a late verdict, an accept, a merge that would not go all prepend their
// sentence to it — while what the work produced never moves. internal/session
// has kept the two apart since task_result.go, on exactly that reasoning, and
// this card had never read the second one.
//
// So: the account leads, in the hue that says whether the work got home; the
// answer follows in the ink a reply is read in, through the surface's one
// markdown door; the facts sit under it on one row.

// answerAndAccount splits a landed card in two: what the work PRODUCED, and the
// landing's own STORY about it.
//
// The engine hands both over when they are different things. When it hands over
// no result the report is all there is, and which of the two roles it plays
// depends on one question — did the work get home:
//
//   - It got home: the report IS the answer, whole, and there is no separate
//     account to draw. This is every ordinary two-line landing, and it is
//     byte-for-byte what this card drew before the split existed.
//   - It did not: the report LEADS with the sentence saying so (the engine
//     composes it that way, and [taskDone.outcome] is already that first line),
//     so that sentence is the account and the rest of the report is the answer.
//
// Nothing here reads the wording of a report. The question asked is the card's
// own settled state, which is the same rule internal/session's
// carriedResultLocked holds itself to and for the same reason: a report can be
// rewritten hours after the fact, and a state cannot be rewritten quietly.
func (card *taskDone) answerAndAccount() (answer, account string) {
	if card.result != "" {
		return card.result, session.TaskReportAccount(card.report, card.result)
	}
	// AND A HELD ANSWER IS NOT AN ANSWER THIS CARD HAS. The check did not accept
	// the work, so the engine named the result rather than handing it over — every
	// line of the report is the landing's own, and treating its tail as the answer
	// would promote a sentence about a branch into the body ink the work's own
	// words are read in. [doneMoreLine] says where the answer is instead.
	if card.resultHeld {
		return "", card.report
	}
	if card.undelivered() {
		return strings.TrimSpace(afterFirstLine(card.report)), card.outcome
	}
	return card.report, ""
}

// saysOutcome reports whether an open card states the landing in its own rows,
// which is what lets the collapsed preview stand down ([app.doneUnder]).
func (card *taskDone) saysOutcome() bool {
	answer, account := card.answerAndAccount()
	return strings.TrimSpace(answer) != "" || strings.TrimSpace(account) != ""
}

// undelivered says THE WORK IS NOT IN THE PERSON'S OWN TREE, and it is the one
// question the hierarchy turns on.
//
// A DELIVERY FAILURE IS NEVER FILED UNDER "done". A node can land done — merged
// by a person's accept, settled by a late verdict — with its branch still
// sitting unmerged over a conflict, and the expansion's job is to say so at the
// top, in the warn hue, once. It is asked of the merge word through the same
// three cases [app.doneTail] draws, plus the two settled states that keep a
// branch by definition, so a state this build has never heard of is not silently
// treated as delivered.
func (card *taskDone) undelivered() bool {
	switch card.status.Changes {
	case session.TaskChangesKept, session.TaskChangesConflicted:
		return true
	}
	// AND ANYTHING THAT IS NOT PLAINLY DONE DID NOT GET HOME EITHER, whatever the
	// merge word says: a stop, a landing that ran out of road and a question
	// nobody has answered all leave the work somewhere other than the person's own
	// tree. It is asked of the READING and not of a state, so a state this build
	// has never heard of is not silently treated as delivered.
	return card.status.Presence != session.TaskPresenceDone
}

// afterFirstLine is everything a block says after its first line, and "" when it
// says nothing else. It is [firstLine]'s other half.
func afterFirstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[i+1:]
	}
	return ""
}

// doneDetail is the full context, and it is the labelled block the proposal's
// own expansion is (task.go).
//
// THE ORDER IS THE ORDER SOMEBODY READS IN, and it changed with this wave. It
// used to be the facts, then the report, then the brief — which put the thing
// the work was delegated for underneath the price. It is now: what happened to
// the delivery, what the work produced, and only then the facts a person opens
// a card to CHECK rather than to read.
//
// THE FACTS IT DOES NOT HAVE ARE ABSENT RATHER THAN EMPTY. Which batch a node
// belonged to is still not on the wire, so no row claims it. The model and the
// PRICE both are now (session's TaskNotice.Model and CostUSD), and they arrived
// exactly where this comment said they would — beside the working copy, with
// nothing else moved — each drawn only when there is one to draw.
//
// WHAT IS STILL NOT HERE IS THE DIFFSTAT'S LINES. The head counts the files a
// node wrote because the engine publishes the list (TaskNotice.Changed); the
// "+42 −7" beside that count needs insertions and deletions, and NOTHING in
// this process has them — not the notice, not the project's index, which counts
// files and nothing finer (session's TaskIndexEntry.FilesChanged). The card is
// already built to draw them the moment they are published ([doneFilesWord]),
// and until then it says the true smaller thing rather than shelling into a
// merged branch for numbers of its own.
func (a *app) doneDetail(card *taskDone, width int) []string {
	if !card.open {
		return nil
	}
	room := width - 2
	var out []string
	say := func(lines ...string) {
		for _, line := range lines {
			out = append(out, a.pal.dim("  "+line))
		}
	}
	answer, account := card.answerAndAccount()
	out = append(out, a.doneAccountRows(card, account, room)...)
	out = append(out, a.doneAnswerRows(card, answer, room)...)
	if len(card.changed) > 0 {
		say(wrap(doneChangedLabel+strings.Join(card.changed, " · "), room)...)
	}
	if card.branch != "" {
		branch := card.branch
		// THROUGH THE TABLE, NEVER THE TOKEN (task.go's [mergeScreenWords]): the
		// expansion is the fourth reader of a merge word and had the same leak.
		if word := mergeScreenWord(card.merge); word != "" {
			branch += " · " + word
		}
		say(fit(a.doneGroundLabel(card)+branch, room))
	}
	out = append(out, a.doneFactsRow(card, room)...)
	if card.acceptance != "" {
		say(capField(wrap(doneAcceptLabel+card.acceptance, room))...)
	}
	if card.brief != "" {
		say(capField(wrap(doneBriefLabel+card.brief, room))...)
	}
	return out
}

// doneAccountRows is THE LANDING'S OWN SENTENCE, drawn once, at the top.
//
// Its first line is the headline and takes the hue: warn behind a `!` when the
// work is not in the person's tree, dim and unmarked when it is. Everything the
// account says after that line — git's own words about a refusal, what was left
// behind, whose decision moved the node — follows underneath in the quiet hue,
// indented under the mark, capped at [doneWindow] like every long field here.
//
// THE DIAGNOSTICS ARE NOT DROPPED, they are RANKED. What a person needs first is
// the sentence saying where their work is; what they need next, and only
// sometimes, is the machine's account of why. Both are on the card and neither
// is repeated: the head says the state, this says the reason, and the answer
// below says what was made.
func (a *app) doneAccountRows(card *taskDone, account string, room int) []string {
	if account = strings.TrimSpace(account); account == "" {
		return nil
	}
	paint, mark := a.pal.dim, ""
	if card.undelivered() {
		paint, mark = a.pal.warn, glyphHalted+" "
	}
	pad := strings.Repeat(" ", ansi.StringWidth(mark))
	var out []string
	for i, line := range wrap(firstLine(account), room-ansi.StringWidth(mark)) {
		lead := mark
		if i > 0 {
			lead = pad
		}
		out = append(out, paint("  "+lead+line))
	}
	if rest := strings.TrimSpace(afterFirstLine(account)); rest != "" {
		for _, line := range capField(wrap(rest, room-ansi.StringWidth(pad))) {
			out = append(out, a.pal.dim("  "+pad+line))
		}
	}
	out = capFieldMark(out, a.pal.dim(glyphMore))
	for i := range out {
		out[i] = fit(out[i], room+2)
	}
	return out
}

// doneAnswerRows is WHAT THE WORK PRODUCED, and it is the one block on this card
// that is not furniture.
//
// It goes through the surface's own markdown door ([app.renderMarkdown]), which
// is the same renderer a reply is read in — so a task that answered with
// headings, a list or a fenced block is read as that rather than as its source.
// The card used to wrap it as plain text inside [palette.dim], and a person
// delegating a piece of writing got their writing back as grey `**answer**`.
//
// PROSE PAINTS ITS OWN ROWS, BODY INK INCLUDED, so nothing here may wrap them in
// a second foreground (markdown.go states the law; homeexchange.go's settled
// reply obeys it in the same words). The two-space indent is the only thing
// added, and the cap marker is dimmed rather than left in whatever hue the last
// row ended on.
func (a *app) doneAnswerRows(card *taskDone, answer string, room int) []string {
	var out []string
	if answer = strings.TrimSpace(answer); answer != "" {
		for _, line := range capFieldMark(trimBlanks(a.renderMarkdown(answer, room)), a.pal.dim(glyphMore)) {
			out = append(out, "  "+fit(line, room))
		}
	}
	// THE POINTER WRAPS RATHER THAN TRUNCATES, which is the one place on this card
	// that rule is worth stating: it ends in a PATH, and a path cut short is a
	// pointer to nothing. Everything else on the block is a label and a short
	// value, where a cut costs a word.
	for _, line := range wrap(doneMoreLine(card), room) {
		if line == "" {
			continue
		}
		out = append(out, a.pal.dim("  "+line))
	}
	return out
}

// doneMoreLine says the answer above is not all of it, and where the rest is.
//
// IT IS DRAWN OR THE CARD IS LYING. A result arrives cut when the work said more
// than one reader is handed (session's taskResultCarry), and a block of text
// that stops mid-sentence with nothing under it is a card claiming the work
// stopped there.
//
// A HELD ANSWER SAYS ONLY WHERE IT IS. The check did not pass the work, so
// nothing was handed over at all — and the sentence this line used to lead with,
// `what it produced was not taken as done`, was a second spelling of a state the
// card's own reason row now says in the engine's words. What is left is the half
// that was always the useful one: where the answer can be read. An answer nobody
// kept a path to says nothing rather than announcing its own absence.
func doneMoreLine(card *taskDone) string {
	where := doneMoreGone
	if card.resultWhole != "" {
		where = doneMoreAt + card.resultWhole
	}
	switch {
	case card.resultHeld:
		if card.resultWhole == "" {
			return ""
		}
		return where
	case card.resultCut:
		return glyphMore + " " + where
	}
	return ""
}

// doneFactsRow is whose hands, what they came to, and when — ONE ROW.
//
// It was three, stacked under each other in the same grey as everything else on
// the card, and three rows is what a table looks like when it has three facts
// that are each four words long. They are secondary by construction: nobody
// opens a landed card to read the price, they open it to read the work and then
// check the price.
//
// IT WRAPS BY FACT AND NEVER INSIDE ONE. At sixty columns a long model name and
// a span do not fit on one row together, and a `ran · 14:02 → 14:14` broken
// across two lines is a fact a person has to reassemble. So the row packs whole
// facts and starts a new one when the next will not fit, which keeps every fact
// readable at every width this surface draws at.
//
// THE START STAMP FALLS BACK HERE. The collapsed card carries `started 14:02`
// and an open one does not draw that line at all ([app.doneUnder]), so a card
// that knows when the work began and not when it ended still says so.
func (a *app) doneFactsRow(card *taskDone, room int) []string {
	var facts []string
	if card.model != "" {
		facts = append(facts, doneModelLabel+card.model)
	}
	// ZERO IS NOBODY PUBLISHED A PRICE and never $0.00 — the emptiness law, kept
	// here exactly as it was kept when this was a row of its own.
	if card.cost > 0 {
		facts = append(facts, dollars(card.cost))
	}
	switch {
	case !card.started.IsZero() && !card.landed.IsZero():
		facts = append(facts, doneSpanLabel+card.started.Format("15:04")+" → "+card.landed.Format("15:04"))
	case !card.started.IsZero():
		facts = append(facts, doneStartWord+card.started.Format("15:04"))
	}
	var out []string
	line := ""
	for _, fact := range facts {
		switch {
		case line == "":
			line = fact
		case ansi.StringWidth(line)+ansi.StringWidth(railSep)+ansi.StringWidth(fact) <= room:
			line += railSep + fact
		default:
			out = append(out, a.pal.dim("  "+fit(line, room)))
			line = fact
		}
	}
	if line != "" {
		out = append(out, a.pal.dim("  "+fit(line, room)))
	}
	return out
}

// capField bounds one field of an open card at [doneWindow] rows, marking the
// cut.
// There is no "… N more" foot to click: the whole of a node's life is in its
// room, one enter away on the same card, and a second cap-lifting mechanic
// would be a second answer to "where is the rest".
func capField(lines []string) []string {
	return capFieldMark(lines, glyphMore)
}

// capFieldMark is [capField] against a stated marker, for the one field whose
// rows paint themselves: an answer comes back from prose with its own colours
// and its own resets, so a bare `…` appended after the last of them would be the
// only character on the card in the terminal's default foreground.
func capFieldMark(lines []string, mark string) []string {
	if len(lines) <= doneWindow {
		return lines
	}
	lines = lines[:doneWindow]
	lines[doneWindow-1] += " " + mark
	return lines
}

// ── the rollup ──────────────────────────────────────────────────────────────

// rollupRows draws a batch as one object:
//
//	✓ 3 tasks done · 9m14s
//	  ◆ Fix nil-map crash · 4m12s · 3 files
//	  ▲ Collect sources · 1m02s
//	  ● Mix audio · 4m00s
//	  "the mix is level and the stems are kept" · started 14:02 · ctrl+o output
//
// THE OUTCOME IS THE MOST RECENT ONE'S, AND ONLY ITS. Three quoted sentences
// stacked under a header is the thing this rollup exists to stop being; the
// last one is the one a person is most likely to be waiting on, and every other
// card opens its own on its own row.
//
// Each compact row keeps its own entry, so a click expands THAT card in place
// and the rollup stays a rollup. The header carries the run's first card, which
// is the only entry a header could honestly point at.
func (a *app) rollupRows(d deck, out []row, from, to, width int) []row {
	out = append(out, row{text: a.rollupHead(d, from, to, width), entry: from, hit: hitDone})
	last := -1
	for i := from; i < to; i++ {
		card := d.entries[i].done
		if card == nil {
			continue
		}
		last = i
		out = append(out, row{text: a.rollupRow(card, width, a.selected(i)), entry: i, hit: hitDone})
		for _, text := range a.doneDetail(card, width-2) {
			out = append(out, row{text: "  " + text, entry: i, hit: hitDone})
		}
	}
	if last >= 0 {
		if card := d.entries[last].done; card != nil && !card.open {
			if line := a.doneUnder(card, width-2); line != "" {
				out = append(out, row{text: "  " + line, entry: last, hit: hitDone})
			}
		}
	}
	return out
}

// rollupHead counts the batch and says how long the whole of it took.
//
// THE SPAN IS WALL-CLOCK, not the sum of the parts: nodes run in parallel, and
// a header that added four four-minute tasks into sixteen minutes would be
// reporting a wait nobody had. It is the first spawn to the last landing, which
// is the thing the person actually lived through.
func (a *app) rollupHead(d deck, from, to, width int) string {
	count := 0
	var loudest *taskDone
	var first, last time.Time
	for i := from; i < to; i++ {
		card := d.entries[i].done
		if card == nil {
			continue
		}
		count++
		if doneLouder(card, loudest) {
			loudest = card
		}
		if !card.started.IsZero() && (first.IsZero() || card.started.Before(first)) {
			first = card.started
		}
		if card.landed.After(last) {
			last = card.landed
		}
	}
	mark, word := a.pal.muted(a.icon(tokens.GSettled)), doneRollupWord
	if loudest != nil {
		// A MIXED BATCH IS NOT A DONE BATCH. The header wears its loudest child's
		// own mark and stops saying "done", because the one thing a rollup must
		// never do is report four successes when it is three and a question; which
		// of them it was is on its own row, in the same mark.
		mark, word = a.doneMark(loudest), doneRollupMix
	}
	head := mark + " " + a.pal.ink(itoa(count)+word)
	if !first.IsZero() && last.After(first) {
		head += a.pal.dim(" · " + taskSpanWord(last.Sub(first)))
	}
	return fitPainted(head, width)
}

// doneLouder reports whether one card outranks another for the mark a folded
// family wears. A QUESTION IN FRONT OF SOMEBODY OUTRANKS EVERYTHING, because it
// is the only one of the three that is waiting on them; after it comes work that
// did not get home, and a batch of plain landings has no loudest child at all.
func doneLouder(card, than *taskDone) bool {
	rank := func(one *taskDone) int {
		switch {
		case one == nil:
			return 0
		case one.status.Tier == session.TaskTierYourCall:
			return 3
		case one.status.Presence == session.TaskPresenceIncomplete:
			return 2
		case one.status.Presence == session.TaskPresenceStopped:
			return 1
		}
		return 0
	}
	return rank(card) > rank(than)
}

// rollupRow is one card inside a batch: the identity, the name, and the two
// facts that distinguish it from its siblings.
func (a *app) rollupRow(card *taskDone, width int, sel bool) string {
	// The indent, the identity and a space is four cells; a mark costs two more,
	// and it is the only thing a compact row says about state — inside a rollup
	// the header has already said the batch is home. There is NO tick on a row
	// that succeeded: the header said that, and a column of them is a column read
	// to learn nothing (the law toolview.go states).
	lead, used := "  "+a.taskMarkSel(card.ident, sel)+" ", 4
	if doneLouder(card, nil) {
		lead += a.doneMark(card) + " "
		used += 2
	}
	tail := ""
	if card.span > 0 {
		tail = " · " + taskSpanWord(card.span)
	}
	if files := doneFilesWord(len(card.changed), card.added, card.removed); files != "" {
		tail += " · " + files
	}
	room := width - used
	if space := room - ansi.StringWidth(tail); space >= doneTitleFloor {
		room = space
	} else {
		tail = ""
	}
	return lead + a.pal.ink(fit(card.title, room)) + a.pal.dim(tail)
}
