package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The incident shape: two asks in one breath that have nothing to do with each
// other. It used to leave the tasker by a second door — laid flat with no
// planner, under a node the machinery named "Deliver together" — and this pins
// that there is one road now and what it arrives with.
//
// Four things are checked because four things were promised when the flat
// layout was retired. The planner is actually consulted. The person's own
// words for each request reach it unaltered, so a leaf's brief is one
// paraphrase from the ask rather than two. The shape that comes back is the
// one the flat layout hardcoded — every request its own leaf, one gathering
// node behind all of them — but chosen. And no node anywhere wears a name out
// of the machinery.
func TestTwoUnrelatedRequestsBecomeOnePlannedJobWithNoMachineryTitle(t *testing.T) {
	const haiku = "Write me a haiku about the first cold morning of autumn."
	const research = "Find out which three vendors ship the parser we are on and what each charges."

	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model", MaxDepth: 1, NodeBudget: 8}
	planner := &partsPlanClient{model: "worker/model", stages: []string{"Answer"}, parts: map[int][]scriptPart{
		1: {{title: "Haiku", summary: haiku}, {title: "Vendors", summary: research}},
	}}
	client := adoptLiveClient(settings, planner.model, planner)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
		Goal:  "Two things, unrelated: a haiku, and what the parser vendors charge.",
		Scale: head.ScaleProject,
		Parts: []string{haiku, research},
	})
	if err != nil {
		t.Fatal(err)
	}

	// One road. The old route's whole claim was that it could skip this call.
	if calls := planner.seen(provider.ClassPlanSpine); calls == 0 {
		t.Fatal("the planner was never consulted — a second route out of the gate still exists")
	}

	// The requests reach the planner as the person wrote them. Checked on the
	// spine and the fan-out because they are the two passes that decide the
	// shape, and checked as substrings of the prompt rather than as a rendering
	// so the test pins the words rather than the layout around them.
	for _, class := range []provider.CallClass{provider.ClassPlanSpine, provider.ClassPlanFanOut} {
		prompt := strings.Join(planner.promptsFor(class), "\n")
		for _, request := range []string{haiku, research} {
			if !strings.Contains(prompt, request) {
				t.Fatalf("the %s pass never saw %q — the request was laundered on the way in", class, request)
			}
		}
	}

	// The shape: two leaves and the gathering node behind both of them.
	var root store.NodeSpec
	leaves := make([]store.NodeSpec, 0, 2)
	for _, spec := range subtree.Nodes {
		if spec.Parent == "" {
			if root.ID != "" {
				t.Fatalf("the job has two roots: %q and %q", root.ID, spec.ID)
			}
			root = spec
			continue
		}
		leaves = append(leaves, spec)
	}
	if root.ID == "" {
		t.Fatal("the job has no root")
	}
	if len(leaves) != 2 {
		t.Fatalf("the plan carries %d leaves under the root, want the two requests: %+v", len(leaves), leaves)
	}
	waits := map[string]bool{}
	for _, need := range root.Needs {
		waits[need.NodeID] = true
	}
	for _, leaf := range leaves {
		if !waits[leaf.ID] {
			t.Fatalf("the gathering node does not wait for %q — it can run beside its own input", leaf.ID)
		}
		if len(leaf.Needs) != 0 {
			t.Fatalf("leaf %q waits for %+v — two unrelated requests were chained", leaf.ID, leaf.Needs)
		}
	}

	// And each request is what exactly one leaf is written for. The pass that
	// writes a leaf's instruction opens on the node it is about — the rest of
	// that message is the inputs arriving, which is why only the opening line
	// is read here — so this is the last place the words could still be swapped
	// for somebody's summary of them.
	for _, request := range []string{haiku, research} {
		aimed := 0
		for _, target := range planner.targetsFor(provider.ClassPlanBrief) {
			if strings.Contains(strings.SplitN(target, "\n", 2)[0], request) {
				aimed++
			}
		}
		if aimed != 1 {
			t.Fatalf("%d leaves are written for %q, want exactly one", aimed, request)
		}
	}

	// The name. "Deliver together" was the machinery talking, and it survived
	// to the rail because it was neither empty nor the planner's own
	// placeholder, so the pass that names a job from the ask stepped over it.
	for _, spec := range subtree.Nodes {
		for _, field := range []string{spec.Title, spec.Brief, spec.Group} {
			if strings.Contains(strings.ToLower(field), "deliver together") {
				t.Fatalf("node %q still wears the retired route's name: %q", spec.ID, field)
			}
		}
	}
	// And the root is left in the state the naming pass acts on, rather than
	// pre-named out of the machinery's vocabulary. See titleSubtree.
	if !strings.EqualFold(strings.TrimSpace(root.Title), "synthesis") {
		t.Fatalf("the root is titled %q, which the naming pass will not replace with a name from the ask", root.Title)
	}
}

