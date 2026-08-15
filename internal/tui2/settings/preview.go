package settings

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Live preview (8.2.19): a row that changes how the surface RENDERS shows one
// sample line in its own hint area, so the user sees the answer instead of
// reading about it. 12.7.E.3 names this as the reason the preview earns its
// place on an appearance row at all — "it renders one sample line in both
// tiers, so the user *sees* the answer".
//
// Three rules keep it from becoming a second renderer:
//
//   - ONE line, always. A preview that grew into a panel would be a second
//     surface inside the settings sheet, and the sheet is where that mistake
//     gets made.
//   - It costs a string concatenation, not a render pass. Nothing here builds
//     a Styler, measures a frame, or asks a sibling package to draw something.
//   - It only exists for rows that change RENDERING. A preview of a dollar
//     amount would be a decoration, and the row already shows its own value.
//
// previews is the whole table, keyed by registry key. It is also the seam for
// 12.7's `nerd_font` row: when that row lands in internal/config, its preview
// is one entry here — the sample glyph line in both tiers — and nothing else
// in this package changes.
var previews = map[string]func(value string) string{
	config.KeyLinearMode: linearPreview,
	config.KeySplitPct:   splitPreview,
}

// preview returns the sample line for a row, or "" when the row has none.
func (m *Model) preview(r row, width int) string {
	build, ok := previews[r.setting.Key]
	if !ok {
		return ""
	}
	sample := build(m.value(r))
	if width > 0 && ansi.StringWidth(sample) > width {
		// A preview clipped in half stops being a preview. On a terminal too
		// narrow to hold it, the row keeps its hint and loses the sample.
		return ""
	}
	return sample
}

// linearPreview shows the shape the chat surface takes, in one line: the
// accessible single column (10.1.5), or the two-pane frame with its divider.
func linearPreview(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "on") {
		return tokens.GlyphPromptChat + " one column " + tokens.GlyphSeparator +
			" no motion " + tokens.GlyphSeparator + " no spinners"
	}
	return tokens.GlyphPromptChat + " chat " + tokens.GlyphAccentRail + " tasks " +
		tokens.GlyphSeparator + " motion on"
}

// splitPreview draws the divider where the value puts it: a hairline for the
// chat side, the rail glyph for the divider, telemetry dots for the task side.
// The bar is fixed at 24 cells so the preview does not change length as the
// terminal does — a sample that resized would be measuring the terminal rather
// than showing the setting.
func splitPreview(value string) string {
	const bar = 24
	pct, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(value), "%"))
	if err != nil {
		return ""
	}
	pct = config.ClampSplitPct(pct)
	if pct == 0 {
		pct = config.DefaultSplitPct
	}
	chat := max(1, min(bar-1, pct*bar/100))
	return strings.Repeat(tokens.GlyphTreeDash, chat) + tokens.GlyphAccentRail +
		strings.Repeat(tokens.GlyphSeparator, bar-chat-1) + "  " + strconv.Itoa(pct) + "% chat"
}
