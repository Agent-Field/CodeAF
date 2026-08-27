package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE MODEL'S HANDS ON THE BIG MACHINERY, from the three sides that matter: the
// hands exist only where they can work, the goal is required and is passed
// through whole, and each one STARTS something and comes straight back.
//
// AND ONE HAND THAT IS NOT HERE ANY MORE. `run_adaptive` was the third of them —
// the model's own way to open a planned graph of nodes — and the tests that
// pinned its behaviour are gone with it rather than inverted, because there is
// nothing left to assert about a verb: what replaces them is the one test below
// that says the belt does not carry it even where a runner is wired.

// ── which hands a build has ─────────────────────────────────────────────────

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. A model told it can build
// a harness plans around that for the rest of the conversation, so a build with
// no registry, no runner, or nobody watching does not carry the verb at all.
func TestTheHarnessHandsAreOnTheBeltOnlyWhereTheyWork(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*Config)
		want  []string
	}{
		{
			name:  "the whole thing wired",
			setup: func(config *Config) { buildConfig(config, t.TempDir()); config.OrchestrateRunner = neverRuns },
			want:  []string{"build_harness", "list_harnesses"},
		},
		{
			name:  "a registry with no engine under it",
			setup: func(config *Config) { buildConfig(config, t.TempDir()); config.RunHarness = nil },
		},
		{
			name:  "nowhere to save a page",
			setup: func(config *Config) { buildConfig(config, t.TempDir()); config.HarnessStore = nil },
		},
		{
			name: "nobody watching",
			setup: func(config *Config) {
				buildConfig(config, t.TempDir())
				config.OrchestrateRunner = neverRuns
				config.AskConsent = false
			},
		},
		{
			// A RUNNER AND NOTHING ELSE USED TO BUY A VERB, and it buys none now:
			// the adaptive runner is still wired here, and the belt is empty.
			name:  "a runner and nothing else",
			setup: func(config *Config) { config.AskConsent = true; config.OrchestrateRunner = neverRuns },
		},
		{
			name:  "a plain session",
			setup: func(*Config) {},
		},
	} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, c.setup)
		var carried []string
		for _, name := range []string{"build_harness", "list_harnesses"} {
			if _, found := onBelt(agent, name); found {
				carried = append(carried, name)
			}
		}
		if strings.Join(carried, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: the belt carries %v, want %v", c.name, carried, c.want)
		}
	}
}

// EVERY HAND CARRIES ITS TEACHING. A tool the model reaches for on its own
// judgement is a tool whose description IS the judgement: what it is for, and
// what it is not for.
func TestTheHarnessHandsSayWhatTheyAreFor(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		buildConfig(config, t.TempDir())
		config.OrchestrateRunner = neverRuns
	})
	for _, c := range []struct{ name, says string }{
		{"build_harness", "propose_task"},
		{"build_harness", "list_harnesses"},
		{"list_harnesses", "build_harness"},
	} {
		tool, found := onBelt(agent, c.name)
		if !found {
			t.Fatalf("the belt has no %s", c.name)
		}
		if len(tool.Schema) == 0 {
			t.Fatalf("%s has no schema, so the model would be told it takes nothing", c.name)
		}
		if !strings.Contains(tool.Description, c.says) {
			t.Errorf("%s never mentions %s, so nothing tells the model when NOT to use it", c.name, c.says)
		}
	}
	// AND NEITHER OF THEM SENDS THE MODEL SOMEWHERE THAT NO LONGER EXISTS. A
	// description is prompt text: naming a verb the belt does not carry is the
	// same fault as carrying a verb that refuses, and it costs the model a call
	// to find out.
	for _, name := range []string{"build_harness", "list_harnesses"} {
		tool, _ := onBelt(agent, name)
		if strings.Contains(tool.Description, "run_adaptive") {
			t.Errorf("%s still points the model at run_adaptive, which is not on the belt", name)
		}
	}
}

// WHAT THE MODEL IS TOLD IT HAS, AND WHAT THE PERSON CAN LOOK UP. Both of these
// hands are conditional, so the two completeness gates this repo runs — the belt
// against the manual (manual_test.go), the prompt against the belt — cannot see
// them on a bare test agent. They are checked here instead, because a tool the
// prompt never mentions is one the model reaches for by luck, and a tool no page
// describes is one the chat cannot answer a question about.
func TestTheHarnessHandsAreInThePromptAndTheManual(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		buildConfig(config, t.TempDir())
		config.OrchestrateRunner = neverRuns
	})
	rendered := renderSystem(agent.config)
	for _, name := range []string{"build_harness", "list_harnesses"} {
		if !strings.Contains(rendered, name) {
			t.Errorf("prompts/system.md never mentions %s", name)
		}
		if !manual.Chat().Mentions(name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", name)
		}
	}
	// And the two things a person asks about by name rather than by tool name.
	for _, question := range []string{"what is an adaptive run", "how do I make a harness"} {
		if len(manual.Chat().Search(question, 0)) == 0 {
			t.Errorf("%q reaches nothing in the chat manual", question)
		}
	}
}

