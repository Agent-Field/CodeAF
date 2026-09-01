package lane

import (
	"testing"
	"time"
)

// ── A LANE THAT SERVED GARBAGE STANDS DOWN, AND COMES BACK BY SERVING ───────
//
// The arithmetic of the quality axis is pinned next door in quality_test.go.
// What this pins is the SENTENCE the harness's own issue asks for, over two
// lanes and with the words the transport actually sends: a provider that served
// garbage is observably deprioritized for the FOLLOWING attempts, and it
// recovers.
//
// It is written with `babble` as the reason because that is the word
// internal/provider stamps on a reply that stopped being language
// (streamguard.go's CutReason.word), and a test that used a word of its own
// would go green on a wire nobody is using.

// twoLanes primes a ledger with two endpoints of the same model that are alike
// in every respect the gate and the score read. Anything that separates them
// afterwards is the quality axis and nothing else.
func twoLanes(t *testing.T) (*ledger, ID, ID) {
	t.Helper()
	l := newLedger()
	good := cloudflareRow()
	good.ID = ID{Model: scriptedModel, Lane: "steady"}
	bad := good
	bad.ID = ID{Model: scriptedModel, Lane: "gusher"}
	l.Prime(good, SheetWeight)
	l.Prime(bad, SheetWeight)
	return l, good.ID, bad.ID
}

func talkAt(model string, now time.Time) Request {
	return Request{
		Model: model, PromptTokens: 4000, Visible: 400, MaxTokens: 400,
		ValueOfTime: 90, QualityNeed: talkNeed, Horizon: 40, Now: now,
	}
}

// onFrontier reports whether a frontier holds a lane.
func onFrontier(front []Scored, lane string) bool {
	for _, candidate := range front {
		if candidate.ID.Lane == lane {
			return true
		}
	}
	return false
}

// soloFront asks the gate about ONE lane, which is the difference between "this
// lane was out-ranked by a better one" and "this lane was refused". Both are
// deprioritization and only the second is a verdict, so a test of the quality
// axis has to be able to tell them apart.
func soloFront(l *ledger, id ID, req Request) []Scored {
	belief, _ := l.Belief(id)
	if since := req.Now.Sub(belief.QualityAt); since > 0 && !belief.QualityAt.IsZero() {
		belief.Quality = belief.Quality.Toward(qualityPrior(belief.Facts.Quant), since, QualityHalfLife)
	}
	return frontierFor([]Belief{belief}, req, gateOptions{}, nil)
}

