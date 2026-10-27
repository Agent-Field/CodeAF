package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A RULE THAT HOLDS A MANUAL JOB IS ON THE WIRE, not only in Auto's message: a
// landed job held by a file its change touches carries the rule and file and
// the rules it started under, so a client can say so on the approval.
func TestARuleHoldIsOnTheWireForAManualJob(t *testing.T) {
	it := factory.Item{ID: 7, Repo: "acme/api", Kind: factory.KindIssue, Autonomy: factory.AutonomyManual, State: factory.StateLanded,
		StartRules: &factory.StartRules{AskFirst: []string{"touch auth/"}, Never: []string{"infra/"}},
		RuleHold:   "ask first · touch auth/ · auth/login.go"}
	j := FromItem(it)
	if j.AutonomyWhy != "" {
		t.Fatalf("a manual job said Auto paused: %q", j.AutonomyWhy)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rule_hold":"ask first · touch auth/ · auth/login.go"`, `"start_rules":{"ask_first":["touch auth/"],"never":["infra/"]}`, `"ready":true`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("the wire lacks %s: %s", want, raw)
		}
	}
}
