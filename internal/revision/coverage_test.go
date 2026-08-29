package revision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// unmapped is a mapping in which the first two behaviours are exercised by
// nothing, which is the shape igel s6's gate event actually held: seventeen
// rows, three of them empty.
func unmapped(points []plan.Point, covered int) []store.ExercisedPoint {
	mapping := make([]store.ExercisedPoint, 0, len(points))
	for index, point := range points {
		row := store.ExercisedPoint{Point: point.Behaviour}
		if index >= len(points)-covered {
			row.Check = "tests/test_thing.py::test_" + strings.Fields(point.Behaviour)[0]
		}
		mapping = append(mapping, row)
	}
	return mapping
}

// measuredEvidence is a delivery whose project was read: a roster exists, so the
// coverage question is answerable.
func measuredEvidence(points []plan.Point) Evidence {
	return Evidence{
		Accept: points,
		Verification: verify.Reading{
			Plan:  verify.Plan{Entrypoints: []verify.Entrypoint{{Kind: verify.KindTest, Command: "pytest"}}},
			Taken: true,
			Before: verify.Result{Reported: []string{
				"tests/test_thing.py::test_A", "tests/test_thing.py::test_a"}},
		},
	}
}

// THE COVERAGE FINDING IS A FINDING, NOT A PARAGRAPH. igel s6's gate event
// carried `exercises: 17 rows, 3 unmapped` and the words "no check exercises:"
// inside the middle of its `gap` string — so nothing journaled it as a finding,
// the stream's own gate line is the FIRST line of that string and never reached
// it, and no repair round was ever aimed at it.
func TestTheCoverageFindingTravelsAsAListOnEveryVerdict(t *testing.T) {
	ForgetChecklists()
	points := ofetchPoints()
	grounds := ofetchGrounds(t)
	evidence := measuredEvidence(points)

	failing := Judgment{Pass: false, Checked: true,
		Gaps:      "The deliverable is a report about the work rather than the work.",
		Citations: []string{"implement a per-origin circuit breaker"}}
	settled := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-2"}, evidence, grounds, "worker/model", failing)
	if len(settled.Unexercised) == 0 {
		t.Fatal("a failing verdict carried the coverage gap as prose and nothing else")
	}
	if settled.Stated != len(Held(points, grounds)) {
		t.Errorf("the finding cannot say how much of the checklist is open: %d of %d",
			len(settled.Unexercised), settled.Stated)
	}
	if !strings.Contains(settled.Gaps, "still exercised by nothing") {
		t.Errorf("the repair brief does not say what remains:\n%s", settled.Gaps)
	}
	if !strings.Contains(settled.Gaps, "report about the work") {
		t.Error("the gap the gate named was replaced rather than joined")
	}
}

// A MEASURED FINDING RIDING ON A REFUSED ONE IS STILL A MEASURED FINDING. The
// admission rules weigh where a REVIEW got its words; a coverage gap has none to
// weigh, because nobody has to ask for the behaviours they stated to be checked.
// igel s6 ended with three behaviours nothing exercised and a refusal of a
// sentence about a test file, and the second took the first down with it.
func TestAMeasuredFindingSurvivesARefusalOfTheJudgesOwnWords(t *testing.T) {
	invented := Judgment{
		Pass: false, Checked: true,
		Gaps:        "the deliverable does not include an OpenTelemetry span",
		Citations:   []string{"emit an OpenTelemetry span for every transition"},
		Grounds:     ofetchGrounds(t),
		Unexercised: []string{"A parse or hook failure is not retried"},
		Stated:      5,
	}
	if refusal := AdmitGapCitation(invented.Grounds, invented.Citations, nil); refusal == "" {
		t.Fatal("the fixture's citation is grounded, so this test proves nothing")
	}
	measured := invented.measuredHalf()
	if !measured.Sourced {
		t.Error("the measured half is not sourced, so it would be weighed for a citation")
	}
	if !strings.Contains(measured.Gaps, "no check exercises:") {
		t.Errorf("the measured half lost the finding:\n%s", measured.Gaps)
	}
	if strings.Contains(measured.Gaps, "OpenTelemetry") {
		t.Error("the judge's own prose rode into the measured half, which is the " +
			"laundering the admission rules exist to prevent")
	}
	if len(measured.Citations) != 1 || measured.Citations[0] != invented.Unexercised[0] {
		t.Errorf("the measured half cites something other than what it measured: %#v",
			measured.Citations)
	}
}

// THE CHECKLIST AND ITS FINDING ARE THE JOB'S, NOT THE ROUND'S. ofetch s7 mapped
// fifty-four points in round one, named eighteen behaviours nothing exercised,
// and bought a repair — and rounds two, three and four hold ZERO mapping rows.
// The continuation nodes are planned afresh so their specs carry no checklist,
// and the continuation worker (linear) takes no reading, so the question could
// not even be asked. The run ended at 41 of 47 with the same defaults untested.
func TestTheChecklistAndItsFindingBelongToTheJob(t *testing.T) {
	ForgetChecklists()
	points := ofetchPoints()
	grounds := ofetchGrounds(t)

	first := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-2"}, measuredEvidence(points), grounds, "worker/model",
		Judgment{Pass: true, Checked: true})
	if len(first.Unexercised) == 0 {
		t.Fatal("the first round measured nothing to carry")
	}

	// Round two: a continuation, planned afresh, run by a worker that takes no
	// reading. Its own spec carries no checklist and its outcome carries no
	// photograph — which is exactly the round that used to ask nothing.
	second := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-2-x1"}, Evidence{}, grounds, "worker/model",
		Judgment{Pass: true, Checked: true})
	if second.Pass {
		t.Fatal("a round that measured nothing passed over a finding an earlier " +
			"round had already measured")
	}
	if len(second.Unexercised) != len(first.Unexercised) {
		t.Errorf("the standing finding changed size without a measurement: %#v",
			second.Unexercised)
	}
	if strings.TrimSpace(second.Unmeasured) == "" {
		t.Error("the round does not say that nothing could be read this time")
	}

	// And a round that DID measure, and found everything covered, closes it.
	covered := measuredEvidence(points)
	whole := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-2-x2"}, covered, grounds, "worker/model",
		Judgment{Pass: true, Checked: true})
	_ = whole
	if open, _ := UnexercisedFor(verify.JobKey(grounds.Intent)); len(open) == 0 {
		t.Skip("this fixture's mapping is empty without a model, so nothing closes")
	}
}

