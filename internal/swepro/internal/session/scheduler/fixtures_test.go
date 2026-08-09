package scheduler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/capability"
	"github.com/Agent-Field/swe-pro-go/internal/session/cutpolicy"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

type schedulerFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadSchedulerFixtures(t *testing.T) []schedulerFixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()
	fixtures := []schedulerFixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<23)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var fixture schedulerFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}

func decodeFixtureArgs(t *testing.T, fixture schedulerFixture) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	return args
}

func decodeFixtureTask(t *testing.T, raw json.RawMessage) *plandb.Task {
	t.Helper()
	var task *plandb.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task
}

type scanFixtureOutput struct {
	CandidateIDs  []string      `json:"candidateIDs"`
	Skipped       []SkippedTask `json:"skipped"`
	DescendantIDs []string      `json:"descendantIDs"`
	Scanned       int           `json:"scanned"`
}

type orderFixtureInput struct {
	Tasks          []*plandb.Task               `json:"tasks"`
	Prioritized    []string                     `json:"prioritized"`
	HighPool       []string                     `json:"highPool"`
	LowPool        []string                     `json:"lowPool"`
	ReliableBands  map[string]sizeband.SizeBand `json:"reliableBands"`
	Outcomes       []leafoutcome.LeafOutcome    `json:"outcomes"`
	ParallelWindow float64                      `json:"parallelWindow"`
	HeftEnabled    bool                         `json:"heftEnabled"`
}

type orderFixtureOutput struct {
	Ordered     []string          `json:"ordered"`
	Assignments []ModelAssignment `json:"assignments"`
}

type fixtureOverviewRunner struct{}

func (fixtureOverviewRunner) Run([]string) plandb.RunResult {
	return plandb.RunResult{
		Code:   0,
		Stdout: []byte(`{"project":null,"status":{},"recent":[]}`),
		Stderr: []byte{},
	}
}

type fixturePools map[ModelTier][]ModelCandidate

func (p fixturePools) CandidatesForTier(tier ModelTier) []ModelCandidate {
	return append([]ModelCandidate(nil), p[tier]...)
}

type fixtureCapability map[string]sizeband.SizeBand

func (c fixtureCapability) MaxReliableBand(model capability.ModelRef, _ ...float64) sizeband.SizeBand {
	if band := c[model.ModelID]; band != "" {
		return band
	}
	return sizeband.BandM
}

type fixtureOutcomes []leafoutcome.LeafOutcome

func (o fixtureOutcomes) ReadLeafOutcomes(string) []leafoutcome.LeafOutcome {
	return append([]leafoutcome.LeafOutcome(nil), o...)
}

