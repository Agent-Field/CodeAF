package tui3

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── the polish wave's own tests (owner review of the demo, 2026-10-08) ─────

// factoryPolishLab is the verb lab with the forge's own doors on it: Open
// names a fixture item's page, Refresh marks the item busy until the test
// says the read is over, and RefreshAll prices the floor under a dry run and
// marks every item busy otherwise.
type factoryPolishLab struct {
	*factoryFake
	busy   map[int]string
	all    string
	polled bool
	opened []string
	real   int
}

func newFactoryPolishLab(t *testing.T) (*app, *factoryPolishLab) {
	t.Helper()
	lab := &factoryPolishLab{factoryFake: &factoryFake{}, busy: map[int]string{}}
	lab.shape = func(s *factory.Snapshot) {
		for id, w := range lab.busy {
			if s.Busy == nil {
				s.Busy = map[int]string{}
			}
			s.Busy[id] = w
		}
		s.BusyAll = lab.all
		s.LastReadCost = 0.0004
		if lab.polled {
			s.Sources[0].Polling = true
		}
	}
	a := factoryVerbLab(t, lab.factoryFake)
	seam := a.factory
	seam.Open = func(id int) (string, error) {
		for _, it := range factory.Fixture(factoryTestNow).Items {
			if it.ID == id && it.URL != "" {
				return it.URL, nil
			}
		}
		return "", factory.ErrNotOnSource
	}
	seam.Refresh = func(ctx context.Context, id int) error {
		lab.busy[id] = "refreshing"
		return lab.rec("Refresh", id)
	}
	seam.RefreshAll = func(ctx context.Context) (int, float64, error) {
		if factory.IsDryRun(ctx) {
			return 10, 0.004, nil
		}
		lab.real++
		return 10, 0.004, lab.rec("RefreshAll")
	}
	a.factory = seam
	was := processOpener
	processOpener = func(target string) error { lab.opened = append(lab.opened, target); return nil }
	t.Cleanup(func() { processOpener = was })
	return a, lab
}

// factoryLabRead folds a fresh read of the lab's seam in, as the beat would.
func factoryLabRead(t *testing.T, a *app) {
	t.Helper()
	snap, err := a.factory.Load()
	if err != nil {
		t.Fatal(err)
	}
	a.factoryFold(snap)
}

// factorySectionRefs is the walk's refs in one state, top to bottom.
func factorySectionRefs(a *app, st factory.State) string {
	var out []string
	for _, i := range a.factoryWalkNow() {
		if it := a.fp.snap.Items[i]; it.State == st {
			out = append(out, it.Ref())
		}
	}
	return strings.Join(out, ",")
}

// THE DEFAULT ORDER is priority, 1 first and unranked last, then the forge
// number descending; A RE-READ NEVER MOVES A ROW, an arrival slides in at the
// top of its section, a change of state moves the row to the top of its new
// one, and `O` sorts again.
func TestFactoryOrderIsStableAcrossReads(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	if got := factorySectionRefs(a, factory.StateNew); got != "ci,#1662,#1540,#31,#702" {
		t.Fatalf("the new section opens as %s", got)
	}
	// A triage update that would sort #31 first does not move it.
	lab.shape = func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == 6 {
				s.Items[i].Triage.Priority = 1
			}
		}
	}
	factoryLabRead(t, a)
	if got := factorySectionRefs(a, factory.StateNew); got != "ci,#1662,#1540,#31,#702" {
		t.Fatalf("a re-read moved a row: %s", got)
	}
	// An arrival, ranked last, still stands at the top of its section.
	lab.shape = func(s *factory.Snapshot) {
		s.Items = append(s.Items, factory.Item{ID: 11, Repo: "agentfield/codeaf", Num: 1700, Kind: factory.KindIssue, Title: "a new arrival", Origin: factory.OriginForge, State: factory.StateNew, Created: factoryTestNow, Triage: factory.Triage{Priority: 4}})
		for i := range s.Items {
			if s.Items[i].ID == 6 {
				s.Items[i].Triage.Priority = 1
			}
		}
	}
	factoryLabRead(t, a)
	if got := factorySectionRefs(a, factory.StateNew); got != "#1700,ci,#1662,#1540,#31,#702" {
		t.Fatalf("the arrival is not at the top: %s", got)
	}
	// `O` round the four orders back to priority sorts the section again.
	for range factoryOrders {
		drive(t, a, key("O"))
	}
	if got := factorySectionRefs(a, factory.StateNew); got != "#31,ci,#1662,#1540,#1700,#702" {
		t.Fatalf("O did not sort the section again: %s", got)
	}
}

