package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// railLines is the side column on the next frame, plain, one string a frame
// row, so an index into it is a screen row.
func railLines(t *testing.T, a *app) []string {
	t.Helper()
	frame, _, _ := a.frame()
	cols := a.railWidth()
	if cols <= railGripCols {
		t.Fatalf("the side column is not a column: %d", cols)
	}
	// A ROW IS AS LONG AS WHAT IS ON IT, so a short row of the column is cut
	// from where the column starts to wherever it ends.
	var out []string
	for _, r := range strings.Split(ansi.Strip(frame), "\n") {
		cells := []rune(r)
		cell := ""
		if len(cells) > a.width-cols {
			cell = string(cells[a.width-cols : min(len(cells), a.width)])
		}
		out = append(out, strings.TrimRight(cell, " "))
	}
	return out
}

// railRowOf is the first frame row whose column cells hold want, -1 for none.
func railRowOf(rows []string, want string) int {
	for y, r := range rows {
		if strings.Contains(r, want) {
			return y
		}
	}
	return -1
}

// sidePainted is the side column's rows as the frame paints them, ANSI and
// all, so an assertion about ink reads the ink.
func sidePainted(a *app) string {
	return strings.Join(a.railRows(a.viewHeight()), "\n")
}

// sideClick presses and lets go at a screen cell, as a hand does.
func sideClick(t *testing.T, a *app, x, y int) {
	t.Helper()
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// sideRowDoor is the screen cell of the door on row key that does act kind,
// failing the test when the row is not drawn or has no such door.
func sideRowDoor(t *testing.T, a *app, key string, kind int) (int, int) {
	t.Helper()
	view, _ := a.railDrawnView(a.viewHeight())
	for i, line := range view {
		if line.side == nil || line.side.key != key {
			continue
		}
		for _, d := range line.side.doors {
			if d.act.kind == kind {
				return a.railLeft() + ansi.StringWidth(railSeam) + d.span.from, a.topHeight() + i
			}
		}
		t.Fatalf("row %q has no door that does act %d: %+v", key, kind, line.side.doors)
	}
	t.Fatalf("the column does not draw row %q", key)
	return 0, 0
}

// threadScenario is the owner's measured afternoon: an older note, then one
// question to three members as ONE entry, each member woken on it, three
// replies, two finishings and the manager woken by them, all linked the way
// the session writes them. It hands back the question's id.
func threadScenario(t *testing.T, a *app, harbor, price, rail string) string {
	t.Helper()
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: price, Text: "an older aside"})
	q, err := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager,
		To: teamstore.ToSeveral, Handles: []string{price, rail, "review"}, Text: "Please provide a brief status update on your part"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{price, rail, "review"} {
		trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindEvent, From: teamstore.FromManager, To: h, State: teamstore.StateRunning, Text: "woke @" + h, Answers: q})
	}
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "Status update: prices are scraped and cached", Answers: q},
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished", Answers: q},
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateRunning, Text: "woke ◆ (with @review)"},
		teamstore.Entry{Kind: teamstore.KindNote, From: "review", To: teamstore.ToManager, Text: "Status update: two findings, both minor", Answers: q},
		teamstore.Entry{Kind: teamstore.KindEvent, From: "review", To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished", Answers: q},
	)
	trafficReadNow(t, a)
	return q
}

