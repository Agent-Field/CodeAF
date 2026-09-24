package session

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec"
)

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
	// AND THE FOURTH IS A PARAGRAPH OF ITS OWN AND NOT A RUN OF BULLETS. What
	// can be left behind to fire later is `stand`'s alone ([standingFacts]), so
	// the token stands on its own line where the section used to open.
	standingFactsToken = "STANDING_FACTS"
)

// ── the predicates ──────────────────────────────────────────────────────────
//
// EVERY ONE IS ANSWERABLE FROM THE CONFIG ALONE, as [Config.mayFanOut] and
// [Config.mayDivide] already are, because the render step has nothing else: the
// prompt is built before the agent exists (agent.go's newAgent). The belt
// functions read these SAME predicates, which is what makes it impossible for
// the two to disagree about a tool.

// hasStore controls writable memory and its prompt extraction. Conversation
// search has its own read-only predicate so workers can inherit just that door.
//
// AND A LEAN PREFIX HAS NO WRITABLE MEMORY (promptprofile.go). The reflex is two
// model calls on every turn on top of the one the person is waiting for, which
// on a small open-weight seat is the most expensive thing in the turn that
// nobody asked for — so the brain is not built (agent.go's newAgent), `remember`
// is not on the belt, and the page's own "Without `remember`, say plainly that
// memory is off" clause becomes the truth rather than a fallback.
//
// THE STORE ITSELF IS UNTOUCHED, and that distinction is the whole of this line.
// Config.Memory is also the conversation's own RECORD — the chat log every
// session writes and `search_conversations` reads — and taking that away would
// be a session that forgets what was said, which is not what "the reflex is off"
// means and is not what any window is short of.
func (c Config) hasStore() bool { return c.Memory != nil && !c.promptProfile().lean() }

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

// mayAsk says whether `ask` belongs on this belt. It is unconditional on the
// conversation's belt — only the shelf changes whether the schema is carried now
// or loaded on the next request — and OFF on a bash-belt node, whose loop reaches
// the person through the plan CLI and never through a consent gate (bashbelt.go
// builds that belt; this is the same fact asked of a config before there is an
// agent, so the belt and the page cannot disagree about the verb).
func (c Config) mayAsk() bool { return !c.mayBashBelt() }

// mayProposeTask says whether the task pair — `propose_task` and the `tasks`
// window onto what it started — belongs on this belt: always in a conversation,
// and in a node only when it was handed the conversation's graph and is not
// standing on the floor of the tree ([Agent.mayProposeTask] is this asked of a
// live agent, and task.go states the fan-out law it comes from).
func (c Config) mayProposeTask() bool { return !c.InTask || (c.mayFanOut() && !c.bashBelt) }

// mayQuickTask says whether `quick_task` belongs on this belt, and it is
// [Config.mayProposeTask] and not a second reading of it (task_quick.go).
//
// THE TWO VERBS COME AND GO TOGETHER because they answer one question — is
// there anywhere for work to go from here — and a belt that carried one without
// the other would be telling a model half a truth about its own depth. It is
// written as its own predicate rather than as the other one spelled twice
// because the page's bullet is about THIS verb, and a fact naming the wrong
// predicate is a sentence nobody can check.
func (c Config) mayQuickTask() bool { return c.mayProposeTask() && !c.oneTaskRoad() }

// oneTaskRoad says whether this agent is a conversation whose hand-offs are
// runs in the plan store: the bash belt is asked for and a run engine is wired.
//
// THERE IS ONE WAY TO PUT WORK OUT UNDER THE BELT, AND IT IS `propose_task`.
// The owner's words, 2026-09-18: one way, a task; the model launches several
// when it needs to, and each is more of the plan. So `quick_task` is ABSENT
// here, not refusing: it runs on the session tree, outside the plan store, where
// no tree on the screen can show it and no check reads it, and a belt that
// carried it would be offering a second engine under the first one's name. The
// predicate is [stagedProposal.Commit]'s own guard, so the verb leaves the belt
// exactly where a proposal starts a run and nowhere else; with no engine wired
// a proposal still runs on the session tree and both verbs stay.
func (c Config) oneTaskRoad() bool {
	return !c.InTask && bashBeltAsked() && chatRunEngine != nil
}

