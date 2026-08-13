package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// steadyLeaf replays what a leaf actually does to its transcript: every turn
// appends an assistant message and one tool result of a fixed size, then runs
// decay before the next call. It reports what each turn did to the messages
// that were already there, which is the only thing a prefix cache cares about.
type steadyLeaf struct {
	fade     *decayer
	messages []ai.Message
	previous map[int]string // index → content after the previous turn's decay
}

func newSteadyLeaf() *steadyLeaf {
	return &steadyLeaf{fade: newDecayer(map[string]string{}, nil), previous: map[int]string{}}
}

// turn appends one assistant/tool pair of the given result size and decays.
// rewrote is true when the pass changed a message that already existed — the
// event that invalidates the provider's cached prefix from that point on.
func (l *steadyLeaf) turn(t *testing.T, size, budget int) (rewrote, wasOver bool) {
	t.Helper()
	id := fmt.Sprintf("c%d", len(l.messages))
	call := ai.ToolCall{ID: id, Type: "function",
		Function: ai.ToolCallFunction{Name: "sh", Arguments: `{"cmd":"ls"}`}}
	l.fade.labels[id] = callLabel(call)
	l.messages = append(l.messages,
		ai.Message{Role: "assistant", Content: text("thinking " + id), ToolCalls: []ai.ToolCall{call}},
		ai.Message{Role: "tool", ToolCallID: id, Content: text(strings.Repeat("x", size))},
	)
	wasOver = liveObservationBytes(l.messages) > budget
	// The loop tells the decayer what each turn added, because that is what the
	// low-water mark is solved against. A leaf that measures nothing gets the
	// fixed fraction, which is the case this replay is no longer in.
	l.fade.observe(size)
	l.fade.decay(l.messages, budget)
	for index, before := range l.previous {
		if contentOf(l.messages[index]) != before {
			rewrote = true
			break
		}
	}
	for index := range l.messages {
		l.previous[index] = contentOf(l.messages[index])
	}
	return rewrote, wasOver
}

// steadyLowWater is where a firing pass lands for a leaf whose every turn adds
// the same number of bytes — the mark the loop itself would compute once it has
// measured that leaf, which is what the replays below run against.
func steadyLowWater(budget, perTurn int) int {
	gauge := newDecayer(nil, nil)
	for sample := 0; sample < minInflowSamples; sample++ {
		gauge.observe(perTurn)
	}
	return gauge.lowWater(budget)
}

// stubbedIDs is the set of tool_call_ids currently carrying a stub.
func (l *steadyLeaf) stubbedIDs() map[string]bool {
	stubbed := map[string]bool{}
	for _, message := range l.messages {
		if message.Role == "tool" && strings.Contains(contentOf(message), "superseded") {
			stubbed[message.ToolCallID] = true
		}
	}
	return stubbed
}

// TestDecayBatchesRewrites is the cache-economics test. A trim-to-budget pass
// retires exactly what the newest result displaced, so once the window is full
// it rewrites an old message on every single turn and the transcript is never
// cached past the fade line. Batching to a low-water mark spends the same
// number of stubs but groups them: one invalidation per K turns instead of K.
//
// The old behaviour is not run, it is counted: a pass that always refills to
// exactly the budget holds the window pinned there, so every turn from the
// first crossing onward displaces something and rewrites. That is turns minus
// the crossing turn. The new count has to come out near turns-over-budget
// divided by the turns of headroom one batch buys.
func TestDecayBatchesRewrites(t *testing.T) {
	const (
		budget = observationBudget
		result = 2 << 10
		turns  = 40
	)
	lowWater := steadyLowWater(budget, result)
	// Turns of headroom each batch buys: the gap it clears, divided by what a
	// turn adds.
	perBatch := (budget - lowWater) / result
	// The turn on which raw output first exceeds the budget, counting from one.
	crossing := budget/result + 1
	// What trimming to exactly the budget every turn would have cost: a rewrite
	// on the crossing turn and on every turn after it.
	trimEveryTurn := turns - crossing + 1

	leaf := newSteadyLeaf()
	rewrites, overBudget := 0, 0
	for turn := 0; turn < turns; turn++ {
		rewrote, wasOver := leaf.turn(t, result, budget)
		if rewrote {
			rewrites++
		}
		if wasOver {
			overBudget++
		}
	}

	if rewrites == 0 {
		t.Fatal("nothing was ever rewritten; decay stopped working")
	}
	if overBudget != rewrites {
		t.Errorf("decay fired on %d turns but %d turns were over budget; it must fire exactly when the window crosses the line, never otherwise", rewrites, overBudget)
	}
	// Batch math: the crossing turn, plus one batch for every perBatch turns
	// after it. Stub bytes left behind by earlier batches count against the
	// window too, which can pull one extra batch forward.
	expected := trimEveryTurn/perBatch + 1
	if rewrites < expected-1 || rewrites > expected+1 {
		t.Errorf("rewrote history on %d of %d turns, want about %d (%d turns over budget / %d turns of headroom per batch)",
			rewrites, turns, expected, trimEveryTurn, perBatch)
	}
	if rewrites*2 > trimEveryTurn {
		t.Errorf("rewrote on %d turns; trimming to the budget every turn would rewrite on all %d, and hysteresis must at least halve that",
			rewrites, trimEveryTurn)
	}
}

