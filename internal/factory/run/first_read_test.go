package run

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A READ THE PERSON PAID FOR BEFORE WORK IS ONE THE WORK USES: each step's
// brief carries its verdict and claims, and a job without one carries
// nothing, nor does a version written after work.
func TestEveryStepIsGivenTheReadWrittenBeforeWork(t *testing.T) {
	read := factory.BriefVersion{Rev: 1, BeforeWork: true, Verdict: "Make a resend after a crash harmless", Blocks: []factory.BriefBlock{
		{Kind: factory.BlockBullets, Heading: "Done when", Bullets: []factory.BriefClaim{{Text: "a restarted worker sends each message once"}}},
		{Kind: factory.BlockOpenQuestion, Heading: "Unclear", Questions: []factory.BriefQuestion{{Text: "Which queue is it?"}}},
	}}
	job := Job{Item: factory.Item{ID: 7, Title: "Retries double-send", Briefs: []factory.BriefVersion{read, {Rev: 2, Verdict: "the plan is written"}}}, Stage: factory.Stage{Name: "plan", Kind: factory.StageChat}}
	got := stageBrief(job)
	for _, want := range []string{firstReadLead, "asked: Make a resend after a crash harmless", "done when:\n- a restarted worker sends each message once", "unclear:\n- Which queue is it?"} {
		if !strings.Contains(got, want) {
			t.Errorf("the step's brief is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "the plan is written") {
		t.Error("a version written after work was handed to a step")
	}
	job.Item.Briefs = []factory.BriefVersion{{Rev: 1, Verdict: "the plan is written"}}
	if strings.Contains(stageBrief(job), firstReadLead) {
		t.Error("a job with no read before work was told it had one")
	}
}
