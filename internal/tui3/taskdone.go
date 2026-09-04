package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
	failed          bool
	// ending is why a failed node stopped, as the engine said (taskending.go),
	// and "" when it did not say.
	ending session.TaskEnding
	// unverified is the third settled state (session's TaskUnverified), and it
	// is a FIELD OF ITS OWN rather than a value of failed: the run finished, no
	// finding was made against it, and a card that folded it into the failure
	// bool would be this surface reporting a verdict nobody gave. A card is
	// never both — failed stays false here.
	unverified bool
	// span is the node's own age at its final state. started is the record's start
	// or the live surface's established fallback, and landed is the record's
	// settling instant; the surface clock is the last resort only for a landing
	// this window actually watched. They are kept because "how long" and "when"
	// are different questions and the second one is what a person matches against
	// their own memory of the afternoon.
	span              time.Duration
	started, landed   time.Time
	outcome, report   string
	changed           []string
	added, removed    int
	branch, merge     string
	brief, acceptance string
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
	// decided is the receipt the card wears once its question has been answered
	// from this surface, and it is empty for every card that was never asking
	// (tasksettle.go). It is a STRING and not a bool because what the row says is
	// what the person did — "you took this as done", "sent back to be checked
	// again" — and a card that recorded only that it had been answered would have
	// to reconstruct the sentence from a state the engine has since moved past.
	decided string
	// asks says the choices belong on THIS card, and it is frozen at landing from
	// the person's `task.settle` row (tasksettle.go). Under `auto` the engine has
	// already handed this decision to the model, so a card that drew four choices
	// would be asking a question somebody else is answering — and reading the row
	// at draw time instead would mean a policy flipped mid-afternoon retroactively
	// took the choices off a card that was genuinely asking.
	asks bool
	// trouble is the one dim line a card carries when an answer could NOT be
	// spent and the question is therefore still standing — no checker to look
	// again with, a working copy that has gone. It is separate from [decided]
	// because the two are opposite facts about the same press: one says the
	// question is over, the other says it is not (tasksettle.go).
	trouble string
	// chips are the pressable columns of the answers row, written by the layout
	// that drew it and read by the pointer. They are recorded here for the reason
	// the proposal's own choices are (task.go's spans): a hit-test that measured
	// the row itself would be measuring a row this frame may not have drawn.
	chips []settleChip
}

// The card's words.
const (
	doneWord      = "done"
	doneFailWord  = "failed"
	doneOutputKey = "ctrl+o output"
	// The word for a landing nobody could judge is the rail's own
	// ([taskUnverifiedWord]): one vocabulary for one state, so a person who read
	// "needs your look" on the column does not have to learn a second name for it
	// in the transcript.
	//
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
	doneCostLabel   = "cost · "
)

