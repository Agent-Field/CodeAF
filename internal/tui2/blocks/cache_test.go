package blocks

import (
	"strconv"
	"strings"
	"testing"
)

// A finalized block is rendered once per (width, version) and NEVER rebuilt.
// This is the whole cache in one assertion.
func TestFinalizedBlockRendersOncePerWidthAndVersion(t *testing.T) {
	tr, blocks := fill(t, 6, 3, 40, 20)
	tr.Strict = false // the invariant check deliberately re-renders

	for i := 0; i < 10; i++ {
		tr.Frame(base)
	}
	for _, b := range blocks {
		if b.renders != 1 {
			t.Fatalf("block %s rendered %d times across 10 frames, want 1", b.id, b.renders)
		}
	}

	// A new width is the one full invalidation.
	tr.SetSize(30, 20)
	tr.Frame(base)
	for _, b := range blocks {
		if b.renders != 2 {
			t.Fatalf("block %s rendered %d times after a resize, want 2", b.id, b.renders)
		}
	}
	tr.Frame(base)
	for _, b := range blocks {
		if b.renders != 2 {
			t.Fatalf("block %s rebuilt at an unchanged width", b.id)
		}
	}
}

// Post-final mutation is coherent through Version — precisely because the bytes
// live in our cache and not in the terminal's scrollback.
func TestVersionBumpRebuildsAndReflows(t *testing.T) {
	tr, blocks := fill(t, 4, 2, 40, 20)
	tr.Strict = false
	tr.Frame(base)
	before := tr.Total()

	blocks[1].setRows("one", "two", "three", "four")
	frame := tr.Frame(base)

	if blocks[1].renders != 2 {
		t.Fatalf("version bump did not rebuild the block (renders=%d)", blocks[1].renders)
	}
	if tr.Total() != before+2 {
		t.Fatalf("total height %d, want %d after a two-row growth", tr.Total(), before+2)
	}
	if !strings.Contains(rowsOf(frame), "three") {
		t.Fatalf("mutated rows never reached the frame:\n%s", rowsOf(frame))
	}
	// Everything below re-laid out: the last block must still end the document.
	if got := tr.Len(); got != 4 {
		t.Fatalf("block count changed: %d", got)
	}
}

// Mutating a finalized block WITHOUT bumping Version is detected in test builds.
func TestStrictDetectsMutationWithoutVersionBump(t *testing.T) {
	if !strictDefault {
		t.Fatal("strict mode should default on under go test")
	}
	tr, blocks := fill(t, 3, 2, 40, 20)
	if !tr.Strict {
		t.Fatal("New did not pick up the test-build strict default")
	}
	tr.Frame(base)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a finalized block mutated without a version bump went undetected")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "without bumping Version") {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	blocks[1].lie("mutated", "quietly")
	tr.Frame(base)
}

func TestStrictDetectsHeightChangeWithoutVersionBump(t *testing.T) {
	tr, blocks := fill(t, 3, 2, 40, 20)
	tr.Frame(base)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("a silent height change went undetected")
		}
	}()
	blocks[1].lie("only one row now")
	tr.Frame(base)
}

// The cache is bounded: a 10k-block transcript holds rendered strings only for
// the visible blocks plus the retention window. Everything else is an int.
func TestCacheHoldsOnlyTheWindow(t *testing.T) {
	const n = 10000
	tr, _ := fill(t, n, 4, 60, 30)
	tr.Strict = false
	tr.Window = 24
	tr.Frame(base)

	stats := tr.Stats()
	if stats.Blocks != n {
		t.Fatalf("blocks %d", stats.Blocks)
	}
	// visible blocks (30 rows / 4 = 8ish) plus 24 either side.
	if stats.Resident > 64 {
		t.Fatalf("%d blocks resident in a %d-block transcript; the window is not bounding memory", stats.Resident, n)
	}
	if stats.ResidentRows > 64*4 {
		t.Fatalf("%d rows resident", stats.ResidentRows)
	}

	// Scrolling to the top moves the window; the old one is evicted.
	tr.GotoTop()
	tr.Frame(base)
	stats = tr.Stats()
	if stats.Resident > 64 {
		t.Fatalf("%d blocks resident after scrolling to the top", stats.Resident)
	}
	if stats.Evictions == 0 {
		t.Fatal("the window moved a full transcript's length and evicted nothing")
	}
	// And the rows are still correct after eviction and re-render.
	frame := tr.Frame(base)
	if frame.Rows[0] != "b0-0" {
		t.Fatalf("top row %q after eviction round-trip", frame.Rows[0])
	}
}

// Eviction happens only for the window and only re-renders on demand: a block
// that never leaves the window is never rebuilt.
func TestEvictionOnlyOutsideTheWindow(t *testing.T) {
	tr, blocks := fill(t, 200, 2, 40, 10)
	tr.Strict = false
	tr.Window = 5
	tr.GotoTop()
	tr.Frame(base)
	first := blocks[0].renders
	for i := 0; i < 5; i++ {
		tr.Frame(base)
	}
	if blocks[0].renders != first {
		t.Fatalf("a block inside the window was rebuilt (%d -> %d)", first, blocks[0].renders)
	}
	tr.GotoBottom()
	tr.Frame(base)
	if blocks[0].rowsResident(tr) {
		t.Fatal("a block far outside the window kept its rendered rows")
	}
}

