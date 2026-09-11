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
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	configpkg "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
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
// `manual`, `build_harness` and `change_setting`'s own descriptions
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
// THE DIET'S DELETE PASS TOOK 677 BYTES AND ADDED A LAW (2026-09-10). Lane C of
// the prompt diet (docs/design/prompt-diet/DESIGN.md §1) deleted the SECOND and
// THIRD copies of five rules and put ONE new sentence in their place, and the
// new sentence is the reason the deletions are safe rather than merely cheap:
// "anything handed off — a job, a watch, a task, a quick task — reports itself
// into this conversation; never sleep, tail or poll for it" is 142 bytes that
// says once what ten per-tool sentences were saying separately. Against it,
// 819 bytes came out of text that stated a law already stated: the ask law was
// on the page twice and in the belt fact under it a third time, so the ladder
// bullet keeps it and the other two are gone; `# Critical`'s informed-action
// bullet was the second telling of "never ask what the record answers";
// prompts/system.md taught `write`'s append and `read`'s offset/limit, which
// tools_write.go's [appendSentence] and bare's readDescription own word for
// word; "Start ONE `watch`" contradicted [watchDescription]'s own count and is
// gone; and the ask and settings belt facts gave up the two sentences about
// what `load_capability` does once it is called, which is that tool's own
// description. Nothing was raised. The page went 23,391 → 22,714, the tool
// block is unmoved at 24,044, and the prefix is 46,758 — 1,242 under.
// lawregistry_test.go is what keeps it there: every law above is filed under an
// id and a class, and a second copy of one is now a build failure rather than a
// thing the next audit finds.
// AND THE EVENT LANE PAID NOTHING AND TOOK 2,189 BYTES BACK (2026-09-10). The
// prompt diet's WITH THE EVENT pass (docs/design/prompt-diet/DESIGN.md §2): a
// harness-authored message now carries its own reading instruction, so the page
// stopped explaining messages it may never see. A landed task's note opens on
// [landingNoteLead] — 234 bytes on the turn a task lands, with the tier word
// interpolated — and a job's ending carries [jobExitNewsRule]. In exchange
// `# Interrupts and steering` lost the woken-turn and `[carry on]` paragraphs
// (the messages say all of it, and [checkpointCarryOnLead] has said its own
// half since it was written), `# Session facts` lost the four-words, `your
// call` and saying-stop bullets, and the belt's `tasks` fact lost its second
// copy of the four words and the resolve verbs — [settleClause] interpolates
// those from [TaskResolutions] on the note itself, and `tasks` own description
// owns "To END running work use stop". Nothing was raised and no law left the
// build. On dev alone it was 23,391 → 21,202; landing after lane C's delete
// pass it is 22,714 → 20,525, and the two together leave the prefix at 44,569,
// which is 3,431 under. Neither raised the budget.
// AND THE TOOL DESCRIPTIONS GAVE BACK 1,221 BYTES AND ADDED A LAW (2026-09-10,
// the prompt diet, lane F). The tool block went 24,044 → 22,823 and nothing was
// raised; the prompt is untouched by this lane, so the prefix went 47,435 →
// 46,214. Every byte came out of text that said something a SECOND time. The
// handed-off-work-reports-itself law was written four times across the belt —
// twice in `jobs`, once in `watch`, once in `propose_task` — and is now on the
// page once; `read`'s senses sentence enumerated what a picture, a recording and
// a video each come back as, where "described, never as bytes" is the whole rule
// (322 → 95); and the routing sentences left `tasks` (three of them),
// `read_document` and `recall` for the page's routing table, which states each
// once for the whole belt rather than once per tool. Per tool: tasks 2,738 →
// 2,272, read 1,169 → 952, jobs 1,103 → 802, watch 1,495 → 1,396, read_document
// 819 → 754, recall 324 → 269, manual 616 → 603, commit 330 → 325, track 787
// unchanged. schemalaw_test.go is the gate that keeps it: a parameter
// description past 200 bytes, or shouting, or reaching for a dash, now fails the
// build instead of waiting for this number to notice it. Merged with the page
// passes above, the prefix is 43,348 — page 20,525 and tools 22,823, which is
// 4,652 under a cap no lane of this wave moved. And `watch.instead-of-polling`
// left lawregistry_test.go with the sentence it filed: `handoff.reports-itself`
// now matches on "never sleep, tail or poll", so putting any of the four
// per-tool copies back fails that gate wherever it is put.
// THE DIET'S ON-DEMAND LANE PAID 3,854 BYTES BACK AND ASKED FOR NOTHING
// (2026-09-10). Four runs of prose came off prompts/system.md and message[0]
// stopped carrying any of them, because each one is already delivered by
// whoever needs it and only then (docs/design/prompt-diet/DESIGN.md §2):
//   - the standing section, 2,482 bytes, is one existence line. Its mechanics
//     are tools_standing.go's [standDescription] and [standSchemaJSON], beside
//     the field each governs; its `[something you set up fired]` frame is
//     standing_run.go's [standingNewsRule], already under the firing's own line;
//     the rest is the chat manual's keeping-an-eye page.
//   - the two paragraphs defining a saved recipe and a saved program are the
//     `harnesses` group's own prose (tools_capabilities.go), emitted under the
//     `Loaded:` line by the load that fetches the four verbs. The page keeps the
//     routing line that names `list_harnesses` and `build_harness`.
//   - the accounts block, 1,142 bytes over three bullets, is one existence line.
//     [serviceRequestDescription] already names the address and says which half
//     the person turned off; the send verbs already say they are asked about
//     first and cannot be called back.
//   - the media-making essay is the `media` group's prose, and the page keeps
//     one line: anchor in a real medium, specify positively, `manual` for the
//     rest. `generate_image` and `generate_video` state it a third time in the
//     `prompt` field, where it is read at the call.
//
// Nothing was raised and no law was dropped, and each of the four is filed in
// lawregistry_test.go under class `demand` with the place that now owns it. On
// its own, on top of lane C's delete pass, it took the page 22,714 → 18,860;
// landing beside the event and description lanes above it leaves the page at
// 16,671, the tool block at 22,823 which it did not touch, and the prefix at
// 39,494 — 8,506 under.
// AND THE ROUTING TABLE BOUGHT BACK 226 BYTES OF WHAT THE DESCRIPTIONS GAVE UP
// (2026-09-10). A description states a contract and never when to reach for the
// verb (DESIGN.md §4), so when the tool lane took the routing prose off
// `tasks`, `read_document` and the rest, three triggers had nowhere left to be
// said: that a look inside running or landed work is `tasks` with its id, that
// "continue task N" is that id with `continue` and never a fresh
// `propose_task` — which mints a second task with a fresh brief and a fresh
// working copy (task_continue.go) — and that `read` is the door for text,
// source and a PDF that has a text layer, `read_document` only for what `read`
// cannot turn into text at all. The first two ride the `tasks` belt fact,
// because a floor node carries neither verb; the third is two clauses folded
// into the `read` bullet that was already there. Nothing was raised, nothing
// was said twice (all three are filed in lawregistry_test.go under class
// `core`), and the "earlier work referred to but not pointed at" trigger was
// left exactly where it already was rather than restated here. The page went
// 16,671 → 16,897, the tool block is unmoved at 22,823, and the prefix is
// 39,720 — 8,280 under.
// ATTRIBUTION COST 863 BYTES AND 333 OF THEM ARE THE FEATURE (2026-09-10). Lane
// I of the prompt diet gave the chat the law the resident has had all along, and
// a third case with it: aforge signs the git work it does in somebody's name —
// one trailer on a commit, one footer line on a pull request or issue body, and
// one small `<sub>` line on the FIRST comment it leaves in a thread and no later
// one. It is a law this page did not state at all, so nothing was deleted for it;
// there was no second copy to delete.
//
// THE FLOOR IS THE CONSTANTS AND THE FLOOR IS 333 BYTES. The trailer, the pull
// footer and the comment line are constants because the exact bytes are what
// attributes — a footer the model half-remembers counts as nothing — so this is
// the one law on the belt that cannot be paraphrased down. What IS paid for: the
// issue footer is named by the single utm parameter that differs rather than
// spelled a second time (145 bytes), and the law is four sentences, three places
// and one nowhere-else, with no example and no reasoning.
//
// It is also CONDITIONAL — off with the row off, off in a hand whose bash cannot
// commit — so this figure is the widest page and not everybody's. On dev alone
// the page was 22,714 → 23,577; landing after lanes A, C, D and F it is
// 16,898 → 17,761, the tool block is unmoved by this lane at 22,823, and the
// prefix is 40,584 — 7,416 under, and the cap is untouched.
// AND THE RESULT CAPS PAID 652 BYTES ON THE WAY TO FIXING A DEFECT (2026-09-10).
// Lane B (§5 item 1) made every result cap a share of the model's window instead
// of a flat 2000 lines / 50KB, which meant the figures had to be rendered from
// the caps in force rather than typed — and a description built at belt time is
// a description that can be weighed. So the same pass cut `read` and `bash` to
// their contract: `bash` gave up the routing sentence naming `propose_task`
// (which road work belongs on is `## Work or words`, and the manual's own "a job
// is the wrong door for work whose result is a deliverable"), and the arrival
// law's three copies — the sentence, the timeout sentence and the `background`
// argument — went to nought, on the strength of the page's new
// `handoff.reports-itself`. `write`'s append and salvage clause says the same
// two rules in 165 fewer bytes. Nothing was raised: the tool block went
// 24,044 → 23,392 on its own branch and the page is unmoved by this lane. The
// figure below is what it and lane F's pass weigh together, since both cut
// `read`.
// widestPage weighs the larger direct/deferred wording for each fact.
// AND THE QUICK TASK IS THE FIRST WAVE SINCE THIS FILE WAS WRITTEN THAT RAISED
// IT (2026-09-10), which is worth saying plainly rather than burying under the
// ledger above: every entry there paid for itself out of a sentence said twice,
// and this one could not, because it is not a sentence — it is a VERB the
// product did not have.
//
// `quick_task` encodes to 1,196 bytes and the belt's own bullet for it is 129
// more, and both were cut to the bone before this line moved. The description is
// the judge and nothing else — the six sentences that decide between a task, a
// quick task and doing the thing yourself (task_quick.go) — with the "the id
// returns at once, so never poll" sentence left off because [taskDescription]
// carries it and the two verbs are on a belt together or on neither. The schema
// is six fields whose descriptions are one clause each, and `depends_on` and
// `model` give up their rules entirely to `propose_task`'s copies of the same
// two fields. For comparison, `propose_task` encodes to 5,720.
//
// AND THE CHOICE WAVE PAID IT BACK THE NEXT DAY (2026-09-10), so the cap is
// 48,000 again and the measured prefix is 46,245, which is 1,755 under. Two
// things happened in one commit. The belt's hand-off section stopped being a
// list of bullets that sorted work by WIDTH and became one picture of what the
// model HAS and what each thing COSTS (beltfacts.go says why, and what a real
// model did with the list); the picture is a net saving on the three rule lists
// it replaced, and it states "never poll" once for every road rather than per
// verb. And `propose_task`'s schema went on the same diet its description went
// on: one clause per field, the dowry prose dropped from `brief` because
// prompts/system.md teaches it and a test pins it there, and the em dashes
// taken out of every description string, small models tokenising them badly.
// The tool block went 23,369 → 21,808 and the page reads 24,437.
//
// WHAT IS STILL OWED. The planner rule is in the prefix twice —
// `taskDescription`'s "do not reach for a planner" and prompts/system.md's own
// `THERE IS NO PLANNER ON YOUR BELT` paragraph — and both are pinned by
// TestTheBeltRoutesWideWorkToOneWorkerAndNotToAPlanner, so paying it back is a
// change to that test's mind and not only to the bytes.
const fixedPrefixBudget = 48_000

