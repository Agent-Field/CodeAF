package tui3

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE POINTER PREVIEWS AND THE CURSOR SELECTS.
//
// These are the laws of the card's subject. Home's right column used to read
// the cursor and nothing else, so a person moving the pointer down the left
// column watched rows light up one after another beside a card that never
// changed — the screen answering a gesture with a highlight and no content.
// What the pointer is over is now what the card is about, and the cursor is
// left exactly where it was put.
//
// THE HOVER MAP IS UNTOUCHED BY THE SWITCHER AND THE ROWS UNDER IT ARE NOT.
// The resting list is one flat ranked reading now (switcher.go), so a pointer
// resting on a project line previews nothing for the plain reason that there
// are no project lines at rest — the rows a pointer can reach are a
// conversation, a watch, a `since you left` door and the fold, and the line it
// cannot reach is the reading's own claim rather than a heading. And there is
// no card at all below [homeCardMin] (homebridge.go), so every test whose
// subject IS the card is drawn on a frame past that floor.

// homeCard is the right column as plain text: what the card is about right now.
func homeCard(t *testing.T, a *app) string {
	t.Helper()
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatal("this frame has no right column, so it can prove nothing about the card")
	}
	return ansi.Strip(strings.Join(a.homeDetail(right, 12, a.pal), "\n"))
}

// homeCardTitle is its first line, which is the one line no frame may drop.
func homeCardTitle(t *testing.T, a *app) string {
	t.Helper()
	card := homeCard(t, a)
	title, _, _ := strings.Cut(card, "\n")
	return strings.TrimSpace(title)
}

// homeLineOfKind finds the column line one project drew of a given kind — the
// row a pointer is aimed at in the tests below.
func homeLineOfKind(t *testing.T, a *app, kind homeRowKind, project string) int {
	t.Helper()
	for at, line := range a.home.lines {
		if line.kind == kind && line.project == project {
			return at
		}
	}
	t.Fatalf("no %v line for %q on the column:\n%s", kind, project, homeText(a))
	return -1
}

// homeLeftText is one line of the LEFT column exactly as it is painted, colour
// and all. The card is drawn on the same screen rows, so a whole frame line
// cannot say whether the cursor's row still wears its selected look; this can.
func homeLeftText(a *app, at int) string {
	width, _ := a.size()
	left, _ := homeColumns(width)
	return a.homeLine(a.home.lines[at], at, left, a.pal)
}

// hoverLab is the resting switcher with one of everything a pointer can land on
// — a quiet conversation in each of three projects, one stopped on a question,
// and one watch that needs somebody — on a frame past [homeCardMin].
//
// THE FRAME IS THE FLOOR AND NOT A ROUND NUMBER. There is no card below that
// width at all (homebridge.go), and a test about what the card is ABOUT drawn on
// a frame that has no card proves nothing at all — which is exactly what the
// hundred-column lab these tests used to open on became the day the everyday
// card tier went.
//
// THE WORKSPACES REALLY EXIST, because the card's place line says `folder gone`
// in place of a branch for a directory that is not there ([app.homeCardPlace]),
// and a repository reading is the subject of the last test in this file.
func hoverLab(t *testing.T) (*app, *homeLab, string) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	alpha, beta, zeta := lab.workspace("alpha"), lab.workspace("beta"), lab.workspace("zeta")
	lab.session("-alpha", "aaaa000000000001", "alpha chat", alpha, now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "beta chat", beta, now.Add(-2*time.Hour))
	// A conversation stopped on a question, so that the reading draws its own
	// claim line over the ranked rows — the one line of the resting list a cursor
	// may not stop on, which is what a project heading used to be here.
	lab.session("-beta", "bbbb000000000002", "beta asking", beta, now.Add(-30*time.Minute))
	lab.asks("-beta", "bbbb000000000002", "run the sweep?", now)
	mine := lab.session("-zeta", "cccc000000000001", "zeta chat", zeta, now.Add(-3*time.Hour))

	a := lab.app(mine)
	// AND ONE WATCH THAT NEEDS SOMEBODY, which is the other kind of row a cursor
	// stops on and the other kind of card the right column draws. It needs a
	// person because that is what earns a standing item a row on the resting list
	// at all now ([readSwitcher]); the ones merely waiting for their time are the
	// standing place's business.
	a.stands.Items = func(workspace string) []standing.Item {
		if workspace != alpha {
			return nil
		}
		return []standing.Item{{
			ID: "w1", Words: "watch the repo", Status: standing.StatusActive,
			Workspace: alpha, NeedsPerson: "should I send the digest?", Updated: now.Add(-time.Hour),
		}}
	}
	a.width, a.height = homeCardMin, 40
	openHomeOn(a, mine)
	return a, lab, mine
}

