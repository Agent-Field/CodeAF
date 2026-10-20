package resident

import (
	"github.com/Agent-Field/codeaf/internal/filememo"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/skills"
)

type importStamp struct {
	path  string
	info  os.FileInfo
	ctime int64
}
type importMemory struct {
	mu    sync.Mutex
	held  map[importKey][]importStamp
	scans uint64
}

// unchanged stats remembered dependencies without listing directories or
// reading skill content. Directories detect new children; file stamps detect
// edits inside existing children. Links also retain their resolved trees.
func (m *importMemory) unchanged(key importKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	stamps, ok := m.held[key]
	if !ok {
		return false
	}
	for _, stamp := range stamps {
		info, err := os.Stat(stamp.path)
		if stamp.info == nil && os.IsNotExist(err) {
			continue
		}
		if err != nil || stamp.info == nil || !os.SameFile(info, stamp.info) || info.Size() != stamp.info.Size() || !info.ModTime().Equal(stamp.info.ModTime()) || info.Mode() != stamp.info.Mode() {
			return false
		}
		ctime, _, _, ok := fileStamp(info)
		if !ok || ctime != stamp.ctime {
			return false
		}
	}
	return true
}

func importStamps(key importKey) ([]importStamp, bool) {
	paths := skills.InputPaths(skills.Options{ProjectDir: key.project, HomeDir: key.home})
	var stamps []importStamp
	seen := map[string]bool{}
	valid := true
	var walk func(string)
	walk = func(path string) {
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			stamps = append(stamps, importStamp{path: path})
			return
		}
		if err != nil {
			valid = false
			return
		}
		ctime, _, _, ok := fileStamp(info)
		if !ok {
			valid = false
		}
		stamps = append(stamps, importStamp{path, info, ctime})
		if !info.IsDir() {
			return
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			valid = false
			return
		}
		if resolved != path {
			walk(resolved)
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			valid = false
			return
		}
		for _, entry := range entries {
			walk(filepath.Join(path, entry.Name()))
		}
	}
	for _, path := range paths {
		walk(path)
	}
	return stamps, valid
}

func (m *importMemory) remember(key importKey, stamps []importStamp) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held == nil {
		m.held = make(map[importKey][]importStamp)
	}
	if _, ok := m.held[key]; !ok && len(m.held) >= filememo.MaxEntries {
		for k := range m.held {
			delete(m.held, k)
			break
		}
	}
	m.held[key] = stamps
}
