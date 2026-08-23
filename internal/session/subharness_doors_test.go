package session

// THE FOUR DOORS, AGAINST A REGISTRY THAT IS ALL FAKE AND ALL REAL.
//
// Every test here builds a real [exec.Registry] with a scripted runner on it, so
// what is exercised is the wiring rather than a mock of the wiring: the same
// lookup a bundle out of a store goes through, the same manifest, the same
// deopt, the same task graph. What none of them does is reach a model or a
// network — the runner is a function in this file, and the one door that would
// make a model call (the intake fill) is fed a scripted completer.
//
// The laws these hold are the ones a person could be hurt by if they broke:
// nothing runs without consent, a whitelist is a ceiling and not a suggestion, a
// question with nobody there is never a yes, and what a run spent lands on the
// bill.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// ── the scripted program ────────────────────────────────────────────────────

// fakeRunner is one subharness written in Go, in this file, so a test can say
// exactly what a run does.
type fakeRunner struct {
	manifest exec.Manifest
	run      func(ctx context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error)
}

func (f *fakeRunner) Manifest() exec.Manifest { return f.manifest }

func (f *fakeRunner) Run(ctx context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error) {
	return f.run(ctx, input, env)
}

// fakeGeneralist is the worker the deoptimization path falls back to. It is an
// [exec.Executor] rather than a Runner because that is what the registry's
// fallback is, and it records what it was handed so a test can check that the
// long way was given the ORIGINAL input.
type fakeGeneralist struct {
	mu     sync.Mutex
	briefs []string
}

func (g *fakeGeneralist) Subharness() string { return exec.LinearSubharness }

func (g *fakeGeneralist) Run(_ context.Context, task exec.Task) (*exec.Outcome, error) {
	g.mu.Lock()
	g.briefs = append(g.briefs, task.Brief)
	g.mu.Unlock()
	return &exec.Outcome{Text: "the long way finished it", Usage: exec.Usage{Calls: 1, PromptTokens: 10, Cost: 0.25}}, nil
}

func (g *fakeGeneralist) took() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.briefs...)
}

// theProgram is the manifest every test in this file registers unless it says
// otherwise: one required field, one optional one with a default, a cue, and a
// whitelist naming exactly one tool.
func theProgram() exec.Manifest {
	return exec.Manifest{
		SubharnessInfo: exec.SubharnessInfo{
			Name:    "flake-triage",
			Purpose: "work out why one test is flaky and say what to do about it",
		},
		Cues:      []string{"flaky test"},
		Whitelist: []string{"read"},
		Input: exec.Schema(`{
  "type": "object",
  "x-order": ["test", "runs"],
  "required": ["test"],
  "properties": {
    "test": {"type": "string", "title": "which test", "description": "the failing test's name"},
    "runs": {"type": "integer", "title": "how many times to run it", "default": 20}
  }
}`),
		Output: exec.Schema(`{"type":"object","required":["finding"],"properties":{"finding":{"type":"string"}}}`),
	}
}

// registryWith builds a registry with the generalist behind it and one scripted
// program on it.
func registryWith(t *testing.T, general *fakeGeneralist, runners ...exec.Runner) *exec.Registry {
	t.Helper()
	registry := exec.NewRegistry(general)
	for _, runner := range runners {
		if err := registry.RegisterRunner(runner); err != nil {
			t.Fatalf("the registry refused a program: %v", err)
		}
	}
	return registry
}

