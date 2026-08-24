package session

// WIDTH, WHERE NOBODY IS WATCHING.
//
// A standing order that fires at 2am and starts work is the one road on which
// "this is wider than one pair of hands" could not be said: the run had no
// graph, so its session had no way to hand anything out and no verb to do it
// with. These drive the REAL standing-run road — [standingRunner.Run], the
// config it builds, the belt that config makes and the tool the model actually
// calls — because the whole of this fix is that a firing joins the road every
// other piece of work is already on rather than getting a second one.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// watchedFiring is [standingChildRunner] with the firing's own session kept, so
// a test can ask what belt it was handed and what graph it was pointed at. The
// hook runs on the config the product built and the agent the product made from
// it: nothing here substitutes either.
func watchedFiring(t *testing.T, root string, completer Completer, ready func(Config, *Agent)) *standingRunner {
	t.Helper()
	runner := standingChildRunner(t, root, completer)
	runner.parent.Divide = true
	inner := runner.child
	runner.child = func(cfg Config) (*Agent, error) {
		agent, err := inner(cfg)
		if err == nil && ready != nil {
			ready(cfg, agent)
		}
		return agent, err
	}
	return runner
}

// wideNightly is one overnight job whose own brief names enough separate items
// to arm the road, and [nightly] is the same job written narrowly.
func wideNightly(workspace string) standing.Item {
	item := nightly(workspace)
	item.Does.Brief = wideBrief
	return item
}

// dividingModel is the model one firing runs against: it reaches for
// `divide_work` on its first turn and then says one sentence per turn for as
// long as it is asked.
//
// IT COUNTS TURNS AND NOT REQUESTS, which is the whole reason it is written out
// rather than scripted step by step. The number of times a divided run
// re-enters the model depends on how many part reports arrive together, and a
// firing's session ALSO pays for one auxiliary call of its own — the namer, on
// its own system prompt, asking what to call this run's journal. An index-based
// script hands the namer's answer to the run and the run's answer to nobody.
//
// The sentence is EMITTED as a stream delta as well as returned, because that
// is the half [standingRunner.Run] reads (standing_nothing_test.go does the
// same).
type dividingModel struct {
	mu       sync.Mutex
	turns    int
	evidence string
	parts    int
	// opening is what it says on the turn it divides in, and folding on every
	// turn after — which only happens if the run waited for the parts.
	opening, folding string
	// heard keeps the answers the belt gave it, so a test can read the words the
	// worker was actually told.
	heard []string
}

func (m *dividingModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if len(messages) == 0 || len(messages[0].Content) == 0 || messages[0].Content[0].Text != "SYSTEM" {
		// Not the firing's own turn: an auxiliary call on a system prompt of its
		// own. It is answered and never counted.
		return textResponse("the nightly tidy"), nil
	}
	m.mu.Lock()
	turn := m.turns
	m.turns++
	for _, message := range messages {
		if message.Role == "assistant" || message.Role == "system" {
			continue
		}
		for _, part := range message.Content {
			m.heard = append(m.heard, part.Text)
		}
	}
	m.mu.Unlock()
	if turn == 0 {
		return toolResponse("d1", "divide_work", string(divideArgs(m.evidence, m.parts))), nil
	}
	said := m.folding
	if turn == 1 {
		said = m.opening
	}
	provider.Emit(ctx, provider.StreamDelta, said)
	return textResponse(said), nil
}

// wasTold reports whether the worker was ever handed anything — a tool's
// answer, a part's landing note — containing this phrase, and answers the whole
// line it was in.
func (m *dividingModel) wasTold(phrase string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, line := range m.heard {
		if strings.Contains(line, phrase) {
			return line
		}
	}
	return ""
}

// ── the road is armed, and only where the work's own words say it is wide ────

func TestAWideFiringsWorkerCarriesTheDivisionVerb(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if !enumeratesWidth(wideBrief) {
		t.Fatal("the wide brief arms nothing, so this test cannot show that its words armed it")
	}

	var worker *Agent
	runner := watchedFiring(t, root, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("looked at all of them"), nil
		},
	}}, func(_ Config, agent *Agent) { worker = agent })

	if _, err := runner.Run(context.Background(), wideNightly(workspace), filepath.Join(root, "runs", "0001"), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if worker == nil {
		t.Fatal("the firing never built a session")
	}
	if !worker.mayDivide() {
		t.Fatal("a firing whose brief names eleven files may not divide: the unattended road is not armed")
	}
	if !beltHas(worker, "divide_work") {
		t.Fatal("the firing's own session has no divide_work: the decision never reached its hands")
	}
	// AND THE PROMPT AGREES WITH THE BELT, which is the one law that keeps a
	// worker from being told about a verb it does not have (prompt.go).
	if !strings.Contains(renderSystem(worker.config), "divide_work") {
		t.Fatal("the firing carries the verb and was never told how to use it")
	}
}

