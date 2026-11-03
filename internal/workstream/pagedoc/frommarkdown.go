package pagedoc

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var workstreamMarkdownHeading = regexp.MustCompile(`^(#+) +(.*)$`)

var workstreamMarkdownList = regexp.MustCompile(`^( *)([-*+] |([0-9]+)[.)] )(.*)$`)

var workstreamMarkdownLive = regexp.MustCompile(`^\[live: (jobs|waves)( milestone=([^\s\]]+))?\]$`)

var workstreamMarkdownJob = regexp.MustCompile(`^\[live: job ([^\s#]*)#([0-9]+)\]$`)

var workstreamMarkdownDecision = regexp.MustCompile(`^\[live: decision ([^\s\]]+)\]$`)

// FromMarkdown accepts prose without requiring the agent to construct editor JSON.
func FromMarkdown(md string) json.RawMessage {
	md = strings.ToValidUTF8(md, "�")
	md = strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\r", "\n")
	doc := map[string]any{"type": "doc", "content": workstreamMarkdownBlocks(strings.Split(md, "\n"), 0)}
	out, _ := json.Marshal(doc)
	return out
}

func workstreamMarkdownNode(kind string, content []map[string]any) map[string]any {
	return map[string]any{"type": kind, "content": content}
}

func workstreamMarkdownBlocks(lines []string, depth int) []map[string]any {
	out := make([]map[string]any, 0)
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "---" {
			i++
			continue
		}
		if fence, language := workstreamMarkdownFence(line); fence != "" {
			node, next := workstreamMarkdownCode(lines, i, fence, language)
			out = append(out, node)
			i = next
			continue
		}
		if match := workstreamMarkdownHeading.FindStringSubmatch(line); match != nil {
			node := workstreamMarkdownNode("heading", workstreamMarkdownInline(match[2]))
			node["attrs"] = map[string]any{"level": min(3, len(match[1]))}
			out = append(out, node)
			i++
			continue
		}
		if node := workstreamMarkdownLiveNode(line); node != nil {
			out = append(out, node)
			i++
			continue
		}
		if depth < 16 && workstreamMarkdownList.MatchString(lines[i]) {
			node, next := workstreamMarkdownListNode(lines, i, depth)
			out = append(out, node)
			i = next
			continue
		}
		parts := []string{workstreamMarkdownProse(line)}
		i++
		for i < len(lines) && !workstreamMarkdownBoundary(lines[i], depth) {
			parts = append(parts, workstreamMarkdownProse(lines[i]))
			i++
		}
		text := strings.Join(parts, " ")
		out = append(out, workstreamMarkdownNode("paragraph", workstreamMarkdownInline(text)))
	}
	return out
}

func workstreamMarkdownProse(line string) string {
	return strings.TrimPrefix(strings.TrimSpace(line), "> ")
}

func workstreamMarkdownBoundary(line string, depth int) bool {
	line = strings.TrimSpace(line)
	fence, _ := workstreamMarkdownFence(line)
	return line == "" || line == "---" || fence != "" || workstreamMarkdownHeading.MatchString(line) || workstreamMarkdownLiveNode(line) != nil || depth < 16 && workstreamMarkdownList.MatchString(line)
}

func workstreamMarkdownFence(line string) (string, string) {
	if !strings.HasPrefix(line, "```") && !strings.HasPrefix(line, "~~~") {
		return "", ""
	}
	i := 0
	for i < len(line) && line[i] == line[0] {
		i++
	}
	return line[:i], strings.TrimSpace(line[i:])
}

func workstreamMarkdownCode(lines []string, at int, fence, language string) (map[string]any, int) {
	i := at + 1
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, fence) && strings.Trim(line, string(fence[0])) == "" {
			break
		}
		i++
	}
	text := strings.Join(lines[at+1:i], "\n")
	node := workstreamMarkdownNode("codeBlock", []map[string]any{{"type": "text", "text": text}})
	var lang any
	if language != "" {
		lang = language
	}
	node["attrs"] = map[string]any{"language": lang}
	return node, min(i+1, len(lines))
}