// TestDecayHoldsTheBudgetInvariant pins what may not change. The window may
// never sit above the budget once decay has run, whatever the low-water mark
// does; a firing pass must clear down to the mark, give or take the newest
// turn's own output, which is measured against the full budget because the
// model has not read it yet; and nothing that was stubbed may ever be un-stubbed
// or rewritten.
func TestDecayHoldsTheBudgetInvariant(t *testing.T) {
	const (
		budget = observationBudget
		result = 3 << 10
		turns  = 30
	)
	lowWater := steadyLowWater(budget, result)

	leaf := newSteadyLeaf()
	stubbed := map[string]bool{}
	frozen := map[string]string{} // tool_call_id → the stub it was given
	fired := 0
	for turn := 0; turn < turns; turn++ {
		before := leaf.stubbedIDs()
		rewrote, _ := leaf.turn(t, result, budget)
		if rewrote {
			fired++
		}

		live := liveObservationBytes(leaf.messages)
		if live > budget {
			t.Fatalf("turn %d: %d live bytes against a %d-byte budget — decay must never leave the window over budget", turn, live, budget)
		}
		// A firing pass clears to the mark; the newest turn's result is held
		// back from the mark by design, so it is the allowance.
		if rewrote && live > lowWater+result {
			t.Errorf("turn %d: a firing decay left %d live bytes, want at most the %d-byte low-water mark plus the newest result", turn, live, lowWater+result)
		}

		now := leaf.stubbedIDs()
		for id := range before {
			if !now[id] {
				t.Fatalf("turn %d: %s was un-stubbed; decay is write-once", turn, id)
			}
		}
		for _, message := range leaf.messages {
			if message.Role != "tool" || !now[message.ToolCallID] {
				continue
			}
			body := contentOf(message)
			if was, seen := frozen[message.ToolCallID]; seen && was != body {
				t.Fatalf("turn %d: stub for %s changed from %q to %q; a stub is written once and then never touched again", turn, message.ToolCallID, was, body)
			}
			frozen[message.ToolCallID] = body
			stubbed[message.ToolCallID] = true
		}
	}
	if fired == 0 || len(stubbed) == 0 {
		t.Fatalf("decay fired %d times and stubbed %d results; the run never exercised the invariant", fired, len(stubbed))
	}
}

// TestDecayLowWaterRespectsTheFloor checks the two ends of the budget. The
// floor is what keeps a small task's window usable, and the mark has to stay
// under the budget it fired at or the invariant above is unenforceable.
func TestDecayLowWaterRespectsTheFloor(t *testing.T) {
	for _, budget := range []int{observationBudget, observationBudget * 3, observationWindow(200_000)} {
		mark := decayLowWater(budget)
		if mark >= budget {
			t.Errorf("low-water mark %d for budget %d does not retire anything", mark, budget)
		}
		if mark < budget/2 {
			t.Errorf("low-water mark %d for budget %d gives up more than half the window", mark, budget)
		}
	}
	if decayLowWater(observationBudget) != observationBudget*decayLowWaterPercent/100 {
		t.Error("the mark is no longer a fixed fraction of the budget")
	}
	// And the fixed fraction is what a leaf gets only until it has been
	// measured: a decayer nobody has told anything is still on it.
	if newDecayer(nil, nil).lowWater(observationBudget) != decayLowWater(observationBudget) {
		t.Error("an unmeasured leaf no longer falls back to the fixed fraction")
	}
}

