package e2e

// tuiwords_test.go is THE ONE PLACE THE TMUX SUITE'S LITERALS ARE WRITTEN DOWN,
// and the cheap gate that keeps them true.
//
// The tmux suite beside this file (tui_e2e_test.go, build tag `e2e`) waits for
// strings on a real screen. Every one of those strings is a sentence the product
// spells somewhere in its own sources, and when a wave respells one the suite
// does not fail loudly — it waits twenty seconds for a screen that will never
// arrive, and the person reading the failure has to work out which of forty
// needles moved. That is exactly what happened between 2026-08-23 and the home
// redesign: seven of nine subtests were waiting for a home that no longer
// existed, and nothing in the tree said so until somebody spent seventeen
// minutes and a real model finding out (issue #184).
//
// SO THE NEEDLES LIVE HERE AND NOWHERE ELSE, and this file's own test — which
// needs no model, no tmux and no build tag — reads the surface's sources and
// asserts that every one of them is still spelled there. Deleting a string from
// internal/tui3 turns THIS test red in four hundred milliseconds, on the pull
// request that deleted it, instead of turning the tmux suite red in a wave
// nobody ran it in.
//
// THE TABLE IS THE ONLY DOOR. The tmux suite reaches a literal through [say],
// which fails the test on a name that is not in the table, so a needle cannot be
// added to the suite without being written down here — and the second half of
// the gate ([TestEveryWordInTheTableIsWaitedForBySomething]) reads the suite's
// own source back and fails on an entry nothing waits for any more. The two
// halves together are what keeps this a source of truth rather than a second
// copy that drifts.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// tuiWord is one literal the tmux suite waits for on a real screen.
//
// THE SCREEN AND THE SOURCE ARE TWO DIFFERENT FACTS and the table carries both.
// Most sentences this surface draws are one constant and the two are the same
// string; some are composed at the draw — `↑↓ pick · enter open · tab next
// place` is two constants joined — and a gate
// that grepped for the whole painted line would fail on a sentence the code
// spells perfectly well in two halves. So `source` is what must stand in the
// sources, and it defaults to `screen` when the two agree.
type tuiWord struct {
	// screen is EXACTLY what the product draws, and what the suite waits for —
	// or, on a row that names a [tuiWord.key], the WORD ALONE, because the
	// punctuation between a key and its word is the product's to choose and
	// [keyedWord] is where this suite writes it down.
	screen string
	// key is the key this word is offered on, where it is offered on one.
	//
	// A ROW THAT NAMES A KEY DOES NOT SPELL THE GAP ITSELF. The surface drew
	// `[c] change` for a year and stopped in #933 — one key, one space, the word
	// — and eleven rows of this table went on waiting for the brackets while
	// their `source` (the word alone) kept the gate perfectly green. That is the
	// hole issue #998 is: a needle half of which nothing checks. Naming the key
	// here puts the whole needle through [keyedWord], so the next wave that
	// respells the grammar respells one function and every row moves with it.
	key string
	// source is the substring that must still stand in `pkg`'s own non-test
	// sources. Empty means "the source spells it exactly as the screen does".
	source string
	// pkg is the package directory that owns the spelling, relative to the module
	// root. Empty means internal/tui3, which owns nearly all of it — the few
	// entries that name another package are words the ENGINE writes and the
	// surface only relays, and a gate that looked for them in the surface would
	// be looking in the wrong tree.
	pkg string
	// why is what this word is evidence of, in the suite's own terms. It is prose
	// on purpose: a table of forty strings with no reasons is a table people
	// delete rows out of.
	why string
}

// tokensPkg is the SHARED GLYPH VOCABULARY, and it is where every mark on this
// surface is now spelled: the icons wave took the literals out of internal/tui3
// and put one slot table behind one door, so a needle that looked for a glyph in
// the surface would be looking where the surface no longer writes one.
const tokensPkg = "internal/tui2/tokens"

// tui3Pkg is where the surface's own words live, and the default for [tuiWord.pkg].
const tui3Pkg = "internal/tui3"

