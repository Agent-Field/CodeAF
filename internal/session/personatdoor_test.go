package session

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// A node run by a command a person typed is a node somebody is watching. The
// live-conversation map cannot say so — a headless command opens no
// conversation — and before the door's latch `aforge do` ran its nodes as
// unattended, so under `simple` the person's own pin never rode their calls.
func TestANodeRunByATypedCommandCountsAsWatched(t *testing.T) {
	before := provider.PersonAtTheDoor()
	t.Cleanup(func() { provider.SetPersonAtTheDoor(before) })
	provider.SetPersonAtTheDoor(false)

	node := &Agent{config: Config{InTask: true}}
	if someoneIsWatching() {
		t.Skip("another conversation is registered in this process; the map already answers")
	}
	if got := node.laneRole(); got != lane.RoleLeafUnattended {
		t.Fatalf("a node with nobody at the door ran as %q, want %q", got, lane.RoleLeafUnattended)
	}
	provider.SetPersonAtTheDoor(true)
	if got := node.laneRole(); got != lane.RoleLeafAttached {
		t.Fatalf("a node run by a typed command ran as %q, want %q", got, lane.RoleLeafAttached)
	}
	if !node.laneRole().Visible() {
		t.Fatal("the role a typed command's node runs as must be one a person is reading")
	}
}
