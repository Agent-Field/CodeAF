package factory

import (
	"reflect"
	"testing"
)

// An agentfield step's input is the job's facts put into the step's mapping,
// and a value that is one fact alone keeps that fact's type: SWE-AF reads
// issue_number as an int, and a string "12" would not decode.
func TestAnAgentFieldStepMapsTheJobIntoItsReasonersInput(t *testing.T) {
	it := Item{ID: 7, Num: 12, Title: "Add Len", Body: "the ledger needs a Len()", Repo: "santoshkumarradha/factory-demo", Base: "main"}
	st := Stage{Name: "build", Kind: StageAgentField, Target: "swe-planner.build_issue", Input: map[string]string{
		"title": "{title}", "goal": "{title}\n\n{body}", "repo_url": "{repo_url}", "base_branch": "{base}",
		"issue_number": "{issue_number}", "issue_url": "{issue_url}", "plan_review": "yes",
	}}
	got := AgentFieldInput(it, st)
	want := map[string]any{
		"title": "Add Len", "goal": "Add Len\n\nthe ledger needs a Len()",
		"repo_url": "https://github.com/santoshkumarradha/factory-demo", "base_branch": "main",
		"issue_number": 12, "issue_url": "https://github.com/santoshkumarradha/factory-demo/issues/12", "plan_review": "yes",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("input = %#v\nwant    %#v", got, want)
	}
	// A typed-in job has no issue: its number is left out, not sent empty.
	typed := AgentFieldInput(Item{ID: 8, Title: "x"}, st)
	if _, ok := typed["issue_number"]; ok {
		t.Fatalf("a job with no issue sent issue_number: %#v", typed)
	}
	// No mapping sends every fact the job has under neutral names.
	plain := AgentFieldInput(it, Stage{Name: "build", Kind: StageAgentField, Target: "swe-planner.build_issue"})
	if plain["job"] != 7 || plain["issue_number"] != 12 || plain["base_branch"] != "main" || plain["title"] != "Add Len" {
		t.Fatalf("default input = %#v", plain)
	}
	if _, ok := plain["base"]; ok {
		t.Fatalf("default input spelled base, not base_branch: %#v", plain)
	}
}

func TestATargetIsNodeDotReasonerAndAPlaceholderIsAFact(t *testing.T) {
	if got, err := AgentFieldTargetWord(" swe-planner.build_issue "); err != nil || got != "swe-planner.build_issue" {
		t.Fatalf("target = %q %v", got, err)
	}
	for _, bad := range []string{"", "build_issue", "a.", ".b", "a b.c"} {
		if _, err := AgentFieldTargetWord(bad); err == nil {
			t.Errorf("%q was taken as a target", bad)
		}
	}
	if p := AgentFieldInputProblem("{title} on {branch}"); p == "" {
		t.Fatal("an unknown placeholder passed")
	}
	if p := AgentFieldInputProblem(`{"check_ci": false}`); p != "" {
		t.Fatalf("JSON text for an object field was read as a placeholder: %s", p)
	}
	if p := AgentFieldInputProblem("{title}\n\n{body} in {repo_url}"); p != "" {
		t.Fatalf("known placeholders refused: %s", p)
	}
}

// The pull request a run opened is read off its result wherever it is, and an
// issue link before it is not mistaken for it.
func TestTheRunsPullRequestIsFoundInItsResult(t *testing.T) {
	raw := []byte(`{"success":true,"summary":"see https://github.com/o/r/issues/12","pr_results":[{"pr_url":"https://github.com/o/r/pull/31","pr_number":31}]}`)
	if link, n := PullRequestIn(raw); link != "https://github.com/o/r/pull/31" || n != 31 {
		t.Fatalf("found %q %d", link, n)
	}
	if link, _ := PullRequestIn([]byte(`{"success":false,"summary":"Plan rejected: no"}`)); link != "" {
		t.Fatalf("a result with no pull request named %q", link)
	}
}

// A pipeline of agentfield steps and approvals runs wholly on AgentField; one
// chat step among them makes it an ordinary pipeline that lands its change.
func TestOnlyAnAllAgentFieldPipelineEndsWithItsRuns(t *testing.T) {
	af := Stage{Name: "build", Kind: StageAgentField, Target: "swe-planner.build_issue", On: true}
	gate := Stage{Name: "approve", Kind: StageGate, On: true}
	chat := Stage{Name: "write", Kind: StageChat, On: true}
	off := Stage{Name: "write", Kind: StageChat}
	for _, tc := range []struct {
		stages []Stage
		want   bool
	}{
		{[]Stage{af}, true},
		{[]Stage{gate, af}, true},
		{[]Stage{af, off}, true},
		{[]Stage{af, chat}, false},
		{[]Stage{gate}, false},
	} {
		if got := AgentFieldPipeline(Item{Stages: tc.stages}); got != tc.want {
			t.Errorf("%v: %v, want %v", tc.stages, got, tc.want)
		}
	}
	if AgentFieldPipeline(Item{RunOn: "support.handle", Stages: []Stage{af}}) {
		t.Error("a run_on job is not an agentfield pipeline")
	}
}
