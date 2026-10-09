package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FLOOR'S GEOMETRY, AT THE FRAME ──────────────────────────────────────
//
// These read the whole frame the router draws, on the still fixture, at the
// four widths a person meets: two columns at 150 and 120, full-width rows at
// 100, and ref and title alone at 80. The tests that need a list too long for
// the window read [factoryBigLab]'s floor.

// factoryLayoutWidths are the four widths, each with the height it is looked
// at.
var factoryLayoutWidths = []struct{ width, height int }{{150, 44}, {120, 40}, {100, 40}, {80, 30}}

// factoryBigCopies is how many times [factoryBigLab] repeats the fixture's
// items beside the originals.
const factoryBigCopies = 6

// factoryBigLab is the factory place over the fake seam ([factoryFake]) whose
// every read is the fixture with its items repeated [factoryBigCopies] more
// times under fresh ids and numbers: a floor long enough to scroll at every
// height, with every door the verbs need, opened at 150×44.
func factoryBigLab(t *testing.T) *app {
	t.Helper()
	f := &factoryFake{shape: func(s *factory.Snapshot) {
		base := s.Items
		for k := 1; k <= factoryBigCopies; k++ {
			for _, it := range base {
				it.ID += 100 * k
				if it.Num > 0 {
					it.Num += 10000 * k
				}
				it.URL = ""
				it.Title = it.Title + " " + itoa(k+1)
				s.Items = append(s.Items, it)
			}
		}
	}}
	return factoryVerbLab(t, f)
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
		for i := 0; i < len(f) && i < 3; i++ {
			if f[i] == ref {
				return r
			}
		}
	}
	return ""
}

