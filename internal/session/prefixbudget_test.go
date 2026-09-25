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
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// fixedPrefixBudget bounds the system prompt plus the marshalled tool block of
// the belt the shipping conversation door assembles (belt_wiring_test.go's
// [shippedShapeAgent] is that shape).
//
// ── IT WEIGHED THE WRONG BELT FOR MONTHS, AND THIS IS THAT REPAIR ───────────
//
// The sentence above has always said "the shipping conversation". The fixture
// under it was [v3ShapedAgent]: a conversation with NO memory store, NO accounts
// hub, NO standing items and NO saved programs — eighteen tools, 39,073 bytes,
// comfortably green. A machine somebody has finished setting up carries five
// more (`stand`, `search_conversations`, `remember`, and the subharness pair)
// and weighs 53,025. So the gate reported eight kilobytes of headroom on a
// prefix that was five kilobytes OVER the cap it was enforcing, and it reported
// it every day. That is #576's shape exactly: a gate pointed at something nobody
// runs passes without having tested anything.
//
// ── SO THE FIGURE IS A RATCHET NOW, AND NOT A BUDGET WITH ROOM IN IT ────────
//
// It used to be "the post-diet measurement plus about a tenth", which was right
// when the number came out of a diet that had just been paid for: the headroom
// was room for a genuinely new law. It is wrong here, because this measurement
// is not the end of a diet — it is the first honest weighing of a prefix that
// has never been weighed, and it is already past what the last diet aimed at. A
// tenth of headroom on top of that would be five more kilobytes nobody chose.
//
// So it is the measurement and nothing else. Anything that grows the shipped
// prefix fails the build and has to be paid for out of what is already here,
// which is what a ratchet is for and what the old figure could not do.
//
// ── AND IT IS THE WIDEST MACHINE'S MEASUREMENT, NOT THIS ONE'S ──────────────
//
// The first spelling of this ratchet was 53,100 — the laptop it was written on
// weighing 53,025 — and it failed on the runner that proved it, at 53,132. The
// prefix is not the same size everywhere: `grep` says a longer sentence about
// itself where ripgrep is absent (134 bytes, and [widestBelt] now weighs that
// spelling wherever it runs), and `load_capability` lists the tool groups THIS
// BUILD has, which is 27 more bytes on a machine that can edit video than on one
// that cannot. A gate whose number depends on who runs it is a gate that passes
// where it is written and fails where it is proved, so the figure is the widest
// machine's and the swap that makes the biggest term machine-independent lives
// in [widestBelt].
//
// The residual is the shelf, and it is owed rather than done: `load_capability`
// would have to name what this build COULD have rather than what it has, which
// is a change to what the model is told and belongs to whoever owns the shelf.
// It is bounded — one short clause per group — and it is why this number was
// measured on the machine that carries every group. It is filed as issue #1010,
// which is also where the `grep` sentence and [widestBelt] itself are deleted:
// removing the variance at its source is the only thing that makes this number
// one number for everybody.
//
// ── WHAT IS OWED, AND WHERE IT HAS TO COME FROM ─────────────────────────────
//
// THE TARGET IS STILL [fixedPrefixTarget] and the shipped prefix is over it by the
// difference between the two constants below. The bill
// is not spread thin — one tool is more than a quarter of the whole tool block:
//
//	stand                  9,607   the standing-item verb's schema
//	propose_task           4,363
//	tasks                  2,272
//	search_conversations   1,613
//	watch                  1,396
//
// `stand` alone is nearly twice the next heaviest and more than the whole overage.
// It is not this file's to cut: what a tool's schema says is its contract with
// the model, and trimming it is a change to what the model is told rather than
// to a byte count. It belongs to whoever owns internal/session's standing belt,
// with the same discipline the 2026-09-10 diet used on `propose_task` — one
// clause per field, no rule stated twice, no em dashes — and it is filed here
// rather than done here because a gate is not the place to decide what a verb
// means.
//
// It is a byte count and not a token count deliberately: bytes are what this
// process can measure exactly, and every tokenizer this build talks to is within
// a small factor of four bytes to the token.
//
// The diet that set it left the prefix at 41,684 bytes — 21,499 of system prompt
// and 20,185 of tool block, down from 37,285 and 32,097. Of what remains, 6,551
// bytes are the seven tools this belt takes VERBATIM from pi (internal/exec's
// bare package): they are the lean baseline the diet was measured against and
// are not codeaf's to trim. The other 35,133 bytes are codeaf's own words, down
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
// a third case with it: codeaf signs the git work it does in somebody's name —
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
// THE MEASUREMENT, AND NOTHING ON TOP OF IT. 53,141 bytes on 2026-09-12: the
// widest page at 19,114 and the fully-wired belt's tool block at 34,027 over
// twenty-three tools, weighed as the widest machine pays for it ([widestBelt]).
// There is no rounding in it and no headroom on it.
const fixedPrefixBudget = fixedPrefixTarget + fixedPrefixWaiver