// agentWithPrograms is every test's opening line: a session with a registry
// wired, somebody watching, and a scripted completer for the one door that
// would otherwise call a model.
func agentWithPrograms(t *testing.T, registry *exec.Registry, mutate func(*Config)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Subharnesses = registry
		config.AskConsent = true
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

// ── the list and the card ───────────────────────────────────────────────────

// THE LIST IS THE REGISTRY'S ANSWER, WITH THE BASELINE STRUCK OFF. `linear` is
// registered and resolvable by name — the deopt needs it — and it is on no list
// anybody picks from, because it is what you get when you pick nothing.
func TestTheListIsEveryProgramExceptTheOneNobodyPicks(t *testing.T) {
	registry := registryWith(t, &fakeGeneralist{}, &fakeRunner{manifest: theProgram()})
	front, err := exec.FrontExecutor(&fakeGeneralist{}, exec.LeafManifest(exec.SubharnessFor(exec.LinearSubharness)))
	if err != nil {
		t.Fatalf("the baseline could not be fronted: %v", err)
	}
	if err := registry.RegisterRunner(front); err != nil {
		t.Fatalf("the baseline could not be registered: %v", err)
	}
	agent := agentWithPrograms(t, registry, func(config *Config) {
		config.SubharnessLastRun = func(name string) string {
			if name == "flake-triage" {
				return "yesterday · finished"
			}
			return ""
		}
	})

	rows := agent.SubharnessList()
	if len(rows) != 1 {
		t.Fatalf("the list has %d rows, not 1: %+v", len(rows), rows)
	}
	if rows[0].Manifest.Name != "flake-triage" {
		t.Fatalf("the list drew %q", rows[0].Manifest.Name)
	}
	if rows[0].LastRun != "yesterday · finished" {
		t.Fatalf("the store's note did not reach the row: %q", rows[0].LastRun)
	}
}

// A LIST WITH NO REGISTRY IS NOTHING, CALMLY. It is the seam's whole posture:
// nil is subharnesses off, and a surface built against these doors draws nothing
// rather than an error.
func TestWithNoRegistryTheDoorsAnswerNothingCalmly(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if rows := agent.SubharnessList(); rows != nil {
		t.Fatalf("a build with no registry listed %d programs", len(rows))
	}
	if _, err := agent.SubharnessIntake("flake-triage"); !errors.Is(err, errSubharnessUnwired) {
		t.Fatalf("the card said %v", err)
	}
	if _, _, err := agent.SubharnessRun(context.Background(), "flake-triage", nil); !errors.Is(err, errSubharnessUnwired) {
		t.Fatalf("the launching door said %v", err)
	}
}

// THE CARD IS THE SCHEMA'S OWN FIELDS IN THE SCHEMA'S OWN ORDER, with the
// DEFAULT already in place and not counted as a filling — a default is the
// program's answer for a question nobody answered, and the card draws the two
// differently.
func TestTheCardCarriesEveryFieldAndTheRequiredBlanks(t *testing.T) {
	registry := registryWith(t, &fakeGeneralist{}, &fakeRunner{manifest: theProgram()})
	agent := agentWithPrograms(t, registry, nil)

	card, err := agent.SubharnessIntake("flake-triage")
	if err != nil {
		t.Fatalf("the card did not open: %v", err)
	}
	if len(card.Fields) != 2 || card.Fields[0].Field.Name != "test" || card.Fields[1].Field.Name != "runs" {
		t.Fatalf("the card's fields are not the schema's, in order: %+v", card.Fields)
	}
	if card.Fields[1].Filled {
		t.Fatal("a schema default was drawn as something somebody filled in")
	}
	if string(card.Fields[1].Value) != "20" {
		t.Fatalf("the default did not reach the card: %q", card.Fields[1].Value)
	}
	if len(card.Missing) != 1 || card.Missing[0] != "test" {
		t.Fatalf("the required blank is not the one that is blank: %v", card.Missing)
	}
}

// A NAME NOTHING HAS IS AN ERROR AND NOT AN EMPTY CARD. Somebody typed a name,
// and a blank card would send them looking for the fields rather than the typo.
func TestANameNothingHasIsATypoAndNotABlankCard(t *testing.T) {
	registry := registryWith(t, &fakeGeneralist{}, &fakeRunner{manifest: theProgram()})
	agent := agentWithPrograms(t, registry, nil)
	if _, err := agent.SubharnessIntake("flaketriage"); !errors.Is(err, exec.ErrNoSubharness) {
		t.Fatalf("a name nothing has answered %v", err)
	}
}

// ── the run as a node ───────────────────────────────────────────────────────

// A RUN IS A TASK NODE, AND THE PAIR IT ANSWERS IS THE PAIR StartTask ANSWERS —
// which is what lets a surface that already knows how to open a room on a
// started task need no second call site.
func TestARunIsANodeThatFinishesAndSaysWhatItProduced(t *testing.T) {
	program := &fakeRunner{manifest: theProgram(), run: func(_ context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error) {
		_ = env.Log(context.Background(), "reading the last twenty runs")
		return exec.RunResult{
			Output: json.RawMessage(`{"finding":"a shared temp directory"}`),
			Report: "it is a shared temp directory",
		}, nil
	}}
	registry := registryWith(t, &fakeGeneralist{}, program)
	agent := agentWithPrograms(t, registry, nil)

	id, title, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	if id == 0 || !strings.Contains(title, "flake-triage") {
		t.Fatalf("the run answered %d, %q", id, title)
	}
	node := waitForSettled(t, agent, id)
	if node.State != TaskDone {
		t.Fatalf("the run settled %s: %s", node.State, node.Report)
	}
	if !strings.Contains(node.Report, "shared temp directory") {
		t.Fatalf("the program's own account did not reach the card: %q", node.Report)
	}
	if node.Kind != TaskKindSubharness {
		t.Fatalf("the node is a %q", node.Kind)
	}
}

// A RUN STOPS THROUGH THE ROUTE EVERY OTHER TASK STOPS THROUGH, and the line it
// answers with does not promise a branch: a run works in no worktree, so the
// half of that sentence about where the work is kept would point at nothing.
func TestARunStopsOnTheSameRouteEveryOtherTaskDoes(t *testing.T) {
	started := make(chan struct{})
	program := &fakeRunner{manifest: theProgram(), run: func(ctx context.Context, _ json.RawMessage, _ exec.Env) (exec.RunResult, error) {
		close(started)
		<-ctx.Done()
		// A CANCELLED RUN COMES BACK INCOMPLETE AND NOT AS AN ERROR, which is the
		// runtime's own bargain and is what keeps a person's ✕ out of the
		// deoptimization path.
		return exec.RunResult{Incomplete: "it was stopped"}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)

	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	<-started
	line, err := agent.Cancel("task:" + itoa64(id))
	if err != nil {
		t.Fatalf("the stop route refused the run: %v", err)
	}
	if !strings.Contains(line, "journal is kept") {
		t.Fatalf("the stop line was %q", line)
	}
	if strings.Contains(line, "branch") {
		t.Fatalf("a run promised a branch it does not have: %q", line)
	}
	node := waitForSettled(t, agent, id)
	if node.State != TaskFailed || !strings.Contains(node.Report, "stopped") {
		t.Fatalf("a stopped run settled %s: %q", node.State, node.Report)
	}
}

// WHAT A RUN SPENT LANDS ON THE SESSION'S BILL, through the auxiliary door, with
// the turn seal untouched. Until this fold existed, DESIGNING a harness was
// billed to the conversation and RUNNING one — dozens of calls — was free to
// every cost surface in the program.
func TestWhatARunSpentLandsOnTheSessionsLedger(t *testing.T) {
	program := &fakeRunner{manifest: theProgram(), run: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
		return exec.RunResult{
			Output: json.RawMessage(`{"finding":"a clock"}`),
			Spend:  exec.Spend{Model: "test/model", Calls: 3, Input: 900, Output: 120, CostUSD: 0.42},
		}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)

	before := agent.Usage()
	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	waitForSettled(t, agent, id)

	// THE FIGURES ARE READ AS A DELTA because the node's own life spends a
	// little of its own — settling a task is not free — and what this test is
	// about is that the RUN's figures arrive at all, on the auxiliary door,
	// rather than that nothing else on this session ever spends anything.
	after := agent.Usage()
	if after.CostUSD-before.CostUSD != 0.42 {
		t.Fatalf("the run's cost did not reach the session: %v to %v", before.CostUSD, after.CostUSD)
	}
	if after.Input-before.Input < 900 || after.Output-before.Output < 120 {
		t.Fatalf("the run's tokens did not reach the session: %+v to %+v", before, after)
	}
	if after.Calls-before.Calls < 3 {
		t.Fatalf("the run's calls did not reach the session: %+v to %+v", before, after)
	}
	// THE TURN SEAL STAYS ZERO-TOKEN. A run has its own client, its own
	// messages and often its own model, so folding its context into this
	// conversation's would move the number compaction is decided on.
	if after.Turns-before.Turns > 1 {
		t.Fatalf("a run's calls were counted as turns of this conversation: %+v to %+v", before, after)
	}
}

// A RUN NOBODY WAS BILLED FOR RECORDS NOTHING, and a run that landed records a
// note the next list can draw.
func TestAFinishedRunLeavesTheNoteTheNextListDraws(t *testing.T) {
	program := &fakeRunner{manifest: theProgram(), run: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
		return exec.RunResult{Output: json.RawMessage(`{"finding":"a clock"}`), Spend: exec.Spend{CostUSD: 0.1}}, nil
	}}
	var (
		mu    sync.Mutex
		notes = map[string]SubharnessRunNote{}
	)
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), func(config *Config) {
		config.SubharnessRecordRun = func(name string, note SubharnessRunNote) {
			mu.Lock()
			notes[name] = note
			mu.Unlock()
		}
	})
	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	waitForSettled(t, agent, id)

	mu.Lock()
	note, kept := notes["flake-triage"]
	mu.Unlock()
	if !kept || !note.Finished || note.CostUSD != 0.1 {
		t.Fatalf("the run left no usable note: %+v (kept: %v)", note, kept)
	}
}