// The mark is solved for the denominator, not declared as a fraction of the
// numerator. That is the whole of F3: a quarter of the window was 6.25KB of
// headroom against a measured 3.7KB of inflow per turn, so a batch bought 1.7
// quiet turns and the hysteresis paid its cost in window size while delivering
// almost none of its benefit. What has to hold now is the ratio, at any inflow
// the leaf turns out to have.
func TestDecayLowWaterBuysHeadroomForTheMeasuredInflow(t *testing.T) {
	// A window a catalog-answered leaf would really get, rather than either
	// clamp: the property under test is the mark, and it has to be exercised
	// where the mark is free to move.
	const budget = 128 << 10
	// Inflows whose solved mark lands between the two bounds, which is where
	// the ratio is the thing being tested. 3,700 bytes is the measured median.
	for _, inflow := range []int{2 << 10, 3_700, 8 << 10} {
		mark := steadyLowWater(budget, inflow)
		if mark >= budget {
			t.Fatalf("inflow %d: mark %d retires nothing", inflow, mark)
		}
		turns := (budget - mark) / inflow
		if turns < 6 || turns > 8 {
			t.Errorf("inflow %d: one batch buys %d quiet turns (mark %d of %d), want six to eight",
				inflow, turns, mark, budget)
		}
	}
	// Below the band the ceiling takes over, and it may only err towards more
	// headroom than the ratio asked for — never less, which would be the
	// every-turn rewrite arriving through the measurement instead of through
	// the arithmetic.
	for _, inflow := range []int{64, 512} {
		mark := steadyLowWater(budget, inflow)
		if turns := (budget - mark) / inflow; turns < decayHeadroomTurns {
			t.Errorf("inflow %d: a near-silent leaf was left only %d quiet turns", inflow, turns)
		}
	}
	// Above it the floor takes over, and it errs the other way because it must:
	// a leaf adding a quarter of the whole window every turn cannot be given
	// seven turns of headroom by any mark. What the floor guarantees instead is
	// that one batch never gives up more than half the window, so a fire stays
	// a batch rather than becoming a reset.
	for _, inflow := range []int{32 << 10, budget} {
		if mark := steadyLowWater(budget, inflow); mark < budget/2 {
			t.Errorf("inflow %d: one fire retired down to %d of %d — that is a reset, not a batch",
				inflow, mark, budget)
		}
	}
	// Both bounds are about measurement rather than policy. A leaf adding more
	// than the whole window per turn must not solve for a full reset, and a leaf
	// whose sampled turns were near-silent must not solve for a mark so close to
	// the budget that the next ordinary result crosses it again.
	if mark := steadyLowWater(budget, budget); mark != budget*decayHeadroomFloorPercent/100 {
		t.Errorf("an enormous inflow solved for %d, want the %d%% floor", mark, decayHeadroomFloorPercent)
	}
	if mark := steadyLowWater(budget, 1); mark != budget*decayHeadroomCeilingPercent/100 {
		t.Errorf("a near-silent leaf solved for %d, want the %d%% ceiling", mark, decayHeadroomCeilingPercent)
	}
}

// The window is sized from what the model can hold and from nothing else.
//
// It used to be maxTokens/6 — a share of the leaf's cumulative *spend* ceiling,
// which is not a quantity that fits in a request. At the default budget that
// arithmetic produced a 25KB window, smaller than the single 26KB file the leaf
// was re-reading all run, in front of a model with hundreds of thousands of
// tokens of room. The category error is what this pins shut.
func TestObservationWindowIsSizedFromContextNotSpend(t *testing.T) {
	// The measured re-read subject: whatever else changes, one ordinary
	// document has to fit inside the memory meant to hold it.
	const reReadSubject = 26 << 10
	// What the old arithmetic gave the default leaf budget.
	const spendSizedWindow = defaultLeafTokens / 6

	unknown := observationWindow(0)
	if unknown < reReadSubject {
		t.Fatalf("with no catalog answer the window is %d bytes, smaller than the %d-byte subject a leaf re-reads",
			unknown, reReadSubject)
	}
	if unknown <= spendSizedWindow {
		t.Fatalf("the default window is %d bytes, no better than the %d the spend ceiling used to buy",
			unknown, spendSizedWindow)
	}

	// And no further than that. Not knowing a context length is not evidence
	// that it is large — a small-context model is exactly the kind a catalog
	// fails to recognise — so the unknown case buys back a little room and stops
	// well short of what a frontier model would be given.
	if unknown > 64<<10 {
		t.Fatalf("the no-answer default is %d bytes; an unknown model must not be treated as a frontier one", unknown)
	}

	// A real context is where the room actually comes from, and there is no
	// longer a ceiling standing between a model and the memory it can hold.
	roomy := observationWindow(200_000)
	if roomy < 2*reReadSubject {
		t.Errorf("a 200k-token model got %d bytes, not even two of the %d-byte subjects it re-reads",
			roomy, reReadSubject)
	}
	// A genuinely small model gets the formula's own answer, small. Clamping it
	// up to the unknown-case floor would size a prompt past what the model
	// accepts in order to look generous, and the failure would arrive as a
	// provider rejection rather than as a shorter memory.
	tiny := observationWindow(8_000)
	if tiny >= observationBudget {
		t.Errorf("an 8k-token model got %d bytes, clamped up to the %d unknown-case floor", tiny, observationBudget)
	}
	if tiny != minObservationBudget {
		t.Errorf("an 8k-token model got %d bytes, want the %d arithmetic minimum", tiny, minObservationBudget)
	}
	// Whatever the context, the window leaves room for the turn itself: the
	// bytes it may carry must fit inside the share of the model's context the
	// fill law permits filling, with the fixed floor and the completion reserve
	// already taken out of it.
	for _, context := range []int{64_000, 128_000, 200_000, 400_000, 1 << 20} {
		window := observationWindow(context)
		if window == minObservationBudget {
			// The arithmetic minimum rather than the law's answer: the floor and
			// the reserve have already eaten this model's whole context, and
			// what is under test here is the law.
			continue
		}
		permitted := context*ctxbudget.FillPercent()/100 - observationFixedFloorTokens - ctxbudget.CompletionReserve()
		if tokens := window / observationBytesPerToken; tokens > permitted {
			t.Errorf("a %d-token model got a window of about %d tokens, past the %d the fill law permits",
				context, tokens, permitted)
		}
	}
}

