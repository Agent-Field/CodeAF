package session

import (
	"bytes"
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
	"reflect"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// Review must retain the ticket that names an application awaiting acknowledgement.
// This check never creates, recovers, or deletes transaction state.
func workspaceApplicationReviewFence(identity, destination string) error {
	sum := sha256.Sum256([]byte(identity + "\x00" + destination))
	base := home.Join("workspace-applications")
	if _, err := os.Lstat(base); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := workspacePathWithoutSymlinks(base); err != nil {
		return err
	}
	root := filepath.Join(canonicalPath(base), hex.EncodeToString(sum[:]))
	_, err := readWorkspaceApplication(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pending application recovery is unreadable; retain this review and retry /task apply: %w", err)
	}
	return errors.New("this review has a pending application; retry /task apply before reviewing again")
}

func persistWorkspaceApplication(root string, j *workspaceApplicationJournal) error {
	path := filepath.Join(root, "journal.json")
	raw, err := json.Marshal(j)
	if len(raw) > taskcopy.WorkspaceCheckpointLimit {
		return errors.New("application recovery journal exceeds its bound")
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, "journal-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncWorkspaceApplicationDirectory(root)
}
func readWorkspaceApplication(root string) (workspaceApplicationJournal, error) {
	var j workspaceApplicationJournal
	path := filepath.Join(root, "journal.json")
	info, err := os.Lstat(path)
	if err != nil {
		return j, err
	}
	if !info.Mode().IsRegular() || info.Size() > taskcopy.WorkspaceCheckpointLimit {
		return j, errors.New("invalid application recovery journal")
	}
	raw, err := readWorkspaceApplicationBounded(path, taskcopy.WorkspaceCheckpointLimit)
	if err != nil {
		return j, err
	}
	err = json.Unmarshal(raw, &j)
	return j, err
}
func validateWorkspaceApplicationJournal(root, identity, destination string, j workspaceApplicationJournal) error {
	sum := sha256.Sum256([]byte(identity + "\x00" + destination))
	if filepath.Base(root) != hex.EncodeToString(sum[:]) {
		return errors.New("application journal identity does not name its generated root")
	}
	if j.Version != 1 || j.Nonce == "" || j.Identity != identity || j.Destination != destination || j.Staging != workspaceApplicationStaging(root, destination) || j.Receipt.Transaction != root {
		return errors.New("application recovery identity disagrees with this reviewed root")
	}
	if err := workspacePathWithoutSymlinks(root); err != nil {
		return err
	}
	for _, entry := range j.Entries {
		if err := validWorkspaceApplicationPath(entry.Path); err != nil {
			return err
		}
	}
	return validateWorkspaceApplicationGitJournal(destination, &j)
}
func validateWorkspaceApplicationGitJournal(destination string, j *workspaceApplicationJournal) error {
	if j.Git != nil {
		index, err := git(destination, "rev-parse", "--path-format=absolute", "--git-path", "index")
		if err != nil {
			return err
		}
		if strings.TrimSpace(index) != j.Git.Index || j.Git.Branch == "" || j.Git.BeforeHead == "" || j.Git.AfterHead == "" {
			return errors.New("application recovery Git binding changed")
		}
		if err := verifyWorkspaceApplicationGitBinding(j); err != nil {
			return err
		}

	}
	return nil
}

func recoverWorkspaceApplication(root, identity, destination string, expected []workspaceApplicationEntry, outcome string, gitExpected bool) (taskWorkspaceApplyReceipt, bool, error) {
	j, err := readWorkspaceApplication(root)
	if os.IsNotExist(err) {
		return taskWorkspaceApplyReceipt{}, false, nil
	}
	if err != nil {
		return taskWorkspaceApplyReceipt{}, true, err
	}
	if err := validateWorkspaceApplicationJournal(root, identity, destination, j); err != nil {
		return taskWorkspaceApplyReceipt{}, true, err
	}
	if !workspaceApplicationIntentMatches(j, expected, outcome, gitExpected) {
		return taskWorkspaceApplyReceipt{}, true, errors.New("recovery journal does not match the immutable reviewed changes")
	}

	switch j.Phase {
	case "committed":
		if err := verifyWorkspaceApplicationCommitted(&j); err != nil {
			return j.Receipt, true, fmt.Errorf("reviewed application was committed, but its destination subsequently changed; no replay was performed: %w", err)
		}
		return j.Receipt, true, nil
	case "preparing", "prepared", "rolledback":
		if err := verifyWorkspaceApplicationBefore(&j); err != nil {
			return taskWorkspaceApplyReceipt{}, true, fmt.Errorf("recovery phase disagrees with destination; all recovery material retained: %w", err)
		}
		if err := discardWorkspaceApplication(root, &j); err != nil {
			return taskWorkspaceApplyReceipt{}, true, err
		}
		return taskWorkspaceApplyReceipt{}, false, nil
	case "applying":
		if err := rollbackWorkspaceApplication(root, &j); err != nil {
			return taskWorkspaceApplyReceipt{}, true, fmt.Errorf("interrupted application requires recovery at %s: %w", root, err)
		}
		return taskWorkspaceApplyReceipt{}, false, nil
	default:
		return taskWorkspaceApplyReceipt{}, true, errors.New("unsupported application recovery phase")
	}
}

func workspaceApplicationIntentMatches(j workspaceApplicationJournal, expected []workspaceApplicationEntry, outcome string, gitExpected bool) bool {
	return !(!reflect.DeepEqual(j.Entries, expected) || j.Receipt.Detail != "" || j.Receipt.Outcome != outcome || !reflect.DeepEqual(j.Receipt.Changes, workspaceApplicationChangedPaths(expected)) || (gitExpected && j.Phase != "preparing" && j.Git == nil) || (!gitExpected && j.Git != nil))
}

// Checkpoint acknowledgement is the only door that releases a committed
// transaction's independent preimages. A lost acknowledgement remains replayable.
func acknowledgeWorkspaceApplication(root string) error {
	base := canonicalPath(home.Join("workspace-applications"))
	if filepath.Dir(root) != base || len(filepath.Base(root)) != 64 {
		return errors.New("invalid application acknowledgement")
	}
	j, err := readWorkspaceApplication(root)
	if err != nil {
		return err
	}
	if err := validateWorkspaceApplicationJournal(root, j.Identity, j.Destination, j); err != nil {
		return err
	}
	if j.Phase != "committed" {
		return errors.New("application is not durably committed")
	}
	return discardWorkspaceApplication(root, &j)
}
func discardWorkspaceApplication(root string, j *workspaceApplicationJournal) error {
	if j.Staging != workspaceApplicationStaging(root, j.Destination) {
		return errors.New("application staging is not generated for this destination")
	}
	if err := releaseWorkspaceApplicationIndex(j); err != nil {
		return err
	}
	if err := discardWorkspaceApplicationStage(root, j); err != nil {
		return err
	}
	for _, name := range []string{"index-before", "index-after", "index-commit", "index-owned", "index-rollback", "stage-cleanup"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if err := discardWorkspaceApplicationTemps(root, j.Nonce); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationDirectory(root); err != nil {
		return err
	}

	if err := os.Remove(filepath.Join(root, "journal.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncWorkspaceApplicationDirectory(root)
}

type workspaceApplicationCleanup struct{ Transaction, Identity, Destination, Nonce, Stage string }

func workspaceApplicationCleanupState(root string, j *workspaceApplicationJournal) (workspaceApplicationCleanup, error) {
	identity, err := workspaceApplicationDirectoryIdentity(j.Staging)
	return workspaceApplicationCleanup{root, j.Identity, j.Destination, j.Nonce, identity}, err
}
func workspaceApplicationCleanupMatches(root string, j *workspaceApplicationJournal) bool {
	expected, err := workspaceApplicationCleanupState(root, j)
	if err != nil {
		return false
	}
	raw, err := readWorkspaceApplicationBounded(filepath.Join(root, "stage-cleanup"), 4096)
	if err != nil {
		return false
	}
	var actual workspaceApplicationCleanup
	return json.Unmarshal(raw, &actual) == nil && actual == expected
}
func persistWorkspaceApplicationCleanup(root string, j *workspaceApplicationJournal) error {
	state, err := workspaceApplicationCleanupState(root, j)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, "cleanup-"+j.Nonce+"-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(root, "stage-cleanup")); err != nil {
		return err
	}
	return syncWorkspaceApplicationDirectory(root)
}
func discardWorkspaceApplicationStage(root string, j *workspaceApplicationJournal) error {
	_, err := os.Lstat(j.Staging)
	if os.IsNotExist(err) {
		return syncWorkspaceApplicationDirectory(filepath.Dir(j.Staging))
	}
	if err != nil {
		return err
	}
	if !j.StagingOwned && !workspaceApplicationOwnerMatches(root, j) {
		return nil
	}
	if !workspaceApplicationOwnerMatches(root, j) {
		return errors.New("application staging ownership marker changed; no cleanup performed")
	}
	if err := workspacePathWithoutSymlinks(j.Staging); err != nil {
		return err
	}
	if err := verifyWorkspaceApplicationHeld(j); err != nil {
		return err
	}
	if err := persistWorkspaceApplicationCleanup(root, j); err != nil {
		return err
	}
	if err := os.RemoveAll(j.Staging); err != nil {
		return err
	}
	return syncWorkspaceApplicationDirectory(filepath.Dir(j.Staging))
}
func discardWorkspaceApplicationTemps(root, nonce string) error {
	f, err := os.Open(root)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		entries, readErr := f.ReadDir(64)
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, "index-rollback-"+nonce+"-") && !strings.HasPrefix(name, "cleanup-"+nonce+"-") {
				continue
			}
			if entry.IsDir() {
				return errors.New("recovery scratch file replaced by a directory; retained")
			}
			if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func rollbackWorkspaceApplication(root string, j *workspaceApplicationJournal) error {
	if j.Phase != "applying" {
		return discardWorkspaceApplication(root, j)
	}
	if err := preflightWorkspaceApplicationRollback(j); err != nil {
		return err
	}
	if err := rollbackWorkspaceApplicationGit(j); err != nil {
		return err
	}
	if err := prepareWorkspaceApplicationRollbackDirectories(j); err != nil {
		return err
	}
	for _, index := range workspaceApplicationRollbackOrder(j.Entries) {
		if err := rollbackWorkspaceApplicationEntry(j, index); err != nil {
			return err
		}
	}
	if err := applyWorkspaceApplicationDirectoryModes(context.Background(), j, true); err != nil {
		return err
	}
	if err := verifyWorkspaceApplicationBefore(j); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationDestination(j, true); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationGitPublished(j); err != nil {
		return err
	}
	j.Phase = "rolledback"
	if err := persistWorkspaceApplication(root, j); err != nil {
		return err
	}
	return discardWorkspaceApplication(root, j)
}
func workspaceApplicationRollbackOrder(entries []workspaceApplicationEntry) []int {
	indices := make([]int, len(entries))
	for index := range entries {
		indices[index] = index
	}
	sort.Slice(indices, func(i, k int) bool { return len(entries[indices[i]].Path) > len(entries[indices[k]].Path) })
	return indices
}
func prepareWorkspaceApplicationRollbackDirectories(j *workspaceApplicationJournal) error {
	entries := append([]workspaceApplicationEntry(nil), j.Entries...)
	sort.Slice(entries, func(i, k int) bool { return len(entries[i].Path) < len(entries[k].Path) })
	for _, entry := range entries {
		if entry.Guard {
			continue
		}
		if entry.Before == nil || entry.Before.Kind != "dir" {
			continue
		}
		path := filepath.Join(j.Destination, filepath.FromSlash(entry.Path))
		actual, err := workspaceApplicationReadEntry(path)
		if err != nil {
			return err
		}
		if actual == nil {
			if err := workspaceApplicationCreateDirectory(j.Destination, entry.Path, entry.Before.Mode.Perm()); err != nil {
				return err
			}
			continue
		}
		if actual.Kind != "dir" {
			continue
		}
		if !reflect.DeepEqual(actual, entry.Before) && !reflect.DeepEqual(actual, entry.After) {
			return fmt.Errorf("directory changed outside application: %s", entry.Path)
		}
		if err := workspaceApplicationChmod(j.Destination, entry.Path, entry.Before.Mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}
func rollbackWorkspaceApplicationEntry(j *workspaceApplicationJournal, index int) error {
	entry := j.Entries[index]
	if entry.Guard {
		return nil
	}
	target := filepath.Join(j.Destination, filepath.FromSlash(entry.Path))
	actual, err := workspaceApplicationReadEntry(target)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(actual, entry.Before) {
		return nil
	}
	if actual != nil && !reflect.DeepEqual(actual, entry.After) && !workspaceApplicationOwnDirectory(actual, entry) {
		return fmt.Errorf("external changes preserved at %s; automatic rollback refused", entry.Path)
	}
	if actual != nil {
		if err := workspaceApplicationRemove(j.Destination, entry.Path, actual.Kind == "dir"); err != nil {
			return err
		}
	}
	if entry.Before == nil {
		return nil
	}
	if entry.Before.Kind == "dir" {
		return workspaceApplicationCreateDirectory(j.Destination, entry.Path, entry.Before.Mode.Perm())
	}
	return restoreWorkspaceApplicationOriginal(j, index, entry)
}
func restoreWorkspaceApplicationOriginal(j *workspaceApplicationJournal, index int, entry workspaceApplicationEntry) error {
	held := workspaceApplicationPayload(j.Staging, "held", index)
	if _, err := os.Lstat(held); err == nil {
		if err := verifyWorkspaceApplicationEntry(held, entry.Before); err != nil {
			return err
		}
		return workspaceApplicationRenameExclusive(held, j.Destination, entry.Path)
	} else if !os.IsNotExist(err) {
		return err
	}
	old := workspaceApplicationPayload(j.Staging, "old", index)
	if err := verifyWorkspaceApplicationEntry(old, entry.Before); err != nil {
		return err
	}
	return restoreWorkspaceApplicationPreimage(old, j.Destination, entry.Path, *entry.Before)
}
func workspaceApplicationOwnDirectory(actual *taskcopy.PortableEntry, entry workspaceApplicationEntry) bool {
	return actual != nil && entry.Before == nil && actual.Kind == "dir" && entry.After != nil && entry.After.Kind == "dir" && actual.Mode == 0700
}

func restoreWorkspaceApplicationPreimage(old, root, relative string, entry taskcopy.PortableEntry) error {
	temporary, err := os.CreateTemp(filepath.Dir(old), "rollback-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	defer os.Remove(name)
	if err := copyWorkspaceApplicationEntry(old, name, entry); err != nil {
		return err
	}
	return workspaceApplicationRenameExclusive(name, root, relative)
}

func verifyWorkspaceApplicationHeld(j *workspaceApplicationJournal) error {
	var paths []string
	for index, entry := range j.Entries {
		if entry.Before == nil || entry.Before.Kind == "dir" {
			continue
		}
		held := workspaceApplicationPayload(j.Staging, "held", index)
		if _, err := os.Lstat(held); os.IsNotExist(err) {
			continue
		}
		paths = append(paths, filepath.ToSlash(filepath.Join("held", fmt.Sprintf("%08d", index))))
		if err := verifyWorkspaceApplicationEntry(held, entry.Before); err != nil {
			return fmt.Errorf("an existing writer changed retained preimages; recovery material preserved: %w", err)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return taskcopy.CheckPortableMutationMetadata(j.Staging, paths)
}
func preflightWorkspaceApplicationRollback(j *workspaceApplicationJournal) error {
	if err := verifyWorkspaceApplicationHeld(j); err != nil {
		return err
	}
	if err := errors.Join(taskcopy.CheckPortableMutationMetadata(j.Destination, workspaceApplicationPaths(j.Entries)), taskcopy.CheckPortableRootInheritance(context.Background(), j.Destination)); err != nil {
		return err
	}
	for _, entry := range j.Entries {
		actual, err := workspaceApplicationReadEntry(filepath.Join(j.Destination, filepath.FromSlash(entry.Path)))
		if err != nil {
			return err
		}
		interimDir := actual != nil && actual.Kind == "dir" && entry.Before == nil && entry.After != nil && entry.After.Kind == "dir" && actual.Mode == 0700
		if actual != nil && !reflect.DeepEqual(actual, entry.Before) && !reflect.DeepEqual(actual, entry.After) && !interimDir {
			return fmt.Errorf("external changes preserved at %s; rollback refused", entry.Path)
		}
	}
	return preflightWorkspaceApplicationGitRollback(j)
}
func verifyWorkspaceApplicationCommitted(j *workspaceApplicationJournal) error {
	if err := verifyWorkspaceApplicationAfter(j); err != nil {
		return err
	}
	if j.Git == nil {
		return nil
	}
	if currentBranch(j.Destination) != j.Git.Branch || branchCommit(j.Destination, j.Git.Branch) != j.Git.AfterHead {
		return errors.New("destination branch changed since application committed")
	}
	_, err := verifyWorkspaceApplicationIndex(j.Git.Index, j.Git.AfterIndexDigest)
	return err
}

func syncWorkspaceApplicationDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func syncWorkspaceApplicationAncestors(path string) error {
	for {
		if err := syncWorkspaceApplicationDirectory(path); err != nil {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
func syncWorkspaceApplicationTree(root string) error {
	var directories []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, path := range directories {
		if err := syncWorkspaceApplicationDirectory(path); err != nil {
			return err
		}
	}
	return nil
}
func syncWorkspaceApplicationDestination(j *workspaceApplicationJournal, before bool) error {
	directories := map[string]bool{j.Destination: true, filepath.Dir(j.Destination): true, j.Staging: true, filepath.Join(j.Staging, "old"): true, filepath.Join(j.Staging, "new"): true, filepath.Join(j.Staging, "held"): true}
	for _, entry := range j.Entries {
		if value := workspaceApplicationEntryState(entry, before); value != nil && value.Kind == "file" {
			if err := syncWorkspaceApplicationFile(filepath.Join(j.Destination, filepath.FromSlash(entry.Path))); err != nil {
				return err
			}
		}
	}

	for _, entry := range j.Entries {
		path := filepath.Join(j.Destination, filepath.FromSlash(entry.Path))
		directories[filepath.Dir(path)] = true
		if value := workspaceApplicationEntryState(entry, before); value != nil && value.Kind == "dir" {
			directories[path] = true
		}
	}
	ordered := make([]string, 0, len(directories))
	for path := range directories {
		ordered = append(ordered, path)
	}
	sort.Slice(ordered, func(i, k int) bool { return len(ordered[i]) > len(ordered[k]) })
	for _, path := range ordered {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		}
		if err := syncWorkspaceApplicationDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func prepareWorkspaceApplicationGit(ctx context.Context, root string, j *workspaceApplicationJournal, original taskTree, reviewedDir string) error {
	if err := checkWorkspaceApplicationGitStorage(ctx, j.Destination); err != nil {
		return err
	}
	if err := refuseWorkspaceApplicationSelectedGitChanges(ctx, j); err != nil {
		return err
	}
	state, err := workspaceApplicationGitDestination(ctx, j.Destination)
	if err != nil {
		return err
	}

	commitIndex, nextIndex, err := prepareWorkspaceApplicationIndices(ctx, root, j.Destination, state)
	if err != nil {
		return err
	}

	j.Git = state
	if err := updateWorkspaceApplicationGitEntries(ctx, j, reviewedDir, commitIndex, nextIndex); err != nil {
		return err
	}
	tree, err := gitContext(ctx, j.Destination, []string{"GIT_INDEX_FILE=" + commitIndex}, "-c", "core.splitIndex=false", "write-tree")
	if err != nil {
		return err
	}
	state.AfterIndexDigest, err = workspaceApplicationFileDigest(nextIndex)
	if err != nil {
		return err
	}
	j.Git = state
	args := append(codeafGitIdentity(), "commit-tree", strings.TrimSpace(tree), "-p", state.BeforeHead, "-m", workspaceApplicationGitBinding(j))
	commit, err := gitContext(ctx, j.Destination, nil, args...)
	if err != nil {
		return err
	}
	state.AfterHead = strings.TrimSpace(commit)
	state.AfterIndexDigest, err = workspaceApplicationFileDigest(nextIndex)
	if err != nil {
		return err
	}
	j.Git = state
	return nil
}
func checkWorkspaceApplicationGitStorage(ctx context.Context, destination string) error {
	storage, err := workspaceApplicationGitInput(ctx, destination, nil, "", "config", "--local", "--get", "extensions.refStorage")
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return fmt.Errorf("cannot verify Git reference storage; private copy retained: %w", err)
	}
	if value := strings.TrimSpace(storage); value != "" && value != "files" {
		return errors.New("portable apply requires Git files reference storage; this repository's reference storage is unsupported and the private copy was retained")
	}
	split, err := workspaceApplicationGitInput(ctx, destination, nil, "", "config", "--bool", "--get", "core.splitIndex")
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return err
	}
	if strings.TrimSpace(split) == "true" {
		return errors.New("portable apply cannot yet recover a configured split index; disable core.splitIndex and run git update-index --no-split-index in the destination, then retry /task apply; private copy retained")
	}
	shared, err := workspaceApplicationGitInput(ctx, destination, nil, "", "rev-parse", "--shared-index-path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(shared) != "" {
		return errors.New("portable apply cannot yet recover a split index; run git update-index --no-split-index in the destination, then retry /task apply; private copy retained")
	}
	return nil
}

func workspaceApplicationGitDestination(ctx context.Context, destination string) (*workspaceApplicationGit, error) {
	branch := currentBranch(destination)
	if branch == "" {
		return nil, errors.New("destination checkout is detached")
	}
	head, err := gitContext(ctx, destination, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	index, err := gitContext(ctx, destination, nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return nil, err
	}
	state := &workspaceApplicationGit{Branch: branch, BeforeHead: strings.TrimSpace(head), Index: strings.TrimSpace(index)}
	if err := workspacePathWithoutSymlinks(filepath.Dir(state.Index)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(state.Index); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("destination Git index is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return state, nil
}

func prepareWorkspaceApplicationIndices(ctx context.Context, root, destination string, state *workspaceApplicationGit) (string, string, error) {
	before := filepath.Join(root, "index-before")
	exists, err := copyWorkspaceApplicationIndex(state.Index, before)
	if err != nil {
		return "", "", err
	}
	state.BeforeIndexExists = exists
	state.BeforeIndexDigest, err = workspaceApplicationOptionalDigest(before, exists)
	if err != nil {
		return "", "", err
	}
	commitIndex := filepath.Join(root, "index-commit")
	if err := os.Remove(commitIndex); err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	if _, err := gitContext(ctx, destination, []string{"GIT_INDEX_FILE=" + commitIndex}, "-c", "core.splitIndex=false", "read-tree", state.BeforeHead); err != nil {
		return "", "", err
	}
	nextIndex := filepath.Join(root, "index-after")
	if exists {
		if _, err := copyWorkspaceApplicationIndex(before, nextIndex); err != nil {
			return "", "", err
		}
	} else {
		if _, err := gitContext(ctx, destination, []string{"GIT_INDEX_FILE=" + nextIndex}, "-c", "core.splitIndex=false", "read-tree", state.BeforeHead); err != nil {
			return "", "", err
		}
	}
	return commitIndex, nextIndex, nil
}

func copyWorkspaceApplicationIndex(source, target string) (bool, error) {
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	input, err := os.Open(source)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	_, copyErr := io.Copy(output, input)
	return true, errors.Join(copyErr, output.Sync(), output.Close())
}
func updateWorkspaceApplicationGitEntries(ctx context.Context, j *workspaceApplicationJournal, reviewedDir string, indices ...string) error {
	records, err := workspaceApplicationGitRecords(ctx, j, reviewedDir)
	if err != nil {
		return err
	}
	for _, index := range indices {
		if _, err := workspaceApplicationGitInput(ctx, j.Destination, []string{"GIT_INDEX_FILE=" + index}, records, "-c", "core.splitIndex=false", "update-index", "-z", "--index-info"); err != nil {
			return err
		}
	}
	return nil
}
func workspaceApplicationGitRecords(ctx context.Context, j *workspaceApplicationJournal, reviewedDir string) (string, error) {
	objects, err := workspaceApplicationNewObjects(ctx, j, reviewedDir)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, entry := range j.Entries {
		if entry.Guard || entry.After != nil && entry.After.Kind == "dir" || entry.After == nil && entry.Before != nil && entry.Before.Kind == "dir" {
			continue
		}
		mode, object := "0", strings.Repeat("0", len(j.Git.BeforeHead))
		if entry.After != nil {
			mode = workspaceApplicationGitMode(*entry.After)
			object = objects[entry.Path]
			if object == "" {
				return "", errors.New("prepared Git blob is missing")
			}
		}
		fmt.Fprintf(&out, "%s %s\t%s%c", mode, object, entry.Path, 0)
	}
	return out.String(), nil
}
func workspaceApplicationGitMode(entry taskcopy.PortableEntry) string {
	if entry.Kind == "symlink" {
		return "120000"
	}
	if entry.Mode.Perm()&0111 != 0 {
		return "100755"
	}
	return "100644"
}
func workspaceApplicationNewObjects(ctx context.Context, j *workspaceApplicationJournal, reviewedDir string) (map[string]string, error) {
	var files, links, linkInputs []string
	for index, entry := range j.Entries {
		if entry.Guard || entry.After == nil || entry.After.Kind == "dir" {
			continue
		}
		if entry.After.Kind == "file" {
			files = append(files, entry.Path)
			continue
		}
		path := filepath.Join(j.Staging, fmt.Sprintf("link-%08d", index))
		if err := os.WriteFile(path, []byte(entry.After.Link), 0600); err != nil {
			return nil, err
		}
		links = append(links, entry.Path)
		linkInputs = append(linkInputs, path)
	}
	result, err := workspaceApplicationBatchObjects(ctx, j.Destination, reviewedDir, files, true, false)
	if err != nil {
		return nil, err
	}
	linked, err := workspaceApplicationBatchObjects(ctx, j.Destination, j.Destination, linkInputs, true, true)
	if err != nil {
		return nil, err
	}
	for index, path := range links {
		result[path] = linked[linkInputs[index]]
	}
	return result, nil
}
func workspaceApplicationOptionalDigest(path string, exists bool) (string, error) {
	if !exists {
		return "absent", nil
	}
	return workspaceApplicationFileDigest(path)
}
func verifyWorkspaceApplicationGitBefore(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	state := j.Git
	if currentBranch(j.Destination) != state.Branch || branchCommit(j.Destination, state.Branch) != state.BeforeHead {
		return errors.New("destination Git branch moved; application refused")
	}
	_, err := verifyWorkspaceApplicationIndex(state.Index, state.BeforeIndexDigest)
	return err
}
func verifyWorkspaceApplicationIndex(path, want string) (bool, error) {
	info, err := os.Lstat(path)
	exists := err == nil
	if exists && !info.Mode().IsRegular() {
		return true, errors.New("Git index is not a regular file; recovery retained")
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	actual, err := workspaceApplicationOptionalDigest(path, exists)
	if err != nil {
		return false, err
	}
	if actual != want {
		return exists, errors.New("destination Git index changed; recovery retained")
	}
	return exists, nil
}
func claimWorkspaceApplicationIndex(root string, j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	source := filepath.Join(root, "index-after")
	if digest, err := workspaceApplicationFileDigest(source); err != nil || digest != j.Git.AfterIndexDigest {
		return errors.New("prepared Git index changed")
	}
	owned := filepath.Join(root, "index-owned")
	if err := copyWorkspaceApplicationIndexExclusive(source, owned); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationDirectory(root); err != nil {
		return err
	}
	if err := os.Link(owned, j.Git.Index+".lock"); err != nil {
		return fmt.Errorf("destination Git index is busy: %w", err)
	}
	return syncWorkspaceApplicationDirectory(filepath.Dir(j.Git.Index))
}
func copyWorkspaceApplicationIndexExclusive(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Sync(), output.Close())
}
func publishWorkspaceApplicationGit(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	state := j.Git
	if err := verifyWorkspaceApplicationGitBefore(j); err != nil {
		return err
	}
	if _, err := git(j.Destination, "update-ref", "refs/heads/"+state.Branch, state.AfterHead, state.BeforeHead); err != nil {
		return err
	}
	if currentBranch(j.Destination) != state.Branch {
		return errors.New("destination checkout changed during application")
	}
	if err := os.Rename(state.Index+".lock", state.Index); err != nil {
		return err
	}
	return syncWorkspaceApplicationDirectory(filepath.Dir(state.Index))
}
func preflightWorkspaceApplicationGitRollback(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	state := j.Git
	if currentBranch(j.Destination) != state.Branch {
		return errors.New("destination checkout changed; Git recovery retained")
	}
	head := branchCommit(j.Destination, state.Branch)
	if head != state.BeforeHead && head != state.AfterHead {
		return errors.New("destination branch has external commits; Git recovery retained")
	}
	if _, err := verifyWorkspaceApplicationIndex(state.Index, state.BeforeIndexDigest); err == nil {
		return nil
	}
	_, err := verifyWorkspaceApplicationIndex(state.Index, state.AfterIndexDigest)
	return err
}
func rollbackWorkspaceApplicationGit(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	state := j.Git
	if err := preflightWorkspaceApplicationGitRollback(j); err != nil {
		return err
	}
	if err := restoreWorkspaceApplicationIndex(j); err != nil {
		return err
	}
	if branchCommit(j.Destination, state.Branch) == state.AfterHead {
		if _, err := git(j.Destination, "update-ref", "refs/heads/"+state.Branch, state.BeforeHead, state.AfterHead); err != nil {
			return err
		}
	}
	return nil
}
func restoreWorkspaceApplicationIndex(j *workspaceApplicationJournal) error {
	state := j.Git
	if _, err := verifyWorkspaceApplicationIndex(state.Index, state.BeforeIndexDigest); err == nil {
		return releaseWorkspaceApplicationIndex(j)
	}
	if err := releaseWorkspaceApplicationIndex(j); err != nil {
		return err
	}
	root := j.Receipt.Transaction
	rollback := filepath.Join(root, "index-rollback")
	if state.BeforeIndexExists {
		if err := prepareWorkspaceApplicationRollbackIndex(root, rollback, state.BeforeIndexDigest, j.Nonce); err != nil {
			return err
		}
		if err := os.Link(rollback, state.Index+".lock"); err != nil {
			return err
		}
		if _, err := verifyWorkspaceApplicationIndex(state.Index, state.AfterIndexDigest); err != nil {
			return err
		}
		if err := os.Rename(state.Index+".lock", state.Index); err != nil {
			return err
		}
		return syncWorkspaceApplicationDirectory(filepath.Dir(state.Index))
	}
	if _, err := verifyWorkspaceApplicationIndex(state.Index, state.AfterIndexDigest); err != nil {
		return err
	}
	return os.Remove(state.Index)
}
func prepareWorkspaceApplicationRollbackIndex(root, target, digest, nonce string) error {
	if _, err := os.Lstat(target); err == nil {
		if _, err = verifyWorkspaceApplicationIndex(target, digest); err != nil {
			return err
		}
		return syncWorkspaceApplicationDirectory(root)
	} else if !os.IsNotExist(err) {
		return err
	}
	name, err := copyWorkspaceApplicationRollbackCandidate(root, nonce)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	if _, err := verifyWorkspaceApplicationIndex(name, digest); err != nil {
		return err
	}
	if err := workspaceApplicationRenameExclusive(name, root, filepath.Base(target)); err != nil && !os.IsExist(err) {
		return err
	}
	if _, err := verifyWorkspaceApplicationIndex(target, digest); err != nil {
		return err
	}
	return syncWorkspaceApplicationDirectory(root)
}
func copyWorkspaceApplicationRollbackCandidate(root, nonce string) (string, error) {
	input, err := os.Open(filepath.Join(root, "index-before"))
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.CreateTemp(root, "index-rollback-"+nonce+"-")
	if err != nil {
		return "", err
	}
	name := output.Name()
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Sync(), output.Close()); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func releaseWorkspaceApplicationIndex(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	path := j.Git.Index + ".lock"
	actual, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	root := j.Receipt.Transaction
	for _, name := range []string{"index-owned", "index-rollback"} {
		owned, err := os.Lstat(filepath.Join(root, name))
		if err == nil && os.SameFile(actual, owned) {
			return os.Remove(path)
		}
	}
	return errors.New("Git index lock belongs to another writer; retained")
}

func workspaceApplicationGitBinding(j *workspaceApplicationJournal) string {
	state := j.Git
	raw, _ := json.Marshal(struct {
		Identity, Destination, Branch, BeforeHead, BeforeIndex, AfterIndex string
		Entries                                                            []workspaceApplicationEntry
	}{j.Identity, j.Destination, state.Branch, state.BeforeHead, state.BeforeIndexDigest, state.AfterIndexDigest, j.Entries})
	sum := sha256.Sum256(raw)
	return "task: explicitly apply reviewed workspace\n\nApplication-binding: " + hex.EncodeToString(sum[:])
}
func verifyWorkspaceApplicationGitBinding(j *workspaceApplicationJournal) error {
	message, err := git(j.Destination, "show", "-s", "--format=%B", j.Git.AfterHead)
	if err != nil || strings.TrimSpace(message) != workspaceApplicationGitBinding(j) {
		return errors.New("application Git candidate does not bind this reviewed transaction")
	}
	parent, err := git(j.Destination, "show", "-s", "--format=%P", j.Git.AfterHead)
	if err != nil || strings.TrimSpace(parent) != j.Git.BeforeHead {
		return errors.New("application Git candidate parent changed")
	}
	return nil
}

func syncWorkspaceApplicationFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func syncWorkspaceApplicationGitCandidate(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	objectDirectory, err := git(j.Destination, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return err
	}
	objectDirectory = strings.TrimSpace(objectDirectory)
	objects, err := git(j.Destination, "rev-list", "--objects", "--no-object-names", j.Git.AfterHead, "--not", j.Git.BeforeHead)
	if err != nil {
		return err
	}
	packed := false
	for _, id := range strings.Fields(objects) {
		if len(id) < 3 {
			return errors.New("invalid Git candidate object")
		}
		path := filepath.Join(objectDirectory, id[:2], id[2:])
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			packed = true
			continue
		}
		if err := syncWorkspaceApplicationFile(path); err != nil {
			return err
		}
		if err := syncWorkspaceApplicationDirectory(filepath.Dir(path)); err != nil {
			return err
		}
	}
	if packed {
		if err := syncWorkspaceApplicationPacks(objectDirectory); err != nil {
			return err
		}
	}
	return syncWorkspaceApplicationDirectory(objectDirectory)
}
func syncWorkspaceApplicationPacks(objects string) error {
	directory := filepath.Join(objects, "pack")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			if err := syncWorkspaceApplicationFile(filepath.Join(directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	return syncWorkspaceApplicationDirectory(directory)
}
func syncWorkspaceApplicationGitPublished(j *workspaceApplicationJournal) error {
	if j.Git == nil {
		return nil
	}
	if err := syncWorkspaceApplicationFile(j.Git.Index); err != nil {
		return err
	}
	reference, err := git(j.Destination, "rev-parse", "--path-format=absolute", "--git-path", "refs/heads/"+j.Git.Branch)
	if err != nil {
		return err
	}
	reference = strings.TrimSpace(reference)
	if err := syncWorkspaceApplicationReference(j.Destination, reference); err != nil {
		return err
	}
	return syncWorkspaceApplicationDirectory(filepath.Dir(j.Git.Index))
}

func syncWorkspaceApplicationReference(destination, reference string) error {
	err := syncWorkspaceApplicationFile(reference)
	if os.IsNotExist(err) {
		packed, lookupErr := workspaceApplicationGitInput(context.Background(), destination, nil, "", "rev-parse", "--path-format=absolute", "--git-path", "packed-refs")
		if lookupErr != nil {
			return lookupErr
		}
		reference = strings.TrimSpace(packed)
		err = syncWorkspaceApplicationFile(reference)
	}
	if err != nil {
		return err
	}
	return syncWorkspaceApplicationAncestors(filepath.Dir(reference))
}

func workspaceApplicationGitView(ctx context.Context, destination, view string) ([]string, error) {
	gitDir, err := gitContext(ctx, destination, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return []string{"GIT_DIR=" + strings.TrimSpace(gitDir), "GIT_WORK_TREE=" + view}, nil
}
func refuseWorkspaceApplicationSelectedGitChanges(ctx context.Context, j *workspaceApplicationJournal) error {
	var paths, regular, links, linkInputs []string
	for index, entry := range j.Entries {
		if entry.Guard {
			continue
		}
		paths = append(paths, entry.Path)
		if entry.Before == nil || entry.Before.Kind == "dir" {
			continue
		}
		if entry.Before.Kind == "file" {
			regular = append(regular, entry.Path)
			continue
		}
		input := filepath.Join(j.Staging, fmt.Sprintf("link-before-%08d", index))
		if err := os.WriteFile(input, []byte(entry.Before.Link), 0600); err != nil {
			return err
		}
		links = append(links, entry.Path)
		linkInputs = append(linkInputs, input)
	}
	staged, err := workspaceApplicationIndexObjects(ctx, j.Destination, paths)
	if err != nil {
		return err
	}
	objects, err := workspaceApplicationBatchObjects(ctx, j.Destination, j.Destination, regular, false, false)
	if err != nil {
		return err
	}
	linked, err := workspaceApplicationBatchObjects(ctx, j.Destination, j.Destination, linkInputs, false, true)
	if err != nil {
		return err
	}
	for index, path := range links {
		objects[path] = linked[linkInputs[index]]
	}
	for _, path := range paths {
		if objects[path] != "" && (staged[path] == "" || staged[path] != objects[path]) {
			return fmt.Errorf("selected destination path %s contains uncommitted or untracked changes; private copy retained", path)
		}
	}
	return nil
}
func workspaceApplicationIndexObjects(ctx context.Context, destination string, paths []string) (map[string]string, error) {
	result := map[string]string{}
	for start := 0; start < len(paths); start += 64 {
		end := start + 64
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		args := append([]string{"diff", "--no-ext-diff", "--no-textconv", "--cached", "--name-only", "-z", "HEAD", "--"}, batch...)
		changed, err := workspaceApplicationGitInput(ctx, destination, nil, "", args...)
		if err != nil {
			return nil, err
		}
		if changed != "" {
			return nil, fmt.Errorf("selected path %s contains staged changes; private copy retained", gitNULPaths(changed)[0])
		}
		args = append([]string{"ls-files", "--stage", "-z", "--"}, batch...)
		raw, err := workspaceApplicationGitInput(ctx, destination, nil, "", args...)
		if err != nil {
			return nil, err
		}
		if err := parseWorkspaceApplicationIndex(raw, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func parseWorkspaceApplicationIndex(raw string, result map[string]string) error {
	for _, record := range gitNULPaths(raw) {
		header, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return errors.New("selected Git index has unresolved or invalid entries")
		}
		result[path] = fields[1]
	}
	return nil
}
func workspaceApplicationBatchObjects(ctx context.Context, destination, view string, paths []string, write, raw bool) (map[string]string, error) {
	result := map[string]string{}
	if len(paths) == 0 {
		return result, nil
	}
	environment, err := workspaceApplicationGitView(ctx, destination, view)
	if err != nil {
		return nil, err
	}
	if !raw {
		if err := workspaceApplicationCheckAttributes(ctx, view, environment, paths); err != nil {
			return nil, err
		}
	}
	var input strings.Builder
	for _, path := range paths {
		input.WriteString(quoteWorkspaceApplicationGitPath(path))
		input.WriteByte('\n')
	}
	args := []string{"hash-object", "--stdin-paths"}
	if write {
		args = append(args, "-w")
	}
	if raw {
		args = append(args, "--no-filters")
	}
	out, err := workspaceApplicationGitInput(ctx, view, environment, input.String(), args...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) != len(paths) {
		return nil, errors.New("Git returned an incomplete object list")
	}
	for index, path := range paths {
		if !validWorkspaceApplicationObject(ids[index]) {
			return nil, errors.New("Git returned an invalid object identity")
		}
		result[path] = ids[index]
	}
	return result, nil
}
func workspaceApplicationCheckAttributes(ctx context.Context, view string, environment, paths []string) error {
	out, err := workspaceApplicationGitInput(ctx, view, environment, strings.Join(paths, "\x00")+"\x00", "check-attr", "-z", "--stdin", "filter", "working-tree-encoding")
	if err != nil {
		return err
	}
	fields := strings.Split(out, "\x00")
	if len(fields) != len(paths)*6+1 || fields[len(fields)-1] != "" {
		return errors.New("Git returned an incomplete attribute list")
	}
	for index, path := range paths {
		for offset, attr := range []string{"filter", "working-tree-encoding"} {
			start := index*6 + offset*3
			if fields[start] != path || fields[start+1] != attr {
				return errors.New("Git returned mismatched attributes")
			}
			value := fields[start+2]
			if value != "unspecified" && value != "unset" {
				return fmt.Errorf("reviewed path %s uses unsupported Git %s; no changes were applied", path, attr)
			}
		}
	}
	return nil
}
func quoteWorkspaceApplicationGitPath(path string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, value := range []byte(path) {
		if value == '"' || value == '\\' {
			out.WriteByte('\\')
			out.WriteByte(value)
		} else if value < 32 || value >= 127 {
			fmt.Fprintf(&out, "\\%03o", value)
		} else {
			out.WriteByte(value)
		}
	}
	out.WriteByte('"')
	return out.String()
}
func validWorkspaceApplicationObject(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func workspaceApplicationOwner(root string, j *workspaceApplicationJournal) string {
	return root + "\n" + j.Identity + "\n" + j.Destination + "\n" + j.Nonce + "\n"
}
func writeWorkspaceApplicationOwner(root string, j *workspaceApplicationJournal) error {
	path := filepath.Join(j.Staging, "owner")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(workspaceApplicationOwner(root, j))
	return errors.Join(writeErr, f.Sync(), f.Close(), syncWorkspaceApplicationDirectory(j.Staging))
}
func workspaceApplicationOwnerMatches(root string, j *workspaceApplicationJournal) bool {
	return workspaceApplicationOriginalOwnerMatches(root, j) || workspaceApplicationCleanupMatches(root, j)
}

func workspaceApplicationOriginalOwnerMatches(root string, j *workspaceApplicationJournal) bool {
	path := filepath.Join(j.Staging, "owner")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return false
	}
	raw, err := readWorkspaceApplicationBounded(path, 4096)
	return err == nil && string(raw) == workspaceApplicationOwner(root, j)
}

func readWorkspaceApplicationBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("application recovery record exceeds its bound")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if len(raw) > int(limit) {
		return nil, errors.New("application recovery record exceeds its bound")
	}
	return raw, err
}

type workspaceApplicationCommandBuffer struct {
	bytes.Buffer
	limit    int
	truncate bool
}

func (b *workspaceApplicationCommandBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		if b.truncate {
			n := len(p)
			_, _ = b.Buffer.Write(p[:b.limit-b.Len()])
			return n, nil
		}
		return 0, errors.New("Git protocol output exceeded its bound")
	}
	return b.Buffer.Write(p)
}
func workspaceApplicationGitInput(ctx context.Context, dir string, environment []string, input string, args ...string) (string, error) {
	if dir == "" {
		return "", errors.New("Git application command has no working directory")
	}
	//codeaf:plumbing the transaction uses bounded local Git protocol streams.
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	command.Env = append(command.Env, environment...)
	command.Stdin = strings.NewReader(input)
	output := &workspaceApplicationCommandBuffer{limit: taskcopy.WorkspaceCheckpointLimit}
	stderr := &workspaceApplicationCommandBuffer{limit: 64 * 1024, truncate: true}
	command.Stdout, command.Stderr = output, stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("Git application protocol failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return output.String(), nil
}
