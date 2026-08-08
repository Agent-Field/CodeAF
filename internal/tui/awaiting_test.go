package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// The complaint, verbatim: "no indication if anything is happening". Between
// pressing enter and the reply landing the thread showed the user's own words
// and then nothing at all, for as long as a routing call takes.
func TestTheHeadIsNeverSilentlyBusy(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "waiting")
	model.setSize(100, 30)

	if strings.Contains(ansi.Strip(model.renderMessages()), "aforge") {
		t.Fatal("an idle thread already claims the head is working")
	}

	posted := store.Message{Seq: 4, SessionID: "waiting", Role: store.RoleUser, Body: "how are the totals?"}
	_, _ = model.Update(postResultMsg{message: posted})
	if model.awaitingSeq != posted.Seq {
		t.Fatalf("the window is waiting on %d, want %d", model.awaitingSeq, posted.Seq)
	}
	waiting := ansi.Strip(model.renderMessages())
	if !strings.Contains(waiting, "aforge") || !strings.Contains(waiting, "·") {
		t.Fatalf("nothing on screen says the head has the turn:\n%s", waiting)
	}
	// It stands where the answer will stand: under the turn that caused it.
	if strings.LastIndex(waiting, "aforge") < strings.LastIndex(waiting, "how are the totals?") {
		t.Fatalf("the indicator is not at the tail of the thread:\n%s", waiting)
	}
	if !model.awaitingAnimating() {
		t.Fatal("the indicator is on screen without keeping the animation alive")
	}

	model.applyPoll(pollResultMsg{
		sessionID: "waiting",
		messages: []store.Message{
			posted,
			{Seq: 5, SessionID: "waiting", Role: store.RoleAgent, Body: "They add up to 41,208."},
		},
	})
	if model.awaitingSeq != 0 {
		t.Fatalf("the reply landed and the window is still waiting on %d", model.awaitingSeq)
	}
	if !model.hasThreadMessage(5) {
		t.Fatal("the reply never reached the thread")
	}
	if model.awaitingAnimating() {
		t.Fatal("the animation is still being kept alive for a finished turn")
	}
}

// A spoken receipt or a refusal is the answer to the turn just as much as a
// reply is, so the indicator clears on any unanchored line the thread accepts
// after it. Ambient work — a node's own progress — is not that line.
func TestTheIndicatorClearsOnWhateverAnswersTheTurn(t *testing.T) {
	for _, probe := range []struct {
		name    string
		arrival store.Message
		clears  bool
	}{
		{"a spoken refusal", store.Message{Seq: 5, Role: store.RoleAgent,
			Body: "I couldn't apply that request: the job had already finished."}, true},
		{"a thread-level receipt", store.Message{Seq: 5, Role: store.RoleSystem,
			Body: "· let go — the old preference"}, true},
		{"a worker's progress", store.Message{Seq: 5, Role: store.RoleSystem, NodeID: "job-1",
			Body: "reading the migration table"}, false},
	} {
		t.Run(probe.name, func(t *testing.T) {
			model := New(&fakeBackend{}, "clearing")
			model.setSize(100, 30)
			_, _ = model.Update(postResultMsg{message: store.Message{
				Seq: 4, SessionID: "clearing", Role: store.RoleUser, Body: "stop that job",
			}})
			arrival := probe.arrival
			arrival.SessionID = "clearing"
			model.applyPoll(pollResultMsg{sessionID: "clearing", messages: []store.Message{arrival}})
			if cleared := model.awaitingSeq == 0; cleared != probe.clears {
				t.Fatalf("%s cleared=%t, want %t", probe.name, cleared, probe.clears)
			}
		})
	}
}

// A second window follows the same thread and must not narrate a turn it did
// not send. The state is only ever entered where this window's own post is
// accepted, so a turn arriving through the poll leaves it silent.
func TestAVisitorWindowNeverWaitsOnSomebodyElsesTurn(t *testing.T) {
	commander := &stubCommander{state: Residency{Visitor: true, PID: 4711}}
	model := NewWithCommander(&fakeBackend{}, "visitor", commander)
	model.setSize(100, 30)
	model.applyPoll(pollResultMsg{
		sessionID: "visitor", residency: commander.state, residencyRead: true,
		messages: []store.Message{
			{Seq: 4, SessionID: "visitor", Role: store.RoleUser, Body: "typed in the other window"},
		},
	})
	if model.awaitingSeq != 0 {
		t.Fatalf("a visitor window took ownership of turn %d", model.awaitingSeq)
	}
	if strings.Contains(ansi.Strip(model.renderMessages()), "aforge  ·") {
		t.Fatalf("a visitor window drew a presence line:\n%s", ansi.Strip(model.renderMessages()))
	}
}

// Two more ways the line has to know when to stop: a live stream is already the
// reply arriving, and a wait nobody ever answered stops breathing rather than
// pinning the repaint loop forever.
func TestTheIndicatorYieldsToTheStreamAndStopsBreathing(t *testing.T) {
	model := New(&fakeBackend{}, "yield")
	model.setSize(100, 30)
	_, _ = model.Update(postResultMsg{message: store.Message{
		Seq: 4, SessionID: "yield", Role: store.RoleUser, Body: "what is running?",
	}})
	if !model.awaitingReply() {
		t.Fatal("the wait never started")
	}
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	if model.awaitingReply() {
		t.Fatal("the indicator sat above a live stream saying the same thing twice")
	}

	model.applyStreamEvent(StreamEvent{Kind: StreamFailed})
	model.awaitingSince = model.standingTime().Add(-awaitingStaleAfter - time.Second)
	if !model.awaitingReply() {
		t.Fatal("a stale wait dropped its line instead of its breath")
	}
	if model.awaitingAnimating() {
		t.Fatal("a wait nobody answered is still driving the animation loop")
	}
}

// Fifty-nine visible columns of a docked pane: the line is still there, still
// inside the frame, and still a line.
func TestTheIndicatorSurvivesTheNarrowDock(t *testing.T) {
	model := New(&fakeBackend{}, "narrow-wait")
	for _, width := range []int{60, 80, 120} {
		model.setSize(width, 24)
		_, _ = model.Update(postResultMsg{message: store.Message{
			Seq: 4, SessionID: "narrow-wait", Role: store.RoleUser, Body: "summarize the migration notes",
		}})
		frame := model.View()
		if !strings.Contains(ansi.Strip(frame), "aforge") {
			t.Fatalf("width %d lost the presence line:\n%s", width, ansi.Strip(frame))
		}
		assertFitsWidth(t, frame, width, "presence")
	}
}

// The one-shot: pressing enter arms the wait through the real submit path, not
// only through a synthesized post result.
func TestSubmittingATurnArmsTheWait(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "submit-wait")
	model.setSize(100, 30)
	model.input.SetValue("draft the launch note")
	command := model.submit()
	if command == nil {
		t.Fatal("submit produced no post")
	}
	_, _ = model.Update(command())
	if model.awaitingSeq == 0 {
		t.Fatal("a submitted turn did not arm the presence line")
	}
	if len(backend.posted) != 1 {
		t.Fatalf("posted %d messages", len(backend.posted))
	}
}
