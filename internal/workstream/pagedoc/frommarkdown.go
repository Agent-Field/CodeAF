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
		body, next := workstreamMarkdownListBody(lines, i, indent, match)
		items = append(items, workstreamMarkdownListItem(body, depth+1))
		i = next
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

func workstreamMarkdownListBody(lines []string, at, indent int, match []string) ([]string, int) {
	width := len(match[1]) + len(match[2])
	body := []string{match[4]}
	i := at + 1
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
	return body, i
}

func workstreamMarkdownListItem(lines []string, depth int) map[string]any {
	content := workstreamMarkdownBlocks(lines, depth)
	if len(content) == 0 || content[0]["type"] != "paragraph" {
		content = append([]map[string]any{workstreamMarkdownNode("paragraph", []map[string]any{})}, content...)
	}
	return workstreamMarkdownNode("listItem", content)
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
	index := workstreamMarkdownInlineIndex{
		code:      workstreamMarkdownCodeRuns(text),
		parens:    workstreamMarkdownParens(text),
		linkClose: strings.Index(text, "]("),
	}
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
		nodes, used := index.special(text, i)
		if used > 0 {
			if nodes == nil {
				plain.WriteString(text[i : i+used])
			} else {
				flush()
				out = append(out, nodes...)
			}
			i += used
			continue
		}
		plain.WriteByte(text[i])
		i++
	}
	flush()
	return out
}

type workstreamMarkdownRun struct {
	end   int
	close int
}

type workstreamMarkdownInlineIndex struct {
	code      map[int]workstreamMarkdownRun
	parens    map[int]int
	linkClose int
}

// Indexing runs once lets an unmatched opener consume its entire run as text.
func workstreamMarkdownCodeRuns(text string) map[int]workstreamMarkdownRun {
	out := make(map[int]workstreamMarkdownRun)
	next := make(map[int]int)
	for i := len(text) - 1; i >= 0; {
		if text[i] != '`' {
			i--
			continue
		}
		end := i + 1
		for i >= 0 && text[i] == '`' {
			i--
		}
		start := i + 1
		out[start] = workstreamMarkdownRun{end: end, close: workstreamMarkdownNextRun(next, end-start)}
		if end-start > 1 {
			// An escaped first backtick leaves the suffix available as an opener.
			out[start+1] = workstreamMarkdownRun{end: end, close: workstreamMarkdownNextRun(next, end-start-1)}
		}
		next[end-start] = start
	}
	return out
}

func workstreamMarkdownNextRun(next map[int]int, width int) int {
	if at, ok := next[width]; ok {
		return at
	}
	return -1
}

// Balanced destinations are indexed once, including failures, for every link.
func workstreamMarkdownParens(text string) map[int]int {
	out := make(map[int]int)
	var stack []int
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
			continue
		}
		switch text[i] {
		case '(':
			stack = append(stack, i)
		case ')':
			if len(stack) > 0 {
				last := len(stack) - 1
				out[stack[last]] = i
				stack = stack[:last]
			}
		}
	}
	return out
}

func (index *workstreamMarkdownInlineIndex) special(text string, at int) ([]map[string]any, int) {
	if text[at] == '`' {
		run := index.code[at]
		if run.close < 0 {
			return nil, run.end - at
		}
		body := text[run.end:run.close]
		if strings.HasPrefix(body, " ") && strings.HasSuffix(body, " ") && strings.TrimSpace(body) != "" {
			body = body[1 : len(body)-1]
		}
		width := run.end - at
		return []map[string]any{{"type": "text", "text": body, "marks": []map[string]any{{"type": "code"}}}}, run.close + width - at
	}
	if text[at] == '[' {
		return index.link(text, at)
	}
	return nil, 0
}

func (index *workstreamMarkdownInlineIndex) link(text string, at int) ([]map[string]any, int) {
	if index.linkClose >= 0 && index.linkClose < at {
		index.linkClose = strings.Index(text[at:], "](")
		if index.linkClose >= 0 {
			index.linkClose += at
		}
	}
	close := index.linkClose
	if close < 0 {
		return nil, 0
	}
	end, ok := index.parens[close+1]
	if !ok {
		return nil, 0
	}
	href := workstreamMarkdownUnescape(text[close+2 : end])
	// The first ]( bounds this label, so parsing it cannot recurse into links.
	nodes := workstreamMarkdownInline(text[at+1 : close])
	for _, node := range nodes {
		marks, _ := node["marks"].([]map[string]any)
		node["marks"] = append(marks, map[string]any{"type": "link", "attrs": map[string]any{"href": href}})
	}
	return nodes, end - at + 1
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
