package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
)

// The settings registry is the one place a user-tunable knob is written down.
// Every row declares how it reads today, how it is applied, where it persists,
// and which environment variable pins it — so a new knob is a row here rather
// than a new lookup scattered through the tree, and the completeness test in
// settings_test.go fails the build until it is registered.
//
// Resolution is the budget rail's order, generalized: environment → the
// profile's config.json → the built-in default. The environment always wins,
// and a pinned row renders read-only rather than letting the surface fight the
// shell it was launched from.

// SettingKind decides how a row reads, edits, and validates.
type SettingKind int

const (
	// SettingModel opens the existing capability-filtered model picker.
	SettingModel SettingKind = iota
	SettingDollars
	SettingDuration
	SettingPercent
	SettingCount
	SettingBool
	SettingChoice
	SettingText
)

// Category names are the faint lowercase words the sheet may announce a section
// with (15). There were seven, and five of them were labels doing structure's
// job: `rhythm`, `documents & vision` and `sharing` each announced two rows or
// one, which is a header naming a mechanism rather than a section a reader could
// otherwise not place. Deleting them is 15's own test — the rows still read,
// because position and spacing already said what the word said.
//
// SPENDING IS MONEY AND NOTHING ELSE, and that is why there are six words here
// rather than four. `spending` had grown to hold twenty rows answering four
// different questions — what may it spend, what may it run without asking, how
// does it run tasks, and which workers exist — so a person looking for "how much
// may it spend" read about load averages and repair rounds first. The three
// questions are three sections now, and docs/design/spending/DESIGN.md is the
// argument: a category is what a row is ABOUT, and a category that answers four
// questions is a drawer rather than a section.
const (
	// CategoryModels is what runs the work: one row per role the router has,
	// then the capability models beside them.
	CategoryModels = "models"
	// CategorySpending is every dollar the product will spend without asking,
	// and NOTHING that is not a dollar.
	CategorySpending = "spending"
	// CategorySafety is what aforge may do without asking you first: the
	// approval gate, its exceptions, the model that stands in for you, and the
	// two clocks that answer when nobody does.
	CategorySafety = "safety"
	// CategoryTasks is how work you can walk away from is run — how it starts,
	// how it is checked, how much of it happens at once, and on whose hands.
	CategoryTasks = "tasks"
	// CategoryPractice is what aforge does with its own time, and what it
	// remembers of yours.
	CategoryPractice = "memory & practice"
	// CategoryInterface is how the surface draws itself, and how it signs the
	// work that leaves the machine.
	CategoryInterface = "interface"
)

// The old spellings, kept as aliases so a surface that still names one keeps
// compiling while it is being ported. They are the same four words; nothing
// resolves to a group that no longer exists.
const (
	CategoryMoney      = CategorySpending
	CategoryLearning   = CategoryPractice
	CategoryAppearance = CategoryInterface
)

// SettingCategories is the render order of the sheet. Spending leads the three
// new sections because "what may it spend" is the question people arrive with;
// safety and tasks follow it in the order the design's own hierarchy names.
var SettingCategories = []string{
	CategoryModels, CategorySpending, CategorySafety, CategoryTasks,
	CategoryPractice, CategoryInterface,
}

// Persisted keys are also the json field names in the profile's config.json.
// KeyDailyBudget keeps the name /budget default already writes.
const (
	KeyDailyBudget    = "daily_budget_usd"
	KeyPlanConsent    = "plan_consent_usd"
	KeyPracticeBudget = "practice_budget_usd"
	KeyPracticeIdle   = "practice_idle"
	KeyBriefAfter     = "brief_after"
	KeyTenureAfter    = "tenure_after"
	KeyDocumentEngine = "document_engine"
	KeyVisionModel    = "vision_model"
	KeyAttribution    = "attribution"
	KeySplitPct       = "split_pct"

	// The two rows the v3 chat surface keeps on disk BESIDE the conversation:
	// what was typed, and what was half-typed. They are one pair of questions —
	// "may aforge remember my own words between sessions" — and they are two
	// rows rather than one because they answer it at different depths: history
	// is every prompt ever submitted from this machine, the draft is the single
	// unsent sentence in front of you right now, and a person who wants the
	// second without the first (or the reverse) is not confused.
	KeyHistoryEnabled = "history.enabled"
	KeyDraftPersist   = "draft.persist"

	// The v3 session's own keys. They are DOTTED where the older ones are
	// snake_case because they name a path into a settings tree the file writer
	// will eventually hold — tools.approval is a map, models.roles is a map —
	// and the flat text rows below are the readable stand-in until it lands.
	KeyToolApprovalMode = "tools.approvalMode"
	KeyToolApprovals    = "tools.approval"
	// KeyBashApprovals is the ordered rule list for the one tool whose arguments
	// are a language. The row above answers per TOOL — "never ask me about read"
	// — and there is no useful per-tool answer for bash: a person who allowed the
	// tool would have allowed every command it will ever be handed. This row is
	// where the answer can name the command, which is what internal/approval's
	// bash patterns are for and where the consent card's "always, this command"
	// lands (approvalmemory.go).
	KeyBashApprovals = "tools.bashPatterns"

	// KeyGuardian turns on the small model that answers a tool prompt before you
	// are asked (internal/session's guardian.go). It is named under `approval.`
	// rather than beside the two `tools.` rows above because it is not a rule
	// about tools at all: it is a statement about WHO ANSWERS — the person, or a
	// model standing in for them — and grouping it with the rule rows would file
	// it as one more exception in a list of exceptions.
	KeyGuardian = "approval.guardian"
	// KeyConsentTimeout is how long an approval question waits for a keystroke
	// before it answers itself. It answers DENY — never allow — so the clock can
	// only ever be the cautious one, and it stops the moment a key is pressed,
	// because a person who has started reading is a person who is going to
	// answer.
	//
	// Seconds, not a duration string, for the reason [KeyTaskAutoApprove] is
	// spelled that way: the number is small and read at a glance off a line that
	// is counting it down. 0 turns the clock off and the question waits forever,
	// which is what a person who reads every prompt wants.
	KeyConsentTimeout = "approval.timeout_seconds"
	KeyTierLowModel   = "models.tiers.low"
	KeyTierHighModel  = "models.tiers.high"
	// KeyTierReflexModel is the third tier, and the only one with a model in it
	// out of the box. It is read TWICE A TURN by the routing and extraction
	// calls the reflex tier exists for (internal/reflex), which is a rhythm no
	// other auxiliary call has: a model that costs a tenth of a cent a call is
	// free on the low tier and is real money here. So the row ships pointed at
	// a model that costs near nothing rather than at "follows the conversation"
	// — a person who never opens the sheet gets the cheap thing, and a person
	// who clears the row gets the conversation's own model, deliberately.
	KeyTierReflexModel = "models.tiers.reflex"
	// KeyTierMastermindModel is the fourth tier and the only one whose model is
	// chosen for THINKING rather than for a price. Two roles ride it — the
	// planner that amends an adaptive run's plan after every node, and the
	// designer that writes a harness page everybody afterwards runs — and both
	// were on the careful-work tier beside the compaction summary, which made one
	// figure answer two unrelated bills: the careful calls are many and short,
	// these are few and decide what all the other calls do.
	//
	// It is the one row whose value may carry a LEVEL as well as a model
	// (`moonshotai/kimi-k3:low`), because it is the one row where how hard the
	// model thinks is the point. Every tier row accepts the notation —
	// [ValidateTierValue] is the same gate on all four — but this is the one the
	// shipped crew writes it into.
	KeyTierMastermindModel = "models.tiers.mastermind"
	// KeyCrew is the four tiers answered as ONE DECISION. Nobody arrives wanting
	// to name four model ids; they arrive wanting to spend pennies, or to spend
	// what it takes. So the row takes one word — frugal, balanced, max — and
	// writes all four tier rows from it.
	//
	// IT IS NOT STORED. The row's reading is DERIVED from the four live tier
	// values: they match a preset and it says so, or they do not and it says
	// custom. A stored word would be a claim about four other rows that anybody
	// could falsify by editing one of them, and a settings sheet that told you
	// "balanced" over a hand-pinned tier would be lying in the one place a person
	// went to check.
	KeyCrew = "models.crew"
	// KeyMouse is whether the surface reports the mouse at all. ON is the
	// default ([DefaultMouse]), because hover, click and the wheel are v3's own
	// language and the thing they cost is bought back by a key: an alt-screen
	// app that reports the mouse OWNS every drag, so the terminal's native text
	// selection dies the moment reporting starts — and `ctrl+s` hands the
	// pointer back for as long as somebody is dragging with it.
	//
	// THIS COMMENT SAID "OFF IS THE DEFAULT" while [DefaultMouse] said on, and a
	// stale sentence here is the expensive kind: it is the first thing anybody
	// reads when the answer to "does aforge ask for the mouse at all" decides
	// whether a pointer bug is in this program or in the terminal. One source of
	// truth — [MouseModes] states the choice and [DefaultMouse] states the
	// answer, and this row's doc may not contradict either.
	KeyMouse = "ui.mouse"
	// KeyTimestamps is how much of the clock the v3 conversation carries: a
	// footer under every finished turn, only the coarse marks where the
	// conversation was put down and picked up again, or nothing at all
	// (internal/tui3's render.go). It is named beside the mouse row because it
	// is the same kind of question — how much furniture the transcript draws —
	// and it is a CHOICE rather than a bool because "less" and "none" are two
	// different answers a reader gives for two different reasons.
	KeyTimestamps = "ui.timestamps"

	// KeyTaskColumn is whether the v3 chat opens with the task roster's column
	// standing beside the conversation. It is a BOOLEAN — the column has no
	// middle rung: its two narrower tiers are decided by the frame's own width,
	// and the one answer a person gives it by hand is whether the column is
	// there at all.
	//
	// A person who put v3's column away has said
	// nothing whatever about v2's three rungs.
	KeyTaskColumn = "ui.task_column"
	// KeyQuickSwitch is whether the conversation switcher (internal/tui3's
	// hop.go) SWITCHES ON EACH PRESS of its chord — `ctrl+k` lands you in the
	// previous conversation at once and pressing again keeps going — or opens as
	// a card that waits for `enter`. It is a BOOLEAN because the two behaviours
	// are the whole of the choice: there is no third rung between "the key is
	// the switch" and "the key is the menu".
	KeyQuickSwitch = "ui.quick_switch"
	// KeyHints is whether the v3 chat shows its earned hints — the one-line tips
	// in the slot above the message box that each retire once the key or command
	// they name has been used (internal/tui3's notice.go) — and, with them, the
	// one-line what's-new notes a new build may say. It is one row and not two
	// because a person who has silenced the tips has said they know the surface,
	// and being told about features is the same conversation.
	KeyHints = "ui.hints"
	// KeyWork controls whether completed turn machinery starts folded or open.
	KeyWork = "ui.work"
	// KeyTaskAudit is whether an independent auditor verifies each task node
	// before its work may merge (internal/session's task_audit.go). It sits
	// beside the guardian because both spend a model on the person's behalf:
	// the guardian to answer, the auditor to check.
	KeyTaskAudit = "task.audit"
	// KeyReplyGuard is whether a reply that has stopped being language is cut
	// and asked again (internal/provider's streamguard.go). It is a row because
	// the cut is a JUDGEMENT about somebody else's text, and a person who writes
	// in four alphabets, or who asks for pages of repeated output outside a code
	// fence, is entitled to say they would rather see whatever arrives.
	//
	// The silence watchdog beside it has no row: a request that produced nothing
	// at all has failed by any reading.
	KeyReplyGuard = "reply.guard"
	// KeyTaskStart is what a bare `/task <brief>` COSTS TO SHAPE, and it is no
	// longer a question about shape at all (internal/tui3's taskcommand.go).
	// Every answer starts ONE WORKER. What the answers differ in is whether a
	// small sizing call reads the brief first, because a yes from that call is
	// what arms the worker to hand the work out mid-run once it has opened the
	// material and found the width is real (internal/session's task_divide.go).
	//
	// It is a row because the question is not really about one task. Somebody who
	// does not want the sizing call on their bill is refusing it every time, and
	// that is a preference stated once rather than on every `/task`.
	//
	// THE PLANNED-GRAPH ANSWER IS GONE, and with it the last way a preference
	// could open an adaptive run behind somebody's back. A `/task` takes one road
	// now — one worker that divides itself from the material — and there is no
	// second road left to prefer: no chat door reaches the planner at all, not a
	// setting, not a hand on the belt, and not a form of words somebody types
	// (internal/session's loop.go). A profile still holding the retired word reads
	// as the default, silently, the way any word this build does not know reads
	// ([TaskStartAt]).
	KeyTaskStart = "task.start"
	// KeyMemoryEnabled is whether this build remembers anything across
	// conversations at all (internal/session's memory.go): the pre-turn router
	// that decides which remembered lines a turn needs, the post-turn pass that
	// decides whether the exchange held anything worth keeping, and the
	// `remember` tool the model reaches for. Off is a conversation that starts
	// knowing nothing about you, and that makes not one extra call.
	KeyMemoryEnabled = "memory.enabled"

	// KeyStandingBackground is whether this machine's own scheduler keeps
	// standing items current when no aforge window is open (internal/standing's
	// watch.go). It is dotted with the other v3 keys, under `standing.` because
	// that is the thing it is about, and it is PROFILE-ONLY — deliberately
	// absent from [ProjectKeys]: installing a timer is a change to somebody's
	// machine, and a checked-in file that could make one is a repository
	// arranging to run a program on every laptop that clones it.
	//
	// ON IS THE DEFAULT and the row is the only place it is ever asked. What
	// the row READS is derived from the timer itself rather than from this
	// value, so the sheet cannot say `on` over a machine where nothing is
	// installed; this value is the person's INTENT, which is what the launch
	// repair reads before it puts a drifted timer back
	// ([BackgroundChecksWantedAt]).
	KeyStandingBackground = "standing.background"

	KeyModelRoles = "models.roles"
	// KeyModelFallbacks is the ordered list of models a conversation moves to
	// when no endpoint serving the one it is on will accept the request at all
	// (internal/provider's endpoints.go). Comma-separated slugs, first tried
	// first. Empty lets the catalog pick the nearest same-class model instead.
	KeyModelFallbacks = "models.fallbacks"
	KeySpendRail      = "session.spendRailUSD"

	// KeyRouting is how a session asks the router to choose among the endpoints
	// serving one model (internal/provider's velocity.go). A model is not one
	// machine: the same id is fanned over several endpoints that answer at very
	// different speeds for the same price, and this row is which of those
	// differences the session is willing to pay attention to.
	KeyRouting = "routing"

	// KeyLaneGuard is whether one slow answer may be rescued by asking a second
	// lane the same question while the first is still thinking (internal/lane's
	// watch.go). It is a separate row from [KeyRouting] because it answers a
	// different question: routing says WHICH lane a request prefers, and this
	// says whether a request that has already gone wrong is allowed to spend a
	// little more to come back on time.
	//
	// It is on by default. The extra call is capped at one per answer and at a
	// tenth of what the session spends, and it is off under price routing,
	// where nobody is buying seconds at all.
	KeyLaneGuard = "lane.guard"

	// KeyTaskAutoApprove is the countdown a proposed task waits before it
	// starts on its own (internal/session's task.go). It is named under `task.`
	// rather than beside the approval rows for the reason the guardian row is
	// named apart from them: this is not a rule about what may run, it is HOW
	// LONG YOU GET to say something about work that is going to run either way.
	//
	// Seconds, not a duration string, because the number is small and read at a
	// glance under a bar that is counting it down — "5" is the row, "5s" would
	// be the row pretending to be a unit it never varies.
	KeyTaskAutoApprove = "task.autoapprove_seconds"

	// KeyTaskRepairRounds is how many times a task whose work came back
	// INCOMPLETE is sent back to close the gaps before it lands as a failure
	// (internal/session's task_audit.go). It is named under `task.` beside the
	// countdown and the audit row because it answers their question in the third
	// currency: those say how long you get to redirect work and who checks it,
	// this says HOW MANY TIMES a piece of work that nearly landed is allowed to
	// finish itself.
	//
	// A count rather than a word, and 0 turns it off: the number is the whole
	// setting, and the person who wants the old behaviour — one pass, and a gap
	// is a dead node — writes 0 rather than learning a vocabulary. It is small on
	// purpose. Each round is another worker and another check on the same node's
	// bill, and a loop that could run five times is a loop that can spend five
	// times without anybody watching.
	KeyTaskRepairRounds = "task.repair_rounds"

	// KeyTaskParallel is how many tasks may RUN AT ONCE (internal/session's
	// task_run.go). It is named under `task.` with the countdown and the repair
	// count because it answers their question in the fourth currency: those say
	// how long you get to redirect work, how many times it may finish itself,
	// and whose hands it is in — this says HOW MUCH OF IT happens at the same
	// time.
	//
	// BLANK IS NO LIMIT, and that is the default. The number of tasks was never
	// the resource: what runs out is this machine's cores and memory — the two
	// rows below — and the model provider's rate limit, which the adapter reads
	// off 429s and adapts to on its own (internal/provider's limiter.go). This
	// row exists for the person who wants a number anyway, and it is a QUEUE and
	// not a refusal: work past the cap waits and starts when a slot frees.
	KeyTaskParallel = "task.parallel"

	// KeyTaskMaxLoad is the load average PER CORE at or above which no new task
	// is started (internal/session's task_pressure.go). It is one of the two
	// real ceilings the cap above stopped pretending to be.
	//
	// Per core, so that the number means the same thing on a laptop and on a
	// workstation: 1.5 is "half again as many runnable threads as there are
	// cores to run them", which is where the scheduler starts handing out slices
	// rather than running work and one more build makes every build slower. 0
	// turns the check off. It holds STARTS only — nothing already running is
	// ever touched, so the pressure drains on its own.
	KeyTaskMaxLoad = "task.max_load"

	// KeyTaskMinFreeMB is the floor of available memory below which no new task
	// is started (internal/session's task_pressure.go), in mebibytes, and the
	// other half of the ceiling above.
	//
	// It reads the kernel's MemAvailable — what a new process could actually get
	// — and not free memory, which on a working machine is near zero by design
	// because the page cache has the rest. 0 turns the check off.
	KeyTaskMinFreeMB = "task.min_free_mb"

	// KeyTaskModel is the model a task runs on when the conversation does not
	// name one for it (internal/session's taskmodel.go). It is named under
	// `task.` beside the countdown rather than among the `models.` rows because
	// it is not a rule about how a call is made: it is WHOSE HANDS the work that
	// leaves this conversation ends up in, which is the same subject the
	// countdown and the audit rows are about.
	//
	// Blank is the conversation's own model, which is what every task ran on
	// before the row existed: a node is the same worker doing the same job
	// somewhere quieter.
	KeyTaskModel = "task.model"

	// KeyTaskSettle is WHO DECIDES a task that finished but that nobody could
	// check (internal/session's task_contract.go). It sits under `task.` beside
	// the audit row because it is the other end of that row's question: the audit
	// says whether the work is checked at all, and this says what happens when
	// the check came back with nothing.
	//
	// `ask` is the default and puts the decision on the landed card, where the
	// person answers it with one press. `auto` hands it to the chat: the landing
	// note tells the model to read the work and settle it, and to come back only
	// when it genuinely cannot tell. Both leave the same three answers available
	// to both of them — this row changes who is asked first, and nothing else.
	KeyTaskSettle = "task.settle"

	// KeyWorkers is WHICH LEAF WORKERS ARE INSTALLED IN THIS PROFILE — the
	// roster (internal/config's workers.go states the law, cmd/aforge's
	// subharness.go applies it at registration).
	//
	// It is named under `work.` rather than `task.` because it is not about the
	// work you hand off from a conversation: a worker is what actually takes a
	// leaf, on every surface there is — a task, an adaptive run's node, a
	// headless `aforge run`, and the resident's continuation ladder. The `task.`
	// rows above are about one road onto that; this is about who is standing at
	// the end of all of them.
	//
	// A comma-separated set of worker names, and BLANK IS EVERY WORKER THIS
	// BUILD HAS — the default, and byte for byte the behaviour every profile had
	// before the row existed. Names this build does not know are said once on
	// stderr and ignored; a line that places no worker at all leaves the
	// generalist, which is never on the roster because it is never registered.
	KeyWorkers = "work.workers"

	// The web-search rows. They are four rather than one because they answer
	// four separable questions: WHERE a lookup goes, and the three credentials
	// that change what "where" can mean. A person with no key still searches —
	// internal/search's last rung takes none — so the keys are an upgrade and
	// never a prerequisite, and none of the three has to be answered for the
	// session to be able to look something up.
	KeySearchProvider = "search.provider"
	KeyExaKey         = "search.exaKey"
	KeyFirecrawlKey   = "search.firecrawlKey"
	KeyJinaKey        = "search.jinaKey"

	// The two rows that let a person connect their Google account
	// (internal/connect). They are a PAIR and neither is useful alone: an
	// application id names the application asking, and its secret proves the
	// ask came from it, so a build holding one of them can offer exactly
	// nothing. The session reads them together for that reason
	// ([GoogleOAuthClientAt]), and offers the connect tools only when both are
	// answered.
	//
	// Answering them REPLACES the registration this build ships with
	// (connect_defaults.go): a person or a deployment that wants its own
	// application asking — its own quota, its own consent screen, its own
	// revocation — writes it here or exports it, and nothing else changes.
	KeyGoogleOAuthClient = "google_oauth_client"
	KeyGoogleOAuthSecret = "google_oauth_secret"

	// The one row that lets a person replace the Slack application this build
	// ships with (connect_defaults.go). It is ONE ROW AND NOT A PAIR because
	// Slack's PKCE registration has no secret. An organisation that registers
	// its own internal application to escape the outside-Marketplace throttle
	// pastes that application's id here.
	KeySlackOAuthClient = "slack_oauth_client"

	// The four context-law knobs. Fill is how much of a model's window any
	// agent may use before compaction fires; the reserve is the room every
	// call keeps for its answer and its reasoning; the working set caps what
	// an agent keeps quoted in front of itself however large the window is;
	// and reuse caps how many times over it may re-send that working set.
	KeyContextFill       = "context_fill_pct"
	KeyCompletionReserve = "completion_reserve"
	KeyWorkingSet        = "working_set_tokens"
	KeyContextReuse      = "context_reuse_pct"
)


