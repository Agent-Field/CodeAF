package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE TASK SURFACE: A DECISION, AND THEN A PRESENCE.
//
// internal/session's task_contract.go says what a task IS — a node in a
// dynamically evolving DAG, of which v1 ships the degenerate one-node case —
// and this file is the two places that node touches a person:
//
//   - THE PROPOSAL IS A DECISION MOMENT, INLINE. It arrives mid-conversation,
//     it blocks the turn that raised it, and it is answered once. That is the
//     shape the consent question already has on this surface, so it takes the
//     consent grammar whole: the question hue (violet, the fifth colour, spent
//     nowhere else), the row that keeps its verdict afterwards, the keyboard
//     lane at the bottom of the screen. It is drawn in the TRANSCRIPT rather
//     than above the input because a proposal is part of what happened — the
//     model asked, you answered, the work started — and a modal that vanished
//     would leave the conversation unable to explain where a running node came
//     from.
//   - THE RUNNING NODE IS A STANDING PRESENCE, IN A RAIL. It outlives the turn
//     that proposed it: its "done" lands minutes later with no turn open and no
//     stream to land on. A transcript line cannot hold something that is still
//     true, so the right column holds it — one row per node, alive at the top,
//     gone when it has come home — and the transcript gets one dim line when it
//     lands. THE RAIL IS PRESENCE; THE NOTE IS HISTORY. Neither says the other's
//     half.
//
// THE RAIL IS TREE-READY AND THE TREE IS NOT DRAWN. v1's graph has no edges, so
// nothing here draws one — but every node carries its DependsOn, the row model
// stores it, and a node whose prerequisites are unmet renders v1's own sentence
// under it ("waits: collect sources", internal/tui's model_test.go). When edges
// arrive the shape is already in the model; what changes is how many rows have
// something to say.

// ── the two rows of state ───────────────────────────────────────────────────

// taskCard is one proposal, from the question to the verdict.
//
// It is a pointer held in two places — the transcript entry that draws it and
// [app.task], the lane that answers it — so the answer and the row can never
// disagree about what was decided.
type taskCard struct {
	id                                       uint64
	title, summary, brief, acceptance, where string
	// name is the two-or-three-word NAME cut from the title, sub the one-line
	// sentence under it, and ident the glyph and hue this node is followed by
	// for the rest of its life (taskident.go). All three are derived once, here,
	// because a card is read on every frame while its clock runs and a name
	// recomputed thirty times a second is a name computed twenty-nine times too
	// often.
	name, sub string
	ident     taskIdent
	dependsOn []uint64
	// model is the model this work will run on, and it is a FACT rather than a
	// question: the engine resolved it before anybody was asked (session's
	// taskmodel.go), and the block states it because a proposal that did not say
	// whose hands the work is going into would be hiding the one thing about it
	// nobody can find out afterwards.
	//
	// THE SHORTLIST IS NOT DRAWN ANY MORE, and this one field is all that is
	// left of it. A word that fitted more than one model used to raise a row of
	// chips here, answered by digits — and the digits are the question block's
	// answers now, one grammar for every decision on this surface, so a second
	// reader for the same keystroke is exactly what this wave exists to end.
	// What runs is what the engine already resolved (session's firstTaskModel),
	// which is what those chips opened on; CORRECTING IT FROM THE PROPOSAL IS
	// OWED, and until it lands the way to ask for another is to say so in the
	// words `c change` takes.
	model string
	// elsewhere is the one dim line saying which of this brief's files another
	// window's work is already in, as the engine wrote it (session's
	// TaskNotice.Elsewhere), and "" when there was nothing to say.
	//
	// IT IS DRAWN AS A FACT AND NOT AS AN ALARM — the same dim lane the branch
	// point and the meta line use — because nothing about the card changes on
	// account of it: the same three options, the same clock, the same work if you
	// say yes. It exists so a person spends the countdown knowing something they
	// would otherwise have found out at merge time.
	elsewhere string
	// deadline is when silence becomes approval, or zero when the clock is off
	// or held (session's TaskNotice.Deadline). A zero deadline draws no countdown:
	// a number counting down to nothing is a promise the engine did not make.
	deadline time.Time
	// born is when this phase of the card arrived: the call's first fragment
	// while it forms, which is what the count-up on that row measures from.
	born time.Time
	// open says the brief and the acceptance are showing, behind the same
	// expand mechanic a tool row's detail is behind.
	open bool
	// verdict is what was decided, in the words the row keeps afterwards. It is
	// empty for exactly as long as the question is open.
	verdict string
	// answer is the ANSWER that settled it, in the question's own word for it —
	// `start it`, `no`, `change` — kept beside the verdict the way a consent row
	// keeps "allowed". It is empty when nobody chose: a clock that ran out and a
	// turn that ended chose nothing.
	answer string

	// forming says this card is a propose_task call that is STILL ARRIVING —
	// the block drawn from the first fragment of the call, before there is a
	// proposal to put in it ([app.formTask]).
	//
	// A FORMING CARD IS NOT A QUESTION. It has no id, no deadline, no options
	// and no answer lane: it is never [app.task], nothing on the keyboard is
	// pointed at it, and the frame it draws is furniture grey rather than the
	// question hue. It exists so the block GROWS where it will stand instead of
	// arriving whole under a reply somebody is in the middle of reading.
	forming bool
	// callID is the provider's identity for the call filling a forming card. It
	// is separate from id, which belongs to the task the engine has not minted
	// yet, and empty when the wire has not named the call.
	callID string
}

// choiceSpan is one option's columns on the choices row: [from, to) answers to
// the option at.
type choiceSpan struct{ from, to, at int }

// settled reports whether this proposal has been answered.
func (c *taskCard) settled() bool { return c.verdict != "" }

// taskNode is one node's life, as the rail holds it.
//
// It is a struct of its own rather than the session's TaskNotice because the
// rail needs one fact the notice cannot carry: WHEN the node started, so the
// elapsed clock counts on the frame tick instead of freezing at whatever the
// last update happened to say. Everything else is the notice, kept.
type taskNode struct {
	id uint64
	// title is what the rail draws: the NAME, two or three words, derived once
	// from the engine's own title (taskident.go). label is that title whole,
	// kept because the cards have width for it and because a name is a cut of
	// something and the thing it was cut from is worth keeping.
	title, label string
	// assignment is the sentence the subtitle comes from — the proposal's
	// summary, or its brief when there was no summary — and brief/acceptance are
	// the node's contract with its runner, carried here so a card that lands ten
	// minutes later can still say what the work was FOR. All three arrive on the
	// proposal, not on the updates: an update carries a state, and the contract
	// is frozen at admission (internal/session's task_room.go says so).
	assignment, brief, acceptance, where string
	// ident is the glyph and the hue this node is followed by, keyed on the id
	// and stable for its whole life (taskident.go).
	ident taskIdent
	// model is the model this node runs on, as the engine published it
	// (session's TaskNotice.Model). Empty means nobody said — a scripted agent,
	// an older engine — and every row that draws it draws nothing instead, the
	// way the spend does.
	model     string
	nextModel string
	thinking  string
	state     session.TaskState
	// dependsOn is the structural half of this file (see the header): stored
	// always, drawn only when a prerequisite is unmet.
	dependsOn []uint64
	// parent is the node this one was SPAWNED UNDER, and paused is the gate it
	// is held at. They are the two facts the roster tree is drawn from and they
	// are read through the seam that owns their law — taskstrip.go's
	// [taskNode.ParentID] and [taskNode.Paused], which say what fills them and
	// why the parent is a string. Empty and false is a session that has never
	// run anything adaptive, which is every session until one does.
	parent string
	paused bool
	// run and node name this row's door inside an adaptive run. A root carries
	// only run; a child carries both, so the same door opens the run's page at
	// the card this row represents. Empty is an ordinary task, unchanged.
	run, node string
	// stopped says a PERSON ended this node rather than the work ending on its
	// own (session's TaskNotice.Stopped). It rides beside the state rather than
	// replacing it — a stopped node still settles as failed — and it is what the
	// roster's ⊘ and the header's "stopped" are drawn from (stop.go).
	stopped bool
	// conflicts names the files that CLASH on a landing whose branch would not
	// fasten onto the person's (session's TaskNotice.Conflicts). An empty list
	// under a conflicted merge is the emptiness law and not a claim that nothing
	// clashed: git does not always say which files it was about.
	conflicts []string
	// decider is WHO HOLDS THIS NODE'S QUESTION right now (TaskNotice.Decider),
	// and it is the whole of what `task.settle = auto`, a person handing one card
	// over and the floor that hands it back at the end of a turn have to say to
	// each other. The zero value reads as the person, which is the only safe
	// reading of a notice that said nothing ([session.TaskAskOwner]).
	decider session.TaskAskOwner
	// ending is WHY a failed node stopped where it did, as the engine said it
	// (session's TaskNotice.Ending), and "" when it gave no reason — which is
	// every row from an older engine or checkpoint, drawn as it always was
	// (taskending.go).
	ending session.TaskEnding
	// started and ended are the record's own instants, carried by the engine on
	// every node update when it has them. began is the older live fallback,
	// derived once from the update's own Elapsed so the clock is the frame's and
	// not the event's. met is when this surface first heard of the node at all,
	// which is the honest spawn time for a node that never reached running IN THIS
	// WINDOW — see [taskNode.restored] for the case where it is not.
	started, ended time.Time
	began, met     time.Time
	// restored is a node this window never watched: the first news it had of it
	// was already settled, which is what a conversation reopened off a checkpoint
	// replays (session's task_run.go rebuilds the roster from the graph).
	//
	// THE RECORDED CLOCK ON ONE OF THESE BELONGS TO THE WORK. A current checkpoint
	// keeps the instants the work started and landed beside how long it ran, so a
	// reopened surface reads those facts directly. An older checkpoint may carry
	// only elapsed_ms; there met is the moment this terminal opened and not the
	// moment the work began — and met plus elapsed, which is what the page used to
	// date these rows by, is a landing time in the FUTURE. A window opened at 23:40
	// dated a twelve-minute node at 23:52 and the tasks place's own date filter then
	// threw it off the page altogether.
	//
	// So a restored node reads the record's start and landing time, and one whose
	// record genuinely carried neither draws neither ([taskNode.spawnedAt],
	// [taskNodeEnded]): absence for want of a figure is silence, never a guess.
	restored bool
	// elapsed is the node's final age, as the update that ended it reported.
	elapsed time.Duration
	// cost is what this node's own agent has spent, as the engine last
	// published it (session's TaskNotice.CostUSD). Zero means nobody published a
	// price, which is not the same claim as "it cost nothing" — the focus header
	// draws no figure for it rather than a $0.00 (room.go).
	cost float64
	// liveCost is the same quantity as cost, counted the other way round: the
	// sum of the turn totals the pilot lane has heard (session.EventTurnDone's
	// Usage.CostUSD) while this node ran. It exists because the notice above
	// arrives ONLY at a state change (session's task_run.go announces from
	// setState and nowhere else), so between "running" and "done" the engine's
	// figure is a number from eleven minutes ago, and a bill that stands still
	// for eleven minutes is a bill nobody believes. Read it through [taskNode.spent].
	liveCost float64
	// tokens is what this node has burned, input plus output, summed over the
	// turns the pilot has seen. Zero means NOBODY COUNTED — no notice carries a
	// token figure, so a node this surface met after it landed has none and
	// never will — and it is not the claim that a node thought for free: a
	// surface draws nothing at all for it, the way it draws nothing for an
	// unpublished price (session's task_contract.go on CostUSD).
	tokens                int
	report, branch, merge string
	// produced is WHAT THE WORK MADE, kept apart from the report above because
	// internal/session keeps the two apart and for its reason (task_result.go):
	// the report is the CARD — a landing's own sentences, which a late verdict,
	// an accept or a re-check rewrites hours afterwards — and the result is the
	// work's half alone, never composed with any of them. A surface that read
	// only the report drew a merge refusal, a decision somebody made this morning
	// and the answer itself as one undifferentiated block, because that is what
	// the report is once a landing has had a hand in it.
	//
	// EMPTY IS NOT "IT PRODUCED NOTHING". The engine omits the result when the
	// report already carries the answer exactly — every task that finished in two
	// or three short lines — and a checkpoint written before results existed has
	// none at all. The card falls back to the report in both cases, which is what
	// it drew before this field existed (taskdone.go's [taskDone.answerAndAccount]).
	produced string
	// producedWhole is where the whole of an answer this reader was handed only
	// the beginning of can be read, and producedCut says that is what happened.
	// producedHeld is the third shape: the check did not accept the work, so the
	// answer is NAMED rather than handed on and producedWhole is where it is.
	producedWhole             string
	producedCut, producedHeld bool
	// rung is WHICH COPY OF THE GROUND this node worked in and mode is what was
	// promised about it (session's TaskNotice.Rung and .Mode). They are held for
	// one reason: the landing note has to name the place the work was left, and
	// only the engine knows whether that place was a branch of the person's
	// repository or a copy of their folder ([session.GroundWord] holds the
	// words, one per rung).
	//
	// BOTH ARE KEPT AND NEVER UNSET, on the rule the branch and the model are
	// kept by: the world a node worked in was settled before its first step and
	// an update quiet about it has not moved it.
	rung session.GroundRung
	mode session.TaskMode
	// transcript is the node journal named by a far world's task row. Local
	// nodes ask their agent for this path; hosted record rows have no local agent
	// door, so the URI is the only honest address the room can hand back.
	transcript string
	changed    []string
	// tool is what the node is doing RIGHT NOW, one line, and toolBegan when it
	// started doing it. They are written by the pilot lane below and are empty
	// between calls: a row that kept the last call's name would be claiming a
	// present that has passed.
	tool      string
	toolBegan time.Time
	// kind is what sort of node this is (session's TaskNotice.Kind), and "" is
	// the ordinary one: work in a worktree. It is written once, from the first
	// update that names it, and never cleared — it is the one fact about a node
	// that is true before it starts and after it lands, and it is what keeps a
	// card from promising a branch to a node that could never have one.
	kind session.TaskKind
	// doing is the phase this node is in, in its own kind's plain words —
	// "designing", "awaiting your look" — and empty for an ordinary task, which
	// has no phases (session's TaskNotice.Doing).
	//
	// IT REPLACES THE STATE WORD RATHER THAN SITTING BESIDE IT, which is the one
	// thing that separates it from the two fields below. A node that is mending
	// or waiting is RUNNING and the surface says so; a harness being designed is
	// running too, and "running" is this package's word for it while "designing"
	// is the person's. So the phase takes the word, and the state underneath is
	// untouched.
	doing string
	// context is the NAMED WORKING CONTEXT this node's room is, in the engine's
	// own person-facing words — "designing subharness flake-triage" — and empty for an
	// ordinary node, whose room is work being watched rather than a place somebody
	// is inside (session's TaskNotice.Context).
	//
	// IT IS THE ONE THING ON THIS NODE THE PERSON'S OWN LINE IS DRAWN FROM
	// (turncontext.go). Everything else here describes the work; this describes
	// what a sentence typed in here is part of, which is what a transcript owes
	// the person reading it back.
	//
	// It is KEPT AND NEVER UNSET, the way the kind and the branch are: a node that
	// named a context is in that context for the rest of its life, and an update
	// quiet about it has not left the context. A LATER, BETTER NAME REPLACES IT —
	// a design says "designing a subharness" until its page has a name and
	// "designing subharness flake-triage" afterwards — which is the same non-monotonic naming
	// [taskRenames] exists for, and [taskRenamesContext] is its second half.
	context string
	// mending is the one plain line naming the gap the node is closing right
	// now, as the engine published it (session's TaskNotice.Mending), and it is
	// empty at every other moment of the node's life.
	//
	// THE MACHINERY IS NOT THE SURFACE'S TO MENTION. What puts a line here is a
	// private round the engine runs on work that came back short, and a person
	// has no use for the existence of that round — what they have a use for is
	// the SENTENCE: the node is still going, and this is what is left in it. So
	// the state stays running (it is running), and this is drawn as the news it
	// is rather than as a fourth state nobody asked for.
	mending string
	// waiting is why this node is not spending its time on the work, in the
	// engine's own word (session's TaskNotice.Waiting): what a queued node is
	// held behind, or the pacing a running node's calls are under. It is empty
	// at every moment the node is simply getting on with it.
	//
	// A HOLD IS NOT A STATE AND IT IS NOT A FAULT. Nothing has gone wrong with a
	// node that is waiting for a slot or being paced by its provider — it is
	// admitted, it is next, and it is going to run — so the state stays exactly
	// where the engine put it and this is drawn as the one thing a person cannot
	// otherwise tell: the difference between work that is stuck and work that is
	// merely waiting its turn. Like [taskNode.mending] it is a report of RIGHT
	// NOW, so it goes the moment the engine stops sending it.
	waiting string
	// phase is which of a running node's three lives it is in, in the engine's
	// own word (session's [session.TaskPhaseWorking], .TaskPhaseChecking,
	// .TaskPhaseRepairing) — and phaseRound, phaseRounds and phaseFinding are
	// the three facts that ride the repairing one. taskphase.go holds the words
	// a person reads for them and says why they exist.
	//
	// THEY ARRIVE ON THEIR OWN EVENT, not on an update ([session.EventTaskPhase]),
	// which is why they are not in [taskLive] with the other three: a phase move
	// does not carry a state, a clock or a cost, and the de-dup that guards a
	// redrawn row is asking about an update. They are a report of RIGHT NOW like
	// mending and waiting are, and they go the moment the node lands.
	phase        string
	phaseRound   int
	phaseRounds  int
	phaseFinding string
	// froze is the clock this node's row is drawn against while somebody is
	// standing in its room, or zero. See [app.taskNow].
	froze time.Time
}

// taskLive is everything a node is saying about its PRESENT: the phase it is in,
// the gap it is closing, and the hold it is under. They are the three fields the
// engine sends without moving the node's state (session's TaskNotice.Doing,
// .Mending and .Waiting), which makes them the three the de-dup has to look at
// by hand, and they are one comparable value so that it asks ONE question about
// them rather than a clause per field. The phase joined the other two here
// exactly as this comment said the third would — in one place, not three.
type taskLive struct{ doing, mending, waiting string }

// taskLiveLines is what an update says about the node's present, trimmed.
func taskLiveLines(notice *session.TaskNotice) taskLive {
	return taskLive{
		doing:   strings.TrimSpace(notice.Doing),
		mending: strings.TrimSpace(notice.Mending),
		waiting: strings.TrimSpace(notice.Waiting),
	}
}

// liveLines is what the node is already saying about its present.
func (n *taskNode) liveLines() taskLive {
	return taskLive{doing: n.doing, mending: n.mending, waiting: n.waiting}
}

// taskStops reports whether an update carries a STOP this node has not heard
// about yet.
//
// IT IS THE DE-DUP'S EXCEPTION FOR THE ONE THING A PERSON DID THEMSELVES, and it
// has to be an exception because a stop does not move the state. The engine cuts
// a running node's context and leaves it RUNNING for as long as the child takes
// to wind up — its own word for that window is "stopping" (session's cancel.go
// answers a second press with "task 7 is already stopping") — so the notice that
// carries the news is a running node publishing running, which is exactly the
// shape the guard above throws away. Without this clause the room's header went
// on saying "working" about work the person had just ended, until some later
// update happened to carry a bigger figure with it, and the honest word arrived
// whenever the accounting felt like it.
//
// It can only ever fire ONCE per node: [app.taskUpdate] never un-stops one.
func taskStops(notice *session.TaskNotice, node *taskNode) bool {
	return notice.Stopped && !node.stopped
}

// taskPauses reports whether an update moves this node ON or OFF a gate a person
// has to open (session's TaskNotice.Paused).
//
// IT IS THE DE-DUP'S FIFTH EXCEPTION, and it is one for [taskStops]' reason: the
// gate does not move the state. An adaptive run whose tank empties goes on
// publishing `running` — its in-flight workers are still working — so the notice
// that carries the news is a running row publishing running, which is exactly the
// shape the guard throws away. Without this the column drew a spinner over a run
// that had stopped and was waiting to be told what to do, until some later row
// happened to differ for another reason.
//
// Unlike the stop it can fire in BOTH directions: a gate that is answered comes
// down, and a row still asking after the person answered is the same defect the
// other way round.
func taskPauses(notice *session.TaskNotice, node *taskNode) bool {
	return notice.Paused != node.paused
}

// taskRenames reports whether an update carries a NAME this node does not have.
//
// IT IS THE DE-DUP'S FOURTH EXCEPTION and the only one that is not about the
// present ([app.taskUpdate] says why it is one anyway). An EMPTY title is never
// a rename: a producer that says nothing about the name has not renamed
// anything, and taking the empty string would put "task 19" back over a row that
// already knows what it is.
func taskRenames(notice *session.TaskNotice, node *taskNode) bool {
	title := strings.TrimSpace(notice.Title)
	return title != "" && title != node.label
}

// taskRenamesContext reports whether an update names a WORKING CONTEXT this node
// does not have. It is [taskRenames]'s law applied to the other name a node
// carries, and it is a separate question for the same reason that one is: the
// naming is not monotonic. A design says "designing a subharness" from the
// moment it is admitted and "designing subharness flake-triage" the moment its page has a
// name, both while it is running — so a guard that only ever looked at the state
// would throw away the one update that carries the real name, and every line the
// person typed afterwards would be marked with the placeholder for the rest of
// the conversation. An EMPTY context is never a rename, for taskRenames' reason.
func taskRenamesContext(notice *session.TaskNotice, node *taskNode) bool {
	word := strings.TrimSpace(notice.Context)
	return word != "" && word != node.context
}

// spawnedAt is when this node's work started, in wall-clock: the moment it
// began running, or — for a node that failed before it ever ran — the moment
// this surface first met it. It is what the completion card's "14:02" says, and
// it is deliberately not the moment the proposal was made: a question asked at
// 13:58 and answered at 14:02 started at 14:02.
//
// A NODE THIS WINDOW NEVER WATCHED READS THE RECORD'S START. Only a record that
// genuinely carries none answers the zero time so the card draws no stamp at
// all. Meeting a settled node is meeting it AFTER the fact — the terminal's open
// time is not the work's start, and a card that stamped one with the other told a
// person the work happened at the moment they sat down ([taskNode.restored]
// carries the whole reasoning).
func (n *taskNode) spawnedAt() time.Time {
	if !n.started.IsZero() {
		return n.started
	}
	if !n.began.IsZero() {
		return n.began
	}
	if n.restored {
		return time.Time{}
	}
	return n.met
}

// spent is what this node has cost, in dollars, from whichever of its two lanes
// knows the most — the engine's published figure, or the pilot's running sum.
// Zero means nobody has priced it, which is not "it was free", and a surface
// that draws this draws nothing rather than a $0.00 it made up.
//
// THE TWO FIGURES ARE THE SAME MONEY AND ARE NEVER ADDED. session's spend()
// answers a running node by asking its child agent for Usage().CostUSD — the
// cumulative meter — and a turn's Usage.CostUSD is one step of that same meter,
// so a surface that summed the notice and the turns would bill every turn twice.
// They are RECONCILED: the larger of the two is the more recent reading of one
// number, and taking the larger is also what makes this monotonic, because both
// halves only ever grow.
//
// ONCE THE NODE LANDS THE NOTICE WINS OUTRIGHT. The landing figure is frozen
// from the engine's own books (session's foldTaskUsage), and a surface holding a
// larger guess after the work is over would be disputing the bill rather than
// reporting it. The cost of that rule is the one case where this can read low: a
// node that was already running when this surface attached has spend the pilot
// never heard, so the row stays on the engine's older figure until the run's new
// turns overtake it and the landing corrects it. Reading low for a minute is the
// cheaper wrong — the alternative overstates what a person is being charged.
func (n *taskNode) spent() float64 {
	switch n.state {
	case session.TaskDone, session.TaskFailed, session.TaskUnverified:
		if n.cost > 0 {
			return n.cost
		}
	}
	if n.liveCost > n.cost {
		return n.liveCost
	}
	return n.cost
}

// A NODE NEVER LEAVES THE ROSTER. It used to: a finished node whose branch had
// come home dropped off the rail, because the rail was a presence list and a row
// that never left would have turned it into a log. The column is the session's
// record of its own work now, and what it does with a settled node instead is
// GROUP it — see task.go's roster section, and [app.railGroupOf] for the one
// placement that is not simply the engine's state read out.

// The merge words session publishes (task_run.go's mergeMerged and friends),
// restated here because the surface reads them and internal/session exports
// them nowhere.
//
// THEY ARE THE ENGINE'S WORDS AND NOT THIS SURFACE'S, and nothing draws one:
// every reader goes through [mergeScreenWord] below.
const (
	mergeWordMerged     = "merged"
	mergeWordConflicted = "conflicted"
	mergeWordInPlace    = "inplace"
	mergeWordAborted    = "aborted"
	mergeWordKept       = "kept"
)

// ── WHERE THE WORK LANDED, IN A PERSON'S WORDS ──────────────────────────────
//
// mergeScreenWords is THE TABLE: one screen word for every merge word the
// engine can publish, and the ONLY place this surface translates them.
//
// IT EXISTS BECAUSE THREE READERS WERE EACH PRINTING THE ENGINE'S TOKEN. The
// room's header, the settled card's tail and the roster's row all reached for
// [taskNode.merge] and drew whatever string was in it, with a case for the two
// words that needed translating and a fall-through for the rest — so a person
// whose task ran in a folder with no repository read `inplace` on all three
// (`✓ run these shell · inplace · 55s`, seen on a real run). That is the
// machinery's own vocabulary on a person's screen, which this codebase bans,
// and it was reachable on a perfectly ordinary launch: every task in a
// directory that is not a repository lands that way.
//
// A TABLE AND NOT A SWITCH, so the guarantee is checkable. A switch says what
// happens to the words somebody thought of; a table can be walked against the
// engine's own list, and taskwords_test.go's structural test does exactly that
// — it reads internal/session's const block and fails when a word in it has no
// line here. That is what makes "the next token cannot leak" a fact rather than
// a hope.
//
// AND NOBODY FALLS BACK TO THE TOKEN. A word this build has never heard of — an
// engine newer than the binary reading it — draws the honest [roomDoneWord]
// instead, because "this work is over" is true of every merge outcome there can
// be and a token nobody can read is true of nothing.
var mergeScreenWords = map[string]string{
	// Two of the five already mean on screen what they mean in the branch.
	mergeWordMerged:     mergeWordMerged,
	mergeWordConflicted: mergeWordConflicted,
	// "aborted" reads as a crash and is almost never one: the commonest way a
	// node wears it is that a person stopped it or it spent the steps it was
	// given (the rail hangs the branch clause off it where a row has the cells
	// for it, [app.railUnder]).
	mergeWordAborted: taskStoppedWord,
	// "kept" says what the engine did with a ref; the person-facing fact is
	// that a finished branch is waiting for them to take it.
	mergeWordKept: taskBranchKept,
	// "inplace" is not an outcome at all — it is WHERE the work is. There was no
	// branch, so nothing had to come home, and the fact a person needs is that
	// their own files were the ones edited.
	mergeWordInPlace: taskInPlaceLanding,
}

// taskInPlaceLanding is what a task with no branch to bring home says: the
// person's own folder, in the words internal/session already uses for that
// ground (groundladder.go's [session.GroundWord]).
//
// IT IS DERIVED AND NOT TYPED OUT, because the settled card, the project's
// record and this line all name one place, and a phrase spelled twice is two
// phrasings a person is asked to reconcile. The lead word is this surface's,
// because the slot here is a clause about the work and the table's line is a
// noun.
var taskInPlaceLanding = "in " + session.GroundWord(session.GroundRungHere, "")

// mergeScreenWord is the door onto the table: what to draw for one merge word,
// and "" when the engine published none. Every reader of [taskNode.merge] goes
// through here.
func mergeScreenWord(merge string) string {
	if merge == "" {
		return ""
	}
	if word, ok := mergeScreenWords[merge]; ok {
		return word
	}
	return roomDoneWord
}

// taskStoppedWord is what a stopped node is drawn with.
//
// A node stops for reasons that are nobody's failure — a person pressed c on its
// room, it spent the steps it was given, its deadline came — and session marks
// every one of them "aborted", which is a word a person reads as "it crashed".
// It did not: it stopped, and its branch was kept precisely so the work is still
// there.
//
// THE BRANCH IS A FACT AND NOT PART OF THE STATE. This constant had a sibling,
// `stopped — branch kept`, which welded the two together and made the branch
// unsayable about any other landing; where the work was left is now its own
// clause on the row ([taskBranchKept], and the landing card's fact line), and
// the state is one word (docs/design/task-states/DESIGN.md).
const taskStoppedWord = "stopped"

// taskBranchKept is WHERE THE WORK WAS LEFT, and it is a fact about source
// control rather than a state: this node wears session's "aborted" merge and
// nothing about it stopped.
//
// THE STATE WORDS THAT USED TO STAND BESIDE IT ARE GONE. `needs your look`,
// `finished, but needs your look` and `finished — look it over` were three
// spellings of one reading — the machine has done what it can, and somebody has
// to say something — which internal/session now spells once as `your call` plus
// the reason for it (docs/design/task-states/DESIGN.md). They were this file's
// own vocabulary, and the roster, the record and home each had a different one.
const taskBranchKept = "branch kept"

// taskFinishingWord is what a node says while it is closing a gap in work it has
// otherwise finished (session's TaskNotice.Mending, carried on [taskNode.mending]).
//
// IT IS NOT A STATE AND IT DOES NOT REPLACE ONE. The node is running — the
// engine says so on every one of these updates — and "finishing" is the surface
// saying WHICH PART of running this is, in the one word that is true of it from
// the outside: the work is nearly there and something is being tied off. What is
// being tied off is the sentence beside it, in the engine's own plain words.
const taskFinishingWord = "finishing"

// taskHeldWord is what a node says while it is HELD — admitted, next, and
// spending its time on something that is not the work (session's
// TaskNotice.Waiting, carried on [taskNode.waiting]).
//
// IT IS THE SAME SPLIT [taskFinishingWord] IS BUILT ON: the word is this
// surface's, saying which part of queued or running this is, and what follows
// the separator is the engine's own reason, verbatim. And it is the same law —
// the hold is not a state, nothing about the node moved, and a person is owed
// the difference between "this has been sitting there for four minutes doing
// nothing" and "this has been sitting there for four minutes because the
// machine is full".
//
// The identifier is "held" and the word is "waiting" because the proposal
// meter's own sentence ([taskWaitingWord]) is about a different moment
// altogether: a proposal nobody has agreed to yet, which says what answering it
// would do rather than that it is waiting.
const taskHeldWord = "waiting"

// The reasons the engine holds a node with (session's TaskNotice.Waiting),
// restated here for the same reason the merge words above are: the surface
// reads them out and internal/session exports them nowhere.
//
// THEY ARE READ OUT AS THEY STAND. Unlike "aborted", each of these three means
// on screen exactly what it means in the engine — a cap that is full, a machine
// under load, a provider pacing the calls — and all three are already the plain
// words a person would use for them. There is nothing to translate.
const (
	waitWordSlot    = "slot"
	waitWordMachine = "machine busy"
	waitWordRate    = "rate limited"
)

