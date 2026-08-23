package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// THE MODEL'S HANDS ON THE BIG MACHINERY, from the three sides that matter: the
// hands exist only where they can work, the goal is required and is passed
// through whole, and each one STARTS something and comes straight back.

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
			want:  []string{"build_harness", "list_harnesses", "run_adaptive"},
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
			name:  "a runner and nothing else",
			setup: func(config *Config) { config.AskConsent = true; config.OrchestrateRunner = neverRuns },
			want:  []string{"run_adaptive"},
		},
		{
			name:  "a plain session",
			setup: func(*Config) {},
		},
	} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, c.setup)
		var carried []string
		for _, name := range []string{"build_harness", "list_harnesses", "run_adaptive"} {
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
		{"run_adaptive", "propose_task"},
		{"run_adaptive", "build_harness"},
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
	// ONE SOURCE OF TRUTH for the default tank: a schema that spelled its own
	// figure would be a figure that drifts from the one the run applies — and
	// so would this assertion, which once pinned the dollars as a literal and
	// broke on the day the default moved.
	tool, _ := onBelt(agent, "run_adaptive")
	if !strings.Contains(string(tool.Schema), orchestrate.Dollars(orchestrateDefaultCap)) {
		t.Errorf("run_adaptive's schema does not name the default tank: %s", tool.Schema)
	}
}

// WHAT THE MODEL IS TOLD IT HAS, AND WHAT THE PERSON CAN LOOK UP. These three
// hands are conditional, so the two completeness gates this repo runs — the belt
// against the manual (manual_test.go), the prompt against the belt — cannot see
// them on a bare test agent. They are checked here instead, because a tool the
// prompt never mentions is one the model reaches for by luck, and a tool no page
// describes is one the chat cannot answer a question about.
func TestTheHarnessHandsAreInThePromptAndTheManual(t *testing.T) {
	for _, name := range []string{"build_harness", "list_harnesses", "run_adaptive"} {
		if !strings.Contains(systemPrompt, name) {
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

// ── run_adaptive ────────────────────────────────────────────────────────────

// THE HAND REACHES THE RUNNER, with the goal whole and the tank the model
// named. The id comes back because everything a surface can do with a run — draw
// it, steer it, answer its gate — is keyed on it.
func TestRunAdaptiveReachesTheRunnerAndAnnouncesTheRun(t *testing.T) {
	var started startedRun
	agent, _ := newTestAgent(t, &scriptedCompleter{}, runConfig(&started))
	lane := agent.Orchestrations()

	text, isError := runTool(t, agent, "run_adaptive",
		`{"goal":"audit the pricing code","fuel_dollars":5}`)
	if isError {
		t.Fatalf("the call was refused: %s", text)
	}
	if started.calls != 1 {
		t.Fatalf("the runner ran %d times", started.calls)
	}
	if started.goal != "audit the pricing code" {
		t.Fatalf("the run was asked for %q", started.goal)
	}
	if started.cap != 5 {
		t.Fatalf("the tank is %v, want the $5 the call named", started.cap)
	}
	if started.model != "" {
		t.Fatalf("the run picked the model %q; a run rides the conversation's own", started.model)
	}
	if !strings.Contains(text, "7") || !strings.Contains(text, "$5.00") {
		t.Errorf("the answer names neither the run nor its tank: %q", text)
	}

	// AND THE SURFACE WAS TOLD. This event is the only thing that says a run
	// exists at all — a run is not a node, so it is on no roster.
	opening := nextRunEvent(t, lane)
	if opening.Kind != EventOrchestrateNote || opening.ID != 7 {
		t.Fatalf("the lane opened with %v / id %d", opening.Kind, opening.ID)
	}
	if !strings.Contains(opening.Text, "audit the pricing code") || !strings.Contains(opening.Text, "$5.00") {
		t.Fatalf("the announcement says %q", opening.Text)
	}
}

// A TANK NOBODY NAMED IS THE DEFAULT, and so is one named backwards: neither is
// worth a refusal that costs the person the run.
func TestRunAdaptiveFallsBackToTheDefaultTank(t *testing.T) {
	for _, args := range []string{`{"goal":"audit the pricing code"}`, `{"goal":"audit the pricing code","fuel_dollars":-3}`} {
		var started startedRun
		agent, _ := newTestAgent(t, &scriptedCompleter{}, runConfig(&started))
		if text, isError := runTool(t, agent, "run_adaptive", args); isError {
			t.Fatalf("%s was refused: %s", args, text)
		}
		if started.cap != orchestrateDefaultCap {
			t.Errorf("%s gave a tank of %v, want the default %v", args, started.cap, orchestrateDefaultCap)
		}
	}
}

// A GOAL IS REQUIRED HERE TOO, and for the same reason: the planner cannot see
// this conversation either.
func TestRunAdaptiveNeedsAGoal(t *testing.T) {
	var started startedRun
	agent, _ := newTestAgent(t, &scriptedCompleter{}, runConfig(&started))
	if text, isError := runTool(t, agent, "run_adaptive", `{"fuel_dollars":5}`); !isError {
		t.Fatalf("a goalless run was accepted: %q", text)
	}
	if started.calls != 0 {
		t.Fatalf("a run started anyway")
	}
}

// A RUNNER THAT DECLINES SAYS SO. An empty id is a run that never started, and
// answering with one would be the tool reporting work it did not do.
func TestRunAdaptiveSaysSoWhenNothingStarted(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) {
			return "", nil
		}
	})
	text, isError := runTool(t, agent, "run_adaptive", `{"goal":"audit the pricing code"}`)
	if !isError {
		t.Fatalf("a run that never started answered %q", text)
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
