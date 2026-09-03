package session

// A SHARE OF THE WALL, AS TESTS (issue #546).
//
// The measured cell: an unattended run with a fifteen-minute wall whose turn read
// and ran tests inline for twelve and a half minutes, handed over with 147
// seconds left, and spent 60 of those opening the task's worktree. The two
// governors that could have moved it count ROUNDS and WRITES, and that turn
// crossed neither. So the clock is a third governor, and these are its laws:
//
//  1. under a steward with a wall, a turn past the share hands over;
//  2. under it, nothing happens and nothing is said;
//  3. a session somebody is sitting in front of is never bounded by it;
//  4. it opens its door ONCE in a turn, and only at a boundary the mark ladder
//     passed over.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A TURN THAT HAS SPENT THE SHARE OF THE WALL HANDS OVER, WITH NO WRITE AND
// BEFORE THE FIRST MARK.
//
// This is the whole feature. Nothing this turn does would move it: it writes
// nothing, so the write seam never counts anything, and it is stopped at its
// first boundary, six rounds short of the first rung of the ladder. What moves it
// is the one fact neither of those can see — that a third of the run's clock has
// gone into this one answer — and what the person reads is this seam's own line.
func TestATurnPastTheShareOfTheWallHandsOver(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	// A MINUTE PAST THE SHARE, ON THE STEWARD'S OWN CLOCK. It is the clock the
	// wall itself is measured on ([Steward.since]), so moving it moves both
	// readings together; and the moment is DERIVED from the session's wall and
	// [turnWallShare] rather than written down, so a run of this file at another
	// share measures the same three moments rather than three durations somebody
	// worked out once by hand ([moveTheStewardsClock]).
	moveTheStewardsClock(t, agent, time.Minute)
	// AND SOMETHING OF THE SESSION'S IS RUNNING, which is what makes the ending a
	// carry-on rather than a done that seals: a goal owner shown a session with
	// nothing landed and nothing left calls the ask finished and no road below it
	// runs at all (unattendeddoor_test.go says the same, for the same reason).
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	if count := admitted(graph); count != 2 {
		t.Fatalf("%d nodes are in the graph, want the one still running and the one the share moved", count)
	}
	said := noticeTexts(collected)
	if !saidSomething(said, turnWallShareNote) {
		t.Fatalf("a turn past the share never said its line; notices were %q", said)
	}
	if timesSaid(said, turnWallShareNote) != 1 {
		t.Fatalf("the share's line was said %d times, want once; notices were %q",
			timesSaid(said, turnWallShareNote), said)
	}
	// AND IT MOVED ON THE CLOCK AND NOT ON THE LADDER. The split note is the
	// mark's own line, and reading it here would mean the turn had reached round
	// ten before anything noticed the wall.
	if saidSomething(said, checkpointSplitNote) {
		t.Fatalf("the turn was moved by the mark ladder rather than by the wall; notices were %q", said)
	}
	lines := closedJournal(t, agent, transcript)
	if !strings.Contains(lines, `"decision":"`+checkpointDecisionRanLong+`"`) {
		t.Fatalf("the seam's own reading reached no line of the journal:\n%s", lines)
	}
	if !strings.Contains(lines, `"decision":"carry on"`) {
		t.Fatalf("the goal owner's answer to this ending reached no line of the journal:\n%s", lines)
	}
	// AND THE ENDING ROW NAMES THIS DOOR. The row is written where the ending is
	// decided and carries the seam that took it (checkpoint.go's seam words), and
	// a bench reading the file afterwards tells a turn the clock moved from one
	// the counters moved by that word alone.
	if !strings.Contains(lines, `"seam":"`+checkpointSeamWall+`"`) {
		t.Fatalf("the ending row does not say it was taken at the wall's share:\n%s", lines)
	}
}

