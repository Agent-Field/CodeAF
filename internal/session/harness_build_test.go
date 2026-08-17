package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// BUILDING A SUB-HARNESS FROM A SENTENCE, from the four sides a person meets
// it: the cue that starts it, the turn it must not hold, the card it ends on,
// and the two answers to that card.

// ── the intent ──────────────────────────────────────────────────────────────

func TestABuildTurnIsReadAsOneAndOtherTurnsAreNot(t *testing.T) {
	for _, c := range []struct {
		turn string
		goal string // empty means this is not a build turn
	}{
		{"make a subharness for triaging flaky tests", "triaging flaky tests"},
		{"make a sub-harness for triaging flaky tests", "triaging flaky tests"},
		{"build a harness to summarise our release notes", "summarise our release notes"},
		{"create an harness that compares three options", "compares three options"},
		{"design a harness for researching pricing tiers", "researching pricing tiers"},
		{"Please make a harness for auditing our SQL", "auditing our SQL"},
		{"can you please build a sub harness to chase test flakes", "chase test flakes"},
		{"MAKE A HARNESS FOR shouting", "shouting"},

		// The negatives, and each one is a different way of not asking.
		{"research the pricing tiers", ""},                        // detection's turn, not this one
		{"run the research harness", ""},                          // reaching for one that exists
		{"make a note about our harnesses", ""},                   // the noun, doing something else
		{"make a harness", ""},                                    // no goal: nothing to design
		{"make a harness for", ""},                                // the joiner with nothing after it
		{"the reason we make a harness for this is speed", ""},    // talking ABOUT building one
		{"i wonder whether to build a harness for the tests", ""}, // the same, at the other end
		{"", ""},
	} {
		goal, ok := harnessBuildGoal(c.turn)
		if want := c.goal != ""; ok != want {
			t.Fatalf("%q was read as a build turn = %v, want %v (goal %q)", c.turn, ok, want, goal)
		}
		if ok && goal != c.goal {
			t.Fatalf("%q gave the goal %q, want %q", c.turn, goal, c.goal)
		}
	}
}

// THE MODEL CLAUSE IS THE OFFER'S CLAUSE. "make a harness for X with opus"
// chose a model and asked for a harness about X, and the designer is handed the
// second thing without the first.
func TestABuildTurnMayNameTheModelItIsDesignedOn(t *testing.T) {
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	goal, model, ok := agent.harnessBuild(userText(buildTurn + " with sonnet"))
	if !ok {
		t.Fatal("the turn was not read as a build turn")
	}
	if goal != buildGoal {
		t.Fatalf("the goal is %q, want %q", goal, buildGoal)
	}
	if want := "anthropic/claude-sonnet-5"; model != want {
		t.Fatalf("the design would ride %q, want %q", model, want)
	}
	// A WORD THIS INSTALL CANNOT PLACE IS NOT A REFUSAL: the design runs on the
	// session's own model, exactly as the offer treats it, and the goal keeps the
	// words that were, on the evidence, not about a model at all.
	goal, model, ok = agent.harnessBuild(userText(buildTurn + " with parchment"))
	if !ok || model != "" || goal != buildGoal+" with parchment" {
		t.Fatalf("an unplaceable model gave (%q, %q, %v)", goal, model, ok)
	}
}

// ── the gates ───────────────────────────────────────────────────────────────

// A BUILD IS AS SILENT AS AN OFFER when nobody can answer the card, when there
// is nowhere to save what comes back, or when nothing could run it.
func TestABuildTurnIsRefusedWhereTheOfferIsRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		undo func(*Config)
	}{
		{"no store", func(config *Config) { config.HarnessStore = nil }},
		{"no runner", func(config *Config) { config.RunHarness = nil }},
		{"nobody watching", func(config *Config) { config.AskConsent = false }},
	} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
			buildConfig(config, t.TempDir())
			c.undo(config)
		})
		if _, _, ok := agent.harnessBuild(userText(buildTurn)); ok {
			t.Fatalf("%s: the build path ran anyway", c.name)
		}
	}
}