// A ROW THAT CHANGES STATE moves to the top of its new section.
func TestFactoryStateChangeMovesTheRowToTheTop(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	lab.shape = func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == 6 {
				s.Items[i].State = factory.StateQueued
			}
		}
	}
	factoryLabRead(t, a)
	if got := factorySectionRefs(a, factory.StateQueued); !strings.HasPrefix(got, "#31") {
		t.Fatalf("the moved row is not at the top of streams: %s", got)
	}
}

// THE PRIORITY COLUMN is one cell after the lead: the first in the accent,
// the rest dim, nothing for an item nobody ranked.
func TestFactoryPriorityColumn(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.columns = true
	for _, c := range []struct {
		id   int
		want string
	}{
		{5, a.pal.accent(a.icon(tokens.GPriorityFirst))},
		{4, a.pal.dim(a.icon(tokens.GPrioritySecond))},
		{8, a.pal.dim(a.icon(tokens.GPriorityThird))},
		{6, a.pal.dim(a.icon(tokens.GPriorityFourth))},
	} {
		it := *factoryPaneItem(t, a, c.id)
		if got := a.factoryPrioCell(it); got != c.want {
			t.Errorf("%s's priority cell is %q, want %q", it.Ref(), got, c.want)
		}
	}
	if got := a.factoryPrioCell(*factoryPaneItem(t, a, 7)); got != " " {
		t.Errorf("an unranked item draws %q in its priority cell", got)
	}
	row := ansi.Strip(a.factoryRailItem(*factoryPaneItem(t, a, 5), 150, false))
	if got := []rune(row)[factoryLeadW]; string(got) != a.icon(tokens.GPriorityFirst) {
		t.Fatalf("the priority is not one cell after the lead: %q", row)
	}
}

// THE CURSOR ROW wears the cursor step, as the Teams page's cursor row and
// home's lists do, and nothing else changes on it.
func TestFactoryCursorRowWearsTheCursorGround(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.columns = true
	it := *factoryPaneItem(t, a, 2)
	if got, want := a.factoryRailItem(it, 150, true), a.pal.cursorRow(a.factoryRailItem(it, 150, false), 150); got != want {
		t.Fatalf("the cursor row is\n%q\nwant\n%q", got, want)
	}
}

// THE HANDOVER IS ONE LINE AND A BLANK, its zero clauses absent; `h` brings
// the four rows back and the choice is written beside the split.
func TestFactoryHandoverOneLine(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	rows := a.factoryHead(150)
	if len(rows) != 2 || strings.TrimSpace(rows[1]) != "" {
		t.Fatalf("the handover is %d rows: %q", len(rows), rows)
	}
	if got := strings.TrimSpace(ansi.Strip(rows[0])); got != "◆ 1 shipped · 4 arrived · ? 1 waiting · $11.31 / $60 · polled 14s ago" {
		t.Fatalf("the one line is %q", got)
	}
	if !strings.HasPrefix(rows[0], factoryMarginPad()+"◆") {
		t.Fatalf("the handover does not stand at the margin: %q", rows[0])
	}
	a.fp.snap.Shift.Shipped, a.fp.snap.Shift.Arrived = 0, 0
	if got := strings.TrimSpace(ansi.Strip(a.factoryHead(150)[0])); got != "◆ ? 1 waiting · $11.31 / $60 · polled 14s ago" {
		t.Fatalf("a zero clause is drawn: %q", got)
	}
	a.width, a.height = 150, 40
	drive(t, a, key("h"))
	if !a.fp.headFull || len(a.factoryHead(150)) != 5 {
		t.Fatal("h did not bring the four rows back")
	}
	raw, err := os.ReadFile(factoryPrefsPath(a.profileDir))
	if err != nil || !strings.Contains(string(raw), `"handover":true`) {
		t.Fatalf("h was not remembered: %s %v", raw, err)
	}
	if p := readFactoryPrefs(factoryPrefsPath(a.profileDir)); !p.Handover {
		t.Fatal("the remembered handover does not read back")
	}
}

