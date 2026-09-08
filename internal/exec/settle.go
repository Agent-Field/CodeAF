package exec

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// Settle writes one leaf's measured ending onto its plan node and hands the
// document that ending changed to the journal.
//
// It is one function because both doors owe the same nine fields and the same
// state rule, while only the chat door has a durable namespace to journal. A
// new door reaches this seam rather than growing a partial copy of the ending.
// For the headless door this also carries Calibration into the profile record
// cmd/aforge/run.go builds, where the worker's account of its own fit was
// previously absent.
//
// The journal is called with the ending settled and before a door's own last
// word about the node, which costs nothing that can be lost: a rehydrated plan
// re-derives State, Result and Failure from the durable rows
// (cmd/aforge/chat.go, syncPlanState), and the fields only the journal can
// carry are exactly the ones written here. journal is nil where a surface has
// nowhere durable to write.
func Settle(node *plan.Node, outcome *Outcome, err error, journal func()) {
	if node == nil {
		return
	}
	if outcome != nil {
		node.Turns = outcome.Turns
		node.Tokens = outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
		node.Cost = outcome.Usage.Cost
		node.Stop = string(outcome.Stop)
		node.Verdict = outcome.Verdict
		node.Artifacts = outcome.Artifacts
		node.Result = outcome.Text
		node.Checked = outcome.Account.Summary()
		node.Calibration = append([]string(nil), outcome.Calibration...)
	}
	if err != nil || outcome == nil || strings.TrimSpace(node.Result) == "" {
		node.State = plan.StateFailed
	} else {
		node.State = plan.StateDone
	}
	if journal != nil {
		journal()
	}
}
