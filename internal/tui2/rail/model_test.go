package rail

import "testing"

func TestNewLoadsHomeAndParksOnTheSurface(t *testing.T) {
	m := New(scene())
	if got := m.Cursor(); got != 0 {
		t.Fatalf("cursor = %d, want 0", got)
	}
	if got := m.Scope().ID; got != HomeScopeID {
		t.Fatalf("scope = %q, want home", got)
	}
	if got := m.Rows()[0].Kind; got != RowSurface {
		t.Fatalf("row 0 kind = %v, want surface", got)
	}
	if got := m.Depth(); got != 0 {
		t.Fatalf("depth = %d, want 0", got)
	}
	if got := m.Len(); got != 4 {
		t.Fatalf("len = %d, want 4", got)
	}
}

func TestNewSurvivesASourceThatCannotAnswer(t *testing.T) {
	for name, src := range map[string]ScopeSource{
		"nil":   nil,
		"empty": &fakeSource{scopes: map[string]Scope{}},
	} {
		t.Run(name, func(t *testing.T) {
			m := New(src)
			if m.Len() != 1 || m.Rows()[0].Kind != RowSurface {
				t.Fatalf("empty source did not yield a lone surface row: %+v", m.Rows())
			}
			if lines := plainView().Render(m, ModeRail, 28, 10); len(lines) != 1 {
				t.Fatalf("render = %d lines, want 1", len(lines))
			}
		})
	}
}

func TestScopeNormalisationSuppliesAMissingSurfaceRow(t *testing.T) {
	src := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "a", Kind: RowTask, Name: "alpha"},
		}},
	}}
	m := New(src)
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2 (synthesised surface + the task)", m.Len())
	}
	if r := m.Rows()[0]; r.Kind != RowSurface || r.Name != "aforge" || r.Composer != ComposerChat {
		t.Fatalf("synthesised surface = %+v", r)
	}
}

func TestScopeNormalisationClampsIndent(t *testing.T) {
	src := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "s", Kind: RowSurface, Name: "aforge"},
			{ID: "a", Kind: RowWorker, Name: "deep", Depth: 99},
			{ID: "b", Kind: RowWorker, Name: "negative", Depth: -3},
		}},
	}}
	m := New(src)
	if got := m.Rows()[1].Depth; got != maxIndentDepth {
		t.Fatalf("deep depth = %d, want %d", got, maxIndentDepth)
	}
	if got := m.Rows()[2].Depth; got != 0 {
		t.Fatalf("negative depth = %d, want 0", got)
	}
}

func TestMoveClampsAndPreviews(t *testing.T) {
	m := New(scene())
	ev := m.Move(1)
	if ev.Kind != EventSelected || ev.Cursor != 1 || ev.RowID != idWisp {
		t.Fatalf("move down = %+v", ev)
	}
	if ev.Composer != ComposerChat {
		t.Fatalf("composer = %v, want chat", ev.Composer)
	}
	// Clamping, not wrapping: a rail that wraps teleports the eye.
	if ev := m.Move(-5); ev.Cursor != 0 {
		t.Fatalf("move up past the top = %+v", ev)
	}
	if ev := m.Move(-1); !ev.Empty() {
		t.Fatalf("a move that changed nothing reported %+v", ev)
	}
	m.Move(99)
	if got := m.Cursor(); got != m.Len()-1 {
		t.Fatalf("cursor = %d, want %d", got, m.Len()-1)
	}
	if ev := m.Move(99); !ev.Empty() {
		t.Fatalf("a move past the end reported %+v", ev)
	}
}

func TestSelectedComposerIsDisabledOnSettledWork(t *testing.T) {
	m := New(scene())
	ev, ok := m.SelectID(idPerf)
	if !ok {
		t.Fatal("perf-audit not found")
	}
	if ev.Composer != ComposerDisabled {
		t.Fatalf("composer = %v, want disabled (5.15: settled — ask aforge)", ev.Composer)
	}
	if mark := m.Selected().EffectiveComposer().Mark(); mark != "" {
		t.Fatalf("a disabled composer promised the mark %q", mark)
	}
}

