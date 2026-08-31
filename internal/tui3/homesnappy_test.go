package tui3

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── WHAT A KEY AND A POINTER ON HOME MAY COST ───────────────────────────────
//
// These are PERF.md's laws about this screen, and they are COUNTS rather than
// stopwatches for the reason that file's doctrine gives: a count is a fact about
// the code and the same fact on a loaded laptop as on idle CI.
//
// WHAT THEY WERE WRITTEN AFTER. The owner reported that "when I hover or go up
// and down in home it's very laggy, I think it has something to do with git".
// Three separate things were true, and the guess was one of them:
//
//   - the deliverables index was read and parsed inside a draw (fixed one commit
//     earlier, pinned by [TestTheDeliverablesIndexIsReadOnceForTheWholeScreen]);
//   - `git status` ran ON THE UPDATE LOOP, on every arrow key and every hover
//     that moved the card, with a one-second ceiling on it;
//   - and every answered pointer motion built TWO whole home frames — one for
//     [app.homeHover] to hit-test against, and one for the repaint it asked for.
//
// Measured on a lab of eight repositories that answer `git status` in ten
// milliseconds each, which is what a real worktree costs (aforge's own is 7.7ms
// warm) and a tenth of what a cold or mounted one costs:
//
//	                    before          after
//	twenty arrivals     1.14ms each     0.048ms each   worst 10.4ms -> 0.10ms
//	sixty motions       0.57ms each     0.116ms each   worst 10.8ms -> 0.79ms
//	git on the loop     3 commands      0
//	frames per motion   2               1
//
// The commands still run — they are ASKED FOR and come back as messages
// (homecardread.go) — so nothing on the card is less true than it was; what
// changed is which goroutine waits for them.

// snappyLab is a home whose conversations live in eight different repositories,
// with a `git status` that counts itself and takes as long as a real one.
//
// EIGHT IS THE POINT. One repository would be answered once and cached
// ([homeRepoTTL]), and the fault being pinned here is a person walking rows that
// belong to different projects — which is what home's resting list IS.
func snappyLab(t *testing.T, delay time.Duration, calls *int) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := ""
	for p := 0; p < 8; p++ {
		name := fmt.Sprintf("proj%d", p)
		workspace := lab.workspace(name)
		for s := 0; s < 3; s++ {
			id := fmt.Sprintf("%04d%012d", p, s)
			transcript := lab.session("-"+name, id, fmt.Sprintf("%s chat %d", name, s),
				workspace, now.Add(-time.Duration(p*3+s)*time.Minute))
			if mine == "" {
				mine = transcript
			}
		}
	}
	old := homeGitStatus
	homeGitStatus = func(context.Context, string) ([]byte, error) {
		*calls++
		time.Sleep(delay)
		return []byte("# branch.head master\n"), nil
	}
	t.Cleanup(func() { homeGitStatus = old })
	a := lab.app(mine)
	a.width, a.height = 180, 45
	openHomeOn(a, mine)
	a.frame()
	return a
}

// A KEY THAT MOVES THE CURSOR ON HOME RUNS NO COMMAND.
//
// It ASKS for one, which is the other half of the law: the branch on the card's
// place line is still read, and still per workspace per [homeRepoTTL] — the
// reading simply happens somewhere a keystroke is not waiting for it.
func TestMovingTheCursorOnHomeRunsNoCommand(t *testing.T) {
	ran := 0
	a := snappyLab(t, 10*time.Millisecond, &ran)
	asked := 0
	for i := 0; i < 20; i++ {
		_, cmd := a.update(key("down"))
		if cmd != nil {
			asked++
		}
		a.dirty = true
		a.frame()
	}
	if ran != 0 {
		t.Fatalf("twenty arrow keys ran git %d times on the update loop", ran)
	}
	if asked == 0 {
		t.Fatal("twenty arrow keys asked for no reading at all, so the card can never learn a branch")
	}
}

// AND SO DOES A POINTER, which is the half the owner met first: a surface in
// AllMotion answers a sweep sixty times a second (coalesce.go), so one command
// per answered motion is sixty commands a second in front of the keys.
func TestAPointerMotionOnHomeRunsNoCommand(t *testing.T) {
	ran := 0
	a := snappyLab(t, 10*time.Millisecond, &ran)
	for i := 0; i < 60; i++ {
		// Each motion is an ARRIVAL rather than part of a sweep, so the fold
		// answers every one of them (coalesce.go) — which is the worst case this
		// law is about.
		a.ptr.moving = false
		a.update(tea.MouseMotionMsg{X: 4, Y: 3 + i%12})
		a.dirty = true
		a.frame()
	}
	if ran != 0 {
		t.Fatalf("sixty pointer motions ran git %d times on the update loop", ran)
	}
}

