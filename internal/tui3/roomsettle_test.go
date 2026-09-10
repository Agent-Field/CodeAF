package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE LANDED `your call` IS A QUESTION, AND IT FOLLOWS THE PERSON EVERYWHERE.
//
// A node that lands needing a look invites the person into its room — the
// roster says `your call`, enter opens the page — and for a while the page they
// arrived on had nothing to answer with: `this task has finished — say it to
// main` at the foot, over work nobody had decided about (#767). The answers are
// the landing question's now, on the block above the box, drawn on whichever
// page a person is standing on; what is left to this file is that the room stops
// claiming the work is finished, and that the frame's hint slot names the same
// letters the block draws (tasksettle.go).

// roomQuestionFake is a room fake that also holds the questions lane, so the
// block has its optional half ([questionAgent] states the law).
type roomQuestionFake struct {
	*roomFake
	open   []session.Question
	lane   chan session.Event
	answer []session.Answer
}

func (f *roomQuestionFake) OpenQuestions() []session.Question { return f.open }

func (f *roomQuestionFake) WatchQuestions() (<-chan session.Event, func()) {
	if f.lane == nil {
		f.lane = make(chan session.Event, 8)
	}
	return f.lane, func() {}
}

func (f *roomQuestionFake) ResolveQuestion(answer session.Answer) error {
	f.answer = append(f.answer, answer)
	return nil
}

// landingAsk is the question the engine builds over a landing's own
// [session.TaskAsk] (internal/session's landingQuestion), as a surface receives
// it. The words are the ask's, exactly as landingOptions fills them in.
func landingAsk(id uint64, kind session.QuestionKind, title, reason, yes, no string) session.Question {
	options := session.AnswerOptions(session.QuestionLanding)
	for at := range options {
		switch options[at].Key {
		case session.LandingYesKey:
			options[at].Label = yes
		case session.LandingNoKey:
			options[at].Label = no
		}
	}
	return session.Question{
		ID: id, Kind: kind, Ask: session.AskLanding, Form: session.FormCard,
		Asker:   session.Asker{Kind: session.AskerTask, Name: title},
		Head:    title,
		Reason:  reason,
		Subject: session.SubjectRef{Kind: session.SubjectNode, ID: id, Name: title},
		Options: options,
		Stakes:  session.StakesCostly,
		Policy:  session.Policy{Kind: session.PolicyAsk},
	}
}

// roomLandingApp is [roomApp] with the questions lane under it, standing in the
// room of node 7 after it landed in the given state.
func roomLandingApp(t *testing.T, state session.TaskState) (*app, *roomQuestionFake) {
	t.Helper()
	base, fake, _ := roomApp(t)
	agent := &roomQuestionFake{roomFake: fake}
	base.agent = agent
	base.profileDir = t.TempDir()
	now := base.now()
	base.clock = func() time.Time { return now }
	// Somebody is at this keyboard (questiondelivery.go's [awayAfter]).
	base.lastQuestionKey = now
	if base.width < 80 {
		base.width, base.height = 140, 30
	}
	notice := unverifiedNotice("nobody could check it — the checker never answered")
	if state == session.TaskDone {
		notice = session.TaskNotice{Elapsed: notice.Elapsed, Merge: mergeWordMerged}
	}
	drive(t, base, streamEventMsg{gen: base.gen, ev: update(7, "Fix the nil-map crash", state, notice)})
	if state == session.TaskUnverified {
		q := landingAsk(7, session.QuestionLanding, "Fix the nil-map crash", askCheckReason, "accept", "not right")
		q.Asked = now
		base.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
		// Drawn, then aged past the block's settle guard (question.go).
		base.questionRows(base.width)
		now = now.Add(2 * questionSettle)
	}
	base.openRoom(7, "Fix the nil-map crash")
	drive(t, base, roomClosedMsg{gen: base.room.gen})
	return base, agent
}

