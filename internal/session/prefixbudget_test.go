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
	"strings"
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
// WHAT HAS BEEN SPENT AND WHAT PAID FOR IT. The working discipline moved into
// the system prompt (prompt.go's [disciplinePrompt]) because the surface that
// picks the approach must carry the discipline for picking it, and it cost 1,779
// bytes. It was paid for, not borrowed: the `# Tool Inventory` section came out,
// 1,832 bytes of prose naming each tool and paraphrasing its description — the
// tool block ahead of message[0] carries every one of those descriptions in
// full, and prompts/system.md's Tool Policy names each tool again where it says
// which one to reach for. The prefix came out 32 bytes lighter than it went in.
// THE MERGE WITH chat-v3-task (2026-08-27). autonomy/v0 sat 39 bytes under
// 45,800. The merge carries two things the v3 side had added on its own
// trunk, neither of them a sentence a lane grew here: `write`'s `append:true`
// (523 bytes of schema and the Tool Policy line naming it) and the media-
// making law (~1.5 KB under `read`/media in the Tool Policy). Dropping either
// in a merge would be a silent regression of a shipped v3 behaviour, so the
// budget moves by what was carried and a little headroom — 48,000 — and the
// follow-up is to say the media law in this file's register (the `read
// perceives media` line was already cut to a third here), not to raise this
// number again.
// THE VIDEO WAVE PAID FOR ITSELF AND FOR #238 (2026-09-01). `edit_video` put
// 1,873 bytes on the belt and landed the prefix 2,632 over, on top of the 758
// #238 had been carrying since the merge above. Nothing was raised and no rule
// was dropped: 3,464 bytes came out of text that stated a law a SECOND time.
// prompts/system.md lost the media-making essay (the manual's own
// making-pictures-audio-and-video page teaches all of it, and `manual` is a
// tool the model can call), the paragraphs restating `bash`, `jobs`, `tasks`,
// `fork`, `manual`, `build_harness` and `change_setting`'s own descriptions
// back at the model, and the three-part propose_task contract its own schema
// fields spell out field by field. It went 23,954 → 20,589; the tool block went
// 26,678 → 26,579; the prefix is 47,168, which is 832 under.
// THE CAPTION WAVE PAID FOR ITSELF OUT OF THE STANDING SECTION (2026-09-03).
// #563 asked the model for one short present-tense line before each tool batch
// and put 319 bytes of prompt on a prefix that was already 5 under, which is
// how it landed 314 over. Nothing was raised: 783 bytes came out of `# Things
// that keep working after this window`, where the recognition warning, the
// discharge test and the anchoring rule were the SECOND copy of what
// `stand`'s own description already says at greater length (tools_standing.go's
// [standDescription], which owns all three by name). A belt without `stand`
// loses nothing either — that section opens by saying it is about the tool. The
// prompt went 21,495 → 20,712 and the prefix is 47,531, which is 469 under.
// THE HANDOFF LAW PAID FOR ITSELF OUT OF A SENTENCE SAID TWICE (2026-09-04). A
// live run ended a turn that had handed its work to a task and was told the ask
// was not finished, so the page now says a handed-off outcome is not work that
// remains — 210 bytes across `## Work or words` and `# Critical`. Nothing was
// raised: the belt's `needs your look` bullet lost its "continue task N"
// sentence, which `tasks` own description already carries word for word
// (tools_tasks.go's [tasksDescription]), and the page went 21,289 → 20,840. The
// prefix is 47,920, which is 80 under.
// AND THE CAUSAL WAVE CAME IN UNDER WHAT IT REPLACED (2026-09-04). A woken turn
// now answers the request its result belongs to, so the landed-work paragraph
// says which request that is (+23), and the handoff receipt lost the clause the
// `# Critical` bullet above already carries (-33). Nothing was raised: the page
// went 20,840 → 20,830 and the prefix is 47,958, which is 42 under.
// THE VERIFICATION CONTRACT PAID FOR ITSELF OUT OF THREE SENTENCES SAID TWICE
// (2026-09-04). `propose_task` and `divide_work` grew a `checks` field — the
// repeatable verification a piece of work is put under contract with, which is
// the only thing its checker may run (task_checks.go) — and it landed the prefix
// 388 over. Nothing was raised. `acceptance` gave up "the command that passes",
// which is now the field next to it; `wide` gave up the two sentences
// prompts/system.md's own handoff section already spells; `model`, `max_steps`
// and `no_progress` gave up their tails about what the harness then does; and the
// belt's `tasks` bullet lost its `id` sentence, which [tasksDescription] carries
// in full. The two waves together leave the prefix at the figure the test prints;
// neither raised the budget and the ledger above is what each of them paid.
const fixedPrefixBudget = 48_000

// widestPage is the page at its heaviest: prompts/system.md with every one of
// its tool-naming facts in the PRESENT case (beltfacts.go).
//
// THE BUDGET WEIGHS THE WIDEST PAGE AND NOT ONE SHAPE'S. Those facts are
// composed per agent now — a worker without `watch` reads one sentence where a
// conversation reads another — so there is no single string to measure any
// more, and the honest thing to bound is the most any agent can be handed. It
// is also the page the person's own conversation reads, which is the one that
// is paid for on every turn of every day.
func widestPage() string {
	widest := func(facts []beltFact, join string) string {
		lines := make([]string, 0, len(facts))
		for _, fact := range facts {
			lines = append(lines, fact.present)
		}
		return strings.Join(lines, join)
	}
	page := strings.Replace(systemPrompt, beltFactsToken, widest(beltFacts, "\n"), 1)
	page = strings.Replace(page, handoffFactsToken, widest(handoffFacts, "\n"), 1)
	return strings.Replace(page, programFactsToken, widest(programFacts, "\n\n"), 1)
}

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
	tools, prompt := len(block), len(widestPage())
	total := tools + prompt
	t.Logf("the fixed prefix is %d bytes (~%d tokens): prompt %d + tools %d", total, total/4, prompt, tools)

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
