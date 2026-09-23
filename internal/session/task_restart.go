package session

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// Restart data preserves a runner's inputs even after its result has settled.
// Older checkpoints remain readable, but missing inputs are never guessed.
type taskRestartRecord struct {
	Goal   string          `json:"goal,omitempty"`
	Name   string          `json:"name,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Why    string          `json:"why,omitempty"`
	Model  string          `json:"model,omitempty"`
	Effort string          `json:"effort,omitempty"`
}

func taskRestartLocked(spec taskSpec) *taskRestartRecord {
	if spec.design != nil {
		return &taskRestartRecord{Goal: spec.design.goal, Model: spec.design.model, Effort: string(spec.design.effort)}
	}
	if spec.run != nil {
		return &taskRestartRecord{Name: spec.run.name, Input: append(json.RawMessage(nil), spec.run.input...), Why: spec.run.why, Model: spec.run.model}
	}
	return nil
}

func restoreTaskRestart(node *TaskNode, record *taskRestartRecord) {
	if record == nil {
		return
	}
	switch node.kind {
	case TaskKindHarness:
		if node.spec.design == nil {
			effort, _ := provider.ParseEffort(record.Effort)
			node.spec.design = &harnessDesignSpec{goal: record.Goal, model: record.Model, effort: effort}
		}
	case TaskKindSubharness:
		node.spec.run = &subharnessRunSpec{name: record.Name, input: append(json.RawMessage(nil), record.Input...), why: record.Why, model: record.Model}
	}
}

// Retry needs a runner's original inputs; summary rows cannot supply them.
func (node *TaskNode) savedRunnerLocked() bool {
	switch node.kind {
	case TaskKindHarness:
		return node.spec.design != nil
	case TaskKindSubharness:
		return node.spec.run != nil
	case TaskKindQuick:
		return node.spec.quick != nil
	case TaskKindJob, TaskKindAdaptive:
		return false
	default:
		return true
	}
}

// Eligibility is checked under the same lock that admits the new attempt.
func (node *TaskNode) reopenErrorLocked(retry bool) error {
	if retry {
		if node.state != TaskFailed {
			return fmt.Errorf("task %d is not incomplete", node.id)
		}
		if !node.savedRunnerLocked() {
			return fmt.Errorf("task %d has no saved restart instructions; start it again from its original request", node.id)
		}
		return nil
	}
	switch node.kind {
	case TaskKindHarness, TaskKindSubharness, TaskKindQuick:
		return fmt.Errorf("task %d is %s, not a run that can be continued", node.id, TaskKindWord(node.kind))
	}
	switch node.state {
	case TaskRunning:
		return fmt.Errorf("task %d is still running", node.id)
	case TaskQueued:
		return fmt.Errorf("task %d has not started yet", node.id)
	case TaskDone, TaskFailed, TaskUnverified:
		return nil
	default:
		return fmt.Errorf("task %d is %s, not a task that has ended", node.id, node.state)
	}
}