// TestALaneThatServedGarbageIsDeprioritizedAndRecovers is the acceptance, in one
// walk: two equal lanes, one of them starts serving soup, the following attempts
// stop treating them as equals — and it comes back by serving.
func TestALaneThatServedGarbageIsDeprioritizedAndRecovers(t *testing.T) {
	l, good, bad := twoLanes(t)

	// BEFORE. Nothing has been judged, so both lanes are candidates: an unjudged
	// lane is not a suspect, which is the emptiness law on this axis.
	before := talkAt(scriptedModel, noon)
	if front := frontierFor(agedFor(l, scriptedModel, before), before, gateOptions{}, nil); len(front) != 2 {
		t.Fatalf("two unjudged lanes gave %d candidates, want both", len(front))
	}

	// THE BAD STRETCH. Four replies from one endpoint that had stopped being
	// language — the shape measured on 2026-08-31, where the same lane was
	// retried on equal footing after every one of them. The other lane answers
	// normally through the same window, so what separates the two afterwards is
	// what each one DID rather than what either was asked.
	at := noon
	for range 4 {
		at = at.Add(20 * time.Second)
		l.NoteOutcome(Outcome{ID: bad, Accepted: false, Reason: "babble", At: at})
	}
	for range 4 {
		at = at.Add(20 * time.Second)
		l.NoteOutcome(Outcome{ID: good, Accepted: true, Reason: "served", At: at})
	}

	after := talkAt(scriptedModel, at)
	front := frontierFor(agedFor(l, scriptedModel, after), after, gateOptions{}, nil)
	if onFrontier(front, bad.Lane) {
		t.Fatalf("the lane that served soup four times is still a candidate on equal footing: %+v", front)
	}
	if !onFrontier(front, good.Lane) {
		t.Fatalf("the lane that served properly was dropped too: %+v", front)
	}
	// And it is the GATE that refused it, not merely a better lane out-ranking
	// it: on its own, with nothing to be compared against, it is still out.
	if solo := soloFront(l, bad, after); len(solo) != 0 {
		t.Fatalf("four unusable answers left the lane passing its own gate: %+v", solo)
	}

	// AND IT RECOVERS BY SERVING. The same lane, answering properly at the pace
	// real requests arrive at, walks its own standing back up until it passes
	// the gate again. Nothing else is done to it — no clock is skipped forward,
	// no refusal is withdrawn.
	for range 12 {
		at = at.Add(5 * time.Minute)
		l.NoteOutcome(Outcome{ID: bad, Accepted: true, Reason: "served", At: at})
	}
	recovered := talkAt(scriptedModel, at)
	if solo := soloFront(l, bad, recovered); len(solo) == 0 {
		judged, _ := l.Belief(bad)
		t.Fatalf("a lane that has served twelve usable answers is still refused by the gate: %+v", judged.Quality)
	}
	// Its standing really rose rather than merely being forgotten: the evidence
	// behind it grew.
	judged, _ := l.Belief(bad)
	if mass := judged.Quality.A + judged.Quality.B; mass <= 10 {
		t.Fatalf("the lane came back on %.1f observations, which is forgetting rather than serving", mass)
	}
}

// TestOneBadAnswerDeprioritizesWithoutCondemning is the other half of the same
// promise, and it is what makes this a decay and not a penalty box.
//
// One unusable reply is enough to prefer somebody else — that is the whole
// point — and it is NOT enough to refuse the lane outright, for the reason the
// strike ledger next door waits for two: acting on one measurement is how a
// ledger learns noise.
func TestOneBadAnswerDeprioritizesWithoutCondemning(t *testing.T) {
	l, good, bad := twoLanes(t)
	at := noon.Add(time.Minute)
	l.NoteOutcome(Outcome{ID: bad, Accepted: false, Reason: "babble", At: at})
	l.NoteOutcome(Outcome{ID: good, Accepted: true, Reason: "served", At: at})

	judgedBad, _ := l.Belief(bad)
	judgedGood, _ := l.Belief(good)
	if judgedBad.Quality.Mean() >= judgedGood.Quality.Mean() {
		t.Fatalf("one soup answer left the lane at %.4f against a serving lane's %.4f",
			judgedBad.Quality.Mean(), judgedGood.Quality.Mean())
	}

	req := talkAt(scriptedModel, at)
	front := frontierFor(agedFor(l, scriptedModel, req), req, gateOptions{}, nil)
	if onFrontier(front, bad.Lane) {
		t.Fatalf("the two lanes are still peers after one of them served soup: %+v", front)
	}
	// NOT CONDEMNED. On its own it still passes its gate, so a model whose only
	// endpoint has one bad answer behind it is not an outage this process built
	// for itself.
	if solo := soloFront(l, bad, req); len(solo) != 1 {
		t.Fatalf("one bad answer refused the lane outright: %+v", solo)
	}

	// And the timing beliefs are untouched by any of it: quality is a different
	// claim about the same machine, and folding it into speed would make a lane
	// that answers badly look slow.
	if !judgedBad.QualityAt.Equal(at) {
		t.Fatalf("QualityAt = %v, want the moment of the outcome", judgedBad.QualityAt)
	}
	if !judgedBad.At.IsZero() {
		t.Fatalf("an outcome moved the TIMING moment to %v — quality is not a timing observation", judgedBad.At)
	}
}
