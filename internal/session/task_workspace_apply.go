package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/home"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if err := ctx.Err(); err != nil {
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
	prior, _, priorErr := loadTaskWorkspaceReview(id, use, tree)
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
	review, err := reviewTaskWorkspaceRoot(ctx, use, stage)
	if err != nil {
		return review, err
	}
	review.Copy, review.Snapshot = tree.dir, ticket.Snapshot
	if review.DiffTruncated {
		review.Diff += "\nComplete reviewed diff: git -C " + quoteShWord(tree.dir) + " diff --no-ext-diff --no-textconv " + quoteShWord(tree.checkBase) + " " + quoteShWord(ticket.Snapshot) + " --\n"
	}
	raw, err := json.Marshal(ticket)
	if err != nil {
		return review, err
	}
	path := taskWorkspaceReviewPath(id, use)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return review, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".review-")
	if err != nil {
		return review, err
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
		return review, err
	}
	published = true
	if priorErr == nil {
		cleanupTaskWorkspaceReview(prior)
	}
	return review, nil
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
		if use.Copy == nil || use.Copy.URI == "" || use.Copy.ChatURI == "" {
			return fail(errors.New("this root has no owned working copy"))
		}
		tree, err := runCopyTree(use.Copy, a.config.Place)
		if err != nil {
			return fail(err)
		}
		if use.Copy.Branch != "" && currentBranch(tree.dir) != use.Copy.Branch {
			return fail(errors.New("the saved working copy now stands on another branch"))
		}
		if tree.checkBase == "" {
			return fail(errors.New("the saved working copy has no review baseline"))
		}
		destination := tree.ground
		if tree.mode != TaskModeMirror {
			destination = tree.root
		}
		if destination == "" || canonicalPath(destination) == canonicalPath(tree.dir) {
			return fail(errors.New("this root has no separate locally mapped destination"))
		}
		if err := workspacePathWithoutSymlinks(destination); err != nil {
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
	if use.Outcome == mergeMerged || use.Outcome == mergeInPlace {
		return "", errors.New("this root was already applied; no work was repeated")
	}
	if err := a.taskWorkspaceApplyIdle(g, kept, tree); err != nil {
		return "", err
	}
	ticket, stage, err := loadTaskWorkspaceReview(id, use, tree)
	if err != nil {
		return "", err
	}
	review, err := reviewTaskWorkspaceRoot(ctx, use, stage)
	if err != nil {
		return "", err
	}
	if len(review.Changed) == 0 {
		return "", errors.New("this root has no changes to apply")
	}
	if err := a.admitCompletedRunHomecoming(ctx, epoch); err != nil {
		return "", err
	}
	// The existing landing checks changed originals and preserves both versions on
	// conflict. This local tree already resolves the receiver's chosen binding.
	outcome, detail, _, _ := stage.comeHome(kept.Title, review.Changed, a.signsGitWork())
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
	kept.Report = withReport(kept.Report, receipt)
	a.publishRunRow(g, kept)
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if err := store.write(g); err != nil {
		return receipt, fmt.Errorf("%s\nits actual outcome could not be saved; inspect the destination before retrying: %w", receipt, err)
	}
	if outcome != mergeMerged && outcome != mergeInPlace {
		return receipt, errors.New(receipt)
	}
	cleanupTaskWorkspaceReview(ticket)
	_ = os.Remove(taskWorkspaceReviewPath(id, use))
	return receipt, nil
}