// ── the deoptimization ──────────────────────────────────────────────────────

// A PROGRAM THAT ASKED FOR THE LONG WAY GETS THE LONG WAY, with the ORIGINAL
// input, and the person is told the step needed a closer look — never that
// anything failed.
func TestAProgramThatNeededACloserLookIsHandledTheLongWay(t *testing.T) {
	program := &fakeRunner{manifest: theProgram(), run: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
		return exec.RunResult{FellBack: "there is no test name in this"}, nil
	}}
	general := &fakeGeneralist{}
	agent := agentWithPrograms(t, registryWith(t, general, program), nil)

	// The generalist takes a TaskInput, so the original input is written in the
	// shape the long way reads — which is the point: it is handed on untouched.
	input := json.RawMessage(`{"brief":"work out why TestFoo is flaky"}`)
	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", input)
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	node := waitForSettled(t, agent, id)

	took := general.took()
	if len(took) != 1 || took[0] != "work out why TestFoo is flaky" {
		t.Fatalf("the long way was not handed the original input: %v", took)
	}
	if node.State != TaskDone {
		t.Fatalf("a run handled the long way settled %s: %q", node.State, node.Report)
	}
	if !strings.Contains(node.Report, exec.DeoptWord) {
		t.Fatalf("the person was not told the step needed a closer look: %q", node.Report)
	}
	for _, banned := range []string{"failed", "error", "guard"} {
		if strings.Contains(strings.ToLower(node.Report), banned) {
			t.Fatalf("the long-way line reads as a fault (%q): %q", banned, node.Report)
		}
	}
}

