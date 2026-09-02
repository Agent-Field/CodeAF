package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE SETTINGS PANEL: /settings, or ctrl+, — the FIRST of the three fullscreen
// pages this surface draws at every width, and the one the other two are
// modelled on. The second is the task page (taskview.go's [taskSheet]), which
// ctrl+. opens; the third is home (home.go's [homeView]), which /home and a
// double space open. The phone tier's status deck (statusdeck.go) and tool
// detail (expand.go) take the frame as well, but only at [tierPhone]. All of
// them take the frame WHOLE, and this file is where the grammar for doing that
// was written down.
//
// It is omp's INTERACTIONS sheet over aforge's own registry, and the whole of
// what this file adds to that registry is a UI SKIN: which tab a row belongs
// under, what it is called there, the one line it says about itself, and which
// widget answers it. NOT ONE SETTING IS DECLARED HERE. Every row comes from
// [config.Settings] — the same rows the v2 sheet renders, written through the
// same writers — because a second place to declare a knob is a second place for
// a knob to disagree with itself.
//
// Four rules hold the design together:
//
//   - THE MAP IS TOTAL over the registry. A row nobody placed would be a row
//     nobody could reach, so chrome_test.go fails the build when a registry key
//     has no [settingMeta]. A new setting lands as one registry row plus one
//     line in [settingUI], which is the same trade omp makes.
//   - The panel is MODAL and fullscreen. A settings sheet is not something you
//     read the conversation past, and the alternative — a bottom-anchored list
//     of twenty-eight rows — would have taken the frame anyway while pretending
//     not to. It was the ONLY such thing for a while and is not any more; the
//     rule it established is that a surface which takes the frame takes it
//     WHOLE, keyboard and pointer with it, and every fullscreen surface since
//     has been written to it. THE THREE ARE MUTUALLY EXCLUSIVE BY CONSTRUCTION:
//     opening any one of settings, the task page or home closes the other two
//     ([app.openSettings], [app.showTaskPlace] and [app.openHome] each say so),
//     because two pages that both believe they own the frame is a frame that
//     draws one and takes keys for the other.
//   - Every write goes through [config.Setting.Apply], which validates in plain
//     language and persists to the GLOBAL profile. The project layer
//     (<workspace>/.aforge-v3/config.json) is deliberately not writable from here:
//     it is a file a repository commits, and a panel that edited it would be
//     this surface committing to somebody's repository on their behalf.
//   - A refusal is SHOWN, never swallowed. A pinned row, a seam the door did
//     not wire, a number outside its band — each answers in the registry's own
//     words on the foot line.
//
// The two-space indent, the dim values and the single accent are styles.go's
// palette and nothing new: the panel is a list, and this surface already knows
// what a list looks like (palette.go's overlayRow draws every row here).

// The tabs, in the order docs/CHAT-V3.md Decision 6 names them.
const (
	// tabSession is this conversation: what it may run, what it may spend, and
	// which models answer the small calls it makes for itself.
	tabSession = "Session"
	// tabContext is what a model carries — the context law, whole.
	tabContext = "Context"
	// tabWorkspace is this machine and this project: what aforge does with its
	// own time here, and what it may reach on your behalf. It is NOT where money
	// lives any more, and that is the whole of docs/design/spending/DESIGN.md's
	// first complaint — twenty rows answering four questions, with the dollar
	// figures filed between `workers` and `memory floor`.
	tabWorkspace = "Workspace"
	// tabDisplay is how the surface draws itself and what it remembers of your
	// typing.
	tabDisplay = "Display"
	// tabSpending is MONEY AND NOTHING ELSE: what aforge may spend, per day, per
	// conversation, per plan, and on its own practice — with what the day has
	// actually cost at the top of it. It is the one editor money has, and every
	// door on this surface that names a rail lands on one of its rows
	// (settingspend.go).
	tabSpending = "Spending"
	// tabSafety is what aforge may do without asking you first: the gate, its
	// exceptions, the model that answers for you, and the two clocks that answer
	// when nobody does.
	tabSafety = "Safety"
	// tabTasks is how work you can walk away from is run — how it starts, how it
	// is checked, how much of it happens at once, and on whose hands.
	tabTasks = "Tasks"
	// tabProviders is which model answers what.
	tabProviders = "Providers"
)

// And the sixth, which is not a reading of the registry at all: the accounts
// this profile has connected and what each of them may do (connectcaps.go). It
// is last because the five before it are one object read five ways, and a
// person walking the bar meets the knobs before their accounts.
// Spending, Safety and Tasks stand between Display and Providers, and Spending
// leads the three: "what may it spend" is asked before "on which machine", and
// before either of the two questions that used to share its tab.
var settingTabs = []string{tabSession, tabContext, tabWorkspace, tabDisplay,
	tabSpending, tabSafety, tabTasks, tabProviders, tabConnections}

// settingTabCategory is the ONE-TO-ONE map between the three new tabs and the
// three registry categories behind them, and it is the seam that keeps the skin
// honest about the one source of truth.
//
// The other tabs are a reading of the ROWS and not of the categories — "session
// ceiling" is a dollar figure that answers "what may THIS conversation do" — and
// that stays true of them. These three are different: the registry's own words
// for them (`spending`, `safety`, `tasks`) are already the product's words for
// them, so a row that is filed under one and drawn under another would be two
// answers to one question. chrome_test.go pins the map in both directions.
var settingTabCategory = map[string]string{
	tabSpending: config.CategorySpending,
	tabSafety:   config.CategorySafety,
	tabTasks:    config.CategoryTasks,
}

// settingWidget is how a row is ANSWERED, which is not quite how it reads.
// The registry's [config.SettingKind] says what a value is; this says what the
// keyboard does to it.
type settingWidget uint8

const (
	// widgetText opens the one-line submenu: enter saves, empty clears, esc
	// cancels. It is the default because most rows are a word or a number.
	widgetText settingWidget = iota
	// widgetToggle flips in place on enter or space. Booleans only.
	widgetToggle
	// widgetCycle walks a short enum in place, in the registry's own order.
	widgetCycle
	// widgetLane walks the four answers to "which machine behind this model" —
	// auto, pinned, pinned but borrowable, and openrouter. It is not
	// [widgetCycle] because two of the four carry a NAME the registry cannot
	// list: the lanes come from what has been measured, and a row of static
	// choices could only offer the ones somebody thought of on the day.
	widgetLane
	// widgetSelect opens THE MODEL PICKER — the same component /model opens
	// (palette.go), filter box, ranking, and rows carrying window, price and
	// arena score. A slot row is a model choice, and a model choice is a thing
	// this surface already knows how to ask; a second, plainer list would be a
	// worse way to answer the same question in the same product.
	widgetSelect
)

// settingMeta is the UI half of a registry row: where it is shown, what it is
// called there, the one line under it, and the widget that answers it.
//
// label and about may be empty, and then the registry's own Label and the first
// sentence of its Hint are used. That is not laziness — the registry's words are
// already the product's words (internal/config, rule 14), and repeating them
// here would be a second copy to keep in step. The overrides exist for the rows
// whose registry hint is three sentences long: a panel row gets ONE line.
// Masking is deliberately NOT one of these fields. A credential row masks
// itself — [config.Setting] carries Secret and its reader already hands back
// dots and a tail — so this panel never sees the key at all, which is the only
// arrangement in which it cannot leak one. What the flag buys HERE is the edit
// box: it opens on the row's displayed value (the mask), and the registry's
// writer treats an unchanged mask as "no change", so enter on a row somebody
// only looked at does not overwrite their key with a row of bullets.
type settingMeta struct {
	tab    string
	label  string
	about  string
	widget settingWidget
}