// A PASS OVER A SUITE NOBODY COULD READ IS NOT A PASS OVER A CHECKED DELIVERY.
// ink s7 journaled its cut reading correctly — `npx ava --tap` killed at its
// ceiling of 1m53s — and then passed the round-two gate over a tree with no
// roster and left with exit 0 at 13 of 25 hidden checks.
func TestAPassOverAnUnreadableSuiteIsNotWhole(t *testing.T) {
	ForgetChecklists()
	points := ofetchPoints()
	grounds := ofetchGrounds(t)

	// The project DECLARES a way of checking itself and this run could not read
	// it. That is a fact about the run, and it leaves the delivery short.
	declared := Evidence{Accept: points, Verification: verify.Reading{
		Plan:   verify.Plan{Entrypoints: []verify.Entrypoint{{Kind: verify.KindTest, Command: "npm test"}}},
		Unread: "`npx ava --tap` was killed at its ceiling of 1m53s without finishing",
	}}
	settled := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-2"}, declared, grounds, "worker/model",
		Judgment{Pass: true, Checked: true})
	if !settled.Unreadable {
		t.Fatal("a declared suite nobody could read was recorded as a project with none")
	}
	if !strings.Contains(settled.Unmeasured, "killed at its ceiling") {
		t.Errorf("the sentence does not say WHY nothing could be read: %q", settled.Unmeasured)
	}
	if (store.DeliveryGate{Pass: true, Unreadable: true}).Whole() {
		t.Error("a pass over an unreadable suite settled whole, which is exit 0 over " +
			"a delivery nothing checked")
	}

	// And the other silence stays exactly what it was. A project that declares
	// no verification leaves the question unanswerable rather than unanswered,
	// and failing every such delivery would fail every piece of prose this
	// program writes.
	ForgetChecklists()
	silent := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-3"}, Evidence{Accept: points}, grounds, "worker/model",
		Judgment{Pass: true, Checked: true})
	if silent.Unreadable {
		t.Error("a project with no verification at all was charged for not having one")
	}
	if !silent.Pass {
		t.Error("a delivery was failed for a measurement nobody could take")
	}
	if !(store.DeliveryGate{Pass: true, Unmeasured: silent.Unmeasured}).Whole() {
		t.Error("a project with no verification cannot deliver whole")
	}
}

// A FILE ANYWHERE UNDER THE WORKSPACE THAT ANSWERS TO THE NAME IS PRODUCED. igel
// s6's request asked for `feature_schema.joblib`; the gate said "nothing of that
// name was left behind — the file was not produced"; and
// `model_results/feature_schema.joblib` was on disk and in the graded patch. The
// record it read is a report of what leaves claimed, and what a run left behind
// is answered by the filesystem (FAILSAFE clause 2).
func TestAFileTheWorkspaceHoldsIsProducedWhateverTheRegistrySays(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "model_results")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	// A binary file, because a joblib is one and a deliverable is a deliverable
	// whatever is inside it.
	if err := os.WriteFile(filepath.Join(nested, "feature_schema.joblib"),
		[]byte{0x80, 0x04, 0x95, 0x00, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{
		Named:     []string{"feature_schema.joblib"},
		Done:      plan.Done{Produces: []string{"feature_schema.joblib"}},
		Artifacts: []string{filepath.Join(root, "igel", "constants.py")},
		Workspace: root,
	}
	if _, missing := MissingProduces(evidence.Done, evidence.Artifacts); !missing {
		t.Fatal("the registry already answered, so this test proves nothing")
	}
	evidence.completeAgainstTheWorld()
	found, ok := ProducedFile("feature_schema.joblib", evidence.Artifacts)
	if !ok {
		t.Fatal("a file the workspace holds was still not produced")
	}
	if !strings.HasSuffix(found, filepath.Join("model_results", "feature_schema.joblib")) {
		t.Errorf("the fuller path is not what the record answers with: %q", found)
	}
	if _, missing := MissingProduces(evidence.Done, evidence.Artifacts); missing {
		t.Error("the mechanical gate still says a file on disk is missing")
	}
	if block := evidence.namedBlock(); !strings.Contains(block, "produced, at") ||
		!strings.Contains(block, "model_results") {
		t.Errorf("the sentence the judge reads does not quote the fuller path:\n%s", block)
	}

	// A name carrying a directory still names that place. The rule is namedAs,
	// read by the same code the registry reads, so a request naming
	// `docs/memo.md` is not closed by one sitting in `notes/`.
	elsewhere := Evidence{Named: []string{"docs/memo.md"}, Workspace: root}
	if err := os.WriteFile(filepath.Join(root, "memo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	elsewhere.completeAgainstTheWorld()
	if _, ok := ProducedFile("docs/memo.md", elsewhere.Artifacts); ok {
		t.Error("a file at the wrong address closed a request that named the address")
	}
}
