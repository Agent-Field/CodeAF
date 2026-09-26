//go:build !windows

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

// snapshotRecorder keeps the workspaceRecorder promises without git. It edits
// the workspace in place, takes no locks on it, writes nothing into it beyond
// what the model writes, and keeps its own copies of the tree outside it.
//
// Identifiers are content addresses: the SHA-256 of a manifest of every path,
// mode and content hash in the tree. Two identical trees therefore have the
// same identifier and two different trees do not, which is the only property
// the run relies on.
type snapshotRecorder struct {
	workspace  string
	note       func(string)
	startRules *ignoreRules

	mu    sync.Mutex
	store string // lazily created; "" until the first snapshot is kept
	// published maps a name to the handle last published under it, so a
	// restore target survives in-process even where nothing writes a ref.
	published map[string]string
}

func newSnapshotRecorder(workspace string, note func(string)) *snapshotRecorder {
	return &snapshotRecorder{
		workspace: workspace, note: note, startRules: startIgnoreRules(workspace), published: map[string]string{},
	}
}

func (recorder *snapshotRecorder) Kind() string         { return "snapshot" }
func (recorder *snapshotRecorder) CommitsOnWrite() bool { return false }

func (recorder *snapshotRecorder) Prepare(ctx context.Context) error {
	if _, err := os.Stat(recorder.workspace); err != nil {
		return fmt.Errorf("workspace is not readable: %w", err)
	}
	// Nothing to arrange. The artifact exclusion the git recorder writes into
	// .git/info/exclude is unnecessary here: the walker skips .senior-dev/ by
	// construction, so the artifacts cannot enter a snapshot in the first
	// place.
	return nil
}

// Base is the tree as the run found it. There is no commit to name, so the
// starting tree names itself, and the run's "unchanged since the start" test
// is an identifier comparison exactly as it is under git.
func (recorder *snapshotRecorder) Base(ctx context.Context) (string, error) {
	return recorder.Snapshot()
}

func (recorder *snapshotRecorder) Snapshot() (string, error) {
	entries, err := recorder.walk()
	if err != nil {
		return "", err
	}
	return manifestID(entries), nil
}

