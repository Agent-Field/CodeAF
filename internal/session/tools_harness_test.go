package session

// The harness tool, as tests: the card a person is shown, the approval between
// writing one and having one, the version that moves on every edit, and the
// gate in the middle of a run — including the third answer.
//
// Nothing here reaches a provider for anything but a scripted step, and nothing
// here runs a real worker: the seam under test is what the SESSION does around
// internal/subharness, and the interpreter's own control flow is proved in that
// package against a scripted environment.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// harnessAgent builds an agent with a registry, and hands back the directory so
// a test can look at what landed on disk.
func harnessAgent(t *testing.T, completer Completer, watched bool) (*Agent, string) {
	t.Helper()
	registry := t.TempDir()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.HarnessDir = registry
		config.AskConsent = watched
	})
	return agent, registry
}

// demoEntry is the JSON a model would write: one worker, then a gate that
// offers to be taken over.
const demoEntry = `{
  "name": "demo",
  "description": "look, then ask",
  "tools": ["read"],
  "verify": "loop",
  "program": [
    {"id": "look", "kind": "agent.loop", "prompt": "look at it", "tools": ["read"]},
    {"id": "land", "kind": "human.gate", "prompt": "land it?", "escalate": true}
  ]
}`

func harnessCall(action, body string) string {
	arguments := map[string]any{"action": action}
	switch action {
	case "preview", "register":
		arguments["harness"] = body
	default:
		arguments["name"] = body
	}
	encoded, _ := json.Marshal(arguments)
	return string(encoded)
}

// A PREVIEW IS A CARD AND NOTHING ELSE: numbered steps, the shape inline, and
// not one byte written.
func TestHarnessPreviewDrawsTheCardAndWritesNothing(t *testing.T) {
	agent, registry := harnessAgent(t, &scriptedCompleter{}, true)

	text, isError := runTool(t, agent, "harness", harnessCall("preview", demoEntry))
	if isError {
		t.Fatalf("preview answered as an error:\n%s", text)
	}
	for _, want := range []string{
		"draft",             // nothing is registered, and the card says so
		"1   agent.loop",    // numbered steps
		"2   human.gate",    //
		"land it? · interv", // the escalation is on the card
		"tools  read",       // the bounds are what approval is about
		"verify  loop",      //
		"dynamism  fixed",   //
		"call register",     // and what to do next
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the preview card is missing %q:\n%s", want, text)
		}
	}
	if entries, _ := filepath.Glob(filepath.Join(registry, "*")); len(entries) != 0 {
		t.Fatalf("a preview wrote to the registry: %v", entries)
	}
}

// A program that will not run is refused on the card, in words the model can
// act on — never registered and never half-written.
func TestHarnessPreviewRefusesABrokenProgram(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, true)
	broken := `{"name":"bad","tools":["read"],"program":[
		{"id":"x","kind":"agent.loop","prompt":"go","tools":["curl"]}]}`
	text, isError := runTool(t, agent, "harness", harnessCall("preview", broken))
	if !isError || !strings.Contains(text, "not in this harness's tools") {
		t.Fatalf("a tool off the whitelist previewed anyway:\n%s", text)
	}
}

// The vocabulary is generated from the registry, so a kind that exists is a
// kind the model is told about.
func TestHarnessKindsPrintsTheWholeVocabulary(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, true)
	text, isError := runTool(t, agent, "harness", `{"action":"kinds"}`)
	if isError {
		t.Fatalf("kinds answered as an error:\n%s", text)
	}
	for _, doc := range subharness.Kinds() {
		if !strings.Contains(text, string(doc.Kind)) {
			t.Fatalf("the vocabulary does not mention %s:\n%s", doc.Kind, text)
		}
	}
	for _, want := range []string{"contains <text>", "adversarial", "selfmod", "cap"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the vocabulary does not mention %q:\n%s", want, text)
		}
	}
}

