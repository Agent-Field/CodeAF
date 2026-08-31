package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// taskPhasesUntilLanded collects every phase move one node makes, up to and
// including the update that lands it.
//
// It reads the SAME stream the updates come on, because that is the claim: the
// phase and the state travel one lane in one order, so a surface holding one
// subscription sees a check start and the landing that follows it without having
// to reconcile two clocks.
func taskPhasesUntilLanded(t *testing.T, updates <-chan Event, id uint64) []TaskPhaseNotice {
	t.Helper()
	var seen []TaskPhaseNotice
	deadline := time.After(30 * time.Second)
	for {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatal("the task lane closed before the node landed")
			}
			if event.Kind == EventTaskPhase && event.TaskPhase != nil && event.TaskPhase.ID == id {
				seen = append(seen, *event.TaskPhase)
				continue
			}
			if event.Kind == EventTaskUpdate && event.Task != nil && event.Task.ID == id && event.Task.State.settled() {
				return seen
			}
		case <-deadline:
			t.Fatalf("task %d never landed; %d phase moves seen", id, len(seen))
			return nil
		}
	}
}

// taskPhaseWords is the phase moves as a sequence of words, for comparing an order.
func taskPhaseWords(moves []TaskPhaseNotice) []string {
	words := make([]string, 0, len(moves))
	for _, move := range moves {
		words = append(words, move.Phase)
	}
	return words
}

// THE MINUTES AFTER THE WORKER STOPS TALKING ARE ON THE WIRE.
//
// This is the hole #76 §5 measured: the worker writes its last line, and then
// the check reads the tree and a repair round rewrites it and a second check
// reads it again — four minutes in the evidence — with the state saying
// "running" the whole way and no event carrying anything else. A person watching
// that concludes the work hung, because from the outside it is indistinguishable
// from work that did.
//
// So the node says which of its three lives it is in, in order, with the round
// numbers on the repair and the check's own finding under it.
func TestACheckedThenRepairedNodeSaysWhichLifeItIsIn(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go and a test for it, checked with `go test ./...`"),
			finalText("handed off"),
		},
		child: nodeLane(8, func(repairing, wrote bool) *ai.Response {
			switch {
			case repairing && !wrote:
				return writeResponse("call-test", "greet_test.go",
					"package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() != \"hi\" {\n\t\tt.Fatal(\"no greeting\")\n\t}\n}\n")
			case repairing:
				return pricedResponse("Added greet_test.go, which covers the greeting.", 0.02)
			case !wrote:
				return writeResponse("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n")
			default:
				return pricedResponse("Wrote greet.go with the greeting.", 0.01)
			}
		}),
		audit: []step{
			bashCall("call-verify", "go test ./..."),
			verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok",
				"REFUTED — go test ./... reports no test files: the acceptance asks for a test and there is none"),
			bashCall("call-verify-again", "go test ./..."),
			verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok · the greeting test runs",
				"REFUTED — go test ./... still reports no test files"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 1
	})
	updates := agent.TaskUpdates()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	moves := taskPhasesUntilLanded(t, updates, 1)

	// THE WHOLE ORDER, AND NOTHING INVENTED. A check, back to working, a repair
	// round, back to working, the check that accepted it, back to working — the
	// node's three lives in the order it actually lived them.
	want := []string{
		TaskPhaseChecking, TaskPhaseWorking,
		TaskPhaseRepairing, TaskPhaseWorking,
		TaskPhaseChecking, TaskPhaseWorking,
	}
	if got := taskPhaseWords(moves); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the node's phases were %v, want %v", got, want)
	}

	// THE ROUND NUMBERS ARE ON THE REPAIR AND NOWHERE ELSE, which is the
	// emptiness law on this wire: a check has no rounds, so it carries no
	// numbers rather than carrying zeros a surface has to know to hide.
	repairs := 0
	for _, move := range moves {
		if move.Phase != TaskPhaseRepairing {
			if move.Round != 0 || move.Rounds != 0 || move.Text != "" {
				t.Fatalf("a %s move carried round %d of %d and %q: only a repair round has those",
					move.Phase, move.Round, move.Rounds, move.Text)
			}
			continue
		}
		repairs++
		if move.Round != 1 || move.Rounds != 1 {
			t.Fatalf("the repair said round %d of %d, want 1 of 1", move.Round, move.Rounds)
		}
		// AND THE FINDING RIDES IT, in the words a person reads: what happened,
		// then the checker's own sentence about what is missing.
		if !strings.HasPrefix(move.Text, "not done — ") {
			t.Fatalf("the finding reads %q, want it to open with what happened", move.Text)
		}
		if !strings.Contains(move.Text, "no test files") {
			t.Fatalf("the finding reads %q, want the gap the checker named", move.Text)
		}
		assertPlainWords(t, "the finding line", move.Text)
	}
	if repairs != 1 {
		t.Fatalf("%d repair rounds were announced, want the one that ran", repairs)
	}
}