// AT EVERY WIDTH THE FRAME IS EXACTLY HEIGHT × WIDTH, on the fixture, and at the plain floor the body carries no SGR at all.
func TestFactoryLayoutFrameIsExactAtEveryWidth(t *testing.T) {
	for name, lab := range map[string]func(*testing.T) *app{"fixture": factoryPlaceLab} {
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
	a.fp.headFull = true

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
	if !strings.Contains(strings.Join(wide, "\n"), "touches three packages") {
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
	for name, lab := range map[string]func(*testing.T) *app{"fixture": factoryPlaceLab} {
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
				got := a.factoryFactsLine(it, w, 0)
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

// factoryPageRowName is the left column's row under the item page's cursor,
// by name: the facet's word, or the stage's own name.
func factoryPageRowName(a *app, it factory.Item) string {
	return factoryPageRowWord(a.factoryItemRows(it)[a.fp.stage])
}

// factoryPageRowWord is one row of the left column by name: the facet's
// word, or the stage's own name.
func factoryPageRowWord(r factoryPageRow) string {
	switch r.kind {
	case factoryPageIssue:
		return wordFacetIssue
	case factoryPageManager:
		return wordFacetManager
	case factoryPageSteps:
		return wordFacetSteps
	case factoryPageAction:
		return r.verb.word
	case factoryPageLog:
		return wordFacetLog
	case factoryPageResult:
		return wordFacetResult
	case factoryPageSettings:
		return wordFacetSettings
	}
	return r.view.stage.Name
}

// ENTER OPENS THE ITEM PAGE where the item is moving: the stage waiting on the
// person, else the running one, else the result of a landed item, else the
// issue; ESC PUTS THE FLOOR BACK ON THE SAME ROW.
func TestFactoryLayoutItemPageOpensOnTheRightStage(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	for _, c := range []struct {
		id    int
		stage string
	}{{2, "review"}, {9, "result"}, {1, "plan"}, {4, "issue"}, {10, "issue"}, {3, "issue"}} {
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
		if got := factoryPageRowName(a, it); got != c.stage {
			t.Fatalf("item %d opened on %q, want %q", c.id, got, c.stage)
		}
		text := strings.Join(factoryFrameLines(a), "\n")
		if !strings.Contains(text, it.Ref()+" "+it.Title) {
			t.Fatalf("item %d's page has no head:\n%s", c.id, text)
		}
		if strings.Contains(text, "NEEDS YOU") {
			t.Fatalf("item %d's page still draws the floor:\n%s", c.id, text)
		}
		drive(t, a, key("enter"))
		said := a.fp.said
		// `enter` on a stage with no chat says why it has none.
		if c.stage != "issue" && c.stage != "result" && !said {
			t.Fatalf("enter on item %d's stage did not say why it has no chat", c.id)
		}
		// `enter` on the issue opens the item's conversation (or its github
		// page), and says there is nothing to open only where neither door is.
		if c.stage == "issue" && said != (a.factoryIssueEnter(it) == factoryIssueNone) {
			t.Fatalf("enter on item %d's issue: said %v with door %v", c.id, said, a.factoryIssueEnter(it))
		}
		drive(t, a, key("esc"))
		if a.fp.open || !a.at(pageFactory) || a.fp.cursor != was {
			t.Fatalf("esc from item %d: open %v, on the factory %v, cursor %d want %d", c.id, a.fp.open, a.at(pageFactory), a.fp.cursor, was)
		}
	}
}

// THE LEFT COLUMN STARTS WITH THE ISSUE, then the manager where the seam has
// the Talk door, then the steps. The issue's pane is the whole body, then
// the read and the facts, a blank row between each, scrolled with J and K;
// the manager's center is its box before it has a chat, the steps' is every
// step as written, and a step's is its knobs and its ask.
func TestFactoryItemPageIssueRow(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	it := factoryPaneItem(t, a, 6)
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, "paragraph line "+itoa(i))
	}
	// EACH LINE IS ITS OWN PARAGRAPH: the body renders as Markdown, which folds
	// single line breaks into one paragraph, so a blank row between them keeps
	// forty rows that overflow the pane.
	it.Body = strings.Join(lines, "\n\n")
	it.Stages = factoryPaneItem(t, a, 1).Stages
	factoryOn(t, a, 6)
	drive(t, a, key("enter"))
	cur, _ := a.factoryCursorItem()
	rows := a.factoryItemRows(cur)
	if rows[0].kind != factoryPageIssue || rows[1].kind != factoryPageSteps || rows[2].kind != factoryPageStage || a.fp.stage != 0 {
		t.Fatalf("the column does not start with the issue and the run, or the cursor is not on it: %+v at %d", rows[:3], a.fp.stage)
	}
	frame := strings.Join(factoryFrameLines(a), "\n")
	if !strings.Contains(frame, "J K scroll") {
		t.Fatalf("the issue's action line does not name J K scroll:\n%s", frame)
	}
	for _, want := range []string{wordFacetIssue, "paragraph line 1", a.icon(tokens.GExpanded) + " more"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the issue pane is missing %q:\n%s", want, frame)
		}
	}
	drive(t, a, key("J"), key("J"))
	frame = strings.Join(factoryFrameLines(a), "\n")
	// Two rows down is the blank after line 1 and then line 2: line 1 is gone,
	// line 2 leads the pane.
	if strings.Contains(frame, "paragraph line 1 ") || !strings.Contains(frame, "paragraph line 2 ") {
		t.Fatalf("J twice did not scroll the issue two rows:\n%s", frame)
	}
	for i := 0; i < 10; i++ {
		drive(t, a, key("pgdown"))
	}
	frame = strings.Join(factoryFrameLines(a), "\n")
	gap := strings.Repeat(" ", factoryFactGap)
	for _, want := range []string{"paragraph line 40", "underspecified; two questions for the author first", "thin" + gap + "stranger"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the end of the issue is missing %q:\n%s", want, frame)
		}
	}
	// THE TALK DOOR PUTS THE MANAGER SECOND, and the cursor still lands on
	// the issue of an item nothing is happening to.
	drive(t, a, key("esc"))
	a.factory.Talk = func(context.Context, int) (string, error) { return "", nil }
	drive(t, a, key("enter"))
	cur, _ = a.factoryCursorItem()
	if rows := a.factoryItemRows(cur); rows[1].kind != factoryPageManager || a.fp.stage != 0 {
		t.Fatalf("the manager row is not second: %+v", rows[:2])
	}
	// The manager's center is its box, the steps' every step, a step's its
	// knobs.
	for _, c := range []struct {
		kind factoryPageKind
		want string
	}{
		{factoryPageManager, wordEnterOrClickToTalk},
		{factoryPageSteps, "review"},
		{factoryPageStage, "plan" + rowSep + string(factory.StageChat)},
	} {
		drive(t, a, key("down"))
		if got := a.factoryItemRows(cur)[a.fp.stage].kind; got != c.kind {
			t.Fatalf("↓ stands on %v, want %v", got, c.kind)
		}
		var pane []string
		for _, r := range a.factoryItemBody(cur, 150, 30) {
			if _, right, div := factorySplitAt(ansi.Strip(r.text)); div >= 0 {
				pane = append(pane, strings.TrimSpace(right))
			}
		}
		if !strings.Contains(strings.Join(pane, "\n"), c.want) {
			t.Fatalf("row %v's center does not say %q:\n%s", c.kind, c.want, strings.Join(pane, "\n"))
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
	home, chats, fac := strings.Index(line, navLabel(pageHome)), strings.Index(line, navLabel(pageChats)), strings.Index(line, navLabel(pageFactory)+" ? 2")
	if home < 0 || chats < home || fac < chats {
		t.Fatalf("the bar does not read Home, Chats, Factory ? 2: %q", line)
	}
	if teams := strings.Index(line, navLabel(pageTeams)); teams >= 0 && teams < fac {
		t.Fatalf("the factory is not third: %q", line)
	}
	snap := factory.Fixture(factoryTestNow)
	for i := range snap.Items {
		switch snap.Items[i].State {
		case factory.StateNeedsYou:
			snap.Items[i].State = factory.StateRunning
		case factory.StateLanded:
			// A landed item waits on the sign-off, so it counts too.
			snap.Items[i].State = factory.StateShipped
		}
	}
	a.factoryFold(snap)
	line = plain(a.navLine(160, a.pal))
	if !strings.Contains(line, navLabel(pageFactory)) || strings.Contains(line, navLabel(pageFactory)+" ?") {
		t.Fatalf("with nothing waiting the factory wears a chip: %q", line)
	}
}

// THE REPO LINE STAYS PUT WHEN THE ROWS SCROLL: a floor narrowed to one repo
// and walked far down still says which repo it is narrowed to, on the first
// row of the floor, and a press on an item row below it lands on that item.
func TestFactoryLayoutRepoLineStaysWhenTheRowsScroll(t *testing.T) {
	a := factoryBigLab(t)
	a.width, a.height = 150, 30
	drive(t, a, key("]"))
	if a.fp.repo == 0 {
		t.Fatal("] narrowed to no repo")
	}
	for i := 0; i < 40; i++ {
		drive(t, a, key("down"))
	}
	room := 20
	rows := factoryBodyPlain(a, 150, room)
	if a.fp.top <= a.fp.pinned {
		t.Fatalf("forty steps down did not scroll the rows (top %d, pinned %d)", a.fp.top, a.fp.pinned)
	}
	repo := factoryRepoShort(a.fp.snap.Repos[a.fp.repo-1].Name)
	if first := strings.TrimSpace(rows[a.fp.headRows]); !strings.HasPrefix(first, repo+" · ") {
		t.Fatalf("the scrolled floor's first row is %q, not the repo line for %s", first, repo)
	}
	// A press on the last row of the window lands on the item drawn there.
	last := a.fp.headRows + a.fp.shown - 1
	plainRow := rows[last]
	if !a.factoryPress(last + placeHeadRows) {
		t.Fatalf("a press on the window's last row %q landed on nothing", plainRow)
	}
	it, _ := a.factoryCursorItem()
	if !strings.Contains(plainRow, it.Ref()) {
		t.Fatalf("a press on %q put the cursor on %s", plainRow, it.Ref())
	}
}

// THE PEEK'S STRIP KEEPS THE PHASE THAT IS MOVING: too narrow for every phase,
// it drops the finished ones before it rather than the running one.
func TestFactoryPeekStripKeepsTheRunningPhase(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	for _, it := range a.fp.snap.Items {
		if it.ID != 2 {
			continue
		}
		strip := ansi.Strip(a.factoryPeekStrip(it, 26))
		if !strings.Contains(strip, "review") || ansi.StringWidth(strip) > 26 {
			t.Fatalf("a narrow strip lost the running review or overflowed: %q", strip)
		}
		return
	}
	t.Fatal("the fixture has no item 2")
}

// ── the split ───────────────────────────────────────────────────────────────

// factoryDividerX is the column the divider was last drawn on.
func factoryDividerX(t *testing.T, a *app) int {
	t.Helper()
	factoryFrameLines(a)
	return a.fp.rowsW
}

// `{` AND `}` MOVE THE DIVIDER FOUR COLUMNS, `|` PUTS IT BACK AT 58%, and
// neither key takes the rows under seventy columns or the peek under forty;
// the frame stays exact wherever it stands.
func TestFactorySplitKeysAndLimits(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	start := factoryDividerX(t, a)
	if start != factoryRowsCols(150) {
		t.Fatalf("the divider starts at %d, want %d", start, factoryRowsCols(150))
	}
	drive(t, a, key("{"))
	if got := factoryDividerX(t, a); got != start-factorySplitStep {
		t.Fatalf("{ put the divider at %d from %d", got, start)
	}
	drive(t, a, key("}"), key("}"))
	if got := factoryDividerX(t, a); got != start+factorySplitStep {
		t.Fatalf("} twice put the divider at %d from %d", got, start-factorySplitStep)
	}
	for i := 0; i < 30; i++ {
		drive(t, a, key("}"))
	}
	if got := factoryDividerX(t, a); got != 150-1-factoryPeekMin {
		t.Fatalf("the divider went to %d, leaving the peek %d columns", got, 150-1-got)
	}
	for _, width := range []int{150, 120} {
		a.width = width
		for _, line := range factoryFrameLines(a) {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("at %d with the divider right the frame is %d cells: %q", width, w, line)
			}
		}
		if peek := width - 1 - a.fp.rowsW; peek < factoryPeekMin || a.fp.rowsW < factoryRowsMin {
			t.Fatalf("at %d the rows are %d and the peek %d", width, a.fp.rowsW, peek)
		}
	}
	a.width = 150
	factoryFrameLines(a)
	for i := 0; i < 30; i++ {
		drive(t, a, key("{"))
	}
	if got := factoryDividerX(t, a); got != factoryRowsMin {
		t.Fatalf("the divider went to %d, under the rows' floor", got)
	}
	drive(t, a, key("|"))
	if got := factoryDividerX(t, a); got != start {
		t.Fatalf("| put the divider at %d, want %d", got, start)
	}
	// Under the peek's floor the keys move nothing and the rows are the width.
	a.width = 100
	drive(t, a, key("{"))
	if got := factoryDividerX(t, a); got != 100 {
		t.Fatalf("at 100 the rows are %d", got)
	}
}

// THE DIVIDER IS REMEMBERED PER HOME: it is written to factory.json beside the
// profile's config and read back on the next launch's first read.
func TestFactorySplitIsRemembered(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	factoryFrameLines(a)
	drive(t, a, key("}"), key("}"))
	moved := factoryDividerX(t, a)
	p := readFactoryPrefs(factoryPrefsPath(a.profileDir))
	if p.Split <= float64(factoryRowsShare) {
		t.Fatalf("factory.json holds %v after } twice", p.Split)
	}
	// The next launch: nothing chosen, nothing read yet.
	a.fp.split, a.fp.splitRead = 0, false
	drive(t, a, runCmd(a.factoryRead())...)
	if got := factoryDividerX(t, a); got != moved {
		t.Fatalf("the remembered divider came back at %d, want %d", got, moved)
	}
	drive(t, a, key("|"))
	if p := readFactoryPrefs(factoryPrefsPath(a.profileDir)); p.Split != 0 {
		t.Fatalf("| left factory.json at %v", p.Split)
	}
}

// THE POINTER DRAGS THE DIVIDER: a press on its column, motion with the button
// held, and the release; the release writes it down.
func TestFactorySplitMouseDrag(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	x := factoryDividerX(t, a)
	y := placeHeadRows + a.fp.headRows + 2
	cursor := a.fp.cursor
	drive(t, a, clickAt(x, y))
	if !a.fp.dragging || a.fp.cursor != cursor {
		t.Fatalf("a press on the divider did not start a drag (dragging %v, cursor %d from %d)", a.fp.dragging, a.fp.cursor, cursor)
	}
	drive(t, a, tea.MouseMotionMsg{X: x - 9, Y: y + 3, Button: tea.MouseLeft})
	if got := factoryDividerX(t, a); got != x-9 {
		t.Fatalf("the drag put the divider at %d, want %d", got, x-9)
	}
	drive(t, a, tea.MouseMotionMsg{X: 10, Y: y, Button: tea.MouseLeft})
	if got := factoryDividerX(t, a); got != factoryRowsMin {
		t.Fatalf("a drag past the floor put the divider at %d", got)
	}
	drive(t, a, tea.MouseMotionMsg{X: x + 5, Y: y, Button: tea.MouseLeft}, releaseAt(x+5, y))
	if a.fp.dragging {
		t.Fatal("the release did not end the drag")
	}
	if got := factoryDividerX(t, a); got != x+5 {
		t.Fatalf("the divider let go at %d, want %d", got, x+5)
	}
	if p := readFactoryPrefs(factoryPrefsPath(a.profileDir)); p.Split == 0 {
		t.Fatal("the release did not write the divider down")
	}
}

// A PRESS ON A ROW PUTS THE CURSOR THERE AND OPENS NOTHING; a second press on
// it opens the item; a press in the peek moves nothing; on the item page a
// press on a rail row selects it.
func TestFactoryClickMovesTheCursorAndDoubleOpens(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 44
	factoryFrameLines(a)
	from := a.fp.cursor
	y := -1
	for row := placeHeadRows; row < a.height; row++ {
		a.fp.cursor = from
		if a.factoryPress(row) && a.fp.cursor != from {
			y = row
			break
		}
	}
	if y < 0 {
		t.Fatal("no row of the floor holds another item")
	}
	a.fp.cursor = from
	want := func() int { a.factoryPress(y); c := a.fp.cursor; a.fp.cursor = from; return c }()
	drive(t, a, clickAt(a.fp.rowsW+10, y))
	if a.fp.cursor != from || a.fp.open {
		t.Fatalf("a press in the peek moved the cursor to %d or opened the page", a.fp.cursor)
	}
	a.clickAt = time.Time{}
	drive(t, a, clickAt(5, y), releaseAt(5, y))
	if a.fp.cursor != want || a.fp.open {
		t.Fatalf("one press: cursor %d want %d, open %v", a.fp.cursor, want, a.fp.open)
	}
	drive(t, a, clickAt(5, y), releaseAt(5, y))
	if !a.fp.open {
		t.Fatal("a second press on the row did not open it")
	}
	factoryFrameLines(a)
	a.clickAt = time.Time{}
	drive(t, a, clickAt(3, placeHeadRows+a.fp.railTop+2))
	if a.fp.stage != a.fp.railFirst+2 || !a.fp.open {
		t.Fatalf("a press on the rail's third row put the cursor at %d", a.fp.stage)
	}
}
