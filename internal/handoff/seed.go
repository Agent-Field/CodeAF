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

// seedFrom fills the empty folder dst with the tree at src: directories are
// made anew and files are hard links to the files of src, so a take that
// changes a few paths writes a few paths. A hard link is safe because the
// engine never writes into a file that exists: it writes a new file beside it
// and renames that over the old one, so the file in src keeps its bytes, mode
// and times whatever the take does to dst.
//
// Seeding only saves work and never decides what the tree holds: the engine
// restores the head over dst by content, adding what is missing, replacing
// what differs and removing what the head does not have. An entry that cannot
// be seeded (a socket, a link the filesystem refuses) is left out, and the
// engine writes it.
func seedFrom(src, dst string) {
	if !exists(src) {
		return
	}
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
		return os.Link(from, to) == nil
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
