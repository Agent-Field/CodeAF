package exec

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	graphstore "github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const timeoutNodeID = "task-timeouts"

func timeoutCalls(count int, arguments string) [][]ai.ToolCall {
	turns := make([][]ai.ToolCall, count)
	for index := range turns {
		turns[index] = []ai.ToolCall{call(fmt.Sprintf("timeout-%d", index), "sh", arguments)}
	}
	return turns
}

func timeoutWorkspace(t *testing.T) (*Workspace, *graphstore.Store) {
	t.Helper()
	space := workspace(t)
	history, err := graphstore.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	if err := history.Splice(graphstore.RootID, graphstore.Subtree{Nodes: []graphstore.NodeSpec{{
		ID: timeoutNodeID, Brief: "finish the work", Stage: 1,
	}}}, graphstore.Provenance{Origin: graphstore.OriginUser, Intent: "finish the work"}); err != nil {
		t.Fatal(err)
	}
	return space, history
}

func runTimeoutRound(
	t *testing.T, space *Workspace, history *graphstore.Store, turns [][]ai.ToolCall, maxTokens int,
) (*Outcome, *scriptedCompleter) {
	t.Helper()
	client := &scriptedCompleter{turns: turns}
	linear := NewLinear(client, space, nil, len(turns)+5, maxTokens, time.Minute)
	if history != nil {
		linear = linear.WithStore(history)
	}
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, StoreNodeID: timeoutNodeID, Brief: "finish the work",
	})
	if err != nil {
		t.Fatal(err)
	}
	return outcome, client
}

func recordedTimeout(t *testing.T, history *graphstore.Store) graphstore.LeafExhausted {
	t.Helper()
	record, ok, err := history.LeafExhaustedFor(timeoutNodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the timeout ending was not recorded")
	}
	return record
}

func expectedTimeoutReason(command string) string {
	return fmt.Sprintf(
		"the same command timed out %d times, so it was stopped rather than run again: %s",
		toolTimeoutRepeatCap, command)
}

// TestThreeIdenticalTimeoutsEndTheRound proves C1 and C2: one exact command's
// repeated timeouts end the round with their own reason before another turn.
func TestThreeIdenticalTimeoutsEndTheRound(t *testing.T) {
	turns := timeoutCalls(toolTimeoutRepeatCap+2, `{"cmd":"sleep 5","t":1}`)
	outcome, client := runTimeoutRound(t, workspace(t), nil, turns, 1_000_000)

	if outcome.Stop != StopToolTimeouts || outcome.Exhausted != StopToolTimeouts {
		t.Fatalf("stop = %q, exhausted = %q, want %q", outcome.Stop, outcome.Exhausted, StopToolTimeouts)
	}
	if outcome.ToolCalls != toolTimeoutRepeatCap {
		t.Fatalf("tool calls = %d, want %d", outcome.ToolCalls, toolTimeoutRepeatCap)
	}
	if len(client.seen) != toolTimeoutRepeatCap {
		t.Fatalf("model turns = %d, want %d; the leaf was asked again", len(client.seen), toolTimeoutRepeatCap)
	}
}

// TestTheReasonNamesTheCommandAndTheCount proves C3: the recorded sentence
// carries both the serial command and the limit that ended its round.
func TestTheReasonNamesTheCommandAndTheCount(t *testing.T) {
	space, history := timeoutWorkspace(t)
	arguments := `{"cmd":["sleep 5","echo never"],"t":1}`
	runTimeoutRound(t, space, history, timeoutCalls(toolTimeoutRepeatCap, arguments), 1_000_000)
	record := recordedTimeout(t, history)
	want := expectedTimeoutReason("sleep 5 && echo never")
	if record.Reason != want {
		t.Fatalf("reason = %q, want %q", record.Reason, want)
	}
	if !strings.Contains(record.Reason, strconv.Itoa(toolTimeoutRepeatCap)) {
		t.Fatalf("reason does not name the timeout count: %q", record.Reason)
	}
}

// TestTheEndingReachesTheStreamAndTheGate proves C4: the store seam read by
// both existing readers carries the ending's bound, sentence and measurement.
func TestTheEndingReachesTheStreamAndTheGate(t *testing.T) {
	space, history := timeoutWorkspace(t)
	arguments := `{"cmd":"sleep 5","t":1}`
	runTimeoutRound(t, space, history, timeoutCalls(toolTimeoutRepeatCap, arguments), 1_000_000)
	record := recordedTimeout(t, history)

	if record.Bound != string(StopToolTimeouts) {
		t.Fatalf("bound = %q, want %q", record.Bound, StopToolTimeouts)
	}
	if want := expectedTimeoutReason("sleep 5"); record.Reason != want {
		t.Fatalf("reason = %q, want %q", record.Reason, want)
	}
	if record.Meter != MeterToolTimeouts || record.Reached != toolTimeoutRepeatCap ||
		record.Allowance != toolTimeoutRepeatCap || record.Unit != "timeouts of one command" {
		t.Fatalf("recorded meter = %+v", record)
	}
}