// ToolApprovalModes are the three answers the tool gate can be set to, in the
// order they widen: ask about everything, run everything, refuse everything.
// They are internal/approval's own words — the registry must not invent a
// fourth spelling for a decision that package already names.
var ToolApprovalModes = []string{"prompt", "allow", "deny"}

// The guardian row's two answers. It is a CHOICE and not a bool for the reason
// every other two-word row here is one: "off/on" is what the sheet renders and
// what the file holds, and a person reading their config.json back should find a
// word they chose rather than a `true` they have to remember the question for.
const (
	GuardianOff = "off"
	GuardianOn  = "on"
)

// GuardianModes lists them, off first — which is also the default, and the order
// the row widens in.
var GuardianModes = []string{GuardianOff, GuardianOn}

const (
	MouseOff = "off"
	MouseOn  = "on"
)

// MouseModes lists them, on first — which is also the default: hover, click
// and the wheel are the surface's own language. Selecting text does not die for
// it — ctrl+s hands the pointer to the terminal for as long as somebody is
// dragging with it, and ctrl+b copies from the keyboard — but a person who
// wants the terminal's plain drag at all times turns the row off.
//
// Shift+drag is the answer this hint used to give, and it was the wrong one:
// it is true, it is a fact about terminals rather than about this product, and
// the terminals that have it disagree about the modifier (Option in iTerm2).
// A key this surface owns is a key this surface can print.
var MouseModes = []string{MouseOn, MouseOff}

// DefaultMouse is on.
const DefaultMouse = MouseOn

const (
	TaskAuditOff = "off"
	TaskAuditOn  = "on"
)

// TaskAuditModes lists them, on first — which is also the default: a node
// that verifies its own work is the whole point of the verified frontier,
// and the audit's cost is the price of trusting what merges.
var TaskAuditModes = []string{TaskAuditOn, TaskAuditOff}

// DefaultTaskAudit is on.
const DefaultTaskAudit = TaskAuditOn

const (
	ReplyGuardOff = "off"
	ReplyGuardOn  = "on"
)

// ReplyGuardModes lists them, on first — which is also the default. A reply that
// has come apart is worth almost nothing and costs the whole of the next
// request, because it goes back into the conversation and the model reads its
// own soup before writing more.
var ReplyGuardModes = []string{ReplyGuardOn, ReplyGuardOff}

// DefaultReplyGuard is on.
const DefaultReplyGuard = ReplyGuardOn

// The two answers to [KeyTaskSettle]: who settles a task that finished with
// nobody able to say whether it holds.
const (
	// TaskSettleAsk puts it in front of the person, on the landed card.
	TaskSettleAsk = "ask"
	// TaskSettleAuto hands it to the chat, which reads the work and decides.
	TaskSettleAuto = "auto"
)

// TaskSettleModes lists them, ask first — which is also the default. Deciding
// on somebody's behalf is a thing they say yes to, never a thing they get by
// saying nothing.
var TaskSettleModes = []string{TaskSettleAsk, TaskSettleAuto}

// DefaultTaskSettle is ask.
const DefaultTaskSettle = TaskSettleAsk

// The two answers to [KeyTaskStart], and they are not two settings but one
// question asked once instead of on every `/task`: what a bare `/task` pays to
// find out before its one worker starts.
//
//	sized      one worker, with the sizing call read over the brief first. A yes
//	           from it arms that worker to hand the work out as it goes, once it
//	           has opened the material and found the width is real — so wide work
//	           runs wide without a planner ever being asked to guess at it.
//	single     one worker, and the sizing call is not made at all. The work can
//	           still divide itself, but only off what its own brief already says
//	           (internal/splitgate), because nothing was read over it.
//
// THERE WERE FOUR, AND BOTH THE RETIRED ONES NAMED A CARD OR A ROAD THAT IS
// GONE. `ask` went with the two-row chooser a yes from the sizing call used to
// raise; `adaptive` went with the planned-graph road itself, which a chat turn
// may no longer open at all. Neither retirement is allowed to be felt: a profile
// still holding either word reads as the default, silently, because [TaskStartAt]
// treats a word this build does not know as no answer at all — and being told
// that a preference set months ago is now an error is the one thing a retirement
// must never do.
// NEITHER RETIRED WORD IS SPELLED HERE, and `ask` set that precedent: a constant
// for a word nothing may write, nothing may offer and nothing may resolve to is a
// name the next reader has to be told is not a mode. The retirement needs no
// constant to work — it is [TaskStartModes] not containing the word, which is
// what the sheet, [writeChoice] and [TaskStartAt] all read.
const (
	TaskStartSized  = "sized"
	TaskStartSingle = "single"
)

// TaskStartModes lists what may be chosen, the default first. The retired word
// is not in it, and that absence is the whole mechanism — the sheet's choices,
// the writer's validation and the resolver all read this one list.
var TaskStartModes = []string{TaskStartSized, TaskStartSingle}

// DefaultTaskStart is sized: one worker that knows whether it is allowed to
// divide, which is the shape the measured road is built around.
const DefaultTaskStart = TaskStartSized

// The timestamps row's three answers, and they are a LADDER rather than three
// unrelated pictures: each rung draws strictly less of the clock than the one
// above it.
//
//	footers      the turn footer (· 14:02 · 2m12s · 3 tools · $0.04 ·) AND the
//	             gap and day marks, which are what a footer is read against
//	separators   only the marks: where the conversation was put down for ten
//	             minutes, and where a day ended
//	off          no clock at all
//
// The middle rung exists because the two features answer the same question at
// different costs: a reader who wants to know that yesterday's exchange was
// yesterday does not necessarily want a line of figures under every turn, and a
// footer with no day mark above it would be a time with no date.
const (
	TimestampsFooters    = "footers"
	TimestampsSeparators = "separators"
	TimestampsOff        = "off"
)

// TimestampModes lists them richest first, which is also the default order the
// row widens in.
var TimestampModes = []string{TimestampsFooters, TimestampsSeparators, TimestampsOff}

const (
	WorkFold = "fold"
	WorkOpen = "open"
)

var WorkModes = []string{WorkFold, WorkOpen}

const DefaultWork = WorkFold

// DefaultTimestamps is the footers. When a turn took two minutes and cost four
// cents, those are facts about work the person paid for, and a transcript that
// never says when anything happened cannot be read back a day later.
const DefaultTimestamps = TimestampsFooters

const (
	MemoryOff = "off"
	MemoryOn  = "on"
)

// MemoryModes lists them, on first — which is the default. A colleague who
// forgot every preference you stated the moment you closed the window would be
// one you had to brief again every morning, and the calls that carry this are
// the cheapest the surface makes.
var MemoryModes = []string{MemoryOn, MemoryOff}

// DefaultMemory is on.
const DefaultMemory = MemoryOn

// The background-checks row's two answers.
const (
	BackgroundOff = "off"
	BackgroundOn  = "on"
)

// BackgroundModes lists them, on first — which is the default. Something you
// asked to happen every morning is something you asked to happen on the
// mornings you do not open a terminal, and a person who wanted it only while
// they were sitting there says so here.
var BackgroundModes = []string{BackgroundOn, BackgroundOff}

// DefaultBackground is on.
const DefaultBackground = BackgroundOn

// The routing row's three answers. They are spelled here rather than imported
// from internal/provider for the reason [DocumentEngines] is: a settings key's
// vocabulary is a string on disk, and it must not change because a package
// renamed a constant.
const (
	// RoutingLatency asks for the currently-fastest endpoint.
	RoutingLatency = "latency"
	// RoutingPrice asks for the cheapest one that can serve the request.
	RoutingPrice = "price"
	// RoutingOff sends no preference at all, and stops measuring with it.
	RoutingOff = "off"
)

// RoutingModes lists them, latency first — which is also the default: a chat
// session is a person waiting, and the endpoint that answers soonest is the one
// they are asking for.
var RoutingModes = []string{RoutingLatency, RoutingPrice, RoutingOff}

// DefaultRouting is latency.
const DefaultRouting = RoutingLatency

// ── WHICH MACHINE, NOT WHICH MODEL ──────────────────────────────────────────
//
// A model id is an address and a LANE is one of the machines behind it. The
// same id is served by a dozen endpoints that differ by seven times on the wait
// before the first word and by twelve times on how fast they write, at roughly
// the same price — so the lane a request lands on is a bigger difference than
// most model changes, and this is where a person says something about it.
//
// The rows are per slot (`lane.talk`, `lane.work`, …) for the reason the model
// rows are: the conversation you are watching and a task worker nobody is
// waiting on want different answers, and one row for both would be a setting
// that is wrong half the time. [KeyRouting] is untouched and still means what
// it meant: it is about the whole preference, and this is about one machine.

const (
	// LaneAuto lets the belief pick a lane per answer. It is the default and it
	// is what an unset row reads as.
	LaneAuto = "auto"
	// LaneOpenRouter asks for no lane at all and lets the router balance on
	// price, which is what this build did before it held an opinion.
	LaneOpenRouter = "openrouter"
)

// LaneSlotTalk is the ONE slot the settings registry carries a row for: the
// conversation. Every slot has a key — a task worker can be pinned by the same
// grammar — but a panel with nine lane rows on it would be a panel about
// endpoints rather than about models, and the other eight are set from the
// picker on the model they belong to.
const LaneSlotTalk = "talk"

// LaneSettingKey is the row holding one slot's lane: `auto`, `openrouter`, or a
// lane's own name as the wire spells it.
func LaneSettingKey(slot string) string { return "lane." + slot }

// LaneBorrowKey is the row beside it: whether a PINNED lane may still be
// borrowed away from when it is slow. It is a second key rather than a fourth
// word in the first because the two are independent — borrowing means nothing
// under `auto` — and because a pin that quietly stopped being a pin the day
// somebody turned rescuing on would be a promise this build did not keep.
func LaneBorrowKey(slot string) string { return LaneSettingKey(slot) + ".borrow" }

// LaneAt is the lane one slot is held to, default [LaneAuto]. A blank or
// unreadable row reads as auto rather than as a pin nobody can see.
func LaneAt(profileDir, slot string) string {
	if value, ok := persistedString(profileDir, LaneSettingKey(slot)); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return LaneAuto
}

// LaneBorrowAt is whether a pinned slot may be borrowed away from when its lane
// is slow. It is false unless somebody said so: a pin means the machine they
// named, and widening it on their behalf is not this row's to do.
func LaneBorrowAt(profileDir, slot string) bool {
	value, ok := persistedBool(profileDir, LaneBorrowKey(slot))
	return ok && value
}

// LanePinned is the lane named by a slot's row, and false when the row names no
// machine — which is every reading of `auto` and of `openrouter`.
func LanePinned(profileDir, slot string) (string, bool) {
	value := LaneAt(profileDir, slot)
	switch strings.ToLower(value) {
	case LaneAuto, LaneOpenRouter, "":
		return "", false
	}
	return value, true
}

// SetLane writes one slot's lane. An empty word clears the row back to auto,
// which is the same thing said two ways and both of them arrive here.
func SetLane(profileDir, slot, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, LaneAuto) {
		return writeProfileValue(profileDir, LaneSettingKey(slot), LaneAuto)
	}
	if strings.ContainsAny(value, " \t") {
		return fmt.Errorf("a lane is one name, like cloudflare")
	}
	return writeProfileValue(profileDir, LaneSettingKey(slot), value)
}

// SetLaneBorrow writes the borrow flag beside a pin.
func SetLaneBorrow(profileDir, slot string, borrow bool) error {
	return writeProfileValue(profileDir, LaneBorrowKey(slot), borrow)
}

// DefaultLaneGuard is on: a slow answer is worth one extra call to rescue, and
// the budget around it is what keeps that true rather than a hope.
const DefaultLaneGuard = true

// LaneGuardAt resolves the speed-guard row. A malformed row reads as the
// default rather than quietly turning the rescue off — a person who never
// touched this row has not asked to wait.
func LaneGuardAt(profileDir string) bool {
	if value, ok := persistedBool(profileDir, KeyLaneGuard); ok {
		return value
	}
	return DefaultLaneGuard
}

// LaneRowWord is one slot's lane row as a person reads it: `auto`,
// `openrouter`, `pinned: cloudflare`, or `pinned: cloudflare, borrow when
// slow`. It is here rather than in a surface because the same words are what
// [WriteLaneRow] reads back, and two spellings of one row is how a row stops
// round-tripping.
func LaneRowWord(profileDir, slot string) string {
	name, pinned := LanePinned(profileDir, slot)
	if !pinned {
		if strings.EqualFold(LaneAt(profileDir, slot), LaneOpenRouter) {
			return LaneOpenRouter
		}
		return LaneAuto
	}
	if LaneBorrowAt(profileDir, slot) {
		return "pinned: " + name + ", " + laneBorrowWord
	}
	return "pinned: " + name
}

// laneBorrowWord is the tail that says a pin may be left when it is slow. It is
// stated once because it is written on the row, read back off it, and said in
// the manual.
const laneBorrowWord = "borrow when slow"

// WriteLaneRow takes what [LaneRowWord] says and puts it back: `auto`,
// `openrouter`, a bare lane name, or either `pinned:` form.
func WriteLaneRow(profileDir, slot, raw string) error {
	value := strings.TrimSpace(raw)
	borrow := false
	if at := strings.LastIndex(strings.ToLower(value), laneBorrowWord); at >= 0 {
		borrow = true
		value = strings.TrimRight(strings.TrimSpace(value[:at]), ",")
		value = strings.TrimSpace(value)
	}
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "pinned:"))
	if err := SetLane(profileDir, slot, value); err != nil {
		return err
	}
	return SetLaneBorrow(profileDir, slot, borrow)
}

// The two tier names internal/roles resolves auxiliary calls under. They are
// spelled here rather than imported for the reason [DocumentEngines] is: the
// registry is a settings surface, and a settings key is a string on disk that
// must not change spelling because a package renamed a constant.
const (
	ModelTierLow  = "low"
	ModelTierHigh = "high"
	// ModelTierReflex is the per-turn tier ([roles.TierReflex]).
	ModelTierReflex = "reflex"
	// ModelTierMastermind is the thinking tier ([roles.TierMastermind]).
	ModelTierMastermind = "mastermind"
)

// ModelTiers lists the four tier words in the order a settings surface renders
// them, cheapest first. It is [roles.Tiers] spelled as the words on disk, and
// [tierKeyFor] is total over it.
var ModelTiers = []string{ModelTierReflex, ModelTierLow, ModelTierHigh, ModelTierMastermind}

// THE SHIPPED CREW. All four tiers arrive pointed at a model, and the four
// together are exactly the `balanced` preset (crew.go) — which is what makes the
// crew row read "balanced" on a profile nobody has touched instead of reading
// "custom" about its own defaults.
//
// Each is a bare OpenRouter id, spelled ONCE here and read by every caller
// through [TierModelAt], so the model this build considers near-free is one
// string rather than a figure repeated in a row, a resolver and a page.
//
// Blank is still an answer on every one of them: a row a person emptied on
// purpose reads empty and the roles on it follow the model the person is talking
// to, which is [roles.Resolve]'s floor. UNSET and CLEARED are different answers
// here, and that distinction is the whole mechanism ([TierModelAt] says how).
const (
	DefaultReflexModel = "nex-agi/nex-n2-mini"
	DefaultLowModel    = "deepseek/deepseek-v4-flash"
	DefaultHighModel   = "qwen/qwen3.8-27b"
	// The mastermind ships with a LEVEL on it, which no other tier does. The
	// balanced crew's whole shape is "one model that thinks, cheaper ones that
	// work", and a mastermind with no level asked for is the thinking half not
	// actually thinking.
	DefaultMastermindModel = "moonshotai/kimi-k3:low"
)

// DocumentEngines are the four rungs AFORGE_DOC_ENGINE accepts.
var DocumentEngines = []string{"auto", "local", "free", "ocr"}

// SearchProviderAuto is the row's default: no pin, and internal/search walks
// its own ladder — the keyed plug when its key is present, the zero-key plug
// otherwise. It is spelled here rather than as the empty string because a
// choice row has to have a word for "I did not choose", and "" would render as
// a blank cell nobody can tell from a broken read.
const SearchProviderAuto = "auto"

// SearchProviders are the answers the search row accepts.
//
// The names are STRINGS HERE and not [search.RegisteredSearch], for the reason
// [DocumentEngines] is a literal: a settings value is a word on disk, and a
// list derived from a registry would silently change what a person's saved
// answer means the day a plug is renamed or one is added. The cost is that a
// new plug needs a line here to be pinnable — which is the right cost, because
// a plug nobody can name in the sheet is still reachable through auto.
// jina-search is named here too so every registered search plug is pinnable.
var SearchProviders = []string{SearchProviderAuto, "exa", "firecrawl", "jina-search", "duckduckgo"}

