package plannertranslate

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
)

// ApplyTranslatedDAGInput mirrors planner-translate.ts:641-647.
type ApplyTranslatedDAGInput struct {
	DAG        DAGData
	Workspace  string
	DBPath     string
	ProjectID  string
	RootTaskID string
}

// ApplyTranslatedDAGResult mirrors planner-translate.ts:649-660.
type ApplyTranslatedDAGResult struct {
	TaskCount       int      `json:"taskCount"`
	EdgeCount       int      `json:"edgeCount"`
	InsertedTaskIDs []string `json:"insertedTaskIDs"`
	OK              bool     `json:"ok"`
	Reason          *string  `json:"reason,omitempty"`
}

// PlanDBRunner is the minimal PlanDB side-effect seam.
type PlanDBRunner interface {
	Run(argv []string) plandb.RunResult
}

// PlanDBRunnerFunc adapts a function to PlanDBRunner.
type PlanDBRunnerFunc func(argv []string) plandb.RunResult

// Run implements PlanDBRunner.
func (function PlanDBRunnerFunc) Run(argv []string) plandb.RunResult {
	return function(argv)
}

type nativePlanDBRunner struct{}

func (nativePlanDBRunner) Run(argv []string) plandb.RunResult {
	return plandb.RunPlanDB(argv)
}

// ApplyTranslatedDAG applies through the process-global native PlanDB bridge.
func ApplyTranslatedDAG(input ApplyTranslatedDAGInput) ApplyTranslatedDAGResult {
	return ApplyTranslatedDAGWithRunner(input, nativePlanDBRunner{})
}

