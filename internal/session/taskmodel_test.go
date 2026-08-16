package session

// The task's model, as tests: what one word resolves to, what a proposal is
// admitted with, and what the person is asked when nobody can tell.

import (
	"strings"
	"testing"
)

// testModels is one plausible install: two vendors, two families, and the two
// shapes that make a word ambiguous — a family with more than one member, and
// one tail two vendors both carry.
var testModels = []string{
	"anthropic/claude-opus-5",
	"anthropic/claude-opus-4.8",
	"anthropic/claude-sonnet-5",
	"openai/gpt-5",
	"openai/gpt-5-mini",
	"vendor/gpt-5",
}

// ── the matcher ─────────────────────────────────────────────────────────────

func TestTaskModelMatchesOneWordToOneModel(t *testing.T) {
	for _, tc := range []struct {
		word string
		want []string
	}{
		// The whole id, as written — and the "~" alias marker is not part of it.
		{"anthropic/claude-opus-5", []string{"anthropic/claude-opus-5"}},
		{"ANTHROPIC/CLAUDE-OPUS-5", []string{"anthropic/claude-opus-5"}},
		{"~openai/gpt-5-mini", []string{"openai/gpt-5-mini"}},
		// The tail, which is how a person says an id out loud. It beats the
		// substring rung, so "gpt-5" is the model called gpt-5 and not the four
		// rows that contain those characters.
		{"claude-opus-5", []string{"anthropic/claude-opus-5"}},
		{"gpt-5", []string{"openai/gpt-5", "vendor/gpt-5"}},
		// Every token, anywhere: the rung that raises shortlists.
		{"opus", []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}},
		{"claude opus 4.8", []string{"anthropic/claude-opus-4.8"}},
		{"sonnet", []string{"anthropic/claude-sonnet-5"}},
		{"fast", nil},
		{"", nil},
	} {
		got := matchTaskModel(tc.word, testModels)
		if len(got) != len(tc.want) {
			t.Fatalf("matchTaskModel(%q) = %v, want %v", tc.word, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("matchTaskModel(%q) = %v, want %v", tc.word, got, tc.want)
			}
		}
	}
}

// A shortlist is ordered so that the plain name leads its own variants: the
// leading option is what the card shows and what silence settles on, so its
// order is a decision and not a detail.
func TestTaskModelShortlistLeadsWithTheLeastQualifiedName(t *testing.T) {
	got := matchTaskModel("gpt", testModels)
	if len(got) == 0 || got[0] != "openai/gpt-5" {
		t.Fatalf("matchTaskModel(\"gpt\") = %v, want the shortest id first", got)
	}
}

// A word only one option can settle: the person's answer wins, an answer naming
// something outside the shortlist does not, and silence takes the closest match.
func TestSettleTaskModelTakesOnlyTheOptionsOffered(t *testing.T) {
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	for _, tc := range []struct{ answer, want string }{
		{"anthropic/claude-opus-4.8", "anthropic/claude-opus-4.8"},
		{"ANTHROPIC/CLAUDE-OPUS-4.8", "anthropic/claude-opus-4.8"},
		{"", "anthropic/claude-opus-5"},
		{"openai/gpt-5", "anthropic/claude-opus-5"},
	} {
		if got := settleTaskModel(options, tc.answer); got != tc.want {
			t.Fatalf("settleTaskModel(%q) = %q, want %q", tc.answer, got, tc.want)
		}
	}
	if got := settleTaskModel(nil, "anthropic/claude-opus-5"); got != "" {
		t.Fatalf("settleTaskModel with no options = %q, want nothing", got)
	}
}

// ── the resolution, on an agent ─────────────────────────────────────────────

