package revision

// A coverage finding is raised only where a check could exist.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// errandPoints is what the checklist actually came back with for the errand:
// two ACTIONS of the run, quoted out of the person's own two clauses. Nothing a
// repository could run would fail if a command that has already been run were
// not run.
func errandPoints() []plan.Point {
	return []plan.Point{
		{
			Behaviour: "the command 'go test ./internal/subharness/ -count=1' is run in this workspace",
			Quote: "Run the command 'go test ./internal/subharness/ -count=1' in this " +
				"workspace and report the final line it prints.",
			Kind: plan.PointAction,
		},
		{
			Behaviour: "the final line it prints is reported",
			Quote: "Run the command 'go test ./internal/subharness/ -count=1' in this " +
				"workspace and report the final line it prints.",
			Kind: plan.PointAction,
		},
	}
}

// A CHECKLIST OF ACTIONS IS NEVER ASKED FOR A CHECK. The gate spent two
// coverage-mapping calls of ~127k prompt tokens each asking which repository
// test exercises "the command is run in this workspace", raised the finding,
// and bought the rounds that ran a satisfied request into its wall.
func TestAChecklistOfActionsRaisesNoCoverageFinding(t *testing.T) {
	ForgetChecklists()
	settings := config.Config{Model: "worker/model"}
	mapper := &scriptedJudge{replies: []*ai.Response{said(`{"mapped":[]}`)}}
	grounds := theErrand()
	evidence := Evidence{Accept: errandPoints(), Observed: true, Workspace: t.TempDir(),
		Verification: verify.Reading{Taken: true,
			Before: verify.Result{Reported: []string{"internal/subharness.TestOne"}}}}

	settled := settleAcceptance(context.Background(), settings,
		pool.Adopt(settings, mapper.Model(), mapper), nil, errandNode(),
		evidence, grounds, "worker/model", Judgment{Pass: true, Checked: true})

	if len(settled.Unexercised) > 0 || len(settled.Unasserted) > 0 {
		t.Fatalf("an action of the run was held to a check that could not exist: %v", settled.Unexercised)
	}
	if !settled.Pass {
		t.Fatalf("a satisfied errand was failed by the coverage question: %q", settled.Gaps)
	}
	if len(mapper.caps) != 0 {
		t.Fatalf("the mapping call was bought for a checklist nothing could map: %d calls", len(mapper.caps))
	}
	if !strings.Contains(settled.Unmeasured, "no behaviour a check could exercise") {
		t.Fatalf("the record does not say why nothing was mapped: %q", settled.Unmeasured)
	}
}

// AND NEITHER IS A RUN THAT CHANGED NO CODE. A check exercises something that
// exists; a run whose whole instruction was "Change no files" left nothing for
// one to be missing from, and the question has no answer whatever the checklist
// says.
func TestARunThatChangedNoCodeIsAskedForNoCheck(t *testing.T) {
	ForgetChecklists()
	settings := config.Config{Model: "worker/model"}
	mapper := &scriptedJudge{replies: []*ai.Response{said(`{"mapped":[]}`)}}
	grounds := ofetchGrounds(t)
	// Every point a behaviour, so the only thing standing between this run and
	// a finding is that it produced nothing to check.
	evidence := Evidence{Accept: ofetchPoints(), Observed: true, Workspace: t.TempDir(),
		Verification: verify.Reading{Taken: true,
			Before: verify.Result{Reported: []string{"tests/test_thing.py::test_A"}}}}

	settled := settleAcceptance(context.Background(), settings,
		pool.Adopt(settings, mapper.Model(), mapper), nil, store.Node{ID: "task-2"},
		evidence, grounds, "worker/model", Judgment{Pass: true, Checked: true})

	if len(settled.Unexercised) > 0 {
		t.Fatalf("a run that changed nothing was told to write checks: %v", settled.Unexercised)
	}
	if len(mapper.caps) != 0 {
		t.Fatalf("the mapping was bought over a tree nothing touched: %d calls", len(mapper.caps))
	}
	if !strings.Contains(settled.Unmeasured, "changed no code") {
		t.Fatalf("the record does not say why nothing was mapped: %q", settled.Unmeasured)
	}
}

