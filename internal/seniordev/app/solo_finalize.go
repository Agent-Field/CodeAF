//go:build !windows

package app

import (
	"context"
	"errors"
	"time"
)

// soloLandingReserve sizes the landing window: two fifteenths of the wall
// budget, at least 45 seconds and at most soloLandingReserveCap, but never more
// than a quarter of the run so short runs keep most of their time for work.
func soloLandingReserve(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	reserve := limit * 2 / 15
	if reserve < 45*time.Second {
		reserve = 45 * time.Second
	}
	if reserve > soloLandingReserveCap {
		reserve = soloLandingReserveCap
	}
	if maximum := limit / 4; reserve > maximum {
		reserve = maximum
	}
	return reserve
}

func (runner *pipeline) soloWorkContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if runner.budget.MaxWallMS == nil {
		return context.WithCancel(ctx)
	}
	limit := time.Duration(*runner.budget.MaxWallMS * float64(time.Millisecond))
	deadline := runner.wallStart.Add(limit - soloLandingReserve(limit))
	if parent, ok := ctx.Deadline(); ok && !parent.After(deadline) {
		return context.WithCancel(ctx)
	}
	return context.WithDeadlineCause(ctx, deadline, errSoloLanding)
}

func (runner *pipeline) soloCaptureStart(state *soloState) error {
	treeSHA, err := runner.currentTreeSHA()
	if err != nil {
		return err
	}
	commitSHA, err := runner.soloRecordTree(treeSHA, "senior-dev: exact starting tree")
	if err != nil {
		return err
	}
	// The starting tree is also reachable by name, so the compaction
	// changed-files record (engine_compaction.go) and anything outside this
	// process can diff against it without knowing the commit.
	if err := runner.recorder.Publish(soloStartRef, commitSHA); err != nil {
		runner.note("[senior-dev] start: could not update " + soloStartRef + ": " + err.Error() + "\n")
	}
	state.setStart(soloCheckpoint{
		CommitSHA: commitSHA, TreeSHA: treeSHA, Source: "starting-tree",
	})
	runner.events.stage("landing", "start-captured", map[string]any{"tree_sha": treeSHA})
	return nil
}

func (runner *pipeline) soloLandingTurn(
	ctx context.Context, goal string, state *soloState, outcome *soloOutcome,
) error {
	findings := runner.soloUnsubmittedFindings(state.baseSHA)
	verification := runner.soloCheckUnsubmitted(
		ctx, state, soloCheckTimeout, "landing",
	)
	if verification != nil {
		outcome.Verification = verification
		findings = append(findings, soloVerificationFindings(*verification)...)
	}
	if runner.budgetIsExhausted() || ctx.Err() != nil {
		return nil
	}
	outcome.LandingTurns++
	runner.events.stage("landing", "repair-turn", map[string]any{
		"timeout_ms": soloLandingTurnTimeout.Milliseconds(),
	})
	landingCtx, cancel := context.WithTimeout(ctx, soloLandingTurnTimeout)
	defer cancel()
	_, err := runner.soloTurn(landingCtx, goal, soloLandingPrompt(findings))
	if candidate := state.candidate(); candidate != nil {
		outcome.TerminalTrigger = "submitted-during-landing"
		outcome.SubmissionReason = candidate.Reason
		return nil
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) {
		outcome.TerminalTrigger = "landing-turn-error"
		return err
	}
	return nil
}

// soloCheckUnsubmitted executes the standard entrypoints itself. It never
// trusts the model's shell pipeline exit status: `cargo build | tail` reports
// success while the build fails.
func (runner *pipeline) soloCheckUnsubmitted(
	ctx context.Context,
	state *soloState,
	maximum time.Duration,
	phase string,
) *projectVerificationResult {
	change, err := runner.soloTreeChange(state.baseSHA)
	if err != nil || !change.changed {
		return nil
	}
	if runner.lastVerify != nil && runner.lastVerifyTreeSHA == change.treeSHA {
		remembered := *runner.lastVerify
		return &remembered
	}
	if ctx.Err() != nil || maximum <= 0 {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, maximum)
	defer cancel()
	verify := runner.verifyForTest
	if verify == nil {
		verify = runner.runProjectVerification
	}
	result := verify(checkCtx)
	runner.rememberVerifiedTree(result)
	command, dead := verificationShowsDeadTree(result)
	_, unsafe := verificationShowsSafetyRegression(result)
	runner.events.stage("landing", "checked", map[string]any{
		"phase": phase, "tree_sha": change.treeSHA,
		"commands": len(result.Commands), "timed_out": result.TimedOut,
		"failing": countFailingEntrypoints(result), "suite_dead": dead,
		"safety_regression": unsafe,
		"dead_command":      command,
	})
	if !result.TimedOut && len(result.Commands) > 0 && !unsafe {
		runner.soloCaptureCoherent(state, change.treeSHA, "coherent-checkpoint")
	}
	return &result
}

