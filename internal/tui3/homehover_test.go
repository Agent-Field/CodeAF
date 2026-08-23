package tui3

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// THE POINTER PREVIEWS AND THE CURSOR SELECTS.
//
// These are the laws of the card's subject. Home's right column used to read
// the cursor and nothing else, so a person moving the pointer down the left
// column watched rows light up one after another beside a card that never
// changed — the screen answering a gesture with a highlight and no content.
// What the pointer is over is now what the card is about, and the cursor is
// left exactly where it was put.

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

// Hovering a conversation shows THAT conversation on the right, and the cursor
// does not move an inch: the row it is on keeps the selected look it had, and
// the hovered row takes the hover look on top of it.
func TestHoveringARowPreviewsItOnTheRight(t *testing.T) {
	a, _, _ := homeTierLab(t)
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
	a, _, _ := homeTierLab(t)
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

// A HEADING IS NOT A THING TO PREVIEW. The rows the cursor may not stop on have
// no card of their own, and a pointer resting on one leaves the card where the
// keyboard is rather than emptying it.
func TestHoveringAHeadingLeavesTheCardOnTheCursor(t *testing.T) {
	a, _, _ := homeTierLab(t)
	at := homeLineOfKind(t, a, homeHeading, "alpha")
	a.homeHover(4, homeLineY(t, a, at))
	if a.home.hover != -1 {
		t.Fatalf("a heading was taken as a hover (line %d)", a.home.hover)
	}
	if title := homeCardTitle(t, a); title != "Zeta Chat" {
		t.Fatalf("hovering a heading changed the card to %q", title)
	}
}

// A FOLDED PROJECT'S LINE PREVIEWS THE PROJECT, exactly as the cursor on it
// does: the pointer reaches every row the cursor can stop on, and the card it
// gets is the card that row has.
func TestHoveringAFoldedProjectPreviewsTheProjectCard(t *testing.T) {
	a, _, _ := homeTierLab(t)
	at := homeLineOfKind(t, a, homeProject, "gamma")
	a.homeHover(4, homeLineY(t, a, at))

	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindProject {
		t.Fatalf("the hovered project line answers subject kind %v, want a project", subject.kind)
	}
	if subject.project != "gamma" || subject.dir != "/tmp/gamma" {
		t.Fatalf("the previewed subject is %q at %q, want gamma at /tmp/gamma", subject.project, subject.dir)
	}
	if card := homeCard(t, a); !strings.Contains(card, "/tmp/gamma") {
		t.Fatalf("the card beside a hovered project is not the project's:\n%s", card)
	}
}

// `→` ACTS ON THE CARD A PERSON IS LOOKING AT. The right column has no cursor
// of its own, so the key that opens every fold on the card has to mean the card
// that is drawn — the previewed row — or the screen would answer one row and
// the keyboard another.
func TestMoreOpensTheFoldsOfThePreviewedCard(t *testing.T) {
	a, _, _ := homeTierLab(t)
	bands := homeBandsFor(bandKindSession)
	if len(bands) == 0 {
		t.Fatal("no band is registered for a conversation, so this proves nothing")
	}
	band := bands[0].name

	at := homeLineOfKind(t, a, homeSession, "alpha")
	a.homeHover(4, homeLineY(t, a, at))
	hovered, ok := a.homeSubject()
	if !ok {
		t.Fatal("the hovered row has no subject")
	}
	// The cursor's own subject, read with the pointer lifted for one line and
	// then put back exactly where it was.
	a.home.hover = -1
	pointed, ok := a.homeSubject()
	if !ok {
		t.Fatal("the cursor's row has no subject")
	}
	a.home.hover = at

	a.homeKey(key("right"))
	if a.bandFolded(band, hovered) {
		t.Fatalf("→ left the %s band of the previewed card folded", band)
	}
	if !a.bandFolded(band, pointed) {
		t.Fatalf("→ acted on the cursor's card instead of the one on the screen")
	}
}

// THE CARD'S READING IS THE CARD'S. The repository band takes one bounded
// reading when a card arrives, and a card that arrived under the pointer must
// read the workspace of the row the pointer is on — hovering a conversation in
// another project shows THAT project's branch, or the band would be reporting
// on a directory nothing on the screen is about.
func TestHoveringAnotherProjectsRowReadsThatProjectsRepository(t *testing.T) {
	a, _, _ := homeTierLab(t)
	var asked []string
	old := homeGitStatus
	homeGitStatus = func(_ context.Context, workspace string) ([]byte, error) {
		asked = append(asked, workspace)
		return []byte("# branch.head feature/" + filepath.Base(workspace) + "\n"), nil
	}
	t.Cleanup(func() { homeGitStatus = old })

	at := homeLineOfKind(t, a, homeSession, "alpha")
	a.homeHover(4, homeLineY(t, a, at))
	if len(asked) != 1 || asked[0] != "/tmp/alpha" {
		t.Fatalf("hovering read %v, want one reading of /tmp/alpha", asked)
	}
	width, _ := a.size()
	_, right := homeColumns(width)
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