func workstreamMarkdownListNode(lines []string, at, depth int) (map[string]any, int) {
	first := workstreamMarkdownList.FindStringSubmatch(lines[at])
	indent, ordered := len(first[1]), first[3] != ""
	items := make([]map[string]any, 0)
	i := at
	for i < len(lines) {
		match := workstreamMarkdownList.FindStringSubmatch(lines[i])
		if match == nil || len(match[1]) != indent || (match[3] != "") != ordered {
			break
		}
		width := len(match[1]) + len(match[2])
		body := []string{match[4]}
		i++
		for i < len(lines) {
			line := lines[i]
			if strings.TrimSpace(line) == "" {
				if i+1 >= len(lines) || len(lines[i+1])-len(strings.TrimLeft(lines[i+1], " ")) <= indent {
					break
				}
			} else if len(line)-len(strings.TrimLeft(line, " ")) <= indent {
				break
			}
			cut := min(width, len(line)-len(strings.TrimLeft(line, " ")))
			body = append(body, line[cut:])
			i++
		}
		items = append(items, workstreamMarkdownNode("listItem", workstreamMarkdownBlocks(body, depth+1)))
	}
	kind := "bulletList"
	if ordered {
		kind = "orderedList"
	}
	node := workstreamMarkdownNode(kind, items)
	if ordered {
		start, err := strconv.Atoi(first[3])
		if err != nil {
			start = 1
		}
		node["attrs"] = map[string]any{"start": start}
	}
	return node, i
}

func workstreamMarkdownLiveNode(line string) map[string]any {
	attrs := map[string]any{"kind": nil, "milestone_id": nil, "factory": nil, "job": nil, "record_id": nil}
	switch {
	case workstreamMarkdownLive.MatchString(line):
		match := workstreamMarkdownLive.FindStringSubmatch(line)
		attrs["kind"] = match[1]
		if match[3] != "" {
			attrs["milestone_id"] = match[3]
		}
	case workstreamMarkdownJob.MatchString(line):
		match := workstreamMarkdownJob.FindStringSubmatch(line)
		n, err := strconv.Atoi(match[2])
		if err != nil {
			return nil
		}
		attrs["kind"], attrs["factory"], attrs["job"] = "job", match[1], n
	case workstreamMarkdownDecision.MatchString(line):
		match := workstreamMarkdownDecision.FindStringSubmatch(line)
		attrs["kind"], attrs["record_id"] = "decision", match[1]
	default:
		return nil
	}
	return map[string]any{"type": "liveBlock", "attrs": attrs}
}

func workstreamMarkdownInline(text string) []map[string]any {
	out := make([]map[string]any, 0)
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out = append(out, map[string]any{"type": "text", "text": plain.String()})
			plain.Reset()
		}
	}
	for i := 0; i < len(text); {
		if text[i] == '\\' && i+1 < len(text) {
			plain.WriteByte(text[i+1])
			i += 2
			continue
		}
		if strings.HasPrefix(text[i:], "**") || strings.HasPrefix(text[i:], "__") {
			i += 2
			continue
		}
		nodes, used := workstreamMarkdownInlineSpecial(text[i:])
		if used > 0 {
			flush()
			out = append(out, nodes...)
			i += used
			continue
		}
		plain.WriteByte(text[i])
		i++
	}
	flush()
	return out
}

func workstreamMarkdownInlineSpecial(text string) ([]map[string]any, int) {
	if text[0] == '`' {
		n := 1
		for n < len(text) && text[n] == '`' {
			n++
		}
		if end := strings.Index(text[n:], text[:n]); end >= 0 {
			body := text[n : n+end]
			if strings.HasPrefix(body, " ") && strings.HasSuffix(body, " ") && strings.TrimSpace(body) != "" {
				body = body[1 : len(body)-1]
			}
			return []map[string]any{{"type": "text", "text": body, "marks": []map[string]any{{"type": "code"}}}}, n + end + n
		}
	}
	if text[0] == '[' {
		return workstreamMarkdownLink(text)
	}
	return nil, 0
}

func workstreamMarkdownLink(text string) ([]map[string]any, int) {
	close := strings.Index(text, "](")
	if close < 0 {
		return nil, 0
	}
	end := workstreamMarkdownLinkEnd(text, close+2)
	if end < 0 {
		return nil, 0
	}
	href := workstreamMarkdownUnescape(text[close+2 : end])
	nodes := workstreamMarkdownInline(text[1:close])
	for _, node := range nodes {
		marks, _ := node["marks"].([]map[string]any)
		node["marks"] = append(marks, map[string]any{"type": "link", "attrs": map[string]any{"href": href}})
	}
	return nodes, end + 1
}

func workstreamMarkdownLinkEnd(text string, start int) int {
	depth := 0
	for i := start; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
			continue
		}
		switch text[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

func workstreamMarkdownUnescape(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
		}
		out.WriteByte(text[i])
	}
	return out.String()
}