// OperatorEnvPins is the explicit allowlist of environment variables that are
// plumbing rather than settings: endpoints, credentials, profile roots, and
// planner internals. They are listed read-only in the sheet's environment
// footer and never become editable rows.
var OperatorEnvPins = []string{
	"AFORGE_BASE_URL",
	// AFORGE_HOME moves the graph, workspace, craft repository and resident
	// lease somewhere else in one word. It is plumbing rather than a setting
	// for the plainest reason there is: it decides which store the sheet
	// itself is being read out of.
	"AFORGE_HOME",
	// AFORGE_RELAY points a headless peer at the relay this host pairs through
	// (internal/pair). It is an address, so it is plumbing for the same reason
	// AFORGE_BASE_URL is.
	"AFORGE_RELAY",
	// AFORGE_FURROW names a furrow to use instead of the one aforge carries
	// (internal/furrow). A path to a program is plumbing.
	"AFORGE_FURROW",
	// The three site-attribution pins — a URL, an app name, a category list —
	// used to sit here, and they are gone rather than moved: the OpenRouter app
	// this binary reports as is a constant in internal/provider that nothing
	// reads from the environment any more. A footer that still listed them
	// would be promising an override that does nothing, which is worse than
	// saying nothing at all.
	"AFORGE_PROFILE_DIR",
	// The two pins on the model-call log (internal/calllog). AFORGE_CALL_LOG
	// switches it off or moves the file; AFORGE_CALL_LOG_BODIES adds the whole
	// request and response to every line. Plumbing rather than settings rows,
	// and the second one emphatically so: a row in the sheet offering to record
	// every prompt a person ever sends is not a preference, it is a decision
	// somebody should have to make on purpose, in a shell, for one run.
	"AFORGE_CALL_LOG",
	"AFORGE_CALL_LOG_BODIES",
	"AFORGE_MODELS",
	"AFORGE_REASONING",
	"AFORGE_EXEC_REASONING",
	// The three `aforge exec` walls. They are plumbing rather than settings
	// for the reason the node budget is: they are the ceilings one headless
	// invocation runs under, set by the harness that made the call, and the
	// preference the product actually has an opinion about is the daily rail.
	// A sheet row offering to persist them would be offering to cap a
	// conversation nobody is holding.
	"AFORGE_EXEC_TURNS",
	"AFORGE_EXEC_BUDGET",
	"AFORGE_EXEC_TIMEOUT",
	// The two walls an UNATTENDED conversation carries its own work on under
	// (--max-hours / --max-cost, internal/session's principal.go). They are
	// plumbing for exactly the reason the three above are: they are the ceilings
	// ONE launch runs under, named by whoever started it, and a sheet row
	// offering to persist them would be offering to make every future
	// conversation an unattended one. The preference the product has an opinion
	// about is the daily rail, and it is a row already.
	"AFORGE_MAX_HOURS",
	"AFORGE_MAX_COST",
	"AFORGE_SPINE_SAMPLES",
	"AFORGE_MAX_DEPTH",
	"AFORGE_NODE_BUDGET",
	"AFORGE_SKILL_DIR",
	"AFORGE_SKILLS_BIN",
	"AFORGE_RTK",
	"AFORGE_RTK_BIN",
	"AFORGE_PREAUTHORIZE_SPEND",
	// AFORGE_SWE_MAX_COST is the dollar ceiling one coding-pipeline leaf may
	// spend inside the vendored engine. It is plumbing rather than a setting
	// for the same reason the node budget is: it is a number handed to a
	// subprocess, not a preference the product has an opinion about, and the
	// preference that governs spending is the daily rail.
	"AFORGE_SWE_MAX_COST",
	// AFORGE_SWEPRO is not a setting anybody would ever want to turn on: it
	// tells the binary, before it has read anything else, that this process
	// is not aforge at all but the vendored swe engine (cmd/aforge/swepro.go).
	// The subharness sets it on the children it spawns; a user who set it
	// would simply lose their own program.
	"AFORGE_SWEPRO",
	// AFORGE_WIRE_LOG names a file the surface appends one line per second of
	// byte-meter readings to (internal/wirelog): a developer's instrument for
	// the SSH-smoothness story, with no settings row and no slash command,
	// because there is no question a person using aforge would ask that it
	// answers. It is plumbing: diagnostic output a preference sheet has no
	// business persisting.
	"AFORGE_WIRE_LOG",
	// AFORGE_GROWTH_GATE is the growth governor's rollback switch
	// (internal/resident/grow.go): set to 0 and the governor keeps its three
	// free checks and never asks the paid satisfaction question. It is
	// plumbing for the reason AFORGE_SWEPRO is — a wave's escape hatch, not a
	// preference — and it has the same lifetime: it disappears once the gate
	// has proven itself, which is exactly the lifetime a persisted setting
	// must not have.
	"AFORGE_GROWTH_GATE",
	// AFORGE_SWARM is cooperative decomposition's ESCAPE HATCH, and it used to
	// be its arming switch (config.go's Config.Swarm). ON IS NOW THE DEFAULT
	// (config.go's DefaultSwarm): a resident leaf carries request_split, a v3
	// task's worker carries divide_work, and the sizing judgments read measured
	// overrun base rates. Set it to 0 and the tree is byte-identical to before
	// the wave. It is plumbing for the reason AFORGE_SWEPRO is — it decides
	// which decomposition doctrine the binary runs, not a preference the
	// product has an opinion about — and it has the same lifetime: it
	// disappears when nobody has a reason to turn the default off any more,
	// which is exactly the lifetime a persisted setting must not have.
	"AFORGE_SWARM",
	// AFORGE_SPLITGATE is the split gate's rollback switch
	// (internal/splitgate, read by cmd/aforge/cooperative.go and by
	// internal/session's task_divide.go): set to 0 and a request to divide is
	// taken at its word instead of being weighed against the evidence it names.
	// It matters more now that swarm is the default, because the gate is what
	// makes that default free: it is the thing that refuses narrow work. It is
	// plumbing for the reason AFORGE_SWARM is — a wave's escape hatch while the
	// gate proves itself against real runs, not a preference — and it has the
	// same lifetime: it disappears when the gate has earned the last word,
	// which is exactly the lifetime a persisted setting must not have.
	"AFORGE_SPLITGATE",
	// AFORGE_MECHANISM names which coordination mechanism the binary arms —
	// today its one recognized word is `quorum`, which sets Config.Quorum the
	// same way AFORGE_QUORUM does (config.go). It is plumbing for the reason
	// its siblings are — it picks a doctrine under benchmark, not a
	// preference — and it shares their lifetime: it disappears when one
	// mechanism has won.
	"AFORGE_MECHANISM",
	// AFORGE_QUORUM is the two-verifier gate's arming switch
	// (config.go's Config.Quorum): on, a passed deliverable is independently
	// verified by two cheap validators before it commits; off, the judge's
	// pass is the final word. Same lifetime as AFORGE_SWARM.
	"AFORGE_QUORUM",
	// The three numbers the response boundary reads (internal/taxonomy, and
	// [ResponseAttemptsAt] below). They are plumbing rather than rows for the
	// reason the context-budget pins are: nobody sets them to express a
	// preference, they are turned when a specific provider is behaving badly or
	// when a run is being held to a price, and the sheet already has the two
	// rows a person actually budgets with — the daily rail and the repair count.
	"AFORGE_RESPONSE_ATTEMPTS",
	"AFORGE_RESPONSE_LIFT_AFTER",
	"AFORGE_RESPONSE_LIFT_CAP",
}

// Defaults the registry owns beyond the ones config.go already declares.
const (
	// DefaultPlanConsentUSD is where ambition stops being cheap. Below it a
	// plan simply runs, because asking about a small errand is the nagging
	// nobody wants; above it the user is quoted a count and a price and gets to
	// say no first. It is deliberately far under the daily rail: the rail is a
	// stop after the fact, and this is the moment before.
	//
	// IT WAS $3 AND $3 IS THE PRICE OF AN ORDINARY PIECE OF WORK, so the
	// question fired on nearly every plan and became a keystroke the person
	// owed rather than a decision they made — which is the failure mode a
	// consent gate cannot survive, because a question asked every time is a
	// question nobody reads. At $100 the gate fires on the plans a person
	// would genuinely want to see priced first. 0 never asks.
	DefaultPlanConsentUSD = 100.0

	// DefaultTenureAfter is the clean-firing count a standing charter needs
	// before it earns tenure.
	DefaultTenureAfter = 3

	// DefaultAttribution signs by default, because the signature is provenance:
	// work the user did not type should be readable as such by whoever reads
	// the history later. One row turns it off.
	DefaultAttribution = true

	// The divider clamps so neither pane can be set into uselessness. The TUI
	// reads these so the drag, the [ ] nudge, and the sheet agree.
	DefaultSplitPct = 80
	MinSplitPct     = 25
	MaxSplitPct     = 85

	// DefaultHistoryEnabled remembers what was typed, because a prompt is the
	// most expensive sentence in the product to re-type and the up arrow is the
	// cheapest way to get it back. The file is a recall list and nothing else —
	// internal/history caps it and never sends it anywhere.
	DefaultHistoryEnabled = true

	// DefaultDraftPersist keeps the unsent sentence across a restart, for the
	// reason a text field in any other application does: the draft is the
	// PERSON's, not the session's, and losing it to a crash or a closed window
	// is the surface throwing away the only thing on screen it did not write.
	DefaultDraftPersist = true

	// DefaultToolApprovalMode asks. It is the only defensible default for a
	// gate: a fresh install that ran every tool the model asked for would be
	// deciding, on the person's behalf, that nothing needs deciding. Widening
	// it is one row; narrowing it after something ran is not possible.
	DefaultToolApprovalMode = "prompt"

	// DefaultGuardian is off, and it is the only defensible default for the same
	// reason the row above asks: this one hands the answer to a model. A gate
	// that answers on your behalf must be something you turned on, not something
	// you failed to notice — so a fresh install makes no guardian call at all,
	// and the person who wants fewer questions opts into one.
	DefaultGuardian = GuardianOff

	// DefaultSpendRailUSD is 0 — no per-session ceiling. The rail that is on by
	// default is the DAILY one, because that is the number a person actually
	// budgets; a session ceiling is for the sitting somebody wants to box in,
	// and a default would box in every sitting at a number nobody chose.
	DefaultSpendRailUSD = 0.0

	// DefaultTaskAutoApprove is five seconds, and the direction it is wrong in
	// is the whole choice. A task proposal is not a permission question — the
	// model has already groomed the work and the brief, and the countdown is
	// the person's window to REDIRECT it or wave it off. A long countdown makes
	// every task a keystroke the person owes; no countdown at all would make
	// the surface a gate the work waits behind. Five seconds is long enough to
	// read a title and a two-line summary and reach for a key, and short enough
	// that ignoring it is a decision rather than a wait.
	DefaultTaskAutoApprove = 5

	// DefaultTaskRepairRounds is ONE, and one is the whole argument. The failure
	// this exists for is a piece of work that came back nearly right — a report
	// covering ten of the eleven companies it was asked for — and died, leaving
	// the person to type the whole task again by hand. One round is what turns
	// that into a task that finishes: the same worktree, the same brief, and the
	// gaps in front of a fresh worker.
	//
	// It is not two, and it is not five. A second round buys much less than the
	// first — work that is still wrong after being told exactly what is missing
	// is work whose brief is wrong, and no number of rounds fixes a brief — while
	// every round costs another worker and another check on the same node. So the
	// default closes the near-misses and stops.
	DefaultTaskRepairRounds = 1

	// DefaultTaskParallel is NO LIMIT, and the change of mind it records is
	// worth the sentence. The frontier used to hold two tasks at once, and two
	// was a guess standing in for a resource nobody had measured — it left a
	// sixteen-core machine idle behind a queue of ready work, and it was still
	// one too many on a laptop already carrying somebody else's compile. The
	// number of tasks is not what runs out. So the count became a row a person
	// may set when they want one, the ceilings became the two below, and the
	// default is the honest one: as much as the machine and the provider will
	// carry.
	DefaultTaskParallel = 0

	// DefaultTaskMaxLoad is one and a half runnable threads per core, which is
	// the number an earlier incident settled on: aforge pinning a laptop's fan
	// by running real compilers and real test suites beside each other
	// (internal/exec's governor.go carries the same figure for the same class of
	// work). Below it the machine is busy; at it, the scheduler is handing out
	// slices and one more task makes every task slower.
	DefaultTaskMaxLoad = 1.5

	// DefaultTaskMinFreeMB is a gibibyte and a half, which is roughly what one
	// more task needs to be worth starting: a child agent, a git worktree, and
	// whatever build it is about to run. Starting one under that floor is how a
	// machine reaches the OOM killer, and what the OOM killer takes is not the
	// task that was too many — it is whichever process was largest, which on a
	// developer's machine is usually theirs.
	DefaultTaskMinFreeMB = 1536

	// DefaultConsentTimeout is ten seconds, and it is a different number from
	// the one above because it is a different KIND of clock. The task countdown
	// runs toward the permissive answer, so it is kept short enough to notice.
	// This one runs toward the refusal: at expiry the call is denied, the model
	// is handed a refusal it can act on, and nothing has happened to the disk.
	// So it can afford to be the longer of the two — ten seconds is long enough
	// to read a command and a rule — and its cost when it fires is one call the
	// model has to ask for again.
	DefaultConsentTimeout = 10

	// DefaultSearchProvider pins nothing. Auto is the only default that stays
	// right as a person's keys change: the day they paste an Exa key the
	// searches move to Exa without a second row being touched, and the day it
	// expires they keep searching instead of getting an error from a plug they
	// pinned six months ago and forgot.
	DefaultSearchProvider = SearchProviderAuto

	// DefaultTaskColumn stands the v3 task column up on a session that has never
	// been told otherwise: THE COLUMN TEACHES BY EXISTING — it is how a person
	// finds out that this chat runs work you can walk away from. Once they put it away we never stand it up again on
	// their behalf — the strip is what keeps running work reachable from a frame
	// with no column on it (internal/tui3's taskstrip.go).
	DefaultTaskColumn = true

	// DefaultQuickSwitch makes the switcher's chord SWITCH rather than ask, on a
	// profile that has never said otherwise. It is the behaviour every window
	// manager and browser taught: the fast gesture is the default and nobody
	// opts into it. The slower card is still one arrow key away — touching
	// anything but the chord converts the fading card into the browsing one —
	// so the default costs a careful reader nothing but `esc`.
	DefaultQuickSwitch = true

	// DefaultHints shows the v3 chat's tips to a profile that has never said
	// otherwise. The tips retire themselves the moment each is acted on, so the
	// default costs a veteran one line per gesture they already know, once.
	DefaultHints = true
)

// Setting is one row: what it is called, what it reads now, and what happens
// when the user changes it.
type Setting struct {
	Key      string
	Category string
	Label    string
	Hint     string
	Kind     SettingKind

	// Slot is the model role a SettingModel row fronts.
	Slot string

	// Choices lists the accepted values of a SettingChoice row.
	Choices []string

	// Env pins the row from the environment: while it is set the value is
	// read-only and the surface says which variable owns it.
	Env string

	// EnvDefault only seeds a value the user has never chosen — the model
	// slots work this way, so a pinned default never freezes the row.
	EnvDefault string

	// PrefsField names the chat-prefs json field this row fronts when the
	// value lives beside the graph rather than in the profile config.
	PrefsField string

	// EmptyLabel reads for a text row whose value is unset.
	EmptyLabel string

	// Secret marks a row whose value is a credential. Such a row READS MASKED
	// — its own reader returns the mask, so every surface that renders a value
	// renders the mask without having to know the row is special — and its
	// writer treats the mask as "unchanged" (see writeCredential), so an editor
	// that opens on the displayed value and is saved unedited cannot overwrite
	// the key with a row of bullets.
	//
	// The flag is here rather than a [SettingKind] because a credential edits
	// exactly like text: the difference is what it shows, not what it accepts,
	// and a fourth kind would make every switch in every surface grow an arm
	// that did the same thing SettingText already does. A surface that wants to
	// suppress its own echo while typing reads this.
	Secret bool

	read    func() string
	write   func(string) error
	receipt func() string
}

// Receipt is the dim fact that belongs beside this row's value — today's spend
// beside the day's ceiling, the price tier beside a model. It is a receipt and
// never a second value: it is derived from a live read, it is never editable,
// and a row with nothing true to add returns the empty string rather than a
// placeholder (13, and 10.2.8's rule against inventing a reading).
func (s Setting) Receipt() string {
	if s.receipt == nil {
		return ""
	}
	return strings.TrimSpace(s.receipt())
}

// Value is the row's current reading, already formatted for display.
func (s Setting) Value() string {
	if s.read == nil {
		return ""
	}
	value := s.read()
	if value == "" && s.EmptyLabel != "" {
		return s.EmptyLabel
	}
	return value
}

// Accepts is what this row will take, in the words its own writer refuses in.
//
// It lives here rather than in the surfaces because it is the WRITER'S sentence
// read forwards: "that's not a dollar amount" and "an amount in dollars" are one
// fact, and a caller that spelled the second for itself would drift from the
// first the day a parser widened. A panel with a picker never asks; a tool
// putting the row in front of a model that has to type a value does, and it is
// the difference between one call and three.
func (s Setting) Accepts() string {
	switch s.Kind {
	case SettingModel:
		return "a model id, like anthropic/claude-opus-5"
	case SettingDollars:
		return "an amount in dollars, like 5 or 2.50 — or none for no limit"
	case SettingDuration:
		return "a length of time, like 20m or 4h, or 0"
	case SettingPercent:
		return "a percentage"
	case SettingCount:
		return "a whole number"
	case SettingBool:
		return "on or off"
	case SettingChoice:
		return "one of: " + strings.Join(s.Choices, ", ")
	}
	if s.EmptyLabel != "" {
		return "text, or blank for " + s.EmptyLabel
	}
	return "text"
}

// PinnedBy names the environment variable holding this row read-only.
func (s Setting) PinnedBy() (string, bool) {
	if s.Env == "" {
		return "", false
	}
	if strings.TrimSpace(os.Getenv(s.Env)) == "" {
		return "", false
	}
	return s.Env, true
}

// Apply validates, persists, and lands the live effect. A pinned row refuses
// calmly rather than writing a value the environment would keep overriding.
func (s Setting) Apply(raw string) error {
	if name, pinned := s.PinnedBy(); pinned {
		return fmt.Errorf("%s is set by %s", s.Label, name)
	}
	if s.write == nil {
		return fmt.Errorf("%s cannot be changed here", s.Label)
	}
	return s.write(raw)
}

// SettingGroup is one rendered category.
type SettingGroup struct {
	Title string
	Rows  []Setting
}

// SettingsOptions supplies the live seams the registry cannot reach on its
// own: the running model slots and the surface's own divider preference.
type SettingsOptions struct {
	ProfileDir string

	// ModelValue and SetModel front the model slots. They stay in the chat
	// prefs file; the registry does not migrate them.
	ModelValue func(slot string) string
	SetModel   func(slot, slug string) error

	// SplitPct and SaveSplitPct front the chat/task divider.
	SplitPct     func() int
	SaveSplitPct func(pct int)

	// RoleModel answers what a router role is bound to right now, for the roles
	// the engine holds no client for. Nil is the honest state of this build —
	// nothing resolves verify or scribe yet — and those rows then read as the
	// role they follow instead of as a guess.
	RoleModel func(role string) (string, bool)

	// SpentTodayUSD is the day's spend, for the receipt beside the day's
	// ceiling. The bool separates "spent nothing" from "nobody counted"
	// (10.2.8); nil leaves the receipt off rather than printing $0.00.
	SpentTodayUSD func() (float64, bool)

	// SpentThisSessionUSD is what the conversation in front of the reader has
	// spent, for the receipt beside its own ceiling. Nil on every door that is
	// not a live conversation, and the bool carries the same distinction
	// [SettingsOptions.SpentTodayUSD] carries: not counted is not zero.
	SpentThisSessionUSD func() (float64, bool)

	// ModelCost is what the provider table knows about one model's price. It is
	// a hint beside a model row and never a filter: an unpriced model is the
	// offline case, not a bad model. [ModelCostHint] is the derivation the
	// wiring lane hands in.
	ModelCost func(slug string) string

	// BackgroundChecks is this machine's own scheduler as internal/standing
	// drives it: the launchd agent or systemd user timer that runs `aforge tick`
	// every [standing.Interval] so standing items are checked with no window
	// open. It is what the `standing.background` row reads and writes.
	//
	// NIL LEAVES THE ROW OUT ALTOGETHER, which is this codebase's law about a
	// capability that cannot work rather than a shortcut: a Windows host, a
	// build with no store, a registry built to read defaults out of a profile
	// nobody has — none of them has a timer to turn on, and a row present and
	// refusing every write would be a switch wired to nothing. The divider row
	// is absent for the same reason ([Settings.build]).
	BackgroundChecks standing.Watch

	// Applied fires after a row is successfully written, so a process holding
	// its own copy of a value can honor the change without waiting for a
	// relaunch.
	Applied func(key string)
}

// Settings is the built registry.
type Settings struct {
	options SettingsOptions
	rows    []Setting
}

// NewSettings builds the registry against one profile directory and whatever
// live seams the caller can supply. Missing seams make a row read-only rather
// than absent, so the sheet always shows the complete surface.
//
// ONE ROW IS THE EXCEPTION and [Settings.build] states it where it happens: the
// divider has nowhere to be written without [SettingsOptions.SaveSplitPct], and
// a read-only divider is not a row somebody can read — it is a control whose
// only behaviour is to refuse.
func NewSettings(options SettingsOptions) *Settings {
	registry := &Settings{options: options}
	registry.rows = registry.build()
	for index := range registry.rows {
		registry.rows[index].write = registry.announce(registry.rows[index].Key, registry.rows[index].write)
	}
	return registry
}

func (s *Settings) announce(key string, write func(string) error) func(string) error {
	if write == nil {
		return nil
	}
	return func(raw string) error {
		if err := write(raw); err != nil {
			return err
		}
		if s.options.Applied != nil {
			s.options.Applied(key)
		}
		return nil
	}
}

// Rows returns every row in category order.
func (s *Settings) Rows() []Setting {
	return append([]Setting(nil), s.rows...)
}

