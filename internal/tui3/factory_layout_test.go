package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/mock"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FLOOR'S GEOMETRY, AT THE FRAME ──────────────────────────────────────
//
// These read the whole frame the router draws, on the still fixture and on a
// moving mock world, at the four widths a person meets: two columns at 150 and
// 120, full-width rows at 100, and ref and title alone at 80.

// factoryLayoutWidths are the four widths, each with the height it is looked
// at.
var factoryLayoutWidths = []struct{ width, height int }{{150, 44}, {120, 40}, {100, 40}, {80, 30}}

// factoryMockLab is the factory place over a generated mock world that has
// slept through nine hours, as the factorymock build opens it.
func factoryMockLab(t *testing.T) *app {
	t.Helper()
	seam := mock.New(7, 12, 400, 6, factoryTestNow.Add(-9*time.Hour))
	if err := seam.Sleep(9 * time.Hour); err != nil {
		t.Fatalf("the mock world would not sleep: %v", err)
	}
	a := placeApp(t)
	a.factory = seam
	a.width, a.height = 150, 44
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) || !a.fp.loaded {
		t.Fatal("the factory place did not open over the mock world")
	}
	return a
}

// factoryFrameLines is the whole frame, plain, one string per row.
func factoryFrameLines(a *app) []string {
	f, _, _ := a.frame()
	return strings.Split(plain(f), "\n")
}

// factoryBodyPlain is the place's body at width and room, plain.
func factoryBodyPlain(a *app, width, room int) []string {
	rows := a.factoryBody(width, room)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r.text)
	}
	return out
}

// factoryRowOf is the body row that carries ref as an item's ref, and "" when
// none does.
func factoryRowOf(rows []string, ref string) string {
	for _, r := range rows {
		f := strings.Fields(r)
		for i := 0; i < len(f) && i < 2; i++ {
			if f[i] == ref {
				return r
			}
		}
	}
	return ""
}

// AT EVERY WIDTH THE FRAME IS EXACTLY HEIGHT × WIDTH, on the fixture and on the
// mock world, and at the plain floor the body carries no SGR at all.
func TestFactoryLayoutFrameIsExactAtEveryWidth(t *testing.T) {
	for name, lab := range map[string]func(*testing.T) *app{"fixture": factoryPlaceLab, "mock": factoryMockLab} {
		for _, sz := range factoryLayoutWidths {
			a := lab(t)
			a.width, a.height = sz.width, sz.height
			lines := factoryFrameLines(a)
			if len(lines) != sz.height {
				t.Fatalf("%s at %d×%d: the frame is %d rows", name, sz.width, sz.height, len(lines))
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > sz.width {
					t.Fatalf("%s at %d×%d: row %d is %d cells: %q", name, sz.width, sz.height, i, w, line)
				}
			}
			a.pal = newPalette(tokens.NoColor, false)
			for i, r := range a.factoryBody(sz.width, sz.height-8) {
				if got := ansi.StringWidth(r.text); got != sz.width {
					t.Fatalf("%s at %d: body row %d is %d cells: %q", name, sz.width, i, got, ansi.Strip(r.text))
				}
				if strings.Contains(r.text, "\x1b[") {
					t.Fatalf("%s at %d: body row %d carries SGR at the plain floor: %q", name, sz.width, i, r.text)
				}
			}
		}
	}
}

// AT 150 THE HANDOVER SPANS THE WHOLE WIDTH and the peek stands beside the
// rows; at 100 there is no peek and the rows carry their state's fact; at 80 a
// row is its ref and its title and nothing else.
func TestFactoryLayoutThreeGeometries(t *testing.T) {
	a := factoryPlaceLab(t)

	wide := factoryBodyPlain(a, 150, 36)
	if !strings.Contains(wide[0], "handover · since") || strings.Contains(wide[0], "│") {
		t.Fatalf("at 150 the handover's first row is not full width: %q", wide[0])
	}
	if !strings.HasSuffix(strings.TrimRight(wide[0], " "), "─") || ansi.StringWidth(strings.TrimRight(wide[0], " ")) < 149 {
		t.Fatalf("at 150 the handover's hairline stops short of the edge: %q", wide[0])
	}
	sep := 0
	for _, r := range wide[5:] {
		if strings.Contains(r, "│") {
			sep++
		}
	}
	if sep != len(wide)-5 {
		t.Fatalf("at 150 only %d of %d floor rows stand beside a peek:\n%s", sep, len(wide)-5, strings.Join(wide, "\n"))
	}
	if !strings.Contains(strings.Join(wide, "\n"), "y n answer · a in words") {
		t.Fatalf("at 150 there is no peek:\n%s", strings.Join(wide, "\n"))
	}

	mid := factoryBodyPlain(a, 100, 36)
	for _, r := range mid {
		if strings.Contains(r, "│") {
			t.Fatalf("at 100 a peek is drawn: %q", r)
		}
	}
	for ref, fact := range map[string]string{"#1538": "plan is ready", "#1660": "queued", "ci": "ci red", "#1661": "1" + a.icon(tokens.GFailed)} {
		if row := factoryRowOf(mid, ref); !strings.Contains(row, fact) {
			t.Fatalf("at 100 %s's row does not carry %q: %q", ref, fact, row)
		}
	}
	if row := factoryRowOf(mid, "#1538"); !strings.Contains(row, factoryAnswerWord) || !strings.Contains(row, "codeaf") {
		t.Fatalf("at 100 the needs-you row has no repo or no %s: %q", factoryAnswerWord, row)
	}

	narrow := factoryBodyPlain(a, 80, 28)
	for _, it := range a.fp.snap.Items {
		row := factoryRowOf(narrow, it.Ref())
		if row == "" {
			continue
		}
		rest := strings.TrimSpace(row[strings.Index(row, it.Ref())+len(it.Ref()):])
		if !strings.HasPrefix(it.Title, strings.TrimSuffix(rest, a.icon(tokens.GEllipsis))) {
			t.Fatalf("at 80 %s's row carries more than its title: %q", it.Ref(), row)
		}
	}
	if strings.Contains(strings.Join(narrow, "\n"), "handover") {
		t.Fatal("at 80 the handover is drawn")
	}
}

