package resident

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func (r *Reconciler) applyCharterCommand(ctx context.Context, command store.Command) (commandOutcome, error) {
	charter, found, err := r.store.CharterByID(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("charter %q no longer exists", command.Target)
	}
	label := clipLabel(firstLine(charter.Invariant), 100)
	switch command.Kind {
	case store.CommandCharterRatify:
		if err := r.store.RatifyCharterWithEvidence(charter.ID, command.Instruction); err != nil {
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

	case store.CommandCharterFire:
		wakeSeq, err := charterCommandWakeSeq(command.Instruction)
		if err != nil {
			return commandOutcome{}, err
		}
		if fired, err := r.store.CharterWakeFired(charter.ID, wakeSeq); err != nil {
			return commandOutcome{}, err
		} else if fired {
			return commandOutcome{status: store.CommandApplied, result: "probation firing already admitted"}, nil
		}
		if charter.Autonomy != store.CharterProbation || !charter.WakePending || charter.WakeSeq != wakeSeq {
			return commandOutcome{}, fmt.Errorf("probation firing approval is stale")
		}
		disposition, jobID, err := r.admitCharterFiring(ctx, charter, true)
		if err != nil {
			return commandOutcome{}, err
		}
		return charterFireCommandOutcome(disposition, jobID, label), nil

	case store.CommandCharterDecline:
		wakeSeq, err := charterCommandWakeSeq(command.Instruction)
		if err != nil {
			return commandOutcome{}, err
		}
		if !charter.WakePending && charter.Autonomy == store.CharterProbation {
			return commandOutcome{status: store.CommandApplied, result: "probation firing already declined"}, nil
		}
		if err := r.store.DeclineCharterFiring(charter.ID, wakeSeq, "user declined this probation firing", false); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{status: store.CommandApplied, result: "probation firing declined; charter remains active",
			receipt: "Skipped this firing; I'll ask again next time: " + label + "."}, nil

	case store.CommandCharterAlways:
		wakeSeq, err := charterCommandWakeSeq(command.Instruction)
		if err != nil {
			return commandOutcome{}, err
		}
		if fired, err := r.store.CharterWakeFired(charter.ID, wakeSeq); err != nil {
			return commandOutcome{}, err
		} else if fired {
			return commandOutcome{status: store.CommandApplied, result: "charter already promoted and firing admitted"}, nil
		}
		if !charter.WakePending || charter.WakeSeq != wakeSeq {
			return commandOutcome{}, fmt.Errorf("always-allow approval is stale")
		}
		if err := r.store.PromoteCharter(charter.ID, "user chose always allow on probation proposal", true); err != nil {
			return commandOutcome{}, err
		}
		charter, _, err = r.store.Charter(charter.ID)
		if err != nil {
			return commandOutcome{}, err
		}
		disposition, jobID, err := r.admitCharterFiring(ctx, charter, false)
		if err != nil {
			return commandOutcome{}, err
		}
		outcome := charterFireCommandOutcome(disposition, jobID, label)
		outcome.result = "charter promoted by user override; " + outcome.result
		return outcome, nil

	case store.CommandCharterNever:
		wakeSeq, err := charterCommandWakeSeq(command.Instruction)
		if err != nil {
			return commandOutcome{}, err
		}
		if !charter.WakePending && charter.Status == store.CharterPaused {
			return commandOutcome{status: store.CommandApplied, result: "probation firing declined and charter already paused"}, nil
		}
		if err := r.store.DeclineCharterFiring(charter.ID, wakeSeq, "user chose never on probation proposal", true); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{status: store.CommandApplied, result: "probation firing declined; charter paused",
			receipt: "Paused after your ‘never’: " + label + "."}, nil

	case store.CommandCharterProbation:
		if err := r.store.ReturnCharterToProbation(charter.ID, "user said back to asking"); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{status: store.CommandApplied, result: "charter returned to probation",
			receipt: "Back to asking: " + label + "."}, nil

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

func charterCommandWakeSeq(instruction string) (int64, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(instruction), "wake:"))
	wakeSeq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || wakeSeq <= 0 {
		return 0, fmt.Errorf("invalid charter wake approval %q", instruction)
	}
	return wakeSeq, nil
}

func charterFireCommandOutcome(disposition store.FireDisposition, jobID, label string) commandOutcome {
	switch disposition {
	case store.FireAdmitted:
		return commandOutcome{status: store.CommandApplied, result: "approved firing admitted as " + jobID,
			receipt: "Approved — doing this now: " + label + "."}
	case store.FireRailWait:
		return commandOutcome{status: store.CommandApplied, result: "approved firing waiting at daily dollar rail"}
	case store.FireQuota:
		return commandOutcome{status: store.CommandApplied, result: "approved firing blocked by charter quota",
			receipt: "That firing hit its daily charter limit: " + label + "."}
	case store.FireExpired:
		return commandOutcome{status: store.CommandApplied, result: "approved firing expired",
			receipt: "That charter expired before it could fire: " + label + "."}
	default:
		return commandOutcome{status: store.CommandRejected, result: "unknown charter firing disposition " + string(disposition)}
	}
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
