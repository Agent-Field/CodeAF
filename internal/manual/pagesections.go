package manual

import "strings"

// A page is not one indivisible thing. corpus.go already cuts every page at its
// `## ` headings to rank it, and those pieces are the same pieces a reader
// wants when the whole page is more than they can take: a topic, named by the
// heading a person would say out loud. The two reads below expose that
// structure, so a caller that has to bound a page has somewhere to send whoever
// wants the rest of it.

// PageSections returns one page's sections in reading order, and nothing when
// there is no such page. The scan is linear over the corpus because a manual is
// a few thousand sections held in memory and read once per lookup; an index
// would be a second structure to keep true for no gain anybody could measure.
func (c *Corpus) PageSections(name string) []Section {
	c.load()
	name = pageName(name)
	found := make([]Section, 0, 16)
	for _, section := range c.sections {
		if section.Page == name {
			found = append(found, section)
		}
	}
	return found
}

// Section returns one section of one page by its heading. The heading is
// matched the way a person types one back — the `## ` and the capitals are
// forgiven, nothing else is — because a near miss that returned a neighbouring
// section would read as though the heading asked for existed.
func (c *Corpus) Section(page, heading string) (Section, bool) {
	want := headingKey(heading)
	if want == "" {
		return Section{}, false
	}
	for _, section := range c.PageSections(page) {
		if headingKey(section.Title) == want {
			return section, true
		}
	}
	return Section{}, false
}

// pageName is how a page is asked for versus how it is filed: people and models
// both write "permissions.md" as often as "permissions".
func pageName(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), ".md")
}

// headingKey is the forgiving form of a heading, for comparison only.
func headingKey(heading string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(heading), "#")))
}