// THE ROOM OF A NODE THAT IS SOMEBODY'S CALL DOES NOT SAY IT HAS FINISHED. That
// foot was the one sentence on the page that was not true about it.
func TestTheRoomOfANodeThatNeedsALookDoesNotClaimItIsFinished(t *testing.T) {
	a, _ := roomLandingApp(t, session.TaskUnverified)

	if !a.roomLandingAsking() {
		t.Fatal("the room of a your-call node does not know it is asking")
	}
	if page := roomText(a); strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("the room says %q under a question it is asking:\n%s", roomFinishedRefusal.what, page)
	}
	if got := a.roomHint(); !strings.Contains(got, "a accept") || !strings.Contains(got, "n not right") {
		t.Fatalf("the hint slot reads %q, want it to name the question's own answers", got)
	}
}

// AND THE ANSWERS ARE ON THE BLOCK, in the room exactly as in the conversation.
func TestTheRoomsQuestionIsDrawnOnTheBlock(t *testing.T) {
	a, agent := roomLandingApp(t, session.TaskUnverified)

	block := plain(strings.Join(a.questionRows(a.width), "\n"))
	for _, want := range []string{"Fix the nil-map crash", askCheckReason, "accept", "not right", "tell it"} {
		if !strings.Contains(block, want) {
			t.Fatalf("the block is missing %q:\n%s", want, block)
		}
	}
	drive(t, a, key("a"))
	if len(agent.answer) != 1 || agent.answer[0].ID != 7 || agent.answer[0].FirstKey() != session.LandingYesKey {
		t.Fatalf("the accept reached the one door as %+v", agent.answer)
	}
}

// AN ORDINARY LANDING KEEPS THE FOOT IT HAS. Only a node waiting on somebody
// changes: a done node's room still says finished, and asks nothing.
func TestADoneNodeRoomKeepsThePlainFoot(t *testing.T) {
	a, _ := roomLandingApp(t, session.TaskDone)

	page := roomText(a)
	if !strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("a finished room lost its foot:\n%s", page)
	}
	if a.roomLandingAsking() {
		t.Fatalf("a finished room asks to be decided about:\n%s", page)
	}
	if got := a.roomHint(); got != "" {
		t.Fatalf("the hint slot offers %q on a node with nothing to decide", got)
	}
}

// A GUEST PAGE NEVER SHOWS THE LOCAL DECISION. A numeric id from another
// conversation must not select this one's question.
func TestAGuestRoomDoesNotAskTheLocalLandingsQuestion(t *testing.T) {
	a, _ := roomLandingApp(t, session.TaskUnverified)
	a.room.guest = &taskGuest{
		session: "/another/conversation/session.jsonl",
		node:    &taskNode{id: 7, title: "Someone else's result", state: session.TaskDone},
	}
	if a.roomLandingAsking() {
		t.Fatal("a foreign task exposed the local landing's question")
	}
}

// AND A NODE THAT SETTLED WHILE THE ROOM WAS OPEN stops asking on the next
// draw: the engine takes its question back, and the foot is the plain one.
func TestANodeSettledElsewhereStopsAskingInTheRoom(t *testing.T) {
	a, _ := roomLandingApp(t, session.TaskUnverified)
	if !a.roomLandingAsking() {
		t.Fatal("the room is not asking to begin with")
	}

	gone := landingAsk(7, session.QuestionLanding, "Fix the nil-map crash", askCheckReason, "accept", "not right")
	gone.Withdrawn = &session.Withdrawal{Reason: "the work settled"}
	a.questionFold(session.Event{Kind: session.EventQuestionWithdrawn, Question: &gone})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskDone,
		session.TaskNotice{Elapsed: 400 * time.Second, Merge: mergeWordMerged})})
	a.room.dirty = true

	if a.roomLandingAsking() {
		t.Fatal("the room still asks about work that has settled")
	}
	if page := roomText(a); !strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("the settled room lost its foot:\n%s", page)
	}
}
