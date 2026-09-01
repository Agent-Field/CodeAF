package blocks

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

var sink Frame

// benchTranscript builds a settled transcript of n blocks with real text
// blocks, the way a long conversation actually looks.
func benchTranscript(n, width, height int) *Transcript {
	tr := New(width, height)
	tr.Strict = false // the invariant check exists to re-render; see cache.go
	for i := 0; i < n; i++ {
		b := NewText("turn"+strconv.Itoa(i), Header{
			Glyph: "✓",
			Title: "aforge",
			Desc:  "answered turn " + strconv.Itoa(i),
			Meta:  []string{"4s", "$0.01"},
		})
		b.Write(strings.Repeat("a sentence of the reply that has to wrap at any sane width. ", 3))
		b.Finalize(EndCompleted)
		tr.Append(b)
	}
	tr.Frame(base)
	return tr
}

// The steady state: nothing changed, nothing is live. Zero allocations, zero
// block renders, zero dirty rows.
func BenchmarkFrameUnchanged(b *testing.B) {
	tr := benchTranscript(200, 100, 40)
	renders := tr.Stats().Renders

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = tr.Frame(base)
	}
	b.StopTimer()

	if got := tr.Stats().Renders; got != renders {
		b.Fatalf("the steady state rebuilt %d blocks", got-renders)
	}
	if len(sink.Dirty) != 0 {
		b.Fatalf("an unchanged frame reported %d dirty rows", len(sink.Dirty))
	}
}

// The same thing as a hard assertion rather than a number.
func TestUnchangedFrameAllocatesNothing(t *testing.T) {
	tr := benchTranscript(200, 100, 40)
	if got := allocs(func() { sink = tr.Frame(base) }); got != 0 {
		t.Fatalf("an unchanged frame allocated %.1f times per render", got)
	}
	if len(sink.Dirty) != 0 {
		t.Fatalf("an unchanged frame reported %d dirty rows", len(sink.Dirty))
	}
}

// A 10k-block transcript in the steady state: the version scan is the only
// O(transcript) work per frame, and it allocates nothing.
func BenchmarkFrameUnchanged10k(b *testing.B) {
	tr := benchTranscript(10000, 100, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = tr.Frame(base)
	}
}

func TestUnchangedFrameAllocatesNothingAt10k(t *testing.T) {
	tr := benchTranscript(10000, 100, 40)
	if got := allocs(func() { sink = tr.Frame(base) }); got != 0 {
		t.Fatalf("an unchanged 10k frame allocated %.1f times per render", got)
	}
	stats := tr.Stats()
	t.Logf("10k transcript: %d blocks, %d resident, %d resident rows", stats.Blocks, stats.Resident, stats.ResidentRows)
}

// A streaming frame costs O(live region): the settled head is kept, only the
// tail is rebuilt, and the committed prefix is not touched at all.
func BenchmarkFrameStreaming(b *testing.B) {
	tr := benchTranscript(200, 100, 40)
	live := NewText("live", Header{Glyph: "◐", Title: "aforge", Desc: "answering"})
	tr.Append(live)
	tr.Frame(base)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		live.Write("token ")
		sink = tr.Frame(base)
	}
}

// The claim in test form: the per-frame cost of streaming does not grow with
// the length of the reply, because the settled head is never rebuilt.
func TestStreamingFrameCostIsBoundedByTheLiveRegion(t *testing.T) {
	tr := benchTranscript(200, 100, 40)
	live := NewText("live", Header{Glyph: "◐", Title: "aforge", Desc: "answering"})
	tr.Append(live)
	tr.Frame(base)

	measure := func() float64 {
		return allocs(func() {
			live.Write("token ")
			sink = tr.Frame(base)
		})
	}
	early := measure()
	for i := 0; i < 4000; i++ {
		live.Write("token ")
		tr.Frame(base)
	}
	late := measure()

	if late > early+2 {
		t.Fatalf("streaming got more expensive as the reply grew: %.1f allocs early, %.1f late", early, late)
	}
	t.Logf("streaming allocs/frame: %.1f early, %.1f after a 4000-token reply", early, late)

	// And the settled head really is being kept: the block is far taller than
	// the viewport, yet the frame only ever walks the visible tail.
	if h := tr.Total(); h < 400 {
		t.Fatalf("the streaming block never grew (total %d rows)", h)
	}
}

// A live block that has not changed still rebuilds every frame — that is what
// live means — but the rebuild itself allocates nothing when the block reuses
// its own buffers and its settled head is kept.
func TestLiveFrameWithNoChangeAllocatesNothing(t *testing.T) {
	tr := benchTranscript(200, 100, 40)
	live := NewText("live", Header{Glyph: "◐", Title: "aforge", Desc: "answering"})
	live.Write(strings.Repeat("a streaming reply that keeps going and going. ", 20))
	tr.Append(live)
	tr.Frame(base)
	tr.Frame(base)
	if got := allocs(func() { sink = tr.Frame(base) }); got != 0 {
		t.Fatalf("a live frame with no change allocated %.1f times", got)
	}
}

func BenchmarkFrameLiveNoChange(b *testing.B) {
	tr := benchTranscript(200, 100, 40)
	live := NewText("live", Header{Glyph: "◐", Title: "aforge", Desc: "answering"})
	live.Write(strings.Repeat("a streaming reply that keeps going and going. ", 20))
	tr.Append(live)
	tr.Frame(base)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = tr.Frame(base)
	}
}

func BenchmarkScroll(b *testing.B) {
	tr := benchTranscript(10000, 100, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.ScrollBy(7)
		if tr.AtBottom() {
			tr.GotoTop()
		}
		sink = tr.Frame(base)
	}
}

func BenchmarkResize10k(b *testing.B) {
	tr := benchTranscript(10000, 100, 40)
	widths := []int{100, 80}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.SetSize(widths[i%2], 40)
		sink = tr.Frame(base)
	}
}

func BenchmarkHeaderRender(b *testing.B) {
	h := Header{
		Glyph:  "◐",
		Title:  "fix",
		Desc:   "rewriting the executor harness",
		Badges: []Badge{{Text: "?2", Hue: HueAttention}},
		Meta:   []string{"K3 ▄ $8.65", "4m"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		strSink = h.Render(80, Plain)
	}
}

var strSink string

func BenchmarkFold(b *testing.B) {
	states := make([]ItemState, 400)
	for i := range states {
		states[i] = ItemPending
	}
	for i := 390; i < 400; i++ {
		states[i] = ItemRunning
	}
	var f Folder
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		planSink = f.Live(states, 8)
	}
}

var planSink Plan

// A frame with a live glyph but no state change still repaints nothing: the
// render tick and the glyph tick are on the same grid.
func BenchmarkFrameLiveButUnchanged(b *testing.B) {
	tr := benchTranscript(200, 100, 40)
	tr.Append(&spin{id: "live", c: tr.Clock(), n: 3})
	now := base.Add(17 * time.Millisecond)
	tr.Frame(now)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = tr.Frame(now)
	}
}
