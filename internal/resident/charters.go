package resident

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// applyCharterCommand maps conversational charter management onto the
// canonical charter lifecycle: ratify is SetCharterStatus(active) carrying the
// user's own words as evidence, pause and retire are status transitions, and
// a cadence edit is ReviseCharter with a freshly derived typed watch.
func (r *Reconciler) applyCharterCommand(command store.Command) (commandOutcome, error) {
	charter, found, err := r.store.Charter(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("charter %q no longer exists", command.Target)
	}
	label := clipLabel(firstLine(charter.Invariant), 100)
	switch command.Kind {
	case store.CommandCharterRatify:
		evidence := strings.TrimSpace(command.Instruction)
		if evidence == "" {
			evidence = "ratified in conversation"
		}
		if err := r.store.SetCharterStatus(charter.ID, store.CharterActive, store.Ratification{
			Origin: store.OriginUser, SessionID: command.SessionID, Evidence: evidence,
		}); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter ratified",
			receipt: "Standing: " + label + ".",
		}, nil

	case store.CommandCharterPause:
		if err := r.store.SetCharterStatus(charter.ID, store.CharterPaused, store.Ratification{}); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter paused",
			receipt: "Paused: " + label + ".",
		}, nil

	case store.CommandCharterRetire:
		if err := r.store.SetCharterStatus(charter.ID, store.CharterRetired, store.Ratification{}); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter retired",
			receipt: "Retired: " + label + ".",
		}, nil

	case store.CommandCharterCadence:
		cadence := strings.TrimSpace(command.Instruction)
		if cadence == "" {
			return commandOutcome{}, fmt.Errorf("charter cadence is empty")
		}
		watch := store.RetimeWatch(charter.Watch, cadence, time.Now())
		if err := r.store.ReviseCharter(charter.ID, charter.Invariant, watch,
			charter.SentinelHint, charter.Action, charter.Rails()); err != nil {
			return commandOutcome{}, err
		}
		updated, _, err := r.store.Charter(charter.ID)
		if err != nil {
			return commandOutcome{}, err
		}
		if updated.Status == store.CharterProposed {
			question, options := charterRatificationQuestion(updated, "")
			return commandOutcome{
				status: store.CommandApplied, result: "draft charter cadence edited",
				receipt: question, asAgent: true, options: options,
			}, nil
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter cadence edited",
			receipt: fmt.Sprintf("Cadence changed: %s → %s.", label, cadence),
		}, nil

	case store.CommandCharterOnce:
		if err := r.store.SetCharterStatus(charter.ID, store.CharterRetired, store.Ratification{}); err != nil {
			return commandOutcome{}, err
		}
		id := fmt.Sprintf("task-%d", command.Seq)
		subtree := store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: charter.Action.Template, Title: clipLabel(label, 48), Stage: 1,
		}}}
		provenance := store.Provenance{
			Origin: store.OriginUser, SessionID: command.SessionID, Intent: charter.Invariant,
		}
		if err := r.store.Splice(store.RootID, subtree, provenance); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter declined; spliced once",
			receipt: "Not standing — doing this once: " + label + ".",
		}, nil
	}
	return commandOutcome{}, fmt.Errorf("unsupported charter command %q", command.Kind)
}

// charterRatificationQuestion reads the canonical charter: the cadence words
// the user said, the executable schedule they compiled into, and the rails
// that bound every firing. justification is the compiler's cap reasoning; it
// travels with the initial draft only.
func charterRatificationQuestion(charter store.Charter, justification string) (string, []store.QuestionOption) {
	rails := charter.Rails()
	fires := strings.TrimSpace(charter.Watch.Cadence)
	if fires == "" {
		fires = charter.Watch.String()
	} else {
		fires += " (" + charter.Watch.String() + ")"
	}
	costs := fmt.Sprintf("~$%.2f/firing, ≤%d/day", rails.PerFiringBudgetUSD, rails.MaxFiringsPerDay)
	if justification = strings.TrimSpace(justification); justification != "" {
		costs += " — " + justification
	}
	expires := "never"
	if rails.ExpiresAt != nil {
		expires = rails.ExpiresAt.Local().Format("2006-01-02 15:04")
	}
	question := fmt.Sprintf("%s\nfires: %s\ncosts: %s\nexpires: %s\nWhat should I do?",
		charter.Invariant, fires, costs, expires)
	options := []store.QuestionOption{
		{Label: "yes, stand this up", Value: "charter:ratify:" + charter.ID},
		{Label: "change the cadence", Value: "charter:cadence:" + charter.ID},
		{Label: "once, not standing", Value: "charter:once:" + charter.ID},
	}
	return question, options
}
