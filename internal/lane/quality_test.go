package lane

// ── WHAT THE SIMULATOR CORRECTED ────────────────────────────────────────────
//
// The gate and the ledger's law are the two places this design was wrong in a
// way no unit test of a seam could show, because both failures are about what
// happens over a WHOLE SESSION: a gate that refuses on the first request and
// then never gets the evidence that would reverse it, and a ledger that gets
// faster the more often a lane returns nothing. `bench/lanelab` found both, and
// the tests here are what they became. The argument is in
// ideation/provider-routing.md, Part III, and in the doc comments on
// [Beta.Upper], [Beta.Toward] and [Ledger.Note].

import (
	"testing"
	"time"
)

// ── C1: THE GATE ASKS THE BOUND ─────────────────────────────────────────────

// TestAFreshQualityPriorPassesEveryGateWeAsk is the bug in one assertion.
//
// Beta(8, 1) is what a lane the sheet has just described starts at, and its
// MEAN is 0.889 — under the 0.90 a talk turn asks for and well under the 0.97 a
// tool loop asks for. A gate on the mean therefore refused every lane of every
// model on the first request of every process, for want of evidence rather than
// for cause. The bound answers the question the gate means to ask.
func TestAFreshQualityPriorPassesEveryGateWeAsk(t *testing.T) {
	fresh := qualityPrior("fp8")
	if fresh.Mean() >= talkNeed {
		t.Fatalf("the prior's mean is %.3f, so this test no longer describes the trap it was written for", fresh.Mean())
	}
	if fresh.Upper(z90) < workNeed {
		t.Fatalf("a lane nobody has judged was refused work at a bound of %.3f", fresh.Upper(z90))
	}
	if fresh.Upper(z90) < talkNeed {
		t.Fatalf("a lane nobody has judged was refused a talk turn at a bound of %.3f", fresh.Upper(z90))
	}
}

// TestThreeRefusalsDropALaneFromATalkTurn is the other half: the bound must
// still be a gate, and real evidence must still close it.
func TestThreeRefusalsDropALaneFromATalkTurn(t *testing.T) {
	judged := qualityPrior("fp8")
	for range 3 {
		judged = judged.Observe(false)
	}
	if judged.Upper(z90) >= talkNeed {
		t.Fatalf("three answers nobody could use left the bound at %.3f, which is not a gate", judged.Upper(z90))
	}
}

// TestADroppedLaneComesBackAfterTwoHalfLives is why the drop is a suspicion and
// not a verdict. A lane outside the candidate set is sent nothing, so nothing
// can contradict the three answers that dropped it; forgetting is the only way
// back, and it is the same forgetting the timing beliefs already do.
func TestADroppedLaneComesBackAfterTwoHalfLives(t *testing.T) {
	prior := qualityPrior("fp8")
	dropped := prior
	for range 3 {
		dropped = dropped.Observe(false)
	}
	if back := dropped.Toward(prior, 2*QualityHalfLife, QualityHalfLife); back.Upper(z90) < talkNeed {
		t.Fatalf("two half-lives later the lane was still refused at a bound of %.3f", back.Upper(z90))
	}
	// And it comes back to the PRIOR rather than to certainty: a lane that has
	// been forgotten is one nobody has judged, which is the mass Beta(8, 1)
	// carries and no more.
	forgotten := dropped.Toward(prior, 100*QualityHalfLife, QualityHalfLife)
	if mass := forgotten.A + forgotten.B; mass < 8.99 || mass > 9.01 {
		t.Fatalf("a forgotten belief kept %.3f observations of evidence rather than the prior's nine", mass)
	}
}

// TestTheGateItselfDropsAndRestoresALane walks the same lane through the real
// gate rather than through the arithmetic, because the bound being right and
// the gate asking for it are two different claims.
func TestTheGateItselfDropsAndRestoresALane(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)

	talk := Request{
		Model: row.ID.Model, PromptTokens: 4000, Visible: 400, MaxTokens: 400,
		ValueOfTime: 90, QualityNeed: talkNeed, Horizon: 40, Now: noon,
	}
	if front := frontierFor(l.Beliefs(row.ID.Model), talk, gateOptions{}, nil); len(front) != 1 {
		t.Fatalf("a lane nobody has judged was not a candidate: %+v", front)
	}
	for n := range 3 {
		l.NoteOutcome(Outcome{ID: row.ID, Accepted: false, Reason: "empty", At: noon.Add(time.Duration(n) * time.Second)})
	}
	refused := talk
	refused.Now = noon.Add(3 * time.Second)
	if front := frontierFor(agedFor(l, row.ID.Model, refused), refused, gateOptions{}, nil); len(front) != 0 {
		t.Fatalf("three answers nobody could use left the lane in the candidate set: %+v", front)
	}
	later := talk
	later.Now = noon.Add(2 * QualityHalfLife)
	if front := frontierFor(agedFor(l, row.ID.Model, later), later, gateOptions{}, nil); len(front) != 1 {
		t.Fatalf("two half-lives with nothing new held the lane out of the set: %+v", front)
	}
}