// mayTickItems says whether `items` belongs on this belt (task_quick.go), and
// it is the presence of the node's own list door and nothing else: a quick
// task's worker has one, and every other agent this package builds has none.
func (c Config) mayTickItems() bool { return c.quickItems != nil }

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

// signsGitWork says whether the attribution law belongs on this belt, and the
// answer is always yes. It used to be the person's `attribution` row, and that
// row is gone (2026-09-23): codeaf signs every commit it writes. It used to
// carry a second half too — a fork's hand was told nothing because its `bash`
// was rebuilt read-only — and that half went with `fork` itself: every shape
// this package still builds carries a `bash` that could make a commit. It stays
// a predicate because the belt-fact table is written in predicates.
func (c Config) signsGitWork() bool { return true }

// assistedByModel is the model this belt's `Assisted-by` line names: the model
// the page is rendered for, or nothing when the person turned the model's name
// off ([Config.AttributionModelOff]), which leaves the line bare.
func (c Config) assistedByModel() string {
	if c.AttributionModelOff {
		return ""
	}
	return c.Model
}

// gitSignature is how the harness signs a commit it writes ITSELF — a node's
// landing, a family's frozen world, a stopped run kept on its branch — and it
// is the same two trailer lines the model is told to write
// (internal/exec's [exec.AttributionTrailers]).
//
// ITS ZERO VALUE STILL SIGNS, with the bare `Assisted-by: CodeAF` line. There is
// no value of this type that leaves a commit unsigned, because the signature has
// no off; what it carries is only whether the line names a model and which.
type gitSignature struct {
	// named is the person's `attribution.model` row: whether the line names
	// the model at all.
	named bool
	// model is the model the work ran on, as the router spells it. The line
	// carries its bare name ([exec.BareModelName]).
	model string
}

// ranOn is this signature for work a particular model did. A node that ran on
// a model of its own names that one; an empty model — a node admitted before
// anybody chose one — keeps the conversation's, which is what such a node ran
// on (task_run.go's [TaskNode.model]).
func (s gitSignature) ranOn(model string) gitSignature {
	if model = strings.TrimSpace(model); model != "" {
		s.model = model
	}
	return s
}

// signedModel is the model a node ran on, for the line its landing signs with,
// and "" for a node that names none — which [gitSignature.ranOn] reads as the
// conversation's own. A node held outside any graph names none.
func signedModel(node *TaskNode) string {
	if node == nil || node.graph == nil {
		return ""
	}
	return node.model()
}

// sign is a commit message as the harness leaves it: one blank line, then the
// two trailer lines. The files where the harness writes its OWN commits import
// os/exec as `exec`, which is why they reach internal/exec's one spelling of
// the block through here rather than by name.
func (s gitSignature) sign(message string) string {
	model := ""
	if s.named {
		model = s.model
	}
	return exec.SignCommitMessage(message, model)
}

// signsGitWork is the signature a live agent's own commits carry, off the same
// two facts the sentence the model is told is rendered from — the model the
// conversation is on NOW, and the person's model-name row — so that the
// harness's commits and the model's own read the same. The commits a landing
// writes are not on any belt; nobody is asked about them (task_run.go's
// [signed]).
func (a *Agent) signsGitWork() gitSignature {
	return gitSignature{named: !a.config.AttributionModelOff, model: a.Model()}
}

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

// mayBashBelt says whether this agent's belt is the experiment's bash belt
// (bashbelt.go, docs/design/bash-task-loop/DESIGN.md): the one `bash` tool
// plus the hands that cannot be a shell command. InTask is half of the
// predicate so no road can hand the experiment to a conversation, and the
// flag is asked ONLY here — the one-reading law every belt verb follows —
// so the prompt and the belt cannot disagree about which belt a worker is
// on.
func (c Config) mayBashBelt() bool { return c.InTask && c.bashBelt }

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
	// bashAbsent is the absent wording ON THE EXPERIMENT'S BELT (bashbelt.go),
	// where a fact about handing work out reads differently: the worker's
	// coordination goes through the plan CLI, not through these verbs. Empty
	// falls back to [beltFact.absent], which is right for every fact whose
	// absence means the same thing on both belts.
	bashAbsent string
	// oneRoad is the present wording for a conversation whose hand-offs are runs
	// in the plan store ([Config.oneTaskRoad]). It names `propose_task` alone,
	// because that is the only verb such a belt carries. Empty falls back to
	// [beltFact.present].
	oneRoad string
}