// taskAgent is the slice of *session.Agent this file needs, and it is asserted
// rather than added to [Agent].
//
// The reason is the seam's own: the task contract is OPTIONAL. A surface can be
// driven by a scripted agent that has never heard of a task (every other test in
// this package is), and widening the package interface would make a session
// without a tasker un-representable — which is exactly the thing the door
// currently hands us on a build with the engine turned off.
type taskAgent interface {
	// ResolveTask answers one proposal: approved as briefed, approved with a
	// correction appended, or denied.
	ResolveTask(id uint64, answer session.TaskAnswer)
	// HoldTask removes the proposal's clock while leaving its answer pending.
	HoldTask(id uint64)
	// TaskUpdates is the STANDING subscription — one channel for the session's
	// whole life, because a node's most important event happens when no turn is
	// open (session's task_run.go).
	TaskUpdates() <-chan session.Event
	// PendingTasks names the proposals the engine is still waiting on. It is
	// how this surface finds out that the card on screen is about a question
	// nobody is asking any more.
	PendingTasks() []uint64
}

// DrawsTasks reports whether an agent carries the WHOLE task seam this surface
// needs: the standing lane, the pending reading, and the door an answer goes
// back through.
//
// IT IS EXPORTED FOR ONE REASON. internal/remote implements this surface's agent
// over a wire and cannot import this package to check that it kept up, so the
// door that wires the two together asserts it instead (cmd/aforge). The seam is
// ALL-OR-NOTHING — [app.tasker] is one type assertion — so a single method
// missing on the far half is not a feature that degrades, it is a rail that is
// never subscribed and never draws a row. That is exactly what happened: the
// wire carried three of the four task doors and not `ResolveTask`, and a task
// started by hand over a connection ran to completion with an empty column
// beside it.
func DrawsTasks(agent Agent) bool {
	_, ok := agent.(taskAgent)
	return ok
}

// tasker is the agent under this surface, when it has a tasker at all.
func (a *app) tasker() (taskAgent, bool) {
	agent, ok := a.agent.(taskAgent)
	return agent, ok
}

// ── the update lane ─────────────────────────────────────────────────────────

// watchTasks opens the standing subscription and starts pumping it into the
// program loop. It is called once at boot and again wherever the agent under
// this surface is REPLACED (/new), because the channel belongs to the agent that
// handed it over.
//
// The generation is the same device the turn stream uses: a lane from an agent
// that has been closed may still deliver, and an event from a conversation that
// no longer exists must not upsert a node into the one that does.
func (a *app) watchTasks() tea.Cmd {
	agent, ok := a.tasker()
	if !ok {
		return nil
	}
	a.taskGen++
	a.taskLane, a.stops.tasks = taskLaneOf(agent)
	return waitTask(a.taskLane, a.taskGen)
}

// leavableTasker is the standing task lane WITH A WAY OUT OF IT (session's
// task_run.go). It is asserted separately from [taskAgent] rather than folded
// into it for that interface's own reason: a scripted agent in this package's
// tests offers the lane and has never heard of the door, and a surface driven
// by one must stay representable.
type leavableTasker interface {
	WatchTaskUpdates() (<-chan session.Event, func())
}

// taskLaneOf opens the lane and hands back whatever way out the agent offers.
//
// A NIL STOP IS AN AGENT THAT CANNOT BE LEFT, and the surface may then only
// abandon the channel — which is what every door did before conversations could
// be detached, and which is safe exactly as long as the agent is being closed in
// the same breath (switcher.go's [laneStops] states the cost when it is not).
func taskLaneOf(agent taskAgent) (<-chan session.Event, func()) {
	if leavable, ok := agent.(leavableTasker); ok {
		return leavable.WatchTaskUpdates()
	}
	return agent.TaskUpdates(), nil
}

// waitTask takes one event off the standing lane and asks for the next.
func waitTask(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return taskLaneClosedMsg{gen: gen}
		}
		return taskEventMsg{gen: gen, ev: ev}
	}
}

// taskEvent folds one event from the standing lane in and re-arms the pump.
//
// AN UPDATE IS ALSO WHAT MAKES THE "@" LIST STALE. A node that has just started
// belongs under "running" and a node that has just landed belongs in the project
// index, and this is the one lane that knows either happened (taskmention.go).
func (a *app) taskEvent(ev session.Event) tea.Cmd {
	var pilot, mentions tea.Cmd
	switch ev.Kind {
	case session.EventTaskProposal:
		a.proposeTask(ev)
	case session.EventTaskUpdate:
		pilot = a.taskUpdate(ev)
		mentions = a.refreshTasks()
	case session.EventTaskPhase:
		a.taskPhaseMoved(ev)
	case session.EventJobUpdate:
		// A CONVERSATION REOPENED REDRAWS THE JOBS IT RAN, and this lane is the
		// only one that can carry them: the engine replays every restored job
		// row onto it when it wakes a checkpointed conversation (session's
		// task_run.go), long after the turn that started them ended. Without
		// this case the column beside a transcript full of jobs draws nothing,
		// and because the section is the only door to a job's page there is no
		// way left to read the log — which is the one thing a person coming
		// back the next morning actually wants.
		if ev.Job != nil && a.jobUpdate(*ev.Job) {
			a.touch()
		}
	case session.EventStandingProposal:
		a.proposeStanding(ev)
	case session.EventTakeover:
		// ANOTHER WINDOW HAS ASKED FOR THIS CONVERSATION and the engine has
		// already agreed — it announces this only when no turn is in flight
		// (internal/session's takeover.go). This window lets go, and the lane
		// pump is deliberately NOT re-armed below: the agent it was reading is
		// about to be closed.
		return a.takeOver()
	case session.EventMoved:
		// AND THE OTHER ROAD: the engine holding this conversation has told this
		// window that another one has opened it (takeover.go's [app.movedAway]).
		// The lane pump is deliberately NOT re-armed below for [session.EventTakeover]'s
		// reason — the agent it was reading is about to be let go of.
		return a.movedAway(ev.Text)
	case session.EventStandingUpdate:
		// AN ITEM FIRES WITH NOBODY IN THE ROOM, which is the whole of the
		// ambient side — so a FIRING reaches this surface here and only here,
		// on the lane that outlives every turn (standing.go, and session's
		// [Agent.emitStandingNews] for why the turn's hub cannot carry it). What
		// was waiting in the inbox while the window was shut arrives on this lane
		// too, as the first thing on it.
		a.standingUpdate(ev)
	}
	return tea.Batch(waitTask(a.taskLane, a.taskGen), pilot, a.wake(), mentions)
}

// ── the pilot lanes ─────────────────────────────────────────────────────────
//
// A RUNNING NODE IS WATCHED WHETHER OR NOT ANYBODY IS IN ITS ROOM.
//
// The standing subscription (above) carries a node's LIFE — queued, running,
// done — and it is silent for the whole of the interesting part: a node runs
// for eleven minutes and says nothing on that lane between minute zero and
// minute eleven. The rail's answer to "is this alive" was therefore a spinner
// and a clock counting the node's own age, which are both true of a node that
// has been stuck on one `go test` for four minutes and of one that is calling a
// tool every second.
//
// A pilot is the room's own door ([taskRoomAgent.WatchTask]) opened WITHOUT a
// room: one watcher per running node, kept for exactly as long as the node
// runs, folding three facts out of the stream and dropping everything else —
// what the node is doing, when it started doing it, and what the step it just
// finished cost. That is what the rail's elapsed clock is measured against (see
// [app.taskClock]) and what its tokens and its price are counted from (see
// [taskNode.spent]).
//
// THE SPEND IS HERE BECAUSE THE STANDING LANE IS SILENT. A node's updates carry
// a price and arrive only at a state change, so the rail's figure would be the
// one the node started with for the whole of the run; the child's own turns end
// several times a minute (session's task_run.go drives it through Submit after
// Submit), and each of those says what it has cost so far.
//
// IT COSTS A MESSAGE PER EVENT AND A REPAINT PER TOOL CALL, and the second half
// of that sentence is the design: a node's text deltas arrive here and are
// dropped without touching the frame, because the rail does not draw what a
// node SAYS. Only a call beginning or ending is news to a 24-column row.
type taskPilot struct {
	id   uint64
	gen  int
	lane <-chan session.Event
	// stop LEAVES that lane, and is nil for an agent that offers no way out of
	// one. A pilot is landed while its conversation goes on running — and, under
	// a switch, while the whole rail is put down with the agent still alive — so
	// there is nobody to close the channel for us (switcher.go's [laneStops]).
	stop func()
}

// flyPilot opens the watcher on one running node, or answers nil when there is
// nothing to watch it with — an agent with no room doors, a node already being
// watched, an id the engine does not know.
func (a *app) flyPilot(id uint64) tea.Cmd {
	doors, ok := a.roomDoors()
	if !ok || a.pilots[id] != nil {
		return nil
	}
	lane, stop, err := roomLaneOf(doors, id)
	if err != nil {
		return nil
	}
	if a.pilots == nil {
		a.pilots = map[uint64]*taskPilot{}
	}
	a.pilotGen++
	pilot := &taskPilot{id: id, gen: a.pilotGen, lane: lane, stop: stop}
	a.pilots[id] = pilot
	return waitPilot(lane, pilot.gen, id)
}

// waitPilot takes one event off a pilot's lane and asks for the next.
func waitPilot(ch <-chan session.Event, gen int, id uint64) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return taskPilotClosedMsg{gen: gen, id: id}
		}
		return taskPilotMsg{gen: gen, id: id, ev: ev}
	}
}

// pilotEvent folds one watched node's event in and re-arms its lane.
//
// The generation is the same device every other lane on this surface uses: a
// pilot that was landed while its channel still had events in flight must not
// write the current tool of a node that has finished.
func (a *app) pilotEvent(msg taskPilotMsg) tea.Cmd {
	pilot := a.pilots[msg.id]
	if pilot == nil || pilot.gen != msg.gen {
		return nil
	}
	if node := a.tasks[msg.id]; node != nil {
		switch msg.ev.Kind {
		case session.EventToolAnnounced:
			// ASKED FOR, NOT STARTED — so the name changes and the clock does not
			// start. It is the tool row's own distinction (toolview.go's
			// toolQueued), and it matters here for the same reason: a clock that
			// started at the announcement would be measuring how long a response
			// took to stream.
			node.tool, node.toolBegan = taskCallWord(msg.ev.Tool, msg.ev.Args, msg.ev.Hint), time.Time{}
			a.touch()
		case session.EventToolBegin:
			node.tool, node.toolBegan = taskCallWord(msg.ev.Tool, msg.ev.Args, msg.ev.Hint), a.now()
			a.touch()
		case session.EventToolEnd, session.EventToolFailed:
			node.tool, node.toolBegan = "", time.Time{}
			a.touch()
		case session.EventTurnDone:
			// ONE STEP'S ACCOUNTING, ADDED TO THE NODE'S. The child is driven
			// through many Submits and this is the end of one of them, so the
			// figures are a turn's own and the running totals are the sum of them
			// ([taskNode.tokens], [taskNode.liveCost] — and read the reconciliation
			// law on [taskNode.spent] before adding a second lane to either).
			//
			// A TURN THAT PRICED NOTHING IS NOT NEWS. An unpriced model and a
			// provider that returned no usage both land here as zeroes, and adding
			// zero to a total is a repaint of a frame that has not changed — which
			// this lane, one message per event, cannot afford to spend.
			used := msg.ev.Usage
			if used.Input+used.Output > 0 || used.CostUSD > 0 {
				node.tokens += used.Input + used.Output
				node.liveCost += used.CostUSD
				a.touch()
			}
		}
	}
	return tea.Batch(waitPilot(pilot.lane, pilot.gen, pilot.id), a.wake())
}

// taskCallWord is one call in the ONE LINE a 24-cell rail row has for it: the
// tool's name with the verb said once, and the payload's own target ahead of the
// session's gloss.
//
// It is composed from the same two renderers the conversation's tool line is
// built from (toolview.go's [toolWords], toolstat.go's [toolTarget]) rather than
// being a rendering of its own — which is the law room.go states in its header
// after giving up a one-line renderer of exactly this shape. The rail cannot use
// the tool LINE itself: that block is two cells of rail, a right-aligned stat
// and an expansion, and this column has room for none of them.
func taskCallWord(tool, args, hint string) string {
	name, _ := toolWords(tool, hint)
	if target := toolTarget(tool, args, hint); target != "" {
		return name + " " + target
	}
	return name
}

// landPilot forgets one node's watcher. The lane closes itself when the node
// reaches its final state; this is the surface's half, and it is called from
// the update that says so rather than waiting for the close, so the row stops
// claiming a current call the instant the work is over.
func (a *app) landPilot(id uint64) {
	if node := a.tasks[id]; node != nil {
		node.tool, node.toolBegan = "", time.Time{}
	}
	if pilot := a.pilots[id]; pilot != nil && pilot.stop != nil {
		// The lane usually closes itself here — the node is finished — and
		// saying goodbye to a stream that has already ended is saying nothing
		// (session's [eventHub.drop] answers a stream it no longer holds by
		// leaving it alone). It is said anyway for the other order: a landing
		// the surface learned from the update rather than from the close.
		pilot.stop()
	}
	delete(a.pilots, id)
}

// ── the proposal ────────────────────────────────────────────────────────────

// proposeTask draws the decision moment (session.EventTaskProposal).
//
// The card lands in the transcript and takes the keyboard's answer lane. One
// question at a time is the engine's own serialization (its ask blocks the tool
// call that raised it, exactly as consent's does), and a second card arriving
// anyway is not dropped: the older one settles as expired, because a question
// that can no longer be answered must stop looking like one.
// taskTool is the call a proposal comes out of (session's task.go). The name is
// written down once here because two different events are recognized by it: the
// forming fragments this file opens a card on, and nothing else on this surface
// may guess at the spelling.
const taskTool = "propose_task"

// formTask opens the SPAWN CARD the moment a propose_task call starts arriving
// (session.EventToolForming), and fills its title in as the wire says it.
//
// THE DEFECT THIS CLOSES: a groomed proposal is the longest call the model
// makes — a title, a summary, a brief of several paragraphs, an acceptance
// condition — and every one of those seconds used to be silence, ending in a
// question-hued block appearing whole under whatever the person was reading.
// The card now grows where it will stand: dim, titled as soon as the title
// field closes, with "forming…" where the countdown will be.
//
// It is one card per call and it is never [app.task]: this is not a question
// yet, and a block that took the answer lane before there was anything to
// answer would be a keyboard pointed at nothing.
func (a *app) formTask(ev session.Event) {
	card := a.formingCard()
	if card == nil {
		card = &taskCard{
			forming: true, callID: ev.CallID, born: a.now(),
		}
		a.closeLive()
		a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: card})
		a.follow()
	} else if card.callID != "" && ev.CallID != "" && card.callID != ev.CallID {
		// A DIFFERENT CALL STARTS A DIFFERENT CLOCK. A refused attempt may be
		// followed by a corrected call in the same turn, and its seconds do not
		// belong under the corrected title. Both ids must be present: an id can
		// land after the first fragment, and learning it is not a new attempt.
		card.callID = ev.CallID
		card.born = a.now()
	} else {
		card.callID = firstNonEmpty(ev.CallID, card.callID)
	}
	// The gloss is "propose_task <title>" once the title field has CLOSED
	// (session's toolhint.go), and empty until then — so the name lands once,
	// whole, and the card never shows half a title.
	if _, title := toolWords(ev.Tool, ev.Hint); title != "" && title != card.title {
		card.title = title
		card.name = taskTitleOf(title, "", 0)
		a.markCardStale(card)
	}
	a.touch()
}

// formingCard is the card a propose_task call is currently forming into, or
// nil. Its lookup walks newest first: a forming card is by construction the
// most recent one on the live turn, and every settled card behind it is
// somebody else's.
func (a *app) formingCard() *taskCard {
	if at := a.formingCardAt(); at >= 0 {
		return a.entries[at].card
	}
	return nil
}

// formingCardLive reports whether the transcript currently carries a proposal
// whose arriving row moves. It is asked here rather than inferred from the turn
// because the clock that draws it must not depend on a second fact staying true.
// It must never be a cached bool: rewind and detach replace [app.entries]
// wholesale, and a stale flag would ask for frames forever after the card left.
func (a *app) formingCardLive() bool { return a.formingCard() != nil }

// dropFormingCard resolves a card whose call never arrived — the turn ended, or
// the stream died, between two fragments of a propose_task.
//
// It SETTLES rather than disappears, for [app.dropForming]'s reason: the model
// began asking for work and did not finish, which is a thing that happened, and
// the two-row settled card is exactly the shape this surface keeps such facts
// in.
func (a *app) dropFormingCard() {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind == entryTask && e.card != nil && e.card.forming && !e.card.settled() {
			e.card.verdict = taskFormingLost
			e.stale = true
		}
	}
}

// dropRetryingFormingCard removes the partial proposal drawn by an attempt the
// session has cut and discarded. Unlike [app.dropFormingCard], this does not
// settle the block: the transcript contains no call to account for, and the
// replacement attempt must begin with a fresh card and a fresh argument stream.
//
// The card is normally the newest entry because [app.formTask] closes live text
// before appending it. The empty-entry fallback preserves every later index if
// a person steered while the call was arriving; removing from the middle would
// move selection and hover targets that are held by index.
func (a *app) dropRetryingFormingCard() {
	at := a.formingCardAt()
	if at < 0 {
		return
	}
	if at == len(a.entries)-1 {
		a.entries = a.entries[:at]
	} else {
		a.entries[at].card = nil
		a.entries[at].stale = true
	}
	a.touch()
}

// refuseFormingCard settles the block when propose_task returns without ever
// raising a proposal. That result is a third ending, distinct from a turn or a
// stream dying halfway through the call, so it keeps its own words.
func (a *app) refuseFormingCard() {
	card := a.formingCard()
	if card == nil {
		return
	}
	card.verdict = taskFormingRefused
	a.markCardStale(card)
	a.touch()
}

// proposeTask takes one session.EventTaskProposal.
//
// THE BLOCK ASKS AND THIS ROW IS WHAT IT IS ABOUT (question.go). The engine
// sends the proposal twice on purpose — this card, which carries the whole
// assignment, and the same moment as a [session.Question] on the questions lane
// — and the two halves are drawn in the two places each belongs: the brief in
// the transcript, where it is part of what happened and can still be read a
// week later, and the ASKING above the box, where every other decision on this
// surface is put. What used to be here as well was a second copy of the asking:
// a choices row, a draining meter and a keyboard lane, all of them a decision
// drawn in a place no other decision is drawn.
//
// The card is REUSED where the id is one this window already has. Holding the
// clock is an update to the proposal every surface has ([session.Agent.
// HoldTask]) and not a second proposal, and the deadline it zeroes travels to
// the question as well — it is the question that draws the clock now.
func (a *app) proposeTask(ev session.Event) {
	notice := ev.Task
	if notice == nil {
		return
	}
	// A PROPOSAL THIS WINDOW HAS ALREADY SEEN IS AN UPDATE, NEVER A SECOND
	// PROPOSAL, and the id is the whole of the test — not "the id AND still
	// open", which is what this asked before a real screen showed what that
	// costs. The engine rebroadcasts the notice whenever the clock is held
	// ([session.Agent.HoldTask]), and the block holds it on the first key a
	// question reads — so the rebroadcast arrives a moment AFTER the answer that
	// caused it, and a window that took it for a new proposal drew the card a
	// second time, raised the question a second time, and then wrote a second
	// receipt when the engine's answer came back to a question that was open
	// again. One proposal, two blocks, two receipts, all from one keystroke.
	if card := a.cardFor(notice.ID); card != nil {
		if card.settled() {
			return
		}
		card.deadline = notice.Deadline
		a.setTaskDeadline(notice.ID, notice.Deadline)
		a.markCardStale(card)
		a.touch()
		return
	}
	if a.task != nil && !a.task.settled() {
		a.task.verdict = taskExpiredWord
	}
	title := strings.TrimSpace(notice.Title)
	summary := strings.TrimSpace(notice.Summary)
	brief := strings.TrimSpace(notice.Brief)
	name := taskTitleOf(title, firstNonEmpty(summary, brief), notice.ID)
	card := &taskCard{
		id:         notice.ID,
		title:      title,
		summary:    summary,
		brief:      brief,
		acceptance: strings.TrimSpace(notice.Acceptance),
		where:      strings.TrimSpace(notice.Where),
		name:       name,
		sub:        taskSubtitleOf(name, firstNonEmpty(summary, brief)),
		ident:      identFor(notice.ID),
		dependsOn:  notice.DependsOn,
		model:      strings.TrimSpace(notice.Model),
		elsewhere:  strings.TrimSpace(notice.Elsewhere),
		deadline:   notice.Deadline,
		born:       a.now(),
	}
	a.task = card
	a.closeLive()
	// The typed lists follow the draft, and the draft is where the correction
	// gets typed: a completion list left open under a question answering to
	// digits is two readers for one keystroke (consent.go makes the same call
	// for the same reason).
	a.closeLists()
	// THE BLOCK THE CALL WAS FORMING INTO BECOMES THIS ONE. The person has been
	// watching this proposal arrive; the assignment is that block's next state,
	// not a second copy of it underneath ([app.formTask]).
	if at := a.formingCardAt(); at >= 0 {
		a.entries[at].card = card
		a.entries[at].turn = a.turn
		a.entries[at].stale = true
	} else {
		a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: card})
	}
	// AND THE QUESTION IS RAISED FROM HERE TOO, dressed with the lane's own two
	// hands. It arrives on the questions lane as well and the block replaces by
	// token, so whichever gets here first draws and the second is the same
	// decision rather than a second one — consent.go's own bargain, and it is
	// what lets this window put the transcript card and the engine's answer
	// together without a second resolver.
	a.raiseQuestion(a.taskShown(notice))
	a.follow()
	a.touch()
}

// taskShown is one proposal as the block holds it: the engine's own question
// object, plus the two things it cannot carry.
//
//   - WHAT THIS PROGRAM DOES ABOUT AN ANSWER ([app.taskAnswered]) — the card in
//     the transcript keeps what was decided, and a yes opens the forming block
//     the approved brief is shaped in.
//   - WHAT A KEYSTROKE MEANS TO THE CLOCK ([app.taskHeld]). A proposal is the
//     one question on this surface whose silence ANSWERS, so the moment there
//     is somebody at the keyboard the engine is told to stop counting.
func (a *app) taskShown(notice *session.TaskNotice) questionShown {
	id := notice.ID
	return questionShown{
		question: a.taskQuestion(notice),
		answered: func(answer session.Answer) session.Answer {
			return a.taskAnswered(id, answer)
		},
		held: func() { a.holdTask(id) },
	}
}

// taskQuestion is the proposal as the object every surface draws, and it is the
// engine's own builder said again (session's [Agent.proposalQuestion]).
//
// The two must stay ONE SENTENCE, for consent.go's reason: the block keys a
// question by its lane and its id, so this one and the one that arrives on the
// questions lane a moment later are the SAME question, and two builders that
// drifted would make them two — one replacing the other on screen while
// somebody was part-way through reading it.
func (a *app) taskQuestion(notice *session.TaskNotice) session.Question {
	built := session.Question{
		ID:       notice.ID,
		Kind:     session.QuestionTask,
		Ask:      session.AskPermission,
		Form:     session.FormCard,
		Asker:    session.Asker{Kind: session.AskerModel},
		Head:     session.TaskProposalLead + strings.TrimSpace(notice.Title),
		Reason:   strings.TrimSpace(notice.Summary),
		Subject:  session.SubjectRef{Kind: session.SubjectNode, ID: notice.ID, Name: strings.TrimSpace(notice.Title)},
		Options:  session.AnswerOptions(session.QuestionTask),
		Stakes:   session.StakesCostly,
		Blocking: session.Blocking{Turn: true},
		Deadline: notice.Deadline,
		Asked:    a.now(),
		// THE MODEL SHORTLIST IS THE ENGINE'S SHAPE, ASKED FOR RATHER THAN
		// REBUILT. A proposal whose `model` argument fit more than one model this
		// install has carries a hole for it, and the answer's own map carries the
		// chosen one back to [session.ResolveTask] — see [session.TaskModelShape].
		Input: session.TaskModelShape(*notice),
	}
	if !notice.Deadline.IsZero() {
		built.Pick = &session.Pick{
			Key:        "1",
			Reason:     session.TaskProposalPickReason,
			Confidence: session.ConfidenceFairly,
		}
		built.Policy = session.Policy{
			Kind:  session.PolicyRecommendThenAuto,
			After: notice.Deadline.Sub(a.now()),
		}
	}
	return built
}

// setTaskDeadline moves the clock on the question this proposal raised, which
// is where the clock is drawn.
//
// A HELD PROPOSAL STOPS COUNTING EVERYWHERE AT ONCE. The engine rebroadcasts
// the notice with a zero deadline and nothing else ([session.Agent.HoldTask]),
// so the question object this window is holding would otherwise go on counting
// down to a moment nothing is waiting for — which is the one thing a countdown
// may never do.
func (a *app) setTaskDeadline(id uint64, deadline time.Time) {
	for i := range a.questions {
		q := &a.questions[i]
		if q.question.Kind != session.QuestionTask || q.question.ID != id {
			continue
		}
		q.question.Deadline = deadline
		if deadline.IsZero() {
			// THE CLOCK GOES AND THE RECOMMENDATION STAYS. What a held proposal
			// stops being is a question that answers itself; what it does not
			// stop being is one the asker has an opinion about — so the policy
			// and the deadline are cleared and the pick is not, and `enter` goes
			// on taking the answer the row is marked with.
			q.question.Policy = session.Policy{}
		}
		a.touch()
		return
	}
}

// dropTaskQuestion takes the proposal's question off the block when the answer
// came from somewhere that is not an answer: the clock, the end of the turn,
// another window.
//
// IT IS A WITHDRAWAL AND NOT AN ANSWER, which is the whole distinction: nobody
// decided anything here, so nothing is recorded as though somebody had. The
// reason is the engine's own sentence for the same event (session's
// [questionGoneReason]).
func (a *app) dropTaskQuestion(id uint64, reason string) {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionTask || open.question.ID != id {
			continue
		}
		a.withdrawQuestion(open.question, reason)
		return
	}
}

// formingCardAt is [app.formingCard]'s index, for the one caller that has to
// replace the card rather than read it.
func (a *app) formingCardAt() int {
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		// THE LIVE TURN BOUNDS THIS WALK. Update asks the derived card question
		// on every message, so letting the ordinary no-card case cross this
		// boundary would make input cost grow with the whole transcript.
		if e.turn != a.turn {
			break
		}
		if e.kind != entryTask || e.card == nil {
			continue
		}
		if e.card.forming && !e.card.settled() {
			return i
		}
	}
	return -1
}

// The words a settled card keeps. They are sentences rather than states because
// the row is read once, later, by somebody reconstructing what happened.
const (
	taskApprovedWord = "approved"
	taskRedirectWord = "approved · you redirected it"
	taskDeclinedWord = "declined"
	taskClockWord    = "approved · the clock"
	taskExpiredWord  = "expired · the turn ended"
	// taskRedirectLane is what the empty box says while a proposal is open: the
	// one thing the box is for at that moment.
	//
	// IT NO LONGER NAMES esc. It used to read `(enter sends it, esc declines)`,
	// which was true while this block owned the keyboard and is a lie now: esc
	// is LATER on every question this surface asks (question.go), the work stays
	// waiting and nothing is decided by making something go away.
	taskRedirectLane = "say what to change… (enter sends it)"
	taskExpandHint   = "ctrl+e for the brief"
	// taskModelTag labels the one fact a proposal carries that nobody can find
	// out afterwards: whose hands the work is going into.
	taskModelTag      = "model "
	taskAcceptanceTag = "done when: "
	// taskBranchPointWord names WHERE THE WORK STARTS FROM, on a proposal whose
	// node is going to get a worktree of its own.
	//
	// It is on the card because of the one surprise the copy costs, and that
	// surprise turned around when the ground law landed (internal/session's
	// groundladder.go): the node's world is now the person's folder AS IT
	// STANDS, so work they have not committed is what the task starts from.
	// That is cheap to know beforehand and expensive to discover afterwards,
	// whichever way round it is. What has NOT changed is the other half — the
	// copy is a copy, and nothing they do to their own checkout while it runs
	// reaches it. There is no machinery in the sentence a person has to be
	// taught.
	taskBranchPointWord = "from your folder as it stands — unsaved edits included"
	// taskWaitingWord is what stands where the meter would be on a proposal the
	// engine is holding open indefinitely. A bar with no end to drain toward
	// would be an animation inventing a deadline nobody set.
	//
	// IT IS THE READING'S OWN SENTENCE FOR THAT QUESTION. A proposal with no
	// clock is the your-call tier's `start` ask and internal/session spells its
	// reason `starts on your word` ([session.TaskAskStart]); this row said
	// `waiting on you`, which is the phrase home spends on a CONVERSATION that
	// wants somebody — so one screen had one phrase for two different objects
	// and neither said what pressing anything would do.
	//
	// THE CLOCK IS THE WHOLE DIFFERENCE BETWEEN THE TWO TIERS. A proposal that
	// will start by itself needs nothing from anybody and says when
	// ([taskAutoWord], which is the engine's spelling too); one that will sit
	// there until somebody answers is the person's call and says so.
	taskWaitingWord = "starts on your word"
	taskAutoWord    = "auto-starts in "
	// taskFormingWord stands where the meter will be while the call that fills
	// this card is still arriving. It is a state and not a promise: there is no
	// deadline yet, because the engine has not been handed anything to hold.
	taskFormingWord = "forming…"
	// taskFormingName is what the head says before the title field has closed.
	// A block with no word in it reads as a rendering fault; this reads as the
	// one thing that is known — something is being proposed.
	taskFormingName = "task"
	// taskFormingLost is what a card keeps when the call never finished
	// arriving: the turn ended, or the stream died, mid-proposal.
	taskFormingLost = "cancelled · the proposal never arrived"
	// taskFormingRefused is what it keeps when the whole call arrived but the
	// engine refused it before there was a proposal to ask about.
	taskFormingRefused = "not started · the call was refused"
)

// awaitingTask reports whether a proposal is open in this window.
//
// IT NO LONGER MEANS "OWNS THE KEYBOARD". The proposal is answered on the
// question block like every other decision on this surface, and the block is
// never modal (question.go's THE NEVER MODAL law) — so what this says is that
// something is being asked, which is what its readers actually want to know.
func (a *app) awaitingTask() bool { return a.task != nil && !a.task.settled() }

