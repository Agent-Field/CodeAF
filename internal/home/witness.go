package home

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var witnessed sync.Map

// ErrRemoved refuses late writes into a home this process has lost.
var ErrRemoved = errors.New("codeaf home was removed or replaced; restart codeaf to use the current folder")

// Witness remembers the first directory this process saw at dir. A later
// missing or replaced root must not be mistaken for a first launch.
func Witness(dir string) {
	key := absolute(dir)
	if before, ok := witness(dir); ok {
		witnessed.LoadOrStore(key, before)
		return
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		before, _ := witnessed.LoadOrStore(canonicalPath(key), info)
		witnessed.LoadOrStore(key, before)
	}
}

func witness(dir string) (any, bool) {
	key := absolute(dir)
	if before, ok := witnessed.Load(key); ok {
		return before, true
	}
	return witnessed.Load(canonicalPath(key))
}

// canonicalPath resolves the surviving ancestor when the home itself is gone.
// This preserves aliases such as /tmp and /private/tmp after a removal.
func canonicalPath(dir string) string {
	ancestor := dir
	var links int
	var rest []string
	for {
		if resolved, err := filepath.EvalSymlinks(ancestor); err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, rest[i])
			}
			return resolved
		}
		// A dangling symlink still identifies its missing target. Resolving only
		// its parent would turn that alias back into an unwitnessed first use.
		if target, err := os.Readlink(ancestor); err == nil {
			if links >= 255 {
				return dir
			}
			links++
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(ancestor), target)
			}
			ancestor = target
			continue
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return dir
		}
		rest = append(rest, filepath.Base(ancestor))
		ancestor = parent
	}
}

// Removed reports whether a witnessed root has disappeared or been replaced.
// Unwitnessed roots remain available for first use.
func Removed(dir string) bool {
	before, ok := witness(dir)
	if !ok {
		return false
	}
	after, err := os.Stat(dir)
	return os.IsNotExist(err) || (err == nil && !os.SameFile(before.(os.FileInfo), after))
}

// MkdirAllUnder makes directories without ever creating a witnessed root.
// Making children one at a time lets removal race the write without a recursive
// mkdir silently rebuilding the home. First use retains recursive creation.
func MkdirAllUnder(root, dir string, perm os.FileMode) error {
	root, dir = absolute(root), absolute(dir)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &os.PathError{Op: "mkdir", Path: dir, Err: os.ErrInvalid}
	}
	Witness(root)
	if _, ok := witness(root); !ok {
		if err := os.MkdirAll(dir, perm); err != nil {
			return err
		}
		Witness(root)
		return nil
	}
	if rel != "." {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if Removed(root) {
				return ErrRemoved
			}
			return nil
		}
	}
	if Removed(root) {
		return ErrRemoved
	}
	if rel == "." {
		return nil
	}
	next := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		next = filepath.Join(next, part)
		if err := os.Mkdir(next, perm); err != nil {
			if Removed(root) {
				return ErrRemoved
			}
			info, statErr := os.Stat(next)
			if !os.IsExist(err) || statErr != nil || !info.IsDir() {
				return err
			}
		}
	}
	if Removed(root) {
		return ErrRemoved
	}
	return nil
}
