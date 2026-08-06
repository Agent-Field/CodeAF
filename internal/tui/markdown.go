package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown styles the subset of markdown that actually appears in
// model output — code fences, inline code, bold, headings, bullets, quotes —
// and preserves blank-line spacing. It never fails: anything unrecognised
// passes through as plain wrapped text, so a half-formed document degrades
// to exactly what was written.
var (
	mdCode    = lipgloss.NewStyle().Foreground(butter)
	mdHeading = lipgloss.NewStyle().Foreground(lavender).Bold(true)
	mdQuote   = lipgloss.NewStyle().Foreground(muted)
	mdBold    = lipgloss.NewStyle().Bold(true)
)

func renderMarkdown(text string, width int) string {
	if width < 4 {
		width = 4
	}
	var out []string
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out = append(out, mutedStyle.Faint(true).Render(truncate("  "+trimmed, width)))
			continue
		}
		if inFence {
			out = append(out, mdCode.Render(truncate("  "+strings.TrimRight(line, " "), width)))
			continue
		}
		switch {
		case trimmed == "":
			out = append(out, "")
		case strings.HasPrefix(trimmed, "/") && !strings.ContainsAny(trimmed, " \t"):
			out = append(out, pathLink(trimmed, width))
		case strings.HasPrefix(trimmed, "#"):
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			out = append(out, mdHeading.Render(truncate(heading, width)))
		case strings.HasPrefix(trimmed, "> "):
			for _, wrapped := range strings.Split(wrapText(strings.TrimPrefix(trimmed, "> "), width-2), "\n") {
				out = append(out, mdQuote.Render("│ "+wrapped))
			}
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ "):
			body := renderInlineMarkdown(trimmed[2:])
			for index, wrapped := range strings.Split(wrapText(body, width-2), "\n") {
				prefix := "• "
				if index > 0 {
					prefix = "  "
				}
				out = append(out, prefix+wrapped)
			}
		default:
			out = append(out, strings.Split(wrapText(renderInlineMarkdown(line), width), "\n")...)
		}
	}
	return strings.Join(out, "\n")
}

// renderInlineMarkdown styles `code` and **bold** spans. Splitting on the
// delimiters keeps it allocation-cheap and impossible to break: an unpaired
// delimiter leaves the text untouched.
func renderInlineMarkdown(text string) string {
	if strings.Count(text, "`") >= 2 {
		parts := strings.Split(text, "`")
		var rebuilt strings.Builder
		for index, part := range parts {
			if index%2 == 1 && index < len(parts)-(len(parts)%2) {
				rebuilt.WriteString(mdCode.Render(part))
			} else {
				rebuilt.WriteString(styleBold(part))
			}
		}
		return rebuilt.String()
	}
	return styleBold(text)
}

// pathLink renders an absolute file path as an OSC 8 hyperlink: the display
// text may be shortened to fit, but the link target is always the full path,
// so cmd-click opens the real file even when the line was too narrow to show
// all of it.
func pathLink(path string, width int) string {
	display := path
	if len(display) > width && width > 1 {
		display = "…" + display[len(display)-(width-1):]
	}
	styled := lipgloss.NewStyle().Foreground(powder).Underline(true).Render(display)
	return "\x1b]8;;file://" + path + "\x1b\\" + styled + "\x1b]8;;\x1b\\"
}

func styleBold(text string) string {
	if strings.Count(text, "**") < 2 {
		return text
	}
	parts := strings.Split(text, "**")
	var rebuilt strings.Builder
	for index, part := range parts {
		if index%2 == 1 && index < len(parts)-(len(parts)%2) {
			rebuilt.WriteString(mdBold.Render(part))
		} else {
			rebuilt.WriteString(part)
		}
	}
	return rebuilt.String()
}
