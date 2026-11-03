package run

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestSkippedCheckNeverVerifiesAJob(t *testing.T) {
	reason := "when touches auth does not match the change files"
	stale := &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 9}, Claims: []factory.Claim{{Text: "stale proof", OK: true, Evidence: "old result"}}, Notes: []string{`Command receipt: {"command":"old check","outcome":"passed"}`}}
	it := factory.Item{Title: "docs", State: factory.StateRunning, Stages: []factory.Stage{{Name: "proof", Kind: factory.StageCheck, On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageCheck, State: factory.PhaseSkipped, SkipReason: reason, Result: stale}}}}
	if ok, _ := PolicyAllows([]string{"no pull request without a green check"}, it, factory.Action{Verb: "pr"}, []factory.StageResult{{}}); ok {
		t.Error("skipped check satisfied required green check")
	}
	if ok, _ := PolicyAllows([]string{"tests pass before anything posts"}, it, factory.Action{Verb: "pr"}, nil); ok {
		t.Error("missing skipped result satisfied required checks")
	}
	if facts := factory.BriefFacts(it); len(facts) != 0 {
		t.Errorf("skipped check supplied proof facts: %+v", facts)
	}
	if brief := factory.BriefStub(it); brief.StepsDone != 0 {
		t.Errorf("skipped step counted done: %+v", brief)
	}
	body := factory.PublishBody(it, "")
	if strings.Contains(body, "Passed locally") || strings.Contains(body, "stale proof") || !strings.Contains(body, "proof: skipped") || !strings.Contains(body, reason) {
		t.Errorf("publication misreports skip: %s", body)
	}
	job := api.FromItem(it)
	if len(job.Steps) != 1 || job.Steps[0].State != "skipped" || job.Steps[0].SkipReason != reason || job.Steps[0].Tests != nil {
		t.Errorf("client checks: %+v", job.Steps)
	}
	before := factory.Item{State: factory.StateRunning, Stages: it.Stages, Proof: stale.Claims, Change: &factory.Change{Tests: []factory.StepTests{{Step: "proof", TestCounts: factory.TestCounts{Passed: 9}}}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageCheck, State: factory.PhaseDone, Result: stale}}}}
	report, _ := Diff(before, it)
	raw, err := json.Marshal(api.FromRunReport(report))
	if err != nil {
		t.Fatal(err)
	}
	var wire api.WorkerReport
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if err := ApplyReport(&before, api.ToWorkerReport(wire), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(before.Change.Tests) != 0 || len(before.Proof) != 0 {
		t.Errorf("skip retained measured proof: tests=%+v claims=%+v", before.Change.Tests, before.Proof)
	}
	if err := FinishReport(&before, factory.StateLanded, time.Now()); err != nil {
		t.Fatal(err)
	}
	if before.Stream.Phases[0].State != factory.PhaseSkipped || before.Stream.Phases[0].SkipReason != reason || before.State != factory.StateLanded {
		t.Fatalf("job cannot finish with visible skip: %+v", before)
	}
}

func TestSkippedStepRemovesEarlierEquivalentProofClaim(t *testing.T) {
	it := factory.Item{State: factory.StateRunning, Stages: []factory.Stage{{Name: "proof", Kind: factory.StageCheck, On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageCheck, State: factory.PhaseRunning}}}}
	for _, claim := range []factory.Claim{{Text: "Tests pass.", OK: true, Evidence: "measured receipt"}, {Text: "tests pass", OK: true}} {
		if err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "proof", State: factory.PhaseDone, Result: &factory.StageResult{Done: true, Claims: []factory.Claim{claim}}}}}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if len(it.Proof) != 1 || !it.Proof[0].OK {
		t.Fatal("fixture did not retain the measured claim")
	}
	if err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "proof", State: factory.PhaseSkipped, SkipReason: "no matching files"}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(it.Proof) != 0 {
		t.Fatalf("skipped step retained equivalent proof claim: %+v", it.Proof)
	}
}