// tuiWords is the whole vocabulary the tmux suite waits for.
//
// EVERY ENTRY WAS TAKEN OFF A REAL SCREEN AND THEN FOUND IN THE CODE, in that
// order — the suite's own file header states the law and this table is where it
// is kept. Nothing here was remembered.
var tuiWords = map[string]tuiWord{
	"roomKinSpawnedWord": {screen: "handed out: ", why: "the waiting parent's child summary is visible"},
	"roomBackWord":       {screen: "esc/← main", why: "the task room offers its way back"},

	// ── this machine's doors on the ordinary engine road ────────────────────
	"connectFilterHint": {
		screen: "filter · ↑↓ · enter connect · esc close",
		why:    "the local connection catalog opened as its searchable panel",
	},
	"connectModelsGroup": {
		screen: "models",
		why:    "the connection panel includes model services rather than only account rows",
	},
	"connectUnavailableWord": {
		screen: "connections are unavailable here",
		why:    "the refusal that must be absent from an ordinary launch and remain available to the hosted seam",
	},
	"noHarnessWord": {
		screen: "no harnesses are registered yet — build one in the conversation",
		why:    "an empty local registry still opens and says what may arrive there",
	},
	"harnessUnavailableWord": {
		screen: "harnesses are unavailable here",
		why:    "the refusal that must be absent from an ordinary launch and remain available to the hosted seam",
	},
	"crewMainKeys": {
		screen: "enter change · esc close · ? keys",
		why:    "/crew opened its panel, framed, with its keys in the bottom edge",
	},
	"crewAutoWord": {
		screen: "auto — codeaf picks per task",
		why:    "enter on a seat opened the seat list on its first row, auto",
	},

	// ── the bounded stop ─────────────────────────────────────────────────────
	"stoppingWord": {
		screen: "stopping",
		why:    "the status word between a person's esc and the engine letting go of the turn",
	},
	"stopDetachWord": {
		screen: "detaching in ",
		why:    "the bound on the stopping window, stated on the status line BEFORE it fires (issue #265)",
	},
	"interruptedWord": {
		screen: "interrupted",
		why:    "the status word once a stopped turn is genuinely over — what the bound is measured against",
	},
	"idleWord": {
		screen: "idle",
		why: "the status word once a turn has finished of its own accord — the other end of the same reading " +
			"[interruptedWord] is one state of ([app.runState]). It is HOW THIS SUITE KNOWS A MODEL HAS " +
			"STOPPED without guessing at a number of seconds, on a scenario whose own card offers nothing " +
			"to wait for",
	},
	// ── the skills a person already has ──────────────────────────────────────
	"skillsCarriedWord": {
		screen: "skills · ",
		pkg:    "internal/tui3",
		why: "the dim note under a message naming the skills its turn carried, kept above the turn's " +
			"`▸ worked` chip — the only screen evidence that a skill from another tool's folder reached " +
			"a turn by itself or by /skill ([testForeignSkills]); the headless --once door prints the " +
			"engine's own `skills carried: ` sentence instead",
	},
	"skillNoShelfWord": {
		screen: "this conversation has no skill shelf",
		why: "the picker row's tail when there is no shelf to attach against. It must be ABSENT on the " +
			"ordinary launch with memory on and off: it once read `memory is off` on every machine",
	},
	"skillCannotCarryWord": {
		screen: "this conversation cannot carry attached skills",
		why: "what choosing a skill says when the session under the surface has no attachment doors — " +
			"which was every choice on the ordinary launch before the doors crossed the session host's socket",
	},
	"stopDetachedWord": {
		screen: "detached — the turn was let go of and nothing is waiting for it",
		why:    "the note a turn let go of at the bound leaves in the conversation",
	},

	// ── home at rest ─────────────────────────────────────────────────────────
	"placeRestWord": {
		screen: "type to search or start something new",
		why:    "the promise in home's box with nothing typed into it — the one box on a place",
	},
	"homeDoorWord": {
		screen: "space space home",
		why:    "the gesture back to home, named on the conversation's own rule",
	},
	"microcopy": {
		screen: "/ commands",
		why:    "the other half of that rule, which is how a conversation is told from home",
	},

	// ── home's panels (docs/design/home-mission-control/DESIGN.md) ────────────
	//
	// THE FLAT RANKED LIST IS GONE, and with it the section line, `alt+g`,
	// `alt+q` and the `quiet since` fold the suite used to wait for. Home is seven
	// panels now, each a heading from homegrid.go's order table; an empty one
	// keeps its heading and one whisper, so a heading on the screen says the
	// panel is there and says nothing about whether anything is in it.
	"homeNeedsHeading": {
		screen: "needs you",
		why: "every question on the machine lands in it, and every resting home draws it. The two words are " +
			"the whole heading — it carried its live count (`needs you · 2`) until #1046 struck it — and they " +
			"are also the front of the gate's own `needs your ok …`, so a test that wants the HEADING has to " +
			"read the column the heading opens rather than grep for the word",
	},
	"homePanelProjects": {
		screen: "projects",
		why:    "the panel of folders, never empty; clicking a project selects the next message’s folder",
	},
	"homePanelRunning": {
		screen: "sessions",
		why: "the recent conversation section, newest first. It was " +
			"`running` until 2026-09-17, and now shares its word with the bar's second tab, which it folds into",
	},
	"switcherSinceLeft": {
		screen: "since you left",
		why:    "the panel over what happened while nobody was looking",
	},
	"homePanelSpend": {
		screen: "spend",
		why:    "the day and the fortnight on home, and the third word of the four-place bar",
	},
	"homePanelNext": {
		screen: "standing",
		why:    "every standing order this machine will act on — reminders, routines, watches, rules — soonest first",
	},
	"homeRunningWhisper": {
		screen: "your recent conversations appear here",
		why: "what `sessions` says with nothing under it — the whisper law (DESIGN §4): an empty panel names " +
			"what arrives there and never announces that it is empty. It is short enough to stand on one line " +
			"at a hundred and twenty cells, which is why it is the whisper the suite waits for",
	},

	"barHomeWord": {
		screen: "home",
		why:    "the first of the four words on the tab bar",
	},
	"barTasksWord": {
		screen: "sessions",
		why:    "the second word on the bar, and the place the sessions heading opens",
	},
	"barSettingsWord": {
		screen: "settings",
		why:    "the last of the four — standing, memory and search are off the bar and reached by command",
	},
	"pulseWantWord": {
		screen: " want you",
		why: "the pulse INSIDE A CHAT, which keeps the machine's counts on its top line (DESIGN §1 law 11); " +
			"on home the pulse leaves them to the panels",
	},
	"homeCardMoreWord": {
		screen: "→ more",
		why: "the last clause of the keys legend that closes the card beside a search (homeband_keys.go). " +
			"It was `→ verbs` on the resting card, which the grid retired; the search card never spelled that",
	},
	"homeHereWord": {
		screen: "here",
		why:    "this window's own row on `threads`, the row the person's last words sit under",
	},
	"homeFactsActive": {
		screen: "last active ",
		why:    "the facts line on the card beside a search — the one card left once the resting card went",
	},
	"homeStartWord": {
		screen: "start a new conversation",
		why:    "the action row under anything typed at home",
	},

	// ── asking from home ─────────────────────────────────────────────────────
	"homeAskHereWord": {
		screen: "ask here",
		why: "what one ↑ off the action row starts, drawn as the row's own heading over the pane. " +
			"`ctrl+enter` is still bound and is no longer advertised — most terminals cannot send it " +
			"and `alt+enter` belongs to the task layer (home.go's [app.homeHintWords])",
	},
	"notifyAskWord": {
		screen: "waiting on you",
		pkg:    tui3Pkg,
		why:    "the tail on any row — an errand or another window's conversation — that is stopped on a person",
	},
	"homeAskWorkingWord": {
		screen: "working",
		why:    "the tail an errand's row wears while its turn is in flight",
	},
	"homeAskStoodTail": {
		screen: "stood",
		why:    "the tail an errand's row wears once something stands because of it",
	},
	// The three words of the live strip. They are GLIMPSED and never waited for —
	// a spinner missed on a fast reply is a fast reply — but they are needles all
	// the same, and a needle outside this table is a needle the gate cannot see.
	"homeAskThinkWord": {
		screen: "thinking ·",
		source: "thinking",
		why:    "the pane before the first token of a turn has arrived",
	},
	"homeAskWriteWord": {
		screen: "writing ·",
		source: "writing",
		why:    "the pane once the reply is streaming",
	},
	"homeAskRunWord": {
		screen: "running ·",
		source: "running",
		why:    "the pane while a call of this turn is executing",
	},
	"homeAskStoodWord": {
		screen: "kept · this exchange is filed under it",
		why:    "the pane saying the exchange is filed under what it made",
	},
	"exchangeAnswerHint": {
		screen: "Don't remind me",
		source: "Don't remind me",
		pkg:    "internal/session",
		why: "the answers a ONE-OFF REMINDER's card offers, spelled in full under the box at every width. " +
			"There are two rows and the key that asks for the box: a reminder has no `3 just once` to give, " +
			"and since #189 the line is built from the answers the question carries rather than typed out, so " +
			"it cannot name one. Since the standing card moved onto the question block (#780) the correction " +
			"is `o other` — the key table's own word — rather than a `2` row. The source is the first " +
			"answer's own constant, because the sentence is no longer a literal anywhere",
	},
	"exchangeFollowUp": {
		screen: "enter sends a follow-up",
		why:    "the exchange pane holding the keyboard",
	},
	"exchangeBack": {
		screen: "tab or esc back to the list",
		why:    "both ways out of the pane, named because on a narrow frame the pane is the whole screen",
	},
	"homeAnswerHint": {
		screen: "enter or tab answer this ",
		why:    "the hint while the cursor stands on an errand row that is asking something",
	},
	"standRemindYes": {
		screen: "Remind me",
		pkg:    "internal/session",
		why:    "the yes on a one-off reminder, which is what the errand in this suite asks for",
	},
	"standSetWord": {
		screen: "set up",
		why:    "the other half — what a settled card keeps as its verdict",
	},

	// ── what stands, and what it costs ───────────────────────────────────────
	"homeKeepingWord": {
		screen: " standing order",
		why:    "the count at the foot of the task column while something stands, and a door onto the standing place — `◦ 2 standing orders`, respelled from `keeping an eye on 2` on 2026-09-09 because that named nothing a person could type, and moved off the status row on the same day; /status and the phone sheet keep the same words under `watching`",
	},
	"homeWatchLabel": {
		screen: "keeping watch",
		why:    "/status's line about whether anything checks the world with no window open",
	},
	"consentRowLine": {
		screen: "needs your ok to run ",
		pkg:    "internal/session",
		why:    "the whole sentence a consent gate hands another window — written once by the engine, repeated on home's row with nothing added to it",
	},
	"standSaidTag": {
		screen: "said: ",
		why:    "the clause a firing's own row wears in the conversation it lands in",
	},
	"standingFiredWord": {
		screen: "fired ",
		pkg:    "internal/standing",
		why:    "what the `since you left` block says about a watch that went off while nobody was here",
	},

	// ── the money, said the same way wherever it is read ─────────────────────
	"spendRailsHint": {
		screen: "/budget sets the limits",
		why:    "the /spend place's own pointer line, which carries the day's figure and the door to the rails",
	},
	"spendTodayResets": {
		screen: " · resets at midnight",
		why:    "the Spending tab's `today` reading, the row the money segment's door lands above",
	},
	"spendThisOneWord": {
		screen: "this one ",
		pkg:    "internal/config",
		why:    "the receipt beside the per-conversation ceiling — the ENGINE's registry writes it and the tab only relays it",
	},
	"spendConversationRow": {
		screen: "per conversation",
		why:    "the Spending row the `this one` receipt hangs off",
	},

	// ── waiting on a machine that has gone quiet ─────────────────────────────
	//
	// THE PHASE CLOCK COMPOSES BOTH OF ITS SENTENCES AT THE DRAW, out of halves
	// two packages own (internal/tui3's phase.go, and the clock that feeds it in
	// internal/provider). So each half is its own row and the suite asserts the
	// join, which is the shape `standRemindYes` and `standSetWord` already have.
	// What varies is not a needle: the machine that went quiet is whatever this
	// run pinned, and the provider a rescue would go to is whatever the frontier
	// named. What stands still is the clause around them.
	"phaseSlowWord": {
		screen: " is slow",
		pkg:    "internal/provider",
		why: "the pinned machine has gone quiet — the ENGINE writes this clause and the surface only relays it, " +
			"so a gate that looked for it in internal/tui3 would be looking in the wrong tree",
	},
	"phaseOfferWord": {
		screen: "switch to ",
		why:    "the rescue the question offers, on a row with room to name where it would go",
	},
	"phaseOfferKeyWord": {
		screen: "(y)",
		why: "the one key that ends the wait, and the last thing a narrow row spends: " +
			"an offer whose key was cut is a question nobody can answer",
	},
	"phaseAllSlowWord": {
		screen: "all hosts slow",
		why:    "every reachable provider is believed slow, so there is nowhere better to be",
	},
	"phaseWaitingWord": {
		screen: "still waiting",
		why:    "the other half of that report — acting would buy nothing, and saying so IS the act",
	},

	// ── a machine that will not serve this model ─────────────────────────────
	//
	// THE SURFACE SAYS WHAT THE WIRE SAID, and for a whole measured run it did
	// not: a 404 meaning `your request's provider.only preference permits only:
	// coreweave` was drawn as `· slow · trying nextbit…`, which is a sentence
	// about a wait rather than about a machine, and it stayed on the row for
	// ten minutes after the arm it described had died (issue #266). All three
	// rows are internal/tui3's own literals, written out rather than composed,
	// so this gate can find them.
	"laneRefusedTrying": {
		screen: " · refused · trying ",
		why:    "a provider said no and the answer is already on its way somewhere else",
	},
	"laneRefusedTail": {
		screen: " refused",
		why: "the retraction: the rescue this row was promising has itself been refused, " +
			"so the promise comes off and the fact is what is left",
	},
	"laneSlowTrying": {
		screen: " · slow · trying ",
		why: "the same row about a provider that was merely LATE — waited for as an ABSENCE " +
			"by the refusal subtest, which is how a wrong word is caught rather than a missing one",
	},

	// ── a question answered from another window ──────────────────────────────
	"consentAskWord": {
		screen: "needs your ok to run",
		pkg:    "internal/session",
		why: "a conversation stopped on a permission question. It is the HEAD the engine writes, " +
			"which the panel writes into the frame's top edge (the questions wave retired `allow? ` " +
			"and every other bracketed offer row: a key is the payload hue and its word is dim now)",
	},
	"answersAllowOnce": {
		screen: "allow once",
		pkg:    "internal/session",
		why:    "the first option on that question, which the ENGINE writes and home only relays",
	},

	// ── a landing that is the person's call (#268, docs/design/task-states) ───
	"settleAskWord": {
		screen: "nobody could check it",
		pkg:    "internal/session",
		why: "the reason row on a landing nobody could check. It is the ENGINE's sentence now, spelled once " +
			"beside the two verbs that answer it (task_status.go's TaskAsk) — the surface used to write its " +
			"own, `finished, but nobody has checked it — your call`, which is how one state came to have " +
			"four names",
	},
	"settleAccept": {
		key:    "a",
		screen: "accept",
		pkg:    "internal/session",
		why: "the answer a person presses. The key is the surface's and the WORD is the ask's, so a conflict " +
			"card can read `a resolve it` on the same column without a second constant anywhere",
	},
	"settleNotRight": {
		key:    "n",
		screen: "not right",
		pkg:    "internal/session",
		why:    "the answer that says checked work is not finished, and drives the refused ending",
	},
	"settleTellIt": {
		key:    "s",
		screen: "tell it",
		pkg:    "internal/session",
		why: "the third column on every one of these cards: say something to the task rather than answering. " +
			"It must be drawn, because a person with something to say who finds only yes and no presses one of them. " +
			"The WORD is the engine's now — one landing question's third answer (answers.go), drawn by the question " +
			"block wherever that question is standing — and the key is still task-states' own",
	},
	"settleTookLine": {
		screen: "you took this as done",
		pkg:    "internal/session",
		why: "the receipt the card wears once the accept has been spent, which is how the pane proves the key landed. " +
			"It NAMES WHO SPENT THE VERB (#767): the person's own press says `you`, and the model spending it under " +
			"`task.settle = auto` says `codeaf took this as done`, so a receipt is never a lie about whose call it was",
	},
	"settleNotRightLine": {
		screen: "you said it is not finished",
		pkg:    "internal/session",
		why:    "the receipt proving the not-right answer reached the engine's settle door",
	},
	"taskIncompleteWord": {
		screen: " · incomplete",
		source: "incomplete",
		why:    "the person-facing state for work a check refused with concrete gaps still open",
	},
	"taskFailedWord": {
		screen: " · failed",
		source: "failed",
		why: "the word a LANDING may no longer wear: the card says `incomplete` plus its reason, and the suite " +
			"asserts this is absent from the screen. It is still the record page's word for a fault",
	},
	"starterTaskWord": {
		screen: "/task <brief> starts work",
		why:    "the greeting's own starter line, and the door a person is pointed at before they have typed anything",
	},
	"refusedCallRowWord": {
		screen: "the call was refused",
		pkg:    "internal/tui3",
		why: "the whole of what a refused call's row draws for the person (feed.go's refusedCallWord), and the tail " +
			"of task.go's taskFormingRefused card. The schema's own `Invalid arguments:` sentence is mail for the model: " +
			"it stays in the tool result and behind ctrl+o, and the suite asserts it is ABSENT from the screen",
	},

	// ── the roster column, and the difference between empty and ignorant ─────
	//
	// #761: a window that showed a conversation's replies and status line while
	// the engine ran that conversation's tasks drew `+ /task` and nothing else.
	// The label is the whole assertion, because the column only earns one when
	// there is a row under it — an empty section would spend a line announcing
	// absence, which the emptiness law forbids (margin.go).
	"railTasksLabel": {
		screen: "tasks",
		why: "the roster column's own section label (margin.go's marginTasksWord), drawn ONLY when the " +
			"column has a task row to put under it — so its presence beside a conversation is the " +
			"surface saying it knows what the engine is running",
	},
	"railTaskDoorWord": {
		screen: "+ /task",
		source: "/task ",
		why: "the roster column's door (margin.go's marginDoorMark plus marginTaskType), which is the " +
			"whole of what a column with no task facts draws. #761's failure is this line ALONE",
	},

	// ── the task room, and the key the door home had to give back ────────────
	//
	// #457/#486 widened `space space` to open home from every place. The room a
	// task's record is read in is the one place on the surface where a bare space
	// already MEANT something — it pages the card, the way `pgdown` and `ctrl+f`
	// do — and the widened door took it. These two words are what the real screen
	// is read for: that the roster offered the room, and that the room was still
	// standing after the space was pressed.
	"tasksEnterRoomWord": {
		screen: "enter open its room",
		why:    "the roster's foot over a node this window's graph is still holding: the door into the LIVE room",
	},
	"tasksUntitledWord": {
		screen: "new conversation",
		why: "the name the tasks place gives a conversation that has said nothing yet (#915): the row the " +
			"launch's own first session puts on the page, which used to draw that session's raw id. The " +
			"word is [unnamedConversationWord] — one constant behind every surface that calls a nameless " +
			"chat by this — and its one string literal stands in internal/tui3/names.go beside the same " +
			"words home's own row draws, so this gate holds the spelling without a second copy of it here",
	},
	"landingKeysWord": {
		screen: "esc interrupts · ctrl+c quits",
		why:    "the notice a conversation greets on, and what a window that RESUMED an earlier one draws instead of home",
	},
	"questionWaitingWord": {
		screen: " · waiting in this conversation · alt+a",
		source: "waiting in this conversation",
		why: "the row a place that is NOT the conversation draws over an open question " +
			"(questiondelivery.go's questionWaitingLine). It stands in the roster's foot where the " +
			"enter-door would be, so a task that landed `your call` and asked something is read here",
	},
	// ── the tasks place's own fold ───────────────────────────────────────────
	//
	// The place groups its rows by conversation and draws every group SHUT, so a
	// scenario that reads a piece of work inside one has to open it — and has to
	// know the press landed. These two words are the two answers the foot gives,
	// and they are the suite's only honest way to tell a page with nothing in it
	// from a page whose fold has not opened yet.
	"tasksFoldShutWord": {
		screen: "→ what ran under it",
		why: "the tasks place's foot over a SHUT conversation group (place_tasks.go's tasksOpenWord). " +
			"It is waited for before the `→` is pressed, because a key that reached the program before " +
			"the place was up is a key nothing answered — and the page then held no row for the work, " +
			"which a screen-wide wait for a state word read straight past",
	},
	"tasksFoldOpenWord": {
		screen: "← fold it back up",
		why: "the same foot once the group is OPEN (place_tasks.go's tasksShutWord), which is what says " +
			"the `→` landed and the rows inside are drawn. Waiting on it rather than sleeping is what " +
			"made the run-engine scenarios repeatable: two runs of one binary split on whether the fold " +
			"had opened by the time the assertion read a row",
	},
	"tasksEnterInsideWord": {
		screen: "enter go inside it",
		why: "the roster's other door, over work no window is holding any more — it is the one that opens the " +
			"record card, which is the mode `space` pages and the door home had to give the key back to",
	},
	"tasksPaneOpenWord": {
		screen: "enter open",
		why: "the verb line of the RECORD PANE beside the tasks list (taskpane.go), which is drawn on " +
			"every frame 110 columns or wider — the suite runs at 120. It is the one clause of that line " +
			"every row has, whatever state the model's work landed in, which is why the wait is on this " +
			"and not on the two answers beside it. IT IS A PREFIX OF [tasksEnterRoomWord] " +
			"(`enter open its room`), so it discriminates only where the foot is NOT offering a room — " +
			"which is exactly where its own subtest waits for it: the window that reads the record back " +
			"holds no node, its foot says `enter go inside it`, and that wait comes first",
	},
	"taskRoomFootWord": {
		screen: "m puts it in your message · ↑↓ scroll",
		why: "the record's own foot, and the one sentence that says a person is still standing in the room — " +
			"it names `↑↓ scroll`, which is the family of keys `space` belongs to on this card. " +
			"IT WAS `esc back · ↑↓ scroll · m puts it in your message` AND THE ORDER IS THE POINT OF THE CHANGE: " +
			"the foot is fitted by [hintFit], which keeps a key row's FINAL clause and drops the ones in front of " +
			"it working backwards, so a sheet that opened on `esc back` gave the way out away first and a " +
			"sixty-cell card offered the mention and no way off the page. This is the HELD sheet — the one the " +
			"foot draws wherever the head's own corner already says `esc back`, which is every width the suite " +
			"runs at — so the way out is not on it at all",
	},

	// ── the run engine's plan, on the tasks place ────────────────────────────
	//
	// A `/task` on the worker harness — the default, and what CODEAF_TASK_BELT=bash
	// names outright — starts a RUN rather than a node of this session's own
	// tree: the conversation seeds a plan store, the engine
	// drives it, and the store's root lands on the tasks place beside the record
	// (internal/tui3's taskplan.go). These rows are what the tmux suite reads to
	// prove the run happened, moved, and left a page of its own.
	"planRunningWord": {
		screen: "running",
		why: "the state word a run's plan row wears while a worker holds the task. planStateWord maps " +
			"the store's `ready` and `claimed` onto it, so the row says what a person does next — work is " +
			"in flight — and never a machinery word of its own. THE PLACE'S OWN HEADING SPELLS IT TOO " +
			"(tasksSectionWord files everything working under `running`), so the suite reads the word off " +
			"the run's row and not off the screen",
	},
	"planDoneWord": {
		screen: "done",
		why: "the state word the same row wears once the run's root has landed — planStateWord's other " +
			"mapping, and the one word every surface gives work that finished. Asked of the row for its " +
			"running neighbour's reason: `done` is an ordinary English word and a screen-wide search for " +
			"it is satisfied by a sentence that is not the row's",
	},
	"planFinishCommand": {
		screen: "plandb done",
		source: "plandb done",
		pkg:    "internal/session",
		why: "the command a bash-belt worker finishes its store task with, recorded in the task's own " +
			"trajectory and drawn as a call row in the task's room (planroom.go reads " +
			"PlanTaskPage.Steps). THE PAGE DOES NOT SPELL IT — the worker runs it — so the gate looks where " +
			"it is written: internal/session's plandb_plan.go, the sentence that teaches the finish. " +
			"IT IS OBSERVED AND NEVER WAITED OUT, because whether it is on a page is the WORKER'S " +
			"choice: the run writes the ending itself for a task whose worker stopped calling tools " +
			"without writing one (internal/run's worker.go), which a small brief on a fast model " +
			"regularly is. What the suite asserts about that room instead is that it is the task's " +
			"room at all: roomTabsWords",
	},
	"planNoteBoxWord": {
		screen: "a note for this task",
		why: "the note box a run's task takes its notes in (taskplan.go's taskPlanNoteWord, drawn as " +
			"the task room's steer box by room.go) while the task can still take a note. A task that has " +
			"ended names another door in its box, so the suite only observes this word; what it asserts " +
			"about the room is roomTabsWords",
	},
	"roomTabsWords": {
		screen: "transcript · work",
		source: "transcript",
		why: "the two tabs every task's room draws on its trail, on either engine (roomtabs.go): the " +
			"transcript and the work the task changed. A run's task opens the same room as any other " +
			"task, so the tabs are what say the press opened the room and not a page of its own",
	},
	"planLiveGlyph": {
		screen: tokens.GlyphStepRunning,
		pkg:    tokensPkg,
		why: "the mark the task's room leads the step it is running RIGHT NOW with (planroom.go draws " +
			"the store's live step as a call in flight, through palette.glyph so the tier picks the rune " +
			"rather than a literal in the surface). It is tokens.GStepRunning — one " +
			"shape for one state, the same rune `working` wears — so the gate looks where the rune is " +
			"spelled, and this suite sees the plain tier its throwaway profile pins",
	},

	// ── the run's plan row, on the live edge (c185, SURFACE.md §2A/§3) ────────
	//
	// A plan row's own live step and the corrections the design makes to its
	// words. The run's engine publishes the in-flight step on the store row
	// (session.PlanTaskRow.Live); the surface draws it under the row's title. A row
	// admitted and not started reads `queued` rather than the machinery's
	// `pending`, and one held behind named work reads the dependency sentence
	// beside it (internal/tui3's planStateWord and planWaits).
	"planQueuedWord": {
		screen: "queued · waits: ",
		source: "waits: ",
		why: "the words a run task's row wears while it is admitted and not started: planStateWord " +
			"maps the store's `pending` onto the surface's own `queued`, and the work it is held behind is " +
			"named beside it — `queued · waits: Add rate limiting`, one sentence, joined by " +
			"session.TaskStatus.RowWord so the state cell, the line the cursor's row grows and the phone " +
			"card all read it the same way. It is there so a person watching a run reads where a task is " +
			"rather than a machinery word of the store's, and what is holding it rather than an id",
	},
	"planLiveLead": {
		screen: "$ ",
		source: tokens.GlyphShell,
		pkg:    tokensPkg,
		why: "the shell lead a plan row's live step line opens with, ahead of the command the step is " +
			"running (task.go's planLiveRow). The `$` is the vocabulary's own shell glyph — the same byte " +
			"a conversation's tool line carries — drawn off the table and never spelled here",
	},
	"planStepsSpend": {
		screen: "steps · $",
		source: "steps",
		why: "the figures a plan row's under-block carries under the live command: how many steps its " +
			"worker has taken and what it has cost, each half omitted when it is nothing (taskplan.go's " +
			"planFigures). It is the same `N steps` and `$` the row already spends in its two columns",
	},

	"taskLookWord": {
		screen: "your call",
		pkg:    "internal/session",
		why: "the tier word on the head of a landing that is waiting on a person. It replaced `needs your look`, " +
			"`awaiting review` and `unverified` — one word for one state, spelled in the engine and drawn by " +
			"every surface (docs/design/task-states/DESIGN.md)",
	},
	"unverifiedGlyph": {
		screen: "?",
		why: "the one cell that asks the question on the roster row and on the landing card — the shared " +
			"vocabulary's tokens.GNeedsHuman slot, drawn through tasktier.go's tierSlot. It is the PLAIN " +
			"tier's spelling, which is what this suite sees: the throwaway profile pins `plain` in the " +
			"Display row (harness_test.go), because a patched terminal draws the private-use icon and a " +
			"capture-pane of one is not a thing a needle could honestly assert. It is one character, so " +
			"the frame it is read in is held together by the sentences beside it",
	},

	// ── the three tiers, one glyph and one word each (task-states) ───────────
	//
	// docs/design/task-states/DESIGN.md is the ruling and these are its words on a
	// real screen. THE WORDS ARE THE ENGINE'S and the KEYS ARE THE SURFACE'S,
	// which is why several rows here name a `key` and a `source`: a card that
	// reads `a resolve it` on the same column where another reads `a accept` is
	// one constant in each place and not two spellings of one answer.
	"taskDoneGlyph": {
		screen: "✓",
		pkg:    tokensPkg,
		why: "the `over` tier's cell for work that ran to the end — the shared vocabulary's GlyphSettled " +
			"slot, drawn through tasktier.go. It is the PLAIN tier's spelling, which is what this suite " +
			"sees: a patched terminal draws the private-use icon instead and a capture-pane of one is not " +
			"a thing a needle could honestly assert",
	},
	"taskBadGlyph": {
		screen: "✕",
		pkg:    tokensPkg,
		why: "the `over` tier's cell for work that did not finish — GlyphFailed. It is NOT the cell a " +
			"person's own stop wears, which is GlyphStopped, because nobody found anything wrong with work " +
			"somebody ended. It was `✗` in internal/tui3 until the icons wave moved every mark into one " +
			"vocabulary (docs/design/icons/DESIGN.md)",
	},
	"taskDoneWord": {
		screen: " · done",
		source: "done",
		pkg:    "internal/session",
		why: "the tier word on the head of a landing that finished, hung off the title by the one separator " +
			"this surface joins facts with. It is the engine's spelling (task_status.go's taskWordDone)",
	},
	"taskOneFileWord": {
		screen: " · 1 file",
		source: " file",
		pkg:    tui3Pkg,
		why: "the file count on the head, SINGULAR. Both spellings exist in the source because `1 files` is " +
			"the surface being sloppy about the one number on the row",
	},
	"taskMergedFact": {
		screen: " · merged",
		source: "merged",
		pkg:    tui3Pkg,
		why: "where the work ended up, as a FACT and never as a state. `delivery needs attention` and " +
			"`stopped — branch kept` fused the two into one phrase and are deleted",
	},
	"taskBranchKeptFact": {
		screen: " · branch kept",
		source: "branch kept",
		pkg:    tui3Pkg,
		why: "the other half of the same fact: a branch that never came home, named on the head so the " +
			"person has a handle back to work that is not on screen. It replaced `stopped — branch kept`, " +
			"which fused a state and a source-control fact into one phrase on the row that says the state",
	},
	"settleAnswersRow": {
		screen: "a  accept",
		source: "tell it",
		pkg:    "internal/session",
		why: "THE FIRST ANSWER ON ITS OWN ROW. The three answers were one bracketed row until the " +
			"questions wave gave every answer a row of its own with the pointer on it, so a single " +
			"string can no longer stand for all three; the words are still the engine's, which is where " +
			"they are searched for, and the two spaces are the panel's own column",
	},
	"settleConflictAnswers": {
		key:    "a",
		screen: "resolve it",
		pkg:    "internal/session",
		why: "a conflict's own two verbs on the same two columns. A conflict's yes is NOT an accept: it " +
			"spends one more merge round, which is why the word is the ask's and not the card's",
	},
	"settleConflictNo": {
		key:    "n",
		screen: "drop it",
		pkg:    "internal/session",
		why: "the half of that row that stands even on an engine with no resolver door — the absence law " +
			"drops each column on its own rather than taking the row down with it",
	},
	"taskConflictReason": {
		screen: "conflicts with your branch",
		pkg:    "internal/session",
		why: "the reason sentence of the one your-call question that is never the model's to answer. The " +
			"files are named after a colon, and the sentence stops here when git would not say which",
	},
	"taskStepsReason": {
		screen: "ran out of steps",
		pkg:    "internal/session",
		why: "the incomplete reason for a spent step threshold. `failed` is gone as a landing's word: what " +
			"a person reads is `incomplete` plus one of these sentences",
	},
	"taskAutoDecidingWord": {
		screen: "codeaf is deciding",
		pkg:    "internal/session",
		why: "the auto-settle floor's own row. A card with no chips MUST say why it has none — that defect, " +
			"a card with no choices and no explanation, is what the whole wave exists to close. THE ANSWERS " +
			"STAY DRAWN BESIDE IT (#767): the clause says who is deciding, and answering it is how a person " +
			"takes the decision back",
	},

	// ── the front door, on a machine that has never run codeaf ───────────────
	"setupTitleWord": {
		screen: "setting up",
		why:    "the dim line over the first-run question, which says where in the flow this is",
	},
	"setupConnectHeading": {
		screen: "connect openrouter",
		why:    "the heading of the step a fresh install meets first — the whole subject of #322",
	},
	"setupConnectSentence": {
		screen: "sign in once in your browser",
		why: "the sentence under that heading, which is what makes the step answerable rather than " +
			"a bare box; the constant runs on past this into what it will and will not send, and the " +
			"block wraps it, so the needle is the clause the reader meets first",
	},
	"welcomeStarterKeysWord": {
		screen: "\u2191\u2193 choose \u00b7 enter fills the box \u00b7 or just type",
		why: "the greeting's own foot, and the door this suite has to recognise: a conversation nobody has " +
			"typed in yet stands on its starting points (internal/tui3's welcome.go), and on the machine's " +
			"FIRST conversation it stands through typing. A subtest that waits only for the home foot or the " +
			"landing keys is waiting for a row this screen is covering",
	},
	"setupSkipWord": {
		screen: "esc skips setup",
		why: "the setup's own foot, on EVERY step of it. A state root built a minute ago opens on the setup " +
			"whatever the profile it copied holds, because the marker that says it has been seen is a file in " +
			"that root — so a suite that seeds a graph and reads it back has to know it is standing on this " +
			"screen and press past it",
	},
	// ── questions: one object, drawn at the size the evidence needs ──────────
	//
	// docs/design/questions/DESIGN.md is a contract about what a person SEES when
	// this engine hands them a decision, and questions_e2e_test.go is the only
	// place that drives it through a real binary. Every sentence below is a
	// product sentence; the heads, labels and bodies in those scenarios are the
	// MODEL's and are steered by the prompt, so they are typed there rather than
	// written down here — a table of things a model happened to say would be a
	// table of coincidences.
	"questionChipTail": {
		screen: " · alt+a",
		source: "alt+a",
		why: "the status line's chip, which is the one thing that is always there while anything is " +
			"waiting — reachable from home, a room and every other page. IT IS THE TAIL AND NOT THE " +
			"WHOLE CHIP, as the name says: the chip leads with the QUESTION'S OWN HEAD now " +
			"(`? delete the build directory? · alt+a`, question.go's questionSegment) and says " +
			"`1 question` only where there is no head to show and `3 questions` where there are " +
			"several. This row waited for the word `question` through all of that, which is the same " +
			"rot as the bracketed keys beside it (#998)",
	},
	"questionLaterKeyWord": {
		key:    "esc",
		screen: "later",
		why: "`esc` IS LATER AND NOT CANCEL. The consent block spelled it `cancel` for a year and " +
			"cancel meant deny; the block is not modal any more, so there is a way out that neither " +
			"answers nor traps, and the word may not say cancelled",
	},
	"questionTakeThePickWord": {
		key:    "enter",
		screen: "take it",
		why:    "`enter` is offered ONLY where the asker named a pick — the emptiness law on a key",
	},
	"questionOpenKeyWord": {
		key:    "o",
		screen: "open full",
		why: "the key to the page, drawn only where opening would show more than the block already " +
			"does; a page that says what the row said is a page nobody should be sent to. It was " +
			"`[o] open it` here for a wave after the surface stopped bracketing its keys, which is " +
			"exactly the rot this gate exists for",
	},
	"questionChangeKeyWord": {
		key:    "c",
		screen: "change",
		why: "taking an answer with words, on every form but the confirmation — whose two answers ARE " +
			"the question and have no third reading a sentence could add",
	},
	"questionUndoKeyWord": {
		key:    "u",
		screen: "undo",
		why: "the way back on a ratify row. The ladder's third rung acts FIRST and tells you after, so " +
			"the undo is the whole of the bargain",
	},
	"questionTickKeyWord": {
		key:    "space",
		screen: "tick it",
		why: "the checklist's own verb. A shape whose key is given up for width is a shape a person " +
			"cannot discover is tickable, which is an ordinary list",
	},
	"questionBlankKeyWord": {
		key:    "tab",
		screen: "next blank",
		why:    "how a sentence with holes in it is walked through — the blanks shape's own verb, ranked with the answers so a hundred-column foot keeps it",
	},
	"questionPairAWord": {
		key:    "a",
		screen: "the first",
		why:    "this-or-this, once per row: the left side",
	},
	"questionPairBWord": {
		key:    "b",
		screen: "the second",
		why:    "and the right one",
	},
	"questionMoveItWord": {
		key:    "←→",
		screen: "move it",
		why: "the arrows on a dial. They are a SECOND row in the key table rather than a second word " +
			"on the first, because `←→ pick` walks a confirmation's cursor and this changes the answer",
	},
	"questionOwnRuleWord": {
		screen: "your rule",
		why: "a countdown running because of something this project was told to do says so on the row. " +
			"Never a hidden rule (DESIGN.md's RULES ARE OFFERED, VISIBLE, FORGETTABLE)",
	},
	"questionReceiptWord": {
		screen: tokens.GlyphSettled + " ",
		source: tokens.GlyphSettled,
		pkg:    tokensPkg,
		why: "THE ANSWER IS THE RECORD: a dim line stays exactly where the question was, because the " +
			"transcript is what happened and `you were asked and said this` is part of it. It " +
			"opened with the word `decided` until the owner's after-you-answer ruling " +
			"(2026-09-11) made it the vocabulary's settled mark and the decision's own " +
			"sentence — `✓ <head> → <answer> · you · 14:02` — so the mark is the needle and it " +
			"is spelled where every mark on this surface is spelled",
	},
	"questionReceiptYouWord": {
		screen: " · you · ",
		source: "you",
		pkg:    "internal/session",
		why: "the record is read by the person who gave it and nobody calls themselves the person. It " +
			"is the ENGINE's word ([session.decidedByWord]) because the row above the box and the line " +
			"in decisions.jsonl have to be one sentence, so the gate looks where it is written",
	},
	"questionOtherWindowWord": {
		screen: "another window",
		pkg:    "internal/session",
		why: "FIRST ANSWER WINS. A window that did not take the key may not write `you` on the receipt " +
			"for it",
	},
	"questionDecidedByRuleWord": {
		screen: "codeaf, on your settings",
		pkg:    "internal/session",
		why: "and a decision the dial took while nobody was there says that instead — F41 was a hidden " +
			"timer recording `denied`, and this is the sentence that makes such a thing impossible to hide",
	},
	"questionWithdrawnWord": {
		screen: " — no longer needed · ",
		why: "WITHDRAWN, WITH A REASON, and never the word cancelled: what a person experiences is the " +
			"thing no longer needing them",
	},
	"questionWithdrawnMark": {
		screen: "⊘",
		pkg:    tokensPkg,
		why:    "the mark on that line, from the vocabulary's plain floor, which is the tier this suite pins",
	},
	"questionSettledMark": {
		screen: "✓",
		pkg:    tokensPkg,
		why: "the ratify line wears the SETTLED mark and not the attention one: nothing waits on it, so " +
			"a `?` there would be the surface asking for something it has already had",
	},
	"questionPageDetailWord": {
		key:    "\u2192",
		screen: "detail",
		why: "the page's own key, and the one word that says it is the page and not the panel: two " +
			"panes, and `\u2192` hands the arrows to the evidence beside the list. The page used to " +
			"owe a `\u2039 back` crumb and to name the asker's answer `my pick`; it draws neither now " +
			"(the recommendation wears `\u25c6 recommended` on its own row, in every view)",
	},
	"questionPageScrollWord": {
		screen: "back to the answers",
		why: "and the other half of that: while the arrows are the evidence pane's, `\u2190` gives them " +
			"back to the list. A page that took the arrows and never said how to get them back is a " +
			"page a person is stuck in",
	},
	"questionRoomWaitsWord": {
		screen: "the turn waits on it",
		why:    "what is stopped on this decision — said with what is NOT stopped, or it reads as everything",
	},
	"questionRoomNoPickWord": {
		screen: "nothing chosen yet",
		why: "the emptiness law in the foot: no pick, no `enter →` line. IT IS READ AS AN ABSENCE " +
			"NOW: since #789 the page opens where the block's pointer stood, so a page with answers " +
			"on it always has one to send and this sentence belongs to the `something else…` row, " +
			"which carries no key. The room scenario waited for it on a page that had just opened " +
			"on `1 postgres` — the same rot as the bracketed keys, one law over (#998)",
	},
	"questionFilledWord": {
		screen: "enter when it reads right",
		why: "the same slot on a shape rather than a list. There is nothing to choose on a dial or a " +
			"row of holes, and `nothing chosen yet` would describe a decision nobody is being asked to make",
	},
	"questionAnsweringWord": {
		screen: "answering ",
		why:    "the foot of a room with an answer composed on it: what pressing enter would send",
	},
	"questionNotedWord": {
		screen: " noted",
		why:    "how many comments are attached to the answer being composed",
	},
	"questionCommentPromptWord": {
		screen: "say what you think about this one, then enter",
		why:    "the prompt `c` opens under whatever it is annotating",
	},
	"questionAskBackPromptWord": {
		screen: "ask it one thing about this answer, then enter",
		why: "ONE exchange per option is the bound the design sets, and the row says so rather than " +
			"letting a person discover it by being refused",
	},
	"questionWouldSwitchWord": {
		screen: "would switch if ",
		why: "the most useful line on the page: what would change the asker's mind, which is usually " +
			"exactly what somebody who disagrees with the pick disagrees with",
	},
	"questionCompareOnlyWord": {
		screen: "only what differs is here",
		why:    "the compare table, built on the asker's own dimensions rather than on anything invented",
	},
	"questionTabKeyWord": {
		key:    "←→",
		screen: "question",
		why: "SEVERAL QUESTIONS FROM ONE STEP ARE ONE PANEL: the key that moves between them is the one " +
			"thing a set's bottom edge says that a panel of one does not (questionset.go)",
	},
	"questionNotAnsweredWord": {
		screen: "not answered — ← to go back",
		why: "the review names a question with nothing held for it and where it is answered, rather than " +
			"sending a pick nobody pressed",
	},
	"questionSetSendWord": {
		screen: "send all 2",
		source: "send all ",
		why:    "the review's one answer: every held answer, in tab order, through the one door in one command",
	},
	"questionGroupAllowWord": {
		screen: "allow all 4",
		source: "allow all ",
		why: "PERMISSIONS FROM ONE STEP ARE ONE FRAME: approving all of them is one answer on it, with " +
			"the count of what it approves (questionset.go)",
	},
	"questionGroupApartWord": {
		screen: "one by one",
		why:    "the frame's way back to answering each permission on its own, which opens them as tabs",
	},
	"questionGroupSafeWord": {
		screen: "safe answer",
		why: "THE FRAME NAMES ITS WAY OUT: `deny all` is the answer that loses nothing on every " +
			"grouped permission — the frame forms only where every member has one — so the row says " +
			"so wherever the pointer is standing. Four ordinary reads open on `allow all` now that " +
			"the gate grades them (#953), which is exactly when a person needs the refusal named, " +
			"and this line is what would catch the mark going missing (questionpanel.go's " +
			"questionSafeWord). It was once read as proof of the pointer's own place; the pointer " +
			"is the tui3 suite's to hold, and the deny-first reading of it expired with the grading",
	},
	"consentOldOfferWord": {
		key:    "enter",
		screen: "take it",
		pkg:    tui3Pkg,
		why: "the keys the panel writes into its bottom edge, which is how a screen says a question " +
			"is up and waiting on a person. It was `allow? [1] allow once` — the approval gate's own " +
			"offer row — until the questions wave gave every question one frame, one key table and one " +
			"spelling for a key (owner ruling 2026-09-11, hints pick A)",
	},
	"consentOldCancelWord": {
		key:    "esc",
		screen: "cancel",
		source: " cancel",
		why: "and the word that block spells for `esc`. THE QUESTIONS WAVE RETIRED IT — `esc` is later " +
			"and cancels nothing — so this row standing is the migration's own ledger, read off a screen",
	},

	"autonomyHeadWord": {
		screen: "questions while you are away",
		why:    "the head of the `/autonomy` sheet: this project's rules, in a person's own words",
	},
	"autonomyUsageWord": {
		screen: "/autonomy <kind> ask · recommend [duration] · decide",
		why: "the sheet's foot and the only place it names a door. An earlier draft put `· change` on " +
			"every row, which is a word with no key behind it",
	},
	"autonomyAlwaysWord": {
		screen: "destructive always asks",
		why: "the row no rule may cover, stated on the sheet rather than discovered by being refused — " +
			"stop.go's law widened to every question of that shape",
	},
	"headlessAskedWord": {
		screen: "(default · nobody to ask)",
		pkg:    "internal/session",
		why: "the HEADLESS law: with nobody to ask the policy applies AND IS PRINTED. A run that took a " +
			"default silently is a run whose decision nobody can find afterwards",
	},

	"setupNotConnectedNote": {
		screen: "openrouter is not connected",
		why: "the dim line the conversation says after esc, which is the other half of a front door: " +
			"a person who declined is told the next direct road rather than left on an empty screen",
	},
}

