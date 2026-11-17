package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A STEP'S QUESTION IS KIND STEP ON THE WIRE, options and all. The answer
// door (appwire's factory.job_answer) and every desk read a step's question
// by that kind, and draw its options as the answers; the second live A+B run
// (2026-11-07, job 78) sent kind "options", so the options printed as text
// and no button was ever drawn. A two-option yes/no step question, and one
// with no options at all, are a step's question too.
func TestAStepsQuestionIsKindStepWithItsOptions(t *testing.T) {
	when := time.Date(2026, 11, 7, 19, 51, 0, 0, time.UTC)
	stream := &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "build", State: factory.PhaseWaiting, At: when}}}
	for _, tc := range []struct {
		name    string
		options []string
	}{
		{"options", []string{"Yes - count zero entries", "No - only negative amounts"}},
		{"yes_no", []string{"yes", "no"}},
		{"words", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := factory.Item{ID: 78, State: factory.StateNeedsYou, QKind: factory.QKindStep, Question: "build asks: count zero?", Stream: stream,
				Asking: &factory.Asked{Stage: "build", Question: "count zero?", Options: tc.options, With: factory.AskedYou, At: when}}
			j := api.FromItem(it)
			if j.NeedsYou == nil || j.NeedsYou.Question == nil {
				t.Fatalf("needs_you = %+v", j.NeedsYou)
			}
			q := j.NeedsYou.Question
			if q.Kind != factory.QKindStep {
				t.Fatalf("question kind = %q, want %q", q.Kind, factory.QKindStep)
			}
			if strings.Join(q.Options, "|") != strings.Join(tc.options, "|") || q.FromStep != "build" {
				t.Fatalf("question = %+v", q)
			}
		})
	}
}

// AN ENDED JOB IS NOBODY'S TO GIVE BACK. Close and Reject keep the job's
// Manual mark, which keeps every automation and old worker report off it, and
// the wire carries that mark whole. What the desk draws its takeover bar and
// Give back from is controls.give_back, which an ended job never offers (live
// A+B run 2 drew the bar on a rejected job, D1); a job a person has taken over
// and that is still under way does.
func TestAClosedOrRejectedJobDoesNotOfferGiveBack(t *testing.T) {
	now := time.Unix(1000, 0)
	waiting := factory.Item{ID: 3, State: factory.StateNeedsYou, QKind: factory.QKindApprove, Question: factory.ApproveQuestion("plan"),
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}}}}
	rejected := waiting
	rejected.EndRejected("sam", "not this", now)
	if !rejected.Manual {
		t.Fatal("a rejected job lost the mark that keeps automation off it")
	}
	if j := api.FromItem(rejected); !j.Manual || j.Controls.GiveBack {
		t.Fatalf("a rejected job: manual=%v give_back=%v; want the mark kept and no Give back", j.Manual, j.Controls.GiveBack)
	}
	closed := waiting
	closed.ID, closed.Manual, closed.DoneHow, closed.State = 4, true, "closed_locally", factory.StateNew
	if api.FromItem(closed).Controls.GiveBack {
		t.Fatal("a closed job offers Give back")
	}
	held := factory.Item{ID: 5, State: factory.StateNeedsYou, Manual: true, QKind: "manual", Question: "Automation paused.",
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "write", State: factory.PhaseDone}}}}
	if !api.FromItem(held).Controls.GiveBack {
		t.Fatal("a job a person took over does not offer Give back")
	}
}

// A REJECTION'S WORDS END THEIR OWN SENTENCE ONCE. "Rejected · <words>." is
// the state's detail; words that already end in a full stop (or ! or ?) read
// "Rejected · Too broad. Reopen…", never "Too broad.. Reopen…".
func TestARejectionsWordsAreNotGivenASecondFullStop(t *testing.T) {
	now := time.Unix(1000, 0)
	for words, want := range map[string]string{
		"Too broad.":  "Rejected · Too broad. Reopen or Run as new to try again.",
		"Too broad":   "Rejected · Too broad. Reopen or Run as new to try again.",
		"Why this?":   "Rejected · Why this? Reopen or Run as new to try again.",
		"":            "Rejected. Reopen or Run as new to try again.",
		"Not now…":    "Rejected · Not now… Reopen or Run as new to try again.",
		"No thanks! ": "Rejected · No thanks! Reopen or Run as new to try again.",
	} {
		it := factory.Item{ID: 5, State: factory.StateNeedsYou, QKind: factory.QKindApprove, Question: factory.ApproveQuestion("plan")}
		it.EndRejected("sam", words, now)
		if got := api.FromItem(it).StatePresentation.Detail; got != want {
			t.Errorf("words %q: detail = %q, want %q", words, got, want)
		}
	}
}
