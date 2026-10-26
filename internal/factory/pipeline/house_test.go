package pipeline

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A HOUSE RULE ONLY ADDS: the repository's rules and start limits stay, the
// factory's join them once, the repository's text is untouched, and the read
// it was given is not changed in place.
func TestHouseRulesAddToARepositoryAndDropNothing(t *testing.T) {
	repo := Defaults()
	repo.Rules = []string{"tests pass before anything posts"}
	repo.Start.AskFirst = []string{"touch billing/"}
	repo.Start.Never = []string{"secrets/"}
	repo.Text = "rules: [tests pass before anything posts]\n"
	house := factory.HouseRules{
		Rules:    []string{"Tests pass before anything posts", "don't touch auth without checking with me"},
		AskFirst: []string{"touch auth/"},
		Never:    []string{"infra/"},
	}
	got := WithHouseRules(repo, house)
	if want := []string{"tests pass before anything posts", "don't touch auth without checking with me"}; !reflect.DeepEqual(got.Rules, want) {
		t.Errorf("rules %q, want %q", got.Rules, want)
	}
	if want := []string{"touch billing/", "touch auth/"}; !reflect.DeepEqual(got.Start.AskFirst, want) {
		t.Errorf("ask_first %q, want %q", got.Start.AskFirst, want)
	}
	if want := []string{"secrets/", "infra/"}; !reflect.DeepEqual(got.Start.Never, want) {
		t.Errorf("never %q, want %q", got.Start.Never, want)
	}
	if got.Text != repo.Text || !reflect.DeepEqual(got.House, house) {
		t.Errorf("text %q, house %#v", got.Text, got.House)
	}
	if len(repo.Rules) != 1 || len(repo.Start.AskFirst) != 1 || !repo.House.Empty() {
		t.Fatalf("the repository read was changed in place: %#v", repo)
	}
	if empty := WithHouseRules(repo, factory.HouseRules{}); !reflect.DeepEqual(empty.Rules, repo.Rules) || !empty.House.Empty() {
		t.Fatalf("no house rules changed the read: %#v", empty)
	}
}

// A PLAN'S FILES ARE CHECKED AGAINST THE RULES A JOB STARTED UNDER: a path an
// ask-first rule or a never line names holds the approval, a sentence rule
// says nothing here, and no rules or no files hold nothing.
func TestAPlanThatTouchesAGuardedPathIsHeld(t *testing.T) {
	rules := &factory.StartRules{AskFirst: []string{"don't touch auth without checking with me", "touch auth/"}, Never: []string{"infra/"}}
	if got := PlanHold(rules, []string{"cmd/main.go", "internal/auth/login.go"}); got != "ask first · touch auth/ · internal/auth/login.go" {
		t.Errorf("auth plan held as %q", got)
	}
	if got := PlanHold(rules, []string{"infra/deploy.yaml"}); got != "never starts on its own · touches infra/deploy.yaml (infra/**)" {
		t.Errorf("infra plan held as %q", got)
	}
	if got := PlanHold(rules, []string{"cmd/main.go"}); got != "" {
		t.Errorf("an unguarded plan was held: %q", got)
	}
	if PlanHold(nil, []string{"internal/auth/login.go"}) != "" || PlanHold(rules, nil) != "" {
		t.Error("a job with no rules or a plan with no files was held")
	}
	f := WithHouseRules(Defaults(), factory.HouseRules{AskFirst: []string{"touch auth/"}})
	if got := StartRulesOf(f); !reflect.DeepEqual(got.AskFirst, []string{"touch auth/"}) || got.Never == nil {
		t.Errorf("start rules %#v", got)
	}
}
