package session

// standing_writeguard.go is THE CONVERSATION NEVER WRITES A REPORT aforge
// PUBLISHES (ruling R11).
//
// The card for work that keeps a report says `aforge publishes this file; the
// run never writes it`, and the chat protocol of 2026-09-11 found the
// conversation writing it anyway, three times of three: it seeded
// marketing/review-notes.md before proposing (h2-spec), wrote over
// reports/upgrades.md after a change of mind (policy), and wrote a baseline the
// runs could never move (regress). Each write was one the next run was then held
// at — `report-changed`, a question for the person about a file they never
// touched. The person's own edits happen outside the chat, and those are what
// that hold is for; a change to what the report says is a change to the work.
//
// So the conversation's own `write` and `edit` of a path that an active or
// paused standing item publishes is refused, naming the item and the edit that
// changes it. The guard is the session's, never internal/exec/bare's: bare's
// tools are pi's, verbatim, and a law about aforge's own records is aforge's to
// keep at its own control plane.
//
// What it cannot see, said plainly: a shell command that writes the file, and a
// task handed out from the conversation, whose worker is given no standing store
// to ask (task_run.go).

import (
	"context"
	"strconv"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// standingReportGuard is the pre-action citizen for the law above, registered
// only on an agent with a standing store to ask ([Agent.standingItems]).
type standingReportGuard struct{ agent *Agent }

func (standingReportGuard) Name() string { return "standing-report" }

func (g standingReportGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	path, shown, writes := g.agent.mutatingPath(call)
	if !writes {
		return call, toolResult{}, true
	}
	item, owned := g.agent.standingReportAt(path)
	if !owned {
		return call, toolResult{}, true
	}
	return call, toolResult{text: shown + " is the report of " + strconv.Quote(item.Words) + " (" + item.ID +
		"): aforge publishes this file, and neither the run nor you writes it. To change what it says, change the work — stand op edit with that id.", isError: true}, false
}
