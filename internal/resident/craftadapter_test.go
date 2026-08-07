package resident

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// presentationCraft is the shape the whole feature was described against: a
// research step, a fan-out over what it found, an assembly, and a verifier
// that buys bounded repair rounds.
func presentationCraft() *craft.Workflow {
	return &craft.Workflow{
		Name:        "presentation",
		Commit:      "abc1234def",
		Description: "a deck about {{topic}}",
		Params: []craft.Param{
			{Name: "topic", Required: true},
			{Name: "tone", Default: "plain"},
		},
		Steps: []craft.Step{
			{ID: "research", Brief: "Research {{topic}} and settle the sections the deck needs."},
			{
				ID: "sections", Brief: "Write the {{tone}} slide for {{item}}.",
				Needs:   []string{"research"},
				ForEach: &craft.ForEach{Source: "research", Fan: 3},
				Skill:   "deckwright", Model: "boost",
			},
			{ID: "assemble", Brief: "Assemble the slides into one deck about {{topic}}.", Needs: []string{"sections"}},
			{
				ID: "check", Needs: []string{"assemble"},
				Verify: &craft.Verify{
					Script:    "verifiers/deck.sh",
					UntilPass: &craft.UntilPass{Revise: []string{"assemble"}, MaxRounds: 3},
				},
			},
		},
		Limits: craft.Limits{CostUSD: 1.50, WallClock: 30 * time.Minute},
	}
}

func craftTestProvenance(workflow *craft.Workflow) store.Provenance {
	return store.Provenance{
		Origin: store.OriginUser, SessionID: "craft", Intent: "run the presentation craft",
		Craft: CraftRef(workflow),
	}
}

// A workflow compiles to an ordinary subtree: leaves, feeds_into edges, filled
// briefs, the skill sentence, and one root that stays open for the whole run.
func TestCompileCraftProducesAnOrdinarySubtree(t *testing.T) {
	workflow := presentationCraft()
	subtree, err := CompileCraftAs("run-1", "/home/craft", workflow,
		map[string]string{"topic": "quantum error correction"}, craftTestProvenance(workflow))
	if err != nil {
		t.Fatal(err)
	}
	if len(subtree.Nodes) != 5 {
		t.Fatalf("compiled %d nodes, want a root plus four steps", len(subtree.Nodes))
	}
	byID := make(map[string]store.NodeSpec, len(subtree.Nodes))
	for _, node := range subtree.Nodes {
		byID[node.ID] = node
	}

	root, ok := byID["run-1"]
	if !ok || root.Parent != "" || len(root.Needs) != 1 || root.Needs[0].NodeID != "run-1~check" {
		t.Fatalf("root = %+v", root)
	}
	for _, id := range []string{"run-1~research", "run-1~sections", "run-1~assemble", "run-1~check"} {
		node, present := byID[id]
		if !present {
			t.Fatalf("step %q did not compile", id)
		}
		if node.Parent != "run-1" {
			t.Fatalf("%s parent = %q", id, node.Parent)
		}
		if strings.Contains(node.Brief, "{{topic}}") || strings.Contains(node.Brief, "{{tone}}") {
			t.Fatalf("%s kept an unfilled reference:\n%s", id, node.Brief)
		}
	}
	if edges := byID["run-1~assemble"].Needs; len(edges) != 1 ||
		edges[0].NodeID != "run-1~sections" || edges[0].Kind != store.FeedsInto {
		t.Fatalf("assemble needs = %+v", edges)
	}
	if stage := byID["run-1~check"].Stage; stage != 4 {
		t.Fatalf("check stage = %d, want one past assemble", stage)
	}
	if !strings.Contains(byID["run-1~research"].Brief, "quantum error correction") {
		t.Fatalf("research brief lost its parameter:\n%s", byID["run-1~research"].Brief)
	}

	fan := byID["run-1~sections"].Brief
	if !strings.Contains(fan, craftItemsMarker) || !strings.Contains(fan, "at most 3") {
		t.Fatalf("fan-out brief does not ask for a capped list:\n%s", fan)
	}
	if !strings.Contains(fan, "{{item}}") {
		t.Fatalf("fan-out brief lost the per-item assignment:\n%s", fan)
	}
	if !strings.Contains(fan, "The deckwright skill is on PATH") {
		t.Fatalf("fan-out brief lost its skill sentence:\n%s", fan)
	}
	if !strings.Contains(fan, "written for the boost model") {
		t.Fatalf("fan-out brief lost its model advice:\n%s", fan)
	}
	// The fan-out step also consumes the step its list comes from, whether or
	// not the file said so twice.
	if needs := byID["run-1~sections"].Needs; len(needs) != 1 || needs[0].NodeID != "run-1~research" {
		t.Fatalf("fan-out needs = %+v", needs)
	}

	check := byID["run-1~check"].Brief
	if !strings.Contains(check, "/home/craft/verifiers/deck.sh") {
		t.Fatalf("verify brief did not resolve the script against the repo:\n%s", check)
	}
	if !strings.Contains(check, craftVerdictMarker+" "+craftVerdictPass) {
		t.Fatalf("verify brief did not demand a verdict:\n%s", check)
	}
}

