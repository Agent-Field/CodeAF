package main

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// doAlways is the rules section a headless `codeaf do` run's workers close on:
// the person's memories marked always that hold for the workspace the run edits
// — their own, this machine's, and that project's — exactly the rules a
// conversation opened in that folder would carry (internal/session's
// memory_always.go).
//
// IT OPENS THE STORE FOR THE READ AND CLOSES IT AFTER. A headless run holds no
// brain of its own, and one read at the start of the run is the whole of what it
// needs from one: the rules are a birth fact of the run, read once.
//
// MEMORY OFF IS RULES OFF, and a store that will not open is no section either,
// which is the same emptiness law every other birth section reads: a run with no
// rule over it gets a brief with no section, never an empty heading.
func doAlways(profileDir, workspace string) string {
	path := v3MemoryPath(profileDir)
	if path == "" {
		return ""
	}
	brain, err := store.Open(path)
	if err != nil {
		return ""
	}
	defer brain.Close()
	return session.AlwaysWorld(brain, session.MemoryOwners(v3ProjectKey(workspace)))
}
