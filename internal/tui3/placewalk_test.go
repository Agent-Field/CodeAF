package tui3

import (
	"strings"
	"testing"
	"time"
)

// WALKING BETWEEN THE PLACES, AND WHAT A PLACE THAT WILL NOT OPEN OWES YOU.
//
// The owner's report against the real binary: "in home left right does not seem
// to move tabs, only shift does". The cause was not the arrows — it was `tab`
// walking into the tasks place, being refused because this machine had run no
// work, and being put straight back on home. Every press did that, so the
// circle had one member and `shift+tab` (which lands on settings, and settings
// always opens) was the only key that appeared to work.

// TAB WALKS TO A PLACE THAT ACTUALLY OPENS. The circle is the whole set of
// rooms; a room that has nothing in it to show is one the walk goes past, and a
// walk that stopped dead at it would make `tab` a key that does nothing.
func TestTabWalksPastAPlaceThatCannotOpen(t *testing.T) {
	a := placeApp(t)
	if a.taskSheetHasAnything() {
		t.Skip("this surface has work to show, so the tasks place does not refuse")
	}
	drive(t, a, key("tab"))
	if a.page == pageHome {
		t.Fatal("tab from home stayed on home")
	}
	if a.page == pageTasks {
		t.Fatal("tab landed on a place that cannot open")
	}
	// AND ROUND AGAIN, however many rooms are shut: pressing it seven times must
	// visit more than one place.
	seen := map[page]bool{a.page: true}
	for i := 0; i < 7; i++ {
		drive(t, a, key("tab"))
		seen[a.page] = true
	}
	if len(seen) < 2 {
		t.Fatalf("tab visited only %v", seen)
	}
	if !seen[pageHome] {
		t.Fatal("the circle never came back to home")
	}
}

// AND shift+tab IS THE SAME CIRCLE WALKED THE OTHER WAY, past the same shut
// rooms.
func TestShiftTabWalksTheCircleBack(t *testing.T) {
	a := placeApp(t)
	drive(t, a, key("shift+tab"))
	if a.page == pageHome {
		t.Fatal("shift+tab from home stayed on home")
	}
	back := a.page
	drive(t, a, key("tab"))
	if a.page != pageHome {
		t.Fatalf("tab back from %q landed on %q rather than home", back.word(), a.page.word())
	}
}

// A PLACE THAT REFUSES SAYS WHY, WHERE THE PERSON IS STANDING. `alt+2` on a
// machine that has run nothing used to write its sentence into the transcript
// under a screen drawn over the top of it, so the key read as broken.
func TestAPlaceThatRefusesSaysSoOnTheFrame(t *testing.T) {
	a := placeApp(t)
	if a.taskSheetHasAnything() {
		t.Skip("this surface has work to show, so the tasks place does not refuse")
	}
	drive(t, a, key("alt+2"))
	if a.page != pageHome {
		t.Fatalf("the refused place took the band: %q", a.page.word())
	}
	if !strings.Contains(placeFrameText(a), taskSheetEmpty) {
		t.Fatalf("the refusal is nowhere on the frame:\n%s", placeFrameText(a))
	}
}

// THE SHIFT ARROWS BELONG TO TIME AND NEVER TO THE TAB BAR (SCREEN 3d). If
// `shift+←→` moved between places anywhere, the two axes of one gesture would
// mean two different things one place apart.
func TestTheShiftArrowsNeverSwitchPlaces(t *testing.T) {
	a := placeApp(t)
	for _, id := range []page{pageHome, pageSpend, pageSearch, pageSettings} {
		a.showPage(id)
		if a.page != id {
			continue
		}
		for _, k := range []string{"shift+left", "shift+right", "shift+up", "shift+down"} {
			drive(t, a, key(k))
			if a.page != id {
				t.Fatalf("%s moved from the %s place to %q", k, id.word(), a.page.word())
			}
		}
	}
}

// AND THE PLAIN ARROWS ARE NOT THE TAB BAR EITHER. `←` and `→` mean what they
// mean on the place a person is standing on — the fold ladder, the walk across
// home's columns, the caret's step inside a box, and `→` opening the row's
// verbs — and never "the next room". The owner tried them for the tab bar,
// which is worth auditing rather than assuming: the answer is that they must
// leave the place exactly where it was.
func TestThePlainArrowsNeverSwitchPlaces(t *testing.T) {
	a := placeApp(t)
	for _, id := range []page{pageHome, pageSpend, pageSearch, pageSettings} {
		a.showPage(id)
		if a.page != id {
			continue
		}
		for _, k := range []string{"left", "right", "up", "down"} {
			drive(t, a, key(k))
			if a.page != id {
				t.Fatalf("%s moved from the %s place to %q", k, id.word(), a.page.word())
			}
		}
	}
}

// ── the launch home ─────────────────────────────────────────────────────────

// THE CONVERSATION YOU ARE IN WEARS `here`, ON THE FIRST HOME AS WELL AS EVERY
// LATER ONE.
//
// The greeting builds home before bubbletea exists ([app.landHome]) and did it
// with a second struct literal of its own, which had never gained the field
// that says which session this window is holding. So the very first home a
// person sees marked their own conversation `another window` — the flock this
// process is holding, read as somebody else's — and offered a door that
// refuses.
func TestTheLaunchHomeMarksTheConversationYouAreIn(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker",
		lab.workspace("alpha"), now.Add(-2*time.Minute))
	lab.session("-beta", "bbbb000000000001", "pricing research",
		lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.launch(mine, true)
	if !a.home.open {
		t.Fatal("the greeting did not land on home")
	}
	if a.home.here == "" {
		t.Fatal("the launch home does not know which conversation this window is holding")
	}
	found := false
	for _, line := range a.home.lines {
		if line.sw != nil && line.sw.row != nil && line.sw.row.here {
			found = true
		}
	}
	if !found {
		t.Fatalf("no row of the launch home wears `here`:\n%s", homeText(a))
	}
}

// AND THE TWO HOMES ARE ONE HOME. A second literal is a second set of fields to
// forget, which is exactly how the one above went missing.
func TestTheLaunchHomeAndTheOpenedHomeAreBuiltTheSameWay(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker",
		lab.workspace("alpha"), now.Add(-2*time.Minute))
	lab.session("-beta", "bbbb000000000001", "pricing research",
		lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.launch(mine, true)
	launched := a.home
	a.openHome()
	opened := a.home
	if launched.here != opened.here || launched.bucket != opened.bucket {
		t.Fatalf("the launch home stands somewhere else: here %q/%q bucket %q/%q",
			launched.here, opened.here, launched.bucket, opened.bucket)
	}
	// AND EVERY MAP IT WRITES TO EXISTS. A nil map on this view is a write away
	// from taking the whole surface down.
	for name, ok := range map[string]bool{
		"last": launched.last != nil, "news": launched.news != nil,
		"deliverables": launched.deliverables != nil,
		"expanded":     launched.expanded != nil, "itemsOpen": launched.itemsOpen != nil,
	} {
		if !ok {
			t.Fatalf("the launch home has no %s map", name)
		}
	}
}