// taskKey is the ONE key the proposal still owns, and it is not an answer.
//
// The three answers and the clock went to the question block with every other
// decision on this surface (question.go), and what is left here is the key that
// opens the assignment in the transcript: `ctrl+e` is what this surface means
// "show me the rest of this" by, and the brief is the rest of a proposal. It
// belongs to the lane rather than to the block because it acts on the BLOCK IN
// THE TRANSCRIPT — the question above the box has nothing to unfold.
//
// A LETTER-LESS KEY IS STILL THE BOX'S THE MOMENT THERE IS A LINE TO END. That
// is the rule input.go applies to the thinking block, and it is applied here for
// the same reason: ctrl+e is end-of-line in every shell a person has used.
func (a *app) taskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.awaitingTask() || msg.String() != "ctrl+e" {
		return nil, false
	}
	if !a.input.empty() {
		return nil, false
	}
	a.toggleCard()
	return nil, true
}

// taskAnswered is the lane's own hand on an answer, on its way to the engine's
// door: the card in the transcript keeps what was decided, and a yes opens the
// block the approved brief is shaped in.
//
// THE ENGINE'S OWN MAPPING SAYS WHAT THE ANSWER MEANS ([session.AnswerFromKey])
// rather than a second reading of the keys here, and the one case that mapping
// does not cover is the one the box carries: WORDS ARE A YES WITH A CORRECTION.
// The most valuable thing a person can do with a groomed piece of work is
// correct it, and correcting it is saying yes to the corrected version — which
// is what the engine's door does with the same answer (session's applyToLane).
func (a *app) taskAnswered(id uint64, answer session.Answer) session.Answer {
	card := a.task
	if card == nil || card.id != id || card.settled() {
		return answer
	}
	redirect := answer.Words()
	approve := redirect != ""
	word := questionKeyWord(questionCommentKey)
	if key := answer.FirstKey(); key != "" {
		action, ok := session.AnswerFromKey(session.QuestionTask, key)
		approve = ok && action.Task.Approved
		word = key
		if option, ok := a.taskOption(id, key); ok && strings.TrimSpace(option.Label) != "" {
			word = strings.TrimSpace(option.Label)
		}
	}
	switch {
	case approve && redirect != "":
		card.verdict = taskRedirectWord
	case approve:
		card.verdict = taskApprovedWord
	default:
		card.verdict = taskDeclinedWord
	}
	card.answer = word
	// A YES OPENS THE SAME WAIT THE TYPED COMMAND STANDS IN. The engine shapes
	// the approved brief before the task exists, and the person who just said
	// yes is owed the same forming block a person who typed /task gets — one
	// vocabulary for one pause, whichever door opened it (taskcommand.go's
	// [app.beginProposalWait]). A no and a redirect raise nothing: there is no
	// task coming to wait for.
	if approve && redirect == "" {
		a.beginProposalWait(card)
	}
	a.endRecall()
	a.closeLists()
	a.markCardStale(card)
	a.touch()
	return answer
}

// taskOption is one answer's option on the question this id names, read back
// off the block rather than rebuilt — consent.go's own reason: the answers a
// person saw are the ones the block actually drew.
func (a *app) taskOption(id uint64, key string) (session.AnswerOption, bool) {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionTask || open.question.ID != id {
			continue
		}
		return open.question.Option(key)
	}
	return session.AnswerOption{}, false
}

// answerTaskWith answers the open proposal on the block with one key, and
// reports whether it found one to answer.
//
// IT IS THE BLOCK'S OWN ANSWER and not a second one beside it, exactly as
// consent's [app.answerWith] is: the same receipt, the same record and the same
// annotated card as the same key pressed in front of the question. The one
// caller is home's errand band, where a person answers a question from the page
// rather than from the conversation it was asked in (homeband_answer.go).
func (a *app) answerTaskWith(id uint64, key string) bool {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionTask || open.question.ID != id {
			continue
		}
		if _, ok := open.question.Option(key); !ok {
			return false
		}
		a.answerQuestion(open, session.Answer{Key: key, Picked: []string{key}})
		return true
	}
	return false
}

// holdTask is what a keystroke means to a clock that ANSWERS.
//
// Every other clock on this block holds by itself and never answers anything
// (question.go's [app.tickQuestion], and F41 is why). The proposal's does the
// opposite — silence starts the work — so the moment there is evidence of
// somebody at the keyboard the engine is told to stop counting, and this window
// zeroes its own copy of the deadline first: the person pressed a key here, and
// the countdown may not go on draining while the news crosses a connection.
func (a *app) holdTask(id uint64) {
	card := a.task
	if card == nil || card.id != id || card.settled() || card.deadline.IsZero() {
		return
	}
	card.deadline = time.Time{}
	a.setTaskDeadline(id, time.Time{})
	a.markCardStale(card)
	a.touch()
	if agent, ok := a.tasker(); ok {
		agent.HoldTask(id)
	}
}

// toggleCard opens or closes the open proposal's brief.
func (a *app) toggleCard() {
	if a.task == nil {
		return
	}
	a.task.open = !a.task.open
	a.markCardStale(a.task)
	a.touch()
}

// toggleCardAt is the click: the whole card is the target, the way the whole of
// a thinking block is (thinking.go) — a card is a paragraph, and asking somebody
// to hit its first row is asking them to aim.
func (a *app) toggleCardAt(i int) {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTask {
		return
	}
	card := a.entries[i].card
	if card == nil {
		return
	}
	card.open = !card.open
	a.entries[i].stale = true
	a.touch()
}

// openCard is ctrl+o on a SELECTED proposal, and it reports whether it took the
// key.
//
// THE BRIEF KEPT A KEY WHEN IT LOST THE CLICK. A press on a spawn card opens the
// node's room now (app.go's [app.press]) — the question a person has about
// running work is what it is doing — and the brief is still one fold away, on
// the key this surface already means "show me the rest of this" by. It sits
// beside the landed card's own ctrl+o (taskdone.go's [app.openDone]): same key,
// same gesture, the same object one state apart.
func (a *app) openCard(i int) bool {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTask || a.entries[i].card == nil {
		return false
	}
	a.toggleCardAt(i)
	return true
}

// markCardStale drops the cached rows of the entry that draws this card.
func (a *app) markCardStale(card *taskCard) {
	for i := range a.entries {
		if a.entries[i].kind == entryTask && a.entries[i].card == card {
			a.entries[i].stale = true
			return
		}
	}
}

// tickTasks is the countdown, and it runs on the frame clock that is already
// turning (app.go's [app.paint]) rather than on a ticker of its own.
//
// At the deadline the card STOPS ASKING. It does not answer: the clock belongs
// to the engine — the deadline on screen is the one the engine is waiting on —
// and a surface that raced it would be a second authority on the same question.
// What it does is stop claiming a question is open when it is not.
func (a *app) tickTasks() {
	if !a.awaitingTask() || a.task.deadline.IsZero() {
		return
	}
	if a.now().Before(a.task.deadline) {
		return
	}
	a.task.verdict = taskClockWord
	// AND THE QUESTION GOES WITH IT. Nobody answered — the clock did what the
	// row said it would — so it is withdrawn rather than answered, and the
	// sentence is the engine's own for this event (session's questionGoneReason).
	a.dropTaskQuestion(a.task.id, taskStartedItselfReason)
	a.markCardStale(a.task)
	a.touch()
}

// taskStartedItselfReason and taskTurnEndedReason are why a proposal stopped
// being a question when nobody answered it. They are internal/session's own two
// sentences for the same two endings (its [questionGoneReason]), said here
// because this window learns of both from the card's lane rather than from the
// questions lane and must retire the question in the same words.
const (
	taskStartedItselfReason = "it started on its own, as the card said it would"
	taskTurnEndedReason     = "the work is no longer waiting on it"
)

// syncTaskAsk drops a card about a question nobody is asking any more.
//
// It runs where consent's [app.dropAsks] runs — a turn ending — and it asks the
// engine rather than assuming: [taskAgent.PendingTasks] is the same
// pending-id machinery consent has, and a proposal missing from it has already
// been approved by the clock, denied elsewhere, or died with its turn.
func (a *app) syncTaskAsk() {
	if !a.awaitingTask() {
		return
	}
	agent, ok := a.tasker()
	if !ok {
		return
	}
	pending, known := taskAsksOpen(agent)
	// AN ENGINE THAT DID NOT ANSWER RETIRES NOTHING. Over a connection this
	// reading is a call, and a call that timed out or a link that died is not
	// evidence that the far machine stopped asking — writing a verdict on that
	// would close a question still waiting on the other end.
	if !known {
		return
	}
	for _, id := range pending {
		if id == a.task.id {
			return
		}
	}
	// The deadline is what distinguishes the two honest stories: a clock that
	// ran out approved it, and anything else ended with the turn.
	word, reason := taskExpiredWord, taskTurnEndedReason
	if !a.task.deadline.IsZero() && !a.now().Before(a.task.deadline) {
		word, reason = taskClockWord, taskStartedItselfReason
	}
	a.task.verdict = word
	a.dropTaskQuestion(a.task.id, reason)
	a.markCardStale(a.task)
	a.touch()
}

// knownTasker is the pending-proposal reading WITH ITS OWN "I DID NOT SAY".
//
// It is asserted separately from [taskAgent] for that interface's reason: the
// local agent always knows — the answer is a map under its own lock — and only a
// hosted surface has a way to be told nothing at all (internal/remote's
// [Agent.TaskProposalsPending]).
type knownTasker interface {
	TaskProposalsPending() ([]uint64, bool)
}

// taskAsksOpen is what the engine says is still being asked, and whether it
// said. An agent with no second answer to give is taken at its word.
func taskAsksOpen(agent taskAgent) ([]uint64, bool) {
	if door, ok := agent.(knownTasker); ok {
		return door.TaskProposalsPending()
	}
	return agent.PendingTasks(), true
}