func (f *fixed) rowsResident(tr *Transcript) bool {
	i, ok := tr.IndexOf(f.id)
	return ok && tr.ents[i].rows != nil
}

// Heights survive eviction, so the viewport can still map offsets to blocks
// without holding a transcript's worth of strings.
func TestHeightsSurviveEviction(t *testing.T) {
	tr, _ := fill(t, 500, 3, 40, 12)
	tr.Strict = false
	tr.Window = 2
	tr.Frame(base)
	if got, want := tr.Total(), 500*3; got != want {
		t.Fatalf("total %d, want %d", got, want)
	}
	tr.ScrollTo(750)
	frame := tr.Frame(base)
	if frame.Rows[0] != "b250-0" {
		t.Fatalf("row 750 is %q, want b250-0", frame.Rows[0])
	}
}

func TestTruncateAndReset(t *testing.T) {
	tr, _ := fill(t, 10, 2, 40, 8)
	tr.Strict = false
	tr.Frame(base)
	tr.Truncate(4)
	tr.Frame(base)
	if tr.Len() != 4 || tr.Total() != 8 {
		t.Fatalf("after truncate: len=%d total=%d", tr.Len(), tr.Total())
	}
	if _, ok := tr.IndexOf("b7"); ok {
		t.Fatal("truncated block still indexed")
	}
	tr.Reset()
	f := tr.Frame(base)
	if tr.Len() != 0 || tr.Total() != 0 {
		t.Fatal("reset left state behind")
	}
	if len(f.Rows) != 8 || strings.TrimSpace(rowsOf(f)) != "" {
		t.Fatalf("reset frame is not blank:\n%q", rowsOf(f))
	}
}

func TestDuplicateIDPanicsInStrictBuilds(t *testing.T) {
	tr := New(40, 10)
	tr.Append(newFixed("same", 1))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("duplicate block id went undetected")
		} else if !strings.Contains(r.(string), "duplicate block id") {
			t.Fatalf("unexpected panic %v", r)
		}
	}()
	tr.Append(newFixed("same", 1))
}

func TestStatsCountersMoveHonestly(t *testing.T) {
	tr, _ := fill(t, 3, 2, 40, 10)
	tr.Strict = false
	tr.Frame(base)
	renders := tr.Stats().Renders
	tr.Frame(base)
	if tr.Stats().Renders != renders {
		t.Fatalf("steady-state frame rendered blocks: %d -> %d", renders, tr.Stats().Renders)
	}
	if tr.Stats().Hits == 0 {
		t.Fatal("no cache hits recorded")
	}
}

func TestReplaceSwapsTheLiveBlockForItsFinalizedForm(t *testing.T) {
	tr := New(40, 10)
	tr.Strict = false
	tr.Append(newFixed("a", 2))
	live := newFixed("live", 2)
	live.final = false
	tr.Append(live)
	tr.Frame(base)

	settled := newFixed("live", 3)
	tr.ReplaceLast(settled)
	frame := tr.Frame(base)
	if !strings.Contains(rowsOf(frame), "live-2") {
		t.Fatalf("replacement not rendered:\n%s", rowsOf(frame))
	}
	if tr.LiveSeam() != 2 {
		t.Fatalf("live seam %d, want 2 after everything settled", tr.LiveSeam())
	}
}

func TestInvalidateForcesARebuild(t *testing.T) {
	tr, blocks := fill(t, 3, 2, 40, 10)
	tr.Strict = false
	tr.Frame(base)
	tr.Invalidate("b1")
	tr.Frame(base)
	if blocks[1].renders != 2 {
		t.Fatalf("Invalidate did not force a rebuild (renders=%d)", blocks[1].renders)
	}
}

func TestNoPanicAtOneByOne(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {1, 40}, {40, 1}, {0, 0}, {-5, -5}} {
		tr := New(size[0], size[1])
		tr.Frame(base)
		tr.Append(NewStatic("s", strings.Repeat("wide ", 40)))
		body := NewText("t", Header{Glyph: "◐", Title: "aforge", Desc: "thinking about it", Meta: []string{"12s"}})
		body.Write("a long stream of words that has to wrap somewhere reasonable " + strconv.Itoa(size[0]))
		tr.Append(body)
		f := tr.Frame(base)
		if len(f.Rows) != max1(size[1]) {
			t.Fatalf("%v: %d rows, want %d", size, len(f.Rows), max1(size[1]))
		}
		for _, row := range f.Rows {
			if w := Width(row); w > max1(size[0]) {
				t.Fatalf("%v: row %q is %d cells wide", size, row, w)
			}
		}
		tr.ScrollBy(-100)
		tr.ScrollBy(100)
		tr.PageUp()
		tr.PageDown()
		tr.Frame(base)
	}
}
