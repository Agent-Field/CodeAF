package head

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// measuredCents is the shape of the measured self-knowledge block the compiler
// is given: one line per measured population, each with a history of its own,
// each ending in the per-run cost that block actually writes.
func measuredCents(costs ...float64) string {
	lines := make([]string, 0, len(costs))
	for index, cost := range costs {
		lines = append(lines, fmt.Sprintf(
			"worker-%d: median 1200 tokens, 3 turns over 40 runs; 95%% succeeded; avg cost $%.4f",
			index+1, cost))
	}
	return "Measured execution costs (this system's own measured history):\n" + strings.Join(lines, "\n")
}

// THE FAILURE, EXACTLY (13.3's head edge).
//
// A charter proposal offered to run something at "$20.00 a run" against a
// measured cost of $0.0017 — ten thousand times the truth, in the one sentence
// a person is asked to consent to spending on. Nothing had computed $20.00: a
// model wrote it, in prose, beside numbers it had been shown, and the
// normalizer let it stand because it only ever replaced a rail the model had
// left empty.
//
// What this pins is not "that number is gone". It is that the model has no
// route to any number at all: whatever it writes into the rails is discarded,
// the rate comes off the measurement, and the sentence beside it is assembled
// from the rate rather than accepted as prose.
func TestACharterProposalNeverPricesAtTenThousandTimesTheMeasurement(t *testing.T) {
	fabricated := store.CharterSpec{
		Rails: store.CharterSpecRails{
			EstimatedCostUSD:       20.00,
			MaxPerDay:              4,
			MaxPerDayJustification: "each run is about $20.00 a run, so four a day is $80.00",
		},
	}
	spec := normalizeCharterSpec(fabricated,
		"every morning check whether the deploy queue is clear", measuredCents(0.0017))

	if spec.Rails.EstimatedCostUSD != 0.0017 {
		t.Fatalf("the rate is %v, want the measured 0.0017 — the model's guess survived", spec.Rails.EstimatedCostUSD)
	}
	if strings.Contains(spec.Rails.MaxPerDayJustification, "20.00") ||
		strings.Contains(spec.Rails.MaxPerDayJustification, "80.00") {
		t.Fatalf("the model's arithmetic rode into the sentence a person consents to: %q",
			spec.Rails.MaxPerDayJustification)
	}
	// And the sentence is not merely free of the lie — it is assembled from the
	// measurement, at a precision that does not round a real cost to nothing.
	if !strings.Contains(spec.Rails.MaxPerDayJustification, "$0.0017") {
		t.Fatalf("the money sentence does not quote the measured rate: %q", spec.Rails.MaxPerDayJustification)
	}
	if !strings.Contains(spec.Rails.MaxPerDayJustification, "at most 4 a day") {
		t.Fatalf("the ceiling the model DID legitimately choose was dropped: %q", spec.Rails.MaxPerDayJustification)
	}
	// Four runs at $0.0017 is under a cent, and saying "$0.00" would read as
	// free rather than as small.
	if !strings.Contains(spec.Rails.MaxPerDayJustification, "$0.0068") {
		t.Fatalf("the worst day was not computed from the measured rate: %q", spec.Rails.MaxPerDayJustification)
	}
}

// With nothing measured, the answer is the standing backstop AND the fact that
// nothing has been measured. A default quoted as a measurement is the same
// fabrication in a smaller size.
func TestAnUnmeasuredProposalSaysItIsUnmeasured(t *testing.T) {
	spec := normalizeCharterSpec(store.CharterSpec{
		Rails: store.CharterSpecRails{EstimatedCostUSD: 12.5, MaxPerDay: 0},
	}, "keep an eye on the build", "")

	if spec.Rails.EstimatedCostUSD != defaultStandingCostUSD {
		t.Fatalf("an unmeasured rail is %v, want the backstop %v", spec.Rails.EstimatedCostUSD, defaultStandingCostUSD)
	}
	if !strings.Contains(spec.Rails.MaxPerDayJustification, "nothing measured yet") {
		t.Fatalf("a backstop was quoted as a measurement: %q", spec.Rails.MaxPerDayJustification)
	}
	if strings.Contains(spec.Rails.MaxPerDayJustification, "12.5") {
		t.Fatalf("the model's guess survived into an unmeasured proposal: %q", spec.Rails.MaxPerDayJustification)
	}
}

// One line per measured population means several measurements, and reading only
// the first meant the price of every standing rule was set by whichever line
// sorted first. The middle one is the honest answer to "what does one run
// cost here".
func TestTheMeasuredRateIsTheMiddleOfEveryMeasurement(t *testing.T) {
	cost, ok := measuredStandingCost(measuredCents(0.0017, 0.4200, 0.0900))
	if !ok {
		t.Fatal("a block full of measurements read as unmeasured")
	}
	if cost != 0.09 {
		t.Fatalf("measured rate = %v, want the median 0.09", cost)
	}
	if _, ok := measuredStandingCost("nothing measurable here"); ok {
		t.Fatal("a block with no measurement in it produced one")
	}
}

