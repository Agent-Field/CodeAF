package session

import (
	"strings"
	"testing"
	"time"
)

// THREE TIERS AND ONE QUESTION (docs/design/task-states/DESIGN.md).
//
// Every row a person reads answers one thing before it says anything else: do I
// need to do anything? These tests hold the engine to the three answers, to the
// one word each state wears, and to the closed set of questions a your-call row
// may be asking — because the defect they were written for is four surfaces each
// working the answer out for itself and disagreeing.

// TestProjectTaskTiers walks every tier and every question in the table. Each
// case is a situation the runtime produces, and the assertion is the word and
// the sentence a person is owed for it.
func TestProjectTaskTiers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts TaskFacts
		tier  TaskTier
		row   string
		kind  TaskAskKind
		yes   string
		no    string
		owner TaskAskOwner
	}{{
		name:  "queued is moving and needs nobody",
		facts: TaskFacts{State: TaskQueued},
		tier:  TaskTierMoving,
		row:   "queued",
	}, {
		name:  "work waiting on other work names it",
		facts: TaskFacts{State: TaskQueued, Waits: []string{"Collect sources"}},
		tier:  TaskTierMoving,
		row:   "waiting on Collect sources",
	}, {
		name:  "a paced run is waiting and says why",
		facts: TaskFacts{State: TaskRunning, Hold: "rate limited"},
		tier:  TaskTierMoving,
		row:   "waiting · rate limited",
	}, {
		name:  "a running node is working",
		facts: TaskFacts{State: TaskRunning, Phase: "reading the parser"},
		tier:  TaskTierMoving,
		row:   "working · reading the parser",
	}, {
		name:  "a check reading the work is finishing",
		facts: TaskFacts{State: TaskRunning, Life: TaskPhaseChecking, Gap: "adding amp-labs"},
		tier:  TaskTierMoving,
		row:   "finishing · adding amp-labs",
	}, {
		name:  "a proposal with a clock running starts itself",
		facts: TaskFacts{Consent: true, Countdown: "9s"},
		tier:  TaskTierMoving,
		row:   "auto-starts in 9s",
	}, {
		name:  "a proposal with no clock is the person's word",
		facts: TaskFacts{Consent: true},
		tier:  TaskTierYourCall,
		row:   "your call · starts on your word",
		kind:  TaskAskStart,
		yes:   "start",
		no:    "don't",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "a design waiting to be approved",
		facts: TaskFacts{State: TaskRunning, Kind: TaskKindHarness, Phase: HarnessPhaseAsking},
		tier:  TaskTierYourCall,
		row:   "your call · design ready to approve",
		kind:  TaskAskApprove,
		yes:   "approve",
		no:    "decline",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "a run standing at its fuel gate names the amount",
		facts: TaskFacts{State: TaskRunning, Paused: true, Cap: "$5.00"},
		tier:  TaskTierYourCall,
		row:   "your call · paused at the $5.00 cap",
		kind:  TaskAskCap,
		yes:   "raise the cap",
		no:    "stop it",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "a gate with no figure still asks a whole sentence",
		facts: TaskFacts{State: TaskRunning, Paused: true},
		tier:  TaskTierYourCall,
		row:   "your call · paused at the cap",
		kind:  TaskAskCap,
		yes:   "raise the cap",
		no:    "stop it",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "nobody could check it",
		facts: TaskFacts{State: TaskUnverified},
		tier:  TaskTierYourCall,
		row:   "your call · nobody could check it",
		kind:  TaskAskCheck,
		yes:   "accept",
		no:    "not right",
		owner: TaskAskOwnerPerson,
	}, {
		name: "a branch that would not fasten names the files",
		facts: TaskFacts{
			State: TaskUnverified, Merge: mergeConflicted, Branch: "task/parser",
			Conflicts: []string{"parser.go", "parser_test.go"},
		},
		tier:  TaskTierYourCall,
		row:   "your call · conflicts with your branch: parser.go, parser_test.go",
		kind:  TaskAskConflict,
		yes:   "resolve it",
		no:    "drop it",
		owner: TaskAskOwnerPerson,
	}, {
		// THE OTHER ROAD TO THE SAME QUESTION. The branch would have fastened and
		// the check passed; what moved was the ground under it, and a row reading
		// `nobody could check it` here was false in both halves.
		name: "a ground that moved says so and names the files",
		facts: TaskFacts{
			State: TaskUnverified, Merge: mergeAborted, Branch: "task/parser",
			Shifted: true, Conflicts: []string{"parser.go", "lex.go"},
		},
		tier:  TaskTierYourCall,
		row:   "your call · your branch changed the same files while it worked: parser.go, lex.go",
		kind:  TaskAskConflict,
		yes:   "resolve it",
		no:    "drop it",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "a shift with nothing named stops after the sentence",
		facts: TaskFacts{State: TaskUnverified, Merge: mergeAborted, Branch: "task/parser", Shifted: true},
		tier:  TaskTierYourCall,
		row:   "your call · your branch changed the same files while it worked",
		kind:  TaskAskConflict,
		yes:   "resolve it",
		no:    "drop it",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "a conflict git would not name stops after the branch",
		facts: TaskFacts{State: TaskUnverified, Merge: mergeConflicted, Branch: "task/parser"},
		tier:  TaskTierYourCall,
		row:   "your call · conflicts with your branch",
		kind:  TaskAskConflict,
		yes:   "resolve it",
		no:    "drop it",
		owner: TaskAskOwnerPerson,
	}, {
		name: "a held landing offers the answer anyway",
		facts: TaskFacts{
			State: TaskFailed, Ending: TaskEndingRefused, Held: true,
			Report: incompleteLead + "the revenue figure is missing",
		},
		tier:  TaskTierYourCall,
		row:   "your call · the check did not pass it: the revenue figure is missing",
		kind:  TaskAskHeld,
		yes:   "accept anyway",
		no:    "not right",
		owner: TaskAskOwnerPerson,
	}, {
		name:  "done is over",
		facts: TaskFacts{State: TaskDone, Merge: mergeMerged, Branch: "task/parser"},
		tier:  TaskTierOver,
		row:   "done",
	}, {
		name:  "a person's stop is stopped and never a failure",
		facts: TaskFacts{State: TaskFailed, Stopped: true, Ending: TaskEndingStopped},
		tier:  TaskTierOver,
		row:   "stopped",
	}, {
		name:  "a dropped connection is incomplete with its reason",
		facts: TaskFacts{State: TaskFailed, Ending: TaskEndingWire},
		tier:  TaskTierOver,
		row:   "incomplete · lost the connection",
	}, {
		name:  "a check that named gaps is incomplete with what it found",
		facts: TaskFacts{State: TaskFailed, Ending: TaskEndingRefused, Report: incompleteLead + "no tests for the error path"},
		tier:  TaskTierOver,
		row:   "incomplete · the check found gaps: no tests for the error path",
	}, {
		name:  "an unclassified error is a fault and quotes its first line",
		facts: TaskFacts{State: TaskFailed, Ending: TaskEndingError, Report: "the worktree could not be made"},
		tier:  TaskTierOver,
		row:   "incomplete · a fault: the worktree could not be made",
	}, {
		name:  "the decision can be the model's while it holds it",
		facts: TaskFacts{State: TaskUnverified, Decider: TaskAskOwnerModel},
		tier:  TaskTierYourCall,
		row:   "your call · nobody could check it",
		kind:  TaskAskCheck,
		yes:   "accept",
		no:    "not right",
		owner: TaskAskOwnerModel,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			status := ProjectTask(tc.facts)
			if status.Tier != tc.tier {
				t.Fatalf("tier is %q, want %q", status.Tier, tc.tier)
			}
			if row := status.RowWord(); row != tc.row {
				t.Fatalf("the row reads %q, want %q", row, tc.row)
			}
			if status.Ask.Kind != tc.kind {
				t.Fatalf("the question is %q, want %q", status.Ask.Kind, tc.kind)
			}
			if tc.kind == "" {
				if status.Ask != (TaskAsk{}) {
					t.Fatalf("a row that is not your call carries a question: %+v", status.Ask)
				}
				return
			}
			if status.Ask.Yes != tc.yes || status.Ask.No != tc.no {
				t.Fatalf("the answers are %q/%q, want %q/%q", status.Ask.Yes, status.Ask.No, tc.yes, tc.no)
			}
			if status.Ask.Owner != tc.owner {
				t.Fatalf("the decision is held by %q, want %q", status.Ask.Owner, tc.owner)
			}
			if status.Tier == TaskTierYourCall && status.Ask.Reason == "" {
				t.Fatal("a your-call row is asking a question with no reason on it")
			}
		})
	}
}

