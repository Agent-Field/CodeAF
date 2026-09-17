package tui3

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/lane"
)

// ── THE PICKER IS A TREE, AND THE LANE IS WRITTEN ON THE MODEL ──────────────
//
// docs/design/lanes-picker/DESIGN.md. The owner opened /model on 2026-09-10,
// pressed ← and →, and asked how anybody changes the provider of a model. Every
// test here is one of the four answers, driven the way a person drives it.

// walkCatalog puts the model in use twelfth, which is where the owner's own
// catalog put it: [picker.start] opens the cursor on it, and [listTop] leaves it
// on the LAST row of the twelve-row window — so anything appended under it is
// off the screen.
func walkCatalog() []Model {
	models := make([]Model, 0, 16)
	for _, id := range []string{
		"a/one", "a/two", "a/three", "a/four", "a/five", "a/six",
		"a/seven", "a/eight", "a/nine", "a/ten", "a/eleven",
	} {
		models = append(models, Model{ID: id})
	}
	models = append(models, Model{ID: flash, ContextLength: 1_000_000})
	for _, id := range []string{"z/one", "z/two", "z/three", "z/four"} {
		models = append(models, Model{ID: id})
	}
	return models
}

// screenLine is the one line of a plain frame that carries want, or empty.
func screenLine(screen, want string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

// A. `→` WALKS IN. On the model in use, sitting on the last row of the window,
// the first `→` a person presses has to change what they see: the machines come
// into view WITH the model they belong to, and the cursor is on the row that is
// true right now — `auto`, with nothing pinned.
func TestArrowWalksIntoTheFoldAndBringsItIntoView(t *testing.T) {
	laneLab(t, threeLanes())
	a := pickerApp(t, &fakeAgent{model: flash}, walkCatalog())
	a.profileDir = t.TempDir()
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	if strings.Contains(plain(frame(a)), "openrouter") {
		t.Fatalf("the fold is open before anybody asked:\n%s", plain(frame(a)))
	}

	drive(t, a, key("right"))
	if line := screenLine(plain(frame(a)), "auto"); !strings.Contains(line, "›") {
		t.Fatalf("the cursor did not walk in onto auto: %q\n%s", line, plain(frame(a)))
	}
	// The machines are a SECOND fold, under `openrouter` ([picker.machines]),
	// and `→` walks into them exactly as it walked into this one.
	drive(t, a, key("down"), key("right"))
	screen := plain(frame(a))
	for _, want := range []string{flash, "auto", "cloudflare", "coreweave", "deepinfra", "openrouter"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("after → the frame does not show %q:\n%s", want, screen)
		}
	}
	if line := screenLine(screen, "cloudflare"); !strings.Contains(line, "›") {
		t.Fatalf("the second → did not walk in onto the first machine: %q\n%s", line, screen)
	}

	// With a machine pinned, the walk lands on THAT row instead. The cursor is
	// already on cloudflare — the second `→` walked it there — so enter pins it.
	// Enter chooses and leaves the list up, so it is closed before it is
	// opened again ([app.pickerKey]).
	drive(t, a, key("enter"), key("esc"))
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	row, on := a.pick.laneUnder()
	if !on || row.lane < 0 || !strings.EqualFold(a.pick.lanes[row.lane].Name, "Cloudflare") {
		t.Fatalf("→ with cloudflare pinned walked onto %+v (on=%v)", row, on)
	}
	// The machines are a table, so the row is its name and its cells rather than
	// a `·` tail (modeltable.go). The row is found by its `note` cell, because
	// the machine's NAME is also on the model row above it, in the `via` column.
	if line := screenLine(plain(frame(a)), "no tools"); !strings.Contains(line, "›") {
		t.Fatalf("the cursor is not on the pinned row:\n%s", plain(frame(a)))
	}
}

