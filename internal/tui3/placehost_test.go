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
// So these tests say the same thing five times, once per place, and each one
// asserts BOTH halves: the sentence is on the frame, and the wrong machine's
// rows are not.

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
// work on it, under a surface whose session is on `box`.
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
		{pageTasks, tasksRemoteWord},
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

// The standing place is the one that keeps half its rows: what stands on THIS
// conversation crosses the wire and is the far machine's own answer. What it
// loses is the walk of this process's projects, and it says so on the note line
// rather than over the whole body.
func TestTheStandingPlaceOverHostSaysWhichHalfIsMissing(t *testing.T) {
	a := hostedPlaceLab(t)
	a.showPage(pageStanding)
	if !a.at(pageStanding) {
		t.Fatal("standing did not open over --host")
	}
	text := placeText(a)
	if !strings.Contains(text, standingRemoteWord) {
		t.Fatalf("standing did not say which half is missing:\n%s", text)
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

// THE WORLD IS THE ONE SEAM, and it is empty over a connection — which is what
// every place above is downstream of. A gate per place would be five gates to
// forget; this is the walk itself declining to answer for the wrong machine.
func TestTheWorldIsEmptyOverHost(t *testing.T) {
	a := hostedPlaceLab(t)
	if world := a.readWorld(); len(world.Projects) > 0 {
		t.Fatalf("the world over --host listed %d of this machine's projects", len(world.Projects))
	}
	a.host = ""
	if world := a.readWorld(); len(world.Projects) == 0 {
		t.Fatal("the world on a local session listed nothing")
	}
}

// A tab may not wear a number over a place that is saying it cannot see the
// machine, and a look at another machine's place may not clear the badge a local
// window on THIS one is measuring its own news against.
func TestNoTabWearsANumberOverHost(t *testing.T) {
	a := hostedPlaceLab(t)
	before := session.LastLookAt(a.placesRoot(), pageTasks.word())
	a.showPage(pageTasks)
	a.refreshPlaceCounts(time.Now())
	for _, id := range pages() {
		if n := a.places.ChangedIn(id.word()); n != 0 {
			t.Fatalf("the %s tab wore %d over --host", id.word(), n)
		}
	}
	a.leavePage(pageTasks)
	if got := session.LastLookAt(a.placesRoot(), pageTasks.word()); !got.Equal(before) {
		t.Fatal("a look at another machine's tasks moved this machine's own stamp")
	}
}
