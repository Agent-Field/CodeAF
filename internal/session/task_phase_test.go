package session

import (
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
