package session

import "strings"

// THE PROMPT NAMES EXACTLY THE TOOLS THE CALL CARRIES.
//
// prompts/system.md is one embedded text read by every agent this package
// builds — the conversation, a worker that may hand parts out, a worker on the
// floor of the tree, a standing check — and the belt those agents get is not one
// belt. Five families come off it by their own gates (tools.go): the settings
// pair and `watch` inside a task, `search_conversations` where no store was
// opened, `tasks` and `propose_task` on the floor of the tree, `use_service`
// where there is no account hub. A sentence in the page naming one of those is
// a sentence that is false for whoever does not have it.
//
// It has cost a real turn. A worker on the floor of its tree on 2026-08-23 did
// exactly what the page told it — "call `tasks` with their words BEFORE
// answering" — and was answered `Unknown tool: tasks`: a step spent, and a
// worker that now has to guess whether the rest of what it was told is true.
// CLAUDE.md records the same defect for `note`/`forget`, and prompt.go already
// honours the law for two whole pages, the fan-out page and the divide page,
// on the same predicates their tools are built from.
//
// So the tool-naming facts of the session-facts section live HERE and are
// composed at render time, each from THE PREDICATE THAT PUTS ITS TOOL ON THE
// BELT. Where a tool is absent the fragment says what to do instead rather than
// saying nothing, in the voice the page's own `remember` and `stand` sentences
// already use ("Without `remember`, say plainly that memory is off"): a worker
// told the truth spends its turn on the work, and a worker told nothing spends
// it guessing.
//
// prompt_belt_test.go is the both-ways proof, over every agent shape this
// package builds.
//
// THERE ARE THREE PLACES THE PAGE HANDS OVER, and they are three because the
// sentences are three runs of prose the page needs kept where they are: the
// session facts, the list of ways work leaves this turn, and the two
// paragraphs that say what a saved recipe and a saved program ARE. A page
// assembled anywhere else would read as an appendix.
const (
	beltFactsToken    = "BELT_FACTS"
	handoffFactsToken = "HANDOFF_FACTS"
	programFactsToken = "PROGRAM_FACTS"
)

// ── the predicates ──────────────────────────────────────────────────────────
//
// EVERY ONE IS ANSWERABLE FROM THE CONFIG ALONE, as [Config.mayFanOut] and
// [Config.mayDivide] already are, because the render step has nothing else: the
// prompt is built before the agent exists (agent.go's newAgent). The belt
// functions read these SAME predicates, which is what makes it impossible for
// the two to disagree about a tool.

// hasStore says whether this agent was opened with the store behind it. The
// store is memory AND the FTS index over every message ever posted, so it is
// the one fact under both `remember` and `search_conversations` (memory.go,
// tools_conversations.go). A task node is handed none (task_run.go sets no
// Config.Memory), and neither is a conversation with memory off.
//
// [Agent.remembers] is the same fact asked of a live agent — it reads the brain
// newAgent builds from exactly this field — and the belt keeps asking it there
// because every memory road dereferences that brain. prompt_belt_test.go pins
// the two to the same answer for every shape.
func (c Config) hasStore() bool { return c.Memory != nil }

// maySeeSettings says whether the settings pair belongs on this belt
// (tools_settings.go). It comes off inside a task for the sharpest reason on
// the belt: a node runs in a worktree with nobody watching, so a permanent
// change to the person's machine made there is one they never saw made.
func (c Config) maySeeSettings() bool { return !c.InTask }

// mayWatch says whether `watch` belongs on this belt (tools.go). It comes off
// inside a task because its whole delivery mechanism is a note arriving in a
// conversation, and a node has none.
func (c Config) mayWatch() bool { return !c.InTask }

// mayProposeTask says whether the task pair — `propose_task` and the `tasks`
// window onto what it started — belongs on this belt: always in a conversation,
// and in a node only when it was handed the conversation's graph and is not
// standing on the floor of the tree ([Agent.mayProposeTask] is this asked of a
// live agent, and task.go states the fan-out law it comes from).
func (c Config) mayProposeTask() bool { return !c.InTask || c.mayFanOut() }

// hasConnect says whether the accounts pair belongs on this belt
// (tools_connect.go). It is written as the hub CONSTRUCTOR'S OWN ANSWER rather
// than as a second reading of the two fields, so that a door that starts
// handing over a hub some other way cannot make the prompt and the belt
// disagree: [newConnectHub] is what agent.go calls at construction, and
// a.connect is never reassigned afterwards, so this is settled for the life of
// the agent at the moment the prompt is rendered.
func (c Config) hasConnect() bool { return newConnectHub(c) != nil }