// A POINTER MOTION BUILDS ONE HOME FRAME, and the one it builds is the paint's.
//
// [app.homeHover] resolves the pointer against the frame that is ON THE SCREEN
// ([homeView.painted]) rather than building another to hit-test against. Two
// frames per motion is the whole draw done twice for a gesture that moved one
// integer, and on this screen the draw is the expensive half.
func TestAPointerMotionOnHomeBuildsOneFrame(t *testing.T) {
	ran := 0
	a := snappyLab(t, 0, &ran)
	const motions = 60
	before := a.homeFrames
	for i := 0; i < motions; i++ {
		a.ptr.moving = false
		a.update(tea.MouseMotionMsg{X: 4, Y: 3 + i%12})
		a.dirty = true
		a.frame()
	}
	if built := a.homeFrames - before; built != motions {
		t.Fatalf("sixty motions built %d home frames, wanted one apiece", built)
	}
}

// AND A KEY WALKS NO DIRECTORY. The world is the expensive reading on this
// screen — every project's index and every session's meta.json — and it belongs
// to the beat and to the open ([homeEvery]).
func TestMovingTheCursorOnHomeWalksNoDirectory(t *testing.T) {
	ran := 0
	a := snappyLab(t, 0, &ran)
	walks := 0
	world := a.home.world
	a.world = func() (session.World, bool) {
		walks++
		return world, true
	}
	for i := 0; i < 20; i++ {
		a.update(key("down"))
		a.dirty = true
		a.frame()
	}
	for i := 0; i < 20; i++ {
		a.ptr.moving = false
		a.update(tea.MouseMotionMsg{X: 4, Y: 3 + i%12})
		a.dirty = true
		a.frame()
	}
	if walks != 0 {
		t.Fatalf("forty keys and motions walked the world %d times", walks)
	}
}

// AND A BEAT WALKS IT ONCE. [app.refreshHome] used to ask for the reading and
// then ask AGAIN whether the reading was an answer, which is the whole cost of
// the walk spent to re-learn a boolean the first one already knew — on the
// update loop, three times a minute, for as long as home is open.
func TestAHomeBeatWalksTheWorldOnce(t *testing.T) {
	ran := 0
	a := snappyLab(t, 0, &ran)
	walks := 0
	world := a.home.world
	a.world = func() (session.World, bool) {
		walks++
		return world, true
	}
	a.refreshHome()
	if walks != 1 {
		t.Fatalf("one beat walked the world %d times, wanted one", walks)
	}
}

// AND THE CARD'S OWN TWO READINGS ARE ASKED FOR RATHER THAN TAKEN.
//
// [session.Peek] scans a whole transcript — measured at 33ms on a long one — and
// the inbox is a second file. Both were opened while the card was DRAWING, which
// is once per arrival, which is once per keystroke.
func TestTheCardsReadingsAreAskedForAndNotTaken(t *testing.T) {
	ran := 0
	a := snappyLab(t, 0, &ran)
	row := a.home.lines[len(a.home.lines)-1].row
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript != "" {
			row = line.row
			break
		}
	}
	// A BAND-ASSEMBLED CARD rather than the switcher's own, because the switcher's
	// five bands carry neither of these readings and this law is about the two
	// that do (place_home.go's [app.homeSwitchCard]).
	a.home.lines = []homeLine{{kind: homeSession, row: row, project: "proj0"}}
	a.home.cursor, a.home.hover = 0, -1
	a.home.last, a.home.news = map[string]session.Summary{}, map[string]homeNewsCache{}

	cmd := a.refreshHomeCard(time.Now())
	if cmd == nil {
		t.Fatal("a card arrived and asked for nothing")
	}
	if len(a.home.last) != 0 || len(a.home.news) != 0 {
		t.Fatalf("the arrival read the disk itself: %d journals, %d inboxes", len(a.home.last), len(a.home.news))
	}
	// AND THE ANSWERS DO COME BACK. Cheapness that costs a person the band is not
	// cheapness, so the same gesture is followed all the way to the state it
	// leaves behind.
	drive(t, a, runCmd(cmd)...)
	if len(a.home.last) != 1 {
		t.Fatalf("the journal reading never landed: %v", a.home.last)
	}
}

// ── THE TEST DOORS FOR THE TWO READINGS A CARD ASKS FOR ─────────────────────
//
// A band draws what it was GIVEN (homecardread.go), so a test whose subject is
// what a band draws takes the reading first, exactly as the arrival does and the
// update loop then files.

// takeHomeNews reads one subject's inbox and files it.
func takeHomeNews(a *app, subject bandSubject, now time.Time) {
	if cmd := a.askHomeNews(subject, now); cmd != nil {
		if msg, ok := cmd().(homeNewsMsg); ok {
			a.tookHomeNews(msg)
		}
	}
}

// takeHomeLeftOff peeks at one conversation's journal and files it.
func takeHomeLeftOff(a *app, transcript string) {
	if cmd := a.askHomeLeftOff(transcript); cmd != nil {
		if msg, ok := cmd().(homeLeftOffMsg); ok {
			a.tookHomeLeftOff(msg)
		}
	}
}
