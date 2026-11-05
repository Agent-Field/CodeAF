package factory

import (
	"fmt"
	"strings"
	"time"
)

// WaitsOnMost is the most parents one job can wait on.
const WaitsOnMost = 20

// BlockWaitsOn names a block caused by a parent job.
const BlockWaitsOn = "waits_on"

// JobBlock says why a job waits for a parent to be fixed.
type JobBlock struct {
	By     string    `json:"by"`
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
	Job    int       `json:"job,omitempty"`
}

// WaitsRule is a decision over one snapshot of the parents, in waits_on order.
// A block also retains the unsatisfied parents; it never changes a job's state.
type WaitsRule struct {
	WaitingOn []int
	Blocked   *JobBlock
	Released  bool
}

// WaitsOverridden is permanent evidence that a person has started the job.
func WaitsOverridden(it Item) bool {
	return it.Stream != nil || StoppedByPerson(it) || JobState(it) != "backlog"
}

// WaitsPending is the guard for automatic intake. Only a scan releases waits.
func WaitsPending(it Item) bool {
	return len(it.WaitsOn) != 0 && !it.WaitsReleased && !WaitsOverridden(it)
}

// CheckWaitsIDs refuses invalid parent lists before either wire sends a write.
func CheckWaitsIDs(ids []int) error {
	if len(ids) > WaitsOnMost {
		return fmt.Errorf("a job can wait on at most %d jobs", WaitsOnMost)
	}
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("waits_on must contain positive job ids")
		}
		if seen[id] {
			return fmt.Errorf("waits_on must contain distinct job ids")
		}
		seen[id] = true
	}
	return nil
}

// EvaluateWaits reads only the child and the given parents. It writes nothing.
func EvaluateWaits(it Item, parents map[int]Item) WaitsRule {
	if it.WaitsReleased || WaitsOverridden(it) {
		return WaitsRule{Released: true}
	}
	var out WaitsRule
	for _, id := range it.WaitsOn {
		parent, ok := parents[id]
		if ok && WaitsSatisfied(parent) {
			continue
		}
		out.WaitingOn = append(out.WaitingOn, id)
		why := ""
		if !ok {
			parent.ID = id
			why = "is no longer available"
		} else {
			why = WaitsEndedReason(parent)
			if why == "" && !StoppedByPerson(parent) {
				why = waitsFailureReason(parent)
			}
		}
		if why != "" && out.Blocked == nil {
			out.Blocked = &JobBlock{By: BlockWaitsOn, Job: id, Reason: "waits on " + JobRef(parent) + ", which " + why}
		}
	}
	out.Released = len(out.WaitingOn) == 0
	return out
}

// WaitsSatisfied is a merge, or a shipped job with nothing to merge.
func WaitsSatisfied(it Item) bool {
	if it.PullRequest != nil {
		return it.PullRequest.State == PRMerged
	}
	return it.State == StateShipped && WaitsEndedReason(it) == "" && waitsFailureReason(it) == ""
}

// WaitsEndedReason is why a parent ended without satisfying its children.
// A failed job can be retried, so failure alone never refuses a new wait.
func WaitsEndedReason(it Item) string {
	if it.PullRequest != nil && it.PullRequest.State == PRMerged {
		return ""
	}
	switch it.DoneHow {
	case "dropped":
		return "was dropped"
	case "closed_locally":
		return "was closed locally"
	}
	if it.PullRequest != nil && it.PullRequest.State == PRClosed {
		return "closed without merging"
	}
	if it.State == StateDismissed {
		return "was dismissed"
	}
	if it.DoneHow != "" && it.State != StateShipped {
		return "closed without merging"
	}
	return ""
}

func waitsFailureReason(it Item) string {
	if f := it.Failure; f != nil && f.Code != CodeStopped {
		return waitsFailedStep(it, f.Step)
	}
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.State == PhaseFailed && (ph.Failure == nil || ph.Failure.Code != CodeStopped) {
				return waitsFailedStep(it, ph.Name)
			}
		}
	}
	if it.PublishError != "" || it.PublishStatus != nil && it.PublishStatus.State == "failed" {
		return "failed to publish"
	}
	if it.Merging != nil && it.Merging.State == MergeFailed {
		return "failed to merge"
	}
	return ""
}

func waitsFailedStep(it Item, name string) string {
	for _, step := range it.Stages {
		if step.Name == name && strings.TrimSpace(step.Label) != "" {
			name = step.Label
			break
		}
	}
	if name != "" {
		return "failed at " + name
	}
	return "failed"
}
