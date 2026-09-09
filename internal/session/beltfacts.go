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
	// AND THE FOURTH IS A WHOLE SECTION AND NOT A RUN OF SENTENCES. What keeps
	// working after the window closes is `stand`'s section from its heading down
	// ([standingFacts]), so the token stands where the heading stood.
	standingFactsToken = "STANDING_FACTS"
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

// shelvesCapabilities says whether this shape holds its rarely-reached tools
// back on a named shelf, one `load_capability` call away, rather than carrying
// them (tools_capabilities.go). It is the ONE reading of that question:
// [Agent.shelveDeferred] is built from this same predicate, so a sentence
// telling the model to load cannot be rendered for a shape whose belt carries
// the tools directly — which a worker or a task node does, because the saving
// is only worth having on a prefix re-sent every turn.
func (c Config) shelvesCapabilities() bool { return !c.InTask }

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

// mayStand says whether `stand` belongs on this belt, and it is
// [Config.standingStore] — the ONE reading of that availability, which
// [Agent.standingTools] builds the tool from (tools_standing.go). It is not a
// second reading of the two fields: the belt and the page must not be able to
// disagree about whether anything can be scheduled from here.
//
// It is the sharpest absence on the belt — a model told it can leave something
// behind will plan a whole reply around one — and it is the predicate the page's
// standing section is composed from.
func (c Config) mayStand() bool { return c.standingStore() != nil }

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
	// present is the wording the page carried for everybody, unchanged. It is
	// the DIRECT case: the tools are in the tool list already.
	present string
	// shelved is the same sentence for a shape that holds these tools back
	// (tools_capabilities.go): it names the group and the verb that fetches it,
	// because a model told to reach for a verb that is one call away and not
	// told about the call is a model that will be answered `Unknown tool`. An
	// empty string means this fact's tools are never shelved.
	shelved string
	// absent is what a worker without the tool is told instead: what it cannot
	// do from here, and what to do in its place. An empty string renders
	// nothing, which is right where the absence needs no instruction.
	absent string
}

