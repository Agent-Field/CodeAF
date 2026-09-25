//go:build !windows

package app

import (
	"context"
	"fmt"
)

// soloShip is stage 4's tail: bounded verification, then one decision about
// what submitted or unsubmitted tree the run leaves behind.
//
// The rule this file exists to enforce is that the run never ships a tree
// worse than the one it submitted. The comparison is against the frozen
// candidate, not against liveness.
func (runner *pipeline) soloShip(
	ctx context.Context, state *soloState, outcome *soloOutcome, converseErr error,
) {
	candidate := state.candidate()
	if candidate == nil {
		// No model-declared candidate exists, so independently check the exact
		// live tree. Ordinary test failures and incomplete observations keep the
		// benefit of the doubt. A build/parse regression restores the strongest
		// earlier green or coherent checkpoint available.
		outcome.Status = "unsubmitted"
		runner.soloFinalizeUnsubmitted(ctx, state, outcome)
		reason := "no submission: the run stopped without calling submit"
		if converseErr != nil {
			reason += " (" + converseErr.Error() + ")"
		}
		if outcome.RestoreSource != "" {
			reason += "; the live tree's suite could not start and it was restored from " + outcome.RestoreSource
		}
		runner.soloTerminal(outcome, reason)
		return
	}
	outcome.Frozen = candidate

	// Verification needs a live context and time to run. When the run is out of
	// wall budget or its context is already cancelled, there is neither: the
	// only honest thing left is to restore the frozen candidate and say that
	// nothing checked it. Attempting it anyway would record an instantly-failed
	// build as evidence against the candidate, which would be a false red.
	if reason, blocked := runner.verificationUnaffordable(ctx); blocked {
		outcome.Status = "pass-unverified"
		endingReason := fmt.Sprintf(
			"%s; shipping the submitted candidate, which nothing checked: %s",
			reason, candidate.describe(),
		)
		runner.soloRestoreIfDiverged(state, outcome)
		runner.soloTerminal(outcome, endingReason)
		return
	}

	// The candidate is already captured, so verification cannot change what
	// ships -- only what the run says about it. That is the whole point of
	// doing it after the freeze rather than before.
	verification := runner.runProjectVerification(ctx)
	outcome.Verification = &verification
	failing := countFailingEntrypoints(verification)

	var endingReason string
	switch {
	case verification.TimedOut && ctx.Err() != nil:
		// The run was stopped while the check ran. What ships is the frozen
		// candidate, and what the run can truthfully say is that it submitted
		// and nothing finished checking it.
		outcome.Status = "pass-unverified"
		endingReason = fmt.Sprintf(
			"the run was stopped while the project's build and tests ran; "+
				"shipping the submitted candidate, which nothing finished checking: %s",
			candidate.describe(),
		)
	case verification.TimedOut:
		// A hung entrypoint is an incomplete observation, not a verdict. The
		// candidate stands.
		outcome.Status = "pass-unverified"
		endingReason = fmt.Sprintf(
			"verification did not complete (an entrypoint hung); shipping the submitted candidate: %s",
			candidate.describe(),
		)
	case verification.Failed == nil:
		// Failed, not the failing-command count, is the verdict. An expected
		// build or test entrypoint that could not be DISCOVERED sets Failed
		// while recording no command at all, so counting commands would call a
		// project whose suite was never found -- the vacuous-green shape -- a
		// verified pass.
		outcome.Status = "pass"
		endingReason = fmt.Sprintf(
			"submitted, and its build and tests passed: %s (%s)", candidate.describe(), candidate.Reason,
		)
	default:
		// The candidate does not verify. It is still what ships: it is the only
		// tree this run ever declared finished, and there is no better one --
		// the alternative is the unverified live tree, which by construction is
		// the same tree. What changes is the honesty of the terminal.
		outcome.Status = "fail"
		endingReason = fmt.Sprintf(
			"submitted candidate failed verification (%s); "+
				"shipping it anyway as the run's own answer: %s",
			verificationFailureSummary(verification, failing), candidate.describe(),
		)
	}
	// The restore can set later edits aside. Build the terminal only after it
	// finishes so the one ending carries their durable location on every road.
	runner.soloRestoreIfDiverged(state, outcome)
	runner.soloTerminal(outcome, endingReason)
}

// verificationUnaffordable reports whether post-submit verification can still
// be run at all, and why not. Both conditions are ordinary endings rather than
// faults: a run is expected to use its whole budget, and the context is
// cancelled when the wall deadline passes.
func (runner *pipeline) verificationUnaffordable(ctx context.Context) (string, bool) {
	if err := ctx.Err(); err != nil {
		return "the run's context ended before verification could start", true
	}
	if exhausted, reason := runner.budgetExhausted(); exhausted {
		if reason == "" {
			reason = "the run budget was exhausted"
		}
		return reason, true
	}
	return "", false
}

