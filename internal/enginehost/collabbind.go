package enginehost

import "github.com/Agent-Field/codeaf/internal/wscollab"

// BindFinder installs the locator the collaboration router asks when a
// conversation has no bound journal seam. It is this package's
// [session.RegisterRunEngine]: the router owns the var so it cannot import
// us, and we bind it. Nil is "no host today", which is pending, not an
// invented recorded line.
func BindFinder(finder wscollab.HostFinder) {
	wscollab.RegisterHostFinder(finder)
}

// init binds the default locator at load. Spawn is empty, so a retired host
// leaves the envelope pending until a door that can actually start a process
// (cmd/codeaf's attach) rebinds with one.
func init() { BindFinder(Locator{}) }