// A RUN THAT COULD NOT BE MADE TO HAPPEN TAKES THE SAME ROAD. An error is a
// broken program rather than a program that decided something, and either way
// the work still has to get done.
func TestARunThatCouldNotBeMadeToHappenIsAlsoHandledTheLongWay(t *testing.T) {
	program := &fakeRunner{manifest: theProgram(), run: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
		return exec.RunResult{}, errors.New("the bundle is not a program")
	}}
	general := &fakeGeneralist{}
	agent := agentWithPrograms(t, registryWith(t, general, program), nil)

	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"brief":"do the work"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	node := waitForSettled(t, agent, id)
	if len(general.took()) != 1 {
		t.Fatal("nothing fell back to the long way")
	}
	if node.State != TaskDone || !strings.Contains(node.Report, exec.DeoptWord) {
		t.Fatalf("the run settled %s: %q", node.State, node.Report)
	}
}

// ── the whitelist and the consent doors ─────────────────────────────────────

// THE WHITELIST IS A CEILING AND THE BELT IS A FACT, and the two refusals are
// two different sentences because they are two different things for a person to
// fix.
func TestTheWhitelistAndTheBeltAreTwoDifferentRefusals(t *testing.T) {
	var refusals []string
	program := &fakeRunner{manifest: theProgram(), run: func(ctx context.Context, _ json.RawMessage, env exec.Env) (exec.RunResult, error) {
		if _, err := env.Tool(ctx, "bash", map[string]any{"command": "echo hi"}); err != nil {
			refusals = append(refusals, err.Error())
		}
		// `read` is on the whitelist AND on the belt, so it goes through — and a
		// file that is not there is the tool's own answer rather than a refusal
		// from this door.
		if _, err := env.Tool(ctx, "read", map[string]any{"file_path": "/nope/nothing.txt"}); err != nil {
			refusals = append(refusals, "read: "+err.Error())
		}
		return exec.RunResult{Output: json.RawMessage(`{"finding":"done"}`)}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)

	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	waitForSettled(t, agent, id)

	if len(refusals) == 0 || !strings.Contains(refusals[0], "declared") {
		t.Fatalf("a tool off the whitelist was not refused as one: %v", refusals)
	}
}

// A TOOL THE POLICY REFUSES IS REFUSED FOR THE PROGRAM TOO, which is what makes
// "through the same consent doors" a fact about this code: the gate is reached
// through [Agent.executeTool] and there is no way round it.
func TestAProgramsToolCallGoesThroughTheSameGateAModelsDoes(t *testing.T) {
	var refusal string
	program := &fakeRunner{manifest: theProgram(), run: func(ctx context.Context, _ json.RawMessage, env exec.Env) (exec.RunResult, error) {
		if _, err := env.Tool(ctx, "read", map[string]any{"file_path": "anything.txt"}); err != nil {
			refusal = err.Error()
		}
		return exec.RunResult{Output: json.RawMessage(`{"finding":"done"}`)}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), func(config *Config) {
		// The policy denies everything, so a call that reached the belt without
		// passing the gate would come back with a file's contents instead of this.
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionDeny}
	})

	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	waitForSettled(t, agent, id)

	if !strings.Contains(refusal, "denied by approval rule") {
		t.Fatalf("the program's tool call did not go through the gate: %q", refusal)
	}
}

