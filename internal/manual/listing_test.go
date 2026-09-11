package manual

import (
	"strings"
	"testing"
)

// A person's label is useful only while no page can impersonate it, so both
// corpora must preserve the boundary that split establishes.
func TestNoSectionBodyCanContainAPersonsLabel(t *testing.T) {
	sections := append(Sections(), Chat().Sections()...)
	for _, section := range sections {
		for _, line := range strings.Split(section.Body, "\n") {
			if strings.HasPrefix(line, "## ") {
				t.Fatalf("%s · %s has a body line that can impersonate a person's label: %q",
					section.Page, section.Title, line)
			}
		}
	}
}

// The compacting answer proves the person's label stays countable even when
// the quoted screen itself begins a line with the model's bracket shape.
func TestRenderWholeLabelsEverySectionWithAUniqueLine(t *testing.T) {
	sections := Chat().Search("where did the folded messages go", DefaultResults)
	if len(sections) == 0 {
		t.Fatal("the compacting question reaches nothing at all")
	}
	rendered := RenderWhole(sections)
	labels := 0
	bodyBracket := false
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "## ") {
			labels++
		}
		if strings.HasPrefix(line, "[") {
			bodyBracket = true
		}
	}
	if labels != len(sections) {
		t.Fatalf("RenderWhole printed %d labels for %d sections:\n%s", labels, len(sections), rendered)
	}
	if !bodyBracket {
		t.Fatal("the compacting answer no longer quotes a bracketed screen line; the person's label uniqueness needs that case")
	}
	// AND THE EXPORTED OPENERS MATCH WHAT WAS PRINTED, so a door and a test that
	// share [PersonSectionOpen] cannot drift from the renderer.
	for _, section := range sections {
		if !strings.Contains(rendered, PersonSectionOpen(section.Page)+section.Title) {
			t.Fatalf("RenderWhole is missing PersonSectionOpen(%q)+%q", section.Page, section.Title)
		}
		if strings.Contains(rendered, ModelSectionOpen(section.Page)) {
			t.Fatalf("RenderWhole used the model's bracketed opener for %q", section.Page)
		}
	}
}
