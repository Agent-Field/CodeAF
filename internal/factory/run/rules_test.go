package run

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// EVERY STEP IS TOLD THE RULES: the repository's, then the factory's, each
// once, as one paragraph of its brief; a job with none says nothing about them.
func TestEveryStepsBriefCarriesTheRepositorysAndTheFactorysRules(t *testing.T) {
	recipe := func(repo string) factory.Recipe {
		if repo != "acme/api" {
			t.Fatalf("recipe read for %q", repo)
		}
		return factory.Recipe{Policy: []string{"tests pass before anything posts", "no new dependencies without asking"}}
	}
	it := factory.Item{ID: 1, Num: 4, Repo: "acme/api", House: &factory.HouseRules{Rules: []string{"No new dependencies without asking", "don't touch auth without checking with me"}}}
	rules := JobRules(recipe, it)
	if want := []string{"tests pass before anything posts", "no new dependencies without asking", "don't touch auth without checking with me"}; !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules %q, want %q", rules, want)
	}
	brief := stageBrief(Job{Item: it, Stage: factory.Stage{Name: "plan", Ask: "read the issue and say how"}, Round: 1, Rules: rules})
	for _, want := range append([]string{"rules for this work"}, rules...) {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not say %q:\n%s", want, brief)
		}
	}
	if got := JobRules(nil, factory.Item{Repo: "acme/api"}); got != nil {
		t.Errorf("a job with no rules has %q", got)
	}
	if bare := stageBrief(Job{Item: factory.Item{ID: 1, Num: 4}, Stage: factory.Stage{Name: "plan", Ask: "x"}, Round: 1}); strings.Contains(bare, "rules for this work") {
		t.Errorf("a job with no rules was told about rules:\n%s", bare)
	}
}

// A HOUSE RULE CODE RECOGNISES IS APPLIED LIKE THE REPOSITORY'S OWN: a factory
// that says "never close an issue" stops a close a repository allowed.
func TestAHouseRuleCodeKnowsIsAppliedToPosts(t *testing.T) {
	it := factory.Item{House: &factory.HouseRules{Rules: []string{"never close an issue"}}}
	ok, why := PolicyAllows(withHouse(nil, it), it, factory.Action{Verb: "close"}, nil)
	if ok || why != "never close an issue" {
		t.Fatalf("a close passed the factory's rule: %v %q", ok, why)
	}
	if got := withHouse([]string{"a"}, factory.Item{}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("no house rules changed the policy: %q", got)
	}
}
