package tui3

import (
	"strings"
	"testing"
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
