package exec

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// writeTurns scripts a leaf that keeps working: one write per turn, forever, so
// the only things that can stop it are its ceilings and the straggler question.
func writeTurns(count int) [][]ai.ToolCall {
	var turns [][]ai.ToolCall
	for index := 0; index < count; index++ {
		turns = append(turns, []ai.ToolCall{call(
			fmt.Sprintf("c%d", index), "write",
			fmt.Sprintf(`{"path":"out-%d.txt","text":"x"}`, index))})
	}
	return turns
}

// A task with no measurement behind it is the task every path had before this
// existed, and it must behave identically: no question is put, no judge is
// consulted, and the loop runs to its own ceilings.
func TestALeafWithNoMeasuredThresholdIsNeverAsked(t *testing.T) {
	asked := 0
	client := &scriptedCompleter{turns: writeTurns(4)}
	linear := NewLinear(client, workspace(t), nil, 20, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "work",
		// A watch with no threshold is as inert as no watch at all, which is
		// what a profile below its evidence gate produces.
		Overrun: &OverrunWatch{Judge: func(OverrunEvidence) OverrunVerdict {
			asked++
			return OverrunHandBack
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatalf("the judge was asked %d times about a leaf with no threshold", asked)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want the ordinary ending", outcome.Stop)
	}
}

// The straggler fires, once, with both sides of the comparison in hand.
//
// The evidence is the whole point: "this leaf spent N tokens" is not evidence of
// anything until it sits beside the median its siblings spent and the number of
// runs that median came from.
func TestCrossingTheThresholdAsksOnceAndCarriesTheComparison(t *testing.T) {
	var seen []OverrunEvidence
	client := &scriptedCompleter{turns: writeTurns(8)}
	linear := NewLinear(client, workspace(t), nil, 20, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "work",
		// The scripted turns cost 15 raw tokens each, so a threshold of 20 is
		// crossed on the second turn and the leaf has plenty of turns left.
		Overrun: &OverrunWatch{
			Threshold: 20, Anchor: 8, Multiple: 2.5, Samples: 11,
			Judge: func(evidence OverrunEvidence) OverrunVerdict {
				seen = append(seen, evidence)
				return OverrunContinue
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("the judge was asked %d times, want exactly once per leaf", len(seen))
	}
	evidence := seen[0]
	if evidence.Spent < 20 || evidence.Turns == 0 {
		t.Fatalf("the evidence does not describe the running leaf: %+v", evidence)
	}
	if evidence.Anchor != 8 || evidence.Threshold != 20 || evidence.Multiple != 2.5 || evidence.Samples != 11 {
		t.Fatalf("the evidence lost the other side of the comparison: %+v", evidence)
	}
	// Judged worth continuing means exactly that: nothing about the leaf
	// changed, and it ended the way it was always going to.
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want the leaf to have carried on untouched", outcome.Stop)
	}
}

// The hand-back is a landing, not a kill. The leaf is told to make things
// consistent and say what it did; its emitted calls still execute; and the
// ending it lands on is the one the existing escalation and continuation paths
// already know how to read.
func TestAJudgedHandBackLandsAndReachesTheEscalationPath(t *testing.T) {
	client := &scriptedCompleter{turns: writeTurns(20)}
	linear := NewLinear(client, workspace(t), nil, 30, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "work",
		Overrun: &OverrunWatch{
			Threshold: 20, Anchor: 8, Multiple: 2.5, Samples: 11,
			Judge: func(OverrunEvidence) OverrunVerdict { return OverrunHandBack },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopOverrun {
		t.Fatalf("stop = %s, want %s", outcome.Stop, StopOverrun)
	}
	if outcome.Exhausted != StopOverrun {
		t.Fatalf("exhausted = %q, want the hand-back recorded when it was ordered", outcome.Exhausted)
	}
	// The three properties the rest of the system reads off a handed-back leaf.
	if !outcome.Overran() {
		t.Fatal("a handed-back leaf does not report having work left, so nothing will continue it")
	}
	if outcome.Verdict != provider.VerdictBudgetStop {
		t.Fatalf("verdict = %s, want the budget grading a straggler earns", outcome.Verdict)
	}
	if !outcome.Verdict.Escalates() {
		t.Fatal("the verdict does not escalate, so the retry loop will never offer it another worker")
	}
	// It landed rather than being cut off: the loop stopped well short of its
	// twenty scripted turns, and it was told to land in so many words.
	if outcome.Turns >= 20 {
		t.Fatalf("ran %d turns; the hand-back did not bound the leaf", outcome.Turns)
	}
	landed := false
	for _, messages := range client.seen {
		for _, message := range messages {
			for _, part := range message.Content {
				if strings.Contains(part.Text, "handed back so the work can be taken up differently") {
					landed = true
				}
			}
		}
	}
	if !landed {
		t.Fatal("the leaf was never told it was being handed back")
	}
}

// A watch installed with no judge behind it must not become a cap by accident.
// This is the failure mode the whole design is written against: a threshold
// nobody answers for is a budget with a derivation attached.
func TestAThresholdWithNoJudgeNeverStopsALeaf(t *testing.T) {
	client := &scriptedCompleter{turns: writeTurns(6)}
	linear := NewLinear(client, workspace(t), nil, 20, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "work",
		Overrun: &OverrunWatch{Threshold: 20, Anchor: 8, Samples: 11},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s — an unanswered threshold acted as a cap", outcome.Stop)
	}
}
