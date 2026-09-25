package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// WHAT CAME IN WHILE THE TASKS WERE IN FRONT IS COUNTED ON THE OTHER WORD,
// `Traffic 2 new`, and when the Traffic comes to the front a thin `new` line
// stands under it and over what was already seen. The line holds still while
// the Traffic is read, a row arriving moves neither the header nor the body,
// and once looked at the word counts everything again.
func TestTheTrafficSaysWhatIsNewAndHoldsStill(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.welcome.open = false
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "old work"})
	trafficReadNow(t, a)
	_ = railLines(t, a) // the Traffic in front: the old work is seen
	a.sideSetView(sideTasks)
	body, cols := a.bodyWidth(), a.railWidth()
	head := railRowOf(railLines(t, a), sideTasksWord)

	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: rail, Text: "first new work"},
		teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "second new work"},
	)
	trafficReadNow(t, a)
	rows := railLines(t, a)
	if railRowOf(rows, sideTrafficWord+" 2 "+sideNewWord) != head || a.bodyWidth() != body || a.railWidth() != cols {
		t.Fatalf("the other word does not count what is new in place (body %d, cols %d):\n%s", a.bodyWidth(), a.railWidth(), strings.Join(rows, "\n"))
	}

	a.sideSetView(sideTraffic)
	rows = railLines(t, a)
	joined := strings.Join(rows, "\n")
	second, first := railRowOf(rows, "second new work"), railRowOf(rows, "first new work")
	line, old := railRowOf(rows, "── "+sideNewWord+" ──"), railRowOf(rows, "old work")
	if railRowOf(rows, sideTrafficWord) != head || second != head+1 || first != second+1 || line != first+1 || old != line+1 {
		t.Fatalf("the new line does not stand between the new and the seen (%d %d %d %d):\n%s", second, first, line, old, joined)
	}
	if strings.Contains(rows[head], sideNewWord) {
		t.Fatalf("the word still says new with the Traffic in front: %q", rows[head])
	}

	// A ROW ARRIVING WHILE IT IS READ goes on top; the line and the header
	// hold, and the body does not move.
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: rail, Text: "third new work"})
	trafficReadNow(t, a)
	rows = railLines(t, a)
	if railRowOf(rows, "third new work") != head+1 || railRowOf(rows, "── "+sideNewWord+" ──") != line+1 || railRowOf(rows, sideTrafficWord) != head || a.bodyWidth() != body {
		t.Fatalf("a row arriving moved the line or the header:\n%s", strings.Join(rows, "\n"))
	}

	// AND LOOKED AT, IT IS SEEN: away and back, no line.
	a.sideSetView(sideTasks)
	a.sideSetView(sideTraffic)
	if rows := railLines(t, a); railRowOf(rows, "── "+sideNewWord+" ──") >= 0 {
		t.Fatalf("the line stayed over what was read:\n%s", strings.Join(rows, "\n"))
	}
}

// IN A MEMBER'S CHAT THE TRAFFIC IS THAT MEMBER'S MESSAGES, newest first, one
// line each: what it said is `→ whom`, what it was told is who told it, and
// what went between two other members is not there. The count on the word is
// the same set.
func TestAMembersTrafficIsItsOwnMessages(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "scrape the prices"},
		teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: rail, Text: "not for price"},
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "prices are in"},
		teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: teamstore.ToEveryone, Text: "all hands"},
	)
	trafficReadNow(t, a)
	spend(t, a, a.trafficGo(priceKey))
	a.sideSetView(sideTraffic)
	rows := railLines(t, a)
	joined := strings.Join(rows, "\n")
	all := railRowOf(rows, "all hands")
	said := railRowOf(rows, "prices are in")
	told := railRowOf(rows, "scrape the prices")
	if all < 0 || said != all+1 || told != said+1 {
		t.Fatalf("the member's messages are not newest first, one line each (%d %d %d):\n%s", all, said, told, joined)
	}
	if !strings.Contains(rows[said], "you → "+teamManagerGlyph) || !strings.Contains(rows[told], teamManagerGlyph+" → you") {
		t.Fatalf("a row does not say who it is from and who it is for:\n%s", joined)
	}
	if strings.Contains(joined, "not for price") {
		t.Fatalf("a message between others is in the member's Traffic:\n%s", joined)
	}
	if n, _ := a.sideTrafficCount(mustTeam(t, a, harbor), sideKindMember, price); n != 3 {
		t.Fatalf("the word counts %d, want the member's 3", n)
	}
	// A PRESS ON A ROW GOES TO ITS MESSAGE HERE, and nowhere else.
	x, y := sideRowOn(t, a, railKeyOfReply(t, a, "prices are in"))
	sideClick(t, a, x, y)
	if a.frontTabKey() != priceKey {
		t.Fatalf("a press on the member's own row left its chat for %q", a.frontTabKey())
	}
}