// C. THE HINT SLOT SAYS WHAT `→` DOES RIGHT NOW. On a model's row it offers the
// fold; inside it, the way back out; and with text typed before the caret —
// where `←` would edit the box — it names `tab` instead of promising a key that
// does something else.
func TestTheHintSlotSaysWhatTheArrowDoesNow(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width = 130
	typeLine(t, a, "/model")

	if got := a.hintWord(); !strings.HasPrefix(got, pickerKeysModel) {
		t.Fatalf("on a model row the slot reads %q, want it to lead with %q", got, pickerKeysModel)
	}
	if !strings.Contains(plain(frame(a)), pickerKeysModel) {
		t.Fatalf("the frame does not draw %q:\n%s", pickerKeysModel, plain(frame(a)))
	}
	drive(t, a, key("right"))
	if got := a.hintWord(); !strings.HasPrefix(got, pickerKeysFold) {
		t.Fatalf("inside the fold the slot reads %q, want %q", got, pickerKeysFold)
	}
	drive(t, a, key("left"))
	if got := a.hintWord(); !strings.HasPrefix(got, pickerKeysModel) {
		t.Fatalf("back on the model the slot reads %q", got)
	}

	// Found by typing: `→` still opens from the end of the box, and inside the
	// fold `tab` is the way out.
	typeInto(t, a, "flash")
	if got := a.hintWord(); !strings.HasPrefix(got, pickerKeysModel) {
		t.Fatalf("with the caret at the end the slot reads %q", got)
	}
	drive(t, a, key("right"))
	if got := a.hintWord(); !strings.HasPrefix(got, pickerKeysFoldTab) {
		t.Fatalf("inside a fold with text typed the slot reads %q, want %q", got, pickerKeysFoldTab)
	}
	drive(t, a, key("tab"))
	if a.pick.unfold != "" {
		t.Fatal("tab inside the fold did not close it")
	}
}

// D. A PIN IS WRITTEN ON THE MODEL'S NAME, everywhere the chrome names the
// model — the seam, the phone deck's chip — and on /status as its own `lane`
// line, with /status --json's `model` exactly what it was. On `auto` nothing is
// added.
func TestAPinnedLaneIsWrittenOnTheModelsName(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 120, 24
	if seam := plain(frame(a)); !strings.Contains(seam, "deepseek-v4-flash") || strings.Contains(seam, laneAtSign) {
		t.Fatalf("on auto the chrome should name the model alone:\n%s", seam)
	}

	typeLine(t, a, "/model @cloudflare")
	if seam := plain(frame(a)); !strings.Contains(seam, "deepseek-v4-flash@cloudflare") {
		t.Fatalf("the seam does not carry the pin:\n%s", seam)
	}
	a.width = 44
	if chip := plain(a.deckModelRow(44)); !strings.Contains(chip, "deepseek-v4-flash@cloudflare") {
		t.Fatalf("the phone deck's chip does not carry the pin: %q", chip)
	}
	a.width = 120

	a.slash("/status")
	if note := lastNote(t, a); !strings.Contains(note, "\nlane ") || !strings.Contains(note, "cloudflare") {
		t.Fatalf("/status does not name the pinned lane:\n%s", note)
	}
	a.slash("/status --json")
	var object map[string]string
	if err := json.Unmarshal([]byte(lastNote(t, a)), &object); err != nil {
		t.Fatalf("/status --json is not an object: %v", err)
	}
	if object["model"] != flash || object["lane"] != "cloudflare" {
		t.Fatalf("/status --json reads model %q lane %q", object["model"], object["lane"])
	}

	// PRESSING THE NAME OPENS THE PICKER ON THE PIN, fold open and cursor on it.
	_ = frame(a)
	x, y := a.seamModelSpan.from+1, markedRowY(a, chromeLegend, 0)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !a.pick.open || a.pick.unfold != flash {
		t.Fatalf("the press opened the picker=%v with fold %q", a.pick.open, a.pick.unfold)
	}
	if row, on := a.pick.laneUnder(); !on || row.lane < 0 || !strings.EqualFold(a.pick.lanes[row.lane].Name, "cloudflare") {
		t.Fatalf("the press did not land on the pinned lane: %+v (on=%v)", row, on)
	}
	drive(t, a, key("esc"))

	// A hosted window cannot see the far machine's pin, so it says none.
	a.host = "devbox"
	if a.modelWord() != "deepseek-v4-flash" {
		t.Fatalf("a hosted window wrote %q", a.modelWord())
	}
	a.host = ""

	typeLine(t, a, "/model auto")
	if seam := plain(frame(a)); strings.Contains(seam, laneAtSign+"cloudflare") {
		t.Fatalf("auto left the pin on the name:\n%s", seam)
	}
}