// say is how the tmux suite reaches a literal, and THE ONLY WAY IT MAY.
//
// A needle typed straight into the suite is a needle this file's gate cannot
// see, which is the whole defect #184 was: forty strings nobody could enumerate.
// So the suite asks for words by name and this fails the test on a name that is
// not in the table.
func say(t *testing.T, name string) string {
	t.Helper()
	word, ok := tuiWords[name]
	if !ok {
		t.Fatalf("no word named %q in tuiWords — add it to tuiwords_test.go rather than typing the literal here", name)
	}
	return word.spelling()
}

// keyedWord is THE ONE PLACE THIS SUITE PUTS A KEY BESIDE ITS WORD, and it is
// the other door beside [say].
//
// A KEY'S WORD COMES FROM THE TABLE; A SCENARIO'S OWN ANSWER DOES NOT. The line
// that asks `delete the build directory?` offers `1 delete it` because the
// scenario told the model to offer it, so `delete it` is the scenario's word and
// belongs in the scenario — but the `1 ` in front of it is the PRODUCT's key
// grammar, and a scenario that pasted that punctuation is a scenario waiting for
// a screen the surface may already have stopped drawing. It did: #933 took the
// brackets off every key and nine needles in questions_e2e_test.go went on
// waiting for `[1] delete it` for a fortnight of green untagged runs (#998).
//
// The gap is one space, which is what internal/tui3 composes on the answers row
// (question.go's questionRowOffer), on the dim key row under it
// (questionkeys.go's questionKeyWords) and on a landing's three columns. The
// panel's stacked rows use two and are a different grammar; a needle about those
// says so with its own spelling.
func keyedWord(key, word string) string { return key + keyedGap + word }

