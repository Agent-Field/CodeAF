package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// TaskWorkspaceReview describes exactly one local copy and the destination
// resolved from its ownership bindings. Reading it grants no landing permission.
type TaskWorkspaceReview struct {
	Slot, Copy, Destination, Diff string
	Snapshot                      string
	DiffTruncated                 bool
	Changed                       []string
}

// ReviewTaskWorkspaceRoot reads actual committed and uncommitted changes. This
// person-facing door is intentionally absent from model tools.
func (a *Agent) ReviewTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (TaskWorkspaceReview, error) {
	if err := taskWorkspaceOperationEnvironment(ctx); err != nil {
		return TaskWorkspaceReview{}, err
	}
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	g, kept, use, tree, err := a.taskWorkspaceRoot(id, slot)
	if err != nil {
		return TaskWorkspaceReview{}, err
	}
	if use.Outcome == mergeMerged || use.Outcome == mergeInPlace {
		return TaskWorkspaceReview{}, errors.New("this root was already applied; its original private copy is still available for inspection")
	}
	if err := a.taskWorkspaceApplyIdle(g, kept, tree); err != nil {
		return TaskWorkspaceReview{}, err
	}
	prior, priorErr := priorTaskWorkspaceReview(id, use, tree)
	if priorErr != nil {
		return TaskWorkspaceReview{}, priorErr
	}
	stage, ticket, err := stageTaskWorkspaceReview(ctx, id, use, tree)
	if err != nil {
		return TaskWorkspaceReview{}, err
	}
	published := false
	defer func() {
		if !published {
			cleanupTaskWorkspaceReview(ticket)
		}
	}()
	review, err := reviewTaskWorkspacePortableRoot(ctx, use, originalPortableReview{tree: tree, ticket: ticket, stage: stage, checkSource: true})
	if err != nil {
		return review, err
	}
	review.Copy, review.Snapshot = tree.dir, ticket.Snapshot
	if review.DiffTruncated {
		review.Diff += "\nComplete reviewed diff: git -C " + quoteShWord(filepath.Join(ticket.Place, "portable")) + " diff --no-ext-diff --no-textconv " + quoteShWord(tree.checkBase) + " " + quoteShWord(ticket.Snapshot) + " --\n"
	}
	if err := persistTaskWorkspaceReview(taskWorkspaceReviewPath(id, use), ticket); err != nil {
		return review, err
	}
	published = true
	if prior.Snapshot != "" {
		cleanupTaskWorkspaceReview(prior)
	}
	return review, nil
}

// Persist the replacement before retiring the prior reviewed snapshot.
func persistTaskWorkspaceReview(path string, ticket taskWorkspaceReviewTicket) error {
	raw, err := json.Marshal(ticket)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".review-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(raw); err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary.Name(), path)
	}
	if err == nil {
		var directory *os.File
		directory, err = os.Open(filepath.Dir(path))
		if err == nil {
			err = directory.Sync()
			_ = directory.Close()
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (a *Agent) taskWorkspaceRoot(id uint64, slot string) (*TaskGraph, TaskNotice, WorkspaceRootUse, taskTree, error) {
	fail := func(err error) (*TaskGraph, TaskNotice, WorkspaceRootUse, taskTree, error) {
		return nil, TaskNotice{}, WorkspaceRootUse{}, taskTree{}, err
	}
	if a.config.InTask || a.config.taskID != 0 {
		return fail(errors.New("workspace apply belongs to the conversation's person"))
	}
	g := a.tasker()
	if g == nil {
		return fail(errors.New("this conversation has no saved task work"))
	}
	kept, ok := runRowOf(g, id)
	if !ok || kept.State != TaskDone || kept.WorkspaceSet == nil {
		return fail(errors.New("select a completed owned task and one of its root slots"))
	}
	for _, use := range kept.WorkspaceSet.Roots {
		if use.Slot != slot {
			continue
		}
		tree, err := adoptTaskWorkspaceRoot(use, a.config.Place)
		if err != nil {
			return fail(err)
		}
		return g, kept, use, tree, nil
	}
	return fail(fmt.Errorf("task %d has no root slot %q", id, slot))
}

func reviewTaskWorkspaceRoot(ctx context.Context, use WorkspaceRootUse, tree taskTree) (TaskWorkspaceReview, error) {
	review := TaskWorkspaceReview{Slot: use.Slot, Copy: tree.dir, Destination: tree.ground}
	if tree.mode != TaskModeMirror {
		review.Destination = tree.root
	}
	changed, err := gitContext(ctx, tree.dir, nil, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "-z", tree.checkBase, "--")
	if err != nil {
		return review, fmt.Errorf("read root changes: %w", err)
	}
	review.Changed = gitNULPaths(changed)
	diff, err := gitContext(ctx, tree.dir, nil, "diff", "--no-ext-diff", "--no-textconv", tree.checkBase, "--")
	if err != nil {
		return review, fmt.Errorf("read root diff: %w", err)
	}
	review.Diff = diff
	untracked, err := gitContext(ctx, tree.dir, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return review, err
	}
	for _, path := range gitNULPaths(untracked) {
		if harnessWrote(path) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return review, err
		}
		//codeaf:plumbing git compares one local untracked path without external diff helpers.
		cmd := exec.CommandContext(ctx, "git", "diff", "--no-ext-diff", "--no-textconv", "--no-index", "--", "/dev/null", path)
		cmd.Dir = tree.dir
		out, err := cmd.Output()
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return review, fmt.Errorf("read untracked root change: %w", err)
			}
		}
		review.Diff += string(out)
		review.Changed = append(review.Changed, path)
	}
	const previewLimit = 256 * 1024
	if len(review.Diff) > previewLimit {
		review.Diff = review.Diff[:previewLimit] + "\n[Diff preview truncated; use the complete reviewed diff command below.]\n"
		review.DiffTruncated = true
	}
	return review, nil
}