// ── the card, drawn ─────────────────────────────────────────────────────────
//
// THE PROPOSAL IS A CONTAINED BLOCK, and it is contained because of what it sits
// between. Every other entry on this surface is a paragraph in a conversation:
// it begins where the last one ended and nothing is lost when the eye runs from
// one into the next. An assignment is not a paragraph. It has a top, a brief and
// a set of facts about where the work will run, and when its rows flowed into
// the reply underneath it the result was a block a person had to reconstruct the
// boundaries of before they could read it.
//
// THE ANSWERS ARE NOT ON IT ANY MORE (question.go). What was here was a decision
// drawn in a place no other decision on this surface is drawn — its own choices
// row, its own draining meter, its own claim on the keyboard — and every law it
// needed had to be argued here separately from the eight other blocks that
// needed the same ones. So the ASKING is above the box with every other
// question, and the block in the transcript is what the question is ABOUT:
//
//	╭─ ? ◆ Fix nil-map crash ───────────────────────────────────────────────
//	│ The parser drops a key on an empty map.
//	│ from your folder as it stands — unsaved edits included
//	│ model deepseek/deepseek-v4-flash · ctrl+e for the brief
//	╰──────────────────────────────────────────────────────────────────────
//
//	? wants to start a task: Fix nil-map crash
//	  the parser drops a key on an empty map · aforge
//	  ▸ 1  start it
//	    2  no
//	  [enter] take the pick · [esc] later · [c] change · start it in 9s
//
// COLLAPSED IS A NAME AND A SENTENCE, and that is the card law this surface now
// applies to all three of a task's cards — the proposal, the rail's presence and
// the card that lands when it finishes (taskdone.go). The head carries the
// two-or-three-word name and the node's own glyph; the line under it is the
// first sentence of the assignment and nothing more.
//
// It used to be three lines of summary, which is what a card looks like when
// nobody has decided what a card is: the third line of a summary is being read
// by somebody who has already decided, and the person who has not decided is
// reading the first. Everything the three lines held is one keystroke away and
// is now MORE than they held — opened, the card is the whole assignment, the
// brief, the done-condition and the context the work will run in.
//
// SETTLED, THE BLOCK COLLAPSES to its head and one verdict line — what was
// chosen and what that came to — because a question that has been answered is a
// fact, and a fact does not need a frame around it.
//
// The blank row that follows the block is [app.layout]'s (render.go): spacing is
// emitted in exactly one place on this surface, and a block that left its own
// gap would be the second.
func (a *app) taskCardRows(card *taskCard, width int, sel bool) []string {
	if card == nil || width < 4 {
		return nil
	}
	// STILL ARRIVING: three rows, and the only rows on this block that move.
	if card.forming && !card.settled() {
		return a.taskFormingRows(card, width, sel)
	}
	head := a.taskHead(card, width, sel)
	if card.settled() {
		return []string{head, a.taskFoot(card, width)}
	}
	stem := a.pal.ask(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	out := []string{head}
	if line := card.sub; line != "" {
		out = append(out, stem+a.pal.ink(fit(line, room)))
	}
	if card.open {
		// OPENED, THE CARD IS THE WHOLE CONTEXT. The summary goes first because it
		// is the sentence the collapsed row was a cut of, then the brief — which is
		// the node's entire contract with its runner and the thing a person is
		// actually auditing when they open a proposal at all.
		for _, line := range wrap(card.summary, room) {
			out = append(out, stem+a.pal.dim(line))
		}
		for _, line := range wrap(card.brief, room) {
			out = append(out, stem+a.pal.dim(line))
		}
		if card.acceptance != "" {
			// The done-condition is LABELLED rather than run on: it is the one line
			// in the brief a person reads to decide whether the work will be
			// finished by something they would call finished.
			for _, line := range wrap(taskAcceptanceTag+card.acceptance, room) {
				out = append(out, stem+a.pal.dim(line))
			}
		}
	}
	if card.elsewhere != "" {
		// WHO ELSE IS ALREADY IN THESE FILES, said once, above the branch point and
		// below the brief — with the facts about the work rather than with the
		// answers, because it is not something to answer. There is no row for it
		// when there is nothing to say, which is the emptiness law and also the only
		// thing keeping this line worth reading: a proposal that carried it every
		// time would be carrying furniture.
		out = append(out, stem+a.pal.dim(fit(card.elsewhere, room)))
	}
	if card.where != "" {
		out = append(out, stem+a.pal.dim(fit("where: "+card.where, room)))
	}
	if point := a.taskBranchPoint(); point != "" {
		// The branch point is the last of the facts about the work, and it is the
		// one thing on the card a person cannot find out afterwards without
		// reading a merge.
		out = append(out, stem+a.pal.dim(fit(point, room)))
	}
	if meta := a.taskMetaWord(card, room); meta != "" {
		out = append(out, stem+a.pal.dim(meta))
	}
	return append(out, a.taskFoot(card, width))
}

// The three-line summary cap that stood here is gone with the three lines: the
// collapsed card is a name and ONE sentence (taskident.go's [taskSubtitleOf]
// cuts it, and caps it at ninety cells), and the whole summary is drawn inside
// the expansion where nothing needs capping.

// The block's own furniture, and its ASCII stand-ins. The stem is the one an
// expanded tool call already hangs from (styles.go's railCont), because a
// vertical line meaning "these rows are one thing" is a vocabulary this surface
// already has.
const (
	taskHeadCorner  = "╭─"
	taskFootCorner  = "╰─"
	taskCornerASCII = "+-"
	taskRule        = "─"
	taskRuleASCII   = "-"
)

// blockStem is the card's left edge.
func (a *app) blockStem() string {
	if a.pal.ascii {
		return railContASCII
	}
	return railCont
}

// blockRule is the line the head and the foot are drawn with.
func (a *app) blockRule() string {
	if a.pal.ascii {
		return taskRuleASCII
	}
	return taskRule
}

// blockPaint is the hue the frame itself takes: THE QUESTION HUE WHILE IT IS A
// QUESTION, and the furniture grey the moment it is not. Violet on this surface
// means somebody is being asked something, and a settled card that kept it would
// be a block still shouting about a decision that has been made.
// A FORMING CARD IS GREY FOR THE SAME REASON A SETTLED ONE IS: violet means a
// person is being asked something, and a call that is still arriving has not
// asked yet. The frame taking the question hue is what the proposal landing
// LOOKS like — the block the person watched grow turns into a question.
func (a *app) blockPaint(card *taskCard) func(string) string {
	if card.settled() || card.forming {
		return a.pal.dim
	}
	return a.pal.ask
}

// taskHead is the block's top: the corner, the question glyph, the title, and
// the rule that runs out to the frame's edge.
//
// THE CLOCK IS NOT UP HERE ANY MORE. It used to ride the right end of this row,
// which put the one thing on the block that changes every frame on the same line
// as the one thing worth reading once — and it said "4s", which is a number
// rather than a countdown. Both now live on the meter (see [app.taskMeter]).
func (a *app) taskHead(card *taskCard, width int, sel bool) string {
	paint, rule := a.blockPaint(card), a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	// The "?" is the consent block's own glyph, and it is the same glyph for the
	// same reason it is the same hue: this is that moment, about a different
	// kind of thing. It is already its own ASCII, so the linear tier needs no
	// stand-in for it.
	//
	// THE NODE'S OWN GLYPH RIDES BESIDE IT, unpainted by the frame's hue and
	// painted by the ring instead (taskident.go). The two are different claims
	// and both are true of this row: somebody is being asked something, and the
	// thing being asked about is THAT one — the same mark that will be on the
	// rail in four seconds and on the card that lands in eleven minutes.
	head := corner + " " + a.icon(tokens.GNeedsHuman) + " "
	mark := a.taskMarkSel(card.ident, sel) + " "
	title := fit(card.name, width-ansi.StringWidth(head)-3)
	line := paint(head) + mark
	if card.settled() {
		line += a.pal.muted(title)
	} else {
		line += a.pal.askBold(title)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(title) - 3; fill > 0 {
		line += paint(" " + strings.Repeat(rule, fill))
	}
	return line
}

// taskFormingRows is the card while the call that will fill it is still
// arriving: the head with whatever the title has said so far, one row saying
// what state this is, and the foot.
//
//	╭─ ◌ Port the streaming ────────────────
//	│ ⠙ forming… · 6s
//	╰───────────────────────────────────────
//
// The shape is the proposal's own, minus everything that would be a lie: no
// summary (nothing has closed), no models row (the engine has not resolved
// one), no choices (there is nothing to answer) and no meter (there is no
// deadline — the clock starts when the engine takes the proposal, not when the
// model starts writing it). What stands in the meter's place is the live account
// of exactly what is happening: a spinner, the forming word and the elapsed time.
func (a *app) taskFormingRows(card *taskCard, width int, sel bool) []string {
	stem := a.pal.dim(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		mark = a.icon(tokens.GWorking)
	}
	line := taskFormingWord
	if word := countUpWord(a.now().Sub(card.born)); word != "" {
		line += " · " + word
	}
	return []string{
		a.taskFormingHead(card, width, sel),
		stem + a.pal.dim(mark+" "+fit(line, room-2)),
		a.taskFoot(card, width),
	}
}

// taskFormingHead is [app.taskHead] for a block that is not yet a question.
//
// Two cells differ and both of them are the same decision. There is no "?",
// because nobody is being asked anything; and the node's own glyph is not there
// either, because the ident is derived from an id the engine has not minted —
// so the cell holds a still ◌ as the ident slot standing empty. The middle row
// already says this is alive; animating the head too would be two answers to one
// question.
func (a *app) taskFormingHead(card *taskCard, width int, sel bool) string {
	rule := a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	head := corner + " "
	mark := a.pal.dim(a.icon(tokens.GQueued)) + " "
	title := fit(firstNonEmpty(card.name, taskFormingName), width-ansi.StringWidth(head)-3)
	line := a.pal.dim(head) + mark
	if sel {
		line += a.pal.bold(a.pal.muted(title))
	} else {
		line += a.pal.muted(title)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(title) - 3; fill > 0 {
		line += a.pal.dim(" " + strings.Repeat(rule, fill))
	}
	return line
}

// taskFoot closes the block — and, once the question is answered, IS the answer.
//
// A settled card is two rows: the head it always had, and this, which keeps both
// halves of what happened. The option is what the person reached for and the
// verdict is what it came to, and they are different facts — "redirect" says
// they corrected it, "approved · you redirected it" says the work started.
func (a *app) taskFoot(card *taskCard, width int) string {
	paint, rule := a.blockPaint(card), a.blockRule()
	corner := taskFootCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	if !card.settled() {
		if fill := width - ansi.StringWidth(corner); fill > 0 {
			return paint(corner + strings.Repeat(rule, fill))
		}
		return paint(corner)
	}
	word := card.verdict
	if card.answer != "" {
		word = card.answer + " · " + card.verdict
	}
	return paint(corner+" ") + a.pal.dim(fit(word, width-ansi.StringWidth(corner)-1))
}

// taskBranchPoint is the branch-point line for this conversation, or nothing at
// all.
//
// THE BRANCH IS THE WHOLE TEST. A node gets a worktree exactly when the
// workspace is a repository with a commit to branch from (internal/session's
// prepareTaskTree), and that is precisely what a non-empty branch here means —
// the probe reads `rev-parse --abbrev-ref HEAD`, which a directory that is not a
// repository and a repository with no commits both refuse (app.go's gitHead). A
// workspace with no repository runs the work IN PLACE, in the person's own
// directory, where there is no branch point to name and nothing is hidden from
// the node: the card says nothing, which is the honest thing to say.
//
// It says nothing over a connection too, because the probe is off there and this
// machine cannot answer for the other one's checkout (app.go). An unknown
// renders as NOTHING rather than as a guess; a remote branch point is a wire
// question for the lane that owns the contract.
func (a *app) taskBranchPoint() string {
	if a.branch == "" {
		return ""
	}
	return taskBranchPointWord
}

// taskMetaWord is the card's dim last line: WHO the work goes to, and the key
// that opens what it is.
//
// The model leads because it is a fact about this proposal that nothing else on
// screen will ever say again — the rail has 24 columns and the landed card is
// twenty minutes away — where the expand hint is a key that is learned once and
// then never read. A narrow frame cuts the hint and keeps the model.
func (a *app) taskMetaWord(card *taskCard, width int) string {
	var parts []string
	if card.model != "" {
		parts = append(parts, taskModelTag+card.model)
	}
	if !card.open && card.brief != "" {
		parts = append(parts, taskExpandHint)
	}
	if len(parts) == 0 {
		return ""
	}
	return fit(strings.Join(parts, " · "), width)
}

// countdownFine spells the time LEFT beside the meter, and it spells the last
// ten seconds in tenths.
//
// The tenth is the whole point of the pair: at one figure per second the number
// beside a moving bar looks stuck, and "3.2s" is the digit that proves the same
// thing the bar does — this is running, and it is running out. Above ten seconds
// the tenth is noise and it falls back to [countdownWord].
func countdownFine(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= 10*time.Second {
		return countdownWord(d)
	}
	// Rounded UP, by the law [countdownWord] follows: the last tenth a person
	// has is drawn as a tenth rather than as a zero.
	tenths := int((d + 100*time.Millisecond - 1) / (100 * time.Millisecond))
	return itoa(tenths/10) + "." + itoa(tenths%10) + "s"
}

// countdownWord spells the time LEFT, rounded up, so the last second a person
// has is drawn as a second rather than as a zero. It is the count-up's mirror
// ([countUpWord]) and it is spelled the same way — spaced parts, no padding —
// because the two numbers appear on one screen.
func countdownWord(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 60 {
		return itoa(seconds) + "s"
	}
	return itoa(seconds/60) + "m " + itoa(seconds%60) + "s"
}

// ── the roster ──────────────────────────────────────────────────────────────
//
// THE RAIL WAS A PRESENCE LIST AND IT IS NOW A ROSTER, because the two stop
// being the same thing somewhere around the fortieth node. A presence list holds
// what is alive and forgets everything else, which is exactly right for a
// session with three nodes in it and useless for a day's work: "where did that
// task go" is the commonest question a person asks a column of work, and a
// column that dropped every landed node had already thrown the answer away.
//
// So the column keeps EVERY node the session has admitted, and it survives
// hundreds of them by four mechanisms and no new scroll machinery:
//
//   - IT IS A FOREST AND NOT FIVE BUCKETS. The column used to open with five
//     state headings — needs you, running, idle, parked, done — and file every
//     node under one of them, which scattered ONE run across four sections: the
//     root under `running`, its finished cuts under `done`, the one that
//     conflicted at the top of the column, and nothing on screen saying they
//     were the same piece of work. A shape is the news an adaptive run makes
//     (internal/orchestrate spawns as it learns), and the state of a single node
//     inside it is a detail. So a family is drawn WHOLE, under its own root, in
//     the order the session admitted it — and a node that belongs to no family
//     is the flat row it has always been.
//
//     What the headings sorted, the ORDER of the families still says: a family
//     stands where its most urgent member puts it ([app.railUrgency]), so work
//     that is waiting on a person is at the top of the column and a settled run
//     is at the bottom, whole, one row deep.
//   - FOLDING IS THE PERSON'S, PER FAMILY, AND IT HAS A DEFAULT WORTH HAVING. A
//     family with anything live in it opens; a family that is entirely settled —
//     or entirely parked — opens as one row wearing the worst thing that
//     happened under it and a count of what it is standing for. The map is the
//     person's correction of that default and it sticks ([app.railShut]).
//   - THE COLUMN IS A WINDOW. What shows is a slice of the line list around the
//     focus, taken by [listTop] — the same function the model picker and the two
//     typed lists scroll with, because a second scroller on this surface would be
//     a second set of off-by-ones.
//   - THE FOOTER SAYS THE WHOLE. What the window cannot show — the spend, the
//     weight, the count of every state the forest has folded into a shape — is
//     one dim block at the bottom of the column, in the group vocabulary the
//     headings used to carry.
//
// THE KEYBOARD IS ASKED FOR, NEVER TAKEN (alt+t, esc to give it back). The
// draft is this surface's rest state and a map that stole keys from it would
// make typing a thing you check before you do — see the marker law at
// [app.railRows].
//
// AND THE COLUMN WIDENS ON DEMAND (w). A tree spends three cells a level, and
// three levels of indent inside thirty columns is a list of first words. The
// wide tier is a third width the person asks for and never a width the surface
// takes: it is charged against the conversation like the other two, and the one
// piece of chrome that says it exists is a hint that appears only while a title
// is actually being cut by its own indent ([app.railFootRows]).

const (
	// railCols is the whole charge a full rail makes on the frame: the seam,
	// its gutter, and the column the nodes are drawn in.
	railCols = 30
	// railSlimCols is the charge under a narrower frame: the same column,
	// tighter.
	railSlimCols = 24
	// railWideCols is the charge of a column a person has ASKED to widen (w,
	// [app.railKey]). Forty-six is thirty plus five levels of indent, which is
	// deeper than any run this surface has drawn — the point of the tier is that
	// a tree stops eating its own names, not that it can nest forever. It is
	// charged against the conversation exactly like the other two, which is why
	// it is a key and not a default.
	railWideCols = 46
	// railWideGain is what taking that tier lends a row, and it is the whole of
	// what the widen hint promises ([app.railEntryRows]).
	railWideGain = railWideCols - railCols
	// railFloor is the frame a FULL rail takes. Under it the conversation
	// would be reading at ninety columns to keep a column of titles on screen.
	railFloor = 120
	// railSlimFloor is the frame a rail of any width takes. Under it the
	// transcript is the thing a person came for; the nodes still land in it
	// as notes when they finish.
	railSlimFloor = 100
	// railSeam is the one line the rail draws, and it is the same line the
	// legend draws below: a seam, not a border.
	railSeam = "│ "
	// railMark is the seam cell of the FOCUSED row — the same two columns, one
	// glyph heavier. The focus is a marker rather than a band because this column
	// is two cells from the conversation: a filled row here would be a block of
	// colour beside a paragraph a person is reading.
	railMark      = "▌ "
	railMarkASCII = "> "
)

// The disclosure marks a family root wears, and their ASCII stand-ins: closed
// points at what it is hiding, open points down at what it showed.
//
// THEY ARE DRAWN UNDER THE POINTER AND NOWHERE ELSE (see [app.railEntryRows]).
// A triangle on every root at rest is a column of widgets; a triangle that
// appears in the glyph cell the moment the pointer is over the row is the
// affordance arriving exactly when there is a hand to use it. What a FOLDED
// family shows at rest is its count instead — there is something hidden, and a
// count is the one thing a person cannot discover by hovering.
const (
	glyphShut      = "▸"
	glyphShutASCII = ">"
	glyphOpen      = "▾"
	glyphOpenASCII = "v"
)

// The tree's connectors and the three cells each level of it costs.
//
// THEY ARE NOT [railMid] AND [railLast], and the difference is what the arrow
// means. The transcript's fold hangs DETAIL off a row — "here is what is inside
// this call" — and an arrow pointing into it is the right mark for that. A
// roster tree hangs WORK off work, sibling beside sibling, and the plain elbow
// is what every tree a person has ever read is drawn with.
//
// The stand-ins are the linear tier's law (styles.go): a shape that means
// something gets a spelling that means the same thing out loud.
const (
	treeBranch      = "├─ "
	treeLast        = "└─ "
	treeStem        = "│  "
	treeVoid        = "   "
	treeBranchASCII = "|- "
	treeLastASCII   = "`- "
	treeStemASCII   = "|  "
	treeIndentCols  = 3
)

// What the keyboard offers while the roster holds it.
const (
	// railHoldHint is what the legend's hint slot says while the roster has the
	// keyboard (render.go's [app.hintWord]) — the keys [app.railKey] takes,
	// quoted from the handler rather than authored twice.
	//
	// It is split at the dismiss key because ONE OF THOSE KEYS IS CONDITIONAL:
	// ctrl+v moves the focused node's rung and the engine refuses a node whose
	// run is over, so it is named only while the row under the cursor could take
	// it ([app.railHoldHintWord]), and esc stays last because leaving is what a
	// person looks to the end of the line for.
	railHoldKeys = "↑↓ move · →← tree · enter open · " + railWidenChord + " wide"
	railHoldHint = railHoldKeys + " · esc"
	// The footer names both answers the handle can give. A bare "w" in a column
	// of counts is a keystroke nobody would risk pressing, and a handle whose
	// return trip is not named is only half an affordance.
	railWideHint   = railWidenChord + " widen · click seam"
	railNarrowHint = railWidenChord + " narrow · click seam"
)

// The two spellings of widen, and why there are two.
//
// [railWidenChord] IS THE ONE THE HINTS NAME, because it is the one that works
// wherever the roster is drawn. The bare letter is a view toggle, and a view
// toggle is the one kind of bare letter this surface may not have beside a
// composer (chordfocus.go): nothing is blocked on it, so a person pressing `w`
// at the start of a sentence meant the sentence. [railWidenKey] is kept for the
// full-frame roster, where there is no box on the screen at all.
const (
	railWidenChord = chordAltWord + "w"
	railWidenKey   = "w"
)

// railHoldChord is the key that HANDS THE ROSTER THE KEYBOARD, and it is
// `alt+t` because ctrl+t is now the new-tab chord this whole surface answers
// (chatstart.go's [app.newChatKey]) — the key every browser opens a fresh tab
// with, on a strip that is drawn as tabs. The roster keeps the same letter under
// the other modifier, which is the smallest move a hand has to make, and it is
// the modifier [railWidenChord] already spends beside it: a person driving this
// column is already pressing alt for `alt+w`. macOS composes Option+t into `†`
// where Option is not meta, which is exactly what [chordDeadKeys] is a table of.
const railHoldChord = chordAltWord + "t"

// The column's own door, and the two lines that name it.
//
// THE KEY IS FREE AND IT IS THE LAST FREE ONE WORTH SPENDING. alt+t is the
// roster's ([app.railKey]) and every other letter this surface could reach for
// is a chord the message box already answers — ctrl+a, ctrl+e, ctrl+b, ctrl+f,
// and ctrl+u are the readline edits a person types without looking, and
// taking one of those for a sidebar would be a keystroke that deleted a word the
// first time somebody meant it. ctrl+g is readline's abort, which this surface
// has always spelled esc, so nothing is lost by binding it.
const (
	railStowKey = "ctrl+g"
	// railStowHint is the last line of the column, and unlike the widen offer
	// above it, it is drawn WHENEVER THE COLUMN IS. The widen tier is contextual —
	// a cut title earns the offer — but the way out of a column is the one thing a
	// person cannot discover by hovering, cannot reach from the keyboard they have
	// not been handed, and will look for at exactly the moment they have decided
	// they are done with it. One dim row at the bottom is the whole cost.
	railStowHint = railStowKey + " hide"
	// railBackHint is the other half, and it lives in the legend's hint slot while
	// the column is away and this session has run anything (render.go's
	// [app.hintWord]). It is the shape of that slot's other lines: the key, then
	// what it reaches.
	railBackHint = railStowKey + " tasks"
)

// ── THE EDGE A CLOSED COLUMN LEAVES BEHIND ──────────────────────────────────
//
// ctrl+g USED TO MAKE THE COLUMN VANISH WITHOUT A TRACE, and a thing with no
// trace is a thing a person cannot get back. The two ways home were the chord
// itself — which is knowledge, not an affordance, and the person who pressed it
// by accident never had that knowledge — and the legend's [railBackHint], which
// is one line of five words in a slot that carries something else most of the
// time and says nothing at all in a session that has run no work.
//
// So a closed column leaves an EDGE: [railGripCols] columns down the right of
// the frame, near-silent, with a handle at the middle of it, and the whole strip
// is a door. Pressing anywhere on it is exactly ctrl+g ([app.railStow] takes
// both).
//
// THREE THINGS KEEP IT HONEST:
//
//   - IT COSTS WHAT IT SHOWS. The strip is charged to the conversation through
//     [app.railWidth] like the column it stands for, so the transcript re-wraps
//     two columns narrower and nothing is ever drawn under it.
//   - IT WHISPERS ONLY WHAT IS TRUE. One cell above the handle carries the state
//     of the work while there is work in a state worth carrying — something
//     running, or something waiting on a person — and NOTHING otherwise, which
//     is the emptiness law in the smallest space this surface has.
//   - IT IS NOT THERE WHEN A COLUMN COULD NOT BE. Under [railSlimFloor] the
//     frame lends no columns to anything, and an edge onto a column that cannot
//     stand would be a door onto a room that does not exist.
const (
	// railGripCols is what the closed column costs. TWO: one blank to hold it off
	// the last word of the conversation, and one for the handle. A single column
	// would put the glyph against the text and read as a typo in the transcript.
	railGripCols = 2
	// railGripGlyph is the handle. It points LEFT because that is the way the
	// column comes back — over the conversation, from the right edge — and it is
	// the one shape on this surface that means "there is more this way".
	//
	// IT USED TO BE `‹` IN THE DIM, AND NOBODY COULD SEE IT. The report was
	// exactly that — "the arrow does not even seem it is there" — and both halves
	// of it were true: a single-guillemet is the lightest arrow in the font, and
	// dim is the weight this surface paints TELEMETRY at. This is not telemetry.
	// It is the only control on the frame for a column somebody has hidden, so it
	// takes the heavy chevron and the ordinary ink, and the hover takes it further
	// ([app.railGripRows]). The state cell above it keeps its own hues, which are
	// louder still and belong to the work rather than to the door.
	railGripGlyph      = "❮"
	railGripGlyphASCII = "<"
	// railGripOpenGlyph is the SAME control in its other state: the chevron the
	// column wears while it stands, pointing right because that is the way it
	// goes. It rides the footer's own door line ([railStowHint]) rather than the
	// seam, which is already the width handle and may not mean two things.
	//
	// SO THE RIGHT EDGE ALWAYS CARRIES ONE CHEVRON — `❯` to close while the column
	// is up, `❮` to open while it is away — and the pointer can go round the whole
	// cycle without ever being told a chord.
	railGripOpenGlyph      = "❯"
	railGripOpenGlyphASCII = ">"
)

// railGroup is what a node is DOING, which is the only thing the roster sorts
// by. The order of these constants IS the order of the column.
type railGroup uint8

const (
	// railAttention is work that is waiting on a PERSON: nobody could say whether
	// it holds, or it finished and its branch never came home. Both are the same
	// sentence — this is not going anywhere until you look at it — and they lead
	// the column because everything below them is a thing that is still moving by
	// itself.
	//
	// A GROUP THAT NEVER FOLDS MUST NEVER ACCUMULATE THE UN-ACTIONABLE. This is
	// the one group with no fold and the top of the column, which is a standing
	// charge on the most valuable rows on the surface, and the only thing that
	// pays for it is that every row here is a DEMAND: something a person has to
	// do, that nothing else is going to do. A row that is merely bad news buys
	// nothing with that position — it pushes down the rows that are demands, and
	// it never leaves, so a day's work ends with the "needs you" heading standing
	// over forty things nobody needs to do. See [app.railGroupOf] for what that
	// costs a failure, and why a failure stopped paying it.
	railAttention railGroup = iota
	// railRunning is a child agent working in its worktree right now.
	railRunning
	// railIdle is admitted, unblocked, and not started: nothing is in its way
	// except a slot.
	railIdle
	// railParked is admitted and BLOCKED — its prerequisites are unfinished, so
	// nothing about it will change until other work does. That is what makes it
	// the group that folds: a parked node is a promise, not a happening.
	railParked
	// railDone is over and delivered.
	railDone
	railGroupCount
)

// The word each group wears, in the roster's heading and in its footer alike.
// One vocabulary: a person who reads "needs you" at the top must not have to
// learn that the bottom calls the same thing "blocked".
// `idle` AND `parked` WERE TWO WORDS FOR ONE SHAPE OF FACT, and neither said it.
// `idle` reads as a machine doing nothing when the node is admitted and about to
// start, `parked` is the machinery's own word, and the tasks page — which does
// not split the two — had to pick one of them and so called slot-queued work
// `parked` while the column called it `idle`. The pair is now `queued` (nothing
// in its way but a slot) and `waiting` (blocked behind other work), which keeps
// the distinction the groups exist for and is readable without learning it. The
// dependency reason still rides on the row ([app.railWaits]).
var railGroupWords = [railGroupCount]string{"needs you", "running", "queued", "waiting", "done"}

// railGroupOf groups the task's own state. Explicit decisions and conflicts
// lead, then active and waiting work, then finished reports. A retained branch
// remains inspectable without demanding an unrequested merge.
func (a *app) railGroupOf(node *taskNode) railGroup {
	// The demand is the reading's ([session.TaskStatus.Attention]); whether a node
	// is queued or running is the scheduler's, and is read from the state so that
	// a queued node behind a full machine is never filed as work in flight.
	status := a.taskStatus(node)
	switch {
	case status.Attention:
		// A parent's running is a FOLD, not a mute. While the parent lives it is
		// the one being asked, so the top of the family is what a person reads
		// first — but the demand stays visible here, because filing it under
		// `done` once let a nested question expire with nobody able to see it
		// (#268). What folds is how loud it is ([app.railGlyphRank]).
		return railAttention
	case status.Tier == session.TaskTierYourCall:
		// A QUESTION SOMEBODY ELSE IS HOLDING IS PARKED AND NEVER DONE. The demand
		// is gone — the model, or the parent's own agent, is answering it — but the
		// work is not finished, and filing it under `done` is exactly how a nested
		// question once expired with nobody able to see it (#268).
		return railParked
	case status.Presence == session.TaskPresenceWaiting:
		return railParked
	case status.State == session.TaskQueued:
		if status.On == session.TaskWaitWork {
			return railParked
		}
		return railIdle
	case status.State == session.TaskRunning:
		return railRunning
	}
	return railDone
}

// taskAwaitsPerson reports whether this node's only remaining step is a
// PERSON'S. It is one half of the demand the roster counts; the other is edits
// nobody brought home ([session.TaskStatus.ChangesUnlanded]), and both name work
// the machine has finished and cannot take further.
//
// TODAY IT IS EXACTLY ONE THING, and the narrowness is the engine's rather than
// this file's: a harness design holds its node open while its card waits to be
// answered — the work genuinely is not over, the room stays open, the stop keeps
// working — and `running` is the machinery's word for the same span in which
// nothing is running. Which phases mean that is decided once, in the reading
// ([session.ProjectTask]), so a surface here never has to guess at the meaning
// of a phase it was never told about.
//
// It is the RUNNING half of "needs a person": work nobody could check needs one
// too, and that node has settled ([app.taskStatus] answers both).
func (a *app) taskAwaitsPerson(node *taskNode) bool {
	status := a.taskStatus(node)
	return status.Presence == session.TaskPresenceNeedsLook && status.State == session.TaskRunning
}

// taskParentDeciding reports whether the node above this one is STILL WORKING,
// and is therefore the one being asked about work under it that nobody could
// check.
//
// IT IS A FOLD AND NOT A MUTE, and the difference is the whole of #268. The
// engine routes a sub-task's landing note to its parent node's own agent, which
// has the `tasks` tool and the diff and every reason to answer it (session's
// deliverTaskNote) — so while the parent lives, the top of the family is what a
// person should be reading first. That is an argument about LOUDNESS and it was
// once read as an argument about presence: the child was filed under `done` and
// drew no card, so a nested question could expire with nobody able to see it.
// What this answers now is only [app.railGlyphRank]'s question — how loud —
// while [app.railGroupOf] keeps the demand where it belongs.
//
// It is asked about exactly one thing — is the parent unsettled — and everything
// else answers "no": a node with no parent is a root and is the person's, a node
// whose parent this session has never heard of has nobody above it that could
// decide, and a node whose parent has landed has been orphaned and is the
// person's again.
//
// It is deliberately not a walk up the whole family. The immediate parent is the
// only node that is ever handed this child's news, so a grandparent's state says
// nothing about whether anybody is reading it.
func (a *app) taskParentDeciding(node *taskNode) bool {
	if node == nil || node.parent == "" {
		return false
	}
	for _, up := range a.tasks {
		if stripKey(up) != node.parent {
			continue
		}
		return up.state == session.TaskRunning || up.state == session.TaskQueued
	}
	return false
}

// railShut reports whether a family is drawn as its root alone.
//
// THE DEFAULT IS THE DESIGN AND THE MAP IS THE PERSON'S CORRECTION OF IT, which
// is why this is not a plain bool per node: a family nobody has touched must
// follow the default even as the work under it moves. A family with anything
// live in it — running, waiting on a person, or waiting for a slot — is open,
// because that is the shape somebody is watching; a family that has entirely
// settled, or that is entirely parked behind other work, is one row with a count
// on it.
func (a *app) railShut(node *taskNode) bool {
	if open, said := a.railOpen[node.id]; said {
		return !open
	}
	return !a.railKinLive(node)
}

// railTwigShut is the same question asked where the family is already grown, and
// it is the one the layout uses: [app.railShut] has to build the subtree back up
// to answer, which down a walk is the family regrown once a row.
func (a *app) railTwigShut(t *railTwig) bool {
	if open, said := a.railOpen[t.node.id]; said {
		return !open
	}
	return !a.railTwigLive(t)
}

// railSetOpen folds one family open or closed.
func (a *app) railSetOpen(node *taskNode, open bool) {
	if node == nil {
		return
	}
	if a.railOpen == nil {
		a.railOpen = map[uint64]bool{}
	}
	if a.railShut(node) == !open {
		return
	}
	a.railOpen[node.id] = open
	a.touch()
}

// railToggle is what a press on a root's glyph cell does.
func (a *app) railToggle(node *taskNode) { a.railSetOpen(node, a.railShut(node)) }

// railEntry is one navigable row of the roster: a node, and where in its family
// it hangs.
type railEntry struct {
	node *taskNode
	// stems is the ancestry as the connectors need it: one entry per level, true
	// where that level's node still has siblings to come. Its length is the
	// node's depth, so a root's is empty and a root has no connector.
	stems []bool
	// root says this node HEADS a family — it has children, so it is the one row
	// in the family that folds. A node that belongs to no family is not a root:
	// it is the flat row this column has always drawn.
	root bool
	// folded says this root is standing for its whole subtree, hidden is how many
	// nodes it is standing for, and worst is the node whose state that one row
	// wears. All three are zero on every other row, and all three are settled at
	// WALK TIME because the walk has the subtree in its hand — asking the same
	// questions again at paint time would be re-growing the family once a row.
	folded bool
	hidden int
	worst  *taskNode
}

// workingNowAgent is the engine door behind the ONE NUMBER this surface takes
// from the live work tree: how many hands are moving right now, which the
// column's head quotes (margin.go's [app.marginHead]). Each surface asserts
// only the slice it reads, so an engine without the door draws the
// byte-identical roster it drew before the door existed.
//
// IT IS A COUNT AND NOT A SECOND ROSTER, and that is the whole of the rule this
// column keeps about live work. Every worker [session.WorkingNow] reports —
// a node of the task graph, an adaptive run, one planned node inside one —
// publishes a TaskNotice naming who spawned it, and this column grows its
// families out of exactly those notices ([app.railForest]). So a worker already
// has a row here by the time the engine can be asked about it, and a preview
// hung under that row would be the same family drawn twice, one copy of it
// carrying less than the other. What the engine's tree can say that the rows
// cannot is how MANY of them are moving at once, so that is what is taken.
type workingNowAgent interface {
	WorkingNow() []session.WorkNode
}

// railSpot names a row by IDENTITY rather than by index, and it is what the
// focus is stored as.
//
// An index would be a cursor that jumps: work landing reorders the families, a
// fold takes a subtree out from under it, and both happen while nobody is
// touching the keyboard. An id survives all of it, and when the node it names is
// genuinely gone the walk clamps rather than teleporting.
//
// AN ID IS ENOUGH BECAUSE EVERY ROW OF THIS COLUMN IS THIS CONVERSATION'S. It
// used to carry a second field for the project's record rows, which cannot be
// named by id at all — ids restart with every conversation (task_index.go says so
// on [session.TaskIndexEntry.ID]) — and those rows are the task page's now.
type railSpot struct {
	id uint64
	// jobs is the jobs section's label, and job is a row of that section. They
	// live here rather than in a second cursor because this column has one
	// walk and one enter, and a second map would be a second idea of where
	// the keyboard is.
	jobs bool
	job  int
}

func (s railSpot) onJobs() bool { return s.jobs || s.job != 0 }

func railSpotOf(e railEntry) railSpot {
	if e.node == nil {
		return railSpot{}
	}
	return railSpot{id: e.node.id}
}

// railMembers buckets every node this session has admitted, NEWEST FIRST inside
// each group — except in the fold, where 8.1.7 has the first word.
func (a *app) railMembers() [railGroupCount][]*taskNode {
	var out [railGroupCount][]*taskNode
	for i := len(a.taskOrder) - 1; i >= 0; i-- {
		node := a.tasks[a.taskOrder[i]]
		if node == nil {
			continue
		}
		g := a.railGroupOf(node)
		out[g] = append(out[g], node)
	}
	out[railDone] = railFinalOrder(out[railDone])
	return out
}

// railFinalOrder is 8.1.7's second clause, applied to the one group this surface
// has that is FINALIZED: incomplete work claims the slots first, and everything
// that merely finished follows it in the order it arrived.
//
// A FINALIZED LIST IS READ TO FIND OUT WHAT WENT WRONG. That is the whole of the
// law's reasoning and it is exactly why a failure could stop standing at the top
// of the column: it did not need the top of the column to stay visible, it
// needed to be the first thing behind the heading it went into. A fold opened on
// a hundred and forty landed nodes shows its first handful of rows, and the
// person opening it is looking for the one that did not come off — so that is
// the one the first rows are spent on, ahead of a hundred clean merges that have
// nothing left to say.
//
// IT IS A PARTITION AND NOT A SORT. Newest-first survives inside each half,
// because the order the roster is built on is arrival order and a group that
// re-sorted itself by anything else would move a row a person is watching for a
// reason they cannot see (the roster section's own law).
func railFinalOrder(nodes []*taskNode) []*taskNode {
	incomplete := 0
	for _, node := range nodes {
		if node.state == session.TaskFailed {
			incomplete++
		}
	}
	if incomplete == 0 || incomplete == len(nodes) {
		return nodes
	}
	out := make([]*taskNode, 0, len(nodes))
	for _, node := range nodes {
		if node.state == session.TaskFailed {
			out = append(out, node)
		}
	}
	for _, node := range nodes {
		if node.state != session.TaskFailed {
			out = append(out, node)
		}
	}
	return out
}

// ── THE FOREST ──────────────────────────────────────────────────────────────

// railTwig is one node of a family as the roster holds it: the node and the
// children the session admitted under it.
//
// It is a shape of its own rather than a flat list of (node, depth) pairs
// because every question the column asks is about a SUBTREE — is anything under
// this live, what is the worst thing that happened in it, how many rows is it
// standing for — and a depth-tagged list answers those by scanning forward for
// the next row at the same depth, which is a tree with its structure taken out
// and then guessed back.
type railTwig struct {
	node *taskNode
	kids []*railTwig
}

// railKin buckets every node this session has admitted by its parent's key, and
// indexes them all by their own.
//
// It walks [app.taskOrder], so a parent's children come out in the order the
// session met them — the one order a family is allowed to use, because any other
// one moves a row a person is watching for a reason they cannot see. The
// alphabet is the parent seam's ([taskNode.ParentID] and [stripKey],
// taskstrip.go): an orchestrate node id is a string, and "" is an honest
// "nobody spawned this".
func (a *app) railKin() (kids map[string][]*taskNode, byKey map[string]*taskNode) {
	byKey = make(map[string]*taskNode, len(a.taskOrder))
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil {
			byKey[stripKey(node)] = node
		}
	}
	for _, id := range a.taskOrder {
		node := a.tasks[id]
		if node == nil {
			continue
		}
		up := node.ParentID()
		if up == "" || up == stripKey(node) || byKey[up] == nil {
			continue
		}
		if kids == nil {
			kids = map[string][]*taskNode{}
		}
		kids[up] = append(kids[up], node)
	}
	// AND EACH SET OF CHILDREN IS PUT IN THE ORDER THE COLUMN ALREADY PUTS
	// FAMILIES IN: what will not move without a person first, then what is
	// running, then what is waiting, then what is over.
	//
	// THE FAMILIES WERE SORTED AND THEIR MEMBERS WERE NOT, which showed on exactly
	// the rows the sort exists for. A run that hands out four pieces finishes them
	// one at a time, and the finished ones arrived FIRST — so the block under an
	// open family read `done, done, running, running`, with the only rows anybody
	// was watching at the bottom of it. The person's own instruction was that
	// active work be easy to find, and it was easy to find down to the level the
	// ordering stopped at.
	//
	// TIES KEEP ARRIVAL ORDER, which is what makes this safe to do under somebody
	// who is reading: the slice is already in [app.taskOrder]'s order and the sort
	// is stable, so two settled siblings never trade places and the block is still
	// the family in the order the session met it wherever the states agree.
	for up := range kids {
		under := kids[up]
		sort.SliceStable(under, func(i, j int) bool {
			return a.railGroupOf(under[i]) < a.railGroupOf(under[j])
		})
	}
	return kids, byKey
}

// railRootOf walks up to the head of a node's family.
//
// The visited set is not defensive tidiness: the parent is written by an adapter
// this package does not own ([taskNode.ParentID]), and a cycle in it would be a
// frame that never returns rather than a frame that looks wrong.
func railRootOf(node *taskNode, byKey map[string]*taskNode) *taskNode {
	seen := map[string]bool{}
	for {
		key := stripKey(node)
		if seen[key] {
			return node
		}
		seen[key] = true
		up := byKey[node.ParentID()]
		if up == nil {
			return node
		}
		node = up
	}
}

// railGrow builds one family, depth first, in the order the session met it. The
// visited set carries the same law [railRootOf] states.
func railGrow(node *taskNode, kids map[string][]*taskNode, seen map[string]bool) *railTwig {
	key := stripKey(node)
	twig := &railTwig{node: node}
	if seen[key] {
		return twig
	}
	seen[key] = true
	for _, kid := range kids[key] {
		twig.kids = append(twig.kids, railGrow(kid, kids, seen))
	}
	return twig
}

// count is every node under this twig, its own row not included — what a folded
// root has to say it is standing for.
func (t *railTwig) count() int {
	n := 0
	for _, kid := range t.kids {
		n += 1 + kid.count()
	}
	return n
}

// railForest is every family this session has, each in the order it was
// admitted, and the roots in the order the column draws them.
//
// A FAMILY STANDS WHERE ITS MOST URGENT MEMBER PUTS IT. The headings are gone
// and this is what replaced them: the work that will not move without a person
// is at the top of the column, then what is running, then what is waiting, then
// what is parked, then what is over — the same five words the footer counts in,
// applied to a whole run rather than to one node of it. Ties are broken by the
// root's own arrival order, which is the order [app.taskOrder] is already in, so
// two settled runs never trade places while nobody is looking.
func (a *app) railForest() []*railTwig {
	kids, byKey := a.railKin()
	seen, grown := map[string]bool{}, map[string]bool{}
	var trees []*railTwig
	for _, id := range a.taskOrder {
		node := a.tasks[id]
		if node == nil {
			continue
		}
		root := railRootOf(node, byKey)
		key := stripKey(root)
		if seen[key] {
			continue
		}
		seen[key] = true
		trees = append(trees, railGrow(root, kids, grown))
	}
	// A STABLE SORT AND NOT A COMPARISON ON ARRIVAL. The slice is already in root
	// arrival order, so stability IS the tie-break — spelling the tie out in the
	// comparison would be the same law written twice.
	sort.SliceStable(trees, func(i, j int) bool {
		return a.railTreeUrgency(trees[i]) < a.railTreeUrgency(trees[j])
	})
	return trees
}

// railTreeUrgency is the family's place in the column: the most urgent thing in
// it, in the group order this file already sorts by ([railGroup]'s constants are
// that order).
func (a *app) railTreeUrgency(t *railTwig) railGroup {
	worst := a.railGroupOf(t.node)
	for _, kid := range t.kids {
		if g := a.railTreeUrgency(kid); g < worst {
			worst = g
		}
	}
	return worst
}

// railKinLive reports whether anything in this node's family is still moving or
// still waiting on somebody: it is the fold's default, and it is asked of the
// root ([app.railShut]).
func (a *app) railKinLive(node *taskNode) bool {
	kids, _ := a.railKin()
	return a.railTwigLive(railGrow(node, kids, map[string]bool{}))
}

func (a *app) railTwigLive(t *railTwig) bool {
	switch a.railGroupOf(t.node) {
	case railAttention, railRunning, railIdle:
		return true
	}
	if t.node.Paused() {
		return true
	}
	for _, kid := range t.kids {
		if a.railTwigLive(kid) {
			return true
		}
	}
	return false
}

// railEntries is the roster's row model: every family, whole, under its own
// root, with the folded ones standing at one row each.
//
// THE FOREST IS THIS COLUMN'S WHOLE ACCOUNT OF WHO IS WORKING, and nothing is
// hung under it from the engine's live tree. It used to be: a second, smaller
// list of the same hands was attached beneath each row from
// [session.Agent.WorkingNow], keyed by the row's bare id — and it never once
// drew, because that door spells a worker `task:7`, `run:2` or `run:2/plan`
// (session's work_tree.go) and this column was asking it for "7". Fixing the
// spelling would not have fixed the surface, it would have started the double
// draw the broken key had been hiding: every worker in that tree ALREADY has a
// row of its own here. A graph node announces itself with the id of whatever
// spawned it (session's task_run.go) and an adaptive run registers a row for
// itself and one per planned node (its family seam), so both halves of the
// engine's tree arrive here as ordinary notices and [app.railForest] hangs them
// on their parents. The preview could only ever have restated them, in one mark
// and a name, under the fuller row that was already there.
//
// So the ownership rule, stated once: A WORKER IS DRAWN BY THE FAMILY THAT
// OWNS IT, on the row its own notice minted. The live tree is still read — for
// the count in the column's head, which is the one thing about it a row cannot
// say (see [workingNowAgent]).
func (a *app) railEntries() []railEntry {
	out := make([]railEntry, 0, len(a.taskOrder))
	for _, tree := range a.railForest() {
		out = a.railWalk(out, tree, nil)
	}
	return out
}

// railWalk lays one family out, depth first.
func (a *app) railWalk(out []railEntry, t *railTwig, stems []bool) []railEntry {
	e := railEntry{node: t.node, stems: stems, root: len(t.kids) > 0}
	if e.root && a.railTwigShut(t) {
		e.folded, e.hidden, e.worst = true, t.count(), a.railWorst(t)
		return append(out, e)
	}
	out = append(out, e)
	for i, kid := range t.kids {
		// The stack is COPIED down rather than appended to in place: one backing
		// array shared between two siblings is the second sibling drawing the
		// first one's stems.
		next := make([]bool, len(stems), len(stems)+1)
		copy(next, stems)
		out = a.railWalk(out, kid, append(next, i < len(t.kids)-1))
	}
	return out
}

// railColsFor is how wide the rail is at a frame width: full from railFloor,
// slim down to railSlimFloor, gone under that.
func railColsFor(width int) int {
	switch {
	case width >= railFloor:
		return railCols
	case width >= railSlimFloor:
		return railSlimCols
	}
	return 0
}

// railColumns is [railColsFor] with the person's own answer folded in: a column
// somebody widened (w) takes [railWideCols], and only where the frame was
// already lending a full one. The floors are the frame's and the tier is the
// person's — a terminal that cannot afford thirty columns cannot afford
// forty-six either.
func (a *app) railColumns(width int) int {
	if a.railWide && width >= railFloor {
		return railWideCols
	}
	return railColsFor(width)
}

// railCanWiden reports whether the third tier is ON OFFER at this frame — the
// person's own answer reaches [app.railColumns] only from [railFloor] up, and
// the roster drawn over the body has no column to widen at all.
//
// IT IS ONE QUESTION BECAUSE IT IS ASKED BY BOTH HANDS AND BY THE HANDLE. The
// footer names the chord only where it works ([app.railOffersResize]) and the
// seam is only a handle where it works (room.go's [app.railSeamAt]) — and the
// seam is the half that had it wrong: it claimed the two leftmost cells of every
// row at EVERY width the column stands at, so between [railSlimFloor] and
// [railFloor], where widening does nothing, those cells swallowed the press and
// changed nothing on screen. Two dead columns of a twenty-four-column list, down
// the edge a hand crossing from the conversation reaches first.
func (a *app) railCanWiden() bool {
	width, _ := a.size()
	return a.railShowing() && !a.railFull() && width >= railFloor
}

// railShowing reports whether the frame has a roster on it right now.
//
// THE COLUMN IS PERMANENT. It stands from the session's first frame, before any
// work exists, and work FILLS it rather than raising it. It used to wait for
// the first node, which made the frame a moving target: the conversation
// re-wrapped thirty columns narrower the moment a task was admitted, and a
// person could not learn where the column lived because it was only ever there
// after the fact. Now the place is part of the frame, the way the status line
// is. What it is NOT is a column somebody has to live with: ctrl+g takes it off
// the frame, the conversation takes back the columns, and the answer is
// remembered for the next session ([app.railStow]). The width tiers keep their
// say too — under [railSlimFloor] there is no column to stand.
//
// WHAT FILLS IT IS THIS CONVERSATION'S WORK AND NOTHING ELSE. It carried a
// dulled sample of the project's record under the forest for a while and no
// longer does (taskview.go says why); what a directory with a history behind it
// gets instead is one dim line at the foot of the column naming the page that
// holds it ([taskSheetPastHint]).
func (a *app) railShowing() bool {
	if a.railAway || a.railQuiet() {
		return false
	}
	width, _ := a.size()
	return a.railColumns(width) > 0
}

// railQuiet reports whether the column has nothing true to say yet: the
// greeting is up, this conversation has run nothing, and nothing stands over
// it.
//
// THE COLUMN IS ABSENT UNTIL IT HAS CONTENT OR THE CONVERSATION HAS BEGUN. It is
// still permanent in the sense that matters — it stands from the first
// keystroke on, before any work exists, so the place is learned before it is
// needed — but on the empty screen it was a bordered column of `+ /task` and
// `+ /standing` beside nothing, and `❯ ctrl+g hide` under them, on a frame whose
// only other content was a greeting (welcome.go). Furniture drawn to mark an
// absence is the one thing this surface does not draw. The doors are one `/`
// away and the greeting's own line says so.
//
// It answers false — the column stands — the moment there is a task or a
// standing order to put on it, so a session with orders over it meets the
// column on its first frame exactly as before. And it is only ever true while
// the greeting is open: neither the closed column's edge nor the column itself
// is on the frame, so nothing here can be pressed ([app.railStowed] asks it
// too).
func (a *app) railQuiet() bool {
	// AND THE NEW-CHAT START PAGE IS THE SECOND ANSWER, for the first one's reason
	// with the sign flipped (chatstart.go). There the column had nothing to say;
	// here it has plenty and none of it is about the page on screen — the roster
	// is THIS CONVERSATION's work, and the page is deliberately not in a
	// conversation yet. A column left standing beside it would offer doors into
	// the work of a conversation the person has just stepped away from, drawn
	// beside a blank box asking for a different one.
	if a.startingChat() {
		return true
	}
	return a.welcome.open && !a.railAvail() && len(a.marginStanding()) == 0
}

// railAvail reports whether there is a roster to raise at all, at ANY width.
//
// It is the width-free half of [app.railShowing], and the two are different
// questions now: what the frame lends the roster is a question about columns,
// and whether there is anything to put a cursor on is not. alt+t asks this one.
//
// THE PROJECT'S RECORD DOES NOT COUNT. It is not on this column — the column is
// this conversation's work (taskview.go) — so an alt+t that took the keyboard on
// the strength of it would hand six keys to a list with no rows in it. What a
// directory with a history behind it has is the footer's door, and that is a
// press and a chord of its own ([taskSheetPastHint]).
//
// A JOB COUNTS. It is this conversation's work as much as a task is, and a
// session that has only started a server still has a row the keyboard can
// stand on (jobsection.go).
func (a *app) railAvail() bool { return len(a.taskOrder) > 0 || len(a.jobs) > 0 }

// railFull reports whether the roster is drawn OVER the body rather than beside
// it — the narrow frame's answer to the same key.
//
// THE COLUMN IS THE FIRST THING A NARROW FRAME GIVES UP and that left the work
// with no door on it at all: under [railSlimFloor] there was no rail, so there
// was no way into a running node except a card that had scrolled away. Squeezing
// the column further was never the fix — a roster at twelve columns is a list of
// first words — so under the breakpoint the same roster opens over the frame
// instead: the same entries, the same folds, the same footer, the same keys, at
// the width it actually has.
//
// It is the SAME STATE as the column's focus ([app.railHold]) and not a second
// flag, because it is the same act: alt+t asks for the roster, and what the
// frame does with the request is a question about its width. One state cannot
// disagree with itself about whether the roster is up.
func (a *app) railFull() bool {
	if !a.railHold || !a.railAvail() || a.railAway || a.railQuiet() {
		return false
	}
	width, _ := a.size()
	return a.railColumns(width) == 0
}

// railStanding reports whether the roster is on the frame in either shape.
func (a *app) railStanding() bool { return a.railShowing() || a.railFull() }

// railRoom is the columns the roster's TEXT gets, seam excluded: its column's
// width where it has one, the whole frame where it is drawn over the body.
func (a *app) railRoom() int {
	width, _ := a.size()
	if a.railFull() {
		return width - ansi.StringWidth(railSeam)
	}
	return a.railColumns(width) - ansi.StringWidth(railSeam)
}

// railStowed reports whether the frame is drawing the CLOSED column's edge: the
// person put the column away, and the frame is wide enough that there was a
// column to put away.
//
// It is deliberately the exact complement of [app.railShowing] at every width
// that lends columns at all — one of the two is true whenever [railColsFor] is
// positive — so the right-hand strip of the frame always belongs to the roster
// in one of its two shapes, and never to nobody.
func (a *app) railStowed() bool {
	if !a.railAway || a.railQuiet() {
		return false
	}
	width, _ := a.size()
	return railColsFor(width) > 0
}

// railWidth is what the rail costs the conversation, in columns: its own where
// it stands, the grip's [railGripCols] where it is closed, and nothing at a
// width that lends it neither.
//
// THE CLOSED COLUMN IS CHARGED FOR TOO, which is what keeps the edge honest: the
// transcript is laid out, hit-tested and wrapped at [app.bodyWidth], so a strip
// the layout did not know about would be two columns of conversation with a
// handle drawn through them.
func (a *app) railWidth() int {
	if a.railStowed() {
		return railGripCols
	}
	if !a.railShowing() {
		return 0
	}
	width, _ := a.size()
	return a.railColumns(width)
}

// railGripRows draws the closed column's edge to exactly height rows.
//
// EVERY ROW IS [railGripCols] CELLS WIDE, blank ones included, and that is not
// decoration: [app.railJoin] pads the conversation out to [app.bodyWidth] only
// on the rows the rail gave it something for, so a strip that returned "" for
// its empty rows would let a long line of the transcript run out under the
// handle on some rows and not on others.
//
// THE HANDLE IS AT THE MIDDLE OF THE FRAME because that is where a hand reaches
// for the edge of a panel, and because the top of this strip is beside the
// oldest thing on screen while the bottom is beside the newest — neither is a
// place a person is looking. The whole strip answers a click, not the one cell
// (room.go's [app.railPress]), so nothing here has to be aimed at.
func (a *app) railGripRows(height int) []string {
	if height < 1 {
		return nil
	}
	blank := strings.Repeat(" ", railGripCols)
	out := make([]string, height)
	for i := range out {
		out[i] = blank
	}
	// THE HANDLE IS INK AND NOT DIM. Everything else this surface paints at the
	// right edge is a report about work — a count, an age, a state — and dim is
	// what a report is worth. A door is not a report: it is the one thing on the
	// frame a person has to FIND, and a person who cannot find it has lost the
	// column ([railGripGlyph] carries the report that said so).
	//
	// UNDER THE POINTER IT GOES FURTHER, which is this surface's one way of saying
	// a thing is pressable (hover.go's law), and the band is laid across both
	// cells rather than the one: it is the whole strip that answers a click, so it
	// is the whole strip that must light.
	mark := " " + a.pal.ink(a.linearMark(railGripGlyph, railGripGlyphASCII))
	if a.hoveringRailGrip() {
		mark = a.pal.cursor(" "+a.pal.accent(a.linearMark(railGripGlyph, railGripGlyphASCII)), railGripCols)
	}
	out[height/2] = mark
	// AND ONE CELL ABOVE IT, WHAT THE WORK IS DOING — while there is anything to
	// say. A closed column is the one state where this surface can be busy and
	// silent about it, and one glyph is what the space allows.
	if mark, hue := a.railGripState(); mark != "" && height > 1 {
		out[height/2-1] = " " + hue(mark)
	}
	return out
}

// railGripState is the one cell the closed edge whispers with: what the work is
// in, or nothing.
//
// TWO STATES EARN IT AND NO OTHERS. Something WAITING ON A PERSON outranks
// something running, because it is the only one of the two that is asking for a
// hand; anything else — idle, parked, done, and a session that has run nothing
// at all — says nothing, which is the emptiness law in one cell.
//
// The marks are home's own ([homeAskGlyph] and [homeLiveGlyph]), which is this
// program's existing vocabulary for "wants you" and "moving" said in a single
// column. The rail's own glyphs are a spinner and a tree, and neither is a thing
// that fits in one static cell.
func (a *app) railGripState() (string, func(string) string) {
	members := a.railMembers()
	switch {
	case len(members[railAttention]) > 0:
		return a.linearMark(homeAskGlyph, homeAskASCII), a.pal.ask
	case len(members[railRunning]) > 0:
		return a.linearMark(homeLiveGlyph, homeLiveASCII), a.pal.accent
	}
	return "", nil
}

// railGripAt reports whether a pointer at these coordinates is over the closed
// column's edge. It is the press's guard and the hover's alike, which is
// [app.railAt]'s own bargain: a strip that answered a click it would not light
// under the pointer is a strip that disagrees with itself about what it is.
func (a *app) railGripAt(x, y int) bool {
	if !a.railStowed() || x < a.bodyWidth() {
		return false
	}
	top := a.bodyTop()
	return top >= 0 && y >= top && y < top+a.viewHeight()
}

// bodyWidth is the conversation's own width, and it is what EVERY geometric
// question about the transcript resolves through — what the frame draws, where
// the wheel lands, which row a click hit. A rail the layout knew about and the
// hit-testing did not would deliver clicks to rows wrapped at another width.
func (a *app) bodyWidth() int {
	width, _ := a.size()
	if body := width - a.railWidth(); body > 0 {
		return body
	}
	return width
}

// railLine is one drawn line of the roster and what it belongs to. It is the
// column's [row] (render.go): the frame draws the text, the pointer hit-tests
// the entry, and the focus marker lands on the head — one mapping from geometry
// to the roster, because two would be a click that opened the node above the one
// under the pointer.
type railLine struct {
	// roomAction and roomSection share the drawn rows with pointer routing.
	roomAction  string
	roomSection int

	text string
	// entry indexes [app.railEntries], or -1 for the padding and the footer.
	entry int
	// head says this is the entry's FIRST line, which is the one a marker goes
	// on: a two-line node with two markers would read as two nodes.
	head bool
	// glyph is the row's FOLD CELL in the column's own coordinates, and badge the
	// ▸ +N a folded root wears. They are written at LAYOUT and read by the click,
	// which is the bargain the strip's chips make (taskstrip.go): the geometry is
	// recorded where it is decided, because a hit-test that recomputed it would
	// be measuring a row the frame has not drawn.
	//
	// glyph IS EMPTY ON EVERY FRAME WHERE THAT CELL IS NOT A CONTROL, which is
	// most of them: the cell holds the row's state until the pointer is on a row
	// that can fold, and only then does it become ▾ or ▸ ([app.railLead] says
	// why the press may not work this out for itself). An empty span is a span
	// that holds no column, so the cells fall to the row and the row is the
	// node's door.
	glyph hudSpan
	badge hudSpan
	// hint says this line is the footer's widen offer, which is pressable and
	// belongs to no entry.
	hint bool
	// stow says this line is the footer's last one, the column's own door
	// ([railStowHint]). It is a second flag rather than a kind on the line above
	// because both can be drawn at once and a press has to tell them apart: one
	// changes the column's width and the other takes it off the frame.
	stow bool
	// more says this line is the footer's door onto the TASK PAGE
	// ([taskSheetMoreHint] or [taskSheetPastHint], taskview.go) — a third flag for
	// the second one's reason: all three can be drawn at once, and a press has to
	// know whether it was asked to widen the column, to hide it, or to leave it
	// for a page that holds work this session never ran.
	more bool
	// door is the slash word this line TYPES INTO THE DRAFT when it is pressed —
	// the `+` row at the foot of each section (margin.go). It is the word itself
	// rather than a flag because there are two of them and they type two different
	// things, and the word is also what the pointer lights by.
	door string
	// stand is the id of the standing order this line draws, on a row of the
	// margin's standing section and nowhere else (margin.go). It is a string
	// because a standing item's id is one ([standing.Item.ID]), and "" is an
	// honest "this line is not an order" where a zero id could one day exist.
	stand string
	// jobs says this line is the jobs section's LABEL, which toggles the
	// section open and shut. It is a flag rather than a door-word because a
	// door TYPES into the draft and this line does not (jobsection.go).
	jobs bool
	// job is the id of the background job this line draws, or 0 for every
	// other line. It is an int because a job's id is one ([session.JobNotice.ID]),
	// and 0 is an honest "this line is not a job" — no job is published with
	// id 0 (jobstate.go's [app.showJobPage] refuses one).
	job int
	// fade is how deep in a CUT-OFF window this line sits, as one past the stop
	// of the fade ladder it takes: zero is full ink and the ordinary case, and
	// one, two or three are the last three lines of a column with more work
	// under them (depthfade.go). It is one-past rather than the stop itself
	// because a railLine is built in half a dozen places and the zero value has
	// to mean "not faded" in every one of them.
	//
	// IT IS DECIDED AT LAYOUT AND SPENT AT PAINT, which is the same bargain
	// [railLine.glyph] makes: whether a line is the third from the bottom of the
	// window is a fact only [app.railView] holds, and a painter that recomputed
	// it would be measuring a window the frame has not drawn.
	fade int
}

// railLines renders every entry, in order. It is the unwindowed list, and the
// window is taken out of it by [app.railView].
func (a *app) railLines(entries []railEntry, width int) []railLine {
	out := make([]railLine, 0, len(entries)+len(entries)/2)
	for i := range entries {
		rows, glyph, badge := a.railEntryRows(entries[i], width)
		for j, text := range rows {
			line := railLine{text: text, entry: i, head: j == 0}
			if j == 0 {
				line.glyph, line.badge = glyph, badge
			}
			out = append(out, line)
		}
	}
	return out
}

// railView is the whole column at a height: the window over the entries, the
// padding under it, and the footer at the bottom of it — exactly height lines.
//
// EVERY GEOMETRIC QUESTION ABOUT THE ROSTER GOES THROUGH HERE, the way every
// question about the conversation goes through [app.window]: the frame draws
// this, the pointer resolves through this, and the focus scrolls this. The
// window offset is written back as it is resolved, which is the same bargain
// [app.visible] makes with its row cache — the alternative is a scroll position
// recomputed in three places that agree until they do not.
//
// It reports the focused entry's index alongside the lines so its two callers do
// not each rebuild the entry list to ask the same question.
//
// WHAT IT COSTS IS BOUNDED BY WHAT IS OPEN, not by the session's length: every
// visible entry is rendered to measure the list, and the two groups that grow
// without limit are the two that open folded. The live groups are bounded by
// what the executor can actually run at once — and a person who expands `done
// 300` pays for it on the frames they are looking at it, which are frames with
// nothing animating on them (see [app.tasksAnimating]).
// The return door is outside both scrolling layouts, so depth and a long task
// list cannot move the way back to the conversation off screen.
const railMainAction = "main"
const railMainWord = "Back to main"

func (a *app) railView(height int) ([]railLine, int) {
	if height <= 0 || !a.railStanding() {
		return nil, -1
	}
	if !a.roomOpen() {
		return a.railContentView(height)
	}
	rows, focus := a.railContentView(height - 1)
	word := a.icon(tokens.GScopeUp) + " " + railMainWord
	head := railLine{text: a.pal.accent(fit(word, a.railRoom())), entry: -1, roomAction: railMainAction}
	return append([]railLine{head}, rows...), focus
}

func (a *app) railContentView(height int) ([]railLine, int) {
	if height <= 0 || !a.railStanding() {
		return nil, -1
	}
	if a.roomPanelShowing(height) {
		return a.roomPanelView(height)
	}
	room := a.railRoom()
	entries := a.railEntries()
	focus := a.railFocusIndex(entries)

	// THE LINES ARE LAID OUT BEFORE THE FOOTER IS ASKED FOR, which is the one
	// ordering this function is not free to choose: whether the footer offers the
	// wide tier is a fact about what the rows did to their titles, and a footer
	// built first would be answering it about the frame before this one
	// ([app.railFootRows]).
	a.railCramped = false
	// A SECTION EARNS ITS LABEL FROM A REAL ROW. The typeable doors remain when
	// nothing exists, while the emptiness law spends no pixels naming absence.
	head := a.marginHead(room, len(entries) > 0)
	lines := a.railLines(entries, room)
	foot, hint, door, more := a.railFootRows(room, height)
	body := height - len(foot)
	if body < 1 {
		body, foot, hint, door, more = height, nil, -1, -1, -1
	}

	// ── THE COLUMN IS A BUDGET AND NOT A STACK ────────────────────────────────
	//
	// Four things share thirty columns of somebody's screen — the label, the
	// roster, the sections under it (margin.go), and the footer — and they
	// used to be simply concatenated into one scrolling list. Which meant the
	// roster, the one part with no upper bound, could take all of it: thirteen
	// landed jobs pushed `standing` to the last row of the window, cut off, and
	// the orders that govern the conversation were reachable only by scrolling
	// past a wall of history. A list that can grow without limit will starve
	// anything stacked under it, every time.
	//
	// SO EVERY SECTION BUT THE ROSTER IS RESERVED FIRST, AND THE ROSTER TAKES WHAT
	// IS LEFT. The label is pinned above the window because a heading that scrolls
	// away is a heading nobody has when they need it; the footer is already
	// measured out of the body above; and the sections under the roster are
	// asked for a block that fits the rows they are allowed
	// ([app.marginRows], [marginStandFit] and [marginJobsFit] state what they
	// give up first).
	// Only then is the roster's window measured, and it is still the same
	// [listTop] scrolling the same offset — a budget is not a second scroller.
	//
	// AND THE ROSTER OUTRANKS THEM WHEN THERE IS NOTHING LEFT. A frame too short
	// for both drops the reserved block rather than the work: the doors are
	// geography and the work is the news.
	margin := a.marginRows(room, marginRoomFor(body-len(head), len(lines)))
	window := max(body-len(head)-len(margin), 0)

	// WORK THAT IS STILL GOING IS NEVER SCROLLED OFF THIS COLUMN. The families
	// are already sorted so that everything live leads ([app.railForest]), and
	// that alone was not enough: a person who walked the cursor down into two
	// hundred landed nodes took the running ones off the top of the window with
	// it, and the column that exists to say "this is happening" said nothing about
	// what was happening. So the live head is PINNED and only what is under it
	// scrolls.
	//
	// IT YIELDS ONE ROW, never more. A session with more live work than the column
	// is tall would otherwise pin the whole window and leave nothing to scroll; the
	// honest answer to a column that has run out of room is the page the footer
	// names ([taskSheetMoreHint], taskview.go) rather than a live row quietly
	// dropped.
	pin := 0
	if window > 1 {
		pin = min(a.railMovingHead(lines, entries), window-1)
	}
	tail := lines[pin:]
	// The rows the WINDOW gets, which is what is left of the roster's own share
	// once the pinned head has taken its own.
	scroll := window - pin

	// The cursor the window follows is the focused entry's first line, and the
	// offset itself when nothing is focused: a roster nobody is navigating stays
	// where it was rather than snapping back to the top under a landing node. A
	// cursor inside the pinned head needs no scroll at all — those lines are on
	// screen by construction — so the offset is merely clamped there.
	cursor := a.railTop + pin
	if focus >= 0 {
		for i, line := range lines {
			if line.entry == focus && line.head {
				cursor = i
				break
			}
		}
	}
	a.railTop = listTop(max(cursor-pin, 0), a.railTop, len(tail), scroll)

	out := make([]railLine, 0, height)
	out = append(out, head...)
	out = append(out, lines[:pin]...)
	i := a.railTop
	for ; i < len(tail) && len(out) < len(head)+window; i++ {
		out = append(out, tail[i])
	}
	// A LONG LIST'S TAIL FADES WITH DEPTH — NEVER STRIPES (depthfade.go). The
	// column is the surface's longest list and the one most often cut off, and
	// what it is cut off BY is a terminal's height rather than anything a person
	// chose — so the last lines before the fold step down toward the background
	// and say there is more of this than fits. A column whose last entry is on
	// screen fades nothing: there is nothing below it to point at.
	//
	// The depth is measured over the label and the pinned head as well as the
	// scrolling part, because the pinned live head is the sharpest head this
	// column has: work that is still going leads the column by construction, and
	// the fade walking away from it is exactly the shape the pin was already
	// drawing. It stops at the ROSTER'S OWN LAST ROW rather than at the bottom of
	// the column — what is under it is the reserved block, which is not the list
	// being cut off and must not read as the quiet end of one.
	for at := range out {
		if stop := tailStop(at, len(out), i < len(tail)); stop >= 0 {
			out[at].fade = stop + 1
		}
	}
	// THE RESERVED BLOCK FOLLOWS THE WORK RATHER THAN THE FRAME'S BOTTOM EDGE, so
	// a session with three tasks in it draws exactly the column it always drew —
	// the door directly under the last row — and a session with three hundred
	// draws the same block in the same order, one window further down.
	out = append(out, margin...)
	// AND WHAT THE SESSION'S OWN ROWS DID NOT NEED IS LEFT BLANK. It used to be
	// filled with a dulled sample of the project's record; that record is the task
	// page's, and the foot of this column names the door onto it (taskview.go says
	// why the sample went).
	for len(out) < body {
		out = append(out, railLine{entry: -1})
	}
	for i, text := range foot {
		out = append(out, railLine{
			text: text, entry: -1, hint: i == hint, stow: i == door, more: i == more})
	}
	return out, focus
}

// railMovingHead is how many lines at the top of the list belong to work that is
// MOVING — running right now, or standing still waiting on a person.
//
// IT IS THOSE TWO GROUPS AND NOT EVERY LIVE ONE, which is the difference between
// a head that stays small and a head that eats the column. What is running at
// once is bounded by the slots the executor has, and what is waiting on a person
// is bounded by the person; what is QUEUED is bounded by nothing at all — one
// plan can admit a hundred nodes in a breath — so a head that pinned the idle
// group would pin the whole window the first time somebody started an adaptive
// run. Queued work is a promise and promises can wait their turn in a scroll.
//
// It is the count through the LAST such line rather than the length of an
// unbroken run, because a family is drawn whole: a settled child sitting between
// two running siblings is part of the live shape, and a head that stopped at it
// would pin half a tree. Families with nothing live in them sort below every
// family that has ([app.railForest]), so what this measures is the moving region
// and not the whole column.
//
// A FOLDED ROOT COUNTS FOR WHAT IT IS HIDING. One row standing for a subtree
// with something running in it is that running work as far as this column is
// concerned, which is the same fact its glyph already carries ([app.railWorst]).
func (a *app) railMovingHead(lines []railLine, entries []railEntry) int {
	head := 0
	for i, line := range lines {
		if line.entry < 0 || line.entry >= len(entries) {
			continue
		}
		if a.railEntryMoving(entries[line.entry]) {
			head = i + 1
		}
	}
	return head
}

// railEntryMoving reports whether one drawn row is work that is running or
// waiting on a person, its hidden descendants included.
func (a *app) railEntryMoving(e railEntry) bool {
	if e.node == nil {
		return false
	}
	nodes := []*taskNode{e.node}
	if e.folded && e.worst != nil {
		nodes = append(nodes, e.worst)
	}
	for _, node := range nodes {
		switch a.railGroupOf(node) {
		case railAttention, railRunning:
			return true
		}
		if node.Paused() {
			return true
		}
	}
	return false
}

// railRows draws the roster to exactly height rows, or nil when there is none.
//
// The rows sit at the TOP of the column: the conversation grows upward from the
// input and the roster does not, because a list is read from its first row down.
//
// THE SEAM CARRIES THE FOCUS. Every line opens with the same two cells, and on
// the focused row those two cells are a heavier glyph in the accent — a marker
// rather than a band, because this column is two cells from a paragraph somebody
// is reading. It is drawn only while the roster HOLDS the keyboard: a cursor on
// a map that keys do not reach is a cursor that lies about what enter will do.
func (a *app) railRows(height int) []string {
	// A CLOSED COLUMN STILL DRAWS SOMETHING, and it is the edge rather than the
	// roster: [railGripCols] columns down the right of the frame with a handle in
	// them, which is the whole of what a person has to find their way back to
	// (the block above [railGripCols] says why it exists).
	if a.railStowed() {
		return a.railGripRows(height)
	}
	view, focus := a.railView(height)
	if len(view) == 0 {
		return nil
	}
	// THE SEAM IS A SEAM AND NOT A BORDER, so it is drawn only where there is
	// something on the other side of it. Over the body there is nothing to the
	// left of the roster, and a vertical rule down the left edge of a full-width
	// list is exactly the border this surface does not draw — so those two cells
	// go blank and keep their width, which is what the marker column is.
	seam := a.pal.dim(railSeam)
	if a.railFull() {
		seam = strings.Repeat(" ", ansi.StringWidth(railSeam))
	} else if a.hoveringRailSeam() {
		seam = a.pal.cursor(a.pal.accent(railSeam), ansi.StringWidth(railSeam))
	}
	room := a.railRoom()
	entries := a.railEntries()
	out := make([]string, len(view))
	for i, line := range view {
		lead := seam
		// A JOB ROW takes the accent mark the way a task row does. The jobs
		// SECTION LABEL does not — replacing the seam there with ▌ left a notch
		// in the continuous │, which read as a glitch on a line that is a fold
		// toggle rather than a piece of work. The cursor band still lands on
		// the label (jobHere below), so the keyboard's place is not silent.
		jobHere := (a.railWhere.jobs && line.jobs) || (a.railWhere.job != 0 && line.job == a.railWhere.job)
		jobRowHere := a.railWhere.job != 0 && line.job == a.railWhere.job
		if (focus >= 0 && line.head && line.entry == focus) || jobRowHere {
			lead = a.pal.accent(a.linearMark(railMark, railMarkASCII))
		}
		text := line.text
		// THE ROW WHOSE DOOR YOU WALKED THROUGH WEARS THE SELECTION BAND, which is
		// what makes this column a map of where you are rather than a list of what
		// exists: a person standing inside a node's page could read the page's own
		// header for its name, and then had to read it, because nothing in the
		// roster beside it said which of these rows they were behind.
		//
		// It is [palette.selected] and not a new mark — the same background the strip
		// puts on the chip of the room a person is standing in (taskstrip.go's
		// [app.stripChip]), and the same one every selected row on this surface
		// wears (palette.go). It covers EVERY line of the entry, not just its head:
		// a node's row is two lines tall when it has something to say under its
		// title, and a band on half of it would read as a row cut in two.
		//
		// AND EVERY NODE ROW TAKES THE HOVER STEP, because every node row answers
		// to a click — the whole row is that node's door (hover.go's own law). It
		// is applied here rather than inside the row's render for the reason the
		// transcript applies it in its layout pass: one place knows where the
		// pointer is, and no renderer has to remember it exists.
		//
		// SELECTED OUTRANKS HOVERED, which is the law the overlay's rows already
		// state: the two backgrounds cannot nest — each closes with SGR 49 — and
		// of the two facts, "you are in here" is the one that is still true when
		// the pointer moves away.
		var node *taskNode
		if line.entry >= 0 && line.entry < len(entries) {
			node = entries[line.entry].node
		}
		switch {
		case line.roomAction != "" && a.hot.kind == hoverRoomControl && a.hot.key == line.roomAction:
			text = a.hoverRow(text, room)
		case a.roomStandingOn(node):
			text = a.pal.selected(text, room)
		case node != nil && a.hoveringRail(node):
			text = a.hoverRow(text, room)
		case jobHere || (focus >= 0 && line.entry == focus):
			// AND THE KEYBOARD'S OWN ROW TAKES THE CURSOR STEP, on the same terms
			// the pointer's does. This column used to say the keyboard's position
			// with the marker in the lead and nothing else, on the argument that a
			// filled row two cells from the conversation would be a block of colour
			// beside a paragraph somebody is reading. That argument was written
			// against the ONE background this surface then had — the one that is now
			// the selected step, and that the row above still wears. The cursor step
			// is a rung quieter than it on purpose, and it is the rung the ladder
			// names for "the row a keyboard cursor sits on".
			//
			// So the two facts this column carries are now two STEPS rather than a
			// step and a glyph: the room you walked into is the louder ground, the
			// row ↑/↓ has reached is the quieter one, and a person can see both at
			// once and tell them apart. The marker stays in the lead, because the
			// ground is the secondary cue and the accent in the lead is the first.
			text = a.pal.cursor(text, room)
		case line.more && a.hoveringRailMore():
			// THE DOOR ONTO THE TASK PAGE TAKES IT TOO, on the terms every other
			// pressable line here takes it on: it answers to a click, so the pointer
			// says so ([taskSheetPastHint], taskview.go).
			text = a.hoverRow(text, room)
		case line.stow && a.hoveringRailDoor():
			// AND THE COLUMN'S OWN DOOR TAKES IT TOO, on the terms every other
			// pressable line here takes it on: it answers to a click, so the pointer
			// says so ([app.railDoorLine]).
			text = a.hoverRow(text, room)
		case line.door != "" && a.hoveringMarginDoor(line.door):
			// AND THE MARGIN'S TWO `+` ROWS, on the same terms and for the same
			// reason: each one is a whole row that answers to a click (margin.go).
			text = a.hoverRow(text, room)
		case line.stand != "" && a.hoveringMarginStand(line.stand):
			text = a.hoverRow(text, room)
		case line.fade > 0 && !(focus >= 0 && line.entry == focus) && !jobHere:
			// THE TAIL FADES AND THE CURSOR NEVER DOES (depthfade.go). It is the
			// last arm of this switch for the reason the first one is first: a row
			// wearing a background has already been told how loud to be, and the
			// row the keyboard is standing on is the one fact the fade exists to
			// keep legible. Every line of the focused entry is spared, not just its
			// head, for the band's own reason — half a row dimmed reads as a row cut
			// in two.
			text = a.pal.fadeRow(text, line.fade-1)
		}
		out[i] = lead + text
	}
	return out
}

// railEntryAt is the roster's hit-testing: which entry is drawn on this screen
// row, and whether there is one at all.
//
// It reads the SAME line list [app.railRows] draws — the same groups, the same
// folds, the same window — because a roster whose layout and whose clicks
// disagreed would open the room of the node above the one under the pointer.
//
// The roster's first row is the first row of the BODY REGION (view.go joins it
// from index zero there, and stacks it there whole when it is drawn over the
// body), which the rows the frame pins above that region move down by their own
// height — so a screen row is a roster index minus [app.topHeight] and not
// before.
func (a *app) railEntryAt(y int) (railEntry, bool) {
	line, ok := a.railLineAt(y)
	if !ok || line.entry < 0 {
		return railEntry{}, false
	}
	entries := a.railEntries()
	if line.entry < len(entries) {
		return entries[line.entry], true
	}
	return railEntry{}, false
}

// railLineAt is the same question one level down: the LINE drawn on this screen
// row, spans and all. The press needs it because two of the roster's targets are
// narrower than the row they are on — a root's glyph cell and its ▸ +N — and
// where those landed is a fact only the layout has.
func (a *app) railLineAt(y int) (railLine, bool) {
	y -= a.topHeight()
	view, _ := a.railView(a.viewHeight())
	if y < 0 || y >= len(view) {
		return railLine{}, false
	}
	return view[y], true
}

// railNodeAt is which NODE is drawn on this screen row, or nil.
func (a *app) railNodeAt(y int) *taskNode {
	e, ok := a.railEntryAt(y)
	if !ok {
		return nil
	}
	return e.node
}

// ── the focus, and the keyboard it answers to ───────────────────────────────

// railFocusAt finds the entry a spot names, or -1.
func railFocusAt(entries []railEntry, spot railSpot) int {
	for i, e := range entries {
		if e.node != nil && e.node.id == spot.id {
			return i
		}
	}
	return -1
}

// railFocusIndex is where the cursor is in the current entry list, or -1 when
// the roster does not have the keyboard.
//
// THE CURSOR FOLLOWS THE WORK AND THEN THE FAMILY. A node that lands does not
// move any more — a family is drawn where it always was — but a fold closing
// over it does, and that is now the one way a focused row leaves the list. Where
// its rows went is its nearest drawn ancestor, which is exactly the row standing
// for it, so that is where the cursor stands.
func (a *app) railFocusIndex(entries []railEntry) int {
	if !a.railHold || a.railWhere.onJobs() || len(entries) == 0 {
		return -1
	}
	if at := railFocusAt(entries, a.railWhere); at >= 0 {
		return at
	}
	if node := a.tasks[a.railWhere.id]; node != nil {
		_, byKey := a.railKin()
		for up := byKey[node.ParentID()]; up != nil; up = byKey[up.ParentID()] {
			if at := railFocusAt(entries, railSpot{id: up.id}); at >= 0 {
				return at
			}
		}
	}
	// The node itself is gone — /new, or a session that dropped it. The top of the
	// column is all that is left, and a roster that dropped to row zero for any
	// lesser reason would be moving a person's place for them.
	return 0
}

// railTake gives the roster the keyboard, or hands it back.
//
// On a frame with no column it is also what RAISES the roster, over the body
// ([app.railFull]): asking for the roster and asking for the keyboard are the
// same request, and which of the two shapes answers it is the frame's business
// and not the caller's.
func (a *app) railTake(hold bool) {
	if hold && !a.railAvail() {
		return
	}
	if hold {
		// ASKING FOR THE ROSTER IS ASKING FOR IT TO BE THERE. alt+t on a frame
		// whose column has been put away, and the strip's own +N door, are both
		// requests for the whole list — and a request that moved a cursor inside a
		// column nobody can see would be the key doing nothing at all. So the
		// column comes back first, on the same terms as any other way of bringing
		// it back: remembered.
		a.railStow(false)
	}
	a.railHold = hold
	if hold {
		// WHERE THE CURSOR LANDS IS THE FIRST ROW THERE IS. The key is refused
		// outright on a column with no rows of this session's ([app.railAvail]), so
		// there is always one to land on — a task, or the jobs section when
		// that is the work this conversation has.
		spots := a.railSpots()
		if railSpotAt(spots, a.railWhere) < 0 && len(spots) > 0 {
			a.railWhere = spots[0]
		}
	}
	a.touch()
}

// railKey is the roster's claim on the keyboard, and it is a claim it can only
// make ONCE IT HAS BEEN GIVEN ONE (alt+t on, esc off).
//
// The draft is this surface's rest state — a person types at it without looking
// — so a map that answered ↑ whenever it happened to be on screen would make
// every keystroke a question about which zone has the focus. Held, it takes six
// keys and gives everything else back: the letters still reach the box, so a
// person who starts typing is typing, not navigating.
//
// The guard is the same precedence law input.go states, restated rather than
// relied on because those keys are that file's: the door, the question the
// SESSION is blocked on, the three modal overlays and the two typed lists all
// outrank a map of work.
//
// TWO KEYS ARE READ WITHOUT THE HOLD, and they are the two that are about the
// roster rather than inside it: alt+t, which asks for it, and ctrl+g, which
// takes the column off the frame and puts it back ([app.railStow]).
//
// HOME IS ON THE STAND-DOWN LIST BESIDE THE OTHER FULLSCREEN PAGES, and it was
// missing from it. Under [railSlimFloor] a held roster is drawn OVER the body
// ([app.railFull]) — and home is drawn INSTEAD of the body (view.go's
// [app.frame] returns home's frame long before the roster is asked for), so a
// hold carried in from the conversation underneath was a list nobody could see
// taking the six keys home's own foot advertises: `↑↓ move · enter open · esc
// close`, and the follow-up enter of a stacked `ask here` with them. The only
// thing left that moved the selection was the pointer.
//
// AND THE THINKING CHOOSER IS ON IT FOR THE SAME REASON, which is the one place
// the ladder's two surfaces could have fought. [effortKey] means "move the rung
// of the thing you are standing on" (effortscope.go), and while that chooser is
// up the thing you are standing on is the CONVERSATION'S ladder — five rungs
// drawn over the box, with the chord named in its own foot. Without this line a
// held roster took the chord first and moved a TASK's rung while the
// conversation's list sat open on screen, and took ↑↓, enter and esc off it too:
// a list nobody could drive, exactly what home was doing above. input.go reads
// the chooser as modal beside the crew list; this is that law restated where the
// roster can see it.
func (a *app) railKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch {
	case key == "ctrl+c", a.asking(), a.awaitingTask(),
		a.at(pageSettings), a.at(pageTasks), a.at(pageHome), a.pick.open, a.copy.on,
		a.welcome.open, a.menu.open, a.comp.open, a.effPick.open:
		return nil, false
	}
	if key == railStowKey {
		// A RUNNING COMMAND OWNS ctrl+g while it can be kept. The roster is read
		// before the message box, so standing down here is what lets the command's
		// door answer; with no such row the column behaves exactly as it always has.
		if a.promotableRow() >= 0 {
			return nil, false
		}
		// THE ONE KEY THAT ANSWERS WITH THE COLUMN ITSELF. It is read before the
		// hold below because it is true in both postures — a column that is up goes
		// away, a column that is away comes back — and it is the only key on this
		// map a person may press without having asked for the roster first.
		//
		// IT ONLY ACTS ON A ROSTER THAT IS ON THE FRAME, or on one it has already
		// taken off. The column stands empty now ([app.railShowing]), so "on the
		// frame" no longer needs any tasks behind it — a column carrying nothing but
		// the project's own record rows, or nothing but its label, closes and reopens
		// like any other. Under [railSlimFloor], where there is no column to close and
		// nobody has raised the overlay, it still falls through untouched, exactly as
		// alt+t does. A keystroke that silently moved a state nothing is drawing is a
		// keystroke a person cannot tell they pressed, and this one would move it into
		// the NEXT session as well.
		if !(a.railStanding() || a.railAway) {
			return nil, false
		}
		a.railStow(!a.railAway)
		return nil, true
	}
	if key == railHoldChord {
		// ONE KEY AT EVERY WIDTH. With a column on the frame it hands the roster
		// the keyboard; without one it raises the roster over the body, which is
		// the same act with the same state behind it ([app.railFull]). Pressed
		// again — or esc — it puts it away.
		//
		// THE CHORD IS alt+t AND NOT ctrl+t ([railHoldChord]). ctrl+t is the
		// new-tab key now, at every one of these widths and with the roster
		// holding the keyboard as well — a person on a running task who presses
		// it gets a fresh conversation, not a column that folds away under them.
		if !a.railHold && !a.railAvail() {
			// Nothing to hold. The key falls through rather than being eaten
			// silently, so a surface that grows another meaning for it later is
			// not fighting a map that is not on screen.
			return nil, false
		}
		a.railTake(!a.railHold)
		return nil, true
	}
	if !a.railHold {
		return nil, false
	}
	switch key {
	case "esc":
		// esc is the dismiss key everywhere on this surface, and what it dismisses
		// here is the focus itself — back to the box, which is where the keyboard
		// lives when nobody has asked for it.
		a.railTake(false)
		return nil, true
	case "up":
		a.railMove(-1)
		return nil, true
	case "down":
		a.railMove(1)
		return nil, true
	case "right":
		a.railOut()
		return nil, true
	case "left":
		a.railIn()
		return nil, true
	case railWidenChord:
		// WIDEN, IN THE ONE SPELLING NOTHING CAN EAT. It used to be the bare
		// letter `w` with no guard on it at all, and it is the only bare letter
		// this surface ever bound to a VIEW TOGGLE rather than to an answer
		// (chordfocus.go states the whole law): nothing was blocked on it, so
		// there was no moment at which pressing it was the only thing a person
		// could have meant — and a held roster is a state people type under.
		// Sentences came out as `riting the port` and `orktree`.
		a.railWiden(!a.railWide)
		return nil, true
	case railWidenKey:
		// THE BARE LETTER SURVIVES WHERE THERE IS NO BOX TO STEAL FROM, which is
		// the home sheet's own rule said about the roster ("it is modal and has
		// no box, so a letter here cannot be the start of anybody's sentence",
		// homesheet.go). The full-frame roster IS that: it is drawn over the
		// body, the composer is not on the frame, and `w` cannot be the first
		// letter of anything. Beside a column it falls through to the box as the
		// letter it is.
		if !a.railFull() || a.chordsStandDown() {
			return nil, false
		}
		a.railWiden(!a.railWide)
		return nil, true
	case "ctrl+v":
		// HOW HARD THE FOCUSED NODE THINKS, one step up the ladder. It is read
		// here — inside the hold, beside the keys that move the cursor — because
		// the roster's cursor IS what "the task you are standing on" means while
		// no room is open, and a rung moved from a list of work is the same
		// gesture as a rung moved from inside one (taskeffort.go). A room open
		// over the roster takes it first, on [app.effortTaskHere]'s precedence.
		a.cycleTaskEffort()
		return nil, true
	case "enter":
		if cmd, ok := a.jobEnter(); ok {
			return cmd, true
		}
		return a.railEnter(), true
	case "tab":
		// EATEN AND NOTHING DONE. tab over an empty box switches conversations
		// (input.go's seventeenth rung), and the roster is read before the box is
		// — so without this case a person would arrive in another conversation
		// holding a rail focus they cannot see. The rail's rule is that explicit
		// focus outranks ambient place, and this is that rule applied to one more
		// key; the way out is esc, which the hint already advertises
		// ([railHoldHint]).
		return nil, true
	}
	// THE ANSWERS TO THE ONE QUESTION A ROW CAN BE ASKING, on the row that is
	// asking it (tasksettle.go's [app.railSettleKey]). It is read last so that
	// nothing above it changes meaning, and it takes the same three letters the
	// card and the room take, under the same guard.
	if a.railSettleKey(msg) {
		return nil, true
	}
	return nil, false
}

// railMove walks the drawn rows, and the drawn rows are what a fold left behind.
// The walk clamps at both ends, the way every other list on this surface does
// ([moveCursor]).
//
// IT WALKS THIS CONVERSATION'S WORK AND NOTHING ELSE, because that is the whole
// of what the column draws. The walk used to carry on into a dulled sample of the
// project's record at the foot of it, which meant a person holding ↓ left this
// conversation for another one without the column ever saying so; the record is
// the task page's now, and the foot of the column is a door onto it rather than a
// continuation of this list (taskview.go).
func (a *app) railMove(delta int) {
	spots := a.railSpots()
	at := railSpotAt(spots, a.railWhere)
	if at < 0 {
		return
	}
	a.railWhere = spots[moveCursor(at, delta, len(spots))]
	a.touch()
}

// railSpots is every row the keyboard walks: this conversation's work, then
// the jobs section under it. One walk, so enter on the jobs label is the
// same key that opens a task, and a person who held ↓ off the last family
// lands on the section that is actually next.
func (a *app) railSpots() []railSpot {
	entries := a.railEntries()
	out := make([]railSpot, 0, len(entries)+len(a.jobs)+1)
	for _, e := range entries {
		out = append(out, railSpotOf(e))
	}
	return append(out, a.jobSpots()...)
}

func railSpotAt(spots []railSpot, want railSpot) int {
	for i, s := range spots {
		switch {
		case s.jobs && want.jobs:
			return i
		case s.job != 0 && s.job == want.job:
			return i
		case !s.onJobs() && !want.onJobs() && s.id != 0 && s.id == want.id:
			return i
		}
	}
	return -1
}

// railWheelAt reports whether a wheel turned at these coordinates belongs to the
// side column — the pointer is over the column's cells AND over a row the column
// actually drew.
//
// THE SECOND HALF IS NOT PEDANTRY. [app.railAt] answers about the whole right
// strip of the frame at every height, which is what a press wants; a wheel wants
// the drawn rows, because the strip also runs behind the top chrome and the draft
// box, and a turn over the status line is a turn over the conversation's
// furniture rather than over the roster.
func (a *app) railWheelAt(x, y int) bool {
	if a.railFull() || !a.railAt(x, y) {
		return false
	}
	_, ok := a.railLineAt(y)
	return ok
}

// railScroll is the wheel's answer over the column, and it moves the SAME offset
// the keyboard moves ([app.railTop], resolved by [listTop] in [app.railView]).
// There is one scroller on this column and there will never be two.
//
// WHILE THE ROSTER HOLDS THE KEYBOARD THE WHEEL WALKS THE CURSOR, which is the
// bargain the full-frame roster already makes (app.go's wheel): the window
// follows the focus while there is one, so an offset nudged out from under it
// would be undone by the next layout and the column would read as a list that
// refuses to move. Without a focus there is no cursor to walk and the offset is
// the whole of the state, so the wheel moves it directly.
//
// THE BOTTOM IS CLAMPED WHERE EVERY OTHER LIST CLAMPS IT. [listTop] holds the
// offset inside the line count on the layout that follows, so a wheel spun past
// the end of a short roster lands on the last window rather than on blank rows —
// and this is deliberately not clamped twice, because the line count is a fact
// the layout holds and a second copy of it here is the copy that would drift.
func (a *app) railScroll(delta int) {
	if a.railHold {
		a.railMove(delta)
		return
	}
	if top := max(a.railTop+delta, 0); top != a.railTop {
		a.railTop = top
		a.touch()
	}
}

// railCursorTo parks the cursor at one row of that list.
func (a *app) railCursorTo(entries []railEntry, at int) {
	if at < 0 || at >= len(entries) {
		return
	}
	a.railWhere = railSpotOf(entries[at])
}

// railOut is →, and it is the tree grammar every file manager a person has used
// spells the same way: on a folded root it OPENS the family, and on a family
// already open it steps INTO it, onto the first child.
//
// ON A LANDED ROW IT OPENS THE ROW'S OWN BLOCK, which is the same grammar one
// scale down: → is "show me what is inside this", and what is inside a row that
// has come home is the branch it kept or the log it wrote ([app.railTucks]). On
// anything else it does nothing — there is nothing further out to go.
func (a *app) railOut() {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 {
		return
	}
	e := entries[at]
	if !e.root {
		if a.railTuckShut(e.node) && a.railTucks(e) {
			a.railSetOpen(e.node, true)
		}
		return
	}
	if e.folded {
		a.railSetOpen(e.node, true)
		return
	}
	if at+1 < len(entries) {
		a.railWhere = railSpotOf(entries[at+1])
		a.touch()
	}
}

// railIn is ←, and it is the mirror: on an open root it FOLDS the family, on a
// landed row whose block is showing it tucks the block back away, and anywhere
// else it walks up to the parent row.
//
// THE CURSOR NEVER STAYS ON A ROW THE FOLD TOOK AWAY. Folding from the root
// leaves it on the root, which is the row the family is now standing in; jumping
// from a leaf leaves it on the parent, which is the row a second ← will fold.
func (a *app) railIn() {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 {
		return
	}
	e := entries[at]
	if e.root && !e.folded {
		a.railSetOpen(e.node, false)
		return
	}
	if !e.root && !a.railTuckShut(e.node) && a.railTucks(e) {
		a.railSetOpen(e.node, false)
		return
	}
	depth := len(e.stems)
	if depth == 0 {
		return
	}
	for i := at - 1; i >= 0; i-- {
		if len(entries[i].stems) < depth {
			a.railWhere = railSpotOf(entries[i])
			a.touch()
			return
		}
	}
}

// railWiden takes the third width tier, or gives it back. It is sticky — a
// person who widened the column meant to keep it — and it goes with the nodes
// when the session does ([app.dropTasks]).
func (a *app) railWiden(wide bool) {
	if a.railWide == wide {
		return
	}
	a.railWide = wide
	a.touch()
}

// railStow puts the column away, or brings it back. It is what ctrl+g does, what
// the footer's last line does when it is pressed, and what any request for the
// roster does on its way in ([app.railTake]).
//
// THE COLUMN IS THIRTY COLUMNS OF SOMEBODY ELSE'S PARAGRAPH. Every other tier
// this file offers is a negotiation with the frame's width — full, slim, gone
// under the breakpoint, wide on request — and none of them could answer the one
// thing a person actually says about a sidebar, which is "not now". So this is
// the person's own answer and it OUTRANKS the width tiers and the roster's own
// "one node raises it" rule alike: put away, the column stays away through
// landings, through new work, and into the next session.
//
// WHAT IS NOT ALLOWED IS WORK GOING QUIET. The column is the whole of what a
// session says about itself, so closing it hands the job back to the two
// surfaces that were written for a frame with no column on it: the strip above
// the conversation, which draws a chip per RUNNING node and stands itself up the
// moment this one stands down (taskstrip.go's [app.stripShowing]), and the
// legend's hint slot, which names the key back while there is anything to come
// back to (render.go's [railBackHint]). Neither says a word about a session that
// has run nothing, which is the emptiness law and also the truth.
//
// THE CHOICE IS WRITTEN TO DISK EVERY TIME IT MOVES, and A FAILED WRITE IS
// DROPPED, exactly as consent.go's is: the column has already moved on screen by
// the time this runs, and an unwritable profile directory must not become a
// terminal that cannot close its own sidebar. What it costs is that the next
// session opens where the last one was told to, which is the behaviour of a
// session that has never been told anything.
func (a *app) railStow(away bool) {
	if a.railAway == away {
		return
	}
	a.railAway = away
	if away {
		// THE KEYBOARD GOES BACK TO THE DRAFT WITH THE COLUMN. A hold left standing
		// on a roster that is not drawn is six keys taken from the box by a list
		// nobody can see, and [app.railFull] would raise the overlay the moment the
		// frame narrowed.
		a.railHold = false
	}
	_ = config.SaveTaskColumn(a.profileDir, !away)
	a.touch()
}

// railEnter is the one activating key, and it opens the door that EXISTS for the
// row under it — the same law the task page states ([app.taskSheetEnter],
// taskview.go), because these are two lists of the same work.
//
// A NODE THIS SESSION HOLDS OPENS ITS ROOM. Folding has its own two keys, which
// is what took the overload off this one.
//
// EVERY ROW OF THIS COLUMN IS SUCH A NODE, so that is the whole of what this key
// does. Work an earlier conversation ran has no room and never will — a room is a
// live lane onto a node in this session's graph — and it is reached through the
// task page instead, where enter goes inside its card (taskrecord.go).
func (a *app) railEnter() tea.Cmd {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 || entries[at].node == nil {
		return nil
	}
	a.openRailRoom(entries[at].node)
	return a.takeRoomPump()
}

// ── the footer ──────────────────────────────────────────────────────────────

// railFootOrder is the order the footer counts the groups in, and it is not the
// column's order: the column leads with what is asking for a decision because
// that is where the eye starts, and the footer leads with what is HAPPENING
// because a total is read as a state of the session.
var railFootOrder = [railGroupCount]railGroup{railRunning, railAttention, railIdle, railParked, railDone}

// railFootMax is how many lines the footer may spend. Three is the whole
// aggregate at the full width; a fourth would be the column reporting on itself.
const railFootMax = 3

// railFootRows is the aggregate: what the window cannot show, said once at the
// bottom of the column.
//
//	Σ $1.42 · 312k tok
//	3 running · 1 needs you
//	148 parked · 12 done
//
// THE MONEY IS THE SESSION'S, AND THAT IS THE HONEST SUM. Per-node spend is not
// on the seam and cannot be: internal/session folds a finished node's usage into
// the session's own auxiliary total the moment its child closes (task_run.go's
// foldTaskUsage), so the figure beside the Σ ALREADY CONTAINS every node in this
// column, plus the conversation that proposed them. It is therefore drawn as the
// whole and never per row — a per-row share is the one number this surface would
// have to invent — and the Σ is what says so.
//
// The two figures are drawn only when they are not zero. A session that has been
// told nothing about what it spent says nothing, rather than reporting $0.00
// beside a hundred and forty-eight nodes.
// AND THE GROUP WORDS OUTLIVED THE GROUPS. The column stopped filing nodes under
// five headings ([app.railEntries] draws families now), and the five words are
// still the vocabulary a person has for what a session is doing — so the count
// of each is what the bottom of the column says, across every node in the
// forest, folded or not.
//
// IT ALSO CARRIES THE ONE CONTEXTUAL OFFER ON THIS SURFACE. When a title is
// being cut by its own indent and the frame could lend the wide tier, the last
// line of the footer says so ([railWideHint]) — and it says it only then. A
// permanent "w widens" is chrome charged to every session that never grew a
// tree. It reports which of its lines that offer landed on, or -1, because the
// line is pressable and the press has to know where it was drawn.
//
// AND UNDER IT, THE COLUMN'S OWN DOOR ([railStowHint]). The chevron is always
// live, while the chord is named only when the column actually owns it: a
// promotable foreground command takes ctrl+g first. Widening is an offer the
// column makes about itself when a title is being cut; hiding remains a pointer
// answer at every moment and a keyboard answer whenever no command can be kept.
func (a *app) railFootRows(width, height int) ([]string, int, int, int) {
	if width < 8 || height < 4 {
		return nil, -1, -1, -1
	}
	var segs []string
	// THE BOOKS DECIDE WHETHER A FIGURE IS DRAWN AND THE CLOCK DECIDES WHAT IT
	// SAYS. The guards ask the exact totals, because the emptiness law is about
	// whether there is anything to report; the figures themselves come off the
	// eased readings, so this foot counts up with the status line rather than
	// jumping beside it (reveal.go).
	if a.cost > 0 {
		segs = append(segs, dollars(a.spendDrawn()))
	}
	if a.tokens > 0 {
		segs = append(segs, tokenWord(a.tokensDrawn())+" tok")
	}
	members := a.railMembers()
	for _, g := range railFootOrder {
		if n := len(members[g]); n > 0 {
			segs = append(segs, itoa(n)+" "+railGroupWords[g])
		}
	}
	// IN THIS TERMINAL'S OWN SPELLING of the modifier (chords.go), because the
	// offer names a chord now rather than a bare letter and a Mac's keycap says
	// `⌥`.
	hintText := a.chords.say(railWideHint)
	if a.railWide {
		hintText = a.chords.say(railNarrowHint)
	}
	offer := a.railOffersResize() && ansi.StringWidth(hintText) <= width
	// THE DOOR IS ONLY DRAWN WHERE THERE IS A COLUMN TO CLOSE. Over the body the
	// roster is an overlay a person raised with alt+t and drops with esc
	// ([app.railFull]), and a second way out named at the bottom of it would be
	// two exits from a room with one.
	// The chevron and its space are charged for here, because the door is drawn
	// with them ([app.railDoorLine]) and a width test that measured only the words
	// would let the mark run off the end of a narrow column.
	stow := !a.railFull() && ansi.StringWidth(a.railDoorHint())+2 <= width
	// THE DOOR ONTO THE TASK PAGE IS OFFERED ONLY WHEN THERE IS MORE BEHIND IT,
	// which is the emptiness law applied to an affordance rather than to a figure.
	// A door on a column that is already showing everything is a row that promises
	// a page and delivers the list you were looking at.
	//
	// AND IT WEARS THE NAME OF WHAT IS BEHIND IT. A project with a record says
	// `earlier`, because that is the section the page opens on and the word a
	// person is looking for; a column whose only held-back thing is a family it
	// folded says `view more`, because there is no earlier work to promise
	// ([taskSheetPastHint] and [taskSheetMoreHint], taskview.go). It is ONE door
	// either way — one line, one press, one page.
	record := a.railHasRecord()
	viewText := taskSheetMoreHint
	if record {
		viewText = taskSheetPastHint
	}
	view := ansi.StringWidth(viewText) <= width && (record || a.railFoldedAny())
	if len(segs) == 0 && !offer && !stow && !view {
		return nil, -1, -1, -1
	}
	// The footer never takes more than a third of the column: a roster that is
	// mostly its own summary has stopped being a roster.
	rooms := min(railFootMax, height/3)
	lines := railPack(segs, width, rooms, railSigma)
	out := make([]string, 0, len(lines)+2)
	// ONE BLANK ABOVE IT, when the column can lend one — whitespace is how this
	// surface separates blocks, and a rule across a two-cell column would be a
	// border on a seam.
	if len(lines)+1 < height {
		out = append(out, "")
	}
	for _, line := range lines {
		out = append(out, a.pal.dim(line))
	}
	// THE PAGE'S DOOR GOES DIRECTLY UNDER THE TALLY, above the two lines about the
	// column itself. The order is what the lines are ABOUT: the counts say what
	// this session has, the door says where the rest of it is, and widening and
	// hiding are answers to "how much of my screen is this taking". A person
	// reading the tally and wanting more finds the next line saying so.
	//
	// IT IS DIM, which is the same weight the tally above it wears and the
	// footnoted record rows used to. A door onto a month of other people's
	// afternoons is not a thing this column should raise its voice about; it is a
	// thing it should never fail to mention.
	more := -1
	if view && len(out)+1 < height {
		more = len(out)
		out = append(out, paintHint(viewText, a.pal, a.pal.dim))
	}
	hint := -1
	if offer && len(out)+1 < height {
		hint = len(out)
		out = append(out, paintHint(hintText, a.pal, a.pal.dim))
	}
	// The door goes UNDER the width offer, at the very bottom of the column, which
	// is where a person looks for the way out of anything.
	door := -1
	if stow && len(out)+1 < height {
		door = len(out)
		out = append(out, a.railDoorLine())
	}
	return out, hint, door, more
}

// railDoorLine is the standing column's own door as it is drawn: the chevron
// that closes it, and then the chord only while the chord does the same thing.
//
// THE CHEVRON IS THE CONTROL AND THE WORDS ARE THE LABEL, which is why they are
// painted at two weights. `ctrl+g hide` tells the hand that types chords what to
// press only while no foreground command owns that key; with one running, the
// label is simply `hide`. The `❯` is what the hand that does NOT type chords
// presses either way, so it takes the ink — the same split the closed edge makes
// at the other end of the cycle ([app.railGripRows]).
//
// IT POINTS RIGHT AND ITS TWIN POINTS LEFT, and between them the pointer can go
// round the whole cycle: `❯` sends the column off the right edge, `❮` brings it
// back over the conversation. One control, two states, and neither of them a
// chord somebody had to be told about.
func (a *app) railDoorLine() string {
	mark := a.linearMark(railGripOpenGlyph, railGripOpenGlyphASCII)
	ink := a.pal.ink
	if a.hoveringRailDoor() {
		ink = a.pal.accent
	}
	return ink(mark) + " " + paintHint(a.railDoorHint(), a.pal, a.pal.dim)
}

// railDoorHint names only the keyboard action available on this frame. The
// pointer's chevron still hides the column while a command owns ctrl+g, so the
// verb stays and only the unavailable chord comes off the line.
func (a *app) railDoorHint() string {
	if a.promotableRow() >= 0 {
		return "hide"
	}
	return railStowHint
}

// railDoorAt reports whether a pointer is on that line. It is the hover's guard,
// and the press resolves the same fact through [railLine.stow] — one geometry
// asked twice, because the line is found by the layout either way.
func (a *app) railDoorAt(x, y int) bool {
	if !a.railAt(x, y) || a.railSeamAt(x, y) {
		return false
	}
	line, ok := a.railLineAt(y)
	return ok && line.stow
}

// railFoldedAny reports whether the column is standing one row for a family it
// has folded — which is the SECOND of the two things that earn the footer's door
// onto the task page, the first being the project's own record
// ([app.railHasRecord], taskview.go).
//
// A folded family is work the column is deliberately not drawing and the page
// draws every family whole, so the door is honest. A landed node of this
// session's, drawn on the column and listed again on the page, earns nothing:
// that is the same row said twice, and a line offering to show you what you are
// already looking at is chrome.
func (a *app) railFoldedAny() bool {
	for _, e := range a.railEntries() {
		if e.folded {
			return true
		}
	}
	return false
}

// railMoreAt reports whether a pointer is on the footer's door onto the task
// page. It is the hover's guard, and the press resolves the same fact through
// [railLine.more] — one geometry asked twice, because the line is found by the
// layout either way ([app.railDoorAt] makes the same bargain one line down).
func (a *app) railMoreAt(x, y int) bool {
	if !a.railAt(x, y) || a.railSeamAt(x, y) {
		return false
	}
	line, ok := a.railLineAt(y)
	return ok && line.more
}

// railOffersResize reports whether the footer should name the handle. A cut
// title earns the offer on its own; focus and the pointer make it visible while
// a person is already acting on the roster. The frame still has the final say.
func (a *app) railOffersResize() bool {
	if !a.railCanWiden() {
		return false
	}
	// The task panel reserves its footer before laying out the tree. Hover may
	// recolor that footer, but must never add a row and move the controls.
	return a.roomOrganized() || a.railCramped || a.railHold || a.hoveringRailArea()
}

// railSigma opens the footer's first line, and it is the whole of what makes the
// figures behind it readable: this is the sum of everything, including what the
// column folded away.
const railSigma = "Σ "

// railPack folds the footer's segments into at most rooms lines of at most width
// cells, joined by this surface's own separator.
//
// A segment that will not fit is DROPPED and the fold is said out loud with the
// ellipsis this surface truncates everything with: a footer that silently stops
// counting is a footer that claims the session is smaller than it is.
func railPack(segs []string, width, rooms int, lead string) []string {
	if rooms < 1 || width < 1 {
		return nil
	}
	out := make([]string, 0, rooms)
	line := lead
	for _, seg := range segs {
		add := seg
		if line != lead {
			add = railSep + seg
		}
		if ansi.StringWidth(line)+ansi.StringWidth(add) <= width {
			line += add
			continue
		}
		// THE FIRST SEGMENT KEEPS THE Σ whatever the width: a column too narrow
		// for "Σ $1.42" is a column that has to choose, and the sign is what says
		// the figure is a total rather than a row's.
		if line == lead {
			line = fit(lead+seg, width)
			continue
		}
		out = append(out, line)
		if len(out) == rooms {
			out[rooms-1] = fit(out[rooms-1]+" "+glyphMore, width)
			return out
		}
		line = fit(seg, width)
	}
	// The loop returns the moment the last line is spoken for, so what reaches
	// here is a line with room left in the block.
	if line != lead {
		out = append(out, line)
	}
	return out
}

// railEntryRows is one row of the forest: WHAT IT IS on the first line, and what
// is true of it on the second.
//
//	⠙ Fix nil-map             #7     a node that belongs to no family
//	  bash go test ./… · 42s
//	⠙ Ship the port            #1     and a family, drawn whole
//	├─ ✓ Read the law          #2
//	├─ ⠙ Write the tree        #3
//	│    42s · 9.9k · $0.31
//	│  └─ ◌ Cut the goldens    #4
//	└─ ◌ Wire the seam         #5
//	⠙ Port the parser        ▸ +7     the same family, folded
//
// EVERY ROW OPENS WITH ONE GLYPH AND IT IS THE STATE. A flat row used to lead
// with two — the state and the node's own ◆ — and the second bought nothing
// here: it is the same mark on every task, the tree rows never carried it, and
// this column holds nothing but tasks, so it marked a distinction the column
// does not contain while spending two of the twenty-two cells the name has
// ([app.railLead] states the whole of it). Down a tree the neighbours are
// already named by the connectors they hang from, and the question left over is
// which limb is still moving: so the column is a column of STATES and it can be
// read downward, flat rows and family rows alike.
//
// THE NAME LEADS AND THE HANDLE TRAILS. The glyph and the title are what a
// person reads down this column — the state, and the words they themselves
// approved — and the id is what identifies the node to the
// MACHINE: the number the engine says in its own sentences ("task 7 done",
// session's task_run.go), the thing to type when you go looking for the branch,
// and the least interesting fact on the row. So it is dim, it is at the far end,
// and the title is measured against what is left.
//
// A FOLDED ROOT SPENDS THAT SAME SLOT ON ITS COUNT. The handle is how you find
// one node and the count is how many nodes this row is standing for — on the one
// row that is hiding work, the second question is the one being asked.
//
// THE SUBTITLE IS NOT HERE. Every other place a task is drawn carries the one
// line that says what it is; this column is twenty-two cells wide and is a
// PRESENCE list — the question it answers is "what is alive", and a sentence
// clipped to twenty-two cells answers no question at all.
//
// It reports the FOLD cell's columns and the badge's alongside the rows, because
// both are pressable and both are narrower than the row they are on — and
// because everything else on the row is the node's own door, so a target
// recorded where nothing is drawn is a click the task swallows.
func (a *app) railEntryRows(e railEntry, width int) ([]string, hudSpan, hudSpan) {
	node := e.node
	if node == nil {
		return nil, hudSpan{}, hudSpan{}
	}
	// Deep ancestry keeps its full navigation identity, but its indentation must
	// leave room for a name and the under-row's child stem. The ellipsis marks
	// omitted outer connectors; only this drawing copy is shortened.
	depthRoom := max((width-railTitleFloor-2-treeIndentCols)/treeIndentCols, 1)
	compressed := len(e.stems) > depthRoom
	if compressed {
		e.stems = e.stems[len(e.stems)-depthRoom:]
	}
	prefix, at := a.railPrefix(e.stems)
	if compressed {
		tail, _ := a.railPrefix(e.stems[1:])
		prefix = a.pal.dim(a.linearMark("…", "~")+strings.Repeat(" ", treeIndentCols-1)) + tail
	}
	glyph, lead, folds := a.railLead(e)
	room := width - at - ansi.StringWidth(lead)
	// The trailing slot: a folded root says how much it is standing for, every
	// other row says its handle, and both stand down when the title cannot afford
	// them.
	meta := railMetaWord(node)
	if e.folded {
		meta = a.linearMark(glyphShut, glyphShutASCII) + " +" + itoa(e.hidden)
	}
	if room-ansi.StringWidth(meta)-1 < railTitleFloor {
		meta = ""
	}
	if meta != "" {
		room -= ansi.StringWidth(meta) + 1
	}
	title, whole := fit(node.title, room), ansi.StringWidth(node.title)
	// THE HINT IS EARNED TWICE OVER: by the INDENT, because a name cut on a row
	// with nothing above it is a name this column is simply too narrow for, and by
	// the WIDE TIER ACTUALLY SAVING IT, because an offer that does not fix what a
	// person can see is worse than no offer.
	if at > 0 && whole > room && whole <= room+railWideGain {
		a.railCramped = true
	}
	line := prefix + lead + a.railTitle(node, title)
	badge := hudSpan{}
	if meta != "" {
		if pad := room - ansi.StringWidth(title) + 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		if e.folded {
			from := width - ansi.StringWidth(meta)
			badge = hudSpan{from: from, to: width}
		}
		line += a.pal.dim(meta)
	}
	rows := []string{line}
	if a.railSaysMore(e) {
		for _, under := range a.railUnder(node, width-a.railUnderCols(e)) {
			rows = append(rows, a.railUnderStem(e)+under)
		}
	}
	// THE CELL IS A TARGET ONLY WHERE IT IS DRAWN AS ONE. At rest it holds the
	// STATE — a spinner, a tick, a demand — and a state is not a control; the
	// disclosure appears in its place under the pointer and only there
	// ([app.railLead]). An empty span holds no column ([hudSpan.holds] asks
	// [hudSpan.pressable] first), so on every other frame these cells belong to
	// the row, which is the node's door.
	cell := hudSpan{}
	if folds {
		cell = hudSpan{from: at, to: at + ansi.StringWidth(glyph)}
	}
	return rows, cell, badge
}

// railSaysMore reports whether this row is allowed the block under its title.
//
// A SETTLED NODE IS ONE LINE, WHEREVER IT STANDS. A family is drawn whole, which
// means the rows that landed are on screen to make the rows that have not landed
// legible — and a merge word and a price under each of them is a second column of
// history inside a shape somebody is reading for its shape. So only the rows that
// are still going somewhere say anything more: what is running, and what is
// waiting on a person. A folded root says nothing extra either — it is standing
// for a whole subtree, and one row is the point of it.
//
// AND THE FLAT ROW FOLLOWS THE SAME LAW, which is the thing that changed. A row
// with no family around it used to keep its block forever, so thirteen jobs that
// had all landed hours ago spent thirty-nine lines of a thirty-cell column
// restating history, and the standing section under them was squeezed to a single
// cut-off row. The column exists to say what is alive; space it spends on what is
// over is space taken from what is not.
//
// WHAT IS QUEUED IS NOT SETTLED, and a flat one keeps its line: `waits: Collect
// sources` is the only place this surface says what is in the way, and inside a
// family the sibling above it is that answer already.
//
// AND NOTHING IS THROWN AWAY — it is TUCKED. A landed flat row that had something
// to say folds it behind the same disclosure a family root wears, opened with →
// or a press on the glyph cell and remembered in the same map ([app.railOpen]).
// So the branch a stopped run kept is one gesture away rather than gone; see
// [app.railTucks].
func (a *app) railSaysMore(e railEntry) bool {
	if e.folded {
		return false
	}
	group := a.railGroupOf(e.node)
	switch group {
	case railRunning, railAttention:
		return true
	}
	if e.node.Paused() {
		return true
	}
	if e.root || len(e.stems) > 0 {
		return false
	}
	if group == railDone {
		return !a.railTuckShut(e.node)
	}
	return true
}

// railTucks reports whether this row folds ITS OWN BLOCK — the landed flat row,
// open or shut, and nothing else on the column.
//
// IT ASKS WHETHER THERE IS ANYTHING BEHIND THE DISCLOSURE, at the width the block
// would be drawn at, because a triangle on a row with nothing under it is an
// affordance that answers a press with silence. A node that came home clean with
// no price to report has nothing tucked, and its glyph cell stays its state.
//
// It is asked of ONE row at a time — the row under the pointer, and the row a key
// arrived on — and never down the whole column, which is why the layout reads
// [app.railSaysMore] instead: that question is answered from the fold map alone,
// and this one renders a block to answer.
func (a *app) railTucks(e railEntry) bool {
	if e.node == nil || e.root || e.folded || len(e.stems) > 0 {
		return false
	}
	if a.railGroupOf(e.node) != railDone {
		return false
	}
	return len(a.railUnder(e.node, a.railRoom()-a.railUnderCols(e))) > 0
}

// railTuckShut reports whether a landed flat row is holding its block back.
//
// THE DEFAULT IS SHUT AND THE MAP IS THE PERSON'S CORRECTION OF IT, which is
// [app.railShut]'s own bargain said about one row instead of a family — and it is
// the same map, so a row and a family are folded by the same gesture and undone
// by it too. It is spelled separately because [app.railShut] answers by regrowing
// the node's family, which the layout may not pay for once a row.
func (a *app) railTuckShut(node *taskNode) bool {
	return node == nil || !a.railOpen[node.id]
}

// railNodeRows is one node with no family around it — the flat row, and the
// shape every caller outside the column's own layout wants.
func (a *app) railNodeRows(node *taskNode, width int) []string {
	rows, _, _ := a.railEntryRows(railEntry{node: node}, width)
	return rows
}

// railLead is the row's glyph cell, the whole lead it sits in, air included, and
// whether that cell IS A FOLD CONTROL on this frame.
//
// THE DISCLOSURE IS THE POINTER'S AND IT REPLACES THE STATE. A family root under
// the pointer trades its state cell for ▾ or ▸ — one cell, in place, so nothing
// on the row moves — because the affordance is worth exactly as much as the
// state for the one moment there is a hand on it. At rest the column is states
// all the way down.
//
// AND A LANDED ROW WITH ITS BLOCK TUCKED AWAY OFFERS THE SAME CELL, because it is
// the same gesture on the same map ([app.railTucks]). One fold vocabulary down
// the column: what is hiding something says so under the hand, and ▸ opens it
// whether what it is hiding is a subtree or two lines of its own history.
//
// THE THIRD ANSWER IS WHAT THE PRESS READS, and returning it is the whole of the
// fix: [app.railPress] used to fold whenever a row COULD disclose — a family
// root, a landed row with a block tucked under it — while the cell only DRAWS
// the triangle under the pointer. So a press on a root this surface was not
// holding a hover for folded the family with a STATE glyph on screen, and the
// task the person was aiming at never opened. Opening a room drops the hover
// ([app.dropHover]) and a pointer that has not moved since sends no motion to
// put it back, so the very next click after opening anything landed in exactly
// that gap. The set that LIGHTS is the set that acts, which is hover.go's own
// law; this is the one answer both halves now read.
//
// AND THE ROW NO LONGER CARRIES THE ◆. It is one marker drawn as furniture,
// saying "this row is a task" and nothing else (taskident.go) — a distinction
// this column does not contain, because every node row on it is a task and the
// tree rows never wore it at all. It cost two cells of NAME on the narrowest
// surface here, on flat rows only, so two rows of the same kind led differently
// and the under-block — indented two cells by [app.railUnderCols] — sat two
// cells to the left of the title it belongs to. Dropping it buys the name those
// cells and squares the block up under it. Every other place a task is drawn
// keeps the marker, because those places hold more than tasks.
func (a *app) railLead(e railEntry) (string, string, bool) {
	glyph := a.railTreeGlyph(e.node)
	if e.folded && e.worst != nil {
		// A FOLDED ROOT WEARS THE WORST THING UNDER IT. The row is standing for a
		// whole subtree, so the one cell it has says what that subtree's news is
		// rather than what its root happens to be doing.
		glyph = a.railTreeGlyph(e.worst)
	}
	fold := a.hoveringRail(e.node) && (e.root || a.railTucks(e))
	if fold {
		mark := a.linearMark(glyphOpen, glyphOpenASCII)
		if e.folded || (!e.root && a.railTuckShut(e.node)) {
			mark = a.linearMark(glyphShut, glyphShutASCII)
		}
		glyph = a.pal.accent(mark)
	}
	return glyph, glyph + " ", fold
}

// railPrefix is the connectors for one row, painted, and the CELLS they cost. A
// root has none — it is the thing everything else hangs from.
func (a *app) railPrefix(stems []bool) (string, int) {
	if len(stems) == 0 {
		return "", 0
	}
	var out strings.Builder
	for _, more := range stems[:len(stems)-1] {
		if more {
			out.WriteString(a.linearMark(treeStem, treeStemASCII))
			continue
		}
		out.WriteString(treeVoid)
	}
	if stems[len(stems)-1] {
		out.WriteString(a.linearMark(treeBranch, treeBranchASCII))
	} else {
		out.WriteString(a.linearMark(treeLast, treeLastASCII))
	}
	return a.pal.dim(out.String()), len(stems) * treeIndentCols
}

// railUnderStem is what an under-row is drawn behind, and railUnderCols is what
// that costs.
//
// THE STEM CONTINUES THROUGH THE UNDER-BLOCK. A node's telemetry sits between
// that node's row and its next sibling's, so a block indented with plain spaces
// would put a gap in the vertical line the eye is following down the family. The
// row's own elbow becomes a stem — or blank air, where the node was the last of
// its siblings — and the node's own stem is added when it has children drawn
// below it. The two cells on the end are the flat row's WHOLE lead — the state
// glyph and its air ([app.railLead]) — so an under-row now starts in the same
// column as the title it belongs to. It did not while the flat row also carried
// a ◆: the block sat two cells to its left, and squaring that up is half of why
// the marker went.
func (a *app) railUnderStem(e railEntry) string {
	var out strings.Builder
	for _, more := range e.stems {
		if more {
			out.WriteString(a.linearMark(treeStem, treeStemASCII))
			continue
		}
		out.WriteString(treeVoid)
	}
	if e.root && !e.folded {
		out.WriteString(a.linearMark(treeStem, treeStemASCII))
	}
	if out.Len() == 0 {
		return "  "
	}
	return a.pal.dim(out.String()) + "  "
}

func (a *app) railUnderCols(e railEntry) int {
	cols := len(e.stems) * treeIndentCols
	if e.root && !e.folded {
		cols += treeIndentCols
	}
	return cols + 2
}

// railWorst is the node whose state a folded family wears: the most urgent thing
// under it, its root included.
//
// IT IS NOT [app.railTreeUrgency] AND THE DIFFERENCE IS THE FAILURE. Where a
// family STANDS in the column is a question about what a person still has to do,
// and a failure that kept no branch is nothing they have to do (see
// [app.railGroupOf]) — but the glyph on a folded row is the news of the subtree,
// and "something in here did not come off" is the loudest news there is short of
// a demand. So the two orders differ by exactly one rank.
func (a *app) railWorst(t *railTwig) *taskNode {
	worst := t.node
	for _, kid := range t.kids {
		if under := a.railWorst(kid); a.railGlyphRank(under) < a.railGlyphRank(worst) {
			worst = under
		}
	}
	return worst
}

// railGlyphRank orders the readings by how loud they are on one cell, and IT IS
// THE TIER'S ORDER: the person's call first, then work in flight, then work that
// is over (docs/design/task-states/DESIGN.md).
//
// THE ONE RANK THE TIERS DO NOT DECIDE IS `over` AND UNFINISHED. The glyph on a
// folded row is the news of the subtree, and "something in here did not come
// off" is the loudest news there is short of a demand — so an incomplete child
// outranks a sibling that has not started, which is why this order and
// [app.railTreeUrgency]'s differ by exactly one rank. Where a family STANDS in
// the column is a question about what a person still has to do; what it WEARS is
// a question about what happened in it.
//
// A CHILD WHOSE DECISION IS ITS PARENT'S AGENT'S DOES NOT MAKE THE FOLDED ROW A
// DEMAND, which is [app.railGroupOf]'s law said on one cell: the parent holds
// the question, so a family drawn as its root alone must wear the root's own
// news and not a question its own head is already answering. That fold is read
// off [session.TaskAsk.Owner] and off nothing else — a design waiting to be
// approved is asking the PERSON, and no agent above it can answer for them —
// and it changes only where the row sorts. The row still reads its reason.
func (a *app) railGlyphRank(node *taskNode) int {
	status := a.taskStatus(node)
	switch {
	case status.Tier == session.TaskTierYourCall:
		// AND THE TIER IS ASKED FIRST. A node whose landing is somebody's call has
		// a branch that never came home by construction, so an unlanded-changes
		// test above this one would answer for every one of them and the fold
		// below could never fire.
		//
		// THE PARENT'S OWN RUN IS THE SAME FACT THE ENGINE HAS NOT PUBLISHED YET.
		// The engine routes a sub-task's landing note to its parent node's agent
		// while that parent lives (session's deliverTaskNote), which IS the model
		// holding the question — but it does not stamp [session.TaskNotice.Decider]
		// on that shape, so reading the owner alone would take the #268 fold away
		// and put a demand back on a family whose head is already answering it.
		// Both roads are the same claim; when the engine publishes the second the
		// clause goes.
		if status.Ask.Owner == session.TaskAskOwnerModel || a.taskParentDeciding(node) {
			return 4
		}
		return 0
	case node.Paused(), status.ChangesUnlanded():
		return 0
	case status.Tier == session.TaskTierMoving:
		if status.Presence == session.TaskPresenceWorking || status.Presence == session.TaskPresenceFinishing {
			return 1
		}
		return 3
	case status.Presence == session.TaskPresenceIncomplete:
		return 2
	}
	return 4
}

// railTreeGlyph is the row's state in one cell: the roster's own glyph, with the
// fuel gate's ⏸ in front of it, because a node held at a gate is running as far
// as the run is concerned and standing still as far as a person is (taskstrip.go
// keeps the same pair on its chips).
func (a *app) railTreeGlyph(node *taskNode) string {
	if node.Paused() {
		return a.stripPausedGlyph()
	}
	return a.railGlyph(node)
}

// railTitleFloor is how little room a title may be left with before the id gives
// up its cells. Twelve is about two words — under that the row has stopped
// naming the work.
const railTitleFloor = 12

// railMetaWord is the node's handle: the id the engine calls it by.
func railMetaWord(node *taskNode) string { return "#" + itoa(int(node.id)) }

// railModelWord is the model this node runs on, as a column this narrow can say
// it: the part of the id AFTER THE VENDOR, which is the part that names the
// model rather than who sells it. Empty when nobody published one, and then
// every row that would have drawn it draws nothing instead.
func railModelWord(node *taskNode) string {
	model := strings.TrimSpace(node.model)
	if slash := strings.LastIndex(model, "/"); slash >= 0 && slash+1 < len(model) {
		model = model[slash+1:]
	}
	return model
}

// railTitle paints an already-fitted title. The cut happens at the call site
// because that is where the id's cells are measured out of it ([app.railNodeRows]):
// a title fitted here and trimmed there would be a row measured twice.
//
// THE ROOM A PERSON IS STANDING IN LEADS THE COLUMN, in the accent and bold —
// which is the strip's own law for the same fact said one row up
// (taskstrip.go's [app.stripTitle]), because the strip and the roster are the
// two lists of the same work and a person who learned the mark on one has
// learned it on the other. It is the TEXT half of that mark; the row's band is
// the other ([app.railRows]), and this half is the one a sixteen-colour
// terminal still gets.
func (a *app) railTitle(node *taskNode, title string) string {
	switch {
	case a.roomStandingOn(node):
		return a.pal.bold(a.pal.accent(title))
	case node.state == session.TaskRunning:
		return a.pal.ink(title)
	}
	return a.pal.muted(title)
}

// railUnder is what a node says under its own title: what it is doing and what
// it is spending while it runs, what it waits on — or what is holding it —
// while it is blocked, and how the branch came home once it has landed.
//
//	bash go test ./…             a live call, in its own hue
//	finishing · adding amp-labs  the gap being closed, while there is one
//	waiting · rate limited       the hold, while something is holding it
//	42s · 9.9k · $0.31 · gpt-5   the telemetry, always, while it runs
//	merged · $0.42               what it came home as, and what it cost
//	conflicted · task/fix-nil    the one loud row, and its one handle back
//	waits: Collect sources       what has to happen before this can
//	waiting · machine busy       and what is holding it when nothing does
//
// THE ROWS THAT CARRY A HANDLE CARRY NOTHING ELSE. A conflicted branch, a kept
// branch, a prerequisite's name and a question's own reason are each one
// fact a person has to ACT on, and a price appended to any of them would be a
// figure competing with the only thing on the row worth reading. The telemetry
// belongs to the states nobody has to do anything about — a node that is running
// and a node that came home clean.
//
// It WRAPS rather than truncates, up to [railUnderRows]. Everything else on this
// surface cuts to an ellipsis, and everything else on this surface is cutting a
// sentence a person can reconstruct; the two facts down here — the name of a
// branch that did not merge, the name of the node being waited on — are the only
// handles back to work that is not on screen, and half of one of those is worth
// nothing at all.
func (a *app) railUnder(node *taskNode, width int) []string {
	paint, text := a.pal.dim, ""
	switch node.state {
	case session.TaskRunning:
		// A RUNNING NODE SAYS WHAT IT IS DOING, when the pilot lane has told this
		// surface (see [taskPilot]) — and that line carries its own clock in its
		// own hue, so it is built here rather than falling through to the
		// single-paint wrap below.
		//
		// AND THEN IT SAYS WHAT IT IS COSTING, always. The call is what the node is
		// doing this second and it is gone the second after; the telemetry is the
		// standing answer to "is this worth what it is burning", which is the
		// question a person opens this column for and cannot ask anywhere else
		// without leaving the conversation. Between calls the telemetry is the
		// whole of the under-block, which is what the bare clock used to be.
		//
		// AND WHILE THE NODE IS FINISHING OFF, THAT LINE IS THE ONE THAT WINS.
		// A node closing a named gap in work it has otherwise done is the most
		// specific news this column will ever have about it — "adding amp-labs to
		// the report" says both that the end is in sight and what the end is
		// missing — and the call it happens to be inside of while it does that
		// says neither. So it takes the call's row rather than a third one: the
		// block is capped at [railUnderRows] and the telemetry underneath is the
		// standing figure a person is owed at every moment of a run.
		//
		// AND A PACED NODE NEVER CLAIMS A LIVE CALL. While the provider is holding
		// this node's calls back (session's TaskNotice.Waiting) the tool line is
		// the last call, sitting there finished — so the row would be this column
		// asserting a present that is not happening, which is the one thing a row
		// down here may not do. The hold takes that row instead and says what is
		// actually true: the node is running, and it is waiting for its turn on
		// the wire. The telemetry underneath is unchanged, because the clock and
		// the bill go on being the clock and the bill.
		//
		// THE GAP OUTRANKS THE HOLD when a node somehow has both. "adding amp-labs
		// to the report" is news about the WORK and "rate limited" is news about
		// the wire, and of the two the first is the one a person came to this
		// column for; the hold only ever displaces the row that would otherwise be
		// false.
		//
		// AND THE LIFE THE NODE IS IN OUTRANKS THE GAP. A node under a check is
		// not inside a call and has no gap yet, so the rows below would draw a
		// finished call or a clock for four minutes of reading — which is the
		// silence #76 §5 measured. A node inside a repair round has a gap, and
		// [app.railPhase] draws that same gap with what [app.railMending] could
		// not know: that a round is closing it, and which round of how many.
		//
		// AND A NAMED PHASE OUTRANKS ALL FOUR. A node of a kind that names its
		// own moments — a harness being designed, which is "designing" and then
		// "awaiting your look" (session's TaskNotice.Doing) — is saying the most
		// specific true thing there is about it, and the rows below would each
		// say something less: a call it is inside of, a hold that is not holding
		// it, or a clock. It takes the row for [app.railDoing]'s reason.
		rows := a.railDoing(node, width)
		if len(rows) == 0 {
			rows = a.railPhase(node, width)
		}
		if len(rows) == 0 {
			rows = a.railMending(node, width)
		}
		if len(rows) == 0 {
			rows = a.railWaiting(node, width)
		}
		if len(rows) == 0 {
			rows = a.railWorking(node, width)
		}
		if len(rows) < railUnderRows {
			if tele := a.railTelemetry(node, width); tele != "" {
				rows = append(rows, paint(tele))
			}
		}
		return rows
	case session.TaskQueued:
		// THE DEPENDENCY SENTENCE, and it is v1's own words — internal/tui says
		// "waits: <title>" and a person who has used that surface has already
		// learned what it means. In a one-node graph nothing is ever unmet and
		// this draws nothing at all; the model carries the edges regardless, so
		// the day the executor grows them the rail already knows.
		//
		// AND IT OUTRANKS THE HOLD WORD, on the one row a queued node gets. Both
		// are true of a node that is behind another node AND behind a full cap,
		// and only one of them is ACTIONABLE: a dependency names other work a
		// person can go and look at, reorder, or stop, while a hold names a queue
		// that is going to clear by itself. The more actionable fact takes the
		// row; the hold is what the row says when there is nothing better on it.
		waits := a.railWaits(node)
		if waits == "" {
			return a.railWaiting(node, width)
		}
		text = "waits: " + waits
	case session.TaskUnverified:
		// NOT THE MERGE SENTENCE. A node whose landing is somebody's call wears
		// session's "aborted" merge like a stopped one does, and the row below
		// would therefore say `stopped` about work that ran to the end. What this
		// row says is the QUESTION and its reason, in the engine's own spelling
		// ([session.TaskStatus.RowWord]) — `your call · nobody could check it`,
		// `your call · conflicts with your branch: parser.go` — because the reason
		// is the half a person can act on and a bare `your call` sends them to the
		// card to find out what for.
		status := a.taskStatus(node)
		paint, text = tierInk(a.pal, status), status.RowWord()
	default:
		switch node.merge {
		case mergeWordKept:
			// FINISHED AND WAITING ON ITS BRANCH. The state stays done because the
			// work is complete; the row names the one action left to the person.
			text = taskBranchKept + " · " + node.branch
		case mergeWordConflicted:
			// THE ONE LOUD ROW ON THE RAIL. A branch that did not merge is work
			// that is finished and not delivered, and its branch is the only
			// thing that gets a person back to it.
			paint, text = a.pal.bad, mergeWordConflicted+" · "+node.branch
		case mergeWordAborted:
			// STOPPED, AND ITS BRANCH KEPT — in those words, and not in the
			// engine's. "aborted" is internal/session's vocabulary for a branch
			// that never merged, and on a screen it reads as a crash: the commonest
			// way a node wears this word is that a person stopped it, or that it
			// ran out of the steps it was given, and neither of those is a failure
			// of anything. The row says what is true and what to do about it —
			// nothing went wrong, and the work is still on that branch — and where
			// the engine said WHY it stopped, the row leads with that instead
			// (taskending.go), because "stopped" was measured true of none of six.
			// THE STATE, THEN WHERE THE WORK WAS LEFT. The state is the reading's
			// own sentence and never this file's — `stopped`, or `incomplete · ran
			// out of steps` — and the branch is a FACT hung off it rather than part
			// of it. The one sentence that used to cover all of this, `stopped —
			// branch kept`, was measured true of one landing in six.
			//
			// AND THE HANDLE OUTLIVES THE REASON. This block is two rows
			// ([railUnderRows]) and a long ending sentence plus a branch name is
			// three, which would drop the branch off the bottom — and the branch is
			// the only way back to work that is not on screen, while the reason is
			// on the card in full one keypress away. So when both will not fit the
			// reason gives way, exactly as the file list does on a card
			// (tasktier.go's [tierRow] states the same law).
			status := a.taskStatus(node)
			paint = tierInk(a.pal, status)
			text = status.RowWord() + railSep + node.branch
			if len(railWrap(text, width)) > railUnderRows {
				text = status.Word + railSep + node.branch
			}
		default:
			// THE MERGE WORD, AND WHAT THE WORK COST TO GET THERE. A node that came
			// home clean is the one settled row with nothing to act on, so it is the
			// one that can afford a figure — and the price is the fact a person goes
			// looking for afterwards, because the footer's Σ is the session's whole
			// spend and says nothing about which node ate it.
			//
			// THE MERGE WORD ALWAYS SURVIVES. The price is appended only when the
			// engine published one and only when the row has the cells for both: a
			// column too narrow for "merged · $0.42" says "merged", never "$0.42".
			text = mergeScreenWord(node.merge)
			// A FAILED NODE WITH NO BRANCH TO KEEP — a non-git workspace ran it in
			// the person's own tree — still leads with why it stopped, when the
			// engine said (taskending.go): where the work is is not what happened
			// to it.
			// A FAILED NODE WITH NO BRANCH TO KEEP — a non-git workspace ran it in
			// the person's own tree — still leads with the reading's own sentence
			// about how it ended, because where the work is is not what happened to
			// it. The sentence is [session.TaskReasonOf]'s and is spelled nowhere on
			// this surface.
			if status := a.taskStatus(node); node.state == session.TaskFailed && status.RowWord() != "" {
				text = status.RowWord()
				if landed := mergeScreenWord(node.merge); landed != "" {
					text += railSep + landed
				}
				paint = tierInk(a.pal, status)
			}
			if spent := node.spent(); text != "" && spent > 0 {
				if priced := text + railSep + dollars(spent); ansi.StringWidth(priced) <= width {
					text = priced
				}
			}
		}
	}
	if text == "" {
		return nil
	}
	lines := railWrap(text, width)
	if len(lines) > railUnderRows {
		lines = lines[:railUnderRows]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, paint(line))
	}
	return out
}

