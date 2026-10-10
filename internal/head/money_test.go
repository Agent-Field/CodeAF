package head

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// A real cost of a fraction of a cent must never render as "$0.00": a figure
// that rounds a true measurement to nothing reads as free, and a model told the
// measurement is meaningless reaches for one that is not.
func TestMoneyRendersAtThePrecisionItActuallyHas(t *testing.T) {
	for amount, want := range map[float64]string{
		0:        "$0.00",
		0.000004: "under $0.0001",
		0.00048:  "$0.0005",
		0.0017:   "$0.0017",
		0.37:     "$0.37",
		1.24:     "$1.24",
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
