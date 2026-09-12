package session

// ONE REVIEW PER QUICK FAN-OUT, BEFORE ANY OF IT STARTS.
//
// This is the division review's shape moved to the one un-gated door in this
// build (task_divide.go's [reviewDivision]): `propose_task` has the countdown
// card where a person may redirect, but a batch of quick tasks starts
// instantly under no review at all — and a mis-shaped fan-out spends money
// before anybody reads anything. So a batch of TWO OR MORE `quick_task` calls
// is read TOGETHER, here, pre-spawn, and the verdict applies before the
// goroutines start: amended asks are rewritten, and refused asks are seeded
// with an ordinary refusal and never begun.
//
// IT MUST HAPPEN BEFORE ADMISSION, because amendment after admission is
// structurally impossible — a quick node's line and files are fixed at
// admission (task_quick.go), the receipt is already returned, and post-hoc
// amendment of a running node would be the wrong mechanism (division review
// rides divide_work before slots are claimed, and this copies that gate
// 1:1, only evaluated earlier, at batch render). [reviewQuickFan] runs this
// review, when it runs at all. Fail-open: an unreachable or unreadable
// reviewer admits as written, mirroring reviewDivision's `unanswered`.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func init() { roles.Register(roles.RoleQuickReview, roles.TierMastermind) }

// quickVerdict is the reviewer's answer: parts amended (rewritten asks), and
// original indexes it rules out altogether. AMEND reads the settled shape;
// REFUSE reads the indexes that must not start.
type quickVerdict struct {
	Why      string `json:"why"`
	NoReview bool   `json:"fine"`
	Refuse   []int  `json:"refuse"`
	Amend    []struct {
		Index int      `json:"index"`
		Line  string   `json:"line"`
		Items []string `json:"items"`
		Files []string `json:"files"`
	} `json:"amend"`
}

// quickReviewBrief is the reviewer's one paragraph: not a contract to groom,
// only the question a batch poses.
const quickReviewBrief = "You review one batch of quick tasks before any of " +
	"them start. Read the asks together. Where two overlap or split what one " +
	"list could carry, merge through amend; where one should be checked work, " +
	"say refuse. Empty means all are fine. Answer with one JSON object only."

// quickReviewQuestion renders the batch in call order for the reviewer.
func quickReviewQuestion(calls []ai.ToolCall, rendered []string) string {
	var out strings.Builder
	for index := range calls {
		fmt.Fprintf(&out, "INDEX %d: %s\n", index, rendered[index])
	}
	return out.String()
}

// reviewQuickFan runs the one review on a batch of ≥2 quick tasks. It returns
// the amended call list, the indexes refused before starting with their
// refusal sentences, and the receipt sentence the person reads on the card.
func (a *Agent) reviewQuickFan(ctx context.Context, calls []ai.ToolCall, rendered []string) ([]ai.ToolCall, map[int]string, string) {
	// QUICK ONLY, AND ONLY WHERE RARITY COSTS MONEY. One `quick_task` alone
	// cannot read a batch, and a batch without two of them is nothing to
	// sort.
	quickIdx := make([]int, 0, len(calls))
	for index, call := range calls {
		if call.Function.Name == quickTaskToolName {
			quickIdx = append(quickIdx, index)
		}
	}
	if len(quickIdx) < 2 {
		return calls, nil, ""
	}
	response, from, err := a.callRole(ctx, roles.RoleQuickReview, a.model,
		[]ai.Message{
			textMessage("system", quickReviewBrief),
			textMessage("user", quickReviewQuestion(calls, rendered)),
		})
	if err != nil || response == nil {
		// A REVIEW THAT CANNOT BE HAD IS NOT A REFUSAL. Admit as written,
		// mirroring reviewDivision's fail-open.
		return calls, nil, fmt.Sprintf("quick fan-out review unanswered — all admitted as written (%s)", errString(err))
	}
	a.addAuxiliaryUsage(response, from, 1)

	var verdict quickVerdict
	raw, err := subharness.Salvage(response.Text())
	if err2 := json.Unmarshal(raw, &verdict); err != nil || err2 != nil {
		return calls, nil, "quick fan-out review unreadable — all admitted as written"
	}

	refused := make(map[int]string)
	for _, index := range verdict.Refuse {
		if index >= 0 && index < len(calls) {
			// THE REFUSAL IS THE REVIEWER'S WHY, cut in the receipt because
			// that is the sentence the model must act on to ask again well.
			refused[index] = fmt.Sprintf("quick fan-out refused: %s", verdict.Why)
		}
	}
	for _, amend := range verdict.Amend {
		if amend.Index < 0 || amend.Index >= len(calls) || strings.TrimSpace(amend.Line) == "" {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"line":  amend.Line,
			"items": amend.Items,
			"files": amend.Files,
		})
		calls[amend.Index].Function.Arguments = string(payload)
		rendered[amend.Index] = argsText(calls[amend.Index])
	}
	return calls, refused, fmt.Sprintf(
		"quick fan-out settled: %d amended, %d refused — %s", len(verdict.Amend), len(refused), verdict.Why)
}
