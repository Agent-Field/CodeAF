// Command manualtruth prints what scriptbench's manual questions are checked
// against, read through codeaf's own manual package from the chat corpus the
// binary ships, so the checker and the `manual` tool read one corpus.
//
// For every chat page it prints the page's title (its one "# " heading) and its
// section headings in order, as one JSON object keyed by page name. A page with
// text before its first "## " heading has that text parsed as a section titled
// with the page's own title; it is left out of the headings, because the model
// reading the page sees no such heading.
package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/manual"
)

type page struct {
	Title    string   `json:"title"`
	Headings []string `json:"headings"`
}

func main() {
	corpus := manual.Chat()
	out := map[string]page{}
	for _, name := range corpus.Pages() {
		text, _ := corpus.Page(name)
		var p page
		for line := range strings.SplitSeq(text, "\n") {
			if title, ok := strings.CutPrefix(line, "# "); ok {
				p.Title = strings.TrimSpace(title)
				break
			}
		}
		for index, section := range corpus.PageSections(name) {
			if index == 0 && section.Title == p.Title {
				continue
			}
			p.Headings = append(p.Headings, section.Title)
		}
		out[name] = p
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", " ")
	if err := encoder.Encode(out); err != nil {
		os.Exit(1)
	}
}
