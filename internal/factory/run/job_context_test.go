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

// E9/M1: disabled, excluded and skipped plans cannot consume another conversation's context.
func TestJobContextBriefInactivePlan(t *testing.T) {
	for _, mode := range []string{"disabled", "excluded", "skipped", "check"} {
		for _, compiled := range []bool{false, true} {
			if mode == "skipped" && !compiled {
				continue
			}
			t.Run(mode+map[bool]string{false: "/stages", true: "/compiled"}[compiled], func(t *testing.T) {
				plan := factory.Stage{Name: "plan", Kind: factory.StageChat, On: true}
				switch mode {
				case "disabled":
					plan.On = false
				case "excluded":
					plan.When = "large"
				case "check":
					plan.Kind = factory.StageCheck
				}
				stages := []factory.Stage{plan, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "review", Kind: factory.StageChat, On: true}}
				it := factory.Item{Stages: stages, Context: []factory.JobContext{{Title: "reference", Markdown: "attached body"}}}
				if compiled {
					phases := []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning}, {Name: "review", Kind: factory.StageChat, State: factory.PhasePending}}
					if mode == "skipped" {
						phases = append([]factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseSkipped}}, phases...)
					}
					if mode == "check" {
						phases = append([]factory.Phase{{Name: "plan", Kind: factory.StageCheck, State: factory.PhaseDone}}, phases...)
					}
					it.Stream = &factory.Stream{Phases: phases}
				}
				for i, s := range stages {
					brief := stageBrief(Job{Item: it, Stage: s, Index: i})
					if strings.Contains(brief, "attached body") != (s.Name == "write") {
						t.Fatalf("%s consumed context incorrectly", s.Name)
					}
				}
			})
		}
	}
}

// E9/M1: a compiled conversation plan wins even when it is not the first phase.
func TestJobContextBriefCompiledPlan(t *testing.T) {
	stages := []factory.Stage{{Name: "inspect", Kind: factory.StageChat, On: true}, {Name: "Plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}}
	it := factory.Item{Stages: stages, Context: []factory.JobContext{{Title: "reference", Markdown: "attached body"}}, Stream: &factory.Stream{Phases: []factory.Phase{
		{Name: "inspect", Kind: factory.StageChat, State: factory.PhaseDone},
		{Name: "Plan", State: factory.PhaseRunning},
		{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
	}}}
	for i, s := range stages {
		if brief := stageBrief(Job{Item: it, Stage: s, Index: i}); strings.Contains(brief, "attached body") != (s.Name == "Plan") {
			t.Fatalf("step %s context recipient is wrong", s.Name)
		}
	}
}