// THE FACTS DROP FROM THE RIGHT, WHOLE, IN RANK ORDER: at every width the
// facts a row carries are a leading run of its ranked facts, and narrowing
// never brings a later one back.
func TestFactoryLayoutFactsDropFromTheRight(t *testing.T) {
	for name, lab := range map[string]func(*testing.T) *app{"fixture": factoryPlaceLab, "mock": factoryMockLab} {
		a := lab(t)
		a.pal = newPalette(tokens.NoColor, false)
		for _, it := range a.fp.snap.Items {
			parts := a.factoryRowFacts(it)
			if len(parts) == 0 {
				continue
			}
			if len(parts) > factoryFactsMost {
				t.Fatalf("%s %s carries %d facts", name, it.Ref(), len(parts))
			}
			last := len(parts)
			for w := 80; w >= 1; w-- {
				got := a.factoryFactsLine(it, w)
				if ansi.StringWidth(got) > w {
					t.Fatalf("%s %s at %d: the facts are %d cells: %q", name, it.Ref(), w, ansi.StringWidth(got), got)
				}
				n := -1
				for k := len(parts); k >= 1; k-- {
					var plains []string
					for _, p := range parts[:k] {
						plains = append(plains, p.plain)
					}
					if got == strings.Join(plains, rowSep) {
						n = k
						break
					}
				}
				if n < 0 {
					// ONLY THE STATE FACT ALONE MAY BE CUT, and only when it is
					// wider than the whole column on its own.
					if ansi.StringWidth(parts[0].plain) <= w {
						t.Fatalf("%s %s at %d: %q is not a leading run of its facts", name, it.Ref(), w, got)
					}
					n = 1
				}
				if n > last {
					t.Fatalf("%s %s at %d: a dropped fact came back (%d after %d)", name, it.Ref(), w, n, last)
				}
				last = n
			}
		}
	}
}

// ENTER OPENS THE ITEM PAGE on the running stage of a running item and on the
// proof of a landed one; ESC PUTS THE FLOOR BACK ON THE SAME ROW.
func TestFactoryLayoutItemPageOpensOnTheRightStage(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	for _, c := range []struct {
		id    int
		stage string
	}{{2, "review"}, {9, "proof"}, {1, "plan"}, {4, "read"}} {
		for at, i := range a.factoryWalkNow() {
			if a.fp.snap.Items[i].ID == c.id {
				a.fp.cursor = at
			}
		}
		was := a.fp.cursor
		drive(t, a, key("enter"))
		if !a.fp.open {
			t.Fatalf("enter on item %d did not open its page", c.id)
		}
		it, _ := a.factoryCursorItem()
		if got := a.factoryItemStages(it)[a.fp.stage].stage.Name; got != c.stage {
			t.Fatalf("item %d opened on %q, want %q", c.id, got, c.stage)
		}
		text := strings.Join(factoryFrameLines(a), "\n")
		if !strings.Contains(text, it.Ref()+" "+it.Title) || !strings.Contains(text, "places:") {
			t.Fatalf("item %d's page has no head:\n%s", c.id, text)
		}
		if strings.Contains(text, "NEEDS YOU") {
			t.Fatalf("item %d's page still draws the floor:\n%s", c.id, text)
		}
		drive(t, a, key("enter"))
		if text := strings.Join(factoryFrameLines(a), "\n"); !strings.Contains(text, factoryStageNoteWords) {
			t.Fatalf("enter on a stage did not say what it will open:\n%s", text)
		}
		drive(t, a, key("esc"))
		if a.fp.open || !a.at(pageFactory) || a.fp.cursor != was {
			t.Fatalf("esc from item %d: open %v, on the factory %v, cursor %d want %d", c.id, a.fp.open, a.at(pageFactory), a.fp.cursor, was)
		}
	}
}

