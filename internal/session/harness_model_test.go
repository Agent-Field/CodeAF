package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// harnessModels is a catalog with ONE row per family, which is what makes
// "opus" a model here rather than a question. The shared testModels carries two
// opus rows on purpose (taskmodel_test.go) and a word that fits both of them is
// the ambiguous case, tested below on that list.
var harnessModels = []string{
	"anthropic/claude-opus-5",
	"anthropic/claude-sonnet-5",
	"google/gemini-3-pro",
	"openai/gpt-5",
}

// THE MODEL A TURN NAMES, from the three sides it is decided on: the words, the
// catalog, and what the run is actually handed.

// ── the words ───────────────────────────────────────────────────────────────

// The clause is read at the END and nowhere else, and everything that is not a
// model clause is left where it was.
func TestATrailingModelClauseIsCutAndNothingElseIs(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		rest, word string
		ok         bool
	}{
		{
			name: "the plain one",
			text: "research X with opus",
			rest: "research X", word: "opus", ok: true,
		},
		{
			name: "the other two prepositions",
			text: "compare the tiers using gemini",
			rest: "compare the tiers", word: "gemini", ok: true,
		},
		{
			name: "a whole id",
			text: "dig into the retries via anthropic/claude-opus-5",
			rest: "dig into the retries", word: "anthropic/claude-opus-5", ok: true,
		},
		{
			name: "a model spelled in words",
			text: "sweep the sources with gpt 5 mini",
			rest: "sweep the sources", word: "gpt 5 mini", ok: true,
		},
		{
			// The clause is the LAST thing said, so this is the one that counts.
			name: "the last clause wins",
			text: "compare it with the old report using opus",
			rest: "compare it with the old report", word: "opus", ok: true,
		},
		{
			name: "prose is not a model",
			text: "find out what broke with the new parser",
		},
		{
			name: "a sentence is not a model",
			text: "research the crash with everything you can find in the logs",
		},
		{
			name: "the preposition in the middle chooses nothing",
			text: "research this with opus and then write it up",
		},
		{
			name: "a clause with nothing in front of it is a fragment",
			text: "with opus",
		},
		{
			name: "a turn that named nothing",
			text: "research this and find out what our sources say",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rest, word, ok := splitHarnessModel(c.text)
			if ok != c.ok {
				t.Fatalf("split=%v, want %v (rest %q, word %q)", ok, c.ok, rest, word)
			}
			if !ok {
				return
			}
			if rest != c.rest || word != c.word {
				t.Fatalf("split into (%q, %q), want (%q, %q)", rest, word, c.rest, c.word)
			}
		})
	}
}

// ── the offer, end to end ───────────────────────────────────────────────────

// A NAMED MODEL RIDES THE RUN, and the words that named it are not part of the
// work: the harness is asked to do the research, not to read the sentence that
// chose its model.
func TestHarnessOfferCarriesTheModelTheTurnNamed(t *testing.T) {
	completer := &scriptedCompleter{}
	var gotText, gotModel string
	agent, ran := harnessAgent(t, completer, func(_, text, model string) (string, error) {
		gotText, gotModel = text, model
		return "the report", nil
	})
	agent.config.TaskModels = func() []string { return harnessModels }

	events, err := agent.Submit(context.Background(), harnessTurn+" with opus")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, true)

	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("no offer was raised; events: %v", kinds(collected))
	}
	// The word resolved to the id this install actually has, so the card draws
	// what will be sent rather than what was typed.
	if offer.Model != "anthropic/claude-opus-5" {
		t.Fatalf("the offer named model %q", offer.Model)
	}
	if offer.ModelNote != "" {
		t.Fatalf("a model that resolved left a note: %q", offer.ModelNote)
	}
	if run, _ := firstOfKind(collected, EventHarnessRun); run.Model != "anthropic/claude-opus-5" {
		t.Fatalf("the run announced model %q", run.Model)
	}
	if got := atomic.LoadInt32(ran); got != 1 {
		t.Fatalf("the harness ran %d times", got)
	}
	if gotModel != "anthropic/claude-opus-5" {
		t.Fatalf("the runner was handed model %q", gotModel)
	}
	if gotText != harnessTurn {
		t.Fatalf("the runner was handed %q, want the turn without its model clause", gotText)
	}
}