// soloRestoreIfDiverged puts the frozen candidate back if anything moved the
// tree after submission. Nothing in the pipeline should -- the submit tool
// tells the model to stop, and no stage after it edits -- but "should not" is
// not a guarantee, and the check is two git commands.
func (runner *pipeline) soloRestoreIfDiverged(state *soloState, outcome *soloOutcome) {
	candidate := state.candidate()
	if candidate == nil {
		return
	}
	current, err := runner.currentTreeSHA()
	if err != nil {
		runner.note("[senior-dev] ship: could not compare the tree to the frozen candidate: " +
			err.Error() + "\n")
		return
	}
	if current == candidate.TreeSHA {
		runner.events.stage("ship", "unchanged", map[string]any{
			"tree_sha": current, "reason": "tree is identical to the frozen candidate",
		})
		return
	}
	// Diverged. Later edits are not part of the submitted answer, whether
	// they look like improvements or not. The restore first copies their bytes
	// outside the workspace and refuses to proceed if that copy fails.
	// soloRestoreTree rather than a bare checkout: a file ADDED after submit is
	// tracked by eager-commit and would survive an overlay checkout, shipping a
	// tree that silently differs from the frozen candidate it claims to be.
	if err := runner.soloRestoreTree(candidate.CommitSHA, candidate.TreeSHA); err != nil {
		runner.events.stage("ship", "restore-failed", map[string]any{
			"error": err.Error(), "commit_sha": candidate.CommitSHA,
		})
		runner.note("[senior-dev] ship: RESTORE FAILED, shipping the diverged tree: " + err.Error() + "\n")
		return
	}
	runner.events.stage("ship", "restored", map[string]any{
		"from_tree": current, "to_tree": candidate.TreeSHA,
		"commit_sha": candidate.CommitSHA,
		"reason":     "the tree changed after submission; the frozen candidate is the answer",
	})
	runner.note(fmt.Sprintf(
		"[senior-dev] ship: tree changed after submission (%s != %s) — restored the frozen candidate\n",
		shortSHA(current), shortSHA(candidate.TreeSHA),
	))
	// outcome.Status is deliberately untouched: the verdict was about the
	// candidate, and the candidate is what is now on disk again.
}

// soloTerminal records the reason the run ended and the evidence behind it.
// "Why did it exit?" must be answerable from the event stream without a log.
func (runner *pipeline) soloTerminal(outcome *soloOutcome, reason string) {
	data := map[string]any{
		"status": outcome.Status, "reason": reason,
		"submitted": outcome.Frozen != nil, "nudges": outcome.Nudges,
		"landing_turns": outcome.LandingTurns, "terminal_trigger": outcome.TerminalTrigger,
	}
	if outcome.LiveTree != "" {
		data["live_tree"] = outcome.LiveTree
	}
	if outcome.FinalTree != "" {
		data["final_tree"] = outcome.FinalTree
	}
	if outcome.RestoreSource != "" {
		data["restore_source"] = outcome.RestoreSource
	}
	if outcome.SuiteDead {
		data["suite_dead"] = true
	}
	if runner.rescuePath != "" {
		data["rescue_path"] = runner.rescuePath
		if runner.rescueDeleted {
			data["rescue_deletions"] = true
			data["rescue_manifest"] = runner.rescueManifest
		}
	}
	if candidate := outcome.Frozen; candidate != nil {
		data["submission_reason"] = candidate.Reason
		data["submission_evidence"] = candidate.Evidence
		data["checklist_satisfied"] = candidate.ChecklistSatisfied
		data["patch_bytes"] = candidate.PatchBytes
		data["patch_files"] = candidate.PatchFiles
		data["frozen_tree"] = candidate.TreeSHA
		data["frozen_commit"] = candidate.CommitSHA
	}
	if verification := outcome.Verification; verification != nil {
		failing := countFailingEntrypoints(*verification)
		data["verification_failing"] = failing
		data["verification_timed_out"] = verification.TimedOut
		data["verification_commands"] = len(verification.Commands)
		// Why the check failed, when it did, in the same words the run's own
		// reason uses. A failure with no failing command (an expected build or
		// test entrypoint nobody could find) is otherwise indistinguishable
		// from a pass in the counts alone.
		if verification.Failed != nil {
			data["verification_failure"] = verificationFailureSummary(*verification, failing)
		}
	}
	// Deliberately NOT emitted here. There is exactly one terminal event per
	// run and the CLI layer emits it (persistTerminalResult), because that is
	// the one place reached by every ending including a crash before ship. This
	// hands it the payload; emitting a second "terminal" from here would produce
	// two events with one name.
	outcome.TerminalData = data
	outcome.TerminalReason = reason
	runner.note("[senior-dev] terminal: " + reason + "\n")
}

// missingEntrypointFailure reports whether verification failed because an
// expected build or test entrypoint could not be discovered at all, rather
// than because a command it ran came back non-zero. recordMissingEntrypoint
// stamps that source string; it is the only producer of it.
func missingEntrypointFailure(result projectVerificationResult) bool {
	return result.Failed != nil &&
		result.Failed.Source == "manifest/CI/documentation discovery"
}

func verificationFailureSummary(result projectVerificationResult, failing int) string {
	if missingEntrypointFailure(result) {
		return "no " + string(result.Failed.Kind) + " entrypoint could be discovered"
	}
	if failing == 1 {
		return "1 failing entrypoint"
	}
	return fmt.Sprintf("%d failing entrypoints", failing)
}