// mayFork says whether `fork` belongs on this belt (fork.go). It is off in a
// hand and nowhere else, which is the whole of the depth-one law: a chat turn
// and a task worker are both minds mid-work with a context worth copying, and a
// hand is not, because the fork is one deep.
func (c Config) mayFork() bool { return !c.inHand }

// mayDesignHarness says whether the two harness hands belong on this belt
// (tools_harness.go): a store to write the page into, a runner to run what was
// written, and somebody watching who can answer the card. A design nobody can
// approve is two model calls spent on a page that will be dropped.
func (c Config) mayDesignHarness() bool {
	return c.HarnessStore != nil && c.RunHarness != nil && c.AskConsent
}

// mayProposeSubharness says whether the saved-programs pair belongs on this
// belt (tools_subharness.go): somebody watching, a surface holding the harness
// lane the card goes out on, and at least one program on the registry.
//
// THE THIRD QUESTION IS ASKED OF THE REGISTRY ITSELF, at the moment it is
// asked, by the same reader the belt counts ([Config.subharnessRows]) — so
// this is a live fact and a config fact at once, and the page and the belt read
// it within microseconds of each other in newAgent. An empty registry is no
// verb and no sentence: a model handed a propose verb over an empty list would
// offer programs it invented.
func (c Config) mayProposeSubharness() bool {
	return c.AskConsent && c.HarnessCards && len(c.subharnessRows()) > 0
}

// ── the facts ───────────────────────────────────────────────────────────────

// beltFact is one run of session-facts bullets that names a tool, together with
// the belt's own predicate for that tool and what to say when it is absent.
type beltFact struct {
	// tools is every tool name the present-case sentences spell. The test reads
	// this: a tool that can be present on one belt and absent on another must
	// appear here, or the page has a sentence nobody is holding to the law.
	tools []string
	// holds is the belt's predicate, verbatim — not a second reading of it.
	holds func(Config) bool
	// present is the wording the page carried for everybody, unchanged.
	present string
	// absent is what a worker without the tool is told instead: what it cannot
	// do from here, and what to do in its place. An empty string renders
	// nothing, which is right where the absence needs no instruction.
	absent string
}