// railUnderRows caps that block. Two is what a branch name or a prerequisite's
// title takes at this width, and it is exactly what a running node spends — the
// call it is in, then the telemetry — so the cap is the design and not a
// backstop. Past it the rail would be a paragraph, and the transcript is where
// paragraphs live.
const railUnderRows = 2

// railSep is what this column joins two facts on one row with, and it is the
// footer's own joiner ([railPack]): one vocabulary down the whole column.
const railSep = " · "

// ── THE ELAPSED CLOCK ───────────────────────────────────────────────────────
//
//	⠙ Fix nil-map
//	  bash go test ./…            under ten seconds: no number at all
//	  bash go test ./… · 24s      dim, because it is only slow
//	  bash go test ./… · 1m 8s    warn, because it is now the reason you are waiting
//
// A NUMBER THAT IS ALWAYS THERE IS A NUMBER NOBODY READS. The rail used to
// carry the node's own age from the first second, which is a figure that is
// true, ticking and almost never actionable: a node is SUPPOSED to take
// minutes. What is actionable is one CALL taking them — the test suite that
// hung, the fetch that is not coming back — so the clock is spent on the
// current call and appears only once that call has been running longer than a
// person would sit still for it.
//
// TWO STEPS, AND THE LOUD ONE IS NOT THE FAILURE HUE. Ten seconds is when the
// wait becomes a fact worth stating; a minute is when it becomes the thing
// about the row, and it takes [hueWarn] — the tier that means "this is about to
// be your problem" rather than "this went wrong", because a four-minute build
// is not a failure and a row that said it was would be the surface guessing.
//
// There is deliberately NO five-second step. The tool line has one (toolview.go
// escalates a bounded call into [hueBad] with five seconds left on its
// timeout), and that step means something precise there: the call is about to
// be killed at a moment the surface knows. Nothing is going to happen to a task
// at five seconds, and a colour that fired on nothing would teach a person to
// ignore the one that fires on something.
const (
	// taskToolFloor is how long a node's current call must run before its row
	// says so.
	taskToolFloor = 10 * time.Second
	// taskToolWarn is when that wait stops being background.
	taskToolWarn = time.Minute
)

