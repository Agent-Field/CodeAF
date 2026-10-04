package handoff

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// identityDir is the engine's own bookkeeping folder at the top of a tree. It
// names the workspace the folder belongs to, so a copy of it would make the
// staging folder pose as the chat's real root.
const identityDir = ".furrow"

// seedFrom fills the empty folder dst with the tree at src: a clone of it where
// the filesystem clones, else directories made anew and files hard-linked to
// the files of src, so a take that changes a few paths writes a few paths. A
// shared file is safe because the engine never writes into a file that exists:
// it writes a new file beside it and renames that over the old one, so the
// file in src keeps its bytes, mode and times whatever the take does to dst.
//
// Seeding only saves work and never decides what the tree holds: the engine
// restores the head over dst by content, adding what is missing, replacing
// what differs and removing what the head does not have. An entry that cannot
// be seeded (a socket, a link the filesystem refuses) is left out, and the
// engine writes it.
func seedFrom(src, dst string) {
	if !exists(src) || cloned(src, dst) {
		return
	}
	linkTree(src, dst)
}

// cloneTree makes dst, which must not exist, a copy-on-write clone of the tree
// at src in one call. It is a variable so a test can refuse the clone the way
// a filesystem that cannot clone does, and its platform file says which
// filesystems can.
var cloneTree = cloneTreeOnPlatform

// cloned seeds the empty folder dst with a clone of src and says whether it
// did. A clone is one system call where hard links are one per file, which is
// the difference between a tenth of a second and several seconds on a
// repository of thousands of files, and it gives the staging folder files of
// its own, so nothing in it can alias a file of src. When the clone is refused
// (another volume, a filesystem without clones) dst is the empty folder it was
// and the caller links instead.
func cloned(src, dst string) bool {
	if os.Remove(dst) != nil {
		return false
	}
	if err := cloneTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return os.Mkdir(dst, 0o700) != nil
	}
	_ = os.RemoveAll(filepath.Join(dst, identityDir))
	return true
}

// linkTree is the seed for a filesystem that cannot clone: directories made
// anew and files hard-linked, an entry that cannot be linked left out.
func linkTree(src, dst string) {
	var dirs []string
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(src, path)
		if err != nil || relErr != nil || rel == identityDir {
			return skipEntry(d)
		}
		if rel != "." && !seedEntry(path, filepath.Join(dst, rel), d) {
			return skipEntry(d)
		}
		if d.IsDir() {
			dirs = append(dirs, rel)
		}
		return nil
	})
	settleDirs(src, dst, dirs)
}

// linkFile makes a hard link. It is a variable so a test can refuse the link
// the way a staging folder on another filesystem does.
var linkFile = os.Link

// skipEntry leaves out an entry, and what is below it when it is a folder.
func skipEntry(d fs.DirEntry) error {
	if d != nil && d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// seedEntry makes the copy of one entry in the staging folder and says
// whether it is there.
func seedEntry(from, to string, d fs.DirEntry) bool {
	switch {
	case d.IsDir():
		return os.Mkdir(to, 0o700) == nil
	case d.Type()&fs.ModeSymlink != 0:
		return linkSymlink(from, to)
	case d.Type().IsRegular():
		return linkFile(from, to) == nil
	}
	return false
}

func linkSymlink(from, to string) bool {
	target, err := os.Readlink(from)
	if err != nil || os.Symlink(target, to) != nil {
		return false
	}
	info, err := os.Lstat(from)
	return err == nil && setSymlinkTime(to, info.ModTime())
}

func setSymlinkTime(path string, at time.Time) bool {
	ts := unix.NsecToTimespec(at.UnixNano())
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, []unix.Timespec{ts, ts}, unix.AT_SYMLINK_NOFOLLOW) == nil
}

// settleDirs gives each seeded folder the mode and time of its original. It
// runs last and deepest first because making an entry in a folder moves its
// time, and a folder whose mode forbids writing could not be filled.
func settleDirs(src, dst string, dirs []string) {
	for i := len(dirs) - 1; i >= 0; i-- {
		info, err := os.Stat(filepath.Join(src, dirs[i]))
		if err != nil {
			continue
		}
		to := filepath.Join(dst, dirs[i])
		_ = os.Chmod(to, info.Mode().Perm()|info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky))
		_ = os.Chtimes(to, info.ModTime(), info.ModTime())
	}
}