// fixedPrefixTarget is what the shipped prefix is SUPPOSED to be: the figure the
// last diet aimed at and the one [fixedPrefixBudget] held until the right belt
// was weighed.
//
// THE CAP IS THIS PLUS A WAIVER, AND THE WAIVER ONLY SHRINKS. See
// [prefixWaivers] for what each arm owes today and why a number that is only
// printed is a number nobody ever pays.
const fixedPrefixTarget = 48_000

// prefixWaivers is what each arm is over its target by, dated, in ONE PLACE.
//
// ── THE RULE, WHICH IS `.github/known-red.txt`'S RULE ────────────────────────
//
// A waiver only ever SHRINKS. A lane that takes bytes out lowers the figure in
// the same commit; a lane that needs more takes it out of something that is
// already being said twice. Raising one is a decision with somebody's name on it
// in a diff, which is the entire difference between a debt and a floor.
//
// 2026-09-12, #996. The lean figure shrank from 13,489 to 13,420 in its own
// first commit, which is the discipline working rather than an edit: fixing the
// page's working directory and giving the lean arm its OWN shelf sentence took
// sixty-nine bytes off a number that had been measured on a hybrid. Both arms
// were weighing a conversation nobody has — no
// memory store, no accounts hub, no standing items, no saved programs — so both
// waivers are the first honest measurement of a prefix that had never been
// weighed, and neither is a wave's overspend. The bill is not spread thin:
// `stand` is 9,607 bytes, more than a quarter of the full tool block and, on a
// sixteen-thousand-token window, roughly one token in six of everything that
// person has before they have said anything.
//
// 2026-09-12, the merge with #1015. The prompt diet's read-dedupe wave added
// one sentence to the shared BELT_FACTS — the pointer that answers
// `[already read]` from the conversation rather than fetching the file again —
// and paid exactly 150 bytes on both arms doing it. Both waivers rise by that
// figure here, in this diff, on purpose: the rule says a raise is a decision
// with a name on it, and the alternative was cutting the sentence the merge
// just bought.
//
// 2026-09-16, #1065 round one. `# Tone` and `# Delivery` became `# The answer`,
// `# Answer or change`, `# When corrected` and `# Messages from codeaf`, in the
// wording the issue fixed and with the five rules the old two carried restored
// inside them, and that paid 2,035 bytes on both arms (fixed 53,276 to 55,311,
// lean 45,051 to 47,086). Fifteen of them came out of room both arms already had,
// so both waivers rise by 2,020 and now sit exactly on the measurement, on
// purpose and with this name on it: the issue that
// bought the sections is the one that puts the page's overall length to its
// second round, and cutting other laws in the first would be that round done
// early and unreviewed.
//
// 2026-09-20, the use_skill wave. A worker gained one verb it did not have:
// `use_skill`, which lists the active skill shelf and resolves one name to its
// shelf path (tools_skill.go), and the page gained the one belt-fact bullet that
// says the shelf is reachable (beltfacts.go). It is a NEW CAPABILITY rather than
// a second copy of a law — nothing on this belt already let a worker reach a
// saved procedure — so there was no sentence to take the bytes out of. It paid
// 741 bytes on the fixed arm (55,280 to 56,021) and 755 on the lean arm (47,055
// to 47,810), and both waivers rise by that figure here, in this diff, on
// purpose: the rule says a raise is a decision with a name on it, and the
// alternative was cutting the verb the wave exists to add.
//
// 2026-09-16, #1067 review. The provenance list had to grow because compaction
// also writes user-role tags into the conversation: `[folded …]` and `[context
// compacted]`. It also stopped calling the tool-only `[held]` a user message or
// declaring every unlisted tag the person's, and it does NOT name standing
// news's `[something you set up fired]`: that line carries its own instruction
// under the event, and TestTheSteeringLineReadsAsNewsAndNotAsARequest refuses
// the page's copy as one law paid for twice. Tightening the surrounding
// sentence paid for the truth with room to spare: fixed is 55,280 and lean is
// 47,055, so both waivers fall by 31 and again sit exactly on the measurement.
//
// 2026-09-20, the assisted-by trailer line. The commit-signing belt fact now
// spells the whole two-line trailer block — the co-author's ID-prefixed
// address, and above it the `Assisted-by` line naming the model, filled in at
// the render because the chat is the one surface that knows its model — and
// the widest page pays for it: fixed is 55,442, over its 55,280 by 162, so that
// waiver rises by 162. The lean shape's page never renders the attribution row
// and lean is 47,055 still, exactly on its measurement.
//
// 2026-09-21, the shelf-on-the-page wave. The page gained the three lines that
// say what a skill IS and when opening one beats improvising — nothing on the
// page had ever said that; the tool description said it only where the verb
// was on the belt. It is a NEW LAW rather than a second copy of one, so there
// was nothing to take the bytes out of; the use_skill description, its schema
// lines and the belt-fact row were tightened in the same commit and paid part
// of the bill. Fixed is 56,277, over by 94, and lean is 47,891, over by 81;
// both waivers rise by that figure here, in this diff, on purpose.
//
// 2026-09-23, #1393. The answer section got back the rule #1209's rewording had
// dropped without saying so — "done" is never half-solved work — and the same
// section paid for it by saying three things in fewer words. The page came out
// four bytes lighter on both arms: fixed is 56,273 and lean is 47,887, so both
// waivers fall by 4 and again sit exactly on the measurement.
//
// 2026-09-23, signing always on. The owner retired the `attribution` row:
// codeaf signs every commit it writes, and the only choice left is whether the
// `Assisted-by` line names the model. The law's commit sentence now spells both
// trailer lines itself, so the page's second sentence spelling the block again
// is gone: fixed is 56,146, and that waiver FALLS by 127. The lean arm RISES by
// 927, and that is not new wording: the shipped shape had the row unset, so this
// arm weighed a page with signing off, while every real conversation had it on
// by default and paid those bytes all along. The arm now weighs the page people
// were already reading, 48,814, and the raise carries this name so it can be
// questioned.
const (
	fixedPrefixWaiver = 8_146
	leanPrefixWaiver  = 17_314
)

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
// THE HELD-RANGES WAVE PAID FOR THE ROUND-TRIP PRICE IT TEACHES (2026-09-12).
// The read tool's description says a call costs one round trip and a range
// already in the conversation answers as a pointer (bare/tools.go's
// [readDescription], the ledger in heldreads.go). The sentence was priced
// into the lean arm's headroom — 87 bytes came in, 9 came back out of the
// description's redundant "the rest", and the lean prefix sits 5 under its
// budget.
// THEREAFTER IT ONLY EVER RATCHETS DOWN, in the ledger discipline the full
// budget above is kept under: a lane that takes bytes out lowers it in the same
// commit, and nothing ever raises it again.
//
// ── AND THEN IT TURNED OUT TO BE WEIGHING A CONVERSATION NOBODY HAS ─────────
//
// Every figure above is real and every one of them was measured against
// [leanShapedAgent], which built a conversation with NO memory store, NO
// accounts hub, NO standing items and NO saved programs — sixteen tools. A
// person on a small window who has finished setting codeaf up carries `stand`
// (9,607 bytes by itself), `remember` and `search_conversations`, none of which
// [Config.leanCapabilityGroups] shelves, and their prefix is 44,989 bytes.
//
// So the arm that exists to protect the person with the LEAST room to spare was
// out by 13,489 bytes, which is more than the whole budget it was enforcing.
// That is #576's shape and it is the same hole the full arm above had, found in
// the same review: a gate pointed at something nobody runs passes without having
// tested anything, every day, in both directions.
//
// THE FIGURE IS THE MEASUREMENT NOW, with no headroom — the argument for slack
// was written when this number came off a diet that had just been paid for, and
// a number that has never been honestly weighed has not earned any. What the
// slack bought (a shared page that can still move by a couple of hundred bytes)
// is now bought by the full arm's 53,141, which both arms read the page through.
//
// WHAT IS OWED IS THE SAME BILL THE FULL ARM OWES, and it falls harder here:
// `stand` alone is 9,607 bytes on a sixteen-thousand-token window, which is
// roughly one token in six of everything that person has, spent before they have
// said anything. [leanPrefixTarget] is what this has to come back to and the
// test prints the shortfall on every green run.
const leanPrefixBudget = leanPrefixTarget + leanPrefixWaiver

