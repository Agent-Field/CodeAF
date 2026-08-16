package plan

import (
	"context"
	"strings"
	"testing"
)

// The spine criterion is the prompt-level gate definition. A goal whose parts
// are sequential — B reads A's files — but whose parts are NOT genuinely
// parallel workers must plan as one stage, because a single agent reads its
// own files sequentially all the time. The gate is not "B reads A's output";
// the gate is "B would otherwise run at the same time as A, and only the
// dependency keeps it from starting."
func TestSpineCriterionDoesNotGateSequentialFileReads(t *testing.T) {
	// The prompt must carry the three-gate criterion.
	prompt := spinePrompt
	for _, want := range []string{
		"Three things and only three things make a real gate",
		"Parallelism that would otherwise be serialized",
		"Worker isolation",
		"Context-window pressure",
		"A single agent working through its own files in sequence meets none of these",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the spine prompt lost the criterion clause %q", want)
		}
	}
	// The old broad gate ("A stage exists where output feeds input") must be
	// gone, replaced by the three-gate criterion.
	if strings.Contains(prompt, "A stage exists where output feeds input, and nowhere else") {
		t.Fatal("the old broad gate definition is still in the prompt — it makes every sequential read a gate")
	}
}

// A bimodal sequential goal — A produces a file, B reads it — must plan as
// one stage when the two halves are not genuinely parallel workers. A single
// agent reads its own files sequentially; the sequence is inside the worker,
// not between workers.
func TestSequentialGoalPlansOneStage(t *testing.T) {
	client := &stubClient{reply: func(system, user string) string {
		if strings.Contains(system, "You break a goal into its ordered stages") {
			// A sequential goal: read the config, then write the module that
			// uses it. One agent does both in sequence — no parallelism, no
			// worker isolation, no context-window pressure.
			return `{"stages":[{"title":"Read config and write the module","summary":"read the config file, then write the module that uses it"}]}`
		}
		return ""
	}}

	choice, _, err := spineWithProgress(context.Background(), client,
		"read config.yaml and write the module that uses its settings", "", nil, 1, nil)
	if err != nil {
		t.Fatalf("spine: %v", err)
	}
	if len(choice.Stages) != 1 {
		t.Fatalf("a sequential goal planned %d stages, want 1 — a single agent reads its own files sequentially", len(choice.Stages))
	}
}

// A genuinely parallel goal — independent halves that could run at the same
// time but where one half assembles what the others produce — must plan
// multi-stage, because the assembler is gated on the others finishing.
func TestParallelGoalPlansMultiStage(t *testing.T) {
	client := &stubClient{reply: func(system, user string) string {
		if strings.Contains(system, "You break a goal into its ordered stages") {
			// A parallel goal: profile three vendors (independent, parallel),
			// then compare them (gated on the profiles). The comparison is a
			// genuine gate: the profiling workers would run at the same time
			// without it, and the comparison cannot start until they finish.
			return `{"stages":[` +
				`{"title":"Profile each vendor","summary":"profile the three vendors in parallel"},` +
				`{"title":"Compare the profiles","summary":"compare the three vendor profiles"}` +
				`]}`
		}
		return ""
	}}

	choice, _, err := spineWithProgress(context.Background(), client,
		"compare three vendors", "", nil, 1, nil)
	if err != nil {
		t.Fatalf("spine: %v", err)
	}
	if len(choice.Stages) < 2 {
		t.Fatalf("a parallel goal with an assembler planned %d stages, want >= 2 — the assembler is gated on the parallel workers", len(choice.Stages))
	}
}

// Expansion before any node runs must not fire: a fresh small task — one node,
// atomic-sized, with no work having run — must not grow. The growth mechanism
// is JudgeSplit, which refuses a node that is within one worker's reach.
func TestExpansionDoesNotFireOnAFreshSmallTask(t *testing.T) {
	graph := &Graph{Goal: "do the thing", Stages: []Stage{{Title: "Do it"}}, NextID: 1}
	graph.Add(Node{
		Kind:  KindWork,
		Stage: 1,
		Title: "Do the thing",
		Size:  SizeAtomic,
		Parts: []string{"one piece"},
		State: StatePending,
	})

	options := Options{MaxDepth: 3, NodeBudget: 40}
	candidates := selectForExpansion(graph, options)
	if len(candidates) != 0 {
		t.Fatalf("a fresh atomic node with one part was selected for expansion: %v — a small task must not grow before any work has run", candidates)
	}

	// And the refusal is journaled so the graph explains its shape.
	node := graph.Node(1)
	if node.Undivided != RefusalUnnamed {
		t.Fatalf("undivided = %q, want %q — the node should say it could not name two pieces", node.Undivided, RefusalUnnamed)
	}
}

// A fresh task that names two parallel parts but is still atomic-sized also
// must not expand, because the size gate says one worker can carry it.
func TestExpansionDoesNotFireOnAFreshAtomicTaskWithParts(t *testing.T) {
	graph := &Graph{Goal: "do two things", Stages: []Stage{{Title: "Do them"}}, NextID: 1}
	graph.Add(Node{
		Kind:  KindWork,
		Stage: 1,
		Title: "Do two things",
		Size:  SizeAtomic,
		Parts: []string{"first thing", "second thing"},
		State: StatePending,
	})

	options := Options{MaxDepth: 3, NodeBudget: 40}
	candidates := selectForExpansion(graph, options)
	if len(candidates) != 0 {
		t.Fatalf("a fresh atomic node with two parts was selected for expansion: %v — size outranks parts, and one worker can carry it", candidates)
	}
}