// beltFacts is the whole of it, in the order the section reads.
var beltFacts = []beltFact{{
	// THE CLOCK, whose second sentence is the one place the session facts named a
	// conditional verb for everybody. The first sentence is true of every shape —
	// the `Project` footer is rendered for all of them — and the second was
	// telling a worker with no `stand` to reach for `when.in`, which is the
	// defect this file was written for, one section further down the same page.
	tools:   []string{"stand"},
	holds:   Config.mayStand,
	present: "- YOU KNOW WHAT TIME IT IS: `Project`'s `Now` line gives local time to the minute, offset, zone by name and weekday, so NEVER run `date` for it. It does not tick inside a turn, so when a MINUTE matters use `stand`'s `when.in` or the `now:` line a `stand` result ends with.",
	// AND THE ABSENT CASE MUST NOT CONTRADICT ITSELF. It cannot say NEVER run
	// `date` and in the same breath send the model to the clock, because with no
	// `stand` the shell IS the only clock: the rule stays what it is for the
	// four facts the footer already gives, and the one case it does not cover is
	// named as the exception.
	absent: "- YOU KNOW WHAT TIME IT IS: `Project`'s `Now` line gives local time to the minute, offset, zone by name and weekday, so never shell out for any of those four. It does not tick inside a turn, so a moment that must be exact to the MINUTE is the one case for a single `date` call.",
}, {
	tools: []string{"propose_task", "tasks"},
	holds: Config.mayProposeTask,
	present: "- ON `propose_task` NEVER NAME THE METHOD: a task is always given its own copy, so \"work in this repo directly\", a branch or a checkout is never yours to specify.\n" +
		"- Earlier work referred to but not pointed at (\"the reconciler task\", \"same as before\"): call `tasks` with their words BEFORE answering.\n" +
		"- A `tasks` row is a citation, not the work: its transcript URI is the JSONL journal of all that node said, called and got back, and `read` takes a row's URIs exactly as printed, `file://` and all. `grep` a journal or `read` it with `offset`/`limit`, never expand an outcome line into work you did not read, and say so when a row prints no transcript. A `[Task reference: ...]` block already carries those URIs.\n" +
		// THE `id` SENTENCE IS NOT REPEATED HERE EITHER, for the reason the one
		// below states: [tasksDescription] already says an id reads, steers,
		// continues or settles one piece of work and is how you look inside
		// running work. What paid for `propose_task`'s `checks` field is this
		// second copy of a law the model holds whenever it holds the verb.
		"- To send the person's current correction to a running task, use `tasks` with `id` and `forward: true`. `say` is your own coordination.\n" +
		// THE CONTINUE SENTENCE IS NOT REPEATED HERE. `tasks` own description
		// carries it word for word ([tasksDescription]), and the prefix is a
		// budget: what pays for the handoff law in prompts/system.md is this
		// second copy of a law the model already holds whenever it holds the verb.
		// THE FOUR WORDS ARE THE SURFACE'S OWN (task_status.go's tier words), so a
		// model relaying a landing says what the person is already looking at. The
		// verbs are the three the tool's schema takes ([TaskResolutions]) with the
		// person's words for two of them beside, because "not right" is what the
		// card says and `refute` is what the call takes.
		"- A LANDED TASK SAYS `done`, `stopped`, `incomplete` (with the reason) or `your call`. On `your call` your verbs are `tasks` id `resolve` with accept, refute (say it is not right) or reaudit (have it checked again), plus `forward` to steer it; a CONFLICT is never yours to accept — say what clashes and leave the merge to them. If the tool says there is no graph, say so and point at the row's branch or working copy.",
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
	present: "- A connected account is the person's own and you act in it on their behalf, so call `use_service` when the work needs one; nothing is connected without them saying yes, and its tools arrive in your tool list on your next request, still this turn. Most arrive as one `<id>_request` tool naming the address its paths hang off, with the service's published documentation as the schema: `get` is free to try, `post`, `put`, `patch` and `delete` are asked about first. A few serve named tools instead, and an account with more tools than a conversation holds answers with its whole list, so call again with `tools` naming the few this needs.\n" +
		"- Sending a message and putting something on a calendar reach other people in the person's name and cannot be undone, so they are asked first: write what they would have written, with real recipients and times, and never send twice because the first was not answered.\n" +
		"- The person decides what each account may be used for, one sentence at a time: what they turned off is absent rather than failing, and a tool saying so is their standing answer, so do the rest without it and say what you could not do.",
	// NOTHING IS SAID WHERE THERE IS NO HUB, and the three lines above travel
	// together for that reason: two of them are about how an account behaves
	// once it is reached, which is not a limit anybody needs told. An agent
	// with no accounts seam has no account to act in, no consent to relay and
	// nothing to do instead.
	absent: "",
}, {
	// AND `load_capability` IS ONE OF THIS SENTENCE'S TOOLS, in the shelved
	// wording only. It is true on exactly this predicate: [Agent.settingsTools]
	// is gated on [Config.maySeeSettings] and nothing else, so a shape holding
	// this fact has the settings group, and a non-empty shelf always carries the
	// loading verb (tools_capabilities.go).
	tools:   []string{"settings", "change_setting", loadCapabilityToolName},
	holds:   Config.maySeeSettings,
	present: "- A preference changed goes through `settings` for the row and `change_setting` for the write, never `edit` or `write` on a config file. Relay a refusal as written and point at `/settings`.",
	shelved: "- A preference changed goes through `settings` for the row and `change_setting` for the write, never `edit` or `write` on a config file. Both wait in the `settings` group, so call `load_capability` and carry straight on: they are in your tool list on your next request, this same turn. Relay a refusal as written and point at `/settings`.",
	absent:  "- YOU CANNOT CHANGE A PREFERENCE FROM INSIDE A TASK: say so and point at `/settings`, and never `edit` or `write` a config file instead.",
}}

// handoffFacts is `## Work or words`: the ways work leaves this turn, one row
// per verb, so that the list a model reads is the list of verbs it has.
//
// THE DELEGATION VERB RIDES ITS OWN PREDICATE. A conversation and a worker that
// may fan out can name `propose_task`; a floor worker and a standing check cannot.
// The absent case still teaches ownership of the work without promising a verb
// that shape does not carry. The general completion rule remains in system.md
// because every shape owns the answer it is producing.
var handoffFacts = []beltFact{{
	tools: []string{"propose_task"},
	holds: Config.mayProposeTask,
	present: "Material can reveal wider work after you begin. When independent work benefits\n" +
		"from its own watched room, use `propose_task` deliberately and carry what you\n" +
		"have learned below with it. The work in front of you decides; no keyword, size\n" +
		"label or automatic reader decides for you.",
	absent: "Material can reveal wider work after you begin. There is no separate task door\n" +
		"here, so complete it with the tools you have; do not stop merely because part\n" +
		"of it would benefit from its own room.",
}, {
	tools: []string{"fork"},
	holds: Config.mayFork,
	present: "When independent parts can be completed concurrently inside this answer, use\n" +
		"`fork` deliberately once you can name the slices and then integrate their findings.",
	// A hand is told nothing, because the fork is one deep and there is no
	// second-best road to point it at (fork.go's forkTools).
	absent: "",
}, {
	tools:   []string{"build_harness", loadCapabilityToolName},
	holds:   Config.mayDesignHarness,
	present: "  - A shape of work that will recur: `build_harness`.",
	shelved: "  - A shape of work that will recur: `build_harness`, in the `harnesses` group.",
	absent:  "",
}, {
	tools:   []string{"propose_subharness", loadCapabilityToolName},
	holds:   Config.mayProposeSubharness,
	present: "  - A shape of work a saved program ALREADY does: `propose_subharness`.",
	shelved: "  - A shape of work a saved program ALREADY does: `propose_subharness`, in the `harnesses` group.",
	absent:  "",
}}

