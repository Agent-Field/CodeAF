package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// First run. The empty thread used to describe the screen — "No messages yet" —
// and taught nothing about what this line is for.
func TestTheEmptyThreadTeachesTheOneLaw(t *testing.T) {
	for _, width := range []int{60, 120} {
		model := New(&fakeBackend{}, "first-run")
		model.setSize(width, 26)
		plain := ansi.Strip(model.View())
		for _, taught := range []string{
			"say it here", "corrections", "resumes where you left it", "? for what you can say",
		} {
			if !strings.Contains(plain, taught) {
				t.Fatalf("width %d empty thread never says %q:\n%s", width, taught, plain)
			}
		}
		if strings.Contains(plain, "No messages yet") {
			t.Fatalf("width %d still describes the screen:\n%s", width, plain)
		}
		if len(welcomeLines) > 5 {
			t.Fatalf("the welcome grew to %d lines", len(welcomeLines))
		}
		// It is the empty state and nothing else: one message retires it.
		model.messages = []store.Message{{Seq: 1, Role: store.RoleUser, Body: "hello"}}
		model.threadGen++
		if strings.Contains(ansi.Strip(model.renderMessages()), "say it here") {
			t.Fatal("the welcome outlived the empty thread")
		}
	}
}

// The ? guide opens with what a person can say, and those rows are the same
// authored lines the head carries in its prompt. One text, two surfaces.
func TestHelpOpensWithTheSameCatalogTheHeadCarries(t *testing.T) {
	if got := helpCategories()[0].title; got != "what you can say" {
		t.Fatalf("the guide opens with %q", got)
	}
	rows := helpCategories()[0].rows
	says := manual.Says()
	if len(rows) != len(says) || len(rows) == 0 {
		t.Fatalf("the guide lists %d verbs, the catalog holds %d", len(rows), len(says))
	}
	for index, row := range rows {
		if row.key != says[index].Verb || row.meaning != says[index].Example {
			t.Fatalf("guide row %d is %+v, catalog row is %+v", index, row, says[index])
		}
	}
	for _, width := range []int{60, 120} {
		model := New(&fakeBackend{}, "catalog-help")
		model.setSize(width, 40)
		model.openHelp()
		content := ansi.Strip(strings.Join(model.helpContentLines(model.helpOverlayWidth()-2), "\n"))
		for _, say := range says {
			if !strings.Contains(content, say.Verb) {
				t.Fatalf("width %d guide is missing %q", width, say.Verb)
			}
		}
		assertFitsWidth(t, model.View(), width, "catalog help")
	}
}
