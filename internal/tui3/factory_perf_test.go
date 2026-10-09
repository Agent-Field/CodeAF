package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FLOOR AT THE OWNER'S SIZE (2026-10-09) ──────────────────────────────
//
// The owner's real store: about three hundred items over many watched
// repositories, long GitHub bodies written with CRLF line ends, three
// comments apiece, and one item running. Every pointer step, key and beat on
// the floor is measured against this floor, in a terminal 250 columns wide
// with the peek open beside the rows.

// factoryPerfBody is a GitHub issue body of the kind the owner's floor holds:
// headings, a list, a fenced block, a table and a link, CRLF line ends, a
// tab, and n paragraphs of prose.
func factoryPerfBody(i, n int) string {
	var b strings.Builder
	b.WriteString("## Why\r\n\r\n")
	for p := 0; p < n; p++ {
		fmt.Fprintf(&b, "The workspace identity is read before the session trees exist (item %d, paragraph %d), so the engine path resolution falls back to the folder the window opened in, and every chat started from the floor lands in the wrong tree. ", i, p)
		b.WriteString("This is ordering, not state: the tree is built a beat later and nothing reads it again.\r\n\r\n")
	}
	b.WriteString("## What\r\n\r\n- touches workspace identity\r\n- touches engine path resolution\r\n- \tkeeps the session trees as they are\r\n\r\n")
	b.WriteString("```go\r\nfunc resolve(dir string) string {\r\n\treturn filepath.Clean(dir)\r\n}\r\n```\r\n\r\n")
	b.WriteString("| field | before | after |\r\n|---|---|---|\r\n| tree | nil | built |\r\n| path | cwd | workspace |\r\n\r\n")
	fmt.Fprintf(&b, "See https://github.com/agentfield/codeaf/issues/%d for the first report.\r\n", 1000+i)
	if i%5 == 0 {
		b.WriteString("\r\n📊 Coverage gate ✅ passed · 📐 patch coverage ➖ unchanged\r\n")
	}
	return b.String()
}

// factoryPerfSnapshot is the owner's floor: items over repos repositories,
// one of them running.
func factoryPerfSnapshot(now time.Time, items, repos int) factory.Snapshot {
	base := factory.Fixture(now)
	recipe := base.Repos[0].Recipe
	snap := base
	snap.Repos = nil
	for r := 0; r < repos; r++ {
		snap.Repos = append(snap.Repos, factory.Repo{Name: fmt.Sprintf("agentfield/repo-%02d", r), Recipe: recipe, Areas: []string{"tui", "engine"}, Hue: r % 6})
	}
	states := []factory.State{factory.StateNew, factory.StateNew, factory.StateNew, factory.StateQueued, factory.StateNeedsYou, factory.StateShipped}
	snap.Items = nil
	for i := 0; i < items; i++ {
		repo := snap.Repos[i%repos].Name
		it := factory.Item{
			ID: i + 1, Repo: repo, Num: 1000 + i, URL: fmt.Sprintf("https://github.com/%s/issues/%d", repo, 1000+i),
			Kind: factory.KindIssue, Title: fmt.Sprintf("workspace identity is read before the session trees exist, case %d", i),
			Author: "qwen-bot", Tier: factory.TierCollab, Origin: factory.OriginForge,
			Created: now.Add(-time.Duration(i) * time.Hour), Changed: now.Add(-time.Duration(i) * time.Minute),
			State: states[i%len(states)], Cap: float64(i%3) * 2.5, Stages: append([]factory.Stage{}, recipe.Stages...),
			Body: factoryPerfBody(i, 8),
			Triage: factory.Triage{Type: "bug", Size: "M", Area: "engine", Readiness: 70, Est: 3, Priority: 1 + i%4,
				Risk: []string{"touches workspace identity", "touches engine path resolution"}, Read: "ordering, not state"},
		}
		for c := 0; c < 3; c++ {
			it.Comments = append(it.Comments, factory.Comment{Author: "deepseek-bot", Body: factoryPerfBody(i, 2), At: now.Add(-time.Duration(c) * time.Hour)})
		}
		snap.Items = append(snap.Items, it)
	}
	// ONE ITEM RUNS.
	run := &snap.Items[1]
	run.State = factory.StateRunning
	run.Stream = base.Items[1].Stream
	return snap
}

// factoryPerfLab is the factory place over the owner's floor, at width by
// height, in colour, with the first read folded in and one frame drawn.
func factoryPerfLab(tb testing.TB, width, height int) *app {
	tb.Helper()
	a := benchApp(0)
	snap := factoryPerfSnapshot(factoryTestNow, 300, 24)
	a.factory = factory.Seam{Load: func() (factory.Snapshot, error) { return snap, nil }}
	a.width, a.height = width, height
	a.pal = newPalette(tokens.TrueColor, false)
	factoryPerfSettle(a, a.showPage(pageFactory))
	if !a.at(pageFactory) || !a.fp.loaded {
		tb.Fatal("the factory place did not open over the owner's floor")
	}
	a.fp.cursor = 0
	a.View()
	return a
}