// ApplyTaskWorkspaceRoot grants permission only for this selected operation.
// It never records Landing, starts a worker, or broadens another root's access.
func (a *Agent) ApplyTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (string, error) {
	if err := taskWorkspaceOperationEnvironment(ctx); err != nil {
		return "", err
	}
	epoch, err := a.beltAdmissionEpoch()
	if err != nil {
		return "", err
	}
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	g, kept, use, tree, err := a.taskWorkspaceRoot(id, slot)
	if err != nil {
		return "", err
	}
	if use.Access != WorkspaceWrite {
		return "", errors.New("a read root cannot be applied")
	}
	if (use.Outcome == mergeMerged || use.Outcome == mergeInPlace) && !taskWorkspaceReviewPending(id, use) {
		return "", errors.New("this root was already applied; no work was repeated")
	}
	if err := a.taskWorkspaceApplyIdle(g, kept, tree); err != nil {
		return "", err
	}
	ticket, stage, err := loadTaskWorkspaceReview(id, use, tree)
	if err != nil {
		return "", err
	}
	review, err := reviewTaskWorkspacePortableRoot(ctx, use, originalPortableReview{tree: tree, ticket: ticket, stage: stage})
	if err != nil {
		return "", err
	}
	if len(review.Changed) == 0 {
		return "", errors.New("this root has no changes to apply")
	}
	if err := a.admitCompletedRunHomecoming(ctx, epoch); err != nil {
		return "", err
	}
	// The transaction compares original bytes and metadata before publishing the
	// exact reviewed snapshot. The original private copy stays untouched.
	baseline, err := taskWorkspaceHistoricalBaseline(tree)
	if err != nil {
		return "", err
	}
	receipt, err := applyReviewedWorkspace(ctx, tree, filepath.Join(ticket.Place, "portable"), baseline, ticket.Portable, ticket.Snapshot, review.Changed)
	if err != nil {
		return receipt.Detail, err
	}
	return a.finishTaskWorkspaceApplication(g, kept, use, ticket, receipt)
}