// keyedGap is that one space, named because three readers share it.
const keyedGap = " "

// spelling is the whole needle: the word on a plain row, and the key beside it
// on a row that names one.
func (w tuiWord) spelling() string {
	if w.key == "" {
		return w.screen
	}
	return keyedWord(w.key, w.screen)
}

// grep is what must still stand in the sources for this word to be honest.
func (w tuiWord) grep() string {
	if w.source != "" {
		return w.source
	}
	return w.screen
}

// where is the package directory that owes the spelling.
func (w tuiWord) where() string {
	if w.pkg != "" {
		return w.pkg
	}
	return tui3Pkg
}

// TestEveryWordTheTmuxSuiteWaitsForStillStandsInTheSurface is the gate.
//
// It reads the non-test sources of every package the table names and asserts
// each word is still spelled there. IT IS DELIBERATELY A SUBSTRING SEARCH OVER
// THE FILE BYTES rather than a parse of the constants: a sentence composed at
// the draw out of two constants passes, a sentence somebody deleted does not,
// and that is exactly the line this gate needs to hold. It is not asserting that
// the string reaches the screen — only the tmux suite can say that — it is
// asserting that the tmux suite is still waiting for words the product knows.
func TestEveryWordTheTmuxSuiteWaitsForStillStandsInTheSurface(t *testing.T) {
	root := moduleRoot(t)
	sources := map[string]string{}
	for name, word := range tuiWords {
		if name != strings.TrimSpace(name) || name == "" {
			t.Errorf("the table holds an unusable name %q", name)
			continue
		}
		dir := word.where()
		if _, ok := sources[dir]; !ok {
			sources[dir] = readPackageSources(t, filepath.Join(root, dir))
		}
		if !strings.Contains(sources[dir], word.grep()) {
			t.Errorf("the tmux suite waits for %s = %q, and %s no longer spells %q anywhere.\n"+
				"It is there because: %s.\n"+
				"Either the surface lost a sentence it should still say, or the wave that respelled it "+
				"owes this table the new words and internal/e2e/tui_e2e_test.go the new assertion.",
				name, word.spelling(), dir, word.grep(), word.why)
		}
	}
}

