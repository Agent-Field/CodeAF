package lane

import (
	"math"
	"testing"
	"time"
)

// ── A REFUSAL IS EVIDENCE ───────────────────────────────────────────────────
//
// THE MEASURED FAILURE (docs/design/routing/ASSESSMENT-20260911.md). In the
// three days to 2026-09-10 a 429 reached the strike ledger and never the belief,
// so the request that followed a refusal asked the same pool 825 times in 1,010,
// two seconds later. Replayed with refusals recorded, the chooser's mean regret
// on deepseek-v4.1-flash fell from 31.9 s to 0.4 s. These tests hold that door.

func refusedBelief(t *testing.T, l *ledger, id ID, refusals int, at time.Time) Belief {
	t.Helper()
	for i := 0; i < refusals; i++ {
		l.NoteOutcome(Outcome{ID: id, Refused: true, Reason: "paced", At: at})
	}
	belief, ok := l.Belief(id)
	if !ok {
		t.Fatalf("no belief for %+v after %d refusals", id, refusals)
	}
	return belief
}

func TestARefusalMovesAvailabilityAndNotQuality(t *testing.T) {
	l := newLedger()
	id := ID{Model: scriptedModel, Lane: "DeepInfra"}
	l.NoteOutcome(Outcome{ID: id, Accepted: true, At: noon})
	before, _ := l.Belief(id)
	after := refusedBelief(t, l, id, 1, noon.Add(time.Second))
	if after.Quality != before.Quality {
		t.Fatalf("a refusal moved quality from %+v to %+v; it is not an answer that can be judged", before.Quality, after.Quality)
	}
	if !after.Availability.Known() || after.Availability.B < 1 {
		t.Fatalf("a refusal left availability at %+v, want a refusal counted", after.Availability)
	}
	if got := after.Serving(); got >= before.Serving() {
		t.Fatalf("serving share went from %.2f to %.2f after a refusal, want lower", before.Serving(), got)
	}
}

func TestFourRefusalsInFiveCostFiveSendsPerAnswer(t *testing.T) {
	l := newLedger()
	id := ID{Model: scriptedModel, Lane: "DeepInfra"}
	l.NoteOutcome(Outcome{ID: id, Accepted: true, At: noon})
	belief := refusedBelief(t, l, id, 4, noon.Add(time.Second))
	// The prior is one answer; one more answered and four refused is two in six.
	if got, want := belief.Serving(), 2.0/6.0; math.Abs(got-want) > 0.01 {
		t.Fatalf("serving share = %.3f, want %.3f", got, want)
	}
}

func TestARefusalIsForgottenOverItsHalfLife(t *testing.T) {
	l := newLedger()
	id := ID{Model: scriptedModel, Lane: "DeepInfra"}
	belief := refusedBelief(t, l, id, 4, noon)
	fresh := servingAt(belief, noon)
	later := servingAt(belief, noon.Add(4*AvailabilityHalfLife))
	if later <= fresh {
		t.Fatalf("serving share %.2f did not recover to more than %.2f after four half-lives", later, fresh)
	}
	if later < 0.75 {
		t.Fatalf("serving share only recovered to %.2f after four half-lives; a queue that drained is held against the lane", later)
	}
}

// TestARefusingFastLaneRanksBehindAnAnsweringSlowerOne is the point of the
// axis: the wait is paid per send, so a lane refusing four in five is five
// sends per answer and goes behind a slower lane that answers every time.
func TestARefusingFastLaneRanksBehindAnAnsweringSlowerOne(t *testing.T) {
	l := newLedger()
	fast := ID{Model: scriptedModel, Lane: "DeepInfra"}
	steady := ID{Model: scriptedModel, Lane: "Novita"}
	for i := 0; i < 6; i++ {
		l.Note(Sighting{ID: fast, TTFT: 800 * time.Millisecond, Gen: 2 * time.Second, Tokens: 400, At: noon.Add(time.Duration(i) * time.Second)})
		l.Note(Sighting{ID: steady, TTFT: 1500 * time.Millisecond, Gen: 3 * time.Second, Tokens: 400, At: noon.Add(time.Duration(i) * time.Second)})
	}
	chooser := &chooser{ledger: l, pages: newSheet()}
	ask := Request{Model: scriptedModel, Visible: 400, ValueOfTime: AttentionValue, QualityNeed: 0.9, Horizon: 50, Now: noon.Add(10 * time.Second), Typical: true}
	if head := HeadOf(chooser.Choose(ask)); head != "DeepInfra" {
		t.Fatalf("before any refusal the quicker lane leads; got %q", head)
	}
	for i := 0; i < 4; i++ {
		l.NoteOutcome(Outcome{ID: fast, Refused: true, Reason: "paced", At: noon.Add(10 * time.Second)})
	}
	if head := HeadOf(chooser.Choose(ask)); head != "Novita" {
		t.Fatalf("after four refusals the lane that answers leads; got %q", head)
	}
}