// EVERY ASK KIND IS REACHABLE AND NO TWO OF THEM SHARE A SENTENCE. A closed set
// with a duplicate in it is a card whose reason cannot be told from another's.
func TestEveryAskKindHasItsOwnSentence(t *testing.T) {
	reached := map[TaskAskKind]string{}
	for _, facts := range []TaskFacts{
		{Consent: true},
		{State: TaskRunning, Kind: TaskKindHarness, Phase: HarnessPhaseAsking},
		{State: TaskRunning, Paused: true, Cap: "$5.00"},
		{State: TaskUnverified, Merge: mergeConflicted, Branch: "task/x", Conflicts: []string{"a.go"}},
		{State: TaskUnverified},
		{State: TaskFailed, Ending: TaskEndingRefused, Held: true},
	} {
		ask := ProjectTask(facts).Ask
		if ask.Kind == "" {
			t.Fatalf("no question was reached for %+v", facts)
		}
		if had, seen := reached[ask.Kind]; seen {
			t.Fatalf("%q was reached twice (%q then %q)", ask.Kind, had, ask.Reason)
		}
		reached[ask.Kind] = ask.Reason
	}
	for _, kind := range []TaskAskKind{
		TaskAskStart, TaskAskApprove, TaskAskCap, TaskAskConflict, TaskAskCheck, TaskAskHeld,
	} {
		if reached[kind] == "" {
			t.Fatalf("no facts reach the %q question", kind)
		}
	}
}

