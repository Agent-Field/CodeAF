package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// countingPlanner answers every pass and records which ones were reached, so a
// test can assert what a build did NOT buy.
type countingPlanner struct {
	mutex     sync.Mutex
	stages    string
	sizeReply string
	chain     string
	passes    []string
}

func (c *countingPlanner) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system string
	for _, message := range messages {
		if message.Role == "system" {
			system = textOf(message)
		}
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	switch system {
	case spinePrompt:
		c.passes = append(c.passes, "spine")
		return textResponse(c.stages), nil
	case groundPrompt:
		c.passes = append(c.passes, "ground")
		return textResponse(`{"settled":[],"open":[]}`), nil
	case fanoutPrompt:
		c.passes = append(c.passes, "fanout")
		return textResponse(`{"parts":[{"title":"Part","summary":"Do the part."}]}`), nil
	case bindPrompt:
		c.passes = append(c.passes, "bind")
		return textResponse(`{"bindings":[],"duplicates":[]}`), nil
	case sizePromptWith(Anchors()):
		c.passes = append(c.passes, "size")
		if c.sizeReply != "" {
			return textResponse(c.sizeReply), nil
		}
		return textResponse(`{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]}]}`), nil
	case auditPrompt:
		c.passes = append(c.passes, "audit")
		return textResponse(`{"checks":[]}`), nil
	case sequencePrompt:
		c.passes = append(c.passes, "stages")
		return textResponse(c.chain), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
}

// made counts one pass, which is how a test says "exactly one sizing call" as
// opposed to "sizing happened".
func (c *countingPlanner) made(pass string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	seen := 0
	for _, reached := range c.passes {
		if reached == pass {
			seen++
		}
	}
	return seen
}

func (c *countingPlanner) reached(pass string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, seen := range c.passes {
		if seen == pass {
			return true
		}
	}
	return false
}

const ungatedRemainder = "Finish work a previous agent started. Run pytest and show the output."

// The extension replan used to buy a whole project pipeline to plan one
// remainder node. Measured on two real extensions: 13,828 and 23,964 prompt
// tokens across seven passes, five to eight times the entire structuring cost
// of the jobs they were repairing, for graphs of one and three nodes.
//
// The judgement is the model's and it is one it already makes: the spine's own
// prompt tells it that exactly one stage is the right answer when the goal has
// no real gate, and calls that common rather than a failure. Undivided takes
// that answer at its word and stops.
func TestAnUngatedRemainderStopsAtOneWorker(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[{"title":"Run pytest","summary":"Run the suite and show what it printed."}]}`}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, Briefs: true, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("an ungated remainder planned %d nodes, want 1", len(graph.Nodes))
	}
	if brief := graph.Nodes[0].Brief; !strings.Contains(brief, "Run pytest and show the output") {
		t.Fatalf("the one worker was not given the remainder: %q", brief)
	}
	for _, unbought := range []string{"fanout", "bind", "audit", "stages"} {
		if client.reached(unbought) {
			t.Fatalf("an ungated remainder still bought the %s pass: %v", unbought, client.passes)
		}
	}
	// One call is what the shortcut now pays, and it is the only thing standing
	// between a spine that was asked about gates and a worker that will be
	// handed the whole remainder.
	if made := client.made("size"); made != 1 {
		t.Fatalf("the shortcut made %d sizing calls, want exactly one: %v", made, client.passes)
	}
}

// THE SHORTCUT IS NOT A SIZE VERDICT. A remainder the spine finds nothing gated
// in, and the ruler puts past one worker's reach, is the leaf that exhausted
// itself being handed back to itself. It falls through to the pipeline, where
// the division of a sequence lives.
func TestAnOversizedRemainderIsNotHandedToOneWorker(t *testing.T) {
	replies := func() *countingPlanner {
		return &countingPlanner{
			stages: `{"stages":[{"title":"Finish","summary":"Finish the remainder."}]}`,
			// The whole is past one worker's reach; the stages it is made of
			// are not.
			sizeReply: `{"sizes":[{"node":1,"size":"oversized","split_into":["one","two"]},{"node":2,"size":"atomic","split_into":[]},{"node":3,"size":"atomic","split_into":[]}]}`,
			chain: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},` +
				`{"title":"Change","summary":"Make the change.","needs":[1]},` +
				`{"title":"Prove","summary":"Show that it holds.","needs":[2]}]}`,
		}
	}

	client := replies()
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Three ordered stages under the node they were drawn for, and the
	// one-sitting collapse leaves them alone: the links sit at depth 1, and the
	// whole they came from is the node the ruler put past one worker's reach.
	links := 0
	for _, node := range graph.Nodes {
		if node.Kind == KindWork && node.Depth == 1 {
			links++
		}
	}
	if links != 3 {
		t.Fatalf("an oversized remainder kept %d ordered stages of 3, in %d nodes: %v",
			links, len(graph.Nodes), client.passes)
	}
	// Binding and audit have nothing to say about a single stage with one node
	// in it; what the fall-through is for is the fan-out, the ruler, and the
	// stage question underneath them.
	for _, required := range []string{"fanout", "size", "stages"} {
		if !client.reached(required) {
			t.Fatalf("the fall-through skipped the %s pass: %v", required, client.passes)
		}
	}
	// A remainder is allowed one level of division, and this is what depth zero
	// costs it: the same remainder, the same rulings, and nowhere for the
	// oversized node to go.
	shallow := replies()
	flat, err := Build(context.Background(), shallow, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 0, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if shallow.reached("stages") || len(flat.Nodes) != 1 {
		t.Fatalf("depth zero divided the remainder after all (%d nodes): %v", len(flat.Nodes), shallow.passes)
	}
}

