// Package pagedoc renders workstream page editor JSON documents to markdown.
package pagedoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Markdown renders a workstream page's editor JSON document into deterministic
// markdown matching the desktop app's reference renderer. An absent document
// (nil, empty or whitespace only) returns "", nil, the same as JSON null.
// Invalid JSON returns an error prefixed with "pagedoc: ".
//
// Two untestable boundary cases differ from the JavaScript reference because
// of runtime semantics: Go's JSON decoder replaces lone UTF-16 surrogate
// escapes with U+FFFD, and documents nested deeply enough to overflow the
// JavaScript call stack (where the reference catches the range error and
// returns "") render normally in Go up to encoding/json's 10000-level nesting
// limit, beyond which json.Valid reports an invalid-JSON error.
func Markdown(doc json.RawMessage) (out string, err error) {
	// The export boundary must not take the engine down on a document nobody
	// foresaw; the reference renderer likewise returns rather than throws.
	defer func() {
		if r := recover(); r != nil {
			out = ""
			err = fmt.Errorf("pagedoc: recovered panic: %v", r)
		}
	}()

	if len(bytes.TrimSpace(doc)) == 0 {
		return "", nil
	}
	if !json.Valid(doc) {
		return "", errors.New("pagedoc: invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return "", fmt.Errorf("pagedoc: invalid JSON: %w", err)
	}
	rendered := block(object(root))
	if rendered == "" {
		return "", nil
	}
	return strings.TrimRight(rendered, "\n") + "\n", nil
}

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func children(node map[string]any) []map[string]any {
	raw, ok := node["content"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, len(raw))
	for i, elem := range raw {
		out[i] = object(elem)
	}
	return out
}

func marks(node map[string]any) []map[string]any {
	raw, ok := node["marks"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, len(raw))
	for i, elem := range raw {
		out[i] = object(elem)
	}
	return out
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}

func intNumber(v any) (float64, bool) {
	f, ok := number(v)
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) || math.Trunc(f) != f {
		return 0, false
	}
	return f, true
}

func jsNumber(x float64) string {
	if x == 0 {
		return "0"
	}
	if math.Abs(x) < 1e21 {
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return strconv.FormatFloat(x, 'e', -1, 64)
}

func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '`', '*', '_', '[', ']', '<', '>':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func longestBackticks(s string) int {
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

func inlineLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
}

func trimInline(s string) string {
	return strings.Trim(s, " \t")
}

func descendantText(node map[string]any) string {
	var b strings.Builder
	b.WriteString(str(node["text"]))
	for _, child := range children(node) {
		b.WriteString(descendantText(child))
	}
	return b.String()
}

func inlineText(node map[string]any) string {
	t := inlineLine(str(node["text"]))
	if t == "" {
		return ""
	}
	hasCode := false
	for _, m := range marks(node) {
		if str(m["type"]) == "code" {
			hasCode = true
			break
		}
	}
	if !hasCode {
		return escape(t)
	}
	delim := strings.Repeat("`", longestBackticks(t)+1)
	pad := ""
	if strings.HasPrefix(t, "`") || strings.HasSuffix(t, "`") {
		pad = " "
	}
	return delim + pad + t + pad + delim
}

func linkHref(node map[string]any) string {
	if str(node["type"]) != "text" {
		return ""
	}
	for _, m := range marks(node) {
		if str(m["type"]) == "link" {
			return str(object(m["attrs"])["href"])
		}
	}
	return ""
}

func encodeHref(href string) string {
	return strings.NewReplacer(
		" ", "%20",
		"(", "%28",
		")", "%29",
		"<", "%3C",
		">", "%3E",
	).Replace(href)
}

func inline(nodes []map[string]any) string {
	var b strings.Builder
	for i := 0; i < len(nodes); {
		n := nodes[i]
		href := linkHref(n)
		text := inlineText(n)
		i++
		if href != "" {
			for i < len(nodes) && linkHref(nodes[i]) == href {
				text += inlineText(nodes[i])
				i++
			}
			if text == "" {
				continue
			}
			b.WriteByte('[')
			b.WriteString(text)
			b.WriteString("](")
			b.WriteString(encodeHref(href))
			b.WriteByte(')')
		} else {
			b.WriteString(text)
		}
	}
	return b.String()
}

func paragraph(s string) string {
	s = trimInline(s)
	var b strings.Builder
	lineStart := true
	for i := 0; i < len(s); {
		if lineStart {
			switch s[i] {
			case '#', '-', '+', '>':
				b.WriteByte('\\')
				b.WriteByte(s[i])
				i++
				lineStart = false
				continue
			}
			if s[i] >= '0' && s[i] <= '9' {
				j := i + 1
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				b.WriteString(s[i:j])
				if j < len(s) && (s[j] == '.' || s[j] == ')') {
					b.WriteByte('\\')
					b.WriteByte(s[j])
					j++
				}
				i = j
				lineStart = false
				continue
			}
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+w])
		i += w
		lineStart = r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
	}
	return b.String()
}

