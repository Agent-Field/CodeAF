package tui3

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	screen := plain(frame(a))
	for _, want := range []string{flash, "● auto", "cloudflare", "coreweave", "deepinfra", "○ openrouter"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("after → the frame does not show %q:\n%s", want, screen)
		}
	}
	if line := screenLine(screen, "● auto"); !strings.Contains(line, "›") {
		t.Fatalf("the cursor did not walk in onto auto: %q\n%s", line, screen)
	}

	// With a machine pinned, the walk lands on THAT row instead.
	drive(t, a, key("down"), key("enter"))
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	row, on := a.pick.laneUnder()
	if !on || row.lane < 0 || !strings.EqualFold(a.pick.lanes[row.lane].Name, "Cloudflare") {
		t.Fatalf("→ with cloudflare pinned walked onto %+v (on=%v)", row, on)
	}
	if line := screenLine(plain(frame(a)), "0.8s · 58 t/s"); !strings.Contains(line, "›") {
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