// The deleted ceiling, pinned as an absence.
//
// 64KB used to be the most memory any model could be given, whatever it held.
// It was the last absolute byte number in the sizing and it made every
// long-context model identical to every other: a 200k model and a 2M model were
// handed the same window and the catalog's answer stopped mattering above about
// 600k tokens. The law now is that a cap is a fraction of the model's own
// context or it does not exist.
func TestTheObservationWindowHasNoAbsoluteCeiling(t *testing.T) {
	const deletedCeiling = 64 << 10
	million := observationWindow(1 << 20)
	if million <= deletedCeiling {
		t.Fatalf("a 1M-token model got %d bytes; the %d ceiling is back", million, deletedCeiling)
	}
	// And it keeps going: the window follows the model rather than converging on
	// one number, which is the whole difference between a share and a clamp.
	last := 0
	for _, context := range []int{200_000, 600_000, 1 << 20, 2_000_000, 10_000_000} {
		window := observationWindow(context)
		if window <= last {
			t.Fatalf("a %d-token model got %d bytes, no more than the smaller model's %d",
				context, window, last)
		}
		last = window
	}
}

// The same bytes are carried once and pointed at afterwards. The pointer is a
// line rather than a copy, and it names where the material can be read, so a
// model that needs the detail can still reach it.
func TestRepeatedBytesBecomeAPointerToTheFirstCopy(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "2", nil)
	fade := newDecayer(map[string]string{}, tools.decaySpill)
	carried := newObservations(fade)
	body := strings.Repeat("the same output\n", 200)

	first := carried.admit(1, call("c1", "sh", `{"cmd":"cat notes.md"}`), Result{Content: body})
	if first != body {
		t.Fatalf("the first copy of some bytes was not carried in full: %q", clipForTest(first))
	}
	// Nothing is written for a body that has only appeared once. The file
	// arrives with the first repeat, not with the first copy.
	if _, err := os.Stat(filepath.Join(space.Root(), obsDir)); err == nil {
		t.Fatal("a body that never repeated was written to disk anyway")
	}

	second := carried.admit(4, call("c9", "sh", `{"cmd":"cat notes.md"}`), Result{Content: body})
	if second == body {
		t.Fatal("the same bytes were carried a second time")
	}
	if !strings.Contains(second, "turn 1") || !strings.Contains(second, "sh {\"cmd\":\"cat notes.md\"}") {
		t.Fatalf("the pointer does not say which earlier result it stands for: %q", second)
	}
	if len(second) >= len(body)/4 {
		t.Fatalf("the pointer is %d bytes against a %d-byte body; it has to be a line", len(second), len(body))
	}
	// Every pointer names a durable address, even while the target is still
	// quoted above it — see the dangling-pointer test below for why.
	wantPath := filepath.Join(obsDir, "2-decay-c1.txt")
	if !strings.Contains(second, wantPath) {
		t.Fatalf("the pointer names no durable address: %q", second)
	}
	if on, err := os.ReadFile(filepath.Join(space.Root(), wantPath)); err != nil || string(on) != body {
		t.Fatalf("the address the pointer names holds %d bytes, err=%v", len(on), err)
	}

	// A third copy points back at the original rather than at the pointer, so
	// the chain never grows a hop.
	third := carried.admit(9, call("c11", "sh", `{"cmd":"sed -n 1,400p notes.md"}`), Result{Content: body})
	if !strings.Contains(third, "turn 1") {
		t.Fatalf("a third copy pointed somewhere other than the original: %q", third)
	}
}