// railWorking is the under-line of a node with a call in flight, or nil when
// the pilot lane has not told this surface what it is doing.
//
// ONE ROW, and the call's name gives up its tail to the clock rather than the
// other way round: which tool is running is a fact a person recognizes from its
// first few cells, and how long it has been running is the fact they came to
// the rail for.
func (a *app) railWorking(node *taskNode, width int) []string {
	if node.tool == "" {
		return nil
	}
	clock, tint := a.taskClock(node)
	tail := ""
	if clock != "" {
		tail = railSep + clock
	}
	name := fit(node.tool, width-ansi.StringWidth(tail))
	line := a.pal.dim(name)
	if clock != "" {
		line += a.pal.dim(railSep) + tint(clock)
	}
	return []string{line}
}

// railDoing is the row a node wears while it is in a phase of its own kind's
// naming, or nil for the ordinary node, which has no phases.
//
//	designing                                a harness page being written
//	awaiting your look                       and the card that page ended on
//
// IT IS THE PHASE ALONE, with no word in front of it, and that is what makes it
// different from the two rows under it. "finishing · adding amp-labs" and
// "waiting · machine busy" are each a word this surface chose followed by the
// engine's reason, because the node is running and the surface is saying which
// part of running that is. A phase is not part of running — it IS what this node
// is doing, in the only vocabulary it has — so a prefix would be the surface
// explaining a plain English word with a second plain English word.
func (a *app) railDoing(node *taskNode, width int) []string {
	if node.doing == "" {
		return nil
	}
	return []string{a.pal.dim(fit(node.doing, width))}
}