// TestTimeoutsOfDifferentCommandsDoNotEndTheRound proves C5: several timeout
// facts about different commands do not say that one command cannot finish.
func TestTimeoutsOfDifferentCommandsDoNotEndTheRound(t *testing.T) {
	turns := [][]ai.ToolCall{
		{call("slow-a", "sh", `{"cmd":"sleep 5","t":1}`)},
		{call("slow-b", "sh", `{"cmd":"sleep 6","t":1}`)},
		{call("slow-c", "sh", `{"cmd":"sleep 7","t":1}`)},
	}
	outcome, _ := runTimeoutRound(t, workspace(t), nil, turns, 1_000_000)
	if outcome.Stop != StopDone || outcome.Exhausted != "" {
		t.Fatalf("stop = %q, exhausted = %q, want a normal finish", outcome.Stop, outcome.Exhausted)
	}
}

// TestTwoTimeoutsOfOneCommandDoNotEndTheRound proves C6: the leaf may diagnose
// after two identical timeouts and still finish normally.
func TestTwoTimeoutsOfOneCommandDoNotEndTheRound(t *testing.T) {
	turns := timeoutCalls(toolTimeoutRepeatCap-1, `{"cmd":"sleep 5","t":1}`)
	turns = append(turns, []ai.ToolCall{call("recovered", "sh", `{"cmd":"echo recovered"}`)})
	outcome, _ := runTimeoutRound(t, workspace(t), nil, turns, 1_000_000)
	if outcome.Stop != StopDone || outcome.Exhausted != "" {
		t.Fatalf("stop = %q, exhausted = %q, want a normal finish", outcome.Stop, outcome.Exhausted)
	}
}

// TestOnlyATimeoutCounts proves C7: repeated success and ordinary command
// failure never count as evidence that a command does not return.
func TestOnlyATimeoutCounts(t *testing.T) {
	var turns [][]ai.ToolCall
	for index := 0; index < toolTimeoutRepeatCap; index++ {
		turns = append(turns, []ai.ToolCall{call(fmt.Sprintf("failure-%d", index), "sh", `{"cmd":"false"}`)})
	}
	for index := 0; index < toolTimeoutRepeatCap; index++ {
		turns = append(turns, []ai.ToolCall{call(fmt.Sprintf("success-%d", index), "sh", `{"cmd":"true"}`)})
	}
	outcome, _ := runTimeoutRound(t, workspace(t), nil, turns, 1_000_000)
	if outcome.Stop != StopDone || outcome.Exhausted != "" {
		t.Fatalf("stop = %q, exhausted = %q, want a normal finish", outcome.Stop, outcome.Exhausted)
	}
}

// TestATimedOutLeafIsStillJudgedOnItsWork proves C8: this ending is not an
// out-of-room condition and does not discard the leaf's account as an overrun.
func TestATimedOutLeafIsStillJudgedOnItsWork(t *testing.T) {
	if StopToolTimeouts.OutOfRoom() {
		t.Fatal("a repeated command timeout was treated as running out of room")
	}
	outcome := &Outcome{Stop: StopToolTimeouts, Exhausted: StopToolTimeouts}
	if outcome.Overran() {
		t.Fatal("a repeated command timeout was treated as an overrun")
	}
}

// TestALandingLeafIsNotReEndedForTimeouts proves C9: a budget landing keeps
// its original ending even when the same command times out throughout it.
func TestALandingLeafIsNotReEndedForTimeouts(t *testing.T) {
	// The first reply spends the tiny grant. All repeated timeouts then fit
	// inside one landing reply, which even a token-bounded reserve must finish.
	landing := make([]ai.ToolCall, toolTimeoutRepeatCap+1)
	for index := range landing {
		landing[index] = call(fmt.Sprintf("landing-timeout-%d", index), "sh", `{"cmd":"sleep 5","t":1}`)
	}
	turns := [][]ai.ToolCall{{call("spend-grant", "sh", `{"cmd":"true"}`)}, landing}
	turns = append(turns, timeoutCalls(landingTurns, `{"cmd":"sleep 5","t":1}`)...)
	outcome, _ := runTimeoutRound(t, workspace(t), nil, turns, 1)
	if outcome.Stop != StopBudget || outcome.Exhausted != StopBudget {
		t.Fatalf("stop = %q, exhausted = %q, want the budget ending", outcome.Stop, outcome.Exhausted)
	}
	if outcome.ToolCalls < 1+len(landing) {
		t.Fatalf("tool calls = %d; want at least the initial call and all %d landing calls", outcome.ToolCalls, len(landing))
	}
}

// TestALeafThatNeverTimesOutIsUnaffected proves C10: an ordinary productive
// leaf finishes done and leaves no timeout ending in the journal.
func TestALeafThatNeverTimesOutIsUnaffected(t *testing.T) {
	space, history := timeoutWorkspace(t)
	turns := [][]ai.ToolCall{{call("write", "write", `{"path":"answer.txt","text":"done"}`)}}
	outcome, _ := runTimeoutRound(t, space, history, turns, 1_000_000)
	if outcome.Stop != StopDone || outcome.Exhausted != "" {
		t.Fatalf("stop = %q, exhausted = %q, want a normal finish", outcome.Stop, outcome.Exhausted)
	}
	if _, ok, err := history.LeafExhaustedFor(timeoutNodeID); err != nil || ok {
		t.Fatalf("timeout ending recorded = %v, error = %v", ok, err)
	}
}
