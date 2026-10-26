package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

type taskWorkspaceApplyReceipt struct {
	Transaction, Outcome, Detail string
	Changes                      []string
}
type workspaceApplicationEntry struct {
	Guard         bool
	Path          string
	Before, After *taskcopy.PortableEntry
}
type workspaceApplicationGit struct {
	Branch, BeforeHead, AfterHead, Index string
	BeforeIndexExists                    bool
	BeforeIndexDigest, AfterIndexDigest  string
}
type workspaceApplicationJournal struct {
	Version                               int
	Identity, Destination, Phase, Staging string
	StagingOwned                          bool
	Nonce                                 string
	Entries                               []workspaceApplicationEntry
	Git                                   *workspaceApplicationGit
	Receipt                               taskWorkspaceApplyReceipt
}

// One transaction owns raw filesystem publication, Git publication, and retained
// preimages. A committed receipt is replayed until its task checkpoint is durable.
func applyReviewedWorkspace(ctx context.Context, original taskTree, reviewedDir string, baseline, reviewed taskcopy.PortableState, identity string, wrote []string) (taskWorkspaceApplyReceipt, error) {
	destination := original.ground
	if original.mode != TaskModeMirror {
		destination = original.root
	}
	if err := workspaceApplicationInputs(destination, identity, baseline, reviewed); err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	root, err := workspaceApplicationDirectory(identity, destination)
	if err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	unlock, err := lockWorkspaceApplication(root, destination)
	defer unlock()
	if err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	expected, err := workspaceApplicationTransactionDelta(baseline, reviewed, wrote, original.mode != TaskModeMirror)
	if err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	expectedOutcome := mergeInPlace
	if original.mode != TaskModeMirror {
		expectedOutcome = mergeMerged
	}
	if receipt, found, err := recoverWorkspaceApplication(root, identity, destination, expected, expectedOutcome, original.mode != TaskModeMirror); found || err != nil {
		return receipt, err
	}
	if original.mode != TaskModeMirror && original.landsInThePersonsRepository() {
		if reason := original.keptLandingSentence(); reason != "" {
			return taskWorkspaceApplyReceipt{}, errors.New(reason)
		}
	}
	return startWorkspaceApplication(ctx, root, original, reviewedDir, baseline, reviewed, identity, expected)
}

func startWorkspaceApplication(ctx context.Context, root string, original taskTree, reviewedDir string, baseline, reviewed taskcopy.PortableState, identity string, expected []workspaceApplicationEntry) (taskWorkspaceApplyReceipt, error) {
	destination := original.root
	if original.mode == TaskModeMirror {
		destination = original.ground
	}
	entries, err := planWorkspaceApplication(destination, baseline, expected)
	if err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	if len(entries) == 0 {
		return taskWorkspaceApplyReceipt{}, errors.New("this root has no portable changes to apply")
	}
	paths := workspaceApplicationPaths(entries)
	if err := taskcopy.CheckPortableRootInheritance(ctx, destination); err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	if err := taskcopy.CheckPortableMutationMetadataContext(ctx, destination, paths); err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	if err := taskcopy.VerifyPortableState(reviewedDir, reviewed); err != nil {
		return taskWorkspaceApplyReceipt{}, err
	}
	j := workspaceApplicationJournal{Version: 1, Identity: identity, Destination: destination, Phase: "preparing", Entries: entries, Nonce: rand.Text()}
	j.Staging = workspaceApplicationStaging(root, destination)
	j.Receipt = taskWorkspaceApplyReceipt{Transaction: root, Outcome: mergeInPlace, Changes: workspaceApplicationChangedPaths(entries)}
	if original.mode != TaskModeMirror {
		j.Receipt.Outcome = mergeMerged
	}
	if err := stageWorkspaceApplication(ctx, root, &j, reviewedDir, original); err != nil {
		cleanupErr := discardWorkspaceApplication(root, &j)
		return taskWorkspaceApplyReceipt{}, errors.Join(err, cleanupErr)
	}
	if err := taskcopy.VerifyPortableState(reviewedDir, reviewed); err != nil {
		return taskWorkspaceApplyReceipt{}, errors.Join(err, discardWorkspaceApplication(root, &j))
	}
	if err := runWorkspaceApplication(ctx, root, &j); err != nil {
		rollbackErr := rollbackWorkspaceApplication(root, &j)
		if rollbackErr != nil {
			return taskWorkspaceApplyReceipt{}, fmt.Errorf("apply failed: %v; recovery retained at %s: %w", err, root, rollbackErr)
		}
		return taskWorkspaceApplyReceipt{}, err
	}
	return j.Receipt, nil
}

