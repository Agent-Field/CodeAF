package session

// A HARNESS BEING DESIGNED IS A TASK, AND A TASK IS A PLACE.
//
// harness_build.go is the design itself — the meta-guide, the review pass, the
// validator, the card nobody may skip — and none of that changed. What changed
// is where it happens. A design used to be a goroutine with a note in front of
// it: one dim line said a harness was being written, a minute or two passed
// with nothing on screen, and a card arrived. From the outside that is
// indistinguishable from a program that did nothing, and there was nothing to
// walk into, nothing to say to it, and no number to refer to it by afterwards.
//
// So a design is admitted to the WORK GRAPH (task_run.go) as a node of its own.
// It costs nothing to say and it buys everything the tasker already has:
//
//	a row on the roster        with a live phase and an id a person can say
//	a room                     enter it, watch the page being written, esc out
//	a journal                  the design thread, on disk, permanently
//	a stop                     the same ✕ and the same `Cancel("task:4")`
//	a settle card              naming the harness and the version it saved as
//
// ── IT IS ADMITTED, NOT PROPOSED ──
//
// [Agent.proposeTask] asks the person before the work starts, on a countdown.
// This does not, and the reason is that THE QUESTION ALREADY EXISTS one step
// later: nothing is written to the registry until somebody answers the design
// card, which is the whole bargain harness_build.go makes. A countdown in front
// of it would be two questions about one decision — "may I write a page?" and
// then "may I keep the page?" — and the first of those is a question about
// spending a minute, which is what the ✕ on the row is for.
//
// ── THE PHASES ARE THE POINT OF THE ROW ──
//
// A node of this kind moves through three named moments and says so on the wire
// ([TaskNotice.Doing]): it is DESIGNING while the page is being written, it is
// AWAITING YOUR LOOK while the card is up, and then it lands — saved, declined,
// or failed with the reason. The state under all of that is plain `running`
// until it settles, because it is running; the phase is the same fact in the
// vocabulary of the work rather than of the graph.
//
// ── AND THE ROOM IS A CONVERSATION, WHICH IS WHY IT HAS AN AGENT ──
//
// An ordinary node's room holds the worker, and the person's words reach it
// because it is always mid-turn. A design has no worker — it is two model calls
// and a wait — so the room would have been empty and steering it would have
// been refused. What stands in it instead is a THREAD AGENT: a child session
// whose transcript is the design's own story (the brief, the page it wrote,
// what became of it), whose journal is the permanent record of it, and which
// wakes to answer anything the person says into the room (Config.roomThread).
// The page is in its context, so "why did it choose two steps?" and "would this
// work for the nightly build?" are questions it can actually answer.
//
// AND THE JOURNAL HOLDS MORE THAN THAT TRANSCRIPT DOES, which is the one place
// the two part company. What the designer THINKS goes into the room as it
// streams, and what each stage REACHED is journaled as it finishes
// ([designSeat.noted]) — the draft with its card, an attempt the law turned
// down, what the review made of the page — so the room reads back tomorrow as
// the story it was, while none of it is put in front of the thread, because a
// draft that was replaced is a wrong answer waiting to be given
// ([Agent.journalOnly]).
//
// WHAT NEVER CROSSES IS THE REPLY ITSELF. The designer answers with one enormous
// JSON envelope, and a room that typed that envelope into itself at reading
// speed buried the very thing somebody walked in to watch. The page being typed
// is accounted for by the replacing progress row instead (harness_build.go's
// harnessProgress), which reads the same stream and says "naming it: X", "4
// steps so far", "receiving · 12.3 KB".
//
// AND THE THING THE MODEL NEEDS THAT NOBODY WANTS TO READ GOES IN SYSTEM-ROLE.
// The thread agent has to reason off the PAGE — the real one, with its node ids
// and fields — while the person in the same room wants the card and nothing but.
// Both live in one transcript, so the split is the role: the surfaces skip system
// messages when they draw a transcript, and the transport takes them anywhere in
// the array ([harnessThreadBrief] states the whole mechanism, and
// [harnessPageContext] is what rides on it).
//
// AND THE THREAD CAN CHANGE THE PAGE, WHICH IS WHY THE ROOM IS WORTH STANDING
// IN. It used to be able only to talk about one: asked to "change it to also run
// the linter" it said, correctly and uselessly, that a revision was a new design
// and pointed at the door. A person looking at a draft, in the draft's own room,
// told to leave and start over — and the key on the card that offered to help
// silently dropped the page first.
//
// It has one verb for that now (tools_harness.go's revise_design), and the verb
// hands the person's own words to the design loop parked on the card
// ([TaskNode.reviseDoor]). The card comes down, the designer writes the page
// again with the change in it, the same milestones are journaled for the rework,
// and a new card goes up. It goes round as many times as it takes.
//
// WHAT IT STILL CANNOT DO IS SAVE. Nothing in this thread reaches the registry:
// only the person's approval of a card does, and that has not moved. Which is
// also why the card is no longer the only door — the room draws an approval row
// of its own while one is waiting (internal/tui3's roomapproval.go), so the
// three things a person can do about a page can all be done where they are
// standing.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// harnessDesignSpec is what one design node is admitted with: the brief the
// designer is given, and the model that writes it. It rides on [taskSpec] and
// is what tells [Agent.runTaskNode] which body this node has.
type harnessDesignSpec struct {
	goal   string
	model  string
	effort provider.Effort
}

// ── WHO IS WATCHING THE PAGE BEING WRITTEN ──────────────────────────────────