func callSchedulerFixture(t *testing.T, fixture schedulerFixture) any {
	t.Helper()
	args := decodeFixtureArgs(t, fixture)
	switch fixture.Fn {
	case "hasTag":
		var tags any
		if err := json.Unmarshal(args[0], &tags); err != nil {
			t.Fatalf("decode tags: %v", err)
		}
		var name string
		if err := json.Unmarshal(args[1], &name); err != nil {
			t.Fatalf("decode tag name: %v", err)
		}
		return hasOwnTagProperty(tags, name)

	case "suggestedAgent":
		return suggestedAgent(decodeFixtureTask(t, args[0]))

	case "isAutoDispatchable":
		return isAutoDispatchable(decodeFixtureTask(t, args[0]))

	case "scanCandidates":
		var root string
		var ready, all []*plandb.Task
		if err := json.Unmarshal(args[0], &root); err != nil {
			t.Fatalf("decode root: %v", err)
		}
		if err := json.Unmarshal(args[1], &ready); err != nil {
			t.Fatalf("decode ready: %v", err)
		}
		if err := json.Unmarshal(args[2], &all); err != nil {
			t.Fatalf("decode all: %v", err)
		}
		scan := scanCandidates(root, ready, all)
		return scanFixtureOutput{
			CandidateIDs:  taskIDs(scan.Candidates),
			Skipped:       scan.Skipped,
			DescendantIDs: scan.DescendantIDs,
			Scanned:       scan.Scanned,
		}

	case "prioritizeRealShapes":
		var ids []string
		if err := json.Unmarshal(args[0], &ids); err != nil {
			t.Fatalf("decode ids: %v", err)
		}
		tasks := seedPlanDBForScheduler(t)
		byID := map[string]*plandb.Task{}
		for _, task := range tasks {
			byID[task.ID] = task
		}
		candidates := make([]*plandb.Task, 0, len(ids))
		for _, id := range ids {
			candidates = append(candidates, byID[id])
		}
		return taskIDs(prioritizeReadyTasks(
			nil,
			"/ignored",
			"/ignored.db",
			"scheduler-project",
			candidates,
		))

	case "fetchDepResultsRealOverview":
		var taskID string
		if err := json.Unmarshal(args[0], &taskID); err != nil {
			t.Fatalf("decode task id: %v", err)
		}
		tasks := seedPlanDBForScheduler(t)
		if _, err := plandb.GetPlanDB().DoneTask(tasks[0].ID, plandb.DoneOpts{Result: "upstream result"}); err != nil {
			t.Fatalf("done upstream: %v", err)
		}
		return fetchDepResults(nil, "/ignored", "/ignored.db", "scheduler-project", taskID)

	case "reapStaleRunningTasks":
		restore := plandb.SetClockForTesting(func() int64 { return 1_000 })
		t.Cleanup(restore)
		tasks := seedPlanDBForScheduler(t)
		db := plandb.GetPlanDB()
		if db.ClaimTask(tasks[0].ID, "scheduler:test") == nil {
			t.Fatal("claim fixture task")
		}
		if db.StartTask(tasks[0].ID) == nil {
			t.Fatal("start fixture task")
		}
		return reapStaleRunningTasks(
			nil,
			"/ignored",
			"/ignored.db",
			"scheduler-project",
			time.Hour,
			time.UnixMilli(7_201_000),
		)

	case "modelRefOf":
		var fullID string
		if err := json.Unmarshal(args[0], &fullID); err != nil {
			t.Fatalf("decode model id: %v", err)
		}
		return modelRefOf(fullID)

	case "taskBandIndex":
		var task map[string]any
		decoder := json.NewDecoder(bytes.NewReader(args[0]))
		decoder.UseNumber()
		if err := decoder.Decode(&task); err != nil {
			t.Fatalf("decode band task: %v", err)
		}
		description, _ := task["description"].(string)
		tags := []string{}
		if values, ok := task["tags"].([]any); ok {
			for _, value := range values {
				if tag, ok := value.(string); ok {
					tags = append(tags, tag)
				}
			}
		}
		band := sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
			Description: description,
			Tags:        tags,
		})
		return cutpolicy.BandToIndex(band)

	case "maxParallelSlice":
		var ids []string
		var capacity float64
		if err := json.Unmarshal(args[0], &ids); err != nil {
			t.Fatalf("decode ids: %v", err)
		}
		if err := json.Unmarshal(args[1], &capacity); err != nil {
			t.Fatalf("decode capacity: %v", err)
		}
		tasks := make([]*plandb.Task, 0, len(ids))
		for _, id := range ids {
			tasks = append(tasks, &plandb.Task{ID: id})
		}
		return taskIDs(candidateSlice(tasks, capacity))

	case "formatSchedulerSummary":
		var result SchedulerCycleResult
		if err := json.Unmarshal(args[0], &result); err != nil {
			t.Fatalf("decode scheduler result: %v", err)
		}
		return FormatSchedulerSummary(result)

	case "orderPlan":
		var input orderFixtureInput
		if err := json.Unmarshal(args[0], &input); err != nil {
			t.Fatalf("decode order input: %v", err)
		}
		byID := map[string]*plandb.Task{}
		descendants := map[string]struct{}{}
		for _, task := range input.Tasks {
			byID[task.ID] = task
			descendants[task.ID] = struct{}{}
		}
		prioritized := make([]*plandb.Task, 0, len(input.Prioritized))
		for _, id := range input.Prioritized {
			prioritized = append(prioritized, byID[id])
		}
		pools := fixturePools{
			ModelTierHigh: make([]ModelCandidate, 0, len(input.HighPool)),
			ModelTierLow:  make([]ModelCandidate, 0, len(input.LowPool)),
		}
		for _, id := range input.HighPool {
			pools[ModelTierHigh] = append(pools[ModelTierHigh], ModelCandidate{ID: id})
		}
		for _, id := range input.LowPool {
			pools[ModelTierLow] = append(pools[ModelTierLow], ModelCandidate{ID: id})
		}
		ordered, assignments := orderCandidatesByPlan(prioritized, candidateOrderingOptions{
			AdaptiveCuts:   true,
			HeftEnabled:    input.HeftEnabled,
			ParallelWindow: input.ParallelWindow,
			Workspace:      "/fixture",
			ProjectID:      "scheduler-fixtures",
			AllTasks:       input.Tasks,
			Descendants:    descendants,
			PlanDB:         fixtureOverviewRunner{},
			Pools:          pools,
			Tracker:        fixtureCapability(input.ReliableBands),
			Outcomes:       fixtureOutcomes(input.Outcomes),
		})
		return orderFixtureOutput{Ordered: taskIDs(ordered), Assignments: assignments}

	default:
		t.Fatalf("unknown fixture fn %q", fixture.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadSchedulerFixtures(t)
	if len(fixtures) < 50 {
		t.Fatalf("expected at least 50 fixtures, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			got := callSchedulerFixture(t, fixture)
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fixture.ArgsJSON, encoded, fixture.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fixture := range loadSchedulerFixtures(t) {
		seen[fixture.Fn]++
	}
	for _, fn := range []string{
		"hasTag",
		"suggestedAgent",
		"isAutoDispatchable",
		"scanCandidates",
		"prioritizeRealShapes",
		"fetchDepResultsRealOverview",
		"reapStaleRunningTasks",
		"modelRefOf",
		"taskBandIndex",
		"maxParallelSlice",
		"formatSchedulerSummary",
		"orderPlan",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}
