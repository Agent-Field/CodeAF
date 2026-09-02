package session

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// answeredOffer is what the transport's side of an offer is asked, recorded.
type answeredOffer struct {
	ask string
	yes bool
}

type recordingAnswerer struct {
	answered []answeredOffer
	open     bool
}

func (a *recordingAnswerer) AnswerOffer(ask string, yes bool) bool {
	a.answered = append(a.answered, answeredOffer{ask: ask, yes: yes})
	return a.open
}

// offerAgent opens a conversation with a recording answerer behind it.
func offerAgent(t *testing.T) (*Agent, *recordingAnswerer) {
	t.Helper()
	t.Cleanup(forgetOffers)
	answerer := &recordingAnswerer{open: true}
	previous := SetOfferAnswerer(answerer)
	t.Cleanup(func() { SetOfferAnswerer(previous) })

	agent, err := newAgent(Config{
		Workspace: t.TempDir(),
		Model:     "talk/model",
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent, answerer
}

// asking is the phase a pinned lane raises when it has gone quiet.
func asking(model string, since time.Time) PhaseNews {
	return PhaseNews{
		Phase:  PhaseAsking,
		Since:  since,
		At:     since,
		Detail: "coreweave is slow",
		Then:   "auto",
		// THE TOKEN NAMES THE REQUEST and nothing else. The transport mints it
		// when it raises the question and the surface hands it back untouched,
		// so a keystroke lands on the wait somebody is answering rather than on
		// whichever request is in flight when the key is pressed. A phase that
		// carried none is a question nobody could answer.
		Ask:   "offer-1",
		Model: model,
		Role:  lane.RoleTalk,
	}
}

// TestAPinnedLaneIsAskedAndTheAnswerReachesTheTransport is the offer end to end
// on this side of the seam: the phase raises it, the surface answers it, and the
// answer arrives with the token that names the request it belongs to.
func TestAPinnedLaneIsAskedAndTheAnswerReachesTheTransport(t *testing.T) {
	agent, answerer := offerAgent(t)
	forwardPhase(asking("talk/model", time.Now()))

	if !agent.AnswerLaneOffer(true) {
		t.Fatal("a standing offer was not answered")
	}
	if len(answerer.answered) != 1 || !answerer.answered[0].yes {
		t.Fatalf("the transport was told %v, want one yes", answerer.answered)
	}
	if answerer.answered[0].ask == "" {
		t.Error("the answer named no offer, so the transport cannot tell which wait it ends")
	}
	// AND IT IS ANSWERED ONCE. A second answer to one question is a second
	// rescue nobody asked for.
	if agent.AnswerLaneOffer(true) {
		t.Error("one question was answered twice")
	}
}

// TestAnythingThatArrivesWithdrawsTheOffer is the withdrawal rule: the first
// visible token makes the question moot, and a question a person can no longer
// act on is worse than no question at all.
func TestAnythingThatArrivesWithdrawsTheOffer(t *testing.T) {
	agent, answerer := offerAgent(t)
	forwardPhase(asking("talk/model", time.Now()))

	writing := asking("talk/model", time.Now())
	writing.Phase, writing.Detail, writing.Then = provider.PhaseWriting, "", ""
	forwardPhase(writing)

	if agent.AnswerLaneOffer(true) {
		t.Fatal("an offer the answer had already overtaken was still answerable")
	}
	if len(answerer.answered) != 0 {
		t.Fatalf("the transport was told %v about a withdrawn offer", answerer.answered)
	}
}

// TestAnOfferThatHasLapsedIsNotAnswered is the window's half of the same
// bargain, said on the engine's side: a surface stops drawing a question after
// [provider.PhaseWindow], and after that nothing may answer it either.
func TestAnOfferThatHasLapsedIsNotAnswered(t *testing.T) {
	agent, answerer := offerAgent(t)
	forwardPhase(asking("talk/model", time.Now().Add(-provider.PhaseWindow-time.Second)))

	if agent.AnswerLaneOffer(true) {
		t.Fatal("a question about a request that is over was still answered")
	}
	if len(answerer.answered) != 0 {
		t.Fatalf("the transport was told %v about a lapsed offer", answerer.answered)
	}
}