// settingUI is the skin: registry key → where it lives and how it is answered.
//
// The tabs are a reading of the rows and not of the categories: internal/config
// groups by what a row IS (models, spending, practice, interface); a person
// opening this panel is looking for what a row is ABOUT. "session ceiling" is a
// dollar figure and it lives under Session, because the question it answers is
// "what may THIS conversation do".
var settingUI = map[string]settingMeta{
	// ── Session ─────────────────────────────────────────────────────────────
	config.KeyToolApprovalMode: {
		tab: tabSafety, label: "ask before running", widget: widgetCycle,
		about: "what happens when the model asks to run a tool. Dangerous shell " +
			"commands are asked about whichever way this is set.",
	},
	config.KeyToolApprovals: {
		tab: tabSafety, label: "tool exceptions", widget: widgetText,
		about: "exceptions to the answer above, one per tool: read:allow, bash:prompt.",
	},
	// And under the tool exceptions, the exceptions for the one tool a per-tool
	// answer cannot really answer. It is where "always, this command" on an
	// approval question lands, so it is also where a person comes to take one
	// back: this is the row the card's receipt sends them to.
	config.KeyBashApprovals: {
		tab: tabSafety, label: "shell command rules", widget: widgetText,
		about: "answers for single shell commands, first match wins: " +
			"allow git status*, deny rm -rf *.",
	},
	// It sits directly under the two rows it modifies, because that is what it
	// is: not a fourth approval mode but a filter in front of the one above —
	// it can only spare you a question, never answer one those rows refuse.
	config.KeyGuardian: {
		tab: tabSafety, label: "guardian", widget: widgetCycle,
		about: "asks a small model first whether a call is plainly safe, so you " +
			"are only asked about the rest.",
	},
	// And directly under those three, because it is the last thing that can
	// happen to the question they raise: the clock that answers it when nobody
	// does. It is a countdown toward NO — the row above can spare you a
	// question, this one refuses on your behalf — which is why it sits here and
	// not beside the task countdown it otherwise looks like.
	config.KeyConsentTimeout: {
		tab: tabSafety, label: "approval countdown", widget: widgetText,
		about: "seconds an approval question waits before it answers no for you. " +
			"Any key stops the clock; 0 turns it off.",
	},
	config.KeyBashBackgroundAfter: {
		tab: tabSafety, label: "background after", widget: widgetText,
		about: config.BashBackgroundAfterHint,
	},
	// THE FOUR ssh ROWS ARE THIS CONVERSATION'S TOO, because a connection to
	// another machine is a property of the session that runs over it and of
	// nothing else on this screen — a change lands on the next launch, which the
	// registry's own hint says. They sit together, after the rows about what a
	// session may do, and they are text and a cycle for the same reason the
	// countdown above is text: a number a person types, and one choice they turn.
	config.KeySSHControlPersist: {
		tab: tabSession, label: "ssh reuse", widget: widgetText,
		about: "seconds an ssh connection stays reusable after it closes, so a quick " +
			"reconnect skips the handshake. 0 turns it off; a change lands next launch.",
	},
	config.KeySSHServerAlive: {
		tab: tabSession, label: "ssh heartbeat", widget: widgetText,
		about: "seconds of silence before ssh asks whether the far machine is still there. " +
			"0 turns heartbeats off; a change lands next launch.",
	},
	config.KeySSHServerMisses: {
		tab: tabSession, label: "ssh missed heartbeats", widget: widgetText,
		about: "how many unanswered heartbeats end a dead connection — three with the " +
			"default heartbeat notices one in about nine seconds. A change lands next launch.",
	},
	config.KeySSHIPQoS: {
		tab: tabSession, label: "ssh traffic", widget: widgetCycle,
		about: "how ssh marks its traffic: lowdelay by default, af21 on networks that honor " +
			"it, none where marking is filtered. A change lands next launch.",
	},
	// THE MACHINERY IS NOT THE SURFACE'S TO NAME, and a settings row is as much
	// the surface as a card is. The key is the engine's ([config.KeyTaskAudit])
	// and it keeps its name; what a person reads is what the switch DOES to their
	// work — the task's own claim that it is finished is taken on trust, or it is
	// not — because that, and not the shape of the apparatus behind it, is the
	// thing they are being asked to decide.
	// And above the check, the other end of a task's life: what happens the
	// moment you type /task. Both answers start ONE worker — the planned-graph
	// answer went with the road it named — so what a person is choosing between is
	// whether their brief is READ for width before that worker starts, and the row
	// is written as that question rather than as a switch over machinery.
	config.KeyTaskStart: {
		tab: tabTasks, label: "starting a task", widget: widgetCycle,
		about: "what /task does with your brief: sized reads it for width first, so " +
			"the one worker that starts can hand the parts out once it has opened the " +
			"material, single starts that worker without reading the brief at all.",
	},
	config.KeyTaskAudit: {
		tab: tabTasks, label: "check task work", widget: widgetCycle,
		about: "each task's work is checked over before it merges. " +
			"Off merges on the task's own word.",
	},
	// And under the check, the row that says what happens when the check came
	// back with nothing. It reads as a question about WHO — you, or the chat —
	// because that is the thing a person is deciding here; the state it is about
	// is spelled the way the card and the roster spell it, "needs your look",
	// rather than as the machinery that could not answer.
	config.KeyTaskSettle: {
		tab: tabSafety, label: "who settles work that needs a look", widget: widgetCycle,
		about: "ask puts it on the landed card for you. auto lets the chat read the " +
			"work and decide, and ask you only when it cannot tell.",
	},
	config.KeyMemoryEnabled: {
		tab: tabSession, label: "memory", widget: widgetCycle,
		about: "a few things are carried from one conversation to the next. " +
			"Off, each one starts knowing nothing about you.",
	},
	// The countdown sits under the gate rows for the same reason the guardian
	// does: it is not a fourth approval mode but the OTHER clock in the room —
	// how long a proposed task waits for you before it starts on its own.
	config.KeyTaskAutoApprove: {
		tab: tabSafety, label: "task countdown", widget: widgetText,
		about: "seconds a proposed task waits for you before it starts. " +
			"0 waits for your answer instead.",
	},
	// And under the countdown, what happens at the OTHER end of a task: how many
	// times work that came back with something missing is sent back to finish it.
	config.KeyTaskRepairRounds: {
		tab: tabTasks, label: "task repair rounds", widget: widgetText,
		about: "times a task that came back with something missing is sent back to " +
			"finish it before it lands as incomplete. 0 lets the first gap end it.",
	},
	// And the three that say how much of it happens at once: the number you may
	// name, and the two readings of the machine that hold the next one back
	// whatever you named.
	config.KeyTaskParallel: {
		tab: tabTasks, label: "tasks at once", widget: widgetText,
		about: "how many tasks may run at the same time. Blank is no limit — the machine " +
			"and the provider are the real ceilings.",
	},
	config.KeyTaskMaxLoad: {
		tab: tabTasks, label: "busy machine", widget: widgetText,
		about: "the load per core at which new tasks wait instead of starting. Running " +
			"tasks are never touched. 0 stops watching.",
	},
	config.KeyTaskMinFreeMB: {
		tab: tabTasks, label: "memory floor", widget: widgetText,
		about: "MB of memory that must be free before another task starts. 0 stops " +
			"watching.",
	},
	// And beside it, the other thing that is true of every task you hand off:
	// whose hands it goes into. It is answered by the PICKER, like the two tier
	// rows below it and for the same reason — a row that asks "which model" and
	// offers a blank line is asking a person to be the catalog.
	config.KeyTaskModel: {
		tab: tabTasks, label: "task model", widget: widgetSelect,
		about: "the model a task runs on when you have not asked for another. " +
			"Blank runs it on the model you are talking to.",
	},
	// THE CREW LIVES ON THE PROVIDERS TAB, with the model it answers under.
	//
	// It was on Session for four waves, one tab away from the row that says which
	// model the conversation is on — so "which model does the planning" and
	// "which model am I talking to" were two errands on two screens, and a person
	// comparing them had to remember one while they walked to the other. They are
	// one question: which model answers what (docs/CHAT-V3.md Decision 6 gives
	// this tab exactly that job, per-role models included). [modelsSection] is
	// the order they read in.
	//
	// EVERY ONE OF THEM IS A MODEL CHOICE AND IS ANSWERED AS ONE. The tier rows
	// were a text box for four waves, which meant the only way to name the cheap
	// model was to type its id from memory — in a panel that already knows every
	// id, what each one holds, what it costs and how it scores.
	config.KeyCrew: {
		tab: tabProviders, label: "crew", widget: widgetCycle,
		// The one line under this row NAMES ALL THREE OPTIONS AND WHAT EACH ONE IS,
		// and it is built in [init] from the preset table rather than written here:
		// a cycle row walks its choices in place, so there is no moment at which a
		// person is shown three rows to compare — the only place the comparison can
		// happen is the line they are reading while they press the key.
	},
	config.KeyTierReflexModel: {
		tab: tabProviders, label: "reflex", widget: widgetSelect,
		about: "near-free · reads every turn — memory, titles, safety",
	},
	config.KeyTierLowModel: {
		tab: tabProviders, label: "small work", widget: widgetSelect,
		about: "cheap · the small calls — names, digests, the safety gate",
	},
	config.KeyTierWorkerModel: {
		tab: tabProviders, label: "worker", widget: widgetSelect,
		about: "does the work · every task, its parts, every run node — most of the bill",
	},
	config.KeyTierHighModel: {
		tab: tabProviders, label: "careful work", widget: widgetSelect,
		about: "careful · checks what must not be wrong — audits, compaction, vision",
	},
	// The fourth class is the one whose value may name a LEVEL as well as a
	// model, so it is a TEXT box and not a picker: the picker returns an id, and
	// `moonshotai/kimi-k3:high` is an id with an instruction on it. ctrl+t in the
	// picker dials the CONVERSATION's effort and lives on the session; this one is
	// written down and outlives it.
	config.KeyTierMastermindModel: {
		tab: tabProviders, label: "mastermind", widget: widgetText,
		about: "thinks · plans runs and designs harnesses — add :low, :medium or :high",
	},
	config.KeyModelRoles: {
		tab: tabProviders, label: "pinned roles", widget: widgetText,
		about: "exceptions to the five rows above, one per role: title:openai/gpt-5-mini.",
	},
	// It is a TEXT box and not a select, unlike the three class rows on Providers,
	// because the answer is an ORDER rather than a choice: a picker that returns
	// one id cannot express "this one, then that one", and a fallback list of one
	// is most of what makes this row worth having.
	config.KeyModelFallbacks: {
		tab: tabSession, label: "fallback models", widget: widgetText,
		about: "where a conversation goes when no endpoint will take the request: " +
			"slugs, comma-separated, first tried first. Blank picks the nearest one.",
	},
	// It sits with the model rows and not with the approval ones because the
	// question it answers is about a MODEL'S OUTPUT rather than about what aforge
	// is allowed to do on your behalf: the row decides what happens when the
	// model on the row above stops writing language.
	config.KeyReplyGuard: {
		tab: tabProviders, label: "reply guard", widget: widgetCycle,
		about: "on cuts a reply that has come apart — one line or one letter " +
			"repeated, alphabets mixed inside words — throws it away and asks " +
			"once more. Code blocks are never judged.",
	},

	// ── Context ─────────────────────────────────────────────────────────────
	//
	// The four rows of the context law, which is where compaction is actually
	// configured: fill decides WHEN a conversation is compacted, and the other
	// three decide what a call carries when it is.
	config.KeyContextFill: {
		tab: tabContext, label: "compact at", widget: widgetText,
		about: "how much of the model's window aforge fills before it compacts, " +
			"as a percent. The rest stays as thinking and answer room.",
	},
	config.KeyCompletionReserve: {
		tab: tabContext, label: "answer room", widget: widgetText,
		about: "tokens every call keeps free for its answer and its reasoning.",
	},
	config.KeyWorkingSet: {
		tab: tabContext, label: "working set", widget: widgetText,
		about: "the most material kept quoted in front of a worker at once, " +
			"however large the model's window is.",
	},
	// THE UNIT IS A MULTIPLE AND THE PANEL SAYS SO. The row reads 250 by
	// default, and one line of "as a percent" over that number reads as a
	// percentage OF something — of a window, of a budget — which makes 250 look
	// like a mistake or like tokens mislabelled. It is neither: the registry's
	// figure is cumulative re-sends of the whole context expressed in hundredths,
	// so 100 is once and 250 is two and a half times over, and it is floored at
	// 100 rather than capped at 100 (internal/config's writeContextReuse: below
	// one whole context it is a refusal, not a governor). The label carries the
	// worked example, because the number a person sees is 250 and the sentence
	// under it has one job — making that number mean something.
	config.KeyContextReuse: {
		tab: tabContext, label: "context reuse", widget: widgetText,
		about: "how many times over one piece of work may re-send its whole " +
			"context before aforge tells it to land: 100 is once, 250 is two and " +
			"a half times. At least 100.",
	},
	// Search is a context row for the reason the four above it are: it decides
	// what a model can put IN its context that it did not already have.
	config.KeySearchProvider: {
		tab: tabContext, label: "searching", widget: widgetCycle,
		about: "where a web search goes. auto uses the best back end your keys " +
			"reach and falls back to one that needs none.",
	},
	// THE KEY EVERY CALL RIDES sits on the Providers tab above the three search
	// keys: it is the credential the browser connection or a manual paste writes
	// (firstrun.go), and remains the replacement door after that.
	config.KeyAPIKey: {
		tab: tabProviders, label: "openrouter key", widget: widgetText,
		about: "the key aforge talks to models with. A missing default key opens " +
			"connect openrouter in your browser; paste a replacement here if needed. " +
			"A change lands on this conversation at once.",
	},
	config.KeyExaKey: {
		tab: tabContext, label: "exa key", widget: widgetText,
		about: "an exa.ai key, which buys better results and page fetches than " +
			"the free back end. Optional.",
	},
	config.KeyFirecrawlKey: {
		tab: tabContext, label: "firecrawl key", widget: widgetText,
		about: "a firecrawl.dev key, for when the free monthly allowance runs " +
			"out. Optional.",
	},
	config.KeyJinaKey: {
		tab: tabContext, label: "jina key", widget: widgetText,
		about: "a jina.ai key. It buys nothing but headroom: page fetches already " +
			"work unauthenticated.",
	},

	// ── Spending ────────────────────────────────────────────────────────────
	//
	// MONEY, AND NOTHING THAT IS NOT MONEY. The four rows are the four rails a
	// person can actually turn, and the label of each one is THE SCOPE it bounds
	// — per day, per conversation, per plan, practice — because that is the
	// question being asked and `daily budget` / `session ceiling` are the names
	// of the keys behind it. The order they read in, and the three readings that
	// stand between them, are settingspend.go's ([spendingItems]).
	//
	// EVERY HINT ENDS WITH WHAT HAPPENS AT THE LINE. A rail whose consequence is
	// unstated is a surprise rather than a setting, so each of these says what
	// the moment of reaching it looks like — waits, asks, refuses, stops — and
	// the registry's own hint says the same thing at more length.
	config.KeyDailyBudget: {
		tab: tabSpending, label: "per day", widget: widgetText,
		about: "what aforge may spend on your work in a day. When the day's calls " +
			"reach it, new work waits for midnight or for you to raise it here. " +
			"none removes the limit.",
	},
	config.KeyPlanConsent: {
		tab: tabSpending, label: "per plan", widget: widgetText,
		about: "above this estimate a planned job quotes its step count and its " +
			"price and waits for your go-ahead — it asks, it does not stop. " +
			"none never asks.",
	},
	config.KeyPracticeBudget: {
		tab: tabSpending, label: "practice", widget: widgetText,
		about: "the slice of the day aforge may spend practicing on itself. When " +
			"it is gone practice stops until tomorrow and your own work is " +
			"untouched. 0 here turns practice off rather than uncapping it.",
	},
	// It is `per conversation` and not `session ceiling` for this tab's whole
	// reason: the label is the SCOPE and the person is reading a column of
	// scopes. It sat on Session for four waves, one tab away from every other
	// figure it is compared against.
	config.KeySpendRail: {
		tab: tabSpending, label: "per conversation", widget: widgetText,
		about: "what one conversation may spend before it stops starting turns. " +
			"The turn in flight always finishes and your message stays yours to " +
			"send again. none removes the limit.",
	},

	// ── Workspace ───────────────────────────────────────────────────────────
	//
	// This machine and this project: what aforge does with its own time here,
	// and what it may reach on your behalf.
	config.KeyPracticeIdle: {
		tab: tabWorkspace, label: "quiet before practice", widget: widgetText,
		about: "how long the room stays quiet before aforge starts practicing.",
	},
	config.KeyBriefAfter: {
		tab: tabWorkspace, label: "arrival brief after", widget: widgetText,
		about: "how long you have to be away before aforge greets you with a " +
			"summary. 0 always briefs.",
	},
	config.KeyTenureAfter: {
		tab: tabWorkspace, label: "tenure after", widget: widgetText,
		about: "how many clean firings a standing charter needs before it earns tenure.",
	},
	// And beside it, the switch on the whole ambient side's timing. It is on
	// this tab rather than under Session because it is not about this
	// conversation at all: it is about what happens on this machine when there
	// is no conversation. The line says what it DOES rather than what it
	// installs — the row's own hint names the launchd agent and the systemd
	// timer for anybody who wants to go and look.
	config.KeyStandingBackground: {
		tab: tabWorkspace, label: "background checks", widget: widgetCycle,
		about: "reminders, watches and routines are checked every " +
			everyWord(standing.Interval) + " with no window open. " +
			"Off checks only while one is.",
	},
	config.KeyAttribution: {
		tab: tabWorkspace, label: "attribution", widget: widgetToggle,
		about: "signs the commits and PRs aforge writes for you — one trailer, " +
			"one footer line.",
	},
	// The three rows Google and Slack connections are signed with. They belong on this tab
	// and not under Providers because they are not about which model answers
	// what: they are about what aforge may REACH on your behalf, which is the
	// question this tab already holds.
	//
	// Neither of them is where a person connects an account — /connect is, and it
	// asks nothing but a keypress. These are for somebody signing in through
	// their own Google or Slack application rather than the ones aforge ships
	// with, which is a setting and not a step.
	config.KeyGoogleOAuthClient: {
		tab: tabWorkspace, label: "google sign-in id", widget: widgetText,
		about: "identifies aforge to Google when you connect an account. Blank " +
			"uses the one aforge ships with.",
	},
	config.KeyGoogleOAuthSecret: {
		tab: tabWorkspace, label: "google sign-in secret", widget: widgetText,
		about: "the secret that goes with the id above. It is kept masked once saved.",
	},
	config.KeySlackOAuthClient: {
		tab: tabWorkspace, label: "slack sign-in id", widget: widgetText,
		about: "identifies aforge to Slack when you connect a workspace. Blank uses " +
			"the one aforge ships with.",
	},

	// ── Display ─────────────────────────────────────────────────────────────
	config.KeyHistoryEnabled: {
		tab: tabDisplay, label: "input history", widget: widgetToggle,
		about: "remembers the messages you send, so the up arrow walks them back " +
			"in a later session.",
	},
	config.KeyDraftPersist: {
		tab: tabDisplay, label: "keep drafts", widget: widgetToggle,
		about: "keeps the half-typed message in the box across a restart, per directory.",
	},
	config.KeyTaskColumn: {
		tab: tabDisplay, label: "task column", widget: widgetToggle,
		about: "stands the task roster beside the chat. With no foreground command " +
			"to background, ctrl+g closes it and brings it back; this is where the answer is remembered.",
	},
	config.KeyQuickSwitch: {
		tab: tabDisplay, label: "quick switch", widget: widgetToggle,
		about: "ctrl+k switches conversations on the press — pause and the card " +
			"fades, esc goes back. Off, it opens the card and waits for enter.",
	},
	config.KeyHints: {
		tab: tabDisplay, label: "hints", widget: widgetToggle,
		about: "one-line tips above the box until you have used what each one " +
			"teaches. Off silences them, and what's-new lines with them.",
	},
	config.KeySplitPct: {
		tab: tabDisplay, label: "chat width", widget: widgetText,
		about: "the chat pane's share of the frame while the task rail is open.",
	},

	// ── Providers ───────────────────────────────────────────────────────────
	//
	// The model slots themselves are added by [init] from [config.ModelSlots],
	// so a sixth role or a sixth modality reaches this panel without anybody
	// editing this file — the same contract internal/config's own sheet keeps.
	//
	// EVERY ONE OF THEM ASKS ITS OWN QUESTION ([filterFor]). Five of these rows
	// are not conversations at all — drawing, speaking, composing, filming, and
	// the one that hears you — and the looking row wants a model that can SEE.
	// Answering all seven with the chat law, which is what this panel did, does
	// not give a media slot a list that is merely too wide: it gives it the exact
	// complement of the rows that could answer it.
	config.KeyMouse: {
		tab: tabDisplay, label: "mouse", widget: widgetCycle,
		about: "on gives hover and click; off gives the terminal's own text selection back.",
	},
	config.KeyTimestamps: {
		tab: tabDisplay, label: "timestamps", widget: widgetCycle,
		about: "footers puts a receipt under each finished turn; separators only marks the gaps.",
	},
	config.KeyWork: {
		tab: tabDisplay, label: "turn work", widget: widgetCycle,
		about: "fold completed turn machinery into one worked chip, or keep it open.",
	},
	config.KeyVisionModel: {
		tab: tabProviders, label: "looking", widget: widgetSelect,
		about: "the model that looks at images. Blank picks one that can see.",
	},
	config.KeyDocumentEngine: {
		tab: tabProviders, label: "reading", widget: widgetCycle,
		about: "which rung reads your documents. auto walks local, then free, then paid OCR.",
	},
	// HOW HARD EVERYTHING ON THIS MACHINE THINKS, on the tab that lists the
	// models it thinks with. The rung is not a model and not a price, but it is
	// the other half of what a call is made of, and a person who has just picked
	// a model is exactly the person deciding how hard to work it. It is the LAST
	// scope the resolver consults (internal/effort) and therefore the answer for
	// every conversation nobody has dialled by hand.
	//
	// IT IS ONE OF SEVERAL DOORS ONTO ONE LADDER and it says so plainly, because
	// a setting a person can reach many ways has to read the same in all of them:
	// this row and the same [effortKey] chord on home with the cursor at rest
	// (homeeffort.go) both move THIS row, while the nearer scopes that outrank it
	// — a conversation's own rung above the message box (effortchip.go), a task's
	// (taskeffort.go), a standing item's (homeband_thinking.go) — take the same
	// chord over their own surfaces. A row that set a default without saying the
	// default could be overridden is a row people come back to confused.
	config.KeyEffort: {
		tab: tabProviders, label: "thinking", widget: widgetCycle,
		about: "how hard the model thinks, unless something nearer the work says " +
			"otherwise. " + effortKey + " moves the rung of whatever you stand on — the chip " +
			"above the message box for one conversation, a task, a standing item, or home " +
			"with the cursor on no row, which is this same row.",
	},
	// It belongs on this tab and not under Session because it is a question
	// about WHERE a request goes, not about what this conversation may do: one
	// model id is served by many endpoints, and this is which of their
	// differences the session pays attention to.
	config.KeyRouting: {
		tab: tabProviders, label: "routing", widget: widgetCycle,
		about: "one model is served by many endpoints. latency asks for the fastest and " +
			"demotes one that keeps being slow; price asks for the cheapest; off asks for " +
			"nothing, measures nothing, and leaves the two rows above it with no machine to name.",
	},
	// AND UNDER IT, THE MACHINE ITSELF. routing is about what every request
	// prefers; this is about which endpoint your conversation actually lands on.
	config.LaneSettingKey(talkSlot): {
		tab: tabProviders, label: "lane", widget: widgetLane,
		about: "which machine behind your model answers you. auto picks the fastest one " +
			"each answer; enter opens them all with what has been measured of each.",
	},
	config.KeyLaneGuard: {
		tab: tabProviders, label: "speed guard", widget: widgetToggle,
		about: "an answer that is slow to start is asked of the next-best machine as well, " +
			"and you read whichever replies first. One extra call, under a tenth of spend.",
	},
}

