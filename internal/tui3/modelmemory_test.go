package tui3

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// A model chosen here has to outlive the session, and the door is the only
// thing that knows where "outlive" is written down ([Options.SaveModel]).

// Every road to a model change is the same road, so all three write. The
// picker's enter and the settings sheet's talk row both land in switchModel;
// /model <slug> lands there too.
func TestChoosingAModelWritesItWhereTheNextLaunchReadsIt(t *testing.T) {
	dir := t.TempDir()
	a := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: "opening/model"},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
		SaveModel:  func(model string) error { return config.WriteChatModel(dir, model) },
	})
	a.width, a.height = 80, 24

	a.switchModel("chosen/model", 100000)

	if got := config.ChatModelAt(dir); got != "chosen/model" {
		t.Fatalf("the profile answered %q after a switch, want the model that was chosen", got)
	}
	// The live half is unchanged by the write: the session is on it, and the
	// note says so.
	if a.model != "chosen/model" {
		t.Fatalf("the surface is on %q", a.model)
	}

	// A second choice replaces the first — what is read back is where the
	// person ended up, not where they passed through.
	a.switchModel("second/model", 0)
	if got := config.ChatModelAt(dir); got != "second/model" {
		t.Fatalf("the profile answered %q after a second switch", got)
	}
}

// NIL IS A SURFACE THAT CANNOT REMEMBER, and it must be silent about it: no
// panic, no note, and above all nothing written to a profile nobody named. The
// --host door is this surface on purpose, and so is every test that does not
// ask to persist.
func TestASurfaceWithNoSeamSwitchesModelsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	a := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: "opening/model"},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
	})
	a.width, a.height = 80, 24

	a.switchModel("chosen/model", 100000)

	if a.model != "chosen/model" {
		t.Fatalf("the switch itself did not take: %q", a.model)
	}
	if got := config.ChatModelAt(dir); got != "" {
		t.Fatalf("a surface with no seam wrote %q into the profile", got)
	}
}

// And the guard on the value itself: a switch that resolved to nothing is not a
// choice, and writing it would leave the next launch resolving an empty row.
func TestAnEmptyModelIsNeverWrittenDown(t *testing.T) {
	dir := t.TempDir()
	if err := config.WriteChatModel(dir, "chosen/model"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: ""},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
		SaveModel:  func(model string) error { return config.WriteChatModel(dir, model) },
	})
	a.width, a.height = 80, 24

	a.rememberModel("   ")

	if got := config.ChatModelAt(dir); got != "chosen/model" {
		t.Fatalf("an empty switch overwrote the choice with %q", got)
	}
}