// factoryPerfSettle runs cmd and feeds what it answers back, the way the
// program would, until nothing is left (ticks are dropped: they are clocks,
// not answers).
func factoryPerfSettle(a *app, cmd tea.Cmd) {
	for _, msg := range runCmd(cmd) {
		a.Update(msg)
	}
}

// factoryPerfArrive is one pointer motion answered as an arrival, the way a
// pointer that moved a row and stopped is: the fold is reset so the motion is
// routed rather than folded, and the frame Bubble Tea asks for is built.
func factoryPerfArrive(a *app, x, y int) {
	a.ptr.moving, a.ptr.settling, a.ptr.have = false, false, false
	a.Update(tea.MouseMotionMsg{X: x, Y: y})
	a.View()
}

// BenchmarkFactoryFloor is the floor's per-message cost, measured as the
// program pays it: Update, then View.
func BenchmarkFactoryFloor(b *testing.B) {
	a := factoryPerfLab(b, 250, 60)
	y0, y1 := factoryRowYB(b, a, 3), factoryRowYB(b, a, 4)
	b.Run("motion-same-row", func(b *testing.B) {
		factoryPerfArrive(a, 4, y0)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			factoryPerfArrive(a, 4+i%20, y0)
		}
	})
	b.Run("motion-new-row", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			y := y0
			if i%2 == 1 {
				y = y1
			}
			factoryPerfArrive(a, 4, y)
		}
	})
	b.Run("motion-over-peek", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			factoryPerfArrive(a, a.fp.rowsW+10+i%40, y0)
		}
	})
	b.Run("key-down-up", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			k := "down"
			if i%2 == 1 {
				k = "up"
			}
			a.Update(key(k))
			a.View()
		}
	})
	b.Run("fold", func(b *testing.B) {
		snap, _ := a.factory.Load()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			a.factoryFold(snap)
			a.drawn = false
			a.View()
		}
	})
	b.Run("frame", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			a.frame()
		}
	})
}

// factoryRowYB is [factoryRowY] for a benchmark.
func factoryRowYB(tb testing.TB, a *app, walk int) int {
	tb.Helper()
	for y := 0; y < a.height; y++ {
		if a.factoryWalkAt(y) == walk {
			return y
		}
	}
	tb.Fatalf("the floor drew no row for walk %d", walk)
	return -1
}

// BenchmarkFactoryItemPage is the item page's per-message cost over the
// owner's floor, opened on the running item: the pointer over the left
// column, the arrows walking it, and the frame alone.
func BenchmarkFactoryItemPage(b *testing.B) {
	a := factoryPerfLab(b, 250, 60)
	for i, at := range a.factoryWalkNow() {
		if a.fp.snap.Items[at].State == factory.StateRunning {
			a.fp.cursor = i
		}
	}
	a.fp.open = true
	a.View()
	b.Run("motion-left", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			factoryPerfArrive(a, 2+i%20, 8+i%12)
		}
	})
	b.Run("key-down-up", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			k := "down"
			if i%2 == 1 {
				k = "up"
			}
			a.Update(key(k))
			a.View()
		}
	})
	b.Run("frame", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			a.frame()
		}
	})
}

// THE RAIL IS LAID OUT ONCE PER FLOOR AND VIEW: asking for the rows again
// hands back the same layout, and a narrowing, a new place or a new read lays
// them out again, each seen in the rows.
func TestFactoryRowsAreLaidOutOncePerFloorAndView(t *testing.T) {
	a := factoryPerfLab(t, 250, 60)
	first := a.factoryRows()
	for i := 0; i < 50; i++ {
		a.frame()
		a.factoryCursorItem()
	}
	if again := a.factoryRows(); &again[0] != &first[0] {
		t.Fatal("the rows were laid out again with nothing changed")
	}
	kept := len(a.factoryWalkNow())
	a.fp.backlog = true
	if all := len(a.factoryWalkNow()); all <= kept {
		t.Fatalf("the backlog shown walks %d items, kept back it walked %d", all, kept)
	}
	a.fp.backlog = false
	if len(a.factoryWalkNow()) != kept {
		t.Fatal("the backlog put away did not walk what it walked before")
	}
	snap, _ := a.factory.Load()
	snap.Items = append([]factory.Item(nil), snap.Items...)
	at := a.factoryWalkNow()[0]
	snap.Items[at].State = factory.StateDismissed
	a.factoryFold(snap)
	for _, i := range a.factoryWalkNow() {
		if snap.Items[i].ID == snap.Items[at].ID {
			t.Fatal("a dismissed item is still walked after the read that dismissed it")
		}
	}
	all := len(a.factoryWalkNow())
	a.fp.query = "case 172"
	if n := len(a.factoryWalkNow()); n == 0 || n >= all {
		t.Fatalf("typed words walk %d items of %d", n, all)
	}
}
