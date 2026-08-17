package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// BUILDING A SUB-HARNESS, from the three sides a person meets it: the job that
// runs beside the conversation, the card it ends on, and the two answers to that
// card. WHO ASKS FOR ONE is tools_harness_test.go's half — a design is
// commissioned by the model's own hand now, and this file starts every design
// through exactly that hand.

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

const buildGoal = "triaging flaky tests"

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

// submitBuild starts a design THE WAY THE MODEL STARTS ONE: one call to the
// belt's build_harness tool, which returns the moment the job is in flight.
func submitBuild(t *testing.T, agent *Agent) {
	t.Helper()
	if text, isError := runTool(t, agent, "build_harness", `{"goal":"`+buildGoal+`"}`); isError {
		t.Fatalf("build_harness refused the call: %s", text)
	}
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
