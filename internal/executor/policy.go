package executor

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
