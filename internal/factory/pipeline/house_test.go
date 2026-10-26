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