// leanPrefixTarget is what the lean prefix is SUPPOSED to be: the figure
// [leanPrefixBudget] held until the right belt was weighed. The cap is this plus
// [leanPrefixWaiver], which only ever shrinks.
const leanPrefixTarget = 31_500

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

// atAFixedPlace is a config whose WORKING DIRECTORY is a constant.
//
// THE PAGE INTERPOLATES WHERE YOU ARE (prompt.go's `- Working directory: %s`),
// and a fixture's workspace is a temp directory named after the test and the
// platform's temp root: `/tmp/TestX123/001` on Linux and
// `/var/folders/9k/…/T/TestX456/001` on macOS. So this arm's number moved by
// forty-five bytes between two machines and failed on the second — the same
// class of defect as `grep`'s two sentences and the capability shelf, found the
// same way, in the gate whose entire purpose is to be one number for everyone.
//
// The page also carries `- Workstation: %s/%s` from runtime.GOOS/GOARCH, which
// is a byte of difference between darwin and linux and is left alone: it is a
// FACT ABOUT THE MACHINE the model is told on purpose, it cannot be made
// constant without lying to the fixture, and a byte is inside nobody's decision.
// The working directory is not that — it is a fixture artefact, and it is tens
// of bytes.
// The full arm does not need it: [widestPage] weighs `systemPrompt` with its
// belt-fact tokens substituted and never renders the machine lines at all, so
// that number is already the same everywhere — and it is an UNDER-count of the
// shipped page by those few lines, which is recorded here rather than fixed
// because moving it would move a ratchet for a reason that is not growth.
func pageAsWeighed(config Config, now time.Time) int {
	config.Workspace = "/w"
	page := renderSystemAt(config, now)
	// AND THE WORKSTATION LINE IS NORMALISED. `- Workstation: %s/%s` is
	// runtime.GOOS/GOARCH, which is `darwin/arm64` on one machine and
	// `linux/arm64` on another — ONE BYTE, and a ratchet with no headroom fails
	// on one byte. It is a fact about the machine the model is told on purpose
	// and cannot be made constant in the product, so it is made constant HERE,
	// at the only place that needs it to be.
	return len(strings.Replace(page, runtime.GOOS+"/"+runtime.GOARCH, "os/arch", 1))
}

