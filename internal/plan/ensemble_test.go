package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// scriptClient answers planning calls by which system prompt they carry.
//
// Matching on the constant itself rather than on a snippet of its wording is
// deliberate: these prompts are edited constantly, and a test that greps them
// for a phrase fails on a rewrite that changed nothing it was testing.
type scriptClient struct {
	mutex    sync.Mutex
	decision string
	seen     []scriptCall
}

type scriptCall struct{ system, user string }

func (c *scriptClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var system, user string
	for _, message := range messages {
		var text string
		for _, part := range message.Content {
			text += part.Text
		}
		if message.Role == "system" {
			system = text
			continue
		}
		user += text
	}
	c.mutex.Lock()
	c.seen = append(c.seen, scriptCall{system: system, user: user})
	c.mutex.Unlock()

	switch system {
	case spinePrompt:
		return textResponse(`{"stages":[{"title":"Review","summary":"Judge the change end to end."}]}`), nil
	case groundPrompt:
		return textResponse(`{"settled":[],"open":[]}`), nil
	case ensemblePrompt:
		return textResponse(c.decision), nil
	case briefWithCriterion:
		// The two briefs an ensemble asks for are told apart the same way the
		// real writer would: by which node the target line names.
		if strings.Contains(user, `"Checkout"`) {
			return textResponse("Check out the branch under review and write the complete diff to a file."), nil
		}
		return textResponse("Read the diff in front of you and report every defect you can verify."), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
}

func (c *scriptClient) calls(system string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	count := 0
	for _, call := range c.seen {
		if call.system == system {
			count++
		}
	}
	return count
}

func (c *scriptClient) prompts(system string) []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var found []string
	for _, call := range c.seen {
		if call.system == system {
			found = append(found, call.user)
		}
	}
	return found
}

func textResponse(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}}
}

const ensembleVerdict = `{
  "mode": "ensemble",
  "reason": "one judgment over one diff",
  "pass_title": "Review",
  "pass_summary": "Review the whole pull request and list every defect you can verify.",
  "pass_sources": ["the full diff", "the test suite"],
  "setup_title": "Checkout",
  "setup_summary": "Check out the branch and produce the full diff.",
  "deliverable": "a merged defect list"
}`

const decomposeVerdict = `{
  "mode": "decompose",
  "reason": "three separate cities",
  "pass_title": "", "pass_summary": "", "pass_sources": [],
  "setup_title": "", "setup_summary": "", "deliverable": ""
}`

func buildEnsemble(t *testing.T, decision string, options Options) (*Graph, *scriptClient) {
	t.Helper()
	client := &scriptClient{decision: decision}
	options.SpineSamples = 1
	graph, err := Build(context.Background(), client, "review this pull request", options)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if graph == nil {
		t.Fatal("Build returned no graph")
	}
	return graph, client
}

func panelistsOf(graph *Graph) []Node {
	var panelists []Node
	for _, node := range graph.Nodes {
		if node.Kind == KindWork && node.Title != "Checkout" {
			panelists = append(panelists, node)
		}
	}
	return panelists
}

