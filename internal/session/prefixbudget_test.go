package session

// THE FIXED PREFIX, AND WHAT IT COSTS TO LET IT GROW.
//
// Two things ride in front of every request this session makes: the system
// prompt, which is message[0], and the tool schema block, which is encoded ahead
// of message[0]. Neither depends on what anybody said. They are sent again, in
// full, on every tool round of every turn — a measured benchmark cell made 69
// requests and paid for both 69 times.
//
// That is what makes a byte here different from a byte anywhere else in this
// repository. A sentence added to a Go comment is free. A sentence added to a
// tool description is bought roughly sixty times per task, and again on the next
// task, forever. When this prefix was last measured against a competing harness
// it was 26,400 tokens against their 3,500 — 45.6% of every prompt token that
// cell spent, on text that taught the model nothing it had not already been
// told twice on the same page.
//
// The prompt cache does not make this free either. Caching discounts the tokens;
// it does not stop them being sent, counted, or re-priced in full the moment any
// byte in front of them moves (prefixcache_test.go states that half of it).
//
// So this test is a BUDGET, not a measurement. It fails when the fixed prefix
// grows past what the diet left it plus a margin, and its failure message names
// the tool that grew, because the lane that adds a paragraph to a description is
// otherwise the one person in the loop who never sees the bill.
//
// WHAT TO DO WHEN IT FAILS. Not raise the number — that is the one move that
// makes the failure meaningless. Find the sentence you added and ask whether it
// states a rule the model does not already have. A schema description teaches
// what a field is FOR in the fewest words that keep the law; the system prompt
// states each law once. If the addition genuinely carries a new law, take the
// bytes back out of something that repeats one, and leave the budget where it
// is.

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
)

// fixedPrefixBudget bounds the system prompt plus the marshalled tool block of
// the belt the shipping conversation door assembles (belt_wiring_test.go's
// [v3ShapedAgent] is that shape). It is the post-diet measurement with about a
// tenth of headroom on top, which is room for a genuinely new law and not room
// for a paragraph of prose about one that is already stated.
//
// It is a byte count and not a token count deliberately: bytes are what this
// process can measure exactly, and every tokenizer this build talks to is within
// a small factor of four bytes to the token.
//
// The diet that set it left the prefix at 41,684 bytes — 21,499 of system prompt
// and 20,185 of tool block, down from 37,285 and 32,097. Of what remains, 6,551
// bytes are the seven tools this belt takes VERBATIM from pi (internal/exec's
// bare package): they are the lean baseline the diet was measured against and
// are not aforge's to trim. The other 35,133 bytes are aforge's own words, down
// from 62,810 — and that is the half of the bill a lane adding a sentence is
// adding to. The budget is that measurement plus a tenth.
const fixedPrefixBudget = 45_800

// TestTheFixedPrefixStaysUnderItsBudget weighs what every request carries before
// anybody has said anything.
func TestTheFixedPrefixStaysUnderItsBudget(t *testing.T) {
	agent := v3ShapedAgent(t)

	definitions := agent.beltDefinitions()
	if len(definitions) == 0 {
		t.Fatal("the belt is empty, so this test would pass on nothing")
	}
	block, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	tools, prompt := len(block), len(systemPrompt)
	total := tools + prompt

	if total <= fixedPrefixBudget {
		return
	}

	// THE FAILURE NAMES THE BILL, BIGGEST FIRST. A lane reading this has just
	// grown one of these lines and has no other way to see which.
	type weighed struct {
		name  string
		bytes int
	}
	heaviest := make([]weighed, 0, len(definitions))
	for _, definition := range definitions {
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		heaviest = append(heaviest, weighed{definition.Function.Name, len(encoded)})
	}
	sort.Slice(heaviest, func(i, j int) bool { return heaviest[i].bytes > heaviest[j].bytes })

	report := fmt.Sprintf("the fixed prefix is %d bytes (~%d tokens), over its %d budget by %d\n"+
		"  prompts/system.md   %6d\n"+
		"  the tool block      %6d over %d tools\n"+
		"the heaviest tools:\n",
		total, total/4, fixedPrefixBudget, total-fixedPrefixBudget, prompt, tools, len(definitions))
	for index, tool := range heaviest {
		if index == 8 {
			break
		}
		report += fmt.Sprintf("  %-20s%6d\n", tool.name, tool.bytes)
	}
	t.Fatalf("%severy byte here is sent again on every request of every turn, so take the "+
		"addition back out of something that already says it rather than raising the budget", report)
}
