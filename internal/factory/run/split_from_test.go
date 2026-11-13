package run

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// S6: split lineage leaves each step's brief unchanged.
func TestSplitFromDoesNotChangeStepBriefs(t *testing.T) {
	for _, name := range []string{"plan", "write", "review", "proof"} {
		t.Run(name, func(t *testing.T) {
			job := handoverJob(factory.Stage{Name: name, Kind: factory.StageChat, On: true, Ask: "Do this step"})
			job.Item.Body = "Only the first part"
			job.Notes = []string{"Keep the public API"}
			job.Item.Context = []factory.JobContext{{Title: "Reference", Markdown: "The first part uses the existing parser"}}
			before := stageBrief(job)
			job.Item.SplitFrom = 80
			if got := stageBrief(job); got != before {
				t.Fatal("split lineage changed the step brief")
			}
		})
	}
}