// TestEveryWordInTheTableIsWaitedForBySomething is the other half of the gate.
//
// A table that only ever grows is a table with dead rows in it, and a dead row
// is a claim about the surface nobody is testing. So the suite's own source is
// read back and every entry must be asked for by name somewhere in it. The read
// is of the FILES and not of the running suite, because the suite is behind a
// build tag this test is deliberately not behind: a gate that needed tmux and a
// model to run would be a gate that runs as rarely as the thing it guards.
func TestEveryWordInTheTableIsWaitedForBySomething(t *testing.T) {
	suite := suiteSources(t)
	for name, word := range tuiWords {
		if !strings.Contains(suite, `"`+name+`"`) {
			t.Errorf("nothing in the tmux suite asks for %s = %q any more (%s). "+
				"Delete the row, or wait for it.", name, word.spelling(), word.why)
		}
	}
}

// TestNoNeedleSpellsAKeyTheSurfaceStoppedSpelling is the THIRD half of the gate,
// and the one that closes the hole the other two left (issue #998).
//
// THE TABLE IS ONLY THE ONLY DOOR IF NOTHING WALKS PAST IT. #933 took the
// brackets off every key on this surface — `[1] delete it` became `1 delete it`
// — and the untagged gate stayed green through it, twice over:
//
//   - nine needles in questions_e2e_test.go were INLINE LITERALS, which
//     [TestEveryWordTheTmuxSuiteWaitsForStillStandsInTheSurface] cannot see at
//     all because it only reads the table; and
//   - eleven rows of the table spelled `[c] change` in `screen` while their
//     `source` said only `change`, so the gate checked the half that had not
//     moved and never looked at the half that had.
//
// TestQuestionsE2E was 14 of 18 red for a fortnight of green pull requests on
// that. So this walks the suite's own source with go/ast and refuses, in a
// NEEDLE POSITION — an argument that is compared against a real screen — any
// literal that spells a key:
//
//   - a key in square brackets, which nothing on this surface draws any more; and
//   - an answer's own key and word pasted together (`1 delete it`), which is the
//     product's grammar and belongs in [keyedWord].
//
// EVERY OTHER LITERAL IN A NEEDLE POSITION IS LEFT ALONE, and that is the law
// and not a gap in it: the file header beside this one says why a sentence the
// MODEL wrote is typed into the scenario that steered it, and a gate that
// demanded a table row for `delete the build directory?` would be demanding a
// source of truth for a coincidence.
func TestNoNeedleSpellsAKeyTheSurfaceStoppedSpelling(t *testing.T) {
	// THE TABLE IS READ AS DATA AND NOT AS SOURCE. Its rows are in this process,
	// so a bracketed spelling is caught by looking at the map rather than by
	// parsing the file that holds it — which also keeps this test's own patterns
	// out of its own reach.
	for name, word := range tuiWords {
		if bracketedKey.MatchString(word.spelling()) {
			t.Errorf("the table spells %s as %q, and this surface stopped bracketing its keys in #933. "+
				"Name the key in the row's `key` field and leave the word in `screen`, so [keyedWord] "+
				"spells the gap once.\nIt is there because: %s.", name, word.spelling(), word.why)
		}
	}
	for file, calls := range suiteNeedleCalls(t) {
		for _, call := range calls {
			for _, needle := range call.needles {
				lit, ok := needle.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				switch {
				case bracketedKey.MatchString(text):
					t.Errorf("%s:%d: %s waits for %q, and this surface stopped bracketing its keys in #933. "+
						"Say it with keyedWord(<key>, <word>), or with say(t, …) where the word is the product's.",
						file, call.line, call.name, text)
				case pastedAnswer.MatchString(text):
					t.Errorf("%s:%d: %s waits for %q, which pastes an answer's key onto its word. "+
						"The key grammar is the product's and moves without this file: say it with "+
						"keyedWord(%q, %q).",
						file, call.line, call.name, text, text[:1], text[2:])
				}
			}
		}
	}
}

