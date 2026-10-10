package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// rulingAgent is a remembering agent that also keeps rules — the optional
// interface /always asserts ([alwaysAgent]). It records where each rule was
// asked to hold, which is the one decision the surface makes about one.
type rulingAgent struct {
	rememberingAgent
	rules      []session.MemoryLine
	everywhere []bool
	listedAt   []bool
}

func (r *rulingAgent) RememberAlways(text string, everywhere bool) (string, error) {
	r.everywhere = append(r.everywhere, everywhere)
	r.rules = append(r.rules, session.MemoryLine{ID: "mem_rule", Title: text, Text: text, Always: true})
	reach := "in this project"
	if everywhere {
		reach = "in every project"
	}
	return "always · " + text + " · " + reach, nil
}

func (r *rulingAgent) AlwaysMemories(everywhere bool) ([]session.MemoryLine, error) {
	r.listedAt = append(r.listedAt, everywhere)
	return r.rules, nil
}

// /always KEEPS A RULE AND ANSWERS WITH THE ENGINE'S RECEIPT; BARE, IT LISTS THE
// RULES IN FORCE, and an empty list says how to make one. Typed in a
// conversation it holds in that conversation's project; typed on home it is the
// person's everywhere.
func TestTheAlwaysCommandKeepsARuleAndListsTheRules(t *testing.T) {
	agent := &rulingAgent{}
	a := newTestApp(agent)

	settleMemorySlash(t, a, "/always")
	if got := lastNote(t, a); got != "no rule holds here yet · /always <text> keeps one" {
		t.Fatalf("bare /always with no rules answered %q", got)
	}
	settleMemorySlash(t, a, "/always use tabs in this repository")
	if got := lastNote(t, a); got != "always · use tabs in this repository · in this project" {
		t.Fatalf("/always <text> answered %q", got)
	}
	if len(agent.everywhere) != 1 || agent.everywhere[0] {
		t.Fatalf("a rule typed in a conversation was asked to hold everywhere: %v", agent.everywhere)
	}
	settleMemorySlash(t, a, "/always")
	if got := lastNote(t, a); !strings.HasPrefix(got, "rules in force here · 1\n· use tabs in this repository  (mem_rule)") {
		t.Fatalf("bare /always listed %q", got)
	}

	a.showPage(pageHome)
	settleMemorySlash(t, a, "/always never force-push a shared branch")
	if len(agent.everywhere) != 2 || !agent.everywhere[1] {
		t.Fatalf("a rule typed on home was not the person's everywhere: %v", agent.everywhere)
	}
	if fate := homeFate("always", ""); fate != fateAnswers {
		t.Fatalf("bare /always on home has the fate %q, want it answered there", fate)
	}
	if fate := homeFate("always", "use tabs"); fate != fateAnswers {
		t.Fatalf("/always <text> on home has the fate %q, want it answered there", fate)
	}
}

// MEMORY OFF IS RULES OFF, said in the sentence every memory command says.
func TestTheAlwaysCommandWithMemoryOffSaysSo(t *testing.T) {
	agent := &rulingAgent{rememberingAgent: rememberingAgent{off: true}}
	a := newTestApp(agent)
	settleMemorySlash(t, a, "/always use tabs")
	if got := lastNote(t, a); got != memoryOffNote {
		t.Fatalf("/always with memory off answered %q, want %q", got, memoryOffNote)
	}
	if len(agent.everywhere) != 0 {
		t.Fatal("a rule was kept with memory off")
	}
}

func TestTheAlwaysCommandIsOnTheList(t *testing.T) {
	var bare, worded bool
	for _, c := range commands {
		if c.name != "always" {
			continue
		}
		bare = bare || c.args == ""
		worded = worded || c.args != ""
	}
	if !bare || !worded {
		t.Fatalf("/always is not on the list in both forms (bare %v, with words %v)", bare, worded)
	}
	if help := helpText("", chordSpelling{}); !strings.Contains(help, "/always") {
		t.Fatal("/always is not in /help")
	}
}