// agedFor is the ageing the chooser does on read, done here so that a test of
// the gate can ask about a moment other than the one the ledger last wrote.
func agedFor(l Ledger, model string, req Request) []Belief {
	beliefs := l.Beliefs(model)
	for i, belief := range beliefs {
		if since := req.Now.Sub(belief.QualityAt); since > 0 && !belief.QualityAt.IsZero() {
			beliefs[i].Quality = belief.Quality.Toward(qualityPrior(belief.Facts.Quant), since, QualityHalfLife)
		}
	}
	return beliefs
}

// talkNeed and workNeed are the two gates the transport asks for, restated here
// so that this file's arithmetic is checked against the figures actually sent
// rather than against numbers of its own.
const (
	talkNeed = 0.90
	workNeed = 0.97
)

// ── C7: WHAT AN ANSWER IS ENTITLED TO TEACH ─────────────────────────────────

// TestAnEmptyAnswerTeachesQualityAndNothingElse is the perverse incentive the
// simulator found: a lane that returns nothing returns it QUICKLY, so a ledger
// that took its wait as a first-token measurement believed the lane faster the
// more often it failed.
func TestAnEmptyAnswerTeachesQualityAndNothingElse(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	before, _ := l.Belief(row.ID)

	l.Note(Sighting{ID: row.ID, TTFT: 40 * time.Millisecond, Tokens: 0, At: noon})
	after, _ := l.Belief(row.ID)
	if after.TTFT != before.TTFT {
		t.Fatalf("an answer with no tokens in it moved the first-token belief from %.0fms to %.0fms",
			before.TTFT.Mean(), after.TTFT.Mean())
	}
	if after.Rate != before.Rate {
		t.Fatal("an answer with no tokens in it moved the rate belief")
	}
	// The half of it that IS evidence still lands.
	l.NoteOutcome(Outcome{ID: row.ID, Accepted: false, Reason: "empty", At: noon})
	if judged, _ := l.Belief(row.ID); judged.Quality.Mean() >= before.Quality.Mean() {
		t.Fatalf("an empty answer taught the quality belief nothing: %+v", judged.Quality)
	}
}

// TestAShortAnswerTeachesTheFirstTokenAndNeverTheRate is the other half of the
// law. Eight tokens is a real answer and its wait is a real first-token
// measurement; its tokens per second is a measurement of a lane clearing its
// throat.
func TestAShortAnswerTeachesTheFirstTokenAndNeverTheRate(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	before, _ := l.Belief(row.ID)

	l.Note(Sighting{ID: row.ID, TTFT: 4 * time.Second, Gen: 80 * time.Millisecond, Tokens: 8, At: noon})
	after, _ := l.Belief(row.ID)
	if after.TTFT.Mean() <= before.TTFT.Mean() {
		t.Fatalf("a four-second wait on a short answer taught the first-token belief nothing: %.0fms", after.TTFT.Mean())
	}
	if after.Rate != before.Rate {
		t.Fatalf("eight tokens in eighty milliseconds was believed as a rate of %.0f tok/s", after.Rate.Mean())
	}
}

// ── C5: THE PRICE TERM HAS TO BE FELT ───────────────────────────────────────

// TestThePriceTermBitesAtTheAttentionValue is the case the simulator flagged: a
// work node with two thousand hidden tokens and somebody waiting on it, where
// one lane is both faster and four times cheaper than another and must win.
//
// It is a test of a UNIT as much as of a ranking. λ is seconds per dollar, so
// money becomes seconds by multiplication; the code divided, which made every
// price term about eight thousand times too small to be felt, and left the four
// lanes that start within a hundred milliseconds of each other to be ordered by
// the sampler's noise. See [scoreOf].
func TestThePriceTermBitesAtTheAttentionValue(t *testing.T) {
	l := newLedger()
	for _, row := range e2eRows() {
		l.Prime(row, SheetWeight)
	}
	// Hidden tokens, because hidden work is the shape where throughput is
	// linear and a rate difference is a real difference; no tools, because two
	// of the six lanes on this sheet do not take them and the gate would settle
	// the question before the price could.
	work := Request{
		Model: e2eModel, PromptTokens: e2ePromptTokens,
		Visible: 0, Hidden: 2000, MaxTokens: 2000,
		ValueOfTime: AttentionValue, QualityNeed: workNeed, Horizon: 40, Now: e2eMoment,
	}
	choice := (&chooser{ledger: l}).Choose(work)
	if len(choice.Frontier) == 0 {
		t.Fatal("nothing survived the gate")
	}
	baidu, cloudflare := scoredLane(choice, "Baidu"), scoredLane(choice, "Cloudflare")
	if baidu == nil {
		t.Fatalf("the faster, cheaper lane was not a candidate at all: %+v", choice.Frontier)
	}
	if cloudflare == nil {
		// Being pruned off the frontier is a stronger form of losing than being
		// scored behind, and it is the right answer too.
		return
	}
	if baidu.Score >= cloudflare.Score {
		t.Fatalf("Baidu scored %.4f and Cloudflare %.4f: a lane that is faster AND four times "+
			"cheaper has to win, or the price term is not being felt",
			baidu.Score, cloudflare.Score)
	}
	// And the size of the money, stated: at ninety seconds to the dollar the
	// gap between $0.28 and $1.32 per million output tokens is worth a third of
	// a second on two thousand tokens, which is a real term beside a
	// three-quarter-second first token and a rounding error when divided.
	if gap := (cloudflare.Price - baidu.Price) * AttentionValue; gap < 0.1 {
		t.Fatalf("the price gap between the two lanes came to %.4f seconds, which nothing could feel", gap)
	}
}

