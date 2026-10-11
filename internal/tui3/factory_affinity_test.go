package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/charmbracelet/x/ansi"
)

func TestFactoryShowsPublishingAndTheWorktreeMachine(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	it := factory.Item{ID: 99, State: factory.StateQueued, WorktreeMachine: "laptop"}
	for _, publishing := range []bool{false, true} {
		it.Publishing = publishing
		want := "waiting for laptop"
		if publishing {
			want = "publishing · " + want
		}
		fact, ok := a.factoryStateFact(it)
		if !ok || fact.plain != want || factoryStateWord(it) != want || !strings.Contains(ansi.Strip(a.factoryItemState(it)), want) {
			t.Fatalf("publishing %v: row %q, head %q, card %q", publishing, fact.plain, factoryStateWord(it), ansi.Strip(a.factoryItemState(it)))
		}
	}
	it.State = factory.StateRunning
	if fact, _ := a.factoryStateFact(it); fact.plain != "publishing" || factoryStateWord(it) != "publishing" {
		t.Fatalf("active publisher still shows a run step: %+v", fact)
	}
}