// WITH THE LOOP OFF THERE IS STILL A CHECK, and the check is still minutes a
// person watches. A node that is never repaired says "checking" and then goes
// back to working, and says nothing about rounds it will not run.
func TestANodeWithNoRepairRoundsStillSaysItIsChecking(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: nodeLane(8, func(repairing, wrote bool) *ai.Response {
			if !wrote {
				return writeResponse("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n")
			}
			return pricedResponse("Wrote greet.go with the greeting.", 0.01)
		}),
		audit: []step{
			verdict("VERIFIED — greet.go has the greeting the brief asked for"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	updates := agent.TaskUpdates()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	moves := taskPhasesUntilLanded(t, updates, 1)
	want := []string{TaskPhaseChecking, TaskPhaseWorking}
	if got := taskPhaseWords(moves); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the node's phases were %v, want %v", got, want)
	}
	for _, move := range moves {
		if move.Round != 0 || move.Rounds != 0 || move.Text != "" {
			t.Fatalf("a %s move with the loop off carried round %d of %d and %q",
				move.Phase, move.Round, move.Rounds, move.Text)
		}
	}
}

// ── THE TWO STAGES THAT USED TO RUN IN THE DARK ─────────────────────────────
//
// Two model runs stood between a person and their work with nothing drawn for
// either. The reading that decides whether work is handed out in parts is a full
// call to the tier that thinks — measured at thirteen seconds — and on the road
// where the harness submits a drawing for the worker it happens on a task card
// that has only just appeared. The handover's two calls are longer still:
// fifteen to thirty seconds between the last thing a model said and the task
// showing up on the rail.
//
// Each of them now says what it is, on the lane that fits where it happens: the
// division is a life of a node that already has a row, and the handover happens
// before there is a node at all, so it rides the turn's own clock.

// phaseMovesAbout collects the next `want` phase moves about one node.
func phaseMovesAbout(t *testing.T, updates <-chan Event, id uint64, want int) []TaskPhaseNotice {
	t.Helper()
	var seen []TaskPhaseNotice
	deadline := time.After(30 * time.Second)
	for len(seen) < want {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatalf("the task lane closed after %d of %d phase moves", len(seen), want)
			}
			if event.Kind == EventTaskPhase && event.TaskPhase != nil && event.TaskPhase.ID == id {
				seen = append(seen, *event.TaskPhase)
			}
		case <-deadline:
			t.Fatalf("only %d of %d phase moves arrived about node %d", len(seen), want, id)
			return nil
		}
	}
	return seen
}