// Decay is running underneath this, so the sentence changes once the target has
// been stubbed: the bytes are no longer above, they are only in the file. The
// address itself does not change, because it was fixed before any pointer
// existed.
func TestAPointerNamesTheSpillFileOnceTheOriginalHasDecayed(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "2", nil)
	fade := newDecayer(map[string]string{}, tools.decaySpill)
	carried := newObservations(fade)
	body := strings.Repeat("A", 20<<10)
	fresh := strings.Repeat("B", 20<<10)

	original := call("c7", "sh", `{"cmd":"cat big.txt"}`)
	fade.labels[original.ID] = callLabel(original)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: original.ID, Content: text(carried.admit(1, original, Result{Content: body}))},
		{Role: "tool", ToolCallID: "c8", Content: text(fresh)},
	}
	if decayed := fade.decay(messages, observationBudget); decayed != 1 {
		t.Fatalf("decayed = %d, want the older result stubbed", decayed)
	}

	pointer := carried.admit(6, call("c12", "sh", `{"cmd":"cat big.txt"}`), Result{Content: body})
	wantPath := filepath.Join(obsDir, "2-decay-c7.txt")
	if !strings.Contains(pointer, wantPath) {
		t.Fatalf("the pointer does not send the model to where the bytes went: %q", pointer)
	}
	if strings.Contains(pointer, "read it above") {
		t.Fatalf("the pointer still sends the model to a stub: %q", pointer)
	}
	if strings.Contains(pointer, body[:64]) {
		t.Fatal("the pointer carried the body it was meant to replace")
	}
	// The stub that replaced the target names the same file the pointer does.
	if stub := contentOf(messages[0]); !strings.Contains(stub, wantPath) {
		t.Fatalf("the stub and the pointer disagree about where the bytes are: %q", stub)
	}
	on, err := os.ReadFile(filepath.Join(space.Root(), wantPath))
	if err != nil || string(on) != body {
		t.Fatalf("the spill file the pointer names holds %d bytes, err=%v", len(on), err)
	}
}

// countingSpill is a spill function that can be made to fail, and remembers how
// often it was actually asked to write.
type countingSpill struct {
	inner  spillFunc
	writes int
	broken bool
}

func (s *countingSpill) fn(key, body string) (string, bool) {
	s.writes++
	if s.broken {
		return "", false
	}
	return s.inner(key, body)
}

// The invariant the whole mechanism rests on: a pointer already in the
// transcript can never be left aimed at bytes nobody can read.
//
// The tempting implementation checks reachability as the pointer is written,
// which is a present-tense check standing in for a promise about the future. A
// target that is live when the pointer is written decays three turns later, and
// if the write that decay attempts happens to fail — a full disk, a directory
// that went away — the target becomes a pathless tombstone with pointers
// already aimed at it. Nothing revisits a pointer, so the model is simply sent
// to read something that is not there.
//
// Preserving before pointing removes the failure rather than narrowing it: the
// address exists before any pointer names it, and the decay pass reuses that
// address instead of writing its own. This forces exactly the failure the
// present-tense check could not survive.
func TestADecayTimeWriteFailureCannotStrandAPointer(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "2", nil)
	spill := &countingSpill{inner: tools.decaySpill}
	fade := newDecayer(map[string]string{}, spill.fn)
	carried := newObservations(fade)
	body := strings.Repeat("A", 20<<10)
	fresh := strings.Repeat("B", 20<<10)

	original := call("c7", "sh", `{"cmd":"cat big.txt"}`)
	fade.labels[original.ID] = callLabel(original)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: original.ID, Content: text(carried.admit(1, original, Result{Content: body}))},
		{Role: "tool", ToolCallID: "c8", Content: text(fresh)},
	}
	// The pointer is written while the target is still live and quoted above.
	pointer := carried.admit(2, call("c9", "sh", `{"cmd":"cat big.txt"}`), Result{Content: body})
	if !strings.Contains(pointer, "identical to the result of") {
		t.Fatalf("the repeat was not pointed at all: %q", clipForTest(pointer))
	}
	wantPath := filepath.Join(obsDir, "2-decay-c7.txt")
	if !strings.Contains(pointer, wantPath) {
		t.Fatalf("the pointer named no durable address while its target was live: %q", pointer)
	}

	// Now the workspace stops accepting writes, and only then does decay come
	// for the pointer's target.
	spill.broken = true
	writesBefore := spill.writes
	if decayed := fade.decay(messages, observationBudget); decayed != 1 {
		t.Fatalf("decayed = %d, want the older result stubbed", decayed)
	}
	if spill.writes != writesBefore {
		t.Errorf("decay tried to write %d more times; a preserved target must be reused, not rewritten",
			spill.writes-writesBefore)
	}

	stub := contentOf(messages[0])
	if !strings.Contains(stub, wantPath) {
		t.Fatalf("a decay-time write failure left the target pathless while a pointer named it: %q", stub)
	}
	on, err := os.ReadFile(filepath.Join(space.Root(), wantPath))
	if err != nil || string(on) != body {
		t.Fatalf("the bytes the pointer promised are unreadable: %d bytes, err=%v", len(on), err)
	}
}