// bracketedKey is a key of the question grammar in square brackets — the
// spelling every form on this surface used until #933 and none uses now.
var bracketedKey = regexp.MustCompile(`\[(?:[0-9A-Za-z=?]|space|tab|enter|esc|shift\+\S+|[` + "←→↑↓" + `]+)\]`)

// pastedAnswer is a numbered answer's key with its word behind it, which the
// suite may only compose through [keyedWord].
//
// IT IS THE DIGITS AND NOT THE LETTERS. `1`–`9` are the product's own keys on
// every list of answers, so a needle that opens with one is always the grammar;
// a needle that opens with a letter and a space is nearly always a sentence
// (`a shape has nothing to choose`), and a law that refused those would be a law
// people turn off.
var pastedAnswer = regexp.MustCompile(`^[1-9] \S`)

// needleCall is one call in the suite that compares something against a real
// screen, with the arguments that are the comparison.
type needleCall struct {
	name    string
	line    int
	needles []ast.Expr
}

// screenReaders is every door in this package through which a string is held up
// against a terminal, and WHICH OF ITS ARGUMENTS ARE THE STRING.
//
// The positions are named rather than "every literal in the call" because the
// last argument of the three assertions is a MOMENT — prose naming what is being
// proved, written for whoever reads the failure — and a law that read those as
// needles would refuse `a shape has nothing to choose, so the foot says when to
// press enter instead`, which is exactly the kind of false red that gets a gate
// deleted.
var screenReaders = map[string]struct {
	from int // the first argument that is held against the screen
	tail int // how many trailing arguments are not
}{
	"awaitQuestion": {from: 2},
	"screenSays":    {from: 2, tail: 1},
	"screenSilent":  {from: 2, tail: 1},
	"screenEchoes":  {from: 2, tail: 1},
	"waitFor":       {from: 1},
	"statesAwait":   {from: 2},
}

