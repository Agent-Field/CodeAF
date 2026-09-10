package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A NESTED PART'S ACCEPT LEAVES THE RECEIPT ON THE CONVERSATION. The tmux
// suite's nested-landing subtest presses `a` and waits for
// `you took this as done`; after the landing moved onto the question block
// (#776) and the arrows started walking the block's pointer (#789), that
// letter must still settle the engine door and the re-landing must still
// write the report into the conversation — without needing the card to be
// the selected transcript row first.
func TestANestedLandingAcceptWritesTheTookLine(t *testing.T) {
	a, agent := settleApp(t)

	// THE FAMILY THE E2E SEEDS: a finished parent, a part that needs a look.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskDone,
		session.TaskNotice{Elapsed: time.Minute, Merge: mergeWordMerged, Report: "the index is rebuilt"})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, "Port the parser", session.TaskUnverified,
		session.TaskNotice{
			Parent: 1, Elapsed: 42 * time.Second, Merge: mergeWordMerged,
			Report: "finished, but needs your look — nobody could check it in 5m0s",
			Changed: []string{"parser.go"},
		})})
	if a.doneEntryFor(2) < 0 {
		t.Fatalf("the nested part wrote no card:\n%s", taskText(a))
	}
	q := landingAsk(2, session.QuestionLanding, "Port the parser", askCheckReason, "accept", "not right")
	q.Asked = a.now()
	a.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
	a.questionRows(a.width)
	*agent.at = agent.at.Add(2 * questionSettle)

	// THE E2E'S OWN RITUAL: a walk of ↑ before the letter, which since #789
	// moves the question block's pointer rather than the transcript selection.
	// The letter must still answer by its own key.
	for i := 0; i < 3; i++ {
		drive(t, a, key("up"))
	}
	drive(t, a, key("a"))

	if len(agent.resolved) != 1 || agent.resolved[0].id != 2 || agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the accept reached the door as %+v", agent.resolved)
	}

	// AND THE ENGINE'S SECOND LANDING — what ResolveUnverified publishes after
	// an accept — is what puts the receipt on the conversation.
	took := "you took this as done"
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, "Port the parser", session.TaskDone,
		session.TaskNotice{
			Parent: 1, Elapsed: 42 * time.Second, Merge: mergeWordMerged,
			Report: took + "\nfinished, but needs your look — nobody could check it in 5m0s",
		})})
	if text := taskText(a); !strings.Contains(text, took) {
		t.Fatalf("the conversation does not say the part was decided:\n%s", text)
	}
	card := a.doneCardFor(2)
	if card == nil || card.status.State != session.TaskDone {
		t.Fatalf("the decided part left its latest card at %+v", card)
	}
}
