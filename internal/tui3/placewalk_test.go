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
//
// THE ROOM IT IS ASKED OF IS MEMORY AND NO LONGER TASKS. This test was written
// against the tasks place — which is where the owner met the fault — and the
// tasks place does not refuse any more: the tab bar's door opens it empty and
// spends the frame teaching (place_tasks.go's [app.showTaskPlace], pinned below
// by [TestTheTabBarOpensTheTasksPlaceOnAMachineThatHasRunNothing]). The LAW is
// untouched, so it is asked of a room that still shuts: this surface has no
// brain and no store, so the memory place refuses ([app.memoryReady]).
func TestTabWalksPastAPlaceThatCannotOpen(t *testing.T) {
	a := placeApp(t)
	if a.memoryReady() {
		t.Skip("this surface has memories to show, so the memory place does not refuse")
	}
	drive(t, a, key("tab"))
	if a.page == pageHome {
		t.Fatal("tab from home stayed on home")
	}
	if a.page == pageMemory {
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

// A PLACE THAT REFUSES SAYS WHY, WHERE THE PERSON IS STANDING. `alt+n` on a
// machine that has nothing in that room used to write its sentence into the
// transcript under a screen drawn over the top of it, so the key read as broken.
//
// IT IS ASKED OF MEMORY for the reason above: tasks stopped refusing, and memory
// is the room this surface genuinely cannot open (place_memory.go's
// [app.openMemory] says so through pages.go's [app.refusePage]).
func TestAPlaceThatRefusesSaysSoOnTheFrame(t *testing.T) {
	a := placeApp(t)
	if a.memoryReady() {
		t.Skip("this surface has memories to show, so the memory place does not refuse")
	}
	drive(t, a, key("alt+4"))
	if a.page != pageHome {
		t.Fatalf("the refused place took the band: %q", a.page.word())
	}
	if !strings.Contains(placeFrameText(a), "memory is off") {
		t.Fatalf("the refusal is nowhere on the frame:\n%s", placeFrameText(a))
	}
}

// AND THE TASKS PLACE IS THE ONE THAT STOPPED REFUSING, which is why the two
// tests above had to move rooms. A person pressing `alt+2` or a tab word is
// WALKING, and a room that bounced them back would be the bar pointing at a
// place they are not allowed to stand in — so it opens on whatever the reading
// holds, and an empty one spends the frame saying what the place is for. The
// COMMAND still refuses ([app.openTaskPage] and [taskSheetEmpty]), because a
// command typed on purpose that answers with silence reads as one that broke.
func TestTheTabBarOpensTheTasksPlaceOnAMachineThatHasRunNothing(t *testing.T) {
	a := placeApp(t)
	if len(a.takeTaskReading().reading.items) > 0 {
		t.Skip("this surface has work to show, so the empty place is not what is drawn")
	}
	drive(t, a, key("alt+2"))
	if a.page != pageTasks || !a.taskSheet.open {
		t.Fatalf("alt+2 over an empty machine left the router on %q", a.page.word())
	}
	screen := placeFrameText(a)
	if !strings.Contains(screen, "enter opens") {
		t.Fatalf("the empty tasks place teaches nothing:\n%s", screen)
	}
	if strings.Contains(screen, taskSheetEmpty) {
		t.Fatalf("the walk was answered with the command's refusal:\n%s", screen)
	}
	// AND THE WALK GOES THROUGH IT rather than past it, because it opens.
	if !a.pageReady(pageTasks) {
		t.Fatal("the walk still treats the tasks place as a room it cannot get into")
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
