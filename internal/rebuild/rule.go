package rebuild

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// LockFor is the lock that licenses the install folder at folder (slash
// separated, relative to tree): the nearest one, looking in the folder's parent
// and then each ancestor up to the root, so `desktop/node_modules` is rebuilt
// from `desktop/package-lock.json` even when the root has a lock of its own.
// carried says whether a lock will travel with the seal; a nearest lock that
// will not is no lock, because the other machine could not rebuild from it. A
// nil carried counts every lock.
func LockFor(tree, folder string, carried func(rel string) bool) (string, bool) {
	kind, ok := byFolder[path.Base(folder)]
	if !ok {
		return "", false
	}
	for dir := path.Dir(folder); ; dir = path.Dir(dir) {
		if lock, found := nearest(tree, dir, kind); found {
			return lock, carried == nil || carried(lock)
		}
		if dir == "." {
			return "", false
		}
	}
}

// nearest is the first of the kind's locks that counts in dir.
func nearest(tree, dir string, kind Kind) (string, bool) {
	for _, l := range kind.Locks {
		if rel := path.Join(dir, l.Name); l.usable(tree, rel) {
			return rel, true
		}
	}
	return "", false
}

// Tracked reports whether git tracks any file under folder. A tree with no git
// repository has no tracked file. A folder that git could not be asked about is
// treated as tracked, because the safe answer to "may this be left out" is no.
func Tracked(tree, folder string) bool {
	cmd := exec.Command("git", "ls-files", "-z", "--", folder) //codeaf:plumbing asks the project's own repository whether a folder is source
	cmd.Dir = tree
	out, err := cmd.Output()
	if err == nil {
		return len(out) > 0
	}
	return !notARepository(err)
}

// notARepository reports git's own answer for a folder outside any repository.
func notARepository(err error) bool {
	ee, ok := err.(*exec.ExitError)
	return ok && strings.Contains(string(ee.Stderr), "not a git repository")
}

// Found is what looking at a tree turned up.
type Found struct {
	// Folders are the install folders that may need leaving out, sorted.
	Folders []string
	// Locks are the lockfiles seen, sorted.
	Locks []string
}

// Scan looks for install folders and lockfiles. skip names paths that are not
// part of the seal, which are not entered. changed is the paths the seal will
// visit, nil for all of it: a full look walks the tree, and never enters an
// install folder it finds, so a dependency tree of a hundred thousand files
// costs one entry; a partial look reads only the changed paths.
//
// A FOLDER THAT WAS KNOWN STAYS KNOWN WHETHER OR NOT IT IS HERE. On a machine a
// chat moved to the folder is absent, and it is exactly the folder the record
// has to keep naming: it is absent because it is rebuilt, and a setup turn that
// brings it back must find it still left out.
func Scan(tree string, changed []string, before Found, skip func(rel string) bool) Found {
	if changed == nil {
		seen := walk(tree, skip)
		return Found{Folders: union(before.Folders, seen.Folders), Locks: seen.Locks}
	}
	return Found{Folders: union(before.Folders, foldersIn(changed)), Locks: exist(tree, union(before.Locks, locksIn(changed)))}
}

func walk(tree string, skip func(rel string) bool) Found {
	var f Found
	_ = filepath.WalkDir(tree, func(p string, d fs.DirEntry, err error) error {
		rel, ok := relative(tree, p, err)
		if !ok || rel == "." {
			return nil
		}
		if skip(rel) {
			return skipOf(d)
		}
		switch {
		case d.IsDir() && IsInstallFolder(d.Name()):
			f.Folders = append(f.Folders, rel)
			return fs.SkipDir
		case !d.IsDir() && IsLockName(d.Name()):
			f.Locks = append(f.Locks, rel)
		}
		return nil
	})
	sort.Strings(f.Folders)
	sort.Strings(f.Locks)
	return f
}

func relative(tree, p string, err error) (string, bool) {
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(tree, p)
	return filepath.ToSlash(rel), err == nil
}

// skipOf leaves out a folder with everything in it, and a file alone.
func skipOf(d fs.DirEntry) error {
	if d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// foldersIn is the install folder each changed path lies in, if it lies in one.
func foldersIn(changed []string) []string {
	var out []string
	for _, rel := range changed {
		parts := strings.Split(rel, "/")
		for i, part := range parts {
			if IsInstallFolder(part) {
				out = append(out, strings.Join(parts[:i+1], "/"))
				break
			}
		}
	}
	return out
}

// locksIn is the changed paths that are lockfiles.
func locksIn(changed []string) []string {
	var out []string
	for _, rel := range changed {
		if IsLockName(path.Base(rel)) {
			out = append(out, rel)
		}
	}
	return out
}

// exist keeps the paths that are still there, sorted and without repeats.
func exist(tree string, rels []string) []string {
	var out []string
	for _, rel := range rels {
		if lstatOK(tree, rel) {
			out = append(out, rel)
		}
	}
	return out
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rel := range append(append([]string(nil), a...), b...) {
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func lstatOK(tree, rel string) bool {
	_, err := os.Lstat(filepath.Join(tree, filepath.FromSlash(rel)))
	return err == nil
}
