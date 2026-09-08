package tui3

// WHERE THE FOLDERS COME FROM, AND WHAT IT COSTS TO ASK.
//
// foldersearch.go ranks what is already known; this file is the other half of
// the same question — HOW THE THINGS TO RANK ARE FOUND, and how a directory is
// read without a keystroke waiting for a disk.
//
// Three laws run through all of it, and they are the three the folder picker
// has been getting wrong:
//
//   - A DIRECTORY THAT CANNOT BE READ IS NOT AN EMPTY DIRECTORY. `os.ReadDir`
//     answers a permission with `nil, err`, and a caller that drops the error
//     tells somebody their work is gone. [folderReadDir] hands the error back
//     and [folderReadWord] is the sentence to draw instead of the rows.
//   - NOTHING ON A KEYSTROKE TOUCHES A DISK. Every read here is a [tea.Cmd],
//     stamped with the generation that asked for it, and an answer whose
//     generation is behind the surface's is an answer to a question nobody is
//     asking any more ([folderStale]).
//   - DISCOVERY IS BOUNDED BEFORE IT IS STARTED. A walk takes a depth, a cap
//     and a wall-clock budget, it says whether it stopped on one of them, and
//     it says how many directories it was refused. A background walk that
//     quietly gave up is indistinguishable from a machine with nothing on it.
//
// AND NOTHING HERE OPENS A FILE. This is a folder chooser: the names of the
// directories and one stat for a `.git` is the whole of what it may know. A
// content index would be a different feature with a different cost, and the
// design's ruling on it is that it is not this one.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── one directory, read honestly ────────────────────────────────────────────

// folderRead is one directory's subdirectories AND why there are none.
type folderRead struct {
	// Dir is the directory that was read, absolute and cleaned.
	Dir string
	// Names are its subdirectories, by name, in readdir order, with the dot
	// directories and [skipDirs] pruned.
	Names []string
	// Err is why the read did not happen. A directory that is genuinely empty
	// has no Err and no Names, and the two cases are told apart by exactly
	// this field.
	Err error
}

// folderReadDir lists one directory's subdirectories — ONE readdir and nothing
// deep, pruned by the very [skipDirs] law the `@` walk keeps, so the two
// surfaces cannot disagree about what a person is allowed to see.
//
// IT KEEPS THE ERROR. That is the whole difference between this and the thing
// it replaces: a permission, a directory that has been deleted under the
// cursor, and a path that turned out to be a file are three different answers,
// and every one of them used to draw as `nothing below here`.
func folderReadDir(dir string) folderRead {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return folderRead{Dir: dir, Err: err}
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || skipDirs[name] || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	return folderRead{Dir: dir, Names: out}
}

// folderReadHidden is [folderReadDir] with the dot-directories kept — the
// answer for somebody who has asked to see them. [skipDirs] still holds,
// because `.git` and `node_modules` are not folders anybody browses to.
func folderReadHidden(dir string) folderRead {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return folderRead{Dir: dir, Err: err}
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || skipDirs[name] {
			continue
		}
		out = append(out, name)
	}
	return folderRead{Dir: dir, Names: out}
}

// The four things a directory can say instead of its rows. Each is a fact the
// read established, which is why saying them is not a breach of the emptiness
// law — the law is about facts nobody established, and "you may not read this"
// is one somebody just did.
const (
	folderDeniedWord  = "this folder cannot be read · permission denied"
	folderMissingWord = "this folder is no longer here"
	folderNotDirWord  = "this is a file, not a folder"
	folderUnreadWord  = "this folder cannot be read"
)

// folderReadWord is the one line a column draws in place of the rows it could
// not get, and NOTHING for a read that worked — including a read that honestly
// found an empty directory, which has its own word already
// ([folderLeafWord]).
func folderReadWord(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, fs.ErrPermission):
		return folderDeniedWord
	case errors.Is(err, fs.ErrNotExist):
		return folderMissingWord
	case errors.Is(err, syscall.ENOTDIR):
		return folderNotDirWord
	}
	return folderUnreadWord
}

// ── reading off the loop ────────────────────────────────────────────────────

// folderReadMsg is one directory listing, coming BACK, stamped with the
// generation that asked for it.
//
// THE GENERATION IS WHY THIS IS NOT A PATH COMPARISON. Somebody walking `→ →
// ←` fast is standing in a directory they have already been in, so an answer
// about the right path can still be an answer to the wrong moment — the second
// visit's rows arriving over the third's. A counter that only ever goes up
// cannot be fooled that way.
type folderReadMsg struct {
	read folderRead
	gen  uint64
}

// folderReadCmd reads one directory OFF THE LOOP.
func folderReadCmd(dir string, gen uint64) tea.Cmd {
	return func() tea.Msg { return folderReadMsg{read: folderReadDir(dir), gen: gen} }
}