// REGISTERING ASKS, AND THE VERSION MOVES ON EVERY EDIT. Two approved
// registrations of the same name are v1 and v2, and the entry on disk points at
// the newest.
func TestHarnessRegisterAsksAndBumpsTheVersion(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "harness", harnessCall("register", demoEntry)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "harness", harnessCall("register", demoEntry)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, registry := harnessAgent(t, completer, true)

	asked := 0
	approve := func(event Event) {
		asked++
		agent.ResolveHarnessGate(event.ID, GateApprove, "")
	}
	drainAnswering(t, mustSubmit(t, agent, "keep this shape"), approve)
	drainAnswering(t, mustSubmit(t, agent, "and again"), approve)

	if asked != 2 {
		t.Fatalf("the person was asked %d times, want one question per registration", asked)
	}
	store := subharness.New(registry)
	current, err := store.Load("demo")
	if err != nil {
		t.Fatalf("nothing was registered: %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("the entry is at v%d after two registrations, want v2", current.Version)
	}
	if first, err := store.LoadVersion("demo", 1); err != nil || first.Version != 1 {
		t.Fatalf("v1 was not kept: %+v (%v)", first, err)
	}
}

// A REFUSAL WRITES NOTHING, and the model is told plainly rather than with an
// error row: it asked a reasonable question and got a no.
func TestHarnessRegisterDeclinedWritesNothing(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "harness", harnessCall("register", demoEntry)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("fine"), nil },
	}}
	agent, registry := harnessAgent(t, completer, true)

	events := drainAnswering(t, mustSubmit(t, agent, "keep it"), func(event Event) {
		agent.ResolveHarnessGate(event.ID, GateDecline, "not yet")
	})
	if _, ok := firstOfKind(events, EventToolFailed); ok {
		t.Fatalf("a refusal was reported as a failed call")
	}
	if _, err := subharness.New(registry).Load("demo"); err == nil {
		t.Fatalf("a declined registration was written anyway")
	}
	if got := messageText(lastToolMessage(t, agent)); !strings.Contains(got, "not yet") {
		t.Fatalf("the model was not told why: %q", got)
	}
}

// NOBODY WATCHING MEANS NOTHING IS WRITTEN. It is consent's own law: blocking
// would hang a headless run on a question with no reader, and allowing would
// make the question mean nothing wherever the surface is not a terminal.
func TestHarnessRegisterRefusesWhenNobodyIsWatching(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "harness", harnessCall("register", demoEntry)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
	}}
	agent, registry := harnessAgent(t, completer, false)
	collect(t, mustSubmit(t, agent, "keep it"))
	if _, err := subharness.New(registry).Load("demo"); err == nil {
		t.Fatalf("an unwatched session registered a harness on nobody's authority")
	}
}

// seedDemo registers the demo harness directly, for the tests that are about
// running one rather than about writing one.
func seedDemo(t *testing.T, registry string) subharness.Harness {
	t.Helper()
	harness, err := subharness.Decode([]byte(demoEntry))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := subharness.New(registry).Save(harness)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// A RUN ASKS FIRST, STOPS AT ITS GATE, AND THE THIRD ANSWER ENDS IT. The trace
// lands under the harness, and it says the person took it over.
func TestHarnessRunStopsAtTheGateAndTheEscalationEndsIt(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "harness", `{"action":"run","name":"demo","input":"the nightly failed"}`), nil
		},
		// The agent.loop node's own call: no tools, one answer.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("it is the reconciler"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, registry := harnessAgent(t, completer, true)
	seedDemo(t, registry)

	var rules []string
	drainAnswering(t, mustSubmit(t, agent, "run demo"), func(event Event) {
		rules = append(rules, event.Rule)
		switch {
		case strings.Contains(event.Rule, "harness run"):
			agent.ResolveHarnessGate(event.ID, GateApprove, "")
		default:
			agent.ResolveHarnessGate(event.ID, GateIntervene, "I will take it from here")
		}
	})

	if len(rules) != 2 {
		t.Fatalf("the person was asked %d times, want the run and the gate: %v", len(rules), rules)
	}
	if !strings.Contains(rules[1], "harness gate") || !strings.Contains(rules[1], "intervene") {
		t.Fatalf("the gate did not offer to be taken over: %q", rules[1])
	}

	store := subharness.New(registry)
	run, ok := store.LastRun("demo")
	if !ok {
		t.Fatalf("the run wrote no trace")
	}
	if run.Status != subharness.StatusIntervened {
		t.Fatalf("the trace says %s, want intervened", run.Status)
	}
	if run.Output != "I will take it from here" {
		t.Fatalf("the person's words are not the outcome: %q", run.Output)
	}
	// THE TRACE IS A DAG WITH THE PATH IN IT: the worker, then the gate it
	// stopped at, with the answer recorded on the node.
	if len(run.Nodes) != 2 || run.Nodes[1].Answer != "intervened" {
		t.Fatalf("the trace does not record the path: %+v", run.Nodes)
	}
	if run.Input != "the nightly failed" {
		t.Fatalf("the run did not record what it was started on: %q", run.Input)
	}
	// And the model is handed the trace rather than an error.
	if got := messageText(lastToolMessage(t, agent)); !strings.Contains(got, "intervened") ||
		!strings.Contains(got, "trace · ") {
		t.Fatalf("the model was not handed the trace:\n%s", got)
	}
}

