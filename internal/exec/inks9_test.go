package exec

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE INK RUN OF 2026-08-29 (s9), AT THE MOMENT ITS LEAVES WERE CUT.
//
// Three leaves, all on the generalist, all landed early, all journaled as "it
// was still working when it ran out of its tokens" against a grant of 150,000.
// Not one of them reached that grant. What landed every one of them was the
// cumulative bound on prompt sent, at 240,000, four landing turns before the
// number the record printed:
//
//	task-2 attempt 1   crossed 240,000 sent at turn 13, landed at 17
//	task-2 attempt 2   crossed 240,000 sent at turn 12, landed at 16
//	task-2-x1          crossed 240,000 sent at turn  9, landed at 13
//
// The figures below are attempt one's, read out of that store's usage_turns.
const (
	inkS9Prompt     = 365764
	inkS9Completion = 7177
	inkS9Cached     = 298752
	inkS9Sent       = 365764
	inkS9Turns      = 17
)

// inkS9Attempt rebuilds that leaf's meter readings.
func inkS9Attempt() *Outcome {
	outcome := &Outcome{
		Turns: inkS9Turns,
		Usage: Usage{
			PromptTokens:     inkS9Prompt,
			CompletionTokens: inkS9Completion,
			CachedTokens:     inkS9Cached,
		},
	}
	// One row carrying the whole of what was sent: contextPressure sums Sent
	// over the turn rows, and the sum is what the bound reads.
	outcome.PerTurn = []TurnUsage{{Turn: 1, Sent: inkS9Sent}}
	return outcome
}

// WHAT LANDS A LEAF IS WHAT ITS WORK COSTS. The cumulative prompt bound cut this
// leaf at turn 13 of a 200-turn grant with 31% of its money unspent; it is a
// warning now and it lands nobody.
func TestTheInkLeafIsNotLandedByHowMuchTextWentPast(t *testing.T) {
	outcome := inkS9Attempt()

	// The bound that actually fired, still crossed — the evidence is kept.
	pressure := outcome.contextPressure()
	ceiling := reuseCeiling(ctxbudget.DefaultWorkingSetTokens)
	if ceiling <= 0 {
		t.Fatal("the reuse ceiling went inert on a known window; the rest of this test means nothing")
	}
	if !pressureReached(pressure, ceiling) {
		t.Fatalf("the ink leaf sent %d prompt tokens against a %d ceiling — the fixture no longer reproduces the run",
			pressure, ceiling)
	}

	// And it no longer stops the leaf.
	if exhausted(outcome, defaultLeafTokens) {
		t.Fatalf("the ink leaf was landed with %d of %d tokens of billed work unspent",
			defaultLeafTokens-spent(outcome), defaultLeafTokens)
	}
	if spent(outcome) >= defaultLeafTokens {
		t.Fatalf("spent() = %d, want a leaf still inside its %d grant", spent(outcome), defaultLeafTokens)
	}

	// THE EVIDENCE IS NOT THROWN AWAY, IT IS DEMOTED. A leaf whose transcript
	// has gone round many times over is told to wrap up — which costs a
	// sentence — instead of being landed, which costs its work.
	if used := budgetUsed(outcome, defaultLeafTokens, ctxbudget.DefaultWorkingSetTokens); used <= wrapUpAt {
		t.Fatalf("budgetUsed = %.2f, want the reuse pressure to still raise the wrap-up warning past %.2f",
			used, wrapUpAt)
	}
}

// AND THE BOUND THAT FIRES NAMES ITSELF. Three ceilings shared one StopReason,
// so the journal said "ran out of its tokens" and printed a grant the leaf had
// never reached — which is why the first three readings of ink s9 each blamed a
// different meter.
func TestALandedLeafSaysWhichBoundLandedIt(t *testing.T) {
	space := workspace(t)
	// A one-turn grant: the turn cap is the bound, and it must say so.
	linear := NewLinear(&scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "sh", `{"command":"true"}`)},
		{call("c2", "sh", `{"command":"true"}`)},
	}}, space, nil, 1, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "do the thing"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome.Stop != StopTurnCap {
		t.Fatalf("stop = %q, want the turn cap", outcome.Stop)
	}
	if !outcome.Meter.Named() {
		t.Fatal("the leaf was landed by a bound that did not name itself")
	}
	if outcome.Meter.Name != "turns" {
		t.Fatalf("meter = %q, want the bound that actually fired", outcome.Meter.Name)
	}
	if words := outcome.Meter.Words(); !strings.Contains(words, "turns") || !strings.Contains(words, "1") {
		t.Fatalf("the meter reads %q, want its own two numbers in it", words)
	}
}

// THE GENERALIST LEAVES A RECORD. exec.TranscriptFrom had one reader in the
// whole tree — the bare belt — so a run on the default worker left a store with
// no transcript at all: BankedRun found nothing, every continuation started
// cold, and the resumption the lease lane built could never fire. ink s9's store
// holds zero transcript rows across three leaves.
func TestTheGeneralistRecordsItsTurnsUnderTheNode(t *testing.T) {
	space := workspace(t)
	sink := &countingSink{}
	ctx := WithTranscript(context.Background(), sink)

	linear := NewLinear(&scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "sh", `{"command":"echo one"}`)},
	}}, space, nil, 4, 1_000_000, time.Minute)
	if _, err := linear.Run(ctx, Task{NodeID: 7, Brief: "add grid layout"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	kinds := sink.kinds()
	for _, want := range []store.TranscriptKind{
		store.TranscriptAssistant, store.TranscriptToolCall, store.TranscriptToolResult,
	} {
		if kinds[want] == 0 {
			t.Fatalf("the generalist recorded no %s entry; it left %v", want, kinds)
		}
	}
	// A call and its result are matched by the provider's own id, or a reader
	// of a parallel turn cannot say which answer belongs to which question.
	if !sink.pairsCallsWithResults() {
		t.Fatal("a recorded tool call and its result do not share a call id")
	}
	if !sink.flushed() {
		t.Fatal("the record was never made durable; a leaf that dies mid-loop would leave nothing")
	}
}

// countingSink is a TranscriptSink that only counts, so a belt can be asked
// what it recorded without a store.
type countingSink struct {
	mutex   sync.Mutex
	entries []store.TranscriptEntry
	flushes int
}

func (c *countingSink) Record(entry store.TranscriptEntry) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.entries = append(c.entries, entry)
}

func (c *countingSink) Flush() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.flushes++
}

func (c *countingSink) kinds() map[store.TranscriptKind]int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	counted := map[store.TranscriptKind]int{}
	for _, entry := range c.entries {
		counted[entry.Kind]++
	}
	return counted
}

func (c *countingSink) flushed() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.flushes > 0
}

func (c *countingSink) pairsCallsWithResults() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	calls := map[string]bool{}
	for _, entry := range c.entries {
		if entry.Kind == store.TranscriptToolCall && entry.CallID != "" {
			calls[entry.CallID] = true
		}
	}
	for _, entry := range c.entries {
		if entry.Kind == store.TranscriptToolResult && calls[entry.CallID] {
			return true
		}
	}
	return false
}