// designSeat is the design's place in the world, carried down to the model call
// itself: the node's number, the room somebody can be standing in while the page
// is written, and the thread whose journal is the permanent record of it.
//
// It is threaded all the way to harness_build.go's harnessComplete because that
// is the only place the two things a watcher needs are produced — the stream,
// delta by delta, and the assembled reply at the end of it. Nothing above that
// call has either.
//
// A ZERO SEAT IS A DESIGN NOBODY IS WATCHING: a caller with no node, and every
// test of the design ladder. Each method below is written to do nothing in that
// case rather than to need a guard at the call site.
type designSeat struct {
	// id is the node's number, and it is the same number the live design block
	// in the chat is keyed by ([Agent.emitHarness]). One design, one id.
	id uint64
	// room is the live lane a person in the room is reading, and thread is the
	// child whose journal that room's HISTORY is read back out of when somebody
	// opens the design again later (internal/tui3's readRoomJournal).
	room   *taskRoom
	thread *Agent
}

// says tees one streamed chunk of the designer's THINKING into the room, in the
// kind a worker's room is already filled with — task_run.go's runTaskChild
// publishes exactly this, and it is what makes a design room the same kind of
// place as a worker's.
//
// THE REPLY ITSELF IS NOT PROSE AND MUST NOT BE TYPED IN HERE. What the designer
// streams as content is one JSON envelope — `{"cues": …, "justification": …,
// "harness": {…}}`, and after it the review pass's `{"findings": …, "ops": …}` —
// so forwarding those deltas filled the room with a wall of braces scrolling past
// at reading speed, over the top of the watching it was meant to serve.
//
// THE ROOM'S LIVE ACCOUNT OF THE PAGE BEING TYPED IS THE REPLACING PROGRESS ROW.
// harness_build.go's harnessProgress reads this same stream and publishes
// EventHarnessProgress — "naming it: X", "4 steps so far", "receiving · 12.3 KB",
// and the stall clock when nothing has arrived for ten seconds — which the
// surface draws as one line that stays a line (internal/tui3's
// progressHarnessRoom). The reasoning is the half of the stream a person can
// actually read, so it is the half that crosses; what a finished stage reached is
// said once, in words, by [designSeat.noted].
func (s designSeat) says(kind provider.StreamEventKind, delta string) {
	if s.room == nil || delta == "" || kind != provider.StreamReasoning {
		return
	}
	s.room.publish(Event{Kind: EventReasoning, Text: delta})
}

// noted closes one design stage: the milestone it reached, in a person's words,
// into the node's journal — and then the room is told the stage is over.
//
// THE JOURNAL IS WHAT THE ROOM IS READ OUT OF AFTERWARDS. The reasoning
// [designSeat.says] published is the live lane only — it exists for whoever was
// subscribed at that instant — so without this line a design opened tomorrow
// would show the finished card and nothing about how it was arrived at.
//
// WHAT IS KEPT IS THE ACCOUNT AND NOT THE TRANSPORT. This used to journal the
// designer's reply verbatim, which meant reopening a design replayed the same
// wall of JSON the live lane had just stopped showing. Every fact worth keeping
// out of that envelope is rendered by [harnessDraftNote], [harnessRefusedNote]
// and [harnessReviewNote] instead — and the raw reply still goes back to the
// MODEL through the retry history exactly as it always did (harness_build.go's
// designPage), because that reader is repairing what it wrote and needs the text
// it wrote.
//
// It is JOURNALED and not recorded, and [Agent.journalOnly] states both reasons.
//
// AN EMPTY MILESTONE IS A STAGE WITH NOTHING TO SAY, AND STILL AN EVENT, which is
// why the two halves are not separable here. THE EVENT IS WHAT THE ROOM'S
// CATCH-UP IS WAITING TO BE TOLD (task_room.go's taskCatchup.record): one of
// these per finished model call, or the reasoning of a stage that said nothing —
// the JSON repair turn, a review that could not be read — would sit in the
// catch-up and be handed to the next person through the door as though it were
// still in flight. It carries no usage deliberately — a design's calls are billed
// to the conversation that asked for it and never to this node
// ([Agent.designHarnessNode] says why) — and a turn that priced nothing is one
// the roster's own fold ignores (internal/tui3's pilotEvent).
func (s designSeat) noted(milestone string) {
	if s.thread != nil && strings.TrimSpace(milestone) != "" {
		s.thread.journalOnly(textMessage("assistant", milestone))
	}
	s.room.publish(Event{Kind: EventTurnDone})
}

// broke ends a design call that never answered. The room says what happened and
// drops the half-written reply it was holding: nothing is coming to finish it,
// and nothing was written to disk for the next person to read it off.
//
// A CANCELLED CALL IS NOT NEWS IN HERE. There are two ways a design's context
// ends — somebody pressed ✕, or the design's own window ran out — and this build
// already has a sentence for each of them, in the words a person would use
// ([harnessDesignEnding], and the settle card under it). "error: context
// canceled" would be a third account of the same event, in the vocabulary of the
// runtime, arriving one moment before the room closes anyway.
func (s designSeat) broke(err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	s.room.publish(Event{Kind: EventError, Err: err})
}

// ── WHAT A STAGE SAYS WHEN IT FINISHES ──────────────────────────────────────
//
// The three renderings a design journals through [designSeat.noted], in the order
// a person meets them: an attempt the law turned down, the draft that passed it,
// and what the review pass made of that draft. Between them they hold every fact
// the designer's envelope carried that a person would want back — and none of the
// envelope.

// harnessDraftNote is the accepted draft: what it is called, how big it is, why
// the designer shaped it that way, the sentences it will answer to, and the card.
//
// THE CARD IS FENCED, and that is not decoration. The room renders assistant text
// as markdown (internal/tui3's renderMarkdown), and markdown folds single
// newlines into running prose — which is all a card is made of. Unfenced, a card
// whose columns line up at a glance arrived as one run-on paragraph.
func harnessDraftNote(page subharness.Harness, draft harnessDesign) string {
	var out strings.Builder
	fmt.Fprintf(&out, "The draft is written — %s · %d steps.", page.Id.Name, len(page.Program.Nodes))
	// A designer that justified nothing gets no paragraph rather than an empty
	// one: unknown renders as nothing, here as everywhere.
	if why := strings.TrimSpace(draft.Justification); why != "" {
		out.WriteString("\n\n" + why)
	}
	if len(draft.Cues) > 0 {
		out.WriteString("\n\nIt answers to: " + strings.Join(draft.Cues, " · "))
	}
	out.WriteString("\n\n```\n" + subharness.Card(page) + "\n```")
	return out.String()
}

