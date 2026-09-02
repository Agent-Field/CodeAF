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
