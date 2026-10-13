package runs

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func materialize(steps []*spec.Step, prefix string, job *job) []bridge.StepState {
	var out []bridge.StepState
	for _, step := range steps {
		path := prefix + "/" + step.ID
		if step.Loop != nil {
			out = append(out, loopState(step, path, job))
			continue
		}
		if rec, ok := job.Done[path]; ok {
			out = append(out, fromRec(step, rec))
			continue
		}
		if job.WaitPath == path {
			out = append(out, bridge.StepState{
				ID: step.ID, Title: step.Title, Status: bridge.RunWaiting, Approval: job.ApprovalID,
			})
		}
	}
	return out
}

func loopState(step *spec.Step, path string, job *job) bridge.StepState {
	lp := job.Loops[path]
	state := bridge.StepState{ID: step.ID, Title: step.Title, Status: bridge.RunRunning}
	if _, ok := job.Done[path]; ok {
		state.Status = bridge.RunDone
	}
	if job.State.Status == bridge.RunNeedsYou || job.State.Status == bridge.RunStopped {
		if _, ok := job.Done[path]; !ok {
			state.Status = job.State.Status
		}
	}
	n := roundsToShow(job, path, lp)
	for i := 0; i < n; i++ {
		children := materialize(step.Steps, path+"#"+strconv.Itoa(i), job)
		status := bridge.RunDone
		if len(children) == 0 {
			status = bridge.RunRunning
		}
		for _, child := range children {
			if child.Status != bridge.RunDone {
				status = child.Status
			}
		}
		state.Rounds = append(state.Rounds, bridge.StepState{ID: strconv.Itoa(i + 1), Status: status, Rounds: children})
	}
	if state.Status == bridge.RunRunning {
		for _, round := range state.Rounds {
			if round.Status == bridge.RunWaiting || round.Status == bridge.RunNeedsYou {
				state.Status = round.Status
			}
		}
	}
	return state
}

func roundsToShow(job *job, path string, lp loopRec) int {
	n := lp.Index
	cur := path + "#" + strconv.Itoa(lp.Index)
	if strings.HasPrefix(job.WaitPath, cur) || hasDonePrefix(job, cur+"/") {
		n++
	}
	if n < 0 {
		return 0
	}
	return n
}

func hasDonePrefix(job *job, prefix string) bool {
	for path := range job.Done {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func fromRec(step *spec.Step, rec stepRec) bridge.StepState {
	return bridge.StepState{
		ID: step.ID, Title: first(rec.Title, step.Title), Status: rec.Status,
		AI: rec.AI, Output: rec.Output, Error: rec.Error, Approval: rec.Approval,
	}
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