func synthesisOf(t *testing.T, graph *Graph) Node {
	t.Helper()
	var found []Node
	for _, node := range graph.Nodes {
		if node.Kind == KindSynthesis {
			found = append(found, node)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one synthesis node, got %d", len(found))
	}
	return found[0]
}

// TestEnsembleGraphShape is the structural claim: setup, then N panelists that
// depend only on it, then one merge that depends on all of them. Nothing else,
// and in particular no fan-out — an ensemble replaces decomposition rather than
// running alongside it.
func TestEnsembleGraphShape(t *testing.T) {
	graph, client := buildEnsemble(t, ensembleVerdict, Options{})

	if len(graph.Nodes) != 1+DefaultPanelists+1 {
		t.Fatalf("graph has %d nodes, want setup + %d panelists + synthesis", len(graph.Nodes), DefaultPanelists)
	}
	if client.calls(fanoutPrompt) != 0 {
		t.Errorf("the ensemble path ran %d fan-out calls, want none", client.calls(fanoutPrompt))
	}

	setup := graph.Nodes[0]
	if setup.Title != "Checkout" || setup.Kind != KindWork {
		t.Fatalf("first node = %q (%s), want the shared setup", setup.Title, setup.Kind)
	}
	if len(setup.Needs) != 0 {
		t.Errorf("setup needs %v, want nothing — it is the root", setup.Needs)
	}

	panelists := panelistsOf(graph)
	if len(panelists) != DefaultPanelists {
		t.Fatalf("got %d panelists, want %d", len(panelists), DefaultPanelists)
	}
	titles := map[string]bool{}
	for _, panelist := range panelists {
		if titles[panelist.Title] {
			t.Errorf("panelist title %q is not distinct", panelist.Title)
		}
		titles[panelist.Title] = true
		if len(panelist.Needs) != 1 || panelist.Needs[0] != setup.ID {
			t.Errorf("panelist %q needs %v, want only the setup node %d", panelist.Title, panelist.Needs, setup.ID)
		}
		if panelist.Summary != panelists[0].Summary {
			t.Errorf("panelist %q summary differs from panelist 1 — coverage must be identical", panelist.Title)
		}
	}

	synthesis := synthesisOf(t, graph)
	if len(synthesis.Needs) != len(panelists) {
		t.Fatalf("synthesis needs %v, want all %d panelists", synthesis.Needs, len(panelists))
	}
	for _, panelist := range panelists {
		if !contains(synthesis.Needs, panelist.ID) {
			t.Errorf("synthesis does not read panelist %d (%s)", panelist.ID, panelist.Title)
		}
	}

	// One deliverable owner: the merge is the only node nothing consumes, so it
	// is the only node that can be writing the final answer.
	sinks := graph.Sinks()
	if len(sinks) != 1 || sinks[0] != synthesis.ID {
		t.Errorf("sinks = %v, want only the synthesis node %d", sinks, synthesis.ID)
	}
	if graph.hasCycle() {
		t.Error("ensemble graph has a cycle")
	}

	// Panelists deliver findings; only the merge delivers the thing asked for.
	for _, panelist := range panelists {
		if !strings.Contains(panelist.Brief, "Do not write a merged defect list") {
			t.Errorf("panelist %q is not told to leave the deliverable to the merge:\n%s", panelist.Title, panelist.Brief)
		}
	}
	if !strings.Contains(synthesis.Summary, "a merged defect list") {
		t.Errorf("synthesis summary %q does not name the deliverable", synthesis.Summary)
	}
}

// TestPanelistBriefsAreIndependent is the point of the whole arrangement. The
// panelists must be identical in coverage and ignorant of each other: a brief
// that carves out a boundary between them reintroduces exactly the correlated
// blind spot the redundancy was bought to remove.
func TestPanelistBriefsAreIndependent(t *testing.T) {
	graph, client := buildEnsemble(t, ensembleVerdict, Options{})
	panelists := panelistsOf(graph)

	// One brief for the panel, not one per panelist. Writing them separately is
	// how identical coverage silently becomes approximate coverage.
	if got := client.calls(briefWithCriterion); got != 2 {
		t.Errorf("brief calls = %d, want 2 (one shared panel brief, one setup)", got)
	}
	for index, panelist := range panelists {
		if strings.TrimSpace(panelist.Brief) == "" {
			t.Fatalf("panelist %q has no brief", panelist.Title)
		}
		if panelist.Brief != panelists[0].Brief {
			t.Errorf("panelist %d's brief differs from panelist 1's; they must be byte-identical", index+1)
		}
	}

	// Nothing shown to the brief writer, and nothing in the brief itself, may
	// name another panelist.
	var others []string
	for _, panelist := range panelists[1:] {
		others = append(others, panelist.Title)
	}
	for _, prompt := range client.prompts(briefWithCriterion) {
		for _, other := range others {
			if strings.Contains(prompt, other) {
				t.Errorf("the brief writer was shown %q — it can only carve a boundary it knows about:\n%s", other, prompt)
			}
		}
	}
	for _, other := range others {
		if strings.Contains(panelists[0].Brief, other) {
			t.Errorf("panelist brief references %q; panelists must not know about each other", other)
		}
	}
	// The charge that makes a lone reader behave like a whole panel — and the
	// clause that keeps it from colliding with the leaf's own 300-word cap. The
	// charge once said nothing left out would be recovered later, which the leaf
	// contract flatly contradicts; both wanted the same thing, and the split
	// between the answer and its working is where they agree.
	for _, phrase := range []string{"Report everything", "a\nfinding you drop is gone for good",
		"Completeness is about the findings, not about where each one is written down",
		"Write the full enumeration to a file"} {
		if !strings.Contains(panelists[0].Brief, phrase) {
			t.Errorf("panelist brief is missing %q:\n%s", phrase, panelists[0].Brief)
		}
	}
}

// TestMergeContract pins the wording that decides whether the ensemble was
// worth paying for. Union with dedup, verification of anything contested, and
// nothing dropped merely for having been seen once — the intuitive merge, which
// keeps what the passes agreed on, throws away precisely the findings the extra
// passes were bought to catch.
func TestMergeContract(t *testing.T) {
	graph, _ := buildEnsemble(t, ensembleVerdict, Options{})
	synthesis := synthesisOf(t, graph)

	for _, phrase := range []string{
		"Take the union of what they found, not the intersection.",
		"is not weaker for having been found once",
		"The only thing that earns the right to drop a finding is having checked",
		"merge them into one",
		"go back to the material and settle it yourself",
		"a merged defect list",
	} {
		if !strings.Contains(synthesis.Brief, phrase) {
			t.Errorf("merge brief is missing %q:\n%s", phrase, synthesis.Brief)
		}
	}
	for _, phrase := range []string{
		"Read every result in full before writing anything",
		"checked against the material itself before you keep or drop it",
		"Rank what survives by consequence, not by how many passes mentioned it",
		"keeping only what the passes agreed on",
	} {
		if !strings.Contains(synthesis.Contract, phrase) {
			t.Errorf("merge contract is missing %q:\n%s", phrase, synthesis.Contract)
		}
	}

	// The merge is the harness's own node, so its instruction is written here
	// and must survive the executor's brief and contract passes untouched.
	usage, err := Briefs(context.Background(), &scriptClient{decision: ensembleVerdict}, graph)
	if err != nil {
		t.Fatalf("Briefs: %v", err)
	}
	if usage.Calls != 0 {
		t.Errorf("a run-time brief pass made %d calls over a finished ensemble, want 0", usage.Calls)
	}
	if got := synthesisOf(t, graph); got.Brief != synthesis.Brief {
		t.Error("the run-time brief pass overwrote the merge contract")
	}
}

// TestEnsembleForcedCount covers the operator's override: N panelists even when
// the judgment call says decompose, because the flag is an instruction and not
// a suggestion.
func TestEnsembleForcedCount(t *testing.T) {
	graph, _ := buildEnsemble(t, decomposeVerdict, Options{Ensemble: 5})
	if got := len(panelistsOf(graph)); got != 5 {
		t.Fatalf("got %d panelists, want the 5 that were asked for", got)
	}
	synthesis := synthesisOf(t, graph)
	if len(synthesis.Needs) != 5 {
		t.Errorf("synthesis needs %v, want all five panelists", synthesis.Needs)
	}
	// The verdict left the shape empty, so the defaults have to carry it.
	for _, panelist := range panelistsOf(graph) {
		if strings.TrimSpace(panelist.Summary) == "" || strings.TrimSpace(panelist.Title) == "" {
			t.Errorf("forced panelist %d has no shape: %+v", panelist.ID, panelist)
		}
		if len(panelist.Needs) != 0 {
			t.Errorf("panelist needs %v, want nothing — no setup was described", panelist.Needs)
		}
	}
}

// TestEnsembleDecomposeFallsThrough is the hook's other half: on the ordinary
// answer it must leave the graph exactly as it found it, so the four passes
// below it run against an untouched plan.
func TestEnsembleDecomposeFallsThrough(t *testing.T) {
	client := &scriptClient{decision: decomposeVerdict}
	graph := &Graph{Goal: "profile three cities", Stages: []Stage{{Title: "Research"}, {Title: "Write"}}, NextID: 1}
	ensemble, chosen, err := ensembleHook(context.Background(), client, graph, Options{}, func(string, time.Duration, string) {}, time.Now())
	if err != nil || chosen || ensemble != nil {
		t.Fatalf("decompose verdict = (%v, %v, %v), want the hook to decline", ensemble, chosen, err)
	}
	if len(graph.Nodes) != 0 || len(graph.Stages) != 2 {
		t.Errorf("the hook modified a graph it declined: %d nodes, %d stages", len(graph.Nodes), len(graph.Stages))
	}
	if graph.Usage.Calls != 1 {
		t.Errorf("usage.Calls = %d, want the one judgment call accounted for", graph.Usage.Calls)
	}
}

// TestEnsembleNeverCostsNothing keeps the opt-out honest: -1 must not spend a
// call to be told what the caller already knows.
func TestEnsembleNeverCostsNothing(t *testing.T) {
	client := &scriptClient{decision: ensembleVerdict}
	graph := &Graph{Goal: "review this pull request", NextID: 1}
	ensemble, chosen, err := ensembleHook(context.Background(), client, graph, Options{Ensemble: EnsembleNever}, func(string, time.Duration, string) {}, time.Now())
	if chosen || ensemble != nil || err != nil {
		t.Fatalf("EnsembleNever = (%v, %v, %v), want a silent decline", ensemble, chosen, err)
	}
	if len(client.seen) != 0 {
		t.Errorf("EnsembleNever made %d calls, want none", len(client.seen))
	}
}

// TestEnsembleSurvivesAFailedJudgment: the panel is an optimisation, so a
// broken judgment call degrades to decomposition rather than failing the plan.
func TestEnsembleSurvivesAFailedJudgment(t *testing.T) {
	graph := &Graph{Goal: "review this pull request", NextID: 1}
	broken := failingClient{}
	ensemble, chosen, err := ensembleHook(context.Background(), broken, graph, Options{}, func(string, time.Duration, string) {}, time.Now())
	if chosen || ensemble != nil || err != nil {
		t.Fatalf("failed judgment = (%v, %v, %v), want a fall-through to decomposition", ensemble, chosen, err)
	}
}

type failingClient struct{}

func (failingClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	return nil, errors.New("provider is down")
}
