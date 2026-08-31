package lane

import (
	"context"
	"math"
	"testing"
	"time"
)

// ── A LANE NOBODY HAS LOOKED UP ─────────────────────────────────────────────
//
// These tests are written from one afternoon's incident. A machine held a
// seventeen-row sheet for a model in memory and TWO beliefs about it on disk,
// both of them lanes that had merely been seen: an id, a moment, and every
// other field a zero nobody wrote. Read as published facts, those zeros said
// "refuses tool calls, will write no tokens, reads no prompt, is free" — and
// the first request carrying a tool definition found the candidate set empty.
//
// The law they hold is one sentence: A ZERO NOBODY WROTE IS UNKNOWN, AND
// UNKNOWN IS NEVER A REASON TO REFUSE.

// seenOnly is a belief as it arrives from a sighting alone: the id and the
// timing, and nothing about what the machine IS.
func seenOnly(model, lane string, ttft float64) Belief {
	return Belief{
		ID:   ID{Model: model, Lane: lane},
		TTFT: Posterior{X: math.Log(ttft), P: 0.1},
		Rate: Posterior{X: math.Log(60), P: 0.1},
		At:   noon,
	}
}

// wideRequest asks for everything a gate can be asked about at once, so that a
// lane which passes it has passed every clause rather than the first one.
func wideRequest() Request {
	return Request{
		Model:        scriptedModel,
		PromptTokens: 21_600,
		Visible:      600,
		Tools:        true,
		MaxTokens:    8_000,
		QualityNeed:  0.9,
		Horizon:      40,
		ValueOfTime:  AttentionValue,
		Now:          noon,
	}
}

// TestASightingOnlyLaneIsNotRefusedForWhatNobodyPublished walks every clause of
// the gate over a lane with no facts at all.
func TestASightingOnlyLaneIsNotRefusedForWhatNobodyPublished(t *testing.T) {
	belief := seenOnly(scriptedModel, "DeepInfra", 1490)
	if !capable(belief, wideRequest(), gateOptions{}) {
		t.Fatal("a lane the sheet has never spoken about was refused for facts nobody published")
	}
	// And each clause on its own, so that a future gate cannot pass this test
	// by refusing for a different reason than the one it used to refuse for.
	for _, only := range []struct {
		what string
		req  Request
	}{
		{"a tool call", Request{Model: scriptedModel, Tools: true, Now: noon}},
		{"an answer ceiling", Request{Model: scriptedModel, MaxTokens: 32_000, Now: noon}},
		{"a long prompt", Request{Model: scriptedModel, PromptTokens: 500_000, Now: noon}},
		{"a quality need", Request{Model: scriptedModel, QualityNeed: 0.99, Now: noon}},
	} {
		if !capable(belief, only.req, gateOptions{}) {
			t.Errorf("a lane with no published facts was refused over %s", only.what)
		}
	}
	// The four zeros, said out loud: none of them is a claim.
	if belief.Facts.Known() {
		t.Fatal("a belief built from a sighting alone reports published facts")
	}
	if lowQuantization(belief.Facts.Quant) {
		t.Fatal("an unstated quantization was read as four-bit")
	}
}

// TestARowThatSaysNoToToolsIsStillRefused is the other half of the same law: a
// fact the sheet DID publish is a fact the gate acts on, and widening the
// unknown case must not widen the published one.
func TestARowThatSaysNoToToolsIsStillRefused(t *testing.T) {
	belief := seenOnly(scriptedModel, "Morph", 6409)
	belief.Facts = Facts{Tools: false, Quant: "fp8", MaxOut: 8_000, Context: 128_000, Uptime5m: 100, PriceOut: 0.0000009}
	if capable(belief, Request{Model: scriptedModel, Tools: true, Now: noon}, gateOptions{}) {
		t.Fatal("a lane the sheet says will not take a tool call was admitted to a request that carries one")
	}
}

