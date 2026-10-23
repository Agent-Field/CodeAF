package cellstore

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// captureIndex turns the guard's mandatory full screen into an engine change
// list. Its baseline advances only after a successful seal; a failed capture
// therefore cannot forget a change. It is local to a seat, never trusted after
// a restart, restore or a move (D3, D4).
type captureIndex struct {
	mu    sync.Mutex
	trees map[string]map[string]fileStamp
}

type fileStamp struct {
	Size int64
	Mode fs.FileMode
	At   time.Time
	Info fs.FileInfo
}

type captureScan struct {
	index        *captureIndex
	key          string
	before, next map[string]fileStamp
	failed       bool
}

func (x *captureIndex) begin(key string) *captureScan {
	if x == nil {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return &captureScan{index: x, key: key, before: x.trees[key], next: map[string]fileStamp{}}
}

func (s *captureScan) visit(rel string, d fs.DirEntry) {
	if s == nil || rel == "." {
		return
	}
	info, err := d.Info()
	if err != nil {
		s.failed = true
		return
	}
	_ = os.SameFile(info, info)
	s.next[rel] = fileStamp{info.Size(), info.Mode(), info.ModTime(), info}
}

func (s *captureScan) changed(tree string) []string {
	if s == nil || s.before == nil || s.failed {
		return nil
	}
	out := []string{}
	for name, stamp := range s.next {
		before, known := s.before[name]
		if !known || before.Size != stamp.Size || before.Mode != stamp.Mode || before.At != stamp.At || !os.SameFile(before.Info, stamp.Info) {
			out = append(out, name)
		}
	}
	for name := range s.before {
		if _, ok := s.next[name]; !ok {
			out = append(out, name)
		}
	}
	// Git history is intentionally carried whole. The secret scanner does not
	// enter .git, so its files cannot be inferred from the screen's metadata.
	out = append(out, ".git")
	return out
}

func (s *captureScan) commit() {
	if s == nil || s.failed {
		return
	}
	s.index.mu.Lock()
	defer s.index.mu.Unlock()
	if s.index.trees == nil {
		s.index.trees = map[string]map[string]fileStamp{}
	}
	s.index.trees[s.key] = s.next
}

func captureKey(tree, id string) string { return filepath.Clean(tree) + "\x00" + id }