// Record copies the working tree into the store under its own identifier. A
// tree already stored is not copied again: identical identifiers mean
// identical content, so the first copy is as good as a second.
func (recorder *snapshotRecorder) Record(treeID, label string) (string, error) {
	store, err := recorder.ensureStore()
	if err != nil {
		return "", err
	}
	target := filepath.Join(store, treeID)
	if _, err := os.Stat(target); err == nil {
		return treeID, nil
	}
	entries, err := recorder.walk()
	if err != nil {
		return "", err
	}
	if actual := manifestID(entries); actual != treeID {
		return "", fmt.Errorf(
			"tree changed while recording it: %s, want %s",
			shortSHA(actual), shortSHA(treeID),
		)
	}
	// Assembled beside the final name and renamed into place, so a crash
	// mid-copy cannot leave a half-tree that a later Stat would accept.
	staging, err := os.MkdirTemp(store, "staging-*")
	if err != nil {
		return "", fmt.Errorf("create snapshot staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	for _, entry := range entries {
		source := filepath.Join(recorder.workspace, filepath.FromSlash(entry.path))
		destination := filepath.Join(staging, filepath.FromSlash(entry.path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		if err := copyFile(source, destination, entry.mode); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(
		filepath.Join(staging, ".senior-dev-label"), []byte(label+"\n"), 0o644,
	); err != nil {
		return "", err
	}
	if err := os.Rename(staging, target); err != nil {
		// Another Record of the same tree won the race; its copy is identical.
		if _, statErr := os.Stat(target); statErr == nil {
			return treeID, nil
		}
		return "", fmt.Errorf("store snapshot: %w", err)
	}
	return treeID, nil
}

// Publish records the name in memory. There is no repository to hang a ref on,
// so unlike the git recorder this does not survive the process -- which is
// why the interface calls it best-effort and nothing depends on it.
func (recorder *snapshotRecorder) Publish(name, handle string) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.published[name] = handle
	return nil
}

// Restore makes the working tree the recorded one: every path the tree has now
// and the snapshot does not is removed, every path the snapshot has is written,
// and the result is re-identified as proof.
func (recorder *snapshotRecorder) Restore(handle, wantTree string) error {
	recorder.mu.Lock()
	store := recorder.store
	recorder.mu.Unlock()
	if store == "" {
		return fmt.Errorf("no snapshot store: nothing was recorded")
	}
	source := filepath.Join(store, handle)
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("snapshot %s is not in the store: %w", shortSHA(handle), err)
	}
	wanted, err := walkTree(source, false)
	if err != nil {
		return err
	}
	wantedPaths := map[string]struct{}{}
	for _, entry := range wanted {
		wantedPaths[entry.path] = struct{}{}
	}
	current, err := walkWorkspaceAll(recorder.workspace)
	if err != nil {
		return err
	}
	startPaths, err := util.InitialIgnoredPaths()
	if err != nil {
		return err
	}
	// Remove first: a path that is a file in the snapshot and a directory now
	// (or the reverse) cannot be written over in place.
	for _, entry := range current {
		if ignoredAtStart(entry.path, recorder.startRules, startPaths) {
			continue
		}
		if _, keep := wantedPaths[entry.path]; keep {
			continue
		}
		if err := os.Remove(
			filepath.Join(recorder.workspace, filepath.FromSlash(entry.path)),
		); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, entry := range wanted {
		destination := filepath.Join(recorder.workspace, filepath.FromSlash(entry.path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := copyFile(
			filepath.Join(source, filepath.FromSlash(entry.path)), destination, entry.mode,
		); err != nil {
			return err
		}
	}
	recorder.pruneEmptyDirs()
	actual, err := recorder.Snapshot()
	if err != nil {
		return err
	}
	if actual != wantTree {
		return fmt.Errorf(
			"restored tree %s, want %s", shortSHA(actual), shortSHA(wantTree),
		)
	}
	return nil
}

// DifferentPaths compares the same file manifests Restore uses, so a changed
// file or a new file is rescued before the snapshot road replaces either.
func (recorder *snapshotRecorder) DifferentPaths(handle string) ([]string, error) {
	recorder.mu.Lock()
	store := recorder.store
	recorder.mu.Unlock()
	if store == "" {
		return nil, fmt.Errorf("no snapshot store: nothing was recorded")
	}
	wanted, err := walkTree(filepath.Join(store, handle), false)
	if err != nil {
		return nil, err
	}
	current, err := walkWorkspaceAll(recorder.workspace)
	if err != nil {
		return nil, err
	}
	startPaths, err := util.InitialIgnoredPaths()
	if err != nil {
		return nil, err
	}
	before := make(map[string]treeEntry, len(wanted))
	for _, entry := range wanted {
		before[entry.path] = entry
	}
	var paths []string
	for _, entry := range current {
		if ignoredAtStart(entry.path, recorder.startRules, startPaths) {
			continue
		}
		old, found := before[entry.path]
		if !found || old.hash != entry.hash || old.mode != entry.mode {
			paths = append(paths, entry.path)
		}
	}
	currentPaths := make(map[string]bool, len(current))
	for _, entry := range current {
		currentPaths[entry.path] = true
	}
	for _, entry := range wanted {
		if !currentPaths[entry.path] {
			paths = append(paths, entry.path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// BaseTree is the identity function: a snapshot base IS a tree identifier,
// where a git base is a commit that has to be resolved to one.
func (recorder *snapshotRecorder) BaseTree(base string) (string, bool) {
	if base == "" {
		return "", false
	}
	return base, true
}

func (recorder *snapshotRecorder) Change(base string) (soloTreeChange, error) {
	entries, err := recorder.walk()
	if err != nil {
		return soloTreeChange{}, err
	}
	change := soloTreeChange{treeSHA: manifestID(entries)}
	recorder.mu.Lock()
	store := recorder.store
	recorder.mu.Unlock()
	if base == "" || store == "" {
		// Nothing to compare against: any tree at all is a change, and
		// refusing to submit because we cannot name the starting point would
		// be worse than accepting one we cannot size.
		change.changed = true
		return change, nil
	}
	baseDir := filepath.Join(store, base)
	baseEntries, err := walkTree(baseDir, false)
	if err != nil {
		change.changed = true
		return change, nil
	}
	change.files = countChangedPaths(baseEntries, entries)
	change.changed = change.files > 0
	// No patch text: producing one needs a diff algorithm this recorder does
	// not carry. The field is advisory -- the submit gate reads `changed` and
	// `files` -- so it is left empty rather than faked.
	return change, nil
}

func (recorder *snapshotRecorder) ListPaths(
	ctx context.Context, maxBytes int,
) ([]string, int, bool, error) {
	entries, err := recorder.walk()
	if err != nil {
		return nil, 0, false, err
	}
	paths := make([]string, 0, len(entries))
	consumed := 0
	for _, entry := range entries {
		consumed += len(entry.path) + 1
		if consumed > maxBytes {
			return nil, consumed, true, nil
		}
		paths = append(paths, entry.path)
	}
	sort.Strings(paths)
	return paths, consumed, false, nil
}

// Summary reports what it can measure without a diff algorithm: which paths
// differ from the base and how many bytes they hold. `additions` and
// `deletions` are absent rather than guessed -- the event contract marks them
// optional for exactly this reason.
func (recorder *snapshotRecorder) Summary(
	ctx context.Context, base string,
) (map[string]any, string) {
	data := map[string]any{"base_sha": base}
	entries, err := recorder.walk()
	if err != nil {
		data["error"] = err.Error()
		return data, "error"
	}
	recorder.mu.Lock()
	store := recorder.store
	recorder.mu.Unlock()
	if store == "" || base == "" {
		data["files"] = len(entries)
		data["untracked_files"] = 0
		return data, "completed"
	}
	baseEntries, err := walkTree(filepath.Join(store, base), false)
	if err != nil {
		data["error"] = err.Error()
		return data, "error"
	}
	data["files"] = countChangedPaths(baseEntries, entries)
	data["binary_files"] = 0
	data["untracked_files"] = 0
	changedBytes := int64(0)
	baseByPath := map[string]treeEntry{}
	for _, entry := range baseEntries {
		baseByPath[entry.path] = entry
	}
	for _, entry := range entries {
		if previous, ok := baseByPath[entry.path]; !ok || previous.hash != entry.hash {
			changedBytes += entry.size
		}
	}
	data["patch_bytes"] = changedBytes
	return data, "completed"
}

// ── the tree walk ────────────────────────────────────────────────────

type treeEntry struct {
	path string // slash-separated, relative to the tree root
	mode os.FileMode
	size int64
	hash string
}

func (recorder *snapshotRecorder) walk() ([]treeEntry, error) {
	entries, err := walkTree(recorder.workspace, true)
	if err != nil {
		return nil, err
	}
	startPaths, err := util.InitialIgnoredPaths()
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, entry := range entries {
		if !ignoredAtStart(entry.path, recorder.startRules, startPaths) {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

// walkTree lists every regular file in root, sorted, with its content hash.
// honourIgnores is false inside the store, where everything present belongs to
// the snapshot by construction and a stray .gitignore must not remove files
// from a tree that was already decided.
//
// A FOLDER OR FILE THE WORKSPACE WILL NOT LET US READ IS NOT PART OF THE TREE.
// It is skipped on every walk alike, so it is in no snapshot, no change count
// and no restore, and nothing of it is removed or written; one folder the
// system keeps to itself (macOS answers `operation not permitted` for some
// even to their owner) no longer ends the run before its first step. Inside
// the store every file is ours, and an error there is still an error.
func walkTree(root string, honourIgnores bool) ([]treeEntry, error) {
	return walkTreeWithOptions(root, honourIgnores, honourIgnores)
}

// walkWorkspaceAll sees newly ignored files without treating a protected
// folder as a destructive restore failure; the snapshot store stays strict.
func walkWorkspaceAll(root string) ([]treeEntry, error) {
	return walkTreeWithOptions(root, false, true)
}

func walkTreeWithOptions(root string, honourIgnores, maySkipUnreadable bool) ([]treeEntry, error) {
	rules := newIgnoreRules()
	if honourIgnores {
		rules.load(root, "")
	}
	var entries []treeEntry
	err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return skipUnreadable(err, name != root && maySkipUnreadable, info)
		}
		relative, relErr := filepath.Rel(root, name)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		if info.IsDir() {
			// .git is never part of the answer, and .senior-dev is senior-dev's own
			// bookkeeping -- the same exclusion seniorDevArtifactPathspecs makes
			// under git.
			if relative == ".git" || relative == ".senior-dev" ||
				strings.HasSuffix(relative, "/.git") {
				return filepath.SkipDir
			}
			if honourIgnores {
				if rules.ignored(relative, true) {
					return filepath.SkipDir
				}
				rules.load(root, relative)
			}
			return nil
		}
		// Symlinks and devices are not content, and following them would let a
		// link out of the workspace pull in a tree that is not the answer.
		if !info.Mode().IsRegular() {
			return nil
		}
		if relative == ".senior-dev-label" {
			return nil
		}
		if honourIgnores && rules.ignored(relative, false) {
			return nil
		}
		hash, hashErr := hashFile(name)
		if hashErr != nil {
			return skipUnreadable(hashErr, maySkipUnreadable, info)
		}
		entries = append(entries, treeEntry{
			path: relative, mode: info.Mode().Perm(),
			size: info.Size(), hash: hash,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, nil
}

// skipUnreadable is [walkTree]'s answer to an error at one path: skip it when
// it is a refusal to read in a walk that may skip one, and stop otherwise.
func skipUnreadable(err error, mayskip bool, info os.FileInfo) error {
	if !mayskip || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if info != nil && info.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// manifestID is the tree's content address: every path, mode and content hash
// in sorted order, hashed. Mode is included so chmod +x alone is a change.
func manifestID(entries []treeEntry) string {
	digest := sha256.New()
	for _, entry := range entries {
		fmt.Fprintf(digest, "%s\x00%o\x00%s\x00", entry.path, entry.mode, entry.hash)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func countChangedPaths(before, after []treeEntry) int {
	beforeByPath := map[string]string{}
	for _, entry := range before {
		beforeByPath[entry.path] = entry.hash
	}
	afterByPath := map[string]string{}
	for _, entry := range after {
		afterByPath[entry.path] = entry.hash
	}
	changed := 0
	for path, hash := range afterByPath {
		if previous, ok := beforeByPath[path]; !ok || previous != hash {
			changed++
		}
	}
	for path := range beforeByPath {
		if _, ok := afterByPath[path]; !ok {
			changed++
		}
	}
	return changed
}

func hashFile(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pruneEmptyDirs removes directories a restore emptied, so a restored tree has
// no leftover shape from the tree it replaced. Failures are ignored: an empty
// directory is invisible to the manifest and cannot make the proof fail.
func (recorder *snapshotRecorder) pruneEmptyDirs() {
	startPaths, err := util.InitialIgnoredPaths()
	if err != nil {
		return
	}
	var dirs []string
	_ = filepath.Walk(recorder.workspace, func(name string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil //nolint:nilerr // a walk error here is not worth failing a restore
		}
		relative, relErr := filepath.Rel(recorder.workspace, name)
		if relErr != nil || relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if relative == ".git" || relative == ".senior-dev" {
			return filepath.SkipDir
		}
		if util.PathIgnoredAtStart(relative, startPaths) || recorder.startRules.ignored(relative, true) {
			return filepath.SkipDir
		}
		dirs = append(dirs, name)
		return nil
	})
	// Deepest first, so a directory emptied by removing its children is itself
	// removable in the same pass.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
}

func (recorder *snapshotRecorder) ensureStore() (string, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.store != "" {
		return recorder.store, nil
	}
	// Outside the workspace on purpose: a store inside it would be part of the
	// tree it is trying to describe.
	root := strings.TrimSpace(os.Getenv("SENIOR_DEV_SCRATCH_ROOT"))
	if root == "" {
		root = os.TempDir()
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create scratch root: %w", err)
	}
	store, err := os.MkdirTemp(root, "senior-dev-snapshots-*")
	if err != nil {
		return "", fmt.Errorf("create snapshot store: %w", err)
	}
	recorder.store = store
	return store, nil
}