// The other half of the same rule. When no durable address can be made at all,
// no pointer is emitted and the bytes are carried again — the saving is
// forfeited, which is the cheap failure, rather than a model being sent to read
// something that was never written.
func TestBytesWithNoDurableAddressAreNeverPointedAt(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "4", nil)
	spill := &countingSpill{inner: tools.decaySpill, broken: true}
	carried := newObservations(newDecayer(map[string]string{}, spill.fn))
	body := strings.Repeat("C", 20<<10)

	first := carried.admit(1, call("c1", "sh", `{"cmd":"cat gone.txt"}`), Result{Content: body})
	if first != body {
		t.Fatalf("the first copy was not carried in full: %q", clipForTest(first))
	}
	for turn, id := range map[int]string{5: "c3", 7: "c4"} {
		again := carried.admit(turn, call(id, "sh", `{"cmd":"cat gone.txt"}`), Result{Content: body})
		if again != body {
			t.Fatalf("bytes with no durable address were pointed at anyway: %q", clipForTest(again))
		}
	}
	if spill.writes == 0 {
		t.Error("no attempt was ever made to give the repeated bytes an address")
	}
	// And a decayer with no filesystem at all behaves the same way rather than
	// pointing into thin air.
	nowhere := newObservations(newDecayer(map[string]string{}, nil))
	nowhere.admit(1, call("d1", "sh", `{"cmd":"cat x"}`), Result{Content: body})
	if again := nowhere.admit(2, call("d2", "sh", `{"cmd":"cat x"}`), Result{Content: body}); again != body {
		t.Fatalf("a leaf with no filesystem emitted a pointer: %q", clipForTest(again))
	}
}

// Three kinds of result are never content-addressed, and each for its own
// reason. An error's whole value is being read where it happened; a background
// job report describes state that was true when it was written; and a result
// carrying multimodal follow-up content is not its text at all.
func TestErrorsJobReportsAndMultimodalResultsAreNeverPointedAt(t *testing.T) {
	tools := NewToolbox(workspace(t), "3", nil)
	carried := newObservations(newDecayer(map[string]string{}, tools.decaySpill))
	body := strings.Repeat("no such file or directory\n", 100)

	failing := Result{Content: body, IsError: true}
	if first, second := carried.admit(1, call("e1", "sh", `{"cmd":"cat x"}`), failing),
		carried.admit(2, call("e2", "sh", `{"cmd":"cat x"}`), failing); first != second {
		t.Fatalf("a repeated error was replaced by a pointer: %q", clipForTest(second))
	}

	report := Result{Content: body, reportedJobs: true}
	if first, second := carried.admit(1, call("j1", "job", `{}`), report),
		carried.admit(2, call("j2", "job", `{}`), report); first != second {
		t.Fatalf("a repeated job report was replaced by a pointer: %q", clipForTest(second))
	}

	looked := Result{Content: body, Followup: []ai.ContentPart{{Type: "image_url"}}}
	if first, second := carried.admit(1, call("v1", "view_image", `{"path":"a.png"}`), looked),
		carried.admit(2, call("v2", "view_image", `{"path":"a.png"}`), looked); first != second {
		t.Fatalf("a repeated image result was replaced by a pointer: %q", clipForTest(second))
	}
}

func clipForTest(body string) string {
	if len(body) <= 120 {
		return body
	}
	return body[:120] + "…"
}

