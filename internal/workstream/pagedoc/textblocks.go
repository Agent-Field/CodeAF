package pagedoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextBlock retains the editor node so a keep changes only its text content.
type TextBlock struct {
	Path []int
	Kind string
	Text string
	Node map[string]any
}

// Decode preserves exact JSON numbers, including unknown editor attributes.
func Decode(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var root map[string]any
	err := decoder.Decode(&root)
	if err == nil && root == nil {
		err = errors.New("pagedoc: expected a document object")
	}
	return root, err
}

func TextBlocks(doc map[string]any) []TextBlock {
	blocks := make([]TextBlock, 0)
	var visit func(map[string]any, []int)
	visit = func(node map[string]any, path []int) {
		kind, _ := node["type"].(string)
		if kind == "paragraph" || kind == "heading" || kind == "codeBlock" {
			blocks = append(blocks, TextBlock{Path: slices.Clone(path), Kind: kind, Text: nodeText(node), Node: node})
		}
		content, _ := node["content"].([]any)
		for i, child := range content {
			visit(object(child), append(slices.Clone(path), i))
		}
	}
	visit(doc, []int{})
	return blocks
}

func nodeText(node map[string]any) string {
	if node["type"] == "text" {
		text, _ := node["text"].(string)
		return text
	}
	var text strings.Builder
	for _, child := range children(node) {
		text.WriteString(nodeText(child))
	}
	return text.String()
}

// Locate tries the stored path before the unique whole-block text fallback.
func Locate(doc map[string]any, path []int, text string) (TextBlock, bool) {
	blocks := TextBlocks(doc)
	for _, block := range blocks {
		if slices.Equal(block.Path, path) && block.Text == text {
			return block, true
		}
	}
	var match TextBlock
	count := 0
	for _, block := range blocks {
		if block.Text == text {
			match = block
			count++
		}
	}
	return match, count == 1
}

// TrimEdit trims only at whitespace word boundaries in both strings.
func TrimEdit(find, replace string) (offset int, del, ins string) {
	a, b := []rune(find), []rune(replace)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for !wordBoundary(a, p) || !wordBoundary(b, p) {
		p--
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	for !wordBoundary(a, len(a)-s) || !wordBoundary(b, len(b)-s) {
		s--
	}
	return p, string(a[p : len(a)-s]), string(b[p : len(b)-s])
}

func wordBoundary(s []rune, p int) bool {
	return p == 0 || p == len(s) || unicode.IsSpace(s[p-1]) || unicode.IsSpace(s[p])
}

// Apply rewrites a located block. Offsets and deletion lengths are code points.
func Apply(block TextBlock, offset int, del, ins string) error {
	text, removed := []rune(block.Text), []rune(del)
	if offset < 0 || offset+len(removed) > len(text) || string(text[offset:offset+len(removed)]) != del {
		return errors.New("pagedoc: edit text does not match")
	}
	if block.Kind != "codeBlock" && strings.ContainsAny(ins, "\r\n") {
		return errors.New("pagedoc: line break outside code block")
	}
	var inherited []any
	if len(removed) > 0 {
		at := offset
		inherited, _ = characterMarks(block.Node, &at)
	} else if offset > 0 && offset < len(text) {
		leftAt, rightAt := offset-1, offset
		left, _ := characterMarks(block.Node, &leftAt)
		right, _ := characterMarks(block.Node, &rightAt)
		inherited = commonMarks(left, right)
	}
	edit := textRewrite{start: offset, end: offset + len(removed), insert: ins, marks: inherited}
	edit.content(block.Node)
	if !edit.inserted && ins != "" {
		content, _ := block.Node["content"].([]any)
		block.Node["content"] = mergeText(append(content, edit.insertion()))
	}
	return nil
}

// characterMarks stops at the requested character without allocating per rune.
func characterMarks(node map[string]any, at *int) ([]any, bool) {
	if node["type"] == "text" {
		text, _ := node["text"].(string)
		length := utf8.RuneCountInString(text)
		if *at < length {
			marks, _ := node["marks"].([]any)
			return marks, true
		}
		*at -= length
		return nil, false
	}
	for _, child := range children(node) {
		if marks, found := characterMarks(child, at); found {
			return marks, true
		}
	}
	return nil, false
}

func commonMarks(a, b []any) []any {
	var out []any
	for _, mark := range a {
		if slices.ContainsFunc(b, func(other any) bool { return reflect.DeepEqual(mark, other) }) {
			out = append(out, mark)
		}
	}
	return out
}

type textRewrite struct {
	start, end, position int
	insert               string
	marks                []any
	inserted             bool
}

func (e *textRewrite) insertion() map[string]any {
	node := map[string]any{"type": "text", "text": e.insert}
	if len(e.marks) > 0 {
		node["marks"] = e.marks
	}
	return node
}

func (e *textRewrite) content(node map[string]any) {
	content, exists := node["content"].([]any)
	if !exists {
		return
	}
	out := make([]any, 0, len(content))
	for _, raw := range content {
		child := object(raw)
		if child["type"] == "text" {
			out = append(out, e.leaf(child)...)
		} else {
			e.content(child)
			out = append(out, raw)
		}
	}
	node["content"] = mergeText(out)
}

func (e *textRewrite) leaf(node map[string]any) []any {
	text, _ := node["text"].(string)
	runes := []rune(text)
	start, end := e.position, e.position+len(runes)
	e.position = end
	if end < e.start || start > e.end {
		return []any{node}
	}
	out := []any{}
	appendText := func(text string) {
		if text != "" {
			part := maps.Clone(node)
			part["text"] = text
			out = append(out, part)
		}
	}
	left := min(len(runes), max(0, e.start-start))
	appendText(string(runes[:left]))
	if !e.inserted && e.start >= start && e.start <= end {
		e.inserted = true
		if e.insert != "" {
			out = append(out, e.insertion())
		}
	}
	right := min(len(runes), max(0, e.end-start))
	appendText(string(runes[right:]))
	return out
}

func mergeText(content []any) []any {
	out := make([]any, 0, len(content))
	for _, raw := range content {
		node := object(raw)
		if node["type"] != "text" {
			out = append(out, raw)
			continue
		}
		text, _ := node["text"].(string)
		if text == "" {
			continue
		}
		if len(out) > 0 {
			previous := object(out[len(out)-1])
			if previous["type"] == "text" && sameTextFields(previous, node) {
				previous["text"] = previous["text"].(string) + text
				continue
			}
		}
		out = append(out, raw)
	}
	return out
}

func sameTextFields(a, b map[string]any) bool {
	aMarks, _ := a["marks"].([]any)
	bMarks, _ := b["marks"].([]any)
	if !slices.EqualFunc(aMarks, bMarks, reflect.DeepEqual) {
		return false
	}
	for key, value := range a {
		if key == "text" || key == "marks" {
			continue
		}
		other, exists := b[key]
		if !exists || !reflect.DeepEqual(value, other) {
			return false
		}
	}
	for key := range b {
		if key == "text" || key == "marks" {
			continue
		}
		if _, exists := a[key]; !exists {
			return false
		}
	}
	return true
}