// A REMAINDER THE RULER PUTS PAST ONE WORKER STILL GETS THE WHOLE PIPELINE.
// That is the whole of what "gated" now means here, and it is the ruler's
// reading of the folded whole rather than the spine's count of its stages: this
// test used to pin the opposite — that two spine stages went to the pipeline
// without the ruler being asked at all — which is exactly the behaviour the
// stage-count law removes.
func TestARemainderPastOneWorkersReachStillGetsTheWholePipeline(t *testing.T) {
	client := &countingPlanner{
		stages: `{"stages":[
		{"title":"Gather","summary":"Collect the numbers."},
		{"title":"Write","summary":"Write them up."}]}`,
		// The folded whole is past one worker; the parts it fans out into are
		// not. The reach probe reads node 1 and so does the pipeline's own
		// sizing, which is what keeps the two stages standing.
		sizeReply: `{"sizes":[{"node":1,"size":"oversized","split_into":["gather","write"]},{"node":2,"size":"borderline","split_into":[]}]}`,
	}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, Briefs: false, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) < 2 {
		t.Fatalf("a remainder past one worker's reach collapsed to %d nodes: %v", len(graph.Nodes), client.passes)
	}
	for _, required := range []string{"fanout", "bind", "size"} {
		if !client.reached(required) {
			t.Fatalf("a remainder past one worker's reach skipped the %s pass: %v", required, client.passes)
		}
	}
	// And the ruler was asked BEFORE any of it, which is the seam the stage
	// count used to jump: the first sizing call of the build is the probe.
	if made := client.made("size"); made < 2 {
		t.Fatalf("the pipeline ran without a reach probe in front of it: %v", client.passes)
	}
}

// THE SPINE'S STAGE COUNT IS NOT A DIVIDER FOR A REMAINDER. Measured on the
// canary acceptance cell: a second remainder carrying "2 unexercised
// behaviours" came back from the spine as three stages — "Check unit tests",
// "Check integration test", "Verify no traceback" — which is one worker's list
// of steps and not three jobs. Requiring exactly one stage meant the ruler was
// never asked, and the round bought fan-out, bind, sizing, audit and seven
// contracts.
func TestTheSpinesStageCountDoesNotDivideARemainder(t *testing.T) {
	const three = `{"stages":[
		{"title":"Check unit tests","summary":"Run the unit suite."},
		{"title":"Check integration test","summary":"Run the integration suite."},
		{"title":"Verify no traceback","summary":"Confirm nothing raised."}]}`

	client := &countingPlanner{stages: three}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Briefs: true, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("a three-stage remainder within reach planned %d nodes, want 1: %v", len(graph.Nodes), client.passes)
	}
	for _, unbought := range []string{"fanout", "bind", "audit", "stages"} {
		if client.reached(unbought) {
			t.Fatalf("a three-stage remainder within reach still bought the %s pass: %v", unbought, client.passes)
		}
	}
	// One call, and it is the ruler's: the stages are folded and the reach
	// question is asked once about the whole.
	if made := client.made("size"); made != 1 {
		t.Fatalf("the shortcut made %d sizing calls, want exactly one: %v", made, client.passes)
	}
	// The one worker is handed the stages as its own list, in the same shape a
	// one-stage answer has.
	only := graph.Nodes[0]
	for _, step := range []string{"Check unit tests", "Check integration test", "Verify no traceback"} {
		if !strings.Contains(only.Title+" "+only.Summary, step) {
			t.Fatalf("the folded node lost the stage %q: %q / %q", step, only.Title, only.Summary)
		}
	}

	// And the same three stages past one worker's reach still divide, because
	// size is the judgment a remainder is divided on.
	oversized := &countingPlanner{
		stages:    three,
		sizeReply: `{"sizes":[{"node":1,"size":"oversized","split_into":["one","two"]},{"node":2,"size":"atomic","split_into":[]},{"node":3,"size":"atomic","split_into":[]}]}`,
		chain: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},` +
			`{"title":"Change","summary":"Make the change.","needs":[1]},` +
			`{"title":"Prove","summary":"Show that it holds.","needs":[2]}]}`,
	}
	divided, err := Build(context.Background(), oversized, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(divided.Nodes) < 2 {
		t.Fatalf("a three-stage remainder past one worker's reach kept %d nodes: %v",
			len(divided.Nodes), oversized.passes)
	}
	if !oversized.reached("fanout") {
		t.Fatalf("the fall-through skipped the fan-out: %v", oversized.passes)
	}
}

// Without the option nothing moves: a fresh one-stage project still fans out,
// because a project with one stage still has parallel parts inside it and
// finding them is the whole point of planning.
func TestAOneStageProjectStillFansOut(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[{"title":"Do it","summary":"Do the whole thing."}]}`}
	if _, err := Build(context.Background(), client, "review the change end to end", Options{
		SpineSamples: 1, NodeBudget: 12, Ensemble: EnsembleNever,
	}); err != nil {
		t.Fatal(err)
	}
	if !client.reached("fanout") {
		t.Fatalf("an ordinary one-stage build stopped at the spine: %v", client.passes)
	}
}