// harnessRefusedNote is an attempt the law turned down, as one line in the story
// of a design that took several.
//
// THE VALIDATOR'S OWN SENTENCE IS KEPT because it is the most specific account
// that exists of what was wrong with the page, and it is FLATTENED onto one line
// because what is being written here is a note in a story rather than a stack of
// errors. The raw draft it refused goes to the model and to nobody else
// (harness_build.go's designPage).
func harnessRefusedNote(attempt int, err error) string {
	return fmt.Sprintf("Attempt %d was refused: %s", attempt, strings.Join(strings.Fields(err.Error()), " "))
}

// harnessReviewNote is what the second pass came back with: whether it changed
// the draft, and everything it noticed either way.
//
// THE PASS LABEL IS DROPPED. Each finding carries the name of the critic's own
// checklist that raised it, which is a fact about the machinery and never about
// the harness; what a person reads is the finding.
func harnessReviewNote(revised harnessRevision) string {
	var out strings.Builder
	if changed := len(revised.Ops); changed > 0 {
		thing := "things"
		if changed == 1 {
			thing = "thing"
		}
		fmt.Fprintf(&out, "The review read the draft and changed %d %s:", changed, thing)
	} else {
		out.WriteString("The review read the draft and left it as written.")
	}
	// The findings are bullets under either sentence, because "here is what I
	// looked at and changed nothing about" is as much of a report as a patch is.
	bullets := make([]string, 0, len(revised.Findings))
	for _, finding := range revised.Findings {
		if text := strings.TrimSpace(finding.Text); text != "" {
			bullets = append(bullets, "- "+text)
		}
	}
	if len(bullets) > 0 {
		out.WriteString("\n\n" + strings.Join(bullets, "\n"))
	}
	return out.String()
}

// The three phases a design node publishes on [TaskNotice.Doing], in the words a
// person would use about them.
//
// "awaiting your look" is spelled exactly as the surface already spells the one
// other moment when work is finished and waiting on somebody (internal/tui3's
// taskUnverifiedWord), because it is the same thing happening: the machine has
// done its part and the next move is a person's.
//
// HarnessPhaseAsking IS EXPORTED AND THE OTHER IS NOT, and the asymmetry is the
// point. A surface has to be able to tell this one phase apart from work that is
// genuinely running, because a design at this phase is NOT running — the machine
// has finished its part and the only step left is a person's, so it belongs in
// the tally that says how many things need somebody rather than in the one that
// says how many things are working (internal/tui3's railGroupOf). Comparing
// against a string spelled out again over there would be the same fact written
// down twice, and the second copy would be the one that drifts.
const (
	harnessPhaseDesigning = "designing"
	HarnessPhaseAsking    = "awaiting your look"
)

// designStoppedWord is the report a design a person ended settles with. It says
// the one thing somebody who stopped a design needs to know, which is not that
// it stopped — they pressed the key — but that the registry is exactly as they
// left it.
const designStoppedWord = "harness design stopped; nothing was saved"

// takesSlot says whether this node is one of the ones the two concurrency
// ceilings are about ([TaskGraph.limit] and its governor).
//
// A WORKER DOES; A DESIGN DOES NOT. The ceilings model what a node costs this
// machine — a whole agent with a checkout, a build and a long run of tool calls
// — and a design has none of that: it is two model calls against a guide, and
// then a card sitting on screen for as long as the person takes to read it.
// Counted, a single design would hold a slot through somebody's lunch, and on a
// machine that is busy it would not start at all — which is the exact opposite
// of what the ceilings are for, because "the machine is full" is not a reason to
// refuse to write a page.
func (n *TaskNode) takesSlot() bool { return n.kind != TaskKindHarness }

// kind is what sort of node this spec makes, in the word the wire carries
// ([TaskNotice.Kind]).
func (s taskSpec) kind() TaskKind {
	if s.design != nil {
		return TaskKindHarness
	}
	return ""
}

// harnessNodeTitle is what a design node is called on the roster and in the
// stop card: the harness word, then the goal.
//
// It leads with "harness" because a chip is eighteen cells wide and a person
// scanning a strip of running work needs to know WHICH KIND of thing this is
// before they need to know what it is about.
func harnessNodeTitle(goal string) string {
	return clip("harness · "+firstLine(goal), hintLimit)
}

// THE ID IS TAKEN BEFORE THE NODE IS, and that is why these are two functions.
//
// [TaskGraph.admit] turns the frontier, and a design has no dependencies and
// takes no slot, so the node is RUNNING on its own goroutine before admit has
// returned — and a fast design is a card on the lane a moment later. The
// announcement that says a design started has to be in front of that card, so
// the caller mints the id here, says its line, and admits second
// (tools_harness.go). A number cannot be announced before it exists, and a card
// must not arrive before the sentence that explains it.
func (a *Agent) reserveHarnessDesign() uint64 { return a.graph().reserve() }

// admitHarnessDesign puts one design into the work graph under an id
// [Agent.reserveHarnessDesign] already handed out.
//
// The spec's brief and acceptance are the node's frozen goal contract like any
// other node's ([TaskNode]), and they are filled honestly even though no auditor
// will ever read them: they are what the room's header and the project index
// show, and a node whose brief said nothing would be a row nobody can place.
func (a *Agent) admitHarnessDesign(id uint64, goal string, call roleRequest) {
	a.graph().admit(id, taskSpec{
		title:      harnessNodeTitle(goal),
		summary:    "Design a reusable sub-harness for: " + firstLine(goal),
		brief:      goal,
		acceptance: "a page the person approves, saved into this machine's harness registry",
		model:      call.model,
		design:     &harnessDesignSpec{goal: goal, model: call.model, effort: call.effort},
	})
}