// AND SHORT OF THE SHARE — OR EXACTLY ON IT — NOTHING HAPPENS AND NOTHING IS
// SAID.
//
// The same session, the same script, the same wall, with the turn a minute short
// of the share instead of a minute past it. The seam is silent, and the ladder is
// the governor it always was: this turn is moved at its first mark, by the
// sketch, on the mark's own line.
//
// AND THE BOUNDARY ITSELF IS INLINE, which is the law's own word: a turn EXCEEDS
// the share, so standing exactly on it is not past it.
func TestATurnUnderTheShareOfTheWallIsLeftAlone(t *testing.T) {
	agent, _ := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	moveTheStewardsClock(t, agent, -time.Minute)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	said := noticeTexts(collected)
	if saidSomething(said, turnWallShareNote) {
		t.Fatalf("a turn a minute short of the share was moved by the wall; notices were %q", said)
	}
	if !saidSomething(said, checkpointSplitNote) {
		t.Fatalf("the mark ladder stopped governing the turn the wall left alone; notices were %q", said)
	}

	// AND THE BOUNDARY, ASSERTED ON THE READING ITSELF. It is the one moment the
	// two sides of the comparison are equal, and a clock built off [time.Now] is
	// already microseconds past whatever it was set to by the time a turn reaches
	// a step boundary — so it is named exactly here rather than approached by a
	// turn that can only ever land near it.
	share := shareOfTheWall(t, agent)
	at := holdTheStewardsClock(t, agent)
	if agent.pastTurnWallShare(&checkpointMeter{}, at.Add(-share)) {
		t.Fatal("a turn standing exactly on the share was moved; the law is EXCEEDS, so the boundary stays inline")
	}
	if !agent.pastTurnWallShare(&checkpointMeter{}, at.Add(-share-time.Nanosecond)) {
		t.Fatal("a turn one nanosecond past the share was left inline")
	}
}

// A TURN THAT BEGINS LATE IN THE RUN IS BOUNDED BY WHAT IS LEFT, NOT BY THE
// SHARE.
//
// THE BEFORE, from the record in docs/design/turn-wall-share-doe: on a 900 s wall
// the reef cell's turn began with 310 s left, was allowed the full 300 s share
// because the share was read off the whole wall from the turn's own start, and
// handed over at 894 s — six seconds before the wall, the exact shape #546
// opened with. The same session, the same script, the same wall, with the run's
// own clock wound forward so this turn begins a minute short of [taskAllowance]
// from the wall while its own stretch is seconds: the seam fires at the first
// boundary, and the journal carries its reading and its seam.
func TestATurnThatBeginsLateIsBoundedByWhatIsLeft(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	beginTheTurnLate(t, agent, taskAllowance-time.Minute)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	if count := admitted(graph); count != 2 {
		t.Fatalf("%d nodes are in the graph, want the one still running and the one the wall moved", count)
	}
	said := noticeTexts(collected)
	if timesSaid(said, turnWallShareNote) != 1 {
		t.Fatalf("a turn that began with less than a task needs was not moved once by the wall; notices were %q", said)
	}
	if saidSomething(said, checkpointSplitNote) {
		t.Fatalf("the turn was moved by the mark ladder rather than by the wall; notices were %q", said)
	}
	lines := closedJournal(t, agent, transcript)
	if !strings.Contains(lines, `"decision":"`+checkpointDecisionRanLong+`"`) {
		t.Fatalf("the seam's own reading reached no line of the journal:\n%s", lines)
	}
	if !strings.Contains(lines, `"seam":"`+checkpointSeamWall+`"`) {
		t.Fatalf("the ending row does not say it was taken at the wall's share:\n%s", lines)
	}

	// AND THE ALLOWANCE'S BOUNDARY IS INLINE, asserted on the reading itself as
	// the share's is: a turn beginning with exactly the allowance in front of it
	// still has enough, one nanosecond less does not.
	fresh, _ := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	at := holdTheStewardsClock(t, fresh)
	beginTheTurnLate(t, fresh, taskAllowance)
	if fresh.pastTurnWallShare(&checkpointMeter{}, at) {
		t.Fatal("a turn beginning with exactly a task's allowance left was moved; the boundary stays inline")
	}
	beginTheTurnLate(t, fresh, taskAllowance-time.Nanosecond)
	if !fresh.pastTurnWallShare(&checkpointMeter{}, at) {
		t.Fatal("a turn beginning one nanosecond short of a task's allowance was left inline")
	}
}