// suiteNeedleCalls parses every test file in this package but this one and
// returns each call on a screen-reading door with its needle arguments.
//
// IT PARSES RATHER THAN GREPS for the reason [readPackageSources] does: a
// bracketed key inside a comment about the wave that removed brackets is history
// and not a needle, and a byte-wise gate cannot tell the two apart.
func suiteNeedleCalls(t *testing.T) map[string][]needleCall {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "internal", "e2e")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the tmux suite is not where this gate expects it: %v", err)
	}
	found := map[string][]needleCall{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") || name == tuiWordsFile {
			continue
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			door := callName(call.Fun)
			where, ok := screenReaders[door]
			if !ok || call.Ellipsis.IsValid() {
				return true
			}
			if len(call.Args) <= where.from+where.tail {
				return true
			}
			found[name] = append(found[name], needleCall{
				name:    door,
				line:    set.Position(call.Pos()).Line,
				needles: call.Args[where.from : len(call.Args)-where.tail],
			})
			return true
		})
	}
	if len(found) == 0 {
		t.Fatalf("%s holds no scenario that reads a screen, which cannot be right", dir)
	}
	return found
}

// callName is the last name in a call's function expression: `screenSays` for a
// plain call and `waitFor` for `r.waitFor`, which are the two shapes this suite
// writes.
func callName(fun ast.Expr) string {
	switch at := fun.(type) {
	case *ast.Ident:
		return at.Name
	case *ast.SelectorExpr:
		return at.Sel.Name
	}
	return ""
}