// Row finds one row by key.
func (s *Settings) Row(key string) (Setting, bool) {
	for _, row := range s.rows {
		if row.Key == key {
			return row, true
		}
	}
	return Setting{}, false
}

// Groups returns the rows grouped for rendering, skipping empty categories.
func (s *Settings) Groups() []SettingGroup {
	groups := make([]SettingGroup, 0, len(SettingCategories))
	for _, title := range SettingCategories {
		group := SettingGroup{Title: title}
		for _, row := range s.rows {
			if row.Category == title {
				group.Rows = append(group.Rows, row)
			}
		}
		if len(group.Rows) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// EnvironmentPins lists the operator plumbing currently set, for the sheet's
// read-only footer.
func (s *Settings) EnvironmentPins() []string {
	set := make([]string, 0, len(OperatorEnvPins))
	for _, name := range OperatorEnvPins {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			set = append(set, name)
		}
	}
	return set
}

// ModelSettingKey is the registry key fronting one model slot.
func ModelSettingKey(slot string) string { return "model." + slot }

// build is the whole sheet, in the order it is read.
//
// Within a group the order is how often a person touches a row, not the order
// the rows were written or the order they happen to persist in: the day's
// ceiling before the ask-first threshold before the practice carve-out, the
// conversation model before the two nobody has ever changed. Between groups the
// order is [SettingCategories]. Nothing here announces either — 15 — the
// sequence is the sequence.
func (s *Settings) build() []Setting {
	dir := s.options.ProfileDir
	slots := ModelSlots()
	rows := make([]Setting, 0, len(slots)+10)

	for _, slot := range slots {
		rows = append(rows, s.modelRow(slot))
	}

	rows = append(rows,
		// The two rows that pick what reads a thing rather than what runs it.
		// They sit with the models because that is the question they answer.
		Setting{
			Key: KeyVisionModel, Category: CategoryModels, Kind: SettingText,
			Label: "looking", Env: "AFORGE_VISION_MODEL", EmptyLabel: "automatic",
			Hint: "the model that looks at images. Leave it blank and aforge picks one that can see. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return VisionModelAt(dir) },
			write: func(raw string) error { return writeText(dir, KeyVisionModel, raw) },
		},
		// How hard the models think, when nothing nearer to the work has said.
		// It sits with the models rather than with spending because that is the
		// question a person is answering when they touch it — the money is a
		// consequence and not the subject.
		Setting{
			Key: KeyEffort, Category: CategoryModels, Kind: SettingChoice,
			Label: "thinking", Choices: EffortChoices,
			Hint: "how hard the model thinks about your turns and the work you hand out. " +
				"xhigh and max ask for a deeper pass than high, and cost the time they take. " +
				"Standing items and their checks stay low whatever this says.",
			read:  func() string { return EffortWord(DefaultEffortAt(dir)) },
			write: func(raw string) error { return writeChoice(dir, KeyEffort, raw, EffortChoices) },
		},
		Setting{
			Key: KeyDocumentEngine, Category: CategoryModels, Kind: SettingChoice,
			Label: "reading", Env: "AFORGE_DOC_ENGINE", Choices: DocumentEngines,
			Hint: "which rung reads your documents. auto walks local, then free, then paid OCR. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return resolvedEngine(DocumentEngineAt(dir)) },
			write: func(raw string) error { return writeChoice(dir, KeyDocumentEngine, raw, DocumentEngines) },
		},

		// Searching sits beside looking and reading because it is the third
		// question of the same shape — which back end answers when aforge has
		// to go outside the machine — and the three keys sit under it because a
		// key is not a preference on its own: it is the thing that decides
		// what the row above it can resolve to.
		Setting{
			Key: KeySearchProvider, Category: CategoryModels, Kind: SettingChoice,
			Label: "searching", Choices: SearchProviders,
			Hint: "where a web search goes. auto uses the best back end your keys reach and " +
				"falls back to one that needs none, so search works with nothing set. " +
				"A change lands on the next session.",
			read:  func() string { return SearchProviderAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeySearchProvider, raw, SearchProviders) },
		},
		// THE KEY EVERY MODEL CALL RIDES. It is a row for the reason the first-run
		// setup exists: a person with no key in their shell has to be able to
		// hand one over somewhere, and "export it and start again" is not a
		// somewhere. It masks like every credential, the environment outranks
		// it as it always has (apikey.go's resolution order), and a write lands
		// on the RUNNING session through the surface's Applied hook rather than
		// waiting for the next launch — the row this was modelled on says "on the
		// next session" because search is an accessory; this is the conversation.
		Setting{
			Key: KeyAPIKey, Category: CategoryModels, Kind: SettingText, Secret: true,
			Label: "openrouter key", Env: APIKeyEnv, EmptyLabel: "not set",
			Hint: "the key aforge talks to models with, from openrouter.ai/settings/keys. " +
				"Set in the shell it outranks this row. A change lands on this conversation at once.",
			read:  func() string { return maskCredential(APIKeyAt(dir)) },
			write: func(raw string) error { return writeCredential(dir, KeyAPIKey, raw, APIKeyAt(dir)) },
		},
		Setting{
			Key: KeyExaKey, Category: CategoryModels, Kind: SettingText, Secret: true,
			Label: "exa key", Env: "EXA_API_KEY", EmptyLabel: "not set",
			Hint: "an exa.ai key, which buys better results and page fetches than the free " +
				"back end. Optional — search works without it. A change lands on the next session.",
			read:  func() string { return maskCredential(ExaKeyAt(dir)) },
			write: func(raw string) error { return writeCredential(dir, KeyExaKey, raw, ExaKeyAt(dir)) },
		},
		Setting{
			Key: KeyFirecrawlKey, Category: CategoryModels, Kind: SettingText, Secret: true,
			Label: "firecrawl key", Env: "FIRECRAWL_API_KEY", EmptyLabel: "not set",
			Hint: "a firecrawl.dev key, for when the free monthly allowance runs out. " +
				"Optional — search works without it. A change lands on the next session.",
			read: func() string { return maskCredential(FirecrawlKeyAt(dir)) },
			write: func(raw string) error {
				return writeCredential(dir, KeyFirecrawlKey, raw, FirecrawlKeyAt(dir))
			},
		},
		Setting{
			Key: KeyJinaKey, Category: CategoryModels, Kind: SettingText, Secret: true,
			Label: "jina key", Env: "JINA_API_KEY", EmptyLabel: "not set",
			Hint: "a jina.ai key. It buys nothing but headroom: page fetches already work " +
				"unauthenticated and the key only raises the rate ceiling. " +
				"A change lands on the next session.",
			read:  func() string { return maskCredential(JinaKeyAt(dir)) },
			write: func(raw string) error { return writeCredential(dir, KeyJinaKey, raw, JinaKeyAt(dir)) },
		},

		// And the rows that name WHICH APPLICATION asks for the accounts a
		// person already has (internal/connect). They sit beside the search keys
		// because they are the same kind of row — optional, and never a
		// prerequisite for anything else, since a blank answer uses the
		// applications this build ships with (connect_defaults.go). Google's
		// are two rows because they are two values copied from two boxes;
		// Slack's public application has only its id.
		Setting{
			Key: KeyGoogleOAuthClient, Category: CategoryModels, Kind: SettingText,
			Label: "google app id", Env: "GOOGLE_OAUTH_CLIENT", EmptyLabel: "not set",
			Hint: "the application id Google sees when aforge asks to use your account. " +
				"Blank uses the one aforge ships with, which is what most people want — " +
				"fill this in only to have your own registration ask instead, and fill in " +
				"the secret below with it. A change lands on the next session.",
			read:  func() string { return googleOAuthClientAt(dir) },
			write: func(raw string) error { return writeProfileValue(dir, KeyGoogleOAuthClient, raw) },
		},
		Setting{
			Key: KeyGoogleOAuthSecret, Category: CategoryModels, Kind: SettingText, Secret: true,
			Label: "google app secret", Env: "GOOGLE_OAUTH_SECRET", EmptyLabel: "not set",
			Hint: "the secret that goes with the application id above. Blank uses the one " +
				"aforge ships with. Write both or neither: your own id against the shipped " +
				"secret connects nothing. A change lands on the next session.",
			read: func() string { return maskCredential(googleOAuthSecretAt(dir)) },
			write: func(raw string) error {
				return writeCredential(dir, KeyGoogleOAuthSecret, raw, googleOAuthSecretAt(dir))
			},
		},
		Setting{
			Key: KeySlackOAuthClient, Category: CategoryModels, Kind: SettingText,
			Label: "slack app id", Env: "SLACK_OAUTH_CLIENT", EmptyLabel: "not set",
			Hint: "the application id Slack sees when aforge asks to use your workspace. " +
				"Blank uses the one aforge ships with, which is what most people want — " +
				"fill this in only to have your own application ask instead, with proof-key " +
				"sign-in turned on. A change lands on the next session.",
			read:  func() string { return slackOAuthClientAt(dir) },
			write: func(raw string) error { return writeProfileValue(dir, KeySlackOAuthClient, raw) },
		},

		Setting{
			Key: KeyDailyBudget, Category: CategorySpending, Kind: SettingDollars,
			Label: "daily budget", Env: "AFORGE_DAILY_BUDGET",
			EmptyLabel: noLimitWord,
			Hint: "what aforge may spend on your work in a day. It starts large — " +
				"it is a backstop against a runaway, not a budget — so set it to what " +
				"you actually want to spend. When the day's calls reach it, new work " +
				"waits for midnight or for you to raise it here. " +
				"Say none for no limit. A change lands at the next rail check.",
			read:    func() string { return moneyValue(resolvedDollars(DailyBudgetUSDAt(dir))) },
			write:   func(raw string) error { return writeDollars(dir, KeyDailyBudget, raw) },
			receipt: s.spentTodayReceipt,
		},
		Setting{
			Key: KeyPlanConsent, Category: CategorySpending, Kind: SettingDollars,
			Label: "ask before spending", Env: "AFORGE_PLAN_CONSENT",
			EmptyLabel: "never asks",
			Hint: "when a planned job is estimated to cost more than this, aforge quotes " +
				"the step count and the price and waits for your go-ahead — it asks, it " +
				"does not stop. Say none and it never asks.",
			read:  func() string { return moneyValue(resolvedDollars(PlanConsentUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyPlanConsent, raw) },
		},
		Setting{
			Key: KeyPracticeBudget, Category: CategorySpending, Kind: SettingDollars,
			Label: "practice budget", Env: "AFORGE_PRACTICE_BUDGET",
			EmptyLabel: "practice off",
			Hint: "the slice of the day reserved for aforge practicing on itself. When it " +
				"is spent, practice stops until tomorrow and your own work is untouched. " +
				"0 is the one money row that does not mean no limit: it turns practice " +
				"off. A change lands the next time aforge starts.",
			read:  func() string { return moneyValue(resolvedDollars(PracticeBudgetUSDAt(dir))) },
			write: func(raw string) error { return writeDollars(dir, KeyPracticeBudget, raw) },
		},

		// WHICH HANDS THIS INSTALL HAS: the workers this install may hand a piece
		// of work to at all. It is a TASK row and not a money one — it was filed
		// under spending because specialists are the expensive way of taking a
		// job, which is a reason to think about it and not a reason to look for
		// it there. A person on the spending tab is asking what may be spent, and
		// a roster of workers is not an answer to that.
		//
		// Its receipt names the build's own workers rather than the hint, because
		// the hint is written once and the workers are whatever the binary ships
		// — a sentence listing them here would be the copy that goes stale.
		Setting{
			Key: KeyWorkers, Category: CategoryTasks, Kind: SettingText,
			Label: "workers", EmptyLabel: "every worker installed", Env: EnvWorkers,
			Hint: "which workers this install may hand a piece of work to, separated by " +
				"commas. Blank is all of them, which is the default. The general-purpose " +
				"worker is never on the list and is never off it — it is what takes the work " +
				"when nothing else is named, so a roster that names nothing runs everything " +
				"the ordinary way.",
			read:    func() string { return WorkersAt(dir) },
			write:   func(raw string) error { return writeText(dir, KeyWorkers, raw) },
			receipt: workersReceipt,
		},

		// The two consent rows sit with spending because they answer the same
		// question about a different currency: what may aforge do without
		// stopping to ask you. The dollars are above; the actions are here.
		Setting{
			Key: KeyToolApprovalMode, Category: CategorySafety, Kind: SettingChoice,
			Label: "ask before running", Choices: ToolApprovalModes,
			Hint: "what happens when the model asks to run a tool: prompt asks you, allow runs it, " +
				"deny refuses it. Dangerous shell commands are asked about whichever way this is set. " +
				"A change lands on the next session.",
			read:  func() string { return ToolApprovalModeAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyToolApprovalMode, raw, ToolApprovalModes) },
		},
		Setting{
			Key: KeyToolApprovals, Category: CategorySafety, Kind: SettingText,
			Label: "tool approvals", EmptyLabel: "none",
			Hint: "exceptions to the answer above, one per tool: `read:allow, bash:prompt`. " +
				"What you write here changes only the tools you name. Reading files and " +
				"aforge's own notes are allowed unless you name them, and everything else " +
				"you do not name follows the setting above. Answering always on an approval " +
				"question writes one, and it takes effect straight away.",
			read:  func() string { return ToolApprovalsAt(dir) },
			write: func(raw string) error { return writeToolApprovals(dir, raw) },
		},
		Setting{
			Key: KeyBashApprovals, Category: CategorySafety, Kind: SettingText,
			Label: "shell command rules", EmptyLabel: "none",
			Hint: "answers for single shell commands, first match wins: " +
				"`allow git status*, deny rm -rf *`. A `*` matches anything; an allow " +
				"answers only for a whole single command, a deny catches its shape " +
				"anywhere in a longer line, and dangerous commands are asked about " +
				"whatever this says. Answering always on an approval question writes one, " +
				"and it takes effect straight away.",
			read:  func() string { return BashApprovalsAt(dir) },
			write: func(raw string) error { return writeBashApprovals(dir, raw) },
		},
		Setting{
			Key: KeyGuardian, Category: CategorySafety, Kind: SettingChoice,
			Label: "guardian", Choices: GuardianModes,
			Hint: "when on, a small model is asked first whether a call is plainly safe — " +
				"read-only, inside this directory, reversible — and you are only asked about the rest. " +
				"It can never approve something the rules above refuse. A change lands on the next session.",
			read:  func() string { return GuardianAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyGuardian, raw, GuardianModes) },
		},
		Setting{
			Key: KeyConsentTimeout, Category: CategorySafety, Kind: SettingCount,
			Label: "approval countdown",
			Hint: "how many seconds an approval question waits for you before it answers itself. " +
				"It answers no — the call is refused and the model is told, never approved — " +
				"and the clock stops the moment you press any key. 0 waits for you forever.",
			read:  func() string { return strconv.Itoa(ConsentTimeoutAt(dir)) },
			write: func(raw string) error { return writeProfileCount(dir, KeyConsentTimeout, raw) },
		},
		Setting{
			Key: KeyRouting, Category: CategoryModels, Kind: SettingChoice,
			Label: "routing", Choices: RoutingModes,
			Hint: "one model id is served by many endpoints, and they answer at very " +
				"different speeds AND very different prices. Left alone, aforge asks for the " +
				"fastest endpoint for your own turns — capped at a quarter over the model's " +
				"list price, because no endpoint is worth four times that — and asks for the " +
				"cheapest for work you are not waiting on: task workers, judges, titles, the " +
				"memory pass. Choosing here overrides that everywhere: latency asks for the " +
				"fastest one for everything and times every answer, demoting an endpoint that " +
				"keeps being slow; price asks for the cheapest for everything; off asks for " +
				"nothing and measures nothing — and with nothing measured there is no lane " +
				"to choose, no sheet of them to open and no speed guard. A change lands on " +
				"the next session.",
			read:  func() string { return RoutingAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyRouting, raw, RoutingModes) },
		},
		// AND THE ROW UNDER IT NAMES A MACHINE. Routing says what a request
		// prefers; this says which endpoint the conversation actually goes to,
		// for the person who has watched the numbers and knows.
		Setting{
			Key: LaneSettingKey(LaneSlotTalk), Category: CategoryModels, Kind: SettingText,
			Label: "lane", EmptyLabel: LaneAuto,
			Hint: "which machine behind your model answers you. One model id is served by " +
				"a dozen endpoints that differ by seven times on the wait before the first " +
				"word, so this is often a bigger change than switching model. auto lets aforge " +
				"pick the fastest one each answer; a name — `cloudflare` — pins it and nothing " +
				"else is asked; `pinned: cloudflare, borrow when slow` keeps the pin but lets " +
				"a slow answer be rescued elsewhere; openrouter asks for no endpoint at all and " +
				"lets the router balance on price. enter on this row opens them with what " +
				"has been measured of each, and so does → on a model row in the picker — " +
				"under /model and under `your model` in the settings panel alike.",
			read:  func() string { return LaneRowWord(dir, LaneSlotTalk) },
			write: func(raw string) error { return WriteLaneRow(dir, LaneSlotTalk, raw) },
		},
		Setting{
			Key: KeyLaneGuard, Category: CategoryModels, Kind: SettingBool,
			Label: "speed guard",
			Hint: "when an answer takes much longer to start than that endpoint normally " +
				"does, the same question is asked of the next-best one and whichever replies " +
				"first is the one you read. It hedges at most one extra call, under a tenth of " +
				"spend; off under price routing.",
			read:  func() string { return formatBool(LaneGuardAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyLaneGuard, raw) },
		},
		Setting{
			Key: KeyMouse, Category: CategoryInterface, Kind: SettingChoice,
			Label: "mouse", Choices: MouseModes,
			Hint: "on gives hover, click and wheel-scroll inside the chat. Selecting text " +
				"still works: press ctrl+s and drag, or ctrl+b to copy from the keyboard. " +
				"Turn off to keep the terminal's plain drag at all times.",
			read:  func() string { return MouseAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyMouse, raw, MouseModes) },
		},
		Setting{
			Key: KeyTimestamps, Category: CategoryInterface, Kind: SettingChoice,
			Label: "timestamps", Choices: TimestampModes,
			Hint: "how much of the clock the conversation carries. footers puts one dim line " +
				"under each finished turn — when it ended, how long it took, how many calls it " +
				"made, what it cost — and marks where the conversation was put down for ten " +
				"minutes or a day. separators keeps only those marks. off draws neither. " +
				"ctrl+o on a turn writes its footer's time out in full.",
			read:  func() string { return TimestampsAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyTimestamps, raw, TimestampModes) },
		},
		Setting{
			Key: KeyWork, Category: CategoryInterface, Kind: SettingChoice,
			Label: "turn work", Choices: WorkModes,
			Hint:  "fold rolls completed reasoning, calls, results, and intermediate text into one worked chip. open keeps that work visible. The change applies immediately.",
			read:  func() string { return WorkAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyWork, raw, WorkModes) },
		},
		// Before the audit row, because it comes first in the life of a task: this
		// says what STARTS when you type /task, the audit row says what has to be
		// true before what started is allowed to land.
		Setting{
			Key: KeyTaskStart, Category: CategoryTasks, Kind: SettingChoice,
			Label: "starting a task", Choices: TaskStartModes,
			Hint: "what /task <brief> pays to find out. sized is the default: the brief " +
				"is read for width first and one worker starts either way, and a brief with " +
				"parts in it starts a worker that can hand them out once it has opened the " +
				"material. single starts that one worker and skips the reading altogether. " +
				"/task solo still says so outright whatever this is set to.",
			read:  func() string { return TaskStartAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyTaskStart, raw, TaskStartModes) },
		},
		Setting{
			Key: KeyTaskAudit, Category: CategoryTasks, Kind: SettingChoice,
			Label: "task audit", Choices: TaskAuditModes,
			Hint: "when on, every task node's work is checked by an independent read-only " +
				"auditor — it runs the repo's own verification and reads the diff — before " +
				"anything may merge into your branch. Off trusts the node's own report and " +
				"merges unaudited. On is the default: the audit is what 'done' means, and it " +
				"roughly doubles a small task's model cost.",
			read:  func() string { return TaskAuditAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyTaskAudit, raw, TaskAuditModes) },
		},
		// And directly under it, the other end of the same question: what happens
		// when the check above came back with nothing at all.
		Setting{
			Key: KeyTaskSettle, Category: CategorySafety, Kind: SettingChoice,
			Label: "who settles a task nobody could check", Choices: TaskSettleModes,
			Hint: "who decides about a task that finished with nobody able to say whether " +
				"it holds. ask is the default and means you do: the landed card offers " +
				"accept, look again and not right, and the chat says what it thinks and " +
				"leaves the choice with you. auto means the chat decides: it reads the " +
				"report and the work, settles the task itself, and comes back to you only " +
				"when it genuinely cannot tell. Either way the task is neither done nor " +
				"failed until somebody answers, its branch is kept, and anything waiting " +
				"on it waits.",
			read:  func() string { return TaskSettleAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyTaskSettle, raw, TaskSettleModes) },
		},
		Setting{
			Key: KeyMemoryEnabled, Category: CategoryPractice, Kind: SettingChoice,
			Label: "memory", Choices: MemoryModes,
			Hint: "when on, aforge carries a handful of things across conversations: what you " +
				"asked it to remember, preferences you stated, corrections you made. A small " +
				"model decides before each message which of them bear on it, and after each " +
				"answer whether anything new is worth keeping. Off remembers nothing and makes " +
				"neither call. On is the default; /memories lists what is kept.",
			read:  func() string { return MemoryAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyMemoryEnabled, raw, MemoryModes) },
		},
		// The countdown sits with the two consent rows and the guardian because
		// it answers their question in the other currency: those say what
		// aforge may DO without asking, this says how long you get to say
		// something about work it has already decided to hand off.
		Setting{
			Key: KeyTaskAutoApprove, Category: CategorySafety, Kind: SettingCount,
			Label: "task countdown",
			Hint: "how many seconds a proposed task waits for you before it starts on its own. " +
				"The countdown is your window to redirect it or wave it off, not a gate — " +
				"0 waits for your answer instead of starting. A change lands on the next session.",
			read:  func() string { return strconv.Itoa(TaskAutoApproveAt(dir)) },
			write: func(raw string) error { return writeProfileCount(dir, KeyTaskAutoApprove, raw) },
		},
		// And beside the countdown, the other number that decides what happens to
		// work you handed off: how many times a task that came back with gaps is
		// sent back to close them before it is called incomplete.
		Setting{
			Key: KeyTaskRepairRounds, Category: CategoryTasks, Kind: SettingCount,
			Label: "task repair rounds",
			Hint: "how many times a task that came back with something missing is sent back " +
				"to finish the job — same working copy, same brief, with the gaps in front of " +
				"it — before it lands as incomplete. Each round costs another run and another " +
				"check. 0 lets the first gap end the task, which is how it worked before.",
			read:  func() string { return strconv.Itoa(TaskRepairRoundsAt(dir)) },
			write: func(raw string) error { return writeProfileCount(dir, KeyTaskRepairRounds, raw) },
		},
		// And beside those two, the three rows about how much work leaves at
		// once. They sit together because they are one subject read at three
		// depths: the number you may name, and the two things the machine itself
		// will say no to whatever you named.
		Setting{
			Key: KeyTaskParallel, Category: CategoryTasks, Kind: SettingCount,
			Label: "tasks at once", EmptyLabel: "no limit",
			Hint: "how many tasks may run at the same time. Blank is no limit, which is the " +
				"default: what actually runs out is this machine — the two rows below hold new " +
				"tasks back when it is loaded — and the model provider's own rate limit, which " +
				"aforge already paces itself against. A cap is a queue, never a refusal.",
			read: func() string {
				if value := TaskParallelAt(dir); value > 0 {
					return strconv.Itoa(value)
				}
				return ""
			},
			write: func(raw string) error { return writeOptionalCount(dir, KeyTaskParallel, raw) },
		},
		Setting{
			Key: KeyTaskMaxLoad, Category: CategoryTasks, Kind: SettingText,
			Label: "busy machine",
			Hint: "the load average per core at which aforge stops starting new tasks — 1.5 by " +
				"default, which is where the machine is handing out slices rather than running " +
				"work. Tasks already running are never touched, so the queue moves again on " +
				"its own. 0 stops watching the load.",
			read:  func() string { return formatNumber(TaskMaxLoadAt(dir)) },
			write: func(raw string) error { return writeProfileNumber(dir, KeyTaskMaxLoad, raw) },
		},
		Setting{
			Key: KeyTaskMinFreeMB, Category: CategoryTasks, Kind: SettingCount,
			Label: "memory floor",
			Hint: "how many MB of memory must be available before another task may start — " +
				"1536 by default, roughly what one more task and its build need. Under it, new " +
				"tasks wait rather than push the machine into swap; running ones carry on. " +
				"0 stops watching memory.",
			read:  func() string { return strconv.Itoa(TaskMinFreeMBAt(dir)) },
			write: func(raw string) error { return writeProfileCount(dir, KeyTaskMinFreeMB, raw) },
		},
		// Which model the work that LEAVES a conversation runs on. It sits with
		// the countdown and the audit rather than among the model rows for the
		// reason those two are here: all three are about the work you hand off —
		// how long you get to redirect it, who checks it, and whose hands it is
		// in — and none of them is a rule about how this conversation's own calls
		// are made.
		Setting{
			Key: KeyTaskModel, Category: CategoryTasks, Kind: SettingText,
			Label: "task model", EmptyLabel: "follows the conversation",
			Hint: "the model a task runs on when you have not asked for another one — " +
				"`anthropic/claude-opus-5`. Leave it blank and a task rides the model you " +
				"are talking to. You can still say which model a particular piece of work " +
				"should go to, and the proposal names the one it will start on.",
			read:  func() string { return TaskModelAt(dir) },
			write: func(raw string) error { return writeText(dir, KeyTaskModel, raw) },
		},
		Setting{
			Key: KeySpendRail, Category: CategorySpending, Kind: SettingDollars,
			Label: "session ceiling", EmptyLabel: noLimitWord,
			Hint: "what one conversation may spend before it stops starting new turns. " +
				"When it is reached the next turn is refused and your message is still " +
				"yours to send again once you raise it; the turn in flight always " +
				"finishes. Say none for no limit. A change lands on the next session.",
			read:    func() string { return moneyValue(SpendRailUSDAt(dir)) },
			write:   func(raw string) error { return writeDollars(dir, KeySpendRail, raw) },
			receipt: s.spentThisSessionReceipt,
		},

		Setting{
			Key: KeyPracticeIdle, Category: CategoryPractice, Kind: SettingDuration,
			Label: "quiet before practice", Env: "AFORGE_PRACTICE_IDLE",
			Hint:  "how long the room stays quiet before aforge starts practicing.",
			read:  func() string { return formatDuration(resolvedDuration(PracticeIdleAt(dir))) },
			write: func(raw string) error { return writeDuration(dir, KeyPracticeIdle, raw) },
		},
		Setting{
			Key: KeyBriefAfter, Category: CategoryPractice, Kind: SettingDuration,
			Label: "arrival brief after", Env: "AFORGE_BRIEF_AFTER",
			Hint:  "how long you have to be away before aforge greets you with a summary. 0 always briefs.",
			read:  func() string { return formatDuration(resolvedDuration(BriefAfterAt(dir))) },
			write: func(raw string) error { return writeDuration(dir, KeyBriefAfter, raw) },
		},
		// THE CREW, AND THEN THE FOUR CLASSES IN IT. The tiers are what a person
		// actually configures for the calls aforge makes on its own — the name it
		// gives a session, the summary a compaction writes, the plan an adaptive
		// run steers by (internal/roles). Four rows, not one per feature: a new
		// call joins a class and needs no row of its own.
		//
		// The crew row comes FIRST because it is the only one most people will
		// ever touch: one word writes all four (crew.go). The four below it are
		// what that word wrote, and each is answerable on its own — which is what
		// turns the crew reading to "custom".
		Setting{
			Key: KeyCrew, Category: CategoryModels, Kind: SettingChoice,
			Label: "crew", Choices: CrewPresets,
			Hint: "the four models aforge works with, chosen as one: `frugal` is deepseek " +
				"everywhere and pennies a day, `balanced` has kimi-k3 think while deepseek " +
				"works, `max` puts kimi-k3 everywhere and lets it think longer. Change one of " +
				"the four rows below and this reads `custom`.",
			read:  func() string { return CrewAt(dir) },
			write: func(raw string) error { return writeCrew(dir, raw) },
		},
		Setting{
			Key: KeyTierReflexModel, Category: CategoryModels, Kind: SettingText,
			Label: "reflex", EmptyLabel: "follows the conversation",
			Hint: "the near-free model that reads every turn — it decides which remembered " +
				"lines this turn needs and whether the exchange is worth keeping. Routing and " +
				"extraction, never reasoning. Blank makes it follow the model you are talking to.",
			read:  func() string { return TierModelAt(dir, ModelTierReflex) },
			write: func(raw string) error { return writeTierModel(dir, ModelTierReflex, raw) },
		},
		Setting{
			Key: KeyTierLowModel, Category: CategoryModels, Kind: SettingText,
			Label: "small work", EmptyLabel: "follows the conversation",
			Hint: "the cheap model that does the bulk of the work — the nodes of an adaptive " +
				"run, session names, digests. Leave it blank and they ride the model you are " +
				"talking to.",
			read:  func() string { return TierModelAt(dir, ModelTierLow) },
			write: func(raw string) error { return writeTierModel(dir, ModelTierLow, raw) },
		},
		Setting{
			Key: KeyTierHighModel, Category: CategoryModels, Kind: SettingText,
			Label: "careful work", EmptyLabel: "follows the conversation",
			Hint: "the capable model for the things that must not be wrong — the check on " +
				"finished task work, the summary a compaction keeps, reading an image.",
			read:  func() string { return TierModelAt(dir, ModelTierHigh) },
			write: func(raw string) error { return writeTierModel(dir, ModelTierHigh, raw) },
		},
		// The fourth tier is the one whose value may name a LEVEL as well as a
		// model, because it is the one class of call where how hard the model
		// thinks is the point rather than the price.
		Setting{
			Key: KeyTierMastermindModel, Category: CategoryModels, Kind: SettingText,
			Label: "mastermind", EmptyLabel: "follows the conversation",
			Hint: "the model that plans adaptive runs and designs saved harnesses — the one " +
				"answer that decides what every other call does. Add `:low`, `:medium` or " +
				"`:high` to ask it to think that hard: `moonshotai/kimi-k3:high`.",
			read:  func() string { return TierModelAt(dir, ModelTierMastermind) },
			write: func(raw string) error { return writeTierModel(dir, ModelTierMastermind, raw) },
		},
		Setting{
			Key: KeyModelRoles, Category: CategoryModels, Kind: SettingText,
			Label: "pinned roles", EmptyLabel: "none",
			Hint: "exceptions to the four rows above, one per role: `title:openai/gpt-5-mini`. " +
				"A role not named here follows its class.",
			read:  func() string { return ModelRolesAt(dir) },
			write: func(raw string) error { return writeModelRoles(dir, raw) },
		},
		Setting{
			Key: KeyModelFallbacks, Category: CategoryModels, Kind: SettingText,
			Label: "fallback models", EmptyLabel: "nearest in the catalog",
			Hint: "where a conversation goes when your model cannot answer — nothing serving " +
				"it will take the request, it keeps going quiet mid-reply, or it is being " +
				"rate limited and will not stop. One or more slugs, comma-separated, first " +
				"tried first: `openai/gpt-5-mini, anthropic/claude-sonnet-4`. Leave it blank " +
				"and the nearest same-class model in the catalog is used. The turn says which " +
				"one it moved to.",
			read:  func() string { return ModelFallbacksAt(dir) },
			write: func(raw string) error { return writeText(dir, KeyModelFallbacks, raw) },
		},
		Setting{
			Key: KeyContextFill, Category: CategoryModels, Kind: SettingCount,
			Label: "context fill", Env: "AFORGE_CONTEXT_FILL_PCT",
			Hint: "how much of a model's context window aforge fills before it starts " +
				"compacting, as a percent. Higher packs more in; the rest stays as thinking " +
				"and answer room. A change lands on the next call.",
			read:  func() string { return strconv.Itoa(ContextFillAt(dir)) },
			write: func(raw string) error { return writeContextFill(dir, raw) },
		},
		Setting{
			Key: KeyCompletionReserve, Category: CategoryModels, Kind: SettingCount,
			Label: "answer room", Env: "AFORGE_COMPLETION_RESERVE",
			Hint: "tokens every call keeps free for its answer and its reasoning. " +
				"Generous costs nothing on turns that do not use it; small produces empty " +
				"replies from a model that thinks past it. A change lands on the next call.",
			read:  func() string { return strconv.Itoa(CompletionReserveAt(dir)) },
			write: func(raw string) error { return writeCompletionReserve(dir, raw) },
		},
		Setting{
			Key: KeyWorkingSet, Category: CategoryModels, Kind: SettingCount,
			Label: "working set", Env: "AFORGE_WORKING_SET",
			Hint: "the most material aforge keeps quoted in front of a worker at once, in tokens, " +
				"however large the model's window is. A huge window is permission to send a lot, " +
				"not a reason to: past this the older material fades to pointers it can still read " +
				"back. A change lands on the next call.",
			read:  func() string { return strconv.Itoa(WorkingSetAt(dir)) },
			write: func(raw string) error { return writeWorkingSet(dir, raw) },
		},
		Setting{
			Key: KeyContextReuse, Category: CategoryModels, Kind: SettingCount,
			Label: "context reuse", Env: "AFORGE_CONTEXT_REUSE_PCT",
			// THE UNIT IS A MULTIPLE IN HUNDREDTHS, AND THE SENTENCE LEADS WITH
			// THAT. "as a percent" over a row whose default reads 250 asks a
			// reader to find the whole this is a percentage OF — a window, a
			// budget — and there is no such whole: 100 is one context over, 250 is
			// two and a half. The old wording is also why the floor reads as a
			// mistake (writeContextReuse refuses below 100, where a percentage
			// would clamp above it), so the worked example comes before anything
			// else the row has to say.
			Hint: "how many times over one piece of work may re-send its whole context before " +
				"aforge tells it to land, in hundredths — 100 is once, 250 is two and a half " +
				"times, and 100 is the floor. Every turn re-sends everything before it, so this " +
				"is what stops a worker going round in circles at full price. " +
				"A change lands on the next job.",
			read:  func() string { return strconv.Itoa(ContextReuseAt(dir)) },
			write: func(raw string) error { return writeContextReuse(dir, raw) },
		},
		Setting{
			Key: KeyReplyGuard, Category: CategoryModels, Kind: SettingChoice,
			Label: "reply guard", Choices: ReplyGuardModes,
			Hint: "when on, a reply that stops being language — a model repeating one line " +
				"or one letter until the budget is gone, words with three alphabets inside " +
				"them — is cut where it went wrong, thrown away, and asked again once. None " +
				"of it is kept, which matters because a bad reply goes back into the " +
				"conversation and the next one reads it. Code blocks are never judged, so a " +
				"page of zeros or a long log is safe. Off shows you whatever arrives. " +
				"A model that goes quiet is cut either way.",
			read:  func() string { return ReplyGuardAt(dir) },
			write: func(raw string) error { return writeChoice(dir, KeyReplyGuard, raw, ReplyGuardModes) },
		},
		Setting{
			Key: KeyTenureAfter, Category: CategoryPractice, Kind: SettingCount,
			Label: "tenure after", Env: "AFORGE_TENURE_AFTER",
			Hint:  "how many clean firings a standing charter needs before it earns tenure.",
			read:  func() string { return strconv.Itoa(TenureAfterAt(dir)) },
			write: func(raw string) error { return writeTenure(dir, raw) },
		},
	)

	// THE SECOND ROW THAT IS NOT ALWAYS BUILT, on the divider's law below and
	// for a plainer reason: on a machine with no timer to install there is
	// nothing for this switch to switch. A Windows host, a registry built over
	// a profile that exists only to read defaults out of, a caller that never
	// opened a standing store — each of them gets a sheet without the row
	// rather than a row that reads nothing and refuses every write.
	if s.options.BackgroundChecks != nil {
		rows = append(rows, s.backgroundRow(s.options.BackgroundChecks))
	}

	// THE ONE ROW THAT IS NOT ALWAYS BUILT, and it is the exception
	// [NewSettings] names: a missing seam makes a row read-only, except where
	// read-only would mean a control that cannot do the one thing it is for.
	//
	// The divider is a number a SURFACE keeps and this file cannot write, so a
	// caller that hands over no [SettingsOptions.SaveSplitPct] has no divider to
	// move. Built anyway, the row read a plausible percentage, accepted a new
	// one, and answered "chat width is unavailable here" — a control that
	// existed only to refuse. The v3 chat is exactly that caller: its rail is a
	// fixed column chosen by frame width (internal/tui3's railColsFor), so there
	// is no share of the frame to set and the honest sheet is one without the
	// row. The v1 and v2 surfaces do keep a divider and do pass the seam
	// (internal/command's Settings), so the row is theirs still — and it stays
	// where it has always been, at the head of the interface group.
	if s.options.SaveSplitPct != nil {
		rows = append(rows, s.splitRow())
	}

	rows = append(rows,
		Setting{
			Key: KeyTaskColumn, Category: CategoryInterface, Kind: SettingBool,
			Label: "task column",
			Hint: "whether the task roster stands in a column on the right of the chat: the " +
				"work this session has run, newest first, foldable into the shape each run " +
				"grew. ctrl+g puts it away and brings it back; this is where the answer is " +
				"remembered. With no column, running work still shows as a row of chips above " +
				"the conversation. A change here lands the next time aforge starts.",
			read:  func() string { return formatBool(TaskColumnAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyTaskColumn, raw) },
		},
		Setting{
			Key: KeyQuickSwitch, Category: CategoryInterface, Kind: SettingBool,
			Label: "quick switch",
			Hint: "whether ctrl+k switches conversations the moment it is pressed — press " +
				"again to go further back, pause and the card fades, esc returns to where " +
				"you started. Turned off, ctrl+k opens the card and waits for enter. Either " +
				"way, touching the arrow keys holds the card open to look around without " +
				"switching.",
			read:  func() string { return formatBool(QuickSwitchAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyQuickSwitch, raw) },
		},
		Setting{
			Key: KeyHistoryEnabled, Category: CategoryInterface, Kind: SettingBool,
			Label: "input history", Env: "AFORGE_HISTORY",
			Hint: "remembers the messages you send, so the up arrow walks them back in a later " +
				"session. Turn it off and nothing is written; what is already on disk stays. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return formatBool(HistoryEnabledAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyHistoryEnabled, raw) },
		},
		Setting{
			Key: KeyDraftPersist, Category: CategoryInterface, Kind: SettingBool,
			Label: "keep drafts", Env: "AFORGE_DRAFT_PERSIST",
			Hint: "keeps the half-typed message in the box across a restart, per directory. " +
				"A change lands the next time aforge starts.",
			read:  func() string { return formatBool(DraftPersistAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyDraftPersist, raw) },
		},
		Setting{
			Key: KeyHints, Category: CategoryInterface, Kind: SettingBool,
			Label: "hints",
			Hint: "one-line tips above the message box, each shown until the key or command " +
				"it names has been used once. Off silences them, and the what's-new line a " +
				"new build may say with them. A change lands at the end of the next turn.",
			read:  func() string { return formatBool(HintsAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyHints, raw) },
		},
		Setting{
			Key: KeyAttribution, Category: CategoryInterface, Kind: SettingBool,
			Label: "attribution", Env: "AFORGE_ATTRIBUTION",
			Hint: "signs commits and PRs aforge writes for you — one trailer, one footer line. " +
				"A change lands on the next job.",
			read:  func() string { return formatBool(AttributionAt(dir)) },
			write: func(raw string) error { return writeBool(dir, KeyAttribution, raw) },
		},
		// The ssh carrier is local surface policy, so its overrides live beside
		// the other interface choices. Keeping them in the registry matters more
		// than their rarity: a network that drops idle TCP or rewrites DSCP is a
		// place where hard-coded command-line options cannot be repaired in
		// ~/.ssh/config, because command-line options win.
		Setting{
			Key: KeySSHControlPersist, Category: CategoryInterface, Kind: SettingCount,
			Label: "ssh reuse",
			Hint: "seconds an ssh connection stays reusable after its channel closes. 300 makes a " +
				"quick reconnect avoid a new handshake; 0 turns persistence off. A change lands next launch.",
			read:  func() string { return strconv.Itoa(SSHTransportAt(dir).ControlPersistSeconds) },
			write: func(raw string) error { return writeProfileCount(dir, KeySSHControlPersist, raw) },
		},
		Setting{
			Key: KeySSHServerAlive, Category: CategoryInterface, Kind: SettingCount,
			Label: "ssh heartbeat",
			Hint: "seconds of silence before ssh asks whether the far machine is still there. 3 detects " +
				"a dead link promptly; 0 turns heartbeats off. A change lands next launch.",
			read:  func() string { return strconv.Itoa(SSHTransportAt(dir).ServerAliveSeconds) },
			write: func(raw string) error { return writeProfileCount(dir, KeySSHServerAlive, raw) },
		},
		Setting{
			Key: KeySSHServerMisses, Category: CategoryInterface, Kind: SettingCount,
			Label: "ssh missed heartbeats",
			Hint: "how many unanswered ssh heartbeats end a dead connection. 3 with the default heartbeat " +
				"notices an unresponsive link in about 9 seconds. A change lands next launch.",
			read:  func() string { return strconv.Itoa(SSHTransportAt(dir).ServerAliveMisses) },
			write: func(raw string) error { return writeProfileCount(dir, KeySSHServerMisses, raw) },
		},
		Setting{
			Key: KeySSHIPQoS, Category: CategoryInterface, Kind: SettingChoice,
			Label:   "ssh traffic",
			Choices: SSHIPQoSChoices,
			Hint: "how ssh marks interactive traffic: lowdelay by default, af21 on networks that honor it, " +
				"or none where traffic marking is filtered. A change lands next launch.",
			read:  func() string { return SSHTransportAt(dir).IPQoS },
			write: func(raw string) error { return writeChoice(dir, KeySSHIPQoS, raw, SSHIPQoSChoices) },
		},
	)
	return rows
}

