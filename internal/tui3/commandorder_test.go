package tui3

import (
	"strings"
	"testing"
)

func TestCommandCatalogueIncludesTheCompleteArgumentForms(t *testing.T) {
	wants := []string{"/crew cap task <dollars>", "/land now", "/cache clean now"}
	for _, want := range wants {
		found := false
		for _, command := range commands {
			if strings.TrimSpace(command.typed()) == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("command catalogue does not offer %s", want)
		}
	}
}

// THE LIST LEADS THE PAIR, AND THE FINISHED WORD STILL GETS ITS PAGE.
// /automations has two rows on purpose: the place that lists them, and the
// exact line that adds one. A person still typing the word is offered the list
// first — saying what they want in the conversation is the ordinary way to make
// one, and the typed line is for somebody who already knows its grammar.
func TestAutomationsOffersTheListFirstWhileTyping(t *testing.T) {
	var m menu
	m.open = true
	m.rank("autom")
	got, ok := m.choice()
	if !ok {
		t.Fatal("a partial word matched nothing")
	}
	if got.name != "automations" || got.args != "" {
		t.Fatalf("the leading row for a partial word is /%s %q — want the bare /automations", got.name, got.args)
	}
}

// A person who has typed the whole word is about to run that word, and enter
// must answer with the bare form rather than swallowing a deliberately typed
// command into a draft still waiting for words.
func TestAFinishedCommandWordChoosesItsBareForm(t *testing.T) {
	for _, word := range []string{"automations", "task", "redo", "workspace"} {
		var m menu
		m.open = true
		m.rank(word)
		got, ok := m.choice()
		if !ok {
			t.Fatalf("%q matched nothing", word)
		}
		want := word
		if got.name != want || got.args != "" {
			t.Fatalf("enter on the finished word %q would take /%s %q — want the bare form", word, got.name, got.args)
		}
	}
}