func TestEnterDescendsAndEscapePopsToTheRowYouCameFrom(t *testing.T) {
	m := New(scene())
	m.SelectID(idWisp)
	ev := m.Enter()
	if ev.Kind != EventScopeEntered {
		t.Fatalf("enter = %+v, want scope-entered", ev)
	}
	if m.Depth() != 1 || m.Scope().ID != idWisp {
		t.Fatalf("scope = %q depth %d", m.Scope().ID, m.Depth())
	}
	if m.Cursor() != 0 || m.Selected().Kind != RowSurface {
		t.Fatalf("entering did not land on the orchestrator: cursor %d row %+v", m.Cursor(), m.Selected())
	}
	if got := m.Breadcrumb(); len(got) != 2 || got[0] != "aforge" || got[1] != "wisp-parity" {
		t.Fatalf("breadcrumb = %v", got)
	}
	if m.Seed() != idWisp {
		t.Fatalf("seed = %q, want the task id", m.Seed())
	}

	// Move inside the scope, then pop: the parent cursor is where it was.
	m.Move(3)
	ev = m.Escape()
	if ev.Kind != EventScopePopped {
		t.Fatalf("escape = %+v", ev)
	}
	if m.Depth() != 0 || ev.RowID != idWisp {
		t.Fatalf("escape landed on %q at depth %d", ev.RowID, m.Depth())
	}
	if ev := m.Escape(); !ev.Empty() {
		t.Fatalf("escape at home reported %+v; the ladder is the shell's (8.2.21)", ev)
	}
}

func TestEnterOpensWhatItCannotDescendInto(t *testing.T) {
	m := New(scene())
	// data-clean has no scope in the source.
	m.SelectID(idClean)
	ev := m.Enter()
	if ev.Kind != EventOpened || ev.RowID != idClean {
		t.Fatalf("enter on a scopeless row = %+v", ev)
	}
	if m.Depth() != 0 {
		t.Fatal("a scopeless row must not push a scope")
	}
	// The surface row is a commitment, never a descent.
	m.Select(0)
	if ev := m.Enter(); ev.Kind != EventOpened {
		t.Fatalf("enter on the surface = %+v", ev)
	}
}

func TestEnterRefusesToDescendIntoAScopeAlreadyOnTheStack(t *testing.T) {
	src := scene()
	// A source that answers every id with the same scope would otherwise let
	// the rail descend forever.
	loop := src.scopes[idWisp]
	loop.Rows = append(loop.Rows, Row{ID: idWisp, Kind: RowTask, Name: "itself"})
	src.set(loop)
	m := New(src)
	m.SelectID(idWisp)
	m.Enter()
	if _, ok := m.SelectID(idWisp); !ok {
		t.Fatal("the self-referencing row is missing")
	}
	if ev := m.Enter(); ev.Kind != EventOpened {
		t.Fatalf("re-entering the current scope = %+v, want opened", ev)
	}
	if m.Depth() != 1 {
		t.Fatalf("depth = %d, want 1", m.Depth())
	}
}

func TestHomePopsEveryScope(t *testing.T) {
	m := New(scene())
	m.SelectID(idWisp)
	m.Enter()
	if ev := m.Home(); ev.Kind != EventScopePopped || m.Depth() != 0 {
		t.Fatalf("home = %+v depth %d", ev, m.Depth())
	}
	if ev := m.Home(); !ev.Empty() {
		t.Fatalf("home at home reported %+v", ev)
	}
}

// 7.2: cards never re-sort while visible. A badge pulls the eye; the row stays
// where the user last saw it.
func TestRefreshKeepsTheOrderOnScreen(t *testing.T) {
	src := scene()
	m := New(src)
	before := names(m.Rows())

	home := src.scopes[HomeScopeID]
	shuffled := []Row{home.Rows[0], home.Rows[3], home.Rows[1], home.Rows[2]}
	// The one that moved to the top also becomes the loudest, which is exactly
	// the case the law is about.
	shuffled[1].Questions = 3
	home.Rows = shuffled
	src.set(home)

	m.Refresh()
	if got := names(m.Rows()); !equal(got, before) {
		t.Fatalf("order moved: %v, want %v", got, before)
	}
	if m.Rows()[3].Questions != 3 {
		t.Fatal("the refreshed row lost its badge")
	}
}

