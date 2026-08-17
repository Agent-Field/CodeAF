package main

import (
	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The consent card's door back to disk: where "always" is written down.
//
// It is a PAIR — one seam for a tool, one for a shell command — handed to the
// surface together, because they answer the same question against the two rows
// that can answer it: tools.approval names a tool, tools.bashPatterns names a
// command line, and the card knows which of the two it is looking at.
//
// It lives in its own file beside chatv3.go for chatv2_rail.go's stated reason:
// the seam that lets the v3 surface reach a registry row is a different thing
// from the window's assembly, and keeping them apart means a wave editing one
// does not collide with a wave editing the other.
//
// THE PROFILE IS THE PLACE. A repository's .openaf/config.json is never written
// from here — it is a file a team commits, and a keystroke on a consent card
// must not commit to somebody's repository. internal/config's approvalmemory.go
// states the honest consequence: while a repository answers one of these rows,
// its answer replaces the person's WHOLE at launch, so what is written here
// takes effect everywhere except inside that repository.

// saveToolApproval remembers one tool's allow in the person's own profile.
//
// The error is returned rather than dropped here, which is the ONE difference
// from saveRail's shape (chatv2_rail.go). The rail's caller has nothing left to
// say once the sidebar has moved; this one's caller is about to print a receipt
// claiming the answer was saved, so it has to be told whether that is true. The
// failure is still dropped — the surface drops it, silently, and keeps the
// session-scoped always it always had.
func saveToolApproval(profileDir, tool string) error {
	return config.RememberToolApproval(profileDir, tool, "allow")
}

// saveBashApproval remembers one whole command line as an allow rule.
//
// It is never a deny, and neither is the seam above: the card only ever persists
// a yes (internal/tui3's consent.go says why), and a standing never is a line a
// person writes in the settings sheet on purpose.
func saveBashApproval(profileDir, command string) error {
	return config.RememberBashApproval(profileDir, command)
}
