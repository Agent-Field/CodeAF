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
	"strconv"
	"strings"
	"testing"
)

// tuiWord is one literal the tmux suite waits for on a real screen.
//
// THE SCREEN AND THE SOURCE ARE TWO DIFFERENT FACTS and the table carries both.
// Most sentences this surface draws are one constant and the two are the same
// string; some are composed at the draw — `type to search or start something new
// · ↑↓ pick · enter open · tab next place` is two constants joined — and a gate
// that grepped for the whole painted line would fail on a sentence the code
// spells perfectly well in two halves. So `source` is what must stand in the
// sources, and it defaults to `screen` when the two agree.
type tuiWord struct {
	// screen is EXACTLY what the product draws, and what the suite waits for.
	screen string
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
	"stopDetachedWord": {
		screen: "detached — the turn was let go of and nothing is waiting for it",
		why:    "the note a turn let go of at the bound leaves in the conversation",
	},

	// ── home at rest ─────────────────────────────────────────────────────────
	"homeFootWord": {
		screen: "type to search or start something new · ↑↓ pick · enter open",
		why:    "home's resting foot: the three verbs this screen offers and stays at three",
	},
	"placeHintTail": {
		screen: "tab next place",
		why:    "every place's foot ends with the one key that leaves it",
	},
	"placeRestWord": {
		screen: "say what you want done",
		why:    "the prompt in the box at the foot of a resting place",
	},
	"homeDoorWord": {
		screen: "space space home",
		why:    "the gesture back to home, named on the conversation's own rule",
	},
	"microcopy": {
		screen: "/ commands",
		why:    "the other half of that rule, which is how a conversation is told from home",
	},
	"homeGoneShort": {
		screen: "folder gone",
		why:    "a row whose project folder is not there any more says so before enter is pressed",
	},

	// ── the switcher home became ─────────────────────────────────────────────
	"switcherSectionWord": {
		screen: "what wants you first",
		why:    "the one section line over the flat ranked list",
	},
	"switcherGroupWord": {
		screen: "alt+g group by project",
		why:    "the key that turns the flat list into one block per project",
	},
	"switcherQuietWord": {
		screen: "alt+q hide the quiet ones",
		why:    "the key that drops everything that is neither asking nor moving",
	},
	"switcherQuietSince": {
		screen: "quiet since ",
		why:    "the clause on the fold at the foot, naming when the hidden rows went quiet",
	},
	"foldMoreWord": {
		screen: " more",
		why:    "the fold at the foot counts what it stands over — `▸ 5 more, quiet since 6d`",
		pkg:    tui3Pkg,
	},
	// The two fold hints are the CLAUSE THE ROW OWNS and not the whole drawn
	// line, because the router puts its own `tab next place` in front of the
	// `esc` clause on the way to the screen (pages.go's placeTailed) — so what a
	// person reads is `enter or → show them · tab next place · esc close`, and a
	// needle carrying the row's clause is the half that belongs to the row.
	"homeFoldOpenHint": {
		screen: "enter or → show them",
		why:    "the hint under the box while the cursor stands on a SHUT fold — the suite's oracle for where the cursor is",
	},
	"homeFoldShutHint": {
		screen: "enter or ← fold them away",
		why:    "the same hint with the fold standing open",
	},
	"switcherSinceLeft": {
		screen: "since you left",
		why:    "the heading over what happened while nobody was looking",
	},
	"homeVerbsWord": {
		screen: "→ verbs",
		why:    "the card's last line, which names the key and the words and never the letters",
	},
	"homeHereWord": {
		screen: "here",
		why:    "the narrow home card keeps the current conversation door beside its address",
	},
	"homeFactsActive": {
		screen: "last active ",
		why:    "the card's facts line — the arithmetic that survived the sixteen bands becoming five",
	},
	"homeStartWord": {
		screen: "start a new conversation",
		why:    "the action row under anything typed at home",
	},

	// ── asking from home ─────────────────────────────────────────────────────
	"homeAskHereWord": {
		screen: "ask here",
		why:    "what ctrl+enter starts, drawn as the row's own heading over the pane",
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
		screen: "1 yes · 2 change when or where · 0 no",
		source: "change when or where",
		why: "the answers a ONE-OFF REMINDER's card offers, spelled in full under the box at every width. " +
			"There are three of them and not four: a reminder has no `3 just once` to give, and since #189 " +
			"the line is built from the chips the card drew rather than typed out, so it cannot name one. " +
			"The source is the middle answer's own constant, because the sentence is no longer a literal anywhere",
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
	"standYesWord": {
		screen: "yes, set it up",
		why:    "the first chip on a standing card, and half of the settled card's `yes, set it up · set up`",
	},
	"standSetWord": {
		screen: "set up",
		why:    "the other half — what a settled card keeps as its verdict",
	},

	// ── what stands, and what it costs ───────────────────────────────────────
	"homeKeepingWord": {
		screen: " keeping an eye on ",
		why:    "the status line's own segment while something stands, and a door onto the standing place",
	},
	"homeWatchLabel": {
		screen: "keeping watch",
		why:    "/status's line about whether anything checks the world with no window open",
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
	// join, which is the shape `standYesWord` and `standSetWord` already have.
	// What varies is not a needle: the machine that went quiet is whatever this
	// run pinned, and the lane a rescue would go to is whatever the frontier
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
		screen: "all lanes slow",
		why:    "every reachable machine is believed slow, so there is nowhere better to be",
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
		why:    "a lane said no and the answer is already on its way somewhere else",
	},
	"laneRefusedTail": {
		screen: " refused",
		why: "the retraction: the rescue this row was promising has itself been refused, " +
			"so the promise comes off and the fact is what is left",
	},
	"laneSlowTrying": {
		screen: " · slow · trying ",
		why: "the same row about a lane that was merely LATE — waited for as an ABSENCE " +
			"by the refusal subtest, which is how a wrong word is caught rather than a missing one",
	},

	// ── a question answered from another window ──────────────────────────────
	"consentAskWord": {
		screen: "allow? ",
		why:    "a conversation stopped on a permission question",
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
		screen: "[a] accept",
		source: "accept",
		pkg:    "internal/session",
		why: "the answer a person presses. The key is the surface's and the WORD is the ask's, so a conflict " +
			"card can read `[a] resolve it` on the same column without a second constant anywhere",
	},
	"settleNotRight": {
		screen: "[n] not right",
		source: "not right",
		pkg:    "internal/session",
		why:    "the answer that says checked work is not finished, and drives the refused ending",
	},
	"settleTellIt": {
		screen: "[s] tell it",
		source: " tell it",
		why: "the third column on every one of these cards: say something to the task rather than answering. " +
			"It must be drawn, because a person with something to say who finds only yes and no presses one of them",
	},
	"settleTookLine": {
		screen: "you took this as done",
		why:    "the receipt the card wears once the accept has been spent, which is how the pane proves the key landed",
	},
	"settleNotRightLine": {
		screen: "you said it is not finished",
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
	"landingKeysWord": {
		screen: "esc interrupts · ctrl+c twice quits",
		why:    "the notice a conversation greets on, and what a window that RESUMED an earlier one draws instead of home",
	},
	"tasksEnterInsideWord": {
		screen: "enter go inside it",
		why: "the roster's other door, over work no window is holding any more — it is the one that opens the " +
			"record card, which is the mode `space` pages and the door home had to give the key back to",
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

	// ── the seat a crew older than it never wrote ────────────────────────────
	//
	// The two halves of one line, and it is the ENGINE'S sentence: the surface
	// says it in the thread and every headless door prints it under the models
	// line, out of one composer (internal/config's Seat.Notice), so the gate
	// looks for it where it is spelled rather than in the surface that relays it.
	"inheritedSeatObservation": {
		screen: "your crew was set before the work seat existed",
		source: "your crew was set before the ",
		pkg:    "internal/config",
		why:    "the observation half: a profile older than the seat is told so, once, when work starts on it",
	},
	"inheritedSeatPromise": {
		screen: "it is running on your small work seat's model",
		source: "it is running on your ",
		pkg:    "internal/config",
		why: "the promise half, naming the row the work is actually on. It stops at the row rather " +
			"than at `until you pick a crew again` because a transcript line is cut to make room for " +
			"the task rail, and the sentence is longer than an ordinary window minus that column",
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
	// which is why several rows here are composed at the draw and name a `source`
	// in internal/session: a card that read `[a] resolve it` on the same column
	// where another reads `[a] accept` is one constant in each place and not two
	// spellings of one answer.
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
		screen: "[a] accept · [n] not right · [s] tell it",
		source: " tell it",
		why: "THE WHOLE ANSWERS ROW, in the one order every card draws it. It is waited for as one string " +
			"because three separate searches would pass on a card that drew the columns on two rows, or in " +
			"the other order, or without the third — and the third is the one the ruling is emphatic about",
	},
	"settleHandRow": {
		screen: "[d] let aforge decide this one",
		source: " let aforge decide this one",
		why: "the one-time hand-over, drawn dimmer beside the answers. It replaced `decide these for me`, " +
			"which was a standing preference disguised as an answer and changed a setting on the way past",
	},
	"settleConflictAnswers": {
		screen: "[a] resolve it · [n] drop it",
		source: "resolve it",
		pkg:    "internal/session",
		why: "a conflict's own two verbs on the same two columns. A conflict's yes is NOT an accept: it " +
			"spends one more merge round, which is why the word is the ask's and not the card's",
	},
	"settleConflictNo": {
		screen: "[n] drop it",
		source: "drop it",
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
		screen: "aforge is deciding",
		why: "the auto-settle floor's own row. A card with no chips MUST say why it has none — that defect, " +
			"a card with no choices and no explanation, is what the whole wave exists to close",
	},
	"taskTakeItBackWord": {
		screen: "[t] take it back",
		source: " take it back",
		why: "the way back on that row, and the last thing it gives up at a narrow width: the reason is on " +
			"the rail and in the record, and this key is only here",
	},

	// ── the front door, on a machine that has never run aforge ───────────────
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
	return word.screen
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
				name, word.screen, dir, word.grep(), word.why)
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
				"Delete the row, or wait for it.", name, word.screen, word.why)
		}
	}
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
