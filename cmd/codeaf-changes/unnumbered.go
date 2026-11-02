package main

import (
	_ "embed"
	"strings"
	"sync"
)

// unnumbered names the entries excused a pull request number: the ones that
// reached the branch by a direct push before the rule held everywhere. The
// file says why; the list only shrinks.
//
//go:embed unnumbered.txt
var unnumbered string

var frozenSet = sync.OnceValue(func() map[string]bool {
	set := map[string]bool{}
	for _, name := range frozenNames() {
		set[name] = true
	}
	return set
})

// frozenNames are the file names unnumbered.txt lists, in its order.
func frozenNames() []string {
	var names []string
	for _, line := range strings.Split(unnumbered, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			names = append(names, line)
		}
	}
	return names
}

// frozenUnnumbered reports that an entry named base, carrying no number, is
// one of the entries excused it.
func frozenUnnumbered(base string, pr int) bool {
	return pr <= 0 && frozenSet()[base]
}
