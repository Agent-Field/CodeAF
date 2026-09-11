package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A LANDING THAT IS RE-SETTLED ASKS THE NEW QUESTION, NOT THE OLD ONE.
//
// A divided task landed `your call · nobody could check it`, the model spent
// `accept` on it under `task.settle = auto`, and the merge was refused by the
// person's own uncommitted copies of the files the task wrote. The node came
// back in the SAME state asking a DIFFERENT question with different answers —
// and the card in the conversation stayed frozen in the shape of the first
// landing while the room's foot said the work had finished (#767).

// THE CARD FOLLOWS THE NEW NOTICE. Its head is never rewritten — the span, the
// files and the branch are all still exactly as true as they were — but the row
// that says what is being ASKED is replaced, because that is the row that
// stopped being true.
func TestARelandedNodeAsksTheNewQuestion(t *testing.T) {
	a, _ := settleApp(t)
	card := landUnverified(t, a)
	if !strings.Contains(plain(strings.Join(a.doneRows(card, a.width, false), "\n")), askCheckReason) {
		t.Fatalf("the first landing does not say what it is asking:\n%s",
			plain(strings.Join(a.doneRows(card, a.width, false), "\n")))
	}
	before := len(a.entries)

	// The engine re-settles the node on the model's answer and publishes it: same
	// id, same state, a merge that has since been refused.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{
			Elapsed: 400 * time.Second, Merge: mergeWordConflicted, Branch: "task/parser",
			GroundHeld: true, Conflicts: []string{"leads.md", "research/method.md"},
		})})

	if len(a.entries) != before {
		t.Fatalf("a re-settle wrote a second card: %d entries, want %d", len(a.entries), before)
	}
	card = a.doneCardFor(7)
	if card == nil {
		t.Fatal("the card went away")
	}
	said := plain(strings.Join(a.doneRows(card, a.width, false), "\n"))
	if !strings.Contains(said, askGroundReason) {
		t.Fatalf("the card still asks the first landing's question:\n%s", said)
	}
	if strings.Contains(said, askCheckReason) {
		t.Fatalf("the card asks two questions at once:\n%s", said)
	}
	// AND THE ANSWERS ARE THE NEW ONES. `resolve it` and `drop it`, not `accept`
	// and `not right`: the question changed, so the verbs did.
	if got := card.status.Ask; got.Yes != "resolve it" || got.No != "drop it" {
		t.Fatalf("the re-landed card offers %q / %q", got.Yes, got.No)
	}
}

// AND ITS QUESTION IS REFRESHED RATHER THAN STACKED. Both shapes are one
// question about one node, so the block replaces what it is drawing.
func TestARelandedNodesQuestionReplacesTheFirst(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)

	q := landingAsk(7, session.QuestionConflict, "Port the parser", askGroundReason, "resolve it", "drop it")
	q.Asked = a.now()
	gone := landingAsk(7, session.QuestionLanding, "Port the parser", askCheckReason, "accept", "not right")
	gone.Withdrawn = &session.Withdrawal{Reason: "what it is waiting on changed"}
	a.questionFold(session.Event{Kind: session.EventQuestionWithdrawn, Question: &gone})
	a.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
	a.questionRows(a.width)
	*agent.at = agent.at.Add(2 * questionSettle)

	if n := len(a.questions); n != 1 {
		t.Fatalf("%d questions stand open about one node, want 1", n)
	}
	block := plain(strings.Join(a.questionRows(a.width), "\n"))
	// THE THREE COLUMNS, IN ONE ROW, IN THE ONE ORDER. It is asserted as a
	// single string for the reason the tmux table states beside the same words:
	// three separate searches would pass on a block that drew the columns on
	// three rows, or in the other order, or without the third — and the third is
	// the one docs/design/task-states/DESIGN.md is emphatic about.
	if !strings.Contains(block, "[a] resolve it · [n] drop it · [s] tell it") {
		t.Fatalf("the block does not draw the task-states row:\n%s", block)
	}
	if !strings.Contains(block, askGroundReason) {
		t.Fatalf("the block is missing %q:\n%s", askGroundReason, block)
	}
	if strings.Contains(block, askCheckReason) {
		t.Fatalf("the block still asks the question that was withdrawn:\n%s", block)
	}
	// AND `[a]` ON THAT ROAD SPENDS THE MERGE ROUND AND NOT AN ACCEPT: what is
	// being asked is which of two versions of the person's own file survives, and
	// no accept answers that (docs/design/task-states/DESIGN.md).
	drive(t, a, key("a"))
	if len(agent.merged) != 1 || agent.merged[0] != 7 {
		t.Fatalf("the conflict's yes reached the engine as %+v (accepts: %+v)", agent.merged, agent.resolved)
	}
}

