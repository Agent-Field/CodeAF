package tui

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown styles the subset of markdown that actually appears in
// model output — code fences, inline code, bold, headings, bullets, quotes —
// and preserves blank-line spacing. It never fails: anything unrecognised
// passes through as plain wrapped text, so a half-formed document degrades
// to exactly what was written.
var (
	mdCode    = lipgloss.NewStyle().Foreground(butter)
	mdHeading = lipgloss.NewStyle().Foreground(ink).Bold(true)
	mdQuote   = lipgloss.NewStyle().Foreground(muted)
	mdBold    = lipgloss.NewStyle().Foreground(ink).Bold(true)
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
	return artifactLink(path, path, "", width)
}

func artifactLink(target, display, glyph string, width int) string {
	prefix := glyph
	if prefix != "" {
		prefix += " "
	}
	room := max(1, width-lipgloss.Width(prefix))
	display = truncate(display, room)
	styled := lipgloss.NewStyle().Foreground(powder).Underline(true).Render(display)
	link := (&url.URL{Scheme: "file", Path: target}).String()
	return mutedStyle.Render(prefix) + "\x1b]8;;" + link + "\x1b\\" + styled + "\x1b]8;;\x1b\\"
}

type mediaPathResolver interface {
	ResolveMediaPath(nodeID, relative string) (string, bool)
}

func (m *Model) renderMediaArtifacts(message store.Message, width int) string {
	paths := append([]string(nil), message.Attachments...)
	paths = append(paths, mediaReferences(message.Body)...)
	seen := make(map[string]bool, len(paths))
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		glyph, ok := mediaGlyph(path)
		if !ok {
			continue
		}
		target := path
		if !filepath.IsAbs(target) {
			resolver, ok := m.commander.(mediaPathResolver)
			if !ok {
				continue
			}
			var found bool
			target, found = resolver.ResolveMediaPath(message.NodeID, path)
			if !found {
				continue
			}
		}
		lines = append(lines, artifactLink(target, filepath.ToSlash(path), glyph, width))
	}
	return strings.Join(lines, "\n")
}

func mediaReferences(body string) []string {
	var paths []string
	for _, field := range strings.Fields(body) {
		candidate := strings.Trim(field, "\"'`()[]{}<>,.!?:;")
		if strings.HasPrefix(filepath.ToSlash(candidate), "media/") {
			if _, ok := mediaGlyph(candidate); ok {
				paths = append(paths, candidate)
			}
		}
	}
	return paths
}

func mediaGlyph(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "⌾", true
	case ".mp3", ".wav", ".m4a", ".ogg", ".flac":
		return "♪", true
	case ".mp4", ".mov", ".m4v", ".webm":
		return "▶", true
	default:
		return "", false
	}
}

func styleBold(text string) string {
	if strings.Count(text, "**") < 2 {
		return inputTextStyle.Render(text)
	}
	parts := strings.Split(text, "**")
	var rebuilt strings.Builder
	for index, part := range parts {
		if index%2 == 1 && index < len(parts)-(len(parts)%2) {
			rebuilt.WriteString(mdBold.Render(part))
		} else {
			rebuilt.WriteString(inputTextStyle.Render(part))
		}
	}
	return rebuilt.String()
}