// folderStale reports whether an answer arrived after the surface moved on. A
// stale answer is DROPPED and never drawn: it is not wrong, it is about a
// moment that has passed.
func folderStale(msg folderReadMsg, gen uint64) bool { return msg.gen != gen }

// folderReadKeep is how many listings are held back from the disk. Walking down
// a tree and back up is the gesture this exists for — `←` to a level somebody
// left two keystrokes ago must not pay for a second readdir — and a handful of
// levels is the whole of that walk. It is small on purpose: this is a cache of
// what is on a disk right now, and a large one is a cache that is wrong.
const folderReadKeep = 32

// folderReadCache holds the last few listings. The zero value is an empty
// cache and is ready to use.
type folderReadCache struct {
	by map[string]folderRead
	// order is the keys in the order they were put in, so the oldest goes
	// first when the cache is full — the arrival order is the walk's order and
	// is a good enough guess at what will not be wanted again.
	order []string
}

// get answers a held listing, and false when there is none.
func (c *folderReadCache) get(dir string) (folderRead, bool) {
	read, ok := c.by[dir]
	return read, ok
}

// put holds one listing, including one that FAILED — a directory somebody may
// not read is still an answer, and asking the disk again on every keystroke
// would be paying repeatedly for the same refusal.
func (c *folderReadCache) put(read folderRead) {
	if c.by == nil {
		c.by = map[string]folderRead{}
	}
	if _, held := c.by[read.Dir]; !held {
		c.order = append(c.order, read.Dir)
	}
	c.by[read.Dir] = read
	for len(c.order) > folderReadKeep {
		delete(c.by, c.order[0])
		c.order = c.order[1:]
	}
}

// drop forgets one directory, which is what a person asking for a level again
// on purpose means.
func (c *folderReadCache) drop(dir string) {
	if _, held := c.by[dir]; !held {
		return
	}
	delete(c.by, dir)
	for at, name := range c.order {
		if name == dir {
			c.order = append(c.order[:at], c.order[at+1:]...)
			break
		}
	}
}

// ── the background walk ─────────────────────────────────────────────────────

// folderIndexRoot is one directory the walk found.
type folderIndexRoot struct {
	// Path is absolute and cleaned.
	Path string
	// Repo says a `.git` sits directly in it.
	Repo bool
	// At is the directory's own modification time, which is what puts the
	// place somebody touched this morning above the one they cloned in March.
	At time.Time
}

// folderIndexAnswer is what one walk found AND what it could not see.
type folderIndexAnswer struct {
	Roots []folderIndexRoot
	// Denied is how many directories the walk was refused. It is counted
	// rather than dropped because a home directory with a hundred unreadable
	// folders in it is a machine where this list is quietly incomplete, and
	// nobody can be told that by a list of what was found.
	Denied int
	// Bound says the walk stopped on one of its own bounds — the cap or the
	// budget — rather than reaching the end of the tree. An index that is a
	// prefix of the truth is a different thing from an index that is the
	// truth, and only this field can say which one is in hand.
	Bound bool
	// Took is the wall clock the walk spent.
	Took time.Duration
}

// folderIndexOpts bound one walk BEFORE IT STARTS. Every field is a stop, and
// there is no way to ask for a walk without them: [folderIndexDefaults] is how
// a caller says "the usual ones".
type folderIndexOpts struct {
	// Base is the directory to walk, and is where a person's folders are.
	Base string
	// Depth is how far below Base a repository may be found.
	Depth int
	// Plain is how far below Base a directory that is NOT a repository may be
	// offered, and is shallower than Depth on purpose — see
	// [folderIndexPlainDepth].
	Plain int
	// Cap is the most roots a walk will collect.
	Cap int
	// Budget is the wall clock the walk may spend. It is the bound that
	// matters on a machine with a slow or a network disk, where a depth and a
	// cap are both reached far too late.
	Budget time.Duration
	// Skip are directory names never entered, over and above the dot ones.
	Skip map[string]bool
}

// The bounds a walk takes when nobody names its own.
//
// SIX LEVELS reaches `~/code/work/client/repo` without reaching a
// `node_modules` nobody asked about. TWO THOUSAND roots is more directories
// than any list can rank usefully. THREE SECONDS is longer than a background
// walk of a normal home directory takes and short enough that a pathological
// one — a network mount, a fuse filesystem — gives up while the answer could
// still matter.
const (
	folderIndexDepth      = 6
	folderIndexPlainDepth = 3
	folderIndexCap        = 2000
	folderIndexBudget     = 3 * time.Second
)

// folderIndexDefaults is the usual bounds over one base directory.
func folderIndexDefaults(base string) folderIndexOpts {
	return folderIndexOpts{
		Base:   base,
		Depth:  folderIndexDepth,
		Plain:  folderIndexPlainDepth,
		Cap:    folderIndexCap,
		Budget: folderIndexBudget,
		Skip:   skipDirs,
	}
}

