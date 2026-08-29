package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sharedPrefixBytes measures the only thing an implicit prompt cache pays for:
// how far two assemblies of the same prompt agree before the first byte that
// moved. Everything past it is re-billed at full price.
func sharedPrefixBytes(first, second string) int {
	limit := len(first)
	if len(second) < limit {
		limit = len(second)
	}
	for index := 0; index < limit; index++ {
		if first[index] != second[index] {
			return index
		}
	}
	return limit
}

func openCacheStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

// The gate's standing half — settled taste, then the retrieved digest — leads,
// so a second pass over the same node shares everything down to the deliverable
// and a newly settled rule appends instead of shifting the block.
func TestGatePromptKeepsChurnBelowTheSettledBlocks(t *testing.T) {
	graph := openCacheStore(t)
	settle := func(body string) {
		t.Helper()
		candidate, err := graph.RecordTasteCandidate("", "user", body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := graph.PromoteTasteRule(candidate.Seq); err != nil {
			t.Fatal(err)
		}
	}
	settle("keep written comparisons under a page")
	if _, err := graph.RecordFact("", "user", store.FactPreference,
		"the user always wants benchmark evidence named explicitly"); err != nil {
		t.Fatal(err)
	}

	settings := config.Config{Model: "worker/model"}
	node := store.Node{
		ID: "job", Brief: "compare the approaches with evidence",
		Provenance: store.Provenance{Intent: "recommend an approach", SessionID: "s1"},
	}
	gateBody := func(deliverable string) string {
		t.Helper()
		capture := &gateCaptureClient{model: "worker/model"}
		client := adoptLiveClient(settings, capture.model, capture)
		revision.JudgeDeliverable(context.Background(), settings, client, graph, node, deliverable, "", revision.Evidence{}, "worker/model")
		return capture.messages[len(capture.messages)-1].Content[0].Text
	}

	// A repair pass: same node, same taste, a rewritten deliverable. Everything
	// down to the deliverable itself must survive.
	first := gateBody("approach A wins")
	second := gateBody("approach A wins, and here are the numbers")
	marker := strings.Index(first, "Deliverable as produced:\n")
	if marker <= 0 {
		t.Fatalf("no deliverable block in the gate prompt:\n%s", first)
	}
	if shared := sharedPrefixBytes(first, second); shared < marker {
		t.Fatalf("a rewritten deliverable churned the prompt at byte %d, above the deliverable at %d:\n%s",
			shared, marker, first[:marker])
	}
	if taste := strings.Index(first, "Settled taste — hold to these:\n"); taste != 0 {
		t.Fatalf("settled taste does not lead the gate prompt (index %d):\n%s", taste, first)
	}
	digest := strings.Index(first, revision.GateLessonsHeading)
	request := strings.Index(first, "Verbatim request:\n")
	if digest <= 0 || request <= digest || request >= marker {
		t.Fatalf("the standing blocks do not lead the job: digest=%d request=%d deliverable=%d\n%s",
			digest, request, marker, first)
	}

	// A newly settled rule appends to the taste block rather than displacing it.
	settle("name the files a job wrote beside the answer")
	third := gateBody("approach A wins")
	if shared := sharedPrefixBytes(first, third); shared < len("Settled taste — hold to these:\n- keep written comparisons under a page") {
		t.Fatalf("a newly settled rule rewrote the gate prompt from byte %d:\n%s", shared, third)
	}
}

// The narrator speaks the same job many times over. The goal and the updates
// already spoken are the append-only half and lead; what is running and what
// just landed are rewritten every heartbeat and sit below them.
func TestNarratorPromptLeadsWithTheAppendOnlyHalf(t *testing.T) {
	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: "talk/model", response: "still going"}
	client := adoptLiveClient(settings, capture.model, capture)
	narrate := narrateProgress(settings, client, nil)

	if _, err := narrate(context.Background(), resident.Narration{
		Goal:     "compare the weather",
		Finished: []string{"Fetch city A weather"},
		Running:  []string{"Fetch city B weather"},
		Queued:   1,
		Previous: []string{"City A is in."},
	}); err != nil {
		t.Fatal(err)
	}
	body := capture.messages[len(capture.messages)-1].Content[0].Text
	previous := strings.Index(body, "Your earlier updates (do not repeat):")
	finished := strings.Index(body, "Just finished:")
	running := strings.Index(body, "In motion now:")
	if previous <= 0 || finished <= 0 || running <= 0 {
		t.Fatalf("narration blocks missing:\n%s", body)
	}
	if previous > finished || finished > running {
		t.Fatalf("narration blocks are not stable-first:\n%s", body)
	}
}