// programFacts is what a saved recipe and a saved program ARE. Each paragraph
// exists to make its own two verbs usable, so it travels with them: a build
// that cannot design one has no reason to carry the definition, and a worker
// paid for both paragraphs on every request of every turn.
var programFacts = []beltFact{{
	tools: []string{"list_harnesses", "build_harness", loadCapabilityToolName},
	holds: Config.mayDesignHarness,
	present: "A **sub-harness** is a reusable recipe: a named, versioned procedure saved\n" +
		"here, and offered by the turn when somebody's words match. `list_harnesses`\n" +
		"lists them, `build_harness` designs one.",
	shelved: "A **sub-harness** is a reusable recipe: a named, versioned procedure saved\n" +
		"here, and offered by the turn when somebody's words match. `list_harnesses`\n" +
		"lists them, `build_harness` designs one, and `load_capability` with\n" +
		"`harnesses` puts both in your tool list on your next request, this same turn.",
	absent: "",
}, {
	tools: []string{"list_subharnesses", "propose_subharness"},
	holds: Config.mayProposeSubharness,
	present: "A **subharness** is a saved PROGRAM rather than a recipe: typed input, a typed\n" +
		"answer, only the tools it declared. `list_subharnesses` lists them and\n" +
		"`propose_subharness` offers one with your line about why it matched. NOTHING\n" +
		"RUNS BECAUSE YOU PROPOSED IT: the person answers that card, so propose only when\n" +
		"the work IS what a program is for.",
	shelved: "A **subharness** is a saved PROGRAM rather than a recipe: typed input, a typed\n" +
		"answer, only the tools it declared. `list_subharnesses` lists them and\n" +
		"`propose_subharness` offers one with your line about why it matched, both from\n" +
		"the same `harnesses` group. NOTHING\n" +
		"RUNS BECAUSE YOU PROPOSED IT: the person answers that card, so propose only when\n" +
		"the work IS what a program is for.",
	absent: "",
}}

