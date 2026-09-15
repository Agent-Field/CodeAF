package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── THE PIN A SURFACE NAMES IS THE PIN THE WIRE WOULD DEMAND ────────────────
//
// Issue #1022. The chip was drawn from the profile row and the wire asked the
// transport, so a pairing the wire retired mid-session left `@morph` on the
// model word over three turns another machine answered. The law in
// internal/provider says the two doors answer one question; these say this
// surface asks through that door and not around it.

// TRANSPORT ON AUTO, ROW STILL NAMING A MACHINE — the exact disagreement, made
// here by moving the transport rather than by collecting a 404, because what is
// under test is which of the two the surface reads.
func TestTheSurfaceNamesNoMachineTheTransportIsNotAskingFor(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 120, 24
	a.pinLane(flash, "cloudflare")
	if a.modelWord() != "deepseek-v4-flash@cloudflare" {
		t.Fatalf("the pin did not reach the model word: %q", a.modelWord())
	}

	// The row on disk is untouched and still names the machine; the transport
	// has stopped asking for it.
	provider.SetLanePin(provider.LanePin{})
	if name, pinned := config.LanePinned(a.profileDir, talkSlot); !pinned || !strings.EqualFold(name, "cloudflare") {
		t.Fatalf("the row stopped naming the machine (%q, %v), so this test is not about the disagreement it names", name, pinned)
	}

	if word := a.modelWord(); strings.Contains(word, laneAtSign) {
		t.Fatalf("the model word still names a machine nothing is asking for: %q", word)
	}
	if got := a.pinnedNow(); got != "" {
		t.Fatalf("the chip reads %q over requests that demand no machine", got)
	}
	if name, ok := a.laneNow(flash, laneViews(flash, a.now())); ok && strings.EqualFold(name, "cloudflare") {
		t.Fatal("a picker row would still say via cloudflare")
	}

	// AND THE FOLD MARKS `auto`, which is where the requests are going — not the
	// machine the row names.
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	screen := plain(frame(a))
	if line := screenLine(screen, "auto"); !strings.Contains(line, "●") {
		t.Fatalf("the fold does not mark auto:\n%s", screen)
	}
	if line := screenLine(screen, "cloudflare"); strings.Contains(line, "●") {
		t.Fatalf("the fold marks a machine nothing is asking for:\n%s", screen)
	}
}

// THE MODEL ROW'S TAIL, OVER EVERY STATE THE ROW AND THE WIRE CAN BE IN. It is
// one function of those two facts ([laneRowTail]) so that the states can be read
// back in one place.
func TestTheModelRowsTailNamesTheMachineOnlyWhileItIsAskedFor(t *testing.T) {
	for _, probe := range []struct {
		state string
		row   string
		force laneForce
		want  string
	}{
		{"auto", config.LaneAuto, laneForce{}, ""},
		{"nothing written", "", laneForce{}, ""},
		{"pinned and carried", "pinned: Cloudflare", laneForce{name: "Cloudflare"}, "pinned: cloudflare"},
		{"openrouter", config.LaneOpenRouter, laneForce{}, config.LaneOpenRouter},
		{
			"retired", "pinned: Morph", laneForce{standDown: "Morph"},
			"auto (morph cannot serve this model)",
		},
	} {
		if got := laneRowTail(probe.row, probe.force); got != probe.want {
			t.Fatalf("%s: the tail reads %q, want %q", probe.state, got, probe.want)
		}
	}
	// AND THE RETIRED TAIL IS THE SENTENCE'S OWN SPELLING, never a second one
	// written here.
	if want := provider.RetiredPinTail("morph"); !strings.HasSuffix(laneRowTail("pinned: Morph", laneForce{standDown: "Morph"}), want) {
		t.Fatalf("the tail does not end in %q", want)
	}
}

// ── THE FOLD'S TWO GESTURES ─────────────────────────────────────────────────

// ENTER ON THE MACHINE ALREADY IN FORCE TAKES THE PIN OFF, and the hint slot
// says so while the cursor is on that row. Going back to `auto` was otherwise a
// walk up past every machine in the fold, with another model's row — where enter
// switches the model — one press further (issue #1022).
func TestEnterOnThePinnedMachineUnpinsIt(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	a.pinLane(flash, "cloudflare")

	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	row, on := a.pick.laneUnder()
	if !on || row.lane < 0 || !strings.EqualFold(a.pick.lanes[row.lane].Name, "cloudflare") {
		t.Fatalf("the walk did not land on the pinned machine: %+v (on=%v)", row, on)
	}
	if got := a.hintWord(); !strings.HasPrefix(got, pickerKeysUnpin) {
		t.Fatalf("on the pinned machine the hint reads %q, want %q", got, pickerKeysUnpin)
	}

	drive(t, a, key("enter"))
	if got := a.pinnedNow(); got != "" {
		t.Fatalf("enter on the pinned machine left the pin at %q", got)
	}
	if word := config.LaneAt(a.profileDir, talkSlot); !strings.EqualFold(word, config.LaneAuto) {
		t.Fatalf("the row reads %q after the unpin, want auto", word)
	}

	// AND ENTER ON A MACHINE THAT IS NOT IN FORCE STILL PINS IT, which is the
	// other half of a toggle.
	typeLine(t, a, "/model")
	drive(t, a, key("right"), key("down"), key("enter"))
	if got := a.pinnedNow(); got == "" {
		t.Fatal("enter on an unpinned machine did not pin it")
	}
}

