package run

// A BAD REPORT IS CAUGHT WHEN IT IS MADE (chatstage.go's stageDoor.Report):
// a report that contradicts its step's own rule is answered at the call with
// the fix, is not held, and leaves the report before it standing, so the step
// puts it right in the same round. An honest shortfall is held like any other.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestFinalReviewMissingChecklistIsCorrectedAtTheReportingDoor(t *testing.T) {
	head := strings.Repeat("a", 40)
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		r := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Reviewed diff and description; nothing unfinished"}}
		if err := c.Spec.Stage.Report(ctx, r); err == nil || !strings.Contains(err.Error(), "repair.readiness") {
			t.Errorf("missing checklist did not reach model: %v", err)
		}
		r.Repair.Readiness = &factory.ReadinessReview{Head: "short", DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}
		if err := c.Spec.Stage.Report(ctx, r); err == nil || !strings.Contains(err.Error(), head) {
			t.Errorf("wrong head did not reach model: %v", err)
		}
		r.Repair.Readiness.Head = head
		if err := c.Spec.Stage.Report(ctx, r); err != nil {
			t.Errorf("corrected checklist refused: %v", err)
		}
	}}
	j := reviewJob()
	j.Stage = factory.Stage{Name: factory.FollowPRStep, Kind: factory.StageChat, Until: factory.UntilProven}
	j.Item.FollowUp = &factory.PRFollowUp{Kind: "readiness", State: "running", Readiness: &factory.PRReadiness{Head: head}}
	res, err := NewChatExecutor(maker).Run(context.Background(), j)
	if err != nil || !res.Done || res.Reports != 1 || len(maker.Opened()) != 1 {
		t.Fatalf("same-turn correction failed: done=%v reports=%d err=%v", res.Done, res.Reports, err)
	}
}

// The refusals as the model reads them after `not reported: `
// (internal/session's tools_stage.go).
const (
	refusedOpenFindings = "you reported 2 findings still open: fix them and report again, or report done: false"
	refusedNoEvidence   = "claim 2 has no evidence: add what shows it, or set ok: false"
)

// proofStage is the shipped proof step: until proven, three rounds.
func proofStage() factory.Stage {
	st, ok := factory.ShippedStage("proof")
	if !ok {
		panic("codeaf ships no proof step")
	}
	return st
}

// A PROOF CLAIM WITH NO EVIDENCE IS ANSWERED AT THE CALL AND CORRECTED IN THE
// SAME ROUND: the step hears which claim and what to do, adds what shows it,
// and the job goes on without a second round.
func TestAProofClaimWithNoEvidenceIsAnsweredAtTheCallAndFixedInTheSameRound(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "both shown", Claims: []factory.Claim{
			{Text: "refunds count once", OK: true, Evidence: "go test ./ledger -run TestRefund passes", Medium: "test"},
			{Text: "the CSV opens", OK: true, Medium: "screenshot"},
		}})
		if err == nil || err.Error() != refusedNoEvidence {
			t.Errorf("a claim with no evidence was answered %v, want %q", err, refusedNoEvidence)
		}
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "both shown", Claims: []factory.Claim{
			{Text: "refunds count once", OK: true, Evidence: "go test ./ledger -run TestRefund passes", Medium: "test"},
			{Text: "the CSV opens", OK: true, Evidence: "opened export.csv in Numbers, 12 rows", Medium: "screenshot"},
		}}); err != nil {
			t.Errorf("the corrected report was refused: %v", err)
		}
	}}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	// An approve step after proof holds the job at landed: a green proof with
	// none after it ships.
	id := g.add("ledger", proofStage(), approveStep())
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if n := len(maker.Opened()); n != 1 {
		t.Fatalf("%d rounds ran, want the one the step corrected itself in", n)
	}
	ph := it.Stream.Phases[0]
	if ph.Result == nil || !factory.Met(proofStage(), *ph.Result) || ph.Result.Reports != 1 {
		t.Fatalf("result = %+v, want the corrected report, held once", ph.Result)
	}
}

// A REVIEW THAT SAYS DONE WITH FINDINGS STILL OPEN IS ANSWERED AT THE CALL:
// it fixes them and reports clean in the same round.
func TestAReviewDoneWithOpenFindingsIsAnsweredAtTheCall(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Findings: 2, Output: "two problems in Export"})
		if err == nil || err.Error() != refusedOpenFindings {
			t.Errorf("done with open findings was answered %v, want %q", err, refusedOpenFindings)
		}
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "fixed both; nothing left"}); err != nil {
			t.Errorf("the clean report was refused: %v", err)
		}
	}}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	id := g.add("ledger", factory.Stage{Name: "review", Kind: factory.StageChat, Ask: "review the diff", Until: factory.UntilClean, Max: 3})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if n := len(maker.Opened()); n != 1 {
		t.Fatalf("%d rounds ran, want 1", n)
	}
	if ph := it.Stream.Phases[0]; ph.Result == nil || ph.Result.Findings != 0 || ph.Result.Output != "fixed both; nothing left" || ph.Result.Reports != 1 {
		t.Fatalf("result = %+v", ph.Result)
	}
}

// AN HONEST SHORTFALL IS HELD: not done with findings open, a claim marked not
// ok with what was seen, a proof not done with nothing yet claimed. The judge
// fails each into another round; the door refuses none.
func TestAnHonestShortfallIsHeld(t *testing.T) {
	cases := []struct {
		name  string
		stage factory.Stage
		r     factory.StageResult
	}{
		{"review not done, findings open", reviewJob().Stage, factory.StageResult{Done: false, Findings: 2, Output: "two left"}},
		{"proof done, a claim not ok with evidence", proofStage(), factory.StageResult{Done: true, Claims: []factory.Claim{
			{Text: "refunds count once", OK: true, Evidence: "TestRefund passes"},
			{Text: "the CSV opens", OK: false, Evidence: "Numbers says the file is damaged"},
		}}},
		{"proof not done, nothing claimed", proofStage(), factory.StageResult{Done: false, Output: "the export is still to run"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			report := c.r
			maker := &FakeMaker{Script: func(ctx context.Context, conv *FakeConversation) {
				if err := conv.Spec.Stage.Report(ctx, report); err != nil {
					t.Errorf("an honest shortfall was refused: %v", err)
				}
			}}
			job := reviewJob()
			job.Stage = c.stage
			res, err := NewChatExecutor(maker).Run(context.Background(), job)
			if err != nil {
				t.Fatal(err)
			}
			if res.Reports != 1 || res.Done != report.Done || res.Findings != report.Findings || len(res.Claims) != len(report.Claims) {
				t.Fatalf("result = %+v, want the report held as made", res)
			}
			if factory.Met(c.stage, res) {
				t.Fatal("a shortfall met its until")
			}
		})
	}
}

// A REFUSED REPORT LEAVES THE ONE BEFORE IT STANDING: it is not held, not
// counted, and nothing of it reaches the result.
func TestARefusedReportLeavesTheEarlierOneStanding(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: false, Findings: 2, Output: "two left in Export", Notes: []string{"Export is shared"}}); err != nil {
			t.Errorf("the first report was refused: %v", err)
		}
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Findings: 2, Output: "all done"}); err == nil || err.Error() != refusedOpenFindings {
			t.Errorf("done with open findings was answered %v, want %q", err, refusedOpenFindings)
		}
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Done || res.Findings != 2 || res.Output != "two left in Export" || len(res.Notes) != 1 || res.Reports != 1 {
		t.Fatalf("result = %+v, want the first report, held once", res)
	}
}
