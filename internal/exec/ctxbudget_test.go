package exec

import (
	"strings"
	"testing"
)

// Every byte bound this package spends is a share of what the leaf can HOLD, or
// a named fallback for not knowing the window, and nothing in between.
//
// The literals are still written down — they are what an unrecognised model
// gets, and keeping them is what makes an offline run byte-for-byte what it
// always was — but they are no longer what a recognised model gets. That was
// the first defect: a leaf on a model holding a million tokens was handed the
// same twelve kilobytes of result as a leaf holding thirty-two thousand,
// because the numbers had been measured once against the small case and then
// written as absolutes.
//
// The law this test used to state was the correction to that, and it overshot:
// "a 1M model gets more than a 200k model" made the share a function of the
// window, while the memory the result has to live in was already a function of
// min(window, working set). The measured consequence is in toolBudgetsFor — one
// sh result permitted 7.5x the whole observation window that must carry it, so
// two results overflowed on arrival and the decay pass fired every turn. The
// honest law is the one below: the bounds converge on the NAMED WORKING SET,
// they are not a belief about a window, and the operator who raises the working
// set raises them with it.
func TestToolBudgetsConvergeOnTheNamedWorkingSet(t *testing.T) {
	// Unknown is the honest unknown, and it resolves to exactly the old numbers.
	unknown := toolBudgetsFor(0)
	if unknown != (toolBudgets{result: maxToolResultBytes, spill: spillBytes,
		preview: previewBytes, recall: maxRecallResultBytes}) {
		t.Fatalf("an unnamed model got %+v, want the named fallbacks", unknown)
	}

	// A model the catalog answered for still gets several times as much, and the
	// answer still follows the model rather than a constant.
	roomy := toolBudgetsFor(200_000)
	if roomy.result <= 2*maxToolResultBytes {
		t.Errorf("a 200k-token model may carry a %d-byte result, barely more than the %d fallback",
			roomy.result, maxToolResultBytes)
	}

	// Past the working set it stops following the window, because the memory
	// holding it stopped following the window there too. Every model above the
	// ceiling gets the ceiling's answer.
	huge := toolBudgetsFor(1 << 20)
	if huge != roomy {
		t.Errorf("a 1M-token model got %+v where a 200k model got %+v; both are over the "+
			"working set, so both must get the working set's answer", huge, roomy)
	}

	// And every bound is inside the window that has to hold it — the whole point
	// of the clamp. A single result may not be most of the leaf's memory.
	fitting := toolBudgetsFor(128_000)
	for _, context := range []int{128_000, 200_000, 1 << 20} {
		budgets, window := toolBudgetsFor(context), observationWindow(context)
		if budgets.result >= window/2 {
			t.Errorf("a %d-token model may carry a %d-byte result into a %d-byte observation "+
				"window — one result is half the leaf's memory", context, budgets.result, window)
		}
	}

	// A dial rather than a belief, exactly as the observation window is: the
	// operator with evidence of their own moves the ceiling and the bounds move.
	t.Setenv("AFORGE_WORKING_SET", "400000")
	if wider := toolBudgetsFor(1 << 20); wider.result <= huge.result ||
		wider.spill <= huge.spill || wider.preview <= huge.preview || wider.recall <= huge.recall {
		t.Errorf("with the working set raised to 400k the bounds are %+v, no more than the %+v "+
			"they were; the ceiling is a buried literal again", wider, huge)
	}
	// A model smaller than the working set is sized by itself either way: the
	// ceiling may only ever clamp down.
	if small := toolBudgetsFor(128_000); small != fitting {
		t.Errorf("raising the working set changed a 128k model's bounds from %+v to %+v",
			fitting, small)
	}

	// The ratios between them are the part that must survive being made
	// relative. Preview is what stays behind when a result spills, so it has to
	// be smaller than the threshold that sent it away; recall is a map rather
	// than a second copy of the territory, so it stays under a whole result.
	for name, budgets := range map[string]toolBudgets{
		"unknown": unknown, "200k": roomy, "1M": huge,
	} {
		if budgets.preview >= budgets.spill {
			t.Errorf("%s: preview %d is not smaller than the %d spill threshold",
				name, budgets.preview, budgets.spill)
		}
		if budgets.recall >= budgets.result {
			t.Errorf("%s: recall %d is not tighter than the %d whole-result bound",
				name, budgets.recall, budgets.result)
		}
		if budgets.spill >= budgets.result {
			t.Errorf("%s: spill %d is not below the %d whole-result bound",
				name, budgets.spill, budgets.result)
		}
	}
}

// The budgets are settled when the toolbox is built and read from there, which
// is a prompt-cache property rather than a performance one: these numbers decide
// how long a result is, and a bound that moved mid-run would rewrite messages
// the provider is otherwise serving warm.
func TestAToolboxCarriesItsBudgetsFromConstruction(t *testing.T) {
	sized := newToolbox(workspace(t), "1", nil, nil, nil, 1<<20)
	if sized.budgets != toolBudgetsFor(1<<20) {
		t.Fatalf("the toolbox carries %+v, not the budgets its window buys", sized.budgets)
	}
	if sized.jobs.results != sized.budgets.result {
		t.Errorf("the job registry clamps at %d where the toolbox clamps at %d — "+
			"a job report and a tool result must be bounded at the same place",
			sized.jobs.results, sized.budgets.result)
	}
	// And a spill on a large-window leaf leaves a proportionally larger preview
	// behind, which is the whole point of deriving the number.
	body := strings.Repeat("x", 4*sized.budgets.spill)
	spilled := sized.spill(Result{Content: body})
	if len(spilled.Content) <= previewBytes {
		t.Errorf("a 1M-token leaf kept %d bytes of preview, no more than the %d fallback",
			len(spilled.Content), previewBytes)
	}
	if len(spilled.Content) >= len(body) {
		t.Error("the result was not spilled at all")
	}
}

// An upstream result is bounded for the leaf that will READ it, and the leaf
// that will read it is the executor the node was routed to. A scheduler with
// nothing to ask — no registry, or a worker that cannot say what its model
// holds — falls back to the literal rather than guessing.
func TestInputBudgetFollowsTheConsumingLeaf(t *testing.T) {
	bare := &Scheduler{}
	if got := bare.inputBudget(""); got != maxInputBytes {
		t.Errorf("a scheduler with no registry bounded an input at %d, want the %d fallback",
			got, maxInputBytes)
	}

	silent := NewScheduler(NewRegistry(NewLinear(nil, nil, nil, 0, 0, 0)), nil, 1)
	if got := silent.inputBudget(""); got != maxInputBytes {
		t.Errorf("an executor that could not name its model bounded an input at %d, want %d",
			got, maxInputBytes)
	}

	roomy := NewScheduler(NewRegistry(
		NewLinear(nil, nil, nil, 0, 0, 0).WithContextLength(1<<20)), nil, 1)
	budget := roomy.inputBudget("")
	if budget <= 4*maxInputBytes {
		t.Fatalf("a 1M-token consumer was handed %d bytes of upstream result, "+
			"barely more than the %d an unrecognised model gets", budget, maxInputBytes)
	}
	// And the bound is the one boundInput actually applies.
	long := strings.Repeat("y", 2*budget)
	if kept := boundInput(long, nil, budget); len(kept) <= maxInputBytes {
		t.Errorf("boundInput kept %d bytes against a %d budget", len(kept), budget)
	}
}