// THE INCOMPLETE REASONS ARE ONE TABLE, SPELLED ONCE. A surface that wrote its
// own copy is what made a dropped connection read as a failure on one page and
// as a stop on another.
func TestTaskReasonOf(t *testing.T) {
	for _, tc := range []struct {
		ending TaskEnding
		report string
		want   string
	}{
		{ending: TaskEndingWire, want: "lost the connection"},
		{ending: TaskEndingUpstream, want: "the model provider refused it"},
		{ending: TaskEndingCircling, want: "went in circles"},
		{ending: TaskEndingBlocked, want: "was blocked by another task"},
		{ending: TaskEndingSteps, want: "ran out of steps"},
		{ending: TaskEndingNotes, want: "would not write its notes down"},
		{ending: TaskEndingStale, want: "its brief went stale"},
		{ending: TaskEndingRefused, want: "would not take a step it was asked to"},
		{ending: TaskEndingRefused, report: incompleteLead + "the amp-labs entry", want: "the check found gaps: the amp-labs entry"},
		{ending: TaskEndingError, report: "no such directory", want: "a fault: no such directory"},
		{ending: TaskEndingError, want: "a fault"},
		{ending: "", report: "something went sideways", want: "a fault: something went sideways"},
		{ending: TaskEndingStopped, want: ""},
	} {
		if got := TaskReasonOf(tc.ending, tc.report); got != tc.want {
			t.Fatalf("%q with report %q reads %q, want %q", tc.ending, tc.report, got, tc.want)
		}
	}
}

