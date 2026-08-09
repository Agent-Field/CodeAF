package criticalpath

import (
	"errors"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

// Translation of src/session/critical-path.test.ts. Subtest names are verbatim
// from the bun `describe`/`test` blocks.

func task(id string) *plandb.Task {
	return &plandb.Task{
		ID:           id,
		ProjectID:    "p",
		ParentTaskID: nil,
		IsComposite:  false,
		Title:        id,
		Description:  nil,
		Status:       "pending",
		Kind:         "code",
		Priority:     0,
		AgentID:      nil,
		ClaimedAt:    nil,
		StartedAt:    nil,
		CompletedAt:  nil,
		Result:       nil,
		Error:        nil,
		Files:        nil,
		Metadata:     nil,
		Tags:         []string{},
		CreatedAt:    0,
		UpdatedAt:    0,
	}
}

func edge(fromTask, toTask string) plandb.Dependency {
	return plandb.Dependency{FromTask: fromTask, ToTask: toTask, Kind: "blocks"}
}

// analyze mirrors the test helper: durations is looked up by task id, and a
// missing entry produces undefined on the TS side.
func analyze(t *testing.T, ids []string, edges []plandb.Dependency, durations map[string]float64) (CriticalPathAnalysis, error) {
	t.Helper()
	tasks := make([]*plandb.Task, len(ids))
	for i, id := range ids {
		tasks[i] = task(id)
	}
	return AnalyzeCriticalPath(tasks, edges, func(candidate *plandb.Task) float64 {
		return durations[candidate.ID]
	})
}

// expectAnalysis is the toEqual(...) equivalent: deep equality over the whole
// result, including an exact slackByTask key set.
func expectAnalysis(t *testing.T, got CriticalPathAnalysis, wantCritical []string, wantSlack map[string]float64, wantMakespan float64) {
	t.Helper()

	if len(got.CriticalIDs) != len(wantCritical) {
		t.Fatalf("criticalIds = %v, want %v", got.CriticalIDs, wantCritical)
	}
	for i := range wantCritical {
		if got.CriticalIDs[i] != wantCritical[i] {
			t.Fatalf("criticalIds = %v, want %v", got.CriticalIDs, wantCritical)
		}
	}

	if got.SlackByTask.Len() != len(wantSlack) {
		t.Fatalf("slackByTask has %d keys, want %d (%v)", got.SlackByTask.Len(), len(wantSlack), got.SlackByTask.Keys())
	}
	for id, want := range wantSlack {
		have, ok := got.SlackByTask.Get(id)
		if !ok {
			t.Fatalf("slackByTask missing %q", id)
		}
		if have != want {
			t.Fatalf("slackByTask[%q] = %v, want %v", id, have, want)
		}
	}

	if got.MakespanEstimate != wantMakespan {
		t.Fatalf("makespanEstimate = %v, want %v", got.MakespanEstimate, wantMakespan)
	}
}

func TestAnalyzeCriticalPath(t *testing.T) {
	t.Run("computes exact slack for a diamond DAG", func(t *testing.T) {
		result, err := analyze(t,
			[]string{"a", "b", "c", "d"},
			[]plandb.Dependency{edge("a", "b"), edge("a", "c"), edge("b", "d"), edge("c", "d")},
			map[string]float64{"a": 2, "b": 4, "c": 1, "d": 3},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectAnalysis(t, result,
			[]string{"a", "b", "d"},
			map[string]float64{"a": 0, "b": 0, "c": 3, "d": 0},
			9,
		)
	})

	t.Run("marks every task critical in a chain", func(t *testing.T) {
		result, err := analyze(t,
			[]string{"a", "b", "c"},
			[]plandb.Dependency{edge("a", "b"), edge("b", "c")},
			map[string]float64{"a": 2, "b": 3, "c": 5},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectAnalysis(t, result,
			[]string{"a", "b", "c"},
			map[string]float64{"a": 0, "b": 0, "c": 0},
			10,
		)
	})

	t.Run("computes exact slack for a wide fan-out/fan-in DAG", func(t *testing.T) {
		result, err := analyze(t,
			[]string{"root", "long", "medium", "short", "join"},
			[]plandb.Dependency{
				edge("root", "long"),
				edge("root", "medium"),
				edge("root", "short"),
				edge("long", "join"),
				edge("medium", "join"),
				edge("short", "join"),
			},
			map[string]float64{"root": 1, "long": 5, "medium": 3, "short": 2, "join": 1},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectAnalysis(t, result,
			[]string{"root", "long", "join"},
			map[string]float64{"root": 0, "long": 0, "medium": 2, "short": 3, "join": 0},
			7,
		)
	})

	t.Run("gives shorter disconnected components project-level slack and preserves critical ties", func(t *testing.T) {
		result, err := analyze(t,
			[]string{"left", "right", "spare"},
			nil,
			map[string]float64{"left": 5, "right": 5, "spare": 2},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectAnalysis(t, result,
			[]string{"left", "right"},
			map[string]float64{"left": 0, "right": 0, "spare": 3},
			5,
		)
	})

	t.Run("rejects cycles with a typed error", func(t *testing.T) {
		_, err := analyze(t,
			[]string{"a", "b", "c"},
			[]plandb.Dependency{edge("a", "b"), edge("b", "c"), edge("c", "a")},
			map[string]float64{"a": 1, "b": 1, "c": 1},
		)
		var cycleErr *CriticalPathCycleError
		if !errors.As(err, &cycleErr) {
			t.Fatalf("expected CriticalPathCycleError, got %#v", err)
		}
	})
}