func (a *Agent) taskWorkspaceApplyIdle(g *TaskGraph, selected TaskNotice, tree taskTree) error {
	blocked := func() error {
		return errors.New("this workspace layout may still have a writer or checker; its private work was retained")
	}
	if selected.Settling != "" || selected.Mending != "" {
		return blocked()
	}
	g.mu.Lock()
	for _, node := range g.nodes {
		if node.state.settled() && !node.resolving {
			continue
		}
		if taskStorageOverlaps(node.worktree, tree.dir) || taskStorageOverlaps(node.Ground, tree.dir) {
			g.mu.Unlock()
			return blocked()
		}
	}
	g.mu.Unlock()
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	// An active run can admit another cooperator. Keep its whole layout resident
	// until the owning run's final join and receipt have completed.
	if live {
		return blocked()
	}
	g.mu.Lock()
	rows := g.runRowsLocked()
	g.mu.Unlock()
	base := filepath.Dir(tree.dir)
	for _, row := range rows {
		if row.ID == selected.ID {
			continue
		}
		if row.State.settled() && row.Settling == "" && row.Mending == "" {
			continue
		}
		copies := []*TaskCopyRecord{row.Copy}
		if row.WorkspaceSet != nil {
			for _, root := range row.WorkspaceSet.Roots {
				copies = append(copies, root.Copy)
			}
		}
		identified := false
		for _, copy := range copies {
			if copy == nil {
				continue
			}
			identified = true
			if copy.URI == tree.uri || copy.Dir != "" && (filepath.Dir(copy.Dir) == base || taskStorageOverlaps(copy.Dir, tree.dir)) {
				return blocked()
			}
		}
		if !identified {
			return blocked()
		}
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
}

func taskWorkspaceReviewPath(id uint64, use WorkspaceRootUse) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", id, use.Slot, use.Copy.URI)))
	return filepath.Join(canonicalPath(home.Join("workspace-apply")), hex.EncodeToString(sum[:])+".json")
}