// NO MACHINERY VOCABULARY AND NO DELETED WORD survives in anything a person
// reads off a row. The words below each belonged to one surface's private table.
func TestNoRowEverSpellsADeletedWord(t *testing.T) {
	banned := []string{
		"awaiting review", "unverified", "needs your look", "failed",
		"delivery needs attention", "auditor", "verdict", "verified", "refuted",
	}
	for _, facts := range []TaskFacts{
		{State: TaskQueued},
		{State: TaskRunning},
		{State: TaskUnverified},
		{State: TaskUnverified, Merge: mergeConflicted, Branch: "task/x"},
		{State: TaskFailed, Ending: TaskEndingWire},
		{State: TaskFailed, Ending: TaskEndingRefused},
		{State: TaskFailed, Stopped: true, Ending: TaskEndingStopped},
		{State: TaskDone},
		{Consent: true},
	} {
		row := ProjectTask(facts).RowWord()
		for _, word := range banned {
			if strings.Contains(strings.ToLower(row), word) {
				t.Fatalf("the row %q for %+v still spells %q", row, facts, word)
			}
		}
	}
}

// ── the auto-settle floor ───────────────────────────────────────────────────

// floorGraph is one landed node nobody could check, in a graph an agent owns.
func floorGraph(merge string) (*TaskGraph, *TaskNode) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{1}}
	node := &TaskNode{
		graph: graph, id: 1, state: TaskUnverified, merge: merge,
		spec: taskSpec{title: "Port the parser"},
	}
	graph.nodes[1] = node
	return graph, node
}

// UNDER AUTO THE MODEL IS ASKED FIRST, and the node says so — the card and the
// note are two halves of one landing and may not disagree about whose move it is.
func TestUnderAutoTheLandingIsHandedToTheModel(t *testing.T) {
	graph, node := floorGraph("")
	// AskConsent false is this build's "nobody is watching", which is the posture
	// [Agent.settlePolicy] answers auto for.
	agent := &Agent{config: Config{tasker: graph}}
	agent.handToModelOnAuto(node)
	if node.decider != TaskAskOwnerModel {
		t.Fatalf("the decision is held by %q, want the model", node.decider)
	}
	if owner := ProjectTask(node.notice().StatusFacts()).Ask.Owner; owner != TaskAskOwnerModel {
		t.Fatalf("the notice says %q holds the decision", owner)
	}
}

// A CONFLICT IS NEVER HANDED TO THE MODEL. It cannot merge by decree, whatever
// the settle policy says about work nobody could check.
func TestAConflictUnderAutoStaysWithThePerson(t *testing.T) {
	graph, node := floorGraph(mergeConflicted)
	agent := &Agent{config: Config{tasker: graph}}
	agent.handToModelOnAuto(node)
	if node.decider == TaskAskOwnerModel {
		t.Fatal("a conflicted landing was handed to the model")
	}
	status := ProjectTask(node.notice().StatusFacts())
	if status.Ask.Kind != TaskAskConflict || status.Ask.Owner != TaskAskOwnerPerson {
		t.Fatalf("the conflict asks %q of %q", status.Ask.Kind, status.Ask.Owner)
	}
}

// AND A GROUND THAT MOVED IS THE SAME REFUSAL. The merge word is `kept` on that
// road — the branch WOULD have fastened — so nothing about the policy can be
// read off it, and the mark on the node is what holds the question here.
func TestAGroundShiftUnderAutoStaysWithThePerson(t *testing.T) {
	graph, node := floorGraph(mergeAborted)
	node.shiftedBy([]string{"parser.go"})
	agent := &Agent{config: Config{tasker: graph}}
	agent.handToModelOnAuto(node)
	if node.decider == TaskAskOwnerModel {
		t.Fatal("a landing whose ground moved was handed to the model")
	}
	status := ProjectTask(node.notice().StatusFacts())
	if status.Ask.Kind != TaskAskConflict || status.Ask.Owner != TaskAskOwnerPerson {
		t.Fatalf("the shift asks %q of %q", status.Ask.Kind, status.Ask.Owner)
	}
	if !strings.HasPrefix(status.Ask.Reason, taskAskShiftReason) {
		t.Fatalf("the shift reads %q", status.Ask.Reason)
	}
}

