package main

import (
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// doMemory is the memory section a headless `codeaf do` run's workers close
// on: every line kept for the person, this machine and the workspace's
// project — exactly what a conversation opened in that folder would carry
// (internal/session's memory.go).
//
// IT OPENS THE STORE FOR THE READ AND CLOSES IT AFTER. A headless run holds no
// store of its own, and one read at the start of the run is the whole of what
// it needs: what is kept is a birth fact of the run. A store that will not open
// is no section, never an empty heading.
func doMemory(workspace string) string {
	brain, err := store.Open(defaultChatDB())
	if err != nil {
		return ""
	}
	defer brain.Close()
	return session.MemoryWorld(brain, session.MemoryOwners(v3ProjectKey(workspace), gitidentity.ProjectKeys(workspace)...))
}