// AND UNDER ROUTING `off` THERE IS NEITHER. That row sends no lane choice and
// measures nothing, so a fold would offer machines no request asks for and a
// line promising measurements that never come, and `@cloudflare` on the name
// would be a pin the wire is not carrying. The capability is absent.
func TestRoutingOffOpensNoFoldAndWritesNoPin(t *testing.T) {
	laneLab(t, threeLanes())
	dir := t.TempDir()
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyRouting)
	if !ok || row.Apply(config.RoutingOff) != nil {
		t.Fatal("could not write the routing row")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: flash}, Workspace: "/tmp/lab", ProfileDir: dir})
	a.models = func() []Model { return laneCatalog }
	a.width, a.height = 120, 24
	a.pinLane(flash, "cloudflare")

	if a.modelWord() != "deepseek-v4-flash" {
		t.Fatalf("under routing off the name reads %q", a.modelWord())
	}
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	if a.pick.unfold != "" {
		t.Fatalf("under routing off → opened the fold at %q", a.pick.unfold)
	}
	if got := a.hintWord(); strings.Contains(got, "providers") {
		t.Fatalf("under routing off the slot offers the fold: %q", got)
	}
}

// AND UNDER ROUTING `simple` THE FOLD OPENS AND THE `auto` ROW STOPS PROMISING
// A TAKEOVER. That row sends no preference of codeaf's own: with nothing pinned
// the request goes out bare and OpenRouter's own routing answers, so nothing on
// this side chooses a machine, predicts which one the next turn lands on, or
// rescues an answer that turns slow — and the sentence a person reads on the row
// where they decide whether to leave the choosing alone has to say so.
func TestUnderSimpleRoutingTheAutoRowPromisesNoTakeover(t *testing.T) {
	laneLab(t, threeLanes())
	dir := t.TempDir()
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyRouting)
	if !ok || row.Apply(config.RoutingSimple) != nil {
		t.Fatal("could not write the routing row")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: flash}, Workspace: "/tmp/lab", ProfileDir: dir})
	a.models = func() []Model { return laneCatalog }
	a.width, a.height = 120, 24

	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	if a.pick.unfold != flash {
		t.Fatalf("under routing simple → left the fold at %q", a.pick.unfold)
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, laneAutoSaid(config.RoutingSimple).note) {
		t.Fatalf("the auto row does not say who is choosing under simple:\n%s", screen)
	}
	// THE TWO CLAIMS THAT ONLY A CHOOSER CAN MAKE ARE GONE WITH IT: the machine
	// the next turn would go to, which under this row nobody on this side picks,
	// and the takeover the sentence used to promise.
	for _, gone := range []string{"codeaf takes over", "cloudflare now"} {
		if strings.Contains(screen, gone) {
			t.Fatalf("under routing simple the fold still says %q:\n%s", gone, screen)
		}
	}
	// AND THE SETTINGS PANEL EXPLAINS THE SAME ROW THE SAME WAY, because both
	// read the one door (palette.go's [laneAutoSaid]).
	drive(t, a, key("esc"))
	a.openSettings()
	meta, found := a.sheet.metaFor(mustRow(t, a, config.LaneSettingKey(talkSlot)))
	if !found || !strings.Contains(meta.about, "openrouter's own routing answers") {
		t.Fatalf("the lane row is explained as %q", meta.about)
	}
}

// mustRow is one registry row by key, for a test that is about what the panel
// SAYS about it rather than about finding it.
func mustRow(t *testing.T, a *app, key string) config.Setting {
	t.Helper()
	row, ok := a.sheet.registry.Row(key)
	if !ok {
		t.Fatalf("the registry has no row %q", key)
	}
	return row
}

