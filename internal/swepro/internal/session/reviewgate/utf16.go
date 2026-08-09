// This file supplies JavaScript UTF-16 string semantics used throughout
// src/session/review-gate.ts:1-1937 and
// src/session/review-synthesizer.ts:1-161.
package reviewgate

import (
	"unicode/utf8"
)

// The TypeScript source measures and slices strings in UTF-16 code units.
// WTF-8 keeps a split surrogate representable long enough for callers that do
// not cross Go's standard JSON encoder.
func decodeWTF8(value string, index int) (rune, int) {
	b := value[index]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xe0 == 0xc0:
		if index+1 < len(value) && value[index+1]&0xc0 == 0x80 {
			cp := rune(b&0x1f)<<6 | rune(value[index+1]&0x3f)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xf0 == 0xe0:
		if index+2 < len(value) &&
			value[index+1]&0xc0 == 0x80 &&
			value[index+2]&0xc0 == 0x80 {
			cp := rune(b&0x0f)<<12 |
				rune(value[index+1]&0x3f)<<6 |
				rune(value[index+2]&0x3f)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xf8 == 0xf0:
		if index+3 < len(value) &&
			value[index+1]&0xc0 == 0x80 &&
			value[index+2]&0xc0 == 0x80 &&
			value[index+3]&0xc0 == 0x80 {
			cp := rune(b&0x07)<<18 |
				rune(value[index+1]&0x3f)<<12 |
				rune(value[index+2]&0x3f)<<6 |
				rune(value[index+3]&0x3f)
			if cp >= 0x10000 && cp <= utf8.MaxRune {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xd800 && cp <= 0xdfff {
		return append(dst,
			byte(0xe0|cp>>12),
			byte(0x80|(cp>>6)&0x3f),
			byte(0x80|cp&0x3f),
		)
	}
	return utf8.AppendRune(dst, cp)
}

func utf16Length(value string) int {
	units := 0
	for index := 0; index < len(value); {
		cp, size := decodeWTF8(value, index)
		index += size
		if cp >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return units
}

func utf16Slice(value string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	total := utf16Length(value)
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	if start == end {
		return ""
	}

	out := make([]byte, 0, len(value))
	position := 0
	for index := 0; index < len(value); {
		cp, size := decodeWTF8(value, index)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		next := position + width
		switch {
		case next <= start || position >= end:
		case width == 1:
			out = append(out, value[index:index+size]...)
		default:
			high := rune(0xd800 + ((cp - 0x10000) >> 10))
			low := rune(0xdc00 + ((cp - 0x10000) & 0x3ff))
			if start <= position && position < end {
				if end == position+1 {
					out = appendWTF8(out, high)
				} else {
					out = append(out, value[index:index+size]...)
				}
			} else if start == position+1 && position+1 < end {
				out = appendWTF8(out, low)
			}
		}
		position = next
		index += size
	}
	return string(out)
}

func compact(text string, limit int) string {
	if utf16Length(text) <= limit {
		return text
	}
	return utf16Slice(text, 0, limit-3) + "..."
}