func TestRefreshAppendsNewRowsAtTheEndAndDropsGoneOnes(t *testing.T) {
	src := scene()
	m := New(src)
	home := src.scopes[HomeScopeID]
	home.Rows = []Row{home.Rows[0], home.Rows[2], {ID: "new", Kind: RowTask, Name: "fresh"}, home.Rows[1]}
	src.set(home)
	m.Refresh()
	want := []string{"aforge", "wisp-parity", "data-clean", "fresh"}
	if got := names(m.Rows()); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestRefreshKeepsTheCursorOnItsRow(t *testing.T) {
	src := scene()
	m := New(src)
	m.SelectID(idPerf)
	home := src.scopes[HomeScopeID]
	home.Rows = append([]Row{home.Rows[0], {ID: "new", Kind: RowTask, Name: "fresh"}}, home.Rows[1:]...)
	src.set(home)
	m.Refresh()
	if got := m.Selected().ID; got != idPerf {
		t.Fatalf("cursor drifted to %q", got)
	}
}

func TestRefreshClampsWhenTheSelectedRowIsGone(t *testing.T) {
	src := scene()
	m := New(src)
	m.SelectID(idPerf)
	home := src.scopes[HomeScopeID]
	home.Rows = home.Rows[:2]
	src.set(home)
	m.Refresh()
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1 (clamped into the shortened scope)", m.Cursor())
	}
}

func TestRefreshPopsAScopeThatIsGone(t *testing.T) {
	src := scene()
	m := New(src)
	m.SelectID(idWisp)
	m.Enter()
	src.drop(idWisp)
	ev := m.Refresh()
	if ev.Kind != EventScopePopped {
		t.Fatalf("refresh = %+v, want scope-popped", ev)
	}
	if m.Depth() != 0 {
		t.Fatalf("depth = %d, want 0", m.Depth())
	}
}

func TestSourceFuncAndNilFuncAreHonest(t *testing.T) {
	var f SourceFunc
	if _, ok := f.Scope(HomeScopeID); ok {
		t.Fatal("a nil SourceFunc claimed to answer")
	}
	f = func(id string) (Scope, bool) { return Scope{Title: "x"}, id == HomeScopeID }
	if _, ok := f.Scope("nope"); ok {
		t.Fatal("SourceFunc ignored its own answer")
	}
	if s, ok := f.Scope(HomeScopeID); !ok || s.Title != "x" {
		t.Fatalf("SourceFunc = %+v %v", s, ok)
	}
}

func TestAttentionPrecedence(t *testing.T) {
	cases := []struct {
		name string
		row  Row
		want Attention
	}{
		{"question outranks working", Row{Life: LifeWorking, Questions: 1}, AttnQuestion},
		{"question outranks failed", Row{Life: LifeFailed, Questions: 2}, AttnQuestion},
		{"failed", Row{Life: LifeFailed}, AttnFailed},
		{"cancelled", Row{Life: LifeCancelled}, AttnCancelled},
		{"settled", Row{Life: LifeSettled}, AttnSettled},
		{"paused", Row{Life: LifePaused}, AttnPaused},
		{"working", Row{Life: LifeWorking}, AttnWorking},
		{"queued behind a sibling", Row{Life: LifeQueued, WaitsOn: []string{"H2"}}, AttnWaitsOn},
		{"queued", Row{Life: LifeQueued}, AttnQueued},
		{"waits-on does not outrank running", Row{Life: LifeWorking, WaitsOn: []string{"H2"}}, AttnWorking},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.row.Attention(); got != c.want {
				t.Fatalf("attention = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStepProgress(t *testing.T) {
	steps := []Step{{Life: LifeSettled}, {Life: LifeWorking}, {Life: LifeSettled}, {Life: LifeQueued}}
	done, total := StepProgress(steps)
	if done != 2 || total != 4 {
		t.Fatalf("progress = %d/%d, want 2/4", done, total)
	}
	if done, total := StepProgress(nil); done != 0 || total != 0 {
		t.Fatalf("empty progress = %d/%d", done, total)
	}
}

func names(rows []Row) []string {
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