// folderIndexWalk finds the directories under a base, bounded every way
// [folderIndexOpts] names, and answers what it could not see as well as what it
// could.
//
// A DIRECTORY WITH A `.git` IN IT IS A ROOT AND IS NOT DESCENDED INTO. That is
// what keeps the walk small: a repository's own subdirectories are not other
// repositories, and the ones that are — a submodule, a vendored checkout — are
// reached by typing a path, which is what typing a path is for.
//
// A DIRECTORY WITHOUT ONE IS STILL OFFERED, down to Plain. People keep work in
// folders that were never a repository — `~/Documents/tax`, `~/Desktop/scans`,
// a directory of notes — and a chooser that could only see repositories was a
// chooser that had decided what counts as work.
func folderIndexWalk(ctx context.Context, opts folderIndexOpts) folderIndexAnswer {
	started := time.Now()
	answer := folderIndexAnswer{}
	if opts.Base == "" {
		answer.Took = time.Since(started)
		return answer
	}
	base := filepath.Clean(opts.Base)
	deadline := started.Add(opts.Budget)
	// The clock is read every so many entries rather than on each one: a
	// `time.Now` per directory on a tree with fifty thousand of them is itself
	// a cost worth avoiding, and a budget overshot by a few directories is a
	// budget kept.
	seen := 0
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// AN UNREADABLE DIRECTORY IS COUNTED AND STEPPED OVER. An index
			// that refused to exist because of one permission is worth less
			// than an index with one directory missing from it, and an index
			// that never mentioned the permission is worth less than both.
			if errors.Is(err, fs.ErrPermission) {
				answer.Denied++
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		seen++
		if seen%folderIndexTick == 0 {
			if ctx.Err() != nil || (opts.Budget > 0 && time.Now().After(deadline)) {
				answer.Bound = true
				return fs.SkipAll
			}
		}
		name := entry.Name()
		if path != base && (opts.Skip[name] || strings.HasPrefix(name, ".")) {
			return fs.SkipDir
		}
		if opts.Cap > 0 && len(answer.Roots) >= opts.Cap {
			answer.Bound = true
			return fs.SkipAll
		}
		down := folderDepth(base, path)
		if down > opts.Depth {
			return fs.SkipDir
		}
		at := time.Time{}
		if info, err := entry.Info(); err == nil {
			at = info.ModTime()
		}
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			answer.Roots = append(answer.Roots, folderIndexRoot{Path: path, Repo: true, At: at})
			return fs.SkipDir
		}
		if down > 0 && down <= opts.Plain {
			answer.Roots = append(answer.Roots, folderIndexRoot{Path: path, At: at})
		}
		return nil
	})
	// NEWEST FIRST, AND REPOSITORIES AHEAD OF PLAIN FOLDERS. The order a
	// source hands its candidates over in is what decides ties inside a layer,
	// and "the repository I touched most recently" is a better guess than "the
	// one alphabetically first" every single time.
	sort.SliceStable(answer.Roots, func(i, j int) bool {
		if answer.Roots[i].Repo != answer.Roots[j].Repo {
			return answer.Roots[i].Repo
		}
		return answer.Roots[i].At.After(answer.Roots[j].At)
	})
	answer.Took = time.Since(started)
	return answer
}

// folderIndexTick is how many directories go by between two looks at the clock.
const folderIndexTick = 64

// folderDepth is how many levels below base a path sits.
func folderDepth(base, path string) int {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// folderIndexPaths flattens an answer into the plain list of paths a store
// keeps, in the order the walk left them.
func folderIndexPaths(answer folderIndexAnswer) []string {
	out := make([]string, 0, len(answer.Roots))
	for _, root := range answer.Roots {
		out = append(out, root.Path)
	}
	return out
}

// folderIndexWord says what a walk COULD NOT SEE, and nothing when it saw
// everything. It is the honest half of the answer, and it is a sentence rather
// than a number because the number on its own is machinery.
func folderIndexWord(answer folderIndexAnswer) string {
	switch {
	case answer.Denied > 0:
		return strconv.Itoa(answer.Denied) + plural(" folder", answer.Denied) +
			" could not be read, so this list may be short"
	case answer.Bound:
		return "there was more to look through than this list holds"
	}
	return ""
}

// ── the walk, off the loop ──────────────────────────────────────────────────

// folderIndexMsg is a background walk, coming back, stamped like a read is.
type folderIndexMsg struct {
	answer folderIndexAnswer
	gen    uint64
}

// folderIndexCmd runs one bounded walk OFF THE LOOP. The context is made here
// and carries the budget, so a caller cannot start an unbounded one by
// forgetting to.
func folderIndexCmd(opts folderIndexOpts, gen uint64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if opts.Budget > 0 {
			var stop context.CancelFunc
			ctx, stop = context.WithTimeout(ctx, opts.Budget)
			defer stop()
		}
		return folderIndexMsg{answer: folderIndexWalk(ctx, opts), gen: gen}
	}
}
