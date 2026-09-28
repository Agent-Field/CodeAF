// Package builtin is the list of programs this build carries — the one place a
// program becomes part of codeaf (internal/delegate). The chat's rows, the
// command line's verbs, the prompt's hand-off paragraph and the manual all read
// this list, so a program is added by one package and one line here, and a
// program not on it does not exist anywhere.
//
// IT IS A LIST IN CODE, NOT A FOLDER ON THE MACHINE. Nothing is installed, and
// no program can differ from the codeaf it ships in. Programs from outside the
// binary are a later road; the first draft of it, manifests read from disk,
// is kept on the tag delegate-manifest-v1.
//
// THIS PACKAGE IS WHERE THE WEIGHT IS. It imports every program it carries, so
// only the doors that must hand a program to something — the command line and
// the chat's launch, both in cmd/codeaf — import it. internal/session and
// internal/run are handed the list and never import it, or a test binary of
// either would carry every program's engine.
package builtin

import (
	"sort"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// list is what this build carries: [carried] for this platform, or what a
// test put in its place ([Override]).
var list = carried

// All is every program this build carries, sorted by name, which is the order
// lists draw them.
func All() []delegate.Delegate {
	out := append([]delegate.Delegate(nil), list...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find answers the program with this name.
func Find(name string) (delegate.Delegate, bool) {
	for _, program := range list {
		if program.Name == name {
			return program, true
		}
	}
	return delegate.Delegate{}, false
}

// Override puts programs in the list's place and answers the restore. It is
// for tests of the doors that read the list, which need a program to exist
// that is not senior-dev's whole engine; nothing in the product calls it.
func Override(programs []delegate.Delegate) (restore func()) {
	previous := list
	list = programs
	return func() { list = previous }
}
