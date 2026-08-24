package tui3

import "testing"

// THE WORDS FORM LEADS THE PAIR, AND THE FINISHED WORD STILL GETS ITS PAGE.
// /standing has two rows on purpose: the one that makes an order (the act the
// command exists for — the owner's own ruling) and the one that opens the page
// of what already stands. A person still typing is offered the act first; a
// person who has typed the whole word, or an alias for it, is about to run
// that word, and enter must answer with the bare form rather than swallowing a
// deliberately typed command into a draft still waiting for words.
func TestStandingOffersTheWordsFormFirstWhileTyping(t *testing.T) {
	var m menu
	m.open = true
	m.rank("stand")
	got, ok := m.choice()
	if !ok {
		t.Fatal("a partial word matched nothing")
	}
	if got.name != "standing" || got.args == "" {
		t.Fatalf("the leading row for a partial word is /%s %q — want the words form of /standing", got.name, got.args)
	}
}

func TestAFinishedCommandWordChoosesItsBareForm(t *testing.T) {
	for _, word := range []string{"standing", "orders"} {
		var m menu
		m.open = true
		m.rank(word)
		got, ok := m.choice()
		if !ok {
			t.Fatalf("%q matched nothing", word)
		}
		if got.name != "standing" || got.args != "" {
			t.Fatalf("enter on the finished word %q would take /%s %q — want the bare form, the page", word, got.name, got.args)
		}
	}
}
