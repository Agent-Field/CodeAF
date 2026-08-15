package tokens

import "math"

// Color is a 24-bit sRGB value. It is a comparable value type with no pointer
// inside it, so passing one costs nothing and a table of them is contiguous.
type Color struct{ R, G, B uint8 }

// hexDigits is used by the hex writers; a table lookup beats fmt on a render
// path that may run for every cell of every frame.
const hexDigits = "0123456789ABCDEF"

// AppendHex writes c as "#RRGGBB" (upper case, the form used in this
// package's literals) and returns the extended buffer. It allocates nothing
// when dst has room.
func (c Color) AppendHex(dst []byte) []byte {
	dst = append(dst, '#')
	for _, v := range [3]uint8{c.R, c.G, c.B} {
		dst = append(dst, hexDigits[v>>4], hexDigits[v&0x0F])
	}
	return dst
}

// Hex renders c as "#RRGGBB". Consumers building lipgloss styles want this
// form; consumers writing escape sequences want [Token.Fg] instead, which is
// precomputed and needs no parsing on the other side.
func (c Color) Hex() string {
	var buf [7]byte
	return string(c.AppendHex(buf[:0]))
}

// MustHex parses "#RRGGBB" and panics on anything else. It exists so the
// palette can be written as literal hex — the form a designer reads — while
// still being one canonical type in memory. Every call site is a package-level
// constant string in this file's neighbourhood, so a panic here is a build-time
// error in practice, never a runtime one.
func MustHex(s string) Color {
	c, ok := ParseHex(s)
	if !ok {
		panic("tokens: bad hex color " + s)
	}
	return c
}

// ParseHex parses "#RRGGBB" or "RRGGBB", case-insensitively.
func ParseHex(s string) (Color, bool) {
	if len(s) == 7 {
		if s[0] != '#' {
			return Color{}, false
		}
		s = s[1:]
	}
	if len(s) != 6 {
		return Color{}, false
	}
	var out [3]uint8
	for i := range out {
		hi, ok1 := unhex(s[i*2])
		lo, ok2 := unhex(s[i*2+1])
		if !ok1 || !ok2 {
			return Color{}, false
		}
		out[i] = hi<<4 | lo
	}
	return Color{out[0], out[1], out[2]}, true
}

func unhex(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// Mix blends a toward b by t in [0,1] in sRGB byte space, rounding half away
// from zero. Byte-space blending (rather than linear-light blending) is
// deliberate: it is what a terminal user perceives as "the same color, quieter",
// and it is the rule the shipped dimmed and tinted literals in palette.go were
// derived with — palette_test.go re-derives them through this function, so the
// data and the law can never drift apart.
func Mix(a, b Color, t float64) Color {
	return Color{mixByte(a.R, b.R, t), mixByte(a.G, b.G, t), mixByte(a.B, b.B, t)}
}

func mixByte(a, b uint8, t float64) uint8 {
	v := float64(a) + math.Round(float64(int(b)-int(a))*t)
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return uint8(v)
}

// Luminance is the WCAG relative luminance of c: the sRGB channels linearized
// and weighted for human sensitivity. Used only by tests and by callers that
// want to sort colors by perceived brightness.
func (c Color) Luminance() float64 {
	return 0.2126*linearize(c.R) + 0.7152*linearize(c.G) + 0.0722*linearize(c.B)
}

func linearize(v uint8) float64 {
	s := float64(v) / 255
	if s <= 0.04045 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

// Contrast is the WCAG contrast ratio between two colors, in [1, 21]. The
// palette's shipping gate (contrast_test.go) is expressed in these numbers:
// 4.5 is AA for body text, 3.0 is AA for large text and for non-text UI
// components.
func Contrast(a, b Color) float64 {
	la, lb := a.Luminance(), b.Luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// distance is a cheap perceptual-ish squared distance used to pick the nearest
// entry of a fixed terminal palette. The weights are the classic low-cost
// approximation of human channel sensitivity; a full CIEDE2000 would move no
// mapping in a palette this small and would cost a cube root per candidate.
func distance(a, b Color) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return 2*dr*dr + 4*dg*dg + 3*db*db
}
