package manual

// WHAT IS IN HERE, WRITTEN ONCE FOR EVERY SURFACE THAT ASKS.
//
// A person can be shown the manual from the command line and from the
// conversation, and both of them answer the same first question — which pages
// are there — so both of them read this listing rather than each formatting a
// list of names beside its own copy of where a title comes from. Two copies of
// that is how one surface ends up naming a page the other has renamed.

import (
	"fmt"
	"strings"
)

// PageTitle is the title a page gives itself: its own `# ` line. The search
// stops at the first `## ` because everything past that is a section rather
// than a title, and the pages run to hundreds of kilobytes. A page with no
// title line is named the way this package names one anywhere else — its file
// name, with the dashes read as spaces.
func (c *Corpus) PageTitle(name string) string {
	text, _ := c.Page(name)
	for text != "" {
		line, rest, _ := strings.Cut(text, "\n")
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		text = rest
	}
	return strings.ReplaceAll(strings.TrimSuffix(strings.TrimSpace(name), ".md"), "-", " ")
}

// Listing is every page in this corpus, one per line, in reading order: the
// name a person types to open it, then the title the page gives itself. The
// name comes first and the column is aligned because the names are what the
// line is for — the title is there to choose by.
func (c *Corpus) Listing() string {
	names := c.Pages()
	width := 0
	for _, name := range names {
		if len(name) > width {
			width = len(name)
		}
	}
	rows := make([]string, 0, len(names))
	for _, name := range names {
		rows = append(rows, fmt.Sprintf("%-*s  %s", width, name, c.PageTitle(name)))
	}
	return strings.Join(rows, "\n")
}

// RenderWhole is [Render] for a person rather than for a model: the same label
// over each section — the page and the heading it came from, so a quoted line
// can be traced back to the page that authorized it — and NOTHING CUT under it.
//
// Render's [SectionBodyCap] is a budget, and it is the model's: a context window
// is paid for by the token, so a long section degrades there by truncation. A
// person reading their own manual is paying for none of that, and a page cut
// short on the surface where the whole of it is free would be a limit wearing a
// reason it does not have.
func RenderWhole(sections []Section) string { return renderSections(sections, 0) }

// renderSections is the arrangement itself, and there is one of it because the
// two readers differ only in whether a body is cut. Two builders would be two
// places for the label to change in, and a person who quoted a section to
// somebody reading the model's copy would be quoting a different shape. A cap at
// or below zero cuts nothing.
func renderSections(sections []Section, cap int) string {
	blocks := make([]string, 0, len(sections))
	for _, section := range sections {
		body := section.Body
		if cap > 0 && len(body) > cap {
			body = body[:cap] + "…"
		}
		blocks = append(blocks, fmt.Sprintf("[%s · %s]\n%s", section.Page, section.Title, body))
	}
	return strings.Join(blocks, "\n\n")
}