func init() {
	for _, slot := range config.ModelSlots() {
		settingUI[config.ModelSettingKey(slot.Slot)] = settingMeta{
			tab: tabProviders, label: slot.Label, widget: widgetSelect,
		}
	}
	// AND THE ONE SLOT THAT IS NOT A SLOT TO A READER. The registry calls it
	// "conversation", which is what it binds; a person opening this tab reads a
	// list of models aforge uses and wants to know which one is theirs. It is the
	// same row, the same write, the same live seam onto [app.switchModel] — only
	// the word above the Models section changed.
	crew := settingUI[config.KeyCrew]
	crew.about = crewAbout()
	settingUI[config.KeyCrew] = crew

	talk := settingUI[config.ModelSettingKey(talkSlot)]
	talk.label = "your model"
	talk.about = "the model you are talking to. Everything below it is a model aforge " +
		"uses on your behalf."
	settingUI[config.ModelSettingKey(talkSlot)] = talk
}

// crewAbout is the crew row's one line: the five rows it writes, then each preset
// with its own sentence, then what makes the row read custom. The sentences are
// [config.CrewLine]'s, so the panel and /crew say the same words about the same
// thing (internal/config's crew.go holds the table).
func crewAbout() string {
	said := make([]string, 0, len(config.CrewPresets))
	for _, preset := range config.CrewPresets {
		said = append(said, preset+" — "+config.CrewLine(preset))
	}
	// The three options lead, because they are what the keypress chooses between
	// and the panel gives a row's line the width it has: what gets cut on a narrow
	// terminal should be the footnote, not the choice.
	return "the five below, chosen as one word: " + strings.Join(said, "; ") +
		". Answer one yourself and this reads custom."
}

// modelsSection is the order the Models rows LEAD the Providers tab in: your
// model, the crew word, the five classes in [roles.Tiers] order, and then the
// pins with the roles list hanging off them.
//
// It exists because registry order is not reading order. internal/config builds
// the model slot rows first and the tier rows a hundred lines later, which is the
// order they were written rather than the order a person meets them — and the
// crew only makes sense read directly above the five rows it writes. Every other
// row on the tab follows in registry order, so a row nobody placed here is still
// reachable rather than dropped ([sheet.build] states that).
//
// THE FOUR CLASSES COME FROM [roles.Tiers] and are not listed again here. A fifth
// tier is one line in that package and no lines in this one, which is the same
// contract the model slots keep.
var modelsSection = modelsSectionOrder()

func modelsSectionOrder() []string {
	// THE MACHINE COMES DIRECTLY UNDER THE MODEL, and that is the whole of why
	// this list exists at all. In registry order these three sat at the FOOT of
	// the tab, under the crew, the four classes and every role aforge has —
	// forty rows below the one they are about — so a person who changed their
	// model never met the row saying which endpoint would serve it. They read
	// narrowest first: which machine answers THIS conversation, what `auto` may
	// spend to keep an answer moving, and then what every request prefers.
	//
	// THEY LIVE HERE AND NOWHERE ELSE. A row on two tabs is two places to look
	// for one answer and two rows that can disagree on screen.
	order := []string{
		config.ModelSettingKey(talkSlot),
		config.LaneSettingKey(talkSlot),
		config.KeyLaneGuard,
		config.KeyRouting,
		config.KeyCrew,
	}
	for _, tier := range roles.Tiers {
		order = append(order, tierSettingKey(tier))
	}
	return append(order, config.KeyModelRoles)
}

// tierSettingKey is the registry row one tier is set by. It is total over
// [roles.Tiers] and is the ONE place the mapping is written — the panel's tier
// word, its own roles source and the section order all read it, and a fifth tier
// with no row here would fail the build's own totality check rather than quietly
// read the cheap row.
func tierSettingKey(tier roles.Tier) string {
	switch tier {
	case roles.TierHigh:
		return config.KeyTierHighModel
	case roles.TierWorker:
		return config.KeyTierWorkerModel
	case roles.TierReflex:
		return config.KeyTierReflexModel
	case roles.TierMastermind:
		return config.KeyTierMastermindModel
	}
	return config.KeyTierLowModel
}

// settingMetaFor is the skin for one row, with the registry's own words filled
// in where the map left them out.
func settingMetaFor(row config.Setting) (settingMeta, bool) {
	meta, ok := settingUI[row.Key]
	if !ok {
		return settingMeta{}, false
	}
	if meta.label == "" {
		meta.label = row.Label
	}
	if meta.about == "" {
		meta.about = firstSentence(row.Hint)
	}
	return meta, true
}

// firstSentence is the registry hint cut to one line. A hint is written as
// prose for a sheet with room; a panel row has one line and takes the sentence
// that carries the meaning.
func firstSentence(hint string) string {
	hint = strings.TrimSpace(hint)
	if at := strings.IndexByte(hint, '.'); at > 0 {
		return hint[:at+1]
	}
	return hint
}

// sheetRows is how many list rows the panel wants at most. It is a ceiling and
// not a promise: the panel is fullscreen, so what it actually draws is whatever
// the terminal has after the head and the foot.
const sheetRows = 16

// sheetItem is one line of the list: a row, or — while a search is on — the
// faint tab heading a group of them sits under.
type sheetItem struct {
	head string
	row  config.Setting
	meta settingMeta
	// conn is set on the rows of the Connections tab, which are accounts rather
	// than registry rows (connectcaps.go). It hangs here so that the cursor
	// walk, the scroll, the pointer and the hover need to know nothing about
	// them: an item is an item, and only what DRAWS it and what ANSWERS it ask
	// which kind this one is.
	conn *connRow
	// role is set on the rows of the roles section, on exactly those terms.
	role *roleRow
	// read is set on a row of the Spending tab that is a RECEIPT and not a
	// setting — `today`, and the two rails this build has but does not keep a
	// registry row for (settingspend.go). It hangs here for [sheetItem.conn]'s
	// reason: an item is an item, and only what draws it and what the cursor
	// does with it ask which kind this one is.
	read *railReading
}

func (i sheetItem) heading() bool { return i.head != "" }

// restful reports whether the CURSOR MAY STOP HERE. A heading is a label, and a
// reading is a fact — neither is a thing `enter` could do anything to — so the
// walk steps over both, which is the whole of what makes `today` "not
// selectable" (DESIGN.md, the Spending tab's first rule).
func (i sheetItem) restful() bool { return i.head == "" && i.read == nil }

// sheet is the panel's whole state. The zero value is closed and costs the
// frame nothing.
type sheet struct {
	tab int

	registry *config.Settings
	// profileDir is retained only for live explanations derived from several
	// rows. The registry remains the writer and reader of every individual row.
	profileDir string
	// conns is the door onto the accounts, for the Connections tab. It is the
	// surface's own door (app.conns) and not a second one: two readings of "is
	// this connected" is how a tab and a panel disagree about somebody's mail.
	conns Connections
	// conn is what that tab remembers between builds (connectcaps.go).
	conn connTab
	rows []config.Setting
	// defaults is every row's reading on a profile nobody has touched, so a row
	// that differs from it can be marked. See [settingDefaults].
	defaults map[string]string
	// sessionModel is the model this conversation is on — the FLOOR of every
	// role's ladder ([roles.Resolve]), and therefore what a role row resolves to
	// when nothing above it is set. It is taken once, when the panel opens: the
	// rows below it are about settings, and a value that changed under a person
	// reading them would be a list that moved while they looked at it.
	sessionModel string

	// items is the current list — one tab's rows, or every tab's matches under
	// their headings while a search is on. cursor indexes it and skips headings.
	items  []sheetItem
	cursor int
	top    int

	// query is the type-to-search box. It filters across ALL tabs; the tab bar
	// follows the first match so that leaving the search leaves you where the
	// thing you found lives.
	query editor

	// edit is the text submenu and sel the select submenu. At most one is open,
	// and while one is, it owns the keyboard.
	edit *sheetEdit
	sel  *sheetSelect

	// today is the Spending tab's first row: what the day has cost, against what
	// it is allowed. It is TAKEN ONCE, when the panel opens (settingspend.go's
	// [app.readDayCost]), for the reason [sheet.sessionModel] is taken once — the
	// rows below it are about settings, and a figure that moved under a person
	// reading them would be a list that changed while they looked at it. Nil is
	// a day nothing has counted, and the row is then absent.
	today *railReading

	// msg is the last refusal, in the registry's own words.
	msg string
}