func taskModelAgent(t *testing.T, mutate func(*Config)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &routedCompleter{}, func(config *Config) {
		config.TaskModels = func() []string { return testModels }
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

// NO MODEL NAMED IS THE ORDINARY CASE: the configured row, and the
// conversation's own model when there is no row.
func TestTaskModelDefaultsToTheConfiguredRowThenTheSession(t *testing.T) {
	agent := taskModelAgent(t, nil)
	if choice := agent.resolveTaskModel(""); choice.model != "test/model" {
		t.Fatalf("resolveTaskModel(\"\") = %+v, want the session's own model", choice)
	}

	configured := taskModelAgent(t, func(config *Config) { config.TaskModel = "opus-5" })
	// The row is resolved through the same matcher a proposal's word is, so a
	// row written loosely still names one id.
	if choice := configured.resolveTaskModel(""); choice.model != "anthropic/claude-opus-5" {
		t.Fatalf("resolveTaskModel(\"\") = %+v, want the configured row resolved", choice)
	}

	unknown := taskModelAgent(t, func(config *Config) { config.TaskModel = "private/model" })
	if choice := unknown.resolveTaskModel(""); choice.model != "private/model" {
		t.Fatalf("a settings row this catalog cannot see = %+v, want it kept as written", choice)
	}
}

// AN UNKNOWN SLUG IS A REFUSAL THE MODEL CAN ACT ON, and it names the nearest
// ids so a wrong guess costs one round trip.
func TestTaskModelRefusesAnUnknownSlugAndNamesNearMatches(t *testing.T) {
	agent := taskModelAgent(t, nil)

	choice := agent.resolveTaskModel("openai/gpt-9")
	if choice.problem == "" {
		t.Fatalf("an id this install does not have resolved to %+v", choice)
	}
	for _, want := range []string{"openai/gpt-9", "openai/gpt-5"} {
		if !strings.Contains(choice.problem, want) {
			t.Fatalf("refusal = %q, want it to name %q", choice.problem, want)
		}
	}

	// A class word matches nothing and is near nothing, so the refusal says what
	// leaving the argument out would run the work on instead.
	class := agent.resolveTaskModel("fast")
	if class.problem == "" || !strings.Contains(class.problem, "test/model") {
		t.Fatalf("refusal for a class word = %q, want the default named", class.problem)
	}

	// A word that fits half the catalog is the same refusal for the same reason:
	// a shortlist of eleven is a list, not a choice.
	vague := agent.resolveTaskModel("5")
	if vague.problem == "" || len(vague.options) != 0 {
		t.Fatalf("a word matching everything = %+v, want a refusal naming a few", vague)
	}
}

// WITH NO CATALOG NOTHING IS REFUSED. A surface that hands over no list has not
// said "there are no models"; it has said nobody can tell, and inventing a
// refusal from that is inventing a catalog.
func TestTaskModelWithoutACatalogTakesTheWordAsWritten(t *testing.T) {
	agent, _ := newTestAgent(t, &routedCompleter{}, nil)
	choice := agent.resolveTaskModel("some/model")
	if choice.model != "some/model" || choice.problem != "" {
		t.Fatalf("resolveTaskModel with no catalog = %+v, want the word as written", choice)
	}
}

// MORE THAN ONE PLAUSIBLE MODEL IS A QUESTION, NOT A GUESS — and not an error
// either: the shortlist rides on the proposal the person is already being shown.
func TestTaskModelAmbiguityBecomesAShortlistRatherThanARefusal(t *testing.T) {
	agent := taskModelAgent(t, nil)
	choice := agent.resolveTaskModel("opus")
	if choice.problem != "" || choice.model != "" {
		t.Fatalf("an ambiguous word = %+v, want a shortlist", choice)
	}
	if len(choice.options) != 2 || choice.options[0] != "anthropic/claude-opus-5" {
		t.Fatalf("shortlist = %v, want both opus rows, closest first", choice.options)
	}
	if shown := firstTaskModel(choice.options, choice.model); shown != "anthropic/claude-opus-5" {
		t.Fatalf("the card would show %q, want the leading option", shown)
	}
}