// designHarnessNode is the design node's whole life, and it is the shape
// [Agent.workTaskNode] has one file along: every ending is a state and a report
// rather than an error, because everything that can go wrong here is something
// the person who asked for the harness has to be told in words.
//
// WHAT IT SPENDS IS NOT ALL ON THIS NODE'S BILL, and it is worth saying rather
// than papering over. The two design calls are made by the SESSION's own client
// (harness_build.go's designPage), so they are charged where they always were —
// to the conversation — and the figure on this node's row is what its thread
// agent spent answering the person. A node that claimed the design's own cost
// would be double-counting money the session has already booked.
func (a *Agent) designHarnessNode(ctx context.Context, node *TaskNode, listed *job) TaskState {
	design := node.spec.design
	goal, model := design.goal, design.model
	ctx = (roleRequest{model: model, effort: design.effort}).context(ctx)
	// THE CLOCK IS TAKEN INSIDE THE ROUND AND NOT HERE, which is where it moved
	// when a design stopped being one page ([designRun.round] carries the whole
	// argument). Nothing in this function has a deadline of its own: what bounds
	// the node is the node's own leash, and what bounds each attempt at writing a
	// page is a window that round takes and gives back.
	log := taskLog(listed)
	fmt.Fprintf(log, "task %d · %s\ndesigning with %s\n", node.id, node.title(), model)

	// THE CHANGES LANE IS OPENED BEFORE THE THREAD IS BUILT, and it has to be:
	// the thread's one extra hand is wired out of it, and a belt is assembled
	// once, when the agent is constructed (agent.go). A door handed over
	// afterwards would be a verb the model is never told it has.
	changes := node.openRevisions()

	// THE THREAD IS BUILT BEFORE THE FIRST MODEL CALL, so that somebody who
	// walks into the room while the page is being written finds a room with
	// somebody in it rather than an empty one. It works in the session's own
	// directory because a design writes no files: there is nothing to isolate,
	// so there is no worktree and no branch to bring home.
	child, err := a.newTaskAgent(ctx, a.config.Workspace, node, "")
	if err != nil {
		node.finish("the design could not be started: "+err.Error(), nil, "", "")
		return TaskFailed
	}
	defer func() {
		_ = child.Close()
		a.foldTaskUsage(node, child)
	}()
	room := node.openRoom()
	// THE LANE IS TAKEN BEFORE ANYBODY CAN BE IN THE ROOM, and only the draining
	// of it is handed to a goroutine. Subscribing inside the pump would leave a
	// window in which a person's first line woke a turn that no lane was carrying
	// — journaled, paid for, and never drawn in the room they typed it into.
	turns := child.Wakes()
	room.speaking(child)
	go pumpHarnessThread(turns, room)

	// The opening of the thread, in two halves that go to two readers. The
	// person's own sentence is what the room shows somebody who walks in; the
	// standing instructions under it are addressed to the thread agent and are
	// SYSTEM-ROLE, which is what keeps them out of the room ([harnessThreadBrief]
	// states the whole mechanism).
	child.record(textMessage("user", harnessThreadOpening(goal)))
	child.record(textMessage("system", harnessThreadBrief(model)))

	run := &designRun{
		agent:   a,
		node:    node,
		child:   child,
		seat:    designSeat{id: node.id, room: room, thread: child},
		log:     log,
		goal:    goal,
		model:   model,
		changes: changes,
	}
	// ── A DESIGN IS A LOOP NOW, AND THAT IS THE WHOLE OF WHAT THIS FILE LEARNED ─
	//
	// It used to be a straight line: write a page, raise a card, take the yes or
	// the no, land. The room underneath it was a place a person could stand and
	// watch and ask questions in, and the one thing they most wanted to do there —
	// say what was wrong with the page — was the one thing the line had no shape
	// for. The thread refused it in so many words, and what a person had to do
	// instead was drop a good design and commission a new one from the top.
	//
	// So the line is a loop, and the extra edge is the only thing that is new: a
	// wait can end in a CHANGE as well as an answer (harness_build.go's
	// [harnessWord]), and a change goes round again with the standing page in
	// hand. Everything else about a round is what it always was — the same
	// gauntlet, the same milestones in the same journal, the same card, the same
	// registry untouched until somebody approves. Nothing about a design that is
	// approved on the first card behaves differently than it did.
	//
	// ONE THING ENDS THIS LOOP, AND IT IS ALWAYS A ROUND SAYING SO. Saved,
	// declined, stopped, written badly enough three times running, or a card the
	// process closed under: every one of them is a state and a report from
	// [designRun.round], and there is no other exit.
	var change string
	for {
		state, settled, next := run.round(ctx, change)
		if settled {
			return state
		}
		change = next
	}
}

// designRun is one design job, carried across however many rounds the person and
// the designer take to agree on a page. Everything on it is constant for the
// whole job except [designRun.standing], which is the page as it currently
// stands.
type designRun struct {
	agent *Agent
	node  *TaskNode
	child *Agent
	// seat is who is watching every one of this job's model calls: the same room
	// and the same journal for the fourth rewrite as for the first draft, which
	// is what makes the room read back as one story ([designSeat]).
	seat designSeat
	log  io.Writer
	// goal is the brief the design was admitted with, and it does not move. A
	// rewrite is a change to the PAGE and never to what the harness is for; a
	// job whose goal drifted with each request would be a design nobody could
	// say what it was of.
	goal  string
	model string
	// changes is the lane the design's own thread asks for a rewrite on
	// ([TaskNode.openRevisions]).
	changes <-chan string
	// standing is the page as it stands: what the first round drafted, and then
	// whatever each rewrite made of it. It is the thing a rewrite is a rewrite OF.
	standing harnessAccepted
}