// A real cost of a fraction of a cent must never render as "$0.00": a figure
// that rounds a true measurement to nothing reads as free, and a model told the
// measurement is meaningless reaches for one that is not.
func TestMoneyRendersAtThePrecisionItActuallyHas(t *testing.T) {
	for amount, want := range map[float64]string{
		0:        "$0.00",
		0.000004: "under $0.0001",
		0.0017:   "$0.0017",
		0.37:     "$0.37",
		18.4:     "$18.40",
	} {
		if got := moneyUSD(amount); got != want {
			t.Fatalf("moneyUSD(%v) = %q, want %q", amount, got, want)
		}
	}
}

// The spending read hands over finished figures, because the alternative is a
// model multiplying a window total by a number of days in prose. Every figure
// here is arithmetic over journaled rows, which 5.23 says is a template's job.
func TestTheSpendingReadComputesTheRateAndTheProjections(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph).WithDailyBudgetUSD(20)
	for index := 0; index < 10; index++ {
		if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 0.05}); err != nil {
			t.Fatal(err)
		}
	}
	// The window closes a moment after the rows land: the bounds are half-open,
	// and a window that ended at the instant they were written would hold none
	// of them.
	now := time.Now()
	lines := head.spendRateBlock(now.AddDate(0, 0, -10), now.Add(time.Minute))
	block := strings.Join(lines, "\n")
	if !strings.Contains(block, "$0.50 over 10 runs") {
		t.Fatalf("the window total and its run count are missing: %q", block)
	}
	if !strings.Contains(block, "$0.05 a run on average") {
		t.Fatalf("the per-run rate was not computed: %q", block)
	}
	if !strings.Contains(block, "the middle run of everything ever priced here cost $0.05") {
		t.Fatalf("the journal's own median run is missing: %q", block)
	}
	// Ten days of $0.50 is a nickel a day, and the projections are that rate
	// continued — labelled as a rate rather than as a forecast.
	if !strings.Contains(block, "$0.05 a day over those 10 days") {
		t.Fatalf("the daily rate was not computed: %q", block)
	}
	if !strings.Contains(block, "a week is $0.35 and thirty days is $1.50") {
		t.Fatalf("the projections were left for the model to multiply: %q", block)
	}
	if !strings.Contains(block, "never work one out yourself") {
		t.Fatalf("the block does not say the figures are the answer: %q", block)
	}
}

// An empty journal produces a sentence rather than a zero, because "nothing has
// been measured" and "it costs nothing" are different facts and only one of
// them is true.
func TestAnUnpricedJournalRefusesToQuoteARate(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	block := strings.Join(head.spendRateBlock(time.Time{}, time.Time{}), "\n")
	if !strings.Contains(block, "no rate to quote") {
		t.Fatalf("an unpriced journal produced a rate: %q", block)
	}
	if containsMoney(block) {
		t.Fatalf("an unpriced journal produced a figure: %q", block)
	}
}

// The law that makes the templates load-bearing has to actually be in front of
// the model, in the one prompt there now is.
func TestTheOnePromptForbidsArithmeticInProse(t *testing.T) {
	if !strings.Contains(orchestratorPrompt, "Numbers are quoted, never worked out") {
		t.Fatal("the one prompt no longer carries the deterministic-first principle about figures")
	}
	for _, clause := range []string{
		"must appear as that figure in something already in front of you",
		"Never carry a number from one label to another",
		"a daily limit is not what one run costs",
	} {
		if !strings.Contains(orchestratorPrompt, clause) {
			t.Fatalf("the law lost the clause %q, which is the shape the failure took", clause)
		}
	}
	// And the compiler is not asked for money it cannot know, because asking is
	// the invitation the failure accepted.
	if strings.Contains(standingCompilerPrompt, "estimated_cost_usd") {
		t.Fatal("the standing compiler still asks a model to price a firing")
	}
	if !strings.Contains(standingCompilerPrompt, "Never write a cost, a rate or any figure in dollars") {
		t.Fatal("the standing compiler does not forbid the figure it used to invite")
	}
}

// The spending tool is the door those figures come through, so the whole loop
// path has to carry them — not just the helper underneath it.
func TestTheSpendingToolHandsTheLoopFinishedFigures(t *testing.T) {
	graph := openHeadStore(t)
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 0.25}); err != nil {
		t.Fatal(err)
	}
	head := New(nil, graph).WithDailyBudgetUSD(20)
	run := &beltRun{head: head, user: postUser(t, graph, "money", "what am I spending?")}

	result, failed := run.execute(beltToolSpending, `{}`)
	if failed {
		t.Fatalf("the spending read failed: %s", result)
	}
	if !strings.Contains(result, "a run on average") || !strings.Contains(result, "thirty days is") {
		t.Fatalf("the spending read did not compute the rate and the projection:\n%s", result)
	}
}