// noLimitWord is what a money row says about itself when its number is zero.
//
// A DOLLAR ROW READING "$0" IS THE ONE PLACE THE EMPTINESS LAW CANNOT REACH.
// Zero on these rows is not an absence — it is the person's own instruction,
// and the instruction it spells is the OPPOSITE of what "$0" reads as at a
// glance: "no money at all" where the code means "no ceiling at all". Every
// other kind of row has a word for its off state ([Setting.EmptyLabel]) and a
// dollar row cannot borrow one, because its reading is a formatted number and
// therefore never empty. So the word goes where a row already says the dim true
// thing beside its value: the receipt.
//
// It is EXPORTED because the surfaces say it too — the v3 spend place's pointer
// line and the settings tab's `today` reading both have to spell "nothing bounds
// this" and there is one spelling of it.
const NoLimitWord = "no limit"

// noLimitWord is the package-internal spelling of the same word.
const noLimitWord = NoLimitWord

// moneyValue is how EVERY dollar row reads, and the whole of what it adds is
// that it never returns "$0".
//
// A row whose figure is zero returns NOTHING, which hands the reading to
// [Setting.EmptyLabel] — the mechanism every other kind of row already uses for
// its off state, and the one place each rail's own word for zero is written
// down: `no limit` on the day and the conversation, `never asks` on the consent
// gate, `practice off` on the carve-out, whose zero switches practice off rather
// than uncapping it. Two things follow for free, and both are the reason it is
// spelled this way rather than as a second receipt:
//
//   - THE EDIT BOX OPENS EMPTY on a row that holds nothing. Every surface
//     already blanks the empty label before it seeds the box, so a person
//     editing "no limit" is offered a place to type a number rather than a
//     sentence to delete first.
//   - THE RECEIPT IS FREED FOR A FACT. `no limit` is not a receipt — it is the
//     value — and while it sat in the receipt column there was nowhere left to
//     say what the row is actually doing right now ($4.25 today, this one $0.41).
func moneyValue(value float64) string {
	if value == 0 {
		return ""
	}
	return formatDollars(value)
}