// beltFacts is the whole of it, in the order the section reads.
var beltFacts = []beltFact{{
	tools:   []string{"ask", loadCapabilityToolName},
	holds:   Config.mayAsk,
	shelved: "- `ask` waits in the `questions` group; `load_capability` fetches it.",
}, {
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
		// AND THE TWO TRIGGERS THAT LOST THEIR HOME when the tool descriptions
		// gave up their routing prose (docs/design/prompt-diet/DESIGN.md §4).
		// [tasksDescription] says what an id DOES; what it cannot say is when to
		// reach for one, and the second half is a refusal: "continue task N" is
		// the `continue` field on a task that already exists, and a model that
		// answers it with a fresh proposal mints a second task with a fresh brief
		// and a fresh working copy (task_continue.go, F23/F25).
		"- Look inside running or landed work with `tasks` and its id; \"continue task N\" is that id with `continue`, never a fresh `propose_task`.\n" +
		"- A `tasks` row is a citation, not the work: its transcript URI is the JSONL journal of all that node said, called and got back, and `read` takes a row's URIs exactly as printed, `file://` and all. `grep` a journal or `read` it with `offset`/`limit`, never expand an outcome line into work you did not read, and say so when a row prints no transcript. A `[Task reference: ...]` block already carries those URIs.\n" +
		// THE `id` AND `continue` SENTENCES ARE NOT REPEATED HERE.
		// [tasksDescription] already says an id reads, steers, continues or settles
		// one piece of work and is how you look inside running work, and the prefix
		// is a budget: what paid for `propose_task`'s `checks` field, and for the
		// handoff law in prompts/system.md, is this second copy of a law the model
		// holds whenever it holds the verb.
		//
		// AND THE CLAUSE ABOUT `say` IS HERE BECAUSE THIS IS WHERE `say` IS NAMED.
		// A model asked to stop a task and holding no stop verb reached for the
		// nearest thing on the belt and said "stop, do not continue" into the work;
		// it kept running, and the check read what came back as an ordinary
		// unfinished run. The verb exists now ([tasksDescription] carries what it
		// does), so what this line owes is the boundary between the two.
		//
		// THE FOUR WORDS HAVE LEFT THIS PAGE, and so have the verbs a `your call`
		// takes. A landing now announces itself: task_run.go's [landingNoteLead]
		// carries the tier word and says to give it back, [settleClause] names the
		// address with its verbs interpolated from [TaskResolutions], a clash with
		// the person's own branch is refused where it happens ([conflictNotYours],
		// [shiftNotYours]), and a verb reaching for a graph that has closed is
		// refused in the tool's own reply (tools_tasks.go). Every one of those was
		// bought on each request of every turn in order to be told a second time on
		// the one turn it mattered (docs/design/prompt-diet/DESIGN.md §2).
		"- To send the person's current correction to a running task, use `tasks` with `id` and `forward: true`. `say` is your own coordination and ends nothing; `stop` ends a task.",
	absent: "- THE RECORD OF EARLIER WORK IS NOT REACHABLE FROM HERE and none of this work goes to anybody else: answer from the brief and from what is in front of you, and say plainly when something earlier is referred to that you cannot see. A `[Task reference: ...]` block you were handed carries transcript URIs, and `read` takes one exactly as printed, `file://` and all: `grep` a journal or `read` it with `offset`/`limit`, and never expand an outcome line into work you did not read.",
}, {
	tools:   []string{"search_conversations"},
	holds:   Config.hasConversationHistory,
	present: "- When asked to find a past conversation or report what was said or decided elsewhere, call `search_conversations` BEFORE answering, even if a saved memory suggests the answer. Memories guide the query; source messages establish what was said. Copy a returned ref to read more and check corrections.",
	absent:  "- What was said in earlier conversations cannot be looked up from here, so answer out of what is in this window rather than reconstructing it.",
}, {
	// THE SKILL SHELF, on the same predicate as propose_task plus a store to
	// read it from (tools_skill.go's [Agent.useSkillTool]): a worker that may
	// hand work out may also look up what this project already knows how to do,
	// and a shape with no shelf behind it is told the shelf is not reachable
	// rather than reaching for a verb that is not on its belt.
	tools:   []string{useSkillToolName},
	holds:   func(c Config) bool { return c.mayProposeTask() && c.skillShelf() != nil },
	present: "- `use_skill` lists active skills (name + one-line doc) or resolves one by name to its shelf path.",
	absent:  "- Skills on the shelf are not reachable from here.",
}, {
	tools:  []string{"watch"},
	holds:  Config.mayWatch,
	absent: "- There is no `watch` here: a foreground `bash` call is how you wait for something to finish.",
}, {
	// BOTH NAMES, because the sentence spells both and each is on some belts
	// and off others: `services` is the look, `use_service` the pick-up, and the
	// law that reads this field is what keeps the page from naming either one
	// where it is not there (prompt_belt_test.go).
	tools: []string{"services", "use_service"},
	holds: Config.hasConnect,
	// ONE EXISTENCE LINE, AND THE ETIQUETTE RIDES WITH THE VERBS IT GOVERNS.
	// This was three bullets and 1,142 bytes, and each of the two that went is
	// said again by the tool it is about, in front of the model at the moment it
	// calls: [serviceRequestDescription] names the address, spells which methods
	// read and which act, and says outright which half the person has turned off
	// ("get reads, and that is all this account may be used for"); `gmail_send`,
	// `slack_send` and `calendar_create` each say that the call leaves in the
	// person's name, that they are asked before it goes, and that it cannot be
	// called back; [useServiceDescription] says a request is never made twice for
	// the same account and that nothing connects without the person agreeing.
	// What is left is the one thing the page must say BEFORE any of them is on
	// the belt: that the accounts are there and which verb reaches them.
	//
	// AND WHO DOES THE CONNECTING IS NOT SAID TWICE. That the person is the one
	// who agrees is [useServiceDescription]'s own sentence, in front of the model
	// at the moment it calls; every byte here is sent again on every request of
	// every turn, so this line carries only what has to be true before the call.
	present: "- The person has accounts you can act in; NEVER answer \"I don't have access to your X\" before calling `services`: ask through `use_service` for one that is not connected yet, and its tools arrive on your next request of this same turn.",
	// NOTHING IS SAID WHERE THERE IS NO HUB. An agent with no accounts seam has
	// no account to act in, no consent to relay and nothing to do instead.
	absent: "",
}, {
	// AND `load_capability` IS ONE OF THIS SENTENCE'S TOOLS, in the shelved
	// wording only. It is true on exactly this predicate: [Agent.settingsTools]
	// is gated on [Config.maySeeSettings] and nothing else, so a shape holding
	// this fact has the settings group, and a non-empty shelf always carries the
	// loading verb (tools_capabilities.go).
	tools: []string{"settings", "change_setting", loadCapabilityToolName},
	holds: Config.maySeeSettings,
	// AND THE ROUTING IS ALL THAT IS LEFT HERE. What to do with a refusal — relay
	// it as written, point them at `/settings` — is the `settings` group's own
	// prose (tools_capabilities.go), read by the turn that fetched the pair,
	// because it is not a fact anybody needs before there is a refusal to relay,
	// and what `load_capability` does once it is called is that verb's own
	// description (lane C's registry files it there).
	present: "- A preference changed goes through `settings` for the row and `change_setting` for the write, never `edit` or `write` on a config file.",
	shelved: "- A preference changed goes through `settings` for the row and `change_setting` for the write, never `edit` or `write` on a config file. Both wait in the `settings` group and `load_capability` fetches them.",
	absent:  "- YOU CANNOT CHANGE A PREFERENCE FROM INSIDE A TASK: say so and point at `/settings`, and never `edit` or `write` a config file instead.",
}, {
	// THE ATTRIBUTION LAW, AND IT IS THE RESIDENT'S OWN WORDING RATHER THAN A
	// SECOND ONE (internal/exec's [exec.AttributionLaw]). Both surfaces do git
	// work in the same person's name into the same history, and two paragraphs
	// about the same four bytes would drift into two laws — the trailer is
	// provenance, so a trailer spelled differently in a task than in the
	// conversation is provenance that cannot be counted.
	//
	// IT NAMES `bash` BECAUSE `bash` IS WHERE IT HAPPENS. This is the only place
	// the model can commit or open a pull request at all; the mechanical commit
	// a landing writes is not the model's and carries the same two lines without
	// being told (task_run.go's [commitTaskWorkAs]).
	//
	// IT HAS NO ABSENT CASE, because signing has no off. The law's own commit
	// sentence spells both trailer lines in their order, with the `Assisted-by`
	// one left as a slot the render fills from the model this page is for
	// ([Config.assistedByModel]) — so the law is the one place both lines are
	// said, and the page does not say them a second time.
	tools:   []string{"bash"},
	holds:   Config.signsGitWork,
	present: "- " + exec.AttributionLaw,
	absent:  "",
}}