// WHILE A SOURCE POLLS the freshness clause spins in its place, and says when
// it last answered after; a whole-floor re-read spins with its own words.
func TestFactoryHandoverPollingClause(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.fp.snap.Sources[0].Polling = true
	line := ansi.Strip(a.factoryHead(150)[0])
	if !strings.Contains(line, "github · "+a.factorySpin()+" polling") || strings.Contains(line, "polled") {
		t.Fatalf("the polling clause is %q", line)
	}
	a.fp.headFull = true
	if facts := ansi.Strip(a.factoryHead(150)[3]); !strings.Contains(facts, "github · "+a.factorySpin()+" polling") || strings.Contains(facts, "polled") {
		t.Fatalf("the facts row while polling is %q", facts)
	}
	a.fp.headFull = false
	a.fp.snap.Sources[0].Polling = false
	a.fp.snap.BusyAll = "refreshing 8 items · 3 done"
	if line := ansi.Strip(a.factoryHead(150)[0]); !strings.Contains(line, a.factorySpin()+" refreshing 8 items · 3 done") {
		t.Fatalf("the whole-floor re-read is not said: %q", line)
	}
}

// A BUSY ITEM spins in its priority cell and its last fact says what is being
// done, dim; the peek's read says `reading…` while the read is empty.
func TestFactoryBusyItemSpins(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.columns = true
	a.fp.snap.Busy = map[int]string{6: "reading"}
	factoryPaneItem(t, a, 6).Triage.Read = ""
	it := *factoryPaneItem(t, a, 6)
	if got := a.factoryPrioCell(it); got != a.pal.accent(a.factorySpin()) {
		t.Fatalf("the busy row's priority cell is %q", got)
	}
	row := ansi.Strip(a.factoryRailItem(it, 150, false))
	if !strings.Contains(row, "reading…") {
		t.Fatalf("the busy row does not say it is reading: %q", row)
	}
	facts := a.factoryRowFacts(it)
	if last := facts[len(facts)-1]; last.painted != a.pal.dim("reading…") {
		t.Fatalf("the busy fact is not the last, dim: %q", last.painted)
	}
	if got := ansi.Strip(a.factoryPeekRead(it, 60)[0]); got != a.factorySpin()+" reading…" {
		t.Fatalf("the peek's read is %q", got)
	}
	a.fp.snap.Busy = nil
	if got := a.factoryPrioCell(it); got != a.pal.dim(a.icon(tokens.GPriorityFourth)) {
		t.Fatalf("a row at rest still spins: %q", got)
	}
}

// `u` READS THE ITEM AGAIN: the note line says so until the floor stops
// saying the item is busy, and then what the read cost.
func TestFactoryRereadNoteSequence(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	factoryOn(t, a, 6)
	drive(t, a, key("u"))
	if got := strings.Join(lab.said(), " "); got != "Refresh(6)" {
		t.Fatalf("u asked %q", got)
	}
	if note := ansi.Strip(strings.Join((placeFactory{}).note(a, 150), "")); !strings.Contains(note, "re-reading #31…") {
		t.Fatalf("the note line says %q", note)
	}
	delete(lab.busy, 6)
	factoryLabRead(t, a)
	if a.pageMsg != "#31 read again · ~$0.0004" {
		t.Fatalf("the read's end says %q", a.pageMsg)
	}
	if note := (placeFactory{}).note(a, 150); len(note) != 0 {
		t.Fatalf("the note line still says %q", note)
	}
}

// `U` ASKS FIRST, with the dry run's count and cost, where the typing row
// stands; `n` asks nothing, `y` reads every item.
func TestFactoryRereadAllAsksFirst(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	drive(t, a, key("U"))
	if a.fp.act.refresh == nil {
		t.Fatal("U put no question")
	}
	if text := factoryFrameText(a); !strings.Contains(text, "re-read 10 items · ~$0.004? [y] go · [n] not now") {
		t.Fatalf("the question is not drawn:\n%s", text)
	}
	if hint := (placeFactory{}).hint(a); hint != "y go · n not now" {
		t.Fatalf("the hint is %q", hint)
	}
	drive(t, a, key("n"))
	if a.fp.act.refresh != nil || lab.real != 0 {
		t.Fatal("n did not put the question away, or read anyway")
	}
	drive(t, a, key("U"))
	drive(t, a, key("y"))
	if lab.real != 1 {
		t.Fatalf("y read the floor %d times", lab.real)
	}
}