// round is one write-and-ask: the page written (or written again), the card
// raised, and whatever answers it.
//
// It reports the node's ending and whether this round WAS the ending. When it
// was not, the third value is the change the next round writes from — the only
// way this loop goes round, and the only thing a round hands the next one.
func (r *designRun) round(ctx context.Context, change string) (TaskState, bool, string) {
	a, node, child := r.agent, r.node, r.child

	// A REWRITE IS A DESIGN AND SAYS SO ON THE ROW. The phase goes back to
	// "designing" for exactly as long as the page is being written again, which
	// is what takes the approval row out of the design's room and puts the live
	// progress row back (internal/tui3): the person asked for a change, and what
	// is happening now is the change being made.
	node.doingNow(harnessPhaseDesigning)

	// ── THE CLOCK IS ON THE WRITING, AND IT IS TAKEN PER ROUND ──────────────
	//
	// THE CLOCK IS ON THE WRITING AND ON NOTHING ELSE, which this node's
	// lifecycle learned the hard way. The window used to cover both halves of the
	// job — the page being written AND the card waiting for an answer — so a
	// design that wrote its page in ten minutes and then sat on somebody's screen
	// was collected at thirty and reported as "the design ran out of time before
	// it finished; nothing was saved". Both clauses were false: it had finished,
	// and the page it finished was in the room. A card is a question on a
	// person's screen and a person is not a step that can be timed out.
	//
	// AND IT IS TAKEN HERE RATHER THAN ONCE FOR THE JOB, because a design is a
	// conversation now and a conversation has more than one page in it. One
	// window over the whole job would start on the first draft and expire in the
	// middle of the third rewrite — cutting somebody off for having worked on the
	// design rather than waved it through. Each attempt at writing a page gets
	// the window whole, and it is given back the instant a page exists.
	writing, cut := context.WithTimeout(ctx, a.harnessWritingWindow())
	// AND THE DESIGNER WRITES INTO THIS ROOM, which is the whole of what the seat
	// is for: the room to stream into while it thinks and drafts, and the thread
	// whose journal keeps that discussion after the card has scrolled away
	// ([designSeat]). Every milestone a first draft journals, a rewrite journals
	// too — the draft that passed, an attempt the law turned down, what the review
	// made of it — because they are the same stages happening again.
	accepted, err := r.write(writing, change)
	if err != nil {
		// The window is given back on the way out rather than here, so that the
		// report below can still ask it whether it was the thing that ran out.
		defer cut()
		if ctx.Err() != nil && !node.stoppedByPerson() {
			return a.pauseHarnessNode(node, child), true, ""
		}
		fmt.Fprintf(r.log, "design failed: %v\n", err)
		return a.landHarnessNode(node, child, harnessDesignEnding(writing, node, harnessWriteFailed(change, err)), TaskFailed), true, ""
	}
	// THE WRITING IS OVER, SO ITS CLOCK IS OVER. Cutting it here rather than
	// leaving it to a deferred call is what makes the paragraph above true: from
	// this line to the answer there is no timer anywhere in this node, and no
	// ending it could reach can say the design ran out of time — because a page
	// exists.
	cut()
	r.standing = accepted
	page := accepted.page

	// THE CARD IS WHAT A PERSON READS AND THE PAGE IS WHAT THE MODEL READS, and
	// they are two messages for exactly that reason ([harnessPageContext]).
	if change == "" {
		child.record(textMessage("assistant", harnessPageThread(page)))
	} else {
		child.record(textMessage("assistant", harnessPageRewritten(page)))
	}
	if encoded, encodeErr := subharness.Encode(page); encodeErr == nil {
		// AND THE MODEL'S COPY IS REPLACED IN WORDS, because it cannot be replaced
		// any other way ([harnessPageSuperseded] states the whole mechanism): a
		// thread carrying two pages would answer from whichever one it read.
		if change == "" {
			child.record(textMessage("system", harnessPageContext(encoded)))
		} else {
			child.record(textMessage("system", harnessPageSuperseded(encoded)))
		}
	}
	fmt.Fprintf(r.log, "page written: %s\n", page.Id.Name)

	// THE CARD, on the same lane and answered by the same method it always was
	// (harness_build.go's askHarnessDesign). The node stays running under it,
	// because it is: the work is not over until somebody says what to do with
	// the page, and a node that settled here would take its own room away one
	// moment before the person needed it — the room being where they can ask the
	// design about the page they are being shown, and where they can say what
	// they want changed about it.
	node.doingNow(HarnessPhaseAsking)
	word, err := a.askHarnessDesign(ctx, node.id, page, r.model, r.changes)
	if err != nil {
		// TWO THINGS END THIS WAIT WITHOUT AN ANSWER, and neither of them is the
		// design failing: a person pressed ✕, or the session closed under the card.
		// The page was written either way, so the report says what became of the
		// page rather than pretending the work never happened, and this node
		// SETTLES instead of pausing — a design has no goal on its checkpoint to
		// resume from, and there is nobody left to answer a card whose process is
		// gone (task_store.go's interrupt says the same).
		fmt.Fprintf(r.log, "card unanswered: %s\n", page.Id.Name)
		return a.landHarnessNode(node, child, harnessCardEnding(node, page), TaskDone), true, ""
	}
	if word.change != "" {
		// ANOTHER ROUND, AND NOTHING HAS LANDED. The card was withdrawn at the
		// instant those words were taken (harness_build.go), the registry is
		// untouched, and the page in hand is what the next round rewrites.
		fmt.Fprintf(r.log, "rewriting: %s\n", firstLine(word.change))
		return "", false, word.change
	}
	if !word.answer.run {
		// A DECLINE IS NOT A FAILURE. The person was asked and they answered,
		// which is this node's whole job done; a failed row here would send
		// somebody looking for a fault that is their own decision (task.go says
		// the same about a declined proposal).
		return a.landHarnessNode(node, child,
			fmt.Sprintf("harness %q was designed and not saved", page.Id.Name), TaskDone), true, ""
	}
	saved, err := a.saveHarness(page, accepted.cues)
	if err != nil {
		fmt.Fprintf(r.log, "save failed: %v\n", err)
		return a.landHarnessNode(node, child,
			fmt.Sprintf("harness %q could not be saved: %v", page.Id.Name, err), TaskFailed), true, ""
	}
	fmt.Fprintf(r.log, "saved: %s v%d\n", saved.Id.Name, saved.Id.Version)
	// THE NODE IS NOW THE HARNESS'S THREAD, and this is where that is written
	// down: the next sentence that names this harness can be pointed at a number
	// (see [Agent.harnessThread]).
	a.rememberHarnessThread(saved.Id.Name, node.id)
	return a.landHarnessNode(node, child, harnessSavedWord(saved), TaskDone), true, ""
}

