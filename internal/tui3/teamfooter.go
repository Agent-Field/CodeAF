package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Team dialogs share quiet key-first hints. Wrapping keeps every instruction
// visible on a narrow card, and clickable hints keep their own exact targets.
type teamFooterHint struct {
	key, action string
	hit         *wallHit
}

func teamHint(key, action string, hit ...wallHit) teamFooterHint {
	h := teamFooterHint{key: key, action: action}
	if len(hit) > 0 {
		h.hit = &hit[0]
	}
	return h
}

func teamFooter(pal palette, width int, hints ...teamFooterHint) []wallCardLine {
	if width < 1 {
		return nil
	}
	var lines []wallCardLine
	text := ""
	var hits []wallHit
	flush := func() {
		if text == "" {
			return
		}
		pad := max(width-ansi.StringWidth(text), 0)
		for i := range hits {
			hits[i].x0 += pad
			hits[i].x1 += pad
		}
		lines = append(lines, wallCardLine{s: pal.muted(strings.Repeat(" ", pad) + text), hits: hits})
		text, hits = "", nil
	}
	for _, hint := range hints {
		word := fit(hint.key+" "+hint.action, width)
		sep := ""
		if text != "" {
			sep = " · "
		}
		if ansi.StringWidth(text+sep+word) > width {
			flush()
			sep = ""
		}
		x := ansi.StringWidth(text + sep)
		text += sep + word
		if hint.hit != nil {
			h := *hint.hit
			h.x0, h.x1, h.y0, h.y1 = x, x+ansi.StringWidth(word), 0, 1
			hits = append(hits, h)
		}
	}
	flush()
	return lines
}

// The compact grid prompt places its footer directly in a frame row.
func teamFooterHitsAt(hits []wallHit, x, y int) []wallHit {
	shifted := append([]wallHit(nil), hits...)
	for i := range shifted {
		shifted[i].x0 += x
		shifted[i].x1 += x
		shifted[i].y0 += y
		shifted[i].y1 += y
	}
	return shifted
}
