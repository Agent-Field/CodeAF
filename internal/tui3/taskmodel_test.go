package tui3

// THE MODEL A TASK RUNS ON, AS A READER SEES IT: stated on the proposal, asked
// about only when one word fitted several, and kept on the work afterwards.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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

// A PROPOSAL THE ENGINE COULD NOT RESOLVE TO ONE MODEL STATES THE ONE IT PICKED
// AND ASKS NOTHING ABOUT IT.
//
// THE SHORTLIST IS NOT DRAWN ANY MORE, and this test is what is left of it. A
// word that fitted several models used to raise a row of chips on the card,
// answered by the digits 1–4 — and the digits are the question block's answers
// now (question.go's ONE KEY GRAMMAR), so a second reader for the same keystroke
// is exactly what this wave exists to end. What runs is the closest match, which
// is what those chips opened on and what the clock would have taken.
//
// CORRECTING IT FROM THE PROPOSAL IS OWED. Until it lands, the way to ask for
// another model is to say so in the words `c change` takes, which the engine
// appends to the brief verbatim.
func TestAnAmbiguousProposalStatesTheModelAndDoesNotAskAboutIt(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: modelProposal(a, 7, 4*time.Second, options[0], options)})
	taskText(a)
	settleAsk(a)

	text := taskText(a)
	if !strings.Contains(text, taskModelTag+"anthropic/claude-opus-5") {
		t.Fatalf("the proposal does not name the model it resolved to:\n%s", text)
	}
	for _, gone := range []string{"[ 1 claude-opus-5 ]", "claude-opus-4.8", "auto-starts in"} {
		if strings.Contains(text, gone) {
			t.Fatalf("the card still draws the shortlist (%q):\n%s", gone, text)
		}
	}
	// THE DIGITS ARE THE QUESTION'S. `2` is the decline and nothing else, which
	// is the whole point of one grammar: a person who has learnt what a digit
	// does on one question has learnt it on all of them.
	drive(t, a, key("2"))
	if len(agent.answered) != 1 || agent.answered[0].answer.Approved {
		t.Fatalf("2 did not answer the question: %+v", agent.answered)
	}
	if agent.answered[0].answer.Model != "" {
		t.Fatalf("the answer named a model nobody was asked about: %+v", agent.answered[0].answer)
	}
}

// THE NODE KEEPS ITS MODEL AFTERWARDS: the rail says it on the telemetry row
// under the name, the room's header states it, and the landed card keeps it
// beside the working copy.
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
	// THE MODEL NEVER BUYS ITS CELLS FROM THE NAME. The first line is the state
	// glyph, the title and the handle — nothing else — and the model rides the
	// telemetry row under it (task.go's [app.railTelemetry]), which is a row that
	// gives up its own tail rather than the title's cells.
	full := plain(strings.Join(a.railNodeRows(node, railCols), "\n"))
	head, under, _ := strings.Cut(full, "\n")
	if strings.Contains(head, "gpt-5") || !strings.Contains(head, "#7") {
		t.Fatalf("the model is on the title's line:\n%s", full)
	}
	if !strings.Contains(under, "gpt-5") {
		t.Fatalf("the rail did not carry the model under the title:\n%s", full)
	}
	// A LONG NAME IS NOW THE SAME ROW. It used to cost the row its model, because
	// the model was measured against the title; nothing is measured against the
	// title any more.
	node.model = "anthropic/claude-opus-4.8"
	long := plain(strings.Join(a.railNodeRows(node, railCols), "\n"))
	if !strings.Contains(long, "claude-opus-4.8") || !strings.Contains(long, "#7") {
		t.Fatalf("a long model cost the row one of its two facts:\n%s", long)
	}
	node.model = "openai/gpt-5"
	narrow := plain(strings.Join(a.railNodeRows(node, railSlimCols), "\n"))
	if !strings.Contains(narrow, "gpt-5") || !strings.Contains(narrow, "#7") {
		t.Fatalf("a slim rail dropped the model with cells to spare:\n%s", narrow)
	}

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