func workspaceApplicationInputs(destination, identity string, baseline, reviewed taskcopy.PortableState) error {
	if !filepath.IsAbs(destination) || identity == "" {
		return errors.New("reviewed application has no absolute destination or identity")
	}
	if err := workspacePathWithoutSymlinks(destination); err != nil {
		return err
	}
	if _, err := baseline.Digest(); err != nil {
		return fmt.Errorf("creation metadata baseline unavailable: %w", err)
	}
	_, err := reviewed.Digest()
	return err
}
func workspaceApplicationDirectory(identity, destination string) (string, error) {
	sum := sha256.Sum256([]byte(identity + "\x00" + destination))
	base := home.Join("workspace-applications")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	if err := workspacePathWithoutSymlinks(base); err != nil {
		return "", err
	}
	root := filepath.Join(canonicalPath(base), hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	if err := workspacePathWithoutSymlinks(root); err != nil {
		return "", err
	}
	return root, syncWorkspaceApplicationAncestors(root)
}

// The closer is always callable, including when acquisition fails, so callers
// can defer it before examining the error.
func lockWorkspaceApplication(root, destination string) (func(), error) {
	sum := sha256.Sum256([]byte(destination))
	path := filepath.Join(filepath.Dir(root), hex.EncodeToString(sum[:])+".lock")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return func() {}, errors.New("application lock is not a regular file")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return func() {}, err
	}
	if err := filelock.Lock(f, true, true); err != nil {
		f.Close()
		return func() {}, fmt.Errorf("another workspace application holds this destination: %w", err)
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}
func workspaceApplicationStaging(root, destination string) string {
	return filepath.Join(filepath.Dir(destination), ".codeaf-applying-"+filepath.Base(root))
}
func workspaceApplicationPaths(entries []workspaceApplicationEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Path)
	}
	return out
}