// And a fresh build with three stages is untouched by the fold: the shortcut is
// keyed on Undivided and nothing else, so the stages stay three stages and the
// pipeline reads them exactly as it always did.
func TestAFreshThreeStageProjectIsUnchanged(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[
		{"title":"Check unit tests","summary":"Run the unit suite."},
		{"title":"Check integration test","summary":"Run the integration suite."},
		{"title":"Verify no traceback","summary":"Confirm nothing raised."}]}`}
	graph, err := Build(context.Background(), client, "run every suite and report", Options{
		SpineSamples: 1, NodeBudget: 12, Ensemble: EnsembleNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The shortcut is not offered: a fresh build goes through the passes that
	// find the parts inside a stage, whatever the spine's count was. (What the
	// graph then collapses to is the pipeline's own evidence question — see
	// collapseAtomicChain — and is unchanged by anything here.)
	if graph == nil {
		t.Fatal("a fresh three-stage build produced no graph")
	}
	for _, required := range []string{"fanout", "bind", "size"} {
		if !client.reached(required) {
			t.Fatalf("a fresh three-stage build skipped the %s pass: %v", required, client.passes)
		}
	}
}

// A REMAINDER THAT LISTS EIGHT FAILING TESTS IS ONE WORKER'S LIST, NOT EIGHT
// JOBS. The shortcut used to read the stage's own words before it spent its
// probe and fall through wherever they named several pieces — the right reading
// for a fresh plan, and the wrong one for the only build that reaches the
// shortcut. Measured on the canary: a repair round drew its remainder as six
// leaves and then as eight, ran fourteen parallel repairs to the wall, and the
// do door's spend doubled with quality flat.
func TestARemainderThatListsWhatIsLeftIsStillOneWorker(t *testing.T) {
	const listed = `{"stages":[{"title":"Fix the failing tests","summary":` +
		`"T1: fix test_dates.; T2: fix test_quantities.; T3: fix test_prefixes.; T4: fix test_totals."}]}`

	client := &countingPlanner{stages: listed}
	graph, err := Build(context.Background(), client, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Briefs: true, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("a remainder listing four repairs planned %d nodes, want 1: %v", len(graph.Nodes), client.passes)
	}
	for _, unbought := range []string{"fanout", "bind", "audit", "stages"} {
		if client.reached(unbought) {
			t.Fatalf("a listed remainder still bought the %s pass: %v", unbought, client.passes)
		}
	}
	// The ruler is asked, which is the whole of the change: the words no longer
	// answer in its place, so the one call the shortcut pays is paid.
	if made := client.made("size"); made != 1 {
		t.Fatalf("the shortcut made %d sizing calls, want exactly one: %v", made, client.passes)
	}

	// And the same list past one worker's reach still divides, because size is
	// the judgment a remainder is divided on.
	oversized := &countingPlanner{
		stages:    listed,
		sizeReply: `{"sizes":[{"node":1,"size":"oversized","split_into":["one","two"]},{"node":2,"size":"atomic","split_into":[]},{"node":3,"size":"atomic","split_into":[]}]}`,
		chain: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},` +
			`{"title":"Change","summary":"Make the change.","needs":[1]},` +
			`{"title":"Prove","summary":"Show that it holds.","needs":[2]}]}`,
	}
	divided, err := Build(context.Background(), oversized, ungatedRemainder, Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(divided.Nodes) < 2 || !oversized.reached("stages") {
		t.Fatalf("a remainder past one worker's reach kept %d nodes: %v", len(divided.Nodes), oversized.passes)
	}
}

// The admission itself is not gone, and a fresh plan is where it lives: a
// one-stage project whose own words name several pieces still fans out, and the
// pieces are found by the passes that exist to find them.
func TestAFreshPlanThatNamesSeveralPiecesStillDivides(t *testing.T) {
	client := &countingPlanner{stages: `{"stages":[{"title":"North, South, East","summary":` +
		`"North: Rewrite North dates.; South: Sort South by quantity.; East: Prefix low items."}]}`}
	if _, err := Build(context.Background(), client, "rework the three blocks", Options{
		SpineSamples: 1, NodeBudget: 12, MaxDepth: 1, Ensemble: EnsembleNever,
	}); err != nil {
		t.Fatal(err)
	}
	if !client.reached("fanout") {
		t.Fatalf("a fresh plan naming three lanes stopped at the spine: %v", client.passes)
	}
}