// The other half of the incident, and the one the flat layout could not
// express: one of the requests is written over what the others produce.
//
// The old route admitted such a request with no edges at all, which is not a
// weak claim about order but a positive claim that there is none — it was
// claimable on the first tick, ran against nothing, and invented the material
// it existed to read. Here the planner puts it in a later stage, the plan
// adapter turns that into real inputs, and the store refuses the claim while
// any of them is live. The refusal is the store's own floor (see the store's
// lifecycle test); what this pins is that a planner-built subtree arrives at
// that floor already wired to stand on it.
func TestAPlannedGatheringRequestCannotBeClaimedWhileAnInputRuns(t *testing.T) {
	requests := []string{
		"How does France measure road distance?",
		"How does the UK measure road distance?",
		"How does Japan measure road distance?",
		"Write the three answers up as one comparison.",
	}

	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model", MaxDepth: 1, NodeBudget: 12}
	planner := &partsPlanClient{model: "worker/model", stages: []string{"Look up", "Write up"}, parts: map[int][]scriptPart{
		1: {
			{title: "France", summary: requests[0]},
			{title: "UK", summary: requests[1]},
			{title: "Japan", summary: requests[2]},
		},
		2: {{title: "Comparison", summary: requests[3]}},
	}}
	client := adoptLiveClient(settings, planner.model, planner)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
		Goal:  "Compare how three countries measure road distance.",
		Scale: head.ScaleProject,
		Parts: requests,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, subtree, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "compare three countries"}); err != nil {
		t.Fatal(err)
	}

	// The gathering request is the one that waits for the lookups; the lookups
	// are the ones that wait for nothing. Found by shape rather than by id,
	// because which plan node became which store node is the adapter's business.
	var gathering string
	lookups := make([]string, 0, 3)
	for _, spec := range subtree.Nodes {
		if spec.Parent == "" {
			continue
		}
		if len(spec.Needs) == 0 {
			lookups = append(lookups, spec.ID)
			continue
		}
		if gathering != "" {
			t.Fatalf("two leaves wait for inputs: %q and %q", gathering, spec.ID)
		}
		gathering = spec.ID
	}
	if gathering == "" {
		t.Fatal("the write-up was admitted with no inputs — it is claimable beside the answers it reads")
	}
	if len(lookups) != 3 {
		t.Fatalf("the plan carries %d independent lookups, want 3", len(lookups))
	}

	// Land them one at a time. At no point before the last is terminal may the
	// write-up be claimed — including while one of its inputs is running, which
	// is the state that shipped a comparison with a country invented.
	for index, id := range lookups {
		if _, won, err := graph.Claim(gathering, "runner"); err != nil || won {
			t.Fatalf("the write-up was claimable with %d of 3 answers written: won=%v err=%v", index, won, err)
		}
		claim, won, err := graph.Claim(id, "runner")
		if err != nil || !won {
			t.Fatalf("claim %s: won=%v err=%v", id, won, err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		if _, won, err := graph.Claim(gathering, "runner"); err != nil || won {
			t.Fatalf("the write-up was claimable while %s was running: won=%v err=%v", id, won, err)
		}
		if err := graph.Complete(claim, "answered "+id); err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
	}
	if _, won, err := graph.Claim(gathering, "runner"); err != nil || !won {
		t.Fatalf("the write-up is unclaimable with every answer in hand: won=%v err=%v", won, err)
	}
}

// The common path, unmoved. An ask that is one answer sends the planner the
// same prompt bytes it always sent — the block that carries the person's own
// division writes nothing when there is no division — and plans into the same
// shape.
func TestAnAskWithNoSeparableRequestsPlansExactlyAsBefore(t *testing.T) {
	const goal = "review the pull request and deliver REVIEW.md"

	shapes := map[string][]string{"no parts": nil, "one part": {goal}}
	prompts := map[string]string{}
	for name, parts := range shapes {
		graph := openCacheStore(t)
		settings := config.Config{Model: "worker/model", MaxDepth: 1, NodeBudget: 6}
		planner := &partsPlanClient{model: "worker/model", stages: []string{"Review"}, parts: map[int][]scriptPart{
			1: {{title: "Review", summary: "Write REVIEW.md."}},
		}}
		client := adoptLiveClient(settings, planner.model, planner)
		plans := &jobPlans{graphs: map[string]plannedJob{}}

		subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
			Goal: goal, Scale: head.ScaleProject, Parts: parts,
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(subtree.Nodes) != 1 {
			t.Fatalf("%s: one request planned into %d nodes", name, len(subtree.Nodes))
		}
		spine := planner.promptsFor(provider.ClassPlanSpine)
		if len(spine) == 0 {
			t.Fatalf("%s: no spine call", name)
		}
		prompts[name] = spine[0]
	}
	if prompts["no parts"] != prompts["one part"] {
		t.Fatalf("a single request changed the planner's prompt\n without: %q\n    with: %q",
			prompts["no parts"], prompts["one part"])
	}
	if prompts["no parts"] != "Goal:\n"+goal {
		t.Fatalf("the ordinary ask no longer sends the bytes it always sent: %q", prompts["no parts"])
	}
}

// scriptPart is one fan-out reply row.
type scriptPart struct {
	title   string
	summary string
}

// partsPlanClient is a planner with a script: a fixed spine, a fixed set of
// parts per stage, and a memory of every prompt it was shown. The per-stage
// script is what the shared planScriptClient cannot do, and it is exactly what
// a job whose last request reads the others needs.
type partsPlanClient struct {
	mutex   sync.Mutex
	model   string
	stages  []string
	parts   map[int][]scriptPart
	calls   map[provider.CallClass]int
	prompts map[provider.CallClass][]string
	// targets is the last user message of each call — the line that says which
	// node this call is about, as distinct from the shared block above it that
	// says what the whole ask is.
	targets map[provider.CallClass][]string
}

func (c *partsPlanClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	class := provider.CallClassFrom(ctx)
	var user strings.Builder
	target := ""
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		target = ""
		for _, part := range message.Content {
			user.WriteString(part.Text)
			user.WriteString("\n")
			target += part.Text
		}
	}
	prompt := strings.TrimSuffix(user.String(), "\n")

	c.mutex.Lock()
	if c.calls == nil {
		c.calls = map[provider.CallClass]int{}
		c.prompts = map[provider.CallClass][]string{}
		c.targets = map[provider.CallClass][]string{}
	}
	c.calls[class]++
	c.prompts[class] = append(c.prompts[class], prompt)
	c.targets[class] = append(c.targets[class], target)
	c.mutex.Unlock()

	text := ""
	switch class {
	case provider.ClassPlanGround:
		text = `{"settled":[],"open":[]}`
	case provider.ClassPlanSpine:
		rows := make([]string, 0, len(c.stages))
		for _, stage := range c.stages {
			rows = append(rows, fmt.Sprintf(`{"title":%q,"summary":%q}`, stage, stage+" what the ask names."))
		}
		text = `{"stages":[` + strings.Join(rows, ",") + `]}`
	case provider.ClassPlanEnsemble:
		text = `{"mode":"decompose","reason":"separate requests","pass_title":"","pass_summary":"",` +
			`"pass_sources":[],"setup_title":"","setup_summary":"","deliverable":""}`
	case provider.ClassPlanFanOut, provider.ClassPlanExpand:
		rows := make([]string, 0, 4)
		for _, part := range c.parts[c.stageOf(prompt)] {
			rows = append(rows, fmt.Sprintf(`{"title":%q,"summary":%q,"sources":[]}`, part.title, part.summary))
		}
		text = `{"parts":[` + strings.Join(rows, ",") + `]}`
	case provider.ClassPlanBind:
		// Empty on purpose. Bind is written to under-connect, and the edge that
		// keeps a later stage behind an earlier one must not depend on it.
		text = `{"bindings":[],"duplicates":[]}`
	case provider.ClassPlanSize:
		text = `{"sizes":[]}`
	case provider.ClassPlanAudit:
		text = `{"checks":[]}`
	case provider.ClassPlanContract:
		text = `{"contract":"Do the thing the request names, then say what you found."}`
	case provider.ClassPlanBrief:
		text = "Do exactly what your request says and deliver the result in full."
	default:
		return nil, errUnexpectedPlanCall(class, messages)
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
	}}}, nil
}

