package blocks

import (
	"strconv"
	"strings"
	"testing"
)

// The anchor's reason for existing: a reader scrolled up keeps the same content
// on screen when a block ABOVE them changes height.
func TestAnchorSurvivesHeightChangeAbove(t *testing.T) {
	tr, blocks := fill(t, 20, 3, 40, 10)
	tr.Strict = false
	tr.Frame(base)

	tr.ScrollTo(32) // two rows into block 10
	frame := tr.Frame(base)
	top := frame.Rows[0]
	if top != "b10-2" {
		t.Fatalf("top row %q, want b10-2", top)
	}

	// A card lands at its birth position above the reader, four rows tall.
	blocks[5].setRows("x0", "x1", "x2", "x3", "x4", "x5", "x6")
	frame = tr.Frame(base)

	if frame.Rows[0] != top {
		t.Fatalf("the reader was yanked: top row %q, want %q", frame.Rows[0], top)
	}
	if tr.YOffset() != 36 {
		t.Fatalf("offset %d, want 36 (32 + the four rows that grew above)", tr.YOffset())
	}
}

// Appending below a scrolled-up reader must not move them at all.
func TestAppendBelowDoesNotMoveTheReader(t *testing.T) {
	tr, _ := fill(t, 20, 3, 40, 10)
	tr.Strict = false
	tr.Frame(base)
	tr.ScrollTo(20)
	tr.Frame(base)

	for i := 0; i < 5; i++ {
		tr.Append(newFixed("late"+strconv.Itoa(i), 4))
		frame := tr.Frame(base)
		if tr.YOffset() != 20 {
			t.Fatalf("append %d moved the reader to %d", i, tr.YOffset())
		}
		if frame.Rows[0] != "b6-2" {
			t.Fatalf("append %d changed the top row to %q", i, frame.Rows[0])
		}
	}
}

// Streaming into the live block while the reader is scrolled up NEVER moves the
// anchor — the case the rebuild exists for.
func TestStreamingDoesNotMoveAScrolledReader(t *testing.T) {
	tr, _ := fill(t, 12, 3, 40, 10)
	tr.Strict = false
	live := NewText("live", Header{Glyph: "◐", Title: "aforge"})
	tr.Append(live)
	tr.Frame(base)

	tr.ScrollTo(15)
	frame := tr.Frame(base)
	top := frame.Rows[0]

	for i := 0; i < 200; i++ {
		live.Write("word" + strconv.Itoa(i) + " ")
		frame = tr.Frame(base)
		if tr.YOffset() != 15 {
			t.Fatalf("stream chunk %d moved the reader to %d", i, tr.YOffset())
		}
		if frame.Rows[0] != top {
			t.Fatalf("stream chunk %d changed the top row: %q -> %q", i, top, frame.Rows[0])
		}
	}
	if tr.Following() {
		t.Fatal("a scrolled-up reader was put back into follow by streaming")
	}
}

// Bottom-follow, and only when at bottom.
func TestBottomFollowOnlyWhenAtBottom(t *testing.T) {
	tr, _ := fill(t, 5, 3, 40, 10)
	tr.Strict = false
	tr.Frame(base)
	if !tr.Following() || !tr.AtBottom() {
		t.Fatal("a new transcript should sit at the bottom, following")
	}

	tr.Append(newFixed("new", 3))
	tr.Frame(base)
	if !tr.AtBottom() {
		t.Fatal("following transcript did not stay at the bottom on append")
	}

	tr.ScrollBy(-4)
	if tr.Following() {
		t.Fatal("scrolling up left follow on")
	}
	before := tr.YOffset()
	tr.Append(newFixed("newer", 3))
	tr.Frame(base)
	if tr.YOffset() != before {
		t.Fatalf("a non-following transcript followed anyway: %d -> %d", before, tr.YOffset())
	}

	tr.GotoBottom()
	if !tr.Following() {
		t.Fatal("GotoBottom did not resume follow")
	}
	tr.Append(newFixed("newest", 3))
	tr.Frame(base)
	if !tr.AtBottom() {
		t.Fatal("follow did not resume")
	}
}

// A block that vanished into another resolves through Alias — the port of the
// old renderer's cardForMessageSeq fallback.
func TestAnchorFollowsAnAbsorbedBlock(t *testing.T) {
	tr, _ := fill(t, 20, 3, 40, 10)
	tr.Strict = false
	tr.Alias = func(id string) string {
		if id == "b10" {
			return "card"
		}
		return ""
	}
	tr.Frame(base)
	tr.ScrollTo(31)
	tr.Frame(base)

	// b10 is folded into a card that replaces b8..b12.
	card := newFixed("card", 6)
	tr.Truncate(8)
	tr.Append(card)
	for i := 13; i < 20; i++ {
		tr.Append(newFixed("c"+strconv.Itoa(i), 3))
	}
	frame := tr.Frame(base)
	if frame.Rows[0] != "card-0" {
		t.Fatalf("the reader did not land on the absorbing card: top %q", frame.Rows[0])
	}
}