// ── build_harness ───────────────────────────────────────────────────────────

// THE HAND STARTS THE DESIGN AND COMES STRAIGHT BACK. A design is two model
// calls against a long guide and ends in a card nobody may be at the keyboard
// for; a tool that waited would hold the conversation for the whole of it.
func TestBuildHarnessStartsTheDesignAndAnswersAtOnce(t *testing.T) {
	answered := make(chan struct{})
	completer := designingCompleter()
	// The design turn hangs until this test says otherwise. If the tool were
	// waiting on it, the call below would not return.
	first := completer.steps[0]
	completer.steps[0] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		select {
		case <-answered:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return first(ctx, messages)
	}
	agent, _ := buildAgent(t, completer, t.TempDir())
	lane := agent.HarnessDesigns()

	text, isError := runTool(t, agent, "build_harness", `{"goal":"`+buildGoal+`"}`)
	if isError {
		t.Fatalf("the call was refused: %s", text)
	}
	if !strings.Contains(text, buildGoal) {
		t.Errorf("the answer does not say what is being designed: %q", text)
	}

	// And the lane said so, which is the only thing on screen saying that work
	// is happening.
	started := nextDesign(t, lane)
	if started.Kind != EventHarnessDesign || started.Text != buildGoal || started.Hint != harnessDesigningWord {
		t.Fatalf("the lane opened with %v / %q / %q", started.Kind, started.Text, started.Hint)
	}
	close(answered)
	if done := nextDesign(t, lane); done.Kind != EventHarnessDesignDone {
		t.Fatalf("the design never landed, got %v", done.Kind)
	}
}

// A GOAL IS THE ONE THING THE DESIGNER CANNOT DO WITHOUT: it never sees this
// conversation, so a call with nothing in it is refused as a call and not as a
// design that failed a minute later.
func TestBuildHarnessNeedsAGoal(t *testing.T) {
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	for _, args := range []string{`{}`, `{"goal":"   "}`, `{"goal":`} {
		text, isError := runTool(t, agent, "build_harness", args)
		if !isError {
			t.Errorf("%s was accepted: %q", args, text)
		}
	}
}

// ── list_harnesses ──────────────────────────────────────────────────────────

// THE LIST IS WHAT DETECTION READS, which means a harness this conversation
// designed and saved a minute ago is in it — the same law that makes one
// reachable from the very next sentence.
func TestListHarnessesReadsTheRegistryThisConversationCanReach(t *testing.T) {
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	empty, isError := runTool(t, agent, "list_harnesses", `{}`)
	if isError || !strings.Contains(empty, "build_harness") {
		t.Fatalf("an empty registry answered %q", empty)
	}
	agent.registerHarness(subharness.Entry{
		Name:        "flake-triage",
		Description: "chase a flaky test to a fix",
		Revision:    3,
	})

	text, isError := runTool(t, agent, "list_harnesses", `{}`)
	if isError {
		t.Fatalf("the call was refused: %s", text)
	}
	for _, want := range []string{"flake-triage", "v3", "chase a flaky test to a fix"} {
		if !strings.Contains(text, want) {
			t.Errorf("the list does not carry %q: %q", want, text)
		}
	}
}

// ── the hand that is gone ───────────────────────────────────────────────────

// ABSENT, NOT REFUSING. The chat model may not open a planned run at all, and
// the way this codebase says that is the verb simply not being there: the belt,
// and so the schema and the prompt the model is handed, are byte-identical to a
// build that never had the hand. A tool left in place to answer "no" would be
// worse than useless — the model plans around a capability it has been told
// about, for the rest of the conversation, long after the first refusal.
//
// THE RUNNER IS STILL WIRED IN EVERY CASE HERE, which is the whole point. The
// engine is untouched and the seam is filled the way the shipping door fills it
// (cmd/aforge's chatv3_orchestrate.go); what is gone is any way for a turn to
// reach it.
func TestNoConversationCarriesTheAdaptiveVerb(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*Config)
	}{
		{
			name:  "the whole thing wired, the way the v3 door wires it",
			setup: func(config *Config) { buildConfig(config, t.TempDir()); config.OrchestrateRunner = neverRuns },
		},
		{
			name:  "a runner and somebody watching, which used to be the entire gate",
			setup: func(config *Config) { config.AskConsent = true; config.OrchestrateRunner = neverRuns },
		},
	} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, c.setup)
		if _, found := onBelt(agent, "run_adaptive"); found {
			t.Errorf("%s: run_adaptive is on the belt, so the model still has the verb", c.name)
		}
	}
}

// onBelt looks one hand up without failing the test when it is absent, which is
// the whole point of the belt table above.
func onBelt(agent *Agent, name string) (bare.Tool, bool) {
	for _, tool := range agent.tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return bare.Tool{}, false
}

// neverRuns is a runner wired but never expected to be called: it is what makes
// a build one that CAN orchestrate, for the belt tests above.
func neverRuns(context.Context, string, string, float64) (string, error) { return "1", nil }