// handoffFacts is `## Work or words`: the ways work leaves this turn, composed
// from the belt so that what a model reads is what it actually has.
//
// IT IS A PICTURE AND NOT A RULE LIST, which is the owner's ruling of
// 2026-09-10 and a reversal of how this section was written. What it hands the
// model is what it HAS and what each thing COSTS — one mind with a clock; a
// quick task that is a copy of its abilities where it stands; a task that is a
// worker in a copy of the folder, checked and merged; the arithmetic that
// pieces kept cost their sum and pieces handed out cost the longest of them —
// and the model does its own reasoning from that. There are no shapes of work
// enumerated here, because a list of shapes is a list somebody has to keep
// true: the moment it says "a survey is quick" it has stopped teaching and
// started matching, and the next request is one nobody wrote a row for.
//
// IT REPLACED A LIST THAT SORTED ON WIDTH, and the list was measurably wrong.
// Until this ruling the first bullet read "WIDE WORK — a sweep across many
// files, research across many sources … ONE `propose_task` with `wide` set.
// That is the default road", and a real model applied it exactly as written:
// asked for a READ-ONLY survey of four packages it answered "wide survey across
// four packages — sizing it before I hand it off" and bought a worktree, a
// check and a landing for four files nothing was going to write. Width was
// never the question, and a second rule saying so would have been one more row
// to match against.
//
// THE CLOCK IS THE HALF THAT WAS NEVER STATED AT ALL. A model left to itself
// does independent pieces one after another, because that is what one thread
// of reasoning feels like from the inside, and the person waits the sum of them
// for an answer that could have cost the longest. So the middle paragraph is
// the arithmetic and then the goal it serves, stated ONCE HERE AND NOWHERE
// ELSE: this is the one paragraph every agent that can hand work out reads,
// the conversation whose fan-out is uncapped included, so the fan-out page a
// task node also reads does not say it again (the law registry's
// `handoff.wall-time-goal` fails a second copy on any page). The last
// paragraph is the conduct that keeps the win: do the
// piece you kept, never look in on running work, and end the turn when nothing
// independent of it is left. Both lean on laws already written — the page's own
// `HANDED-OFF WORK IS NOT WORK THAT REMAINS` and [taskHandoffWakeSentence] on
// every receipt — and say only the part neither of them says.
//
// THE ABSENT CASE IS THE OPPOSITE INSTRUCTION AND NOT A SHORTER ONE. On the
// floor of the tree there is nowhere to hand anything, so a picture of two
// roads is a picture of two roads that are not there; what is true there is
// that the work is yours, so open it. Both verbs are on a belt together or on
// neither ([Config.mayQuickTask]), which is what lets one fragment name them
// both and be true wherever it renders.
var handoffFacts = []beltFact{{
	tools: []string{"propose_task", quickTaskToolName},
	holds: Config.mayProposeTask,
	present: "You are one mind with a clock, and two ways to put more minds on the work run\n" +
		"beside you. A `quick_task` is a copy of your abilities working where you stand:\n" +
		"it starts the instant you ask, reads and writes in this same folder, and its\n" +
		"last message comes back to you as a note. A `propose_task` is a worker in a copy\n" +
		"of the folder, checked and merged when it finishes, that outlives this window.\n" +
		"Both are yours to steer and to stop, and both wake you when they land.\n" +
		"\n" +
		"So WEIGH THE CLOCK BEFORE YOU BEGIN, and again each time the material shows you\n" +
		"more than you knew. WHEN THE ASK ITSELF NAMES SEVERAL THINGS, THOSE ARE THE\n" +
		"PARTS: the person has already done the dividing, and work with parts in it is\n" +
		"not yours to grind through inline. Pieces done in your own hands cost their sum,\n" +
		"and independent pieces handed out in one breath cost the longest of them alone.\n" +
		"THE GOAL IS THE SHORTEST WALL TIME FOR THE WHOLE JOB: when what is ahead has\n" +
		"parts that do not need each other, hand them all out at once, however many there\n" +
		"are, before you open the first, the way one mind with a team of workers would,\n" +
		"and keep one to begin yourself, rather than working through them in turn, the\n" +
		"order you fall into unless you choose otherwise. Do it even where each part is\n" +
		"plainly something you could do yourself. That you could is not the question. The\n" +
		"clock is. When the parts\n" +
		"feed each other, keep them together: your own steps, or one quick task's items.\n" +
		"One read, one edit, one command is never worth a hand-off. What must be checked\n" +
		"and landed on its own, or must survive you, is a task; what you will read and\n" +
		"carry on with is quick, and a quick task is a small thing: a few files, a few\n" +
		"minutes.\n" +
		"\n" +
		"AFTER HANDING OUT YOU ARE NOT WAITING. Do the piece you kept, or answer what you\n" +
		"can. Each landing comes to you as a note, and starts a turn if yours has ended,\n" +
		"so fold them as they arrive. When nothing independent of what you handed out\n" +
		"remains, end your turn. That is how you wait.",
	oneRoad: "You are one mind with a clock, and ONE way to put more minds on the work:\n" +
		"`propose_task`. Each is a worker in a copy of the folder, checked when it\n" +
		"finishes, that outlives this window. The first opens a run; every one after it\n" +
		"while that run lives joins the same run as another task of it, and so does a\n" +
		"`/task` the person types. SEVERAL PROPOSALS IN ONE MESSAGE ARE HOW YOU WORK IN\n" +
		"PARALLEL: parts that do not need each other go out together and cost the\n" +
		"longest of them alone, where done in your own hands they cost their sum. A part\n" +
		"that must follow another is proposed once that one's id is back, naming it in\n" +
		"`depends_on`. Parts that feed each other closely are ONE task: its worker\n" +
		"splits it further in the plan when the material shows it is wide.\n" +
		"\n" +
		"So WEIGH THE CLOCK BEFORE YOU BEGIN, and again each time the material shows you\n" +
		"more than you knew. WHEN THE ASK ITSELF NAMES SEVERAL THINGS, THOSE ARE THE\n" +
		"PARTS. THE GOAL IS THE SHORTEST WALL TIME FOR THE WHOLE JOB. One read, one edit,\n" +
		"one command is never worth a hand-off: do it here.\n" +
		"\n" +
		"AFTER HANDING OUT YOU ARE NOT WAITING. Do the piece you kept, or answer what you\n" +
		"can, and end your turn when nothing independent of what you handed out remains.\n" +
		"A landing speaks here only when the person is owed an answer. " +
		"A finished task is asked about with `tasks` and is never redone or rechecked by hand.\n" +
		"\n" +
		// AND THE MANAGER'S JOB, WHICH NOBODY ELSE CAN DO. While a run is live
		// the person's message arrives with a digest of its rows in front of it
		// (plandigest.go), so the fact is already in hand; this is what to do
		// with it. It is one sentence because it is one decision, and it is
		// here rather than on a page because this is the paragraph every
		// conversation that can start a run reads.
		"WHILE WORK IS RUNNING, YOUR MESSAGE FROM THE PERSON OPENS WITH ITS ROWS. If what\n" +
		"they just said makes one of those tasks wrong — they changed their mind, dropped\n" +
		"a part, told you a fact it is built on is untrue — act on THAT row before you\n" +
		"answer them: `tasks` with `stop` ends work that should not go on, and `tasks`\n" +
		"with `note` tells a worker a fact it is missing. You are the only one holding\n" +
		"the conversation, so you are the only one who can know. Leave the rest alone.",
	bashAbsent: "Work goes out through the plan when it has parts that do not need each other:\n" +
		"`plandb add` and `plandb split` in bash are how, and every ready task they make\n" +
		"is given a worker of its own. What is yours alone you carry here, in the order\n" +
		"that finishes it.",
	absent: "WORK IS YOURS TO DO HERE. There is nowhere to launch it at from where you\n" +
		"stand, so a sweep across many files, research across many sources or the same\n" +
		"change over many items is work you open and carry yourself, in the order that\n" +
		"finishes it.",
}, {
	tools:   []string{"build_harness", loadCapabilityToolName},
	holds:   Config.mayDesignHarness,
	present: "AND A SHAPE OF WORK THAT WILL RECUR is neither of them: `build_harness` designs it once and saves it.",
	shelved: "AND A SHAPE OF WORK THAT WILL RECUR is neither of them: `build_harness` designs it once and saves it, in the `harnesses` group.",
	absent:  "",
}, {
	tools:   []string{"propose_subharness", loadCapabilityToolName},
	holds:   Config.mayProposeSubharness,
	present: "AND A SHAPE OF WORK A SAVED PROGRAM ALREADY DOES: `propose_subharness`.",
	shelved: "AND A SHAPE OF WORK A SAVED PROGRAM ALREADY DOES: `propose_subharness`, in the `harnesses` group.",
	absent:  "",
}}

