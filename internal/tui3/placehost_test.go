package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE PLACES OVER --host ──────────────────────────────────────────────────
//
// A place is a listing of one machine's disk, and over a connection that machine
// is the one the SESSION runs on. Until this file existed only home said so: the
// other five walked ~/.aforge/v3 under this process — the laptop's — and drew
// what they found under a conversation living somewhere else. The tasks place
// was the worst of them, because it drew a count and a total in dollars: "work
// aforge ran on its own. 8, $22.54 of it." was the laptop's eight tasks and the
// laptop's money, on a session on a server.
//
// THREE OF THE SEVEN HAVE CROSSED THE WIRE SINCE and are tested elsewhere: home
// and tasks read the far machine's world (home_test.go, tasks_host_test.go), and
// standing always read the far machine's items. What is left here is the three
// that have not, and each one asserts BOTH halves: the sentence is on the frame,
// and the wrong machine's rows are not.

// placeText is whatever place is standing, drawn and stripped of its paint.
func placeText(a *app) string {
	width, height := a.size()
	lines, _, _, _, ok := a.placeFrameNow(width, height)
	if !ok {
		return ""
	}
	return ansi.Strip(strings.Join(lines, "\n"))
}

// hostedPlaceLab is a machine with one conversation and one finished piece of
// work on it, under a surface whose session is on `box` AND WHOSE DOOR FORGOT TO
// WIRE THE WORLD. It is the shape of the fault rather than the shape of the
// product: a surface in this state must draw nothing, never this disk.
func hostedPlaceLab(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now)
	lab.task("-alpha", session.TaskIndexEntry{
		ID: "1", Name: "trimming", Label: "trimming the index", Title: "trimming the index",
		Status: string(session.TaskDone), Cost: 22.54, SessionID: "aaaa000000000001",
		EndedAt: now.Add(-time.Minute),
	})
	a := lab.app("")
	a.host = "box"
	return a
}

// The five places that read this process's disk each say so, in their own words,
// where their rows would have been — and none of them draws a row.
func TestEveryPlaceOverHostSaysWhoseMachineItIsAbout(t *testing.T) {
	for _, want := range []struct {
		id   page
		line string
	}{
		{pageMemory, memoryRemoteWord},
		{pageSpend, spendRemoteWord},
		{pageSearch, searchRemoteWord},
	} {
		t.Run(want.id.word(), func(t *testing.T) {
			a := hostedPlaceLab(t)
			a.showPage(want.id)
			if !a.at(want.id) {
				t.Fatalf("%s did not open over --host", want.id.word())
			}
			text := placeText(a)
			if !strings.Contains(text, want.line) {
				t.Fatalf("%s did not say whose machine it is about:\n%s", want.id.word(), text)
			}
			if strings.Contains(text, "trimming the index") || strings.Contains(text, "22.54") {
				t.Fatalf("%s drew this machine's work under a session on another:\n%s",
					want.id.word(), text)
			}
		})
	}
}

// A hosted surface whose door wired no world seam reads NOTHING. It is the one
// state that can silently become the old fault — a fall-through to this
// process's own disk — so it is pinned where it can be seen.
func TestAHostedSurfaceWithNoWorldSeamReadsNothing(t *testing.T) {
	a := hostedPlaceLab(t)
	if world, known := a.worldOf(); known || len(world.Projects) > 0 {
		t.Fatalf("a hosted surface with no seam read this machine's disk: known=%v, %d projects",
			known, len(world.Projects))
	}
	a.showPage(pageTasks)
	if text := placeText(a); strings.Contains(text, "trimming the index") {
		t.Fatalf("the tasks place drew this machine's work:\n%s", text)
	}
}

// The two places whose reading crosses the wire draw no refusal at all. It is
// asserted as the ABSENCE of a sentence because that is what the frame reads:
// [place.remote] is the one gate between a place and its rows, and a place that
// went on answering it after learning to cross would draw a dim line over a list
// it could perfectly well have shown.
func TestThePlacesThatCrossDrawNoRefusal(t *testing.T) {
	a := hostedPlaceLab(t)
	for _, id := range []page{pageHome, pageTasks, pageStanding} {
		if line := placeFor(id).remote(a); line != "" {
			t.Fatalf("%s still refuses over --host: %q", id.word(), line)
		}
	}
}

