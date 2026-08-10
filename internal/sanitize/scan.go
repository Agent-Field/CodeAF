package sanitize

import "unicode/utf8"

// scanEscape looks at the byte(s) immediately after an ESC (already
// consumed by the caller at index start-1) and returns the index just past
// the whole sequence, plus whether that sequence is an SGR CSI ("ESC '['
// ... 'm'") and therefore allowlisted content the caller should copy
// through. Every branch is bounds-checked before it indexes s, and every
// branch advances start by at least one byte, so a lone trailing ESC (or
// any other truncated form) is consumed to len(s) and returned, never
// panics, never loops.
func scanEscape(s string, start int) (next int, sgr bool) {
	if start >= len(s) {
		return start, false // a bare trailing ESC with nothing after it
	}
	switch s[start] {
	case '[':
		return scanCSIBody(s, start+1)
	case ']':
		return scanStringBody(s, start+1), false
	case 'P', 'X', '^', '_':
		return scanStringBody(s, start+1), false
	default:
		return scanSimpleEscape(s, start), false
	}
}

// scanCSIBody consumes a CSI sequence's parameter/intermediate bytes and
// its final byte, starting right after the introducer (ESC '[' or the C1
// 0x9b). It reports whether the final byte was 'm' (SGR) — the only CSI
// family this package allowlists — and always returns an index that has
// made forward progress, even when no final byte is ever found (a
// truncated CSI consumes to end of string and is reported as non-SGR).
func scanCSIBody(s string, start int) (next int, sgr bool) {
	i := start
	for i < len(s) {
		b := s[i]
		switch {
		case b >= 0x30 && b <= 0x3f: // parameter bytes
			i++
		case b >= 0x20 && b <= 0x2f: // intermediate bytes
			i++
		case b >= 0x40 && b <= 0x7e: // final byte
			return i + 1, b == 'm'
		default:
			// Something that cannot legally appear inside a CSI sequence
			// (a raw control byte, an embedded ESC, whatever an
			// adversarial corpus throws at it) ends the sequence here
			// without consuming the offending byte, so the outer loop in
			// render gets a chance to interpret it on its own terms.
			return i, false
		}
	}
	return len(s), false
}

// scanStringBody consumes an ECMA-48 "string" construct (OSC/DCS/SOS/PM/
// APC) starting right after its introducer, up to and including its
// terminator. BEL is accepted as a terminator for all five, which is
// stricter than the letter of the spec (only OSC is BEL-terminated in
// practice) but never a security hole — it only ever means this package
// strips slightly more than the ECMA-48 minimum, never less. ST is
// accepted in both its two-byte ESC '\\' form and its single-byte C1
// (0x9c) form. An ESC that is not immediately followed by '\\' does not
// terminate the string — it is payload, exactly like the "nested ESC"
// case in the adversarial corpus — and scanning continues past it. A
// string with no terminator before the end of input is consumed entirely;
// there is nothing after it that could be interpreted safely anyway.
func scanStringBody(s string, start int) int {
	i := start
	for i < len(s) {
		switch s[i] {
		case bel:
			return i + 1
		case c1ST:
			return i + 1
		case esc:
			if i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			i++
		default:
			i++
		}
	}
	return len(s)
}

// scanSimpleEscape consumes the non-string, non-CSI ESC forms: a bare
// "Fp"/"Fe"/"Fs" two-byte escape (ESC 'c', ESC '7', ESC '=', ...) or an
// "nF" escape with one or more intermediate bytes before its final byte
// (ESC '(' 'B', ...). None of these are in the allowlist, so the return
// value is always just "how far to skip" — render only calls this for its
// side effect on i.
func scanSimpleEscape(s string, start int) int {
	i := start
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++ // intermediate bytes
	}
	if i < len(s) {
		return i + 1 // the final byte, whatever it is
	}
	return i // ran off the end mid-sequence; nothing left to consume
}

// runeSize reports how many bytes of s belong to the rune at its start —
// the whole multi-byte encoding for a valid rune, or exactly 1 for a
// malformed leading byte (utf8.DecodeRuneInString's documented behavior on
// error). It is never 0, so a caller that does `i += runeSize(s[i:])` always
// makes progress regardless of how malformed s is.
func runeSize(s string) int {
	_, size := utf8.DecodeRuneInString(s)
	return size
}
