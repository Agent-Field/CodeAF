package tui3

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// catchUpReveal walks every live edge to the end of the bytes the stream
// already holds. Tests that are about the TRANSCRIPT — what a fold produced,
// whether a forming reply is still source — must not also be tests of the
// walk: they catch up, then look.
func catchUpReveal(a *app) {
	if a == nil {
		return
	}
	for i := 0; i < revealSlots+4 && a.liveRevealing(); i++ {
		a.tickReveal()
	}
}

func TestAShortBurstIsShownWhole(t *testing.T) {
	var shown int
	text := "it parses."
	catchReveal(&shown, text, len(text), false)
	if shown != len(text) {
		t.Fatalf("a %d-byte burst started at %d, want the whole of it", len(text), shown)
	}
	if revealedText(text, shown, false) != text {
		t.Fatal("a short burst was held back")
	}
}

func TestALumpOpensOnItsHeadAndCatchesOnTheClock(t *testing.T) {
	lump := strings.Repeat("the loader never makes the map. ", 20) // well past revealAtOnce
	var shown int
	catchReveal(&shown, lump, len(lump), false)
	if shown <= 0 || shown >= len(lump) {
		t.Fatalf("a lump opened at %d of %d, want the head only", shown, len(lump))
	}
	if shown > revealHead {
		// UTF-8 walk can only land on or before the head.
		t.Fatalf("the head was %d, want at most %d", shown, revealHead)
	}

	// Eight slots is the snappy bound: whatever the size, the last frame of
	// the window has caught up. The test walks one slot at a time so the
	// edge is seen to MOVE rather than snap on the first paint.
	frames := 0
	for revealing(shown, lump, false) {
		frames++
		if !advanceReveal(&shown, lump, 1, false) {
			t.Fatal("the edge stopped moving while bytes were still unread")
		}
		if frames > revealSlots+1 {
			t.Fatalf("a lump took %d frames to arrive, want at most %d", frames, revealSlots)
		}
	}
	if shown != len(lump) {
		t.Fatalf("caught up at %d of %d", shown, len(lump))
	}
	if frames < 2 {
		t.Fatal("a lump landed in one frame: the edge never moved")
	}
}

func TestASettleSnapsTheUnreadRemainder(t *testing.T) {
	e := &entry{kind: entryAssistant, text: strings.Repeat("word ", 40)}
	e.catchReveal(len(e.text), false)
	if !e.revealing() {
		t.Fatal("a lump was shown whole before the settle")
	}
	e.settled = true
	if e.revealed() != e.text {
		t.Fatal("a settled block still held bytes back")
	}
	// The first paint after the settle snaps the cursor; the next one has
	// nothing left to walk.
	e.advanceReveal(1, false)
	if e.advanceReveal(1, false) {
		t.Fatal("a settled block still had an edge to walk")
	}
}

func TestLinearShowsTheBurstAtOnce(t *testing.T) {
	lump := strings.Repeat("αβγ ", 30)
	var shown int
	catchReveal(&shown, lump, len(lump), true)
	if shown != len(lump) {
		t.Fatalf("linear opened at %d of %d, want everything", shown, len(lump))
	}
}

func TestTheEdgeLandsOnARuneBoundary(t *testing.T) {
	text := "café 日本語"
	for n := 0; n <= len(text)+3; n++ {
		cut := cutUTF8(text, n)
		if cut < 0 || cut > len(text) {
			t.Fatalf("cutUTF8(%d) = %d, out of range", n, cut)
		}
		if cut < len(text) && !utf8.RuneStart(text[cut]) {
			t.Fatalf("cutUTF8(%d) = %d, mid-rune", n, cut)
		}
	}
}

func TestACaughtUpStreamShowsTheNextWordAtOnce(t *testing.T) {
	var shown int
	text := "hello"
	catchReveal(&shown, text, len(text), false)
	text += " there"
	catchReveal(&shown, text, len(" there"), false)
	if shown != len(text) {
		t.Fatalf("a short follow-up started at %d of %d, want the whole word", shown, len(text))
	}
}

func TestMeterEaseIsFastThenFine(t *testing.T) {
	shown := 0
	target := 150
	steps := []int{}
	for shown != target {
		next := easeInt(shown, target, 1)
		if next <= shown && target > shown {
			t.Fatalf("easeInt(%d, %d) = %d, did not advance", shown, target, next)
		}
		shown = next
		steps = append(steps, shown)
		if len(steps) > 12 {
			t.Fatalf("150 tokens took %d frames to arrive: %v", len(steps), steps)
		}
	}
	if len(steps) < 3 {
		t.Fatalf("150 tokens arrived in %d steps, want a walk: %v", len(steps), steps)
	}
	// First step takes the most — ease-out, not a linear count.
	if steps[0] < steps[1]-steps[0] {
		t.Fatalf("the first step was not the largest: %v", steps)
	}
}

func TestAStreamingLumpIsDrawnAcrossFrames(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	lump := strings.Repeat("the map is never made. ", 16)
	a.event(text(session.EventTextDelta, lump))
	if a.live < 0 {
		t.Fatal("the lump did not open a live block")
	}
	e := &a.entries[a.live]
	if e.shown <= 0 || e.shown >= len(e.text) {
		t.Fatalf("the live edge opened at %d of %d", e.shown, len(e.text))
	}
	first := e.revealed()
	a.tickReveal()
	if e.revealed() == first {
		t.Fatal("a paint did not walk the live edge")
	}
	for i := 0; i < revealSlots+2 && e.revealing(); i++ {
		a.tickReveal()
	}
	if e.revealed() != e.text {
		t.Fatal("the clock did not catch the lump")
	}
}
