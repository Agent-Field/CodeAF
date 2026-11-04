// sweep.go reaps idle disposable storage once per launch.
//
//  1. Files in session logs expire after a week; live sessions stay untouched.
//  2. Temp-rooted conversations idle for a week are removed after retiring forks.
//  3. Other conversations and their work stay, except unfinished older-chat copies:
//     a week-untouched .moving destination can go only under its existing exclusive
//     copy lock, without copy-ready, sealed turns or an adopted head, and with its
//     matching original transcript still beside .moving. The folder goes before
//     its lock. Completed copies and all other .moving entries stay untouched.
//  4. Unsaved, unlocked errands untouched for a week are removed. The errands
//     pass cannot reach projects; the projects pass applies rules 1–3 there.
//
// Roots and clocks are injected so these rules can be tested without real state.
package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

const (
	// sweepTTL is how long a dropping lives and how long a temp-rooted session
	// sits idle before it is litter. A week is the span Decision 26 names, and
	// it is one number for both because they are one judgement: nothing here has
	// been wanted for longer than a person's working memory of it.
	sweepTTL = 7 * 24 * time.Hour

	// placesDirName is where the session folders live under the state root:
	// v3/projects/<encoded-workspace>/<session-id>/ — the same word
	// cmd/codeaf's layout spells when it creates a bucket (chatv3_layout.go).
	// The sweep needs only the root and never the encoder.
	placesDirName = "projects"

	// errandKeep is how long an errand's folder sits untouched before it is
	// litter: [sweepTTL], for the same judgement.
	errandKeep = sweepTTL
)

// SweepHome runs one pass over this machine's session folders and one over
// home's errands. It is the launch door's call ([SweepPlaces] and
// [SweepErrands] are the testable ones underneath it).
//
// errandsRoot is passed in rather than resolved here for the reason every other
// root in this file is: a pass that computed its own paths could not be pointed
// at a temp directory and therefore could not be proved. An empty root is a
// build with no errands, and rule 4 does nothing at all. held answers whether
// an errand's folder is still somebody's — an automation was saved from it —
// and may be nil.
func SweepHome(errandsRoot string, held func(dir string) bool, note func(string)) {
	SweepHomeContext(context.Background(), errandsRoot, held, note)
}

// SweepHomeContext is SweepHome with cancellation. Cancellation is checked
// before resolving the home so a stopped launch cannot target a home selected
// after it started.
func SweepHomeContext(ctx context.Context, errandsRoot string, held func(dir string) bool, note func(string)) {
	if contextDone(ctx) {
		return
	}
	now := time.Now()
	SweepPlacesContext(ctx, home.Join("v3", placesDirName), now, note)
	if contextDone(ctx) {
		return
	}
	SweepErrandsContext(ctx, errandsRoot, now, held, note)
}

// SweepPlaces applies the session and unfinished-copy rules over one projects root.
//
// A root that is not there is not a failure — it is a machine that has not held
// a conversation yet — and answers silently.
func SweepPlaces(root string, now time.Time, note func(string)) {
	SweepPlacesContext(context.Background(), root, now, note)
}

// SweepPlacesContext is SweepPlaces with cancellation between entries and
// immediately before each destructive operation.
func SweepPlacesContext(ctx context.Context, root string, now time.Time, note func(string)) {
	if contextDone(ctx) {
		return
	}
	if note == nil {
		note = func(string) {}
	}
	buckets, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		note(fmt.Sprintf("sweep: could not read %s: %v", root, err))
		return
	}
	for _, bucket := range buckets {
		if contextDone(ctx) {
			return
		}
		if !bucket.IsDir() {
			continue
		}
		path := filepath.Join(root, bucket.Name())
		sessions, err := os.ReadDir(path)
		if err != nil {
			note(fmt.Sprintf("sweep: could not read %s: %v", path, err))
			continue
		}
		for _, entry := range sessions {
			if contextDone(ctx) {
				return
			}
			if !entry.IsDir() {
				continue
			}
			if entry.Name() == ".moving" {
				sweepUnfinishedCopies(ctx, path, sessions, now, note)
				continue
			}
			sweepSession(ctx, filepath.Join(path, entry.Name()), now, note)
		}
	}
}