func stageTaskWorkspaceReview(ctx context.Context, id uint64, use WorkspaceRootUse, original taskTree) (taskTree, taskWorkspaceReviewTicket, error) {
	fail := func(err error) (taskTree, taskWorkspaceReviewTicket, error) {
		return taskTree{}, taskWorkspaceReviewTicket{}, err
	}
	snapshot, err := sealGroundWork(original.dir, "review task workspace")
	if err != nil {
		return fail(err)
	}
	if snapshot == "" {
		head, err := gitContext(ctx, original.dir, nil, "rev-parse", "HEAD")
		if err != nil {
			return fail(err)
		}
		snapshot = strings.TrimSpace(head)
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
	if err := SaveMeta(owned, Meta{WorkspacePolicy: DefaultWorkspacePolicy(), Workspace: original.ground}); err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	stage := original
	stage.dir = filepath.Join(place.Trees(), "1", use.Slot)
	stage.branch = taskBranchName("review-" + use.Slot)
	stage.place = place
	stage.uri, stage.chatURI, stage.projectURI, stage.groundURI, stage.rootURI = "", "", "", "", ""
	stage.universe = ""
	baselineDigest := ""
	var baseline []byte
	if original.mode == TaskModeMirror {
		baseline, err = readTaskDropping(original.dir, groundBaselineRecord)
		if err != nil {
			_ = os.RemoveAll(owned)
			return fail(fmt.Errorf("the folder's original comparison baseline is unavailable: %w", err))
		}
		sum := sha256.Sum256(baseline)
		baselineDigest = hex.EncodeToString(sum[:])
	}
	destination := original.ground
	if original.mode != TaskModeMirror {
		destination = original.root
	}
	treeID, err := gitContext(ctx, original.dir, nil, "rev-parse", snapshot+"^{tree}")
	if err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	message := taskWorkspaceSnapshotMessage(use.Copy.URI, destination, original.checkBase, baselineDigest)
	commit, err := gitContext(ctx, original.dir, nil, append(codeafGitIdentity(), "commit-tree", strings.TrimSpace(treeID), "-p", snapshot, "-m", message)...)
	if err != nil {
		_ = os.RemoveAll(owned)
		return fail(err)
	}
	snapshot = strings.TrimSpace(commit)
	ready := false
	defer func() {
		if !ready {
			_, _ = git(original.dir, "worktree", "remove", "--force", stage.dir)
			_, _ = git(original.dir, "branch", "-D", stage.branch)
			_ = os.RemoveAll(owned)
			if stage.uri != "" {
				_ = os.RemoveAll(workspaceRuntimeRoot(stage.uri))
			}
		}
	}()
	if err := os.MkdirAll(filepath.Dir(stage.dir), 0700); err != nil {
		return fail(err)
	}
	if output, err := gitContext(ctx, original.dir, nil, "worktree", "add", "-b", stage.branch, stage.dir, snapshot); err != nil {
		return fail(fmt.Errorf("stage reviewed snapshot: %s: %w", output, err))
	}
	if original.mode == TaskModeMirror {
		if err := os.MkdirAll(filepath.Join(stage.dir, codeafDroppings), 0700); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(filepath.Join(stage.dir, codeafDroppings, groundBaselineRecord), baseline, 0600); err != nil {
			return fail(err)
		}
	}
	stage, err = registerOwnedWorkspace(stage, place)
	if err != nil {
		return fail(err)
	}
	if stage.uri == "" {
		return fail(errors.New("reviewed staging copy could not record ownership"))
	}
	ticket := taskWorkspaceReviewTicket{TaskID: id, Slot: use.Slot, BaselineDigest: baselineDigest, CopyURI: use.Copy.URI, Destination: destination, Snapshot: snapshot, Origin: original.dir, Place: owned, Stage: runCopyOf(stage)}
	ready = true
	return stage, ticket, nil
}

func loadTaskWorkspaceReview(id uint64, use WorkspaceRootUse, original taskTree) (taskWorkspaceReviewTicket, taskTree, error) {
	var ticket taskWorkspaceReviewTicket
	raw, err := os.ReadFile(taskWorkspaceReviewPath(id, use))
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
	if ticket.TaskID != id || ticket.Slot != use.Slot || ticket.CopyURI != use.Copy.URI || ticket.Destination != destination || ticket.Origin != original.dir || filepath.Dir(ticket.Place) != strings.TrimSuffix(taskWorkspaceReviewPath(id, use), ".json") || ticket.Stage == nil {
		return ticket, taskTree{}, errors.New("the reviewed root or its local mapping changed; review it again before applying")
	}
	stage, err := runCopyTree(ticket.Stage, Place{Dir: ticket.Place, Workspace: destination})
	if err != nil {
		return ticket, stage, err
	}
	stageDestination := stage.ground
	if stage.mode != TaskModeMirror {
		stageDestination = stage.root
	}
	if ticket.Stage.Dir != stage.dir || stage.dir != filepath.Join((Place{Dir: ticket.Place}).Trees(), "1", use.Slot) || ticket.Stage.Branch != currentBranch(stage.dir) || stageDestination != destination || stage.ground != original.ground || stage.root != original.root || stage.projectURI != original.projectURI || stage.checkBase != original.checkBase || stage.mode != original.mode || stage.home != original.home || stage.homeSha != original.homeSha {
		return ticket, stage, errors.New("the reviewed staging identity or destination changed; review this root again")
	}
	message, err := git(stage.dir, "show", "-s", "--format=%B", ticket.Snapshot)
	if err != nil || strings.TrimSpace(message) != taskWorkspaceSnapshotMessage(use.Copy.URI, destination, original.checkBase, ticket.BaselineDigest) {
		return ticket, stage, errors.New("the reviewed snapshot binding does not match this task root")
	}
	if original.mode == TaskModeMirror {
		baseline, err := readTaskDropping(stage.dir, groundBaselineRecord)
		if err != nil {
			return ticket, stage, errors.New("the reviewed comparison baseline is missing")
		}
		sum := sha256.Sum256(baseline)
		if hex.EncodeToString(sum[:]) != ticket.BaselineDigest {
			return ticket, stage, errors.New("the reviewed comparison baseline changed")
		}
	}
	head, err := git(stage.dir, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != ticket.Snapshot {
		return ticket, stage, errors.New("the reviewed snapshot changed; review this root again")
	}
	dirty, err := git(stage.dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return ticket, stage, err
	}
	// The mirror's baseline is harness metadata, deliberately outside the commit.
	for _, line := range strings.Split(strings.TrimSpace(dirty), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 || !harnessWrote(strings.TrimSpace(line[3:])) {
			return ticket, stage, errors.New("the reviewed staging copy changed; review this root again")
		}
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
	// Only the generated review worktree and its generated branch are retired.
	_, _ = git(ticket.Origin, "worktree", "remove", "--force", ticket.Stage.Dir)
	_, _ = git(ticket.Origin, "branch", "-D", ticket.Stage.Branch)
	_ = os.RemoveAll(ticket.Place)
	_ = os.RemoveAll(workspaceRuntimeRoot(ticket.Stage.URI))
}

func taskWorkspaceSnapshotMessage(copyURI, destination, base, baselineDigest string) string {
	sum := sha256.Sum256([]byte(copyURI + "\x00" + destination + "\x00" + base + "\x00" + baselineDigest))
	return "CodeAF reviewed workspace snapshot\n\nReview-binding: " + hex.EncodeToString(sum[:])
}