// A SESSION SOMEBODY IS SITTING IN FRONT OF IS NEVER BOUNDED BY A SHARE OF
// ANYTHING.
//
// The ceiling here is three milliseconds, so real time crosses the share before
// the first batch comes back — every clock in the building agrees this turn is
// past it. What is absent is the only thing that could act on that: an attended
// session has a [Person] behind it, no goal owner reads its endings, and its turn
// takes exactly as long as it takes.
func TestAPersonsTurnIsNeverBoundedByTheWall(t *testing.T) {
	dir := t.TempDir()
	agent := checkpointAgent(t, splitSketchSteps(), func(config *Config) {
		config.Workspace = dir
		config.Divide = true
		// A CEILING NAMED WITH SOMEBODY THERE. It is the sharpest arm of this
		// law: the number is set, real time is past its share within a
		// millisecond, and what makes the seam absent is the posture and nothing
		// else ([Agent.who] builds a [Person] for an attended session whatever
		// the budget says).
		config.Budget = Budget{Wall: 3 * time.Millisecond}
	})
	if agent.steward() != nil {
		t.Fatal("the fixture put a goal owner behind a session somebody is sitting in front of")
	}
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))
	ran.await(t)

	if said := noticeTexts(collected); saidSomething(said, turnWallShareNote) {
		t.Fatalf("a person's own turn was moved by a share of a wall; notices were %q", said)
	}
}

// THE DOOR OPENS ONCE IN A TURN.
//
// The road below can decline, and it is asked with two model calls behind it, so
// a seam that answered the same question at every boundary after would charge the
// person for the rest of the turn. The claim is made where the reading is
// ([Agent.pastTurnWallShare]), exactly as the write seam claims its own
// ([writeMeter.pastAllowance]), so this is the whole of the proof.
func TestTheShareOfTheWallOpensItsDoorOnceInATurn(t *testing.T) {
	agent, _ := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	share := shareOfTheWall(t, agent)
	moveTheStewardsClock(t, agent, time.Minute)
	meter := &checkpointMeter{}
	if !agent.pastTurnWallShare(meter, time.Now()) {
		t.Fatal("a turn past the share of its wall did not open the door")
	}
	for boundary := 2; boundary <= 5; boundary++ {
		if agent.pastTurnWallShare(meter, time.Now()) {
			t.Fatalf("the share opened its door a second time, at boundary %d", boundary)
		}
	}
	// AND A TURN THAT NEVER REACHES THE SHARE NEVER OPENS IT. This one began a
	// share ago on a clock that is a minute past one, so it is a minute old.
	if agent.pastTurnWallShare(&checkpointMeter{}, time.Now().Add(share)) {
		t.Fatal("a turn a minute old was moved by a share it was nowhere near")
	}
}

// AND A CEILING WITH NO WALL IN IT IS NOT A CLOCK.
//
// `--max-cost` alone states a ceiling in dollars, and dollars say nothing about
// whether what is left of a run has time to set a task up and check it. A share
// taken off one would be a clock invented out of a number that is not one.
func TestAMoneyCeilingWithNoWallNeverMovesATurn(t *testing.T) {
	agent, _ := stewardCheckpointAgent(t, splitSketchSteps(), func(config *Config) {
		config.Budget = Budget{USD: 5}
	})
	moveTheStewardsClock(t, agent, time.Minute)
	if agent.pastTurnWallShare(&checkpointMeter{}, time.Now()) {
		t.Fatal("a session with money left and no wall was moved by a share of a wall it never had")
	}
}

// THE SHARE IS ASKED ONLY AT A BOUNDARY THE MARK LADDER PASSED OVER.
//
// It is the coordinator's condition on this law and it is enforced by the ORDER
// of the two readings rather than by a flag, so this is the predicate that order
// is written in ([wallShareIsAsked]).
func TestTheShareIsAskedOnlyWhereTheLadderHasNothingToSay(t *testing.T) {
	working := []ai.ToolCall{{Function: ai.ToolCallFunction{Name: "bash"}}}
	watching := []ai.ToolCall{{Function: ai.ToolCallFunction{Name: "tasks"}}}
	if !wallShareIsAsked(0, working) {
		t.Fatal("the share was not asked at an ordinary boundary of a working turn")
	}
	if wallShareIsAsked(1, working) {
		t.Fatal("the share was asked at a boundary that had just crossed a mark")
	}
	if wallShareIsAsked(0, watching) {
		t.Fatal("a turn watching work it had already handed out was moved by the clock")
	}
}