// ONE QUESTION, TWO TRUE SENTENCES. The two roads close with the same answers
// and must never be told apart by reading their prose — nor say the same thing,
// which would leave a person unable to tell what actually happened.
func TestTheTwoRoadsToTheConflictQuestionSayDifferentThings(t *testing.T) {
	files := []string{"parser.go"}
	conflicted := ProjectTask(TaskFacts{State: TaskUnverified, Merge: mergeConflicted, Conflicts: files}).Ask
	shifted := ProjectTask(TaskFacts{State: TaskUnverified, Merge: mergeAborted, Shifted: true, Conflicts: files}).Ask
	if conflicted.Kind != shifted.Kind {
		t.Fatalf("the two roads ask %q and %q", conflicted.Kind, shifted.Kind)
	}
	if conflicted.Yes != shifted.Yes || conflicted.No != shifted.No {
		t.Fatalf("the answers differ: %q/%q against %q/%q",
			conflicted.Yes, conflicted.No, shifted.Yes, shifted.No)
	}
	if conflicted.Reason == shifted.Reason {
		t.Fatalf("both roads read %q, so nothing says which happened", shifted.Reason)
	}
}

// UNDER ASK NOTHING MOVES. A session somebody is watching keeps the decision
// where they left it.
func TestUnderAskTheDecisionIsNeverHandedOver(t *testing.T) {
	graph, node := floorGraph("")
	agent := &Agent{config: Config{tasker: graph, AskConsent: true}}
	agent.handToModelOnAuto(node)
	if node.decider == TaskAskOwnerModel {
		t.Fatal("a watched session handed its decision to the model")
	}
}

// A TASK NEVER STAYS UNOWNED PAST THE END OF A TURN. The model had its window;
// what it did not answer comes back to the person, and the surface is told.
func TestTheFloorHandsAnUnsettledDecisionBack(t *testing.T) {
	graph, node := floorGraph("")
	agent := &Agent{config: Config{tasker: graph}}
	agent.handToModelOnAuto(node)

	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	drainTaskLane(lane)

	agent.handBackUnsettled()
	if node.decider != TaskAskOwnerPerson {
		t.Fatalf("the decision is still held by %q", node.decider)
	}
	notice, told := nextTaskNotice(lane)
	if !told {
		t.Fatal("nothing was published about the decision coming back")
	}
	if notice.ID != 1 || notice.Decider != TaskAskOwnerPerson {
		t.Fatalf("the update says task %d is held by %q", notice.ID, notice.Decider)
	}
	if owner := ProjectTask(notice.StatusFacts()).Ask.Owner; owner != TaskAskOwnerPerson {
		t.Fatalf("the card would still draw %q as the holder", owner)
	}
}

// A NODE THE MODEL ACTUALLY SETTLED IS NOT NEWS. The resolution published its
// own landing; a second update saying the question is back would put a card up
// over work that has finished being decided.
func TestTheFloorSaysNothingAboutASettledNode(t *testing.T) {
	graph, node := floorGraph("")
	agent := &Agent{config: Config{tasker: graph}}
	agent.handToModelOnAuto(node)

	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	drainTaskLane(lane)

	graph.mu.Lock()
	node.state = TaskDone
	graph.mu.Unlock()

	agent.handBackUnsettled()
	if _, told := nextTaskNotice(lane); told {
		t.Fatal("the floor published an update about a node that was decided")
	}
}

// drainTaskLane empties whatever a freshly opened lane was handed (the roster
// replay), so that what is left afterwards is what the floor published. The
// replay rides a goroutine, so this waits out a quiet moment rather than polling
// once.
func drainTaskLane(lane <-chan Event) {
	for {
		select {
		case <-lane:
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}

// nextTaskNotice is the next update on a lane, or false when none arrives. The
// wait is short and one-sided: a lane is fed by a goroutine of its own, so a
// bare poll would race the publication rather than test it.
func nextTaskNotice(lane <-chan Event) (TaskNotice, bool) {
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-lane:
			if event.Kind == EventTaskUpdate && event.Task != nil {
				return *event.Task, true
			}
		case <-deadline:
			return TaskNotice{}, false
		}
	}
}

