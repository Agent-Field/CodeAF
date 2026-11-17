package jira

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// ── ATLASSIAN DOCUMENT FORMAT ───────────────────────────────────────────────
//
// Jira Cloud's REST v3 speaks rich text as ADF: a tree of typed nodes. The
// factory reads a description as plain text (a job's body is text, and a
// model reads it), and writes a comment as the smallest ADF document that
// says one or more lines, with every link a real link. Nothing else of ADF is
// needed, so nothing else is modelled.

// adfNode is one node of an ADF tree, with only the fields text is read from.
type adfNode struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Content []adfNode       `json:"content,omitempty"`
	Attrs   json.RawMessage `json:"attrs,omitempty"`
	Marks   []adfMark       `json:"marks,omitempty"`
}

type adfMark struct {
	Type  string            `json:"type"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// PlainText is an ADF document as plain text: paragraphs and headings
// separated by a blank line, list items as "- " (or "1. ") lines, a code
// block's text as it is, a mention or an emoji by its text, a card by its
// link. A description that is a JSON string (the v2 shape) is that string,
// and one that is null, absent or unreadable is "".
func PlainText(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var doc adfNode
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(blockText(doc))
}

// blockText is a node read as a block: its own lines, with no trailing break.
func blockText(n adfNode) string {
	switch n.Type {
	case "doc", "blockquote", "panel", "expand", "nestedExpand", "layoutSection", "layoutColumn", "listItem", "tableCell", "tableHeader":
		parts := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			if t := blockText(c); t != "" {
				parts = append(parts, t)
			}
		}
		sep := "\n\n"
		if n.Type == "listItem" || n.Type == "tableCell" || n.Type == "tableHeader" {
			sep = "\n"
		}
		out := strings.Join(parts, sep)
		if n.Type == "blockquote" {
			out = "> " + strings.ReplaceAll(out, "\n", "\n> ")
		}
		return out
	case "bulletList", "orderedList":
		order := 1
		if n.Type == "orderedList" {
			var attrs struct {
				Order *int `json:"order"`
			}
			if json.Unmarshal(n.Attrs, &attrs) == nil && attrs.Order != nil {
				order = *attrs.Order
			}
		}
		lines := make([]string, 0, len(n.Content))
		for i, item := range n.Content {
			marker := "- "
			if n.Type == "orderedList" {
				marker = strconv.Itoa(order+i) + ". "
			}
			text := blockText(item)
			lines = append(lines, marker+strings.ReplaceAll(text, "\n", "\n  "))
		}
		return strings.Join(lines, "\n")
	case "table":
		rows := make([]string, 0, len(n.Content))
		for _, row := range n.Content {
			cells := make([]string, 0, len(row.Content))
			for _, cell := range row.Content {
				cells = append(cells, strings.ReplaceAll(blockText(cell), "\n", " "))
			}
			rows = append(rows, strings.Join(cells, " | "))
		}
		return strings.Join(rows, "\n")
	case "rule":
		return "---"
	case "mediaSingle", "mediaGroup", "media":
		return ""
	}
	// Paragraphs, headings, code blocks and anything unknown read inline.
	var b strings.Builder
	for _, c := range n.Content {
		inlineText(&b, c)
	}
	if n.Text != "" {
		b.WriteString(n.Text)
	}
	return strings.TrimSpace(b.String())
}

// inlineText writes one inline node.
func inlineText(b *strings.Builder, n adfNode) {
	var attrs struct {
		Text      string `json:"text"`
		ShortName string `json:"shortName"`
		URL       string `json:"url"`
	}
	_ = json.Unmarshal(n.Attrs, &attrs)
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteString("\n")
	case "mention", "status", "date":
		b.WriteString(attrs.Text)
	case "emoji":
		if attrs.Text != "" {
			b.WriteString(attrs.Text)
		} else {
			b.WriteString(attrs.ShortName)
		}
	case "inlineCard", "blockCard", "embedCard":
		b.WriteString(attrs.URL)
	default:
		// A block nested where an inline was expected still says its words.
		b.WriteString(blockText(n))
	}
}

// reLink finds the links a comment's text carries, so each becomes a real
// link in Jira rather than text a person has to copy.
var reLink = regexp.MustCompile(`https?://[^\s<>"]+`)

// Doc is text as an ADF document: one paragraph per blank-line-separated
// block, a hard break for every other newline, and every http(s) URL marked
// as a link to itself.
func Doc(text string) any {
	type doc struct {
		Type    string    `json:"type"`
		Version int       `json:"version"`
		Content []adfNode `json:"content"`
	}
	out := doc{Type: "doc", Version: 1, Content: []adfNode{}}
	for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		para := adfNode{Type: "paragraph"}
		for i, line := range strings.Split(block, "\n") {
			if i > 0 {
				para.Content = append(para.Content, adfNode{Type: "hardBreak"})
			}
			para.Content = append(para.Content, linked(line)...)
		}
		out.Content = append(out.Content, para)
	}
	return out
}

// linked is one line as text nodes, its links carrying a link mark.
func linked(line string) []adfNode {
	var out []adfNode
	at := 0
	for _, loc := range reLink.FindAllStringIndex(line, -1) {
		if loc[0] > at {
			out = append(out, adfNode{Type: "text", Text: line[at:loc[0]]})
		}
		url := line[loc[0]:loc[1]]
		out = append(out, adfNode{Type: "text", Text: url, Marks: []adfMark{{Type: "link", Attrs: map[string]string{"href": url}}}})
		at = loc[1]
	}
	if at < len(line) {
		out = append(out, adfNode{Type: "text", Text: line[at:]})
	}
	return out
}