// spentFigure is how a SPEND is written, which is not how a LIMIT is written.
//
// A limit is a figure somebody typed and [formatDollars] writes it back the
// shortest way that is still the same number — right for a config file and right
// for `$500`. A spend is a measurement nobody chose, and the shortest honest
// form of one is a disaster: four fifths of a tenth of a cent came out of the
// provider as 0.0005688764200000001 and went onto the row exactly like that,
// twenty-two digits of float noise where a person wanted to read a price. So a
// spend is cents, and four decimals under a cent — the same ladder the surface's
// own money word uses, so the receipt and the figure beside it agree.
func spentFigure(usd float64) string {
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// spentTodayReceipt is the day's spend beside the day's ceiling (13). Nil seam
// or an uncounted day renders nothing at all rather than $0.00, which would be
// a claim nobody made.
func (s *Settings) spentTodayReceipt() string {
	if s.options.SpentTodayUSD == nil {
		return ""
	}
	spent, counted := s.options.SpentTodayUSD()
	// AND A DAY THAT HAS COST NOTHING SAYS NOTHING. Counted-zero and
	// not-counted are different facts about the seam and the SAME fact about
	// the money: no call has been paid for today, and `$0 today` beside a limit
	// is the emptiness law broken on the one row that exists to state a figure.
	if !counted || spent <= 0 {
		return ""
	}
	return spentFigure(spent) + " today"
}

// spentThisSessionReceipt is the conversation ceiling's own receipt: what THIS
// conversation has spent against it.
//
// It is the same seam as the day's and answers with the same pair, for the same
// reason: a door with no conversation behind it — the v2 sheet, the model's own
// settings tool, a headless read — has not counted zero, it has not counted, and
// the row draws nothing rather than a $0.00 nobody earned.
func (s *Settings) spentThisSessionReceipt() string {
	if s.options.SpentThisSessionUSD == nil {
		return ""
	}
	spent, counted := s.options.SpentThisSessionUSD()
	if !counted || spent <= 0 {
		return ""
	}
	return "this one " + spentFigure(spent)
}

func (s *Settings) modelRow(slot ModelSlot) Setting {
	// A CAPABILITY SLOT IS A KNOB WITH A READER, and a role slot is a live seam
	// into a running engine. That is the whole split, and it is why the two are
	// different rows: the drawing model is a value written down in the profile
	// and read at the moment something draws (docs/MULTIMODAL.md Decision 5),
	// while the conversation model is a thing a running session is ON and can
	// only be asked of the session.
	if slot.Role == "" {
		return s.mediaModelRow(slot)
	}
	options := s.options
	row := Setting{
		Key: ModelSettingKey(slot.Slot), Category: CategoryModels, Kind: SettingModel,
		Label: slot.Label, Slot: slot.Slot,
		Hint: modelSlotHint(slot),
	}
	if slot.Held {
		// Only a slot the engine holds lives in the chat prefs file. A role
		// bound in the roles table and resolved nowhere has no prefs field to
		// name, and claiming one would send the surface looking for provenance
		// in a file that has never heard of it.
		row.PrefsField = modelPrefsField(slot.Slot)
	}
	if slot.Follows != "" {
		row.EmptyLabel = "follows " + slot.Follows
	}
	if name, ok := modelSlotEnvDefault(slot.Slot); ok {
		row.EnvDefault = name
	}
	row.read = func() string { return modelSlotReading(options, slot) }
	row.write = func(slug string) error {
		if options.SetModel == nil {
			return fmt.Errorf("model switching is unavailable here")
		}
		return options.SetModel(slot.Slot, strings.TrimSpace(slug))
	}
	row.receipt = func() string {
		if options.ModelCost == nil {
			return ""
		}
		return options.ModelCost(modelSlotReading(options, slot))
	}
	return row
}

// mediaModelRow is one of the five capability slots — drawing, speaking,
// composing, filming, voice — and since the v3 revision its write is ACCEPTED.
//
// THE ROW IS THE FRONT DOOR (docs/MULTIMODAL.md Decision 5). The double-knob
// era, where the sheet held a row nothing read beside an environment variable
// or a role pin that everything read, is over: the value is written into the
// profile's config.json under this row's own key, and every resolver that asks
// which model draws — the v3 use-time resolver, and [Load] for the older
// surfaces — reads that key back through [MediaSlotModelAt]. One knob, one
// reader.
//
// It carries no [Setting.PrefsField] on purpose. A prefs field means "this row
// fronts a store the registry does not own", which was true while these rows
// lived beside the graph and is exactly wrong now: the value is in the file
// this registry writes, so the provenance chip can and must say so.
func (s *Settings) mediaModelRow(slot ModelSlot) Setting {
	dir := s.options.ProfileDir
	options := s.options
	row := Setting{
		Key: ModelSettingKey(slot.Slot), Category: CategoryModels, Kind: SettingModel,
		Label: slot.Label, Slot: slot.Slot,
		// An unset capability slot is not a blank: the resolver walks on to the
		// catalog and picks a model that publishes the capability, so the honest
		// reading is the same word the looking row has always used.
		EmptyLabel: "automatic",
		Hint:       modelSlotHint(slot),
	}
	if name, ok := modelSlotEnvDefault(slot.Slot); ok {
		row.EnvDefault = name
	}
	row.read = func() string { return MediaSlotModelAt(dir, slot.Slot) }
	row.write = func(slug string) error {
		return writeText(dir, ModelSettingKey(slot.Slot), slug)
	}
	row.receipt = func() string {
		if options.ModelCost == nil {
			return ""
		}
		return options.ModelCost(MediaSlotModelAt(dir, slot.Slot))
	}
	return row
}

// MediaSlotModelAt is ONE CAPABILITY SLOT'S MODEL, in the order every other
// persisted knob resolves in: the operator's environment variable, then the row
// the settings sheet wrote into the profile, then nothing.
//
// Nothing is the honest last answer rather than a curated name. This function
// answers "what did a person choose", and the ladder that turns no choice into
// a working model is the resolver's business (cmd/aforge's chatv3_media.go, and
// [CandidateMediaModel] behind it) — a default invented here would be a rung
// the resolver could not tell from a deliberate pin.
//
// A slot that is not a capability slot answers nothing at all: "talk" is the
// conversation, and reading AFORGE_MODEL through this door would let a media
// resolver quietly take the chat model for a modality it cannot serve.
func MediaSlotModelAt(profileDir, slot string) string {
	slot = strings.TrimSpace(slot)
	if _, media := mediaSlotWords[slot]; !media {
		return ""
	}
	if name, ok := modelSlotEnvDefault(slot); ok {
		if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
			return raw
		}
	}
	if value, ok := persistedString(profileDir, ModelSettingKey(slot)); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// modelSlotReading asks the ONE source that can answer for this slot. A role
// the engine holds no client for is asked of the roles table if the wiring lane
// supplied one, and of nothing otherwise — never of the engine, whose slot
// lookup would hand back the conversation model for a word it does not know
// (internal/command's CurrentModel falls through), which is a wrong answer
// wearing a confident face.
func modelSlotReading(options SettingsOptions, slot ModelSlot) string {
	if slot.Held {
		if options.ModelValue == nil {
			return ""
		}
		return strings.TrimSpace(options.ModelValue(slot.Slot))
	}
	if options.RoleModel == nil {
		return ""
	}
	value, bound := options.RoleModel(string(slot.Role))
	if !bound {
		return ""
	}
	return strings.TrimSpace(value)
}

// backgroundRow is the `background checks` switch.
//
// WHAT IT READS IS DERIVED FROM THE MACHINE AND NEVER FROM THE FILE. The value
// on disk is what the person last asked for; whether anything is actually
// checking is a definition on this host, and [standing.Timer.Status] answers it
// by reading that definition's own bytes. A row that read the file would say
// `on` over a timer somebody removed with launchctl, in the one place a person
// went to check — so the file's answer is used for exactly one thing, the
// launch repair, and never for the sentence in front of you.
//
// A status that cannot be read renders as NOTHING, which is the emptiness law:
// a switch that guessed would be worse than a switch that admits it does not
// know.
func (s *Settings) backgroundRow(watch standing.Watch) Setting {
	dir := s.options.ProfileDir
	return Setting{
		Key: KeyStandingBackground, Category: CategoryPractice, Kind: SettingChoice,
		Label: "background checks", Choices: BackgroundModes,
		Hint: "whether reminders, watches and routines are checked when no aforge window " +
			"is open. On installs one small timer under your own login that runs " +
			"`aforge tick` every " + standing.IntervalWords() + " — a launchd agent called " +
			standing.DarwinTickLabel + " on a Mac, a systemd user timer called " +
			standing.LinuxTickTimer + " on Linux — and on is the default. Off removes it, and " +
			"then things are checked only while a window is open. Either way nothing runs " +
			"while the machine is asleep, and nothing runs when you are not logged in.",
		read: func() string {
			status, err := watch.Status()
			if err != nil {
				return ""
			}
			if status.Installed {
				return BackgroundOn
			}
			return BackgroundOff
		},
		write: func(raw string) error {
			value := strings.ToLower(strings.TrimSpace(raw))
			if value == "" {
				value = DefaultBackground
			}
			if value != BackgroundOn && value != BackgroundOff {
				return fmt.Errorf("pick one of: %s", strings.Join(BackgroundModes, ", "))
			}
			// THE ANSWER IS WRITTEN DOWN BEFORE THE MACHINE IS TOUCHED, which is
			// the law the first-item notice keeps too (internal/session's
			// tools_standing.go): a person whose install half-worked is somebody
			// this build knows what they asked for, rather than somebody it
			// quietly does the opposite for at the next launch.
			if err := writeProfileValue(dir, KeyStandingBackground, value); err != nil {
				return err
			}
			if value == BackgroundOff {
				return watch.Uninstall(context.Background())
			}
			return watch.Install(context.Background())
		},
	}
}

func (s *Settings) splitRow() Setting {
	options := s.options
	row := Setting{
		Key: KeySplitPct, Category: CategoryInterface, Kind: SettingPercent,
		Label: "chat width", PrefsField: KeySplitPct,
		Hint: fmt.Sprintf("the chat pane's share of the frame while the task rail is open (%d–%d). "+
			"[ and ] nudge it too.", MinSplitPct, MaxSplitPct),
	}
	row.read = func() string {
		return formatPercent(ClampSplitPct(readSplit(options)))
	}
	row.write = func(raw string) error {
		value, err := parsePercent(raw, MinSplitPct, MaxSplitPct)
		if err != nil {
			return err
		}
		// Unreachable, and kept: [Settings.build] will not make this row without
		// the seam, so nobody meets this sentence any more. A future caller that
		// builds the row by hand gets a refusal rather than a nil call.
		if options.SaveSplitPct == nil {
			return fmt.Errorf("chat width is unavailable here")
		}
		options.SaveSplitPct(value)
		return nil
	}
	return row
}

func readSplit(options SettingsOptions) int {
	if options.SplitPct == nil {
		return DefaultSplitPct
	}
	if pct := options.SplitPct(); pct != 0 {
		return pct
	}
	return DefaultSplitPct
}

// ClampSplitPct keeps the divider inside the usable band. Zero stays "unset"
// and resolves to the default at layout time.
func ClampSplitPct(pct int) int {
	if pct == 0 {
		return 0
	}
	return max(MinSplitPct, min(MaxSplitPct, pct))
}

func modelPrefsField(slot string) string {
	switch slot {
	case "talk":
		return "chat_model"
	case "work":
		return "task_model"
	default:
		return slot + "_model"
	}
}

func modelSlotEnvDefault(slot string) (string, bool) {
	switch slot {
	case "talk", "work":
		return "AFORGE_MODEL", true
	case "plan":
		return "AFORGE_PLAN_MODEL", true
	case "voice":
		return "AFORGE_VOICE_MODEL", true
	case "image":
		return "AFORGE_IMAGE_MODEL", true
	case "speech":
		return "AFORGE_SPEECH_MODEL", true
	case "music":
		return "AFORGE_MUSIC_MODEL", true
	case "video":
		return "AFORGE_VIDEO_MODEL", true
	default:
		return "", false
	}
}

// modelSlotHint is what the row says about itself, in the product's words. The
// two roles nothing resolves yet say so — 5.20 rule 3: a row that cannot change
// anything today must not read as though it can.
func modelSlotHint(slot ModelSlot) string {
	switch slot.Role {
	case store.RoleOrchestrate:
		return "the model that answers you here. It changes on your next message."
	case store.RoleWork:
		return "the model that does the work. It changes on the next job."
	case store.RolePlan:
		return "the model that plans and reviews the work. Empty follows the work model."
	case store.RoleVerify:
		return "the model that checks the work — gates, judges, second opinions. " +
			"Nothing reads this binding yet; it is written down and waiting."
	case store.RoleScribe:
		return "the model that writes the short things — titles, labels, summaries. " +
			"Nothing reads this binding yet; it is written down and waiting."
	}
	switch slot.Slot {
	case "voice":
		return "the model that hears you when you speak."
	case "image":
		return "the model that draws."
	case "speech":
		return "the model that speaks."
	case "music":
		return "the model that composes."
	case "video":
		return "the model that films."
	}
	return ""
}

// Persisted resolution — environment, then the profile's config.json, then the
// built-in default. Malformed persisted values fall back to the default rather
// than stopping a launch; a malformed environment value is the operator's own
// explicit instruction and still errors.

// PlanConsentUSDAt resolves the estimate above which a plan asks first.
func PlanConsentUSDAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_PLAN_CONSENT")); raw != "" {
		return validateDailyBudgetValue(raw, "AFORGE_PLAN_CONSENT")
	}
	if value, ok := persistedFloat(profileDir, KeyPlanConsent); ok && value >= 0 {
		return value, nil
	}
	return DefaultPlanConsentUSD, nil
}

// PracticeBudgetUSDAt resolves the daily self-practice carve-out.
func PracticeBudgetUSDAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_PRACTICE_BUDGET")); raw != "" {
		return validateDailyBudgetValue(raw, "AFORGE_PRACTICE_BUDGET")
	}
	if value, ok := persistedFloat(profileDir, KeyPracticeBudget); ok && value >= 0 {
		return value, nil
	}
	return DefaultPracticeBudgetUSD, nil
}

// PracticeIdleAt resolves the quiet period before self-practice.
func PracticeIdleAt(profileDir string) (time.Duration, error) {
	return durationAt(profileDir, "AFORGE_PRACTICE_IDLE", KeyPracticeIdle, DefaultPracticeIdle)
}

// BriefAfterAt resolves the absence that earns an arrival brief.
func BriefAfterAt(profileDir string) (time.Duration, error) {
	return durationAt(profileDir, "AFORGE_BRIEF_AFTER", KeyBriefAfter, DefaultBriefAfter)
}

func durationAt(profileDir, envName, key string, fallback time.Duration) (time.Duration, error) {
	if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < 0 {
			return 0, fmt.Errorf("%s: want a non-negative duration, got %q", envName, raw)
		}
		return value, nil
	}
	if value, ok := persistedDuration(profileDir, key); ok {
		return value, nil
	}
	return fallback, nil
}

// TenureAfterAt resolves the clean-firing count that earns a charter tenure.
func TenureAfterAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_TENURE_AFTER")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return DefaultTenureAfter
	}
	if value, ok := persistedInt(profileDir, KeyTenureAfter); ok && value > 0 {
		return value
	}
	return DefaultTenureAfter
}

// The two resolvers that used to sit here — practice_demand_pct and
// propose_new_skills — are gone with their rows. Nothing in this build read
// either one: the value was resolved, copied onto a Config field, and never
// looked at again, so the sheet was offering a person a dial wired to nothing.
// 5.20 is that an affordance which does nothing must not be offered as though
// it does, and the fix for a knob with no reader is to delete the knob, not to
// leave it turning. When the learning loop that wants them lands it brings its
// own rows, and the completeness gate will make sure of it.

// AttributionAt resolves whether aforge signs the git work it does for the
// user. A malformed pin reads as the default rather than refusing a launch over
// a signature.
func AttributionAt(profileDir string) bool {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_ATTRIBUTION")); raw != "" {
		if value, err := parseBool(raw); err == nil {
			return value
		}
		return DefaultAttribution
	}
	if value, ok := persistedBool(profileDir, KeyAttribution); ok {
		return value
	}
	return DefaultAttribution
}


// HistoryEnabledAt resolves whether the v3 chat surface records what was typed
// into ~/.aforge/v3/history.jsonl. A malformed pin reads as the default
// rather than refusing a launch over a recall list.
func HistoryEnabledAt(profileDir string) bool {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_HISTORY")); raw != "" {
		if value, err := parseBool(raw); err == nil {
			return value
		}
		return DefaultHistoryEnabled
	}
	if value, ok := persistedBool(profileDir, KeyHistoryEnabled); ok {
		return value
	}
	return DefaultHistoryEnabled
}

