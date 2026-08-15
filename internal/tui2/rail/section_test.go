package rail

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The unified rail: two sections in one column, a real tree under the work
// section, and the collapsed handle.
//
// What these tests are about is the three facts the rest of the package has to
// keep straight now that one scope holds two lists: a heading is chrome and
// never a place, a limb wears a connector and a conversation does not, and the
// tree's grammar is the same shape whether it hangs off the surface row inside a
// job or off a card at home.

// sectioned is the home rail this wave draws: a surface, a threads section with
// two conversations and its door, a work section with one live job whose parts
// hang under it, and a settled job that does not expand.
func sectioned() Scope {
	return Scope{
		ID:    HomeScopeID,
		Title: "aforge",
		Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge", Composer: ComposerChat},
			{ID: "section:threads", Kind: RowSection, Name: "threads"},
			{ID: "room:a", Kind: RowThread, Name: "the wisp parity push",
				Status: "you are here", When: "2h", Composer: ComposerChat},
			{ID: "room:b", Kind: RowThread, Name: "importer rewrite",
				Status: "parked on the schema question", When: "yesterday",
				Unseen: true, Composer: ComposerChat},
			{ID: "new-room", Kind: RowThread, Name: "+ new"},
			{ID: "section:work", Kind: RowSection, Name: "work"},
			{ID: idWisp, Kind: RowTask, Name: "wisp-parity", Life: LifeWorking,
				Composer: ComposerChat, Seed: idWisp},
			{ID: idH2, Kind: RowStep, Name: "H2", Depth: 1, Tree: true,
				Life: LifeWorking, Seed: idWisp},
			{ID: idH2 + "b", Kind: RowWorker, Name: "KeyCutter", Depth: 1, Tree: true,
				Seed: idWisp},
			{ID: idPerf, Kind: RowTask, Name: "perf-audit", Life: LifeSettled, Seed: idPerf},
			{ID: "homes", Kind: RowStep, Name: "more", Depth: 0},
		},
	}
}

func sectionedModel() *Model {
	src := &fakeSource{scopes: map[string]Scope{}}
	src.set(sectioned())
	return New(src)
}

// A HEADING IS NOT A PLACE. j crossing the seam between two sections lands on
// the first row of the next one, in both directions, and a caller that points
// straight at a heading is snapped off it.
func TestASectionRowIsNeverACursorStop(t *testing.T) {
	m := sectionedModel()
	rows := m.Rows()

	// Down from the surface: over `threads`, onto the first conversation.
	m.Move(1)
	if got := rows[m.Cursor()].Name; got != "the wisp parity push" {
		t.Fatalf("one step down landed on %q", got)
	}
	// Down over the `work` heading, from the door.
	m.Select(4)
	m.Move(1)
	if got := rows[m.Cursor()].Name; got != "wisp-parity" {
		t.Fatalf("crossing the work heading landed on %q", got)
	}
	// And back up over it.
	m.Move(-1)
	if got := rows[m.Cursor()].Name; got != "+ new" {
		t.Fatalf("crossing the work heading upwards landed on %q", got)
	}

	// Pointing at a heading directly — a click, a restored selection, a digit
	// that miscounted — snaps FORWARD, onto the rows the heading describes.
	for _, at := range []int{1, 5} {
		m.Select(0)
		m.Select(at)
		if !rows[m.Cursor()].Kind.Selectable() {
			t.Fatalf("selecting row %d left the cursor on chrome", at)
		}
		if m.Cursor() != at+1 {
			t.Fatalf("selecting heading %d snapped to %d, want the row under it", at, m.Cursor())
		}
	}
}

// Every gesture agrees about it, because they all ask one question: a refresh
// that lost the row under the cursor may not park it on a heading either.
func TestARefreshNeverParksTheCursorOnAHeading(t *testing.T) {
	src := &fakeSource{scopes: map[string]Scope{}}
	src.set(sectioned())
	m := New(src)
	m.Select(2) // the first conversation, directly under the threads heading

	gone := sectioned()
	gone.Rows = append(gone.Rows[:2], gone.Rows[3:]...) // that conversation retires
	src.set(gone)
	m.Refresh()
	if !m.Rows()[m.Cursor()].Kind.Selectable() {
		t.Fatalf("the refresh parked the cursor on %v", m.Rows()[m.Cursor()].Kind)
	}
}