// THE OTHER SIDE OF THE SAME LAW, and it is what keeps the road free: a firing
// whose brief enumerates nothing is byte-identically the run it was before any
// of this existed — no graph, no verb, nothing in its prompt.
func TestANarrowFiringIsTheRunItAlwaysWas(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	narrow := nightly(workspace)
	if enumeratesWidth(narrow.Words, narrow.Does.Brief, narrow.Does.Acceptance) {
		t.Fatal("the ordinary nightly job arms itself, so it cannot show that narrow work stays free")
	}

	var (
		worker  *Agent
		posture Config
	)
	runner := watchedFiring(t, root, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("nothing to fix"), nil },
	}}, func(cfg Config, agent *Agent) { posture, worker = cfg, agent })

	if _, err := runner.Run(context.Background(), narrow, filepath.Join(root, "runs", "0001"), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if posture.tasker != nil {
		t.Fatal("a narrow firing was given a graph it will never put anything in")
	}
	if worker.mayDivide() || beltHas(worker, "divide_work") {
		t.Fatal("a narrow firing carries divide_work, so its schema and its prompt are not what they were")
	}
	if strings.Contains(renderSystem(worker.config), "divide_work") {
		t.Fatal("a narrow firing was told about a verb it does not have")
	}
}

// AND THE ROAD ITSELF STILL SWITCHES OFF. `AFORGE_SWARM=0` reaches a firing the
// way it reaches everything else — through the posture the door builds — and
// with it off the widest brief in the world is one worker.
func TestAFiringNeverDividesWithTheRoadTurnedOff(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	var worker *Agent
	runner := watchedFiring(t, root, &scriptedCompleter{}, func(_ Config, agent *Agent) { worker = agent })
	runner.parent.Divide = false

	if _, err := runner.Run(context.Background(), wideNightly(workspace), filepath.Join(root, "runs", "0001"), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if worker.mayDivide() || beltHas(worker, "divide_work") {
		t.Fatal("the division road is off and a firing took it anyway")
	}
}

// ── one firing, divided, end to end ─────────────────────────────────────────

// WHAT A DIVISION UNDER A FIRING HAS TO DO, and all three are one run here: the
// parts are real nodes on the firing's own graph and show in its live work; the
// firing does NOT return while they are still going, but folds their reports and
// says one thing at the end; and every cent they spent is in the figure the pass
// writes down for this run.
func TestADivisionUnderAFiringIsWaitedForAndBilledToTheRun(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()

	const partCost = 0.25
	spender := spentChild(t, partCost)

	var (
		mu      sync.Mutex
		trees   [][]WorkNode
		started int
	)
	completer := &dividingModel{
		evidence: wideEvidence, parts: 2,
		opening: "split it into two parts",
		folding: "both parts are home and folded together",
	}

	runner := watchedFiring(t, root, completer, func(cfg Config, agent *Agent) {
		// The part's own run is scripted, exactly as every other test of the
		// frontier scripts it: a test may not have a worktree or a provider, and
		// everything else on this road is the product's.
		graph := cfg.tasker
		graph.mu.Lock()
		graph.run = func(node *TaskNode) {
			// A PART TAKES LONGER THAN THE TURN THAT NAMED IT, which is the
			// ordinary shape of this and the one a run that did not wait would
			// walk straight past: the dividing turn is finished and the firing
			// is sitting on the reports.
			time.Sleep(40 * time.Millisecond)
			mu.Lock()
			started++
			trees = append(trees, agent.WorkingNow())
			mu.Unlock()
			agent.foldTaskUsage(node, spender)
			node.finish("this part is done", nil, "", mergeInPlace)
			node.graph.complete(node, TaskDone)
		}
		graph.mu.Unlock()
	})

	outcome, err := runner.Run(context.Background(), wideNightly(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	ran, seen := started, trees
	mu.Unlock()
	if ran != 2 {
		t.Fatalf("%d parts were started under the firing, want 2", ran)
	}

	// THE FIRING WAITED FOR ITS PARTS AND READ WHAT THEY SAID. The turn that
	// divided was over before either of them landed, so a run that walked away
	// when its own turn ended would have come back on `split it into two parts`
	// with nobody's report in front of it and neither part's bill in its figure.
	if line := completer.wasTold("this part is done"); line == "" {
		t.Fatal("no part's report ever reached the firing: the run did not wait for its own parts")
	}
	if outcome.Text != "both parts are home and folded together" {
		t.Fatalf("the firing came to %q, want what it said after its parts landed", outcome.Text)
	}
	if outcome.Kind != "landed" {
		t.Fatalf("a firing that divided and folded came to %q", outcome.Kind)
	}

	// THE MONEY IS IN THE RUN'S OWN FIGURE, which is what the pass writes into
	// the ledger and what the day's allowance is spent against. The firing's own
	// turns are priced at nothing here, so the whole of this is its parts.
	if outcome.USD < 2*partCost {
		t.Fatalf("the run was billed $%.2f, want at least the $%.2f its two parts spent", outcome.USD, 2*partCost)
	}

	// AND THE PARTS WERE VISIBLE WHILE THEY RAN, under the firing's own work as
	// its children — the same two depths a task and its parts are drawn at.
	if len(seen) == 0 {
		t.Fatal("nothing was watching while the parts ran")
	}
	tree := seen[len(seen)-1]
	if len(tree) != 1 {
		t.Fatalf("the firing's live work has %d roots, want the one piece of work it is", len(tree))
	}
	if len(tree[0].Children) == 0 {
		t.Fatalf("the parts are not drawn under the firing they were born from: %+v", tree[0])
	}
}

// A DIVISION IS STILL REFUSED WHERE IT BUYS NOTHING, and the refusal is the
// product's own — the same evidence floor an attended worker meets, reached
// through a firing's own belt.
func TestANarrowLookInsideAWideFiringIsStillRefused(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	completer := &dividingModel{
		evidence: narrowEvidence, parts: 2,
		opening: "carried on as one worker",
		folding: "carried on as one worker",
	}
	born := 0
	runner := watchedFiring(t, root, completer, func(cfg Config, _ *Agent) {
		cfg.tasker.mu.Lock()
		cfg.tasker.run = func(node *TaskNode) {
			born++
			node.graph.complete(node, TaskFailed)
		}
		cfg.tasker.mu.Unlock()
	})

	outcome, err := runner.Run(context.Background(), wideNightly(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if born != 0 {
		t.Fatalf("%d parts were born from evidence the floor refuses", born)
	}
	if answer := completer.wasTold("not split"); answer == "" {
		t.Fatal("the firing was not turned back at the evidence floor")
	} else if !strings.Contains(answer, "Carry on with the work in your own hands") {
		t.Fatalf("the refusal did not tell the firing what to do instead: %q", answer)
	}
	if outcome.Text != "carried on as one worker" {
		t.Fatalf("a refused division changed what the firing came to: %q", outcome.Text)
	}
}

// ── the machine, and the person's own cap ───────────────────────────────────

// AN UNATTENDED DIVISION RESPECTS THE MACHINE THE WAY AN ATTENDED ONE DOES, and
// it does it by carrying the same two readings rather than by a rule of its own:
// the firing's graph is built with the person's `task.parallel` cap and the
// admission governor made from `task.max_load` and `task.min_free_mb`. From
// there the frontier holds the parts on a loaded box and lifts them by itself,
// and [divideWork] — the same function an attended worker calls — is what turns
// a full lane into a refusal and a busy machine into a wait (task_divide.go).
//
// AND THE FIRING'S OWN WORK HOLDS A LANE. A machine capped at one task at a time
// has no second pair of hands to give parts to, and the free-hand count says so
// only if the work already under way is counted as under way.
func TestAFiringsDivisionCarriesThePersonsOwnCeilings(t *testing.T) {
	base := Config{
		Workspace:     t.TempDir(),
		Model:         "test/model",
		SessionFile:   filepath.Join(t.TempDir(), placeTranscript),
		Divide:        true,
		TaskParallel:  1,
		TaskMaxLoad:   1.5,
		TaskMinFreeMB: 512,
	}
	item := wideNightly(base.Workspace)

	armed, graph, root := standingWideWork(base, item, item.Does.Brief)
	if graph == nil || root == nil {
		t.Fatal("a wide firing was given no graph to hand parts out on")
	}
	if armed.tasker != graph || armed.taskID != root.id {
		t.Fatal("the session was not pointed at the work it is")
	}
	if !root.dividing() {
		t.Fatal("the firing's own work was not armed")
	}
	if graph.limit != base.TaskParallel {
		t.Fatalf("the firing runs %d at once, want the person's own cap of %d", graph.limit, base.TaskParallel)
	}
	if graph.governor == nil {
		t.Fatal("a firing's parts are admitted with no reading of the machine at all")
	}
	if free := graph.freeHands(); free != 0 {
		t.Fatalf("a firing capped at one task has %d free hands, want none — its own work is the one", free)
	}

	// AND NOTHING OF THIS EXISTS FOR NARROW WORK. The road off answers the same
	// way, which is what makes `AFORGE_SWARM=0` the whole of the way out.
	if _, none, _ := standingWideWork(base, nightly(base.Workspace), "look at last night's failures"); none != nil {
		t.Fatal("a narrow firing was given a graph")
	}
	off := base
	off.Divide = false
	if _, none, _ := standingWideWork(off, item, item.Does.Brief); none != nil {
		t.Fatal("the road is off and a firing was given a graph anyway")
	}
}
