package appwire

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// factory.routes_set is bounded before anything is sent, by the same check
// the factory keeps the table with ([route.Clean]).
func TestRoutesSetIsBoundedBeforeAnythingIsSent(t *testing.T) {
	table := []route.Rule{{Name: " jira ", When: route.Match{Sources: []string{"JIRA"}}, Then: route.Action{Start: "auto"}}}
	p, err := CheckRoutesSet(FactoryRoutesSetParams{Factory: "local", Rules: &table})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*p.Rules)[0]; got.Name != "jira" || got.When.Sources[0] != "jira" || got.Then.Start != route.StartAuto {
		t.Fatalf("cleaned %+v", got)
	}
	bad := []route.Rule{{Name: "x", Then: route.Action{Start: "later"}}}
	for name, tc := range map[string]struct {
		p   FactoryRoutesSetParams
		say string
	}{
		"no factory": {FactoryRoutesSetParams{Rules: &table}, ""},
		"no rules":   {FactoryRoutesSetParams{Factory: "local"}, "rules is required"},
		"bad rule":   {FactoryRoutesSetParams{Factory: "local", Rules: &bad}, "auto, ask or backlog"},
		"bad expect": {FactoryRoutesSetParams{Factory: "local", Rules: &table, Expect: &bad}, "expect:"},
	} {
		_, err := CheckRoutesSet(tc.p)
		if err == nil || !strings.Contains(err.Error(), tc.say) {
			t.Errorf("%s: err %v, want one saying %q", name, err, tc.say)
		}
	}
	for _, m := range []string{"factory.routes", "factory.routes_set"} {
		if _, ok := methods[m]; !ok {
			t.Fatalf("%s is not served", m)
		}
	}
}

// factory.routes_test is bounded the same way before anything is sent.
func TestRoutesTestIsBoundedBeforeAnythingIsSent(t *testing.T) {
	table := []route.Rule{{Name: "a", When: route.Match{Ask: " small? "}, Then: route.Action{Start: "ask"}}}
	p, err := CheckRoutesTest(FactoryRoutesTestParams{Factory: "local", Rules: &table, Limit: 10})
	if err != nil || (*p.Rules)[0].When.Ask != "small?" {
		t.Fatalf("checked %+v, %v", p, err)
	}
	long := []route.Rule{{Name: "a", When: route.Match{Ask: strings.Repeat("q", route.MaxAskLen+1)}, Then: route.Action{Start: "ask"}}}
	for name, tc := range map[string]FactoryRoutesTestParams{
		"no rules":   {Factory: "local"},
		"long ask":   {Factory: "local", Rules: &long},
		"over limit": {Factory: "local", Rules: &table, Limit: route.MaxPreview + 1},
	} {
		if _, err := CheckRoutesTest(tc); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, ok := methods["factory.routes_test"]; !ok {
		t.Fatal("factory.routes_test is not served")
	}
}