// The tree hangs off the CARD at home and off the SURFACE row inside a job, and
// it is the same grammar at both altitudes: one connector column, not two.
func TestTheTreeIsTheSameShapeAtBothAltitudes(t *testing.T) {
	v := NewView(nil)
	m := sectionedModel()
	home := strings.Join(v.Rail(m, 40, 30), "\n")

	if !strings.Contains(home, tokens.GlyphTreeBranch) || !strings.Contains(home, tokens.GlyphTreeLast) {
		t.Fatalf("the home rail drew no connectors:\n%s", home)
	}
	branch := glyphColumn(t, home, tokens.GlyphTreeBranch)
	last := glyphColumn(t, home, tokens.GlyphTreeLast)
	if branch != last {
		t.Fatalf("the two connectors start in different columns: %d and %d", branch, last)
	}

	// The same rows inside a job scope: depth 0 there, depth 1 at home, and the
	// branch has to land in the same column either way.
	src := &fakeSource{scopes: map[string]Scope{}}
	src.set(Scope{ID: HomeScopeID, Title: "aforge", Rows: []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		{ID: idWisp, Kind: RowTask, Name: "wisp-parity", Life: LifeWorking},
	}})
	src.set(Scope{ID: idWisp, Title: "wisp-parity", Seed: idWisp, Rows: []Row{
		{ID: idWisp, Kind: RowSurface, Name: "orchestrator"},
		{ID: idH2, Kind: RowStep, Name: "H2", Life: LifeWorking},
		{ID: idH2 + "b", Kind: RowWorker, Name: "KeyCutter"},
	}})
	inside := New(src)
	inside.Select(1)
	inside.Enter()
	room := strings.Join(v.Rail(inside, 40, 30), "\n")
	if got := glyphColumn(t, room, tokens.GlyphTreeBranch); got != branch {
		t.Fatalf("the job room's branch is in column %d and the home rail's in %d", got, branch)
	}
}

// A CONVERSATION IS NOT A LIMB. The threads section is a flat list and the
// homes group's own rows sit at the same depth a plan step does, so a tree
// drawn from depth alone would grow branches over both.
func TestOnlyLimbsWearConnectors(t *testing.T) {
	v := NewView(nil)
	lines := v.Rail(sectionedModel(), 40, 30)
	for _, line := range lines {
		if !strings.Contains(line, tokens.GlyphTreeBranch) && !strings.Contains(line, tokens.GlyphTreeLast) {
			continue
		}
		if !strings.Contains(line, "H2") && !strings.Contains(line, "KeyCutter") {
			t.Fatalf("a connector was drawn on a row that is not a limb: %q", line)
		}
	}
}

// The last limb of a job wears the corner even when something at the same DEPTH
// follows it further down the column. The wall is what makes that true: a row
// with no connector ends the tree above it.
func TestALaterRowAtTheSameDepthDoesNotStealTheCorner(t *testing.T) {
	scope := sectioned()
	// A second job with its own single part, below the first job's two.
	scope.Rows = append(scope.Rows,
		Row{ID: "job2", Kind: RowTask, Name: "second", Life: LifeWorking, Seed: "job2"},
		Row{ID: "job2/a", Kind: RowStep, Name: "only part", Depth: 1, Tree: true, Seed: "job2"})
	src := &fakeSource{scopes: map[string]Scope{}}
	src.set(scope)

	v := NewView(nil)
	lines := v.Rail(New(src), 44, 40)
	corners := 0
	for _, line := range lines {
		if strings.Contains(line, tokens.GlyphTreeLast) {
			corners++
		}
	}
	// One per job: KeyCutter closes the first plan, `only part` closes the
	// second. A single corner would mean the second job's limb had been read as
	// a sibling of the first job's.
	if corners != 2 {
		t.Fatalf("the column drew %d corners, want one per job:\n%s", corners, strings.Join(lines, "\n"))
	}
}

// A thread row's anatomy: the name, the relative time flush right, the left-at
// line one tier down, and the ornament column reserved on every row so the names
// line up whether or not the dot is lit.
func TestAThreadRowDrawsItsLeftAtLineAndItsTime(t *testing.T) {
	v := NewView(nil)
	lines := v.Rail(sectionedModel(), 40, 30)
	frame := strings.Join(lines, "\n")
	if !strings.Contains(frame, "parked on the schema question") {
		t.Fatalf("the left-at line is not drawn:\n%s", frame)
	}
	if !strings.Contains(frame, "yesterday") || !strings.Contains(frame, "2h") {
		t.Fatalf("the relative time is not drawn:\n%s", frame)
	}
	dotted := lineWith(t, lines, "importer rewrite")
	plain := lineWith(t, lines, "the wisp parity push")
	if wordColumn(dotted, "importer") != wordColumn(plain, "the wisp") {
		t.Fatalf("the dot pushed its own row's name out of line:\n%q\n%q", dotted, plain)
	}
	// ONE ORNAMENT MEANS ONE. Row 0 is excluded because the room dot beside
	// `aforge` is the same character spent on a different sentence (5.15's "which
	// room am I in"), and it is drawn whatever the threads below it are doing.
	ornaments := 0
	for _, line := range lines[1:] {
		ornaments += strings.Count(line, tokens.GlyphStepDone)
	}
	if ornaments != 1 {
		t.Fatalf("the column carries %d ornaments, want the one unseen delivery:\n%s",
			ornaments, frame)
	}
}