// THE LEAN PROFILE GETS A BUDGET OF ITS OWN (2026-09-10, the prompt diet's lane
// G). promptprofile.go added a second shape of prefix for a model with a small
// window or the crew's open-weight worker seat: two sections come off the page,
// four more groups wait on the shelf, `ask` is handed over rather than fetched,
// the memory reflex does not run and the project's instruction file rides under
// 2 KiB. Nothing above it moved — the full arm renders byte for byte what it
// rendered before, which [TestAFrontierShapeIsUntouchedByTheProfile] asserts at
// every door — so this is a second number and not a raised one.
//
// WHAT IT MEASURES AND WHERE IT HAS TO GET TO. On a 16k window the lean prefix
// is what this test prints. On the profile's own branch it was 34,343 bytes
// against the full arm's 47,435; merged onto the rest of the diet — lane C's
// delete pass and routing table, lane D's self-describing messages, lane E's
// pulled mechanics, lane F's contract-only descriptions, lane B's window-scaled
// caps and the quick-task wave that took `fork` off the belt — it is 31,006
// against 38,742: page 16,825 plus tool block 14,181 over twelve tools.
//
// THE BELT IT WEIGHS IS THE SHAPE THE DESIGN ASKED FOR: the seven pi tools,
// `quick_task`, `jobs`, `manual`, `load_capability` and `ask`, with
// `propose_task`, `tasks`, `watch`, `track`, `commit`, `recall` and
// `read_document` one call away.
//
// The diet's target for this arm is 12,000 bytes
// (docs/design/prompt-diet/DESIGN.md §6) and nothing in this wave reaches it,
// which is worth saying plainly rather than rounding away. The two numbers that
// would move it were both named on landing: `ask`'s schema, and the page's
// 16,825, which is the shared CORE minus one section and so comes down when CORE
// does and not before.
//
// `ask` HAS NOW BEEN THROUGH, AND THE NUMBER CAME DOWN WITH IT (2026-09-10).
// It was 4,277 bytes of the tool block, thirty percent of the lean arm, because
// it is the one verb that is shelved on a full belt and PRE-ARMED on a lean one
// (promptprofile.go) — the schema a small model pays for on every request and a
// frontier model never sees. It is 3,844 now: no field left, no enum left, and
// what went was prose saying a field's own name back at the model (`"Block
// kind"` beside the list of block kinds, `"Subject kind"`, `"Turn waits"`,
// `"Certainty"` on a low/medium/high enum, and the same six repeated inside the
// evidence block, which is spliced into the schema TWICE) plus the tail of the
// two long ones. The lean prefix went 31,006 → 30,573, and the budget below went
// down with it rather than staying where it was: page 16,825 unchanged, tool
// block 14,181 → 13,748. The full arm is untouched at 38,742, byte for byte,
// which is what a shelved verb means.
//
// AND IT CARRIES A LITTLE HEADROOM, DELIBERATELY. Pinned to the exact
// measurement it was the one number in the tree that made the shared page
// unmovable: the routing-table commit added 226 bytes of CORE, which both arms
// read, and a lean budget with no slack failed a change the full budget waved
// through with thousands to spare. So the figure it LANDS at is about three
// percent over what it measured on landing — room for a few moves of that size,
// and not room for a paragraph.
//
// AND `ask` GREW AGAIN, WHICH THE HEADROOM ABSORBED AND THIS LINE SAYS OUT LOUD
// (2026-09-11, the questions wave). The schema's enums are now written from the
// Go constants rather than typed out beside them — so `pick.confidence` offers
// the three words the code actually reads, `input.blanks` and `input.dial` carry
// their shapes, and `subject.kind` carries `order`. That is four vocabularies and
// two objects the model could not see before, and it cost bytes: 3,844 → 4,235.
// 137 of them were won back the way the last pass won its 433, by deleting prose
// that says a field's own name back at the model (`"Comparison axis values, one
// short text per axis"`, `"What it holds before they type"`, `"kind blanks only"`)
// — leaving the schema at 4,098 and the lean prefix at 31,035 against the 31,500
// here, about 465 bytes of room.
//
// THAT IS THE HEADROOM DOING ITS JOB AND NOT A LICENCE. The paragraph above says
// what it is for: a few moves of a couple of hundred bytes, so a shared page can
// still move. A wave that needs more than this is a wave that takes something
// out first.
//
// THEREAFTER IT ONLY EVER RATCHETS DOWN, in the ledger discipline the full
// budget above is kept under: a lane that takes bytes out lowers it in the same
// commit, and nothing ever raises it again.
const leanPrefixBudget = 31_500