// TestTwoLanesOfTheSameSpeedAreSeparatedByMoney is the talk turn, and it is the
// decision [PerceivedSeconds] leaves entirely to the price: above the reading
// rate every lane is the same speed to a person, so the cheaper one must win
// and nothing else may decide it.
func TestTwoLanesOfTheSameSpeedAreSeparatedByMoney(t *testing.T) {
	dear := Facts{PriceIn: 5.28e-7, PriceOut: 1.32e-6}
	cheap := Facts{PriceIn: 1.12e-7, PriceOut: 2.8e-7}
	talk := Request{PromptTokens: 4000, Visible: 400, MaxTokens: 400, ValueOfTime: AttentionValue}
	// The same first token and two rates that are both above reading speed.
	felt := PerceivedSeconds(0.8, 58, 400, 0)
	same := PerceivedSeconds(0.8, 75, 400, 0)
	if felt != same {
		t.Fatalf("the two rates were not the same wait: %.3f and %.3f", felt, same)
	}
	dearScore := scoreOf(PriceOf(dear, talk), felt, AttentionValue)
	cheapScore := scoreOf(PriceOf(cheap, talk), same, AttentionValue)
	if cheapScore >= dearScore {
		t.Fatalf("at the same speed the dearer lane scored %.4f and the cheaper %.4f", dearScore, cheapScore)
	}
	if dearScore-cheapScore < 0.05 {
		t.Fatalf("a four-times price difference was worth %.4f seconds, which is not a decision",
			dearScore-cheapScore)
	}
}

// scoredLane is one lane's row of the frontier, nil when it was pruned.
func scoredLane(choice Choice, lane string) *Scored {
	for i := range choice.Frontier {
		if choice.Frontier[i].ID.Lane == lane {
			return &choice.Frontier[i]
		}
	}
	return nil
}

// ── C1 AGAIN: A GATE THAT CANNOT FIRE IS NOT A LENIENT GATE ─────────────────

// TestTheGateStillFiresWhenEveryRequestTakesMinutes is the second half of the
// bug the bound was meant to fix, and the half the first fix caused.
//
// Ageing quality at [HalfLife] made the gate INERT rather than absorbing. A
// tool loop's request takes minutes, so a belief about answers was forgotten
// faster than answers arrived to build it: the evidence sat for ever at the
// prior's nine observations, and at that mass the 90% bound on a lane refusing
// one answer in six is still 1.000. Every lane passed every gate for ever,
// which reads as "the gate never fires" and is exactly as wrong as "the gate
// always fires". [QualityHalfLife] is the fix: it must be long enough that
// evidence gathered at the pace of real requests outruns the forgetting.
func TestTheGateStillFiresWhenEveryRequestTakesMinutes(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)

	// A tool loop on a slow lane: one answer every five minutes and forty
	// seconds, which is what `bench/lanelab`'s `work` scenario measures, and one
	// refusal in every six.
	const cadence = 340 * time.Second
	at := noon
	for n := range 60 {
		at = at.Add(cadence)
		l.NoteOutcome(Outcome{ID: row.ID, Accepted: n%6 != 0, Reason: "empty", At: at})
	}

	req := Request{
		Model: row.ID.Model, PromptTokens: 4000, Visible: 0, MaxTokens: 2000,
		ValueOfTime: 90, QualityNeed: workNeed, Horizon: 40, Now: at,
	}
	beliefs := agedFor(l, row.ID.Model, req)
	if len(beliefs) != 1 {
		t.Fatalf("the ledger lost the only lane it was taught: %+v", beliefs)
	}
	quality := beliefs[0].Quality
	if mass := quality.A + quality.B; mass < 20 {
		t.Fatalf("five hours of answers left %.1f observations of evidence — forgetting outran the evidence", mass)
	}
	if bound := quality.Upper(z90); bound >= workNeed {
		t.Fatalf("a lane refusing one answer in six kept a bound of %.3f against a need of %.2f", bound, workNeed)
	}
	if front := frontierFor(beliefs, req, gateOptions{}, nil); len(front) != 0 {
		t.Fatalf("the gate kept a lane that refuses one answer in six: %+v", front)
	}
}