// DraftPersistAt resolves whether the v3 chat surface keeps the unsent draft on
// disk between sessions. Shaped exactly like [HistoryEnabledAt].
func DraftPersistAt(profileDir string) bool {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_DRAFT_PERSIST")); raw != "" {
		if value, err := parseBool(raw); err == nil {
			return value
		}
		return DefaultDraftPersist
	}
	if value, ok := persistedBool(profileDir, KeyDraftPersist); ok {
		return value
	}
	return DefaultDraftPersist
}



// TaskColumnAt resolves whether the v3 chat stands its task column up, default
// on. A row that will not parse reads as the default rather than as off, for
// [TimestampsAt]'s reason: a garbled row must not quietly take the session's
// record of its own work off the screen.
func TaskColumnAt(profileDir string) bool {
	if value, ok := persistedBool(profileDir, KeyTaskColumn); ok {
		return value
	}
	return DefaultTaskColumn
}

// SaveTaskColumn records what the person did to the column with their hands.
//
// It is EXPORTED because the surface writes it from a chord, and it is the v3 half of the same
// bargain: this is the one interface row whose value is normally chosen by a
// keystroke rather than by visiting the sheet, so the key needs a door to disk
// that goes through the same writer the row's own does. Two doors, one
// validation.
func SaveTaskColumn(profileDir string, open bool) error {
	return writeBool(profileDir, KeyTaskColumn, formatBool(open))
}

// QuickSwitchAt resolves whether the conversation switcher's chord switches on
// each press, default on. A row that will not parse reads as the default rather
// than as off, for [TaskColumnAt]'s reason: a garbled row must not quietly slow
// a gesture down.
func QuickSwitchAt(profileDir string) bool {
	if value, ok := persistedBool(profileDir, KeyQuickSwitch); ok {
		return value
	}
	return DefaultQuickSwitch
}

// HintsAt resolves whether the v3 chat shows its tips, default on. A row that
// will not parse reads as the default rather than as off, for [TaskColumnAt]'s
// reason: a garbled row must not quietly take a newcomer's only pointers away.
func HintsAt(profileDir string) bool {
	if value, ok := persistedBool(profileDir, KeyHints); ok {
		return value
	}
	return DefaultHints
}




// DocumentEngineAt resolves the document-reading rung.
func DocumentEngineAt(profileDir string) (string, error) {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_DOC_ENGINE")); raw != "" {
		engine := strings.ToLower(raw)
		if !knownDocumentEngine(engine) {
			return "", fmt.Errorf("AFORGE_DOC_ENGINE: unknown engine %q (auto, local, free, ocr)", engine)
		}
		return engine, nil
	}
	if value, ok := persistedString(profileDir, KeyDocumentEngine); ok {
		if engine := strings.ToLower(strings.TrimSpace(value)); knownDocumentEngine(engine) {
			return engine, nil
		}
	}
	return DefaultDocumentEngine, nil
}

// VisionModelAt resolves the image-inspection proxy slot. Empty means resolve
// from the live catalog at use.
func VisionModelAt(profileDir string) string {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_VISION_MODEL")); raw != "" {
		return raw
	}
	if value, ok := persistedString(profileDir, KeyVisionModel); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// InstallPersistedEnv exports the persisted value of every knob whose only
// reader is the process environment, so a choice made in the sheet survives a
// relaunch without a second lookup path. A variable the user actually set is
// never overwritten — the environment still wins.
func InstallPersistedEnv(profileDir string) {
	if strings.TrimSpace(os.Getenv("AFORGE_TENURE_AFTER")) != "" {
		return
	}
	if value, ok := persistedInt(profileDir, KeyTenureAfter); ok && value > 0 {
		_ = os.Setenv("AFORGE_TENURE_AFTER", strconv.Itoa(value))
	}
}

// ── the web-search rows ─────────────────────────────────────────────────────
//
// The three key rows ARE environment-pinned, where the v3 rows below are not,
// and the difference is what the variable can do. A pin on the tool gate would
// be a bypass — a stray export widening what may run without asking. A pin on
// a credential is the credential itself: EXA_API_KEY, FIRECRAWL_API_KEY and
// JINA_API_KEY are the vendors' own variable names, already exported in the
// shells of the people who have keys, and a settings sheet that ignored them
// would make the same person paste the same secret twice and then wonder which
// copy was live.
//
// They are the vendors' spellings rather than AFORGE_-prefixed ones for that
// same reason: the value is not ours, and renaming somebody's key variable to
// claim it would be the product asking the world to accommodate it.
//
// The provider row has NO pin. It is a preference and not a secret, and the
// sheet is where a preference is answered so it can be read back.

// SearchProviderAt resolves where a web search goes: the persisted row when it
// names a provider this build accepts, otherwise auto. An unrecognised value
// reads as auto rather than as an error, which is the direction a garbled
// setting may be wrong in — auto still searches.
func SearchProviderAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeySearchProvider); ok {
		if provider := strings.ToLower(strings.TrimSpace(value)); knownSearchProvider(provider) {
			return provider
		}
	}
	return DefaultSearchProvider
}

// ExaKeyAt resolves the Exa credential: the environment first, then the sheet,
// then empty — and empty is a working configuration, not a fault.
func ExaKeyAt(profileDir string) string {
	return credentialAt(profileDir, "EXA_API_KEY", KeyExaKey)
}

// FirecrawlKeyAt resolves the optional Firecrawl ceiling credential the same way.
func FirecrawlKeyAt(profileDir string) string {
	return credentialAt(profileDir, "FIRECRAWL_API_KEY", KeyFirecrawlKey)
}

// JinaKeyAt resolves the Jina credential the same way.
func JinaKeyAt(profileDir string) string {
	return credentialAt(profileDir, "JINA_API_KEY", KeyJinaKey)
}

// GoogleOAuthClientAt resolves the Google registration: the environment first,
// then the sheet, then the registration this build ships with
// (connect_defaults.go, which states why a desktop client's secret may be
// compiled in at all). So the answer is never empty, and every build can offer
// to connect an account without a person filling in a registration form first.
//
// IT ANSWERS BOTH HALVES OR NEITHER IS WORTH HAVING, which is why it is one
// call and not two. An id without its secret cannot ask Google for anything, so
// a caller handed half a pair would have to write the same "and the other one"
// check every reader of these rows already needs; here it is written once, and
// the caller's test is the one it should be — is the id there.
//
// EACH HALF WALKS THE RUNGS ALONE, which is the shape the rows already had: the
// two are separate values a person copies from two separate boxes, and each is
// resolved by the same credentialAt every other credential uses. The one edge
// that leaves is a person who answers ONE half of their own registration — they
// get their id against this build's secret, which Google refuses — and the cure
// is the obvious one, answer the other half too, which is what the sheet's hint
// on both rows already says.
func GoogleOAuthClientAt(profileDir string) (id, secret string) {
	id = googleOAuthClientAt(profileDir)
	if id == "" {
		id = defaultGoogleOAuthClient
	}
	secret = googleOAuthSecretAt(profileDir)
	if secret == "" {
		secret = defaultGoogleOAuthSecret
	}
	return id, secret
}

// The two halves on their own, for the registry rows: a row reads and writes one
// value, and a row that displayed a pair would have nothing to write back to.
func googleOAuthClientAt(profileDir string) string {
	return credentialAt(profileDir, "GOOGLE_OAUTH_CLIENT", KeyGoogleOAuthClient)
}

func googleOAuthSecretAt(profileDir string) string {
	return credentialAt(profileDir, "GOOGLE_OAUTH_SECRET", KeyGoogleOAuthSecret)
}

// SlackOAuthClientAt resolves the Slack registration: the environment first,
// then the sheet, then the public application this build ships with. The
// answer is never empty, so every build can offer Slack without asking a
// person to register an application first.
func SlackOAuthClientAt(profileDir string) string {
	id := slackOAuthClientAt(profileDir)
	if id == "" {
		id = defaultSlackOAuthClient
	}
	return id
}

// slackOAuthClientAt is the bare rung walk for the registry row. The row shows
// only what the person answered, so an unanswered row still reads "not set"
// while [SlackOAuthClientAt] uses the application this build ships with.
func slackOAuthClientAt(profileDir string) string {
	return credentialAt(profileDir, "SLACK_OAUTH_CLIENT", KeySlackOAuthClient)
}

func credentialAt(profileDir, env, key string) string {
	if raw := strings.TrimSpace(os.Getenv(env)); raw != "" {
		return raw
	}
	if value, ok := persistedString(profileDir, key); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func knownSearchProvider(provider string) bool {
	for _, candidate := range SearchProviders {
		if candidate == provider {
			return true
		}
	}
	return false
}

// maskCredentialBullets is how many bullets stand in for the head of a key. It
// is a FIXED count and not the key's real length: a mask that grew with the
// secret would leak its length, and a forty-character row of dots reads as
// damage rather than as a value.
const maskCredentialBullets = 8

// maskCredentialTail is how much of the key survives the mask — enough to tell
// two keys apart when a person is checking which one is loaded, and far too
// little to be worth anything to somebody reading over their shoulder.
const maskCredentialTail = 4

// maskCredential is what a secret row reads as. An unset row masks to empty so
// the row's EmptyLabel still speaks: "not set" and a row of bullets are
// different facts and must not render the same.
func maskCredential(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	bullets := strings.Repeat("•", maskCredentialBullets)
	runes := []rune(value)
	if len(runes) <= maskCredentialTail {
		// A value this short is not a key — a typo, a paste that lost its
		// tail. Show none of it rather than all of it.
		return bullets
	}
	return bullets + string(runes[len(runes)-maskCredentialTail:])
}

// writeCredential persists a secret row, and treats the MASK as "unchanged".
//
// That guard is the whole reason this is not writeText. Every editor in the
// tree opens on the value the row displays, which for a secret row is the
// mask; a person who opens the row to look at it and presses enter would
// otherwise replace their key with eight bullets and four characters of it —
// an unrecoverable edit made by doing nothing.
//
// Empty still CLEARS. "I did not change it" is the mask; "" is a person
// deleting the field, which is the only way to remove a key from the sheet.
func writeCredential(profileDir, key, raw, current string) error {
	value := strings.TrimSpace(raw)
	if value != "" && value == maskCredential(current) {
		return nil
	}
	return writeProfileValue(profileDir, key, value)
}

func knownDocumentEngine(engine string) bool {
	for _, candidate := range DocumentEngines {
		if candidate == engine {
			return true
		}
	}
	return false
}

// ── the v3 session's own rows ───────────────────────────────────────────────
//
// None of these six is pinned by an environment variable, and that is
// deliberate rather than an omission. A tool gate that a stray export could
// widen to "allow everything" is a gate with a bypass; a spend ceiling the
// shell can lift is not a ceiling. They are answered in the sheet, where the
// answer is written down and can be read back.

// ToolApprovalModeAt resolves the blanket answer the tool gate starts from. An
// unrecognised persisted value reads as the default, which is the strictest of
// the three — a garbled setting must never be the one that opens the gate.
func ToolApprovalModeAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyToolApprovalMode); ok {
		if mode := strings.ToLower(strings.TrimSpace(value)); knownToolApprovalMode(mode) {
			return mode
		}
	}
	return DefaultToolApprovalMode
}

// GuardianAt resolves whether a small model answers a tool prompt before the
// person is asked. An unrecognised persisted value reads as the default, which
// is off — a garbled setting must never be the one that appoints a stand-in.
func GuardianAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyGuardian); ok {
		if mode := strings.ToLower(strings.TrimSpace(value)); mode == GuardianOff || mode == GuardianOn {
			return mode
		}
	}
	return DefaultGuardian
}

// GuardianEnabledAt is [GuardianAt] as the bool internal/session's Config takes.
// The two exist separately because the row's value is a WORD — that is what the
// sheet renders and what the file holds — and the seam on the other side is a
// switch; one function answering both would have to pick which of those it lies
// about.
func GuardianEnabledAt(profileDir string) bool {
	return GuardianAt(profileDir) == GuardianOn
}

// MouseAt resolves the mouse row to its word, default off.
func MouseAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyMouse); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return DefaultMouse
}

// MouseEnabledAt is [MouseAt] as the bool the surface's View takes — the same
// word/switch split [GuardianEnabledAt] documents.
func MouseEnabledAt(profileDir string) bool {
	return MouseAt(profileDir) == MouseOn
}

// TimestampsAt resolves the timestamps row to its word, default footers. An
// unknown word reads as the default rather than as off, for [RoutingAt]'s
// reason: a garbled row must not quietly take a fact off the screen.
func TimestampsAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyTimestamps); ok {
		value = strings.TrimSpace(strings.ToLower(value))
		for _, mode := range TimestampModes {
			if value == mode {
				return value
			}
		}
	}
	return DefaultTimestamps
}

// WorkAt resolves the completed-work presentation, default fold.
func WorkAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyWork); ok {
		value = strings.TrimSpace(strings.ToLower(value))
		for _, mode := range WorkModes {
			if value == mode {
				return value
			}
		}
	}
	return DefaultWork
}

// RoutingAt resolves the routing row to its word, default latency. An
// unreadable or unknown word falls back to the default rather than to off: a
// garbled row must not quietly stop a session chasing the fastest endpoint.
func RoutingAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyRouting); ok {
		value = strings.TrimSpace(strings.ToLower(value))
		for _, mode := range RoutingModes {
			if value == mode {
				return value
			}
		}
	}
	return DefaultRouting
}

// RoutingChoiceAt is the routing row A PERSON ACTUALLY WROTE, empty when they
// have written nothing readable.
//
// It is the same read as [RoutingAt] without the fallback, and the two are both
// needed because they answer different questions. A settings sheet asks "what
// is in force?" and must be told latency, which is what an unset row does. The
// adapter asks "did somebody CHOOSE?", and it must be able to hear no — that is
// the whole of what lets it route a person's own turn by speed and an errand
// nobody is waiting on by price, while an explicit word still wins over both
// (internal/provider's velocity.go).
func RoutingChoiceAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyRouting); ok {
		value = strings.TrimSpace(strings.ToLower(value))
		for _, mode := range RoutingModes {
			if value == mode {
				return value
			}
		}
	}
	return ""
}

// TaskAuditAt resolves the audit row to its word, default on.
func TaskAuditAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyTaskAudit); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return DefaultTaskAudit
}

// TaskAuditEnabledAt is [TaskAuditAt] as the bool the session's Config takes.
func TaskAuditEnabledAt(profileDir string) bool {
	return TaskAuditAt(profileDir) == TaskAuditOn
}

// ReplyGuardAt resolves the reply-guard row to its word, default on.
func ReplyGuardAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyReplyGuard); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return DefaultReplyGuard
}

// ReplyGuardEnabledAt is [ReplyGuardAt] as the bool the session's Config takes.
// The two exist separately for the reason the guardian's pair does: the row's
// value is a WORD, and the seam on the other side is a switch.
func ReplyGuardEnabledAt(profileDir string) bool {
	return ReplyGuardAt(profileDir) == ReplyGuardOn
}

// TaskSettleAt resolves the row to its word, default ask. A value this build
// does not recognise reads as the default rather than as an error, on the rule
// [TaskStartAt] states: this row decides who is asked about somebody's work, and
// a typo in a config file must not start answering on their behalf.
func TaskSettleAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyTaskSettle); ok {
		if value = strings.TrimSpace(value); value == TaskSettleAuto || value == TaskSettleAsk {
			return value
		}
	}
	return DefaultTaskSettle
}

// TaskStartAt resolves [KeyTaskStart]: the persisted row, else the default. A
// value this build does not recognise reads as the default rather than as an
// error, because the row decides what a command does and a typo in a config file
// must not be a command that refuses.
//
// THAT RULE IS ALSO HOW A RETIREMENT IS PAID FOR. `adaptive` and `ask` were both
// answers here once; taking a word out of [TaskStartModes] is the whole of
// retiring it, because a profile that still holds one falls through this loop
// and reads as [DefaultTaskStart] — no migration, no error, and nothing said to
// somebody about a preference they set months ago.
func TaskStartAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyTaskStart); ok {
		value = strings.ToLower(strings.TrimSpace(value))
		for _, mode := range TaskStartModes {
			if mode == value {
				return value
			}
		}
	}
	return DefaultTaskStart
}

// MemoryAt resolves the memory row to its word, default on.
func MemoryAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyMemoryEnabled); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return DefaultMemory
}

// MemoryEnabledAt is [MemoryAt] as the bool the v3 door reads before it opens a
// brain at all: memory off is a session handed no store, which is what makes
// "no block and no calls" a property of the wiring rather than a branch every
// caller has to remember (internal/session's memory.go states the law).
func MemoryEnabledAt(profileDir string) bool {
	return MemoryAt(profileDir) == MemoryOn
}

// BackgroundChecksAt resolves the background-checks row to its word, default
// on. It is the person's INTENT and not a reading of the machine: what the
// settings row shows is derived from the timer ([Settings.build]), and this is
// what the row was last told.
func BackgroundChecksAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyStandingBackground); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return DefaultBackground
}

// BackgroundChecksWantedAt is [BackgroundChecksAt] as the bool the launch reads
// before it repairs a timer that has drifted off a program that moved
// (cmd/aforge's chatv3_process.go). A person who turned the row off is a person
// whose machine must stay as they left it.
func BackgroundChecksWantedAt(profileDir string) bool {
	return BackgroundChecksAt(profileDir) == BackgroundOn
}

// ToolApprovalsAt resolves the per-tool exceptions as the person wrote them.
// The text is the record; [ParseToolApprovals] is how a caller reads it.
func ToolApprovalsAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyToolApprovals); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// ParseToolApprovals reads `read:allow, bash:prompt` into the map
// internal/approval's Load takes as its "tools" section.
//
// The flat text is a STAND-IN. The structured map — per-tool rules, and the
// ordered bash pattern list beside them — lands with the settings file that can
// hold a nested shape; until then a person needs some way to say "never ask me
// about read", and one line they can read back beats a nested editor nobody has
// written yet. The parse is strict about the action for the reason
// approval.ParseAction is: a typo that was silently dropped would be a rule
// somebody thinks is protecting them.
func ParseToolApprovals(raw string) (map[string]string, error) {
	pairs, err := parsePairs(raw, "tool")
	if err != nil {
		return nil, err
	}
	for tool, action := range pairs {
		if !knownToolApprovalMode(action) {
			return nil, fmt.Errorf("%s: %q is not allow, prompt or deny", tool, action)
		}
	}
	return pairs, nil
}

// TierModelAt resolves the model one auxiliary tier runs on. Empty means the
// tier follows the session's own model, which is internal/roles' floor.
//
// UNSET AND CLEARED ARE DIFFERENT ANSWERS, on all four tiers. A profile that has
// never held the key gets this build's own choice for that class of work
// ([DefaultReflexModel] and its three neighbours), because a person who never
// opened the sheet should not have the whole crew answering on the most
// expensive model in the build — which is what following the conversation means
// once there is a mastermind tier in it. A row somebody emptied ON PURPOSE reads
// empty and follows the conversation, because refusing to let them turn it off
// would make a default into a rule.
//
// The reflex tier was the first row written this way, for the reason its key
// still states: a call made twice a turn is a bill nobody agreed to. The other
// three joined it when the crew landed, and the four defaults together are one
// preset rather than four opinions (crew.go).
//
// The value may carry a level (`moonshotai/kimi-k3:low`) and IS RETURNED WHOLE.
// Splitting is [roles.SplitEffort]'s job at the point of resolution, because a
// settings surface wants the string the person wrote and a request wants the two
// halves apart.
func TierModelAt(profileDir, tier string) string {
	key := tierKeyFor(tier)
	if value, ok := persistedString(profileDir, key); ok {
		return strings.TrimSpace(value)
	}
	return defaultTierModel(tier)
}

// tierKeyFor is the settings key one tier word writes. It is total over
// [ModelTiers] and degrades to the low row, which is what an unknown word has
// always resolved to here.
func tierKeyFor(tier string) string {
	switch tier {
	case ModelTierHigh:
		return KeyTierHighModel
	case ModelTierReflex:
		return KeyTierReflexModel
	case ModelTierMastermind:
		return KeyTierMastermindModel
	}
	return KeyTierLowModel
}

// defaultTierModel is what a tier answers on a profile that has never held its
// key. The four together are the balanced crew.
func defaultTierModel(tier string) string {
	switch tier {
	case ModelTierReflex:
		return DefaultReflexModel
	case ModelTierHigh:
		return DefaultHighModel
	case ModelTierMastermind:
		return DefaultMastermindModel
	}
	return DefaultLowModel
}