func (a *Agent) recordTaskWorkspaceApplication(g *TaskGraph, kept TaskNotice, use WorkspaceRootUse, ticket taskWorkspaceReviewTicket, outcome, detail string) (string, error) {
	id, slot := kept.ID, use.Slot
	previous := kept
	kept.WorkspaceSet = cloneWorkspaceSet(kept.WorkspaceSet)
	for i := range kept.WorkspaceSet.Roots {
		if kept.WorkspaceSet.Roots[i].Slot == slot {
			kept.WorkspaceSet.Roots[i].Outcome = outcome
		}
	}
	receipt := fmt.Sprintf("task %d root %s: %s (reviewed snapshot %s)", id, slot, outcome, ticket.Snapshot)
	if strings.TrimSpace(detail) != "" {
		receipt += "\n" + detail
	}
	if !strings.Contains(kept.Report, receipt) {
		kept.Report = withReport(kept.Report, receipt)
	}
	a.publishRunRow(g, kept)
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if err := store.write(g); err != nil {
		a.publishRunRow(g, previous)
		return receipt, fmt.Errorf("%s\nits actual outcome could not be saved; retry /task apply to recover its committed receipt without applying again: %w", receipt, err)
	}
	if err := syncTaskWorkspaceCheckpoint(g); err != nil {
		a.publishRunRow(g, previous)
		return receipt, fmt.Errorf("%s\nits checkpoint is not durable; retry /task apply to recover its committed receipt: %w", receipt, err)
	}
	if outcome != mergeMerged && outcome != mergeInPlace {
		return receipt, errors.New(receipt)
	}
	return receipt, nil
}

func (a *Agent) taskWorkspaceApplyIdle(g *TaskGraph, selected TaskNotice, tree taskTree) error {
	blocked := func() error {
		return errors.New("this workspace layout may still have a writer or checker; its private work was retained")
	}
	if selected.Settling != "" || selected.Mending != "" {
		return blocked()
	}
	if taskWorkspaceNodesBusy(g, tree.dir) {
		return blocked()
	}
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	// An active run can admit another cooperator. Keep its whole layout resident
	// until the owning run's final join and receipt have completed.
	if live {
		return blocked()
	}
	if taskWorkspaceRowsBusy(g, selected.ID, tree) {
		return blocked()
	}
	for _, job := range a.jobs.all() {
		if (job.running() || job.group.Alive()) && (taskStorageOverlaps(job.dir, tree.dir) || taskStorageOverlaps(job.dir, tree.ground)) {
			return blocked()
		}
	}
	return nil
}

// Review receipts are local profile state, never portable checkpoint authority.
// The original copy stays resident, including when uncontained writers make
// destructive retirement unsafe. Only this separate immutable snapshot lands.
type taskWorkspaceReviewTicket struct {
	CopyURI, Destination, Snapshot, Origin, Place string
	TaskID                                        uint64
	Slot, BaselineDigest                          string
	Stage                                         *TaskCopyRecord
	Portable                                      taskcopy.PortableState
	DeletedDirectories                            []string
}

func taskWorkspaceReviewPath(id uint64, use WorkspaceRootUse) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", id, use.Slot, use.Copy.URI)))
	return filepath.Join(canonicalPath(home.Join("workspace-apply")), hex.EncodeToString(sum[:])+".json")
}