// ONE QUESTION TO THREE MEMBERS IS ONE ROW OF WORK, at the top. The row names
// whom it went to and what it asked, says the state its answers leave it in
// and how many messages it holds, and folds its replies behind ▸. Laid open,
// each member is one `↳` line with its finishing folded in as ✓, a member
// woken with nothing said yet reads working…, and no wake or finishing is a
// line of its own. The chatter older than it is the one General row under it.
func TestTrafficThreadOneQuestionIsOneWorkRow(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q := threadScenario(t, a, harbor, price, rail)
	rows := railLines(t, a)
	joined := strings.Join(rows, "\n")
	head := railRowOf(rows, sideTrafficWord)
	top := railRowOf(rows, "@"+price+" +2  Pleas")
	if head < 0 || top != head+1 {
		t.Fatalf("the question's row is not straight under the header (%d, %d):\n%s", head, top, joined)
	}
	// AT THE COLUMN'S WIDEST THE COUNT GIVES WAY TO THE WORDS, and the hint
	// line says it.
	if !strings.Contains(rows[top], "running "+glyphShut) {
		t.Fatalf("the row does not say its state and its fold:\n%s", joined)
	}
	if hint := a.sideRowOf("thread/" + q).hint; !strings.Contains(hint, "running · 3 msgs") {
		t.Fatalf("the row's hint does not say its state and its messages: %q", hint)
	}
	general := railRowOf(rows, "General")
	if general != top+1 || !strings.Contains(rows[general], "1 msg "+glyphShut) {
		t.Fatalf("the older chatter is not one General row under the work:\n%s", joined)
	}
	for _, never := range []string{"woke", "finished", "Status update", "an older aside"} {
		if strings.Contains(joined, never) {
			t.Fatalf("%q is drawn on a folded column:\n%s", never, joined)
		}
	}

	x, y := sideRowDoor(t, a, "thread/"+q, sideActThread)
	sideClick(t, a, x, y)
	rows = railLines(t, a)
	joined = strings.Join(rows, "\n")
	want := []string{
		"↳ ",
		"@" + price + " → " + teamManagerGlyph + "  ✓ Status update",
		"@" + rail + " → " + teamManagerGlyph + "  working…",
		"@review → " + teamManagerGlyph + "  ✓ Status update",
	}
	for i, w := range want[1:] {
		if r := rows[top+1+i]; !strings.Contains(r, want[0]) || !strings.Contains(r, w) {
			t.Fatalf("reply %d reads %q, want %q:\n%s", i, r, w, joined)
		}
	}
	for _, never := range []string{"woke", ": finished", ": ✓ finished"} {
		if strings.Contains(joined, never) {
			t.Fatalf("%q is drawn as a line of its own:\n%s", never, joined)
		}
	}
	if !strings.Contains(rows[top], glyphOpen) || railRowOf(rows, "General") != top+4 {
		t.Fatalf("the open row does not say it is open, or General moved off its place:\n%s", joined)
	}
	// AND THE SAME DOOR FOLDS IT AGAIN.
	x, y = sideRowDoor(t, a, "thread/"+q, sideActThread)
	sideClick(t, a, x, y)
	if joined := strings.Join(railLines(t, a), "\n"); strings.Contains(joined, "Status update") {
		t.Fatalf("the second press did not fold the replies:\n%s", joined)
	}
}

// A MEMBER ASKING IS THE BAND'S ONE AMBER ROW, and its thread says asking; a
// failure after it is a ✗ in ordinary ink and nothing on the column is amber.
// An old finishing that answers nothing is chatter, under General.
func TestTrafficThreadAnAskIsTheBandAndAFailureIsInk(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindEvent, From: rail, To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished"})
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "run the migration"})
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateAsking, Text: "asks: may I run it on prod?", Answers: q})
	trafficReadNow(t, a)
	rows := railLines(t, a)
	joined := strings.Join(rows, "\n")
	head := railRowOf(rows, sideTrafficWord)
	ask := railRowOf(rows, "@"+price+" → "+teamManagerGlyph+"  may I run")
	if head < 0 || ask != head+1 {
		t.Fatalf("the ask is not the band's row under the header (%d, %d):\n%s", head, ask, joined)
	}
	if strings.HasSuffix(strings.TrimRight(rows[ask], " "), "now") || strings.Contains(rows[ask], "asks:") {
		t.Fatalf("the band row still carries its age or the old asks lead: %q", rows[ask])
	}
	for _, r := range a.side.last {
		if strings.HasPrefix(r.key, "ask/") && !strings.Contains(r.hint, "now") {
			t.Fatalf("the band's hint does not carry the age: %q", r.hint)
		}
	}
	work := railRowOf(rows, teamManagerGlyph+" → @"+price)
	if work <= ask || !strings.Contains(rows[work], "asking") || !strings.Contains(rows[work], "run the") {
		t.Fatalf("the thread does not say it is asking, under the band:\n%s", joined)
	}
	if general := railRowOf(rows, "General"); general <= work {
		t.Fatalf("the old finishing is not chatter under the work:\n%s", joined)
	}
	probe := a.pal.ask("x")
	warm := probe[:strings.Index(probe, "x")]
	amber := func() int {
		n := 0
		for _, r := range strings.Split(sidePainted(a), "\n") {
			if warm != "" && strings.Contains(r, warm) {
				n++
			}
		}
		return n
	}
	if warm != "" && amber() != 1 {
		t.Fatalf("%d rows carry the needs-you amber, want the band's one", amber())
	}

	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateFailed, Text: "the migration failed: lock timeout", Answers: q})
	trafficReadNow(t, a)
	rows = railLines(t, a)
	joined = strings.Join(rows, "\n")
	if strings.Contains(joined, "asks: may I run") {
		t.Fatalf("an answered ask is still in the band:\n%s", joined)
	}
	work = railRowOf(rows, teamManagerGlyph+" → @"+price)
	if work < 0 || !strings.Contains(rows[work], "failed") || !strings.Contains(rows[work], "run the") {
		t.Fatalf("the thread does not say it failed:\n%s", joined)
	}
	if warm != "" && amber() != 0 {
		t.Fatalf("a failure is painted amber on %d rows", amber())
	}
	x, y := sideRowDoor(t, a, "thread/"+q, sideActThread)
	sideClick(t, a, x, y)
	if rows := railLines(t, a); railRowOf(rows, "@"+price+" → "+teamManagerGlyph+"  "+tokens.GlyphFailed+" the migration") < 0 {
		t.Fatalf("the failure did not fold into the member's line:\n%s", strings.Join(rows, "\n"))
	}
}

