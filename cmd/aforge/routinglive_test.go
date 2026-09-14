package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE ROUTING ROW IS INSTALLED, NEVER HANDED DOWN ─────────────────────────
//
// THE MEASURED FAILURE (issue #1022). This door read the routing row and wrote
// it onto the session's config, which the session handed to every client it
// built as an answer OF ITS OWN — and an answer handed down beats the row this
// process installs, because an engine host and a task child carry their parent's
// row that way on purpose. So the conversation's clients were pinned to the word
// that was on disk at launch: somebody cycled `routing` in the settings panel,
// watched the row change, and went on being routed by the old word until they
// relaunched.
//
// The fix is an absence, and an absence is the one thing a reader cannot see, so
// it is asserted here: the door installs the row and hands down nothing.
func TestTheProfileDoorInstallsTheRoutingRowAndHandsDownNone(t *testing.T) {
	before := provider.RoutingNow()
	t.Cleanup(func() { provider.InstallRouting(before) })
	t.Setenv("AFORGE_HOME", t.TempDir())
	dir := t.TempDir()
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyRouting)
	if !ok || row.Apply(config.RoutingLatency) != nil {
		t.Fatal("could not write the routing row")
	}

	built, err := applyV3Governance(session.Config{Workspace: t.TempDir()}, dir, false, false)
	if err != nil {
		t.Fatalf("the door refused a plain profile: %v", err)
	}
	if built.Routing != "" {
		t.Fatalf("the door handed the session its own routing answer (%q), which no later write can overrule", built.Routing)
	}
	if got := provider.RoutingNow(); got != provider.RoutingLatency {
		t.Fatalf("the door installed routing %q, want the row the person wrote", got)
	}

	// AND THE ROW MOVES UNDER THE SESSION THAT WAS BUILT, which is the whole
	// point of installing it: the panel re-installs on the write
	// ([config.InstallRoutingRow]) and the next request is answered by the new
	// word with nothing relaunched.
	if row.Apply(config.RoutingSimple) != nil {
		t.Fatal("could not cycle the routing row")
	}
	config.InstallRoutingRow(dir)
	if got := provider.RoutingNow(); got != provider.RoutingSimple {
		t.Fatalf("after the write the transport routes by %q, want simple", got)
	}
	if built.Routing != "" {
		t.Fatal("the session built a moment ago acquired a routing row of its own")
	}
}