// leanWindow is the window the lean budget is weighed at. Sixteen thousand
// tokens is the shape the profile was written for — a local open-weight model —
// and it is comfortably under [leanWindowThreshold], so the shape this measures
// is the shape a person on such a model actually gets.
const leanWindow = 16_000

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

// ── the lean arm ────────────────────────────────────────────────────────────

// leanShapedAgent is the shipping conversation door on a small window: the same
// config [v3ShapedAgent] builds, with the window a local open-weight model
// actually has. Everything else about the shape is deliberately identical, so
// the difference between the two numbers below is the profile and nothing else.
func leanShapedAgent(t *testing.T) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		config.ContextWindow = leanWindow
		config.AskConsent = true
		config.BashBackgroundAfterSeconds = configpkg.DefaultBashBackgroundAfter
		config.HarnessStore = subharness.At(t.TempDir())
		config.RunHarness = func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error) {
			return "", subharness.Usage{}, nil
		}
		config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) {
			return "", nil
		}
	})
	return agent
}

// TestTheLeanPrefixStaysUnderItsBudget weighs the other arm.
//
// IT WEIGHS THE PAGE THE SHAPE ACTUALLY READS and not the widest one, which is
// the difference between the two budgets and is deliberate: the full budget
// bounds the most any agent can be handed, because one page is read by every
// shape and a lane adding a sentence must see the worst case. The lean arm is
// one shape — a conversation on a small window — and what it costs that person
// is what it renders for them.
func TestTheLeanPrefixStaysUnderItsBudget(t *testing.T) {
	agent := leanShapedAgent(t)
	if !agent.config.promptProfile().lean() {
		t.Fatalf("a %d-token window did not resolve to the lean profile, so this test is weighing the wrong arm", leanWindow)
	}

	definitions := agent.beltDefinitions()
	if len(definitions) == 0 {
		t.Fatal("the belt is empty, so this test would pass on nothing")
	}
	block, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	// AND `ask` IS IN THE BLOCK, because it is pre-armed rather than shelved
	// (promptprofile.go's [Config.prearmedGroups]). A lean belt that had to load
	// its way to a question would be measured lighter here and be unable to ask
	// one, which is the one regression this number could hide.
	if !agent.hasTool("ask") {
		t.Fatal("`ask` is not carried on a lean belt: a one-call-per-message model cannot load-then-ask")
	}
	prompt := len(renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)))
	tools := len(block)
	total := tools + prompt
	t.Logf("the lean prefix is %d bytes (~%d tokens): prompt %d + tools %d over %d tools",
		total, total/4, prompt, tools, len(definitions))

	if total <= leanPrefixBudget {
		return
	}
	heaviest := make([]struct {
		name  string
		bytes int
	}, 0, len(definitions))
	for _, definition := range definitions {
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		heaviest = append(heaviest, struct {
			name  string
			bytes int
		}{definition.Function.Name, len(encoded)})
	}
	sort.Slice(heaviest, func(i, j int) bool { return heaviest[i].bytes > heaviest[j].bytes })
	report := fmt.Sprintf("the lean prefix is %d bytes (~%d tokens), over its %d budget by %d\n"+
		"  the page            %6d\n"+
		"  the tool block      %6d over %d tools\n"+
		"the heaviest tools:\n",
		total, total/4, leanPrefixBudget, total-leanPrefixBudget, prompt, tools, len(definitions))
	for index, tool := range heaviest {
		if index == 8 {
			break
		}
		report += fmt.Sprintf("  %-20s%6d\n", tool.name, tool.bytes)
	}
	t.Fatalf("%sthis arm's number only ever comes down: shelve the verb, cut the law that is stated twice, "+
		"or leave it — never raise the budget", report)
}

