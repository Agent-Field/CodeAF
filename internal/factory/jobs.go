package factory

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// DefaultJobBudget is the pipeline default when no file supplies one.
const DefaultJobBudget = 3.0

// ApprovalUndo is the interval during which consent has no outward effect.
const ApprovalUndo = 5 * time.Second

// DroppedBranchRetention is how long a dropped job keeps its branch.
const DroppedBranchRetention = 7 * 24 * time.Hour

// JobPriority records a person's choice; defaults are derived from labels.
type JobPriority struct {
	Value string `json:"value"`
	SetBy string `json:"set_by,omitempty"`
}

// Approval preserves consent across server restarts without storing credentials.
type Approval struct {
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	Matched   string    `json:"matched,omitempty"`
	DecidedBy string    `json:"decided_by,omitempty"`
	At        time.Time `json:"at"`
	UndoUntil time.Time `json:"undo_until"`
	Commit    string    `json:"commit,omitempty"`
	Words     string    `json:"words,omitempty"`
	Edited    bool      `json:"edited,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// StopUndo retains the run that Stop interrupted; a resumed run gets a new lease.
type StopUndo struct {
	Failure  *Failure  `json:"failure,omitempty"`
	By       string    `json:"by"`
	Until    time.Time `json:"until"`
	State    State     `json:"state"`
	Question string    `json:"question,omitempty"`
	QKind    string    `json:"qkind,omitempty"`
	Stream   *Stream   `json:"stream,omitempty"`
}

// JobRef is the stable human reference; the numeric document id remains compatible.
func JobRef(it Item) string {
	repo := it.Repo
	if n := strings.LastIndex(repo, "/"); n >= 0 {
		repo = repo[n+1:]
	}
	number := it.Num
	if number == 0 {
		number = it.ID
	}
	return fmt.Sprintf("%s#%d", repo, number)
}

func ValidPriority(value string) bool {
	return slices.Contains([]string{"urgent", "high", "medium", "low"}, value)
}

// PriorityFor never consults AI ranking: a person's persisted mark wins over
// priority:* and p0..p3 issue labels. A "bug" label is a type, not a priority,
// so it yields no mark: priority is only ever something a person chose.
func PriorityFor(it Item) *JobPriority {
	if it.Priority != nil {
		p := *it.Priority
		return &p
	}
	labels := make(map[string]bool)
	for _, label := range it.Labels {
		labels[strings.ToLower(strings.TrimSpace(label))] = true
	}
	for _, p := range []struct{ value, label, short string }{
		{"urgent", "priority:urgent", "p0"}, {"high", "priority:high", "p1"},
		{"medium", "priority:medium", "p2"}, {"low", "priority:low", "p3"},
	} {
		if labels[p.label] || labels[p.short] {
			return &JobPriority{Value: p.value}
		}
	}
	return nil
}

// JobSize comes only from the issue's explicit size label.
func JobSize(it Item) string {
	for _, label := range it.Labels {
		prefix, size, ok := strings.Cut(strings.ToLower(strings.TrimSpace(label)), ":")
		if ok && prefix == "size" && slices.Contains([]string{"xs", "s", "m", "l", "xl"}, size) {
			return strings.ToUpper(size)
		}
	}
	return ""
}

func IsDismissed(it Item, person string) bool {
	person = NormalizeHandle(person)
	return person != "" && slices.Contains(it.DismissedBy, person)
}

// JobState projects historical engine states into the five canvas states.
func JobState(it Item) string {
	if it.DoneHow != "" || it.State == StateDismissed {
		return "done"
	}
	if it.PublishError != "" || it.Failure != nil || (it.PublishStatus != nil && it.PublishStatus.State == "failed") {
		return "needs_you"
	}
	if it.State == StateShipped {
		if pr := it.PullRequest; pr != nil {
			if pr.State == PRMerged || pr.State == PRClosed {
				return "done"
			}
			if pr.ReviewDecision == "changes_requested" || pr.Conflict {
				return "needs_you"
			}
			return "running"
		}
		return "done"
	}
	if it.Publishing {
		return "running"
	}
	switch it.State {
	case StateNeedsYou, StateLanded:
		return "needs_you"
	case StateRunning:
		return "running"
	case StateQueued:
		return "queued"
	default:
		return "backlog"
	}
}

func JobDoneHow(it Item) string {
	if it.DoneHow != "" {
		return it.DoneHow
	}
	if it.State == StateDismissed {
		return "dropped"
	}
	if it.State == StateShipped {
		if it.PullRequest == nil {
			return "answered"
		}
		switch it.PullRequest.State {
		case PRMerged:
			return "merged"
		case PRClosed:
			return "closed"
		}
	}
	return ""
}

// JobReason is a finite vocabulary for Needs you and Running.
func JobReason(it Item) string {
	if JobState(it) == "needs_you" {
		if it.PublishError != "" || (it.PublishStatus != nil && it.PublishStatus.State == "failed") {
			return "publish_failed"
		}
		if it.Failure != nil {
			if it.Failure.Code == CodeMachineLost {
				return "runner_offline"
			}
			return "failed"
		}
		if it.PullRequest != nil && it.PullRequest.Conflict {
			return "conflict"
		}
		if it.PullRequest != nil && it.PullRequest.ReviewDecision == "changes_requested" {
			return "changes_requested"
		}
		if it.QKind == QKindAskFirst {
			return "approval_start"
		}
		switch NeedsKind(it) {
		case NeedsRunnerOffline:
			return "runner_offline"
		case NeedsApprovePlan:
			return "approval_plan"
		case NeedsApproveProof:
			return "approval_proof"
		case NeedsSpendingLimit:
			return "budget"
		case NeedsOutOfAttempts:
			return "out_of_rounds"
		case NeedsPublishFailed:
			return "publish_failed"
		default:
			return "question"
		}
	}
	if JobState(it) == "running" {
		if it.Publishing {
			return "publishing"
		}
		if it.PullRequest != nil {
			return "github"
		}
		return "step"
	}
	return ""
}

func JobApproval(it Item) *Approval {

	kind := ""
	switch JobReason(it) {
	case "approval_start":
		kind = "start"
	case "approval_plan":
		kind = "plan"
	case "approval_proof":
		kind = "proof"
	}
	// A plan approval that settled before the question now asked is the old
	// consent for an old question; the new ask needs its own approval.
	settledBefore := kind == "plan" && it.Approval != nil && it.Approval.Kind == "plan" && it.Approval.State == "approved" && it.Asking != nil && it.Asking.At.After(it.Approval.At)
	if it.Approval != nil && !settledBefore && (kind == "" || it.Approval.Kind == kind) {
		a := *it.Approval
		return &a
	}
	if kind == "" {
		return nil
	}
	return &Approval{Kind: kind, State: "waiting", Matched: it.AskedFirstReason}
}

// Overlaps is derived from actual branch changes, never from planned paths.
func Overlaps(a, b Item) bool {
	if a.ID == b.ID || a.Repo != b.Repo || a.Product != b.Product || JobState(a) != "running" || JobState(b) != "running" {
		return false
	}
	files := map[string]bool{}
	for _, f := range a.Files {
		files[f.Path] = true
	}
	if a.Change != nil {
		for _, f := range a.Change.Files {
			files[f.Path] = true
		}
	}
	for _, f := range b.Files {
		if files[f.Path] {
			return true
		}
	}
	if b.Change != nil {
		for _, f := range b.Change.Files {
			if files[f.Path] {
				return true
			}
		}
	}
	return false
}

// PriorityRank orders the four choices and puts an unset default last.
func PriorityRank(it Item) int {
	if p := PriorityFor(it); p != nil {
		switch p.Value {
		case "urgent":
			return 0
		case "high":
			return 1
		case "medium":
			return 2
		case "low":
			return 3
		}
	}
	return 4
}
