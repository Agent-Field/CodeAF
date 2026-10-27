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

// EVERY STEP IS TOLD WHAT TO ASK ABOUT AND WHAT NEVER TO TOUCH: the ask-first
// and never lines the job started under, the repository's with the factory's,
// are in its brief beside the rules, so "don't touch auth/ without asking"
// reaches the step that would touch it, not only the approval after the plan.
func TestEveryStepsBriefSaysWhatToAskAboutAndWhatNeverToTouch(t *testing.T) {
	it := factory.Item{ID: 1, Num: 4, Repo: "acme/api", StartRules: &factory.StartRules{
		AskFirst: []string{"touch auth/", "Touches billing/**", "change the public API"},
		Never:    []string{"infra/", "secrets/**"},
	}}
	brief := stageBrief(Job{Item: it, Stage: factory.Stage{Name: "write", Ask: "make the change"}, Round: 1, Rules: []string{"keep the diff small"}})
	// EVERY PROTECTED PATH IS ONE LINE: ask-first paths and never paths hold a
	// job the same way, so the steps are told one thing about both.
	for _, want := range []string{
		"rules for this work",
		"- keep the diff small",
		"- ask before touching: auth/, billing/**, infra/, secrets/**",
		"- ask before you: change the public API",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not say %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "never touch") || strings.Count(brief, "ask before touching") != 1 {
		t.Errorf("the brief has more than one protected line:\n%s", brief)
	}
	// A never path alone is still told, on the same line.
	alone := stageBrief(Job{Item: factory.Item{ID: 1, Num: 4, StartRules: &factory.StartRules{Never: []string{"infra/"}}}, Stage: factory.Stage{Name: "plan", Ask: "x"}, Round: 1})
	if !strings.Contains(alone, "rules for this work") || !strings.Contains(alone, "- ask before touching: infra/") {
		t.Errorf("a job with only a never line was not told it:\n%s", alone)
	}
	// Empty lists say nothing.
	if bare := stageBrief(Job{Item: factory.Item{ID: 1, Num: 4, StartRules: &factory.StartRules{AskFirst: []string{}, Never: []string{}}}, Stage: factory.Stage{Name: "plan", Ask: "x"}, Round: 1}); strings.Contains(bare, "rules for this work") {
		t.Errorf("a job with no lines was told about rules:\n%s", bare)
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
