package run

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
	"time"
)

func TestLiveChatIdentityDiffAndReport(t *testing.T) {
	before := []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1}}
	after := append([]factory.Phase(nil), before...)
	after[0].Chat = "/worker/plan/transcript.jsonl"
	diff := stepsMoved(before, after)
	if len(diff) != 1 || diff[0].Chat != after[0].Chat {
		t.Fatalf("lost live chat: %+v", diff)
	}
	it := factory.Item{Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}}, Stream: &factory.Stream{Phases: before}}
	if err := reportStep(&it, diff[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[0].Chat != after[0].Chat {
		t.Fatal("live chat did not reach main store")
	}
}
func TestAutomaticInternalApprovalStillHoldsPublication(t *testing.T) {
	it := factory.Item{Stages: []factory.Stage{{Name: "approve", Kind: factory.StageGate, On: false}}, Proof: []factory.Claim{{Text: "test", OK: true, Evidence: "receipt"}}, Stream: &factory.Stream{}}
	if landItem(&it, time.Now()) || it.State != factory.StateLanded {
		t.Fatalf("skipped plan gate published: %+v", it)
	}
}

func TestPublishSummaryUsesPortablePaths(t *testing.T) {
	got := portablePublishText("Tests: cd /Users/private-user/work && go test; /home/private-user/project/file.go")
	if got != "Tests: cd $HOME/work && go test; $HOME/project/file.go" {
		t.Fatalf("private path in publication: %s", got)
	}
}
