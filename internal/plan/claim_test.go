package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// capturingClient records the final user message of every call so a test can
// read the prompt a pass was actually sent.
type capturingClient struct {
	reply func(system, user string) string

	mutex  sync.Mutex
	fanout []string
}

func (c *capturingClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system, user string
	for _, message := range messages {
		text := textOf(message)
		if message.Role == "system" {
			system += text
			continue
		}
		user += text
	}
	if strings.Contains(system, "You list the parts of one stage") {
		c.mutex.Lock()
		c.fanout = append(c.fanout, user)
		c.mutex.Unlock()
	}
	return response(c.reply(system, user)), nil
}

func splitReply(system, _ string) string {
	if strings.Contains(system, "You list the parts of one stage") {
		return `{"parts":[
			{"title":"First","summary":"Own the first units","sources":["a"]},
			{"title":"Second","summary":"Own the second units","sources":["b"]}]}`
	}
	return `{"sizes":[
		{"node":1,"size":"atomic","split_into":[]},
		{"node":2,"size":"atomic","split_into":[]},
		{"node":3,"size":"atomic","split_into":[]}]}`
}

func claimableGraph() (*Graph, int) {
	graph := &Graph{Goal: "the whole of it", Stages: []Stage{{Title: "Only"}}, NextID: 1}
	source := graph.Add(Node{Stage: 1, Title: "Gather", Summary: "Gather the units", Size: SizeAtomic})
	parent := graph.Add(Node{Stage: 1, Title: "Wide", Summary: "It enumerates its units",
		Size: SizeOversized, Parts: []string{"first", "second"}, Needs: []int{source}})
	return graph, parent
}

// The whole point of asking at claim time rather than at build time: the node
// is divided against what its inputs actually produced, and the titles it was
// divided against before are not what it is shown.
func TestClaimTimeExpansionIsShownWhatLandedInsteadOfWhatItWasCalled(t *testing.T) {
	client := &capturingClient{reply: splitReply}
	graph, parent := claimableGraph()

	scope := ClaimContext{
		Landed: []Landed{{Title: "job-n1", Result: "Fourteen units, listed by name in units.txt"}},
		Criterion: Done{Produces: []string{"a per-unit note"},
			Conditions: []Check{{Kind: CheckRead, Check: "every unit has one"}}},
	}
	if _, _, err := ExpandOne(t.Context(), client, graph, parent, Options{MaxDepth: 4, NodeBudget: 40}, scope); err != nil {
		t.Fatalf("ExpandOne: %v", err)
	}
	if len(client.fanout) != 1 {
		t.Fatalf("fan-out calls = %d, want 1 — an expansion is two calls, one of them a fan-out", len(client.fanout))
	}
	prompt := client.fanout[0]

	if strings.Contains(prompt, "It will receive the results of:") {
		t.Error("the node was still shown the titles of work it could have read the results of")
	}
	for _, want := range []string{"Fourteen units, listed by name", landedPreamble, criterionPreamble, criterionCoverage} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the expansion prompt does not carry %q", want)
		}
	}

	// L7: the shared head first, the node's own delta last. A per-node payload
	// in the middle of a prefix every expansion of the job shares is the whole
	// of a cache miss.
	burden := strings.Index(prompt, expandBurden)
	landed := strings.Index(prompt, landedPreamble)
	criterion := strings.Index(prompt, criterionPreamble)
	node := strings.Index(prompt, "Break down only this piece of it:")
	if !(burden < landed && landed < criterion && criterion < node) {
		t.Fatalf("the delta is not last: burden=%d landed=%d criterion=%d node=%d",
			burden, landed, criterion, node)
	}
}

