package tui3

// THE MODEL A TASK RUNS ON, AS A READER SEES IT: stated on the proposal, asked
// about only when one word fitted several, and kept on the work afterwards.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// modelProposal is [proposal] with the engine's model fields filled in.
func modelProposal(a *app, id uint64, countdown time.Duration, model string, options []string) session.Event {
	event := proposal(a, id, countdown)
	event.Task.Model, event.Task.ModelOptions = model, options
	return event
}

// A PROPOSAL SAYS WHOSE HANDS THE WORK IS GOING INTO, on the block's one meta
// line and beside the key that opens the brief — no extra row for it.
func TestAProposalNamesTheModelItWillRunOn(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, "anthropic/claude-opus-5", nil)})

	text := taskText(a)
	if !strings.Contains(text, taskModelTag+"anthropic/claude-opus-5") {
		t.Fatalf("the proposal does not name its model:\n%s", text)
	}
	if !strings.Contains(text, taskExpandHint) {
		t.Fatalf("the model line took the expand hint's place:\n%s", text)
	}
}

// A PROPOSAL THE ENGINE COULD NOT RESOLVE TO ONE MODEL OFFERS THEM, ON THE
// QUESTION, AS A SENTENCE WITH A HOLE IN IT.
//
// THE SHORTLIST IS A HOLE AND NO LONGER A ROW OF CHIPS. The chips were answered
// by the digits 1–4, and the digits are the question block's ANSWERS now
// (question.go's ONE KEY GRAMMAR) — two readers for one keystroke is exactly
// what this wave exists to end. So the choice moved onto the shape the object
// already had for it ([session.TaskModelShape]): one row, `←→` to change it, and
// nothing about it answers the question.
func TestAnAmbiguousProposalOffersTheModelsInAHoleAndPicksTheClosest(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, options[0], options)})
	taskText(a)
	settleAsk(a)

	if text := taskText(a); !strings.Contains(text, taskModelTag+options[0]) {
		t.Fatalf("the proposal does not name the model it resolved to:\n%s", text)
	}
	block := questionBlockText(a)
	for _, want := range []string{"run it on", options[0], "o other", "? clarify"} {
		if !strings.Contains(block, want) {
			t.Fatalf("the proposal's question does not offer %q:\n%s", want, block)
		}
	}
	// The chips are gone from the card, and so is the second countdown they used
	// to sit under: the question above the box owns both now.
	for _, gone := range []string{"[ 1 claude-opus-5 ]", "auto-starts in"} {
		if strings.Contains(taskText(a), gone) {
			t.Fatalf("the card still draws the shortlist (%q):\n%s", gone, taskText(a))
		}
	}

	// `→` MOVES THE CHOICE AND ANSWERS NOTHING. The proposal is not approved by
	// picking a model for it, which is the bargain the row of chips was built on
	// and the reason the arrows are not one of the answers.
	drive(t, a, key("right"))
	if block := questionBlockText(a); !strings.Contains(block, options[1]) {
		t.Fatalf("→ did not move the hole onto %q:\n%s", options[1], block)
	}
	if len(agent.answered) != 0 {
		t.Fatalf("picking a model answered the proposal: %+v", agent.answered)
	}

	// And the answer carries it, so the node is admitted on what was picked.
	drive(t, a, key("1"))
	if len(agent.answered) != 1 {
		t.Fatalf("the proposal was not answered: %+v", agent.answered)
	}
	got := agent.answered[0].answer
	if !got.Approved || got.Model != options[1] {
		t.Fatalf("the answer did not carry the chosen model: %+v", got)
	}
}

// A PROPOSAL WITH ONE MODEL ASKS NOTHING ABOUT IT. One option is not a choice,
// and a hole offering the model the work was already going to run on is a
// question that has answered itself.
func TestAnUnambiguousProposalDrawsNoModelHole(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, "anthropic/claude-opus-5", nil)})
	taskText(a)
	settleAsk(a)

	if block := questionBlockText(a); strings.Contains(block, "run it on") {
		t.Fatalf("a proposal with nothing to pick drew a model hole:\n%s", block)
	}
	drive(t, a, key("1"))
	if len(agent.answered) != 1 {
		t.Fatalf("the proposal was not answered: %+v", agent.answered)
	}
	if got := agent.answered[0].answer; got.Model != "" {
		t.Fatalf("the answer named a model nobody was asked about: %+v", got)
	}
}

// questionBlockText is the block above the box as a reader sees it.
func questionBlockText(a *app) string {
	return plain(strings.Join(a.questionRows(a.width), "\n"))
}

// THE NODE KEEPS ITS MODEL AFTERWARDS: the column's hint line says it under
// the pointer on the node's row, the room's header states it, and the landed
// card keeps it beside the working copy.
func TestTheModelFollowsTheNodeOntoTheRailAndTheLandedCard(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 0, "openai/gpt-5", nil)})
	drive(t, a, key("enter"))
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash", session.TaskRunning,
		session.TaskNotice{Model: "openai/gpt-5"})})

	node := a.tasks[7]
	if node == nil || node.model != "openai/gpt-5" {
		t.Fatalf("the node did not keep its model: %+v", node)
	}
	// THE MODEL NEVER BUYS ITS CELLS FROM THE NAME. The row is the state glyph,
	// the name and the clock, one line, and the model is the hint line's under
	// the pointer (sidecol.go's [app.sideHoverWords] reads the telemetry,
	// task.go's [app.railTelemetry]).
	row := plain(a.railEntryRow(railEntry{node: node, group: railRunning}, 28))
	if strings.Contains(row, "gpt-5") || !strings.Contains(row, "Fix the nil-map") {
		t.Fatalf("the model is on the task's row, or the name is not: %q", row)
	}
	a.hot = hoverAt{kind: hoverRail, id: node.id}
	if hint := a.sideHoverWords(); !strings.Contains(hint, "gpt-5") || !strings.Contains(hint, "Fix the nil-map crash") {
		t.Fatalf("the hint line over the row does not carry the name and the model: %q", hint)
	}
	// A LONG MODEL IS NEVER CUT ON THE HINT LINE: it has the room the row did
	// not.
	node.model = "anthropic/claude-opus-4.8"
	if hint := a.sideHoverWords(); !strings.Contains(hint, "claude-opus-4.8") {
		t.Fatalf("a long model was cut from the hint line: %q", hint)
	}
	node.model = "openai/gpt-5"
	a.hot = hoverAt{}

	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash", session.TaskDone,
		session.TaskNotice{Model: "openai/gpt-5", Report: "the guard is in", Merge: "merged"})})

	// The landed card keeps it INSIDE, beside the working copy: the head is what
	// happened, and this is a fact somebody opens the card to check.
	if strings.Contains(taskText(a), "openai/gpt-5") {
		t.Fatalf("the collapsed card recites the model:\n%s", taskText(a))
	}
	clickHit(t, a, hitDone)
	if !strings.Contains(taskText(a), doneModelLabel+"openai/gpt-5") {
		t.Fatalf("the opened card does not say what ran the work:\n%s", taskText(a))
	}
}