// sheetEdit is the one-line text submenu.
type sheetEdit struct {
	key    string
	label  string
	secret bool
	box    editor
}

// sheetSelect is a model slot being answered: which registry row is being
// written, what it is called on the panel, and THE PICKER ITSELF — the same
// [picker] /model opens, not a copy of it.
//
// It was a list of id strings with a substring filter over it, which is to say
// a second, worse picker: no ranking, and rows that said nothing about the
// models they named. A person choosing which model does the careful work is
// asking the same three questions they ask in /model — how much does it hold,
// what does it cost, is it any good — and the answer is one component with two
// entry points.
type sheetSelect struct {
	key   string
	label string
	// role is set when the picker was opened from a role row. The key is still
	// the registry row that gets written — every pin lives in "pinned roles" —
	// and this says WHICH PAIR inside it the chosen model belongs to.
	role roles.Role
	// keep is the QUESTION THIS ROW ASKS of a model (models.go's [modelFilter]),
	// chosen from the key by [filterFor]. It is held rather than applied and
	// forgotten because it is the row's own meaning: "looking" is not a slot
	// that happens to have been opened over a shorter list, it is a slot that
	// only models which can see may answer.
	keep modelFilter
	pick picker
}

// filterFor is the WHOLE map from a settings row to the question its picker
// asks, and it is deliberately one function: a second slot with a modality of
// its own is one case here, and a slot nobody thought about gets the general
// chat law rather than the whole catalog.
//
// THE FIVE MEDIA SLOTS ARE THE REASON THIS IS A MAP AND NOT AN IF. Every one of
// them was answered with [chatModel], which is not "a list that was too wide" —
// it is the exact complement of the right list, so "drawing" offered a picker in
// which no row could draw. The slot words are [config.ModelSlots]'s own
// (modelslots.go's mediaSlotWords), and the predicates are models.go's; a sixth
// modality is one line in each place and no new list anywhere.
func filterFor(key string) modelFilter {
	switch key {
	case config.KeyVisionModel:
		return inspectsImages
	case config.ModelSettingKey("image"):
		return drawsImages
	case config.ModelSettingKey("speech"):
		return speaksAloud
	case config.ModelSettingKey("music"):
		return composesMusic
	case config.ModelSettingKey("video"):
		return filmsVideo
	case config.ModelSettingKey("voice"):
		return hearsSpeech
	}
	return chatModel
}

// choice is the id under the cursor. It answers a STRING and not a [Model]
// because that is what the registry writer takes: a slot row holds an id.
func (s *sheetSelect) choice() (string, bool) {
	model, ok := s.pick.choice()
	return model.ID, ok
}

// ── opening, and the registry behind it ─────────────────────────────────────

// pristineProfile is a profile directory that does not exist, and is never
// created. [config] answers a missing config.json with an empty map, so a
// registry built over it reads every row's built-in default — which is exactly
// the comparison the changed-mark needs, without this package having to know
// what any default IS or where the file lives.
var pristineProfile = filepath.Join(os.TempDir(), "openaf-settings-defaults-do-not-create")

// settingDefaults is every row as a profile nobody has touched reads it.
func settingDefaults() map[string]string {
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: pristineProfile})
	rows := registry.Rows()
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value()
	}
	return out
}

// registry is the settings this panel edits: the one the door handed over, or
// one built here over the profile directory it named.
//
// The two live seams it wires are the two this surface can honestly answer. The
// conversation model is the model in the status line, and setting it is the
// same road /model takes ([app.switchModel]) — one door, one effect. The five
// capability slots do not come through here at all: they are written down in
// the profile by the registry itself and read back by the use-time resolver
// (docs/MULTIMODAL.md Decision 5), which is what makes their writes ACCEPTED
// rather than refused. What is left is the ROLE slots, answered somewhere this
// process cannot reach, and they say so ([app.slotRefusal]).
func (a *app) registry() *config.Settings {
	if a.settings != nil {
		return a.settings
	}
	a.settings = config.NewSettings(config.SettingsOptions{
		ProfileDir: a.profileDir,
		ModelValue: func(slot string) string {
			if slot == talkSlot {
				return a.model
			}
			return ""
		},
		SetModel: func(slot, slug string) error {
			if slot != talkSlot {
				return a.slotRefusal()
			}
			a.switchModel(slug, 0)
			return nil
		},
		// THE TWO MONEY RECEIPTS: what the day has cost, and what this
		// conversation has. They are seams and not reads this package makes,
		// because a receipt is derived from a LIVE reading and internal/config
		// has no way to ask a running surface what it has spent.
		SpentTodayUSD:       a.spentTodayUSD,
		SpentThisSessionUSD: a.spentThisSessionUSD,
		// The `background checks` row's own hand: this machine's timer, read for
		// the row's value and turned by its write. Nil on a machine that cannot
		// have one, and the row is then absent rather than present and refusing
		// (internal/config's backgroundRow).
		BackgroundChecks: a.stands.Background,
		// A key written through the row — from the first-run screen or from the
		// sheet — reaches the running session here, in the same breath as the
		// file (firstrun.go's [app.handAPIKey]). It is the one row whose write
		// has a live half on this surface.
		Applied: func(key string) {
			a.touch()
			if key == config.KeyAPIKey {
				a.handAPIKey()
			}
			// AND THE CONVERSATION'S OWN CEILING IS RE-READ HERE and nowhere
			// else, so the status line's warm ink follows an edit without the
			// paint ever touching the disk (moneydoor.go).
			if key == config.KeySpendRail {
				a.readSpendRail()
			}
		},
	})
	return a.settings
}

// talkSlot is the model slot this surface is: the conversation.
const talkSlot = "talk"

// slotRefusal is what a slot this surface cannot write answers, and THE ONLY
// SLOTS LEFT IN IT ARE THE ROLES.
//
// The five media slots used to land here, and the refusal they got was the
// wrong sentence about the wrong thing: "that model is chosen where its session
// is opened" sent a person looking for a door that did not exist, and the
// version that named an environment variable was true only because the row
// itself was dead. Since Decision 5 they are not refused at all — a pick writes
// the slot key straight into the profile ([config.Settings]'s own model row) and
// the use-time resolver reads it on the very next picture.
//
// What remains is a ROLE bound in the roles table and resolved nowhere on this
// surface: planning, verification, naming. Those genuinely belong to the session
// that opens them, and the old sentence is the true one for them.
func (a *app) slotRefusal() error {
	return fmt.Errorf("that model is chosen where its session is opened")
}

// openSettings is /settings and ctrl+,, and it is THE ROUTER'S DOOR like every
// other way into a place (pages.go's [app.showPage]).
func (a *app) openSettings() { a.showPage(pageSettings) }

// raiseSettings builds the panel. It is [placeSettings]'s `open` and nothing
// else calls it, which is what makes the router the one road in.
func (a *app) raiseSettings() {
	// THE PANEL OPENS AND SAYS WHOSE ROWS THESE ARE. Over --host it edits this
	// machine's profile, and only some of these rows are about this machine: the
	// mouse, the timestamps, the draft and the history are the surface's own and
	// apply; the tool gate, the spend rail and the auxiliary models are the
	// SESSION's, and the session reads them from the profile on the other machine.
	// Closing the panel would take the working half away; opening it silently
	// would let somebody turn a gate off and watch it stay on (host.go).
	if a.hosted() {
		a.note(settingsRemoteWord)
	}
	// The day's own figure, read once for the whole of this visit (see
	// [app.spentTodayUSD]).
	a.readDayCost()
	// AND THIS CONVERSATION'S, off the same ledger and on the same beat, so the
	// `this one` receipt is as fresh as the day above it and says the same
	// number the status line's money segment does (settingspend.go's
	// [app.spentThisSessionUSD], issue #269). It is a tail read
	// ([session.UsageCache]) and it happens once per visit, never on a draw.
	a.readTreeSpend()
	a.sheet = sheet{
		registry:     a.registry(),
		profileDir:   a.profileDir,
		conns:        a.conns,
		defaults:     settingDefaults(),
		sessionModel: a.model,
		today:        a.todayReading(),
	}
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
	a.touch()
}

// closeSettings is the DOOR out of the panel, and it goes through the router.
func (a *app) closeSettings() {
	if a.at(pageSettings) {
		a.leavePlace()
	}
}

// dropSettings is [placeSettings]'s `close`. Nothing but the router calls it.
func (a *app) dropSettings() {
	a.sheet = sheet{}
	a.touch()
}

// standDownFullscreen closes every page that takes the frame at every width, so
// that the one about to open is alone in believing it owns it.
//
// THE LAW IS THAT THEY ARE MUTUALLY EXCLUSIVE: the seven places (pages.go) and
// the rewind timeline (rewindsheet.go) each take the frame WHOLE — keyboard and
// pointer with it — and view.go's [app.frame] can only draw one, so a second one
// opened underneath would take the keys of a page nobody can see. Every open
// path calls this FIRST and none of them tests for the others itself, because
// four pages each remembering to close three others is twelve places for the
// rule to be forgotten in, and the day one is is the day a person stacks home
// over the task page and finds esc goes to the wrong screen.
//
// IT USED TO NAME SEVEN PAGES AND ASK EACH ONE'S `open` FLAG. There is one flag
// now — [app.page] — so the places close through the router in one line, and
// what is left to name here is the one fullscreen page that is not a place.
//
// The phone tier's status deck and tool detail are deliberately not here: they
// take the frame only at [tierPhone] and are dismissed by their own keys, and a
// panel opened over one of them is a panel a person asked for while it was up.
func (a *app) standDownFullscreen() { a.showPage(pageNone) }

// standDownRest is every fullscreen page that is NOT one of the places, closed
// on the way into one. It is called by [app.showPage] rather than the other way
// round, which is what keeps the two out of a loop.
//
// THE REWIND TIMELINE IS ONE OF THEM, and it is not a place on purpose: it
// is a thing you do to THIS conversation rather than a room in the machine, and
// putting it in the tab bar would put a knife in the cutlery drawer (pages.go).
// It goes RESTORING the draft it is holding, because the sentence it stashed on
// the way in belongs to the person and not to the page that took it.
//
// A BACKGROUND JOB'S PAGE IS THE OTHER, and it is not a place for the same
// reason: it is one job this conversation started, reached from that
// conversation's own column, and a job that ends with the conversation would be
// a tab standing over nothing (jobpage.go). It holds no draft, so it simply
// closes.
func (a *app) standDownRest() {
	if a.rewSheet.open {
		a.closeRewindSheet(true)
	}
	a.closeJobPage()
}

func (s *sheet) searching() bool { return strings.TrimSpace(s.query.String()) != "" }

// build rebuilds the item list from the tab and the query.
//
// With no query it is one tab's rows in registry order. With one it is every
// tab's matches, each group under its own faint heading, and THE TAB FOLLOWS
// THE FIRST MATCH: a person who typed three letters and found the thing has
// already been told which tab it lives on, so backing the search out leaves
// them there instead of back where they started.
func (s *sheet) build() {
	s.items = s.items[:0]
	query := strings.ToLower(strings.TrimSpace(s.query.String()))
	// THE CONNECTIONS TAB BUILDS ITS OWN ROWS, from the engine rather than from
	// the registry (connectcaps.go). It is one branch and no second list: what
	// it appends is [sheetItem]s, so everything downstream of here — the cursor,
	// the window, the pointer, the hover — is the code that was already there.
	if s.onConnections() {
		s.buildConnections()
		return
	}
	if query == "" {
		// THE SPENDING TAB LAYS ITSELF OUT, because three of its rows are not
		// registry rows at all and the order of the rest is a reading of how
		// often a person worries about each one rather than of registry order
		// (settingspend.go).
		if settingTabs[s.tab] == tabSpending {
			s.items = append(s.items, s.spendingItems()...)
			s.cursor = s.clampCursor(s.cursor)
			return
		}
		for _, row := range s.tabRows() {
			meta, _ := s.metaFor(row)
			s.items = append(s.items, sheetItem{row: row, meta: meta})
			// THE ROLES SECTION HANGS OFF THE ROW IT WRITES. Every pin those rows
			// set lands in "pinned roles" and nowhere else, so it is drawn
			// directly under it: a person reading one is reading the other, and a
			// section further down the tab would be a second place to look for one
			// answer.
			if row.Key == config.KeyModelRoles {
				s.items = append(s.items, s.roleItems("")...)
			}
		}
		s.cursor = s.clampCursor(s.cursor)
		return
	}
	first := -1
	for tab, title := range settingTabs {
		start := len(s.items)
		for _, row := range s.rows {
			meta, ok := s.metaFor(row)
			if !ok || meta.tab != title || !settingMatches(row, meta, query) {
				continue
			}
			if len(s.items) == start {
				s.items = append(s.items, sheetItem{head: title})
			}
			s.items = append(s.items, sheetItem{row: row, meta: meta})
		}
		// A ROLE IS FOUND BY ITS OWN NAME, or by the line that says what it does.
		// Somebody searching for "planner" is not searching for a registry key —
		// the word is not in one — so the section answers the search itself, under
		// the tab it lives on.
		if title == tabProviders {
			if matched := s.roleItems(query); len(matched) > 0 {
				if len(s.items) == start {
					s.items = append(s.items, sheetItem{head: title})
				}
				s.items = append(s.items, matched...)
			}
		}
		if len(s.items) > start && first < 0 {
			first, s.tab = start+1, tab
		}
	}
	// Clamped rather than taken, because the first item of a group is a heading
	// and the second one can be a heading too: a search that matched only roles
	// opens on the tab's name, the section's name, and then a row.
	if first >= 0 {
		s.cursor = s.clampCursor(first)
	} else {
		s.cursor = 0
	}
	s.top = 0
}