// TestAFrontierShapeIsUntouchedByTheProfile is the other half of the deal.
//
// A DIET THAT MOVED THE DEFAULT ARM WOULD BE A DIET NOBODY MEASURED. The lean
// profile is a second shape and not a change to the shipped one, so every door
// promptprofile.go opens is asserted to be the IDENTITY on a frontier window —
// including the page door itself, which is proved on bytes: the composed page
// enters [renderSystemAt]'s output untouched, exactly as it did before there was
// a profile at all.
func TestAFrontierShapeIsUntouchedByTheProfile(t *testing.T) {
	agent := v3ShapedAgent(t)
	config := agent.config
	if config.promptProfile().lean() {
		t.Fatalf("the shipping conversation shape resolved to the lean profile, so every assertion below is about the wrong arm")
	}
	if window := config.promptWindow(); window < leanWindowThreshold {
		t.Fatalf("the frontier shape's window is %d, under the %d threshold: this test is not weighing a frontier shape", window, leanWindowThreshold)
	}

	// THE PAGE DOOR, ON BYTES. What the composer built is what the render
	// carries: no section dropped, no line added.
	composed := strings.TrimRight(promptWithBeltFacts(config), "\n")
	page := renderSystemAt(config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(page, composed) {
		t.Error("a frontier page is no longer the composed page byte for byte: the profile's cut is reaching the full arm")
	}
	if pointer := config.leanShelfPointer(); pointer != "" {
		t.Errorf("a frontier page is handed the lean shelf line %q", pointer)
	}

	// THE SHELF DOOR. The partition is the shipped table and nothing else, and
	// nothing is handed over rather than fetched.
	shelf := config.capabilityShelf()
	if len(shelf) != len(capabilityGroups) {
		t.Fatalf("a frontier belt partitions %d groups, want the shipped %d", len(shelf), len(capabilityGroups))
	}
	for index, group := range shelf {
		if group.name != capabilityGroups[index].name {
			t.Errorf("the frontier partition's group %d is %q, want %q", index, group.name, capabilityGroups[index].name)
		}
	}
	if armed := config.prearmedGroups(); len(armed) != 0 {
		t.Errorf("a frontier belt pre-arms %v; every group waits on the shelf as it always did", armed)
	}
	if !config.shelvesFact(beltFacts[0]) {
		t.Error("`ask` is no longer shelved on a frontier belt, so the page stopped telling it how to fetch one")
	}

	// AND THE TWO NUMBERS prompt.go reads through the profile.
	if got := config.instructionLimit(); got != agentsFileLimit {
		t.Errorf("a frontier prefix bounds the project's instructions at %d, want %d", got, agentsFileLimit)
	}
	if config.onlyOneInstructionFile() {
		t.Error("a frontier prefix quotes one instruction file; both have always ridden")
	}
}
