//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

// A rescue can include newly ignored files that the person did not expect
// codeaf to copy. Twenty-five MiB covers ordinary source files, while ten
// such files fit in one rescue; larger generated output stays in the project
// and leaves the checkout unchecked instead of filling the state root.
const rescueFileLimitBytes int64 = 25 << 20
const rescueTotalLimitBytes int64 = 250 << 20

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
				outcome.RestoreFailed = err.Error()
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
	paths, err := runner.recorder.DifferentPaths(commitSHA)
	if err != nil {
		return fmt.Errorf("read files that a restore would replace: %w", err)
	}
	if len(paths) > 0 {
		if err := runner.rescueBeforeRestore(paths); err != nil {
			return fmt.Errorf("keep later files before restoring: %w", err)
		}
	}
	return runner.recorder.Restore(commitSHA, wantTree)
}

// rescueBeforeRestore copies the current bytes outside the repository BEFORE
// a recorder's forceful restore. The state root is durable and separate from
// the person's tracked tree; a copy failure refuses the destructive restore.
func (runner *pipeline) rescueBeforeRestore(paths []string) error {
	var deleted []string
	var existing []string
	var total int64
	for _, path := range paths {
		info, err := os.Lstat(filepath.Join(runner.workspace, filepath.FromSlash(path)))
		switch {
		case os.IsNotExist(err):
			deleted = append(deleted, path)
		case err != nil:
			return err
		default:
			if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("cannot preserve %s before restore", path)
			}
			if info.Size() > rescueFileLimitBytes {
				return fmt.Errorf("a changed file is larger than %d MiB: %s", rescueFileLimitBytes>>20, path)
			}
			if info.Size() < 0 || info.Size() > rescueTotalLimitBytes-total {
				return fmt.Errorf("changed files exceed the %d MiB rescue limit", rescueTotalLimitBytes>>20)
			}
			total += info.Size()
			existing = append(existing, path)
		}
	}
	// The deletion manifest is a rescued file too. Check its prospective size
	// before making any rescue folder or touching the project.
	var manifestBytes int64
	for _, path := range deleted {
		if strings.ContainsAny(path, "\r\n") {
			path = strconv.Quote(path)
		}
		entryBytes := int64(len(path) + 1)
		if entryBytes > rescueFileLimitBytes-manifestBytes || entryBytes > rescueTotalLimitBytes-total-manifestBytes {
			return fmt.Errorf("deletion manifest exceeds the rescue size limit")
		}
		manifestBytes += entryBytes
	}
	if len(deleted) == 0 && len(existing) == 0 {
		return nil
	}
	root := home.Join("v3", "carried", "senior-dev", "rescued")
	if relative, err := filepath.Rel(runner.workspace, root); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
		root = filepath.Join(os.TempDir(), "codeaf-rescued")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	if runner.rescuePath == "" {
		created, err := os.MkdirTemp(root, "run-")
		if err != nil {
			return err
		}
		runner.rescuePath = created
	}
	destination := runner.rescuePath
	if runner.rescueCount > 0 {
		destination = filepath.Join(destination, fmt.Sprintf("later-%d", runner.rescueCount+1))
	}
	remaining := rescueTotalLimitBytes - manifestBytes
	for _, path := range existing {
		from := filepath.Join(runner.workspace, filepath.FromSlash(path))
		info, err := os.Lstat(from)
		if err != nil {
			return err
		}
		to := filepath.Join(destination, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		switch {
		case info.Mode().IsRegular():
			copied, err := copyRescueFile(from, to, remaining)
			if err != nil {
				return err
			}
			remaining -= copied
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(from)
			if err != nil {
				return err
			}
			if int64(len(link)) > rescueFileLimitBytes || int64(len(link)) > remaining {
				return fmt.Errorf("a changed file is larger than the rescue size limit: %s", path)
			}
			if err := os.Symlink(link, to); err != nil {
				return err
			}
			remaining -= int64(len(link))
		default:
			return fmt.Errorf("cannot preserve %s before restore", from)
		}
	}
	if len(deleted) > 0 {
		if err := os.MkdirAll(runner.rescuePath, 0o700); err != nil {
			return err
		}
		if runner.rescueManifest == "" {
			runner.rescueManifest = "deleted-files.txt"
			for number := 2; ; number++ {
				_, err := os.Lstat(filepath.Join(runner.rescuePath, runner.rescueManifest))
				if os.IsNotExist(err) {
					break
				}
				if err != nil {
					return err
				}
				runner.rescueManifest = fmt.Sprintf("deleted-files-%d.txt", number)
			}
		}
		manifest := filepath.Join(runner.rescuePath, runner.rescueManifest)
		prior, err := os.ReadFile(manifest)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		listed := map[string]bool{}
		for _, path := range strings.Split(string(prior), "\n") {
			if path != "" {
				listed[path] = true
			}
		}
		for _, path := range deleted {
			// A filename can contain a newline. Quote only that exceptional
			// spelling so the manifest still has one readable line per path.
			if strings.ContainsAny(path, "\r\n") {
				path = strconv.Quote(path)
			}
			listed[path] = true
		}
		all := make([]string, 0, len(listed))
		for path := range listed {
			all = append(all, path)
		}
		sort.Strings(all)
		body := []byte(strings.Join(all, "\n") + "\n")
		if int64(len(body)) > rescueFileLimitBytes || int64(len(body)) > remaining+manifestBytes {
			return fmt.Errorf("deletion manifest exceeds the rescue size limit")
		}
		if err := os.WriteFile(manifest, body, 0o600); err != nil {
			return err
		}
		if err := os.Chmod(manifest, 0o600); err != nil {
			return err
		}
		runner.rescueDeleted = true
	}
	runner.rescueCount++
	return nil
}

// copyRescueFile copies no more than either bound and refuses a source that
// grew since preflight. The extra read detects growth without copying it.
func copyRescueFile(source, destination string, remaining int64) (copied int64, err error) {
	limit := min(rescueFileLimitBytes, remaining)
	in, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(destination)
		}
	}()
	copied, err = io.CopyN(out, in, limit)
	if err != nil && err != io.EOF {
		_ = out.Close()
		return copied, err
	}
	var extra [1]byte
	n, readErr := in.Read(extra[:])
	if readErr != nil && readErr != io.EOF {
		_ = out.Close()
		return copied, readErr
	}
	if n > 0 {
		_ = out.Close()
		return copied, fmt.Errorf("a changed file is larger than the %d MiB per-file or %d MiB total rescue limit: %s", rescueFileLimitBytes>>20, rescueTotalLimitBytes>>20, source)
	}
	if err = out.Close(); err != nil {
		return copied, err
	}
	if err = os.Chmod(destination, 0o600); err != nil {
		return copied, err
	}
	return copied, nil
}