// AN EMPTY OR UNREADABLE ROSTER IS NOT EVIDENCE THAT NO CHECK EXISTS. A real
// issue's fix was correct and its own tests green; the project's suite could not
// collect at base; the mapping call went out carrying a roster of nothing; and
// six stated behaviours were declared unexercised three times over.
func TestAnUnreadableRosterIsUnmeasuredAndNeverUnexercised(t *testing.T) {
	ForgetChecklists()
	settings := config.Config{Model: "worker/model"}
	mapper := &scriptedJudge{replies: []*ai.Response{said(`{"mapped":[]}`)}}
	grounds := ofetchGrounds(t)
	root := t.TempDir()
	source := filepath.Join(root, "breaker.py")
	if err := os.WriteFile(source, []byte("def probe():\n    return True\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{Accept: ofetchPoints(), Observed: true, Workspace: root,
		Artifacts: []string{source},
		// The shape the exhibit had: the command ran, and it collected nothing.
		Verification: verify.Reading{Taken: true,
			Before: verify.Result{Uncollected: true, Error: "6 errors during collection"}}}

	settled := settleAcceptance(context.Background(), settings,
		pool.Adopt(settings, mapper.Model(), mapper), nil, store.Node{ID: "task-2"},
		evidence, grounds, "worker/model", Judgment{Pass: true, Checked: true})

	if len(settled.Unexercised) > 0 {
		t.Fatalf("silence was read as proof that no check exists: %v", settled.Unexercised)
	}
	if len(mapper.caps) != 0 {
		t.Fatalf("the mapping was bought against an empty roster: %d calls", len(mapper.caps))
	}
	if !strings.Contains(settled.Unmeasured, "could not be read") {
		t.Fatalf("the record does not say the question had no measurement: %q", settled.Unmeasured)
	}
}

// AND A ROSTER THAT WAS READ AND IS GENUINELY EMPTY IS STILL A MEASUREMENT. A
// project with a runner and no tests answered the question: there is no check
// for this. That is a finding and it must survive the clause above it.
func TestARosterReadAndGenuinelyEmptyIsStillAFinding(t *testing.T) {
	ForgetChecklists()
	settings := config.Config{Model: "worker/model"}
	grounds := ofetchGrounds(t)
	root := t.TempDir()
	source := filepath.Join(root, "breaker.py")
	if err := os.WriteFile(source, []byte("def probe():\n    return True\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{Accept: ofetchPoints(), Observed: true, Workspace: root,
		Artifacts:    []string{source},
		Verification: verify.Reading{Taken: true}}

	settled := settleAcceptance(context.Background(), settings, nil, nil,
		store.Node{ID: "task-2"}, evidence, grounds, "worker/model",
		Judgment{Pass: true, Checked: true})

	if len(settled.Unexercised) == 0 {
		t.Fatal("a project that answered 'no checks' was treated as a project nobody could read")
	}
}

// THE WORK'S OWN CHECKS ARE THE ONE SOURCE THAT DOES NOT NEED THE PROJECT TO
// COLLECT — and they were the one source that never arrived. The diff route is
// dead in this tree (nothing sets exec.Outcome.Account, so Evidence.Patch is
// always empty), so a run that wrote 195 lines of pytest reached the mapping
// call with a roster of nothing while its own ten tests sat on disk.
func TestTheChecksTheRunWroteAreReadOffTheTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	check := filepath.Join(root, "tests", "test_reef_contracts.py")
	if err := os.WriteFile(check, []byte(
		"def test_bearer_scheme_case_insensitive(client):\n    assert True\n\n"+
			"def test_mixed_case_scheme_authenticates(client):\n    assert True\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "auth.py")
	if err := os.WriteFile(source, []byte("SCHEME = 'bearer'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checks := checkEvidence(Evidence{Workspace: root, Artifacts: []string{check, source}},
		verify.Reading{Taken: true, Before: verify.Result{Uncollected: true}})

	for _, want := range []string{"test_bearer_scheme_case_insensitive",
		"test_mixed_case_scheme_authenticates"} {
		found := false
		for _, check := range checks {
			if check == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("the run's own check %q is not in what the gate may match against: %v", want, checks)
		}
	}
}

// A SUITE THAT TIMED OUT SOMEWHERE ELSE SAYS NOTHING ABOUT A RUN WITH NO
// COVERAGE QUESTION TO ANSWER. Unreadable is settled before anything has
// classified what the request asked for, so a read-only errand — "run this
// command, report the final line, change no files" — failed Whole() over a
// whole-suite reading that was cut at its ceiling, and left as `partial`. There
// was never a check that could have exercised anything it asked for.
func TestAnErrandWithNoCoverageQuestionIsWholeOverASuiteNobodyCouldRead(t *testing.T) {
	ForgetChecklists()
	// A project that DECLARES a test command, whose reading was never taken:
	// the exact shape that sets Unreadable.
	cut := verify.Reading{
		Plan:   verify.Plan{Entrypoints: []verify.Entrypoint{{Kind: verify.KindTest, Command: "go test ./..."}}},
		Unread: "the whole-suite reading was killed at its ceiling of 4m0s without finishing",
	}
	for _, run := range []struct {
		name     string
		evidence Evidence
	}{
		// The request states only actions of the run.
		{"an all-action checklist", Evidence{Accept: errandPoints(), Observed: true,
			Workspace: t.TempDir(), Verification: cut}},
		// And the request states behaviours, over a run that changed nothing.
		{"a run that changed no code", Evidence{Accept: ofetchPoints(), Observed: true,
			Workspace: t.TempDir(), Verification: cut}},
	} {
		grounds := theErrand()
		if run.name == "a run that changed no code" {
			grounds = ofetchGrounds(t)
		}
		settled := settleAcceptance(context.Background(), config.Config{}, nil, nil,
			store.Node{ID: "task-2"}, run.evidence, grounds, "worker/model",
			Judgment{Pass: true, Checked: true})

		if settled.Unreadable {
			t.Fatalf("%s was charged for a suite that could not have acquitted it either", run.name)
		}
		if !(store.DeliveryGate{Pass: settled.Pass, Unreadable: settled.Unreadable,
			Unexercised: settled.Unexercised}).Whole() {
			t.Fatalf("%s delivered partial with no coverage question to answer", run.name)
		}
		if strings.TrimSpace(settled.Unmeasured) == "" {
			t.Fatalf("%s says nothing about why nothing was measured", run.name)
		}
	}

	// AND THE THIRD SILENCE STILL STANDS. A request that states behaviours, over
	// a change that was made, leaves a real question unanswered when the suite
	// cannot be read — which is where ink s7 exited 0 at 13 of 25.
	root := t.TempDir()
	source := filepath.Join(root, "breaker.py")
	if err := os.WriteFile(source, []byte("def probe():\n    return True\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := settleAcceptance(context.Background(), config.Config{}, nil, nil,
		store.Node{ID: "task-2"},
		Evidence{Accept: ofetchPoints(), Observed: true, Workspace: root,
			Artifacts: []string{source}, Verification: cut},
		ofetchGrounds(t), "worker/model", Judgment{Pass: true, Checked: true})
	if !changed.Unreadable {
		t.Fatal("a delivery whose suite nobody could read stopped being held short")
	}
}