// programFacts is the ROUTING LINE for saved recipes and saved programs: which
// verb exists here, and — where they are shelved — the group to load it from.
//
// WHAT A SAVED RECIPE AND A SAVED PROGRAM *ARE* IS NO LONGER HERE. Both
// paragraphs were mechanics for verbs this belt is not carrying, bought on every
// request of every turn against a group the model has to fetch before it can
// call anything. They are now the `harnesses` group's own prose
// (tools_capabilities.go), emitted under the `Loaded:` line by the load that
// fetches the verbs — which is the turn that first needs to know the difference
// — and the four descriptions each state their own contract in full besides.
//
// THE PAGE STILL NAMES THE VERBS IT CAN VOUCH FOR, because a model cannot ask
// for a group whose tools it has never heard of: `list_harnesses` and
// `build_harness` are here on their own predicate, `propose_subharness` is in
// [handoffFacts]'s road list on ITS predicate, and tools_harness_test.go holds
// the page to the first two by name. `list_subharnesses` is named nowhere on the
// page, which is always safe — the loading verb's own catalog lists it, and the
// group's prose says what it is for.
var programFacts = []beltFact{{
	tools: []string{"list_harnesses", "build_harness", loadCapabilityToolName},
	holds: Config.mayDesignHarness,
	present: "A **sub-harness** is a reusable recipe this machine has saved and offers by\n" +
		"itself when somebody's words match: `list_harnesses` lists them,\n" +
		"`build_harness` designs one.",
	shelved: "A **sub-harness** is a reusable recipe this machine has saved and offers by\n" +
		"itself when somebody's words match: `list_harnesses` lists them,\n" +
		"`build_harness` designs one. Both wait in the `harnesses` group, so call\n" +
		"`load_capability`: it fetches them and says what each verb there is for.",
	absent: "",
}}