func stageTaskWorkspaceReview(ctx context.Context, id uint64, use WorkspaceRootUse, original taskTree) (taskTree, taskWorkspaceReviewTicket, error) {
	fail := func(err error) (taskTree, taskWorkspaceReviewTicket, error) {
		return taskTree{}, taskWorkspaceReviewTicket{}, err
	}
	if err := checkTaskWorkspaceWithheldOutputs(original); err != nil {
		return fail(err)
	}
	root := strings.TrimSuffix(taskWorkspaceReviewPath(id, use), ".json")
	if err := os.MkdirAll(root, 0700); err != nil {
		return fail(err)
	}
	owned, err := os.MkdirTemp(root, "snapshot-")
	if err != nil {
		return fail(err)
	}
	place := Place{Dir: owned, Workspace: original.ground}
	if err := SaveMeta(owned, Meta{ID: place.ID(), WorkspacePolicy: DefaultWorkspacePolicy(), Workspace: original.ground}); err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	stage := original
	stage.dir = filepath.Join(place.Trees(), "1", use.Slot)
	stage.branch = taskBranchName("review-" + use.Slot)
	stage.place = place
	stage.uri, stage.chatURI, stage.projectURI, stage.groundURI, stage.rootURI = "", "", "", "", ""
	stage.universe = ""
	portable, err := taskcopy.CapturePortableSnapshot(original.dir, filepath.Join(owned, "carrier"), filepath.Join(owned, "portable"))
	if err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	repository := filepath.Join(owned, "portable")
	snapshot, err := sealTaskWorkspaceReview(ctx, original, repository)
	if err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	deletedDirectories, portableDigest, err := describeTaskWorkspaceSnapshot(original, portable)
	if err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	bound, err := bindTaskWorkspaceSnapshot(ctx, use, original, snapshot, portableDigest, repository)
	if err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	snapshot = bound.commit
	baseline, baselineDigest, destination := bound.baseline, bound.baselineDigest, bound.destination
	ready := false
	defer func() {
		if !ready {
			_, _ = git(repository, "worktree", "remove", "--force", stage.dir)
			_, _ = git(repository, "branch", "-D", stage.branch)
			_ = os.RemoveAll(owned)
			if stage.uri != "" {
				_ = os.RemoveAll(workspaceRuntimeRoot(stage.uri))
			}
		}
	}()
	if err := os.MkdirAll(filepath.Dir(stage.dir), 0700); err != nil {
		return fail(err)
	}
	if output, err := gitContext(ctx, repository, nil, "worktree", "add", "-b", stage.branch, stage.dir, snapshot); err != nil {
		return fail(fmt.Errorf("stage reviewed snapshot: %s: %w", output, err))
	}
	if err := materializeTaskWorkspaceBaseline(stage, baseline); err != nil {
		return fail(err)
	}
	stage, err = registerTaskWorkspaceReviewStage(stage, place)
	if err != nil {
		return fail(err)
	}
	ticket := taskWorkspaceReviewTicket{Portable: portable, DeletedDirectories: deletedDirectories, TaskID: id, Slot: use.Slot, BaselineDigest: baselineDigest, CopyURI: use.Copy.URI, Destination: destination, Snapshot: snapshot, Origin: original.dir, Place: owned, Stage: runCopyOf(stage)}
	ready = true
	return stage, ticket, nil
}

func loadTaskWorkspaceReview(id uint64, use WorkspaceRootUse, original taskTree) (taskWorkspaceReviewTicket, taskTree, error) {
	var ticket taskWorkspaceReviewTicket
	raw, err := readTaskWorkspaceReview(taskWorkspaceReviewPath(id, use))
	if err != nil {
		return ticket, taskTree{}, errors.New("review this root first with /task review; no reviewed local snapshot is available")
	}
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return ticket, taskTree{}, err
	}
	destination := original.ground
	if original.mode != TaskModeMirror {
		destination = original.root
	}
	if err := validateTaskWorkspaceReviewTicket(id, use, original, ticket, destination); err != nil {
		return ticket, taskTree{}, err
	}
	stage, err := runCopyTree(ticket.Stage, Place{Dir: ticket.Place, Workspace: destination})
	if err != nil {
		return ticket, stage, err
	}
	if err := validateTaskWorkspaceReviewStage(use, original, ticket, stage, destination); err != nil {
		return ticket, stage, err
	}
	if err := validateTaskWorkspaceSnapshotBinding(use, original, ticket, stage, destination); err != nil {
		return ticket, stage, err
	}
	if err := validateTaskWorkspaceComparisonBaseline(original, ticket, stage); err != nil {
		return ticket, stage, err
	}
	if err := verifyTaskWorkspaceSnapshotFiles(ticket, stage); err != nil {
		return ticket, stage, err
	}
	return ticket, stage, nil
}

func cleanupTaskWorkspaceReview(ticket taskWorkspaceReviewTicket) {
	if ticket.Stage == nil || ticket.CopyURI == "" || ticket.TaskID == 0 || !validWorkspaceSlot(ticket.Slot) {
		return
	}
	use := WorkspaceRootUse{Slot: ticket.Slot, Copy: &TaskCopyRecord{URI: ticket.CopyURI}}
	if filepath.Dir(ticket.Place) != strings.TrimSuffix(taskWorkspaceReviewPath(ticket.TaskID, use), ".json") || ticket.Stage.Dir != filepath.Join((Place{Dir: ticket.Place}).Trees(), "1", ticket.Slot) || !strings.HasPrefix(ticket.Stage.Branch, "task/review-") {
		return
	}
	resolved, err := resolveOwnedWorkspace(ticket.Stage, Place{Dir: ticket.Place})
	if err != nil || resolved != ticket.Stage.Dir {
		return
	}
	if err := validateTaskWorkspaceReviewRepository(ticket, resolved); err != nil {
		return
	}
	repository := filepath.Join(ticket.Place, "portable")
	// Only the generated review repository, worktree and branch are retired.
	_, _ = git(repository, "worktree", "remove", "--force", ticket.Stage.Dir)
	_, _ = git(repository, "branch", "-D", ticket.Stage.Branch)
	_ = os.RemoveAll(ticket.Place)
	_ = os.RemoveAll(workspaceRuntimeRoot(ticket.Stage.URI))
}