// Hovering a conversation shows THAT conversation on the right, and the cursor
// does not move an inch: the row it is on keeps the selected look it had, and
// the hovered row takes the hover look on top of it.
func TestHoveringARowPreviewsItOnTheRight(t *testing.T) {
	a, _, _ := hoverLab(t)
	cursor := a.home.cursor
	if title := homeCardTitle(t, a); title != "Zeta Chat" {
		t.Fatalf("the card does not open on the window's own conversation: %q", title)
	}
	selected := homeLeftText(a, cursor)

	at := homeLineOfKind(t, a, homeSession, "alpha")
	before := homeLeftText(a, at)
	a.homeHover(4, homeLineY(t, a, at))

	if title := homeCardTitle(t, a); title != "Alpha Chat" {
		t.Fatalf("hovering a row did not move the card to it: the card says %q", title)
	}
	if a.home.cursor != cursor {
		t.Fatalf("the pointer moved the cursor from %d to %d", cursor, a.home.cursor)
	}
	if now := homeLeftText(a, cursor); now != selected {
		t.Fatalf("the cursor's row lost its selected look while another row was hovered:\n%q\n%q", selected, now)
	}
	if now := homeLeftText(a, at); now == before {
		t.Fatalf("the hovered row is painted exactly as it was, so nothing on the left says the pointer is there: %q", now)
	}
}

// AND THE CARD COMES BACK THE MOMENT THE POINTER LEAVES. The right column is
// the pane's own ground: a pointer resting there is not pointing at any row of
// the list, so the card returns to the cursor's conversation rather than being
// held on whatever row shares that screen line.
func TestThePointerLeavingTheColumnGivesTheCardBackToTheCursor(t *testing.T) {
	a, _, _ := hoverLab(t)
	at := homeLineOfKind(t, a, homeSession, "alpha")
	y := homeLineY(t, a, at)
	a.homeHover(4, y)
	if title := homeCardTitle(t, a); title != "Alpha Chat" {
		t.Fatalf("hovering a row did not move the card to it: the card says %q", title)
	}

	width, _ := a.size()
	left, _ := homeColumns(width)
	a.homeHover(left+homeGutter+1, y)
	if a.home.hover != -1 {
		t.Fatalf("a pointer in the right column is still hovering column line %d", a.home.hover)
	}
	if title := homeCardTitle(t, a); title != "Zeta Chat" {
		t.Fatalf("the card did not go back to the cursor when the pointer left the column: %q", title)
	}
}

// A LINE NO CURSOR MAY STOP ON IS NOT A THING TO PREVIEW, and a pointer resting
// on one leaves the card where the keyboard is rather than emptying it.
//
// THE LINE USED TO BE A PROJECT HEADING AND IS NOW THE READING'S OWN CLAIM —
// `4 chats · what wants you first` ([homeSwitchHead]). The law did not change
// with it: the rows the cursor is not allowed to rest on have no card of their
// own, whatever those rows happen to be, and the pointer must not blank the
// column by drifting across one on its way somewhere.
func TestHoveringALineNoCursorMayStopOnLeavesTheCardOnTheCursor(t *testing.T) {
	a, _, _ := hoverLab(t)
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeSwitchHead {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the reading drew no claim over its ranked rows:\n%s", homeText(a))
	}
	a.homeHover(4, homeLineY(t, a, at))
	if a.home.hover != -1 {
		t.Fatalf("a line no cursor may stop on was taken as a hover (line %d)", a.home.hover)
	}
	if title := homeCardTitle(t, a); title != "Zeta Chat" {
		t.Fatalf("hovering the list's own claim changed the card to %q", title)
	}
}

// EVERY ROW THE CURSOR CAN STOP ON THE POINTER CAN REACH, AND THE CARD IT GETS
// IS THE CARD THAT ROW HAS.
//
// This used to be said about a folded project's line, and there are no project
// lines at rest any more — the tiers and the `elsewhere` block went with the
// tree (switcher.go). The second kind of stop on the resting list is a WATCH,
// and its card is the standing item's own ([StandingItemCard], which this wave
// did not touch), so the law is pinned where it still has two kinds to be true
// of.
func TestHoveringAWatchPreviewsTheWatchsOwnCard(t *testing.T) {
	a, lab, _ := hoverLab(t)
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeItem {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the watch has no row on the column:\n%s", homeText(a))
	}
	a.homeHover(4, homeLineY(t, a, at))

	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindItem {
		t.Fatalf("the hovered watch answers subject kind %v, want an item", subject.kind)
	}
	alpha := lab.workspace("alpha")
	if subject.dir != alpha {
		t.Fatalf("the previewed subject is at %q, want the watch's own workspace %q", subject.dir, alpha)
	}
	card := homeCard(t, a)
	if !strings.Contains(card, "watch the repo") {
		t.Fatalf("the card beside a hovered watch is not the watch's:\n%s", card)
	}
	if strings.Contains(card, "Zeta Chat") {
		t.Fatalf("the card stayed on the cursor's conversation:\n%s", card)
	}
}