// ── the question ────────────────────────────────────────────────────────────

// WITH NOBODY WATCHING, A QUESTION TAKES WHAT THE GATE DECLARED — and where the
// gate declared nothing, the run STOPS. This is the exact line the old bridge
// crossed: its gate auto-approved and wrote "nobody was there to ask, so it
// carried on" into the trail afterwards.
func TestAQuestionWithNobodyThereIsNeverAYes(t *testing.T) {
	answers := make(chan exec.AskAnswer, 2)
	program := &fakeRunner{manifest: theProgram(), run: func(ctx context.Context, _ json.RawMessage, env exec.Env) (exec.RunResult, error) {
		declared, _ := env.Ask(ctx, "run it twenty times?", exec.AskOptions{Default: "yes"})
		answers <- declared
		bare, _ := env.Ask(ctx, "shall I open a pull request?", exec.AskOptions{})
		answers <- bare
		return exec.RunResult{Incomplete: "nobody was there to answer whether to open a pull request"}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), func(config *Config) {
		config.AskConsent = false
	})

	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	node := waitForSettled(t, agent, id)

	declared := <-answers
	if declared.Text != "yes" || declared.Unanswered {
		t.Fatalf("the declared answer was not taken: %+v", declared)
	}
	bare := <-answers
	if !bare.Unanswered || bare.Text != "" {
		t.Fatalf("a question with no declared answer was answered anyway: %+v", bare)
	}
	if !strings.Contains(node.Report, "incomplete") {
		t.Fatalf("a run that stopped on a question did not say it was incomplete: %q", node.Report)
	}
}

