package resident

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// hangingCompile is the provider that caused the incident: it accepts the call
// and never answers. It returns only when somebody above it stops waiting,
// which is precisely the behaviour there was previously no defence against.
func hangingCompile(attempts *atomic.Int64) CompileFunc {
	return func(ctx context.Context, _, _ string) (Compiled, error) {
		if attempts != nil {
			attempts.Add(1)
		}
		<-ctx.Done()
		return Compiled{}, ctx.Err()
	}
}

// TestAStalledCommandStrikesAndTheQueueBehindItProceeds is the incident, in a
// test. One command's model call never returns; every later command used to
// queue behind it permanently.
func TestAStalledCommandStrikesAndTheQueueBehindItProceeds(t *testing.T) {
	graph := openStore(t)
	stalling, err := graph.RequestCommand(store.Command{
		SessionID:   "session-stall",
		Kind:        store.CommandSplice,
		Instruction: "Plan something that will never come back",
	})
	if err != nil {
		t.Fatalf("request stalling command: %v", err)
	}
	// A reflex splice is not an independent command, so it is applied on the
	// serial path strictly after the one above it — which is exactly the
	// position that used to be unreachable.
	behind, err := graph.RequestCommand(store.Command{
		SessionID:   "session-stall",
		Kind:        store.CommandSplice,
		Reflex:      true,
		Instruction: "Answer the quick thing",
	})
	if err != nil {
		t.Fatalf("request following command: %v", err)
	}

	var attempts atomic.Int64
	reconciler := New(graph, hangingCompile(&attempts), nil)
	reconciler.commandWall = 50 * time.Millisecond

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// The stalled command did not fail. It did not settle at all.
	stalled := commandBySeq(t, graph, stalling.Seq)
	if stalled.Status != store.CommandPending {
		t.Fatalf("a first stall settled the command: %+v", stalled)
	}
	if got := reconciler.commandStrikes[stalling.Seq]; got != 1 {
		t.Fatalf("strikes after one stall = %d, want 1", got)
	}

	// And the command behind it ran anyway, on the same pass.
	followed := commandBySeq(t, graph, behind.Seq)
	if followed.Status != store.CommandApplied {
		t.Fatalf("the command behind the stall did not apply: %+v", followed)
	}

	// A second tick is the retry, and it is a real one: the compiler is called
	// again rather than the first refusal being remembered as an answer.
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("compile attempts = %d, want 2 — the retry never happened", got)
	}

	settled := commandBySeq(t, graph, stalling.Seq)
	if settled.Status != store.CommandRejected {
		t.Fatalf("a second stall did not settle the command: %+v", settled)
	}
	if _, held := reconciler.commandStrikes[stalling.Seq]; held {
		t.Fatal("a settled command is still carrying strikes")
	}

	// The refusal speaks. A rejection is the one receipt that must reach the
	// person whoever else spoke first, because the head has already said "on it".
	receipt := settlementLine(t, graph, stalling.Seq)
	if receipt.Role != store.RoleAgent {
		t.Fatalf("the stall receipt was filed as %s rather than spoken", receipt.Role)
	}
	if receipt.Body != stalledReceipt {
		t.Fatalf("stall receipt = %q, want %q", receipt.Body, stalledReceipt)
	}
	for _, line := range []string{"the model stopped answering twice", "try again"} {
		if !strings.Contains(receipt.Body, line) {
			t.Errorf("stall receipt does not say %q: %q", line, receipt.Body)
		}
	}
}

// TestAStalledCommandSaysSoOnTheCard proves the wait is legible while it is
// still a wait. A card that pulses with no phase line is the shape of the bug.
func TestAStalledCommandSaysSoOnTheCard(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-stall-row",
		Kind:        store.CommandSplice,
		Instruction: "Something slow",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	reconciler := New(graph, hangingCompile(nil), nil)
	reconciler.commandWall = 50 * time.Millisecond
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	rows := stageRows(t, graph, command.Seq)
	latest, ok := rows[stageStalled]
	if !ok {
		t.Fatalf("no row said the command stalled: %+v", rows)
	}
	if !strings.Contains(latest, "trying again") {
		t.Fatalf("the stall row does not say what happens next: %q", latest)
	}
}