// The compiler is one call with no lineage, so its affinity key is a constant.
// Two different instructions must ask for the same warm instance.
func TestCompileRidesOneConstantCacheKey(t *testing.T) {
	settings := config.Config{Model: "talk/model"}
	capture := &compileCaptureClient{model: "talk/model"}
	client := adoptLiveClient(settings, capture.model, capture)
	compile := compileIntent(settings, head.NewCompiler(client), client, nil, nil, nil)

	for _, instruction := range []string{"summarise this file", "benchmark the parser"} {
		if _, err := compile(context.Background(), instruction, "Live graph snapshot:\n(nothing)"); err != nil {
			t.Fatal(err)
		}
	}
	keys := capture.snapshot()
	if len(keys) != 2 {
		t.Fatalf("compile calls = %d, want 2", len(keys))
	}
	if keys[0] == "" {
		t.Fatal("the compiler call carries no cache key at all")
	}
	if keys[0] != keys[1] {
		t.Fatalf("two compiles asked for two different instances: %q vs %q", keys[0], keys[1])
	}
	if keys[0] != provider.RunCacheKey("compile", settings.Model) {
		t.Fatalf("compile cache key = %q, want the constant class key", keys[0])
	}
}

// The headless entrypoints — run, plan, revise — pin one affinity key for the
// whole run, and every context they derive from it keeps it.
//
// The key is set once, in Config.Context, and then wrapped twice on the way to
// the loop: ExecContext layers the executor's own reasoning level over the
// planning economy, and signal.NotifyContext makes Ctrl+C land the run rather
// than vanish it. Both are places where a rebuild from context.Background would
// look harmless and silently split the run into one cache lineage per call —
// which is exactly the shape of the defect this pins shut, and the one that had
// the whole headless path writing its prefix cold on every request.
func TestTheHeadlessRunPathPinsOneAffinityKeyThroughout(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	const goal = "review the pull request and deliver REVIEW.md"
	want := provider.RunCacheKey(goal, settings.Model)

	planning := settings.Context(context.Background(), goal)
	if got := provider.CacheKeyFrom(planning); got != want {
		t.Fatalf("the run's planning context carries %q, want %q", got, want)
	}
	executing := settings.ExecContext(planning)
	if got := provider.CacheKeyFrom(executing); got != want {
		t.Fatalf("the executor context dropped the run key: %q", got)
	}
	interruptible, stop := signal.NotifyContext(executing, os.Interrupt)
	defer stop()
	if got := provider.CacheKeyFrom(interruptible); got != want {
		t.Fatalf("the interrupt-aware context dropped the run key: %q", got)
	}
	// One run is one lineage: a different goal is a different key, and the same
	// goal on a later run is the same one, so a repeat reuses the warm prefix
	// rather than paying to write it again.
	if provider.RunCacheKey("something else entirely", settings.Model) == want {
		t.Fatal("two different runs collapsed onto one key")
	}
	if provider.RunCacheKey(goal, settings.Model) != want {
		t.Fatal("the same run asked for two different instances")
	}
}

type compileCaptureClient struct {
	mutex sync.Mutex
	model string
	keys  []string
}

func (c *compileCaptureClient) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.mutex.Lock()
	c.keys = append(c.keys, provider.CacheKeyFrom(ctx))
	c.mutex.Unlock()
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{
		Type: "text",
		Text: `{"goal":"Produce the requested summary.","deliverable":"a summary","budget":"$0.10","assumptions":["Use the current file"]}`,
	}}}}}}, nil
}

func (c *compileCaptureClient) Model() string { return c.model }