// TYPING WHILE A FOLD IS OPEN FILTERS THAT MODEL'S MACHINES. The shipped reading
// took the same keystrokes as a hunt for a MODEL — `morph` matched
// `morph/morph-v3-large` in the catalog — and closed the fold the person was
// standing in.
func TestTypingInsideAnOpenFoldFiltersTheMachines(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key("right"))

	typeInto(t, a, "core")
	if a.pick.unfold != flash {
		t.Fatalf("typing inside the fold closed it (fold %q)", a.pick.unfold)
	}
	// THE RUNGS ARE THE MODEL LIST'S OWN, so the machine that CARRIES the word
	// sorts above the one whose letters merely appear in order — `core` is a
	// subsequence of `cloudflare` — and deepinfra, which carries no `c` at all,
	// is not here.
	if names := laneNames(a.pick.lanes); len(names) != 2 || names[0] != "coreweave" || names[1] != "cloudflare" {
		t.Fatalf("the fold holds %v, want coreweave then cloudflare", names)
	}
	row, on := a.pick.laneUnder()
	if !on || row.lane != 0 {
		t.Fatalf("the cursor is not on the machine that matched best: %+v (on=%v)", row, on)
	}
	drive(t, a, key("enter"))
	if got := a.pinnedNow(); !strings.EqualFold(got, "coreweave") {
		t.Fatalf("enter on the matched machine pinned %q", got)
	}

	// AND A QUERY ONLY ONE MACHINE ANSWERS LEAVES ONLY THAT ONE.
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	typeInto(t, a, "corew")
	if names := laneNames(a.pick.lanes); len(names) != 1 || names[0] != "coreweave" {
		t.Fatalf("the fold holds %v, want coreweave alone", names)
	}
	drive(t, a, key("esc"))

	// AND A QUERY NO MACHINE MATCHES STILL FILTERS THE MODELS, closing the fold
	// with it — the fall-through that keeps one filter box doing one thing.
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	typeInto(t, a, "kimi")
	if a.pick.unfold != "" {
		t.Fatalf("a query matching no machine left the fold open at %q", a.pick.unfold)
	}
	if chosen, _ := a.pick.choice(); chosen.ID != "moonshotai/kimi-k3" {
		t.Fatalf("the model filter landed on %q", chosen.ID)
	}
}

// laneNames is a fold's machines, lowercased, for a test that is about which of
// them survived a filter and in what order.
func laneNames(views []laneView) []string {
	out := make([]string, 0, len(views))
	for _, view := range views {
		out = append(out, strings.ToLower(view.Name))
	}
	return out
}

// ── A ROUTING CHANGE LANDS ON THE NEXT MESSAGE ──────────────────────────────
//
// The row went to disk and nowhere else: the surface's reading of it and the
// session's clients were both launch snapshots, so a person cycled `routing`,
// watched the row change, and went on being routed by the old word — with the
// `lane` row beside it still explaining `auto` in that old word on the same
// screen (issue #1022).
func TestARoutingChangeReachesTheTransportAndThePanelAtOnce(t *testing.T) {
	laneLab(t, threeLanes())
	before := provider.RoutingNow()
	t.Cleanup(func() { provider.InstallRouting(before) })
	dir := t.TempDir()
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyRouting)
	if !ok || row.Apply(config.RoutingLatency) != nil {
		t.Fatal("could not write the routing row")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: flash}, Workspace: "/tmp/lab", ProfileDir: dir})
	a.models = func() []Model { return laneCatalog }
	a.width, a.height = 130, 40
	provider.InstallRouting(provider.RoutingLatency)

	a.openSettings()
	if a.sheet.routing != config.RoutingLatency {
		t.Fatalf("the panel opened under routing %q", a.sheet.routing)
	}
	meta, found := settingMetaFor(mustRow(t, a, config.KeyRouting))
	if !found {
		t.Fatal("the panel has no routing row")
	}
	a.applySetting(sheetItem{row: mustRow(t, a, config.KeyRouting), meta: meta}, config.RoutingSimple)

	// THE TRANSPORT, FIRST: the very next request is the one this has to reach.
	if got := provider.RoutingNow(); got != provider.RoutingSimple {
		t.Fatalf("after the write the transport routes by %q, want simple", got)
	}
	// AND THE SURFACE'S OWN TWO READINGS, on the same frame.
	if a.routing != config.RoutingSimple || a.sheet.routing != config.RoutingSimple {
		t.Fatalf("the surface reads routing %q / %q after the write", a.routing, a.sheet.routing)
	}
	laneMeta, ok := a.sheet.metaFor(mustRow(t, a, config.LaneSettingKey(talkSlot)))
	if !ok || !strings.Contains(laneMeta.about, "openrouter's own routing answers") {
		t.Fatalf("the lane row is still explained as %q", laneMeta.about)
	}
	// AND THE FOLD THE SAME ROW OPENS SAYS IT TOO.
	a.closeSettings()
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	if screen := plain(frame(a)); !strings.Contains(screen, "openrouter's own routing; codeaf stays out") {
		t.Fatalf("the fold still promises the old row's takeover:\n%s", screen)
	}
}
