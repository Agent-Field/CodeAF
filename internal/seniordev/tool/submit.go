//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

// Submitting is the point of no return. Calling it hands senior-dev the tree as it
// stands and ends the run's authority to change it: the candidate is captured
// inside this call, so nothing the model does afterwards can reach what ships.
//
// Finishing is an explicit action rather than a condition inferred from the
// tree. A run that ends because its budget ran out leaves "done" as whatever
// the tree happened to look like at that moment; submit turns it into a
// decision the model makes, with evidence attached.
const submitDescription = `Declare the work finished. The working tree at this instant -- committed,
modified, and untracked files alike, minus git-ignored paths and .senior-dev/ -- is
captured as the answer, and the run ends. This is irreversible: later edits are
not part of the answer.

Takes a reason, the evidence you verified with (the command you ran and what it
returned), and checklist_satisfied. Refuses, naming the cause, when the tree is
identical to the starting commit, when .senior-dev/checklist.md does not exist,
when reason or evidence is empty, or when this run already submitted. A refusal
does not capture anything and does not end the run.`

const submitSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"properties": {
		"reason": {
			"description": "Why the work is finished, in one sentence.",
			"type": "string"
		},
		"evidence": {
			"description": "The commands you ran to prove it and what they returned. Name the pinned command and its exit status, and the test counts.",
			"type": "string"
		},
		"checklist_satisfied": {
			"description": "True only if every intake checklist item is satisfied. Do not set it true to get past this gate.",
			"type": "boolean"
		}
	},
	"required": ["reason", "evidence", "checklist_satisfied"]
}`

type submitInput struct {
	Reason             string `json:"reason"`
	Evidence           string `json:"evidence"`
	ChecklistSatisfied bool   `json:"checklist_satisfied"`
}

// Submission is what the model claimed when it submitted. The claim is
// recorded verbatim and separately from what senior-dev verifies itself afterwards
// -- a run that says "all tests pass" and did not run them must leave both
// facts in the record, not one reconciled story.
type Submission struct {
	Reason             string
	Evidence           string
	ChecklistSatisfied bool
	SessionID          string
}

// SubmitFreezer captures the candidate at submit time. It returns a short
// human-readable description of what was frozen (a patch size, a sha) that is
// echoed back to the model so the transcript records the handoff, or an error
// if there was nothing to freeze.
//
// The freeze happens inside the tool call rather than after the turn returns
// because that is the only placement where "no later stage may reopen the
// implementation" is structural instead of aspirational.
type SubmitFreezer func(ctx context.Context, submission Submission) (string, error)

func validateSubmit(raw json.RawMessage) error {
	var input submitInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	if strings.TrimSpace(input.Reason) == "" {
		return errors.New("submit: reason is required")
	}
	if strings.TrimSpace(input.Evidence) == "" {
		return errors.New("submit: evidence is required — name the command you ran and what it returned")
	}
	return nil
}

func (r *Registry) executeSubmit(
	ctx context.Context, call steploop.ToolCall,
) (steploop.ToolResult, error) {
	var input submitInput
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return steploop.ToolResult{}, fmt.Errorf("submit: %w", err)
	}
	if r.submitFreeze == nil {
		return steploop.ToolResult{}, errors.New(
			"submit: this run has no submission handler; finish by describing the work instead",
		)
	}
	submission := Submission{
		Reason:             strings.TrimSpace(input.Reason),
		Evidence:           strings.TrimSpace(input.Evidence),
		ChecklistSatisfied: input.ChecklistSatisfied,
		SessionID:          call.SessionID,
	}
	description, err := r.submitFreeze(ctx, submission)
	if err != nil {
		// A refused submit is not a crash: the model is told why and may keep
		// working. Refusing loudly here is the whole point -- an empty patch or
		// a dirty tree caught at submit is worth more than the same thing
		// discovered by the verification that runs after the freeze.
		return steploop.ToolResult{
			Title:  "submit refused",
			Output: "Submission refused: " + err.Error() + "\n\nThe tree was NOT captured. Fix the problem and submit again.",
		}, nil
	}
	return steploop.ToolResult{
		Title: "submitted",
		Output: "Submission accepted and the tree is frozen: " + description +
			"\n\nThis is your answer. Stop editing. Reply with a short summary of what you changed" +
			" and the evidence it works; nothing you do now can change what ships.",
	}, nil
}

// SetSubmitFreezer installs the submission handler after construction. The
// registry is built inside the runtime, before the pipeline that owns the
// freeze exists; this is the seam between them. Setting it also advertises the
// submit tool, so it must be called before the first turn is configured.
func (r *Registry) SetSubmitFreezer(freeze SubmitFreezer) {
	r.submitFreeze = freeze
}
