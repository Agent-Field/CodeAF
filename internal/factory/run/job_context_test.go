package run

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// E9: plan gets every attachment whole, after notes, and later steps get none.
func TestJobContextBriefOnlyOnPlan(t *testing.T) {
	stages := []factory.Stage{
		{Name: "inspect", Kind: factory.StageChat, On: true},
		{Name: "Plan", Kind: factory.StageChat, On: true},
		{Name: "write", Kind: factory.StageChat, On: true},
	}
	attachments := []factory.JobContext{
		{Title: "reference alpha", Source: "guide alpha", Markdown: "alpha beginning\n" + strings.Repeat("é", 59900) + "\nalpha end"},
		{Title: "reference beta", Source: "guide beta", Markdown: "beta beginning\n\nbeta end"},
	}
	for i, stage := range stages {
		job := Job{Item: factory.Item{Stages: stages, Context: attachments}, Stage: stage, Index: i, Notes: []string{"keep the public API"}, Round: 1}
		brief := stageBrief(job)
		for _, attachment := range attachments {
			for _, want := range []string{attachment.Title, attachment.Source, attachment.Markdown} {
				if strings.Contains(brief, want) != (i == 1) {
					t.Fatalf("step %q attachment presence is wrong", stage.Name)
				}
			}
		}
		if i == 1 {
			if strings.Index(brief, "notes:") > strings.Index(brief, attachments[0].Title) || !strings.Contains(brief, "reference material") || !strings.Contains(brief, "does not override") {
				t.Fatal("context lacks placement or reference instructions")
			}
		}
	}
}

// E9: without a plan, only the first enabled conversation step receives context.
func TestJobContextBriefFirstConversationWithoutPlan(t *testing.T) {
	stages := []factory.Stage{
		{Name: "check", Kind: factory.StageCheck, On: true},
		{Name: "unused", Kind: factory.StageChat},
		{Name: "approve", Kind: factory.StageGate, On: true},
		{Name: "draft", Kind: factory.StageChat, On: true},
		{Name: "review", Kind: factory.StageChat, On: true},
	}
	for i, stage := range stages {
		job := Job{Item: factory.Item{Stages: stages, Context: []factory.JobContext{{Title: "attached reference", Markdown: "attached body"}}}, Stage: stage, Index: i, Round: 1}
		if got := stageBrief(job); strings.Contains(got, "attached body") != (i == 3) {
			t.Fatalf("step %q context presence is wrong", stage.Name)
		}
	}
}

// E9: runtime skipped phases do not take the first conversation's context.
func TestJobContextBriefSkipsRuntimeSkippedConversations(t *testing.T) {
	stages := []factory.Stage{
		{Name: "check", Kind: factory.StageCheck, On: true},
		{Name: "optional", Kind: factory.StageChat, On: true},
		{Name: "draft", Kind: factory.StageChat, On: true},
		{Name: "review", Kind: factory.StageChat, On: true},
	}
	item := factory.Item{Stages: stages, Context: []factory.JobContext{{Title: "reference", Markdown: "attached body"}}, Stream: &factory.Stream{Phases: []factory.Phase{
		{Name: "check", State: factory.PhaseDone},
		{Name: "optional", Kind: factory.StageChat, State: factory.PhaseSkipped},
		{Name: "draft", Kind: factory.StageChat, State: factory.PhaseRunning},
		{Name: "review", Kind: factory.StageChat, State: factory.PhasePending},
	}}}
	for i := 2; i < len(stages); i++ {
		if got := stageBrief(Job{Item: item, Stage: stages[i], Index: i}); strings.Contains(got, "attached body") != (i == 2) {
			t.Fatalf("step %q context presence is wrong", stages[i].Name)
		}
	}
}