// A MODEL NOBODY HAS IS A NOTE, NEVER A REFUSAL. The offer stands, the harness
// runs, and the sentence is left exactly as it was typed — a word that matched
// nothing was, on the evidence, not a model at all.
func TestHarnessOfferNotesAModelItCouldNotFind(t *testing.T) {
	completer := &scriptedCompleter{}
	var gotText, gotModel string
	agent, ran := harnessAgent(t, completer, func(_, text, model string) (string, error) {
		gotText, gotModel = text, model
		return "the report", nil
	})

	turn := harnessTurn + " with gpt-9"
	events, err := agent.Submit(context.Background(), turn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, true)

	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("the offer was lost with the model; events: %v", kinds(collected))
	}
	if offer.Model != "" {
		t.Fatalf("a model nobody has resolved to %q", offer.Model)
	}
	if !strings.Contains(offer.ModelNote, "not found") || !strings.Contains(offer.ModelNote, "gpt-9") {
		t.Fatalf("the note reads %q", offer.ModelNote)
	}
	if got := atomic.LoadInt32(ran); got != 1 {
		t.Fatalf("the harness ran %d times", got)
	}
	if gotModel != "" {
		t.Fatalf("the runner was handed %q for a model nobody has", gotModel)
	}
	if gotText != turn {
		t.Fatalf("the runner was handed %q, want the turn as it was typed", gotText)
	}
}

// A word half the catalog answers to has named a FAMILY and not a model, and it
// is the same note for the same reason: the run takes the default and says so.
func TestHarnessOfferNotesAModelThatFitsSeveral(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "the report", nil
	})
	match, ok := agent.harnessMatch(userText(harnessTurn + " with gpt-5"))
	if !ok {
		t.Fatal("the offer was lost with the model")
	}
	if match.Model != "" {
		t.Fatalf("an ambiguous word resolved to %q", match.Model)
	}
	if !strings.Contains(match.ModelNote, "several") {
		t.Fatalf("the note reads %q", match.ModelNote)
	}
}

// NOBODY HOLDING A LIST IS NOT A REFUSAL. A session with no catalog cannot
// check an id, so the word travels as written — taskmodel.go's own rule, kept
// here so the two cannot disagree about what an unwired surface means.
func TestHarnessModelTravelsAsWrittenWithoutACatalog(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "", nil
	})
	agent.config.TaskModels = nil

	match, ok := agent.harnessMatch(userText(harnessTurn + " with vendor/unknown-5"))
	if !ok {
		t.Fatal("no offer was raised")
	}
	if match.Model != "vendor/unknown-5" {
		t.Fatalf("the word became %q", match.Model)
	}
	if match.Turn.Text != harnessTurn {
		t.Fatalf("the turn kept its clause: %q", match.Turn.Text)
	}
}

// DETECTION IS UNAFFECTED. The clause is off the text before anything is
// scored, so naming a model can neither raise an offer nor lose one.
func TestTheModelClauseIsNotScored(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "", nil
	})
	agent.config.TaskModels = func() []string { return harnessModels }
	plain, ok := agent.harnessMatch(userText(harnessTurn))
	if !ok {
		t.Fatal("the plain turn raised no offer")
	}
	named, ok := agent.harnessMatch(userText(harnessTurn + " with opus"))
	if !ok {
		t.Fatal("naming a model lost the offer")
	}
	if named.Score != plain.Score {
		t.Fatalf("the same turn scored %v with a model and %v without", named.Score, plain.Score)
	}
	if named.Turn.Text != plain.Turn.Text {
		t.Fatalf("the scored text was %q, want %q", named.Turn.Text, plain.Turn.Text)
	}
}