// metaFor adds the one explanation whose answer comes from several rows. The
// sheet rebuilds after every write, so the selected search row follows keys and
// pins immediately while ordinary frame rendering stays read-only.
func (s *sheet) metaFor(row config.Setting) (settingMeta, bool) {
	meta, ok := settingMetaFor(row)
	if ok && row.Key == config.KeySearchProvider {
		meta.about = config.SearchProviderHintAt(s.profileDir)
	}
	return meta, ok
}

// tabRows is this tab's rows in READING order: the keys [modelsSection] leads
// with, then everything else the registry has for the tab in its own order.
//
// A row named in the section order and absent from this tab is skipped, and a row
// on this tab that nobody named is still emitted — the two halves of "keep every
// existing row reachable". The lead list is short and the loop is over one tab's
// rows, so this costs nothing worth measuring on a panel rebuild.
func (s *sheet) tabRows() []config.Setting {
	title := settingTabs[s.tab]
	mine := make([]config.Setting, 0, len(s.rows))
	for _, row := range s.rows {
		if meta, ok := settingMetaFor(row); ok && meta.tab == title {
			mine = append(mine, row)
		}
	}
	led := map[string]bool{}
	ordered := make([]config.Setting, 0, len(mine))
	for _, key := range modelsSection {
		for _, row := range mine {
			if row.Key == key {
				ordered = append(ordered, row)
				led[key] = true
			}
		}
	}
	for _, row := range mine {
		if !led[row.Key] {
			ordered = append(ordered, row)
		}
	}
	return ordered
}

