package tui3

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// ── ONE LADDER, WHEREVER A PERSON CAN MOVE IT ───────────────────────────────
//
// The picker's ctrl+t used to walk a list of its own — off, low, medium, high —
// while the ladder had five rungs and the `thinking` settings row offered all
// five. Two vocabularies for one dial cost two things, and both are pinned here:
// a level this walk did not recognise was CLEARED rather than climbed, so
// `aforge --reasoning xhigh` lost its pin to one keypress; and `xhigh` and `max`
// could not be reached from the picker at all.

// A PIN IS NEVER THROWN AWAY BY ONE KEYPRESS. This is the surface's half of the
// law #643 settled on the wire: a level somebody asked for reaches the request,
// or the person is told what displaced it. Silently dropping it is neither.
func TestOneCtrlTClimbsALaunchedLevelRatherThanClearingIt(t *testing.T) {
	agent := &fakeAgent{model: "deepseek/deepseek-v4", levels: map[string]string{
		"deepseek/deepseek-v4": "xhigh",
	}}
	a := newTestApp(agent)
	a.pick.startFor([]Model{{ID: "deepseek/deepseek-v4", Reasoning: true}}, a.model, chatModel)
	a.cycleReasoning()
	if got := agent.ReasoningFor("deepseek/deepseek-v4"); got != "max" {
		t.Fatalf("one ctrl+t moved a launched xhigh level to %q, want max", got)
	}
}

// EVERY RUNG THE SETTINGS ROW OFFERS IS REACHABLE FROM THE PICKER, and the walk
// comes back to absence off the top — which is the only door a per-model pin can
// be cleared through short of /new, and why this wheel is the clearing one.
func TestThePickerWalksTheWholeLadderAndComesBackToAuto(t *testing.T) {
	const model = "deepseek/deepseek-v4"
	agent := &fakeAgent{model: model}
	a := pickerApp(t, agent, []Model{{ID: model, Reasoning: true}})
	typeLine(t, a, "/model")

	var got []string
	for range 6 {
		drive(t, a, ctrlT())
		got = append(got, agent.ReasoningFor(model))
	}
	want := []string{"low", "medium", "high", "xhigh", "max", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the picker walked %v, want %v", got, want)
	}
}

// AND IT IS THE SAME WALK THE TASK CONTROL TAKES, off one list. The first half
// is the law — the words this walk visits are the words the `thinking` row
// offers, read off the walk rather than written down twice — and the second is
// the two doors taking the same step from the same rung.
func TestOneWalkServesThePickerAndATasksThinkingControl(t *testing.T) {
	seen := make(map[string]bool, len(config.EffortChoices))
	level := ""
	for range len(config.EffortChoices) {
		rung, known := effort.Parse(level)
		if !known {
			t.Fatalf("the picker produced an unknown level %q", level)
		}
		seen[config.EffortWord(rung)] = true
		level = nextReasoning(level)
	}
	for _, choice := range config.EffortChoices {
		if !seen[choice] {
			t.Errorf("the picker never visits the thinking-row choice %q", choice)
		}
	}
	if len(seen) != len(config.EffortChoices) {
		t.Fatalf("the picker visits %v, but the thinking row offers %v", seen, config.EffortChoices)
	}

	const model = "deepseek/deepseek-v4"
	pickerAgent := &fakeAgent{model: model, levels: map[string]string{model: "max"}}
	picker := newTestApp(pickerAgent)
	picker.pick.startFor([]Model{{ID: model, Reasoning: true}}, picker.model, chatModel)
	drive(t, picker, ctrlT())
	if got := pickerAgent.ReasoningFor(model); got != "" {
		t.Fatalf("the picker's ctrl+t moved max to %q, want absence", got)
	}

	task, taskAgent := effortTaskApp(t)
	taskAgent.rungs[7] = "max"
	if !task.cycleNodeEffort(task.tasks[7]) {
		t.Fatal("the task's thinking control did not handle its max rung")
	}
	if got := taskAgent.TaskEffort(7); got != "" {
		t.Fatalf("the task's thinking control moved max to %q, want absence", got)
	}
}

// reasoningWriteAgent counts the writes, because "nothing happened" is a claim
// about what the surface ASKED FOR and not only about what the level ended up
// being: a write of the same word back would still be the knob being sold.
type reasoningWriteAgent struct {
	*fakeAgent
	writes int
}

func (f *reasoningWriteAgent) SetReasoningFor(model, level string) {
	f.writes++
	f.fakeAgent.SetReasoningFor(model, level)
}

// A WIDER LADDER IS STILL NOT OFFERED WHERE THE ENDPOINT WILL NOT HONOUR IT.
// ctrl+t does nothing at all, silently, on a model whose catalog row takes no
// reasoning knob — and on a window with no agent behind it.
func TestCtrlTStillDoesNothingWithoutAReasoningKnobOrAnAgent(t *testing.T) {
	const model = "openai/gpt-4.1-mini"
	agent := &reasoningWriteAgent{fakeAgent: &fakeAgent{
		model: model, levels: map[string]string{model: "high"},
	}}
	a := newTestApp(agent)
	a.pick.startFor([]Model{{ID: model, Reasoning: false}}, a.model, chatModel)
	drive(t, a, ctrlT())
	if agent.writes != 0 {
		t.Fatalf("ctrl+t wrote %d times for a model with no reasoning knob", agent.writes)
	}
	if got := agent.ReasoningFor(model); got != "high" {
		t.Fatalf("ctrl+t changed an unsupported model from high to %q", got)
	}

	withoutAgent := newTestApp(nil)
	withoutAgent.pick.startFor([]Model{{ID: model, Reasoning: true}}, withoutAgent.model, chatModel)
	drive(t, withoutAgent, ctrlT())
	if got := withoutAgent.reasoningFor(model); got != "" {
		t.Fatalf("ctrl+t with no agent left a level %q", got)
	}
}
