package run

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStageBriefShippedMethodsAndEvidenceScope(t *testing.T) {
	for _, st := range factory.ShippedStages() {
		if st.Kind != factory.StageChat {
			continue
		}
		t.Run(st.Name, func(t *testing.T) {
			job := Job{Item: factory.Item{Title: "Change behaviour", Stages: []factory.Stage{st}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: st.Name}, {Name: "next"}}}}, Stage: st, Dir: "/repo", Round: 1}
			brief := stageBrief(job)
			method := factory.StageMethod(st)
			if method == "" || !strings.Contains(brief, method) {
				t.Fatal("brief lacks shipped method")
			}
			if strings.Contains(resumedStageBrief(job), method) {
				t.Error("resumed conversation repeats method")
			}
			place, rules := strings.Index(brief, stagePlace(job)), strings.Index(brief, method)
			if place < 0 || place >= rules {
				t.Error("method must follow placement")
			}
			job.Rules = []string{"Protect the public interface"}
			brief = stageBrief(job)
			if strings.Index(brief, method) >= strings.Index(brief, "rules for this work") {
				t.Error("method must precede rules")
			}
			if strings.Contains(brief, "publish_summary") != factory.IsProofStage(st) {
				t.Error("publication fields leaked to another step")
			}
			if st.Name == "plan" && (strings.Contains(brief, "Files:") || strings.Contains(brief, "cmd/app/main.go")) {
				t.Error("plan asks for a second prose file list")
			}
			job.Stage.Ask += " personally edited"
			if strings.Contains(stageBrief(job), method) {
				t.Error("edited ask inherited method")
			}
			job.Stage = factory.Stage{Name: "custom", Kind: factory.StageChat, Ask: st.Ask}
			if strings.Contains(stageBrief(job), method) {
				t.Error("custom stage inherited method")
			}
		})
	}
	it := factory.Item{}
	factory.EnsureFollowPR(&it)
	st := it.Stages[len(it.Stages)-1]
	if !strings.Contains(stageBrief(Job{Item: it, Stage: st, Round: 1}), "publish_summary") {
		t.Error("Follow PR lacks publication instruction")
	}
}

func TestCIFollowUpBriefOwnsConflictsButNotPublication(t *testing.T) {
	it := factory.Item{
		PullRequest: &factory.PullRequest{Number: 7},
		FollowUp:    &factory.PRFollowUp{State: "running", Attempt: 1, Limit: 3},
	}
	brief := ciFollowUpBrief(Job{Item: it})
	for _, want := range []string{
		"The service has merged the PR's remote head into this checkout",
		"conflicts left there are yours to resolve, keeping the intent of both sides and other people's work",
		"Use needs_you only for",
		"Do not push, force-push, create another PR, mark ready or merge",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("Follow PR brief lacks %q", want)
		}
	}
	if strings.Contains(brief, "If reconciliation is unresolved, ask") {
		t.Error("Follow PR delegates its conflict resolution to the person")
	}
}
