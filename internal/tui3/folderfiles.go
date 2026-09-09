package tui3

// FILES IN THE BROWSER — the one listing both kinds of row come out of.
//
// folderpick.go opened as a chooser of DIRECTORIES, and that is half of what
// the owner asked for: a person deciding what this conversation is about is
// nearly always looking at a folder and a file in the same window — the
// repository, and the log inside it. So the middle column lists both, and which
// kind of row the cursor is on is what decides what the action row offers
// (folderact.go).
//
// THE TWO KINDS ARE KEPT APART IN THE STRUCT AND NOT IN A FLAG PER ROW.
// [folderListing.names] is still exactly what it was — the subdirectories, in
// readdir order — and the files come after them in their own slice. That
// ordering is Finder's and the screenshot's: what you can walk into first, what
// you can only look at second. It is also why every existing caller that asks
// about a directory row still reads the field it always read, and why a
// listing written down in a test without files is a listing of folders and
// nothing else.
//
// A DIRECTORY IS READ ONCE AND STAT'D SPARINGLY. `os.ReadDir` already returns
// every name; a SIZE, though, is one `lstat` per file, and a directory with
// forty thousand files in it would be forty thousand syscalls to fill a column
// eighteen rows tall. So sizes are gathered only while the count is under
// [folderSizeCap] and simply not drawn above it, which is the emptiness law
// doing the right thing by accident: a number nobody obtained is a number
// nobody draws.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// folderFile is one file on offer in the middle column: the name a person
// reads, and how many bytes it weighs.
type folderFile struct {
	name string
	// bytes is the size, and sized says it was actually obtained — a directory
	// past [folderSizeCap] is listed with no sizes at all rather than with a
	// column of zeroes [design-law §EMPTINESS].
	bytes int64
	sized bool
}

// folderRowsCap bounds how many rows one directory offers. A directory with
// half a million entries in it is a directory nobody scrolls, and holding every
// name would be this surface spending a person's memory on rows they will never
// reach. The bound is generous — five thousand is far past what anybody pages
// through. [folderListing.cut] records that it was reached, but nothing draws
// that fact yet.
const folderRowsCap = 5000

// folderSizeCap is how many files this browser will stat for their sizes. Two
// thousand lstats is a couple of milliseconds off the loop; forty thousand is a
// visible wait for a column of numbers nobody asked for.
const folderSizeCap = 2000

// folderCutWord is written for a directory whose rows were bounded. Nothing
// draws it yet, so no page may quote it as something a person sees.
const folderCutWord = "more of this folder is not shown"

// rows is how many rows this listing offers — the directories and then the
// files. It is the ONE count the cursor, the window and the pointer are all
// clamped against, so none of them can disagree about how long the column is.
func (l folderListing) rows() int { return len(l.names) + len(l.files) }

// isDir reports whether row `at` is a directory. The directories lead, so this
// is an index comparison and never a second lookup that could drift.
func (l folderListing) isDir(at int) bool { return at >= 0 && at < len(l.names) }

// rowName is the name drawn on one row, and "" for a row that is not there.
func (l folderListing) rowName(at int) string {
	switch {
	case at < 0:
		return ""
	case at < len(l.names):
		return l.names[at]
	case at-len(l.names) < len(l.files):
		return l.files[at-len(l.names)].name
	}
	return ""
}

// rowFile is the file on one row, and false where the row is a directory or is
// not there.
func (l folderListing) rowFile(at int) (folderFile, bool) {
	at -= len(l.names)
	if at < 0 || at >= len(l.files) {
		return folderFile{}, false
	}
	return l.files[at], true
}

// rowAt finds the row a NAME is on, and -1 when the listing does not hold it.
// It is what keeps a cursor on the thing it was on across a level arriving, a
// hidden-folder toggle and a walk back out ([folderCols.keep]).
func (l folderListing) rowAt(name string) int {
	if name == "" {
		return -1
	}
	for at, held := range l.names {
		if held == name {
			return at
		}
	}
	for at, held := range l.files {
		if held.name == name {
			return len(l.names) + at
		}
	}
	return -1
}

// folderEntries is ONE readdir turned into a column: the subdirectories, then
// the files, each half sorted by name.
//
// IT IS SORTED AND THE OLD ONE WAS NOT. `os.ReadDir` already sorts by name, so
// the directories came out ordered by accident; splitting the two kinds keeps
// that order within each half explicitly rather than relying on it.
//
// THE PRUNING RULE IS THE `@` WALK'S, AND IT IS THE SAME RULE FOR BOTH KINDS. A
// dot file is hidden exactly when a dot directory is, and [skipDirs] still
// holds for directories — `.git` and `node_modules` are not folders anybody
// browses to. One word, one rule: somebody who asked for the hidden things
// asked for all of them.
//
// THE ERROR IS CARRIED AND NEVER FLATTENED TO AN EMPTY LIST, for
// [folderListing]'s stated reason.
func folderEntries(dir string, hidden bool) folderListing {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return folderListing{err: err, done: true}
	}
	out := folderListing{done: true}
	// The sizes are gathered only where there are few enough of them to be
	// worth a syscall each; the count is known before the loop because ReadDir
	// has already answered.
	sizing := len(entries) <= folderSizeCap
	for _, entry := range entries {
		name := entry.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			if !hidden && skipDirs[name] {
				continue
			}
			if len(out.names) >= folderRowsCap {
				out.cut = true
				continue
			}
			out.names = append(out.names, name)
			continue
		}
		// A SYMLINK IS OFFERED AS WHATEVER IT POINTS AT, which is what a person
		// means by clicking it — and a broken one is offered as a file, whose
		// preview then says it is no longer here. Neither case is worth a row
		// this browser refuses to draw.
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
				if len(out.names) >= folderRowsCap {
					out.cut = true
					continue
				}
				out.names = append(out.names, name)
				continue
			}
		}
		if len(out.files) >= folderRowsCap {
			out.cut = true
			continue
		}
		file := folderFile{name: name}
		if sizing {
			if info, err := entry.Info(); err == nil {
				file.bytes, file.sized = info.Size(), true
			}
		}
		out.files = append(out.files, file)
	}
	sort.Slice(out.names, func(i, j int) bool { return folderNameLess(out.names[i], out.names[j]) })
	sort.Slice(out.files, func(i, j int) bool { return folderNameLess(out.files[i].name, out.files[j].name) })
	return out
}

// folderNameLess orders two names the way a person reads a directory: case
// folded, so `README` does not sort above every lowercase name on the machine,
// and by the raw bytes where the folded forms tie so the order is total.
func folderNameLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

// folderSizeWord is one file's size as the middle column draws it, and NOTHING
// for a size nobody obtained or a file with no bytes in it — [byteWord]'s own
// rule, which this surface keeps [design-law §EMPTINESS].
func folderSizeWord(file folderFile) string {
	if !file.sized {
		return ""
	}
	return byteWord(int(file.bytes))
}
