package orch

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/praf/appx"
	"github.com/Agent-Field/codeaf/internal/praf/config"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// fakeApp is an App for tests that drive the orchestrator's control flow,
// where no model call is expected.
type fakeApp struct{ notes []string }

func (f *fakeApp) Note(_ context.Context, message string, _ ...string) {
	f.notes = append(f.notes, message)
}
func (f *fakeApp) Harness(context.Context, string, map[string]any, any, appx.HarnessOptions) (*appx.HarnessResult, error) {
	panic("Harness not expected in a control-flow test")
}
func (f *fakeApp) AI(context.Context, string, ...ai.Option) (*ai.Response, error) {
	panic("AI not expected in a control-flow test")
}

// runOnce is an orchestrator whose phases are stubbed: it records the
// guidance its reviewers got and whether its output was asked to post.
func runOnce(t *testing.T, in schemas.ReviewInput, hints []string) (feedback string, post bool) {
	t.Helper()
	cfg := config.DefaultReviewConfig()
	cfg.Hints = hints
	o := New(Deps{App: &fakeApp{}}, in, cfg)
	o.runIntakeFn = func(context.Context) (schemas.IntakeResult, error) { return schemas.IntakeResult{}, nil }
	o.runAnatomyFn = func(context.Context, schemas.IntakeResult) (schemas.AnatomyResult, error) {
		return schemas.AnatomyResult{}, nil
	}
	o.resolveDepthFn = func(schemas.IntakeResult) string { return "standard" }
	o.runReviewPhasesFn = func(_ context.Context, _ schemas.IntakeResult, _ schemas.AnatomyResult, _, guidance string) (schemas.ReviewPlan, []schemas.ScoredFinding, error) {
		feedback = guidance
		return schemas.ReviewPlan{}, nil, nil
	}
	o.generateOutputFn = func(_ context.Context, _ []schemas.ScoredFinding, _ schemas.IntakeResult, _ schemas.AnatomyResult, _ schemas.ReviewPlan, p bool) (schemas.ReviewResult, error) {
		post = p
		return schemas.ReviewResult{}, nil
	}
	o.cleanupFn = func() {}
	if _, err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return feedback, post
}

// TestADryRunNeverPosts: inside codeaf every review is a dry run, and the
// pipeline posts only when it is not one and names a pull request.
func TestADryRunNeverPosts(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	if _, post := runOnce(t, schemas.ReviewInput{PrURL: &url, DryRun: true}, nil); post {
		t.Error("a dry run asked its output to post")
	}
	if _, post := runOnce(t, schemas.ReviewInput{PrURL: &url}, nil); !post {
		t.Error("a review that is not a dry run did not post")
	}
	if _, post := runOnce(t, schemas.ReviewInput{}, nil); post {
		t.Error("a review with no pull request asked to post")
	}
}

// TestHintsReachReviewers: the brief's focus reaches every reviewer as
// guidance.
func TestHintsReachReviewers(t *testing.T) {
	feedback, _ := runOnce(t, schemas.ReviewInput{DryRun: true}, []string{"focus on the auth middleware", " ", "and retries"})
	if want := "focus on the auth middleware | and retries"; feedback != want {
		t.Errorf("guidance = %q, want %q", feedback, want)
	}
}

func strPtr(s string) *string { return &s }
