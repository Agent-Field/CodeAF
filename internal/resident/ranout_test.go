package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ranOutJob is a single claimable leaf for the runner to settle one way or the
// other.
func ranOutJob(t *testing.T) *store.Store {
	t.Helper()
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-1")
	return graph
}

// THE SEAM. A leaf that ran out of its tokens reaches the scheduler with err ==
// nil — running out is not an error, the executor grants a landing reserve and
// the leaf lands — and the ending has to cross with it or the scheduler cannot
// tell it from a leaf that finished.
//
// `resident.ExecResult` carried a summary, some money and some turns and nothing
// about how the work ENDED, so `runOne` saw no error and called `graph.Complete`.
// One earlier attempt at this defect added the fields and never populated them at
// the construction site, which is dead code that reads like a fix; the fields are
// asked for here through the same door production fills them through.
func TestALeafThatRanOutOfItsBudgetIsNotCompleted(t *testing.T) {
	graph := ranOutJob(t)
	if err := graph.RecordTranscript("task-1", "worker/model", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptAssistant, Text: "editing runner.go"},
	}); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{
			Summary: "All 722 tests pass. Let me verify the dry-run tests specifically:",
			Stop:    executor.StopBudget,
			Meter: executor.Meter{Name: executor.MeterCost, Reached: 199131,
				Allowed: 176834, Unit: "tokens of billed work"},
		}, nil
	}, "budget-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	if node.Status == store.Done {
		t.Fatalf("a leaf that ran out mid-edit was settled done on its own last sentence: %+v", node)
	}
	if node.Status != store.Pending {
		t.Fatalf("a leaf that ran out was settled %q, want it back on the queue", node.Status)
	}
	if node.Summary != "" {
		t.Fatalf("a cut leaf's own words were kept as the node's account: %q", node.Summary)
	}
}

// And the ending is only news where nothing else is carrying the work. A leaf
// whose remainder was spliced has a successor holding it, and the node itself is
// finished with — which is what `Continued` says and why `RanOut` asks it.
func TestALeafWhoseRemainderWasSplicedStillSettles(t *testing.T) {
	graph := ranOutJob(t)
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "as far as it got",
			Stop: executor.StopBudget, Continued: true}, nil
	}, "continued-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Done {
		t.Fatalf("a leaf whose work was already spliced on was left %q", node.Status)
	}
}

// A leaf that finished is settled exactly as it always was, and an ExecResult
// that says nothing about its ending is not read as one that ran out. A
// StopReason is a string and its zero value must never mean "it ran out".
func TestAnEndingNobodyRecordedIsNotReadAsRunningOut(t *testing.T) {
	for _, probe := range []struct {
		name   string
		result ExecResult
	}{
		{"finished", ExecResult{Summary: "done", Stop: executor.StopDone}},
		{"nothing recorded", ExecResult{Summary: "done"}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			graph := ranOutJob(t)
			runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
				return probe.result, nil
			}, "done-runner", 1)
			if _, err := runner.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			runner.Wait()
			node, ok, err := graph.Node("task-1")
			if err != nil || !ok {
				t.Fatalf("node: ok=%v err=%v", ok, err)
			}
			if node.Status != store.Done {
				t.Fatalf("a leaf that finished was left %q", node.Status)
			}
		})
	}
}

// The release carries the turns the attempt banked, because the next claim
// resumes from them and the headless stream tells a requeue that is PROGRESS
// from a claim taken off a worker that never answered.
func TestTheReleaseOfACutLeafCarriesItsRecordedTurns(t *testing.T) {
	graph := ranOutJob(t)
	if err := graph.RecordTranscript("task-1", "worker/model", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptAssistant, Text: "reading the runner"},
		{Turn: 2, Kind: store.TranscriptAssistant, Text: "editing the runner"},
		{Turn: 3, Kind: store.TranscriptAssistant, Text: "still editing"},
	}); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "cut off", Stop: executor.StopBudget,
			Meter: executor.Meter{Name: executor.MeterCost, Reached: 199131, Allowed: 176834,
				Unit: "tokens of billed work"}}, nil
	}, "release-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	events, err := graph.Events(0, 200)
	if err != nil {
		t.Fatal(err)
	}
	released := ""
	for _, event := range events {
		if event.Kind == store.EventNodeReleased && event.NodeID == "task-1" {
			released = string(event.Payload)
		}
	}
	if released == "" {
		t.Fatal("the node went back on the queue with no reason on the release")
	}
	if !strings.Contains(released, "still working when it ran out") {
		t.Fatalf("the release does not say what happened: %s", released)
	}
	if !strings.Contains(released, "199131") {
		t.Fatalf("the release does not carry what ran out: %s", released)
	}
	if !strings.Contains(released, `"recorded":3`) {
		t.Fatalf("the release does not carry the turns the next attempt resumes from: %s", released)
	}
}