// soloCaptureCoherent records a tree whose verification completed without a
// build, parse or suite-start regression, as the checkpoint an unsubmitted
// dead tree is restored to.
func (runner *pipeline) soloCaptureCoherent(state *soloState, treeSHA, source string) {
	commitSHA, err := runner.soloRecordTree(treeSHA, "senior-dev: coherent "+source+" checkpoint")
	if err != nil {
		return
	}
	state.setCoherent(soloCheckpoint{CommitSHA: commitSHA, TreeSHA: treeSHA, Source: source})
}

func soloVerificationFindings(result projectVerificationResult) []string {
	if command, dead := verificationShowsDeadTree(result); dead {
		return []string{
			"independent verification proves the suite cannot start: `" + command + "`",
			"the exact failure is: " + verificationFailureSummary(
				result, countFailingEntrypoints(result),
			),
		}
	}
	if result.TimedOut {
		return []string{"independent verification did not complete; do not claim it passed"}
	}
	if result.Failed != nil {
		return []string{"independent verification failed: " + verificationFailureSummary(
			result, countFailingEntrypoints(result),
		)}
	}
	if len(result.Commands) > 0 {
		return []string{"independent verification passed; finish the checklist and call submit"}
	}
	return nil
}

func (runner *pipeline) soloFinalizeUnsubmitted(
	ctx context.Context, state *soloState, outcome *soloOutcome,
) {
	if live, err := runner.currentTreeSHA(); err == nil {
		outcome.LiveTree = live
	}
	verification := runner.soloCheckUnsubmitted(
		ctx, state, soloFinalCheckTimeout, "final",
	)
	if verification != nil {
		outcome.Verification = verification
		_, outcome.SuiteDead = verificationShowsDeadTree(*verification)
	}
	if outcome.SuiteDead {
		// A tree whose suite cannot start is restored to the strongest
		// earlier checkpoint: the latest coherent one, else the starting
		// tree, else the base commit.
		start, coherent := state.checkpoints()
		var target *soloCheckpoint
		if coherent != nil && coherent.TreeSHA != outcome.LiveTree {
			target = coherent
		}
		if target == nil && start != nil && start.TreeSHA != outcome.LiveTree {
			target = start
		}
		if target == nil && state.baseSHA != "" {
			if tree, ok := runner.recorder.BaseTree(state.baseSHA); ok {
				target = &soloCheckpoint{
					CommitSHA: state.baseSHA, TreeSHA: tree, Source: "starting-commit",
				}
			}
		}
		if target != nil {
			if err := runner.soloRestoreCheckpoint(*target); err != nil {
				runner.events.stage("landing", "restore-failed", map[string]any{
					"source": target.Source, "error": err.Error(),
				})
			} else {
				outcome.RestoreSource = target.Source
				runner.events.stage("landing", "restored", map[string]any{
					"source": target.Source, "from_tree": outcome.LiveTree,
					"to_tree": target.TreeSHA,
				})
			}
		}
	}
	if final, err := runner.currentTreeSHA(); err == nil {
		outcome.FinalTree = final
	}
}

func (runner *pipeline) soloRestoreCheckpoint(checkpoint soloCheckpoint) error {
	return runner.soloRestoreTree(checkpoint.CommitSHA, checkpoint.TreeSHA)
}

// soloRestoreTree makes the working tree the recorded one and proves it did.
// How that is achieved is the recorder's business; both implementations
// re-identify the result rather than trusting the operation.
func (runner *pipeline) soloRestoreTree(commitSHA, wantTree string) error {
	return runner.recorder.Restore(commitSHA, wantTree)
}