// Provenance is what makes a compiled subtree a craft run. Compiling against a
// provenance that names something else is a mistake worth refusing, because
// every survival statistic downstream reads that one field.
func TestCompileCraftRequiresItsOwnVersionInProvenance(t *testing.T) {
	workflow := presentationCraft()
	_, err := CompileCraftAs("run-1", "", workflow, map[string]string{"topic": "x"},
		store.Provenance{Origin: store.OriginUser, Intent: "run it", Craft: "presentation@older"})
	if err == nil || !strings.Contains(err.Error(), "provenance names craft") {
		t.Fatalf("mismatched craft provenance compiled: %v", err)
	}
}

// A hole nobody filled is worse than no run: the leaf would run with braces in
// its brief and nobody would find out until the deliverable landed.
func TestCompileCraftRefusesAnUnfilledReference(t *testing.T) {
	workflow := presentationCraft()
	// The refusal is the workflow's own — the compiler fills through Fill
	// rather than keeping a second copy of the filling law.
	_, err := CompileCraftAs("run-1", "", workflow, nil, craftTestProvenance(workflow))
	if err == nil || !strings.Contains(err.Error(), "missing required param: topic") {
		t.Fatalf("missing required parameter compiled: %v", err)
	}

	loose := &craft.Workflow{
		Name: "loose", Commit: "c0ffee",
		Steps: []craft.Step{{ID: "one", Brief: "use {{nobody}} for this"}},
	}
	_, err = CompileCraftAs("run-2", "", loose, nil, store.Provenance{
		Origin: store.OriginUser, Intent: "run it", Craft: CraftRef(loose),
	})
	if err == nil || !strings.Contains(err.Error(), "{{nobody}}") {
		t.Fatalf("unresolved reference compiled: %v", err)
	}
}

// CompileCraft's own namespace is derived from what the run is, so the same
// ask compiles to the same ids twice — the property every replay depends on.
func TestCompileCraftDerivesAReproducibleNamespace(t *testing.T) {
	workflow := presentationCraft()
	params := map[string]string{"topic": "quantum"}
	first, err := CompileCraft(workflow, params, craftTestProvenance(workflow))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileCraft(workflow, params, craftTestProvenance(workflow))
	if err != nil {
		t.Fatal(err)
	}
	if first.Nodes[0].ID != second.Nodes[0].ID || !strings.HasPrefix(first.Nodes[0].ID, "craft-presentation-") {
		t.Fatalf("namespace = %q then %q", first.Nodes[0].ID, second.Nodes[0].ID)
	}
}

// The ceilings are enforced here rather than trusted from the file: a compiled
// run has to be able to believe every number it reads.
func TestCraftCeilingsAreEnforcedAtCompile(t *testing.T) {
	if fan := craftFanCap(&craft.ForEach{Fan: 500}); fan != craft.MaxFanCap {
		t.Fatalf("fan cap = %d", fan)
	}
	if fan := craftFanCap(&craft.ForEach{}); fan != craft.DefaultFanCap {
		t.Fatalf("default fan cap = %d", fan)
	}
	if rounds := craftMaxRounds(&craft.UntilPass{MaxRounds: 99}); rounds != craft.MaxRounds {
		t.Fatalf("rounds = %d", rounds)
	}
	if cost := craftCostCeiling(craft.Limits{CostUSD: 500}); cost != craft.MaxRunBudgetUSD {
		t.Fatalf("cost ceiling = %.2f", cost)
	}
	if wall := craftWallClock(craft.Limits{}); wall != craft.DefaultWallClock {
		t.Fatalf("wall clock = %s", wall)
	}
}

