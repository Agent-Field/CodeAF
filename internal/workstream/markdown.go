package workstream

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Markdown is an export of the editor JSON. Marks retain their plain text;
// unfamiliar nodes retain their own and their children's text as well.
func Markdown(doc json.RawMessage) (string, error) {
	if err := checkDoc(doc); err != nil {
		return "", err
	}
	var root map[string]any
	dec := json.NewDecoder(strings.NewReader(string(doc)))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return "", err
	}
	var render func(map[string]any) string
	render = func(n map[string]any) string {
		kind, _ := n["type"].(string)
		text, _ := n["text"].(string)
		content, _ := n["content"].([]any)
		attrs, _ := n["attrs"].(map[string]any)
		switch kind {
		case "bulletList", "bullet_list", "orderedList", "ordered_list":
			var list strings.Builder
			for i, item := range content {
				child, ok := item.(map[string]any)
				if !ok {
					continue
				}
				prefix := "- "
				if kind == "orderedList" || kind == "ordered_list" {
					prefix = fmt.Sprintf("%d. ", i+1)
				}
				lines := strings.Split(strings.TrimSpace(render(child)), "\n")
				for j, line := range lines {
					if j == 0 {
						list.WriteString(prefix)
					} else {
						list.WriteString(strings.Repeat(" ", len(prefix)))
					}
					list.WriteString(line)
					list.WriteByte('\n')
				}
			}
			return list.String() + "\n"
		}
		var b strings.Builder
		b.WriteString(text)
		for _, item := range content {
			if child, ok := item.(map[string]any); ok {
				b.WriteString(render(child))
			}
		}
		text = b.String()
		switch kind {
		case "heading":
			number, _ := attrs["level"].(json.Number)
			level, _ := number.Int64()
			return strings.Repeat("#", int(max(1, min(level, 6)))) + " " + strings.TrimSpace(text) + "\n\n"
		case "paragraph":
			return text + "\n\n"
		case "hardBreak", "hard_break":
			return "\n"
		case "codeBlock", "code_block":
			language, _ := attrs["language"].(string)
			fence := "```"
			for strings.Contains(text, fence) {
				fence += "`"
			}
			return fence + language + "\n" + strings.TrimRight(text, "\n") + "\n" + fence + "\n\n"
		case "blockquote":
			lines := strings.Split(strings.TrimSpace(text), "\n")
			for i := range lines {
				lines[i] = "> " + lines[i]
			}
			return strings.Join(lines, "\n") + "\n\n"
		default:
			return text
		}
	}
	return strings.TrimSpace(render(root)), nil
}
