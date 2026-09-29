// Command relay is the codeaf relay: a blind pipe between a machine that runs
// codeaf and a machine somebody is sitting at, and, given --store, the
// directory and blob store a person's machines share.
//
// IT IS A SEPARATE BINARY BECAUSE IT IS A SEPARATE THING. Nothing in it imports
// the session, the surface, or the wire they speak; it cannot open a frame and
// has nothing to open one with. Building it apart from `codeaf` is how that
// stays true — a relay that linked the session package would be one careless
// import away from being able to read what it forwards.
//
// It is also what internal/pair's own tests drive, which is the other reason it
// exists: the end-to-end story is tested against this exact service and not
// against a stub of it. `codeaf relay` runs the same relayserve.Main.
//
//	relay --listen :8787 [--store /var/lib/codeaf]
package main

import (
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/relayserve"
)

func main() {
	if err := relayserve.Main(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "relay:", err)
		os.Exit(1)
	}
}