// A DOOR A KEY ASKED says on the note line what it is doing, with the
// spinner, until it answers.
func TestFactoryDoorsSayTheyAreWorking(t *testing.T) {
	a, _ := newFactoryPolishLab(t)
	a.factory.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) { return nil, nil, nil }
	cmd := a.factoryOpenRepos()
	if a.fp.act.doing == "" {
		t.Fatal("R says nothing while it asks")
	}
	note := ansi.Strip(strings.Join((placeFactory{}).note(a, 150), ""))
	if !strings.Contains(note, a.factorySpin()+" asking") {
		t.Fatalf("the note line while R asks is %q", note)
	}
	drive(t, a, runCmd(cmd)...)
	if a.fp.act.doing != "" {
		t.Fatalf("the note line still says %q after the door answered", a.fp.act.doing)
	}
	a.fp.pick = nil
	factoryOn(t, a, 8)
	drive(t, a, key("b"))
	if a.fp.act.doing != "" || a.pageMsg != "these stages are codeaf's recipe now" {
		t.Fatalf("b ended on %q / %q", a.fp.act.doing, a.pageMsg)
	}
	a.factory.Talk = func(context.Context, int) (string, error) { return "", os.ErrNotExist }
	factoryOn(t, a, 1)
	_ = a.factoryTalk(*factoryPaneItem(t, a, 1))
	if a.fp.act.doing != "opening #1538's conversation…" {
		t.Fatalf("T says %q while it opens", a.fp.act.doing)
	}
}

// THE BODY IS MARKDOWN, RENDERED: no raw backticks, a heading's words, a link
// as its words; the comments are rendered the same way.
func TestFactoryBodyIsRenderedMarkdown(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 4)
	it.Body = "## Claims\n\nThe fix is in `media.go`, see [the issue](https://example.com/x)."
	lines := ansi.Strip(strings.Join(a.factoryMarkdown(it.Body, factoryProseW), "\n"))
	for _, bad := range []string{"`", "##", "example.com"} {
		if strings.Contains(lines, bad) {
			t.Fatalf("the rendered body carries %q:\n%s", bad, lines)
		}
	}
	if !strings.Contains(lines, "media.go") || !strings.Contains(lines, "the issue") {
		t.Fatalf("the rendered body lost its words:\n%s", lines)
	}
	comments := ansi.Strip(strings.Join(a.factoryCommentsBlock(*factoryPaneItem(t, a, 6), factoryProseW, 0), "\n"))
	for _, want := range []string{"comments", "santosh · 2h", "Which build? codeaf --version prints it.", "olu · 1h"} {
		if !strings.Contains(comments, want) {
			t.Fatalf("the comments block is missing %q:\n%s", want, comments)
		}
	}
	if strings.Contains(comments, "`") || strings.Contains(comments, "*") {
		t.Fatalf("a comment is drawn raw:\n%s", comments)
	}
}

// THE FORGE'S BLOCKS stand after the body in their order, each absent when
// it has nothing to say.
func TestFactoryForgeBlocks(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	it := *factoryPaneItem(t, a, 4)
	var heads []string
	for _, b := range a.factoryForgeBlocks(it, factoryProseW, 0) {
		if len(b) > 0 {
			heads = append(heads, ansi.Strip(b[0]))
		}
	}
	if got := strings.Join(heads, ","); got != "comments,checks,files,activity" {
		t.Fatalf("the blocks are %s", got)
	}
	files := ansi.Strip(strings.Join(a.factoryFilesBlock(it, factoryProseW), "\n"))
	for _, want := range []string{"+218 " + a.icon(tokens.GDiffDel) + "44 · 6 files", "internal/tui3/media.go", "+120 " + a.icon(tokens.GDiffDel) + "30"} {
		if !strings.Contains(files, want) {
			t.Fatalf("the files block is missing %q:\n%s", want, files)
		}
	}
	bare := *factoryPaneItem(t, a, 8)
	for i, b := range a.factoryForgeBlocks(bare, factoryProseW, 0) {
		if len(b) != 0 {
			t.Fatalf("an item the forge knows nothing about drew block %d: %q", i, b)
		}
	}
}