// ApplyTranslatedDAGWithRunner is the I/O-seamed form used by scheduler
// adapters and shell tests.
func ApplyTranslatedDAGWithRunner(
	input ApplyTranslatedDAGInput,
	runner PlanDBRunner,
) ApplyTranslatedDAGResult {
	tasks := input.DAG.Tasks
	residual := residualContent(input.DAG)
	persistResidual := func() {
		if residual == "" {
			return
		}
		_ = runner.Run([]string{
			"plandb", "context", residual, "--kind", "residual",
			"--task", input.RootTaskID, "--json",
		})
	}

	if len(tasks) == 0 {
		if residual != "" {
			persistResidual()
			reason := "frontier tranche is empty; residual preserved"
			return ApplyTranslatedDAGResult{
				TaskCount: 0, EdgeCount: 0, InsertedTaskIDs: []string{},
				OK: true, Reason: &reason,
			}
		}
		reason := "DAG had zero tasks — planner-translate fallback or empty input."
		return ApplyTranslatedDAGResult{
			TaskCount: 0, EdgeCount: 0, InsertedTaskIDs: []string{},
			OK: false, Reason: &reason,
		}
	}

	keySet := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		keySet[task.TaskKey] = true
	}
	dangling := []string{}
	for _, task := range tasks {
		for _, dep := range task.Deps {
			if !keySet[dep.FromTask] {
				dangling = append(dangling,
					task.TaskKey+" → "+dep.FromTask+" (no such taskKey)")
			}
		}
	}
	if len(dangling) > 0 {
		sample := dangling
		if len(sample) > 5 {
			sample = sample[:5]
		}
		reason := "DAG has " + itoa(len(dangling)) +
			" dangling edge(s): " + strings.Join(sample, "; ")
		return ApplyTranslatedDAGResult{
			TaskCount: 0, EdgeCount: 0, InsertedTaskIDs: []string{},
			OK: false, Reason: &reason,
		}
	}

	inDegree := make(map[string]int, len(tasks))
	adjacency := make(map[string][]string, len(tasks))
	byKey := make(map[string]DAGTask, len(tasks))
	for _, task := range tasks {
		byKey[task.TaskKey] = task
		inDegree[task.TaskKey] = 0
		adjacency[task.TaskKey] = []string{}
	}
	for _, task := range tasks {
		for _, dep := range task.Deps {
			adjacency[dep.FromTask] = append(adjacency[dep.FromTask], task.TaskKey)
			inDegree[task.TaskKey]++
		}
	}
	queue := []string{}
	for _, task := range tasks {
		if inDegree[task.TaskKey] == 0 {
			queue = append(queue, task.TaskKey)
		}
	}
	order := make([]DAGTask, 0, len(tasks))
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		task, exists := byKey[key]
		if exists {
			order = append(order, task)
		}
		for _, next := range adjacency[key] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(order) != len(tasks) {
		reason := "topo-sort: cycle detected (resolved " + itoa(len(order)) +
			"/" + itoa(len(tasks)) + " tasks)"
		return ApplyTranslatedDAGResult{
			TaskCount: 0, EdgeCount: 0, InsertedTaskIDs: []string{},
			OK: false, Reason: &reason,
		}
	}

	keyToID := map[string]string{}
	insertedIDs := []string{}
	failures := []applyFailure{}
	for _, task := range order {
		argv := []string{
			"plandb", "add", task.Title, "--json", "--kind", task.Kind,
			"--parent", input.RootTaskID, "--description", task.Description,
		}
		for _, tag := range task.Tags {
			argv = append(argv, "--tag", tag)
		}
		for _, dep := range task.Deps {
			upstreamID := keyToID[dep.FromTask]
			if upstreamID == "" {
				failures = append(failures, applyFailure{
					taskKey: task.TaskKey,
					reason:  "internal: dep " + dep.FromTask + " not yet inserted (topo bug)",
				})
				continue
			}
			kind := dep.Kind
			if kind == "" {
				kind = "feeds_into"
			}
			argv = append(argv, "--dep", upstreamID+":"+kind)
		}

		result := runner.Run(argv)
		if result.Code != 0 {
			stderr := strings.TrimSpace(string(result.Stderr))
			failures = append(failures, applyFailure{
				taskKey: task.TaskKey,
				reason:  "exit " + itoa(result.Code) + ": " + sliceBytes(stderr, 200),
			})
			continue
		}
		stdout := strings.TrimSpace(string(result.Stdout))
		var row map[string]json.RawMessage
		if json.Unmarshal([]byte(stdout), &row) != nil {
			failures = append(failures, applyFailure{
				taskKey: task.TaskKey,
				reason:  "add returned non-JSON: " + sliceBytes(stdout, 120),
			})
			continue
		}
		var id string
		_ = json.Unmarshal(row["id"], &id)
		if id == "" {
			failures = append(failures, applyFailure{
				taskKey: task.TaskKey,
				reason:  "add returned no id: " + sliceBytes(stdout, 120),
			})
			continue
		}
		keyToID[task.TaskKey] = id
		insertedIDs = append(insertedIDs, id)
	}

	totalEdges := 0
	for _, task := range tasks {
		totalEdges += len(task.Deps)
	}
	if len(insertedIDs) == 0 {
		firstReason := "unknown"
		if len(failures) > 0 {
			firstReason = failures[0].reason
		}
		reason := "every plandb add failed; first error: " + firstReason
		return ApplyTranslatedDAGResult{
			TaskCount: 0, EdgeCount: 0, InsertedTaskIDs: []string{},
			OK: false, Reason: &reason,
		}
	}

	persistResidual()
	result := ApplyTranslatedDAGResult{
		TaskCount: len(insertedIDs), EdgeCount: totalEdges,
		InsertedTaskIDs: insertedIDs, OK: true,
	}
	if len(failures) > 0 {
		reason := "partial: " + itoa(len(failures)) + "/" +
			itoa(len(tasks)) + " tasks failed to insert"
		result.Reason = &reason
	}
	return result
}

type applyFailure struct {
	taskKey string
	reason  string
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var out [32]byte
	index := len(out)
	for value > 0 {
		index--
		out[index] = digits[value%10]
		value /= 10
	}
	if negative {
		index--
		out[index] = '-'
	}
	return string(out[index:])
}

// String.slice is UTF-16 in the source; all current stderr/stdout cuts are
// ASCII in normal operation. This helper keeps the byte path for malformed
// UTF-8 process output, matching Buffer.toString's already-decoded string.
func sliceBytes(value string, maximum int) string {
	return sliceUTF16(value, 0, maximum)
}