// THE MEMORY PLACE MARKS A RULE AND `a` TURNS A LINE INTO ONE AND BACK. The rule
// leads its facts with `always`, typing `always` narrows the page to it, the
// strip and the foot name `a` in the word for the line's state, and pressing it
// writes the flag, corrects the page in place and says what changed.
func TestTheMemoryPlaceMarksARuleAndATogglesIt(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{
		{ID: "m1", Owner: store.OwnerUser, Title: "uses neovim", Text: "uses neovim daily",
			Scope: store.MemoryScopeUser, Type: store.MemoryFact, Status: store.MemoryActive},
		{ID: "m2", Owner: store.OwnerUser, Title: "tabs", Text: "use tabs everywhere",
			Scope: store.MemoryScopeUser, Type: store.MemoryPreference, Status: store.MemoryActive, Always: true},
	})
	a.slash("/memory")

	var ruleFacts []rowField
	for _, line := range a.mem.reading.lines {
		if line.memory != nil && line.memory.ID == "m2" {
			ruleFacts = line.facts
		}
		if line.memory != nil && line.memory.ID == "m1" && len(line.facts) > 0 && line.facts[0] == rowSay(memoryRuleFactWord) {
			t.Fatal("an ordinary line wears the rule's fact")
		}
	}
	if len(ruleFacts) == 0 || ruleFacts[0] != rowSay(memoryRuleFactWord) {
		t.Fatalf("the rule's facts = %v, want %q first", ruleFacts, memoryRuleFactWord)
	}
	filtered := readMemory(a.mem.shelves, map[string]bool{store.MemoryScopeUser: true}, "always", a.mem.read)
	var shown []string
	for _, line := range filtered.lines {
		if line.memory != nil {
			shown = append(shown, line.memory.ID)
		}
	}
	if len(shown) != 1 || shown[0] != "m2" {
		t.Fatalf("typing always kept %v, want the rule alone", shown)
	}

	// The cursor walks to the ordinary line; its `a` makes it a rule.
	drive(t, a, key("down"))
	line, ok := a.mem.choice()
	if !ok || line.ID != "m1" {
		t.Fatalf("the cursor is not on the ordinary line: %+v %v", line, ok)
	}
	if got := (placeMemory{}).hint(a); got != memoryLineHint+" · a "+memoryAlwaysWord {
		t.Fatalf("over an ordinary line the foot reads %q", got)
	}
	pressVerb(t, a, 'a', memoryAlwaysWord)
	if len(memory.ruled) != 1 || memory.ruled[0] != "m1=true" {
		t.Fatalf("`a` wrote %v, want m1 made a rule", memory.ruled)
	}
	if a.mem.footer != "always · 'uses neovim' is in front of every conversation now" {
		t.Fatalf("the receipt reads %q", a.mem.footer)
	}
	if line, _ := a.mem.choice(); !line.Always {
		t.Fatal("the page was not corrected in place")
	}
	// And `a` again, in the other word, takes it back.
	if got := (placeMemory{}).hint(a); got != memoryLineHint+" · a "+memoryWhenMattersWord {
		t.Fatalf("over a rule the foot reads %q", got)
	}
	pressVerb(t, a, 'a', memoryWhenMattersWord)
	if len(memory.ruled) != 2 || memory.ruled[1] != "m1=false" {
		t.Fatalf("`a` on a rule wrote %v, want it taken back", memory.ruled)
	}
	if a.mem.footer != "'uses neovim' comes up only when it matters now" {
		t.Fatalf("the receipt reads %q", a.mem.footer)
	}

	// The card says so first.
	a.mem.openMemoryCard(a, store.Memory{ID: "m2"})
	card := strings.Join(a.mem.card(80, a.pal), "\n")
	if !strings.Contains(card, memoryRuleFactWord+" · "+store.MemoryPreference) {
		t.Fatalf("the rule's card does not lead with %q:\n%s", memoryRuleFactWord, card)
	}
}

// A LINE NOBODY CAN PROVE THE PROJECT OF IS NEVER OFFERED `a`, and neither is a
// line that was let go of: neither can be put in front of anything.
func TestTheMemoryPlaceOffersNoRuleWhereNoneCanHold(t *testing.T) {
	for name, row := range map[string]store.Memory{
		"quarantined": {Owner: store.OwnerLegacyProject, Scope: store.MemoryScopeProject, Status: store.MemoryActive},
		"let go":      {Owner: store.OwnerUser, Scope: store.MemoryScopeUser, Status: store.MemoryForgotten},
	} {
		row.ID, row.Title, row.Text, row.Type = "m1", "a line", "a line", store.MemoryFact
		if _, offered := memoryRuleVerb(row); offered {
			t.Errorf("%s: `a` is offered on a line that cannot be a rule", name)
		}
	}
	if word, offered := memoryRuleVerb(store.Memory{Owner: store.OwnerUser, Status: store.MemoryActive, Always: true}); !offered || word != memoryWhenMattersWord {
		t.Errorf("a rule is offered %q (%v), want %q", word, offered, memoryWhenMattersWord)
	}
}

// pressVerb runs the strip verb on key, failing when the strip does not offer it
// in the word expected.
func pressVerb(t *testing.T, a *app, key rune, word string) {
	t.Helper()
	for _, v := range (placeMemory{}).verbs(a) {
		if v.key == key {
			if v.word != word {
				t.Fatalf("`%c` reads %q, want %q", key, v.word, word)
			}
			v.do()
			return
		}
	}
	t.Fatalf("the strip does not offer `%c`", key)
}