// ── THE QUALITY GATE CLOSES ONCE THERE IS EVIDENCE ──────────────────────────

func TestTheQualityGateReadsTheMeanOnceJudgedEnough(t *testing.T) {
	few := Beta{A: 5, B: 1}   // a mean of 0.83 on six observations
	many := Beta{A: 10, B: 2} // the same mean on twelve
	if qualityBound(few) < 0.9 {
		t.Fatalf("a lane judged %v times was refused on its bound %.2f; the bound must stay open until there is evidence", few.A+few.B, qualityBound(few))
	}
	if qualityBound(many) >= 0.9 {
		t.Fatalf("a lane judged %v times at a 0.65 mean cleared 0.9 on %.2f; the gate never closes", many.A+many.B, qualityBound(many))
	}
}

// ── THE SERVICE FLOOR ───────────────────────────────────────────────────────

func sureBelief(lane string, ttftMS, rate float64) Belief {
	return Belief{
		ID:   ID{Model: scriptedModel, Lane: lane},
		TTFT: Posterior{X: math.Log(ttftMS), P: 0.05},
		Rate: Posterior{X: math.Log(rate), P: 0.05},
		At:   noon,
	}
}

func TestALaneSurelyUnderTheFloorIsNotACandidate(t *testing.T) {
	morph := sureBelief("Morph", 27_000, 6)
	quick := sureBelief("Novita", 1_800, 234)
	crawl := sureBelief("DeepInfra", 1_500, 12)
	if !underFloor(morph, noon) {
		t.Fatal("twenty-seven seconds to a first token at six tokens a second is over the floor")
	}
	if !underFloor(crawl, noon) {
		t.Fatal("twelve tokens a second is under the rate floor")
	}
	if underFloor(quick, noon) {
		t.Fatal("a lane at 1.8 s and 234 tok/s was put under the floor")
	}
	// A wide belief is never refused on the floor: the ledger is not sure.
	wide := morph
	wide.TTFT.P, wide.Rate.P = 2, 2
	if underFloor(wide, noon) {
		t.Fatal("a belief the ledger is not sure of was refused on the floor")
	}
	front := frontierFor([]Belief{morph, quick, crawl}, Request{Model: scriptedModel, Visible: 400, ValueOfTime: AttentionValue, QualityNeed: 0.9, Now: noon}, gateOptions{}, nil)
	if len(front) != 1 || front[0].ID.Lane != "Novita" {
		t.Fatalf("frontier = %+v, want Novita alone", front)
	}
}

func TestTheFloorNeverEmptiesTheSet(t *testing.T) {
	morph := sureBelief("Morph", 27_000, 6)
	crawl := sureBelief("DeepInfra", 1_500, 12)
	front := frontierFor([]Belief{morph, crawl}, Request{Model: scriptedModel, Visible: 400, ValueOfTime: AttentionValue, QualityNeed: 0.9, Now: noon}, gateOptions{}, nil)
	if len(front) == 0 {
		t.Fatal("a model with no lane over the floor was left with no lane at all")
	}
}

func TestARefusingLaneIsUnderTheFloorOnEvidenceOnly(t *testing.T) {
	l := newLedger()
	id := ID{Model: scriptedModel, Lane: "DeepInfra"}
	one := refusedBelief(t, l, id, 1, noon)
	if underFloor(one, noon) {
		t.Fatal("one refusal put a lane under the floor; the floor needs evidence")
	}
	four := refusedBelief(t, l, id, 3, noon)
	if !underFloor(four, noon) {
		t.Fatalf("four refusals and one prior answer (%+v) is under half and stays a candidate", four.Availability)
	}
}