// sweepSession checks live ownership before expiring logs or reaping litter.
func sweepSession(ctx context.Context, dir string, now time.Time, note func(string)) {
	if contextDone(ctx) || sessionIsOpen(dir) {
		// Somebody is talking to it. Not its logs, not its folder, nothing.
		return
	}
	sweepLogs(ctx, dir, now, note)
	meta, err := LoadMeta(dir)
	if err != nil {
		note(fmt.Sprintf("sweep: could not read the identity of %s: %v", dir, err))
		return
	}
	// A SESSION THAT CANNOT SAY WHAT IT IS, STAYS. LoadMeta answers a zero Meta
	// for a file that is missing or will not parse, and a missing identity is
	// exactly the case where deleting would be a guess. The rule reaps what is
	// PROVABLY litter and leaves everything else alone forever.
	if strings.TrimSpace(meta.ID) == "" {
		return
	}
	if !sweepIsLitter(meta, dir, now) {
		retireCheckpointForks(ctx, dir, true, note)
		return
	}
	reapSession(ctx, dir, meta, note)
}

// sessionIsOpen reports whether another codeaf holds this session's transcript.
//
// It asks the way sessionfile.go's own claim asks — a non-blocking exclusive
// flock, dropped the instant it is taken — because a held flock IS a live
// writer: the kernel releases it when the holder dies however it dies, so there
// is no staleness to second-guess and no pid file to go wrong. Taking it here
// costs the live session nothing, since the lock rides our own open file
// description and closing it releases only ours.
//
// EVERY UNCERTAINTY ANSWERS "OPEN". A transcript that cannot be opened, a
// filesystem that does not do locks, a directory this process may not read: each
// of them is a reason to leave the folder alone, because the sweep's whole
// licence is that it only removes what it is sure about.
func sessionIsOpen(dir string) bool {
	path := layoutOf(dir).transcript(dir)
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		// No transcript at all: a folder that never held a conversation, and
		// there is nothing for anybody to be holding.
		return false
	}
	if err != nil {
		return true
	}
	defer file.Close()
	err = filelock.Lock(file, true, true)
	if err != nil {
		// Held by another window, or a filesystem with no locks to offer. Both
		// answer "leave it".
		return true
	}
	_ = filelock.Unlock(file)
	return false
}