// THE THREE ENDINGS OF A LEAF THAT RAN OUT, and the incidental work every one of
// them still owes the record.
//
// The queue used to be the only answer here, and it was reached by asking two
// questions that are both true of the very first fruitless round: did this leaf
// run out, and is anything continuing it. So a job whose own governor had
// already concluded that carrying on changed nothing — twice over, printed on
// the stream and written to the journal — went back on the queue for a third
// attempt at the same standstill. And a leaf whose delivery had been read and
// PASSED went back on it too, because running out of room outranked a judgement
// of the work itself.
//
// Each case asserts the settlement AND the two ledgers the requeue path was
// writing incidentally: the money the attempt cost, which is what the daily rail
// and every receipt are summed from, and the per-turn shape beside it. An arm
// that settles a node and forgets those is an attempt that reads as free.
func TestWhatSettlesALeafThatRanOut(t *testing.T) {
	for _, probe := range []struct {
		name    string
		gate    *store.DeliveryGate
		refused string
		want    store.Status
		says    string
	}{
		// A gate pass is a reading of the WORK against what was asked; running
		// out is a reading of the meter. The work's question is answered by the
		// work's verdict.
		{name: "a passed gate settles it", gate: &store.DeliveryGate{Pass: true},
			want: store.Done, says: ""},
		{name: "a standstill ends it", refused: CauseStandstill,
			want: store.Failed, says: RefusedStandstill},
		{name: "a fixed point ends it", refused: CauseFixedPoint,
			want: store.Failed, says: RefusedFixedPoint},
		// And everything else is the queue exactly as it was, first fruitless
		// round included: the governor encodes that clause and nothing here
		// re-derives it.
		{name: "neither requeues it", want: store.Pending,
			says: "still working when it ran out"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			graph := ranOutJob(t)
			if err := graph.RecordTranscript("task-1", "worker/model", []store.TranscriptEntry{
				{Turn: 1, Kind: store.TranscriptAssistant, Text: "reading the notes"},
				{Turn: 2, Kind: store.TranscriptAssistant, Text: "still reading the notes"},
			}); err != nil {
				t.Fatal(err)
			}
			if probe.gate != nil {
				if err := graph.RecordDeliveryGate("task-1", *probe.gate); err != nil {
					t.Fatal(err)
				}
			}
			runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
				return ExecResult{
					Summary: "as far as it got", Stop: executor.StopBudget,
					RefusedGrowth:    probe.refused,
					PromptTokens:     900_000,
					CompletionTokens: 40_000,
					Cost:             12.50,
					Model:            "worker/model",
					Turns: []executor.TurnUsage{
						{Turn: 1, Usage: executor.Usage{PromptTokens: 450_000, CompletionTokens: 20_000, Cost: 6.25}},
						{Turn: 2, Usage: executor.Usage{PromptTokens: 450_000, CompletionTokens: 20_000, Cost: 6.25}},
					},
					Meter: executor.Meter{Name: executor.MeterCost, Reached: 199131,
						Allowed: 176834, Unit: "tokens of billed work"},
				}, nil
			}, "settlement-runner", 1)
			if _, err := runner.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			runner.Wait()

			node, ok, err := graph.Node("task-1")
			if err != nil || !ok {
				t.Fatalf("node: ok=%v err=%v", ok, err)
			}
			if node.Status != probe.want {
				t.Fatalf("a leaf that ran out settled %q, want %q", node.Status, probe.want)
			}
			if probe.says != "" && !strings.Contains(endingWords(t, graph, node), probe.says) {
				t.Fatalf("the ending does not say what happened: %q", endingWords(t, graph, node))
			}
			// A leaf that ran out spent real money whichever way it ends, and
			// the rail is summed from this table and nowhere else.
			rail, err := graph.DailyRailToday(20)
			if err != nil {
				t.Fatal(err)
			}
			if rail.Spend < 12.49 || rail.Spend > 12.51 {
				t.Fatalf("the daily rail saw $%.2f of the $12.50 this attempt cost", rail.Spend)
			}
			impact, err := graph.Impact("task-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if impact.Cost < 12.49 {
				t.Fatalf("the node's own impact = $%.2f; a receipt would quote a free attempt", impact.Cost)
			}
			turns, err := graph.TurnUsageFor("task-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(turns) != 2 {
				t.Fatalf("the per-turn ledger holds %d rows, want the attempt's 2", len(turns))
			}
		})
	}
}

// endingWords is whatever the store now says about how this node ended, from
// whichever of the three places holds it: the failure message, the reason on the
// release, or nothing at all for a node that settled done.
func endingWords(t *testing.T, graph *store.Store, node store.Node) string {
	t.Helper()
	if strings.TrimSpace(node.Error) != "" {
		return node.Error
	}
	events, err := graph.Events(0, 400)
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	for _, event := range events {
		if event.NodeID == node.ID && event.Kind == store.EventNodeReleased {
			said = string(event.Payload)
		}
	}
	return said
}

// The two sentences a stopped job ends on are the governor's own, verbatim. The
// stream printed them at the moment the growth was refused, and a second wording
// of one event is a person working out whether two things happened.
func TestTheEndingIsTheGovernorsOwnSentence(t *testing.T) {
	for cause, want := range map[string]string{
		CauseStandstill: RefusedStandstill,
		CauseFixedPoint: RefusedFixedPoint,
	} {
		words, stopped := GrowthStopped(cause)
		if !stopped || words != want {
			t.Fatalf("GrowthStopped(%q) = %q,%t", cause, words, stopped)
		}
	}
	// And nothing else stops a job. A cap, a wall, a rail and an unrefused
	// round all leave the queue exactly as they found it.
	for _, cause := range []string{"", CauseRounds, CauseCeiling, CauseRail,
		CauseCovered, CauseFindingStood, CauseOutOfWall} {
		if _, stopped := GrowthStopped(cause); stopped {
			t.Fatalf("%q stopped the job; only a reading of the world may", cause)
		}
	}
	// The figures still ride with the sentence, because an autopsy asking how
	// much this cost has nowhere else to read it.
	ending := nothingChangedFailure(ExecResult{Meter: executor.Meter{Name: executor.MeterCost,
		Reached: 199131, Allowed: 176834, Unit: "tokens of billed work"}}, RefusedStandstill)
	if !strings.HasPrefix(ending, RefusedStandstill) || !strings.Contains(ending, "199131") {
		t.Fatalf("ending = %q", ending)
	}
}