// beltFacts is the whole of it, in the order the section reads.
var beltFacts = []beltFact{{
	tools: []string{"propose_task", "tasks"},
	holds: Config.mayProposeTask,
	present: "- ON `propose_task` NEVER NAME THE METHOD: a task is always given its own copy, so \"work in this repo directly\", a branch or a checkout is never yours to specify.\n" +
		"- Earlier work referred to but not pointed at (\"the reconciler task\", \"same as before\"): call `tasks` with their words BEFORE answering.\n" +
		"- A `tasks` row is a citation, not the work: its transcript URI is the JSONL journal of all that node said, called and got back, and `read` takes a row's URIs exactly as printed, `file://` and all. `grep` a journal or `read` it with `offset`/`limit`, never expand an outcome line into work you did not read, and say so when a row prints no transcript. A `[Task reference: ...]` block already carries those URIs.\n" +
		"- `tasks` with `id` shows the call in flight, the steps, the spend and the last of what a running task said and did: pull it to SEE inside a run. Steer with `id` and `say`; forward the person's own correction with `forward`.\n" +
		// THE CONTINUE SENTENCE IS NOT REPEATED HERE. `tasks` own description
		// carries it word for word ([tasksDescription]), and the prefix is a
		// budget: what pays for the handoff law in prompts/system.md is this
		// second copy of a law the model already holds whenever it holds the verb.
		"- `needs your look`: not done or failed, branch kept; settle with `tasks` id and `resolve`. If the tool says there is no graph, say so and point at the row's branch or working copy.",
	absent: "- THE RECORD OF EARLIER WORK IS NOT REACHABLE FROM HERE and none of this work goes to anybody else: answer from the brief and from what is in front of you, and say plainly when something earlier is referred to that you cannot see. A `[Task reference: ...]` block you were handed carries transcript URIs, and `read` takes one exactly as printed, `file://` and all: `grep` a journal or `read` it with `offset`/`limit`, and never expand an outcome line into work you did not read.",
}, {
	tools:   []string{"search_conversations"},
	holds:   Config.hasStore,
	present: "- For what was said, call `search_conversations` ONCE with their own words.",
	absent:  "- What was said in earlier conversations cannot be looked up from here, so answer out of what is in this window rather than reconstructing it.",
}, {
	tools:   []string{"watch"},
	holds:   Config.mayWatch,
	present: "- Start ONE `watch` to follow something that changes.",
	absent:  "- There is no `watch` here: a foreground `bash` call is how you wait for something to finish.",
}, {
	tools: []string{"use_service"},
	holds: Config.hasConnect,
	present: "- A connected account is the person's own and you act in it on their behalf, so call `use_service` when the work needs one; nothing is connected without them saying yes, and its tools arrive on your NEXT turn. Most arrive as one `<id>_request` tool naming the address its paths hang off, with the service's published documentation as the schema: `get` is free to try, `post`, `put`, `patch` and `delete` are asked about first. A few serve named tools instead, and an account with more tools than a conversation holds answers with its whole list, so call again with `tools` naming the few this needs.\n" +
		"- Sending a message and putting something on a calendar reach other people in the person's name and cannot be undone, so they are asked first: write what they would have written, with real recipients and times, and never send twice because the first was not answered.\n" +
		"- The person decides what each account may be used for, one sentence at a time: what they turned off is absent rather than failing, and a tool saying so is their standing answer, so do the rest without it and say what you could not do.",
	// NOTHING IS SAID WHERE THERE IS NO HUB, and the three lines above travel
	// together for that reason: two of them are about how an account behaves
	// once it is reached, which is not a limit anybody needs told. An agent
	// with no accounts seam has no account to act in, no consent to relay and
	// nothing to do instead.
	absent: "",
}, {
	tools:   []string{"settings", "change_setting"},
	holds:   Config.maySeeSettings,
	present: "- A preference changed goes through `settings` for the row and `change_setting` for the write, never `edit` or `write` on a config file. Relay a refusal as written and point at `/settings`.",
	absent:  "- YOU CANNOT CHANGE A PREFERENCE FROM INSIDE A TASK: say so and point at `/settings`, and never `edit` or `write` a config file instead.",
}}

// handoffFacts is `## Work or words`: the ways work leaves this turn, one row
// per verb, so that the list a model reads is the list of verbs it has.
//
// THE LEAD-IN RIDES `propose_task` AND NOT THE LIST. "Launch first, then
// answer" is the instruction of an agent that has somewhere to launch at; on
// the floor of the tree there is nowhere, and the truth there is the opposite
// instruction — the work is yours, so open it. Everything else in the section
// names no verb and stays in the page for everybody: what a hand-off costs,
// that the question is asked again while you work, and that what you learned
// goes with it.
var handoffFacts = []beltFact{{
	tools: []string{"propose_task"},
	holds: Config.mayProposeTask,
	present: "WORK — research across sources, changes across files, anything with several\n" +
		"independent parts, anything they would otherwise watch a spinner for — is NOT\n" +
		"yours to do inline. Launch first, then answer:\n" +
		"  - WIDE WORK — a sweep across many files, research across many sources, the\n" +
		"    same change over many independent items: ONE `propose_task` with `wide`\n" +
		"    set. That is the default road: the worker opens the material and hands the\n" +
		"    real parts out under itself, each a worker in a copy of its own,\n" +
		"    folding their reports into one deliverable. Do not decompose it\n" +
		"    here, since the parts are only visible from inside, and never split related\n" +
		"    work, which shards the context it shares.\n" +
		"  - One self-contained linear job: `propose_task`, without `wide`.",
	absent: "WORK IS YOURS TO DO HERE. There is nowhere to launch it at from where you\n" +
		"stand, so a sweep across many files, research across many sources or the same\n" +
		"change over many items is work you open and carry yourself, in the order that\n" +
		"finishes it.",
}, {
	tools: []string{"fork"},
	holds: Config.mayFork,
	present: "  - SEVERAL PARTS OF THE REPLY YOU ARE ALREADY WRITING, on files that do not\n" +
		"    touch: `fork`, mid-work only, once you can name the slices.",
	// A hand is told nothing, because the fork is one deep and there is no
	// second-best road to point it at (fork.go's forkTools).
	absent: "",
}, {
	tools:   []string{"build_harness"},
	holds:   Config.mayDesignHarness,
	present: "  - A shape of work that will recur: `build_harness`.",
	absent:  "",
}, {
	tools:   []string{"propose_subharness"},
	holds:   Config.mayProposeSubharness,
	present: "  - A shape of work a saved program ALREADY does: `propose_subharness`.",
	absent:  "",
}}

