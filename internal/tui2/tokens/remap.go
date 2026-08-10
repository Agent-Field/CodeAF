package tokens

// ANSI-16 remap for tool output (10.2.7, 12.4.4).
//
// Tool output arrives in someone else's colors. The sanitizer already parses
// every SGR sequence at the one output chokepoint and can re-slot the classic
// sixteen on the way through; what it has lacked is a palette to re-slot them
// ONTO. This is that table.
//
// What the mechanism can and cannot do, stated plainly, because the difference
// matters for what anyone should expect on screen: sanitize.Table maps an index
// to an index. It cannot make a tool's red into our coral, because the actual
// pixels of index 9 belong to the user's terminal theme, not to us. What it CAN
// do is (a) move dark, unreadable standard colors into the bright family where
// they clear contrast on a dark ground, and (b) cap how loud a tool is allowed
// to be, so nothing in someone else's text outranks our own primary tier. That
// is coherence, not fidelity. True fidelity would require rewriting 30-37 into
// 38;2;R;G;B truecolor sequences — a strictly larger change to the sanitizer's
// writer, noted for a later wave.

// ANSI16Remap is the table the sanitizer plugs in place of its Identity. It is
// a plain [16]int so this package stays a leaf; the call site converts:
//
//	sanitize.TextWithPalette(s, sanitize.Table(tokens.ANSI16Remap))
//
// Entry by entry:
//
//	0  black         → 0   the ground; a tool drawing on the ground is correct
//	1  red           → 9   coral lives in the bright family; standard red
//	2  green         → 10  (#800000-ish) is illegible on a dark ground and
//	3  yellow        → 11  reads as a bug, not as a color choice. Every
//	4  blue          → 12  chromatic standard color moves to its bright
//	5  magenta       → 13  counterpart: same hue, same meaning, legible.
//	6  cyan          → 14
//	7  white         → 7   a tool's body text sits at our secondary tier
//	8  bright black  → 8   a tool's own dim text stays our chrome tier
//	9  bright red    → 9   the bright chromatics are already where our pastel
//	10 bright green  → 10  vocabulary lives; they pass through untouched, and
//	11 bright yellow → 11  the hue families line up one-to-one with 5.16's
//	12 bright blue   → 12  words (9 broken, 10 success, 11 needs-a-human,
//	13 bright magenta→ 13  14 alive; 12 and 13 have no semantic word and read
//	14 bright cyan   → 14  as identity-ish, which is the honest reading of a
//	15 bright white  → 7   tool's own accent).
//
// The one demotion is 15 → 7: bright white is a tool shouting, and the primary
// text tier is reserved for our own voice (5.13's hierarchy is only a hierarchy
// if nothing else may occupy its top). A tool's emphasis survives as position
// and as its own internal contrast against 8; it just stops outranking the
// sentence the user is reading.
var ANSI16Remap = [16]int{
	0, 9, 10, 11, 12, 13, 14, 7,
	8, 9, 10, 11, 12, 13, 14, 7,
}

// TokenForANSI16 answers the other direction: which of our tokens is the
// nearest reading of a raw ANSI-16 index. It is what a renderer uses when it
// wants to draw remapped tool output in OUR colors on a truecolor terminal —
// the fidelity path the table above cannot take. Indices with no semantic word
// in the five-hue vocabulary resolve to the grey ramp rather than borrowing an
// identity hue, because identity means "which task", and a tool's blue does
// not.
func TokenForANSI16(index int) Token {
	switch index {
	case 1, 9:
		return Coral
	case 2, 10:
		return Green
	case 3, 11:
		return Amber
	case 6, 14:
		return Cyan
	case 4, 12, 5, 13:
		return TextSecondary
	case 8:
		return TextTertiary
	case 15:
		return TextSecondary
	case 7:
		return TextSecondary
	}
	return TextTertiary
}