func taskWorkspaceSnapshotMessage(copyURI, destination, base, baselineDigest, portableDigest, creationDigest string) string {
	sum := sha256.Sum256([]byte(copyURI + "\x00" + destination + "\x00" + base + "\x00" + baselineDigest + "\x00" + portableDigest + "\x00" + creationDigest))
	return "CodeAF reviewed workspace snapshot\n\nReview-binding: " + hex.EncodeToString(sum[:])
}

func adoptTaskWorkspaceRoot(use WorkspaceRootUse, place Place) (taskTree, error) {
	if use.Copy == nil || use.Copy.URI == "" || use.Copy.ChatURI == "" {
		return taskTree{}, errors.New("this root has no owned working copy")
	}
	tree, err := runCopyTree(use.Copy, place)
	if err != nil {
		return taskTree{}, err
	}
	if use.Copy.Branch != "" && currentBranch(tree.dir) != use.Copy.Branch {
		return taskTree{}, errors.New("the saved working copy now stands on another branch")
	}
	if tree.checkBase == "" {
		return taskTree{}, errors.New("the saved working copy has no review baseline")
	}
	destination := tree.ground
	if tree.mode != TaskModeMirror {
		destination = tree.root
	}
	if destination == "" || canonicalPath(destination) == canonicalPath(tree.dir) {
		return taskTree{}, errors.New("this root has no separate locally mapped destination")
	}
	if err := workspacePathWithoutSymlinks(destination); err != nil {
		return taskTree{}, err
	}
	return tree, nil
}

func taskWorkspaceNodesBusy(g *TaskGraph, path string) bool {
	g.mu.Lock()
	for _, node := range g.nodes {
		if node.state.settled() && !node.resolving {
			continue
		}
		if taskStorageOverlaps(node.worktree, path) || taskStorageOverlaps(node.Ground, path) {
			g.mu.Unlock()
			return true
		}
	}
	g.mu.Unlock()
	return false
}

func taskWorkspaceRowsBusy(g *TaskGraph, selected uint64, tree taskTree) bool {
	g.mu.Lock()
	rows := g.runRowsLocked()
	g.mu.Unlock()
	for _, row := range rows {
		if taskWorkspaceRowBusy(row, selected, tree) {
			return true
		}
	}
	return false
}
func taskWorkspaceRowBusy(row TaskNotice, selected uint64, tree taskTree) bool {
	if row.ID == selected {
		return false
	}
	if row.State.settled() && row.Settling == "" && row.Mending == "" {
		return false
	}
	copies := taskWorkspaceRowCopies(row)
	identified := false
	for _, copy := range copies {
		if copy == nil {
			continue
		}
		identified = true
		if copy.URI == tree.uri || copy.Dir != "" && (filepath.Dir(copy.Dir) == filepath.Dir(tree.dir) || taskStorageOverlaps(copy.Dir, tree.dir)) {
			return true
		}
	}
	return !identified
}
func taskWorkspaceRowCopies(row TaskNotice) []*TaskCopyRecord {
	copies := []*TaskCopyRecord{row.Copy}
	if row.WorkspaceSet != nil {
		for _, root := range row.WorkspaceSet.Roots {
			copies = append(copies, root.Copy)
		}
	}
	return copies
}

