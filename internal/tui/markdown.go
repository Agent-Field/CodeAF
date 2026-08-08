package tui

import (
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown styles the subset of markdown that actually appears in
// model output — code fences, inline code, bold, headings, bullets, quotes —
// and preserves blank-line spacing. It never fails: anything unrecognised
// passes through as plain wrapped text, so a half-formed document degrades
// to exactly what was written.
var (
	mdCode    = butterStyle
	mdHeading = inkStyle.Bold(true)
	mdQuote   = lipgloss.NewStyle().Foreground(muted)
	mdBold    = inkStyle.Bold(true)
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
	styled := powderStyle.Underline(true).Render(display)
	return mutedStyle.Render(prefix) + osc8FileLink(target, styled)
}

func osc8FileLink(target, display string) string {
	link := (&url.URL{Scheme: "file", Path: target}).String()
	return "\x1b]8;;" + link + "\x1b\\" + display + "\x1b]8;;\x1b\\"
}

type workspacePathResolver interface {
	ResolveWorkspacePath(nodeID, relative string) (string, bool)
}

type workspaceDirectoryResolver interface {
	WorkspacePath(nodeID string) (string, bool)
}

type mediaPathResolver interface {
	ResolveMediaPath(nodeID, relative string) (string, bool)
}

func (m *Model) resolveWorkspacePath(nodeID, relative string) (string, bool) {
	if resolver, ok := m.commander.(workspacePathResolver); ok {
		return resolver.ResolveWorkspacePath(nodeID, relative)
	}
	// Compatibility for embedders written against the original media-only
	// seam. The production commander implements the generalized interface.
	if resolver, ok := m.commander.(mediaPathResolver); ok {
		return resolver.ResolveMediaPath(nodeID, relative)
	}
	return "", false
}

func (m *Model) workspaceDirectoryLink(nodeID string) string {
	resolver, ok := m.commander.(workspaceDirectoryResolver)
	if !ok {
		return ""
	}
	target, found := resolver.WorkspacePath(nodeID)
	if !found {
		return ""
	}
	return osc8FileLink(target, mutedStyle.Faint(true).Render("▸ workspace"))
}

func (m *Model) renderMediaArtifacts(message store.Message, width int) string {
	paths := make([]string, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		// An attachment is journaled as a reference to our own copy; what a
		// person wants to click is still their own file, under its own name.
		paths = append(paths, cas.SourcePath(attachment))
	}
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
			var found bool
			target, found = m.resolveWorkspacePath(message.NodeID, path)
			if !found {
				continue
			}
		}
		lines = append(lines, artifactLink(target, filepath.ToSlash(path), glyph, width))
	}
	return strings.Join(lines, "\n")
}

var workspaceToken = regexp.MustCompile(`\S+`)

type workspaceReferenceSpan struct {
	start int
	end   int
}

// linkWorkspaceReferences turns only existing workspace-relative files into
// inline OSC 8 links. Escape sequences wrap the original bytes, so punctuation,
// markdown, and visible text remain byte-for-byte recognizable after ANSI is
// stripped. The resolver enforces both containment and existence.
func (m *Model) linkWorkspaceReferences(nodeID, body string) string {
	if nodeID == "" || body == "" {
		return body
	}
	spans := workspaceReferenceSpans(body)
	if len(spans) == 0 {
		return body
	}
	var linked strings.Builder
	linked.Grow(len(body))
	previous := 0
	for _, span := range spans {
		if span.start < previous {
			continue
		}
		candidate := body[span.start:span.end]
		if candidate == "" || filepath.IsAbs(candidate) || strings.Contains(candidate, "://") {
			continue
		}
		target, found := m.resolveWorkspacePath(nodeID, filepath.Clean(candidate))
		if !found {
			continue
		}
		linked.WriteString(body[previous:span.start])
		linked.WriteString(osc8FileLink(target, candidate))
		previous = span.end
	}
	if previous == 0 {
		return body
	}
	linked.WriteString(body[previous:])
	return linked.String()
}

func workspaceReferenceSpans(body string) []workspaceReferenceSpan {
	spans := make([]workspaceReferenceSpan, 0)
	// A quoted or inline-code path may contain spaces. Try the complete visible
	// span first; existence checking keeps ordinary quoted prose untouched.
	for start := 0; start < len(body); start++ {
		quote := body[start]
		if quote != '\'' && quote != '"' && quote != '`' {
			continue
		}
		if end := strings.IndexByte(body[start+1:], quote); end >= 0 {
			end += start + 1
			if end > start+1 && !strings.ContainsAny(body[start+1:end], "\r\n") {
				spans = append(spans, workspaceReferenceSpan{start: start + 1, end: end})
			}
			start = end
		}
	}
	for _, match := range workspaceToken.FindAllStringIndex(body, -1) {
		start, end := match[0], match[1]
		token := body[start:end]
		leading := len(token) - len(strings.TrimLeft(token, "\"'`()[]{}<>*"))
		trimmed := strings.TrimRight(token[leading:], "\"'`()[]{}<>,.!?:;*")
		if len(trimmed) > 0 {
			spans = append(spans, workspaceReferenceSpan{start: start + leading, end: start + leading + len(trimmed)})
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	return spans
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
	case ".pdf":
		return "▤", true
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
