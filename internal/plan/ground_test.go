package plan

import (
	"strings"
	"testing"
)

// TestGroundPromptSettlesTheEvidenceStandard pins the rule that stops "a short
// written report comparing three vector databases" from becoming a fifteen-node
// benchmarking project: the standard is read off the goal, and it is a ceiling.
func TestGroundPromptSettlesTheEvidenceStandard(t *testing.T) {
	for _, want := range []struct {
		name   string
		phrase string
	}{
		{"it is settled here", "the evidence this goal warrants"},
		{"the three standards", "reading sources and citing them, by running something\nand measuring it, or by building something and demonstrating"},
		{"chosen from the goal's words", "the goal's own words say which it is\nasking for"},
		{"the report example", `"A short written report comparing three options" warrants reading`},
		{"a ceiling, not only a floor", "It is the ceiling as well as the floor"},
		{"cheapest sufficient standard", "the cheapest\nstandard that actually satisfies what was asked is the correct one"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if !strings.Contains(groundPrompt, want.phrase) {
				t.Errorf("ground prompt no longer states %s: missing %q", want.name, want.phrase)
			}
		})
	}
}

// TestGroundReturnsTheEvidenceStandard checks the wiring end to end: the reply
// is parsed, and the standard lands in the frozen preamble that every later
// planning call shares, alongside the scope it was settled with.
func TestGroundReturnsTheEvidenceStandard(t *testing.T) {
	client := &stubClient{reply: func(_, _ string) string {
		return `{"settled":["The three databases are Qdrant, Weaviate and pgvector."],
		         "open":["Which of the three suits the workload best."],
		         "evidence":"Read the projects' own documentation and cite it; run and measure nothing."}`
	}}

	grounding, _, err := Ground(t.Context(), client, "a short written report comparing three vector databases")
	if err != nil {
		t.Fatalf("Ground: %v", err)
	}
	if grounding.Evidence == "" {
		t.Fatal("the evidence standard was dropped on the way out of Ground")
	}

	graph := &Graph{Goal: "a short written report", Settled: grounding.Settled, Open: grounding.Open, Evidence: grounding.Evidence}
	shared := graph.context()
	if !strings.Contains(shared, grounding.Evidence) {
		t.Errorf("the shared preamble does not carry the evidence standard:\n%s", shared)
	}
	if !strings.Contains(shared, "ceiling as well as the floor") {
		t.Errorf("the preamble states the standard without saying it may not be escalated:\n%s", shared)
	}
}

// TestSubtreeInheritsTheEvidenceStandard is the part that matters for a deep
// plan. A sub-planner sees only the scope it was handed, so a standard that did
// not travel with it would be re-chosen — generously — for every subtree.
func TestSubtreeInheritsTheEvidenceStandard(t *testing.T) {
	const standard = "Read published documentation and cite it; build nothing."
	client := &stubClient{reply: func(system, _ string) string {
		if strings.Contains(system, "You list the parts of one stage") {
			return `{"parts":[
				{"title":"Qdrant","summary":"Profile Qdrant from its documentation","sources":["its docs"]},
				{"title":"Weaviate","summary":"Profile Weaviate from its documentation","sources":["its docs"]}]}`
		}
		return `{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]}]}`
	}}

	graph := &Graph{
		Goal:     "a short written report comparing three vector databases",
		Settled:  []string{"The three databases are Qdrant, Weaviate and pgvector."},
		Evidence: standard,
		Stages:   []Stage{{Title: "Compare"}},
		NextID:   1,
	}
	parent := graph.Add(Node{Stage: 1, Title: "Profiles", Summary: "Profile each database",
		Size: SizeOversized, Parts: []string{"qdrant", "weaviate"}})

	result := expandOne(t.Context(), client, graph, parent, Options{MaxDepth: 2, NodeBudget: 40})
	if result.err != nil {
		t.Fatalf("expandOne: %v", result.err)
	}
	if result.sub.Evidence != standard {
		t.Errorf("subtree evidence = %q, want the goal's standard %q", result.sub.Evidence, standard)
	}
	if !strings.Contains(result.sub.context(), standard) {
		t.Error("the subtree's own preamble omits the evidence standard, so its nodes may escalate it")
	}
}