// THE RAIL SHEDS THE LIST AND THE COLON WITH IT. `conflicts with your branch:`
// is a sentence that has told nobody anything: it announces a list and then does
// not have one.
func TestTheRailShedsTheColonWithTheList(t *testing.T) {
	for _, tc := range []struct{ word, want string }{
		{"your call · " + askGroundReason + ": a.md, b/c.md", "your call · " + askGroundReason},
		{"your call · " + askConflictReason + ":", "your call · " + askConflictReason},
		{"your call · " + askCheckReason, "your call · " + askCheckReason},
	} {
		if got := tierWordShed(tc.word); got != tc.want {
			t.Fatalf("tierWordShed(%q) = %q, want %q", tc.word, got, tc.want)
		}
	}
}

// AND THE COLUMN SHEDS IT BEFORE IT CUTS. The rail's block is two rows tall, so
// a reason naming sixteen files used to wrap and be cut mid-list — which left
// the person reading the announcement of a list with the list gone.
func TestTheRailRowDropsTheFileListRatherThanCuttingIt(t *testing.T) {
	a, _ := settleApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Emails and socials for the fifteen leads",
		session.TaskUnverified, session.TaskNotice{
			Elapsed: 400 * time.Second, Merge: mergeWordConflicted, Branch: "task/emails",
			GroundHeld: true,
			Conflicts: []string{
				"leads-contact-sheet.md", "research/method.md", "research/sections/one.md",
				"research/sections/two.md", "research/sections/three.md", "research/sections/four.md",
			},
		})})
	node := a.tasks[7]
	if node == nil {
		t.Fatal("the roster has no node to draw")
	}
	for _, line := range a.railUnder(node, 24) {
		said := strings.TrimRight(plain(line), " ")
		if strings.HasSuffix(said, ":") {
			t.Fatalf("a rail row ends in a bare colon: %q", said)
		}
	}
}

// THE COLUMN SAYS WHICH ROAD IT IS ON, and it is the same road the card says.
//
// All three of the your-call questions that name files ask the same two answers
// and differ only in the sentence between them, so a row that read `conflicts
// with your branch` about a folder with no branch of theirs in it told a person
// to go looking for a merge that was never the problem. The rail built its
// reading without the two facts that pick the road (taskstatus.go).
func TestTheColumnNamesTheRoadTheCardNames(t *testing.T) {
	a, _ := settleApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Emails for the leads",
		session.TaskUnverified, session.TaskNotice{
			Elapsed: 400 * time.Second, Merge: mergeWordConflicted, Branch: "task/emails",
			GroundHeld: true, Conflicts: []string{"leads.md"},
		})})
	node := a.tasks[7]
	if node == nil {
		t.Fatal("the roster has no node to draw")
	}
	said := plain(strings.Join(a.railUnder(node, 60), "\n"))
	if !strings.Contains(said, askGroundReason) {
		t.Fatalf("the column does not name the road the landing is on:\n%s", said)
	}
	if strings.Contains(said, askConflictReason) {
		t.Fatalf("the column calls it a branch clash when no branch of theirs is in it:\n%s", said)
	}
	// AND A REAL BRANCH CLASH IS STILL A BRANCH CLASH.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Port the parser",
		session.TaskUnverified, session.TaskNotice{
			Elapsed: 400 * time.Second, Merge: mergeWordConflicted, Branch: "task/parser",
			Conflicts: []string{"parser.go"},
		})})
	plainNode := a.tasks[8]
	if plainNode == nil {
		t.Fatal("the roster lost the second node")
	}
	if said := plain(strings.Join(a.railUnder(plainNode, 60), "\n")); !strings.Contains(said, askConflictReason) {
		t.Fatalf("an ordinary conflict stopped saying so:\n%s", said)
	}
}
