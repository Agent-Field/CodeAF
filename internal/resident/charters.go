package resident

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func (r *Reconciler) applyCharterCommand(command store.Command) (commandOutcome, error) {
	charter, found, err := r.store.CharterByID(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("charter %q no longer exists", command.Target)
	}
	label := clipLabel(firstLine(charter.Spec.Invariant), 100)
	switch command.Kind {
	case store.CommandCharterRatify:
		if err := r.store.RatifyCharter(charter.ID); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter ratified",
			receipt: "Standing: " + label + ".",
		}, nil

	case store.CommandCharterPause:
		if err := r.store.PauseCharter(charter.ID); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "charter paused",
			receipt: "Paused: " + label + ".",
		}, nil

	case store.CommandCharterRetire:
		if err := r.store.RetireCharter(charter.ID); err != nil {
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
		if err := r.store.EditCharterCadence(charter.ID, cadence, store.ScheduleForCadence(cadence)); err != nil {
			return commandOutcome{}, err
		}
		updated, _, err := r.store.CharterByID(charter.ID)
		if err != nil {
			return commandOutcome{}, err
		}
		if updated.Status == store.CharterDraft {
			question, options := charterRatificationQuestion(updated)
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
		if err := r.store.RetireCharter(charter.ID); err != nil {
			return commandOutcome{}, err
		}
		id := fmt.Sprintf("task-%d", command.Seq)
		subtree := store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: charter.Spec.Action, Title: clipLabel(label, 48), Stage: 1,
		}}}
		provenance := store.Provenance{
			Origin: store.OriginUser, SessionID: command.SessionID, Intent: charter.Spec.Invariant,
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

func charterRatificationQuestion(charter store.Charter) (string, []store.QuestionOption) {
	spec := charter.Spec
	question := fmt.Sprintf(
		"%s\nfires: %s (%s:%s)\ncosts: ~$%.2f/firing, ≤%d/day — %s\nexpires: %s\nWhat should I do?",
		spec.Invariant, spec.Watch.Cadence, spec.Watch.Kind, spec.Watch.Schedule,
		spec.Rails.EstimatedCostUSD, spec.Rails.MaxPerDay,
		spec.Rails.MaxPerDayJustification, spec.Rails.Expiry,
	)
	options := []store.QuestionOption{
		{Label: "yes, stand this up", Value: "charter:ratify:" + charter.ID},
		{Label: "change the cadence", Value: "charter:cadence:" + charter.ID},
		{Label: "once, not standing", Value: "charter:once:" + charter.ID},
	}
	return question, options
}