// ModelRolesAt resolves the per-role pins as the person wrote them.
func ModelRolesAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyModelRoles); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// ParseModelRoles reads `title:openai/gpt-5-mini` into role → model. The value
// is split at the FIRST colon only, because a model slug can carry one of its
// own (`…/model:free`).
func ParseModelRoles(raw string) (map[string]string, error) {
	return parsePairs(raw, "role")
}

// ModelFallbacksAt resolves the fallback chain as the person wrote it.
func ModelFallbacksAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyModelFallbacks); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// ParseModelFallbacks reads the row into an ordered list of model slugs.
//
// It is comma-separated and NOT a pair list, unlike the two model rows above it:
// there is no key here, only an order, and the order is the whole content. A
// slug may carry a colon of its own (`…/model:free`), which is exactly why this
// splits on commas and nothing else.
//
// Blank entries are dropped and duplicates collapse to their first appearance,
// so a trailing comma or a name written twice is a tidy-up rather than an error.
// It cannot fail: this row names models, and whether a model exists is a
// question only the provider can answer.
func ParseModelFallbacks(raw string) []string {
	seen := map[string]bool{}
	var models []string
	for _, field := range strings.Split(raw, ",") {
		model := strings.TrimSpace(field)
		key := strings.ToLower(model)
		if model == "" || seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, model)
	}
	return models
}

// ── the response boundary's three numbers ───────────────────────────────────

// The keys the boundary's numbers persist under. They are named `response.`
// because that is what the boundary reads — one response, and what kind of
// failure it was — rather than `task.` or `model.`, either of which would put
// the row beside the wrong question (internal/taxonomy).
const (
	// KeyResponseAttempts is N: how many times ONE request is tried on its own
	// tier before the wire is given up on. The first try is included.
	KeyResponseAttempts = "response.attempts"
	// KeyResponseLiftAfter is K: how many checks must read finished work and
	// find gaps in it, on the same tier and with the wire ruled out, before a
	// stronger model is bought.
	KeyResponseLiftAfter = "response.lift_after"
	// KeyResponseLiftCap is what that stronger model may cost ONE piece of
	// work, in dollars. 0 is no cap.
	KeyResponseLiftCap = "response.lift_cap_usd"
)

// ResponseLimitsAt resolves the whole of [taxonomy.Limits] for a profile:
// environment pin, then the persisted row, then the package default, which is
// the order every other number in this file resolves in.
//
// IT IS ONE READER AND NOT THREE, because the three numbers are one policy and
// a caller that resolved two of them would be running a boundary nobody
// configured. The backoff is not among them: it is derived from the attempt
// count's own schedule and there has never been a reason to turn it apart from
// the count.
func ResponseLimitsAt(profileDir string) taxonomy.Limits {
	return taxonomy.Limits{
		TransportAttempts: ResponseAttemptsAt(profileDir),
		TransportBackoff:  taxonomy.DefaultTransportBackoff,
		SemanticFailures:  ResponseLiftAfterAt(profileDir),
		TierCapUSD:        ResponseLiftCapAt(profileDir),
	}
}

// ResponseAttemptsAt resolves N. A pin below one is nonsense — a request that is
// never sent — and reads as the default rather than as an instruction.
func ResponseAttemptsAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_RESPONSE_ATTEMPTS")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 1 {
			return value
		}
		return taxonomy.DefaultTransportAttempts
	}
	if value, ok := persistedInt(profileDir, KeyResponseAttempts); ok && value >= 1 {
		return value
	}
	return taxonomy.DefaultTransportAttempts
}

// ResponseLiftAfterAt resolves K, the same way.
func ResponseLiftAfterAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_RESPONSE_LIFT_AFTER")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 1 {
			return value
		}
		return taxonomy.DefaultSemanticFailures
	}
	if value, ok := persistedInt(profileDir, KeyResponseLiftAfter); ok && value >= 1 {
		return value
	}
	return taxonomy.DefaultSemanticFailures
}

// ResponseLiftCapAt resolves the cap, in dollars.
//
// A PERSISTED 0 IS A VALUE AND NOT AN ABSENCE, for [TaskAutoApproveAt]'s reason:
// 0 means no cap, and somebody who wrote it must not find one back in the
// morning.
func ResponseLiftCapAt(profileDir string) float64 {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_RESPONSE_LIFT_CAP")); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value >= 0 {
			return value
		}
		return taxonomy.DefaultTierCapUSD
	}
	if value, ok := persistedFloat(profileDir, KeyResponseLiftCap); ok && value >= 0 {
		return value
	}
	return taxonomy.DefaultTierCapUSD
}

// TaskAutoApproveAt resolves the task countdown, in seconds. 0 is a clock that
// is off: the proposal waits for an answer instead of starting itself.
//
// A persisted 0 is a VALUE and not an absence, which is why the reader tests
// ok before it tests the number: a person who turned the clock off must not
// find it back at five the next morning.
func TaskAutoApproveAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyTaskAutoApprove); ok && value >= 0 {
		return value
	}
	return DefaultTaskAutoApprove
}

// TaskRepairRoundsAt resolves how many repair rounds a task gets, default one.
//
// A persisted 0 is a VALUE and not an absence, for [TaskAutoApproveAt]'s reason:
// a person who turned the loop off must not find it back on in the morning.
func TaskRepairRoundsAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyTaskRepairRounds); ok && value >= 0 {
		return value
	}
	return DefaultTaskRepairRounds
}

// TaskParallelAt resolves how many tasks may run at once. 0 is no limit, and
// no limit is the default.
//
// A persisted 0 is a VALUE and not an absence, for [TaskAutoApproveAt]'s
// reason — though here the value and the default agree, so the test that
// matters is the other direction: a person who wrote 2 must not find it gone.
func TaskParallelAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyTaskParallel); ok && value >= 0 {
		return value
	}
	return DefaultTaskParallel
}

// TaskMaxLoadAt resolves the per-core load average above which no new task is
// started. 0 turns the check off.
func TaskMaxLoadAt(profileDir string) float64 {
	if value, ok := persistedFloat(profileDir, KeyTaskMaxLoad); ok && value >= 0 {
		return value
	}
	return DefaultTaskMaxLoad
}

// TaskMinFreeMBAt resolves the available-memory floor under starting a task, in
// mebibytes. 0 turns the check off.
func TaskMinFreeMBAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyTaskMinFreeMB); ok && value >= 0 {
		return value
	}
	return DefaultTaskMinFreeMB
}

// TaskModelAt resolves the model tasks run on, as the person wrote it. Empty
// is the ordinary answer and means "the conversation's own": the row is an
// override, and whether the name in it exists is a question only the catalog
// can answer (internal/session resolves it against one).
func TaskModelAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyTaskModel); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// ConsentTimeoutAt resolves the approval countdown, in seconds. 0 is a clock
// that is off: the question waits for an answer and never answers itself.
//
// It tests ok before it tests the number for the reason [TaskAutoApproveAt]
// does: a persisted 0 is a person who turned the clock off, not an absence.
func ConsentTimeoutAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyConsentTimeout); ok && value >= 0 {
		return value
	}
	return DefaultConsentTimeout
}

// writeProfileCount persists a whole-number row as itself. The context-law
// writers have their own because each carries a band and hands the whole law
// to ctxbudget; a plain count has neither.
func writeProfileCount(profileDir, key, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

// writeOptionalCount is [writeProfileCount] for a row whose ZERO IS ITS EMPTY
// READING — task.parallel shows "no limit" rather than "0" (its EmptyLabel), so
// a person who opens it, clears it and saves is asking for exactly that, and a
// row that answered "that's not a whole number" would be punishing them for
// reading it the way it is written.
func writeOptionalCount(profileDir, key, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return writeProfileValue(profileDir, key, 0)
	}
	return writeProfileCount(profileDir, key, raw)
}

func writeProfileNumber(profileDir, key, raw string) error {
	value, err := parseNumber(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

// SpendRailUSDAt resolves one conversation's own ceiling. 0 is off.
func SpendRailUSDAt(profileDir string) float64 {
	if value, ok := persistedFloat(profileDir, KeySpendRail); ok && value >= 0 {
		return value
	}
	return DefaultSpendRailUSD
}

func knownToolApprovalMode(mode string) bool {
	for _, known := range ToolApprovalModes {
		if known == mode {
			return true
		}
	}
	return false
}

// parsePairs reads `a:b, c:d` — comma, semicolon or newline separated — into a
// map. Duplicate keys are an error rather than last-one-wins: two answers for
// one name is a person who meant something and cannot be told which half was
// obeyed.
func parsePairs(raw, subject string) (map[string]string, error) {
	pairs := map[string]string{}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	}) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, value, found := strings.Cut(item, ":")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if !found || name == "" || value == "" {
			return nil, fmt.Errorf("write one %s per entry, like `%s:value` — %q is not a pair", subject, subject, item)
		}
		if _, repeated := pairs[name]; repeated {
			return nil, fmt.Errorf("%s %q is named twice", subject, name)
		}
		pairs[name] = value
	}
	return pairs, nil
}

// writeToolApprovals validates the pairs, then keeps the person's own text.
// Storing the text rather than the parsed map is what lets the row read back
// exactly as it was typed; the map is derived on every read anyway.
func writeToolApprovals(profileDir, raw string) error {
	if _, err := ParseToolApprovals(raw); err != nil {
		return err
	}
	return writeText(profileDir, KeyToolApprovals, raw)
}

// writeModelRoles is [writeToolApprovals] for the role pins. The model name is
// not validated against the catalog: a person configuring a model they have not
// pulled yet is early, not wrong, and the call that uses it will say so.
//
// THE ROLE NAME IS VALIDATED, and that half is not the same bargain at all. A
// model slug nobody recognises fails loudly at the call that uses it; a ROLE
// nobody recognises fails silently forever — the pin is stored, it reads back
// exactly as it was typed, the panel shows it, and not one call ever consults
// it. `harness_designer:some/model` is a sentence about nothing, and the only
// evidence is work that keeps coming out on the wrong model.
//
// It became worth refusing when the settings pair landed
// (internal/session's tools_settings.go): a person choosing this row in the
// panel is reading the list of roles printed directly above it, while a model
// writing the row is guessing the name — and it guessed `harness_designer` the
// first time it was asked.
func writeModelRoles(profileDir, raw string) error {
	pins, err := ParseModelRoles(raw)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, role := range roles.Registered() {
		known[string(role)] = true
	}
	for name := range pins {
		if known[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		return fmt.Errorf("%q is not a role. The roles are: %s", name, strings.Join(roleNames(), ", "))
	}
	return writeText(profileDir, KeyModelRoles, raw)
}

// roleNames is the registered roles as a sorted list of plain words, for the
// refusal above to name them all rather than make somebody go looking.
func roleNames() []string {
	names := make([]string, 0, len(roles.Registered()))
	for _, role := range roles.Registered() {
		names = append(names, string(role))
	}
	sort.Strings(names)
	return names
}

// Writers. Each validates in plain language, then persists atomically through
// the same profile config.json the budget rail already writes.

func writeDollars(profileDir, key, raw string) error {
	value, err := parseDollars(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

func writeDuration(profileDir, key, raw string) error {
	value, err := parseDuration(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, formatDuration(value))
}

func writePercent(profileDir, key, raw string, low, high int) error {
	value, err := parsePercent(raw, low, high)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

func writeBool(profileDir, key, raw string) error {
	value, err := parseBool(raw)
	if err != nil {
		return err
	}
	return writeProfileValue(profileDir, key, value)
}

func writeChoice(profileDir, key, raw string, choices []string) error {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, choice := range choices {
		if choice == value {
			return writeProfileValue(profileDir, key, value)
		}
	}
	return fmt.Errorf("pick one of: %s", strings.Join(choices, ", "))
}

func writeText(profileDir, key, raw string) error {
	return writeProfileValue(profileDir, key, strings.TrimSpace(raw))
}

// ContextFillAt resolves the fill law: environment pin, then the persisted
// row, then the package default. A malformed pin reads as the default.
func ContextFillAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_CONTEXT_FILL_PCT")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return ctxbudget.DefaultFillPercent
	}
	if value, ok := persistedInt(profileDir, KeyContextFill); ok && value > 0 {
		return value
	}
	return ctxbudget.DefaultFillPercent
}

// CompletionReserveAt resolves the answer-and-reasoning reserve the same way.
func CompletionReserveAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_COMPLETION_RESERVE")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return ctxbudget.DefaultCompletionReserveTokens
	}
	if value, ok := persistedInt(profileDir, KeyCompletionReserve); ok && value > 0 {
		return value
	}
	return ctxbudget.DefaultCompletionReserveTokens
}

// WorkingSetAt resolves the cap on the live working set the same way.
func WorkingSetAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_WORKING_SET")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return ctxbudget.DefaultWorkingSetTokens
	}
	if value, ok := persistedInt(profileDir, KeyWorkingSet); ok && value > 0 {
		return value
	}
	return ctxbudget.DefaultWorkingSetTokens
}

// ContextReuseAt resolves the cumulative re-send allowance the same way.
func ContextReuseAt(profileDir string) int {
	if raw := strings.TrimSpace(os.Getenv("AFORGE_CONTEXT_REUSE_PCT")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
		return ctxbudget.DefaultReusePercent
	}
	if value, ok := persistedInt(profileDir, KeyContextReuse); ok && value > 0 {
		return value
	}
	return ctxbudget.DefaultReusePercent
}

// contextLaw is the whole of the context law as this profile currently states
// it. It is assembled in one place because the law is configured as one value:
// a writer that rebuilt only its own field would hand ctxbudget zeroes for the
// other three and quietly revert them to their defaults for the life of the
// process.
func contextLaw(profileDir string) ctxbudget.Limits {
	return ctxbudget.Limits{
		FillPercent:             ContextFillAt(profileDir),
		CompletionReserveTokens: CompletionReserveAt(profileDir),
		WorkingSetTokens:        WorkingSetAt(profileDir),
		ReusePercent:            ContextReuseAt(profileDir),
	}
}

// writeContextFill persists the fill percent and hands the whole law to
// ctxbudget, so the change lands in this process as well as the next one.
func writeContextFill(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value < 10 || value > 90 {
		return fmt.Errorf("that needs to be between 10 and 90")
	}
	if err := writeProfileValue(profileDir, KeyContextFill, value); err != nil {
		return err
	}
	ctxbudget.Configure(contextLaw(profileDir))
	return nil
}

// writeCompletionReserve persists the reserve and hands it to ctxbudget.
func writeCompletionReserve(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value < 1024 {
		return fmt.Errorf("that needs to be at least 1024 tokens")
	}
	if err := writeProfileValue(profileDir, KeyCompletionReserve, value); err != nil {
		return err
	}
	ctxbudget.Configure(contextLaw(profileDir))
	return nil
}

// writeWorkingSet persists the working-set ceiling and hands it to ctxbudget.
// The lower bound is the completion reserve: a working set smaller than the
// room every call already keeps for its own answer leaves nothing for the work.
func writeWorkingSet(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if floor := CompletionReserveAt(profileDir); value < floor {
		return fmt.Errorf("that needs to be at least %d tokens — the room kept for the answer", floor)
	}
	if err := writeProfileValue(profileDir, KeyWorkingSet, value); err != nil {
		return err
	}
	ctxbudget.Configure(contextLaw(profileDir))
	return nil
}

// writeContextReuse persists the re-send allowance and hands it to ctxbudget.
// Below 100 it would stop a worker before it had sent its context once, which
// is a refusal rather than a governor.
func writeContextReuse(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value < 100 {
		return fmt.Errorf("that needs to be at least 100 — one whole context")
	}
	if err := writeProfileValue(profileDir, KeyContextReuse, value); err != nil {
		return err
	}
	ctxbudget.Configure(contextLaw(profileDir))
	return nil
}

// writeTenure persists the count and exports it, because the standing watch
// reads the variable at each check: the change lands in this process too.
func writeTenure(profileDir, raw string) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value <= 0 {
		return fmt.Errorf("that needs to be at least 1")
	}
	if err := writeProfileValue(profileDir, KeyTenureAfter, value); err != nil {
		return err
	}
	_ = os.Setenv("AFORGE_TENURE_AFTER", strconv.Itoa(value))
	return nil
}

// Parsers. Their errors are the words the row shows under itself, so they read
// like a person talking rather than a validator.

// noLimitWords are every way a person spells "take the limit off" on a money
// row, and they all land the same zero.
//
// NO LIMIT IS A WORD AND A NUMBER IS A NUMBER. `0` has always been the
// instruction, and `0` is the one spelling of it that reads at a glance as its
// own opposite — "no money at all" where the code means "no ceiling at all". A
// person who wants the rail gone types what they mean, and every word they might
// reach for is here rather than in a refusal that tells them to type a digit.
// The row then READS `no limit` ([Setting.EmptyLabel]), so what was typed and
// what is shown agree.
var noLimitWords = []string{"none", "no", "off", "unlimited", "∞", "no limit", "nolimit", "never"}

func parseDollars(raw string) (float64, error) {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$"))
	for _, word := range noLimitWords {
		if strings.EqualFold(text, word) {
			return 0, nil
		}
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a dollar amount — a number, or none for no limit")
	}
	return value, nil
}

func parseDuration(raw string) (time.Duration, error) {
	text := strings.TrimSpace(raw)
	if text == "0" {
		return 0, nil
	}
	value, err := time.ParseDuration(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a length of time — try 20m or 4h")
	}
	return value, nil
}

func parsePercent(raw string, low, high int) (int, error) {
	text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
	value, err := strconv.Atoi(text)
	if err != nil || value < low || value > high {
		return 0, fmt.Errorf("that's not a percentage between %d and %d", low, high)
	}
	return value, nil
}

// parseNumber reads a row that is a plain non-negative figure rather than a
// count — a load average is 1.5 and rounding it to 1 or 2 would be the row
// changing what the person asked for.
func parseNumber(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a number")
	}
	return value, nil
}

func parseCount(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("that's not a whole number")
	}
	return value, nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("that's not on or off")
}

// Formatters.

func formatDollars(value float64) string {
	return "$" + strconv.FormatFloat(value, 'f', -1, 64)
}

func formatPercent(value int) string { return strconv.Itoa(value) + "%" }

// formatNumber writes a plain figure back the shortest way that is still the
// same number: 1.5 stays 1.5 and 2 does not become 2.0.
func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatBool(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

// formatDuration writes the shortest honest form: 4h, 20m, 1h30m.
func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "0"
	}
	var text strings.Builder
	if hours := int(value / time.Hour); hours > 0 {
		fmt.Fprintf(&text, "%dh", hours)
	}
	if minutes := int(value % time.Hour / time.Minute); minutes > 0 {
		fmt.Fprintf(&text, "%dm", minutes)
	}
	if seconds := int(value % time.Minute / time.Second); seconds > 0 {
		fmt.Fprintf(&text, "%ds", seconds)
	}
	if text.Len() == 0 {
		return value.String()
	}
	return text.String()
}

// A row still has to read while the environment holds an unparsable value.
// The sheet shows the pin and refuses the edit; the reading falls back rather
// than blanking.

func resolvedDollars(value float64, err error) float64 {
	if err != nil {
		return 0
	}
	return value
}

func resolvedDuration(value time.Duration, err error) time.Duration {
	if err != nil {
		return 0
	}
	return value
}

func resolvedEngine(value string, err error) string {
	if err != nil {
		return DefaultDocumentEngine
	}
	return value
}

// Typed reads of the profile config. A malformed entry reports "absent" so a
// hand-edited file degrades to the default instead of refusing to start.

func persistedFloat(profileDir, key string) (float64, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(encoded, &value); err != nil {
		return 0, false
	}
	return value, true
}

func persistedInt(profileDir, key string) (int, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return 0, false
	}
	var value int
	if err := json.Unmarshal(encoded, &value); err != nil {
		return 0, false
	}
	return value, true
}

func persistedBool(profileDir, key string) (bool, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(encoded, &value); err != nil {
		return false, false
	}
	return value, true
}

func persistedString(profileDir, key string) (string, bool) {
	encoded, ok := persistedValue(profileDir, key)
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return "", false
	}
	return value, true
}

func persistedDuration(profileDir, key string) (time.Duration, bool) {
	text, ok := persistedString(profileDir, key)
	if !ok {
		return 0, false
	}
	value, err := parseDuration(text)
	if err != nil {
		return 0, false
	}
	return value, true
}

func persistedValue(profileDir, key string) (json.RawMessage, bool) {
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return nil, false
	}
	encoded, ok := values[key]
	return encoded, ok
}