// standingFacts is `# Things that keep working after this window`, and it is a
// WHOLE SECTION composed from one predicate rather than a run of bullets.
//
// The section used to open by saying it was about a tool — "When your tool list
// carries `stand`" — which is the page admitting in its own first clause that it
// is writing 2.4KB for readers who do not have the verb. Every worker in a task
// room is one of those: a node is handed no standing store (task_run.go), and so
// are `--once` and a firing's own headless session (tools.go states why). They
// were paying for the waking kinds, the card's four answers, the RFC3339
// arithmetic and the background-checks row on every request of every turn, for a
// tool that is not on their belt — and the clause that named the condition is
// gone from the present case because the predicate below IS that condition.
//
// The absent case is the sentence the page already carried for it, which is why
// this row leaves [promptNamesBeyondTheBelt] with one entry fewer: that ledger is
// for what predates the seam, and this no longer does.
var standingFacts = []beltFact{{
	tools: []string{"stand"},
	holds: Config.mayStand,
	present: "# Things that keep working after this window\n" +
		"Some of what a person says is not work for now but something to leave behind\n" +
		"with `stand`: \"remind me at 6\", \"tell me when CI goes red\",\n" +
		"\"every Monday draft the update\", \"always run the tests\". Doing one instead\n" +
		"of proposing it answers a request they did not make. Send their sentence\n" +
		"verbatim, what wakes it, what a firing does, and its rails; the card prices it.\n" +
		"\n" +
		"WAKING OR HOLDING. A standing sentence naming a moment, a rhythm or a condition\n" +
		"gets the waking kind it names: `at`, `every`, `file`, `idle`, `probe`. One\n" +
		"naming none of them, a rule or preference (\"always ...\", \"we use X here\"), is\n" +
		"`when.kind: hold`: it never fires and never spends, riding into every\n" +
		"conversation and task it reaches, and is sent with no `does` and no `rails`.\n" +
		"\n" +
		"UNSURE MEANS INSTRUCTION PLUS AN OFFER: bind it to the work in front\n" +
		"of you AND offer the standing version in one line at the end of your reply.\n" +
		"Never a card on a guess.\n" +
		"\n" +
		"SAYING WHEN. For a distance from now (\"in 1 minute\") ALWAYS send `when.in` with\n" +
		"a Go duration (\"1m\", \"1h30m\") and NEVER work a stamp out for it, since aforge\n" +
		"resolves it against the real clock as you call. For a moment they NAMED (\"at 6\")\n" +
		"work the RFC3339 stamp out from `Now` yourself, in the same offset, as `when.at`.\n" +
		"One or the other, never both. A MOMENT ALREADY GONE IS REFUSED: work it out\n" +
		"again from THE TIME THE TOOL GAVE YOU, the `now:` line every `stand` result ends\n" +
		"with. AND NEVER TELL THEM YOU CANNOT HOLD A TIMER: \"remind me in 1 minute\" is a\n" +
		"standing one-off, `when.in: \"1m\"` with `does.kind: say`, and that IS the timer.\n" +
		"\n" +
		"A CARD OFFERS `yes, set it up`, an outright no, `just once` on anything but a\n" +
		"one-off reminder, and `change when or where`, whose answer returns as their own\n" +
		"words to re-propose with.\n" +
		"\n" +
		"WHERE A FIRING ARRIVES: the person, not a room, so never promise a reminder\n" +
		"\"here\" as though this window were the only door. NOTHING STANDS UNTIL THEY SAY\n" +
		"YES, and an unanswered card declines. Say what now stands and what it costs, and\n" +
		"never re-ask an answered card.\n" +
		"\n" +
		"BACKGROUND CHECKS ARE ON AND NOBODY IS ASKED: the first thing that ever stands\n" +
		"turns on this machine's own timer, so items are checked with no aforge window\n" +
		"open. Never promise otherwise, and turn the `background checks` row in /settings\n" +
		"if they ask.\n" +
		"\n" +
		"A LINE THAT OPENS `[something you set up fired]` IS NEWS AND NOT A REQUEST: the\n" +
		"thing already ran, so relay it to the person in one line and never call `stand`\n" +
		"again for it.",
	// AND THE ABSENT CASE NAMES NO VERB, which is what lets the section go with
	// it: a heading over one sentence is a heading nobody needs, and a sentence
	// naming a tool this belt does not carry is the lie the whole file exists to
	// prevent (prompt_belt_test.go asks it of every shape).
	//
	// IT DENIES SCHEDULING AND NOTHING ELSE. An earlier wording said nothing
	// this agent does keeps working once the window closes, which is far wider
	// than the missing verb and false on this build: work handed to a task
	// outlives the turn that started it, is checkpointed and comes home on its
	// own (task_run.go), and a session is restored rather than lost. What
	// [Config.mayStand] actually decides is whether a thing can be left to fire
	// LATER, so that is the whole of what this sentence says.
	absent: "NOTHING CAN BE SCHEDULED FROM HERE: there is no way to leave a reminder, a\n" +
		"rhythm or a condition to watch behind you, so say so plainly rather than\n" +
		"promising to check back later. Work already handed off is a different thing\n" +
		"and is not affected.",
}}

// revisionFacts uses the same capability predicate as the tool and joins the
// other conditional fragments, so the prompt contract can check both directions.
var revisionFacts = []beltFact{{
	tools:   []string{"revise_assignment"},
	holds:   Config.mayRevise,
	present: strings.TrimRight(revisePrompt, "\n"),
}}

// allBeltFacts is every row, for the tests that hold the whole table to the
// law rather than one section of it.
func allBeltFacts() []beltFact {
	all := make([]beltFact, 0, len(revisionFacts))
	for _, section := range promptSections {
		all = append(all, section.facts...)
	}
	return append(all, revisionFacts...)
}

// promptSection is one place prompts/system.md hands over to a predicate: the
// token that stands there, the rows composed into it, and how they are parted.
//
// THE LIST IS THE COMPOSITION AND THE MEASUREMENT AT ONCE. [promptWithBeltFacts]
// walks it to build the page an agent reads, and prefixbudget_test.go walks the
// same list to build the widest page any agent can be handed — so a section
// added here is composed and weighed without a second edit, which is the drift
// the fourth token would otherwise have introduced.
type promptSection struct {
	token string
	facts []beltFact
	// join is the page's own separator: bullets sit on consecutive lines, whole
	// paragraphs and sections are parted by a blank one.
	join string
}

var promptSections = []promptSection{
	{token: beltFactsToken, facts: beltFacts, join: "\n"},
	{token: handoffFactsToken, facts: handoffFacts, join: "\n"},
	{token: programFactsToken, facts: programFacts, join: "\n\n"},
	{token: standingFactsToken, facts: standingFacts, join: "\n\n"},
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
			// AND THE SHELVED WORDING ONLY WHERE THE SHAPE ACTUALLY SHELVES.
			// A worker or a task node carries these tools directly, so telling
			// it to call `load_capability` — which is not on its belt at all —
			// would be the very defect this file exists to prevent, written the
			// other way round.
			if fact.shelved != "" && config.shelvesCapabilities() {
				text = fact.shelved
			}
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
	page := systemPrompt
	for _, section := range promptSections {
		page = strings.Replace(page, section.token, renderBeltFacts(config, section.facts, section.join), 1)
	}
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
}
