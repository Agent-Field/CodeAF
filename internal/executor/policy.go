package executor

import (
	"path/filepath"
	"sync"
)

// rule is what one class allows (docs/ARCHITECTURE.md section 8.3). Network
// policy is the side-effect oracle: a call with the network denied is provably
// local, one with it open is external.
type rule struct {
	Spawns bool      // may run a process at all
	Net    NetPolicy // an ordinary call
	Setup  NetPolicy // a setup turn's call (network allowed, external)
}

var (
	openNet = NetPolicy{Open: true}
	// rules is the whole class to policy mapping; there is no other place a
	// policy is chosen.
	rules = map[Class]rule{
		FilesOnly: {Spawns: false},
		Sandboxed: {Spawns: true, Net: NetPolicy{}, Setup: openNet},
		HostBound: {Spawns: true, Net: openNet, Setup: openNet},
	}
	classNames = map[string]Class{"files-only": FilesOnly, "sandboxed": Sandboxed, "host-bound": HostBound}
)

func (c Class) rule() rule { return rules[c] }

// PolicyFor is the network policy of a call under a class.
func PolicyFor(c Class, setup bool) NetPolicy {
	if setup {
		return c.rule().Setup
	}
	return c.rule().Net
}

// ParseClass reads a declared class name as stored in a cell's meta.
func ParseClass(name string) (Class, bool) {
	c, ok := classNames[name]
	return c, ok
}

// A workspace without a declared class is a legacy one: it runs as it always
// has, unjailed with the network open, which is exactly the host-bound rule.
const undeclared = HostBound

var declared = struct {
	sync.RWMutex
	byRoot map[string]Class
}{byRoot: map[string]Class{}}

// Declare records the class a workspace root was declared with. It is called
// when a cell is opened; workspaces never declared keep the legacy rule.
func Declare(root string, c Class) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	declared.Lock()
	declared.byRoot[abs] = c
	declared.Unlock()
}

// ClassOf is the class declared for the workspace that holds dir, found by
// walking up from dir; an undeclared tree is legacy.
func ClassOf(dir string) Class {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return undeclared
	}
	declared.RLock()
	defer declared.RUnlock()
	for p := abs; ; p = filepath.Dir(p) {
		if c, ok := declared.byRoot[p]; ok {
			return c
		}
		if p == filepath.Dir(p) {
			return undeclared
		}
	}
}