// ── the note the model reads ────────────────────────────────────────────────

// THE NOTE WEARS THE SAME WORD AS THE CARD. The model is about to say what
// happened in its own sentence, and a note that spelled the landing differently
// from the card the person is looking at is two accounts of one landing.
func TestTheLandingNoteWearsTheTierWord(t *testing.T) {
	for _, tc := range []struct {
		name   string
		notice TaskNotice
		head   string
		gone   []string
	}{{
		name:   "finished work is done",
		notice: TaskNotice{ID: 7, Title: "Port the parser", State: TaskDone, Merge: mergeMerged, Branch: "task/parser"},
		head:   "task 7 done: Port the parser",
		gone:   []string{"finished:"},
	}, {
		name:   "a person's stop is stopped",
		notice: TaskNotice{ID: 7, Title: "Port the parser", State: TaskFailed, Stopped: true, Ending: TaskEndingStopped},
		head:   "task 7 stopped: Port the parser",
	}, {
		name:   "a dropped connection is incomplete with its reason",
		notice: TaskNotice{ID: 7, Title: "Port the parser", State: TaskFailed, Ending: TaskEndingWire},
		head:   "task 7 incomplete: Port the parser · lost the connection",
		gone:   []string{"task 7 lost the connection:", "failed"},
	}, {
		name:   "a fault is incomplete and quotes what broke",
		notice: TaskNotice{ID: 7, Title: "Port the parser", State: TaskFailed, Ending: TaskEndingError, Report: "the worktree could not be made"},
		head:   "task 7 incomplete: Port the parser · a fault: the worktree could not be made",
		gone:   []string{"task 7 failed"},
	}, {
		name: "work nobody could check is the person's call",
		notice: TaskNotice{
			ID: 7, Title: "Port the parser", State: TaskUnverified,
			Report: yourCallLead(TaskFacts{}) + "the checker answered neither way",
		},
		head: "task 7 your call: Port the parser · nobody could check it",
		gone: []string{"needs your look"},
	}, {
		name: "a conflict names the files and is not the model's to accept",
		notice: TaskNotice{
			ID: 7, Title: "Port the parser", State: TaskUnverified,
			Merge: mergeConflicted, Branch: "task/parser",
			Conflicts: []string{"parser.go", "parser_test.go"},
		},
		head: "task 7 your call: Port the parser · conflicts with your branch: parser.go, parser_test.go",
		gone: []string{"resolve " + TaskResolveVerbs()},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			note := taskNote(tc.notice, "", TaskSettleAsk, landingAddress{person: true})
			if !strings.HasPrefix(note, tc.head) {
				t.Fatalf("the note does not open %q:\n%s", tc.head, note)
			}
			for _, never := range tc.gone {
				if strings.Contains(note, never) {
					t.Fatalf("the note still says %q:\n%s", never, note)
				}
			}
		})
	}
}

// A CONFLICT IS NOT THE MODEL'S TO ACCEPT, and its note says so in place of the
// settle clause — that clause offers `accept`, which is the one thing this
// landing is not asking for.
func TestAConflictedNoteRefusesTheModelTheMerge(t *testing.T) {
	notice := TaskNotice{
		ID: 7, Title: "Port the parser", State: TaskUnverified,
		Merge: mergeConflicted, Branch: "task/parser", Conflicts: []string{"parser.go"},
	}
	for _, settle := range []TaskSettle{TaskSettleAsk, TaskSettleAuto} {
		note := taskNote(notice, "", settle, landingAddress{person: true})
		if !strings.Contains(note, "not yours to accept") {
			t.Fatalf("under %q the note does not refuse the merge:\n%s", settle, note)
		}
		if strings.Contains(note, "settle it yourself") {
			t.Fatalf("under %q the note tells the model to settle a conflict:\n%s", settle, note)
		}
	}
}

