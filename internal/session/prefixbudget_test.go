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
// (INVERTED ON 2026-09-11: the page section owns all three now and
// [standDescription] points at it — see the entry that set the budget below.)
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
// AND THE FORWARDING DOOR PAID FOR ITSELF OUT OF THREE SECOND COPIES
// (2026-09-04). The person steering from the main chat needs one more field on
// `tasks` and one clause on the belt saying when to reach for it (+301, mostly
// the field's own "their words, never yours"). Nothing was raised: the tool's
// description lost the "continue task N" sentence and the URI sentence, both of
// which the `continue` field and the belt's own citation bullet already carry
// word for word; `id` lost "Running, it answers with its LIVE state", which the
// belt bullet under it says at greater length; and the belt lost the
// `scope: "everywhere"` clause the `scope` field governs. The tool block went
// 27,128 → 27,169, the page 20,830 → 20,797, and the prefix is 47,966, which is
// 34 under.
// Specialist discovery (2026-09-05) reduced this fixture's tool block from
// 27,079 to 23,438 bytes. The widest prompt grew from 20,527 to 20,909 bytes
// to explain loading, leaving 44,347 combined: 3,259 fewer than the 47,606
// baseline. The 48,000-byte cap stays unchanged. With every media model and
// saved procedure configured, the complete tool block is 40,595 bytes and
// discovery carries 26,740, including its 708-byte loader. Measure whole JSON
// arrays rather than adding separately encoded array sizes.
// widestPage weighs the larger direct/deferred wording for each fact.
// THE BELT WAS NOT THE DOOR'S, AND THE BUDGET IS NOW SET ON THE DOOR (2026-09-11).
// [v3ShapedAgent] was a hand list with no standing store, no collections
// database, no journal, no accounts hub and no memory store, so every figure
// above weighed a conversation without `stand`, `collections`,
// `shared_context`, `context_trace`, `services`, `use_service`, `remember` or
// `search_conversations` — eight tools the interactive door carries on every
// request. It reported 47,942 while the door sent 67,034. It builds through
// [conversationDoor] now, which is chatv3.go's list and no second copy of it.
//
// What that first honest measurement was paid down with: `stand` held its
// recognition twice — the discharge test, anchoring, waking or holding and the
// passed-moment recomputation in its own description AND on the page section
// built from the same predicate — so those are now said once, on the page, and
// the description points at them (tools_standing.go's [standDescription]). The
// limits law is NOT said once: the page states it as a law and each of the six
// fields it governs keeps its own "only when they named it" clause, because the
// field is where the model decides. `stand` went 11,008 → 5,749 and the page
// 23,898 → 24,607.
//
// The door then measured 62,484 bytes: 24,607 of page and 37,877 of tool block
// over 26 tools, of which the eight above are 13,825 (`stand` 5,749,
// `shared_context` 1,792, `search_conversations` 1,613, `collections` 1,456,
// `use_service` 999, `remember` 994, `services` 695, `context_trace` 527). The
// budget is that measurement plus a tenth, rounded up to the thousand — the
// founding rule of this constant, applied to the shape it was always meant to
// bound. THIS IS A SHAPE CORRECTION AND NOT A LANE RAISING THE NUMBER: 48,000
// bounded a belt this door never had, and the bytes it did not see were already
// being sent. The review's two restorations then put 113 bytes back — `altitude`
// maps "everywhere" to machine again, and a hold's one rail is `expires` — for
// 62,597, which the same rule still rounds to 69,000. The next failure against
// it is paid for out of a second copy and not raised, exactly as every entry
// above was.
//
// 69,000 BOUNDS THE SHIPPING DEFAULT DOOR, NOT EVERY DOOR. The fixture carries
// what the door carries on every machine; these it carries only on some, and
// they are unweighed: `web_search` and `web_fetch` (a search provider is
// configured, chatv3.go's `v3Search`), `view_image` (a media client and a vision
// model), furrow's `workspace_snapshots`, `workspace_restore`, `workspace_fork`
// and `workspace_merge` (a folder attached to furrow), `workspace` (an owned
// conversation with no project yet), and the longer `load_capability` lines a
// media group and saved programs add. Together they are several kilobytes, so a
// fully configured door likely spends the tenth of headroom and may pass it.
//
// AND THE FIRST PLACE TO LOOK WHEN IT FAILS. These rules are still said more
// than once on this prefix, in other lanes' tools, and were left for them:
//   - a line into a task does not stop it — 4: [tasksDescription], `tasks`'
//     `say`, the belt's `tasks` bullet (beltfacts.go) and prompts/system.md's
//     `SAYING "STOP" TO A TASK DOES NOT STOP IT`;
//   - a landed task's four words and the `your call` verbs — 2:
//     prompts/system.md's `A LANDED TASK SAYS ONE OF FOUR WORDS` and
//     `your call IS A QUESTION` against the belt's `A LANDED TASK SAYS` bullet;
//   - wide work is one task, never split — 3: `propose_task`'s description, the
//     `WIDE WORK` handoff row and prompts/system.md's `THERE IS NO PLANNER`;
//   - what you learned goes with the brief — 2: `propose_task`'s `brief` and
//     prompts/system.md's `AND WHAT YOU HAVE ALREADY LEARNED GOES WITH IT`;
//   - never wait or poll for what was handed off — 4: `propose_task`, `fork`,
//     `jobs` and `tasks`, each in its own description.
const fixedPrefixBudget = 69_000

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
	page := systemPrompt
	// THE SAME LIST THE PAGE IS COMPOSED FROM (beltfacts.go's [promptSections]),
	// walked with every row in its PRESENT case. A section added there is weighed
	// here without a second edit, so the budget cannot go on measuring a page the
	// composer stopped building.
	for _, section := range promptSections {
		lines := make([]string, 0, len(section.facts))
		for _, fact := range section.facts {
			// AND THE HEAVIER OF THE TWO PRESENT CASES. A fact whose tools wait
			// on a shelf reads one sentence for the conversation that must
			// fetch them and a shorter one for the worker that carries them
			// (beltfacts.go), and the page this budget bounds is the widest any
			// agent can be handed.
			widest := fact.present
			if len(fact.shelved) > len(widest) {
				widest = fact.shelved
			}
			lines = append(lines, widest)
		}
		page = strings.Replace(page, section.token, strings.Join(lines, section.join), 1)
	}
	return page
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