// DEFECT 5. A BELIEF WHOSE CONFIDENCE HAS DECAYED HAS NO TAIL. A day without a
// sighting doubles the spread a hundred and forty-four times, and the p99 of
// that is +Inf — which a row once printed as `tail 9223372036854775807s`.
func TestADecayedBeliefDrawsNoTail(t *testing.T) {
	old := laneBelief(flash, "Cloudflare", 800, 58, 0.5, lane.Facts{
		Tools: true, Uptime5m: 100, MaxOut: 345_000, Quant: "fp8",
	})
	old.At = time.Now().Add(-24 * time.Hour)
	laneLab(t, map[string][]lane.Belief{flash: {old}})

	views := laneViews(flash, timeNow())
	if len(views) != 1 {
		t.Fatalf("want one view, got %+v", views)
	}
	view := views[0]
	if view.Tail != 0 || !view.Vague {
		t.Fatalf("a decayed belief has tail %v (vague=%v)", view.Tail, view.Vague)
	}
	if note := laneNote(view); strings.Contains(note, "tail") {
		t.Fatalf("a decayed belief is noted %q", note)
	}
	left, right := laneRowText(view, 120)
	if strings.Contains(left+right, "922337") || strings.Contains(left+right, "tail") {
		t.Fatalf("the row draws a tail nobody measured: %q %q", left, right)
	}
	// And a p99 that is finite but past the longest a request may stay open is
	// the same arithmetic, not a wait.
	if tail, vague := laneTail(math.MaxFloat64, 1); tail != 0 || !vague {
		t.Fatalf("an absurd p99 became tail %v", tail)
	}
}

// DEFECT 6. EMPTYING THE BOX PUTS THE CURSOR BACK ON THE MODEL IN USE, which
// is where the picker opened it: a list with nothing typed is the list the
// picker opened on, and enter on it confirms.
func TestClearingTheFilterReturnsToTheModelInUse(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "gpt-5-classic"}, pickerCatalog)
	typeLine(t, a, "/model")
	typeInto(t, a, "kimi")
	if chosen, _ := a.pick.choice(); chosen.ID != "moonshotai/kimi-k3" {
		t.Fatalf("the filter left the cursor on %q", chosen.ID)
	}
	drive(t, a, key("ctrl+u"))
	if chosen, _ := a.pick.choice(); chosen.ID != "gpt-5-classic" {
		t.Fatalf("ctrl+u left the cursor on %q, want the model in use", chosen.ID)
	}
}

// THE MODEL ROW PREDICTS NO MACHINE WHERE NOTHING CHOOSES. The `via` on a
// picker row is a claim about the next request, and under `simple` the next
// request names nobody: a row that still said `via modal` while the status
// line and the record said Sail Research was the surface predicting a choice
// nobody was making (the 2026-09-13 acceptance drive). The ranked road keeps
// its prediction, because there the chooser really is about to make one.
func TestUnderSimpleRoutingTheModelRowPredictsNoMachine(t *testing.T) {
	views := []laneView{{Name: "modal", Known: true, TTFT: 0.8, Rate: 90}}
	now := time.Now()
	if got := laneAuto(config.RoutingSimple, "vendor/quiet", views, now); got != "" {
		t.Fatalf("under simple the model row would say via %q, want no machine", got)
	}
	if got := laneAuto(config.RoutingLatency, "vendor/quiet", views, now); got != "modal" {
		t.Fatalf("under latency the model row names %q, want the machine the belief holds", got)
	}
	if note := rowAll(modelFields(Model{ID: "vendor/quiet", ContextLength: 128_000}, "", config.RoutingSimple)); strings.Contains(note, "via ") {
		t.Fatalf("the simple model row still carries a prediction: %q", note)
	}
	// AND NOT THE PREDICTED MACHINE'S NUMBERS EITHER: with no name and no
	// chooser the row speaks for no lane, so it draws no wait and no rate.
	if shown, ok := laneShown(config.RoutingSimple, views, ""); ok {
		t.Fatalf("under simple the model row speaks for %q, want no lane", shown.Name)
	}
	if shown, ok := laneShown(config.RoutingSimple, views, "modal"); !ok || shown.Name != "modal" {
		t.Fatal("a machine the row names still speaks for itself under simple")
	}
	if shown, ok := laneShown(config.RoutingLatency, views, ""); !ok || shown.Name != "modal" {
		t.Fatal("under latency the best-known lane still speaks for the row")
	}
}
