// Package cachedir owns the one directory under aforge's state root that holds
// nothing but caches: ~/.aforge/cache, where every task worker's toolchain
// caches live (internal/exec's sweCacheRoot puts go-mod, go-build, cargo, npm
// and pip under it).
//
// The package exists so that "delete the cache" is one verb with one blast
// radius, written down once. Everything in this directory is re-downloadable by
// construction — a module cache is content-addressed, a build cache is derived —
// so deleting it costs cold builds and nothing else. What it must NEVER touch
// is everything beside it under the state root: the journal, the CAS, the v3
// session files, the config and the credentials are a person's own things, and
// a "cache clean" that reached them would be data loss wearing a janitor's
// name. Both surfaces that offer the verb — `aforge cache clean` and the chat's
// /cache clean — call in here rather than spelling a path of their own, so the
// radius cannot drift between them.
package cachedir

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// Root is the cache directory itself. It is the parent of exec's toolchain
// root on purpose: anything a future lane files under cache/ is cache by
// address, and this verb reaps it without being taught its name.
func Root() string { return home.Join("cache") }

// Size is how many bytes the cache holds right now. A cache that does not
// exist, or a corner of it that cannot be read, counts as zero — this figure
// decides what a person is told they would free, and the honest answer to an
// unreadable entry is to promise less rather than fail the question.
func Size() int64 {
	var total int64
	_ = filepath.WalkDir(Root(), func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Clean deletes the cache directory whole and reports how many bytes that
// freed. A cache that is already absent is zero bytes freed and no error.
//
// The chmod walk first is not decoration: Go's module cache is written
// read-only, directories included, precisely so nothing edits a module in
// place — and a read-only directory is one os.RemoveAll cannot empty. Owner
// write is added back onto every directory before the removal, and a chmod
// that fails is skipped rather than fatal, because the removal itself is about
// to say definitively whether the tree could be taken down.
func Clean() (int64, error) {
	root := Root()
	freed := Size()
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			_ = os.Chmod(path, info.Mode().Perm()|0o200)
		}
		return nil
	})
	if err := os.RemoveAll(root); err != nil {
		return 0, err
	}
	return freed, nil
}

// Human is the byte figure as a person reads it: whole bytes under a kilobyte,
// one decimal above, stepping through KB, MB and GB. One formatter shared by
// both surfaces, so the CLI's answer and the chat's answer to "how big" can
// never disagree about what 514 MB is called.
func Human(size int64) string {
	switch {
	case size < 1<<10:
		return itoa(size) + " B"
	case size < 1<<20:
		return tenth(size, 1<<10) + " KB"
	case size < 1<<30:
		return tenth(size, 1<<20) + " MB"
	}
	return tenth(size, 1<<30) + " GB"
}

// tenth divides to one decimal place, rounded, without a float.
func tenth(n, unit int64) string {
	t := (n*10 + unit/2) / unit
	return itoa(t/10) + "." + itoa(t%10)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
