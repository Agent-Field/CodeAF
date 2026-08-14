package steploop

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
)

// completedPart builds a ToolPart with a completed tool call for testing.
func completedPart(tool, callID, input, output string) msgmodel.ToolPart {
	return msgmodel.ToolPart{
		PartBase: msgmodel.PartBase{ID: "p1", SessionID: "ses_1", MessageID: "m1"},
		Type:     msgmodel.PartTypeTool,
		CallID:   callID,
		Tool:     tool,
		State: msgmodel.CompletedToolState(
			msgmodel.RawObject(input),
			output,
			"file",
			nil, 1, 2, nil,
		),
	}
}

// The floor concludes a fidgeting leaf — productive just often enough to
// dodge the repeat and stagnant caps — and leaves a working leaf alone.
func TestStepGuardFloorReadsTheWindow(t *testing.T) {
	// A working leaf — a write every step — crosses the floor untouched.
	working := newStepGuard()
	for i := range stepTurnFloor + 5 {
		parts := msgmodel.Parts{
			completedPart("write", "call_1",
				`{"filePath":"/work/f-`+stepItoa(i)+`.txt","content":"x"}`, "ok"),
		}
		if v := working.observe(parts); v != stepProgress {
			t.Fatalf("working leaf: observe returned %d at step %d, want progress", v, i)
		}
	}

	// A fidgeting leaf: one write every six steps, flat re-reads between.
	g := newStepGuard()
	flat := func(name string) msgmodel.Parts {
		return msgmodel.Parts{
			completedPart("read", "call_1", `{"filePath":"/work/`+name+`.txt"}`, name+"-content"),
		}
	}
	step := 0
	for step < stepTurnFloor-1 {
		write := msgmodel.Parts{
			completedPart("write", "call_1",
				`{"filePath":"/work/g-`+stepItoa(step)+`.txt","content":"x"}`, "ok"),
		}
		if v := g.observe(write); v != stepProgress {
			t.Fatalf("fidgeting leaf: observe returned %d at step %d, want progress", v, step)
		}
		step++
		for k := 0; k < 5 && step < stepTurnFloor-1; k++ {
			name := "flat-a"
			if k%2 == 1 {
				name = "flat-b"
			}
			if v := g.observe(flat(name)); v != stepProgress {
				t.Fatalf("fidgeting leaf: observe returned %d at step %d, want progress", v, step)
			}
			step++
		}
	}
	final := msgmodel.Parts{
		completedPart("write", "call_1", `{"filePath":"/work/final.txt","content":"x"}`, "ok"),
	}
	if v := g.observe(final); v != stepConclude {
		t.Fatalf("fidgeting leaf: observe returned %d at the floor, want conclude", v)
	}
}

func TestStepGuardRepeatSignal(t *testing.T) {
	g := newStepGuard()
	parts := msgmodel.Parts{
		completedPart("read", "call_1", `{"filePath":"/work/data.txt"}`, "file content here"),
	}
	for i := range stepRepeatCap - 1 {
		if v := g.observe(parts); v != stepProgress {
			t.Fatalf("observe returned %d at iteration %d, want progress", v, i)
		}
	}
	if v := g.observe(parts); v != stepConclude {
		t.Fatalf("observe returned %d at the cap, want conclude", v)
	}
}

// A guard that sees several steps with no writes and no new output must fire
// the stagnant signal.
func TestStepGuardStagnantSignal(t *testing.T) {
	g := newStepGuard()
	for i := range stepStagnantCap {
		parts := msgmodel.Parts{
			completedPart("read", "call_1",
				`{"filePath":"/work/file-`+stepItoa(i)+`.txt"}`, "same output"),
		}
		if v := g.observe(parts); v != stepProgress {
			t.Fatalf("observe returned %d at iteration %d, want progress", v, i)
		}
	}
	parts := msgmodel.Parts{
		completedPart("read", "call_1", `{"filePath":"/work/file-99.txt"}`, "same output"),
	}
	if v := g.observe(parts); v != stepConclude {
		t.Fatalf("observe returned %d at the stagnant cap, want conclude", v)
	}
}

// A write tool resets the stagnant counter.
func TestStepGuardWriteResetsStagnant(t *testing.T) {
	g := newStepGuard()
	for i := range stepStagnantCap {
		parts := msgmodel.Parts{
			completedPart("read", "call_1",
				`{"filePath":"/work/f-`+stepItoa(i)+`.txt"}`, "same"),
		}
		g.observe(parts)
	}
	writeParts := msgmodel.Parts{
		completedPart("write", "call_w", `{"filePath":"/work/out.txt","content":"x"}`, "wrote"),
	}
	if v := g.observe(writeParts); v != stepProgress {
		t.Fatalf("observe returned %d after a write, want progress", v)
	}
	readParts := msgmodel.Parts{
		completedPart("read", "call_r", `{"filePath":"/work/out.txt"}`, "same"),
	}
	if v := g.observe(readParts); v != stepProgress {
		t.Fatalf("observe returned %d after reset, want progress", v)
	}
}

// A step with no completed tool calls is a thinking step.
func TestStepGuardThinkingStep(t *testing.T) {
	g := newStepGuard()
	if v := g.observe(msgmodel.Parts{}); v != stepProgress {
		t.Fatalf("observe returned %d for a thinking step, want progress", v)
	}
}

// The grace period after the conclude directive: a re-trigger terminates.
func TestStepGuardGraceTerminates(t *testing.T) {
	g := newStepGuard()
	parts := msgmodel.Parts{
		completedPart("read", "call_1", `{"filePath":"/work/data.txt"}`, "content"),
	}
	for range stepRepeatCap {
		g.observe(parts)
	}
	g.markConcluded()
	if v := g.observe(parts); v != stepTerminate {
		t.Fatalf("observe returned %d after conclude+repeat, want terminate", v)
	}
}

// extractToolActions skips pending parts and includes completed/error.
func TestExtractToolActionsSkipsPending(t *testing.T) {
	parts := msgmodel.Parts{
		msgmodel.ToolPart{
			PartBase: msgmodel.PartBase{ID: "p1", SessionID: "ses_1", MessageID: "m1"},
			Type:     msgmodel.PartTypeTool,
			CallID:   "call_pending",
			Tool:     "read",
			State:    msgmodel.PendingToolState(),
		},
		completedPart("read", "call_done", `{"filePath":"/work/data.txt"}`, "content"),
	}
	actions := extractToolActions(parts)
	if len(actions) != 1 {
		t.Fatalf("got %d actions, want 1 (pending skipped)", len(actions))
	}
	if actions[0].tool != "read" || actions[0].output != "content" {
		t.Fatalf("action = %+v", actions[0])
	}
}

var _ = orclient.FinishStop
