package run

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestCheckpointNeedsAnExplicitFinalReportInTheSameConversation(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Checkpoint: true, Done: true, Output: "progress"})
	}, FollowUp: func(ctx context.Context, c *FakeConversation, _ string) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "completed"})
	}}
	job := reviewJob()
	job.Stage = factory.Stage{Name: "validate", Until: factory.UntilGreen}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || result.Checkpoint || !result.Done || result.Output != "completed" || len(maker.Opened()) != 1 {
		t.Fatalf("checkpoint adjudicated as final: %+v %v", result, err)
	}
	if len(maker.Opened()[0].Noted()) != 1 {
		t.Fatal("checkpoint did not get a bounded final-report continuation")
	}
}
func TestVerificationCannotInferSuccessAfterOnlyProgress(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Checkpoint: true, Output: "still checking"})
	}, Infer: func(context.Context, *FakeConversation, string) (factory.StageResult, error) {
		t.Error("verification asked a model to invent completion")
		return factory.StageResult{Done: true}, nil
	}}
	job := reviewJob()
	job.Stage = factory.Stage{Name: "custom-check", Until: factory.UntilGreen}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || result.Checkpoint || result.Done || result.ExecutionBlocked != "report_incomplete" || factory.Met(job.Stage, result) {
		t.Fatalf("verification invented success: %+v %v", result, err)
	}
}

func TestCustomGreenStageCannotBypassMeasuredFailures(t *testing.T) {
	maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "looks green"})
	}}, notes: []string{"Unresolved verification command failed: go test ./... · failed"}}
	job := reviewJob()
	job.Stage = factory.Stage{Name: "validate", Until: factory.UntilGreen}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || result.Done || result.Exit == 0 || factory.Met(job.Stage, result) {
		t.Fatalf("custom stage bypassed check guard: %+v %v", result, err)
	}
}

func TestAContinuationCannotInvalidateItsNewerReport(t *testing.T) {
	door := newStageDoor()
	if err := door.Report(context.Background(), factory.StageResult{Done: true, Output: "old"}); err != nil {
		t.Fatal(err)
	}
	prior := door.result().Reports
	if err := door.Report(context.Background(), factory.StageResult{Done: true, Output: "new"}); err != nil {
		t.Fatal(err)
	}
	door.invalidateCompletion(prior)
	if got := door.result(); got.Checkpoint || got.Output != "new" {
		t.Fatalf("new report invalidated by old continuation: %+v", got)
	}
	door.invalidateCompletion(door.result().Reports)
	if !door.result().Checkpoint {
		t.Fatal("superseded completion remained accepted")
	}
}

func TestRefusedNewReportWithdrawsEarlierCompletion(t *testing.T) {
	for _, medium := range []string{"screenshot", "policy"} {
		t.Run(medium, func(t *testing.T) {
			door := newStageDoor()
			door.stage = proofStage()
			valid := factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "verified", OK: true, Medium: medium, Evidence: "concrete evidence"}}}
			if err := door.Report(context.Background(), valid); err != nil {
				t.Fatal(err)
			}
			invalid := valid
			invalid.Claims = []factory.Claim{{Text: "newer claim", OK: true, Medium: medium}}
			if err := door.Report(context.Background(), invalid); err == nil {
				t.Fatal("invalid report accepted")
			}
			if got := door.result(); !got.Checkpoint || factory.Met(door.stage, got) || len(got.Claims) != 1 || got.Claims[0].Text != "verified" {
				t.Fatalf("stale terminal candidate passed or evidence lost: %+v", got)
			}
		})
	}
	t.Run("readiness", func(t *testing.T) {
		door := newStageDoor()
		door.stage = factory.Stage{Name: "followpr", Until: factory.UntilDone}
		door.readinessHead = "abc123"
		valid := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "checked", Readiness: &factory.ReadinessReview{Head: "abc123", DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}
		if err := door.Report(context.Background(), valid); err != nil {
			t.Fatal(err)
		}
		if err := door.Report(context.Background(), factory.StageResult{Done: true}); err == nil {
			t.Fatal("invalid readiness report accepted")
		}
		if got := door.result(); !got.Checkpoint || factory.Met(door.stage, got) {
			t.Fatalf("stale readiness candidate passed: %+v", got)
		}
	})
}

func TestIdleCheckpointWithMeasuredFailureDoesNotBuyReportReminders(t *testing.T) {
	maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Checkpoint: true, Output: "checking"})
	}}, notes: []string{`Check receipt: {"exit":7,"outcome":"failed"}`, "Unresolved verification command failed: go test ./... · failed"}}
	job := reviewJob()
	job.Stage = factory.Stage{Name: "test", Until: factory.UntilGreen}
	got, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || got.Done || got.Checkpoint || got.Exit == 0 || got.ExecutionBlocked != "" || len(maker.Opened()[0].Noted()) != 0 {
		t.Fatalf("real terminal failure triggered paid reporting turns: %+v %v", got, err)
	}
}

func TestUnfinishedReceiptRemainsMechanicalWhenFinalAssessmentIsMissing(t *testing.T) {
	maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Checkpoint: true, Output: "checking"})
	}}, notes: []string{`Check receipt: {"exit":null,"outcome":"unknown"}`, "Unresolved verification command failed: go test ./... · unknown"}}
	job := reviewJob()
	job.Stage = factory.Stage{Name: "test", Until: factory.UntilGreen}
	got, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || got.Done || got.ExecutionBlocked != "report_incomplete" || len(maker.Opened()[0].Noted()) != stageReminders {
		t.Fatalf("unmeasured exit became a content failure or invented success: %+v %v", got, err)
	}
}
