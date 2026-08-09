package scheduler

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

func stringPointer(value string) *string { return &value }

func task(id, title, parent, description string, status plandb.TaskStatus, tags ...string) *plandb.Task {
	var parentID *string
	if parent != "" {
		parentID = stringPointer(parent)
	}
	return &plandb.Task{
		ID:           id,
		Title:        title,
		ParentTaskID: parentID,
		Description:  stringPointer(description),
		Status:       status,
		Tags:         tags,
	}
}

func taskIDs(tasks []*plandb.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			ids = append(ids, "")
		} else {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func TestAutoDispatchableKeepsArrayOwnPropertyBug(t *testing.T) {
	if hasOwnTagProperty([]string{"auto:scheduler"}, "auto:scheduler") {
		t.Fatal("array value incorrectly treated as an own property")
	}
	if !hasOwnTagProperty([]string{"auto:scheduler"}, "0") {
		t.Fatal("array index was not treated as an own property")
	}
	if !hasOwnTagProperty([]string{}, "length") {
		t.Fatal("array length was not treated as an own property")
	}
	for _, candidate := range []*plandb.Task{
		nil,
		task("plain", "Plain", "root", "", plandb.StatusReady),
		task("tagged", "Tagged", "root", "", plandb.StatusReady, "auto:scheduler"),
	} {
		if !isAutoDispatchable(candidate) {
			t.Fatalf("isAutoDispatchable(%#v) = false", candidate)
		}
	}
}

func TestSuggestedAgentPrecedence(t *testing.T) {
	tests := []struct {
		name string
		task *plandb.Task
		want string
	}{
		{
			name: "tag wins",
			task: task("a", "", "root", "agent: oracle\ntask_role: probe", plandb.StatusReady, "agent:fixer"),
			want: "fixer",
		},
		{
			name: "policy",
			task: task("a", "", "root", "agent: oracle\ntask_role: probe", plandb.StatusReady),
			want: "oracle",
		},
		{
			name: "orchestrator policy ignored",
			task: task("a", "", "root", "agent: orchestrator\ntask_role: research", plandb.StatusReady),
			want: "explore",
		},
		{
			name: "role",
			task: task("a", "", "root", "task_role: architecture", plandb.StatusReady),
			want: "oracle",
		},
		{
			name: "default",
			task: task("a", "", "root", "", plandb.StatusReady),
			want: "fixer",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestedAgent(tc.task); got != tc.want {
				t.Fatalf("suggestedAgent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScanCandidatesDescendantsDuplicatesAndComposites(t *testing.T) {
	root := task("root", "Root", "", "", plandb.StatusRunning)
	first := task("a", "same", "root", "body", plandb.StatusReady)
	duplicate := task("b", "same", "root", "body", plandb.StatusReady)
	deepParent := task("parent", "Parent", "root", "", plandb.StatusRunning)
	deepParent.IsComposite = true
	deep := task("deep", "Deep", "parent", "", plandb.StatusReady)
	composite := task("composite", "Composite", "root", "", plandb.StatusReady)
	composite.IsComposite = true
	outside := task("outside", "Outside", "other-root", "", plandb.StatusReady)

	ready := []*plandb.Task{duplicate, deep, outside, composite, first}
	all := []*plandb.Task{root, first, duplicate, deepParent, deep, composite, outside}
	got := scanCandidates("root", ready, all)
	if ids := taskIDs(got.Candidates); len(ids) != 2 || ids[0] != "deep" || ids[1] != "a" {
		t.Fatalf("candidate ids = %#v", ids)
	}
	if len(got.Skipped) != 2 ||
		got.Skipped[0] != (SkippedTask{TaskID: "b", Reason: "duplicate of a"}) ||
		got.Skipped[1] != (SkippedTask{TaskID: "composite", Reason: "composite"}) {
		t.Fatalf("skipped = %#v", got.Skipped)
	}
	if _, ok := got.Descendants["deep"]; !ok {
		t.Fatal("deep descendant missing")
	}
	if got.Scanned != len(ready) {
		t.Fatalf("scanned = %d", got.Scanned)
	}
}

func seedPlanDBForScheduler(t *testing.T) []*plandb.Task {
	t.Helper()
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	db.Init("scheduler-project")
	feed := plandb.DepFeedsInto
	a, err := db.AddTask(plandb.AddTaskInput{
		Title:    "A",
		Project:  "scheduler-project",
		CustomID: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.AddTask(plandb.AddTaskInput{
		Title:    "B",
		Project:  "scheduler-project",
		CustomID: "b",
		Deps:     []plandb.DepSpec{{TaskID: "a", Kind: &feed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.AddTask(plandb.AddTaskInput{
		Title:    "C",
		Project:  "scheduler-project",
		CustomID: "c",
		Deps:     []plandb.DepSpec{{TaskID: "b", Kind: &feed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return []*plandb.Task{a, b, c}
}

func TestPrioritizeReadyTasksRealPlanDBShapesYieldFIFO(t *testing.T) {
	tasks := seedPlanDBForScheduler(t)
	input := []*plandb.Task{tasks[2], tasks[0], tasks[1]}
	got := prioritizeReadyTasks(nil, "/ignored", "/ignored.db", "scheduler-project", input)
	want := []string{"c", "a", "b"}
	ids := taskIDs(got)
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("prioritized = %#v, want %#v", ids, want)
		}
	}
}

func TestFetchDepResultsOverviewShapeIsAlwaysEmpty(t *testing.T) {
	tasks := seedPlanDBForScheduler(t)
	db := plandb.GetPlanDB()
	if _, err := db.DoneTask(tasks[0].ID, plandb.DoneOpts{Result: "upstream result"}); err != nil {
		t.Fatalf("done upstream: %v", err)
	}
	got := fetchDepResults(nil, "/ignored", "/ignored.db", "scheduler-project", tasks[1].ID)
	if got.FanIn != 0 || len(got.Lines) != 0 {
		t.Fatalf("fetchDepResults = %#v", got)
	}
}

type staticPools map[ModelTier][]ModelCandidate

func (p staticPools) CandidatesForTier(tier ModelTier) []ModelCandidate {
	return append([]ModelCandidate(nil), p[tier]...)
}

type staticCapability map[string]sizeband.SizeBand

func (c staticCapability) MaxReliableBand(model capability.ModelRef, _ ...float64) sizeband.SizeBand {
	if band := c[model.ModelID]; band != "" {
		return band
	}
	return sizeband.BandM
}

type staticOutcomes []leafoutcome.LeafOutcome

func (o staticOutcomes) ReadLeafOutcomes(string) []leafoutcome.LeafOutcome {
	return append([]leafoutcome.LeafOutcome(nil), o...)
}

func TestOrderCandidatesByPlanConsumesEmptyOverviewEdges(t *testing.T) {
	tasks := seedPlanDBForScheduler(t)
	for i, task := range tasks {
		task.ParentTaskID = stringPointer("root")
		task.Status = plandb.StatusReady
		task.Description = stringPointer([]string{
			"scope: tiny\nFix one name.",
			"scope: medium\nImplement the middle.",
			"scope: large\nImplement the largest.",
		}[i])
	}
	descendants := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	ordered, assignments := orderCandidatesByPlan(
		[]*plandb.Task{tasks[2], tasks[0], tasks[1]},
		candidateOrderingOptions{
			AdaptiveCuts:   true,
			HeftEnabled:    true,
			ParallelWindow: 2,
			Workspace:      t.TempDir(),
			ProjectID:      "scheduler-project",
			AllTasks:       tasks,
			Descendants:    descendants,
			Pools: staticPools{
				ModelTierHigh: {{ID: "openrouter/high-model"}},
				ModelTierLow:  {{ID: "openrouter/low-model"}},
			},
			Tracker: staticCapability{"low-model": sizeband.BandXL},
			Outcomes: staticOutcomes{
				{
					TaskID:   "old",
					Model:    leafoutcome.LeafOutcomeModel{ModelID: "high-model"},
					SizeBand: sizeband.BandXS,
					WallMs:   800,
				},
			},
		},
	)
	if len(ordered) != 3 {
		t.Fatalf("ordered = %#v", taskIDs(ordered))
	}
	if len(assignments) == 0 {
		t.Fatal("expected at least one HEFT/slack assignment")
	}
	for _, assignment := range assignments {
		if assignment.Model.ModelID == "" {
			t.Fatalf("empty model assignment: %#v", assignment)
		}
	}
}

func TestCandidateSliceAndModelRef(t *testing.T) {
	tasks := []*plandb.Task{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := taskIDs(candidateSlice(tasks, 2)); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("slice = %#v", got)
	}
	if got := candidateSlice(tasks, 0); len(got) != 0 {
		t.Fatalf("zero slice = %#v", got)
	}
	if got := modelRefOf("openrouter/qwen/model"); got.ProviderID != "openrouter" || got.ModelID != "qwen/model" {
		t.Fatalf("modelRefOf = %#v", got)
	}
	if got := modelRefOf("/leading"); got.ProviderID != "" || got.ModelID != "/leading" {
		t.Fatalf("leading modelRefOf = %#v", got)
	}
}