func workspaceApplicationDelta(baseline, reviewed taskcopy.PortableState, wrote []string) ([]workspaceApplicationEntry, error) {
	return workspaceApplicationTransactionDelta(baseline, reviewed, wrote, false)
}
func workspaceApplicationTransactionDelta(baseline, reviewed taskcopy.PortableState, wrote []string, gitMode bool) ([]workspaceApplicationEntry, error) {
	selected := map[string]bool{}
	for _, path := range wrote {
		if err := validWorkspaceApplicationPath(path); err != nil {
			return nil, err
		}
		selected[path] = true
	}
	for path, after := range reviewed.Entries {
		if before, ok := baseline.Entries[path]; !ok || before != after {
			selected[path] = true
		}
	}
	var entries []workspaceApplicationEntry
	for path := range selected {
		if err := validWorkspaceApplicationPath(path); err != nil {
			return nil, err
		}
		entry, changed, err := workspaceApplicationDeltaEntry(path, baseline, reviewed)
		if err != nil {
			return nil, err
		}
		if changed {
			entries = append(entries, entry)
		}
	}
	entries = workspaceApplicationAncestorGuards(entries, baseline)
	if gitMode {
		entries = workspaceApplicationAttributeGuards(entries, baseline)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
func workspaceApplicationDeltaEntry(path string, baseline, reviewed taskcopy.PortableState) (workspaceApplicationEntry, bool, error) {
	before, beforeOK := baseline.Entries[path]
	after, afterOK := reviewed.Entries[path]
	if beforeOK == afterOK && before == after {
		return workspaceApplicationEntry{}, false, nil
	}
	if beforeOK && afterOK && before.Kind != after.Kind && (before.Kind == "dir" || after.Kind == "dir") {
		return workspaceApplicationEntry{}, false, fmt.Errorf("directory type replacement requires explicit recovery and is not supported at %s", path)
	}
	entry := workspaceApplicationEntry{Path: path}
	if beforeOK {
		v := before
		entry.Before = &v
	}
	if afterOK {
		v := after
		entry.After = &v
	}
	return entry, true, nil
}

func workspaceApplicationAncestorGuards(entries []workspaceApplicationEntry, baseline taskcopy.PortableState) []workspaceApplicationEntry {
	present := map[string]bool{}
	for _, entry := range entries {
		present[entry.Path] = true
	}
	originals := append([]workspaceApplicationEntry(nil), entries...)
	for _, entry := range originals {
		for parent := filepath.ToSlash(filepath.Dir(entry.Path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if present[parent] {
				continue
			}
			value, ok := baseline.Entries[parent]
			if !ok {
				continue
			}
			before, after := value, value
			entries = append(entries, workspaceApplicationEntry{Path: parent, Before: &before, After: &after, Guard: true})
			present[parent] = true
		}
	}
	return entries
}
func workspaceApplicationAttributeGuards(entries []workspaceApplicationEntry, baseline taskcopy.PortableState) []workspaceApplicationEntry {
	present := map[string]bool{}
	for _, entry := range entries {
		present[entry.Path] = true
	}
	originals := append([]workspaceApplicationEntry(nil), entries...)
	for _, entry := range originals {
		if entry.Guard {
			continue
		}
		for directory := filepath.ToSlash(filepath.Dir(entry.Path)); ; directory = filepath.ToSlash(filepath.Dir(directory)) {
			path := filepath.ToSlash(filepath.Join(directory, ".gitattributes"))
			if !present[path] {
				guard := workspaceApplicationEntry{Path: path, Guard: true}
				if value, ok := baseline.Entries[path]; ok {
					before, after := value, value
					guard.Before = &before
					guard.After = &after
				}
				entries = append(entries, guard)
				present[path] = true
			}
			if directory == "." {
				break
			}
		}
	}
	return entries
}
func workspaceApplicationChangedPaths(entries []workspaceApplicationEntry) []string {
	var out []string
	for _, entry := range entries {
		if !entry.Guard {
			out = append(out, entry.Path)
		}
	}
	return out
}
func planWorkspaceApplication(destination string, baseline taskcopy.PortableState, entries []workspaceApplicationEntry) ([]workspaceApplicationEntry, error) {
	current, err := taskcopy.InspectPortablePaths(destination, workspaceApplicationPaths(entries))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := workspaceApplicationCompareBaseline(entry.Path, baseline, current); err != nil {
			return nil, err
		}
	}
	return entries, workspaceApplicationDirectoryCoverage(destination, entries)
}
func validWorkspaceApplicationPath(path string) error {
	if path == "" || path == "." || !filepath.IsLocal(filepath.FromSlash(path)) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path || harnessWrote(path) || path == ".git" || strings.HasPrefix(path, ".git/") {
		return fmt.Errorf("invalid application path %q", path)
	}
	return nil
}
func workspaceApplicationCompareBaseline(path string, baseline, current taskcopy.PortableState) error {
	for probe := path; probe != "."; probe = filepath.ToSlash(filepath.Dir(probe)) {
		expected, had := baseline.Entries[probe]
		actual, has := current.Entries[probe]
		if had != has || expected != actual {
			return fmt.Errorf("destination metadata or contents changed at %s; reviewed work remains private", probe)
		}
	}
	return nil
}
func workspaceApplicationDirectoryCoverage(destination string, entries []workspaceApplicationEntry) error {
	removed := map[string]bool{}
	for _, entry := range entries {
		if entry.After == nil {
			removed[entry.Path] = true
		}
	}
	for _, entry := range entries {
		if entry.Before == nil || entry.Before.Kind != "dir" || entry.After != nil {
			continue
		}
		root := filepath.Join(destination, filepath.FromSlash(entry.Path))
		if err := filepath.WalkDir(root, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(destination, path)
			if err != nil {
				return err
			}
			if !removed[filepath.ToSlash(relative)] {
				return fmt.Errorf("directory %s has retained or unselected contents", entry.Path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func stageWorkspaceApplication(ctx context.Context, root string, j *workspaceApplicationJournal, reviewedDir string, original taskTree) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := persistWorkspaceApplication(root, j); err != nil {
		return err
	}
	if err := createWorkspaceApplicationStaging(root, j); err != nil {
		return err
	}
	if err := persistWorkspaceApplication(root, j); err != nil {
		return err
	}
	for _, directory := range []string{"old", "new", "held"} {
		if err := os.Mkdir(filepath.Join(j.Staging, directory), 0700); err != nil {
			return err
		}
	}
	if err := stageWorkspaceApplicationEntries(ctx, j, reviewedDir); err != nil {
		return err
	}
	if original.mode != TaskModeMirror {
		if err := prepareWorkspaceApplicationGit(ctx, root, j, original, reviewedDir); err != nil {
			return err
		}
	}
	j.Phase = "prepared"
	if err := syncWorkspaceApplicationTree(j.Staging); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationDirectory(filepath.Dir(j.Staging)); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationGitCandidate(j); err != nil {
		return err
	}
	return persistWorkspaceApplication(root, j)
}
func createWorkspaceApplicationStaging(root string, j *workspaceApplicationJournal) error {
	if err := os.Mkdir(j.Staging, 0700); err != nil {
		return fmt.Errorf("prepare destination-local transaction: %w", err)
	}
	if err := os.Chown(j.Staging, -1, os.Getgid()); err != nil {
		return err
	}
	if err := os.Chmod(j.Staging, 0700); err != nil {
		return err
	}
	if err := writeWorkspaceApplicationOwner(root, j); err != nil {
		return err
	}
	j.StagingOwned = true
	if err := checkWorkspaceApplicationDevice(j); err != nil {
		return err
	}
	if err := probeWorkspaceApplicationRename(j.Staging); err != nil {
		return err
	}
	return nil
}

func checkWorkspaceApplicationDevice(j *workspaceApplicationJournal) error {
	device, err := workspaceApplicationDevice(j.Staging)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	paths := append([]string{j.Destination}, workspaceApplicationPaths(j.Entries)...)
	for index, relative := range paths {
		path := j.Destination
		if index != 0 {
			path = filepath.Join(j.Destination, filepath.FromSlash(relative))
		}
		for {
			if seen[path] {
				break
			}
			seen[path] = true
			current, err := workspaceApplicationDevice(path)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil && current != device {
				return fmt.Errorf("destination path %s is on a different filesystem from the prepared transaction; private copy retained", path)
			}
			if path == j.Destination {
				break
			}
			path = filepath.Dir(path)
		}
	}
	return nil
}

func probeWorkspaceApplicationRename(staging string) error {
	source, target := filepath.Join(staging, "rename-probe-source"), filepath.Join(staging, "rename-probe-target")
	defer os.Remove(source)
	defer os.Remove(target)
	if err := os.WriteFile(source, []byte("first"), 0600); err != nil {
		return err
	}
	if err := workspaceApplicationRenameExclusive(source, staging, "rename-probe-target"); err != nil {
		return fmt.Errorf("filesystem cannot publish an exclusive atomic rename; private copy retained: %w", err)
	}
	if err := os.WriteFile(source, []byte("second"), 0600); err != nil {
		return err
	}
	if err := workspaceApplicationRenameExclusive(source, staging, "rename-probe-target"); !os.IsExist(err) {
		return fmt.Errorf("filesystem cannot refuse an existing publication target; private copy retained: %v", err)
	}
	raw, err := readWorkspaceApplicationBounded(target, 16)
	if err != nil {
		return err
	}
	if string(raw) != "first" {
		return errors.New("filesystem replaced an exclusive publication target; private copy retained")
	}
	return nil
}

func stageWorkspaceApplicationEntries(ctx context.Context, j *workspaceApplicationJournal, reviewedDir string) error {
	for index, entry := range j.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := stageWorkspaceApplicationEntry(j.Staging, index, entry, j.Destination, reviewedDir); err != nil {
			return err
		}
	}
	return nil
}

func stageWorkspaceApplicationEntry(staging string, index int, entry workspaceApplicationEntry, destination, reviewedDir string) error {
	if entry.Guard {
		return nil
	}
	if entry.Before != nil && entry.Before.Kind != "dir" {
		if err := copyWorkspaceApplicationEntry(filepath.Join(destination, filepath.FromSlash(entry.Path)), workspaceApplicationPayload(staging, "old", index), *entry.Before); err != nil {
			return err
		}
	}
	if entry.After != nil && entry.After.Kind != "dir" {
		return copyWorkspaceApplicationEntry(filepath.Join(reviewedDir, filepath.FromSlash(entry.Path)), workspaceApplicationPayload(staging, "new", index), *entry.After)
	}
	return nil
}
func workspaceApplicationPayload(staging, kind string, index int) string {
	return filepath.Join(staging, kind, fmt.Sprintf("%08d", index))
}
func copyWorkspaceApplicationEntry(source, target string, entry taskcopy.PortableEntry) error {
	if entry.Kind == "symlink" {
		if err := os.Symlink(entry.Link, target); err != nil {
			return err
		}
		return workspaceApplicationSetSymlinkMode(filepath.Dir(target), filepath.Base(target), entry.Mode)
	}
	if entry.Kind != "file" {
		return errors.New("unsupported application payload type")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	modeErr := output.Chmod(entry.Mode.Perm())
	syncErr := output.Sync()
	closeErr := output.Close()
	if err := errors.Join(copyErr, modeErr, syncErr, closeErr); err != nil {
		return err
	}
	return verifyWorkspaceApplicationEntry(target, &entry)
}

func runWorkspaceApplication(ctx context.Context, root string, j *workspaceApplicationJournal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := claimWorkspaceApplicationIndex(root, j); err != nil {
		return err
	}
	if err := verifyWorkspaceApplicationBefore(j); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	j.Phase = "applying"
	if err := persistWorkspaceApplication(root, j); err != nil {
		return err
	}
	if err := publishWorkspaceApplicationFiles(ctx, j); err != nil {
		return err
	}
	if err := applyWorkspaceApplicationDirectoryModes(ctx, j, false); err != nil {
		return err
	}
	if err := verifyWorkspaceApplicationHeld(j); err != nil {
		return err
	}
	if err := verifyWorkspaceApplicationAfter(j); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishWorkspaceApplicationGit(j); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationDestination(j, false); err != nil {
		return err
	}
	if err := syncWorkspaceApplicationGitPublished(j); err != nil {
		return err
	}
	return commitWorkspaceApplication(ctx, root, j)
}
func commitWorkspaceApplication(ctx context.Context, root string, j *workspaceApplicationJournal) error {
	if err := verifyWorkspaceApplicationCommitted(j); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	committed := *j
	committed.Phase = "committed"
	if err := persistWorkspaceApplication(root, &committed); err != nil {
		return err
	}
	*j = committed
	return nil
}
func publishWorkspaceApplicationFiles(ctx context.Context, j *workspaceApplicationJournal) error {
	for _, index := range workspaceApplicationRemovalOrder(j.Entries) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := removeWorkspaceApplicationTarget(j, index); err != nil {
			return err
		}
	}
	for _, index := range workspaceApplicationCreationOrder(j.Entries) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := publishWorkspaceApplicationTarget(j, index); err != nil {
			return err
		}
	}
	return nil
}

func verifyWorkspaceApplicationBefore(j *workspaceApplicationJournal) error {
	for _, entry := range j.Entries {
		if err := verifyWorkspaceApplicationEntry(filepath.Join(j.Destination, filepath.FromSlash(entry.Path)), entry.Before); err != nil {
			return err
		}
	}
	return verifyWorkspaceApplicationGitBefore(j)
}
func verifyWorkspaceApplicationAfter(j *workspaceApplicationJournal) error {
	for _, entry := range j.Entries {
		if err := verifyWorkspaceApplicationEntry(filepath.Join(j.Destination, filepath.FromSlash(entry.Path)), entry.After); err != nil {
			return err
		}
	}
	return errors.Join(taskcopy.CheckPortableMutationMetadata(j.Destination, workspaceApplicationPaths(j.Entries)), taskcopy.CheckPortableRootInheritance(context.Background(), j.Destination))
}
func workspaceApplicationRemovalOrder(entries []workspaceApplicationEntry) []int {
	var indices []int
	for index, entry := range entries {
		if !entry.Guard && entry.Before != nil && (entry.Before.Kind != "dir" || entry.After == nil || entry.After.Kind != "dir") {
			indices = append(indices, index)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return len(entries[indices[i]].Path) > len(entries[indices[j]].Path) })
	return indices
}
func workspaceApplicationCreationOrder(entries []workspaceApplicationEntry) []int {
	var indices []int
	for index, entry := range entries {
		if !entry.Guard && entry.After != nil && (entry.After.Kind != "dir" || entry.Before == nil || entry.Before.Kind != "dir") {
			indices = append(indices, index)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return len(entries[indices[i]].Path) < len(entries[indices[j]].Path) })
	return indices
}
func removeWorkspaceApplicationTarget(j *workspaceApplicationJournal, index int) error {
	entry := j.Entries[index]
	target := filepath.Join(j.Destination, filepath.FromSlash(entry.Path))
	if err := verifyWorkspaceApplicationEntry(target, entry.Before); err != nil {
		return err
	}
	if entry.Before.Kind == "dir" {
		return workspaceApplicationRemove(j.Destination, entry.Path, true)
	}
	held := workspaceApplicationPayload(j.Staging, "held", index)
	if err := workspaceApplicationMoveOut(j.Destination, entry.Path, j.Staging, filepath.ToSlash(filepath.Join("held", fmt.Sprintf("%08d", index)))); err != nil {
		return err
	}
	return verifyWorkspaceApplicationEntry(held, entry.Before)
}
func publishWorkspaceApplicationTarget(j *workspaceApplicationJournal, index int) error {
	entry := j.Entries[index]
	target := filepath.Join(j.Destination, filepath.FromSlash(entry.Path))
	if err := workspacePathWithoutSymlinks(filepath.Dir(target)); err != nil {
		return err
	}
	switch entry.After.Kind {
	case "dir":
		return workspaceApplicationCreateDirectory(j.Destination, entry.Path, 0700)
	case "symlink", "file":
		staged := workspaceApplicationPayload(j.Staging, "new", index)
		return workspaceApplicationRenameExclusive(staged, j.Destination, entry.Path)
	default:
		return errors.New("unsupported application entry")
	}
}
func workspaceApplicationEntryState(entry workspaceApplicationEntry, before bool) *taskcopy.PortableEntry {
	if before {
		return entry.Before
	}
	return entry.After
}

func applyWorkspaceApplicationDirectoryModes(ctx context.Context, j *workspaceApplicationJournal, before bool) error {
	entries := append([]workspaceApplicationEntry(nil), j.Entries...)
	sort.Slice(entries, func(i, k int) bool { return len(entries[i].Path) > len(entries[k].Path) })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Guard {
			continue
		}
		value := entry.After
		if before {
			value = entry.Before
		}
		if value == nil || value.Kind != "dir" {
			continue
		}
		actual, err := workspaceApplicationReadEntry(filepath.Join(j.Destination, filepath.FromSlash(entry.Path)))
		if err != nil {
			return err
		}
		ownNew := workspaceApplicationOwnDirectory(actual, entry)
		if !before && !ownNew && !reflect.DeepEqual(actual, entry.Before) && !reflect.DeepEqual(actual, entry.After) {
			return fmt.Errorf("directory metadata changed during application: %s", entry.Path)
		}
		if err := workspaceApplicationChmod(j.Destination, entry.Path, value.Mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

func workspaceApplicationReadEntry(path string) (*taskcopy.PortableEntry, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry := &taskcopy.PortableEntry{Mode: info.Mode().Perm()}
	switch {
	case info.IsDir():
		entry.Kind = "dir"
	case info.Mode()&os.ModeSymlink != 0:
		entry.Kind = "symlink"
		entry.Link, err = os.Readlink(path)
	case info.Mode().IsRegular():
		entry.Kind = "file"
		entry.Size = info.Size()
		entry.Digest, err = workspaceApplicationFileDigest(path)
	default:
		err = errors.New("unsupported filesystem entry during application")
	}
	return entry, err
}
func workspaceApplicationFileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func verifyWorkspaceApplicationEntry(path string, want *taskcopy.PortableEntry) error {
	actual, err := workspaceApplicationReadEntry(path)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, want) {
		return fmt.Errorf("application path changed: %s", path)
	}
	return nil
}
