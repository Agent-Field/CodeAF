package jscompat

import (
	"math"
	"strconv"
	"strings"
)

// FormatNumber reproduces JS Number → string conversion (the `String(n)` /
// template-literal path, which JSON.stringify also uses for finite numbers).
// V8 prints the shortest round-trip decimal, switching to exponent form only
// for |v| >= 1e21 or 0 < |v| < 1e-6, and prints exponents without zero
// padding ("1e-7", not "1e-07"). Go's strconv 'g' format switches to exponent
// far earlier and pads, so this must be hand-rolled.
func FormatNumber(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == 0 {
		// JS prints both +0 and -0 as "0".
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	// Exponent form, JS-style: mantissa uses shortest repr, exponent has an
	// explicit sign and no zero padding.
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// Go: "1.5e-07" / "1e+21" → JS: "1.5e-7" / "1e+21"
	i := strings.IndexByte(s, 'e')
	mantissa, exp := s[:i], s[i+1:]
	sign := ""
	if exp[0] == '+' || exp[0] == '-' {
		sign, exp = string(exp[0]), exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	return mantissa + "e" + sign + exp
}

// ToNumber reproduces JS Number(string) coercion for the cases the plandb
// cli-bridge exercises: leading/trailing JS whitespace is trimmed, the empty
// string is 0, "0x"/"0o"/"0b" radix prefixes are honored, "Infinity" parses,
// and anything unparseable is NaN.
func ToNumber(s string) float64 {
	t := Trim(s)
	if t == "" {
		return 0
	}
	neg := false
	if strings.HasPrefix(t, "+") {
		t = t[1:]
	} else if strings.HasPrefix(t, "-") {
		neg = true
		t = t[1:]
	}
	if t == "Infinity" {
		if neg {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	if len(t) > 2 && t[0] == '0' {
		var base int
		switch t[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			// Radix literals reject signs in JS: Number("-0x10") is NaN.
			if neg {
				return math.NaN()
			}
			n, err := strconv.ParseUint(t[2:], base, 64)
			if err != nil {
				return math.NaN()
			}
			return float64(n)
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return math.NaN()
	}
	if neg {
		return -f
	}
	return f
}

// Truthy reproduces JS truthiness for a number: 0, -0 and NaN are falsy.
func Truthy(f float64) bool {
	return f != 0 && !math.IsNaN(f)
}

// ToInt32Index converts a JS number used as an array index/count the way
// Array.prototype.slice does: NaN → 0, truncation toward zero.
func toSliceInt(f float64, length int) int {
	if math.IsNaN(f) {
		return 0
	}
	n := int(math.Trunc(f))
	if n < 0 {
		n += length
		if n < 0 {
			n = 0
		}
	}
	if n > length {
		n = length
	}
	return n
}

// SliceTo reproduces arr.slice(0, end) for a JS-number end value, including
// negative-from-the-end and NaN semantics. Returns a copy.
func SliceTo[T any](s []T, end float64) []T {
	n := toSliceInt(end, len(s))
	out := make([]T, n)
	copy(out, s[:n])
	return out
}

// Trim reproduces String.prototype.trim: JS WhiteSpace ∪ LineTerminator,
// which includes U+FEFF (BOM) — Go's unicode.IsSpace does not.
func Trim(s string) string {
	return strings.TrimFunc(s, isJSWhitespace)
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// Mulberry32 is the seeded PRNG the TS side uses when determinism is needed
// (src/router/adaptive.ts). Implemented here so a TS↔Go differential harness
// can pin Math.random on both sides to the identical stream.
func Mulberry32(seed uint32) func() float64 {
	state := seed
	return func() float64 {
		state += 0x6d2b79f5
		z := state
		z = (z ^ (z >> 15)) * (z | 1)
		z ^= z + (z^(z>>7))*(z|61)
		return float64(z^(z>>14)) / 4294967296.0
	}
}
