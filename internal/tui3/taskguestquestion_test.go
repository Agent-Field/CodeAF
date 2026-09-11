package tui3

// ── A PAGE READING SOMEBODY ELSE'S WORK SAYS WHEN THAT WORK IS WAITING ─────
//
// The owner's task lane cannot answer this: a node stopped on a question is
// still `running`, so a reading page drew a clock over work that had not moved
// since somebody was asked something. The questions lane can — it replays
// everything open the moment the view attaches — and the whole of what this page
// does with it is DRAW it. Answering belongs to the window that owns the work,
// and the wire refuses this connection the answering door by construction
// (internal/remote's watcherReads).

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ownerQuestion is one question of the conversation this page is reading.
func ownerQuestion(id uint64, head string) session.Event {
	asked := session.Question{
		ID:     id,
		Kind:   session.QuestionAsk,
		Ask:    session.AskChoice,
		Head:   head,
		Reason: "two shapes are viable and the record does not choose",
		Stakes: session.StakesCostly,
	}
	return session.Event{Kind: session.EventQuestion, ID: id, Question: &asked}
}

// THE FAILURE, PINNED. Work that has stopped on a question reads as work that is
// going, on the one page a person opens precisely to find out which it is.
func TestAReadingPageSaysTheWorkItIsWatchingIsWaitingOnSomebody(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	door.askingWith()
	enterAway(t, a)

	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	if body := roomText(a); strings.Contains(body, roomGuestAskedWord) {
		t.Fatalf("a page with nothing open said the work was waiting:\n%s", body)
	}

	next := ownerAsks(t, a, ownerQuestion(3, "which storage shape should this use?"))
	body := roomText(a)
	if !strings.Contains(body, "which storage shape should this use?") {
		t.Fatalf("the page never said what the work is waiting on:\n%s", body)
	}
	if !strings.Contains(body, roomGuestAskedWord) {
		t.Fatalf("the page never said where that question gets answered:\n%s", body)
	}
	if next == nil {
		t.Fatal("the page stopped listening for the owner's questions")
	}

	// AND IT OFFERS NO KEY. This window is reading; the block that answers a
	// question is the owning window's, and a row here that looked answerable
	// would be a key that presses nothing.
	if strings.Contains(body, "[1]") || strings.Contains(body, "[esc] later") {
		t.Fatalf("a reading page offered to answer somebody else's question:\n%s", body)
	}
}

// ANSWERED OR WITHDRAWN, IT STOPS BEING SAID. The lane carries all three kinds,
// and a page that only folded the raising would leave a question on screen that
// the owning window had already dealt with.
func TestAQuestionAnsweredInTheOwningWindowLeavesTheReadingPage(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	door.askingWith()
	enterAway(t, a)

	ownerAsks(t, a, ownerQuestion(3, "which storage shape should this use?"))
	if body := roomText(a); !strings.Contains(body, "which storage shape should this use?") {
		t.Fatalf("the page never drew the question:\n%s", body)
	}

	settled := ownerQuestion(3, "which storage shape should this use?")
	settled.Kind = session.EventQuestionAnswered
	given := session.Answer{Kind: session.QuestionAsk, ID: 3, Key: "2"}
	settled.Answer = &given
	ownerAsks(t, a, settled)
	if body := roomText(a); strings.Contains(body, roomGuestAskedWord) {
		t.Fatalf("a question answered in the owning window is still on the reading page:\n%s", body)
	}
}

// A QUESTION RE-EMITTED IS ONE QUESTION. The lane replays what is open whenever
// a surface attaches and re-emits a question whose words moved, so a page that
// appended would say a conversation was waiting on two answers when it is
// waiting on one.
func TestAReplayedQuestionIsNotCountedTwice(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	door.askingWith()
	enterAway(t, a)

	ownerAsks(t, a, ownerQuestion(3, "which storage shape should this use?"))
	ownerAsks(t, a, ownerQuestion(3, "which storage shape should this use?"))
	guest := a.roomGuest()
	if guest == nil || len(guest.asking) != 1 {
		t.Fatalf("one question was held as %d", len(guest.asking))
	}
}

// A DOOR THAT CANNOT ASK SAYS NOTHING RATHER THAN SOMETHING FALSE, which is what
// a capability that cannot work is owed — and leaving gives the lane back.
func TestAReadingPageWithNoQuestionsLaneSaysNothingAboutQuestions(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)

	if body := roomText(a); strings.Contains(body, roomGuestAskedWord) {
		t.Fatalf("a page with no questions lane made a claim about questions:\n%s", body)
	}
	if guest := a.roomGuest(); guest == nil || guest.questions != nil {
		t.Fatal("a door that offered no questions lane left one behind")
	}
}

// AND THE SUBSCRIPTION IS GIVEN BACK WITH THE PAGE, once, beside the roster's.
func TestLeavingAReadingPageGivesBackTheQuestionsLane(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	door.askingWith()
	enterAway(t, a)

	ownerAsks(t, a, ownerQuestion(3, "which storage shape should this use?"))
	a.closeRoom()
	if door.leftAsking != 1 {
		t.Fatalf("leaving the page left %d questions subscriptions behind", 1-door.leftAsking)
	}
}
