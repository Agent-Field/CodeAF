package revision

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// refusedGates is the DeepSWE sweep's own record, read rather than retyped:
// every delivery gate across the ten runs that named a gap and refused it, with
// the request and working method that gate was weighed against. The fixture was
// extracted from the runs' stores under bench/deepswe/results — the eight
// requests and eleven gate events, and nothing else out of a megabyte apiece.
type refusedGates struct {
	Promises map[string]struct {
		Intent string `json:"intent"`
		Method string `json:"method"`
	} `json:"promises"`
	Gates []struct {
		Run       string   `json:"run"`
		Node      string   `json:"node"`
		Citations []string `json:"citations"`
		Gap       string   `json:"gap"`
		Refused   string   `json:"refused"`
	} `json:"gates"`
}

func loadRefusedGates(t *testing.T) refusedGates {
	t.Helper()
	raw, err := os.ReadFile("testdata/deepswe-refused-gates.json")
	if err != nil {
		t.Fatalf("the sweep's own record is the acceptance criterion: %v", err)
	}
	var gates refusedGates
	if err := json.Unmarshal(raw, &gates); err != nil {
		t.Fatal(err)
	}
	if len(gates.Gates) == 0 {
		t.Fatal("the fixture holds no gates")
	}
	return gates
}

func (r refusedGates) groundsFor(t *testing.T, run string) Grounds {
	t.Helper()
	promise, ok := r.Promises[run]
	if !ok {
		t.Fatalf("no request recorded for %s", run)
	}
	return Grounds{Intent: promise.Intent, Method: promise.Method}
}

// A QUOTATION THAT SKIPS A MIDDLE IS STILL A QUOTATION. Seven of the ten
// findings the sweep refused were the person's own sentences with an elision
// between two of them — the way anybody quotes a long request — and the rule
// read the result as one token of prose, found it a substring of nothing, and
// called the review an invention. Every refusal delivered the shortfall as done
// at a tenth of the run's wall.
func TestAQuotationOfTheRequestIsGroundedThroughItsElisions(t *testing.T) {
	recorded := loadRefusedGates(t)
	// EVERY ONE OF THEM. Eleven gates across eight runs, and not one of them was
	// citing something nobody promised: seven quote the request through an
	// elision, three quote the working method, and ofetch's continuation quotes
	// the method its own repair round was written against — which is still a
	// standard fixed before that node produced anything and one the worker was
	// held to, so it grounds like any other. The finding it names is true: the
	// branch was never pushed. What stops a replanned method from becoming a
	// standard that moves is not this rule; it is the growth journal, which
	// refuses a lineage handed the same remainder twice.
	admitted := 0
	for _, gate := range recorded.Gates {
		if refusal := admitGapCitations(gate.Citations, recorded.groundsFor(t, gate.Run)); refusal != "" {
			t.Errorf("%s %s: refused as %q, but the finding quotes what the run promised.\n  gap: %s\n  cited: %q",
				gate.Run, gate.Node, refusal, gate.Gap, gate.Citations)
			continue
		}
		admitted++
	}
	if admitted != len(recorded.Gates) {
		t.Fatalf("only %d of the sweep's %d refused findings were admitted", admitted, len(recorded.Gates))
	}
	// And every one of them recorded the same sentence, which is the thing this
	// change exists to stop being possible on grounded input.
	for _, gate := range recorded.Gates {
		if want := "what the review asked for next is not in the request"; gate.Refused != want {
			t.Fatalf("%s %s: the fixture no longer records the refusal under repair: %q",
				gate.Run, gate.Node, gate.Refused)
		}
	}
}

// AND THE METHOD IS A GROUND AT BOTH DOORS. ink s1 spent both of its gates
// citing the working method verbatim: the revision door admitted it, the
// extension door had never been told the method existed, and the run settled at
// four minutes of ninety. Two doors weighing one finding against two different
// ground sets is the same defect FAILSAFE names in the row about the mechanical
// gate and the citation invariant.
func TestTheWorkingMethodGroundsAFindingAtEveryDoor(t *testing.T) {
	recorded := loadRefusedGates(t)
	grounds := recorded.groundsFor(t, "ink-grid-box-layout-s1")
	const cited = "The whole finished implementation is written out in the worker's own final message, not merely saved to a file."
	if !strings.Contains(grounds.Method, cited) {
		t.Fatalf("the fixture no longer records the method the run was held to")
	}
	if refusal := AdmitGapRevision(grounds, []string{cited}); refusal != "" {
		t.Fatalf("the revision door refused the working method as %q", refusal)
	}
	if refusal := AdmitGapCitation(grounds, []string{cited}, nil); refusal != "" {
		t.Fatalf("the extension door refused the working method as %q", refusal)
	}
	// And the request alone is not the whole of the grounds any more: the same
	// citation weighed without the method is what the extension door used to
	// see, and it is refused, which is what the run recorded.
	if refusal := AdmitGapCitation(Grounds{Intent: grounds.Intent}, []string{cited}, nil); refusal == "" {
		t.Fatal("the method grounded a finding it does not contain")
	}
}

