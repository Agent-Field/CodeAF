package main

import (
	"fmt"
	"sort"
	"strings"
)

// The line CHANGELOG.md carries so that rolling a version up is an insertion at
// a known place rather than a guess about which heading came first.
const insertMarker = "<!-- aforge-changes inserts new versions directly below this line -->"

const repoURL = "https://github.com/Agent-Field/aforge-v2"

// Render turns a set of entries into one version's section.
//
// TWO LAYERS, BECAUSE THERE ARE TWO READERS AND THEY WANT OPPOSITE THINGS. A
// person scanning for what shipped wants one line per change and nothing else on
// the screen. A model that has to decide whether what it remembers is still true
// wants every claim, spelled out. So the line is always visible and the claims
// are always one fold away — neither reader pays for the other.
//
// An entry with nothing to invalidate renders as a bare line. An empty
// disclosure triangle is a promise of detail that is not there, and after two of
// them nobody opens the third.
func Render(version, date string, entries []Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n", version, date)

	byKind := map[string][]Entry{}
	for _, e := range entries {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}

	for _, k := range kinds {
		group := byKind[k.key]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].PR < group[j].PR })

		fmt.Fprintf(&b, "\n### %s\n\n", k.heading)
		for _, e := range group {
			b.WriteString(renderEntry(e))
		}
	}
	return b.String()
}

func renderEntry(e Entry) string {
	var b strings.Builder

	fmt.Fprintf(&b, "- **%s** — [#%d](%s/pull/%d)", e.Title, e.PR, repoURL, e.PR)
	if len(e.Surface) > 0 {
		tags := make([]string, 0, len(e.Surface))
		for _, s := range e.Surface {
			tags = append(tags, "`"+s+"`")
		}
		fmt.Fprintf(&b, " · %s", strings.Join(tags, " "))
	}
	b.WriteString("\n")

	if len(e.Invalidates) == 0 && e.Body == "" {
		return b.String()
	}

	// The summary is written in the words somebody would use to ask the
	// question. "What this invalidates" is the field's name and nobody's
	// question; "what is no longer true" is the question.
	noun := "thing that is no longer true"
	if len(e.Invalidates) != 1 {
		noun = "things that are no longer true"
	}
	summary := "why"
	if len(e.Invalidates) > 0 {
		summary = fmt.Sprintf("%d %s", len(e.Invalidates), noun)
	}

	// A blank line before and after the block, and two spaces of indent, is what
	// GitHub needs to render Markdown inside <details> that sits inside a list
	// item. Without them the bullets come out as literal hyphens.
	fmt.Fprintf(&b, "\n  <details><summary>%s</summary>\n\n", summary)
	for _, s := range e.Invalidates {
		fmt.Fprintf(&b, "  - %s\n", strings.TrimSpace(s))
	}
	if e.Body != "" {
		if len(e.Invalidates) > 0 {
			b.WriteString("\n")
		}
		for _, line := range strings.Split(e.Body, "\n") {
			if line == "" {
				b.WriteString("\n")
				continue
			}
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	b.WriteString("\n  </details>\n\n")

	return b.String()
}

// Insert puts a rendered section into CHANGELOG.md at the marker, newest first.
func Insert(changelog, section string) (string, error) {
	i := strings.Index(changelog, insertMarker)
	if i < 0 {
		return "", fmt.Errorf("CHANGELOG.md has no %q line, so there is nowhere unambiguous to insert", insertMarker)
	}
	at := i + len(insertMarker)
	return changelog[:at] + "\n\n" + strings.TrimRight(section, "\n") + "\n" + changelog[at:], nil
}