// The quiet state is composed, not broken: one faint line where the rows would
// have been, and no glyph — ○ on an absence would claim something is pending.
func TestAnEmptySectionSaysSoWithoutLookingLikeAnError(t *testing.T) {
	src := &fakeSource{scopes: map[string]Scope{}}
	src.set(Scope{ID: HomeScopeID, Title: "aforge", Rows: []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		{ID: "section:work", Kind: RowSection, Name: "work"},
		{ID: "note:work", Kind: RowNote, Name: "nothing running"},
	}})
	v := NewView(nil)
	lines := v.Rail(New(src), 40, 20)
	note := lineWith(t, lines, "nothing running")
	for _, glyph := range []string{tokens.GlyphQueued, tokens.GlyphWorking, tokens.GlyphFailed} {
		if strings.Contains(note, glyph) {
			t.Fatalf("the quiet line carries a state glyph: %q", note)
		}
	}
	if m := New(src); m.Cursor() != 0 || !m.Rows()[m.Cursor()].Kind.Selectable() {
		t.Fatalf("a rail with nothing in it has no place to stand: cursor %d", m.Cursor())
	}
}

// The handle: exactly height lines, one column, and one signal.
func TestTheHandleCarriesTheDotAndNothingElse(t *testing.T) {
	v := NewView(nil)
	lit := v.Handle(true, 1, 6)
	if len(lit) != 6 {
		t.Fatalf("the handle drew %d lines, want 6", len(lit))
	}
	if strings.TrimSpace(lit[0]) != tokens.GlyphStepDone {
		t.Fatalf("the handle's first line is %q", lit[0])
	}
	for _, line := range lit[1:] {
		if strings.TrimSpace(line) != "" {
			t.Fatalf("the handle carries a second signal: %q", line)
		}
	}

	// Copy it out before the next render reuses the buffer, then compare.
	quiet := append([]string(nil), v.Handle(false, 1, 6)...)
	for _, line := range quiet {
		if strings.TrimSpace(line) != "" {
			t.Fatalf("a handle with nothing unseen drew %q", line)
		}
	}
}

// Scope.Unseen is what the handle asks, and it is a question about the WHOLE
// scope because one cell has no room to say which row.
func TestScopeUnseenAnswersForTheWholeColumn(t *testing.T) {
	scope := sectioned().normalize()
	if !scope.Unseen() {
		t.Fatal("a scope with an unseen delivery in it says nothing landed")
	}
	for i := range scope.Rows {
		scope.Rows[i].Unseen = false
	}
	if scope.Unseen() {
		t.Fatal("a scope with nothing unseen still claims a delivery")
	}
}

// THE RESORT BARRIER (7.2's other half). While the column is open the order is
// frozen; the moment it comes back the source's own order is taken, once.
func TestResortTakesTheSourcesOrderExactlyOnce(t *testing.T) {
	src := &fakeSource{scopes: map[string]Scope{}}
	src.set(sectioned())
	m := New(src)

	// The source re-sorts: the settled job floats above the live one.
	shuffled := sectioned()
	shuffled.Rows[6], shuffled.Rows[9] = shuffled.Rows[9], shuffled.Rows[6]
	src.set(shuffled)

	m.Refresh()
	if got := m.Rows()[6].Name; got != "wisp-parity" {
		t.Fatalf("a refresh re-sorted the rows under the reader: row 6 is %q", got)
	}

	m.Resort()
	m.Refresh()
	if got := m.Rows()[6].Name; got != "perf-audit" {
		t.Fatalf("the resort barrier did not take the source's order: row 6 is %q", got)
	}

	// And it is spent. The next refresh freezes again, or the barrier would be
	// a policy rather than a moment.
	src.set(sectioned())
	m.Refresh()
	if got := m.Rows()[6].Name; got != "perf-audit" {
		t.Fatalf("the barrier outlived its own refresh: row 6 is %q", got)
	}
}

// glyphColumn is the printable column a glyph was drawn in, on the first line
// that carries it.
func glyphColumn(t *testing.T, frame, glyph string) int {
	t.Helper()
	for _, line := range strings.Split(frame, "\n") {
		if at := wordColumn(line, glyph); at >= 0 {
			return at
		}
	}
	t.Fatalf("no line carries %q:\n%s", glyph, frame)
	return -1
}

// wordColumn is the printable COLUMN a word starts in, which is not its byte
// offset: the connectors and the ornament are multi-byte, so a byte comparison
// would report two aligned rows as crooked.
func wordColumn(line, word string) int {
	at := strings.Index(line, word)
	if at < 0 {
		return -1
	}
	return blocks.Width(line[:at])
}

// lineWith is the first rendered line containing a word.
func lineWith(t *testing.T, lines []string, word string) string {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, word) {
			return line
		}
	}
	t.Fatalf("no line carries %q:\n%s", word, strings.Join(lines, "\n"))
	return ""
}
