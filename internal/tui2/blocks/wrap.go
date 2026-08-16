package blocks

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Wrap greedily wraps plain text to width cells, appending the rows to dst and
// returning the grown slice together with the byte offset at which the LAST row
// begins.
//
// That offset is what makes streaming cheap. Under greedy wrapping, appending
// text can only change the last row: every earlier row is already decided. A
// streaming block keeps the earlier rows as its settled head and re-wraps from
// the offset, so a frame costs O(the tail) rather than O(the reply).
//
// Text is treated as plain: the ANSI sanitizer upstream (10.2) is what makes
// that safe, and a body carrying raw escapes would have no honest cell
// boundaries to wrap on.
func Wrap(dst []string, text string, width int) ([]string, int) {
	if width < 1 {
		width = 1
	}
	rows := dst
	start, lastBreak, cells := 0, -1, 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == '\n' {
			rows = append(rows, text[start:i])
			i += size
			start, lastBreak, cells = i, -1, 0
			continue
		}
		w := runeCells(text, i, size)
		if cells+w > width {
			switch {
			case r == ' ':
				// The overflowing rune IS the separator: break here and
				// swallow it, so the next row never opens on a space.
				rows = append(rows, strings.TrimRight(text[start:i], " "))
				i += size
				for i < len(text) && text[i] == ' ' {
					i++
				}
				start, lastBreak, cells = i, -1, 0
				continue
			case lastBreak > start:
				rows = append(rows, strings.TrimRight(text[start:lastBreak], " "))
				start = lastBreak + 1
				for start < i && text[start] == ' ' {
					start++
				}
				// The carried text is a single word — the break we just used
				// was the last space — so there is no break left inside it.
				cells, lastBreak = stringWidth(text[start:i]), -1
			default:
				// A word longer than the row: break it where the row ends.
				rows = append(rows, text[start:i])
				start, lastBreak, cells = i, -1, 0
			}
		}
		if r == ' ' {
			lastBreak = i
		}
		cells += w
		i += size
	}
	return append(rows, text[start:]), start
}

func runeCells(s string, i, size int) int {
	if size == 1 {
		if s[i] < 0x20 || s[i] == 0x7f {
			return 0
		}
		return 1
	}
	return ansi.StringWidth(s[i : i+size])
}
