package tui3

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
	if got := strings.TrimSpace(ansi.Strip(rows[0])); got != "◆ 1 shipped · 4 arrived · ? 2 waiting · $11.31 / $60 · polled 14s ago" {
		t.Fatalf("the one line is %q", got)
	}
	if !strings.HasPrefix(rows[0], factoryMarginPad()+"◆") {
		t.Fatalf("the handover does not stand at the margin: %q", rows[0])
	}
	a.fp.snap.Shift.Shipped, a.fp.snap.Shift.Arrived = 0, 0
	if got := strings.TrimSpace(ansi.Strip(a.factoryHead(150)[0])); got != "◆ ? 2 waiting · $11.31 / $60 · polled 14s ago" {
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

// factoryFirstReadLab is a floor the moment repositories are saved: no item,
// no shift, no money, and github watching three repositories.
func factoryFirstReadLab(t *testing.T, shape func(*factory.SourceInfo)) *app {
	t.Helper()
	f := &factoryFake{shape: func(s *factory.Snapshot) {
		s.Items, s.Shift, s.Daily, s.Rail = nil, factory.Shift{}, 0, 0
		gh := factory.SourceInfo{Name: "github", Writes: true,
			Repos: []string{"Agent-Field/CodeAF", "Agent-Field/platform", "Agent-Field/whisper"}}
		shape(&gh)
		s.Sources = []factory.SourceInfo{gh, {Name: string(factory.OriginChat)}}
	}}
	a := factoryVerbLab(t, f)
	a.pal = newPalette(tokens.NoColor, false)
	return a
}

// THE FIRST READ IS SAID FROM THE MOMENT REPOSITORIES ARE SAVED (owner
// screenshot, 2026-10-08): the handover's one line is the read's own clause,
// spinning once and never `quiet`, and the bare floor says the read is out.
func TestFactoryFirstReadSaysWhereTheReadIs(t *testing.T) {
	a := factoryFirstReadLab(t, func(gh *factory.SourceInfo) {
		gh.Polling, gh.Reading, gh.Read, gh.Of, gh.Items = true, "Agent-Field/CodeAF", 1, 3, 200
	})
	spin := a.factorySpin()
	for _, width := range []int{150, 120, 100} {
		a.width = width
		frame := factoryFrameText(a)
		head := ""
		for _, row := range strings.Split(frame, "\n") {
			if strings.Contains(row, "reading Agent-Field/CodeAF") {
				head = row
				break
			}
		}
		if !strings.Contains(head, spin+" reading Agent-Field/CodeAF · 1 of 3 · 200 items so far") {
			t.Fatalf("at %d the handover does not say where the read is: %q\n%s", width, head, frame)
		}
		if strings.Count(head, spin) != 1 {
			t.Fatalf("at %d the handover spins more than once: %q", width, head)
		}
		if strings.Contains(frame, "quiet") {
			t.Fatalf("at %d a floor being read says quiet:\n%s", width, frame)
		}
		if !strings.Contains(strings.Join(strings.Fields(frame), " "), factoryBareReadingWords) {
			t.Fatalf("at %d the bare floor does not say the read is out:\n%s", width, frame)
		}
		for i, row := range strings.Split(frame, "\n") {
			if w := ansi.StringWidth(row); w > width {
				t.Fatalf("at %d row %d is %d cells: %q", width, i, w, row)
			}
		}
	}
	// The four-row strip carries the progress in the source's own clause,
	// and its shift row drops `quiet` while the read is out.
	a.width = 150
	a.fp.headFull = true
	rows := a.factoryHead(150)
	if facts := ansi.Strip(rows[3]); !strings.Contains(facts, "github · "+spin+" reading Agent-Field/CodeAF · 1 of 3") || strings.Count(facts, spin) != 1 {
		t.Fatalf("the facts row while reading is %q", facts)
	}
	if shift := strings.TrimSpace(ansi.Strip(rows[1])); shift != factoryHeadNothingWords {
		t.Fatalf("the shift row while reading is %q", shift)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != 150 {
			t.Errorf("row %d of the strip is %d cells: %q", i, got, ansi.Strip(r))
		}
	}
}

// A SOURCE POLLING WITHOUT SAYING WHERE keeps `github · ⠋ polling`, and the
// bare floor still says the read is out.
func TestFactoryFirstReadWithoutProgressSaysPolling(t *testing.T) {
	a := factoryFirstReadLab(t, func(gh *factory.SourceInfo) { gh.Polling = true })
	frame := factoryFrameText(a)
	if !strings.Contains(frame, "github · "+a.factorySpin()+" polling") || strings.Contains(frame, "quiet") {
		t.Fatalf("a poll with no progress reads:\n%s", frame)
	}
	if !strings.Contains(strings.Join(strings.Fields(frame), " "), factoryBareReadingWords) {
		t.Fatalf("the bare floor does not say the read is out:\n%s", frame)
	}
}

// A READ FLOOR WITH NOTHING OPEN says how many repositories it read and what
// brings work anyway, not what arrives from where.
func TestFactoryFirstReadDoneSaysNothingOpen(t *testing.T) {
	a := factoryFirstReadLab(t, func(gh *factory.SourceInfo) { gh.Polled = factoryTestNow.Add(-5 * time.Second) })
	text := strings.Join(strings.Fields(factoryFrameText(a)), " ")
	want := "nothing open in 3 repositories · github polls every minute · n adds work by hand"
	if factoryBareReadWords(3) != want || !strings.Contains(text, want) {
		t.Fatalf("a read floor with nothing open reads:\n%s", factoryFrameText(a))
	}
	if strings.Contains(text, factoryBareWords) || strings.Contains(text, factoryBareReadingWords) {
		t.Fatalf("a read floor says two of its sentences:\n%s", factoryFrameText(a))
	}

	// One repository is one repository.
	if got := factoryBareReadWords(1); !strings.HasPrefix(got, "nothing open in 1 repository ·") {
		t.Fatalf("one repository reads %q", got)
	}

	// Watched but never read and not reading: `nothing open` would be a guess.
	b := factoryFirstReadLab(t, func(*factory.SourceInfo) {})
	if text := strings.Join(strings.Fields(factoryFrameText(b)), " "); !strings.Contains(text, factoryBareWords) {
		t.Fatalf("a floor never read says:\n%s", factoryFrameText(b))
	}
}

// THE UNCONNECTED FLOOR IS UNCHANGED by the first read's sentences.
func TestFactoryFirstReadLeavesTheUnconnectedFloor(t *testing.T) {
	if factoryUnconnectedWords != "nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected" {
		t.Fatalf("the unconnected sentence moved: %q", factoryUnconnectedWords)
	}
	a := placeApp(t)
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	text := strings.Join(strings.Fields(factoryFrameText(a)), " ")
	if !strings.Contains(text, factoryUnconnectedWords) || strings.Contains(text, factoryBareReadingWords) {
		t.Fatalf("the unconnected floor reads:\n%s", factoryFrameText(a))
	}
}

// THE PICKER'S RECEIPT SAYS THE READ HAS BEGUN; how often github is read is
// the manual's and the read floor's, not the receipt's.
func TestFactoryWatchingWordsSayTheReadBegan(t *testing.T) {
	if got := factoryWatchingWords(3); got != "watching 3 repositories · reading them now" {
		t.Fatalf("the receipt is %q", got)
	}
}

// factorySaveLab is the settings fake over a floor with no item, no shift and
// no money, github watching what the fake says is watched and never polled,
// on a clock the test moves. src shapes github on every read.
func factorySaveLab(t *testing.T, src func(*factory.SourceInfo)) (*app, *factorySettingsFake, *time.Time) {
	t.Helper()
	f := newSettingsFake()
	f.factoryFake.shape = func(s *factory.Snapshot) {
		s.Items, s.Shift, s.Daily = nil, factory.Shift{}, 0
		f.mu.Lock()
		gh := factory.SourceInfo{Name: "github", Writes: true, Repos: append([]string(nil), f.watched...)}
		f.mu.Unlock()
		if src != nil {
			src(&gh)
		}
		s.Sources = []factory.SourceInfo{gh, {Name: string(factory.OriginChat)}}
	}
	a := factorySettingsLab(t, f, 150)
	a.linear = false
	a.pal = newPalette(tokens.NoColor, false)
	now := factoryTestNow
	a.clock = func() time.Time { return now }
	return a, f, &now
}

// factoryHeadRow is the frame's first row carrying words, plain.
func factoryHeadRow(a *app, words string) string {
	for _, row := range strings.Split(factoryFrameText(a), "\n") {
		if strings.Contains(row, words) {
			return row
		}
	}
	return ""
}

// FROM THE PICKER'S SAVE THE FLOOR SAYS IT IS READING (recording, 2026-10-08:
// one second after `enter` the foot said `reading them now` and the head above
// still said quiet). The head is the reading clause and nothing else, the bare
// line is the reading sentence, and the spinner turns.
func TestFactoryFirstReadFromTheSave(t *testing.T) {
	var polled time.Time
	a, f, now := factorySaveLab(t, func(gh *factory.SourceInfo) { gh.Polled = polled })
	if text := factoryFrameText(a); !strings.Contains(text, "quiet") {
		t.Fatalf("the lab's floor before the save is not quiet:\n%s", text)
	}
	drive(t, a, key("R"))
	drive(t, a, key("down"), key(" "), key("enter"))
	if got := strings.Join(f.said(), " "); got != "SetRepos(agentfield/codeaf agentfield/agentfield)" {
		t.Fatalf("enter asked %q", got)
	}
	if !a.fp.readingSince.Equal(*now) {
		t.Fatalf("the save stood no reading moment: %v", a.fp.readingSince)
	}
	frame := factoryFloorAgrees(t, a)
	head := factoryHeadRow(a, factoryFirstReadWords)
	if !strings.Contains(head, a.factorySpin()+" "+factoryFirstReadWords) || strings.Contains(frame, "quiet") {
		t.Fatalf("the frame after the save:\n%s", frame)
	}
	if strings.Contains(head, "polled") || strings.Contains(head, factoryHeadNothingWords) {
		t.Fatalf("the head after the save is not the reading clause alone: %q", head)
	}
	text := strings.Join(strings.Fields(frame), " ")
	if !strings.Contains(text, factoryBareReadingWords) || strings.Contains(text, factoryBareWords) {
		t.Fatalf("the bare line after the save:\n%s", frame)
	}
	if !a.factorySpinning() {
		t.Fatal("the floor reading from the save does not spin")
	}

	// The one-second beat reads while the moment stands; a stale save's beat
	// reads nothing.
	soon := a.factoryReadSoon(a.fp.readingGen)
	if soon == nil {
		t.Fatal("the reading moment's beat asked nothing")
	}
	spend(t, a, soon)
	if a.factoryReadSoon(a.fp.readingGen-1) != nil {
		t.Fatal("a stale save's beat asked something")
	}

	// A source that answered before the save says nothing about this read.
	polled = now.Add(-time.Second)
	spend(t, a, a.factoryRead())
	if a.fp.readingSince.IsZero() {
		t.Fatal("a poll from before the save cleared the reading moment")
	}
	// One that answered after it does.
	polled = now.Add(time.Second)
	spend(t, a, a.factoryRead())
	if !a.fp.readingSince.IsZero() {
		t.Fatal("a poll after the save left the reading moment standing")
	}
	if a.factoryReadSoon(a.fp.readingGen) != nil {
		t.Fatal("the beat went on after the read was known")
	}
}

// A SNAPSHOT THAT SAYS THE READ — a source mid-poll, or items on the floor —
// clears the moment, and the source's own clause or the rows take over.
func TestFactoryFirstReadClearsOnPollingOrItems(t *testing.T) {
	var polling bool
	a, f, _ := factorySaveLab(t, func(gh *factory.SourceInfo) { gh.Polling = polling })
	drive(t, a, key("R"))
	drive(t, a, key("enter"))
	if a.fp.readingSince.IsZero() {
		t.Fatal("the save stood no reading moment")
	}
	polling = true
	spend(t, a, a.factoryRead())
	if !a.fp.readingSince.IsZero() {
		t.Fatal("a source mid-poll left the reading moment standing")
	}
	if !strings.Contains(factoryFrameText(a), "github · "+a.factorySpin()+" polling") {
		t.Fatalf("the source's own clause did not take over:\n%s", factoryFrameText(a))
	}

	polling = false
	drive(t, a, key("R"))
	drive(t, a, key("enter"))
	if a.fp.readingSince.IsZero() {
		t.Fatal("the second save stood no reading moment")
	}
	shape := f.factoryFake.shape
	f.factoryFake.shape = func(s *factory.Snapshot) {
		shape(s)
		s.Items = factory.Fixture(factoryTestNow).Items
	}
	spend(t, a, a.factoryRead())
	if !a.fp.readingSince.IsZero() {
		t.Fatal("items on the floor left the reading moment standing")
	}
}

// factoryFloorAgrees is the frame-level law the floor's three lines keep
// after a save (owner screenshots, 2026-10-08 15:14 and 15:15): no frame says
// both `quiet` and `reading`; the foot's `reading them now` stands exactly
// when the head's spinning reading clause does; and the head's no-word clause
// stands exactly when the bare line's does. It answers the frame, plain.
func factoryFloorAgrees(t *testing.T, a *app) string {
	t.Helper()
	frame := factoryFrameText(a)
	if strings.Contains(frame, "quiet") && strings.Contains(frame, "reading") {
		t.Fatalf("one frame says both quiet and reading:\n%s", frame)
	}
	text := strings.Join(strings.Fields(frame), " ")
	foot := strings.Contains(text, factoryReadingNowWords)
	head := strings.Contains(text, a.factorySpin()+" "+factoryFirstReadWords)
	if foot != head {
		t.Fatalf("the foot says reading %v and the head %v:\n%s", foot, head, frame)
	}
	noHead := strings.Contains(text, factoryNoWordWords)
	noBare := strings.Contains(text, factoryBareNoWordWords)
	if noHead != noBare && (noHead || a.factoryBare()) {
		t.Fatalf("the head says no word %v and the bare line %v:\n%s", noHead, noBare, frame)
	}
	if noHead && (head || foot) {
		t.Fatalf("one frame says both no word and reading:\n%s", frame)
	}
	return frame
}

// A POLLER THAT NEVER COMES DOES NOT GO BACK TO QUIET (owner screenshots,
// 2026-10-08 15:14 and 15:15): past factoryFirstReadWait the spinner stops,
// the head says there is no word from the read, the bare line says what to
// do, and the foot's `reading them now` lapses in the same frame.
func TestFactoryFirstReadTurnsToNoWord(t *testing.T) {
	a, _, now := factorySaveLab(t, nil)
	factoryFloorAgrees(t, a)
	drive(t, a, key("R"))
	drive(t, a, key("enter"))
	if !a.factoryFirstReading() {
		t.Fatal("the save stood no reading moment")
	}
	if frame := factoryFloorAgrees(t, a); !strings.Contains(frame, factoryReadingNowWords) {
		t.Fatalf("the foot after the save does not say the read began:\n%s", frame)
	}
	*now = now.Add(factoryFirstReadWait - time.Second)
	if !a.factoryFirstReading() || a.factoryNoWord() {
		t.Fatal("the reading moment turned early")
	}
	factoryFloorAgrees(t, a)
	*now = now.Add(time.Second)
	if a.factoryFirstReading() || a.factorySpinning() || !a.factoryNoWord() {
		t.Fatal("past factoryFirstReadWait the floor is not at no word")
	}
	if a.factoryReadSoon(a.fp.readingGen) != nil {
		t.Fatal("the one-second beat went on past factoryFirstReadWait")
	}
	if a.fp.readingSince.IsZero() {
		t.Fatal("the wait passing cleared the read that was asked for")
	}
	frame := factoryFloorAgrees(t, a)
	head := factoryHeadRow(a, factoryNoWordWords)
	if !strings.Contains(head, "◆ "+factoryNoWordWords) || strings.Contains(head, a.factorySpin()) {
		t.Fatalf("the head at no word is %q:\n%s", head, frame)
	}
	text := strings.Join(strings.Fields(frame), " ")
	if !strings.Contains(text, factoryBareNoWordWords) || strings.Contains(text, factoryBareWords) {
		t.Fatalf("the bare line at no word:\n%s", frame)
	}
	if strings.Contains(frame, factoryReadingNowWords) || strings.Contains(frame, "quiet") {
		t.Fatalf("the frame at no word still says reading or quiet:\n%s", frame)
	}
	if note := a.factoryReadNote(); !strings.HasPrefix(note, "watching ") || strings.Contains(note, "reading") {
		t.Fatalf("the note at no word is %q", note)
	}
	// It stands: a minute later, with still no word, it says the same.
	*now = now.Add(time.Minute)
	spend(t, a, a.factoryRead())
	if !a.factoryNoWord() || !strings.Contains(factoryFloorAgrees(t, a), factoryNoWordWords) {
		t.Fatalf("no word lapsed with still no word:\n%s", factoryFrameText(a))
	}
}

// A SOURCE MID-POLL AFTER THE WAIT CLEARS NO WORD, and the source's own
// clause takes the head; a later answer clears it the same way.
func TestFactoryNoWordClearsOnPolling(t *testing.T) {
	var polling bool
	var polled time.Time
	a, _, now := factorySaveLab(t, func(gh *factory.SourceInfo) { gh.Polling, gh.Polled = polling, polled })
	drive(t, a, key("R"))
	drive(t, a, key("enter"))
	*now = now.Add(factoryFirstReadWait + time.Second)
	if !a.factoryNoWord() {
		t.Fatal("the floor is not at no word past the wait")
	}
	polling = true
	spend(t, a, a.factoryRead())
	if !a.fp.readingSince.IsZero() || a.factoryNoWord() || a.factoryReadNote() != "" {
		t.Fatal("a source mid-poll left no word standing")
	}
	frame := factoryFloorAgrees(t, a)
	if strings.Contains(frame, factoryNoWordWords) || !strings.Contains(frame, "github · "+a.factorySpin()+" polling") {
		t.Fatalf("the floor after the poll began:\n%s", frame)
	}

	// A later answer clears it too, and the floor reads as read.
	polling = false
	drive(t, a, key("R"))
	drive(t, a, key("enter"))
	*now = now.Add(factoryFirstReadWait + time.Second)
	if !a.factoryNoWord() {
		t.Fatal("the second save did not reach no word")
	}
	polled = *now
	spend(t, a, a.factoryRead())
	if a.factoryNoWord() || strings.Contains(factoryFloorAgrees(t, a), factoryNoWordWords) {
		t.Fatalf("an answer after the save left no word:\n%s", factoryFrameText(a))
	}
}

// A FAILING READ IS NEVER QUIET: on an empty floor the one-line head says the
// source's trouble as the facts row does, before the save, after the wait,
// and beside when it last answered.
func TestFactoryTroubleOnAnEmptyFloor(t *testing.T) {
	a := factoryFirstReadLab(t, func(gh *factory.SourceInfo) { gh.Trouble = "not reachable" })
	frame := factoryFloorAgrees(t, a)
	head := factoryHeadRow(a, "◆")
	if !strings.Contains(head, "◆ github · not reachable") || strings.Contains(frame, "quiet") {
		t.Fatalf("a failing read on an empty floor:\n%s", frame)
	}
	b := factoryFirstReadLab(t, func(gh *factory.SourceInfo) {
		gh.Trouble, gh.Polled = "not reachable", factoryTestNow.Add(-3*time.Minute)
	})
	if head := factoryHeadRow(b, "◆"); !strings.Contains(head, "◆ github · not reachable · polled 3m ago") {
		t.Fatalf("a failing read with an older answer: %q", head)
	}
	// The four-row strip's shift row drops quiet with it.
	b.fp.headFull = true
	if shift := strings.TrimSpace(ansi.Strip(b.factoryHead(150)[1])); shift != factoryHeadNothingWords {
		t.Fatalf("the shift row with a failing read is %q", shift)
	}

	c, _, now := factorySaveLab(t, func(gh *factory.SourceInfo) { gh.Trouble = "token refused" })
	drive(t, c, key("R"))
	drive(t, c, key("enter"))
	*now = now.Add(factoryFirstReadWait)
	factoryFloorAgrees(t, c)
	if head := factoryHeadRow(c, "◆"); !strings.Contains(head, "◆ "+factoryNoWordWords+" · github · token refused") {
		t.Fatalf("no word with a failing source: %q\n%s", head, factoryFrameText(c))
	}
}

// AN EMPTY SAVE OWES NO READ: `watching no repositories` stands nothing.
func TestFactoryFirstReadNotOnAnEmptySave(t *testing.T) {
	a, f, _ := factorySaveLab(t, nil)
	drive(t, a, key("R"))
	drive(t, a, key(" "), key("enter"))
	if got := strings.Join(f.said(), " "); got != "SetRepos()" {
		t.Fatalf("enter asked %q", got)
	}
	if !a.fp.readingSince.IsZero() || a.factorySpinning() {
		t.Fatal("an empty save stood a reading moment")
	}
	if strings.Contains(factoryFrameText(a), factoryFirstReadWords) {
		t.Fatalf("an empty save's floor says it is reading:\n%s", factoryFrameText(a))
	}
}