// AND THE NOTE FOR A GROUND THAT MOVED SAYS THE SAME THING ABOUT THE MERGE AND
// A DIFFERENT THING ABOUT THE BRANCH. That landing keeps the merge word `kept`,
// so a note reading the merge word alone offered the model `accept` while the
// card beside it offered `resolve it` — one landing, two accounts.
func TestAShiftedNoteRefusesTheModelTheMergeAndSaysWhatMoved(t *testing.T) {
	notice := TaskNotice{
		ID: 7, Title: "Port the parser", State: TaskUnverified,
		Merge: mergeAborted, Branch: "task/parser", Shifted: true, Conflicts: []string{"parser.go"},
	}
	for _, settle := range []TaskSettle{TaskSettleAsk, TaskSettleAuto} {
		note := taskNote(notice, "", settle, landingAddress{person: true})
		head := "task 7 your call: Port the parser · " + taskAskShiftReason + ": parser.go"
		if !strings.HasPrefix(note, head) {
			t.Fatalf("under %q the note does not open %q:\n%s", settle, head, note)
		}
		if !strings.Contains(note, "not yours to accept") {
			t.Fatalf("under %q the note does not refuse the merge:\n%s", settle, note)
		}
		if strings.Contains(note, "resolve "+TaskResolveVerbs()) {
			t.Fatalf("under %q the note offers the model the settle verbs:\n%s", settle, note)
		}
		if strings.Contains(note, "nobody could check it") {
			t.Fatalf("under %q the note still says nobody could check it:\n%s", settle, note)
		}
		if strings.Contains(note, "its branch conflicts with the person's") {
			t.Fatalf("under %q the note claims a conflict that did not happen:\n%s", settle, note)
		}
	}
}

// TAKING IT BACK IS THE SAME MOVE THE FLOOR MAKES, pressed early. It resolves
// nothing: the node stays where it is and the chips come back.
func TestTakeBackDecisionReturnsTheQuestionWithoutSettlingIt(t *testing.T) {
	graph, node := floorGraph("")
	agent := &Agent{config: Config{tasker: graph}}
	agent.handToModelOnAuto(node)
	if err := agent.TakeBackDecision(1); err != nil {
		t.Fatalf("taking the decision back: %v", err)
	}
	if node.decider != TaskAskOwnerPerson {
		t.Fatalf("the decision is still held by %q", node.decider)
	}
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("taking it back settled the node as %q", state)
	}
	if err := agent.TakeBackDecision(2); err == nil {
		t.Fatal("a task that is not in this session was taken back")
	}
}

// A TURN TAKES BACK ONLY WHAT IT WAS ASKED. Every agent in a family shares one
// graph, so a floor that swept the whole of it would have a sub-task's worker
// taking a decision out of the conversation's hands mid-thought — which is the
// same defect the floor exists to fix, pointed the other way.
func TestTheFloorOnlyTakesBackWhatThisTurnWasAsked(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{1, 2}}
	root := &TaskNode{
		graph: graph, id: 1, state: TaskRunning, decider: TaskAskOwnerModel,
		spec: taskSpec{title: "the whole job"},
	}
	piece := &TaskNode{
		graph: graph, id: 2, parent: 1, state: TaskUnverified, decider: TaskAskOwnerModel,
		spec: taskSpec{title: "a piece of it"},
	}
	graph.nodes[1], graph.nodes[2] = root, piece

	worker := &Agent{config: Config{tasker: graph, taskID: 1}}
	worker.handBackUnsettled()
	if piece.decider != TaskAskOwnerPerson {
		t.Fatalf("the worker did not take back its own piece's question (%q)", piece.decider)
	}
	if root.decider != TaskAskOwnerModel {
		t.Fatal("a worker's turn ending took back the conversation's own question")
	}

	graph.mu.Lock()
	root.state = TaskUnverified
	graph.mu.Unlock()
	conversation := &Agent{config: Config{tasker: graph}}
	conversation.handBackUnsettled()
	if root.decider != TaskAskOwnerPerson {
		t.Fatalf("the conversation did not take back its own question (%q)", root.decider)
	}
}