// An approved gate lets the run finish, and the history keeps every run.
func TestHarnessRunApprovedFinishesAndIsKept(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "harness", `{"action":"run","name":"demo"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("looked"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, registry := harnessAgent(t, completer, true)
	seedDemo(t, registry)

	drainAnswering(t, mustSubmit(t, agent, "run demo"), func(event Event) {
		agent.ResolveHarnessGate(event.ID, GateApprove, "")
	})

	store := subharness.New(registry)
	run, ok := store.LastRun("demo")
	if !ok || run.Status != subharness.StatusOK {
		t.Fatalf("the approved run did not finish: %+v", run)
	}
	paths, _ := store.Runs("demo")
	if len(paths) != 1 {
		t.Fatalf("the history holds %d runs, want the one that happened", len(paths))
	}
	if filepath.Base(filepath.Dir(paths[0])) != "run" {
		t.Fatalf("the trace did not land under the harness's run directory: %s", paths[0])
	}
	// The version the run was made against is on the record, which is the whole
	// point of the version being a pointer.
	if run.Version != 1 {
		t.Fatalf("the run does not name the version it ran: v%d", run.Version)
	}
}

// The list is what is registered, with the version and the last run beside it.
func TestHarnessListSaysVersionAndLastRun(t *testing.T) {
	agent, registry := harnessAgent(t, &scriptedCompleter{}, true)
	seedDemo(t, registry)
	text, isError := runTool(t, agent, "harness", `{"action":"list"}`)
	if isError {
		t.Fatalf("list answered as an error:\n%s", text)
	}
	if !strings.Contains(text, "demo  v1  look, then ask") {
		t.Fatalf("the list does not say what is registered:\n%s", text)
	}
	if strings.Contains(text, "last run") {
		t.Fatalf("a harness that has never run claims it has:\n%s", text)
	}
}

// A session with no registry configured says so instead of growing a tool that
// refuses — the belt's own law (tools.go).
func TestHarnessToolIsAbsentWithoutARegistry(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, tool := range agent.tools {
		if tool.Name == "harness" {
			t.Fatalf("a session with no registry was handed a harness tool")
		}
	}
}

// A task node has nobody to ask, so it does not get the tool at all.
func TestHarnessToolIsAbsentInATaskNode(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.HarnessDir = t.TempDir()
		config.InTask = true
	})
	for _, tool := range agent.tools {
		if tool.Name == "harness" {
			t.Fatalf("a task node was handed the registry")
		}
	}
}

// A check that reads like a command is run as one; a check that reads like a
// question is put to a model. The rule is crude on purpose and stated here so
// that making it cleverer breaks a test rather than a run.
func TestChecksAreSortedIntoCommandsAndQuestions(t *testing.T) {
	for _, one := range []struct {
		check   string
		command bool
	}{
		{"go test ./...", true},
		{"make check", true},
		{"./scripts/verify.sh --fast", true},
		{"does the report name every file it changed?", false},
		{"The answer must list the files, the counts, and the totals", false},
		{"", false},
		{"go test ./...\ngo vet ./...", false},
	} {
		if got := harnessLooksLikeCommand(one.check); got != one.command {
			t.Fatalf("%q read as command=%v, want %v", one.check, got, one.command)
		}
	}
}
