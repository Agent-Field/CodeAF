package session

// readhandoff_test.go — the sweep ledger's contract: what counts, what breaks
// the count, and where the line is. The ledger is the whole of the decision
// the loop trusts, so its edges are pinned here rather than rediscovered by
// the next person to touch the threshold.

import (
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// readCall builds the tool call the ledger sees: a name and the raw arguments,
// which are all the sweep key is made of.
func readCall(name, args string) ai.ToolCall {
	return ai.ToolCall{
		ID:   name + ":" + args,
		Type: "function",
		Function: ai.ToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

// TestReadSweepThirdDistinctTargetDue pins the line: one file is a step, two
// files is a step, and the third distinct read-only target is a sweep.
func TestReadSweepThirdDistinctTargetDue(t *testing.T) {
	sweep := &readSweep{}
	sweep.count([]ai.ToolCall{readCall("read", `{"path":"a.go"}`)})
	sweep.count([]ai.ToolCall{readCall("read", `{"path":"b.go"}`)})
	if sweep.due([]ai.ToolCall{readCall("read", `{"path":"b.go"}`)}) {
		t.Fatal("re-reading a held file must not arm the sweep")
	}
	if !sweep.due([]ai.ToolCall{readCall("read", `{"path":"c.go"}`)}) {
		t.Fatal("the third distinct target is the sweep line")
	}
}

// TestReadSweepSingleBigBatchDue pins the batched style: a model that emits
// its whole reading in one reply is intercepted on that first batch, before
// any of it runs inline.
func TestReadSweepSingleBigBatchDue(t *testing.T) {
	sweep := &readSweep{}
	batch := []ai.ToolCall{
		readCall("read", `{"path":"a.go"}`),
		readCall("read", `{"path":"b.go"}`),
		readCall("read", `{"path":"c.go"}`),
	}
	if !sweep.due(batch) {
		t.Fatal("a batch that arrives already at the line is due")
	}
}

// TestReadSweepSameTargetTwiceIsOne pins the sweep key: the same read issued
// twice is one target, so a turn that re-reads a file it holds never sweeps.
func TestReadSweepSameTargetTwiceIsOne(t *testing.T) {
	sweep := &readSweep{}
	call := readCall("read", `{"path":"a.go"}`)
	sweep.count([]ai.ToolCall{call})
	sweep.count([]ai.ToolCall{call})
	if sweep.due([]ai.ToolCall{call}) {
		t.Fatal("one target three times is one target")
	}
}

// TestReadSweepWhitespaceNormalizedInKey pins JSON whitespace normalization in
// the sweep key: formatting variations for the same read are the same target.
func TestReadSweepWhitespaceNormalizedInKey(t *testing.T) {
	sweep := &readSweep{}
	call1 := readCall("read", `{"path": "a.go"}`)
	call2 := readCall("read", `{"path":"a.go"}`)
	sweep.count([]ai.ToolCall{call1})
	sweep.count([]ai.ToolCall{call2})
	if len(sweep.keys()) != 1 {
		t.Fatalf("expected 1 target after whitespace variation, got %d", len(sweep.keys()))
	}
}

// TestReadSweepMixedBatchHandsOffItsReaders pins the mixed-batch rule: a bash
// or a write riding beside the readers changes nothing — the calls in one
// batch are emitted blind to each other's results, so the sweep a model sizes
// with a wc is still a sweep, and its read-only subset is what gets handed
// off. An executed mixed batch still empties the ledger: the run broke.
func TestReadSweepMixedBatchHandsOffItsReaders(t *testing.T) {
	sweep := &readSweep{}
	sized := []ai.ToolCall{
		readCall("bash", `{"command":"wc -l a.go b.go c.go"}`),
		readCall("read", `{"path":"a.go"}`),
		readCall("read", `{"path":"b.go"}`),
		readCall("read", `{"path":"c.go"}`),
	}
	if !sweep.due(sized) {
		t.Fatal("a wc beside three reads does not launder the sweep")
	}
	if sweep.due([]ai.ToolCall{
		readCall("bash", `{"command":"wc -l a.go"}`),
		readCall("grep", `{"pattern":"x"}`),
	}) {
		t.Fatal("one reader beside a bash is one step, not a sweep")
	}
	sweep.count([]ai.ToolCall{readCall("read", `{"path":"a.go"}`)})
	sweep.count([]ai.ToolCall{readCall("read", `{"path":"b.go"}`)})
	mixed := []ai.ToolCall{
		readCall("read", `{"path":"c.go"}`),
		readCall("edit", `{"path":"c.go"}`),
	}
	if !sweep.due(mixed) {
		t.Fatal("the third distinct reader is due even with a write beside it")
	}
	sweep.count(mixed)
	if sweep.due([]ai.ToolCall{readCall("read", `{"path":"c.go"}`)}) {
		t.Fatal("the write broke the run; the count starts over")
	}
}

// TestReadSweepBashBreaksTheRun pins bash as a breaker: it is never one of the
// four readers, so a turn that shells out between reads starts its count over.
func TestReadSweepBashBreaksTheRun(t *testing.T) {
	sweep := &readSweep{}
	sweep.count([]ai.ToolCall{readCall("read", `{"path":"a.go"}`)})
	sweep.count([]ai.ToolCall{readCall("read", `{"path":"b.go"}`)})
	sweep.count([]ai.ToolCall{readCall("bash", `{"command":"wc -l a.go"}`)})
	if sweep.due([]ai.ToolCall{readCall("read", `{"path":"c.go"}`)}) {
		t.Fatal("bash between the reads broke the run")
	}
}

// TestReadSweepDisabledStaysDisabled pins the failure latch: once a hand-off
// has failed, the sweep does not arm again this turn, so the failure is paid
// once rather than round after round.
func TestReadSweepDisabledStaysDisabled(t *testing.T) {
	sweep := &readSweep{disabled: true}
	batch := []ai.ToolCall{
		readCall("read", `{"path":"a.go"}`),
		readCall("read", `{"path":"b.go"}`),
		readCall("read", `{"path":"c.go"}`),
	}
	if sweep.due(batch) {
		t.Fatal("a failed hand-off disables the sweep for the turn")
	}
	sweep.count(batch)
	if len(sweep.keys()) != 0 {
		t.Fatal("a disabled sweep does not count")
	}
}

// keys is the test's window into the ledger.
func (s *readSweep) keys() []string {
	keys := make([]string, 0, len(s.targets))
	for key := range s.targets {
		keys = append(keys, key)
	}
	return keys
}