// `→` ACTS ON THE ROW A PERSON IS LOOKING AT. The right column has no cursor of
// its own, so a key that reaches past the list has to mean the row that is
// drawn — the previewed one — or the screen would answer one row and the
// keyboard another.
//
// THE KEY IT REACHES CHANGED AND THE LAW DID NOT. `→` used to open every fold on
// the card; it opens the row's VERB STRIP now, and only where the row has verbs
// (placekeys.go's [app.placeKey], verbstrip.go) — which on the resting list is
// every conversation and every watch, so the fold arm below it is no longer
// reachable from a row at all. What is pinned here is the half that survived
// both: the verbs are the PREVIEWED row's and never the cursor's.
func TestTheVerbKeyActsOnTheRowThePointerIsOn(t *testing.T) {
	a, _, _ := hoverLab(t)
	watch := -1
	for i, line := range a.home.lines {
		if line.kind == homeItem {
			watch = i
			break
		}
	}
	if watch < 0 {
		t.Fatalf("the watch has no row on the column:\n%s", homeText(a))
	}
	// The cursor is on this window's own conversation, whose verbs are a
	// conversation's; the pointer goes to the watch, whose verbs are an item's.
	// The two lists share no word, which is what lets the strip say which row it
	// was opened for.
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeSession {
		t.Fatalf("home did not open on a conversation, so the two rows cannot be told apart:\n%s", homeText(a))
	}
	a.homeHover(4, homeLineY(t, a, watch))

	a.homeKey(key("right"))
	if !a.strip.open {
		t.Fatalf("→ opened no verbs at all:\n%s", homeText(a))
	}
	var words []string
	for _, v := range a.strip.verbs {
		words = append(words, v.word)
	}
	joined := strings.Join(words, ", ")
	if !strings.Contains(joined, homeItemPauseWord) {
		t.Fatalf("→ did not offer the previewed watch's own verbs, it offered %q", joined)
	}
	if strings.Contains(joined, "put it away") {
		t.Fatalf("→ acted on the cursor's conversation instead of the row on the screen: %q", joined)
	}
}

// THE CARD'S READING IS THE CARD'S. The repository is read once, bounded, when a
// card arrives, and a card that arrived under the pointer must read the
// workspace of the row the pointer is on — hovering a conversation in another
// project shows THAT project's branch, or the reading would be reporting on a
// directory nothing on the screen is about.
//
// AND THE BRANCH IS ON THE CARD'S PLACE LINE NOW, not in a band of its own. The
// switcher's card is five things that ACT (place_home.go's [homeCardBands]), and
// a branch and a dirty count are facts about the address the place line already
// names — so `repo` was folded into it ([app.homeCardPlace]). The reading is
// still homeband_repo.go's and is still taken exactly once per workspace.
func TestHoveringAnotherProjectsRowReadsThatProjectsRepository(t *testing.T) {
	a, lab, _ := hoverLab(t)
	alpha := lab.workspace("alpha")
	var asked []string
	old := homeGitStatus
	homeGitStatus = func(_ context.Context, workspace string) ([]byte, error) {
		asked = append(asked, workspace)
		return []byte("# branch.head feature/" + filepath.Base(workspace) + "\n"), nil
	}
	t.Cleanup(func() { homeGitStatus = old })

	at := homeLineOfKind(t, a, homeSession, "alpha")
	// The reading is ASKED FOR on the hover and taken off the update loop
	// (homeband_repo.go), so the gesture is followed the way the program follows
	// it: the command runs and its answer is filed.
	settleHomeRepo(a, a.homeHover(4, homeLineY(t, a, at)))
	if len(asked) != 1 || asked[0] != alpha {
		t.Fatalf("hovering read %v, want one reading of %s", asked, alpha)
	}
	width, _ := a.size()
	_, right := homeColumns(width)
	// THE CARD IS DRAWN WIDE ENOUGH TO HOLD THE ADDRESS AND THE BRANCH BOTH. The
	// place line spends its cells on the address first (place_home.go's
	// [app.homeCardPlace]) and this lab's workspace is a forty-character
	// temporary directory, which a thirty-six-cell card cannot hold a branch
	// beside — dropping the clause there is the layout law, not a lost reading.
	if room := ansi.StringWidth(alpha) + 24; right < room {
		right = room
	}
	card := ansi.Strip(strings.Join(a.homeDetail(right, 40, a.pal), "\n"))
	if !strings.Contains(card, "feature/alpha") {
		t.Fatalf("the hovered project's branch is not on the card:\n%s", card)
	}
	// AND ONLY WHEN THE ANSWER CHANGED. A pointer wandering along the row it is
	// already on must not run git once per motion event.
	a.homeHover(6, homeLineY(t, a, at))
	if len(asked) != 1 {
		t.Fatalf("a motion event that changed nothing read the repository again: %v", asked)
	}
}
