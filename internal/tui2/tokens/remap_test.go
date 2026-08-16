package tokens

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
)

// The remap seam (10.2.7, 12.4.4).
//
// internal/sanitize landed the remap MECHANISM in Wave 1 with an identity
// table, and this package supplies the palette that replaces it. The tests
// below run the real sanitizer with the real table, because a [16]int that
// merely looks right is not a seam — the thing worth proving is that the
// conversion compiles, that the sanitizer accepts it, and that tool output
// actually comes out recoloured.
//
// This file is the only place tokens touches internal/sanitize, and it does so
// in a test: the shipping package exports a plain [16]int precisely so it stays
// free of that dependency, and the call site does the conversion.

// TestTableShapeMatchesTheSanitizer is the compile-time half of the seam. If
// sanitize.Table ever changes shape, this file stops building and the assembly
// wave finds out here rather than at wiring time.
func TestTableShapeMatchesTheSanitizer(t *testing.T) {
	var tbl sanitize.Table = sanitize.Table(ANSI16Remap)
	if tbl.IsIdentity() {
		t.Fatal("the palette table is the identity table; 12.4.4's placeholder was never replaced")
	}
	if len(tbl) != len(ANSI16Remap) {
		t.Fatalf("sanitize.Table has %d entries, the palette table has %d", len(tbl), len(ANSI16Remap))
	}
}

// TestRemapRecolorsToolOutput runs the actual chokepoint. Each case is a piece
// of tool output and the colours it must come out wearing.
func TestRemapRecolorsToolOutput(t *testing.T) {
	tbl := sanitize.Table(ANSI16Remap)
	cases := []struct {
		name, in, want string
	}{
		{
			"a dark standard red becomes the legible bright one",
			"\x1b[31merror\x1b[0m", "\x1b[91merror\x1b[0m",
		},
		{
			"every chromatic standard colour moves to its bright counterpart",
			"\x1b[32ma\x1b[33mb\x1b[34mc\x1b[35md\x1b[36me",
			"\x1b[92ma\x1b[93mb\x1b[94mc\x1b[95md\x1b[96me",
		},
		{
			"bright white is demoted so a tool never outranks our primary tier",
			"\x1b[97mSHOUTING\x1b[0m", "\x1b[37mSHOUTING\x1b[0m",
		},
		{
			"a tool's own dim text stays our chrome tier",
			"\x1b[90mdim\x1b[0m", "\x1b[90mdim\x1b[0m",
		},
		{
			"backgrounds stay backgrounds",
			"\x1b[41mbg\x1b[0m", "\x1b[101mbg\x1b[0m",
		},
		{
			"non-colour attributes are untouched",
			"\x1b[1;4;31mbold underline red\x1b[0m", "\x1b[1;4;91mbold underline red\x1b[0m",
		},
		{
			"256-colour and truecolour output passes through opaquely",
			"\x1b[38;5;196mx\x1b[38;2;10;20;30my", "\x1b[38;5;196mx\x1b[38;2;10;20;30my",
		},
		{
			"plain text costs nothing",
			"no escapes here", "no escapes here",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitize.TextWithPalette(c.in, tbl); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestRemapStillSanitizes: the palette rides the SAME pass as the security
// chokepoint (10.2.6), so plugging a table in must not weaken it. A remap that
// let OSC 52 through would be a palette that opened a clipboard.
func TestRemapStillSanitizes(t *testing.T) {
	tbl := sanitize.Table(ANSI16Remap)
	hostile := []string{
		"\x1b]52;c;aGVsbG8=\x07clipboard",
		"\x1b]0;retitled\x07title",
		"\x1b]8;;http://evil\x07link\x1b]8;;\x07",
	}
	for _, in := range hostile {
		got := sanitize.TextWithPalette(in, tbl)
		if strings.Contains(got, "\x1b]") {
			t.Errorf("an OSC sequence survived the remap pass: %q -> %q", in, got)
		}
	}
}

// TestRemapIsIdempotent: the chokepoint may be crossed more than once as text
// moves between a poll, a cache and a render. Remapping twice must not walk a
// colour further each time.
func TestRemapIsIdempotent(t *testing.T) {
	for i := range 16 {
		if got := ANSI16Remap[ANSI16Remap[i]]; got != ANSI16Remap[i] {
			t.Errorf("remapping index %d twice gives %d then %d", i, ANSI16Remap[i], got)
		}
	}
}

// TestRemapPreservesMeaning states the table's design rules as assertions, so a
// future edit has to argue with them:
//
//  1. Every chromatic colour keeps its HUE FAMILY. Recolouring is for coherence,
//     not for reinterpretation — a tool's red must never come out green.
//  2. Nothing lands on a dark standard chromatic slot, which is illegible on a
//     dark ground and reads as a bug rather than as a colour choice.
//  3. Nothing lands on 15. Bright white is our primary tier's, and 5.13's
//     hierarchy is only a hierarchy if nothing else may occupy its top.
func TestRemapPreservesMeaning(t *testing.T) {
	family := func(i int) int {
		if i == 0 || i == 7 || i == 8 || i == 15 {
			return -1 // achromatic
		}
		return i % 8
	}
	for i := range 16 {
		out := ANSI16Remap[i]
		if out < 0 || out > 15 {
			t.Fatalf("index %d maps outside the palette, to %d", i, out)
		}
		if in, got := family(i), family(out); in != -1 && in != got {
			t.Errorf("index %d (family %d) maps to %d (family %d): a tool's colour changed meaning", i, in, out, got)
		}
		if out >= 1 && out <= 6 {
			t.Errorf("index %d maps to %d, a dark standard chromatic that is illegible on our ground", i, out)
		}
		if out == 15 {
			t.Errorf("index %d maps to bright white, which is our primary tier's alone", i)
		}
	}
}

// TestTokenForANSI16 is the fidelity path the index-to-index table cannot take.
// It must be total, and it must never hand a tool's colour an IDENTITY hue —
// identity means "which task", and a tool's blue does not.
func TestTokenForANSI16(t *testing.T) {
	want := map[int]Token{
		1: Coral, 9: Coral,
		2: Green, 10: Green,
		3: Amber, 11: Amber,
		6: Cyan, 14: Cyan,
	}
	for i := -5; i < 300; i++ {
		got := TokenForANSI16(i)
		if got >= tokenCount {
			t.Fatalf("TokenForANSI16(%d) returned an invalid token", i)
		}
		if _, isIdentity := IdentityIndex(got); isIdentity {
			t.Errorf("TokenForANSI16(%d) = %s: a tool's colour must never borrow an identity hue", i, got)
		}
		if got.IsSurface() {
			t.Errorf("TokenForANSI16(%d) = %s, which is a background", i, got)
		}
		if w, ok := want[i]; ok && got != w {
			t.Errorf("TokenForANSI16(%d) = %s, want %s", i, got, w)
		}
	}
}
