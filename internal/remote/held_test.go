package remote

import (
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A CARD LEAVES THE WAITING ROOM WHEN ITS QUESTION IS NO LONGER OPEN, whatever
// door settled it — which is the whole of the change: a connect ask whose
// five-minute wait ran out and one somebody answered are the same fact to this
// room, because both are questions the engine no longer lists.
func TestASettledQuestionLeavesTheWaitingRoom(t *testing.T) {
	held := newHeldSet()
	held.raise(EventWire{Event: session.Event{Kind: session.EventConnectAsk, ConnectID: "connect-1"}}, 7, nil)
	if got := len(held.waitingFor(1)); got != 1 {
		t.Fatalf("waiting questions = %d, want 1", got)
	}
	held.keepOnly(nil)
	if got := len(held.waitingFor(1)); got != 0 {
		t.Fatalf("waiting questions after the engine settled it = %d, want 0", got)
	}
	if got := held.outstanding(); got != 0 {
		t.Fatalf("outstanding = %d, want 0", got)
	}
}

func TestAnOpenQuestionStaysInTheWaitingRoom(t *testing.T) {
	held := newHeldSet()
	held.raise(EventWire{Event: session.Event{Kind: session.EventConnectAsk, ConnectID: "connect-1"}}, 7, nil)
	held.keepOnly(map[heldKey]bool{{kind: HeldConnect, text: "connect-1"}: true})
	if got := len(held.waitingFor(1)); got != 1 {
		t.Fatalf("waiting questions = %d, want 1", got)
	}
}

// THE TWO SPELLINGS OF A KEY MUST AGREE. The room is filled from the lane's own
// EVENT and emptied against the engine's own QUESTION, so a kind read one way
// there and another way here would prune a card a person is still looking at.
func TestTheEventAndTheQuestionNameOneCard(t *testing.T) {
	for _, row := range []struct {
		what     string
		event    session.Event
		question session.Question
	}{
		{
			what:     "consent",
			event:    session.Event{Kind: session.EventConsentRequest, ID: 3},
			question: session.Question{Kind: session.QuestionConsent, ID: 3},
		},
		{
			what:     "standing",
			event:    session.Event{Kind: session.EventStandingProposal, Standing: &session.StandingNotice{ID: 4}},
			question: session.Question{Kind: session.QuestionStanding, ID: 4},
		},
		{
			what:     "harness",
			event:    session.Event{Kind: session.EventHarnessOffer, ID: 5},
			question: session.Question{Kind: session.QuestionHarness, ID: 5},
		},
		{
			what:     "connect",
			event:    session.Event{Kind: session.EventConnectAsk, ConnectID: "connect-9"},
			question: session.Question{Kind: session.QuestionConnect, Ref: "connect-9"},
		},
	} {
		fromEvent, ok := heldKeyOf(row.event)
		if !ok {
			t.Fatalf("%s: the event names no card", row.what)
		}
		fromQuestion, ok := heldKeyOfQuestion(row.question)
		if !ok {
			t.Fatalf("%s: the question names no card", row.what)
		}
		if fromEvent != fromQuestion {
			t.Fatalf("%s: the event says %+v and the question says %+v", row.what, fromEvent, fromQuestion)
		}
	}
}

// AN ANSWERED QUESTION NEVER COMES BACK, WHICHEVER DOOR ANSWERED IT.
//
// The owner answered a standing proposal from the chat at 14:55 and the same
// card was on the block again forty minutes later, after every tab switch,
// conversation switch and new window — each a new arrival number, each handed
// the answered card again. The cause was a door-keyed clear: the waiting room
// dropped a question only when that question's OWN resolve-door was called, and
// since the questions wave the chat answers every lane through the one door
// [MethodQuestionResolve], which had no such line. This is the law that keeps a
// new door from re-opening that hole: the room is emptied from the question's
// own life, so there is no line for a door to forget.
func TestAnAnswerThroughTheOneDoorEmptiesTheWaitingRoom(t *testing.T) {
	far := newAskingAgent(session.Question{Kind: session.QuestionStanding, ID: 4})
	sess := NewSession(&Engine{
		Agent:       far,
		Workspace:   "/home/somebody/api",
		SessionFile: "/home/somebody/.codeaf/v3/sessions/-home-somebody-api/one.jsonl",
	}, true)
	sess.mu.Lock()
	sess.held.raise(WireEvent(session.Event{
		Kind: session.EventStandingProposal, Standing: &session.StandingNotice{ID: 4},
	}), 1, nil)
	sess.mu.Unlock()

	if got := sess.heldCount(); got != 1 {
		t.Fatalf("a standing card nobody has answered is outstanding %d times, want 1", got)
	}
	if got := len(sess.heldWaiting(2)); got != 1 {
		t.Fatalf("a surface arriving before the answer was handed %d questions, want 1", got)
	}

	// THE ANSWER GOES THROUGH THE ONE DOOR AND TELLS THIS PACKAGE NOTHING —
	// which is exactly the case the old per-door clear could not see.
	link := dialSession(t, sess)
	link.hello(Hello{Version: Version, Surface: "macbook"})
	link.ok(1, MethodQuestionResolve, QuestionArgs{Answer: session.Answer{
		Kind: session.QuestionStanding, ID: 4, Key: "3",
	}})

	if got := sess.heldCount(); got != 0 {
		t.Fatalf("the answered card is still outstanding %d times — the conversation goes on reading `waiting`", got)
	}
	// AND ON EVERY LATER ATTACH. A tab switch, a conversation switch and a
	// second window are three new arrival numbers, and the defect was that each
	// of them was handed the card again.
	for arrived := uint64(3); arrived < 6; arrived++ {
		if got := len(sess.heldWaiting(arrived)); got != 0 {
			t.Fatalf("attach %d was handed %d answered questions", arrived, got)
		}
	}
}

// heldWaiting is what a surface with this arrival number would be handed, read
// under the session's own lock.
func (sess *Session) heldWaiting(arrived uint64) []HeldQuestion {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.heldWaitingLocked(arrived)
}

// askingFake is a [fakeAgent] that also answers the door the waiting room is
// reconciled against, and takes a question off when its own resolver is called —
// which is the whole of what a real engine does. It exists because a bare
// fakeAgent has no OpenQuestions at all, and the optional-door pattern reads
// that as "this engine cannot say", which keeps every card rather than losing
// one ([Session.dropSettledLocked]).
type askingFake struct {
	*fakeAgent

	asked sync.Mutex
	open  []session.Question
}

func (a *askingFake) OpenQuestions() []session.Question {
	a.asked.Lock()
	defer a.asked.Unlock()
	return append([]session.Question(nil), a.open...)
}

func (a *askingFake) settle(kind session.QuestionKind, id uint64) {
	a.asked.Lock()
	defer a.asked.Unlock()
	kept := a.open[:0]
	for _, standing := range a.open {
		if standing.Kind == kind && standing.ID == id {
			continue
		}
		kept = append(kept, standing)
	}
	a.open = kept
}

func (a *askingFake) ResolveConsent(id uint64, allow bool) {
	a.settle(session.QuestionConsent, id)
	a.fakeAgent.ResolveConsent(id, allow)
}

func (a *askingFake) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	a.settle(session.QuestionConsent, id)
	a.fakeAgent.ResolveConsentRemember(id, allow, scope)
}
