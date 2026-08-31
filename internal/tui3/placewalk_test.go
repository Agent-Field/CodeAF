package tui3

import (
	"strings"
	"testing"
	"time"
)

// WALKING BETWEEN THE PLACES.
//
// The owner's report against the real binary: "in home left right does not seem
// to move tabs, only shift does". The cause was not the arrows — it was `tab`
// walking into the tasks place, being refused because this machine had run no
// work, and being put straight back on home. Every press did that, so the
// circle had one member and `shift+tab` (which lands on settings, and settings
// always opened) was the only key that appeared to work.
//
// THE WALK ANSWERED IT BY STEPPING PAST A SHUT ROOM. Then the refusals went
// (pages.go's [app.showPage]), and with them the idea of a shut room — so the
// walk is `tab` and nothing else again, and what these tests pin is that the
// circle is the whole set and that every step of it lands.

// TAB WALKS THE WHOLE CIRCLE AND EVERY ROOM ON IT OPENS. On a surface with no
// brain, no store and nothing run, all seven still take the frame.
func TestTabWalksEveryPlaceAndEachOneOpens(t *testing.T) {
	a := placeApp(t)
	seen := map[page]bool{a.page: true}
	for range len(pages()) {
		was := a.page
		drive(t, a, key("tab"))
		if a.page == was {
			t.Fatalf("tab from the %s place stayed where it was", was.word())
		}
		if !a.pageShowing() {
			t.Fatalf("tab landed the router on %q with nothing on the frame", a.page.word())
		}
		seen[a.page] = true
	}
	if len(seen) != len(pages()) {
		t.Fatalf("tab visited %d of the %d places: %v", len(seen), len(pages()), seen)
	}
	if a.page != pageHome {
		t.Fatalf("the circle came back to %q rather than home", a.page.word())
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

// NO PLACE REFUSES, AND MEMORY IS THE ONE THAT REFUSED LAST. This surface has
// no brain and no store, which used to be the state that shut the room and wrote
// `memory is off` into a transcript nobody could see under the screen drawn over
// it. It opens now, on the three sentences that say what memory is for, with
// that same line said once under them where it can be read.
func TestThePlaceWithNoStoreOpensAndSaysSoOnTheFrame(t *testing.T) {
	a := placeApp(t)
	if a.memoryReady() {
		t.Skip("this surface has memories to show, so there is nothing to say about a missing store")
	}
	drive(t, a, key("alt+4"))
	if a.page != pageMemory || !a.at(pageMemory) {
		t.Fatalf("alt+4 left the router on %q", a.page.word())
	}
	screen := placeFrameText(a)
	if !strings.Contains(screen, memoryTeaching[0]) {
		t.Fatalf("the place with no store teaches nothing:\n%s", screen)
	}
	if !strings.Contains(screen, memoryOffNote) {
		t.Fatalf("the place with no store does not say so:\n%s", screen)
	}
}

// AND THE TASKS PLACE OPENS EMPTY FROM EVERY DOOR. A person pressing `alt+2` or
// a tab word is WALKING, and a room that bounced them back would be the bar
// pointing at a place they are not allowed to stand in — so it opens on whatever
// the reading holds, and an empty one spends the frame saying what the place is
// for. The command reaches the same page: /history on a machine that has run
// nothing used to answer with one line and no screen, which on a fresh machine
// was every door onto it.
func TestTheTasksPlaceOpensOnAMachineThatHasRunNothing(t *testing.T) {
	a := placeApp(t)
	if len(a.takeTaskReading().reading.items) > 0 {
		t.Skip("this surface has work to show, so the empty place is not what is drawn")
	}
	drive(t, a, key("alt+2"))
	if a.page != pageTasks || !a.at(pageTasks) {
		t.Fatalf("alt+2 over an empty machine left the router on %q", a.page.word())
	}
	screen := placeFrameText(a)
	if !strings.Contains(screen, "enter opens") {
		t.Fatalf("the empty tasks place teaches nothing:\n%s", screen)
	}
	// AND THE LAST LINE OF THAT LESSON IS WHAT THE COMMAND USED TO SAY INSTEAD OF
	// OPENING, moved onto the page it is about.
	if !strings.Contains(screen, taskSheetEmpty) {
		t.Fatalf("the empty tasks place does not say what to do about it:\n%s", screen)
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
	if !a.at(pageHome) {
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
	//
	// THE DELIVERABLES INDEX IS NOT ONE OF THEM ANY MORE. It stopped being a map
	// per row and became ONE reading for the whole screen, assigned whole and
	// never written into (homeband_deliverables.go), so an unread one is a read
	// waiting to happen rather than a panic waiting to happen.
	for name, ok := range map[string]bool{
		"last": launched.last != nil, "news": launched.news != nil,
		"expanded": launched.expanded != nil, "itemsOpen": launched.itemsOpen != nil,
	} {
		if !ok {
			t.Fatalf("the launch home has no %s map", name)
		}
	}
}
