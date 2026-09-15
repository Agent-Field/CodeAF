package main

import (
	"context"
	"testing"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// TestTheExecDoorRunsAsALeafSomebodyIsWatching is the door's half of the
// `lane.talk` scope.
//
// THE MEASURED FAILURE (2026-09-13). With `routing: simple` and a machine
// pinned, `codeaf exec --model … --debug` put a body on the wire carrying
// `provider: null`: the pin a person had written was not demanded, and nothing
// said so. The cause was here — this command named no role at all, so every
// call it made read as [lane.RoleUnknown], which is a hidden background errand,
// and the routing row's demand rides only the calls a person is reading.
//
// The other half is internal/provider's: a visible role draws the pin under
// `simple` and an unattended one does not
// (TestUnderSimpleOnlyThePersonsOwnTurnCarriesTheTalkPin).
func TestTheExecDoorRunsAsALeafSomebodyIsWatching(t *testing.T) {
	role := provider.RoleFrom(typedDoorContext(context.Background()))
	if role != lanes.RoleLeafAttached {
		t.Fatalf("`codeaf exec` runs as %q, want the leaf somebody is watching", role)
	}
	// THE CLAIM IS WHAT THE TABLE MAKES OF IT, and the table is the authority:
	// a role is only the person's own turn to everything downstream if it reads
	// as visible there (internal/lane's roles.go).
	if !role.Visible() {
		t.Fatalf("%q is not a role anybody is reading, so a pinned run would still go out bare", role)
	}
	// AND A NODE A SPAWNER LAUNCHED IS STILL NOT. The subharness asks its own
	// questions under this role (subharness_env.go) and it must stay the
	// conservative reading: nobody is waiting on one, so it buys no speed with
	// somebody's money and demands no machine of its own.
	if lanes.RoleLeafUnattended.Visible() {
		t.Fatal("an unattended leaf now reads as watched, which puts every spawned node back on the person's pin")
	}
}