// doneWindow caps the two long fields inside an open card — the report and the
// brief. Twenty rows is about a screen; past it a person is reading a document
// in a transcript, and the room (room.go) is where a node's whole life is.
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
		id:         node.id,
		ident:      node.ident,
		title:      title,
		subtitle:   taskSubtitleOf(title, node.assignment),
		failed:     node.state == session.TaskFailed,
		ending:     node.ending,
		unverified: node.state == session.TaskUnverified,
		span:       node.elapsed,
		started:    node.spawnedAt(),
		landed:     landed,
		outcome:    strings.TrimSpace(firstLine(node.report)),
		report:     strings.TrimSpace(node.report),
		changed:    node.changed,
		branch:     node.branch,
		merge:      node.merge,
		rung:       node.rung,
		mode:       node.mode,
		brief:      node.brief,
		acceptance: node.acceptance,
		model:      node.model,
		cost:       node.spent(),
	}
	// WHO IS BEING ASKED IS SETTLED HERE, ONCE, from the row the engine wrote this
	// node's landing note under (tasksettle.go's [app.settlePolicyAsks]).
	card.asks = card.unverified && a.settlePolicyAsks()
	if card.span == 0 && !node.began.IsZero() {
		card.span = a.now().Sub(node.began)
	}
	// A FAILURE THIS SURFACE WAS TOLD NOTHING ABOUT IS A NODE THAT STOPPED, and
	// it says so in the word the rail uses (task.go's [taskStoppedWord]). The
	// engine's own report is kept verbatim wherever there is one — "stopped: 40
	// steps and no finish" is the difference between work that broke and work
	// that ran out, and nothing here rewrites it.
	if card.failed && card.outcome == "" {
		card.outcome = taskStoppedWord
	}
	// AND THE SAME FOR A LANDING NOBODY COULD JUDGE. The engine leads such a
	// node's report with a line already in a person's words ("finished, but needs
	// your look — …"), and it is kept verbatim for the reason the failure
	// sentence is: it is what a person reads to decide, and this surface is not
	// the thing that decided it. The gloss stands in only for a node that arrived
	// with no report at all, which would otherwise be a card that names the state
	// and then says nothing about why.
	if card.unverified && card.outcome == "" {
		card.outcome = taskUnverifiedGloss
	}
	// A card lands in the middle of whatever the model was saying, exactly as a
	// note did: the streaming block is closed first so the card is a block of its
	// own rather than a paragraph inside the reply (app.go's [feed.note]).
	a.closeLive()
	a.entries = append(a.entries, entry{kind: entryDone, turn: a.turn, done: card})
	a.follow()
	a.touch()
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
		// AND THE DECISION UNDER THE FACTS, which is the order somebody reads in:
		// what happened, what it came to, what is behind it, and only then the
		// answers (tasksettle.go).
		out = a.settleRows(out, card, i, width, 0)
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
//	✗ ▲ Mix audio · failed · 2m03s · stopped — branch kept · task/mix
//	? ● Port the parser · needs your look · 6m40s · 2 files · branch kept · task/parser
//
// The state mark is the rail's own (task.go's [app.railGlyph] draws the same
// three), the identity is the one cell that never changes, and everything after
// the title is dim: the title is what the row is about and the rest is what
// became of it.
func (a *app) doneHead(card *taskDone, width int, sel bool) string {
	mark := a.pal.muted(a.linearMark(glyphDone, glyphDoneASCII))
	switch {
	case card.failed && refused(card.ending):
		mark = a.pal.warn(glyphHalted)
	case card.failed:
		mark = a.pal.bad(a.pal.badGlyph())
	case card.unverified:
		// THE RAIL'S OWN THIRD MARK (task.go's [glyphUnverified]): the question
		// this card is, in the hue that says it is not a failure.
		mark = a.pal.warn(glyphUnverified)
	}
	lead := mark + " " + a.taskMarkSel(card.ident, sel) + " "
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

// doneTail is everything the head says after the name.
//
// A KEPT BRANCH IS NAMED HERE, in the words the rail uses for it (task.go's
// [taskStoppedKept]): a branch that did not come home is the one outcome a
// person still has to do something about, and the name of it is the only handle
// back to work that is not on screen.
func (a *app) doneTail(card *taskDone) string {
	verb := doneWord
	switch {
	case card.failed && refused(card.ending):
		verb = taskRecordStoppedWord
	case card.failed:
		verb = doneFailWord
	case card.unverified:
		verb = taskUnverifiedWord
	}
	tail := " · " + verb
	// ONE SEPARATOR MEANS ONE THING ON THIS ROW. The span used to be joined to
	// the state word with a bare space while every other fact on the same row was
	// joined with ` · `, so the card read `? □ Cut every list · needs your look
	// 12m00s · 4 files · merged` — in which `needs your look` and `12m00s` are two
	// separate facts fused into one phrase, and the state word, which is the
	// reason the card is asking for a hand at all, reads as part of a duration.
	// The list on the roster was fixed the same way and for the same reason
	// (audit-tasks.md's row 4).
	if word := taskSpanWord(card.span); card.span > 0 {
		tail += " · " + word
	}
	if files := doneFilesWord(len(card.changed), card.added, card.removed); files != "" {
		tail += " · " + files
	}
	switch card.merge {
	case mergeWordConflicted:
		// A FOLDER FAMILY WEARS THIS WORD WITHOUT A BRANCH. Its landing refuses
		// over the person's own edit to a file it was going to lay, which is the
		// same fact a conflict is, and there is no branch to send anybody to — the
		// report names the directory instead. The name is drawn when there is one
		// and the tail simply ends when there is not.
		tail += " · " + mergeWordConflicted
		if card.branch != "" {
			tail += " · " + card.branch
		}
	case mergeWordAborted:
		// AN UNVERIFIED NODE DID NOT STOP. It ran to the end and its branch was
		// kept because nothing merges on an answer nobody gave, so it takes the
		// half of the sentence that is true of it (task.go's [taskBranchKept]).
		kept := endingKept(card.ending)
		if card.unverified {
			kept = taskBranchKept
		}
		tail += " · " + kept + " · " + card.branch
	case mergeWordMerged, mergeWordInPlace:
		// THROUGH THE TABLE, NEVER THE TOKEN (task.go's [mergeScreenWords]). This
		// arm used to append the engine's own word, so a task that ran in a
		// folder with no repository closed its card with `· inplace`.
		tail += " · " + mergeScreenWord(card.merge)
	}
	return tail
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
func (a *app) doneUnder(card *taskDone, width int) string {
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
	}
	return a.pal.dim("  " + said + tail)
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
func (a *app) doneHasDetail(card *taskDone) bool {
	return card.report != "" || len(card.changed) > 0 || card.branch != "" ||
		card.brief != "" || card.acceptance != "" || card.model != "" ||
		card.cost > 0
}

// doneDetail is the full context, and it is the labelled block the proposal's
// own expansion is (task.go): the facts first, because they are what a person
// opened the card to check, then the two long fields.
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
	if card.model != "" {
		say(fit(doneModelLabel+card.model, room))
	}
	if card.cost > 0 {
		say(fit(doneCostLabel+dollars(card.cost), room))
	}
	if !card.started.IsZero() && !card.landed.IsZero() {
		say(fit(doneSpanLabel+card.started.Format("15:04")+" → "+card.landed.Format("15:04"), room))
	}
	if card.acceptance != "" {
		say(capField(wrap(doneAcceptLabel+card.acceptance, room))...)
	}
	if card.report != "" {
		say(capField(wrap(card.report, room))...)
	}
	if card.brief != "" {
		say(capField(wrap(doneBriefLabel+card.brief, room))...)
	}
	return out
}

// capField bounds one field of an open card at [doneWindow] rows, marking the
// cut.
// There is no "… N more" foot to click: the whole of a node's life is in its
// room, one enter away on the same card, and a second cap-lifting mechanic
// would be a second answer to "where is the rest".
func capField(lines []string) []string {
	if len(lines) <= doneWindow {
		return lines
	}
	lines = lines[:doneWindow]
	lines[doneWindow-1] += " " + glyphMore
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
		// A BATCH DOES NOT SWALLOW A QUESTION. A rollup exists to stop three
		// landings saying the word "task" three times, and a node inside one that
		// is waiting on a decision is the one thing in a batch that is not merely
		// news — so its answers row is drawn on its own row, indented with the rest
		// of the batch (tasksettle.go).
		out = a.settleRows(out, card, i, width, 2)
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
	count, failed, unverified := 0, false, false
	var first, last time.Time
	for i := from; i < to; i++ {
		card := d.entries[i].done
		if card == nil {
			continue
		}
		count++
		failed = failed || card.failed
		unverified = unverified || card.unverified
		if !card.started.IsZero() && (first.IsZero() || card.started.Before(first)) {
			first = card.started
		}
		if card.landed.After(last) {
			last = card.landed
		}
	}
	mark, word := a.pal.muted(a.linearMark(glyphDone, glyphDoneASCII)), doneRollupWord
	switch {
	case failed:
		// A MIXED BATCH IS NOT A DONE BATCH. The header keeps the failure mark and
		// stops saying "done", because the one thing a rollup must never do is
		// report four successes when it is three and a failure; which of them
		// failed is on its own row, in its own mark.
		mark, word = a.pal.bad(a.pal.badGlyph()), doneRollupMix
	case unverified:
		// AND A BATCH WITH A QUESTION IN IT IS NOT A DONE BATCH EITHER, for the
		// same reason and one step quieter: nothing failed, so the mark is the
		// question rather than the cross, and the header stops claiming that
		// everything under it came home.
		mark, word = a.pal.warn(glyphUnverified), doneRollupMix
	}
	head := mark + " " + a.pal.ink(itoa(count)+word)
	if !first.IsZero() && last.After(first) {
		head += a.pal.dim(" · " + taskSpanWord(last.Sub(first)))
	}
	return fitPainted(head, width)
}

// rollupRow is one card inside a batch: the identity, the name, and the two
// facts that distinguish it from its siblings.
func (a *app) rollupRow(card *taskDone, width int, sel bool) string {
	// The indent, the identity and a space is four cells; a failure mark costs
	// two more, and it is the only thing a compact row says about state — inside
	// a rollup the header has already said the batch is home. There is NO tick
	// on a row that succeeded: the header said that, and a column of them is a
	// column read to learn nothing (the law toolview.go states).
	lead, used := "  "+a.taskMarkSel(card.ident, sel)+" ", 4
	switch {
	case card.failed:
		lead += a.pal.bad(a.pal.badGlyph()) + " "
		used += 2
	case card.unverified:
		// The one other state worth a cell inside a rollup: the header says the
		// batch is home, and this row says which of them is not finished being
		// decided.
		lead += a.pal.warn(glyphUnverified) + " "
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