// A QUESTION REACHES THE PERSON THROUGH THE RUN'S OWN ROOM, and while it stands
// the ROW says the work needs somebody — which is how anybody who is not
// standing in that room finds out.
func TestAQuestionReachesTheRoomAndTheRowSaysItNeedsSomebody(t *testing.T) {
	asked := make(chan struct{})
	answered := make(chan exec.AskAnswer, 1)
	program := &fakeRunner{manifest: theProgram(), run: func(ctx context.Context, _ json.RawMessage, env exec.Env) (exec.RunResult, error) {
		close(asked)
		answer, err := env.Ask(ctx, "shall I keep going?", exec.AskOptions{Options: []string{"yes", "no"}})
		if err != nil {
			return exec.RunResult{}, err
		}
		answered <- answer
		return exec.RunResult{Output: json.RawMessage(`{"finding":"done"}`)}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)

	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", json.RawMessage(`{"test":"TestFoo"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	<-asked
	waitFor(t, "the question to be registered", func() bool { return agent.PendingSubharnessAsk(id) })
	waitFor(t, "the row to say it needs somebody", func() bool {
		return agent.taskNode(id).notice().Doing == HarnessPhaseAsking
	})

	agent.AnswerSubharness(id, "yes", false)
	answer := <-answered
	if answer.Text != "yes" || answer.TakingOver {
		t.Fatalf("the person's answer arrived as %+v", answer)
	}
	if node := waitForSettled(t, agent, id); node.State != TaskDone {
		t.Fatalf("the run settled %s: %q", node.State, node.Report)
	}
}

// ── the proposal ────────────────────────────────────────────────────────────

// NOTHING RUNS BECAUSE CHAT PROPOSED IT. The card is the consent, and the only
// road from a proposal to a launch is a person answering yes.
func TestAProposalRunsNothingUntilSomebodySaysYes(t *testing.T) {
	program := &fakeRunner{manifest: theProgram(), run: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
		return exec.RunResult{Output: json.RawMessage(`{"finding":"a clock"}`)}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)
	agent.record(textMessage("user", "TestFoo is a flaky test and I want to know why"))

	cards, _ := agent.WatchHarnessDesigns()
	declined := make(chan string, 1)
	go func() {
		text, _ := runTool(t, agent, "propose_subharness",
			`{"name":"flake-triage","reason":"the test name is here"}`)
		declined <- text
	}()

	card := waitForCard(t, cards)
	if card.Subharness == nil || card.Subharness.Manifest.Name != "flake-triage" {
		t.Fatalf("the card carried no program: %+v", card)
	}
	if !strings.Contains(card.Subharness.Why, "this looks like flake-triage") {
		t.Fatalf("chat's reason did not reach the card: %q", card.Subharness.Why)
	}
	if agent.taskNode(1) != nil {
		t.Fatal("a node was admitted before anybody answered the card")
	}

	agent.ResolveSubharness(card.ID, false, nil)
	if text := <-declined; !strings.Contains(text, "did not run") {
		t.Fatalf("a declined proposal said %q", text)
	}
	if agent.taskNode(1) != nil {
		t.Fatal("a declined proposal started something anyway")
	}
}

// A WEAK MATCH RAISES NOTHING. Phase 1's confidence rule is lexical and dull,
// and its whole job is to make silence the answer to a guess — a conversation
// that has to swat away a suggestion is worse off than one that never got it.
func TestAWeakMatchRaisesNothingAtAll(t *testing.T) {
	program := &fakeRunner{manifest: theProgram()}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)
	agent.record(textMessage("user", "can you help me rename this variable"))

	text, isError := runTool(t, agent, "propose_subharness",
		`{"name":"flake-triage","reason":"it feels related"}`)
	if isError {
		t.Fatalf("a weak match was reported as a fault: %q", text)
	}
	if !strings.Contains(text, "Nothing was raised") {
		t.Fatalf("a weak match raised something: %q", text)
	}
}

// THE VERB IS ABSENT WHERE IT CANNOT WORK, which is this belt's law: a model
// told it can run a saved program plans around that ability for the rest of the
// conversation, long after the first refusal.
func TestTheProposeVerbIsAbsentWithNobodyToAnswerTheCard(t *testing.T) {
	program := &fakeRunner{manifest: theProgram()}
	registry := registryWith(t, &fakeGeneralist{}, program)

	watched := agentWithPrograms(t, registry, nil)
	if !hasTool(watched, "propose_subharness") || !hasTool(watched, "list_subharnesses") {
		t.Fatal("a watched session with programs on it was not given the verbs")
	}

	unwatched, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Subharnesses = registry
		config.AskConsent = false
	})
	if hasTool(unwatched, "propose_subharness") {
		t.Fatal("a session with nobody watching was given a verb whose card nobody can answer")
	}

	empty, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Subharnesses = exec.NewRegistry(&fakeGeneralist{})
		config.AskConsent = true
	})
	if hasTool(empty, "propose_subharness") {
		t.Fatal("a session with no programs was given a verb over an empty list")
	}
}

// ── the small helpers these tests share ─────────────────────────────────────

// waitForSettled waits for one node to stop moving and answers its notice.
func waitForSettled(t *testing.T, agent *Agent, id uint64) TaskNotice {
	t.Helper()
	var settled TaskNotice
	waitFor(t, "the run to settle", func() bool {
		node := agent.taskNode(id)
		if node == nil {
			return false
		}
		notice := node.notice()
		if !notice.State.settled() {
			return false
		}
		settled = notice
		return true
	})
	return settled
}

// waitForCard reads past whatever else is on the standing lane to the intake
// card, failing rather than hanging.
func waitForCard(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatal("the standing lane closed before a card arrived")
			}
			if event.Kind == EventSubharnessProposal {
				return event
			}
		case <-deadline:
			t.Fatal("no intake card was ever raised")
			return Event{}
		}
	}
}