func liveBlock(attrs map[string]any) string {
	kind := str(attrs["kind"])
	if kind == "" {
		kind = "unknown"
	}
	switch kind {
	case "jobs", "waves":
		if m := str(attrs["milestone_id"]); m != "" {
			return "[live: " + kind + " milestone=" + m + "]"
		}
		return "[live: " + kind + "]"
	case "job":
		if job, ok := intNumber(attrs["job"]); ok && job >= 0 {
			return "[live: job " + str(attrs["factory"]) + "#" + jsNumber(job) + "]"
		}
		return "[live: job]"
	case "decision":
		if r := str(attrs["record_id"]); r != "" {
			return "[live: decision " + r + "]"
		}
		return "[live: decision]"
	default:
		return "[live: " + kind + "]"
	}
}

func listItem(node map[string]any, prefix string) string {
	var body strings.Builder
	for _, child := range children(node) {
		r := block(child)
		if r == "" {
			continue
		}
		if body.Len() > 0 {
			t := str(child["type"])
			if t == "bulletList" || t == "orderedList" {
				body.WriteByte('\n')
			} else {
				body.WriteString("\n\n")
			}
		}
		body.WriteString(r)
	}
	if body.Len() == 0 {
		return strings.TrimRight(prefix, " ")
	}
	lines := strings.Split(body.String(), "\n")
	indent := strings.Repeat(" ", len(prefix))
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
		} else if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func block(node map[string]any) string {
	attrs := object(node["attrs"])
	switch str(node["type"]) {
	case "doc":
		var parts []string
		for _, child := range children(node) {
			if r := block(child); r != "" {
				parts = append(parts, r)
			}
		}
		return strings.Join(parts, "\n\n")
	case "paragraph":
		return paragraph(inline(children(node)))
	case "heading":
		t := trimInline(inline(children(node)))
		if t == "" {
			return ""
		}
		level := 1
		if f, ok := number(attrs["level"]); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
			tr := math.Trunc(f)
			if tr < 1 {
				level = 1
			} else if tr > 3 {
				level = 3
			} else {
				level = int(tr)
			}
		}
		return strings.Repeat("#", level) + " " + t
	case "bulletList", "orderedList":
		start := 1.0
		if s, ok := intNumber(attrs["start"]); ok {
			start = s
		}
		kids := children(node)
		items := make([]string, len(kids))
		isBullet := str(node["type"]) == "bulletList"
		for i, item := range kids {
			prefix := "- "
			if !isBullet {
				prefix = jsNumber(start+float64(i)) + ". "
			}
			items[i] = listItem(item, prefix)
		}
		return strings.Join(items, "\n")
	case "listItem":
		return listItem(node, "")
	case "codeBlock":
		t := descendantText(node)
		fence := strings.Repeat("`", max(3, longestBackticks(t)+1))
		if t != "" {
			return fence + str(attrs["language"]) + "\n" + t + "\n" + fence
		}
		return fence + str(attrs["language"]) + "\n" + fence
	case "liveBlock":
		return liveBlock(attrs)
	default:
		return paragraph(escape(inlineLine(descendantText(node))))
	}
}