// THE READING THAT DECIDES WHETHER THE WORK SPLITS IS DRAWN, AND THEN CLEARS.
//
// It is announced from the CONVERSATION and not from whoever noticed it, which
// is the half of this that is easy to get wrong: a division is read by the node's
// own worker, and a worker's only lanes are its room's — so a move sent from
// there would reach somebody sitting inside the task and nobody at all on the
// card, the rail or the home row ([TaskNode.phaseTeller]).
func TestTheReadingThatSizesTheWorkIsDrawnAndThenClears(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"parts":[` +
		`{"title":"the adapters","summary":"s","brief":"b","acceptance":"a"},` +
		`{"title":"the tests","summary":"s","brief":"b","acceptance":"a"}]}`}
	nest := newDivideNestOn(t, wideBrief, 0, reviewer, nil)
	updates := nest.session.TaskUpdates()

	if answer := nest.divide(t, divideArgs(wideEvidence, 2)); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the division the reviewer admitted", answer)
	}
	if reviewer.reads() != 1 {
		t.Fatalf("the reviewer was asked %d times, want the one reading this is about", reviewer.reads())
	}

	moves := phaseMovesAbout(t, updates, nest.parent.id, 2)
	want := []string{TaskPhaseSizing, TaskPhaseWorking}
	if got := taskPhaseWords(moves); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the node's phases were %v, want %v", got, want)
	}
	// AND THE READING CARRIES NO NUMBERS AND NO FINDING, which is the emptiness
	// law on this wire: how many parts there are is what the reading is deciding,
	// so there is nothing true to say about them yet.
	for _, move := range moves {
		if move.Round != 0 || move.Rounds != 0 || move.Text != "" {
			t.Fatalf("a %s move carried round %d of %d and %q", move.Phase, move.Round, move.Rounds, move.Text)
		}
	}
	// AND THE NODE IS BACK AT ITS OWN WORK, on the pulse other windows read as
	// well as on the wire.
	nest.graph.mu.Lock()
	life := nest.parent.life
	nest.graph.mu.Unlock()
	if life != TaskPhaseWorking {
		t.Fatalf("the node was left in %q after the reading ended, want it back at work", life)
	}
}

// AND A REFUSAL THAT COSTS NOTHING SAYS NOTHING.
//
// The evidence gate is arithmetic over text and answers in microseconds. A word
// drawn for it would be the surface narrating machinery rather than a wait, which
// is the emptiness law: a stage nobody waits through is not a stage.
func TestADivisionRefusedWithoutAReadingDrawsNoPhaseAtAll(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"refuse": true, "why": "unused"}`}
	nest := newDivideNestOn(t, wideBrief, 0, reviewer, nil)
	updates := nest.session.TaskUpdates()

	if answer := nest.divide(t, divideArgs(narrowEvidence, 2)); !strings.HasPrefix(answer, "not split:") {
		t.Fatalf("the worker was told %q, want the floor's refusal", answer)
	}
	if reviewer.reads() != 0 {
		t.Fatalf("the reviewer was asked %d times about a division the floor refused for free", reviewer.reads())
	}

	// Nothing is on the lane. It is read with a short deadline rather than
	// drained, because the claim is an absence and an absence has no arrival to
	// wait for.
	select {
	case event := <-updates:
		if event.Kind == EventTaskPhase {
			t.Fatalf("a free refusal announced the phase %q", event.TaskPhase.Phase)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// THE HANDOVER SAYS IT IS WRITING THE BRIEF, FOR THE WHOLE OF IT.
//
// This is the other silence and it is the harder one, because it happens BEFORE
// there is a task to point at: the turn writes down what it found and a
// mastermind turns that into an instruction, and only then does a node exist. So
// it rides the turn's own clock (phasenews.go) — which says what is true while it
// is true and takes itself off the screen when the stage ends, whichever way the
// handover goes.
func TestTheHandoverSaysItIsBriefingAWorkerWhileItWritesTheBrief(t *testing.T) {
	log := watchPhases(t)
	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, "Finish the currency module.", "Finish the currency module; the suite is the done-condition.")}
	agent := checkpointAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	// IT WAS SAID, AND IT NAMED WHO THE BRIEF IS FOR. "briefing" alone is the
	// harness naming its own paperwork; the noun is what makes the row a sentence
	// somebody watching their turn stop can act on.
	said := 0
	for _, one := range log.all() {
		if one.Phase != PhaseBriefing {
			continue
		}
		said++
		if one.Detail != checkpointBriefingWho {
			t.Fatalf("the briefing phase named %q, want %q", one.Detail, checkpointBriefingWho)
		}
		if one.Since.IsZero() {
			t.Fatal("the briefing phase carries no start, so nothing can count up from it")
		}
	}
	// TWICE, ONCE PER CALL. A surface drops a phase it has not heard again for
	// fifteen seconds and nothing here beats, so a single post would go dark
	// halfway through the stage it was added to cover.
	if said != 2 {
		t.Fatalf("the handover said it was briefing %d times, want one per model call", said)
	}

	// AND IT IS CLOSED. A phase left open is a clock a surface goes on drawing for
	// work that ended, which is the defect this lane exists to prevent.
	words := phaseWords(log.all())
	last := -1
	for index, one := range log.all() {
		if one.Phase == PhaseBriefing {
			last = index
		}
	}
	if last < 0 {
		t.Fatalf("no briefing phase at all; phases were %v", words)
	}
	closed := false
	for _, one := range log.all()[last+1:] {
		if one.Phase == "" {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatalf("the briefing clock was never taken off the screen; phases were %v", words)
	}
}
