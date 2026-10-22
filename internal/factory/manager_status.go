package factory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PendingQuestionID identifies the decision, not the frequently changing job
// document. Polling or spending updates cannot expire it; a new ask or round can.
func PendingQuestionID(it Item) string {
	if it.Asking == nil || it.Asking.With != AskedYou || it.State != StateNeedsYou || it.QKind != "step" {
		return ""
	}
	round := 0
	if it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) {
		round = it.Stream.Phases[it.Stream.Cur].Round
	}
	body, _ := json.Marshal(struct {
		ID, Round         int
		Stage, Kind, Text string
		At                time.Time
	}{it.ID, round, it.Asking.Stage, it.QKind, it.Question, it.Asking.At})
	return fmt.Sprintf("step-%x", sha256.Sum256(body))
}

// ManagerStatus is a read of saved runner facts, never a model's reconstruction
// from old chat prose. It does not start a worker, extend budget or answer an ask.
type ManagerStatus struct {
	Text       string    `json:"text"`
	QuestionID string    `json:"question_id,omitempty"`
	At         time.Time `json:"at"`
}

func StatusForManager(it Item, held bool, now time.Time) ManagerStatus {
	out := ManagerStatus{At: now, QuestionID: PendingQuestionID(it)}
	var done []string
	step := "pipeline"
	var current *Phase
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.State == PhaseDone {
				done = append(done, ph.Name)
			}
		}
		if it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) {
			current = &it.Stream.Phases[it.Stream.Cur]
			step = current.Name
		}
	}
	if len(done) > 0 {
		out.Text = "Completed: " + strings.Join(done, ", ") + ".\n"
	}
	switch {
	case it.DoneHow == "dropped":
		out.Text += "Job dropped. No pipeline work is running."
	case it.PullRequest != nil && (it.PullRequest.State == PRMerged || it.PullRequest.State == PRClosed):
		out.Text += "PR is " + it.PullRequest.State + ". No pipeline work is running."
	case it.Manual:
		out.Text += "Pipeline is under manual control. Resume pipeline when ready."
	case it.Question != "":
		out.Text += step + " is waiting: " + it.Question + "\nNo step work is running while this decision waits. Answer here or use the job's decision controls."
	case it.Failure != nil:
		out.Text += step + " stopped: " + it.Failure.Detail + "\nUse Try again to retry this step within the job budget."
	case it.FollowUp != nil && (it.FollowUp.State == "healthy" || it.FollowUp.State == "watching" || it.FollowUp.State == "needs_you"):
		out.Text += it.FollowUp.Reason
	case it.Stream != nil && it.Stream.Paused:
		out.Text += step + " is paused. Resume pipeline to continue."
	case it.State == StateQueued:
		out.Text += step + " is queued; waiting for the runner."
	case it.State == StateRunning && held:
		out.Text += step + " is running on its worker."
	case it.State == StateRunning:
		out.Text += step + " has no live worker. The saved running state is not active work. Use Try again to recover the step."
	case it.State == StateLanded:
		out.Text += "Local work is complete; publication is pending. Check the approval controls."
	default:
		out.Text += "Pipeline state: " + string(it.State) + "."
	}
	if current != nil && !current.At.IsZero() {
		out.Text += "\nLast recorded step progress: " + current.At.UTC().Format(time.RFC3339) + "."
	}
	if out.QuestionID != "" {
		out.Text += "\nPending question ID: " + out.QuestionID + ". Use factory_answer with this question_id to save the person's decision, including a delegated choice. A tool failure means the decision was not saved; never describe it as answered."
	}
	return out
}

// StatusOnlyRequest deliberately does not infer permission to repair or resume.
func StatusOnlyRequest(text string) bool {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.NewReplacer("’", "'", "?", "", ".", "", "!", "").Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	switch text {
	case "status", "status please", "what's the status", "what is the status", "where do we stand", "where do we stand here", "what's next", "what is next", "where do we stand here what's next", "where do we stand what's next", "are we stuck", "are we stuck here":
		return true
	}
	return false
}
