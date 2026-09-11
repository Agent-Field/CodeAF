package standing

// filewatch.go is what a file watch reads: which paths its pattern reaches, and
// how a file is compared with the last reading of it.
//
// `**` IS A WHOLE SEGMENT THAT REACHES DOWN. `filepath.Glob` reads `**` as `*`,
// so `inbox/**/*.md` reached exactly one folder down and an edit two folders
// down woke nothing (validator S02). A pattern with a `**` segment is read by
// walking the folder above its first wildcard, and `**` matches any number of
// folders there, none included. A pattern without one is still read by
// `filepath.Glob`, exactly as before.
//
// A WATCH IS BOUNDED WHEN IT IS SET UP. Every pass reads every entry a watch
// reaches, so a watch whose folders hold more than [WatchLimit] entries is
// refused with one line saying so ([WatchTooLarge]), rather than walking a
// whole disk every five minutes. One that grows past the limit later stops at
// it and says so on its check line; it never reads a partial world as the
// truth.
//
// AN IDENTICAL REWRITE IS NOT A CHANGE. Size and modification time are the
// first comparison, and they are free. Only a file seen for the first time, or
// one whose size or time moved, is read and hashed; a file whose time moved and
// whose contents did not is the same file ([fileEntry.differs]). A file larger
// than [HashLimit] is never read and is compared by size and time alone, and so
// is a folder the pattern matches.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// WatchLimit is the most files and folders one watch may read on a pass.
// It is the same figure the stores aim every folder at, and for the same
// reason: a pass reads all of them.
const WatchLimit = 10000

// HashLimit is the largest file whose contents a watch compares. Past it a
// file is compared by size and modification time only.
const HashLimit = 4 << 20

// recursiveSegment is the one pattern segment that reaches down.
const recursiveSegment = "**"

// errWatchTooLarge stops a walk the moment it passes [WatchLimit].
var errWatchTooLarge = errors.New("watch too large")

// WatchTooLarge is the watch limit's refusal, which a person reads at setup
// and on a check line.
type WatchTooLarge struct{ Glob string }

func (w WatchTooLarge) Error() string {
	return fmt.Sprintf("%s reaches more than %d files and folders, and a watch reads every one of them on every pass; watch a narrower pattern", w.Glob, WatchLimit)
}

// CheckWatch refuses what the pass could not honour as written: a condition
// with nothing to judge it against, a say line with a placeholder nothing fills
// (condition.go), and a file watch whose pattern cannot be read or reaches more
// than [WatchLimit] entries. It is checked where an item is set up or its
// waking or its line changes ([Store.Create], [Store.Revise]), and by whoever
// asks a person first, so the refusal is heard before a yes that could only
// fail.
//
// IT IS NOT PART OF [Item.Validate], ON PURPOSE. Validate runs on every write
// of a document, a pause and a quiet check included, and an item made before
// one of these refusals existed must still be pausable and stoppable. A
// refusal about what an item SAYS belongs where the person says it.
func (it Item) CheckWatch() error {
	if err := it.checkCondition(); err != nil {
		return err
	}
	if err := it.checkSay(); err != nil {
		return err
	}
	if it.When.Kind != WhenFile {
		return nil
	}
	// BRACES ARE NOT EXPANDED, SO THEY ARE REFUSED HERE, where an item is
	// written. `{inbox/*,notes/*}` reads as those characters and matches
	// nothing, and a watch that can never fire says nothing about it (the live
	// one-path case, 2026-09-11). The ticker does not refuse an item written
	// before this: it reads it as a watch that matches nothing ([bracedQuiet]).
	if hasBraces(it.When.Glob) {
		return fmt.Errorf("standing: the pattern %q uses braces, which a watch does not expand: one order watches one pattern. Watch a folder they are all under, if its report is not inside it; otherwise tell the person one order cannot watch those folders into one report, since two orders cannot keep one file", it.When.Glob)
	}
	_, err := watched(it.Workspace, it.When.Glob)
	return err
}

// hasBraces says glob spells a set of alternatives a watch does not expand.
func hasBraces(glob string) bool {
	return strings.Contains(glob, "{") && strings.Contains(glob, ",") && strings.Contains(glob, "}")
}