func validateTaskWorkspaceReviewTicket(id uint64, use WorkspaceRootUse, original taskTree, ticket taskWorkspaceReviewTicket, destination string) error {
	if ticket.TaskID != id || ticket.Slot != use.Slot || ticket.CopyURI != use.Copy.URI || ticket.Destination != destination || ticket.Origin != original.dir || filepath.Dir(ticket.Place) != strings.TrimSuffix(taskWorkspaceReviewPath(id, use), ".json") || ticket.Stage == nil {
		return errors.New("the reviewed root or its local mapping changed; review it again before applying")
	}
	return nil
}
func validateTaskWorkspaceReviewStage(use WorkspaceRootUse, original taskTree, ticket taskWorkspaceReviewTicket, stage taskTree, destination string) error {
	stageDestination := stage.ground
	if stage.mode != TaskModeMirror {
		stageDestination = stage.root
	}
	if ticket.Stage.Dir != stage.dir || stage.dir != filepath.Join((Place{Dir: ticket.Place}).Trees(), "1", use.Slot) || ticket.Stage.Branch != currentBranch(stage.dir) || stageDestination != destination || stage.ground != original.ground || stage.root != original.root || stage.projectURI != original.projectURI || stage.checkBase != original.checkBase || stage.mode != original.mode || stage.home != original.home || stage.homeSha != original.homeSha {
		return errors.New("the reviewed staging identity or destination changed; review this root again")
	}
	return validateTaskWorkspaceReviewRepository(ticket, stage.dir)
}
func validateTaskWorkspaceSnapshotBinding(use WorkspaceRootUse, original taskTree, ticket taskWorkspaceReviewTicket, stage taskTree, destination string) error {
	portableDigest, err := taskWorkspacePortableReviewDigest(ticket.Portable, ticket.DeletedDirectories)
	if err != nil {
		return err
	}
	message, err := git(stage.dir, "show", "-s", "--format=%B", ticket.Snapshot)
	if err != nil || strings.TrimSpace(message) != taskWorkspaceSnapshotMessage(use.Copy.URI, destination, original.checkBase, ticket.BaselineDigest, portableDigest, original.portableBaselineDigest) {
		return errors.New("the reviewed snapshot binding does not match this task root")
	}
	return nil
}
func validateTaskWorkspaceComparisonBaseline(original taskTree, ticket taskWorkspaceReviewTicket, stage taskTree) error {
	if original.mode == TaskModeMirror {
		baseline, err := readTaskDropping(stage.dir, groundBaselineRecord)
		if err != nil {
			return errors.New("the reviewed comparison baseline is missing")
		}
		sum := sha256.Sum256(baseline)
		if hex.EncodeToString(sum[:]) != ticket.BaselineDigest {
			return errors.New("the reviewed comparison baseline changed")
		}
	}
	return nil
}

