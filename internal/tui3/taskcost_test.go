package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// WHAT THE WORK CAME TO IS ON THE CARD (taskdone.go).
//
// The price is the last of the three facts a person opens a landed card to
// check — whose hands, what came home, what it cost — and it was the one the
// card could not say while nothing on the wire carried it. It does now
// (session's TaskNotice.CostUSD), and these are the rules it arrives under:
// inside the card rather than on its head, from the node's own reconciled
// figure rather than the raw notice, and ABSENT rather than $0.00 when nobody
// published a price.

// TestTheLandedCardSaysWhatTheWorkCostWhenSomebodyPricedIt is the ordinary
// case: the engine closed the books at 0.75, and the opened card says so.
func TestTheLandedCardSaysWhatTheWorkCostWhenSomebodyPricedIt(t *testing.T) {
	a, _, advance := roomApp(t)

	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{
			Model: "openai/gpt-5", Report: "the guard is in", Merge: mergeWordMerged, CostUSD: 0.75,
		})})

	// THE HEAD IS WHAT HAPPENED, and a price is not that: the collapsed card
	// recites no figure, exactly as it recites no model (taskmodel_test.go).
	if strings.Contains(taskText(a), dollars(0.75)) {
		t.Fatalf("the collapsed card recites the price:\n%s", taskText(a))
	}
	clickHit(t, a, hitDone)
	if !strings.Contains(taskText(a), doneCostLabel+dollars(0.75)) {
		t.Fatalf("the opened card does not say what the work cost:\n%s", taskText(a))
	}
	// It sits BESIDE the model, in the block's own order: whose hands, then what
	// the hands came to.
	text := taskText(a)
	model, cost := strings.Index(text, doneModelLabel), strings.Index(text, doneCostLabel)
	if model < 0 || cost < model {
		t.Fatalf("the price is not beside the model that earned it:\n%s", text)
	}
}

// TestACardNobodyPricedDrawsNoCostRowAtAll is the emptiness law this surface
// keeps everywhere it draws money: zero is "nobody published a price" — an
// unpriced model, an older engine — and it is NOT the claim that the work was
// free. A $0.00 on a card would be this surface inventing a figure.
func TestACardNobodyPricedDrawsNoCostRowAtAll(t *testing.T) {
	a, _, advance := roomApp(t)

	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{
			Model: "openai/gpt-5", Report: "the guard is in", Merge: mergeWordMerged,
		})})
	clickHit(t, a, hitDone)

	text := taskText(a)
	if strings.Contains(text, doneCostLabel) || strings.Contains(text, dollars(0)) {
		t.Fatalf("an unpriced node was given a figure:\n%s", text)
	}
	// The rest of the block is unchanged by the absence — a missing price costs
	// the card a row and nothing else.
	if !strings.Contains(text, doneModelLabel+"openai/gpt-5") {
		t.Fatalf("the card lost the facts it does have:\n%s", text)
	}
}

// TestTheCardsPriceIsTheNodesReconciledFigureAndNotTheRawNotice is why the card
// freezes [taskNode.spent] rather than TaskNotice.CostUSD. The node spent its
// whole life keeping the engine's published price and the pilot's running sum
// in ONE number (task.go), and a landing that publishes no price at all — a
// provider that reported nothing on the final step — must not throw away the
// money the pilot watched being spent.
func TestTheCardsPriceIsTheNodesReconciledFigureAndNotTheRawNotice(t *testing.T) {
	a, _, advance := roomApp(t)

	flyTurn(t, a, 7, turnDone(4000, 1000, 0.30))
	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{Report: "the guard is in", Merge: mergeWordMerged})})
	clickHit(t, a, hitDone)

	if !strings.Contains(taskText(a), doneCostLabel+dollars(0.30)) {
		t.Fatalf("the card dropped the spend the pilot counted:\n%s", taskText(a))
	}
}

// TestTheCardsPriceIsEnoughToOpenTheCardFor guards the pair of rules that make
// the key honest: a card offers "ctrl+o output" only when there is something
// behind it, and a price on its own is something.
func TestTheCardsPriceIsEnoughToOpenTheCardFor(t *testing.T) {
	a, _, _ := roomApp(t)
	card := &taskDone{title: "Fix the nil-map crash"}
	if a.doneHasDetail(card) {
		t.Fatal("a card with nothing behind it offers a key that opens nothing")
	}
	card.cost = 0.42
	if !a.doneHasDetail(card) {
		t.Fatal("a priced card offers no way to read the price")
	}
	card.open = true
	if got := plainOf(a.doneDetail(card, 60)); !strings.Contains(got, doneCostLabel+dollars(0.42)) {
		t.Fatalf("the opened card drew no price:\n%s", got)
	}
}

// THE DIFFSTAT'S LINES ARE STILL NOT ON THE WIRE, and the card says the true
// smaller thing: the file COUNT the engine publishes, with no "+42 −7" beside
// it. Nothing in this process carries insertions and deletions — not the
// notice, not the project index, which counts files and nothing finer — so the
// parenthesis stays absent until something does. The card is already built to
// draw it the moment it arrives, which is what the second half of this asserts.
func TestTheCardCountsFilesAndInventsNoLineNumbers(t *testing.T) {
	a, _, advance := roomApp(t)

	advance(2 * time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{
			Report: "the guard is in", Merge: mergeWordMerged,
			Changed: []string{"internal/session/loop.go", "internal/session/loop_test.go"},
		})})

	text := taskText(a)
	if !strings.Contains(text, "2"+doneFilesSuffix) {
		t.Fatalf("the card does not count the files the node wrote:\n%s", text)
	}
	// The parenthesis is the whole of the claim — "+" alone is in every key this
	// surface prints — and it is what must not be there.
	if strings.Contains(text, "("+glyphAdd) {
		t.Fatalf("the card invented a diffstat nobody published:\n%s", text)
	}
	// And the words it WOULD draw, the day the numbers arrive.
	if got := doneFilesWord(2, 42, 7); got != "2"+doneFilesSuffix+" ("+glyphAdd+"42/"+glyphDel+"7)" {
		t.Fatalf("the card cannot draw a diffstat it is given: %q", got)
	}
	if got := doneFilesWord(1, 0, 0); got != "1"+doneFileSuffix {
		t.Fatalf("one file is not spelled in the singular: %q", got)
	}
}

// plainOf strips the paint off a block of rows and joins it.
func plainOf(rows []string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = plain(r)
	}
	return strings.Join(out, "\n")
}
