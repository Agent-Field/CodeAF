package factory

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrNotManager is the only status refusal that permits ordinary conversation.
var ErrNotManager = errors.New("this conversation is not the manager of any item in the factory")

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
	case it.DoneHow == "closed_locally":
		out.Text += "Job closed locally. Work and spending are kept. Reopen to continue; nothing is running."
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
		out.Text += step + " is queued; waiting for an available execution slot. Configure Jobs at once in Settings → Factory concurrency. Idle PR monitoring does not use an execution slot."
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

// Status questions describe the run, never an implementation change. Keep the
// grammar anchored and scoped to the job so an instruction containing "status"
// or a mixed question-and-action request cannot be consumed as a free read.
const statusSubject = `(?:it|this|that|things|you|we|(?:the )?(?:job|pipeline|run|plan|write|test|review|proof|follow pr|ci|pr))`

var statusQuestion = regexp.MustCompile(`^(?:` + strings.Join([]string{
	`(?:what is )?(?:(?:the |current |latest )?(?:status|progress|update|updates|news))(?: update)?(?: (?:on|of|with) ` + statusSubject + `)?`,
	`any (?:updates|news|progress)(?: (?:on|of|with) ` + statusSubject + `)?`,
	`(?:is )?(?:everything|all|it|this)(?: still)? (?:ok|okay|alright|all right|fine|on track|going well)(?: here| so far)?`,
	`are (?:we|you|things)(?: still)? (?:ok|okay|alright|all right|stuck|blocked|on track|making progress|working)(?: here| so far| on (?:this|it))?`,
	`is ` + statusSubject + `(?: still)? (?:running|working|stuck|blocked|paused|done|finished|on track|making progress)(?: here| so far)?`,
	`how (?:is|are) ` + statusSubject + ` (?:going|doing|progressing|coming along)(?: here| so far)?`,
	`what is (?:next|going on|happening|taking so long|the latest|the next step)`,
	`what are (?:the )?(?:next steps|blockers)`,
	`(?:where do we stand|where are we(?: at)?)(?: here)?(?: what is next)?`,
	`(?:give|show|tell) me (?:a |an |the )?(?:current |latest )?(?:status|progress|update|updates)(?: update)?`,
	`why (?:are (?:we|you)|is ` + statusSubject + `) (?:stuck|blocked|paused|taking so long|still running|still waiting)`,
	`(?:have (?:we|you)|has ` + statusSubject + `) (?:finished|completed|made (?:any )?progress)(?: yet)?`,
	`is there (?:any |an? )?(?:update|news|progress)`,
}, `|`) + `)$`)

// StatusOnlyRequest recognizes read-only check-ins in the person's wording.
// Every clause must be a status question; punctuation, courtesy and a second
// status question are harmless, but an action in any clause keeps the normal
// conversation path. This never infers permission to repair, resume or answer.
func StatusOnlyRequest(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer("’", "'", ",", " ").Replace(text)
	// Curly apostrophes are replaced first, then contractions are expanded.
	text = strings.NewReplacer("what's", "what is", "how's", "how is").Replace(text)
	seen := false
	for _, sentence := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("?!.;\n\r", r) }) {
		for _, clause := range strings.Split(strings.Join(strings.Fields(sentence), " "), " and ") {
			for {
				before := clause
				for _, prefix := range []string{"hi ", "hey ", "hello ", "please ", "just ", "can you ", "could you ", "would you "} {
					clause = strings.TrimPrefix(clause, prefix)
				}
				for _, suffix := range []string{" please", " thanks", " thank you"} {
					clause = strings.TrimSuffix(clause, suffix)
				}
				if clause == before {
					break
				}
			}
			switch clause {
			case "hi", "hey", "hello", "please", "thanks", "thank you":
				continue
			}
			if !statusQuestion.MatchString(clause) {
				return false
			}
			seen = true
		}
	}
	return seen
}
