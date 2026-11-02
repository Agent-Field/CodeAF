package run

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A STEP WITH NO MODEL OF ITS OWN, ON A JOB WITH NONE, CARRIES THE FACTORY'S
// DEFAULT to the conversation it opens, beside its own (empty) pick, so the
// maker can still let a one-model launch win.
func TestAStepWithNoModelCarriesTheFactorysDefault(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	job := reviewJob()
	job.Item.FactoryModel = " vendor/factory "
	if _, err := NewChatExecutor(maker).Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	spec := maker.Opened()[0].Spec
	if spec.Model != "" || spec.FactoryModel != "vendor/factory" {
		t.Fatalf("opened on %q with factory default %q", spec.Model, spec.FactoryModel)
	}
}
