package provider

import (
	"context"
	"sync"
	"testing"
)

// TestVerdictsGradeOnlyWhatIsEvidence is the rule the ledger depends on, stated
// where the taxonomy is. Two of the eight verdicts are deliberately inert, and
// which two is the whole argument: a transport failure says nothing about a
// model, and an answer nobody checked says nothing about an answer.
func TestVerdictsGradeOnlyWhatIsEvidence(t *testing.T) {
	tests := []struct {
		verdict  Verdict
		positive bool
		graded   bool
	}{
		{VerdictVerifiedSuccess, true, true},
		{VerdictUnverifiedSuccess, false, false},
		{VerdictProviderFailure, false, false},
		{VerdictFormatFailure, false, true},
		{VerdictSemanticFailure, false, true},
		{VerdictBudgetStop, false, true},
		{VerdictTurnCap, false, true},
		{VerdictEmptyResponse, false, true},
	}
	for _, test := range tests {
		t.Run(string(test.verdict), func(t *testing.T) {
			positive, graded := test.verdict.Graded()
			if positive != test.positive || graded != test.graded {
				t.Fatalf("Graded() = %t, %t; want %t, %t", positive, graded, test.positive, test.graded)
			}
			if got := test.verdict.Escalates(); got != (graded && !positive) {
				t.Fatalf("Escalates() = %t", got)
			}
		})
	}
}

// TestAnUnroutedCallIsANoOpEverywhere is the kill switch at the level it
// actually has to hold. Every method on this file's mechanism runs on the
// single-model path too, and none of it may do anything there.
func TestAnUnroutedCallIsANoOpEverywhere(t *testing.T) {
	ctx := context.Background()
	if CallFrom(ctx) != nil {
		t.Fatal("a bare context carries a call slot")
	}
	if CallClassFrom(ctx) != "" || CallFrom(ctx).Class() != "" || CallFrom(ctx).Attempt() != 0 {
		t.Fatal("a nil slot answered as though it were open")
	}
	if got := CallFrom(ctx).Pin("a/model"); got != "a/model" {
		t.Fatalf("Pin on a nil slot = %q, want the caller's own choice back", got)
	}
	CallFrom(ctx).Observe(func(Verdict) { t.Fatal("an observer on a nil slot was called") })
	Report(ctx, VerdictVerifiedSuccess)
}

// TestTheFirstReportWins keeps one unit of work to one observation. An error
// path that reports and then falls through to a shared return would otherwise
// count twice, and the second count would be the wrong one.
func TestTheFirstReportWins(t *testing.T) {
	ctx := WithCall(context.Background(), ClassPlanSpine)
	var seen []Verdict
	CallFrom(ctx).Observe(func(verdict Verdict) { seen = append(seen, verdict) })

	Report(ctx, VerdictSemanticFailure)
	Report(ctx, VerdictVerifiedSuccess)
	if len(seen) != 1 || seen[0] != VerdictSemanticFailure {
		t.Fatalf("observed %v, want only the first verdict", seen)
	}
	// A verdict reported before anything registered is not resurrected by a
	// later registration: the call is settled, and settling it twice would
	// attribute one outcome to two attempts.
	late := WithCall(context.Background(), ClassPlanBind)
	Report(late, VerdictVerifiedSuccess)
	CallFrom(late).Observe(func(Verdict) { t.Fatal("an observer registered after the verdict was fired") })
	Report(late, VerdictVerifiedSuccess)
}

// TestPinHoldsForTheWholeLoop is the cache-lineage guarantee. Concurrency is in
// the test because a leaf's turns are serial but the harness runs many leaves at
// once against one router, and the slot is per-leaf state a router writes to.
func TestPinHoldsForTheWholeLoop(t *testing.T) {
	ctx := WithCall(context.Background(), ClassExecLeaf)
	first := CallFrom(ctx).Pin("cheap/one")

	var group sync.WaitGroup
	for _, candidate := range []string{"mid/two", "top/three", "cheap/one"} {
		group.Add(1)
		go func(candidate string) {
			defer group.Done()
			if got := CallFrom(ctx).Pin(candidate); got != first {
				t.Errorf("Pin returned %q mid-loop, want the pinned %q", got, first)
			}
		}(candidate)
	}
	group.Wait()
}

// TestAnOuterClassOverridesTheCallSite covers the one case where a pass means
// two different things. A fan-out at the top of a plan and the same code inside
// an expansion are different populations, and pooling them would learn the
// average of two things that could have been known separately.
func TestAnOuterClassOverridesTheCallSite(t *testing.T) {
	inner := WithCall(context.Background(), ClassPlanFanOut)
	if CallClassFrom(inner) != ClassPlanFanOut {
		t.Fatal("a call site cannot name its own class")
	}
	expansion := WithCallClass(context.Background(), ClassPlanExpand)
	if got := CallClassFrom(WithCall(expansion, ClassPlanFanOut)); got != ClassPlanExpand {
		t.Fatalf("class = %s, want the expansion's override", got)
	}
	if got := CallClassFrom(WithCall(expansion, ClassPlanSize)); got != ClassPlanExpand {
		t.Fatalf("class = %s, want every call under the expansion to inherit it", got)
	}
}

// TestRetriesCarryTheirAttemptNumber is how a caller asks for escalation without
// knowing what it is escalating to.
func TestRetriesCarryTheirAttemptNumber(t *testing.T) {
	if got := CallFrom(WithCall(context.Background(), ClassExecLeaf)).Attempt(); got != 0 {
		t.Fatalf("a first attempt reported %d", got)
	}
	if got := CallFrom(WithCallAttempt(context.Background(), ClassExecLeaf, 2)).Attempt(); got != 2 {
		t.Fatalf("attempt = %d, want 2", got)
	}
	if got := CallFrom(WithCallAttempt(context.Background(), ClassExecLeaf, -1)).Attempt(); got != 0 {
		t.Fatalf("a negative attempt reached the router as %d", got)
	}
}
