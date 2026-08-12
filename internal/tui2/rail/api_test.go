package rail

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The doors the integration lane will actually call, exercised so a rename or a
// wrong default fails here rather than in the shell.

func TestNamedRenderingsMatchTheirModes(t *testing.T) {
	m := New(scene())
	m.Select(1)
	v := plainView()
	assertLines(t, copyOf(v.Rail(m, 28, 20)), copyOf(v.Render(m, ModeRail, 28, 20)))
	assertLines(t, copyOf(v.List(m, 80, 20)), copyOf(v.Render(m, ModeList, 80, 20)))
	assertLines(t, copyOf(v.HUD(m, 60, 8)), copyOf(v.Render(m, ModeHUD, 60, 8)))
}

func TestViewReportsAndReplacesItsStyler(t *testing.T) {
	v := NewView(nil)
	if v.Profile() != tokens.NoColor || v.Focus() != tokens.FocusNormal {
		t.Fatalf("a nil styler gave %v/%v", v.Profile(), v.Focus())
	}
	v.SetStyler(tokens.NewStyler(tokens.ANSI256, tokens.FocusDimmed))
	if v.Profile() != tokens.ANSI256 || v.Focus() != tokens.FocusDimmed {
		t.Fatalf("SetStyler gave %v/%v", v.Profile(), v.Focus())
	}
	m := New(scene())
	if lines := v.Render(m, ModeRail, 28, 10); len(lines) == 0 {
		t.Fatal("a re-styled view rendered nothing")
	}
}

func TestPreviewIsTheCurrentSelection(t *testing.T) {
	m := New(scene())
	m.Select(2)
	ev := m.Preview()
	if ev.Kind != EventSelected || ev.RowID != idClean || ev.Cursor != 2 {
		t.Fatalf("preview = %+v", ev)
	}
	if ev.ScopeID != HomeScopeID || ev.RowKind != RowTask {
		t.Fatalf("preview scope/kind = %+v", ev)
	}
}

func TestPaneRespectsAnExplicitMode(t *testing.T) {
	m := New(scene())
	p := &Pane{Model: m, View: plainView(), Mode: ModeHUD}
	out := p.Render(60, 8)
	if strings.Contains(out, "aforge") {
		t.Fatalf("an explicit ModeHUD pane rendered the map:\n%s", out)
	}
	if len(strings.Split(out, "\n")) > tokens.HUDRowCap {
		t.Fatalf("the HUD pane exceeded its cap:\n%s", out)
	}
}

func TestScopeHelpers(t *testing.T) {
	m := New(scene())
	s := m.Scope()
	if got := len(s.Members()); got != 3 {
		t.Fatalf("members = %d, want 3", got)
	}
	if !s.Live() {
		t.Fatal("a scope with running work reported settled")
	}
	if (Scope{}).Members() != nil {
		t.Fatal("an empty scope has no members to hand out")
	}
	if (Scope{Rows: []Row{{Life: LifeSettled}}}).Live() {
		t.Fatal("a settled scope reported live")
	}
}

// The String methods are the vocabulary the logs and any future help surface
// speak; an unnamed enum value is a debugging session nobody enjoys.
func TestEnumsAreNamed(t *testing.T) {
	pairs := []struct{ got, want string }{
		{RowSurface.String(), "surface"},
		{RowTask.String(), "task"},
		{RowStep.String(), "step"},
		{RowWorker.String(), "worker"},
		{RowKind(9).String(), "invalid"},
		{LifeQueued.String(), "queued"},
		{LifeWorking.String(), "working"},
		{LifeSettled.String(), "settled"},
		{LifeFailed.String(), "failed"},
		{LifeCancelled.String(), "cancelled"},
		{LifePaused.String(), "paused"},
		{Lifecycle(9).String(), "invalid"},
		{ComposerNone.String(), "none"},
		{ComposerChat.String(), "chat"},
		{ComposerSteer.String(), "steer"},
		{ComposerDisabled.String(), "disabled"},
		{ComposerMode(9).String(), "invalid"},
		{ModeAuto.String(), "auto"},
		{ModeRail.String(), "rail"},
		{ModeList.String(), "list"},
		{ModeHUD.String(), "hud"},
		{Mode(9).String(), "invalid"},
		{EventNone.String(), "none"},
		{EventSelected.String(), "selected"},
		{EventOpened.String(), "opened"},
		{EventScopeEntered.String(), "scope-entered"},
		{EventScopePopped.String(), "scope-popped"},
		{EventKind(9).String(), "invalid"},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Fatalf("named %q, want %q", p.got, p.want)
		}
	}
}

func TestLifecycleTerminality(t *testing.T) {
	for life, want := range map[Lifecycle]bool{
		LifeQueued:    false,
		LifeWorking:   false,
		LifePaused:    false,
		LifeSettled:   true,
		LifeFailed:    true,
		LifeCancelled: true,
	} {
		if got := life.Terminal(); got != want {
			t.Fatalf("%v terminal = %v, want %v", life, got, want)
		}
	}
}

func TestRefEmptiness(t *testing.T) {
	if !(Ref{}).Empty() {
		t.Fatal("a zero Ref is not empty")
	}
	if (Ref{Label: "diagram"}).Empty() {
		t.Fatal("a labelled Ref is empty")
	}
}

func TestTelemetryEmptiness(t *testing.T) {
	if !(Telemetry{}).Empty() {
		t.Fatal("a zero Telemetry is not empty")
	}
	for _, tel := range []Telemetry{
		{Model: "K3"},
		{HasCost: true},
		{ContextWindow: 1},
		{HasElapsed: true},
		{Counts: StateCounts{Running: 1}},
	} {
		if tel.Empty() {
			t.Fatalf("%+v reported empty", tel)
		}
	}
	// The retired fields (§14) do not make a line 3. Nothing draws them, so a
	// source that still fills them must not buy a blank row with them.
	for _, tel := range []Telemetry{{HasWorkers: true, Workers: 4}, {Atomic: true}} {
		if !tel.Empty() {
			t.Fatalf("%+v bought a line 3 with a retired field", tel)
		}
	}
}

// The census has ONE spelling, and it is exported so a surface at page altitude
// (§6's board) can say it in the same words a card does. Two spellings of one
// fact is the failure internal/tui2/reltime exists to prevent for time.
func TestTheCensusCellIsTheVocabularyAndNotARailFeature(t *testing.T) {
	cases := []struct {
		counts StateCounts
		want   string
	}{
		{StateCounts{}, ""},
		{StateCounts{Running: 2, Done: 2}, "2◐ 2✓"},
		{StateCounts{Queued: 1}, "1○"},
		{StateCounts{Failed: 1, Cancelled: 2}, "3✕"},
	}
	for _, c := range cases {
		if got := c.counts.Cell(); got != c.want {
			t.Fatalf("%+v spells %q, want %q", c.counts, got, c.want)
		}
		if got, inner := c.counts.Cell(), countsCell(c.counts); got != inner {
			t.Fatalf("the exported census (%q) and the row's own (%q) disagree", got, inner)
		}
	}
}