// A NOTE THE SESSION WROTE IS NOT SOMEBODY ASKING FOR A HARNESS. A task's own
// completion report can say anything; a harness commissioned out of one would be
// the harness commissioning itself.
func TestABuildTurnIsOnlyEverWhatAPersonTyped(t *testing.T) {
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	for _, user := range []userMessage{
		wakeNote(buildTurn),
		{message: textMessage("user", buildTurn), authored: true},
		{},
	} {
		if _, _, ok := agent.harnessBuild(user); ok {
			t.Fatalf("a note the session wrote started a design: %+v", user)
		}
	}
}

// ── the turn ────────────────────────────────────────────────────────────────

// THE TURN DOES NOT WAIT FOR THE DESIGNER. This is the whole arrangement: the
// design is two model calls against a long guide and ends in a question nobody
// may be at the keyboard for, so the turn ends the moment it starts.
func TestABuildTurnEndsBeforeTheDesignerAnswers(t *testing.T) {
	answered := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// The design turn hangs until the test says otherwise. If the turn
			// loop were waiting on this, the drain below would time out.
			select {
			case <-answered:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse(designReply), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(reviewReply), nil
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	lane := agent.HarnessDesigns()

	events, err := agent.Submit(context.Background(), buildTurn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended while the designer was still thinking: %v", kinds(collected))
	}
	// AND NOTHING WAS SAID IN THE TURN. The design is the answer and it is not
	// written yet, so a blank assistant message here would be a line every later
	// request carries forever.
	if _, ok := firstOfKind(collected, EventTextDelta); ok {
		t.Fatalf("the build turn wrote an answer of its own: %v", kinds(collected))
	}
	if _, ok := firstOfKind(collected, EventHarnessOffer); ok {
		t.Fatalf("a build turn raised a RUN offer: %v", kinds(collected))
	}

	// The lane said the design had started before any of that.
	started := nextDesign(t, lane)
	if started.Kind != EventHarnessDesign || started.Text != buildGoal || started.Hint != harnessDesigningWord {
		t.Fatalf("the lane opened with %v / %q / %q", started.Kind, started.Text, started.Hint)
	}
	close(answered)
	if done := nextDesign(t, lane); done.Kind != EventHarnessDesignDone {
		t.Fatalf("the design never landed, got %v", done.Kind)
	}
}

// ── the card ────────────────────────────────────────────────────────────────

// THE DESIGN COMES BACK AS A PAGE, decoded, validated, and carried whole so
// that a surface draws the card everything else draws.
func TestADesignArrivesAsAValidHarness(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	if done.Harness == nil {
		t.Fatal("the design landed with no harness on it")
	}
	page := *done.Harness
	if page.Id.Name != "flake-triage" {
		t.Fatalf("the design is called %q", page.Id.Name)
	}
	if err := subharness.Validate(page); err != nil {
		t.Fatalf("a page that does not validate reached the card: %v", err)
	}
	if done.Text != page.Id.Name || done.Hint != page.Id.Desc {
		t.Fatalf("the card was named %q / %q", done.Text, done.Hint)
	}
	if done.ID == 0 {
		t.Fatal("the card carries no id, so nothing could answer it")
	}
	// NOTHING IS WRITTEN BEFORE THE ANSWER.
	if names, _ := agent.config.HarnessStore.Names(); len(names) != 0 {
		t.Fatalf("the registry already holds %v", names)
	}
	agent.ResolveHarness(done.ID, false, "")
}

// THE SECOND PASS IS A PATCH, and it lands on the page the card carries: the
// critic never re-emits the design, so what it changed and what it says it
// changed are the same object.
func TestTheReviewPassPatchesTheDraft(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"findings": [{"pass": "cost", "text": "three turns buys nothing here"}],
				"ops": [{"op": "set_field", "node": "look", "field": "max_turns", "text": "1"}],
				"calls": {"draft": 4, "revised": 2},
				"cues": ["flaky test", "triage the flake", "chase a flake", "flake"]}`), nil
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	node, found := done.Harness.Program.Node("look")
	if !found {
		t.Fatal("the patched page lost the node the op named")
	}
	if turns := node.Fields.Get("max_turns"); turns != "1" {
		t.Fatalf("the op did not land: max_turns is %q", turns)
	}
	// THE CRITIC SAW THE DRAFT AND THE LAW IT WAS JUDGING AGAINST. A review turn
	// shown neither would be reviewing its recollection of both.
	review := completer.request(1)
	if len(review) != 2 {
		t.Fatalf("the review turn was %d messages", len(review))
	}
	if !strings.Contains(messageText(review[0]), "PART FOUR") {
		t.Fatal("the critic was not handed the reviewer's addendum")
	}
	if !strings.Contains(messageText(review[1]), "flake-triage") {
		t.Fatal("the critic was not shown the draft it is patching")
	}
	// And the cues it rewrote are the ones the entry is registered with.
	agent.ResolveHarness(done.ID, true, "")
	designNote(t, lane)
	entry, _ := entryNamed(agent.harnessRegistry(), "flake-triage")
	if len(entry.Cues) != 4 {
		t.Fatalf("the entry kept the draft's cues: %v", entry.Cues)
	}
}

// A YES WRITES THE PAGE AND MAKES IT REACHABLE FROM THE NEXT SENTENCE.
func TestAnApprovedDesignIsSavedAndDetectableAtOnce(t *testing.T) {
	dir := t.TempDir()
	agent, _ := buildAgent(t, designingCompleter(), dir)
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, true, "")

	note := designNote(t, lane)
	if !strings.Contains(note, `"flake-triage" v1 saved`) {
		t.Fatalf("the save said %q", note)
	}
	saved, err := subharness.At(dir).Load("flake-triage", 0)
	if err != nil {
		t.Fatalf("the registry has no page: %v", err)
	}
	if saved.Id.Version != 1 {
		t.Fatalf("the page landed as v%d", saved.Id.Version)
	}
	// THE CUES CAME OFF THE ENVELOPE, because the page has nowhere to put them —
	// and without them the harness would answer to its own name and nothing else.
	registry := agent.harnessRegistry()
	entry, found := entryNamed(registry, "flake-triage")
	if !found {
		t.Fatalf("the harness is not in what detection reads: %v", registry)
	}
	if len(entry.Cues) < 2 || entry.Revision != 1 || entry.Description != saved.Id.Desc {
		t.Fatalf("the entry is %+v", entry)
	}
	// And the model is told, so the next thing said in this conversation happens
	// after a harness was saved rather than before it.
	if queued := steeringText(agent); !strings.Contains(queued, "saved") {
		t.Fatalf("the transcript was never told; the queue says %q", queued)
	}
}

// AND THE NEXT SENTENCE CAN REACH IT. This is the whole point of building one
// in conversation: the harness a person just approved is offered by the turn
// after it, not by the next process.
func TestAHarnessBuiltHereIsOfferedByTheVeryNextTurn(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, true, "")
	designNote(t, lane)

	events, err := agent.Submit(context.Background(), "chase a flake in the render tests")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, false)
	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("the harness this session built was not offered: %v", kinds(collected))
	}
	if offer.Text != "flake-triage" {
		t.Fatalf("the offer named %q", offer.Text)
	}
}

// A NO CHANGES NOTHING, which is what makes the card free to answer.
func TestADeclinedDesignIsNotSaved(t *testing.T) {
	dir := t.TempDir()
	agent, _ := buildAgent(t, designingCompleter(), dir)
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, false, "")

	if note := designNote(t, lane); !strings.Contains(note, "not saved") {
		t.Fatalf("the decline said %q", note)
	}
	if names, _ := subharness.At(dir).Names(); len(names) != 0 {
		t.Fatalf("a declined design was written: %v", names)
	}
	if entry, found := entryNamed(agent.harnessRegistry(), "flake-triage"); found {
		t.Fatalf("a declined design is detectable: %+v", entry)
	}
}

// A DESIGN THAT NEVER PARSED SAYS SO. The person asked for a harness and
// silence is the one answer that leaves them wondering.
func TestADesignThatWillNotParseEndsInANote(t *testing.T) {
	// Every reply is prose, so salvage fails, the repair turn fails, and the two
	// retries after it fail the same way.
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	if started := nextDesign(t, lane); started.Kind != EventHarnessDesign {
		t.Fatalf("the lane opened with %v", started.Kind)
	}
	note := designNote(t, lane)
	if !strings.HasPrefix(note, "harness design failed:") || !strings.Contains(note, "attempts") {
		t.Fatalf("the failure said %q", note)
	}
}

// ── the fixtures ────────────────────────────────────────────────────────────

const (
	buildGoal = "triaging flaky tests"
	buildTurn = "make a harness for " + buildGoal
)

// designReply is one designer turn: the envelope the guide asks for, around a
// page this package will accept.
const designReply = `{
  "cues": ["flaky test", "triage the flake", "chase a flake"],
  "justification": "Two jobs: read the failure, then check the report names it.",
  "derivation": [
    {"a": "look", "b": "check", "rel": "depends", "why": "the check reads the report the loop wrote"}
  ],
  "harness": {
    "id": {"name": "flake-triage", "desc": "chase a flaky test to a fix"},
    "program": {
      "nodes": [
        {"id": "look", "kind": "agent.loop", "fields": {"brief": "read the failing test and say what it does", "tools": "read", "max_turns": "3"}},
        {"id": "check", "kind": "verify", "fields": {"ladder": "accept", "check": "the report names the failing test"}}
      ],
      "edges": [["look", "check"]]
    },
    "whitelist": ["read"],
    "verify": {"ladder": "accept"},
    "dyn": {"ladder": "fixed"}
  }
}`

// reviewReply is a critic that found nothing worth patching, which is a real
// review outcome and the cheapest one.
const reviewReply = `{"findings": [], "ops": [], "calls": {"draft": 2, "revised": 2}}`

// designingCompleter answers the two turns of a whole design.
func designingCompleter() *scriptedCompleter {
	return &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
}

// buildConfig is the three seams a build needs, on a registry of its own.
func buildConfig(config *Config, dir string) {
	config.AskConsent = true
	config.HarnessStore = subharness.At(dir)
	config.TaskModels = func() []string { return testModels }
	config.RunHarness = func(context.Context, string, string, string) (string, error) { return "", nil }
}

func buildAgent(t *testing.T, completer Completer, dir string) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, completer, func(config *Config) { buildConfig(config, dir) })
}

// submitBuild sends the build turn and drains it, which is instant: the turn
// ends as the design starts.
func submitBuild(t *testing.T, agent *Agent) {
	t.Helper()
	events, err := agent.Submit(context.Background(), buildTurn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)
}

// nextDesign takes the next event off the standing lane, failing rather than
// hanging.
func nextDesign(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	select {
	case event, open := <-lane:
		if !open {
			t.Fatal("the design lane closed")
		}
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("nothing arrived on the design lane")
		return Event{}
	}
}

// designDone reads past the "designing" line to the card.
func designDone(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	for {
		event := nextDesign(t, lane)
		switch event.Kind {
		case EventHarnessDesignDone:
			return event
		case EventNotice:
			t.Fatalf("the design ended in a note instead of a card: %q", event.Text)
		}
	}
}

// designNote reads past everything to the next thing said in words.
func designNote(t *testing.T, lane <-chan Event) string {
	t.Helper()
	for {
		if event := nextDesign(t, lane); event.Kind == EventNotice {
			return event.Text
		}
	}
}

func entryNamed(entries []subharness.Entry, name string) (subharness.Entry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return subharness.Entry{}, false
}

// steeringText is what the session has queued for the model to read at the top
// of the next turn.
func steeringText(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	var said []string
	for _, note := range agent.steering {
		said = append(said, note.text())
	}
	return strings.Join(said, "\n")
}