// EVERY ROW IS ONE LINE AND THE HINT SAYS IT WHOLE. A long reply is cut with
// an ellipsis in the column and said in full on the hint line; enter on a
// thread with the column holding the keyboard lays it open and folds it, and
// neither moves the conversation in front.
func TestTrafficThreadRowsAreOneLineAndEnterFoldsThem(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	long := "Status update: " + strings.Repeat("the scrape is cached and every price is checked twice ", 3) + "END"
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "status?"})
	reply, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: long, Answers: q})
	trafficReadNow(t, a)
	front := a.frontTabKey()

	drive(t, a, altT())
	if !a.railHold {
		t.Fatal("alt+t did not give the column the keyboard")
	}
	_ = railLines(t, a) // the loop draws a frame before any key reaches it
	a.railWhere = railSpot{key: "thread/" + q}
	drive(t, a, key("enter"))
	rows := railLines(t, a)
	y := railRowOf(rows, "@"+price+" → "+teamManagerGlyph+"  Status update")
	if y < 0 || strings.Contains(strings.Join(rows, "\n"), "END") || !strings.Contains(rows[y], "…") {
		t.Fatalf("enter did not lay the reply open as one cut line:\n%s", strings.Join(rows, "\n"))
	}
	if a.frontTabKey() != front || !a.railHold {
		t.Fatal("laying the thread open moved the focus or took the keyboard back")
	}
	x, ry := sideRowOn(t, a, "reply/"+reply)
	a.setHover(x, ry)
	if words := a.dockHoverWords(); !strings.Contains(words, "END") {
		t.Fatalf("the hint line does not say the reply whole: %q", words)
	}
	a.dropHover()
	a.railWhere = railSpot{key: "thread/" + q}
	drive(t, a, key("enter"))
	if joined := strings.Join(railLines(t, a), "\n"); strings.Contains(joined, "Status update") {
		t.Fatalf("the second enter did not fold the thread:\n%s", joined)
	}
}

// A HANDLE ON THE COLUMN IS A LINK, inked as the chat inks one, and a press on
// it opens its member at the message; an address that is nobody's is plain
// words.
func TestTrafficThreadHandlesAreLinks(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q := threadScenario(t, a, harbor, price, rail)
	a.sideToggleThread(sideThreadKey(harbor, q))
	_ = railLines(t, a)
	painted := sidePainted(a)
	if !strings.Contains(painted, teamLinkInk(a.pal, "@"+price)) {
		t.Fatal("a handle is not inked as a chat link is")
	}
	if strings.Contains(painted, teamLinkInk(a.pal, "@review")) {
		t.Fatal("an address that is nobody's is inked as a link")
	}
	links := 0
	for _, row := range a.side.last {
		for _, d := range row.doors {
			if d.act.kind == sideActJump {
				links++
				if d.act.key == "" || d.act.entry == "" {
					t.Fatalf("a link opens nobody or nowhere: %+v", d)
				}
			}
		}
	}
	if links < 3 {
		t.Fatalf("%d handle links drawn", links)
	}
	x, y := sideRowDoor(t, a, "thread/"+q, sideActJump)
	a.setHover(x, y)
	if words := a.dockHoverWords(); !strings.Contains(words, "@"+price) || !strings.Contains(words, "click") {
		t.Fatalf("the handle's hint says %q", words)
	}
	a.dropHover()
	sideClick(t, a, x, y)
	if a.frontTabKey() != priceKey {
		t.Fatalf("the handle went to %q, want %q", a.frontTabKey(), priceKey)
	}
}