// bracedQuiet is the check line of a watch written with braces before they
// were refused: it matches nothing, which the item's page says plainly.
const bracedQuiet = "its pattern uses braces, which a watch does not expand, so it matches nothing — change its pattern"

// watched answers every path the pattern reaches, absolute, or the one line
// saying why it cannot.
func watched(workspace, glob string) ([]string, error) {
	pattern := glob
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(workspace, pattern)
	}
	segments := strings.Split(filepath.ToSlash(pattern), "/")
	for _, segment := range segments {
		if _, err := filepath.Match(segment, ""); err != nil {
			return nil, fmt.Errorf("standing: cannot read the pattern %q: %w", glob, err)
		}
	}
	fixed := 0
	for fixed < len(segments) && segments[fixed] != recursiveSegment && !hasMeta(segments[fixed]) {
		fixed++
	}
	if !recursive(segments[fixed:]) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("standing: cannot read the pattern %q: %w", glob, err)
		}
		if len(matches) > WatchLimit {
			return nil, WatchTooLarge{Glob: glob}
		}
		return matches, nil
	}
	root := filepath.FromSlash(strings.Join(segments[:fixed], "/"))
	if root == "" {
		root = string(filepath.Separator)
	}
	rest := segments[fixed:]
	var matches []string
	read := 0
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil || path == root {
			// An unreadable or missing folder holds nothing to watch, the way
			// `filepath.Glob` reads one.
			return nil
		}
		read++
		if read > WatchLimit {
			return errWatchTooLarge
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr == nil && matchSegments(rest, strings.Split(filepath.ToSlash(relative), "/")) {
			matches = append(matches, path)
		}
		return nil
	})
	if errors.Is(err, errWatchTooLarge) {
		return nil, WatchTooLarge{Glob: glob}
	}
	if err != nil {
		return nil, fmt.Errorf("standing: cannot read the pattern %q: %w", glob, err)
	}
	return matches, nil
}

// watchMatches answers whether a pattern reaches name, both relative to the
// same place, by the same reading [watched] walks with.
func watchMatches(pattern, name string) bool {
	return matchSegments(strings.Split(filepath.ToSlash(pattern), "/"), strings.Split(filepath.ToSlash(name), "/"))
}

// matchSegments matches a path segment by segment, `**` standing for any
// number of whole segments, none included.
func matchSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == recursiveSegment {
		for skip := 0; skip <= len(path); skip++ {
			if matchSegments(pattern[1:], path[skip:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	matched, _ := filepath.Match(pattern[0], path[0])
	return matched && matchSegments(pattern[1:], path[1:])
}

func recursive(segments []string) bool {
	for _, segment := range segments {
		if segment == recursiveSegment {
			return true
		}
	}
	return false
}

func hasMeta(segment string) bool { return strings.ContainsAny(segment, `*?[\`) }

// contentHash is a file's hash for this reading: the last reading's while its
// size and time have not moved, a fresh one when they have or it is new, and
// none for a folder, a file past [HashLimit] or one that cannot be read.
func contentHash(path string, info fs.FileInfo, last fileEntry) string {
	if !info.Mode().IsRegular() || info.Size() > HashLimit {
		return ""
	}
	if last.Hash != "" && last.Size == info.Size() && last.MTime == info.ModTime().UnixNano() {
		return last.Hash
	}
	file, err := openRegular(path, 0)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, HashLimit+1)); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// errNotRegular is a path that turned out, once opened, not to be a file.
var errNotRegular = errors.New("not a regular file")

// openRegular opens a file to read ONLY IF IT IS A REGULAR FILE, and never
// waits to find out.
//
// A PIPE OPENED TO READ WAITS FOR A WRITER, and the pass has nobody to give up
// on it: one named pipe in a watched folder stopped every standing item behind
// it, forever (wave 5 review). Every caller asks the file's kind before it
// opens, and this closes the gap between that look and the open — a pipe put
// in the file's place is opened without waiting (O_NONBLOCK is nothing to a
// regular file) and refused by what the open handle says it is. flags adds to
// the open: [noFollow] where the path was already resolved and a link in its
// place is a swap, never a file.
func openRegular(path string, flags int) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|flags, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errNotRegular
	}
	return file, nil
}
