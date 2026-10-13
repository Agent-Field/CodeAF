package prebuilt

import "github.com/Agent-Field/codeaf/internal/apps/bridge"

const (
	// ID is the app id apps.prebuilt returns.
	ID = "factory"
	// Kind is the wire kind of that app. It is not a second id.
	Kind = "factory"
	// deniedMessage is what archive, delete, copy and Shape say for Factory.
	deniedMessage = "prebuilt"
)

// IsPrebuilt reports whether id is codeaf's prebuilt Factory.
// Lifecycle calls this at the start of Archive, Delete and Copy, and the
// Builder calls it at the start of Shape (EditPlan and Write). A true result
// returns Denied and does not change the app. This package does not edit
// those methods.
func IsPrebuilt(id string) bool { return id == ID }

// Denied is the bridge error those methods return for a prebuilt id.
func Denied() *bridge.Error {
	return bridge.Errorf(bridge.CodeDenied, deniedMessage)
}