// write is the one branch between a first draft and a rewrite, and it is the
// only one: both end in the same gauntlet one function down
// (harness_build.go's writeHarness).
func (r *designRun) write(ctx context.Context, change string) (harnessAccepted, error) {
	if change == "" {
		return r.agent.draftPage(ctx, r.goal, r.model, r.seat)
	}
	return r.agent.revisePage(ctx, r.goal, r.model, r.standing, change, r.seat)
}

// harnessWriteFailed is the report a round that never reached a card settles
// with, and it says WHICH of the two failed.
//
// A rewrite that could not be written is not the first design failing: there is
// a page the person has already read and asked about, and a settle card reading
// "the design failed" would be a sentence about the wrong thing. What both
// spellings agree on is the part that matters, which is that nothing was saved.
func harnessWriteFailed(change string, err error) string {
	if change == "" {
		return "the design failed: " + err.Error()
	}
	return "the rewrite failed and nothing was saved: " + err.Error()
}

// pauseHarnessNode is what a design says when the PROCESS ended under it — the
// session closed while the page was still being written, or while the card was
// still up.
//
// IT DOES NOT PROMISE A RESUME, because a design does not get one. The node is
// deliberately left running so the checkpoint carries it, and the next session
// settles it with this same sentence (task_store.go's [interrupt], which also
// says why it must not go back on the frontier). A room that said "paused — it
// resumes" tonight and a recovery note that said nothing was saved tomorrow
// would be the harness telling somebody two different things about one page.
func (a *Agent) pauseHarnessNode(node *TaskNode, child *Agent) TaskState {
	const report = harnessInterruptedReport
	child.record(textMessage("assistant", report))
	node.doingNow("")
	node.finish(report, nil, "", "")
	return ""
}

// landHarnessNode settles the node and closes the thread with the same sentence.
//
// ONE SENTENCE, TWO READERS, and that is the whole reason this is a function.
// The report is what the settle card shows the person and what the completion
// note tells the model (task_run.go's taskNote); the same line recorded into the
// thread is the last thing in the transcript, so somebody opening the room a
// week later reads the outcome at the bottom of the story rather than having to
// infer it from a card that has scrolled away.
//
// THE STATE IS THE CALLER'S TO NAME, and every call site names it, because the
// difference between a design that broke and a design the person declined is a
// judgement about what happened rather than something readable off the sentence
// that describes it. The one thing decided here is the STOP: [TaskGraph.stop]
// sets `stopped` before it cuts the context, so by the time this body notices
// its context is gone the answer to "who did that" is already written down —
// and a stopped node settles failed whatever the caller asked for, because
// nothing finished and the surface has to be able to say who ended it.
func (a *Agent) landHarnessNode(node *TaskNode, child *Agent, report string, state TaskState) TaskState {
	child.record(textMessage("assistant", report))
	node.doingNow("")
	node.finish(report, nil, "", "")
	if node.stoppedByPerson() {
		return TaskFailed
	}
	return state
}

// harnessDesignEnding is the report for a design that did not reach a card:
// the reason, or — when a person ended it — their own word for it.
//
// THE CONTEXT IT IS HANDED IS THE WRITING'S, and it is the only one that may be
// asked, because "it ran out of time" is a true sentence about writing a page and
// was never a true sentence about a card. A design that reached a page never
// comes through here at all ([Agent.designHarnessNode]); [harnessCardEnding] is
// what speaks for that one.
func harnessDesignEnding(writing context.Context, node *TaskNode, reason string) string {
	if node.stoppedByPerson() {
		return designStoppedWord
	}
	if writing.Err() != nil {
		return "the design ran out of time before it finished; nothing was saved"
	}
	return reason
}

// harnessCardEnding is the report for a design whose page was written and whose
// card was never answered — a person's ✕, or the session closing under it.
//
// IT NAMES THE PAGE, because the page is the fact the person needs: the design
// did its work, and what did not happen is the keeping of it. Nothing here can
// say the design failed or ran out of time; the only clock this node ever had
// stopped when the page was written.
func harnessCardEnding(node *TaskNode, page subharness.Harness) string {
	if node.stoppedByPerson() {
		return designStoppedWord
	}
	return fmt.Sprintf("harness %q was designed; the card went unanswered, so nothing was saved", page.Id.Name)
}

// harnessWritingWindow is how long this session gives the writing of one page,
// which is [harnessDesignWindow] unless the person's config named another. The
// override exists for the same reason Config.TaskDeadline's does: the behaviour
// at the end of the window is worth a test, and half an hour is not a thing a
// test can wait for.
func (a *Agent) harnessWritingWindow() time.Duration {
	if window := a.config.HarnessDesignWindow; window > 0 {
		return window
	}
	return harnessDesignWindow
}

// harnessSavedWord is the settle card's line: the name, the version it landed
// as, and what to do with it.
func harnessSavedWord(saved subharness.Harness) string {
	line := fmt.Sprintf("harness %q v%d saved", saved.Id.Name, saved.Id.Version)
	// The version is spelled even at v1 here, unlike the registry listing, and
	// it is deliberate: this line is about a thing that has JUST come into
	// existence, and "v1" is the news that it is the first of them.
	return line + "\nIt is offered by the turn itself whenever somebody's words match it; there is no command that runs one."
}

// doingNow moves a node to a named phase and tells the world, on
// [TaskNode.mending]'s terms and for its reason: nothing about the node's state
// moved, so this is an update and never a landing, and it is announced only when
// the phase actually changes.
func (n *TaskNode) doingNow(phase string) {
	n.graph.mu.Lock()
	changed := n.doing != phase
	n.doing = phase
	n.graph.mu.Unlock()
	if changed {
		n.graph.announce(n)
	}
}