// sweepLogs expires the droppings and NOTHING ELSE: it walks logs/ and only
// logs/, removes files past the TTL, and leaves every directory standing. A
// directory removed here would be one a live job's next line could not recreate.
func sweepLogs(ctx context.Context, dir string, now time.Time, note func(string)) {
	dir = jobRetentionAnchor(dir)
	logs := filepath.Join(dir, placeLogs)
	cutoff := now.Add(-sweepTTL)
	err := filepath.WalkDir(logs, func(path string, entry fs.DirEntry, err error) error {
		if contextDone(ctx) {
			return fs.SkipAll
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && path == filepath.Join(logs, droppingJobs) {
			// Job ownership and durable id history must not expire by age.
			// This existing-directory startup pass uses the same leased
			// retention as claim/close and preserves unmarked legacy writers.
			if err := jobRetentionSweepBefore(path, defaultJobRetentionBudget(), cutoff); err != nil {
				note(fmt.Sprintf("sweep: job retention deferred for %s: %v", path, err))
			}
			return fs.SkipDir
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		if contextDone(ctx) {
			return fs.SkipAll
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			note(fmt.Sprintf("sweep: could not expire %s: %v", path, err))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		note(fmt.Sprintf("sweep: could not read %s: %v", logs, err))
	}
}

// sweepIsLitter decides whether a session is the disposable kind, and it takes
// BOTH halves: the place says the conversation was never meant to be kept, and
// the clock says nobody has come back to it.
//
// The idle stamp is [Meta.LastUserAt] — when the PERSON last said something —
// and the folder's own mtime only when there is no such stamp. That order is
// v2's resume law (internal/store/session_rooms.go): a background write touching
// a file is not a person returning to a conversation, and a sweep that read
// mtime first would keep a dead session alive forever because something wrote a
// log line into it.
func sweepIsLitter(meta Meta, dir string, now time.Time) bool {
	if !underTempDir(meta.Workspace) && !underTempDir(meta.LaunchDir) {
		return false
	}
	idle := meta.LastUserAt
	if idle.IsZero() {
		info, err := os.Stat(dir)
		if err != nil {
			return false
		}
		idle = info.ModTime()
	}
	return idle.Before(now.Add(-sweepTTL))
}

// underTempDir reports whether a path sits inside a directory the machine
// itself considers disposable. Both the configured temp directory and /tmp are
// asked, because TMPDIR moves the first without making the second any less of a
// temp directory to the person who typed it.
func underTempDir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	for _, temp := range []string{os.TempDir(), "/tmp"} {
		temp = filepath.Clean(strings.TrimSpace(temp))
		if temp == "" || temp == string(filepath.Separator) {
			continue
		}
		if path == temp || strings.HasPrefix(path, temp+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// reapSession removes one litter session, EVERY REGISTRATION FIRST.
//
// A working copy is two things: a directory, and a record of it held somewhere
// this sweep does not own — a worktree registered in the person's repository, a
// universe furrow is keeping a line about. Removing the directory alone leaves
// that record pointing at a path that is gone, which is the litter this function
// exists to prevent, in somebody else's project instead of ours. So the
// registrations go first and the folder goes last.
//
// A worktree is the first of the two: a directory, and a registration in the
// repository it was cut from. Removing the directory alone leaves the person's repository
// holding a registration for a path that is gone — `git worktree list` names it,
// `git worktree add` refuses to reuse it, and the mess is in THEIR repository
// rather than ours. So every entry under trees/ is unregistered against the
// recorded workspace before the folder goes, and the prune sweeps up whatever
// the removes could not name.
//
// Git unregistration is best-effort: the workspace
// may have moved, been deleted, or stopped being a repository since the session
// last ran, and none of those is a reason to leave the litter standing. When the
// workspace is gone there is nothing holding a registration either, which is why
// the git commands are skipped entirely rather than run into an error.
func reapSession(ctx context.Context, dir string, meta Meta, note func(string)) {
	if contextDone(ctx) || !retireCheckpointForks(ctx, dir, false, note) {
		return
	}
	if root, ok := repositoryRoot(meta.Workspace); ok {
		treesRoot := (Place{Dir: dir}).Trees()
		trees, err := os.ReadDir(treesRoot)
		if err == nil && len(trees) > 0 {
			release := lockGitRoot(Place{Dir: dir}, root)
			for _, tree := range trees {
				if contextDone(ctx) {
					release()
					return
				}
				if !tree.IsDir() {
					continue
				}
				// The registration carries git's resolved spelling, so removal
				// must use the same canonical path creation and checkpoints use.
				path := canonicalPath(filepath.Join(treesRoot, tree.Name()))
				if contextDone(ctx) {
					release()
					return
				}
				_, _ = git(root, "worktree", "remove", "--force", path)
			}
			if contextDone(ctx) {
				release()
				return
			}
			_, _ = git(root, "worktree", "prune")
			release()
		}
	}
	// AND THE FOLDERS THIS CONVERSATION ONLY REFERRED TO. Its own working copy
	// of one of those is a worktree cut from THAT repository rather than from
	// the workspace above (standingtree.go), so the loop above cannot see it and
	// the registration would be left in a repository this sweep never opened —
	// which is the litter this function exists to prevent, in somebody else's
	// project instead of ours. Each root is asked once however many copies hang
	// off it, and the remove is best-effort for this function's stated reason.
	reaped := map[string]bool{}
	for _, tree := range meta.Trees {
		if contextDone(ctx) {
			return
		}
		root := strings.TrimSpace(tree.Root)
		if root == "" || reaped[root] || strings.TrimSpace(tree.Dir) == "" {
			continue
		}
		reaped[root] = true
		release := lockGitRoot(Place{Dir: dir}, root)
		for _, other := range meta.Trees {
			if contextDone(ctx) {
				release()
				return
			}
			if other.Root == root {
				if contextDone(ctx) {
					release()
					return
				}
				_, _ = git(root, "worktree", "remove", "--force", canonicalPath(other.Dir))
			}
		}
		if contextDone(ctx) {
			release()
			return
		}
		_, _ = git(root, "worktree", "prune")
		release()
	}
	if contextDone(ctx) {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		note(fmt.Sprintf("sweep: could not remove %s: %v", dir, err))
	}
}

// ── rule 4: home's own litter ───────────────────────────────────────────────

// SweepErrands applies rule 4 over one errands root: the folders behind errands
// said at home that went nowhere.
//
// EVERY PATH IT TOUCHES IS <root>/<id>/. It cannot reach v3/projects because it
// never reads that directory, and an empty or missing root answers silently: a
// machine where nobody has asked anything at home has nothing here.
func SweepErrands(root string, now time.Time, held func(dir string) bool, note func(string)) {
	SweepErrandsContext(context.Background(), root, now, held, note)
}

// SweepErrandsContext is SweepErrands with cancellation between entries and
// immediately before each removal.
func SweepErrandsContext(ctx context.Context, root string, now time.Time, held func(dir string) bool, note func(string)) {
	if contextDone(ctx) {
		return
	}
	if note == nil {
		note = func(string) {}
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		note(fmt.Sprintf("sweep: could not read %s: %v", root, err))
		return
	}
	for _, entry := range entries {
		if contextDone(ctx) {
			return
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if (held != nil && held(dir)) || !sweepIsStale(dir, now) {
			continue
		}
		if contextDone(ctx) {
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			note(fmt.Sprintf("sweep: could not remove %s: %v", dir, err))
		}
	}
}

func contextDone(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// sweepIsStale is rule 2's two questions asked of an errand's folder: is
// anybody holding it, and has anything in it been touched inside
// [errandKeep].
//
// EVERY UNCERTAINTY ANSWERS "KEEP IT", exactly as [sessionIsOpen] does: a
// folder that cannot be walked, an entry that cannot be stated, a transcript
// under a flock. The newest thing in the tree is what is asked about rather
// than the folder's own mtime, because an errand writes its transcript into a
// directory whose mtime stopped moving the moment the files were created.
func sweepIsStale(dir string, now time.Time) bool {
	if sessionIsOpen(dir) {
		return false
	}
	newest, ok := newestUnder(dir)
	if !ok {
		return false
	}
	return newest.Before(now.Add(-errandKeep))
}

// newestUnder is the most recent modification time anywhere in a tree, and
// false when the tree could not be read whole. A folder with nothing in it
// answers its own time, which is when it was made.
func newestUnder(dir string) (time.Time, bool) {
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}, false
	}
	newest := info.ModTime()
	failed := false
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			failed = true
			return err
		}
		at, err := entry.Info()
		if err != nil {
			failed = true
			return err
		}
		if at.ModTime().After(newest) {
			newest = at.ModTime()
		}
		return nil
	})
	if err != nil || failed {
		return time.Time{}, false
	}
	return newest, true
}

// sweepUnfinishedCopies only reaps scratch destinations whose untouched original
// still exists. CopyLegacy uses the same lock and history test before replacing one.
func sweepUnfinishedCopies(ctx context.Context, bucket string, originals []os.DirEntry, now time.Time, note func(string)) {
	for _, original := range originals {
		if contextDone(ctx) {
			return
		}
		if !original.IsDir() || original.Name() == ".moving" {
			continue
		}
		source := filepath.Join(bucket, original.Name())
		info, err := os.Stat(filepath.Join(source, placeTranscript))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		root := cell.CopyRoot(source)
		info, err = os.Lstat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		sweepUnfinishedCopy(ctx, root, now, note)
	}
}

func sweepUnfinishedCopy(ctx context.Context, root string, now time.Time, note func(string)) {
	lock, err := os.OpenFile(root+".lock", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		return
	}
	if _, err := os.Lstat(filepath.Join(root, cell.CopyReadyPath)); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if cell.HasSealedTurns(root) {
		return
	}
	newest, ok := newestUnder(root)
	if !ok || !newest.Before(now.Add(-sweepTTL)) || contextDone(ctx) {
		return
	}
	if err := os.RemoveAll(root); err != nil {
		note(fmt.Sprintf("sweep: could not remove unfinished copy %s: %v", root, err))
		return
	}
	// Home hides scratch copies by their lock, so the folder must disappear first.
	note(fmt.Sprintf("sweep: removed unfinished copy %s", root))
	if err := os.Remove(root + ".lock"); err != nil {
		note(fmt.Sprintf("sweep: could not remove copy lock %s: %v", root, err))
	}
}