// THE PLAN'S PROMISES ARE GROUNDS TOO. The mechanical half of the gate emits its
// citations FROM plan.Done.Produces, and for a while no grounding rule could
// read that structure — so the gate named an output the plan itself had promised
// and the invariant called it an invention.
func TestWhatTheCompiledPlanPromisedGroundsAFinding(t *testing.T) {
	grounds := Grounds{
		Intent: "Sort out the release notes.",
		Done:   plan.Done{Produces: []string{"docs/RELEASE.md"}},
	}
	if refusal := admitGapCitations([]string{"docs/RELEASE.md"}, grounds); refusal != "" {
		t.Fatalf("an output the plan promised was refused as %q", refusal)
	}
	// The bare spelling of a promised path is the same promise, by the identity
	// law this package states once.
	if refusal := admitGapCitations([]string{"RELEASE.md"}, grounds); refusal != "" {
		t.Fatalf("the bare name of a promised file was refused as %q", refusal)
	}
	// A file nobody promised is still an invention wearing a filename.
	if refusal := admitGapCitations([]string{"docs/ROADMAP.md"}, grounds); refusal == "" {
		t.Fatal("a file neither the ask nor the plan named bought a round")
	}
}

// A FINDING THAT NAMES WHAT THE REQUEST NAMES IS GROUNDED BY ENTAILMENT. "The
// deliverable does not contain the code that writes feature_schema.joblib and
// validates dataset.features" quotes not one contiguous clause of the request
// and is about nothing else. It is admitted because every name in it is a name
// the request uses.
func TestAFindingIsGroundedWhenEveryNameInItIsTheRequestsOwn(t *testing.T) {
	recorded := loadRefusedGates(t)
	grounds := recorded.groundsFor(t, "igel-persist-feature-schema-s1")
	for _, cited := range []string{
		"no code writes feature_schema.joblib",
		"dataset.features is never validated",
		"duplicate_feature_aliases is missing from description.json",
	} {
		if refusal := admitGapCitations([]string{cited}, grounds); refusal != "" {
			t.Errorf("%q: refused as %q, though it names only what the request names", cited, refusal)
		}
	}
	// AND A PLAIN WORD IS NOT A NAME. The measured failure this invariant exists
	// for is a gate holding a worker to a working decision the run wrote for
	// itself out of the person's own vocabulary. Under the shape rule that
	// citation names no symbol at all, so the entailment door never opens for it.
	if refusal := admitGapCitations([]string{"any calendar year present in the data"},
		Grounds{Intent: "Total the March sales and chart them by region."}); refusal == "" {
		t.Fatal("a requirement the run invented out of the person's own words bought a round")
	}
	// A name the grounds do not hold poisons the finding, however many of its
	// neighbours are real.
	if refusal := admitGapCitations([]string{"feature_schema.joblib is never handed to sklearn.Pipeline"},
		grounds); refusal == "" {
		t.Fatal("a finding naming something nobody promised was admitted")
	}
}

// A RUN THAT CANNOT AFFORD THE REPAIR SAYS SO, AND IT SAYS IT AS UNCLOSED. A
// repair the wall will kill mid-flight spends money to deliver nothing, and the
// floor is derived from the attempt that produced the finding rather than typed.
// The direction matters: this is a floor under settlement, and a run whose
// deadline nobody set is never refused on a clock it cannot read.
func TestARepairIsNotBoughtWhenTheWallCannotHoldOne(t *testing.T) {
	node := store.Node{ID: "job", StartedAt: time.Now().Add(-10 * time.Minute)}
	roomy, cancel := context.WithDeadline(context.Background(), time.Now().Add(30*time.Minute))
	defer cancel()
	if refusal := outOfWall(roomy, node); refusal != "" {
		t.Fatalf("a run with twenty minutes to spare refused a ten-minute repair: %q", refusal)
	}
	spent, cancelSpent := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancelSpent()
	if refusal := outOfWall(spent, node); refusal == "" {
		t.Fatal("a run with one minute left bought a ten-minute repair")
	}
	if refusal := outOfWall(context.Background(), node); refusal != "" {
		t.Fatalf("a run with no deadline was refused on a clock it cannot read: %q", refusal)
	}
	if refusal := outOfWall(spent, store.Node{ID: "job"}); refusal != "" {
		t.Fatalf("an untimed attempt was refused on a duration nobody measured: %q", refusal)
	}
}
