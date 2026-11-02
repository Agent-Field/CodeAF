package run

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A STEP WITH ITS OWN MODEL OPENS ON IT, whatever the job's model is, and
// does not choose models automatically: the person picked this one for this
// step. A step without one opens on the job's, as before.
func TestAStepWithItsOwnModelOpensOnIt(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	automatic := true
	job := reviewJob()
	job.Item.Model, job.Item.AutomaticModels = "vendor/job", &automatic
	job.Stage.Model = " vendor/step "
	if _, err := NewChatExecutor(maker).Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	spec := maker.Opened()[0].Spec
	if spec.Model != "vendor/step" || spec.AutomaticModels == nil || *spec.AutomaticModels {
		t.Fatalf("opened on %q, automatic %v", spec.Model, spec.AutomaticModels)
	}
}