// programFacts is what a saved recipe and a saved program ARE. Each paragraph
// exists to make its own two verbs usable, so it travels with them: a build
// that cannot design one has no reason to carry the definition, and a worker
// paid for both paragraphs on every request of every turn.
var programFacts = []beltFact{{
	tools: []string{"list_harnesses", "build_harness"},
	holds: Config.mayDesignHarness,
	present: "A **sub-harness** is a reusable recipe: a named, versioned procedure saved\n" +
		"here, and offered by the turn when somebody's words match. `list_harnesses`\n" +
		"lists them, `build_harness` designs one.",
	absent: "",
}, {
	tools: []string{"list_subharnesses", "propose_subharness"},
	holds: Config.mayProposeSubharness,
	present: "A **subharness** is a saved PROGRAM rather than a recipe: typed input, a typed\n" +
		"answer, only the tools it declared. `list_subharnesses` lists them and\n" +
		"`propose_subharness` offers one with your line about why it matched. NOTHING\n" +
		"RUNS BECAUSE YOU PROPOSED IT: the person answers that card, so propose only when\n" +
		"the work IS what a program is for.",
	absent: "",
}}

// allBeltFacts is every row, for the tests that hold the whole table to the
// law rather than one section of it.
func allBeltFacts() []beltFact {
	all := make([]beltFact, 0, len(beltFacts)+len(handoffFacts)+len(programFacts))
	all = append(all, beltFacts...)
	all = append(all, handoffFacts...)
	return append(all, programFacts...)
}

// renderBeltFacts composes the section for one shape.
// The separator is the page's own: bullets sit on consecutive lines, whole
// paragraphs are parted by a blank one.
func renderBeltFacts(config Config, facts []beltFact, join string) string {
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		text := fact.absent
		if fact.holds(config) {
			text = fact.present
		}
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, join)
}

// promptWithBeltFacts is the embedded page as THIS agent reads it. It is the
// whole of the fixed prefix that depends on the shape, which is why
// prefixbudget_test.go weighs this and not [systemPrompt].
func promptWithBeltFacts(config Config) string {
	page := strings.Replace(systemPrompt, beltFactsToken, renderBeltFacts(config, beltFacts, "\n"), 1)
	page = strings.Replace(page, handoffFactsToken, renderBeltFacts(config, handoffFacts, "\n"), 1)
	page = strings.Replace(page, programFactsToken, renderBeltFacts(config, programFacts, "\n\n"), 1)
	// AND THE HOLE A WHOLE SECTION LEFT IS CLOSED. A table that renders nothing
	// — the saved-programs paragraphs on a worker, which has neither verb —
	// leaves its blank line behind, and the page would open a paragraph gap of
	// three newlines where a reader expects one. prompts/system.md contains no
	// triple newline of its own, so this is unambiguous and is done once here
	// rather than by giving every token a hand-tuned surrounding.
	for strings.Contains(page, "\n\n\n") {
		page = strings.ReplaceAll(page, "\n\n\n", "\n\n")
	}
	return page
}

// promptNamesBeyondTheBelt is the DEBT LEDGER, and it exists so that the
// remainder is visible rather than merely absent.
//
// Every name here is a tool the embedded page still spells for everybody while
// some belt does not carry it. They fall in two classes, and only the first is
// honest:
//
//   - WRITTEN FOR BOTH CASES ALREADY. The page names the tool and, in the same
//     breath, says what to do without it — "Without `remember`, say plainly that
//     memory is off", "Without `stand` this build cannot watch anything once the
//     window closes". prompt_belt_test.go holds these to that: where the belt
//     lacks the tool, the sentence stating the absence must be in the rendered
//     prompt.
//
// THE LEDGER IS FOR WHAT PREDATES THE SEAM AND NOTHING ELSE. A tool whose
// sentence could be composed is composed; an entry added here for one that
// could would be a way of not doing the work, and the reverse test is written
// so that the next conditional tool cannot take that road quietly.
//
// The value is the marker the test looks for in the absent case.
var promptNamesBeyondTheBelt = map[string]string{
	"remember": "Without `remember`, say plainly that memory is off",
	"stand":    "Without `stand` this build cannot watch anything",
}