// THE ITEM PAGE IS EXACTLY THE ROOM AT EVERY WIDTH, its stage rail beside the
// pane and, under the stage floor, one line above it; ↓ walks the stages.
func TestFactoryLayoutItemPageIsExact(t *testing.T) {
	a := factoryPlaceLab(t)
	for _, it := range a.fp.snap.Items {
		if it.State == factory.StateDismissed {
			continue
		}
		for _, width := range []int{150, 100, 80, 60} {
			for _, room := range []int{1, 3, 5, 12, 36} {
				rows := a.factoryItemBody(it, width, room)
				if len(rows) != room {
					t.Fatalf("%s at %d×%d drew %d rows", it.Ref(), width, room, len(rows))
				}
				for i, r := range rows {
					if got := ansi.StringWidth(r.text); got != width {
						t.Fatalf("%s at %d×%d: row %d is %d cells: %q", it.Ref(), width, room, i, got, ansi.Strip(r.text))
					}
				}
			}
		}
	}
	for at, i := range a.factoryWalkNow() {
		if a.fp.snap.Items[i].ID == 2 {
			a.fp.cursor = at
		}
	}
	drive(t, a, key("enter"))
	from := a.fp.stage
	drive(t, a, key("down"))
	if a.fp.stage != from+1 || !a.fp.open {
		t.Fatalf("↓ on the item page put the stage at %d from %d", a.fp.stage, from)
	}
	drive(t, a, key("up"), key("up"))
	if a.fp.stage != from-1 || !a.fp.open {
		t.Fatalf("↑ twice put the stage at %d from %d", a.fp.stage, from)
	}
	it, _ := a.factoryCursorItem()
	strip := ansi.Strip(strings.Join(func() []string {
		var out []string
		for _, r := range a.factoryItemBody(it, 60, 20) {
			out = append(out, r.text)
		}
		return out
	}(), "\n"))
	if strings.Contains(strip, "│") || !strings.Contains(strip, "test") {
		t.Fatalf("under the stage floor the rail is not one line of stages:\n%s", strip)
	}
}

// Z ADDS THE FACTORY'S READ UNDER EACH ROW, and a second z takes it away.
func TestFactoryLayoutZAddsTheReadLine(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 100, 44
	read := "probably the paste path"
	if strings.Contains(strings.Join(factoryFrameLines(a), "\n"), read) {
		t.Fatal("the compact floor already draws a read")
	}
	drive(t, a, key("z"))
	if !a.fp.comfy || !strings.Contains(strings.Join(factoryFrameLines(a), "\n"), read) {
		t.Fatalf("z did not add the read line:\n%s", strings.Join(factoryFrameLines(a), "\n"))
	}
	drive(t, a, key("z"))
	if a.fp.comfy || strings.Contains(strings.Join(factoryFrameLines(a), "\n"), read) {
		t.Fatal("a second z did not take the read line away")
	}
}

// THE BAR SHOWS THE FACTORY THIRD, after home and the chats, with `? N` while
// N items wait on the person and no chip at all at zero.
func TestFactoryLayoutBarShowsFactoryThird(t *testing.T) {
	if got := barPages(pageHome, false); len(got) < 3 || got[2] != pageFactory {
		t.Fatalf("the bar's order is %v", got)
	}
	a := factoryPlaceLab(t)
	line := plain(a.navLine(160, a.pal))
	home, chats, fac := strings.Index(line, navLabel(pageHome)), strings.Index(line, navLabel(pageChats)), strings.Index(line, navLabel(pageFactory)+" ? 1")
	if home < 0 || chats < home || fac < chats {
		t.Fatalf("the bar does not read Home, Chats, Factory ? 1: %q", line)
	}
	if teams := strings.Index(line, navLabel(pageTeams)); teams >= 0 && teams < fac {
		t.Fatalf("the factory is not third: %q", line)
	}
	snap := factory.Fixture(factoryTestNow)
	for i := range snap.Items {
		if snap.Items[i].State == factory.StateNeedsYou {
			snap.Items[i].State = factory.StateRunning
		}
	}
	a.factoryFold(snap)
	line = plain(a.navLine(160, a.pal))
	if !strings.Contains(line, navLabel(pageFactory)) || strings.Contains(line, navLabel(pageFactory)+" ?") {
		t.Fatalf("with nothing waiting the factory wears a chip: %q", line)
	}
}