// TestDecayStubsAnOversizedResult is the degenerate case, unchanged by
// batching: one result larger than the entire budget cannot be kept by any
// mark, so it is stubbed on its own — and, being alone, it still leaves the
// window as small as it can be made.
func TestDecayStubsAnOversizedResult(t *testing.T) {
	huge := strings.Repeat("q", observationBudget*2)
	messages := []ai.Message{
		{Role: "user", Content: text("do the thing")},
		{Role: "assistant", Content: text("looking"), ToolCalls: []ai.ToolCall{{ID: "big"}}},
		{Role: "tool", ToolCallID: "big", Content: text(huge)},
	}
	fade := newDecayer(map[string]string{"big": "sh cat huge"}, nil)

	if decayed := fade.decay(messages, observationBudget); decayed != 1 {
		t.Fatalf("decayed = %d, want the one oversized result stubbed", decayed)
	}
	stub := contentOf(messages[2])
	if !strings.Contains(stub, "sh cat huge") || !strings.Contains(stub, "superseded") {
		t.Errorf("stub %q does not name what was retired", stub)
	}
	if again := fade.decay(messages, observationBudget); again != 0 {
		t.Errorf("a second pass decayed %d more; the stub must be left alone", again)
	}
	if contentOf(messages[2]) != stub {
		t.Error("the stub was rewritten on a later pass")
	}
}

// TestDecayPreservesPairing guards the one hard constraint. Every tool message
// answers an assistant tool_call by id; dropping one, or losing its id, is a
// provider-level rejection rather than a degraded prompt. Decay may only ever
// shorten content.
func TestDecayPreservesPairing(t *testing.T) {
	big := strings.Repeat("x", 20<<10)
	messages := []ai.Message{
		{Role: "system", Content: text("rules")},
		{Role: "user", Content: text("do the thing")},
	}
	labels := map[string]string{}
	for turn := 0; turn < 5; turn++ {
		id := string(rune('a' + turn))
		call := ai.ToolCall{ID: id, Type: "function",
			Function: ai.ToolCallFunction{Name: "sh", Arguments: `{"cmd":"ls"}`}}
		messages = append(messages,
			ai.Message{Role: "assistant", Content: text("thinking " + id), ToolCalls: []ai.ToolCall{call}},
			ai.Message{Role: "tool", ToolCallID: id, Content: text(big)},
		)
		labels[id] = callLabel(call)
	}
	before := len(messages)

	decayed := newDecayer(labels, nil).decay(messages, observationBudget)

	if len(messages) != before {
		t.Fatalf("message count changed from %d to %d — decay must never remove a message", before, len(messages))
	}
	if decayed == 0 {
		t.Fatal("nothing decayed despite 100KB of observations against a 24KB budget")
	}

	var toolMessages, stubbed, kept int
	for _, message := range messages {
		switch message.Role {
		case "assistant":
			if !strings.HasPrefix(contentOf(message), "thinking ") {
				t.Error("an assistant message was modified; those are the compressed state and must survive")
			}
		case "tool":
			toolMessages++
			if message.ToolCallID == "" {
				t.Error("a tool message lost its ToolCallID and can no longer be paired")
			}
			if strings.Contains(contentOf(message), "superseded") {
				stubbed++
			} else {
				kept++
			}
		}
	}
	if toolMessages != 5 {
		t.Errorf("tool messages = %d, want 5", toolMessages)
	}
	if kept == 0 {
		t.Error("everything was stubbed; the newest observations must survive in full")
	}
	if stubbed == 0 {
		t.Error("nothing was stubbed despite exceeding the budget")
	}
}

// TestDecayKeepsNewestInFull checks the direction of the walk. The most recent
// observation is the one the model is actually reasoning about.
func TestDecayKeepsNewestInFull(t *testing.T) {
	big := strings.Repeat("y", 20<<10)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: "old", Content: text(big)},
		{Role: "tool", ToolCallID: "new", Content: text(big)},
	}
	newDecayer(map[string]string{"old": "sh ls", "new": "sh pwd"}, nil).decay(messages, observationBudget)

	if strings.Contains(contentOf(messages[1]), "superseded") {
		t.Error("the newest observation was stubbed")
	}
	if !strings.Contains(contentOf(messages[0]), "superseded") {
		t.Error("the oldest observation survived past the budget")
	}
	if !strings.Contains(contentOf(messages[0]), "sh ls") {
		t.Error("the stub does not say what the result was, so the model cannot tell it already ran that")
	}
}