// A block with interior churn anchors on its own content-derived keys — the
// port of node.go's feedAnchor.
type keyedBlock struct {
	*fixed
	keys []string
}

func (k *keyedBlock) AnchorAt(row int) (string, int) {
	if row < 0 || row >= len(k.keys) {
		return "", 0
	}
	return k.keys[row], 0
}

func (k *keyedBlock) AnchorRow(key string) (int, bool) {
	for i, have := range k.keys {
		if have == key {
			return i, true
		}
	}
	return 0, false
}

func TestAnchorUsesContentDerivedKeys(t *testing.T) {
	tr := New(40, 6)
	tr.Strict = false
	tr.Append(newFixed("head", 4))
	feed := &keyedBlock{fixed: newFixed("feed", 12),
		keys: []string{"e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9", "e10", "e11"}}
	tr.Append(feed)
	tr.Frame(base)

	tr.ScrollTo(6) // feed row 2 -> key e2
	tr.Frame(base)
	if a := tr.Anchor(); a.Key() != "e2" {
		t.Fatalf("anchor key %q, want e2", a.Key())
	}

	// The feed's window slides off its own head: e0 and e1 are gone.
	feed.setRows("feed-2", "feed-3", "feed-4", "feed-5", "feed-6", "feed-7",
		"feed-8", "feed-9", "feed-10", "feed-11", "feed-12", "feed-13")
	feed.keys = []string{"e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9", "e10", "e11", "e12", "e13"}
	frame := tr.Frame(base)
	if frame.Rows[0] != "feed-2" {
		t.Fatalf("the reader lost their place through a sliding window: top %q", frame.Rows[0])
	}
}

func TestLiveSeamAndLiveFrom(t *testing.T) {
	tr := New(40, 12)
	tr.Strict = false
	tr.Append(newFixed("a", 3))
	tr.Append(newFixed("b", 3))
	if tr.LiveSeam() != 2 {
		t.Fatalf("seam %d with nothing live", tr.LiveSeam())
	}
	live := NewText("live", Header{Title: "aforge"})
	live.Write("streaming")
	tr.Append(live)
	frame := tr.Frame(base)
	if tr.LiveSeam() != 2 {
		t.Fatalf("seam %d, want 2", tr.LiveSeam())
	}
	if frame.LiveFrom != 6 {
		t.Fatalf("LiveFrom %d, want 6", frame.LiveFrom)
	}
	live.Finalize(EndCompleted)
	frame = tr.Frame(base)
	if frame.LiveFrom != -1 {
		t.Fatalf("LiveFrom %d after everything settled, want -1", frame.LiveFrom)
	}
}

// The frame is a fixed grid: exactly Height rows, none wider than Width.
func TestFrameIsAFixedGrid(t *testing.T) {
	tr, _ := fill(t, 3, 2, 24, 20)
	tr.Strict = false
	frame := tr.Frame(base)
	if len(frame.Rows) != 20 {
		t.Fatalf("%d rows, want 20", len(frame.Rows))
	}
	if frame.Rows[6] != "" {
		t.Fatalf("past the end of the transcript is %q, want blank", frame.Rows[6])
	}
}

func TestResizePreservesTheReadersBlock(t *testing.T) {
	tr := New(40, 8)
	tr.Strict = false
	for i := 0; i < 12; i++ {
		body := NewText("t"+strconv.Itoa(i), Header{Title: "turn " + strconv.Itoa(i)})
		body.Write(strings.Repeat("some words to wrap ", 4))
		body.Finalize(EndCompleted)
		tr.Append(body)
	}
	tr.Frame(base)
	tr.ScrollTo(20)
	tr.Frame(base)
	anchored := tr.Anchor().ID()

	tr.SetSize(24, 8)
	tr.Frame(base)
	if got := tr.Anchor().ID(); got != anchored {
		t.Fatalf("resize moved the reader from %q to %q", anchored, got)
	}
}

func TestScrollClampsAtBothEnds(t *testing.T) {
	tr, _ := fill(t, 4, 2, 40, 20)
	tr.Strict = false
	tr.Frame(base)
	tr.ScrollBy(-1000)
	if tr.YOffset() != 0 {
		t.Fatalf("offset %d after scrolling far up", tr.YOffset())
	}
	tr.ScrollBy(1000)
	if tr.YOffset() != 0 {
		t.Fatalf("offset %d in a transcript shorter than the viewport", tr.YOffset())
	}
	if !tr.AtBottom() || !tr.AtTop() {
		t.Fatal("a short transcript is at both ends at once")
	}
}