// suiteSources is every test file in this package except this one, joined — the
// tmux suite as it stands today.
//
// IT IS THE WHOLE PACKAGE AND NOT ONE FILE because the suite outgrew one file.
// [say] is a package-level door and any file beside it may wait through it, so a
// gate that named tui_e2e_test.go would answer a question nobody asked: it would
// call a row dead the day its waiter was written next door, and it would push
// scenarios into the file the gate happens to read rather than the file they
// belong in.
//
// THIS FILE IS THE ONE EXCLUSION, and it is not an exception so much as the
// point: every name in the table is spelled here, so a gate that read its own
// source would find every row waited for by the table itself and go green on
// exactly the rot it exists to catch.
func suiteSources(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "internal", "e2e")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the tmux suite is not where this gate expects it: %v", err)
	}
	var b strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") || name == tuiWordsFile {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		t.Fatalf("%s holds no suite beside this gate", dir)
	}
	return b.String()
}

// tuiWordsFile is this file's own name, which [suiteSources] skips.
const tuiWordsFile = "tuiwords_test.go"

// readPackageSources is every STRING LITERAL in every non-test .go file of one
// directory, joined — the words the package can actually put on a screen.
//
// IT IS THE LITERALS AND NOT THE FILE BYTES, and the difference is the whole
// gate. This codebase comments heavily and quotes its own sentences inside those
// comments, so a search over the raw file passes on a constant somebody
// respelled while a comment above it still shows the old wording — which was
// measured: renaming `→ verbs` and leaving its own doc comment alone kept a
// byte-wise gate perfectly green. Parsing costs a few milliseconds and answers
// the question that was actually asked.
//
// TEST FILES ARE SKIPPED ON PURPOSE. A string that survives only in the unit
// test that pinned it is a string the product has already stopped drawing, and a
// gate that accepted it would go green on exactly the change it exists to catch.
func readPackageSources(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var b strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// A literal that will not unquote is a literal this gate cannot read,
			// and skipping it can only make the gate stricter.
			if text, err := strconv.Unquote(lit.Value); err == nil {
				b.WriteString(text)
				b.WriteString("\n")
			}
			return true
		})
	}
	if b.Len() == 0 {
		t.Fatalf("%s holds no non-test sources at all", dir)
	}
	return b.String()
}

// moduleRoot walks up from this test's own directory to the module root.
//
// It is spelled here rather than borrowed from tmux_test.go's [repoRoot] because
// that file is behind the `e2e` build tag and this gate is deliberately not: a
// guard that could only be built with the thing it guards would never run in the
// pull-request gate, which is the one place it has to.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