// ── THE LANE A PERSON ASKS FOR A CHANGE ON ──────────────────────────────────
//
// A design card had two answers and now has three, and the third one arrives
// through a completely different door than the other two. Save and drop come
// from a SURFACE, through [Agent.ResolveHarness], because they are keys somebody
// pressed. "Make it also run the linter" arrives from the design's own THREAD,
// because it is a sentence somebody said — and the thing that hears sentences in
// this room is the thread agent, one turn later, having decided that what it just
// heard was a request and not a question (tools_harness.go's revise_design).
//
// SO THE CHANNEL HANGS ON THE NODE. It is the one object both ends can reach:
// the design loop is parked on it in [Agent.askHarnessDesign], and the thread
// agent was handed a closure over it when it was built (task_run.go's
// newTaskAgent wires Config.reviseDesign from [TaskNode.reviseDoor]).

// openRevisions mints the lane a design's thread asks for a rewrite on and
// answers with the reading end. It is called once, before the thread agent
// exists, because that agent's belt is assembled at construction.
//
// IT IS BUFFERED TO ONE AND IT IS NOT A QUEUE. A person asks for one change and
// waits to see what it did to the page; a second ask arriving while the first is
// still being written is refused in words rather than stacked behind it, because
// a designer handed two changes one after another would answer the second about a
// page written for the first that nobody has read.
func (n *TaskNode) openRevisions() <-chan string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.revise == nil {
		n.revise = make(chan string, 1)
	}
	return n.revise
}

// reviseDoor is how a design's thread reaches the loop parked on its card — or
// NIL, which is every node that is not a design.
//
// The nil is the whole gate. tools.go's law is that a capability with nothing
// behind it is absent rather than broken, so an agent with no door here is not
// given the revise_design verb at all: an ordinary task node's thread cannot
// even name the tool, let alone call it and be refused.
func (n *TaskNode) reviseDoor() func(string) error {
	n.graph.mu.Lock()
	lane := n.revise
	n.graph.mu.Unlock()
	if lane == nil {
		return nil
	}
	return func(change string) error {
		// THE CARD HAS TO BE UP, AND THE PHASE IS HOW THAT IS ASKED. A change
		// wanted while the page is still being written is a change to a page
		// nobody has read; one wanted after the design has landed is a change to
		// something already saved or already gone. Neither is this door's to take,
		// and both are worth a sentence rather than a silence.
		n.graph.mu.Lock()
		waiting := n.doing == HarnessPhaseAsking
		n.graph.mu.Unlock()
		if !waiting {
			return errors.New("this design is not waiting on an answer right now, so there is no page in front of them to change")
		}
		select {
		case lane <- change:
			return nil
		default:
			return errors.New("a change to this page is already being made")
		}
	}
}

// stoppedByPerson reports whether [TaskGraph.stop] claimed this node. The flag
// is set BEFORE the context is cut, which is what makes it readable here: by
// the time this body notices its context is gone, the answer to "who did that"
// is already written down.
func (n *TaskNode) stoppedByPerson() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.stopped
}

// harnessAskID mints the token the design card is answered by
// ([Agent.ResolveHarness]). It comes off the offer lane's counter rather than
// the graph's because that is the map the answer is delivered into — one
// question shape, one counter — and it is the reason a design node has two
// numbers: the task id a person says out loud, and this, which no surface shows.
func (a *Agent) harnessAskID() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.harnessSeq++
	return a.harnessSeq
}

// ── the thread ──────────────────────────────────────────────────────────────

// pumpHarnessThread carries the thread agent's own turns into the room.
//
// A worker's events reach the room because the runner is draining them
// (task_run.go's runTaskChild). A thread has no runner: its turns are started by
// the person steering it, which happens on somebody else's goroutine, so the
// only way to see them is the standing subscription every woken turn is
// published on. It ends when the child is closed, which closes the lane.
func pumpHarnessThread(turns <-chan (<-chan Event), room *taskRoom) {
	for turn := range turns {
		for event := range turn {
			room.publish(event)
		}
	}
}

// harnessThreadOpening is the first message in the design thread, and the first
// thing a person sees on walking into the room: the sentence that started the
// design, and nothing else.
//
// IT IS WRITTEN AS THE PERSON'S OWN WORDS because that is what it is — their
// request, forwarded. Everything the thread agent has to be TOLD about the job
// used to ride on the end of it, which meant a person opening a design read a
// paragraph of instructions addressed to a model; that half is
// [harnessThreadBrief] now.
func harnessThreadOpening(goal string) string {
	return "Design a reusable sub-harness for this:\n\n" + goal
}

// ── WHAT THE THREAD AGENT IS TOLD AND NOBODY READS ──────────────────────────
//
// THE SYSTEM ROLE IS THE ROOM'S ONE-WAY GLASS, and both of the functions below
// depend on it, so it is stated once here. A design thread has two audiences
// sharing one transcript: the person standing in the room, and the model that
// answers them. Everything recorded reaches both — [Agent.record] appends to the
// model's context AND to the journal the room is read out of — except this: the
// surfaces skip system-role messages when they turn a transcript into a page
// (internal/tui3's readRoomJournalTail switches on user, assistant and tool;
// agent.go's shapeEntries drops "system" outright). So a system message is
// exactly the thing this file needed and did not have — text the model reasons
// from that a person is never shown — and the transport takes it mid-transcript
// without complaint, because a request is an ordinary array of roled messages
// (internal/provider's client.go) and the cache breakpoints only ever count the
// system messages at the HEAD of one (its caching.go).

