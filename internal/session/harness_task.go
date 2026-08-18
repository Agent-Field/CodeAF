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
// WHAT IT CANNOT DO IS SAVE A SECOND VERSION, and the thread says so rather
// than implying otherwise. Writing a page is the designer's job and the
// designer is entered through build_harness; a revision is therefore a new
// design, with a node and a thread of its own, and this thread's answer to
// "change it to also run the linter" is to say that and to say what to ask for.

import (
	"context"
	"fmt"
	"strings"

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

// The three phases a design node publishes on [TaskNotice.Doing], in the words a
// person would use about them.
//
// "awaiting your look" is spelled exactly as the surface already spells the one
// other moment when work is finished and waiting on somebody (internal/tui3's
// taskUnverifiedWord), because it is the same thing happening: the machine has
// done its part and the next move is a person's.
const (
	harnessPhaseDesigning = "designing"
	harnessPhaseAsking    = "awaiting your look"
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
	processCtx := ctx
	// THE DESIGN'S OWN WINDOW, taken off the node's hour-long leash. Both halves
	// of this job are bounded by it — the writing and the wait for an answer —
	// and it is shorter than a node's deadline because the second half is a card
	// on somebody's screen: half an hour of nobody answering is a person who is
	// not coming back, and a goroutine parked on that question is parked forever
	// (harness_build.go's harnessDesignWindow).
	ctx, cut := context.WithTimeout(ctx, harnessDesignWindow)
	defer cut()
	log := taskLog(listed)
	fmt.Fprintf(log, "task %d · %s\ndesigning with %s\n", node.id, node.title(), model)

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

	// The opening of the thread, written into its transcript rather than said to
	// a model: this is what the room shows somebody who walks in, and it is what
	// the thread reads back when the person asks it something later.
	child.record(textMessage("user", harnessThreadOpening(goal, model)))

	node.doingNow(harnessPhaseDesigning)
	page, cues, err := a.designPage(ctx, goal, model, node.id)
	if err != nil {
		if processCtx.Err() != nil && !node.stoppedByPerson() {
			return a.pauseHarnessNode(node, child)
		}
		fmt.Fprintf(log, "design failed: %v\n", err)
		return a.landHarnessNode(node, child, harnessDesignEnding(ctx, node, "the design failed: "+err.Error()), TaskFailed)
	}
	child.record(textMessage("assistant", harnessPageThread(page)))
	fmt.Fprintf(log, "page written: %s\n", page.Id.Name)

	// THE CARD, on the same lane and answered by the same method it always was
	// (harness_build.go's askHarnessDesign). The node stays running under it,
	// because it is: the work is not over until somebody says what to do with
	// the page, and a node that settled here would take its own room away one
	// moment before the person needed it.
	node.doingNow(harnessPhaseAsking)
	answer, err := a.askHarnessDesign(ctx, node.id, page, model)
	if err != nil {
		if processCtx.Err() != nil && !node.stoppedByPerson() {
			return a.pauseHarnessNode(node, child)
		}
		return a.landHarnessNode(node, child, harnessDesignEnding(ctx, node, "the design ended before it was answered"), TaskFailed)
	}
	if !answer.run {
		// A DECLINE IS NOT A FAILURE. The person was asked and they answered,
		// which is this node's whole job done; a failed row here would send
		// somebody looking for a fault that is their own decision (task.go says
		// the same about a declined proposal).
		return a.landHarnessNode(node, child,
			fmt.Sprintf("harness %q was designed and not saved", page.Id.Name), TaskDone)
	}
	saved, err := a.saveHarness(page, cues)
	if err != nil {
		fmt.Fprintf(log, "save failed: %v\n", err)
		return a.landHarnessNode(node, child,
			fmt.Sprintf("harness %q could not be saved: %v", page.Id.Name, err), TaskFailed)
	}
	fmt.Fprintf(log, "saved: %s v%d\n", saved.Id.Name, saved.Id.Version)
	// THE NODE IS NOW THE HARNESS'S THREAD, and this is where that is written
	// down: the next sentence that names this harness can be pointed at a number
	// (see [Agent.harnessThread]).
	a.rememberHarnessThread(saved.Id.Name, node.id)
	return a.landHarnessNode(node, child, harnessSavedWord(saved), TaskDone)
}

func (a *Agent) pauseHarnessNode(node *TaskNode, child *Agent) TaskState {
	const report = "paused — it resumes"
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
func harnessDesignEnding(ctx context.Context, node *TaskNode, reason string) string {
	if node.stoppedByPerson() {
		return designStoppedWord
	}
	if ctx.Err() != nil {
		return "the design ran out of time before it finished; nothing was saved"
	}
	return reason
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

// harnessThreadOpening is the first message in the design thread: what was
// asked for, who is writing it, and what this thread is and is not for.
//
// IT IS WRITTEN AS THE PERSON'S OWN WORDS because that is what it is — the
// sentence that started the design, forwarded — and because the thread agent
// has to read it as its brief rather than as something it said itself.
func harnessThreadOpening(goal, model string) string {
	var out strings.Builder
	out.WriteString("Design a reusable sub-harness for this:\n\n")
	out.WriteString(goal)
	out.WriteString("\n\nThis is that harness's own thread. The page is being written now")
	if model = strings.TrimSpace(model); model != "" {
		out.WriteString(" by " + model)
	}
	out.WriteString("; when it is ready it appears here and I am shown a card that saves it or drops it.\n\n")
	out.WriteString("Your job in this thread is to answer questions about this harness — why it is shaped the way it is, what its steps do, whether it fits some other work — using the page below as your source. ")
	out.WriteString("You cannot write or save a version of it from here: designing a page is a job of its own, and a revision is a new design, asked for the same way this one was. When somebody wants the harness changed, say that plainly and say what to ask for.")
	return out.String()
}

// harnessPageThread is the page as the thread records it: the card a person
// reads, and the page itself so the thread agent is reasoning about the real
// thing rather than about a rendering of it.
//
// The two are one message because they are one artifact. A surface renders the
// card (internal/subharness's card.go is the renderer every surface shares) and
// the fenced page under it is what the model reads when somebody asks what step
// three actually does.
func harnessPageThread(page subharness.Harness) string {
	var out strings.Builder
	out.WriteString("The page is written.\n\n")
	out.WriteString(subharness.Card(page))
	if encoded, err := subharness.Encode(page); err == nil {
		out.WriteString("\n\nThe page itself:\n\n```yaml\n")
		out.Write(encoded)
		out.WriteString("\n```")
	}
	out.WriteString("\n\nNothing is saved yet — the card is up, and it is saved only if it is approved.")
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