func (c *compileCaptureClient) snapshot() []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]string(nil), c.keys...)
}

// Contracts are the widest fan-out in a plan — one call per leaf, launched at
// once. They used to run on the reconciler's bare context, with no affinity key
// at all, so every one of them was load-balanced somewhere new and wrote the
// shared prefix cold. They now ride the same run key the spine did.
func TestPlanContractsRideTheRunCacheKey(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model", MaxDepth: 1, NodeBudget: 6}
	capture := &planScriptClient{model: "worker/model"}
	client := adoptLiveClient(settings, capture.model, capture)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
		Goal:  "review the pull request and deliver REVIEW.md",
		Scale: head.ScaleProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(subtree.Nodes) == 0 {
		t.Fatal("planning produced no subtree")
	}
	keys := capture.keysFor(provider.ClassPlanContract)
	if len(keys) == 0 {
		t.Fatal("no contract call was made, so nothing was pinned")
	}
	spine := capture.keysFor(provider.ClassPlanSpine)
	if len(spine) == 0 || spine[0] == "" {
		t.Fatalf("the spine call carries no cache key: %q", spine)
	}
	for _, key := range keys {
		if key != spine[0] {
			t.Fatalf("contract call key = %q, want the run's key %q", key, spine[0])
		}
	}
}

// planScriptClient answers each planning pass by the class stamped on its
// context, and remembers the affinity key every pass was given.
type planScriptClient struct {
	mutex sync.Mutex
	model string
	keys  map[provider.CallClass][]string
	// parts overrides the fan-out reply for tests that need a plan wide enough
	// to grow the gathering node the harness appends.
	parts    string
	contract string
}

func (c *planScriptClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	class := provider.CallClassFrom(ctx)
	c.mutex.Lock()
	if c.keys == nil {
		c.keys = map[provider.CallClass][]string{}
	}
	c.keys[class] = append(c.keys[class], provider.CacheKeyFrom(ctx))
	c.mutex.Unlock()

	text := ""
	switch class {
	case provider.ClassPlanSpine:
		text = `{"stages":[{"title":"Review","summary":"Review the change."}]}`
	case provider.ClassPlanGround:
		text = `{"settled":[],"open":[]}`
	case provider.ClassPlanEnsemble:
		text = `{"mode":"decompose","reason":"the work splits","pass_title":"","pass_summary":"",` +
			`"pass_sources":[],"setup_title":"","setup_summary":"","deliverable":""}`
	case provider.ClassPlanFanOut, provider.ClassPlanExpand:
		text = `{"parts":[{"title":"Review","summary":"Write REVIEW.md."}]}`
		if c.parts != "" {
			text = c.parts
		}
	case provider.ClassPlanBind:
		text = `{"bindings":[],"duplicates":[]}`
	case provider.ClassPlanSize:
		text = `{"sizes":[{"node":1,"size":"atomic","split_into":[]}]}`
	case provider.ClassPlanAudit:
		text = `{"checks":[{"node":1,"ok":true,"missing":[]}]}`
	case provider.ClassPlanContract:
		text = `{"contract":"Read the diff, then write the review it earns."}`
		if c.contract != "" {
			text = c.contract
		}
	case provider.ClassPlanBrief:
		text = "Read the diff and write REVIEW.md with the verdict in its first line."
	default:
		return nil, errUnexpectedPlanCall(class, messages)
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
	}}}, nil
}

func (c *planScriptClient) Model() string { return c.model }

func (c *planScriptClient) keysFor(class provider.CallClass) []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]string(nil), c.keys[class]...)
}

func errUnexpectedPlanCall(class provider.CallClass, messages []ai.Message) error {
	head := ""
	if len(messages) > 0 && len(messages[0].Content) > 0 {
		head = messages[0].Content[0].Text
		if len(head) > 48 {
			head = head[:48]
		}
	}
	return &unexpectedPlanCall{class: class, head: head}
}

type unexpectedPlanCall struct {
	class provider.CallClass
	head  string
}

func (e *unexpectedPlanCall) Error() string {
	return "unscripted planning call " + string(e.class) + ": " + e.head + "…"
}