// TestAKnownToolsLaneOutranksAnUnknownOne is the tiebreak that replaced the
// gate. The unknown lane stays a candidate — it is faster, so it would have led
// the order on the numbers — and it is asked second all the same.
func TestAKnownToolsLaneOutranksAnUnknownOne(t *testing.T) {
	quick := seenOnly(scriptedModel, "Unknown", 400)
	slow := seenOnly(scriptedModel, "Modal", 900)
	slow.Facts = Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 256_000, Uptime5m: 100, PriceOut: 0.0000024}
	aged := map[ID]Belief{quick.ID: quick, slow.ID: slow}

	order := []Scored{{ID: quick.ID, TTFT: 400}, {ID: slow.ID, TTFT: 900}}
	with := toolsLast(order, Request{Tools: true}, aged)
	if with[0].ID.Lane != "Modal" {
		t.Fatalf("a request carrying tools was sent first to %q, which nobody has said takes one", with[0].ID.Lane)
	}
	if len(with) != 2 || with[1].ID.Lane != "Unknown" {
		t.Fatalf("the unknown lane left the candidate set rather than being ranked behind: %+v", with)
	}
	// A request with no tools is left exactly as it was ranked.
	without := toolsLast(order, Request{}, aged)
	if without[0].ID.Lane != "Unknown" {
		t.Fatalf("a request with no tools was reordered: %+v", without)
	}
	// And with nothing known either way there is nothing to rank behind.
	onlyUnknown := []Scored{{ID: quick.ID}}
	if got := toolsLast(onlyUnknown, Request{Tools: true}, aged); len(got) != 1 {
		t.Fatalf("the only lane there was left the set: %+v", got)
	}
}

// TestAnUnknownTariffIsNotAFreeOne keeps a lane nobody has priced from winning
// every request with nobody waiting, where the score IS the price.
func TestAnUnknownTariffIsNotAFreeOne(t *testing.T) {
	// It is the FASTEST lane in the set, so it survives the prune on the axis
	// it has evidence on and the test is really about the axis it does not.
	unknown := seenOnly(scriptedModel, "Unknown", 700)
	priced := seenOnly(scriptedModel, "Modal", 900)
	priced.Facts = Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 256_000, Uptime5m: 100, PriceOut: 0.0000024}
	dearer := seenOnly(scriptedModel, "Fireworks", 1000)
	dearer.Facts = Facts{Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 256_000, Uptime5m: 100, PriceOut: 0.0000028}

	req := Request{Model: scriptedModel, Visible: 600, Horizon: 40, Now: noon}
	front := frontierFor([]Belief{unknown, priced, dearer}, req, gateOptions{}, nil)
	for _, candidate := range front {
		if candidate.ID.Lane != "Unknown" {
			continue
		}
		if candidate.Price <= 0 {
			t.Fatal("a lane nobody has published a tariff for was priced at nothing, which wins every request with nobody waiting")
		}
		return
	}
	t.Fatalf("the unpriced lane left the candidate set entirely: %+v", front)
}

// ── DRESSING A LANE THE SHEET ALREADY KNOWS ─────────────────────────────────

// TestALaneTheSheetKnowsIsDressedTheMomentItIsSeen is why a sighting-only
// belief should be rare rather than merely survivable: the row is usually
// sitting in memory, so it is taken.
func TestALaneTheSheetKnowsIsDressedTheMomentItIsSeen(t *testing.T) {
	row := cloudflareRow()
	row.At = noon
	l := newLedger()
	l.readFrom(&fixedSheet{rows: map[string][]Row{scriptedModel: {row}}})
	l.Note(Sighting{ID: row.ID, TTFT: 700 * time.Millisecond, Gen: time.Second, Tokens: 64, At: noon})

	belief, ok := l.Belief(row.ID)
	if !ok {
		t.Fatal("the sighting was not believed")
	}
	if belief.Facts != row.Facts {
		t.Fatalf("a lane the sheet knows was filed with no facts: %+v", belief.Facts)
	}
	if !belief.Quality.Known() {
		t.Fatal("the quality prior was not adopted from the row's quantization")
	}
	if !belief.Rate.Known() {
		t.Fatal("a sighting too short to rate left the rate belief empty when the sheet had one")
	}
	// A lane the sheet does not know is left an honest blank rather than
	// dressed from somebody else's row.
	l.Note(Sighting{ID: ID{Model: scriptedModel, Lane: "Nobody"}, TTFT: time.Second, Gen: time.Second, Tokens: 64, At: noon})
	stranger, _ := l.Belief(ID{Model: scriptedModel, Lane: "Nobody"})
	if stranger.Facts.Known() {
		t.Fatalf("a lane the sheet has never named was dressed anyway: %+v", stranger.Facts)
	}
}

