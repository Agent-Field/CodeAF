package factory_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// waitingAt is a job held at its approve step, which names who.
func waitingAt(who, person string) factory.Item {
	stages := []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true, Who: who}, {Name: "write", Kind: factory.StageChat, On: true}}
	return factory.Item{ID: 1, Assignee: person, Autonomy: factory.AutonomyAuto, Cap: 5, State: factory.StateNeedsYou, QKind: factory.QKindApprove, Stages: stages,
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "write"}}}}
}

func TestApproverWordsAreAHandleTheOperatorRoleOrTheJobsPerson(t *testing.T) {
	for in, want := range map[string]string{"": "", "@Sam": "@sam", " operator ": "operator", "@a-b": "@a-b"} {
		if got, ok := factory.ApproverWord(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"sam", "@", "@-sam", "everyone", "@sam smith", "admin"} {
		if _, ok := factory.ApproverWord(bad); ok {
			t.Errorf("%q was taken", bad)
		}
	}
}

// AN APPROVAL THAT NAMES SOMEONE IS THEIRS, INSTEAD OF THE JOB'S PERSON: the
// named person (or any operator, for the role) may answer it, the job's person
// may not, and neither Auto nor the manager answers for them.
func TestAnApprovalWaitsOnWhomItNames(t *testing.T) {
	it := waitingAt("@rel", "sam")
	if !factory.MayApprove(it, "rel", false) || factory.MayApprove(it, "sam", false) || factory.MayApprove(it, "sam", true) {
		t.Fatal("only @rel may approve")
	}
	if !factory.NamesOtherApprover(it) || !strings.Contains(factory.AutonomyBlockReason(it), "@rel") {
		t.Fatalf("Auto would answer for @rel: %q", factory.AutonomyBlockReason(it))
	}
	op := waitingAt(factory.ApproverOperator, "sam")
	if !factory.MayApprove(op, "anyone", true) || factory.MayApprove(op, "sam", false) {
		t.Fatal("the operator role")
	}
	// Unnamed: the job's person, as always; named as the job's person: the same.
	plain := waitingAt("", "sam")
	if !factory.MayApprove(plain, "sam", false) || factory.MayApprove(plain, "rel", false) || factory.NamesOtherApprover(plain) {
		t.Fatal("the job's person")
	}
	if factory.NamesOtherApprover(waitingAt("@sam", "sam")) {
		t.Fatal("naming the job's person is not someone else")
	}
	// A landed job's proof waits on whom its last approve step names.
	landed := waitingAt("", "sam")
	landed.Stages = append(landed.Stages, factory.Stage{Name: "approve2", Kind: factory.StageGate, On: true, Who: "@rel"})
	landed.State, landed.QKind = factory.StateLanded, ""
	if factory.WaitingApprover(landed) != "@rel" || factory.MayApprove(landed, "sam", false) {
		t.Fatal("the landing approval")
	}
}