// railMending is the row a node wears while it is closing a named gap in work it
// has otherwise finished, or nil when there is no gap being closed.
//
//	finishing · adding amp-labs to the report   the whole of it, on a wide column
//	finishing · adding amp-labs to t…           and the same row, fitted
//
// THE WORD IS THE SURFACE'S AND THE SENTENCE IS THE ENGINE'S, which is the same
// split every other row down here is built on ([taskBranchKept] states it about
// a merge word). "finishing" is this column saying which part of running this is;
// what follows the separator is the engine's own plain line about what is left,
// kept verbatim, because the whole value of the row is that it is SPECIFIC.
//
// IT CUTS RATHER THAN WRAPS. The two rows that wrap down here carry a handle
// back to work that is off screen — a branch name, a prerequisite's title — and
// half of one of those is worth nothing; this is a sentence, a person
// reconstructs a sentence from its front, and the row under it is the telemetry
// that has to survive too.
func (a *app) railMending(node *taskNode, width int) []string {
	if node.mending == "" {
		return nil
	}
	line := fit(taskFinishingWord+railSep+node.mending, width)
	if line == "" {
		return nil
	}
	return []string{a.pal.dim(line)}
}

// railWaiting is the row a node wears while it is HELD — behind a full slot,
// behind a machine under load, or behind a provider pacing its calls — or nil
// when nothing is holding it.
//
//	waiting · machine busy     a queued node the machine has no room for
//	waiting · slot             a queued node behind the parallelism cap
//	waiting · rate limited     a running node whose calls are being paced
//
// IT IS DIM, AND THAT IS THE WHOLE POINT OF IT. A hold is not a failure and it
// is not a thing to do anything about: the queue clears, the cap frees, the
// provider lets the next call through, and every one of those happens without a
// person. What the row is FOR is the question this column could not answer at
// all before it — a node that has sat still for four minutes is either stuck or
// merely waiting its turn, and those are opposite news wearing the same row. So
// it says which, in the quietest ink the column has.
//
// IT CUTS RATHER THAN WRAPS, for [app.railMending]'s reason: the two rows that
// wrap down here carry a handle back to work that is off screen, and this is a
// two-word state whose first word is the half that matters.
func (a *app) railWaiting(node *taskNode, width int) []string {
	if node.waiting == "" {
		return nil
	}
	line := fit(a.taskStatus(node).RowWord(), width)
	if line == "" {
		return nil
	}
	return []string{a.pal.dim(line)}
}

