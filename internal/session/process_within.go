package session

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// A CONVERSATION READ FOR SOMEBODY ELSE'S PATH IS PINNED BY DESCRIPTOR, NOT BY
// STRING. A team file names each member's transcript, and that file may have
// been written by another machine or by anything with the user's rights, so its
// path is untrusted input. Inspecting the path and then opening it by name
// leaves a gap in which a folder can be swapped for a link; here each component
// below the places root is looked at with Lstat and opened relative to the one
// before it, and the opened folder must be the folder looked at. Every later
// read of the history goes through that opened folder, so nothing can leave it.

// ErrWithinRoot is the refusal of a location that is not exactly a regular
// transcript file in a conversation folder in a bucket below the root.
var ErrWithinRoot = errors.New("that conversation is not in the places root")

// OpenConversationWithin opens the folder places/bucket/id as a root, each
// component taken without following a link, and answers it only when it holds
// a regular transcript file under its fixed name. The caller closes the root.
func OpenConversationWithin(places, bucket, id string) (*os.Root, error) {
	top, err := os.OpenRoot(places)
	if err != nil {
		return nil, err
	}
	bucketRoot, err := openDirWithin(top, bucket)
	top.Close()
	if err != nil {
		return nil, err
	}
	conv, err := openDirWithin(bucketRoot, id)
	bucketRoot.Close()
	if err != nil {
		return nil, err
	}
	info, err := conv.Lstat(placeTranscript)
	if err != nil || !info.Mode().IsRegular() {
		conv.Close()
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil, fs.ErrNotExist
		}
		return nil, ErrWithinRoot
	}
	return conv, nil
}

// openDirWithin opens name, one component of root, as a root of its own. A
// link, a file or a name that is not one plain component is refused, and the
// opened folder must be the one inspected.
func openDirWithin(root *os.Root, name string) (*os.Root, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return nil, ErrWithinRoot
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrWithinRoot
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, ErrWithinRoot
	}
	return child, nil
}

// openRegularWithin opens a regular file at rel (slash-separated, at most one
// folder deep) under root for reading, refusing a link at any component.
func openRegularWithin(root *os.Root, rel string) (*os.File, error) {
	parts := strings.Split(rel, "/")
	for i := range parts[:len(parts)-1] {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errNotRegular
		}
	}
	seen, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !seen.Mode().IsRegular() {
		return nil, errNotRegular
	}
	file, err := root.OpenFile(rel, os.O_RDONLY|rootReadFlags, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(seen, opened) {
		file.Close()
		return nil, errNotRegular
	}
	return file, nil
}

// legacyMarkAt is the mark of the k-th legacy array as it stands, and false
// when there is no regular file there.
func (s processStore) legacyMarkAt(k int) (legacyMark, bool) {
	if s.anchor == nil {
		return legacyMarkOf(processLegacyFiles(s.dir)[k], processLegacyNames[k])
	}
	info, err := s.anchor.Lstat(processLegacyNames[k])
	if err != nil || !info.Mode().IsRegular() {
		return legacyMark{}, false
	}
	return legacyMark{File: processLegacyNames[k], Size: info.Size(), MTime: info.ModTime().UnixNano()}, true
}

// openLegacy opens the k-th legacy array for reading without following a link.
func (s processStore) openLegacy(k int) (*os.File, error) {
	if s.anchor == nil {
		return openRegularNoFollow(processLegacyFiles(s.dir)[k])
	}
	return openRegularWithin(s.anchor, processLegacyNames[k])
}

// ReadProcessPageWithinRoot is [ReadProcessPage] for a conversation named by a
// bucket and an id below places rather than by a path. It pins the folder as
// [OpenConversationWithin] does and reads only through it. It never creates,
// folds or repairs anything.
func ReadProcessPageWithinRoot(ctx context.Context, places, bucket, id string, ask ProcessPageAsk) (ProcessPage, error) {
	limit, err := CheckProcessPageAsk(ask)
	if err != nil {
		return ProcessPage{}, err
	}
	conv, err := OpenConversationWithin(places, bucket, id)
	if err != nil {
		return ProcessPage{}, err
	}
	defer conv.Close()
	return processStore{dir: conv.Name(), anchor: conv}.page(ctx, ask, limit, nil, nil, false)
}
