package atlas

import (
	"fmt"
	"strings"
)

// Maps is the ordered registry of every atlas map. Every entry point —
// `codeaf atlas`, the /atlas slash command and their pickers — reads this list
// and nothing else; adding a map here is all it takes to publish one.
var Maps = []*Map{
	Pairing,
}

// ByName answers the map a person named, in the order it was registered.
func ByName(name string) (*Map, bool) {
	for _, mp := range Maps {
		if mp.Name == name {
			return mp, true
		}
	}
	return nil, false
}

// NoMap is the one sentence every entry point says for a name that is not in
// the registry — `codeaf atlas nope` on stderr, /atlas nope in the
// transcript — so the two surfaces cannot drift apart about what went wrong
// and what was available instead.
func NoMap(name string) string {
	names := make([]string, 0, len(Maps))
	for _, mp := range Maps {
		names = append(names, mp.Name)
	}
	return fmt.Sprintf("no map %q — available maps: %s", name, strings.Join(names, ", "))
}