// The rollback, stated as a prompt: a caller that knows nothing extra asks
// exactly what the build has always asked, byte for byte.
func TestAnExpansionWithNothingLandedAsksTheBuildsOwnQuestion(t *testing.T) {
	fromBuild := &capturingClient{reply: splitReply}
	built, _ := claimableGraph()
	if _, _, err := ExpandLevel(t.Context(), fromBuild, built, Options{MaxDepth: 4, NodeBudget: 40}); err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}

	atClaim := &capturingClient{reply: splitReply}
	claimed, claimedParent := claimableGraph()
	if _, _, err := ExpandOne(t.Context(), atClaim, claimed, claimedParent,
		Options{MaxDepth: 4, NodeBudget: 40}, ClaimContext{}); err != nil {
		t.Fatalf("ExpandOne: %v", err)
	}

	if len(fromBuild.fanout) != 1 || len(atClaim.fanout) != 1 {
		t.Fatalf("fan-out calls = %d and %d, want one each", len(fromBuild.fanout), len(atClaim.fanout))
	}
	if fromBuild.fanout[0] != atClaim.fanout[0] {
		t.Errorf("the claim-time question differs from the build's:\n--- build ---\n%s\n--- claim ---\n%s",
			fromBuild.fanout[0], atClaim.fanout[0])
	}
}

// BuildDepth says who decides the depth, never how deep the graph may be. Zero
// is every caller that existed before this wave and is the rollback.
func TestBuildDepthBoundsOnlyTheBuildsOwnLevels(t *testing.T) {
	for _, test := range []struct {
		name     string
		options  Options
		expected int
	}{
		{"unset is the ceiling", Options{MaxDepth: 3}, 3},
		{"shallower is honoured", Options{MaxDepth: 3, BuildDepth: 1}, 1},
		{"deeper never exceeds the ceiling", Options{MaxDepth: 2, BuildDepth: 9}, 2},
		{"no depth at all stays none", Options{MaxDepth: 0, BuildDepth: 4}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.options.buildLevels(); got != test.expected {
				t.Fatalf("buildLevels = %d, want %d", got, test.expected)
			}
		})
	}
}

// dividingClient answers every fan-out with two parts and sizes every node as
// divisible, so a build recurses as far as it is allowed to.
type dividingClient struct{}

func (c *dividingClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system, user string
	for _, message := range messages {
		text := textOf(message)
		if message.Role == "system" {
			system = text
			continue
		}
		user += text
	}
	switch {
	case system == spinePrompt:
		return response(`{"stages":[{"title":"Only","summary":"The only stage."}]}`), nil
	case system == groundPrompt:
		return response(`{"settled":[],"open":[]}`), nil
	case system == bindPrompt:
		return response(`{"bindings":[],"duplicates":[]}`), nil
	case system == auditPrompt:
		return response(`{"checks":[]}`), nil
	case strings.Contains(system, "You list the parts of one stage"):
		// Distinct subjects, because a division whose parts return the same
		// thing is refused before depth is ever reached.
		depth := strings.Count(user, "It sits under:")
		return response(fmt.Sprintf(`{"parts":[
			{"title":"Left %d","summary":"Own the left half at %d","sources":["l%d"]},
			{"title":"Right %d","summary":"Own the right half at %d","sources":["r%d"]}]}`,
			depth, depth, depth, depth, depth, depth)), nil
	case strings.Contains(system, "size"):
		sizes := make([]string, 0, 24)
		for id := 1; id <= 24; id++ {
			sizes = append(sizes, fmt.Sprintf(
				`{"node":%d,"size":"borderline","split_into":["one","two"]}`, id))
		}
		return response(`{"sizes":[` + strings.Join(sizes, ",") + `]}`), nil
	}
	return nil, fmt.Errorf("unexpected planning call: %.48s…", system)
}

// The wave, at the build: the same ceiling, one level of it decided in advance.
func TestABuildStopsWhereBuildDepthSaysAndTheCeilingStaysWhereItWas(t *testing.T) {
	deepest := func(options Options) int {
		graph, err := Build(context.Background(), &dividingClient{}, "divide the whole of it", options)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		depth := 0
		for _, node := range graph.Nodes {
			if node.Depth > depth {
				depth = node.Depth
			}
		}
		return depth
	}
	full := deepest(Options{MaxDepth: 3, NodeBudget: 200, Ensemble: EnsembleNever, SpineSamples: 1})
	shallow := deepest(Options{MaxDepth: 3, BuildDepth: 1, NodeBudget: 200, Ensemble: EnsembleNever, SpineSamples: 1})
	if full < 2 {
		t.Fatalf("the unbounded build reached depth %d, so this test proves nothing", full)
	}
	if shallow != 1 {
		t.Fatalf("a build asked for one level reached depth %d", shallow)
	}
}