// widestBelt is the shipped belt weighed as the WIDEST MACHINE pays for it.
//
// A TOOL WHOSE DESCRIPTION DEPENDS ON WHAT IS INSTALLED still sends those bytes
// on every request, and this gate runs on machines of both kinds: `grep` says
// one sentence where ripgrep is present and a longer one where it is not
// (bare's [bare.WidestGrepDescription]), and the difference measured 107 bytes
// of prefix — enough that the same commit passed on the laptop it was written on
// and failed on the runner that proved it. So the number below is one number
// everywhere, and it is the larger one.
func widestBelt(t *testing.T, agent *Agent) []ai.ToolDefinition {
	t.Helper()
	definitions := append([]ai.ToolDefinition(nil), agent.beltDefinitions()...)
	if len(definitions) == 0 {
		t.Fatal("the belt is empty, so this test would pass on nothing")
	}
	widest := map[string]string{
		// `grep` says a longer sentence about itself where ripgrep is absent.
		"grep": bare.WidestGrepDescription(agent.resultCaps()),
		// `load_capability` NAMES THE GROUPS THIS BUILD HAS, and a group whose
		// every member was gated off is not named at all — `edit_video` is built
		// only where ffmpeg is on PATH, so a machine that can edit video pays for
		// one more clause than one that cannot (tools_capabilities.go).
		//
		// IT IS THIS SHAPE'S OWN SHELF AND NOT THE FULL ONE. A lean belt pre-arms
		// `questions` instead of shelving it (promptprofile.go), so pasting the
		// full shape's sentence into the lean arm would have made that number a
		// hybrid of two belts — a figure neither of them pays.
		"load_capability": widestLoadCapability(agent.config),
	}
	swapped := 0
	for index := range definitions {
		longest, varies := widest[definitions[index].Function.Name]
		if !varies {
			continue
		}
		swapped++
		// ASSIGNED, NOT COMPARED. A swap that only fired when it made the number
		// bigger would be a swap that silently stopped firing the day the other
		// spelling grew — which is the same defeat, one level up. `widest` is
		// widest by construction and this takes it at its word.
		definitions[index].Function.Description = longest
	}
	// AND THE SWAP CANNOT SILENTLY STOP APPLYING. A tool that leaves the belt, or
	// is renamed, would take its machine-variance off this number without anybody
	// noticing the budget had quietly got easier.
	if swapped != len(widest) {
		t.Fatalf("%d of the %d tools that vary by machine are on the belt", swapped, len(widest))
	}
	return definitions
}

