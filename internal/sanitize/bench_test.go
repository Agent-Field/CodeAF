package sanitize

import "testing"

// BenchmarkTextFastPath is the render-hot-path case this package is tuned
// for: ordinary text with no ESC/C1 byte anywhere in it. It must show 0
// allocations — the fast path returns the input string unchanged.
func BenchmarkTextFastPath(b *testing.B) {
	s := "Sure — here's a plain reply with no escape sequences in it at all, " +
		"just an ordinary sentence about as long as a real chat turn tends to run."
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Text(s)
	}
}

// BenchmarkTextWithSGR exercises the slow path on realistic styled tool
// output (a handful of SGR spans in an otherwise plain line).
func BenchmarkTextWithSGR(b *testing.B) {
	s := "\x1b[32m+added line\x1b[0m one\n\x1b[31m-removed line\x1b[0m two\nplain three"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Text(s)
	}
}

// BenchmarkTextWithHostileOSC exercises the slow path against a payload
// carrying the sequences this package exists to neutralize.
func BenchmarkTextWithHostileOSC(b *testing.B) {
	s := "before \x1b]52;c;aGVsbG8gd29ybGQ=\x07 middle \x1b]8;;https://evil.example\x1b\\link\x1b]8;;\x1b\\ after"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Text(s)
	}
}

// BenchmarkTextWithPaletteIdentity checks that composing the palette-remap
// mode with the identity table costs the same as plain Text.
func BenchmarkTextWithPaletteIdentity(b *testing.B) {
	s := "\x1b[32m+added\x1b[0m \x1b[31m-removed\x1b[0m plain text tail"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = TextWithPalette(s, Identity)
	}
}