// railTelemetry is the standing row under a running node: how long it has been
// going, how much it has burned, what that has cost, and who is doing it.
//
//	42s · 9.9k · $0.31 · gpt-5 · thinking high   a wide roster, everything known
//	42s · 9.9k · $0.31 · gpt-5    a full column: the rung is the first to go
//	42s · 9.9k · $0.31            a slim one: the worker goes next
//	42s · gpt-5                   an engine that publishes no usage
//	42s                           and one that publishes nothing at all
//
// RICHEST FIRST, DROPPED FROM THE RIGHT, AND THAT IS THE WHOLE WIDTH STORY. The
// segments are in the order a person needs them — the age is what says whether
// to look, the weight and the price are what say whether to stop it, the model
// is context for both — so the row is built whole and shortened by giving up its
// tail until it fits. That is one rule for a 26-cell column, a 20-cell one and
// the roster drawn over the whole frame: a mode switch here would be three
// layouts to keep true instead of one.
//
// ABSENCE RENDERS AS NOTHING. A figure nobody published is zero, and zero is not
// a measurement: the segment is skipped entirely rather than drawn as "$0.00" or
// "0" beside a node that has been working for a minute. A row with nothing known
// on it at all is no row.
func (a *app) railTelemetry(node *taskNode, width int) string {
	segs := make([]string, 0, 5)
	if clock := countUpWord(a.taskNow(node).Sub(node.began)); clock != "" {
		segs = append(segs, clock)
	}
	if node.tokens > 0 {
		// BARE, WITH NO UNIT ON IT. The footer's figure wears "tok" because it sits
		// beside a dollar sum and a row of counts; here the count is one of four
		// segments on a row twenty-odd cells wide, and three of those cells are the
		// difference between keeping the price and dropping it.
		segs = append(segs, tokenWord(node.tokens))
	}
	if spent := node.spent(); spent > 0 {
		segs = append(segs, dollars(spent))
	}
	if model := railModelWord(node); model != "" {
		segs = append(segs, model)
	}
	// AND THE RUNG IS LAST, which is to say it is the first thing given up. It
	// is a setting rather than a measurement — the four in front of it all move
	// while a person watches and it does not — so on the twenty-six cells this
	// column usually gets it is absent, and on a roster raised over the whole
	// frame it is there. That is the drop-from-the-right rule applied to one
	// more fact rather than a second layout, and the room's own header states
	// the same rung with no width to fight over (taskeffort.go).
	if rung := a.taskEffortClause(node); rung != "" {
		segs = append(segs, rung)
	}
	for ; len(segs) > 0; segs = segs[:len(segs)-1] {
		if line := strings.Join(segs, railSep); ansi.StringWidth(line) <= width {
			return line
		}
	}
	return ""
}

// taskClock is how long this node's current call has been running, and the hue
// that says how that is going. Both are empty under [taskToolFloor]: a call
// that has just started is a call nobody is waiting on yet.
func (a *app) taskClock(node *taskNode) (string, func(string) string) {
	if node.toolBegan.IsZero() {
		return "", nil
	}
	age := a.taskNow(node).Sub(node.toolBegan)
	if age < taskToolFloor {
		return "", nil
	}
	if age >= taskToolWarn {
		return countUpWord(age), a.pal.warn
	}
	return countUpWord(age), a.pal.dim
}

// taskNow is the clock ONE NODE's row is drawn against, and it is not always
// the surface's.
//
// A CLOCK FREEZES WHILE YOU ARE LOOKING INTO ITS TASK. The elapsed number
// exists to answer "should I go and look at this", and once a person is
// standing in the node's room they are already looking: the room shows the
// calls themselves, one line each, live. A number still climbing in the corner
// of the screen at that point is pressure applied to somebody who has already
// answered it — and it is pressure that keeps climbing while they read, which
// is the opposite of what a person reading needs. It thaws when they leave, at
// the value it would have had all along, because nothing here stops the clock
// so much as stops reporting it.
func (a *app) taskNow(node *taskNode) time.Time {
	if !node.froze.IsZero() {
		return node.froze
	}
	return a.now()
}

// freezeNode and thawNode are the two sides of that, called by the room's own
// door (room.go). They are here rather than there because the field is this
// file's and a room is a VIEW: it says where the person is, and what that means
// for a clock is the clock's own business.
func (a *app) freezeNode(id uint64) {
	if node := a.tasks[id]; node != nil && node.froze.IsZero() {
		node.froze = a.now()
	}
}

func (a *app) thawNode(id uint64) {
	if node := a.tasks[id]; node != nil {
		node.froze = time.Time{}
	}
}

// railWrap breaks a rail sentence ON ITS SPACES, and breaks a word only when
// one word is wider than the whole column.
//
// Neither of the two wrappers this tree already has does that. The transcript's
// [wrap] breaks anywhere, which is right for a paragraph; ansi.Wordwrap always
// treats a hyphen as a breakpoint, which turns "task/fix-nil-map" into
// "task/fix-nil-" and "map". A branch cut in the middle of its name is a branch
// nobody can retype, and retyping it is the entire reason it is on screen.
func railWrap(text string, width int) []string {
	if width < 1 {
		return nil
	}
	var out []string
	for _, word := range strings.Fields(text) {
		if len(out) > 0 {
			if joined := out[len(out)-1] + " " + word; ansi.StringWidth(joined) <= width {
				out[len(out)-1] = joined
				continue
			}
		}
		if ansi.StringWidth(word) <= width {
			out = append(out, word)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(word, width, ""), "\n")...)
	}
	return out
}

// railWaits is the prerequisite sentence a blocked node wears, and "" for a
// node that is not waiting on other work. It is the reading's own answer
// ([session.TaskWaitWork], taskstatus.go) rather than a second walk of the
// edges, so the row, the group it is filed under and the composer's line about
// it cannot disagree about whether this node is behind anything.
func (a *app) railWaits(node *taskNode) string {
	status := a.taskStatus(node)
	if status.On != session.TaskWaitWork {
		return ""
	}
	return status.Reason
}

// railGlyph is the node's state, in one cell.
//
// ✓ IS THE ONE SUCCESS GLYPH ON THIS SURFACE, and the tool rows' law against it
// (toolview.go: a column of ticks is a column read to learn nothing) does not
// reach here. There, a quiet line IS the success and the row stays on screen; a
// rail row is a presence that disappears when the work comes home, so the tick
// is not decoration on a permanent row — it is the last thing the row says.
// It is the CELL and the HUE asked separately and put back together, because the
// hue is needed on its own: the composer's room segment says the name of the task
// you are typing to in the state's own colour (room.go's [app.roomLead]), and a
// second table of which state is which colour would be a segment that disagreed
// with the glyph beside the same name in the roster.
func (a *app) railGlyph(node *taskNode) string {
	return a.taskStateInk(node)(a.taskStateMark(node))
}

// taskStateMark is a node's state in one cell, UNPAINTED — [tierMark]'s cell
// asked about a node this window is watching. The table itself is tasktier.go's
// and there is only one of it: which `failed` is a fault, which is a person's
// own stop and which is a run the wire ended are decided once in
// [session.ProjectTask] and drawn once there.
func (a *app) taskStateMark(node *taskNode) string {
	return a.tierMark(a.taskStatus(node))
}

// taskStateInk is the hue that state is said in — the paint half of
// [app.railGlyph], asked separately so the two can never fall out of step.
// Anything that says a node's name in the colour of what it is doing asks this:
// the roster's glyph, and the composer's room segment.
func (a *app) taskStateInk(node *taskNode) func(string) string {
	return tierInk(a.pal, a.taskStatus(node))
}

// railJoin lays one conversation row beside the rail's column for that row. It
// is the ONLY place the two columns meet, and it pads through
// [ansi.StringWidth] because a row measured through its escape sequences is a
// row measured wrong.
func (a *app) railJoin(text, rail string) string {
	if rail == "" {
		return text
	}
	body := a.bodyWidth()
	// MEASURED THE WAY IT WILL BE DRAWN. `ansi.StringWidth` reads a
	// variation-selector emoji and a flag as two cells; the renderer under us
	// draws them as one unless the terminal answered mode 2027, so padding
	// computed the first way left the rail two cells short on exactly those rows
	// and straight everywhere else (cellwidth.go).
	switch width := a.ruler.cells(text); {
	case width < body:
		text += strings.Repeat(" ", body-width)
	case width > body:
		// A row WIDER than the column it is drawn in would run under the rail,
		// and the frozen viewport is where one comes from: copy mode snapshots
		// the transcript when the key is pressed, at whatever width it was laid
		// out for (copymode.go). Cutting the row to its column is the lesser of
		// the two wrongs — the alternative is a rail with a sentence through it.
		text = fit(text, body)
	}
	return text + rail
}

// ── the update, folded in ───────────────────────────────────────────────────

// taskUpdate upserts one node and, when it lands, writes the transcript's line
// about it.
//
// THE DE-DUP IS (ID, STATE), and it is not an optimization. An update that
// happens while a turn runs arrives on BOTH lanes — the turn's hub and the
// standing subscription (session's emitTaskUpdate says so out loud) — so every
// in-turn state change is delivered twice, and a surface that took both would
// write two "task done" lines into the conversation. The states a node moves
// through are monotonic (queued → running → done|failed), so a repeat of the
// state last seen for an id is always the second copy of one event.
//
// UNVERIFIED IS THE ONE STATE THAT CAN BE LEFT AGAIN, and the pair holds through
// it: a person resolving one (session's ResolveUnverified) re-settles the node
// into done or failed, which is a state it has not been in, so the surface draws
// the second card — the decision is an event, and the card that says the work
// was accepted is the record of it.
func (a *app) taskUpdate(ev session.Event) tea.Cmd {
	notice := ev.Task
	if notice == nil {
		return nil
	}
	// A BACKGROUND JOB IS NOT A TASK AND IS NOT FILED AS ONE. Jobs arrive on
	// their own notice now (jobstate.go) and draw in their own section
	// (jobsection.go). Taking one here would put it back on the roster, which
	// is the defect this wave exists to close.
	if notice.Kind == session.TaskKindJob {
		return nil
	}
	if last, seen := a.taskSeen[notice.ID]; seen && last == notice.State {
		// THE DE-DUP HAS EXCEPTIONS, and every one of them is news that arrives
		// without a state change. The pair above catches the same update arriving
		// on both lanes — those two carry identical figures and identical text —
		// but a node that has spent more since the last event is news, and the
		// focus header is where it is read (room.go).
		//
		// AND SO IS EVERY LINE THAT REPORTS THE PRESENT. A node closing a gap in
		// work it has otherwise finished stays RUNNING for the whole of it
		// (session's TaskNotice.Mending); a node held behind a slot or paced by
		// its provider stays exactly where it was for the whole of THAT
		// (TaskNotice.Waiting); and a harness moving from "designing" to
		// "awaiting your look" is running through both (TaskNotice.Doing) — so
		// the sentence naming the gap, the word naming the hold, the phase, and
		// the empty strings that take any of them away again would all be thrown
		// out by a guard that only ever looked at the state.
		// The exception is written as one comparison over those live lines rather
		// than as a clause per field, because they are one kind of thing: what is
		// true of this node RIGHT NOW. Anything that is none of that is the
		// duplicate this guard exists for.
		//
		// AND A NAME IS THE LOUDEST NEWS OF ALL. A notice that carries a title the
		// node does not have was thrown away here, and the row kept the only name
		// this surface can make without one — "task 19" ([taskTitleOf],
		// taskident.go) — for the whole of that node's life, on the column, on the
		// strip, in its room's header and on the card that landed. The states are
		// monotonic but the NAMING is not: a row can be published before its title
		// is known and again after, in the same state, and the second one is the
		// only chance this surface gets to learn what the work is called.
		//
		// AND A DECISION CHANGING HANDS IS THE QUIETEST NEWS OF ALL AND THE ONE
		// NOBODY MAY MISS. The floor hands an unanswered question back to the person
		// at the end of the model's turn by publishing a row whose state, span,
		// branch and report are all exactly what they were and whose only news is
		// [session.TaskNotice.Decider] — so a guard that only ever looked at the
		// state would throw away the event that puts the chips back on the card
		// somebody is waiting in front of (taskdone.go's [app.handedBackCard]).
		node := a.tasks[notice.ID]
		if node == nil || (notice.CostUSD <= node.cost &&
			taskLiveLines(notice) == node.liveLines() && !taskRenames(notice, node) &&
			!taskRenamesContext(notice, node) && !taskStops(notice, node) &&
			!taskPauses(notice, node) && notice.Decider == node.decider && notice.NextModel == node.nextModel && notice.Thinking == node.thinking &&
			(notice.Brief == "" || notice.Brief == node.brief) &&
			(notice.Acceptance == "" || notice.Acceptance == node.acceptance)) {
			return nil
		}
	}
	if a.taskSeen == nil {
		a.taskSeen = map[uint64]session.TaskState{}
	}
	a.taskSeen[notice.ID] = notice.State
	// THE ROW-SPACE HAS MOVED, and an open task page has to be told: this window's
	// live graph is one of the two authorities that page re-files against, and
	// over a connection it is the only one that ever moves (app.go's railStamp).
	a.railStamp++

	node := a.tasks[notice.ID]
	if node == nil {
		if a.tasks == nil {
			a.tasks = map[uint64]*taskNode{}
		}
		// A NODE WHOSE FIRST NEWS IS ALREADY SETTLED WAS NEVER WATCHED HERE. Live
		// work is announced from every state change it passes through, so a node
		// this window saw at all was seen queued or running first; one that turns
		// up done, failed or needing a look is a row replayed out of a checkpoint
		// by a conversation being reopened. It is marked here, at the only moment
		// the difference is visible, and [taskNode.restored] says what the mark
		// costs a clock that is not there.
		node = &taskNode{
			id: notice.ID, ident: identFor(notice.ID), met: a.now(),
			started: notice.StartedAt, ended: notice.EndedAt,
			restored: notice.State != session.TaskRunning && notice.State != session.TaskQueued,
		}
		// THE CONTRACT IS COPIED OFF THE PROPOSAL, ONCE. The updates carry a state
		// and a title and nothing about what the work was for; the card that lands
		// minutes from now wants the brief, the acceptance and the sentence the
		// subtitle is cut from, and the only place any of those was ever said is
		// the question this surface already drew (see [app.cardFor]).
		if card := a.cardFor(notice.ID); card != nil {
			node.label = card.title
			node.assignment = firstNonEmpty(card.summary, card.brief)
			node.brief, node.acceptance, node.where = card.brief, card.acceptance, card.where
		}
		a.tasks[notice.ID] = node
		a.taskOrder = append(a.taskOrder, notice.ID)
	}
	// Replayed engine updates carry the original contract without a proposal card.
	if notice.Brief != "" {
		node.brief = notice.Brief
	}
	if notice.Acceptance != "" {
		node.acceptance = notice.Acceptance
	}
	if notice.Summary != "" {
		node.assignment = notice.Summary
	}
	a.takeTypedTaskBrief(node)
	if title := strings.TrimSpace(notice.Title); title != "" {
		node.label = title
	}
	node.title = taskTitleOf(node.label, node.assignment, node.id)
	// AND A ROOM STANDING ON THIS NODE LEARNS THE NAME WITH IT. The header is a
	// title taken once, at the door ([app.openRoom]), which is right for every
	// room but one: a room opened on a node this surface had not been told the
	// name of would keep "task 19" over the page for as long as it stayed open,
	// which is the exact half of the law the de-dup above exists to keep — the
	// id-form is what a nameless node is called, never what a named one is. Only
	// that id-form is replaced, so a room that was given its own name (an
	// adaptive run's, [app.openOrchRoom]) keeps it.
	if a.room != nil && a.room.id == node.id && a.room.title == taskIDWord(node.id) {
		a.room.title = node.title
	}
	node.state = notice.State
	if len(notice.DependsOn) > 0 {
		node.dependsOn = notice.DependsOn
	}
	// WHO SPAWNED IT, which is the fact the roster tree is drawn from
	// (taskstrip.go's parent seam). It is kept and never unset for the reason the
	// branch and the model are: a run's node was spawned by that run for its whole
	// life, and an update quiet about it has not changed it. The key is the
	// parent's own id spelled the way [stripKey] spells a node's.
	if notice.Parent != 0 {
		node.parent = itoa(int(notice.Parent))
	}
	// THE RUN DOOR IS KEPT AND NEVER UNSET. Belonging to an adaptive run and
	// the node's place inside it are facts for the row's whole life; an update
	// quiet about either one has not turned it back into an ordinary task.
	if notice.Run != "" {
		node.run = notice.Run
	}
	if notice.Node != "" {
		node.node = notice.Node
	}
	if notice.Branch != "" {
		node.branch = notice.Branch
	}
	if notice.Merge != "" {
		node.merge = notice.Merge
	}
	if where := strings.TrimSpace(notice.Where); where != "" {
		node.where = where
	}
	// AND WHAT THAT DIRECTORY IS, kept on the same rule as the branch above: the
	// ground ladder settled it before the node's first step, and an update quiet
	// about it has not turned a fork back into a worktree.
	if notice.Rung != "" {
		node.rung = notice.Rung
	}
	if notice.Mode != "" {
		node.mode = notice.Mode
	}
	// WHO ENDED IT IS KEPT AND NEVER UNSET, on the rule the branch and the price
	// are kept by: a person stopping this node is a fact about the work, and an
	// update that says nothing about it is not an update that undid it. It also
	// cannot arrive twice — nothing on the engine's side ever un-stops a node.
	if notice.Stopped {
		node.stopped = true
	}
	// AND WHY, kept on the same rule: an ending is a fact about how the work
	// ended, and no later update un-ends it.
	if notice.Ending != "" {
		node.ending = notice.Ending
	}
	if notice.Report != "" {
		node.report = notice.Report
	}
	// AND WHAT THE WORK PRODUCED, kept on the report's own rule — a notice quiet
	// about it has not unmade the answer — with the two facts that qualify it
	// taken from the SAME notice that carried the body. Cut and held are
	// statements about the text beside them, so a later notice's flags over an
	// older notice's body would be this surface claiming a pointer belongs to an
	// answer it was never published with (session's TaskNotice.Result).
	if notice.Result != "" || notice.ResultHeld {
		node.produced, node.producedWhole = notice.Result, notice.ResultWhole
		node.producedCut, node.producedHeld = notice.ResultCut, notice.ResultHeld
	}
	// THE TWO FACTS A QUESTION IS MADE OF, taken from every update including their
	// absence. Which files clash and who is holding the decision are both reports
	// of what is true RIGHT NOW — a merge round that resolved a clash and a floor
	// that handed a question back both publish a row that stops carrying what the
	// last one did — so keeping either past the notice that dropped it would be
	// this surface asking a question somebody has already answered
	// (docs/design/task-states/DESIGN.md).
	node.conflicts, node.decider = notice.Conflicts, notice.Decider
	// The model is kept whenever an update carries one and never overwritten
	// with an empty: it is a property of the work, settled at admission, and an
	// update that says nothing about it is not an update that changed it.
	if model := strings.TrimSpace(notice.Model); model != "" {
		node.model = model
	}
	node.nextModel = strings.TrimSpace(notice.NextModel)
	node.thinking = notice.Thinking
	if len(notice.Changed) > 0 {
		node.changed = notice.Changed
	}
	// The spend is kept whenever the engine has one to publish, and never
	// overwritten with a zero: a later update carrying no price would otherwise
	// take a figure off the focus header that was true (room.go).
	if notice.CostUSD > 0 {
		node.cost = notice.CostUSD
	}
	// THE LIVE LINES ARE COPIED WHOLE, INCLUDING THEIR ABSENCE, and they are the
	// fields on this node that are deliberately not kept when an update stops
	// carrying them. Everything above is a FACT about the work — a branch, a
	// price, a model — and a fact does not stop being true because the next event
	// was quiet about it. These are reports of what is happening RIGHT NOW, and a
	// surface still saying "finishing · adding the amp-labs section" about a node
	// that finished that ten seconds ago — or "waiting · machine busy" about a
	// node the machine let through a minute ago — is a surface reporting a
	// present that has passed (the same law [taskNode.tool] is held to).
	live := taskLiveLines(notice)
	node.doing, node.mending, node.waiting = live.doing, live.mending, live.waiting
	// AND SO IS THE GATE, on the same law: a run held at its fuel gate is held
	// until somebody answers, and the row that says the answer landed is a row
	// that stops carrying it (session's TaskNotice.Paused). It is taken from
	// every update INCLUDING ITS ABSENCE, so nothing on this surface has to
	// decide when a gate comes down — the engine stamps every row the run
	// publishes, and the last one to arrive is the truth.
	node.paused = notice.Paused
	// AND SO IS THE LIFE IT WAS IN, on the same law one field over. The phase
	// arrives on its own event and is cleared here rather than there, because the
	// event that says a node has stopped checking is the LANDING — a node that
	// settles while a check is in flight sends no phase move on its way out, and a
	// row left saying "checking what it left" under a card that has merged would
	// be this column reporting a present that has passed (taskphase.go).
	if notice.State != session.TaskRunning && notice.State != session.TaskQueued {
		node.phase, node.phaseRound, node.phaseRounds, node.phaseFinding = "", 0, 0, ""
	}
	// The kind is a FACT and is kept the way the branch and the price above are:
	// an update that says nothing about it has not changed it.
	if notice.Kind != "" {
		node.kind = notice.Kind
	}
	// AND SO IS THE WORKING CONTEXT, on the same rule and for the same reason: a
	// node that named one is in it for the rest of its life, and an update quiet
	// about it has not taken the person out of it. A better name replaces the one
	// that stood, which is the whole of what [taskRenamesContext] lets through.
	if word := strings.TrimSpace(notice.Context); word != "" {
		node.context = word
	}
	// THE RECORD'S CLOCK IS KEPT LIKE EVERY OTHER FACT ABOVE. A later resolution
	// may move the landing instant, while an update from an older engine that
	// carries no stamp cannot erase one this surface already received.
	if !notice.StartedAt.IsZero() {
		node.started = notice.StartedAt
	}
	if !notice.EndedAt.IsZero() {
		node.ended = notice.EndedAt
	}
	// The clock is anchored ONCE, from the age the update reported, so the row
	// counts on the frame tick instead of standing still between events.
	if notice.State == session.TaskRunning && node.began.IsZero() {
		node.began = a.now().Add(-notice.Elapsed)
	}
	// A node that started is a proposal that was approved, whatever answered it:
	// the card stops asking here for the case where the engine's clock, and not
	// this surface, was the thing that said yes.
	if a.task != nil && a.task.id == notice.ID && !a.task.settled() {
		a.task.verdict = taskClockWord
		a.dropTaskQuestion(a.task.id, taskStartedItselfReason)
		a.markCardStale(a.task)
	}
	var pilot tea.Cmd
	switch notice.State {
	case session.TaskRunning:
		// The node is alive, so the surface starts WATCHING it (see [taskPilot]).
		pilot = a.flyPilot(notice.ID)
		// AND THIS IS THE MOMENT THE WORK SEAT IS ACTUALLY SPENT, which is where
		// the one line about a seat nobody pinned belongs (crew.go's
		// [app.sayWorkSeat], said once per session). It is here rather than at
		// either door — the /task command's own receipt, and the proposal a
		// person answers — because both roads end at this fold and a line hung
		// on one of them would be silent on the other.
		a.sayWorkSeat()
	case session.TaskDone, session.TaskFailed, session.TaskUnverified:
		// UNVERIFIED IS A LANDING. The run is over, the slot is handed back and
		// the pilot's lane has ended, so a surface that waited for one of the
		// other two would keep a spinner on a node nothing is doing and would
		// never write the one card that says a person has to decide
		// (session's task_contract.go).
		node.elapsed = notice.Elapsed
		a.landPilot(notice.ID)
		// A CHILD LANDS ON THE ROSTER AND NOT IN THE CONVERSATION. The card is how
		// work a person HANDED OVER reports back, one card per decision they made;
		// an adaptive run's nodes are cut by its planner, there are a dozen of them,
		// and a card each would bury the conversation under the internals of one
		// answer. The run itself is a root and still writes its card, which is the
		// decision that was actually made.
		//
		// EXCEPT THAT A DECISION IS NEVER MUTE, WHATEVER ITS DEPTH. A node that
		// landed needing somebody's look is a QUESTION, and the card is the only
		// place on this surface the answers row exists (tasksettle.go). While that
		// card was root-only, a nested part could land needing a look, sit there
		// for the whole run and expire with nobody ever able to see it — measured,
		// on a part two levels down (#268, and session's pending.go carries the
		// law). So the roster-only rule holds for work that came home DECIDED, and
		// a decision is written wherever it is. It may fold under its family; it
		// may not be absent.
		if node.parent == "" || node.state == session.TaskUnverified {
			a.landedCard(node)
		}
	}
	a.touch()
	return pilot
}

// cardFor is the proposal this surface drew about one node, or nil.
//
// It WALKS THE TRANSCRIPT, newest first, rather than keeping a second index of
// cards by id. The walk happens exactly once per node — at the moment the
// engine first admits it — and an index would be a third place that has to
// agree with [app.task] and the entry list about which card is which id, for a
// lookup that costs nothing on any conversation a person can scroll.
func (a *app) cardFor(id uint64) *taskCard {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if e := &a.entries[i]; e.kind == entryTask && e.card != nil && e.card.id == id {
			return e.card
		}
	}
	return nil
}

// THE LANDED LINE IS NOW A CARD (taskdone.go). What stood here was the one dim
// sentence the transcript kept about a node — "task Fix the nil-map crash done
// in 2m 10s · merged" — and every fact in it survives, in a block that is read
// rather than skipped: the outcome, the elapsed, the kept branch and the
// engine's own failure sentence, verbatim, for the reason it was verbatim here.
// [app.landedCard] is the one place that record is written now.

// tasksAnimating reports whether anything on this surface's task side is
// moving: a countdown running down, a spinner turning, a clock counting up.
// It is what keeps the paint clock alive between turns — a node runs for
// minutes with no stream open, and the rail would otherwise freeze at whatever
// the last event drew.
func (a *app) tasksAnimating() bool {
	if a.jobsAnimating() {
		return true
	}
	if a.awaitingTask() && !a.task.deadline.IsZero() {
		return true
	}
	// The strip turns the same spinner on a frame too narrow for a column, and
	// the roster over the body is the column by another shape — either one is a
	// reason to keep the paint clock alive (taskstrip.go, [app.railFull]).
	if !a.railStanding() && !a.stripShowing() {
		return false
	}
	// A ROSTER FULL OF SETTLED WORK IS A STILL PICTURE. The column stands for the
	// whole session now, so "is anything on it moving" is a question about the
	// running nodes and not about the list's length — otherwise a session that
	// finished its work an hour ago would still be repainting a spinner-less
	// column thirty times a second.
	//
	// AND A DESIGN WAITING ON YOU IS A STILL PICTURE TOO, for the same reason
	// exactly: its row wears the ? rather than the spinner now
	// ([taskAwaitsPerson]), so a card that sits unanswered over lunch is no
	// longer an hour of repaints for a row that never changes.
	for _, node := range a.tasks {
		if node != nil && node.state == session.TaskRunning && !a.taskAwaitsPerson(node) {
			return true
		}
	}
	return false
}

// dropTasks forgets the whole task side. It runs where the agent is replaced
// (/new): a rail carried into the next conversation would be claiming nodes
// that died with the session that started them.
func (a *app) dropTasks() {
	// A ROOM GOES WITH ITS NODE. The page on screen is one node's transcript,
	// and a node that died with its session is a page that cannot be steered,
	// cannot be finished and cannot be left by any door but this one (room.go).
	a.closeRoom()
	a.task = nil
	a.tasks = nil
	a.typedTaskBriefs = nil
	a.taskOrder = nil
	a.taskSeen = nil
	a.taskLane = nil
	// AND THIS CONVERSATION'S BACKGROUND JOBS GO WITH ITS NODES, for the reason
	// they are said to go in jobstate.go: a job belongs to the conversation that
	// started it, down to the folder its log is written in.
	a.dropJobs()
	// THE ROSTER GOES WITH ITS NODES, the keyboard included. A column that kept
	// its folds and its cursor into the next conversation would be a map of work
	// that no longer exists, holding keys the draft is waiting for.
	a.railOpen = nil
	a.railTop = 0
	a.railWhere = railSpot{}
	a.railHold = false
	a.railWide, a.railCramped = false, false
	// [app.railAway] STAYS. It is the one fact in this block that is not about
	// these nodes: a person who put the column away said something about their
	// screen, not about the conversation they have just replaced, and standing it
	// back up on /new would be the surface undoing a preference it had already
	// written to disk.
	// THE WATCHERS GO TOO, and the generation is bumped so an event already in
	// flight on one of their lanes cannot write a current tool into the session
	// that replaced them (see [taskPilot]). Each is given back rather than
	// dropped: under a switch the agent they are watching is still running, so
	// nothing else will ever close their channels.
	for _, pilot := range a.pilots {
		if pilot != nil && pilot.stop != nil {
			pilot.stop()
		}
	}
	a.pilots = nil
	a.pilotGen++
	// The "@" list's snapshot goes with them. The project's index survives — it
	// is the directory's and not this conversation's — but the live rows merged
	// into it are this conversation's, and the next "@" reads it again
	// (taskmention.go).
	a.dropTaskMentions()
}

// redirectLane is the placeholder the input box wears while a proposal is open.
//
// It is applied to the block the input already rendered rather than passed into
// it, because the box's hint slot belongs to the model picker (input.go) and the
// two are never up at the same time. An empty draft renders as the bare prompt,
// which is exactly the row a placeholder goes on.
func (a *app) redirectLane(rows []string, width int) []string {
	if !a.awaitingTask() || len(rows) == 0 || !a.input.empty() || a.pick.open {
		return rows
	}
	// THE ROOM SEGMENT SURVIVES THIS ROW, because the row it is replacing was laid
	// out with the segment in front of it: rebuilding row zero without it would
	// leave the caret's column counted through a lead the frame had stopped
	// drawing (room.go's [app.roomLead]). It is "" whenever no room is open, which
	// is every frame a proposal is normally answered on.
	lead := a.roomLead(width)
	room := width - ansi.StringWidth(lead) - ansi.StringWidth(prompt)
	out := append([]string(nil), rows...)
	out[0] = lead + a.pal.dim(prompt) + a.pal.ask(fit(taskRedirectLane, room))
	return out
}