// TestDecayLeavesSmallResultsAlone confirms the budget is spent on what costs.
// Many small observations should all survive; stubbing them would lose detail
// for no saving.
func TestDecayLeavesSmallResultsAlone(t *testing.T) {
	var messages []ai.Message
	for index := 0; index < 30; index++ {
		messages = append(messages, ai.Message{
			Role: "tool", ToolCallID: string(rune('a' + index)), Content: text("ok"),
		})
	}
	if decayed := newDecayer(nil, nil).decay(messages, observationBudget); decayed != 0 {
		t.Errorf("%d small results were stubbed; they cost nothing to keep", decayed)
	}
}

// TestDecaySpillsLosslessly is the decay-to-pointer contract: a stubbed result
// must name the file its bytes went to, and that file must hold the original
// bytes exactly — decay defers detail, it never destroys it.
func TestDecaySpillsLosslessly(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "2", nil)
	old := strings.Repeat("A", 20<<10)
	fresh := strings.Repeat("B", 20<<10)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: "c7", Content: text(old)},
		{Role: "tool", ToolCallID: "c8", Content: text(fresh)},
	}
	labels := map[string]string{"c7": "sh cat foo", "c8": "sh cat bar"}

	decayed := newDecayer(labels, tools.decaySpill).decay(messages, observationBudget)
	if decayed != 1 {
		t.Fatalf("decayed = %d, want exactly the older result", decayed)
	}

	stub := contentOf(messages[0])
	if !strings.Contains(stub, "sh cat foo") || !strings.Contains(stub, "20480 bytes") {
		t.Errorf("stub %q does not identify what was decayed", stub)
	}
	wantPath := filepath.Join(obsDir, "2-decay-c7.txt")
	if !strings.Contains(stub, "spilled to "+wantPath) {
		t.Fatalf("stub %q does not carry the spill path %q", stub, wantPath)
	}
	body, err := os.ReadFile(filepath.Join(space.Root(), wantPath))
	if err != nil {
		t.Fatalf("the spill file the stub points at cannot be read: %v", err)
	}
	if string(body) != old {
		t.Errorf("spill file holds %d bytes, want the original %d untouched", len(body), len(old))
	}
	if contentOf(messages[1]) != fresh {
		t.Error("the newest observation was touched; only older ones may decay")
	}
}

// TestDecayIsIdempotentAcrossTurns guards the every-turn re-run. Decay walks
// the same transcript before each model call; the second pass must not write
// the file again, grow the stub, or stub the stub.
func TestDecayIsIdempotentAcrossTurns(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "4", nil)
	big := strings.Repeat("z", 20<<10)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: "a1", Content: text(big)},
		{Role: "tool", ToolCallID: "a2", Content: text(big)},
	}
	fade := newDecayer(map[string]string{"a1": "sh ls", "a2": "sh pwd"}, tools.decaySpill)

	if first := fade.decay(messages, observationBudget); first != 1 {
		t.Fatalf("first pass decayed %d, want 1", first)
	}
	stub := contentOf(messages[0])
	path := filepath.Join(space.Root(), obsDir, "4-decay-a1.txt")
	written, err := os.Stat(path)
	if err != nil {
		t.Fatalf("spill file missing after first pass: %v", err)
	}

	for pass := 0; pass < 3; pass++ {
		if again := fade.decay(messages, observationBudget); again != 0 {
			t.Fatalf("repeat pass decayed %d more results; the same result must not decay twice", again)
		}
	}
	if got := contentOf(messages[0]); got != stub {
		t.Errorf("stub changed across passes:\n first %q\n later %q", stub, got)
	}
	rewritten, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !rewritten.ModTime().Equal(written.ModTime()) || rewritten.Size() != written.Size() {
		t.Error("the spill file was rewritten on a later pass; spilling must happen once")
	}
	entries, err := os.ReadDir(filepath.Join(space.Root(), obsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("obs dir holds %v, want exactly one spill file", names)
	}
}

// TestSpillLeavesReadableFile checks the other half of the context fix: a large
// result must be recoverable, or bounding it would be data loss rather than
// deferral.
func TestSpillLeavesReadableFile(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "3", nil)

	result := tools.Execute(t.Context(), "sh", `{"cmd":"printf 'LINE%s\\n' 1 2 3 4 5 6 7 8 9 10 | awk '{for(i=0;i<200;i++) print}'"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}
	if len(result.Content) > spillBytes+512 {
		t.Errorf("spilled result is still %d bytes in context", len(result.Content))
	}
	if !strings.Contains(result.Content, obsDir) {
		t.Fatalf("spilled result does not say where the full output went: %q", result.Content)
	}

	read := tools.Execute(t.Context(), "sh", `{"cmd":"wc -c .obs/3-1.txt"}`)
	if read.IsError {
		t.Errorf("the spilled file could not be read back: %s", read.Content)
	}
}
