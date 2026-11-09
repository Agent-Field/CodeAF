package route_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// A RULE MAY RUN ITS JOBS ON AN AGENTFIELD REASONER: run_on is trimmed and
// kept, spelled node.reasoner, and never beside a pipeline.
func TestCleanKeepsARunOnAndRefusesABadOne(t *testing.T) {
	rules, err := route.Clean([]route.Rule{{Name: "support", When: route.Match{Sources: []string{"agentfield:*"}}, Then: route.Action{RunOn: " support-bot.draft_reply ", Start: route.StartAuto}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := rules[0].Then.RunOn; got != "support-bot.draft_reply" {
		t.Fatalf("run_on kept as %q", got)
	}
	for name, tc := range map[string]struct {
		then route.Action
		say  string
	}{
		"no agent":      {route.Action{RunOn: "support-bot", Start: route.StartAuto}, "node.agent"},
		"spaces":        {route.Action{RunOn: "support bot.draft", Start: route.StartAuto}, "node.agent"},
		"with pipeline": {route.Action{RunOn: "support-bot.draft", Pipeline: "quick", Start: route.StartAuto}, "names no pipeline"},
	} {
		_, err := route.Clean([]route.Rule{{Name: "a", Then: tc.then}})
		if err == nil || !strings.Contains(err.Error(), tc.say) || !strings.Contains(err.Error(), `"a"`) {
			t.Errorf("%s: err %v, want one naming the rule and saying %q", name, err, tc.say)
		}
	}
}

// A SOURCE WRITTEN agentfield:* MATCHES EVERY AGENTFIELD SOURCE, as
// agentfield does, and agentfield:linear only Linear's.
func TestAnAgentFieldWildcardSourceMatchesEveryAgentFieldJob(t *testing.T) {
	linear := factory.Item{Origin: "agentfield:linear", SourceRef: "agentfield:linear:ENG-4"}
	bearer := factory.Item{Origin: "agentfield:generic-bearer", SourceRef: "agentfield:generic-bearer:d1"}
	jira := factory.Item{Origin: "jira", SourceRef: "jira:FL-7"}
	for _, tc := range []struct {
		source string
		it     factory.Item
		want   bool
	}{
		{"agentfield:*", linear, true},
		{"agentfield:*", bearer, true},
		{"agentfield:*", jira, false},
		{"agentfield", bearer, true},
		{"agentfield:linear", linear, true},
		{"agentfield:linear", bearer, false},
		{"agentfield:generic-bearer", bearer, true},
		{"agentfield/*", linear, true},
		{"*", linear, false},
	} {
		rules, err := route.Clean([]route.Rule{{Name: "r", When: route.Match{Sources: []string{tc.source}}, Then: route.Action{Start: route.StartAuto}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := route.Matches(rules[0].When, tc.it); got != tc.want {
			t.Errorf("%s against %s: %v, want %v", tc.source, tc.it.Origin, got, tc.want)
		}
	}
}