// settingMatches is the search: the label, the key and the one-line description,
// case-folded, substring. The KEY is in it deliberately — a person who knows
// the registry knows "spendRail" and should not have to guess what it is called
// in the product's words.
func settingMatches(row config.Setting, meta settingMeta, query string) bool {
	for _, field := range []string{meta.label, row.Key, meta.about, row.Label} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// clampCursor keeps the cursor on a row and never on a heading.
func (s *sheet) clampCursor(at int) int {
	if len(s.items) == 0 {
		return 0
	}
	if at < 0 {
		at = 0
	}
	if at >= len(s.items) {
		at = len(s.items) - 1
	}
	if s.items[at].restful() {
		return at
	}
	for i := at; i < len(s.items); i++ {
		if s.items[i].restful() {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if s.items[i].restful() {
			return i
		}
	}
	return at
}

// move walks the list by rows, stepping over the headings rather than landing
// on them: a heading is a label, and a cursor on a label is a cursor on nothing
// enter could do.
func (s *sheet) move(delta int) {
	if len(s.items) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	at := s.cursor
	for n := 0; n < abs(delta); n++ {
		next := at
		for {
			next += step
			if next < 0 || next >= len(s.items) {
				next = at
				break
			}
			if s.items[next].restful() {
				break
			}
		}
		at = next
	}
	s.cursor = at
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// current is the row under the cursor.
func (s *sheet) current() (sheetItem, bool) {
	if s.cursor < 0 || s.cursor >= len(s.items) || !s.items[s.cursor].restful() {
		return sheetItem{}, false
	}
	return s.items[s.cursor], true
}

// changed reports whether this row differs from a untouched profile's reading.
//
// Two kinds are skipped and the reason is the same for both: their reading
// comes from a LIVE SEAM rather than from the file — the model slots answer
// from the running session, the divider from the surface's own preference — so
// the pristine registry, which has no seams, would report every one of them as
// changed. A mark that is on for rows nobody touched is not a mark.
func (s *sheet) changed(item sheetItem) bool {
	switch item.row.Kind {
	case config.SettingModel, config.SettingPercent:
		return false
	}
	was, known := s.defaults[item.row.Key]
	return known && was != item.row.Value()
}

// ── the roles ───────────────────────────────────────────────────────────────

// THE ROLES SECTION: one row per auxiliary call aforge makes on its own, and
// which model is answering it today.
//
// The four class rows above it are the setting; this is the READING of them. Before
// the section existed, "small work" and "careful work" were two model ids with
// no way of finding out what actually ran on them — the roles are declared
// across the binary from init functions (internal/roles' open registry), so
// nothing on screen could name them — and the only way to pin one was to type
// `planner:openai/gpt-5` into a text box from memory.
//
// IT IS A VIEW OF ONE REGISTRY ROW AND NOT A SECOND KNOB. Every pin these rows
// write lands in `models.roles`, the "pinned roles" row they sit under, parsed
// and re-serialized rather than appended — so the section and that row cannot
// disagree about what is pinned, and a role pinned by hand in the text box
// shows here as pinned.

// rolesHead is what the section is called on the panel. Lowercase, because the
// Capitalized words on this sheet are tab names and this is not one.
const rolesHead = "roles"

// roleRow is one registered role as the panel holds it, hung off [sheetItem]
// exactly as an account is (connectcaps.go): the cursor walk, the scroll, the
// pointer and the hover need to know nothing about it.
type roleRow struct {
	role roles.Role
	tier roles.Tier
	// tierLabel is the tier in the panel's words ([sheet.tierWord]), resolved
	// HERE and not while drawing: it costs a registry read, and a row that took
	// one per frame would be paying for a word that only changes when the sheet
	// is rebuilt anyway.
	tierLabel string
	// model is what the ladder resolves to right now, and pin is rung one of it
	// alone — the row says WHICH MODEL and, separately, whether that answer was
	// chosen for this role or inherited from its tier.
	model string
	pin   string
}

// roleItems is the section: one item per registered role, GROUPED UNDER ITS
// CLASS in [roles.Tiers] order, each carrying what it resolves to as the panel
// currently reads the settings. A query keeps only the roles it matches, and a
// class with nothing left in it contributes no heading.
//
// THE GROUPING IS THE POINT. A flat alphabetical list put `auditor` beside
// `compaction` and `designer` beside `guardian`, which is four unrelated bills in
// a row — and the four rows directly above this section are exactly the four
// classes those roles are answering under. Grouped, the section reads as the
// answer to the question the tier rows ask: this is what "careful work" bought
// you. Inside a class the order is the registry's own sorted one, because it is a
// map filled from init functions and a list that reshuffled per launch is a list
// nobody trusts.
func (s *sheet) roleItems(query string) []sheetItem {
	source := s.rolesSource()
	var items []sheetItem
	for _, tier := range roles.Tiers {
		started := false
		for _, role := range roles.Registered() {
			if got, ok := roles.TierOf(role); !ok || got != tier {
				continue
			}
			row := &roleRow{role: role, tier: tier, tierLabel: s.tierWord(tier)}
			row.pin, _ = roles.Pinned(source, role)
			// A ROLE WITH NO MODEL ANYWHERE IS STILL A ROW. Resolve refuses when
			// the ladder runs out — no pin, no tier, no session model — and the
			// honest drawing of that is the role's name with nothing beside it,
			// not a role the panel pretends is not there.
			//
			// It resolves through [roles.ResolveCall] and prints the call, so a
			// class carrying a level says so: the row a person reads is
			// `kimi-k3:low`, which is the notation the model picker and /status
			// already spell an effort in.
			if call, err := roles.ResolveCall(source, role, s.sessionModel); err == nil {
				row.model = call.String()
			}
			if query != "" && !roleMatches(row, query) {
				continue
			}
			if !started {
				items = append(items, sheetItem{head: rolesHead + " · " + row.tierLabel})
				started = true
			}
			items = append(items, sheetItem{
				role: row,
				meta: settingMeta{tab: tabProviders, label: string(role), about: s.roleAbout(row)},
			})
		}
	}
	return items
}

// roleMatches is the search over a role row: its name, the line that says what
// it does, and the model answering it — case-folded, substring, [settingMatches]
// over the fields a role row actually has.
//
// THE DESCRIPTION IS IN IT DELIBERATELY. Nobody looking for the model that reads
// their images searches for "vision"; they search for "image", and the sentence
// under the row is where that word is written.
func roleMatches(row *roleRow, query string) bool {
	for _, field := range []string{string(row.role), roles.Describe(row.role), row.model} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// rolesSource is [roles.Source] over THE PANEL'S OWN READING of the registry —
// the four class rows and the pins in "pinned roles".
//
// The door builds one of these at boot (cmd/aforge's v3RolesSource) and that is
// the one a running session's calls go through. This one exists because they
// answer different questions: the session's is what is running now, and a
// settings panel showing that while somebody edits the row above it would be
// showing the wrong half of its own screen.
func (s *sheet) rolesSource() roles.Source {
	values := map[string]string{}
	if s.registry == nil {
		return func(string) (string, bool) { return "", false }
	}
	// EVERY TIER, DERIVED. It reads [roles.Tiers] through [tierSettingKey] rather
	// than listing the rows again, so a class added to that package appears in
	// this reading without anybody remembering this function exists.
	for _, tier := range roles.Tiers {
		if row, ok := s.registry.Row(tierSettingKey(tier)); ok {
			values[roles.TierKey(tier)] = rowText(row)
		}
	}
	if row, ok := s.registry.Row(config.KeyModelRoles); ok {
		// A ROW MID-EDIT IS A ROW WITH NO PINS IN IT, not an error on the foot
		// line: the registry refuses a malformed value at the write, so the only
		// way one is read back here is a hand-edited config file, and the useful
		// thing to draw then is every role following its tier.
		if pins, err := config.ParseModelRoles(rowText(row)); err == nil {
			for role, model := range pins {
				values[roles.PinKey(roles.Role(role))] = model
			}
		}
	}
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

// tierWord is a tier in the panel's own words, READ OFF THE ROW THAT SETS IT
// rather than spelled again here: a role says "careful work" because that is
// what the row a person changes to move it is called, and the two cannot drift.
func (s *sheet) tierWord(tier roles.Tier) string {
	key := tierSettingKey(tier)
	if s.registry != nil {
		if row, ok := s.registry.Row(key); ok {
			if meta, found := settingMetaFor(row); found {
				return meta.label
			}
		}
	}
	return string(tier)
}

// roleAbout is the one line under a selected role row: where its answer came
// from, and the key that changes it. It is the section's whole help — the keys
// line says the same two words, and a person who stopped on a row is the one
// person who wants the sentence.
func (s *sheet) roleAbout(row *roleRow) string {
	// THE ROLE'S OWN LINE COMES FIRST, because "what is this" is the question and
	// "where did its model come from" is the follow-up. Before the descriptions
	// landed (internal/roles' Describe), the only thing this line could say about
	// `auditor` was which row above it to change — which is help for somebody who
	// already knew what an auditor was.
	said := roles.Describe(row.role)
	if said != "" {
		said += " · "
	}
	if row.pin != "" {
		return said + "pinned, so it ignores " + row.tierLabel + " above. del clears the pin."
	}
	return said + "follows " + row.tierLabel + " above. enter pins it to a model of its own."
}

// roleFilter is the question a role's picker asks. Two registered roles are not
// conversations at all — the one that looks at images and the one that draws
// them — and offering either the chat list is offering the exact complement of
// the models that could answer it ([filterFor] makes this argument for the
// media slot rows, over the same predicates).
func roleFilter(role roles.Role) modelFilter {
	switch role {
	case roles.RoleVision:
		return inspectsImages
	case roles.RoleImageGen:
		return drawsImages
	}
	return chatModel
}

// roleRowLines is one role: its name, the tier it answers under, and the model
// that answers it. It is [sheet.rowLines]'s shape and [overlayLines]'s row — the
// same two-line law at [tierPhone], the same band, the same hover.
func (s *sheet) roleRowLines(row *roleRow, selected, hovered bool, width int, pal palette) []string {
	// THE CLASS IS THE HEADING THIS ROW SITS UNDER, so it is not repeated on the
	// row. It used to lead the value — "careful work · some/model" on every one of
	// ten rows — which spent the widest column on a word the section already said
	// once, and left the id it was there to show being the first thing [fit] cut.
	//
	// THE EMPTINESS LAW. A role with no model resolved says nothing beside its
	// name; there is no id to print, and printing the class's own blank label
	// would be the panel answering "which model" with a setting.
	value := row.model
	if row.pin != "" {
		if value != "" {
			value += "  "
		}
		value += "pinned"
	}
	return overlayLines(string(row.role), value, selected, false, hovered, width, pal)
}

// applyRolePin writes one role's pin back into the row that holds them all.
//
// IT PARSES, EDITS AND RE-SERIALIZES rather than appending, because the row is a
// SET: a role already pinned from the text box would otherwise be named twice,
// which the registry refuses in the same breath it would have taken the change.
// An empty model clears the pin, which is what del on the row does.
func (a *app) applyRolePin(row config.Setting, role roles.Role, model string) {
	pins, err := config.ParseModelRoles(rowText(row))
	if err != nil {
		// The row is not readable as pairs, so it was hand-edited into something
		// this panel cannot safely rewrite. It says so rather than dropping
		// somebody's line on the way past.
		a.sheet.msg = err.Error()
		return
	}
	if model = strings.TrimSpace(model); model == "" {
		delete(pins, string(role))
	} else {
		pins[string(role)] = model
	}
	meta, _ := settingMetaFor(row)
	a.applySetting(sheetItem{row: row, meta: meta}, formatModelRoles(pins))
}

// unpinRole is del on a pinned role row. It is the one thing a role row can do
// that enter cannot: enter opens the catalog, and no row in a catalog means
// "no model".
func (a *app) unpinRole() {
	s := &a.sheet
	item, ok := s.current()
	if !ok || item.role == nil || item.role.pin == "" {
		return
	}
	row, found := s.registry.Row(config.KeyModelRoles)
	if !found {
		return
	}
	s.msg = ""
	a.applyRolePin(row, item.role.role, "")
}

// rowText is a row's value with its empty label taken back off — the reading
// [app.activate] gives a text row before it opens a box on it, which is the one
// place a label like "none" must not be mistaken for a value.
func rowText(row config.Setting) string {
	value := row.Value()
	if value == row.EmptyLabel {
		return ""
	}
	return value
}

// formatModelRoles writes the pins back as the row's own `role:model` pairs,
// SORTED, because a map has no order and a row that reshuffled itself every
// time it was saved is a row nobody can read a diff of.
func formatModelRoles(pins map[string]string) string {
	names := make([]string, 0, len(pins))
	for name := range pins {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name+":"+pins[name])
	}
	return strings.Join(out, ", ")
}

// ── the keyboard ────────────────────────────────────────────────────────────

// sheetKey routes one keypress while the panel is up. It reports whether it
// took the key; only ctrl+c is read before it (input.go), because leaving is
// never modal.
// It hands back a COMMAND as well, for the one row on this sheet whose answer
// leaves the process: a service on the Connections tab that is not connected
// yet starts the same browser trip /connect starts, and a sign-in is a thing
// that reaches the network (connectcaps.go).
func (a *app) sheetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.at(pageSettings) {
		return nil, false
	}
	s := &a.sheet
	defer a.touch()
	// THE THREE BOXES THAT TAKE THE WHOLE KEYBOARD ARE READ BEFORE THE ROUTER AND
	// ARE NOT HERE. They are [placeSettings.owns], because a layer inside a place
	// that has claimed every key outranks even the six classes — and the router
	// reads them in that order for every place at once (place_settings.go).
	if cmd, took := (placeSettings{}).owns(a, msg); took {
		return cmd, true
	}
	// AND THE CARET'S OWN CHORDS BEFORE THE PANEL'S KEYS (editkeys.go). `home`
	// and `end` walk this sheet's LIST rather than the search box's caret, which
	// is why they are not in that vocabulary and are read below with the rest of
	// the walk — but the word jumps and `ctrl+a` mean nothing else here, and a
	// search box a person cannot move a word in is a box that is worse to type
	// in than the one on the screen behind it.
	if editorMotion(&s.query, msg.String()) {
		return nil, true
	}
	if editorWordKill(&s.query, msg.String()) {
		s.build()
		return nil, true
	}

	switch msg.String() {
	case "esc":
		// esc backs out one layer at a time: the search first, then whatever the
		// tab on show has standing open — a confirmation, an expanded service —
		// and the panel after all of it. A key that closed the whole sheet from
		// inside a search would throw away the only thing on screen the person
		// typed, and one that closed it over an open account would take the page
		// away instead of the thing they were looking at.
		if s.searching() {
			s.query.reset()
			s.build()
			return nil, true
		}
		if a.connEsc() {
			return nil, true
		}
		a.closeSettings()
		return nil, true

	// ← AND → MOVE THIS PANEL'S OWN SECTIONS, and `tab` no longer does. `tab` is
	// the way to the NEXT PLACE now (pages.go), and a key that meant "next
	// section" on one place and "next place" on the other six would be the exact
	// per-place grammar the router exists to end. The two arrows were always the
	// other half of this binding and they keep it.
	case "left":
		s.tabBy(-1)
	case "right":
		s.tabBy(1)

	case "up", "ctrl+p":
		s.move(-1)
	case "down", "ctrl+n":
		s.move(1)
	case "pgup":
		s.move(-sheetRows)
	case "pgdown":
		s.move(sheetRows)
	case "home":
		s.cursor = s.clampCursor(0)
	case "end":
		s.cursor = s.clampCursor(len(s.items) - 1)

	case "enter", " ", "space":
		return a.activate(), true

	case "delete":
		// The one key on this sheet that only one kind of row answers, and it
		// takes it from nothing else: del anywhere but a pinned role row does
		// nothing at all, rather than deleting whatever was under the cursor.
		a.unpinRole()

	case "backspace":
		s.query.deleteBackward()
		s.build()
	case "ctrl+u":
		s.query.reset()
		s.build()
	case "ctrl+w":
		s.query.deleteWord()
		s.build()

	default:
		if text := msg.Key().Text; text != "" && text != " " {
			s.query.insert(text)
			s.build()
		}
	}
	return nil, true
}

// tabBy switches tabs, clamping rather than wrapping — the same rule every list
// on this surface walks by (palette.go). A search is dropped by it: the tabs
// and the search are two ways of asking the same question, and answering both
// at once would show a tab's name over rows from five of them.
func (s *sheet) tabBy(delta int) {
	if s.searching() {
		s.query.reset()
	}
	s.tab = moveCursor(s.tab, delta, len(settingTabs))
	s.cursor, s.top, s.msg = 0, 0, ""
	// A confirmation does not survive the page it was asked on, and neither does
	// a half-typed key: leaving the tab is as much a way of not answering either
	// of them as moving off the row is (connectcaps.go). What stays open is the
	// SERVICE, because coming back to a tab you left half-read and finding it
	// collapsed is the tab forgetting where you were.
	s.conn.armed, s.conn.entry = false, nil
	s.build()
}

// activate is enter on a row: flip it, cycle it, or open the submenu that
// answers it — and on the Connections tab, open an account, walk one of its
// answers, or start a sign-in, which is the one of them that needs a command.
func (a *app) activate() tea.Cmd {
	s := &a.sheet
	item, ok := s.current()
	if !ok {
		return nil
	}
	if item.conn != nil {
		return a.connAct(item.conn)
	}
	s.msg = ""
	if item.role != nil {
		// A ROLE IS A MODEL CHOICE, so it opens the picker the class rows open
		// and for their reason — a row that asks "which model" and offers a blank
		// line is asking a person to be the catalog. It opens ON THE PIN and not
		// on the resolved model: the picker's mark means "this is what this row
		// holds", and a role following its tier holds nothing.
		sel := &sheetSelect{
			key: config.KeyModelRoles, label: string(item.role.role),
			keep: roleFilter(item.role.role), role: item.role.role,
		}
		sel.pick.startFor(a.modelsFor(sel.keep), item.role.pin, sel.keep)
		s.sel = sel
		return nil
	}
	switch item.meta.widget {
	case widgetToggle:
		next := "on"
		if item.row.Value() == "on" {
			next = "off"
		}
		a.applySetting(item, next)

	case widgetCycle:
		choices := item.row.Choices
		if len(choices) == 0 {
			return nil
		}
		at := 0
		for i, choice := range choices {
			if choice == item.row.Value() {
				at = (i + 1) % len(choices)
				break
			}
		}
		a.applySetting(item, choices[at])

	case widgetLane:
		// ENTER ON THIS ROW OPENS THE MACHINES. It used to walk four words —
		// auto, pinned, pinned but borrowable, openrouter — with nothing on
		// screen saying what pinning would pin or what it would cost, which is
		// asking a person to choose an endpoint they cannot see. Now it opens
		// the same fold `→` opens in the picker, on the model in use, with the
		// cursor on the lane in force. It cycles ONLY when nothing has been
		// measured, and then auto and openrouter are honestly the only two
		// answers there are ([app.openLaneList], [app.cycleLane]).
		if !a.openLaneList() {
			a.cycleLane(item)
		}

	case widgetSelect:
		// The picker opens ON the id the row currently holds, the way /model
		// opens on the model in use: enter with nothing typed confirms rather
		// than changes. A row holding its empty label ("follows the
		// conversation") matches no id and the cursor stays at the top, which is
		// the honest reading of "this slot has not been set".
		sel := &sheetSelect{
			key: item.row.Key, label: item.meta.label,
			keep: filterFor(item.row.Key),
		}
		// The list is resolved through the SLOT'S OWN question, not through the
		// chat list narrowed afterwards ([app.modelsFor]). The predicate is
		// handed to the picker as well because that is the door every slot comes
		// through, and a second application of the same filter is a no-op.
		sel.pick.startFor(a.modelsFor(sel.keep), item.row.Value(), sel.keep)
		// AND THIS IS THE SAME LIST /model OPENS, machines and all. A row that
		// has a lane row behind it (lanes.go's [laneSlotForRow]) folds, shows
		// the speed column, takes the lane grammar in its filter and pins on
		// enter; a row that has none — a media slot, a role — is armed with
		// nothing and folds nothing, which is the emptiness law and not a
		// second, plainer list.
		a.armLanes(&sel.pick, laneSlotForRow(item.row.Key))
		s.sel = sel

	default:
		value := item.row.Value()
		if value == item.row.EmptyLabel {
			// The empty label is what the row SAYS when it holds nothing
			// ("none", "follows the conversation"). Putting that word in the box
			// would offer the person a value to edit that they never set.
			value = ""
		}
		box := editor{}
		box.setText(value)
		s.edit = &sheetEdit{
			key: item.row.Key, label: item.meta.label,
			secret: item.row.Secret, box: box,
		}
	}
	return nil
}

// openLaneList is enter on the `lane` row: THE SAME PICKER the model row opens,
// already unfolded on the model you are talking to, with the cursor on the lane
// in force.
//
// One list and one gesture. The alternative was a lane-shaped list of its own
// beside a model-shaped list that already draws lanes — two components that
// would have to be kept saying the same thing about the same machines — and the
// fold is where a person has already learnt to read them.
//
// IT ANSWERS FALSE WHEN THERE IS NOTHING TO OPEN. A session that has measured
// no lane for this model has no fold ([picker.unfoldAt] refuses one), and the
// row falls back to the walk between the only two answers that exist without a
// measurement.
func (a *app) openLaneList() bool {
	slot := laneSlotFor(a.model)
	if slot == "" {
		return false
	}
	sel := &sheetSelect{
		key: config.ModelSettingKey(talkSlot), label: "lane · " + a.model,
		keep: filterFor(config.ModelSettingKey(talkSlot)),
	}
	sel.pick.startFor(a.modelsFor(sel.keep), a.model, sel.keep)
	a.armLanes(&sel.pick, slot)
	// THE FOLD HAS TO BE THE ONE THIS ROW IS ABOUT. [picker.start] leaves the
	// cursor on row zero when the model in use is not in the list at all — a
	// catalog that has not loaded, a model nobody publishes — and unfolding
	// whatever happened to sort first would be this row opening somebody else's
	// machines.
	if chosen, ok := sel.pick.choice(); !ok || chosen.ID != a.model {
		return false
	}
	if !sel.pick.unfoldHere() {
		return false
	}
	sel.pick.cursorToPin()
	a.sheet.sel = sel
	return true
}

// cycleLane is enter on the lane row: auto → pinned → pinned but borrowable →
// openrouter → auto, writing the row's own words back through the registry so
// that the panel, the picker and the `settings` tool all read one spelling
// (config's [config.LaneRowWord]).
//
// THE PINNED RUNGS ARE SKIPPED WHEN THERE IS NO MACHINE TO NAME. On a session
// that has measured nothing there is no honest lane to pin, so the walk is auto
// ↔ openrouter and the two missing rungs are simply not there — which is the
// emptiness law applied to a gesture rather than to a number.
func (a *app) cycleLane(item sheetItem) {
	slot := laneSlotFor(a.model)
	name, pinned := config.LanePinned(a.profileDir, slot)
	if !pinned {
		if best, ok := bestLane(laneViews(a.model, a.now())); ok {
			name = best.Name
		}
	}
	current := config.LaneAt(a.profileDir, slot)
	borrow := config.LaneBorrowAt(a.profileDir, slot)
	next := config.LaneAuto
	switch {
	case strings.EqualFold(current, config.LaneOpenRouter):
		next = config.LaneAuto
	case !pinned && name != "":
		next = "pinned: " + name
	case !pinned:
		next = config.LaneOpenRouter
	case !borrow:
		next = "pinned: " + name + ", borrow when slow"
	default:
		next = config.LaneOpenRouter
	}
	a.applySetting(item, next)
}

// applySetting writes one row and keeps whatever the registry said about it.
func (a *app) applySetting(item sheetItem, raw string) {
	if err := item.row.Apply(raw); err != nil {
		a.sheet.msg = err.Error()
		return
	}
	if item.row.Key == config.KeyWork {
		a.workMode = config.WorkAt(a.profileDir)
		a.touch()
	}
	// THE TWO LANE ROWS LAND ON THE LIVE TRANSPORT, not at the next launch.
	// They are the panel's half of what the picker does when somebody pins from
	// it (lanes.go's laneRowChanged): a settings row that a person watched
	// themselves change, and that then did nothing to the very next answer,
	// would be the panel telling them something untrue.
	switch item.row.Key {
	case config.LaneSettingKey(talkSlot), config.LaneBorrowKey(talkSlot):
		a.laneRowChanged()
	case config.KeyLaneGuard:
		provider.SetLaneGuard(config.LaneGuardAt(a.profileDir))
	}
	a.sheet.msg = ""
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
}

// sheetEditKey drives the text submenu. enter saves, an empty box clears the
// row, esc leaves it exactly as it was.
func (a *app) sheetEditKey(msg tea.KeyPressMsg) {
	s := &a.sheet
	edit := s.edit
	// The word and line jumps are the surface's, said once (editkeys.go).
	if editorMotion(&edit.box, msg.String()) {
		return
	}
	switch msg.String() {
	case "esc":
		s.edit = nil
	case "enter":
		row, ok := s.registry.Row(edit.key)
		s.edit = nil
		if !ok {
			return
		}
		meta, _ := settingMetaFor(row)
		a.applySetting(sheetItem{row: row, meta: meta}, edit.box.String())
	case "backspace":
		edit.box.deleteBackward()
	case "delete":
		edit.box.deleteForward()
	// Under every name they send under, as in the message box and in every
	// filterable overlay (input.go, palette.go's [listNavigate]).
	case "ctrl+u", "super+backspace":
		edit.box.killToStart()
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		edit.box.deleteWord()
	case "left", "ctrl+b":
		edit.box.left()
	case "right", "ctrl+f":
		edit.box.right()
	case "home":
		// `ctrl+a` is the same jump, read above with the rest of the surface's
		// line vocabulary (editkeys.go).
		edit.box.home()
	case "end", "ctrl+e":
		edit.box.end()
	default:
		if text := msg.Key().Text; text != "" {
			edit.box.insert(text)
		}
	}
}

// sheetSelectKey drives the model picker while a slot row owns it. Only the two
// keys that MEAN something different here are handled: esc leaves the row as it
// was, and enter writes the chosen id through the registry rather than switching
// the conversation. Everything else — the walk, the scroll, the filter — is
// [picker.navigate], the same code /model runs.
func (a *app) sheetSelectKey(msg tea.KeyPressMsg) {
	s := &a.sheet
	sel := s.sel
	switch msg.String() {
	case "esc":
		s.sel = nil
	case "enter":
		// ENTER INSIDE AN OPEN FOLD IS A LANE AND NOT A SLOT. It is read before
		// anything else for [app.pickerKey]'s reason — closing the list is what
		// forgets which row the cursor was on.
		if lane, onLane := sel.pick.laneUnder(); onLane {
			a.applyLaneFromSheet(sel, lane)
			return
		}
		chosen, ok := sel.choice()
		row, found := s.registry.Row(sel.key)
		role := sel.role
		s.sel = nil
		if !ok || !found {
			return
		}
		// A role writes ONE PAIR of the row it shares with every other pin;
		// everything else writes the row whole.
		if role != "" {
			a.applyRolePin(row, role, chosen)
			return
		}
		meta, _ := settingMetaFor(row)
		a.applySetting(sheetItem{row: row, meta: meta}, chosen)
	default:
		// The fold's three keys are the LIST's and not this door's
		// ([picker.foldKey]); everything else is the walk and the filter box.
		if !sel.pick.foldKey(msg.String()) {
			sel.pick.navigate(msg)
		}
	}
}

// applyLaneFromSheet is enter on a lane row inside the panel's own copy of the
// picker: the machine, and — when the fold was opened under a model this
// conversation is not on — that model too.
//
// THE MODEL IS WRITTEN THROUGH THE ROW and not through [app.switchModel]
// directly, which is the one difference from /model's half of this gesture: in
// here the slot is a registry row, and the row's own write is what carries the
// change to the session (the registry's SetModel seam, [app.registry]). Two
// answers at once for [app.pickerKey]'s reason — a lane pinned under a model
// somebody is not talking to is a setting that takes effect the next time they
// happen to switch.
func (a *app) applyLaneFromSheet(sel *sheetSelect, lane pickRow) {
	s := &a.sheet
	chosen, ok := sel.pick.choice()
	lanes := sel.pick.lanes
	key, role := sel.key, sel.role
	s.sel = nil
	if !ok || role != "" {
		return
	}
	if row, found := s.registry.Row(key); found && row.Value() != chosen.ID {
		meta, _ := settingMetaFor(row)
		a.applySetting(sheetItem{row: row, meta: meta}, chosen.ID)
	}
	a.applyLaneChoice(chosen.ID, lane, lanes)
	// THE PANEL RE-READS WHAT IT JUST WROTE. The lane row and the model row's
	// own tail are two readings of this one fact ([sheet.laneWord]), and a
	// panel that kept drawing the old word over a pin the person watched
	// themselves set would be the panel lying about it.
	s.rows = s.registry.Rows()
	s.build()
}

// ── the pointer ─────────────────────────────────────────────────────────────

// sheetHitKind is what one screen row of the panel answers to a click.
type sheetHitKind uint8

const (
	sheetHitNone sheetHitKind = iota
	sheetHitTabs
	sheetHitRow
	// sheetHitOption is one row of the select submenu; index is its position in
	// that submenu's own hits.
	sheetHitOption
)

type sheetHit struct {
	kind  sheetHitKind
	index int
}

// sheetPress is a click inside the panel: a tab word switches tabs, a row
// selects and answers, anything else does nothing. It hands back a command for
// the reason [app.sheetKey] does — a sign-in reaches the network.
func (a *app) sheetPress(x, y int) tea.Cmd {
	if a.sheet.conn.entry != nil {
		// A BOX BEING TYPED INTO IS NOT A LIST. Every press is swallowed and none
		// of them acts — esc is the way out, which is the way out of every box on
		// this surface (connectpanel.go's [app.connectPanelPress] says it first).
		return nil
	}
	width, height := a.size()
	_, hits, _, _ := a.sheetFrame(width, height)
	if y < 0 || y >= len(hits) {
		return nil
	}
	switch hit := hits[y]; hit.kind {
	case sheetHitTabs:
		if tab, ok := tabAtColumn(x); ok {
			if a.sheet.searching() {
				a.sheet.query.reset()
			}
			a.sheet.tab = tab
			a.sheet.cursor, a.sheet.top, a.sheet.msg = 0, 0, ""
			a.sheet.conn.armed, a.sheet.conn.entry = false, nil
			a.sheet.build()
			a.touch()
		}
	case sheetHitRow:
		// A click selects, and a click on the row already selected answers it.
		// One press cannot do both: a toggle that flipped the moment a pointer
		// touched it would change a setting the person was only reading — and on
		// the Connections tab it would be an account disconnected by a pointer
		// that was passing through.
		if a.sheet.cursor != hit.index {
			a.sheet.cursor = hit.index
			a.touch()
			return nil
		}
		cmd := a.activate()
		a.touch()
		return cmd

	case sheetHitOption:
		if a.sheet.sel == nil {
			return nil
		}
		if a.sheet.sel.pick.cursor != hit.index {
			a.sheet.sel.pick.cursor = hit.index
			a.touch()
			return nil
		}
		a.sheetSelectKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		a.touch()
	}
	return nil
}

// sheetHover records which row the pointer is over, repainting only when the
// answer changed (hover.go's rule, applied to the panel).
func (a *app) sheetHover(y int) {
	width, height := a.size()
	_, hits, _, _ := a.sheetFrame(width, height)
	next := hoverAt{}
	if y >= 0 && y < len(hits) && hits[y].kind == sheetHitRow {
		next = hoverAt{kind: hoverSheet, index: hits[y].index}
	}
	if next == a.hot {
		return
	}
	a.hot = next
	a.touch()
}

// ── the frame ───────────────────────────────────────────────────────────────

// sheetFrame is the whole screen while the panel is open: exactly height rows,
// what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for the reason [app.chrome] is: the frame draws these rows
// and the pointer resolves against them, and two answers to "where is the tab
// bar" is how a click lands on the wrong tab.
func (a *app) sheetFrame(width, height int) ([]string, []sheetHit, int, int) {
	// THE HEAD AND THE FOOT BELONG TO THE ROUTER (pages.go). This panel's title
	// row is gone — the place tab bar above says `settings` — and so is its keys
	// line, which is the one hint every place shares now. ITS OWN TAB BAR STAYS,
	// as the first row of its body, and the two bars are not a repetition: the
	// upper one is the seven places and the lower one is this place's sections.
	// The panel is where [placeTabBar] was lifted from, so they are drawn by the
	// same geometry and read as one object at two scales.
	s := &a.sheet
	pal := a.pal
	lines, hits, caretX, caretY := placeFrame(a, width, height,
		func(width, room int) []placeRow {
			rows := make([]placeRow, 0, room)
			rows = append(rows, placeRow{text: sheetTabBar(width, s.tab, pal), hit: sheetHit{kind: sheetHitTabs}})
			rows = append(rows, placeRow{text: pal.dim(rule(width))})
			rows = append(rows, placeRow{})
			room -= len(rows)
			if room < 1 {
				room = 1
			}
			if s.sel != nil {
				body, at := s.selectLines(width, room, pal, a.reasoningFor)
				for i, line := range body {
					hit := sheetHit{}
					if at[i] >= 0 {
						hit = sheetHit{kind: sheetHitOption, index: at[i]}
					}
					rows = append(rows, placeRow{text: line, hit: hit})
				}
				return rows
			}
			body, owner := s.listLines(width, room, pal, a.hoveredSheetRow())
			at := s.cursorLine(owner)
			// THE CURSOR'S ROW IS SCROLLED IN WHOLE. At [tierPhone] it is two
			// lines — the name and the value under it — and a window that pinned
			// only the first would push the value off the bottom edge, leaving a
			// selection band with one end cut off and the fact being changed off
			// screen. The last line is pinned first and the first line second, so
			// a row taller than the window still shows its name.
			if last := s.cursorLastLine(owner, at, width); last != at {
				s.top = listTop(last, s.top, len(body), room)
			}
			s.top = listTop(at, s.top, len(body), room)
			for i := 0; i < room; i++ {
				index := s.top + i
				if index >= len(body) {
					rows = append(rows, placeRow{})
					continue
				}
				hit := sheetHit{}
				if owner[index] >= 0 {
					hit = sheetHit{kind: sheetHitRow, index: owner[index]}
				}
				rows = append(rows, placeRow{text: body[index], hit: hit})
			}
			return rows
		})
	return lines, placeHitsOf(hits, sheetHit{}), caretX, caretY
}

func rule(width int) string {
	if width < 1 {
		return ""
	}
	return strings.Repeat("─", width)
}

// tabSpan is where one tab's CHIP sits on the bar, so the render and the click
// agree about it. It covers the chip's padding as well as its word: the cell
// beside "Context" is part of Context, because a one-cell miss between two words
// is a miss people make and a bar that answered it with nothing would be a bar
// that has to be aimed at.
type tabSpan struct{ from, to int }

const (
	// tabGap is what is left between two chips once each carries its own air.
	tabGap = 1
	// tabPad is that air, one cell each side — the same chip the task strip
	// wears (taskstrip.go), because the two rows are the same object in two
	// places and a person should not have to learn it twice.
	tabPad     = " "
	tabPadCols = 2
	// tabLead is the bar's left margin, which every other line of this panel
	// keeps as well.
	tabLead = 1
)

func tabSpans() []tabSpan {
	spans := make([]tabSpan, 0, len(settingTabs))
	at := tabLead
	for _, title := range settingTabs {
		width := len(title) + tabPadCols
		spans = append(spans, tabSpan{from: at, to: at + width})
		at += width + tabGap
	}
	return spans
}

func tabAtColumn(x int) (int, bool) {
	for i, span := range tabSpans() {
		if x >= span.from && x < span.to {
			return i, true
		}
	}
	return 0, false
}

// sheetTabBar is the one place this panel spends the accent: the tab you are
// on. Everything else on the bar is dim, which is what makes the one word read
// as a position rather than as a menu of five shouting words.
//
// THE ACCENT NOW ARRIVES ON A BAND, and the band is why the chips are padded.
// Five words in a row with one of them brighter is a sentence with an emphasis
// in it; five padded chips with one of them filled is a tab bar, and this panel
// IS a tab bar — the same object the task strip is, drawn the same way, so that
// "which page am I on" is one visual question across the app rather than two
// (taskstrip.go's [app.stripLabel]).
//
// There is one cursor here and not two. The tab a person has focused is the tab
// that is open — moving the focus switches the page — so the bar has nothing to
// say that the band does not already say, and it draws no second mark.
func sheetTabBar(width, active int, pal palette) string {
	line, plain := strings.Repeat(" ", tabLead), strings.Repeat(" ", tabLead)
	for i, title := range settingTabs {
		if i > 0 {
			line += strings.Repeat(" ", tabGap)
			plain += strings.Repeat(" ", tabGap)
		}
		chip := tabPad + title + tabPad
		if i == active {
			line += pal.selected(pal.bold(pal.accent(chip)), len(title)+tabPadCols)
		} else {
			line += pal.dim(chip)
		}
		plain += chip
	}
	if ansi.StringWidth(plain) > width {
		return fit(line, width)
	}
	return line
}

// listLines is the rows, plus the ONE description this panel ever shows: the
// selected row's. omp shows a line under every row; this surface does not,
// because twenty-eight rows each carrying a sentence is a wall, and the
// sentence a person needs is the one about the row they are on.
//
// It returns the item each line belongs to (-1 for a heading or a gap), which
// is what the pointer resolves against.
func (s *sheet) listLines(width, room int, pal palette, hover int) ([]string, []int) {
	lines := make([]string, 0, len(s.items)+8)
	owner := make([]int, 0, len(s.items)+8)
	put := func(text string, at int) {
		lines = append(lines, text)
		owner = append(owner, at)
	}
	if len(s.items) == 0 {
		// The Connections tab has its own two sentences, because "nothing
		// matches" is an answer about a search and this tab is not searched
		// (connectcaps.go).
		word := "nothing matches"
		if s.onConnections() {
			word = s.connEmptyWord()
		}
		put(pal.dim("  "+word), -1)
		return lines, owner
	}
	for i, item := range s.items {
		if item.heading() {
			if len(lines) > 0 {
				put("", -1)
			}
			put(pal.dim("  "+item.head), -1)
			continue
		}
		// AN ACCOUNT IS A BLOCK AND A BLOCK HAS AIR OVER IT (connectcaps.go).
		// The blank belongs to no row, exactly as a heading's does, so the
		// pointer over it acts on nothing and the cursor cannot land on it.
		if item.conn != nil && item.conn.air && len(lines) > 0 {
			put("", -1)
		}
		for _, line := range s.rowLinesWithin(item, i == s.cursor, i == hover, width, connBoxRows(room), pal) {
			put(line, i)
		}
		if i != s.cursor {
			continue
		}
		// THE ONE DESCRIPTION THIS PANEL EVER SHOWS is the selected row's, and
		// on the accounts tab it is the selected SERVICE'S — a catalog row's own
		// line about what connecting it buys, drawn under the row a person has
		// stopped on and under no other. Most rows of that tab have none: a
		// sentence explaining "read your mail" would be this surface saying the
		// same thing twice (connectcaps.go's [connAbout]).
		about := item.meta.about
		if item.conn != nil {
			about = connAbout(item.conn)
		}
		if about == "" {
			continue
		}
		for n, line := range wrap(about, width-6) {
			if n >= 2 {
				break
			}
			put("    "+pal.dim(line), i)
		}
	}
	return lines, owner
}

// cursorLine is the display row the cursor's item starts on.
func (s *sheet) cursorLine(owner []int) int {
	for i, at := range owner {
		if at == s.cursor {
			return i
		}
	}
	return 0
}

// cursorLastLine is the display row the cursor's ROW ends on — the wrapped
// value's line at [tierPhone], and the row itself everywhere else.
//
// It stops at the row and does not walk the whole item: the selected setting's
// description follows it under the same owner, and pinning that into the window
// would scroll the list by two rows the moment somebody moved the cursor.
func (s *sheet) cursorLastLine(owner []int, at, width int) int {
	if phoneList(width) && at+1 < len(owner) && owner[at+1] == s.cursor {
		return at + 1
	}
	return at
}

// changedMark is the one cell that says "you chose this". A glyph and not a
// colour, because the accent is already spent on the tab and the palette's own
// rule is that a distinction drawn in colour is drawn in text too (styles.go).
const changedMark = "•"

// rowLines is one setting: its name, and the value it is at. The two share a
// line on any frame with room for both and split at [tierPhone], where a value
// like "anthropic/claude-sonnet-4.5  set by AFORGE_MODEL" is the whole of what
// the row is about and the first thing a narrow row used to cut (palette.go's
// [overlayLines]). The pair stays ONE item to the pointer and to the cursor —
// [sheet.listLines] hands both lines the same owner.
func (s *sheet) rowLines(item sheetItem, selected, hovered bool, width int, pal palette) []string {
	return s.rowLinesWithin(item, selected, hovered, width, draftRows, pal)
}

// rowLinesWithin draws a row with the space this sheet can give an open
// connection box. Ordinary rows do not spend it; the one tall row may use it
// before dropping the end of a wrapped answer list.
func (s *sheet) rowLinesWithin(item sheetItem, selected, hovered bool, width, boxRows int, pal palette) []string {
	if item.conn != nil {
		return s.connRowLines(item.conn, selected, hovered, width, boxRows, pal)
	}
	if item.role != nil {
		return s.roleRowLines(item.role, selected, hovered, width, pal)
	}
	// A READING IS A ROW WITH NOTHING TO EDIT — `today`, and the two rails this
	// build enforces somewhere a settings row cannot reach (settingspend.go). It
	// draws like every other row and the cursor steps over it, which is the whole
	// of "not selectable".
	if item.read != nil {
		return overlayLines(item.read.name, s.readingNote(item, width), false, false, hovered, width, pal)
	}
	// AND A MONEY ROW RANKS ITS OWN FACTS. The value, the pin that froze it and
	// the receipt are three facts in priority order, fitted by rowfit into the
	// cells the label leaves — so a narrow frame drops the receipt whole rather
	// than cutting a figure in half (settingspend.go's [sheet.spendNote]).
	if item.row.Category == config.CategorySpending && item.row.Kind == config.SettingDollars {
		return overlayLines(item.meta.label, s.spendNote(item, width, pal.ascii), selected, false, hovered, width, pal)
	}
	value := item.row.Value()
	if value == "" {
		value = "—"
	}
	if s.changed(item) {
		mark := changedMark
		if pal.ascii {
			mark = "*"
		}
		value = mark + " " + value
	}
	if name, pinned := item.row.PinnedBy(); pinned {
		value += "  set by " + name
	}
	// THE MODEL ROW SAYS WHICH MACHINE IS ANSWERING IT. The id alone names a
	// decision this session did not make — one id is a dozen endpoints — and the
	// row that a person opens to change their model is exactly where the rest of
	// that fact belongs. Nothing is added when nothing is known (lanes.go).
	if word := s.laneWord(item); word != "" {
		value += " · " + word
	}
	// AND EVERY OTHER ROW THAT CARRIES A RECEIPT SAYS IT HERE. A receipt is a
	// live fact standing beside a value and never a second value
	// ([config.Setting.Receipt]); the money rows have their own ranked layout
	// above, and this is every other row that has something true to add.
	if receipt := item.row.Receipt(); receipt != "" {
		value += " · " + receipt
	}
	return overlayLines(item.meta.label, value, selected, false, hovered, width, pal)
}

// laneWord is the tail on the conversation's model row: `auto (cloudflare now)`
// when the lane is being chosen for you, `pinned: cloudflare` when it is not.
//
// It is only ever on THAT row. The other model rows are slots aforge fills on
// your behalf, and a lane pinned for the conversation is not a claim about them.
func (s *sheet) laneWord(item sheetItem) string {
	if item.row.Key != config.ModelSettingKey(talkSlot) {
		return ""
	}
	// THE ANSWER IS READ OFF THE LANE ROW ITSELF and not out of the profile a
	// second time: the two rows are two readings of one fact, and a panel where
	// they could disagree would be a panel that is wrong about one of them.
	if row, ok := s.registry.Row(config.LaneSettingKey(talkSlot)); ok {
		if word := row.Value(); word != "" && !strings.EqualFold(word, config.LaneAuto) {
			return strings.ToLower(word)
		}
	}
	if best, ok := bestLane(laneViews(s.sessionModel, timeNow())); ok {
		return "auto (" + strings.ToLower(best.Name) + " now)"
	}
	return ""
}

// connBoxRows is the most an open box may take from the list window. Its row
// heading and the line of air below it are spoken for first; a short window
// keeps the six-row ceiling that preserves the beginning of the question.
func connBoxRows(room int) int {
	return max(draftRows, room-2)
}

// selectLines draws the model picker in the list's place — LITERALLY the picker's
// own rows ([picker.rows]), so a slot row offers the window, the price and the
// arena score the /model overlay offers, and the row in use is marked the same
// way. It returns each line's position in the picker's hits, or -1, so the
// pointer reaches it too.
//
// level is [app.reasoningFor], threaded through for the same reason the overlay
// threads it: the effort a model has been dialled to lives on the agent, and a
// list is not a thing that holds a session.
func (s *sheet) selectLines(width, room int, pal palette, level func(string) string) ([]string, []int) {
	sel := s.sel
	// The picker hands back the hit each LINE belongs to rather than a count to
	// add to its top: a row at [tierPhone] is two lines, and "line i is hit
	// top+i" would put every click one row further down the list than the one
	// that was pressed (palette.go's [picker.rowsOwned]).
	out, owner := sel.pick.rowsOwned(width, room, pal, -1, level)
	for len(out) < room {
		out = append(out, "")
		owner = append(owner, -1)
	}
	return out, owner
}

// editLine is the text submenu's box, and the column its caret sits in.
func (s *sheet) editLine(width int, pal palette) (string, int) {
	edit := s.edit
	shown := edit.box.String()
	if edit.secret {
		shown = strings.Repeat("•", len([]rune(shown)))
		if pal.ascii {
			shown = strings.Repeat("*", len([]rune(shown)))
		}
	}
	lead := " " + edit.label + " "
	line := " " + pal.dim(edit.label) + " " + pal.accent(prompt) + pal.ink(fit(shown, width-len(lead)-3))
	return line, ansi.StringWidth(lead+prompt) + ansi.StringWidth(shown)
}

// footNote is what the panel says when it has nothing to complain about: where
// the writes land. It is one sentence and it is the truth people most often
// want from a settings panel they share between machines.
func (s *sheet) footNote() string {
	// The Connections tab writes somewhere else and answers a different
	// question, so it says its own line (connectcaps.go).
	if s.onConnections() {
		return s.connFootNote()
	}
	if item, ok := s.current(); ok {
		if name, pinned := item.row.PinnedBy(); pinned {
			return "held by " + name + " — unset it to change this here"
		}
	}
	// The path is READ from internal/config rather than spelled here, so the one
	// late rename (docs/CHAT-V3.md, Decision 26) moves the sentence a person
	// reads along with the directory it names.
	return "saved to your profile · a project's own " + config.ProjectConfigDir + "/" + config.ProjectConfigFile + " is a hand edit"
}

func (s *sheet) keysLine() string {
	switch {
	case s.edit != nil:
		return "enter save · empty clears · esc cancel"
	case s.sel != nil:
		// THE LEGEND SAYS `→ lanes` ONLY WHERE `→` OPENS THEM — on a row that
		// has a lane row behind it. Offering the key on the drawing slot would
		// be the foot of the screen promising a gesture that does nothing.
		if s.sel.pick.laneSlot != "" {
			return "↑↓ move · → or tab lanes · enter choose · esc cancel · type to filter"
		}
		return "↑↓ move · enter choose · esc cancel · type to filter"
	case s.onConnections():
		return s.connKeysLine()
	default:
		// A PINNED ROLE HAS A KEY THE REST OF THE SHEET DOES NOT, so the legend
		// says so on the row it works on and nowhere else — a line that offered
		// del everywhere would be offering it on rows where it does nothing.
		if item, ok := s.current(); ok && item.role != nil && item.role.pin != "" {
			return "↑↓ move · enter pin · del unpin · type to search · esc close"
		}
		return "↑↓ move · ←→ tabs · enter change · type to search · esc close"
	}
}

// sheetLayerOwnsKeys is whether one of the three layers inside this panel has
// taken the WHOLE keyboard ([placeSettings.owns]). The foot asks, because a
// hint line that named the router's `tab` while a layer had it would be naming
// a key nothing on screen answers (pages.go's [app.placeHintSaid]).
func (a *app) sheetLayerOwnsKeys() bool {
	if !a.at(pageSettings) {
		return false
	}
	s := &a.sheet
	return s.edit != nil || s.sel != nil || (s.conn.entry != nil && s.onConnections())
}

// hoveredSheetRow is the item the pointer is over, or -1.
func (a *app) hoveredSheetRow() int {
	if a.hot.kind == hoverSheet {
		return a.hot.index
	}
	return -1
}
