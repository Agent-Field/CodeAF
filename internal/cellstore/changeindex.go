package cellstore

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// captureIndex remembers directory membership, not file contents. It widens an
// executor's hints for newly created files and atomic editor replacements
// without repeating the engine's full file walk (U2). Only capture-plan tokens
// authorize reads; these observations are never a secret-screening boundary.
type captureIndex struct {
	mu   sync.Mutex
	dirs map[string]map[string]fileStamp
}

type fileStamp struct {
	At   time.Time
	Info fs.FileInfo
}

func captureKey(tree, id string) string { return filepath.Clean(tree) + "\x00" + id }

func (x *captureIndex) hints(key, tree string, changed []string) []string {
	if changed == nil {
		return nil
	}
	if x == nil {
		return changed
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	dirs, known := x.dirs[key]
	if !known {
		return nil
	}
	out := append([]string{}, changed...)
	for rel, before := range dirs {
		info, err := os.Stat(filepath.Join(tree, filepath.FromSlash(rel)))
		if err != nil || info.ModTime() != before.At || info.Size() != before.Info.Size() || info.Mode() != before.Info.Mode() || !os.SameFile(info, before.Info) {
			// Git updates refs, index and object directories atomically. If any of
			// that private metadata changed, history and the associated working files
			// need a full plan, including edits outside the executor's hints.
			if rel == ".git" || strings.HasPrefix(rel, ".git/") {
				return nil
			} else {
				out = append(out, rel)
			}
		}
	}
	return out
}

func (x *captureIndex) noteDirectories(key string, full bool, next map[string]fileStamp) {
	if x == nil {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.dirs == nil {
		x.dirs = map[string]map[string]fileStamp{}
	}
	dirs := x.dirs[key]
	if dirs == nil || full {
		dirs = map[string]fileStamp{}
	}
	for rel, stamp := range next {
		dirs[rel] = stamp
	}
	x.dirs[key] = dirs
}
