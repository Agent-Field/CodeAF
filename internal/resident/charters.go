package resident

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// applyCharterCommand maps conversational charter management onto the
// canonical charter lifecycle: ratify is SetCharterStatus(active) carrying the
// user's own words as evidence, pause and retire are status transitions, and
// a cadence edit is ReviseCharter with a freshly derived typed watch.
func (r *Reconciler) applyCharterCommand(ctx context.Context, command store.Command) (commandOutcome, error) {
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
		// Ratification is already durable at this point. The standing-watch
		// offer is an adjacent consequence gate: a failure to inspect or post it
		// must not lie by marking the charter command rejected after activation.
		if err := r.offerStandingWatch(command.SessionID, charter.ID); err != nil {
			log.Printf("standing watch offer after charter %s: %v", charter.ID, err)
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
		// Retiring something that was never stood up is declining it, and the
		// difference matters: a decline writes the durable "do not offer this
		// shape again" record, so the retrospective that noticed the recurring
		// ask three times does not notice it again next week and ask again.
		// This is also the only route by which DeclineCharterProposal is ever
		// called — the proposal question offers it under the retire code the
		// head already decodes, rather than inventing a second vocabulary.
		if charter.Status == store.CharterProposed {
			reason := strings.TrimSpace(command.Instruction)
			if reason == "" {
				reason = "the user declined the proposal"
			}
			if err := r.store.DeclineCharterProposal(charter.ID, reason); err != nil {
				return commandOutcome{}, err
			}
			return commandOutcome{
				status: store.CommandApplied, result: "charter proposal declined",
				receipt: "Understood — not standing, and I won't offer it again: " + label + ".",
			}, nil
		}
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

func (r *Reconciler) offerStandingWatch(sessionID, charterID string) error {
	if r.standingWatch == nil {
		return nil
	}
	status, err := r.standingWatch.Status()
	if err != nil {
		return err
	}
	decision, err := r.store.StandingWatchDecisionState()
	if err != nil {
		return err
	}
	if decision == store.StandingWatchEnabled {
		if !status.Installed {
			return r.installStandingWatch(context.Background())
		}
		return nil
	}
	if decision == store.StandingWatchOffered || decision == store.StandingWatchDeclined {
		return nil
	}
	if status.Installed {
		return nil
	}
	_, err = r.store.OfferStandingWatch(sessionID, charterID)
	return err
}

// standingWatchInstallTimeout bounds one host install. launchctl and systemctl
// can block indefinitely on a wedged daemon, and this call reaches a subprocess
// on the host, so an unbounded wait would be an unbounded stall.
const standingWatchInstallTimeout = 5 * time.Second

// installStandingWatch is the only path that reaches the host. Every caller
// goes through it so no install can outlive its timeout.
func (r *Reconciler) installStandingWatch(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	installCtx, cancel := context.WithTimeout(ctx, standingWatchInstallTimeout)
	defer cancel()
	return r.standingWatch.Install(installCtx)
}

// uninstallStandingWatch is Install's mirror, bounded the same way and for the
// same reason: it shells out to launchctl or systemctl on the way out.
func (r *Reconciler) uninstallStandingWatch(ctx context.Context) error {
	if r.standingWatch == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	removeCtx, cancel := context.WithTimeout(ctx, standingWatchInstallTimeout)
	defer cancel()
	return r.standingWatch.Uninstall(removeCtx)
}

// proposeStandingWatchStandDown is the ask-to-disable that mirrors the
// ask-to-enable. The moment it looks for is the honest one: quiet checks were
// consented to on behalf of a standing goal, and there are no standing goals
// left — so the machine is waking every five minutes on nobody's behalf and is
// the only party that knows.
func (r *Reconciler) proposeStandingWatchStandDown() {
	if r.store == nil || r.standingWatch == nil {
		return
	}
	decision, err := r.store.StandingWatchDecisionState()
	if err != nil || decision != store.StandingWatchEnabled {
		return
	}
	active, err := r.store.Charters(store.CharterActive)
	if err != nil {
		return
	}
	for _, charter := range active {
		if !store.IsPracticeCharter(charter) {
			return
		}
	}
	seen, found, err := r.store.LastSeen()
	if err != nil || !found || seen.State != store.SeenAttached {
		return
	}
	session := strings.TrimSpace(seen.SessionID)
	if session == "" {
		return
	}
	if _, err := r.store.OfferStandingWatchStandDown(session); err != nil {
		log.Printf("standing watch stand-down offer: %v", err)
	}
}

// reconcileStandingWatch repairs a missing host timer. It runs outside the
// reconciler mutex — the install shells out to launchctl or systemctl, and a
// slow host must never hold the lock every other resident path waits on. Its
// own mutex keeps two overlapping Ticks from repairing at once.
func (r *Reconciler) reconcileStandingWatch(ctx context.Context) {
	r.standingMu.Lock()
	defer r.standingMu.Unlock()
	if r.store == nil || r.standingWatch == nil ||
		(!r.standingWatchCheck.IsZero() && r.now().Before(r.standingWatchCheck)) {
		return
	}
	r.standingWatchCheck = r.now().Add(5 * time.Minute)
	decision, err := r.store.StandingWatchDecisionState()
	if err != nil || decision != store.StandingWatchEnabled {
		if err != nil {
			log.Printf("standing watch decision: %v", err)
		}
		return
	}
	status, err := r.standingWatch.Status()
	if err != nil {
		log.Printf("standing watch status: %v", err)
		return
	}
	if status.Installed {
		return
	}
	if r.standingWatchKeyPersist != nil {
		if _, _, err := r.standingWatchKeyPersist(); err != nil {
			log.Printf("standing watch key: %v", err)
		}
	}
	if err := r.installStandingWatch(ctx); err != nil {
		log.Printf("standing watch repair: %v", err)
	}
}

func (r *Reconciler) applyStandingWatchCommand(ctx context.Context, command store.Command) (commandOutcome, error) {
	reason := strings.TrimSpace(command.Instruction)
	if reason == "" {
		reason = "answered in conversation"
	}
	switch command.Kind {
	case store.CommandStandingWatchEnable:
		if r.standingWatch == nil {
			return commandOutcome{}, fmt.Errorf("standing watch is unavailable in this process")
		}
		// The choice is durable before touching the host. A process death or a
		// transient host failure therefore becomes a repair on the next check,
		// never a lost "yes".
		if err := r.store.RecordStandingWatchDecision(store.StandingWatchEnabled, reason); err != nil {
			return commandOutcome{}, err
		}
		// Timer-driven wakes run without the shell environment, so the key
		// must survive on disk or every quiet check dies at startup.
		var persisted bool
		var keyPath string
		if r.standingWatchKeyPersist != nil {
			var keyErr error
			persisted, keyPath, keyErr = r.standingWatchKeyPersist()
			if keyErr != nil {
				log.Printf("standing watch key: %v", keyErr)
			}
		}
		if err := r.installStandingWatch(ctx); err != nil {
			log.Printf("standing watch install: %v", err)
			return commandOutcome{}, fmt.Errorf("quiet background checks could not be enabled")
		}
		receipt := "I'll keep watch — a quiet check every few minutes, even with no terminal open."
		if persisted {
			receipt += " Your API key now lives in " + keyPath + ", readable only by you, so those checks can run."
		}
		return commandOutcome{
			status: store.CommandApplied, result: "standing watch enabled",
			receipt: receipt,
		}, nil
	case store.CommandStandingWatchDecline:
		// The same word, read against the state it arrives in. Before the timer
		// exists, "no" declines an offer. Once it exists, "no" is the reverse
		// gear — and the reverse gear has to reach the host, because the repair
		// pass reinstalls a timer it finds missing every five minutes, so a
		// journalled stand-down that never called Uninstall would leave the
		// user's own `launchctl unload` being undone by us.
		current, err := r.store.StandingWatchDecisionState()
		if err != nil {
			return commandOutcome{}, err
		}
		if current == store.StandingWatchEnabled {
			if err := r.store.RecordStandingWatchDecision(store.StandingWatchStoodDown, reason); err != nil {
				return commandOutcome{}, err
			}
			if err := r.uninstallStandingWatch(ctx); err != nil {
				log.Printf("standing watch uninstall: %v", err)
			}
			return commandOutcome{
				status: store.CommandApplied, result: "standing watch stood down",
				receipt: "Stood down — no more checks with the terminal closed. Say the word and I'll pick it back up.",
			}, nil
		}
		if err := r.store.RecordStandingWatchDecision(store.StandingWatchDeclined, reason); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{
			status: store.CommandApplied, result: "standing watch declined",
			receipt: "Okay — I'll watch only while you're around.",
		}, nil
	default:
		return commandOutcome{}, fmt.Errorf("unsupported standing watch command %q", command.Kind)
	}
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

// charterRatificationQuestion reads the canonical charter back to the person
// who said it: when it runs, what each run costs, and — when the schedule is
// one nobody stated — the schedule itself, asked rather than asserted.
// justification is the compiler's cap reasoning; it travels with the initial
// draft only.
//
// Two things this must never do again. It must not print the engine's own
// spelling of a schedule: "fires: about every 2 minutes (cron:every 2 minutes)"
// showed a user a guess twice, once in words she could not read. And it must
// not state a guess as a fact — a card is already a question, and the cheapest
// possible place to catch a misread rhythm is the sentence that asks about it.
func charterRatificationQuestion(charter store.Charter, justification string) (string, []store.QuestionOption) {
	rails := charter.Rails()
	when := "when: " + charter.Watch.Spoken()
	if charter.Watch.CadenceGuessed {
		when = "when: " + charter.Watch.Spoken() + " — you didn't say, so that's my guess. Right?"
	}
	costs := fmt.Sprintf("costs: about $%.2f a run, at most %d a day",
		rails.PerFiringBudgetUSD, rails.MaxFiringsPerDay)
	if justification = strings.TrimSpace(justification); justification != "" {
		costs += " — " + justification
	}
	lines := []string{charter.Invariant, when, costs}
	if rails.ExpiresAt != nil {
		lines = append(lines, "then it's done — this one runs once.")
	}
	lines = append(lines, "What should I do?")
	options := []store.QuestionOption{
		{Label: "yes, stand this up", Value: "charter:ratify:" + charter.ID},
		{Label: "change when it runs", Value: "charter:cadence:" + charter.ID},
		{Label: "once, not standing", Value: "charter:once:" + charter.ID},
	}
	return strings.Join(lines, "\n"), options
}