// THE LINE AND THE CONSTANT SAY THE SAME FRACTION.
//
// [turnWallShareNote] spells the share as a word because that is what a person
// reads, and [turnWallShare] is the number the code divides by. Nothing but this
// keeps them saying the same thing, and a line telling somebody a third had gone
// while the code took a half would be the harness lying about its own reason.
func TestTheShareNoteAndTheConstantSayTheSameFraction(t *testing.T) {
	if turnWallShare != 3 {
		t.Fatalf("the share is now %d, so %q says the wrong fraction: change the word and this test together",
			turnWallShare, turnWallShareNote)
	}
	if !strings.Contains(turnWallShareNote, "a third of the time") {
		t.Fatalf("the share is a third and its line does not say so: %q", turnWallShareNote)
	}
}

// moveTheStewardsClock puts the session's goal owner THAT FAR PAST ITS OWN
// SHARE — a negative offset is that far short of it — and it is the ONE clock
// this law reads ([Steward.since]), so moving it moves the share and the wall
// together.
//
// THE OFFSET IS RELATIVE TO THE SHARE AND THE SHARE IS READ OFF THE SESSION,
// which is this file's whole answer to one source of truth. A fixture that said
// "25 minutes" would be [turnWallShare] worked out by hand against a wall stated
// somewhere else, and it would go quietly red — not wrong, RED — the moment
// somebody measured a different share. Here only the constant and the word in
// [turnWallShareNote] differ between arms, and
// [TestTheShareNoteAndTheConstantSayTheSameFraction] pins those two to each
// other.
func moveTheStewardsClock(t *testing.T, agent *Agent, past time.Duration) {
	t.Helper()
	on := shareOfTheWall(t, agent) + past
	steward := agent.steward()
	steward.mu.Lock()
	defer steward.mu.Unlock()
	steward.now = func() time.Time { return time.Now().Add(on) }
}

// beginTheTurnLate winds the RUN's clock forward so that a turn beginning now
// has `left` of the wall in front of it, while the turn's own stretch stays
// whatever it really is. It moves the steward's start and not its now, which is
// the one way to move the wall's remainder without moving the turn's stretch:
// the two are read off the same clock ([Steward.Budget], [Steward.since]).
func beginTheTurnLate(t *testing.T, agent *Agent, left time.Duration) {
	t.Helper()
	steward := agentWithGoalOwner(t, agent)
	steward.mu.Lock()
	defer steward.mu.Unlock()
	steward.started = steward.now().Add(left - steward.wall)
}

// holdTheStewardsClock STOPS the session's goal owner at one instant and hands
// that instant back, for the assertions that have to name both sides of the
// comparison exactly. A clock that runs cannot say "exactly on the share": by the
// time anything reads it, it is past.
func holdTheStewardsClock(t *testing.T, agent *Agent) time.Time {
	t.Helper()
	steward := agentWithGoalOwner(t, agent)
	at := time.Now()
	steward.mu.Lock()
	defer steward.mu.Unlock()
	steward.now = func() time.Time { return at }
	return at
}

// shareOfTheWall is what this session's own ceiling allows one turn, read off the
// session so that nothing in this file restates either the wall or the fraction.
func shareOfTheWall(t *testing.T, agent *Agent) time.Duration {
	t.Helper()
	return agentWithGoalOwner(t, agent).Budget().Wall / turnWallShare
}

// agentWithGoalOwner is the one place this file insists there is a steward to
// move a clock on, so the helpers above say it once between them.
func agentWithGoalOwner(t *testing.T, agent *Agent) *Steward {
	t.Helper()
	steward := agent.steward()
	if steward == nil {
		t.Fatal("the fixture built a session with no goal owner behind it")
	}
	return steward
}

// timesSaid counts the notices carrying a line, because "said once" is a
// different law from "said" and this file asserts both.
func timesSaid(said []string, want string) int {
	times := 0
	for _, line := range said {
		if strings.Contains(line, want) {
			times++
		}
	}
	return times
}