// standingFacts is what the page says about work that outlives this window, and
// it is now ONE SENTENCE where it was a 2,482-byte section.
//
// EXISTENCE IS THE PAGE'S; MECHANICS RIDE WITH THE VERB; CONSEQUENCES RIDE WITH
// THE EVENT. Those are the three classes the prompt diet files every law under
// (docs/design/prompt-diet/DESIGN.md §2), and the old section was all three of
// them stacked in message[0]:
//
//   - EXISTENCE — that a sentence can be left behind rather than done — is the
//     only part needed BEFORE the model plans, because "remind me at 6" has to
//     be recognised as `stand`'s before anything else happens. That is the
//     sentence below, and it stays.
//   - MECHANICS — the waking kinds, the hold, `when.in` against `when.at`, the
//     RFC3339 arithmetic, what a card offers, what it costs — are needed at the
//     CALL, and tools_standing.go's [standDescription] and [standSchemaJSON]
//     already own every one of them, at greater length and beside the field
//     each governs. The page was the second copy.
//   - CONSEQUENCES — what to do when one fires — are needed only on the turn one
//     fires, and standing_run.go's [standingNewsRule] is already appended to the
//     firing's own line: "this already happened. Relay it to the person in one
//     line. Do not call stand again for it". The page was the second copy of
//     that too, and a page that explains a message the message explains itself
//     is a page paid for on every request for a turn most sessions never have.
//
// The rest — background checks running with no window open, the card's four
// answers, that "remind me in 1 minute" IS the timer — is ON DEMAND: the chat
// manual's keeping-an-eye and standing-orders pages carry all of it in the words
// a person asks it in, and `manual` is a tool the model has.
//
// The absent case is the sentence the page already carried for it, which is why
// this row leaves [promptNamesBeyondTheBelt] with one entry fewer: that ledger is
// for what predates the seam, and this no longer does.
var standingFacts = []beltFact{{
	tools: []string{"stand"},
	holds: Config.mayStand,
	present: "SOMETHING TO LEAVE BEHIND — a reminder, a watch on the world, a rhythm, a rule\n" +
		"that binds work nobody has done yet — is `stand`'s: \"remind me at 6\", \"tell me\n" +
		"when CI goes red\", \"every Monday draft the update\", \"always run the tests\".\n" +
		"PROPOSE IT, and never do it instead of proposing it, which answers a request\n" +
		"they did not make. `stand`'s own description says how to tell one from the work\n" +
		"in front of you and how to say when.",
	// AND THE ABSENT CASE NAMES NO VERB: a sentence naming a tool this belt does
	// not carry is the lie the whole file exists to prevent (prompt_belt_test.go
	// asks it of every shape). Neither case carries a heading any more, because
	// a heading over one sentence is a heading nobody needs.
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
	{token: handoffFactsToken, facts: handoffFacts, join: "\n\n"},
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
		if config.mayBashBelt() && fact.bashAbsent != "" {
			text = fact.bashAbsent
		}
		if fact.holds(config) {
			text = fact.present
			if fact.oneRoad != "" && config.oneTaskRoad() {
				text = fact.oneRoad
			}
			// AND THE SHELVED WORDING ONLY WHERE THE SHAPE ACTUALLY SHELVES
			// THESE TOOLS. A worker or a task node carries them directly, so
			// telling it to call `load_capability` — which is not on its belt at
			// all — would be the very defect this file exists to prevent,
			// written the other way round. The question is asked per FACT rather
			// than per shape because a lean prefix is handed the `questions`
			// group at construction (promptprofile.go's [Config.shelvesFact]):
			// it shelves plenty and carries `ask`.
			if fact.shelved != "" && config.shelvesFact(fact) {
				text = fact.shelved
			}
		}
		if text != "" {
			// THE ASSISTED-BY LINE IS FILLED HERE because this is the one point
			// that holds both the belt's bytes and the model the page is
			// rendered for: the attribution fact carries the law's slot, and
			// [exec.FillAttribution] puts the line there — named, or bare when
			// the person turned the model's name off.
			text = exec.FillAttribution(text, config.assistedByModel())
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
	// A hand inherits the caller's page but intentionally carries a smaller,
	// closed belt; its appended tail is the truthful capability account.
	"ask": "Use `ask` only as the last rung",
}