// And the tab bar says whose machine all of this is about — on every place, and
// on none of them at home.
func TestTheTabBarNamesTheMachineOverHostAndNeverAtHome(t *testing.T) {
	a := hostedPlaceLab(t)
	a.showPage(pageTasks)
	if text := placeText(a); !strings.Contains(text, placeMachineLead+"box") {
		t.Fatalf("the tab bar did not name the machine over --host:\n%s", text)
	}
	a.host = ""
	if text := placeText(a); strings.Contains(text, placeMachineLead+"box") {
		t.Fatalf("the tab bar named a machine on a local session:\n%s", text)
	}
}

// The tasks place over --host draws THE FAR MACHINE'S work and not one row of
// this one's. It is the owner's own report, turned into a test: they attached to
// spark, pressed the tasks tab, and read "work aforge ran on its own. 8, $22.54
// of it." — the laptop's eight tasks and the laptop's money.
func TestTheTasksPlaceOverHostDrawsTheFarMachinesWork(t *testing.T) {
	a := hostedPlaceLab(t)
	now := time.Now()
	a.world = func() (session.World, bool) {
		return session.World{
			Read: now,
			Projects: []session.Project{{
				Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
				Sessions: []session.SessionRow{{
					ID: "bbbb000000000002", Title: "rewriting the importer",
					Project: "api", ProjectDir: "/srv/code/api", Workspace: "/srv/code/api",
					At: now, Created: now,
					Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{{
						ID: "9", Name: "widening", Label: "widening the pipe",
						Title: "widening the pipe", Status: string(session.TaskDone),
						Cost: 3.10, SessionID: "bbbb000000000002", EndedAt: now.Add(-time.Minute),
					}}},
				}},
			}},
		}, true
	}
	a.showPage(pageTasks)
	text := placeText(a)
	if !strings.Contains(text, "widening the pipe") {
		t.Fatalf("the tasks place did not draw the far machine's work:\n%s", text)
	}
	if strings.Contains(text, "trimming the index") || strings.Contains(text, "22.54") {
		t.Fatalf("the tasks place drew this machine's work under a session on another:\n%s", text)
	}
}

// Settings is NOT one of them, and that is the point of the default being "".
// Every row on it is either this surface's own or is read from the far machine's
// profile, and it already says so as it opens ([settingsRemoteWord]) — a place
// that drew one dim line instead would have taken working rows away.
func TestTheSettingsPlaceIsNotGatedOverHost(t *testing.T) {
	a := hostedPlaceLab(t)
	if line := placeFor(pageSettings).remote(a); line != "" {
		t.Fatalf("settings drew a refusal over --host: %q", line)
	}
}

// THE WORLD IS THE ONE SEAM, and over a connection it is the seam's answer
// rather than this process's disk — which is what every place above is
// downstream of. A gate per place would be five gates to forget.
func TestTheWorldOverHostIsTheSeamsAndNeverThisDisk(t *testing.T) {
	a := hostedPlaceLab(t)
	// With no seam wired, a hosted surface reads its own disk, which is exactly
	// the fault: the guard is the door, and this is what the door must fill.
	a.world = func() (session.World, bool) {
		return session.World{Read: time.Now(), Projects: []session.Project{{
			Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
		}}}, true
	}
	world := a.readWorld()
	if len(world.Projects) != 1 || world.Projects[0].Name != "api" {
		t.Fatalf("the world over --host was not the seam's: %+v", world.Projects)
	}
	a.world = nil
	a.host = ""
	if world := a.readWorld(); len(world.Projects) == 0 {
		t.Fatal("the world on a local session listed nothing")
	}
}

// A look at another machine's place may not clear the badge a local window on
// THIS one is measuring its own news against: the stamps are kept per machine,
// on this disk, under [app.looksRoot].
func TestALookAtAnotherMachineLeavesThisOnesStampsAlone(t *testing.T) {
	a := hostedPlaceLab(t)
	before := session.LastLookAt(a.placesRoot(), pageTasks.word())
	a.showPage(pageTasks)
	a.leavePage(pageTasks)
	if got := session.LastLookAt(a.placesRoot(), pageTasks.word()); !got.Equal(before) {
		t.Fatal("a look at another machine's tasks moved this machine's own stamp")
	}
	// AND IT IS WRITTEN SOMEWHERE — a stamp that went nowhere would keep this
	// test green while losing every remote place its origin.
	if session.LastLookAt(a.looksRoot(), pageTasks.word()).IsZero() {
		t.Fatal("the look at the far machine's tasks was never recorded")
	}
	if a.looksRoot() == a.placesRoot() {
		t.Fatal("a hosted surface stamps the same root a local one does")
	}
}
