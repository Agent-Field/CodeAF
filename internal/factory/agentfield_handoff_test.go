package factory

import (
	"reflect"
	"testing"
)

func TestAgentFieldPullRequestBindingKeepsDefaultInputs(t *testing.T) {
	it := Item{ID: 7, Title: "Build then review", Repo: "acme/api"}
	before := AgentFieldInput(it, Stage{})
	it.PullRequest = &PullRequest{URL: "https://github.com/acme/api/pull/42", Number: 42}
	explicit := AgentFieldInput(it, Stage{Input: map[string]string{"pr_url": "{pr_url}", "brief": "Review {pr_url} for {title}"}})
	if explicit["pr_url"] != it.PullRequest.URL || explicit["brief"] != "Review "+it.PullRequest.URL+" for "+it.Title {
		t.Fatalf("PR binding was not interpolated: %#v", explicit)
	}
	if after := AgentFieldInput(it, Stage{}); !reflect.DeepEqual(before, after) {
		t.Fatalf("PR binding changed unmapped default inputs: before=%#v after=%#v", before, after)
	}
	if problem := AgentFieldInputProblem("{pr_url}"); problem != "" {
		t.Fatal(problem)
	}
	if problem := AgentFieldInputProblem("{unknown_output}"); problem == "" {
		t.Fatal("unknown placeholder was accepted")
	}
}
