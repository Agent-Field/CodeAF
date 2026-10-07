package praf

// The review's page, in its own words: what each line of its action log reads
// as on the task's page (delegate.Delegate's Present).
//
// ONE LINE PER STAGE ENTERED AND ONE PER FINISHED STEP. A review reports a
// stage again each time a call in it starts or ends, so a fan-out of eight
// reviewers is sixteen stage records; the page shows the stage once, as the
// run enters it, and each finished step under it with what it found.

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// stepWords is each stage as the one word its page prints at the head of the
// stage's lines, and its task's row reads while the review is in it.
var stepWords = map[string]string{
	stageStarting: "setup", stageIntake: "intake", stageAnatomy: "anatomy", stagePlan: "plan",
	stageReview: "review", stageFilter: "filter", stageEvidence: "evidence", stageChallenge: "challenge",
	stageDeepen: "deepen", stageCrossRef: "cross-ref", stageObligations: "obligations",
	stageCoverage: "coverage", stageReport: "report", stagePost: "post",
}

// presentActions is the review's reader of its own action log.
func presentActions() delegate.ActionReader {
	shown := map[string]bool{}
	return func(action delegate.Action) (delegate.Shown, bool) {
		switch action.Kind {
		case delegate.ActionStage:
			if action.Stage == "" || shown[action.Stage] {
				return delegate.Shown{}, false
			}
			shown[action.Stage] = true
			text := stageText(action.Data)
			if text == "" {
				text = stageWords[action.Stage]
			}
			return delegate.Shown{Step: stepWords[action.Stage], Text: text}, text != ""
		case delegate.ActionStep:
			if strings.TrimSpace(action.Command) == "" {
				return delegate.Shown{}, false
			}
			return delegate.Shown{Step: stepWords[action.Step], Text: plainWords(action.Command),
				Outcome: outcome(action.Observation), Detail: plainWords(action.Observation)}, true
		case delegate.ActionEnd:
			return delegate.Shown{Step: stepWords[stageReport], Text: plainWords(action.Message)}, true
		}
		return delegate.Shown{}, false
	}
}

// stageText is what a stage record says the review is doing.
func stageText(raw json.RawMessage) string {
	var data struct {
		Doing string `json:"doing"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &data) != nil {
		return ""
	}
	return strings.TrimSpace(data.Doing)
}

// outcome is a step's observation in a word or two: `failed` for an error,
// otherwise its count (`3 findings`, `nothing found`).
func outcome(observation string) string {
	observation = strings.TrimSpace(observation)
	if strings.HasPrefix(observation, "error:") {
		return "failed"
	}
	head, _, _ := strings.Cut(observation, ":")
	if strings.HasSuffix(head, "finding") || strings.HasSuffix(head, "findings") || head == "nothing found" {
		return head
	}
	return ""
}