// The id is the run's whole runtime memory: prefix, step, and which generation
// of it landed. It has to read back exactly.
func TestCraftNodeIDsParseBackToStepAndGeneration(t *testing.T) {
	prefix, step, kind, index, ok := craftNodeParts("run-1~check")
	if !ok || prefix != "run-1" || step != "check" || kind != "" || index != 0 {
		t.Fatalf("compiled id parsed as %q %q %q %d %t", prefix, step, kind, index, ok)
	}
	prefix, step, kind, index, ok = craftNodeParts(craftGenerationID("run-1", "check", craftRoundGeneration, 3))
	if !ok || prefix != "run-1" || step != "check" || kind != craftRoundGeneration || index != 3 {
		t.Fatalf("round id parsed as %q %q %q %d %t", prefix, step, kind, index, ok)
	}
	if _, _, _, _, ok := craftNodeParts("task-12-n4"); ok {
		t.Fatalf("an ordinary planned id parsed as craft")
	}
}

// An optional param with no default is a hole the file said may stay empty.
// Both entry points into a compile — recognition and a run by name — have to
// read that the same way, or the same workflow runs from a matched request and
// refuses when it is asked for outright.
func TestAnOptionalParamWithNoDefaultCompilesFromEitherEntryPoint(t *testing.T) {
	workflow := &craft.Workflow{
		Name: "note", Commit: "abc1234",
		Params: []craft.Param{{Name: "topic", Required: true}, {Name: "aside"}},
		Steps:  []craft.Step{{ID: "write", Brief: "Write about {{topic}}. Aside: {{aside}}"}},
		Limits: craft.Limits{CostUSD: 1, WallClock: time.Minute},
	}
	graph := openStore(t)
	runner := NewCraftRunner(graph, &fakeCraftRepo{workflow: workflow}, "/home/craft")
	run, err := runner.RunCraft("note", map[string]string{"topic": "the outage"}, "craft", "")
	if err != nil {
		t.Fatalf("run by name: %v", err)
	}
	node, ok, err := graph.Node(run.Prefix + "~write")
	if err != nil || !ok {
		t.Fatalf("read the step: found=%t err=%v", ok, err)
	}
	if strings.Contains(node.Brief, "{{aside}}") {
		t.Fatalf("an unfilled optional param reached the brief:\n%s", node.Brief)
	}

	filled, err := workflow.Fill(map[string]string{"topic": "the outage"})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if _, err := CompileCraftAs("run-1", "", workflow, filled, store.Provenance{
		Origin: store.OriginUser, Intent: "write it", Craft: CraftRef(workflow),
	}); err != nil {
		t.Fatalf("the recognized path compiled differently: %v", err)
	}
}

// One id law, asserted as one law: a file the validator accepts is a file that
// compiles, and a file it refuses is one that never could. Anything else and a
// forged craft announces itself, saves, and fails forever with nobody watching.
func TestValidatedStepIDsAreExactlyTheOnesThatCompile(t *testing.T) {
	for _, id := range []string{"outline", "first-draft", "step2", "step_one", "first--draft", "draft-", "Draft"} {
		workflow := &craft.Workflow{
			Name: "one-step", Commit: "abc1234",
			Steps:  []craft.Step{{ID: id, Brief: "do the thing"}},
			Limits: craft.Limits{CostUSD: 1, WallClock: time.Minute},
		}
		validated := len(workflow.Validate()) == 0
		_, err := CompileCraftAs("run-1", "", workflow, nil, store.Provenance{
			Origin: store.OriginUser, Intent: "run it", Craft: CraftRef(workflow),
		})
		if compiled := err == nil; compiled != validated {
			t.Errorf("step id %q: validated=%t compiled=%t (%v)", id, validated, compiled, err)
		}
	}
}