// TestACancelledTickDoesNotStrikeItsCommands separates a stall from a
// shutdown. When the process is going away every command in flight reports a
// deadline, and counting those as provider failures would burn a real
// command's strikes on the way out — and would make an orderly exit
// indistinguishable, in the strike map, from a wedged provider.
//
// What a cancelled tick then does with the command is the settlement path this
// change did not touch, and it is deliberately not asserted here.
func TestACancelledTickDoesNotStrikeItsCommands(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-shutdown",
		Kind:        store.CommandSplice,
		Instruction: "Something interrupted",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	reconciler := New(graph, hangingCompile(nil), nil)
	// Long enough that the command wall is certainly not what ends this.
	reconciler.commandWall = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_ = reconciler.Tick(ctx)

	if got := reconciler.commandStrikes[command.Seq]; got != 0 {
		t.Fatalf("a cancelled tick struck its command %d times", got)
	}
}

// TestAHappyPathSpliceLeavesNoSilentPhase walks the whole life of a pending
// command and insists every boundary said so. The compile, the plan and the
// graph write are three round-trips a person waits through; two of the three
// used to report nothing at all.
func TestAHappyPathSpliceLeavesNoSilentPhase(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-phases",
		Kind:        store.CommandSplice,
		Instruction: "Benchmark the parser",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Goal: "Benchmark the parser and keep its output", Scale: "project"}, nil
	}
	planned := func(context.Context, Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "bench", Brief: "Run the benchmark", Stage: 1},
			{ID: "report", Parent: "bench", Brief: "Write it up", Stage: 2},
		}}, nil
	}

	if err := New(graph, compile, planned).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if settled := commandBySeq(t, graph, command.Seq); settled.Status != store.CommandApplied {
		t.Fatalf("splice did not apply: %+v", settled)
	}

	rows := stageRows(t, graph, command.Seq)
	for _, phase := range []string{stageReading, stagePlanning, stageStarting} {
		if _, ok := rows[phase]; !ok {
			t.Errorf("phase %q was silent: rows = %+v", phase, rows)
		}
	}
	// The reading is the half of the compile a person can check, so the phase
	// has to carry it rather than merely having happened.
	if reading := rows[stageReading]; !strings.Contains(reading, "Benchmark the parser and keep its output") {
		t.Errorf("the reading row does not carry the reading: %q", reading)
	}
	// And the last row says what actually landed.
	if starting := rows[stageStarting]; starting != "2 steps" {
		t.Errorf("the starting row = %q, want the step count", starting)
	}
}

// TestPhaseRowsAreRecordedNotSpoken keeps the stage rows on the side of the
// wall the whole progress mechanism already lives on: the job's record draws
// every phase in order, and the conversation never sees one.
func TestPhaseRowsAreRecordedNotSpoken(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-voice",
		Kind:        store.CommandSplice,
		Instruction: "Do the thing",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	messages, err := graph.Messages("", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	anchor := commandPlanAnchor(command)
	seen := 0
	for _, message := range messages {
		if message.Progress == nil || message.CommandSeq != command.Seq {
			continue
		}
		seen++
		if message.Role != store.RoleSystem {
			t.Errorf("phase row %q spoke as %s", message.Body, message.Role)
		}
		if message.SessionID != "" {
			t.Errorf("phase row %q reached the conversation", message.Body)
		}
		if message.NodeID != anchor {
			t.Errorf("phase row %q anchored to %q, want %q", message.Body, message.NodeID, anchor)
		}
		if message.Body != message.Progress.Phase {
			t.Errorf("phase row body %q disagrees with its payload %q", message.Body, message.Progress.Phase)
		}
	}
	if seen == 0 {
		t.Fatal("a splice posted no phase rows at all")
	}
}

// settlementLine finds the one settlement line for a command, skipping the
// phase rows that share its seq.
func settlementLine(t *testing.T, graph *store.Store, commandSeq int64) store.Message {
	t.Helper()
	messages, err := graph.Messages("", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	for _, message := range messages {
		if message.CommandSeq == commandSeq && message.Progress == nil {
			return message
		}
	}
	t.Fatalf("no receipt for command %d", commandSeq)
	return store.Message{}
}

// stageRows collects the last Latest line each phase reported for one command.
func stageRows(t *testing.T, graph *store.Store, commandSeq int64) map[string]string {
	t.Helper()
	messages, err := graph.Messages("", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	rows := map[string]string{}
	for _, message := range messages {
		if message.CommandSeq != commandSeq || message.Progress == nil {
			continue
		}
		// A later row for the same phase replaces an earlier one only when it
		// has something to add; the opening row of a phase carries no latest.
		if existing, ok := rows[message.Progress.Phase]; ok && message.Progress.Latest == "" && existing != "" {
			continue
		}
		rows[message.Progress.Phase] = message.Progress.Latest
	}
	return rows
}
