package tui3

// THE MODEL A TASK RUNS ON, AS A READER SEES IT: stated on the proposal, asked
// about only when one word fitted several, and kept on the work afterwards.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// modelProposal is [proposal] with the engine's model fields filled in.
func modelProposal(a *app, id uint64, countdown time.Duration, model string, options []string) session.Event {
	event := proposal(a, id, countdown)
	event.Task.Model, event.Task.ModelOptions = model, options
	return event
}

// A PROPOSAL SAYS WHOSE HANDS THE WORK IS GOING INTO, on the card's one meta
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
	// One model is not a choice, so nothing is offered and nothing is asked.
	if strings.Contains(text, "[ 1 ") {
		t.Fatalf("an unambiguous proposal drew a models row:\n%s", text)
	}
	if a.task.modelRow != -1 {
		t.Fatalf("a card with nothing to pick kept a models row at %d", a.task.modelRow)
	}
}

// A proposal the engine could not resolve to one model OFFERS them, with the
// closest already picked: the countdown keeps running, because an ambiguity the
// harness raised is not a reason for the work to stop.
func TestAnAmbiguousProposalOffersTheModelsAndPicksTheClosest(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, options[0], options)})

	text := taskText(a)
	for _, want := range []string{
		"[ 1 claude-opus-5 ]  [ 2 claude-opus-4.8 ]",
		taskModelTag + "anthropic/claude-opus-5",
		"auto-starts in 4.0s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the ambiguous proposal is missing %q:\n%s", want, text)
		}
	}

	// THE DIGIT IS THE KEY, and it moves the choice without answering the
	// question: the work has not been approved by picking a model for it.
	drive(t, a, key("2"))
	if a.task.model != "anthropic/claude-opus-4.8" {
		t.Fatalf("2 picked %q", a.task.model)
	}
	if !strings.Contains(taskText(a), taskModelTag+"anthropic/claude-opus-4.8") {
		t.Fatalf("the meta line did not follow the choice:\n%s", taskText(a))
	}
	if len(agent.answered) != 0 {
		t.Fatalf("picking a model answered the proposal: %+v", agent.answered)
	}

	// And the answer carries it, so the node is admitted on what was picked.
	drive(t, a, key("y"))
	if len(agent.answered) != 1 {
		t.Fatalf("the proposal was not answered: %+v", agent.answered)
	}
	got := agent.answered[0].answer
	if !got.Approved || got.Model != "anthropic/claude-opus-4.8" {
		t.Fatalf("the answer = %+v, want approved on the picked model", got)
	}
	// The settled card keeps it: this is the only place the choice is recorded.
	if !strings.Contains(taskText(a), "anthropic/claude-opus-4.8") {
		t.Fatalf("the settled card forgot which model was chosen:\n%s", taskText(a))
	}
}

// A CLICK ON THE MODELS ROW IS THAT ROW'S, and it lands on the option under the
// pointer rather than on the card's expansion.
func TestClickingAModelPicksItRatherThanOpeningTheBrief(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, options[0], options)})

	// The layout is what writes the row and its targets, so the frame is taken
	// first and the columns are read off it — one layout, one set of targets.
	body, pad := a.window(a.width, a.viewHeight())
	y := -1
	for i, r := range body {
		if r.hit == hitModel {
			y = a.bodyTop() + pad + i
			break
		}
	}
	if y < 0 {
		t.Fatalf("no visible row answers to the models row:\n%s", taskText(a))
	}
	card := a.task
	if len(card.modelSpans) != 2 {
		t.Fatalf("the models row has %v targets, want one per option", card.modelSpans)
	}
	// The second chip's own columns.
	span := card.modelSpans[1]
	drive(t, a, tea.MouseClickMsg{X: span.from + 1, Y: y, Button: tea.MouseLeft})

	if a.task.model != "anthropic/claude-opus-4.8" {
		t.Fatalf("the click picked %q", a.task.model)
	}
	if a.task.open {
		t.Fatal("the click on the models row opened the brief as well")
	}
	if len(agent.answered) != 0 {
		t.Fatalf("the click answered the proposal: %+v", agent.answered)
	}
}

// A DIGIT IS A DIGIT WHILE SOMEBODY IS WRITING. The redirect lane is the same
// trap the y/r/n keys avoid, and the models row is not exempt from it.
func TestADigitIsTextOnceTheRedirectLaneHasTheFocus(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, options[0], options)})

	drive(t, a, key("r"), key("2"))
	if a.task.model != "anthropic/claude-opus-5" {
		t.Fatalf("a digit typed into the redirect lane moved the model to %q", a.task.model)
	}
	if got := a.input.String(); got != "2" {
		t.Fatalf("the redirect lane holds %q, want the digit as text", got)
	}
}

// THE NODE KEEPS ITS MODEL AFTERWARDS: the rail says it where the column can
// afford it, the room's header states it, and the landed card keeps it beside
// the worktree.
func TestTheModelFollowsTheNodeOntoTheRailAndTheLandedCard(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 0, "openai/gpt-5", nil)})
	drive(t, a, key("y"))
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash", session.TaskRunning,
		session.TaskNotice{Model: "openai/gpt-5"})})

	node := a.tasks[7]
	if node == nil || node.model != "openai/gpt-5" {
		t.Fatalf("the node did not keep its model: %+v", node)
	}
	// THE RAIL SPENDS THE CELLS ONLY WHEN THEY ARE CHEAP. A full column carries a
	// short name beside the handle; the same column keeps the handle alone rather
	// than cutting the title down for a long one, and a slim column keeps it
	// whatever the name is. The title is the row — the model is a bonus.
	full := plain(strings.Join(a.railNodeRows(node, railCols), "\n"))
	if !strings.Contains(full, "gpt-5 #7") {
		t.Fatalf("the rail did not carry the model where it fits:\n%s", full)
	}
	node.model = "anthropic/claude-opus-4.8"
	long := plain(strings.Join(a.railNodeRows(node, railCols), "\n"))
	if strings.Contains(long, "claude-opus-4.8") || !strings.Contains(long, "#7") {
		t.Fatalf("a long model took the title's cells:\n%s", long)
	}
	node.model = "openai/gpt-5"
	narrow := plain(strings.Join(a.railNodeRows(node, railSlimCols), "\n"))
	if strings.Contains(narrow, "gpt-5") || !strings.Contains(narrow, "#7") {
		t.Fatalf("a narrow rail spent its cells on the model:\n%s", narrow)
	}

	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash", session.TaskDone,
		session.TaskNotice{Model: "openai/gpt-5", Report: "the guard is in", Merge: "merged"})})

	// The landed card keeps it INSIDE, beside the worktree: the head is what
	// happened, and this is a fact somebody opens the card to check.
	if strings.Contains(taskText(a), "openai/gpt-5") {
		t.Fatalf("the collapsed card recites the model:\n%s", taskText(a))
	}
	clickHit(t, a, hitDone)
	if !strings.Contains(taskText(a), doneModelLabel+"openai/gpt-5") {
		t.Fatalf("the opened card does not say what ran the work:\n%s", taskText(a))
	}
}
