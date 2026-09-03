package tui3

import (
	"strings"
	"testing"
)

// A NAME KEEPS ITS ACCENT ALL THE WAY ONTO THE SCREEN.
//
// A conversation called `the café pricing page` is stored the way a Mac's own
// keyboard writes it — `e` followed by U+0301 — and home drew `the Cafe Pricing
// Page`, with the mark gone from the bytes altogether. Every layer of ours
// carries it; it is lost under us, in a cell buffer that is not using grapheme
// widths because tmux said no to mode 2027. [app.frame] composes, so the letter
// and its accent reach the terminal as one rune.
func TestAFrameNeverBreaksALetterFromItsAccent(t *testing.T) {
	// The two forms of the same word. They are DIFFERENT BYTES and neither is a
	// prefix of the other, which is what makes the two assertions below say
	// different things.
	const decomposed = "the café pricing page"
	const composed = "the café pricing page"

	a := newTestApp(&fakeAgent{model: "m"})
	a.input.setText(decomposed)

	// First that the words reach the frame at all, so a rename of the box or a
	// width that clipped the draft cannot make this test pass by drawing
	// neither form.
	body, _, _ := a.frameBody()
	if !strings.Contains(body, decomposed) {
		t.Fatalf("the draft never reached the frame, so this test proves nothing:\n%s", body)
	}

	got, _, _ := a.frame()
	if strings.Contains(got, decomposed) {
		t.Fatalf("the frame drew %q — an `e` and a combining acute on their own, which the cell buffer puts on two cells; want %q", decomposed, composed)
	}
	if !strings.Contains(got, composed) {
		t.Fatalf("the frame lost the accent altogether, want %q in:\n%s", composed, got)
	}
}

// AND IT COSTS NOTHING ON A FRAME THAT IS ALREADY COMPOSED, which is every
// frame on a machine whose names are typed the ordinary way. [app.frame] runs on
// every draw and this surface is under an allocation law (PERF.md's scroll
// ceiling is measured through this very function), and [norm.Form.String] spans
// the frame for the first byte composition could change and, finding none, hands
// back THE STRING IT WAS GIVEN. This is the law that holds that fast path: a
// normaliser that rebuilt the frame — norm.Form.Bytes is the one somebody
// reaches for — costs a whole screen of copying on every draw.
func TestComposingAFrameCostsNothingWhenThereIsNothingToCompose(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.input.setText("say what you want done")
	a.frame()

	body := allocsPerFrame(a, func() { a.frameBody() })
	whole := allocsPerFrame(a, func() { a.frame() })
	t.Logf("frame body %.0f allocations, composed frame %.0f", body, whole)
	if whole > body {
		t.Fatalf("composing an already-composed frame cost %.0f allocations on top of the frame's own %.0f", whole-body, body)
	}
}

// allocsPerFrame measures one redraw, with the frame marked dirty so the caches
// cannot answer it.
func allocsPerFrame(a *app, draw func()) float64 {
	return testing.AllocsPerRun(50, func() {
		a.dirty = true
		draw()
	})
}
