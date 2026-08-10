package adaptive

// JS string primitives this port needs and Go does not provide identically.
// The bodies are the same ones internal/session/hygiene and
// internal/session/ledgers use; they are duplicated rather than imported
// because cross-package sharing of port-local helpers is out of scope for the
// wave (jscompat is the only shared surface, and it does not carry these).

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ── String.prototype.toLowerCase ─────────────────────────────────────────

// jsLowerCase mirrors String.prototype.toLowerCase (locale-independent, FULL
// Unicode lowercase). Go's strings.ToLower implements the SIMPLE mapping; the
// default (non-language-sensitive) part of SpecialCasing.txt adds exactly two
// rules on top of it:
//
//   - U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE → "i" + U+0307 (two code
//     units, so it also changes the string's length).
//   - Final_Sigma: U+03A3 → U+03C2 when preceded by a cased letter and not
//     followed by one, skipping case-ignorable characters on both sides.
//
// Reachable here: deriveFamily lowercases a caller-supplied model id, and
// normalizeConfig lowercases a caller-supplied `family`.
func jsLowerCase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			b.WriteRune('i')
			b.WriteRune(0x0307)
		case r == 0x03A3 && isFinalSigma(runes, i):
			b.WriteRune(0x03C2)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isFinalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !isCased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && isCaseIgnorable(runes[k]) {
		k++
	}
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', 0x2019, 0x00AD, 0x02B9, 0x0385, 0x1FBF, 0x1FC1, 0x1FCD, 0x1FCE,
		0x1FCF, 0x1FDD, 0x1FDE, 0x1FDF, 0x1FED, 0x1FEE, 0x1FEF, 0x1FFD, 0x1FFE, 0x2027:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r)
}

// ── UTF-16 slicing (String.prototype.slice) ──────────────────────────────

// decodeWTF8 decodes one code point at s[i], accepting the surrogate range
// D800-DFFF that utf8.DecodeRuneInString rejects, so a string that already
// holds an unpaired surrogate (produced by an earlier slice) stays stable.
func decodeWTF8(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xE0 == 0xC0:
		if i+1 < len(s) && s[i+1]&0xC0 == 0x80 {
			cp := rune(b&0x1F)<<6 | rune(s[i+1]&0x3F)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xF0 == 0xE0:
		if i+2 < len(s) && s[i+1]&0xC0 == 0x80 && s[i+2]&0xC0 == 0x80 {
			cp := rune(b&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xF8 == 0xF0:
		if i+3 < len(s) && s[i+1]&0xC0 == 0x80 && s[i+2]&0xC0 == 0x80 && s[i+3]&0xC0 == 0x80 {
			cp := rune(b&0x07)<<18 | rune(s[i+1]&0x3F)<<12 | rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
			if cp >= 0x10000 && cp <= 0x10FFFF {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xD800 && cp <= 0xDFFF {
		return append(dst,
			byte(0xE0|cp>>12),
			byte(0x80|(cp>>6)&0x3F),
			byte(0x80|cp&0x3F))
	}
	return utf8.AppendRune(dst, cp)
}

// utf16SliceTo is JS `s.slice(0, n)`: the cut lands on a UTF-16 code-unit
// boundary, so slicing through an astral character keeps its HIGH surrogate
// alone. register() clips error text at 400 and 300 units this way.
func utf16SliceTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		if units+width > n {
			high := rune(0xD800 + ((cp - 0x10000) >> 10))
			return s[:i] + string(appendWTF8(nil, high))
		}
		units += width
		i += size
		if units == n {
			return s[:i]
		}
	}
	return s
}