// widestLoadCapability is the loader's sentence for ONE SHAPE at the widest
// machine: every group that shape shelves rather than carries, each with every
// member the table declares — which is what it says where all the programs its
// tools shell out to are installed.
//
// It is built through the product's own [loadCapabilityDescription], so the
// three sentences after the group list are stated once and cannot drift out of
// this number.
//
// THE ONLY THING TAKEN OUT IS WHAT THIS SHAPE PRE-ARMS, because that is the one
// difference between the two belts' shelves that is a property of the SHAPE
// rather than of the machine. A group gated off by config would be
// over-counted here; that is the safe direction for a budget and it is the same
// answer everywhere, which is the whole point.
func widestLoadCapability(config Config) string {
	prearmed := map[string]bool{}
	for _, group := range config.prearmedGroups() {
		prearmed[group] = true
	}
	order := make([]string, 0, len(capabilityGroups))
	members := make(map[string][]string, len(capabilityGroups))
	for _, group := range capabilityGroups {
		if prearmed[group.name] {
			continue
		}
		order = append(order, group.name)
		members[group.name] = group.members
	}
	return loadCapabilityDescription(func(group string) []string { return members[group] }, order)
}

// TestTheFixedPrefixStaysUnderItsBudget weighs what every request carries before
// anybody has said anything.
func TestTheFixedPrefixStaysUnderItsBudget(t *testing.T) {
	reportPrefix(t, prefixArm{
		what:        "the fixed prefix",
		definitions: widestBelt(t, shippedShapeAgent(t)),
		page:        len(widestPage()),
		budget:      fixedPrefixBudget,
		target:      fixedPrefixTarget,
		owed:        "see this file's head for the bill and who owes it",
		howToPay: "every byte here is sent again on every request of every turn, so take the " +
			"addition back out of something that already says it rather than raising the budget",
	})
}

// prefixArm is one weighing: what to call it, what it carries, and the two
// figures it is judged against.
type prefixArm struct {
	what        string
	definitions []ai.ToolDefinition
	page        int
	budget      int
	target      int
	owed        string
	howToPay    string
}

