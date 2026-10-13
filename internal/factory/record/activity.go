package record

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
)

// enrich derives S2 fields for older receipts without rewriting append-only history.
func enrich(e *Event) {
	if e.Actor.Kind == "" {
		e.Actor = Actor{Kind: "factory"}
		if handle := factory.NormalizeHandle(e.Handle); handle != "" {
			e.Actor = Actor{Kind: "person", Name: "@" + handle}
		}
	}
	if e.Object == "" && e.Job > 0 {
		e.Object = fmt.Sprintf("job:%d", e.Job)
	}
	if e.Verb == "" {
		switch e.Kind {
		case StepEdited:
			e.Verb = "edited steps"
		case StepStopped:
			e.Verb = "stopped"
		case StepRerun:
			e.Verb = "reran step"
		case HandOffNotNow:
			e.Verb = "put away hand-off"
		case AutoStarted, Started:
			e.Verb = "started"
		case AskedFirst:
			e.Verb = "asked"
		case Posted:
			e.Verb = "published"
		case RequestChanges:
			e.Verb = "requested changes"
		case PRMerged:
			e.Verb = "merged"
		case PRClosed:
			e.Verb = "closed"
		case StepFailed:
			e.Verb = "failed"
		case "priority_changed":
			e.Verb = "changed priority"
		case "budget_raised":
			e.Verb = "raised budget"
		case "branch_deleted":
			e.Verb = "deleted branch"
		case Took:
			e.Verb = "assigned"
		case Released:
			e.Verb = "unassigned"
		case RunnerAdded:
			e.Verb = "added runner"
		case RunnerChanged:
			e.Verb = "changed runner"
		case ReposChanged:
			e.Verb = "connected repo"
			if strings.HasPrefix(e.Detail, "turned off ") {
				e.Verb = "disconnected repo"
			}
		case LinkCreated:
			e.Verb = "created access link"
		case LinkRevoked:
			e.Verb = "revoked access link"
		case OperatorClaimed:
			e.Verb = "claimed factory"
		case GitHubGranted:
			e.Verb = "granted GitHub read access"
		case SessionsRevoked:
			e.Verb = "revoked sessions"
		default:
			e.Verb = e.Kind
		}
	}
	if e.Actor.Operator && e.Detail == "" {
		e.Detail = "as operator"
	}
}