func verifyTaskWorkspaceSnapshotFiles(ticket taskWorkspaceReviewTicket, stage taskTree) error {
	head, err := git(stage.dir, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != ticket.Snapshot {
		return errors.New("the reviewed snapshot changed; review this root again")
	}
	dirty, err := git(stage.dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	// The mirror's baseline is harness metadata, deliberately outside the commit.
	for _, line := range strings.Split(strings.TrimSpace(dirty), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 || !harnessWrote(strings.TrimSpace(line[3:])) {
			return errors.New("the reviewed staging copy changed; review this root again")
		}
	}
	return nil
}

type taskWorkspaceSnapshot struct {
	commit, baselineDigest, destination string
	baseline                            []byte
}

func bindTaskWorkspaceSnapshot(ctx context.Context, use WorkspaceRootUse, original taskTree, snapshot, portableDigest, repository string) (taskWorkspaceSnapshot, error) {
	var err error
	baselineDigest := ""
	var baseline []byte
	if original.mode == TaskModeMirror {
		baseline, err = readTaskDropping(original.dir, groundBaselineRecord)
		if err != nil {
			return taskWorkspaceSnapshot{}, fmt.Errorf("the folder's original comparison baseline is unavailable: %w", err)
		}
		sum := sha256.Sum256(baseline)
		baselineDigest = hex.EncodeToString(sum[:])
	}
	destination := original.ground
	if original.mode != TaskModeMirror {
		destination = original.root
	}
	treeID, err := gitContext(ctx, repository, nil, "rev-parse", snapshot+"^{tree}")
	if err != nil {
		return taskWorkspaceSnapshot{}, err
	}
	message := taskWorkspaceSnapshotMessage(use.Copy.URI, destination, original.checkBase, baselineDigest, portableDigest, original.portableBaselineDigest)
	commit, err := gitContext(ctx, repository, nil, append(codeafGitIdentity(), "commit-tree", strings.TrimSpace(treeID), "-p", snapshot, "-m", message)...)
	if err != nil {
		return taskWorkspaceSnapshot{}, err
	}
	snapshot = strings.TrimSpace(commit)
	return taskWorkspaceSnapshot{commit: snapshot, baseline: baseline, baselineDigest: baselineDigest, destination: destination}, nil
}

func materializeTaskWorkspaceBaseline(stage taskTree, baseline []byte) error {
	if stage.mode == TaskModeMirror {
		if err := os.MkdirAll(filepath.Join(stage.dir, codeafDroppings), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage.dir, codeafDroppings, groundBaselineRecord), baseline, 0600); err != nil {
			return err
		}
	}
	return nil
}

func taskWorkspacePortableReviewDigest(state taskcopy.PortableState, deleted []string) (string, error) {
	if _, err := state.Digest(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		State   taskcopy.PortableState
		Deleted []string
	}{state, deleted})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func taskWorkspaceDeletedDirectories(original taskTree, reviewed taskcopy.PortableState) ([]string, error) {
	baseline, err := taskWorkspaceHistoricalBaseline(original)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for path, entry := range baseline.Entries {
		if entry.Kind == "dir" {
			if _, exists := reviewed.Entries[path]; !exists {
				candidates = append(candidates, path)
			}
		}
	}
	actual, err := taskcopy.InspectPortablePaths(original.dir, candidates)
	if err != nil {
		return nil, err
	}
	var deleted []string
	for _, path := range candidates {
		if _, present := actual.Entries[path]; !present {
			deleted = append(deleted, path)
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

type originalPortableReview struct {
	checkSource bool
	tree, stage taskTree
	ticket      taskWorkspaceReviewTicket
}

func reviewTaskWorkspacePortableRoot(ctx context.Context, use WorkspaceRootUse, input originalPortableReview) (TaskWorkspaceReview, error) {
	review, err := reviewTaskWorkspaceRoot(ctx, use, input.stage)
	if err != nil {
		return review, err
	}
	baseline, err := taskWorkspaceHistoricalBaseline(input.tree)
	if err != nil {
		return review, err
	}
	review.Changed = append(review.Changed, input.ticket.DeletedDirectories...)
	delta, err := workspaceApplicationDelta(baseline, input.ticket.Portable, review.Changed)
	if err != nil {
		return review, err
	}
	touched := describeTaskWorkspacePortableChanges(&review, delta)
	// Before publishing a review, refuse source metadata the portable snapshot
	// cannot represent. Later edits to the original remain private after review.
	if input.checkSource {
		if err = taskcopy.CheckPortableMutationMetadataContext(ctx, input.tree.dir, touched); err != nil {
			return review, err
		}
	}
	sort.Strings(review.Changed)
	const previewLimit = 256 * 1024
	if len(review.Diff) > previewLimit {
		review.Diff = review.Diff[:previewLimit] + "\n[Portable review preview truncated.]\n"
		review.DiffTruncated = true
	}
	return review, nil
}

func syncTaskWorkspaceCheckpoint(g *TaskGraph) error {
	path := g.store.pathOf()
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	err = errors.Join(file.Sync(), file.Close())
	if err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func taskWorkspaceReviewPending(id uint64, use WorkspaceRootUse) bool {
	info, err := os.Lstat(taskWorkspaceReviewPath(id, use))
	return err == nil && info.Mode().IsRegular()
}

func sealTaskWorkspaceReview(ctx context.Context, original taskTree, repository string) (string, error) {
	if _, err := taskWorkspaceHistoricalBaseline(original); err != nil {
		return "", err
	}
	snapshot, err := sealGroundWork(repository, "review task workspace")
	if err != nil {
		return "", err
	}
	if snapshot != "" {
		return snapshot, nil
	}
	head, err := gitContext(ctx, repository, nil, "rev-parse", "HEAD")
	return strings.TrimSpace(head), err
}

func describeTaskWorkspacePortableChanges(review *TaskWorkspaceReview, delta []workspaceApplicationEntry) []string {
	paths := map[string]bool{}
	for _, path := range review.Changed {
		paths[path] = true
	}
	var touched []string
	for _, change := range delta {
		touched = append(touched, change.Path)
		if !change.Guard && !paths[change.Path] {
			review.Changed = append(review.Changed, change.Path)
			paths[change.Path] = true
		}
		if !change.Guard && change.After != nil && (change.Before == nil || change.Before.Mode != change.After.Mode || change.After.Kind == "dir") {
			review.Diff += fmt.Sprintf("\nPortable representation: %s (%s %04o)\n", change.Path, change.After.Kind, change.After.Mode)
		}
	}

	return touched
}

func (a *Agent) finishTaskWorkspaceApplication(g *TaskGraph, kept TaskNotice, use WorkspaceRootUse, ticket taskWorkspaceReviewTicket, receipt taskWorkspaceApplyReceipt) (string, error) {
	message, err := a.recordTaskWorkspaceApplication(g, kept, use, ticket, receipt.Outcome, receipt.Detail)
	if err != nil {
		return message, err
	}
	if err = acknowledgeWorkspaceApplication(receipt.Transaction); err != nil {
		return message, err
	}
	cleanupTaskWorkspaceReview(ticket)
	_ = os.Remove(taskWorkspaceReviewPath(kept.ID, use))
	return message, nil
}

func validateTaskWorkspaceReviewRepository(ticket taskWorkspaceReviewTicket, stage string) error {
	repository := filepath.Join(ticket.Place, "portable")
	if err := workspacePathWithoutSymlinks(repository); err != nil {
		return err
	}
	common, err := git(stage, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || strings.TrimSpace(common) != filepath.Join(repository, ".git") {
		return errors.New("reviewed stage no longer belongs to its generated private repository")
	}
	return nil
}

func taskWorkspaceOperationEnvironment(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if taskWorkspaceGitRoutingVariable(upper) {
			return fmt.Errorf("unset %s and relaunch CodeAF before reviewing or applying a task root", name)
		}
	}
	return nil
}
func taskWorkspaceGitRoutingVariable(name string) bool {
	switch name {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_NAMESPACE", "GIT_GRAFT_FILE", "GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE", "GIT_QUARANTINE_PATH":
		return true
	}
	return strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

func readTaskWorkspaceReview(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 32<<20 {
		return nil, errors.New("review receipt exceeds its supported bound")
	}
	return raw, nil
}

func priorTaskWorkspaceReview(id uint64, use WorkspaceRootUse, original taskTree) (taskWorkspaceReviewTicket, error) {
	var ticket taskWorkspaceReviewTicket
	raw, err := readTaskWorkspaceReview(taskWorkspaceReviewPath(id, use))
	if errors.Is(err, os.ErrNotExist) {
		return ticket, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &ticket)
	}
	destination := original.ground
	if original.mode != TaskModeMirror {
		destination = original.root
	}
	if err == nil {
		err = validateTaskWorkspaceReviewTicket(id, use, original, ticket, destination)
	}
	if err != nil {
		return ticket, fmt.Errorf("existing review recovery information is unreadable; retain this private copy and retry /task apply before replacing its review: %w", err)
	}
	if err = workspaceApplicationReviewFence(ticket.Snapshot, destination); err != nil {
		return ticket, err
	}
	// A damaged old stage is retained, never silently replaced or cleaned.
	if _, _, err = loadTaskWorkspaceReview(id, use, original); err != nil {
		return ticket, fmt.Errorf("existing review needs recovery; retry /task apply before replacing its review: %w", err)
	}
	return ticket, nil
}

func describeTaskWorkspaceSnapshot(original taskTree, portable taskcopy.PortableState) ([]string, string, error) {
	deleted, err := taskWorkspaceDeletedDirectories(original, portable)
	if err != nil {
		return nil, "", err
	}
	digest, err := taskWorkspacePortableReviewDigest(portable, deleted)
	return deleted, digest, err
}

func registerTaskWorkspaceReviewStage(stage taskTree, place Place) (taskTree, error) {
	stage, err := registerOwnedWorkspace(stage, place)
	if err != nil {
		return stage, err
	}
	if stage.uri == "" {
		return stage, errors.New("reviewed staging copy could not record ownership")
	}
	return stage, nil
}