// THE GROUND THE POINTER LIGHTS IS THE TARGET THE PRESS HITS. On every row the
// Traffic draws, laid open and not, the hover at a cell names the row, or the
// door, that a press at the same cell acts on; the whole row lights for a
// row, and lighting moves no word of it.
func TestTheTrafficsHoverGroundIsItsClickTarget(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q := threadScenario(t, a, harbor, price, rail)
	a.sideToggleThread(sideThreadKey(harbor, q))
	a.sideToggleThread(sideThreadKey(harbor, sideGeneral))
	_ = railLines(t, a)
	view, _ := a.railDrawnView(a.viewHeight())
	left := a.railLeft() + ansi.StringWidth(railSeam)
	checked := 0
	for i, line := range view {
		if line.side == nil || line.side.key == sideHeadKey {
			continue
		}
		y := a.topHeight() + i
		for col := 0; col < a.railRoom(); col++ {
			a.setHover(left+col, y)
			door := line.side.doorAt(col)
			switch {
			case door >= 0:
				if a.hot.kind != hoverSide || a.hot.key != line.side.key || a.hot.index != door {
					t.Fatalf("row %q col %d: the pointer lit %+v, the press hits door %d", line.side.key, col, a.hot, door)
				}
			case line.side.pressable():
				if a.hot.kind != hoverSide || a.hot.key != line.side.key || a.hot.index != -1 {
					t.Fatalf("row %q col %d: the pointer lit %+v, the press hits the row", line.side.key, col, a.hot)
				}
			default:
				if a.hot.kind == hoverSide {
					t.Fatalf("row %q col %d lights with nothing to press", line.side.key, col)
				}
			}
			checked++
		}
		// THE WHOLE ROW LIGHTS, and the words under the light are the words.
		if line.side.pressable() {
			x, _ := sideRowOn(t, a, line.side.key)
			a.dropHover()
			before := a.railRows(a.viewHeight())
			a.setHover(x, y)
			after := a.railRows(a.viewHeight())
			y0 := y - a.topHeight()
			if before[y0] == after[y0] || ansi.Strip(before[y0]) != ansi.Strip(after[y0]) {
				t.Fatalf("row %q: the hover did not light it, or moved its words:\n%q\n%q", line.side.key, before[y0], after[y0])
			}
		}
	}
	if checked == 0 {
		t.Fatal("the Traffic drew no row to point at")
	}
	a.dropHover()
}

// rowEndsWithAge reports that a Traffic row keeps its age at the right:
// `now`, `2m`, `3h`, `1d`.
func rowEndsWithAge(row string) bool {
	row = strings.TrimRight(row, " ")
	if strings.HasSuffix(row, "now") {
		return true
	}
	if len(row) < 2 {
		return false
	}
	unit := row[len(row)-1]
	if (unit == 'm' || unit == 'h' || unit == 'd' || unit == 's') && row[len(row)-2] >= '0' && row[len(row)-2] <= '9' {
		return true
	}
	return false
}

// rowKeepsTheArrow reports that a Traffic row still says who it is from and
// who it is for, cuts its words at a word, and keeps its age. full is the
// words the row was cut from.
func rowKeepsTheArrow(row, full string) bool {
	row = strings.TrimRight(row, " ")
	if !strings.Contains(row, "→") || !rowEndsWithAge(row) {
		return false
	}
	// A cut through a word of full leaves a prefix of that word and the
	// ellipsis, with the rest of the word missing.
	for _, w := range strings.Fields(full) {
		if len(w) < 4 {
			continue
		}
		for n := 2; n < len(w); n++ {
			frag := w[:n] + "…"
			if strings.Contains(row, frag) && !strings.Contains(row, w) {
				return false
			}
		}
	}
	return true
}

// EVERY WIDTH DRAWS `from → to` AND CUTS ONLY THE WORDS. At a column of 28,
// 32 and 40 the arrow and the names stay, the age stays at the right, and the
// words stop at a word. The same is true of a member's own rows.
func TestTrafficRowsKeepTheArrowAtEveryWidth(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q := threadScenario(t, a, harbor, price, rail)
	full := "Please provide a brief status update on your part"
	for _, width := range []int{112, 128, 160} {
		a.width, a.height = width, 40
		a.touch()
		if got := a.railWidth(); got != sideColsFor(width) || (got != 28 && got != 32 && got != 40) {
			t.Fatalf("width %d lends column %d, want 28, 32 or 40", width, got)
		}
		rows := railLines(t, a)
		at := railRowOf(rows, "@"+price+" +2")
		if at < 0 || !strings.Contains(rows[at], teamManagerGlyph+" → ") || !rowKeepsTheArrow(rows[at], full) {
			t.Fatalf("at column %d the work row lost its arrow or cut a word:\n%s", a.railWidth(), strings.Join(rows, "\n"))
		}
		hint := a.sideRowOf("thread/" + q).hint
		if !strings.Contains(hint, "Open ") || !strings.Contains(hint, hintSegment+"now"+hintSegment) || !strings.Contains(hint, hintSegment+"click") {
			t.Fatalf("at column %d the hint does not open the message at its age: %q", a.railWidth(), hint)
		}
	}
	spend(t, a, a.trafficGo(priceKey))
	a.sideSetView(sideTraffic)
	for _, width := range []int{112, 128, 160} {
		a.width, a.height = width, 40
		a.touch()
		rows := railLines(t, a)
		told := railRowOf(rows, teamManagerGlyph+" → you")
		if told < 0 || !rowKeepsTheArrow(rows[told], full) {
			t.Fatalf("at column %d a member row is not `from → you`:\n%s", a.railWidth(), strings.Join(rows, "\n"))
		}
		if hint := a.sideRowOf(railKeyOfReply(t, a, teamManagerGlyph+" → you")).hint; !strings.Contains(hint, "Open ") || !strings.Contains(hint, "now") {
			t.Fatalf("at column %d the member row's hint does not open the message: %q", a.railWidth(), hint)
		}
	}
}

// A CUT NEVER LEAVES A CLAUSE ITS SEPARATOR AND NOTHING ELSE.
func TestABandRowIsCutAtAClause(t *testing.T) {
	for _, c := range []struct {
		words string
		width int
		want  string
	}{
		{"Ship the price table · your call", 24, "Ship the price table…"},
		{"Ship the price table · your call", 29, "Ship the price table · your…"},
		{"Ship the price table", 30, "Ship the price table"},
	} {
		if got, w := fitClauses(c.words, c.width, "…"); got != c.want || w > c.width {
			t.Fatalf("%q at %d is %q, want %q", c.words, c.width, got, c.want)
		}
	}
}