// ── the designer's own model ────────────────────────────────────────────────

// THE PAIR THAT WRITES A PAGE IS ONE PURCHASE, and with nothing named on the
// turn it is RoleDesigner's. A harness page is SAVED and picked off a menu by
// everybody afterwards, so it goes to the careful model rather than to whatever
// the conversation happens to be sitting on.
func TestTheDesignerResolvesItsRole(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierHigh): "test/careful-model",
			roles.TierKey(roles.TierLow):  "test/cheap-model",
		})
	})
	if got := agent.harnessDesignModel(""); got != "test/careful-model" {
		t.Fatalf("the designer thinks with %q, want the high tier's model", got)
	}
	// The turn's own word outranks the role, exactly as it does for a run.
	if got := agent.harnessDesignModel("anthropic/claude-opus-5"); got != "anthropic/claude-opus-5" {
		t.Fatalf("a named model became %q", got)
	}
	// And a pin outranks the tier.
	agent.config.RolesSource = tierSettings(map[string]string{
		roles.PinKey(roles.RoleDesigner): "test/pinned-model",
		roles.TierKey(roles.TierHigh):    "test/careful-model",
	})
	if got := agent.harnessDesignModel(""); got != "test/pinned-model" {
		t.Fatalf("a pinned designer thinks with %q", got)
	}
}

// AND THE EVENT THAT SAYS A DESIGN HAS STARTED CARRIES THAT SAME MODEL.
//
// It is the only thing on screen while the design runs — the turn is over the
// moment it begins — and the model on it is the RESOLVED one rather than the
// turn's own word, which is usually empty. An event carrying the word would name
// a model exactly in the case where the person had already typed it.
func TestTheDesignStartedEventNamesTheResolvedModel(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	agent.config.RolesSource = tierSettings(map[string]string{
		roles.TierKey(roles.TierHigh): "test/careful-model",
		roles.TierKey(roles.TierLow):  "test/cheap-model",
	})
	lane := agent.HarnessDesigns()

	submitBuild(t, agent)

	started := nextDesign(t, lane)
	if started.Kind != EventHarnessDesign {
		t.Fatalf("the lane opened with %v", started.Kind)
	}
	if started.Model != "test/careful-model" {
		t.Fatalf("the design started on %q, want the designer role's model", started.Model)
	}
}

// AN INSTALL THAT CONFIGURED NOTHING DESIGNS AS IT ALWAYS DID: the ladder's
// floor is the session's own model, which is where this call went before the
// role existed.
func TestTheDesignerFallsToTheSessionModel(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := agent.harnessDesignModel(""); got != "test/model" {
		t.Fatalf("the designer thinks with %q, want the session's own model", got)
	}
}

// ── the answer ──────────────────────────────────────────────────────────────

// The answer is about the model the CARD SHOWED. A surface that hands back
// nothing gets the offer's own model; one that hands back a word this session
// cannot place gets it too, because an unresolvable answer must not become a
// run on a model nobody has.
func TestTheAnsweredModelFallsBackToTheOffers(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "", nil
	})
	agent.config.TaskModels = func() []string { return harnessModels }
	match := harnessRoute{Model: "anthropic/claude-opus-5"}
	for _, c := range []struct {
		name, answered, want string
	}{
		{"a surface that says nothing", "", "anthropic/claude-opus-5"},
		{"the model it drew", "anthropic/claude-opus-5", "anthropic/claude-opus-5"},
		{"the word behind it", "opus", "anthropic/claude-opus-5"},
		{"a model nobody has", "vendor/nothing", "anthropic/claude-opus-5"},
		{"another model this install carries", "sonnet", "anthropic/claude-sonnet-5"},
	} {
		if got := agent.answeredHarnessModel(match, c.answered); got != c.want {
			t.Errorf("%s ran on %q, want %q", c.name, got, c.want)
		}
	}
}