// fixedSheet is a sheet that holds what it was handed and fetches nothing.
type fixedSheet struct{ rows map[string][]Row }

func (s *fixedSheet) Rows(model string) []Row               { return s.rows[BareModel(model)] }
func (s *fixedSheet) Refresh(context.Context, string) error { return nil }

// ── A TIER IS NOT A DEPLOYMENT ──────────────────────────────────────────────

// TestATierSuffixReadsTheBareModelsLanes is the second half of the incident: a
// talk model spelled `…:high` asked a ledger that had every one of its lanes
// under `…` and was told there was nothing to choose between.
func TestATierSuffixReadsTheBareModelsLanes(t *testing.T) {
	const bare, tier = "moonshotai/kimi-k3", "moonshotai/kimi-k3:high"
	l := newLedger()
	for _, row := range twoProcessRows(bare) {
		l.Prime(row, SheetWeight)
	}
	if got := len(l.Beliefs(tier)); got != 3 {
		t.Fatalf("%s reads %d of %s's lanes", tier, got, bare)
	}
	choice := (&chooser{ledger: l}).Choose(Request{
		Model: tier, Visible: 600, Tools: true, QualityNeed: 0.9,
		Horizon: 40, ValueOfTime: AttentionValue, Now: noon,
	})
	if choice.Empty() {
		t.Fatal("a request for a tier of a model with three known lanes came back with no opinion at all")
	}
	if len(choice.Order) == 0 || choice.Order[0] == "" {
		t.Fatalf("the choice named nothing to try first: %+v", choice)
	}
}

// TestASightingUnderATierSuffixLandsOnTheBareModel closes the loop: what is
// measured while talking to a tier teaches the beliefs the tier reads.
func TestASightingUnderATierSuffixLandsOnTheBareModel(t *testing.T) {
	const bare, tier = "moonshotai/kimi-k3", "moonshotai/kimi-k3:high"
	l := newLedger()
	l.Note(Sighting{ID: ID{Model: tier, Lane: "Modal"}, TTFT: 900 * time.Millisecond, Gen: time.Second, Tokens: 64, At: noon})
	if _, ok := l.Belief(ID{Model: bare, Lane: "Modal"}); !ok {
		t.Fatal("a sighting taken while talking to a tier was filed where nothing will ever read it")
	}
	l.NoteOutcome(Outcome{ID: ID{Model: tier, Lane: "Modal"}, Accepted: true, At: noon})
	belief, _ := l.Belief(ID{Model: tier, Lane: "Modal"})
	if !belief.Quality.Known() {
		t.Fatal("an outcome recorded under a tier did not reach the belief the tier reads")
	}
}

// TestBareModelLeavesAVariantAlone is why the list of tier words is closed. A
// free endpoint is a DIFFERENT set of machines wearing the same model name, and
// crediting its wait to the paid lane of the same name is the mis-attribution
// this package refuses everywhere else.
func TestBareModelLeavesAVariantAlone(t *testing.T) {
	for _, tier := range []string{
		"moonshotai/kimi-k3:high", "moonshotai/kimi-k3:nitro", "moonshotai/kimi-k3:floor",
		"moonshotai/kimi-k3:low", "moonshotai/kimi-k3:MEDIUM",
	} {
		if got := BareModel(tier); got != "moonshotai/kimi-k3" {
			t.Errorf("%s bared to %q", tier, got)
		}
	}
	for _, kept := range []string{
		"moonshotai/kimi-k3:free", "anthropic/claude-sonnet-4:beta",
		"moonshotai/kimi-k3", "openrouter/auto", "", "model:",
	} {
		if got := BareModel(kept); got != kept {
			t.Errorf("%q was rewritten to %q; only a tier word comes off", kept, got)
		}
	}
}