// AN ITEM WITH A PAGE ON THE FORGE says `github ↗` at the end of its meta, and
// `g` opens the page through the seam's Open door.
func TestFactoryGOpensTheForgePage(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	factoryOn(t, a, 4)
	title := ansi.Strip(strings.Join(a.factoryPeekTitle(*factoryPaneItem(t, a, 4), 60), "\n"))
	if !strings.HasSuffix(title, "github "+a.icon(tokens.GActionBrowse)) {
		t.Fatalf("the meta does not end github ↗: %q", title)
	}
	drive(t, a, key("g"))
	if len(lab.opened) != 1 || lab.opened[0] != "https://github.com/agentfield/codeaf/pull/1662" {
		t.Fatalf("g opened %q", lab.opened)
	}
	if a.pageMsg != "opened #1662 on github" {
		t.Fatalf("the note is %q", a.pageMsg)
	}
	// On an item that lives only here `g` keeps its sync meaning.
	factoryOn(t, a, 8)
	drive(t, a, key("g"))
	if got := strings.Join(lab.said(), " "); got != "Sync(8,true)" || len(lab.opened) != 1 {
		t.Fatalf("g on a terminal item asked %q and opened %q", got, lab.opened)
	}
	// With no door, no key and no clause.
	a.factory.Open = nil
	factoryOn(t, a, 4)
	if strings.Contains((placeFactory{}).hint(a), "g github") {
		t.Fatal("the hint names g with no door")
	}
}

// THE REF IS A HYPERLINK where the terminal takes them, and takes no cells.
func TestFactoryRefIsAHyperlink(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pathLinks = true
	a.fp.columns = true
	it := *factoryPaneItem(t, a, 4)
	row := a.factoryRailItem(it, 150, false)
	if !strings.Contains(row, linkOpen(it.URL)) {
		t.Fatalf("the ref is not a link: %q", row)
	}
	if ansi.StringWidth(row) != 150 {
		t.Fatalf("the link took cells: %d", ansi.StringWidth(row))
	}
}

// THE ITEM PAGE'S HEAD IS A TRAIL OF CRUMBS, and the recipe page's too.
func TestFactoryBreadcrumbs(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 30
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	rows := factoryBodyPlain(a, 150, 30)
	if !strings.HasPrefix(rows[0], factoryMarginPad()+"Factory › codeaf › #1551 filters lost on compact") || !strings.HasSuffix(strings.TrimRight(rows[0], " "), "running 26m  $1.42 / $5") {
		t.Fatalf("the crumbs row is %q", rows[0])
	}
	drive(t, a, key("esc"))
	if a.fp.open {
		t.Fatal("esc did not climb to the floor")
	}
}

// `n`'S TYPING ROW stands under the rows, at their width, not under the peek.
func TestFactoryNewRowStandsUnderTheRows(t *testing.T) {
	a, _ := newFactoryPolishLab(t)
	factoryOn(t, a, 8)
	drive(t, a, key("n"))
	var row string
	for _, r := range factoryBodyPlain(a, 150, 40) {
		if strings.Contains(r, "new work ›") {
			row = r
		}
	}
	at, rule := strings.Index(row, "new work ›"), strings.Index(row, "│")
	if row == "" || rule < 0 || at > rule {
		t.Fatalf("the new-work row is not left of the divider: %q", row)
	}
}

// COMFORTABLE WRAPS A LONG TITLE TO A SECOND LINE, which the compact density
// never does.
func TestFactoryComfortableTitlesWrap(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 100, 60
	factoryPaneItem(t, a, 3).Title = "standing order ignores a paste with newlines and then the whole order is dropped on the floor"
	drive(t, a, key("z"))
	rows := factoryBodyPlain(a, 100, 60)
	text := strings.Join(rows, "\n")
	second := wrap(factoryPaneItem(t, a, 3).Title, a.fp.titleCols)
	at := -1
	for i, r := range rows {
		if strings.Contains(r, "#1660") {
			at = i
		}
	}
	if a.fp.titleCols <= 0 || len(second) < 2 || at < 0 || at+1 >= len(rows) || !strings.HasPrefix(strings.TrimSpace(rows[at+1]), strings.Fields(second[1])[0]) {
		t.Fatalf("the long title has no second line:\n%s", text)
	}
	drive(t, a, key("z"))
	if text := strings.Join(factoryBodyPlain(a, 100, 60), "\n"); strings.Contains(text, "dropped on the floor") {
		t.Fatalf("the compact density wrapped a title:\n%s", text)
	}
}