// stageOf reads which stage a fan-out call is asking about off the one line
// that names it. One reply per stage is the whole point of this client.
func (c *partsPlanClient) stageOf(prompt string) int {
	for stage := len(c.stages); stage >= 1; stage-- {
		if strings.Contains(prompt, fmt.Sprintf("List the simultaneous parts of stage %d,", stage)) {
			return stage
		}
	}
	return 1
}

func (c *partsPlanClient) Model() string { return c.model }

func (c *partsPlanClient) seen(class provider.CallClass) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.calls[class]
}

func (c *partsPlanClient) promptsFor(class provider.CallClass) []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]string(nil), c.prompts[class]...)
}

func (c *partsPlanClient) targetsFor(class provider.CallClass) []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]string(nil), c.targets[class]...)
}

// A timeout mid-split used to deliver the split receipt as the FINAL ANSWER —
// "[splitting the remaining work — 6 pieces queued]" scored against GAIA
// ground truth. A receipt about scheduling is never an answer.
func TestATimeoutMidSplitNeverDeliversTheReceipt(t *testing.T) {
	graph := openCacheStore(t)
	provenance := store.Provenance{SessionID: "gaia", Origin: store.OriginUser, Intent: "the question"}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "q", Brief: "answer the question"},
	}}, provenance); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("q", "w")
	if err != nil || !won {
		t.Fatal(err)
	}
	receipt := "partial work so far [" + resident.OverrunContinuationMessage(6) + "]"
	if err := graph.Complete(claim, receipt); err != nil {
		t.Fatal(err)
	}
	watch := &settlementWatch{graph: graph}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	outcome := watch.compose(nodes)
	if strings.Contains(outcome.Deliverable, "splitting the remaining work") {
		t.Fatalf("the split receipt shipped as the answer:\n%s", outcome.Deliverable)
	}
	if !strings.Contains(outcome.Deliverable, "never finished") {
		t.Fatalf("the timeout partial does not say what happened:\n%s", outcome.Deliverable)
	}
}