// harnessThreadBrief is what the thread agent is for, addressed to it.
//
// It is the half of the old opening that was never the person's to read: what
// this thread answers, what it may DO about what it is told, and what it still
// may not do.
//
// IT USED TO BEGIN BY REFUSING. The brief said a revision was a new design and
// told the thread to say so — which was true of the machinery and useless to the
// person, because they were standing in the design's own room, looking at the
// page, saying the one thing anybody ever says about a draft. The room is where
// a page is worked on now (revise_design), so the brief that denied it is gone
// and this one is written for a collaborator.
//
// WHAT IT STILL REFUSES IS THE SAVE, and that has not moved an inch: nothing in
// this thread writes to the registry, and the card is the person's to answer.
func harnessThreadBrief(model string) string {
	var out strings.Builder
	out.WriteString("This is one sub-harness's own thread, and you are the person's collaborator on the page in it. The page is being written now")
	if model = strings.TrimSpace(model); model != "" {
		out.WriteString(" by " + model)
	}
	out.WriteString("; when it is ready it appears here as a card, and the person is asked whether to save it or drop it.\n\n")
	out.WriteString("ANSWER QUESTIONS about this harness from the page — why it is shaped the way it is, what its steps do, whether it would fit some other work. Say what it does in words; never quote the page back as JSON.\n\n")
	out.WriteString("WHEN THEY WANT SOMETHING CHANGED, CHANGE IT. Call revise_design with the change stated completely and the harness is written again with it in, then put back in front of them as a new card. Do not tell them a change means starting over — it does not, and it has not for a while.\n\n")
	out.WriteString("WHAT YOU CANNOT DO IS SAVE IT. Nothing here reaches this machine's registry: only the person's approval of the card saves a page, and if they ask you to save it, say exactly that.")
	return out.String()
}

// harnessPageContext is the page itself, for the thread agent and for nobody
// else: the real thing rather than a rendering of it, so that "what does step
// three actually do?" is answered off the page and not off the card.
//
// THE PERSON IS NOT SHOWN THIS AND DOES NOT WANT TO BE. A page of JSON in the
// middle of a room is the same wall of braces this whole lane exists to have
// stopped showing — what a person came to read is the card, which says the same
// things in the shape a person reads them in. So it goes in system-role (see
// above), where the model has it and the room does not draw it.
func harnessPageContext(encoded []byte) string {
	return "The page that was written, as it will be saved:\n\n" + string(encoded) +
		"\n\nAnswer questions about this harness from this page. Do not quote it back as JSON — say what it does in words."
}

// harnessPageSuperseded is a REWRITTEN page for the thread agent, and it opens
// by retiring every copy above it.
//
// A TRANSCRIPT IS APPEND-ONLY AND THE MODEL READS ALL OF IT. [Agent.record]
// writes to the model's context and to the journal the room is read out of, and
// neither can be edited afterwards — so the page a rewrite replaces is still
// sitting in the context, and a thread with two pages in front of it answers
// from whichever one it happens to read. That is the worst kind of wrong answer
// available in this room: fluent, specific, and about a draft that no longer
// exists.
//
// It cannot be removed, so it is SUPERSEDED IN WORDS, which is a thing a model
// does act on: the sentence names the earlier copies, says they no longer
// describe this harness, and points at the one below it.
func harnessPageSuperseded(encoded []byte) string {
	return "The page has been REWRITTEN, and every earlier copy of it in this thread is superseded: none of them describes this harness any more. Ignore them. This is the page as it stands, and as it will be saved:\n\n" +
		string(encoded) +
		"\n\nAnswer questions about this harness from THIS page. Do not quote it back as JSON — say what it does in words."
}

// harnessPageThread is the page as the room shows it: the card, and what has and
// has not happened to it. A surface renders the card the same way every other
// surface does (internal/subharness's card.go is the renderer they share).
//
// THE CARD IS FENCED. It is columns of text held together by single newlines,
// and the room renders this message as markdown (internal/tui3's renderMarkdown)
// — which folds single newlines into running prose, so an unfenced card arrived
// as one run-on paragraph with its alignment gone.
//
// THE PAGE ITSELF IS NOT IN HERE ANY MORE. It used to be fenced underneath —
// labelled `yaml`, which was wrong about bytes subharness.Encode writes as JSON —
// and it was the model's copy sitting in a person's reading. The model still has
// it, one message along and out of sight ([harnessPageContext]).
func harnessPageThread(page subharness.Harness) string {
	var out strings.Builder
	out.WriteString("The page is written.\n\n```\n")
	out.WriteString(subharness.Card(page))
	out.WriteString("\n```")
	out.WriteString("\n\nNothing is saved yet — the card is up, and it is saved only if it is approved.")
	return out.String()
}

// harnessPageRewritten is the page as the room shows it after a rewrite: the
// same card, drawn by the same renderer, and one sentence saying that this is the
// second thing to stand in this spot.
//
// IT NAMES THE CHANGE AS THEIRS. A person who asked for something and is handed
// a card wants to know that the card is the answer to what they asked, and a
// room that redrew the card in silence would read as the design having started
// over by itself.
func harnessPageRewritten(page subharness.Harness) string {
	var out strings.Builder
	out.WriteString("The page is written again, with your change in it.\n\n```\n")
	out.WriteString(subharness.Card(page))
	out.WriteString("\n```")
	out.WriteString("\n\nStill nothing is saved — the card is up again, and it is saved only if it is approved.")
	return out.String()
}

// ── which thread a harness belongs to ───────────────────────────────────────

// rememberHarnessThread records that this session designed a harness in a node,
// so that a later sentence about it can be pointed at the thread rather than at
// nothing.
//
// IT IS THIS SESSION'S MEMORY AND NOT THE REGISTRY'S. A harness saved by a
// conversation last week has a thread on disk and this process has never heard
// of it, so the answer here is honestly nothing — and a surface that guessed a
// number would be offering a door onto a room that is not there.
func (a *Agent) rememberHarnessThread(name string, id uint64) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.harnessThreads == nil {
		a.harnessThreads = make(map[string]uint64, 1)
	}
	a.harnessThreads[name] = id
}

// harnessThread is the node one harness was designed in, or 0.
func (a *Agent) harnessThread(name string) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.harnessThreads[strings.TrimSpace(name)]
}