// reportPrefix weighs one arm, says what is still owed on a green run, and names
// the bill biggest-first when the ratchet is broken.
//
// IT IS ONE FUNCTION BECAUSE THE TWO ARMS ARE ONE TEST with two sets of
// constants. They were forty lines each, written twice, and the copies had
// already drifted: only one of them printed its shortfall against the target,
// so the arm carrying the larger debt was the arm that never mentioned it.
func reportPrefix(t *testing.T, arm prefixArm) {
	t.Helper()
	block, err := json.Marshal(arm.definitions)
	if err != nil {
		t.Fatal(err)
	}
	tools := len(block)
	total := tools + arm.page
	t.Logf("%s is %d bytes (~%d tokens): prompt %d + tools %d over %d tools",
		arm.what, total, total/4, arm.page, tools, len(arm.definitions))
	// AND THE DEBT IS ENFORCED, NOT LOGGED. It was a t.Logf, which is invisible:
	// nothing in the Makefile passes -v, so the one line saying this prefix is
	// thousands of bytes over what it is supposed to be was printed where no
	// human and no CI log would ever show it. A debt nobody is reminded of is a
	// debt that has quietly become the floor.
	//
	// So the TARGET is the cap, and the overage is a WAIVER: one dated figure per
	// arm, in one place, which may only ever shrink ([prefixWaivers]). That is
	// the known-red discipline — `.github/known-red.txt` and its ratchet — applied
	// to bytes. A wave that grows the prefix has to raise a waiver, in a diff, with
	// its name on it; nothing can drift.
	if total <= arm.budget {
		return
	}
	// THE FAILURE NAMES THE BILL, BIGGEST FIRST. A lane reading this has just
	// grown one of these lines and has no other way to see which.
	type weighed struct {
		name  string
		bytes int
	}
	heaviest := make([]weighed, 0, len(arm.definitions))
	for _, definition := range arm.definitions {
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		heaviest = append(heaviest, weighed{definition.Function.Name, len(encoded)})
	}
	sort.Slice(heaviest, func(i, j int) bool { return heaviest[i].bytes > heaviest[j].bytes })
	report := fmt.Sprintf("%s is %d bytes (~%d tokens), over its %d budget by %d\n"+
		"  the page            %6d\n"+
		"  the tool block      %6d over %d tools\n"+
		"the heaviest tools:\n",
		arm.what, total, total/4, arm.budget, total-arm.budget, arm.page, tools, len(arm.definitions))
	for index, tool := range heaviest {
		if index == 8 {
			break
		}
		report += fmt.Sprintf("  %-20s%6d\n", tool.name, tool.bytes)
	}
	t.Fatalf("%sthe cap is the %d target plus a waiver of %d, and a waiver only ever shrinks "+
		"([prefixWaivers]). %s", report, arm.target, arm.budget-arm.target, arm.howToPay)
}

// ── the lean arm ────────────────────────────────────────────────────────────

// leanShapedAgent is the SHIPPING CONVERSATION on a small window: the shape
// [shippedShapeAgent] builds, with the one thing that differs — the window a
// local open-weight model actually has — changed and nothing else. The
// difference between the two numbers below is therefore the profile, and only
// the profile.
//
// ── IT WEIGHED A THINNER AGENT THAN ANYBODY RUNS, AND THIS IS THAT REPAIR ───
//
// It used to build its own config: no memory store, no accounts hub, no standing
// items, no saved programs. That is #576's shape again and the same one the full
// arm was repaired for in this PR — a gate pointed at something nobody runs
// passes without having tested anything. A real lean conversation carries
// `stand` (9.6 KB on its own), `remember` and `search_conversations`, and
// [Config.leanCapabilityGroups] shelves none of the three, so every one of those
// bytes is bought on every request of every turn by exactly the person whose
// window has the least room for them.
func leanShapedAgent(t *testing.T) *Agent {
	t.Helper()
	shape := beltShapeNamed(t, shippedBeltShape)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		shape.build(t, config)
		config.ContextWindow = leanWindow
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
	// AND `ask` IS IN THE BLOCK, because it is pre-armed rather than shelved
	// ([Config.prearmedGroups]). A lean belt that had to load its way to a
	// question would be measured lighter here and be unable to ask one, which is
	// the one regression this number could hide.
	if !agent.hasTool("ask") {
		t.Fatal("`ask` is not carried on a lean belt: a one-call-per-message model cannot load-then-ask")
	}
	reportPrefix(t, prefixArm{
		what:        "the lean prefix",
		definitions: widestBelt(t, agent),
		page:        pageAsWeighed(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)),
		budget:      leanPrefixBudget,
		target:      leanPrefixTarget,
		owed:        "see [leanPrefixBudget] for the bill and who owes it",
		howToPay: "this arm's number only ever comes down: shelve the verb, cut the law that is " +
			"stated twice, or leave it — never raise the budget",
	})
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
