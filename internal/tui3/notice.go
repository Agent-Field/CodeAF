package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
)

// THE NOTICES: telling a person one thing at the moment it becomes true.
//
// A surface learns you by what you have already done, and this file is where it
// keeps what it has told you. Two kinds of thing live here at launch:
//
//   - EARNED HINTS. One dim line beside the box — `/files finds files codeaf
//     wrote for you` — on the row directly above the rule over home's box, and
//     on the lowest rung of a conversation's keys row. A tip RETIRES FOR GOOD
//     the first time the gesture it teaches is used (the files place opened),
//     or after it has been shown [noticeShownDefault] times without being
//     acted on. A hint that stays
//     up after you have learned the key is a cheatsheet, and a cheatsheet is
//     read once and never again (render.go's [app.hintWord] says the same about
//     static keys).
//   - NEWS. One dim transcript line, said once, the first time this binary runs
//     after its build changed — the place a shipped feature announces itself.
//     The channel exists and is empty; a wave that ships something registers a
//     row with [notice.news] set and writes nothing else.
//
// ONE TABLE, TWO BOXES, TWO RULES. Until 2026-09-22 a hint row named which box
// it could draw beside; the owner ruled that there is ONE set of tips, and the
// two boxes say them by rules of their own, because a conversation is a screen
// you sit in and home is a screen you pass through. A CONVERSATION RANKS: the
// first eligible row in the table's order takes the foot, so a tip that has
// just become true is said at once — `/compact summarizes the conversation
// now` when the window crosses half, not forty minutes later — and
// [noticeGap] keeps a busy first session from reading as a slideshow
// ([noticeBoard.rank]). HOME TAKES TURNS: every tip that is true gets one, the
// row moving on with every visit and every [hintEvery] at rest
// ([noticeBoard.rotate]). Both go through [noticeBoard.pick].
//
// THE TABLE BELOW IS THE ONE PLACE A NOTICE IS WRITTEN DOWN, the way commands.go
// is the one place a command is. [checkNotices] runs over it at init and fails
// the build on a duplicate id, an empty line, a retire event nobody defined, or
// a word from the machinery vocabulary this surface does not use in front of a
// person.
//
// EVENTS, NOT POLLING. The surface never asks every frame whether a hint has
// become relevant; it is TOLD, at the small set of seams that prove a gesture
// happened ([app.noticeEvent]) — a task started, the task page opened, a turn
// ended. An event retires every notice that names it, records that it happened
// (which is what arms the hints that wait on it), and re-decides what each slot
// holds. The deciding is [noticeBoard.pick], and it is pure: it reads the board
// and the candidates and touches nothing else, so the arbitration has tests that
// need no frame.
//
// WHAT IS REMEMBERED IS PER PROFILE, in one small file beside config.json
// (notice_ledger.go): how many times each notice has been shown, when it
// retired, and which build the news channel last saw. A missing or unreadable
// ledger is an empty one — a person is never told their hints file is corrupt,
// because the worst case is a tip they have seen before.

// noticeSlot is where a notice may draw. The type is an enum rather than a bool
// so a later wave can add one without touching the rows that exist — a new
// slot lands as one constant above [noticeSlots] and one case in
// [app.noticeShow].
type noticeSlot uint8

const (
	// slotHint is the lowest rung of a conversation's keys row at the foot
	// (render.go's [app.footHint] draws it), taken whenever the frame is quiet
	// enough for a tip to be read over an idle box ([app.noticeHint]).
	slotHint noticeSlot = iota
	// slotNote is one calm transcript line through [feed.note]. It is reserved
	// for news: a hint belongs beside the box it is about, and a line in the
	// conversation is for something that is true once.
	slotNote
	// slotHome is the same row over home's box (pages.go's [placeFrameWithBar]),
	// and it is the hint slot's twin on the other box a person types into: the
	// same table, the same ledger, the same retirement — and a different clock,
	// because home has no turns. Every hint row draws in both, so a tip retired
	// by its gesture is retired on both boxes at once.
	slotHome
	// noticeSlots is how many there are. A new slot goes above this line.
	noticeSlots
)

// The events that prove a gesture happened. They are named constants beside the
// table so a retire rule cannot be spelled with a typo and silently never
// fire: [checkNotices] refuses a rule naming an event that is not in
// [noticeEvents], and notice_test.go proves every one of them is fired from a
// seam in this package.
const (
	// eventBoot is the surface coming up, after the replay and before the first
	// thing it says. It retires nothing; it is the moment the hints that wait on
	// facts about this directory get their first look.
	eventBoot = "boot"
	// eventTurnEnded is [app.settle]: a turn is over, however it ended.
	eventTurnEnded = "turn-ended"
	// eventTaskStarted is a task the engine accepted ([taskStartedMsg] with no
	// error).
	eventTaskStarted = "task-started"
	// eventTaskPageOpened is ctrl+. or /history actually raising the page
	// (place_tasks.go's [app.showTaskPlace]).
	eventTaskPageOpened = "task-page-opened"
	// eventMenuOpened is the command list opening under a typed "/" (app.go's
	// [app.syncLists]).
	eventMenuOpened = "menu-opened"
	// eventRewound is a rewind that landed, from either surface (rewind.go's
	// [app.rewindLand]).
	eventRewound = "rewound"
	// eventCopyEntered is copy mode freezing the viewport (copymode.go).
	eventCopyEntered = "copy-entered"
	// eventModelSwitched is the conversation's model changing by any door
	// (palette.go's [app.switchModel]).
	eventModelSwitched = "model-switched"
	// eventCompacted is a /compact that came back without an error.
	eventCompacted = "compacted"
	// eventFilesOpened is /files reached for, whether or not there was anything
	// to list — the gesture is the command, and a person who typed it knows it.
	eventFilesOpened = "files-opened"
	// eventResumeOpened is /resume reached for, on the same terms.
	eventResumeOpened = "resume-opened"
	// eventCostShown is /cost answered.
	eventCostShown = "cost-shown"
	// eventStandingOpened is the standing page raised or an order made — either
	// proves the person knows the word.
	eventStandingOpened = "standing-opened"
	// eventDeliverableMade is something written for the person: an export that
	// landed on disk.
	eventDeliverableMade = "deliverable-made"
	// eventAsked is a question sent through home's own door — `/ask`, or
	// `alt+enter` over home's box (homeexchange.go's [app.askHereWith]).
	eventAsked = "asked"
	// eventTaskTyped is `/task <brief>` reaching its command (taskcommand.go);
	// the task it starts fires [eventTaskStarted] on its own later.
	eventTaskTyped = "task-typed"
	// eventManualAsked is /manual reaching its command, bare or with a page or
	// a question (app.go).
	eventManualAsked = "manual-asked"
	// eventTabReopened is ctrl+shift+t bringing a closed tab back
	// (tabreopen.go).
	eventTabReopened = "tab-reopened"
	// eventAtOpened is the `@` completion list coming up under the box
	// (app.go's [app.syncLists]).
	eventAtOpened = "at-opened"
	// eventAttached is a file or picture put on the tray by path, or the
	// browser opened to choose one (attach.go, folderplace.go).
	eventAttached = "attached"
	// eventFolderPicked is the folder chooser raised from a conversation —
	// /folder itself, or the project word on the keys row (folderplace.go,
	// projectseam.go).
	eventFolderPicked = "folder-picked"
	// eventProjectSet is /project on home reaching a folder: a path after it
	// taken, or the browser it opens bare (projectcmd.go).
	eventProjectSet = "project-set"
	// eventModelListOpened is the model list raised, over a conversation or
	// over home's draft (palette.go, homedraft.go).
	eventModelListOpened = "model-list-opened"
	// eventCrewShown is /crew answered, bare or with a preset (crew.go).
	eventCrewShown = "crew-shown"
	// eventBudgetShown is /budget answered, bare or with a figure (budget.go).
	eventBudgetShown = "budget-shown"
	// eventSpendOpened is the spend place raised by any door (pages.go).
	eventSpendOpened = "spend-opened"
	// eventSteered is enter over a running answer steering it (steer.go).
	eventSteered = "steered"
	// eventQueued is ctrl+q holding a message for after the turn (followup.go).
	eventQueued = "queued"
	// eventChatStarted is the new-chat page raised by ctrl+t or the tab strip's
	// plus (chatstart.go).
	eventChatStarted = "chat-started"
	// eventPlaceJumped is alt+<digit> reaching a place (placekeys.go).
	eventPlaceJumped = "place-jumped"
	// eventRemembered is /remember reaching its command (memory.go).
	eventRemembered = "remembered"
	// eventSearchOpened is the search place raised by any door (pages.go).
	eventSearchOpened = "search-opened"
	// eventSubharnessOpened is /subharness reaching its command, bare or named
	// (app.go).
	eventSubharnessOpened = "subharness-opened"
	// eventConnectOpened is the connect panel reached for (connectpanel.go).
	eventConnectOpened = "connect-opened"
	// eventAutonomyAsked is /autonomy reaching its command, bare or with a
	// rule (autonomysheet.go).
	eventAutonomyAsked = "autonomy-asked"
	// eventHomeGesture is two spaces over an empty box opening home, from a
	// conversation or from a place (input.go, placekeys.go). It is the
	// gesture alone and not every way home: `/home` and a click on the tab
	// are doors a person already knows, and the tip is about the one they
	// cannot see.
	eventHomeGesture = "home-gesture"
)

// noticeEvents is every event there is, in one list, so the table check can
// refuse a retire rule that names a word nobody fires.
var noticeEvents = []string{
	eventBoot, eventTurnEnded, eventTaskStarted, eventTaskPageOpened,
	eventMenuOpened, eventRewound, eventCopyEntered, eventModelSwitched,
	eventCompacted, eventFilesOpened, eventResumeOpened, eventCostShown,
	eventStandingOpened, eventDeliverableMade,
	eventAsked, eventTaskTyped, eventManualAsked, eventTabReopened, eventAtOpened, eventAttached,
	eventFolderPicked, eventProjectSet, eventModelListOpened, eventCrewShown, eventBudgetShown,
	eventSpendOpened, eventSteered, eventQueued, eventChatStarted,
	eventPlaceJumped, eventRemembered, eventSearchOpened, eventSubharnessOpened,
	eventConnectOpened, eventAutonomyAsked, eventHomeGesture,
}

// notice is one thing the surface may tell a person, and the whole of the rule
// for when.
type notice struct {
	// id is the stable name the ledger files it under: kebab-case, and NEVER
	// REUSED once a build has shipped it, because a retired id is a promise that
	// a person has already been told this and does not want to be again.
	id   string
	slot noticeSlot
	// armed says whether the notice is relevant right now. It is asked at every
	// event and never between them, so it must be cheap and must read only what
	// the surface already holds — a hint whose arming fact would need a counter
	// plumbed through the session is a hint that does not belong in the table.
	armed func(*app) bool
	// text is the line, or say builds it when the line depends on state. The
	// grammar is the hint slot's: the key or command first, then what it does
	// (payload.go's [paintHint] lifts the chord to ink).
	text string
	say  func(*app) string
	// retire is the event that proves the gesture was used, after which the
	// notice never shows again on this profile. Empty for a notice that only
	// ages out.
	retire string
	// maxShown is how many showings the notice gets before it retires by
	// itself, whether or not the gesture was ever used; zero means
	// [noticeShownDefault]. A showing is one turn of a row's rotation, on
	// either box: a tip standing on home for an hour is one showing.
	maxShown int
	// news marks the what's-new channel: a row that is armed only on the first
	// launch after the binary's build changed, and shown once.
	news bool
}

// noticeShownDefault is how many showings a hint gets before it is taken as
// read. Six turns of a rotation, on either box, is one afternoon of a tip
// coming round: a tip seen that often and never acted on is a tip about
// something the person does not want, and the seventh showing would be the
// surface nagging.
const noticeShownDefault = 6

// noticeReadTime is how long a tip has to stand on a row somebody can see
// before that counts as a showing. Until 2026-09-22 every change of hands
// counted, and every road home is a change of hands — so an afternoon of
// stepping through home to check something else spent every tip on the ring
// in one-second flashes nobody read, and the owner's ledger closed the whole
// table before evening. Twenty seconds is longer than a bounce through home
// and shorter than any reading of a line: a tip that stood that long on a
// visible row was on a screen somebody was looking at.
const noticeReadTime = 20 * time.Second

// noticeGap is the fewest turns between one tip standing down on the
// CONVERSATION's row and a different one taking it. It is what keeps a busy
// first session from reading as a slideshow: three tips arming in three
// consecutive turns are shown one at a time, each with room to be read. Home's
// row is not on turns at all — it is on visits and a clock ([hintEvery]).
const noticeGap = 2

// hintEvery is how long a tip stands on a row before the next one takes it,
// while the row is left at rest: home at rest, or a conversation the person
// has gone quiet in. Two minutes is long enough to be read and short enough
// that a window left open over lunch has said a few things.
const hintEvery = 2 * time.Minute

// The arming thresholds, each named once so the manual page and the table
// cannot drift apart about when a hint appears.
const (
	// longAnswerRunes is what counts as a long answer — the size at which
	// somebody first wishes they had asked differently.
	longAnswerRunes = 1500
	// contextHintPct is the fill at which the compact hint arms: half the
	// window, well before the session compacts on its own.
	contextHintPct = 50
	// costHintUSD is the spend at which the cost hint arms — the first figure
	// on the status line that reads as money rather than as rounding.
	costHintUSD = 0.10
)

// The arming rules the table shares. A rule reads only what the surface
// already holds ([notice.armed] says why), and these are the three facts most
// rows want: nothing at all, a conversation that has been spoken to, and home's
// own door standing.
var (
	// ready is a tip that is true as soon as there is somebody to tell: on
	// home from the first minute, and in a conversation once it has had an
	// exchange. A fresh conversation's foot stays quiet until then, which is
	// the law the `/ shows every command` row kept when it was here — the
	// greeting is the first thing a person reads, not a tip.
	ready = func(a *app) bool { return a.turn >= 1 || a.at(pageHome) }
	// spoken is a conversation that has had at least one exchange: a tip about
	// steering or queueing over an answer means nothing before one has arrived.
	spoken = func(a *app) bool { return a.turn >= 1 }
	// onHome is a tip about a command home is the only screen for: it is armed
	// while home is in front and stands down the moment it is not, so the one
	// list can hold a sentence that would be a lie over a conversation's box.
	onHome = func(a *app) bool { return a.at(pageHome) }
	// awayFromHome is a conversation that has had an exchange and is in
	// front: the other half of [onHome], for a tip about the way back, which
	// would be a lie over home's own box.
	awayFromHome = func(a *app) bool { return a.turn >= 1 && !a.at(pageHome) }
)

// notices is the table, and ITS ORDER IS THE ORDER THE ROWS COME ROUND IN on
// both boxes ([noticeBoard.pick]). Text is chosen to agree with the manual page
// that answers each hint (internal/manual/chat's hints-and-tips.md), so the tip
// and the page say the same words — and notice_test.go holds the page to every
// line here, so the table cannot say a thing the manual does not.
//
// TWENTY-THREE ROWS, AND EVERY CUT WAS DELIBERATE. A survey of the surface on
// 2026-09-21 turned up forty-eight lines worth saying; thirty of those shipped,
// /project made thirty-one when it became a command of its own on 2026-09-22,
// and three reads of the whole list by the owner that same day took it to
// twenty-two; the way home by two spaces made it twenty-three on 2026-09-24,
// at the owner's word. `alt+3`, `alt+1`–`alt+7`, `/search` and `/subharness` came off
// as rows the foot or the tab bar already teaches; the two lines about a
// running answer became one; `/ask`'s came off ahead of the door it taught;
// `/folder`'s came off because it was not true and /attach's line now covers
// both kinds; the second /attach row was one row too many about one command;
// and `ctrl+.` came off on the owner's word. What was left out is
// what the foot already names — `alt+p`, `alt+e`, `alt+a`, `alt+k`, `/` — and
// the second spelling of anything already here. `/ shows every command` was a
// row until both feet started saying `/ commands` outright (footswap.go).
var notices = []notice{
	// ── the rows this surface shipped with ─────────────────────────────────
	{
		id: "compact-at-half", slot: slotHint,
		armed: func(a *app) bool {
			pct, ok := a.ctxPercent()
			return ok && pct >= contextHintPct
		},
		text:   "/compact summarizes the conversation now",
		retire: eventCompacted,
	},
	{
		id: "cost-after-spend", slot: slotHint,
		armed:  func(a *app) bool { return a.cost >= costHintUSD },
		text:   "/cost says what this conversation has spent",
		retire: eventCostShown,
	},
	// `ctrl+. sees every task this project has run` was here from the first
	// seven rows until 2026-09-22, when the owner took it off. The chord, the
	// page and `/history` are untouched, and [eventTaskStarted] and
	// [eventTaskPageOpened] are still fired at their seams: nothing in the
	// table waits on either one now, and a later row may.
	{
		// BOTH DOORS, BECAUSE THEY ARE ONE THING. The row said `/rewind takes
		// back an earlier message` until #1388 gave `esc esc` back and
		// respelled it as the chord alone. Either spelling teaches half of it:
		// the chord is the half nobody discovers, and the command is the half
		// that makes the chord findable again tomorrow — a person who reads
		// only `esc esc` has no word to type into `/` or to ask the manual
		// about. One row names both and retires on either (eventRewound fires
		// from both doors, rewind.go's [app.rewindLand]).
		id: "rewind-after-long-answer", slot: slotHint,
		armed:  func(a *app) bool { return a.lastAnswerRunes() >= longAnswerRunes },
		text:   "esc esc or /rewind takes back an earlier message",
		retire: eventRewound,
	},
	{
		id: "files-after-first-deliverable", slot: slotHint,
		armed:  func(a *app) bool { return a.notices.seen[eventDeliverableMade] },
		text:   "/files finds files codeaf wrote for you",
		retire: eventFilesOpened,
	},
	{
		// The welcome box already walked this directory for its recent column
		// (welcome.go), so the fact is at hand for nothing; a fresh directory
		// with no earlier conversation has an empty list and the hint stays down.
		id: "resume-when-earlier-exists", slot: slotHint,
		armed:  func(a *app) bool { return len(a.welcome.recent) > 0 },
		text:   "/resume opens an earlier conversation",
		retire: eventResumeOpened,
	},
	{
		id: "standing-after-several-sessions", slot: slotHint,
		armed: func(a *app) bool { return len(a.welcome.recent) >= 3 },
		// THE TWO "KEEPS" ROWS ARE TOLD APART SINCE 2026-09-22. This one and
		// `/remember` both said "keeps", which taught a person that the two
		// commands did the same thing in different words. They do not: a
		// standing order is a CONDITION the work has to honour — it rides into
		// a task's brief under its own heading and the worker reports when it
		// cannot meet one — and a memory is a fact carried forward.
		text:   "/standing turns a message into a rule work must follow",
		retire: eventStandingOpened,
	},
	// ── starting work ───────────────────────────────────────────────────────
	//
	// `/ask answers right here without opening a conversation` stood here
	// until 2026-09-22 and came off ahead of the door it taught: /ask is on
	// its way out, and a tip is a thing to teach somebody who will still have
	// it tomorrow. The command itself is untouched.
	{
		id: "task-in-chat", slot: slotHint,
		armed:  spoken,
		text:   "/task starts a single-shot task on the side",
		retire: eventTaskTyped,
	},
	{
		// THE THIRD ROW ABOUT A STANDING ORDER, and it says the same thing as
		// the one above in the same words since 2026-09-22. It read `sends your
		// message as something to keep true` — which is exactly the spelling
		// `/standing`'s row had just been taken off, for teaching that a rule
		// and a memory were one thing (see `remember-one-thing`). The two rows
		// retire on the SAME event, so they are one lesson told twice, and
		// telling it twice in two vocabularies is the way to teach neither.
		id: "standing-by-chord", slot: slotHint,
		armed:  ready,
		text:   "ctrl+enter makes your message a rule instead of a request",
		retire: eventStandingOpened,
	},
	{
		id: "manual-answers", slot: slotHint,
		armed:  ready,
		text:   "/manual answers any question about codeaf",
		retire: eventManualAsked,
	},
	{
		id: "reopen-tab", slot: slotHint,
		armed:  ready,
		text:   "ctrl+shift+t reopens the last conversation tab",
		retire: eventTabReopened,
	},
	// ── files and context ───────────────────────────────────────────────────
	{
		id: "at-completion", slot: slotHint,
		armed:  ready,
		text:   "type @ to find paths in the current project",
		retire: eventAtOpened,
	},
	{
		id: "attach-a-file", slot: slotHint,
		armed:  ready,
		text:   "/attach sends a file or folder with your message",
		retire: eventAttached,
	},
	// `/folder picks the folder codeaf works in` stood here until 2026-09-22
	// and was NOT TRUE: /folder never moves the directory codeaf is standing
	// in — that is fixed for the life of a conversation — it registers a
	// directory the conversation is ABOUT (folderplace.go's [app.referPlace]),
	// which is what /attach does with a folder after it, through the very same
	// seam. So the row came off and /attach's says "a file or folder". The
	// command, and `/place` and `/dir` with it, is untouched.
	{
		// ON HOME ALONE, because /project is home's alone (projectcmd.go). A
		// conversation's box would be reading it over a command that answers
		// there by pointing back at home.
		id: "pick-a-project", slot: slotHint,
		armed:  onHome,
		text:   "/project sets the project folder for a new conversation",
		retire: eventProjectSet,
	},
	{
		id: "export-the-conversation", slot: slotHint,
		armed:  func(a *app) bool { return a.turn >= 2 },
		text:   "/export writes the current conversation to a file",
		retire: eventDeliverableMade,
	},
	// ── models, thinking and cost ───────────────────────────────────────────
	{
		id: "model-list", slot: slotHint,
		armed:  ready,
		text:   "/model lets you see and choose models and providers",
		retire: eventModelListOpened,
	},
	{
		id: "crew-presets", slot: slotHint,
		armed:  ready,
		text:   "/crew sets the models codeaf uses on its own behalf",
		retire: eventCrewShown,
	},
	{
		id: "budget-cap", slot: slotHint,
		armed:  ready,
		text:   "/budget sets the spending cap for the day",
		retire: eventBudgetShown,
	},
	// ── steering a running answer ───────────────────────────────────────────
	{
		// ONE LINE FOR THE TWO THINGS A KEY DOES OVER A RUNNING ANSWER, since
		// 2026-09-22: `steer-with-enter` and `queue-with-ctrl-q` were a row
		// each and the owner folded them together. It is a NEW id and not
		// either of theirs, because a person who retired one of the pair has
		// not been told the other half ([notice.id]).
		id: "steer-and-queue", slot: slotHint,
		armed:  spoken,
		text:   "using enter steers conversations · use ctrl+q to queue messages",
		retire: eventQueued,
	},
	// ── moving around ───────────────────────────────────────────────────────
	{
		id: "new-chat", slot: slotHint,
		armed:  ready,
		text:   "ctrl+t starts a fresh chat in this project",
		retire: eventChatStarted,
	},
	{
		// THE WAY BACK, AT THE OWNER'S WORD ON 2026-09-24. The foot names the
		// door as `space space home` beside every tip ([app.footHint]), but a
		// label of three words reads as chrome after the first day, and this
		// row says what it is in a sentence. It is never armed on home, where
		// there is nowhere to go back to, and it retires on the gesture itself
		// rather than on reaching home by `/home` or the tab.
		id: "home-by-two-spaces", slot: slotHint,
		armed:  awayFromHome,
		text:   "space space takes you back to home",
		retire: eventHomeGesture,
	},
	// ── memory, accounts and the rest ───────────────────────────────────────
	{
		id: "remember-one-thing", slot: slotHint,
		armed: ready,
		// The other half of the pair above: a fact, and the way back out of it.
		text:   "/remember carries a fact forward, /forget drops it",
		retire: eventRemembered,
	},
	{
		id: "connect-accounts", slot: slotHint,
		armed:  ready,
		text:   "/connect links Notion, Slack and other services",
		retire: eventConnectOpened,
	},
	{
		// The seat `ask for a picture, a voiceover, music or a video` held
		// until 2026-09-22, and `ctrl+b freezes the screen so you can read
		// and copy from it` for one build the same day, both the owner's
		// call: the rule for what happens to a question while nobody is at
		// the keyboard is the one setting a person cannot guess exists until
		// it has already decided something for them (autonomysheet.go).
		//
		// COPY MODE ITSELF IS NOT GONE, and this row is the only reason to
		// think it might be. It was taken out on 2026-09-22 and put back on
		// 2026-09-23 at the owner's word — "we don't need a hint, but don't
		// remove the feature" — so `ctrl+b`, `/copy` and the frozen viewport
		// all work and simply have no row here.
		id: "autonomy-rule", slot: slotHint,
		armed:  spoken,
		text:   "/autonomy sets how questions are handled while you are away",
		retire: eventAutonomyAsked,
	},
}

// noticeBanned is the machinery vocabulary no person-facing line may carry.
// The words are the codebase's own law (CLAUDE.md), restated where a table of
// sentences is most likely to break it.
var noticeBanned = []string{"auditor", "verdict", "verified", "refuted"}

// noticeIDShape is what an id may look like: lower-case words joined by hyphens.
var noticeIDShape = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// checkNotices is THE TABLE CHECK, run at init over [notices] and by the tests
// over tables of their own.
func checkNotices(list []notice) error {
	known := make(map[string]bool, len(noticeEvents))
	for _, name := range noticeEvents {
		known[name] = true
	}
	seen := make(map[string]bool, len(list))
	for _, n := range list {
		switch {
		case !noticeIDShape.MatchString(n.id):
			return fmt.Errorf("notice %q has an id that is not kebab-case", n.id)
		case seen[n.id]:
			return fmt.Errorf("notice %q is written down twice", n.id)
		case n.slot >= noticeSlots:
			return fmt.Errorf("notice %q draws in a slot that does not exist", n.id)
		case n.armed == nil:
			return fmt.Errorf("notice %q has no arming rule", n.id)
		case strings.TrimSpace(n.text) == "" && n.say == nil:
			return fmt.Errorf("notice %q has nothing to say", n.id)
		case n.retire != "" && !known[n.retire]:
			return fmt.Errorf("notice %q retires on %q, which nothing fires", n.id, n.retire)
		case n.maxShown < 0:
			return fmt.Errorf("notice %q has a negative showing limit", n.id)
		case n.slot == slotHome:
			return fmt.Errorf("notice %q is filed under home's slot; a hint draws on home by being a hint", n.id)
		}
		seen[n.id] = true
		for _, word := range noticeBanned {
			if strings.Contains(strings.ToLower(n.text), word) {
				return fmt.Errorf("notice %q says %q, which is machinery vocabulary", n.id, word)
			}
		}
	}
	return nil
}

// It fails at startup and not at the first turn, for the reason the command
// table does: a broken table is a mistake in this file, and the loudest place
// to say so is before anything has been drawn.
func init() {
	if err := checkNotices(notices); err != nil {
		panic("tui3: the notice table is broken: " + err.Error())
	}
}

// limit is the showing limit with the default applied.
func (n notice) limit() int {
	if n.maxShown > 0 {
		return n.maxShown
	}
	return noticeShownDefault
}

// draws reports whether the row may stand in a slot: a hint row stands in
// both hint slots, and a news row in the note slot.
func (n notice) draws(slot noticeSlot) bool {
	if n.slot == slotHint {
		return slot == slotHint || slot == slotHome
	}
	return n.slot == slot
}

// line is what the notice says right now.
func (n notice) line(a *app) string {
	if n.say != nil {
		return n.say(a)
	}
	return n.text
}

// ── THE BOARD ───────────────────────────────────────────────────────────────

// noticeBoard is the session's side of the notices: the ledger, what each slot
// holds, and what has happened since the surface came up.
type noticeBoard struct {
	ledger noticeLedger
	// path is where the ledger is written, or "" on a door with no profile —
	// in which case the board remembers for this session only.
	path string
	// build is what this binary calls itself, or "" when the toolchain
	// recorded nothing; news is whether the ledger last saw a different one.
	build string
	news  bool
	// enabled is the Workspace tab's "disable hints" row, read the other way
	// up. Off silences every slot.
	enabled bool
	// current is the id standing in each slot, "" for none.
	current [noticeSlots]string
	// seen is every event fired this session — what arms the hints that wait
	// on one. done is every notice retired this session, which may not come
	// back before the surface restarts even if its arming rule is still true:
	// a hint that reappeared the moment after the gesture it taught would be
	// the surface not having noticed.
	seen map[string]bool
	done map[string]bool
	// armed is, per slot, whether each row was armed at that slot's last
	// decision — what makes a row FRESH at the next one ([noticeCandidate.fresh]).
	armed [noticeSlots]map[string]bool
	// ── HOME'S HALF ────────────────────────────────────────────────────────
	//
	// advance asks the next decision about home's row to move on to the next
	// eligible tip rather than keep the one standing. It is raised by
	// [app.noticeRotate] — a visit to home, a beat at rest — and spent by the
	// pick that honours it, so an event between two rotations leaves the row
	// alone unless the tip on it has just retired or a fresh one has arrived.
	advance [noticeSlots]bool
	// at is when each slot last changed hands, or zero when it never has;
	// [hintEvery] is measured from it by home's beat.
	at [noticeSlots]time.Time
	// since is when the tip standing in home's row became VISIBLE — home in
	// front and the tip on it — or zero while it cannot be seen. A showing is
	// counted from it when the tip leaves or the row goes out of sight
	// ([noticeBoard.settle]), and only if it stood [noticeReadTime].
	since [noticeSlots]time.Time
	// hidden is the cross on home's row having been pressed: the row draws
	// nothing at all until HOME ITSELF GOES OUT OF VIEW ([app.dropHome]),
	// which is the only thing that lifts it. Deciding the slot again does not
	// lift it, and neither does the two-minute beat: a cross answered with
	// another sentence on the same screen is the surface talking over somebody
	// who asked it to stop (the owner's ruling, 2026-09-22). It is this
	// session's and never the ledger's — putting a tip away is not using it.
	hidden [noticeSlots]bool

	// ── THE CONVERSATION'S HALF ────────────────────────────────────────────
	//
	// shown is every notice counted as shown this session on the conversation's
	// row, so an hour in the slot is one showing and not one per event. Home's
	// row does not use it: a showing there is a standing, measured in seconds.
	shown map[string]bool
	// lastHintTurn is the turn the conversation's slot last changed hands on,
	// or -1 when it never has; [noticeGap] is measured from it.
	lastHintTurn int
}

// bareNoticeBoard is a board with nothing behind it: no ledger on disk, no
// build to compare, and therefore no news. It is what a surface that was never
// handed a profile gets, and it is the half of [newNoticeBoard] that touches
// nothing — which is why it is reachable from a frame and the loader is not.
func bareNoticeBoard() noticeBoard {
	return noticeBoard{
		enabled:      true,
		seen:         map[string]bool{},
		done:         map[string]bool{},
		shown:        map[string]bool{},
		lastHintTurn: -1,
	}
}

// newNoticeBoard loads the ledger and decides whether there is news.
func newNoticeBoard(path, build string, enabled bool) noticeBoard {
	b := noticeBoard{
		ledger:       loadNoticeLedger(path),
		path:         path,
		build:        build,
		enabled:      enabled,
		seen:         map[string]bool{},
		done:         map[string]bool{},
		shown:        map[string]bool{},
		lastHintTurn: -1,
	}
	// A LEDGER FROM THE OLD COUNTING RULE IS FORGIVEN ONCE, on the way in
	// (notice_ledger.go's [noticeLedgerRule]): the tips it spent on flashes
	// come back, the ones a gesture retired stay retired, and the rule is
	// written down so this happens exactly once per profile.
	if b.ledger.Rule < noticeLedgerRule {
		b.ledger.forgive(noticeShownDefault)
		b.ledger.Rule = noticeLedgerRule
		b.save()
	}
	// A FIRST LAUNCH HAS NO NEWS. Nothing is new to somebody who has never
	// seen the older build; the channel opens on the second build a profile
	// meets. The build is written down either way, so the next change counts.
	if build != "" {
		b.news = b.ledger.Build != "" && b.ledger.Build != build
		if b.ledger.Build != build {
			b.ledger.Build = build
			b.save()
		}
	}
	return b
}

// save writes the ledger, and swallows the failure: a hint that shows once
// more because the disk was full is not a thing to interrupt anybody about.
func (b *noticeBoard) save() {
	if b.path == "" {
		return
	}
	_ = b.ledger.write(b.path)
}

// retired says whether the profile has already retired this notice.
func (b *noticeBoard) retired(id string) bool { return b.ledger.retired(id) }

// retire files the notice for good, this session and every one after.
func (b *noticeBoard) retire(id string) {
	b.ledger.retire(id)
	b.done[id] = true
	for slot := range b.current {
		if b.current[slot] == id {
			b.current[slot] = ""
		}
	}
}

// noticeCandidate is one row as the board sees it at an event: evaluated, so
// that [noticeBoard.pick] needs no frame to be tested against.
type noticeCandidate struct {
	id    string
	armed bool
	// fresh is a row that is armed now and was not at this slot's last
	// decision — the one thing that jumps the ring.
	fresh bool
}

// pick decides what a slot should hold, given the candidates for it. It
// returns "" for nothing, and it changes nothing on the board but the
// [noticeBoard.advance] it spends — [noticeBoard.take] records the decision.
//
// TWO BOXES, TWO RULES, ONE LIST (the owner's ruling, 2026-09-22). Home's row
// is a ROTATION and the conversation's is a RANKING, because the two rows are
// read in different ways: home is a screen somebody passes through, where
// every tip should get its turn; a conversation is a screen somebody sits in,
// where the tip worth saying is the one about what is happening RIGHT NOW.
// The conversation's rule is the one this surface shipped with and is restored
// here after a build that put it on the rotation with home.
func (b *noticeBoard) pick(slot noticeSlot, cands []noticeCandidate, turn int) string {
	if slot == slotHint {
		return b.rank(cands, turn)
	}
	return b.rotate(slot, cands)
}

// rank is THE CONVERSATION'S RULE: the first eligible row in the table's own
// order takes the slot, and a row that has stopped being eligible stands down
// at once. The table's order IS the ranking — a row written above another is
// the more urgent thing to say — which is what the old `priority` column on
// every row said in numbers before the two boxes were merged onto one list.
//
// THE SLOT CHANGES HANDS SLOWLY, and that is what stops the ranking from
// reading as a slideshow. A different id may take it only once [noticeGap]
// turns have passed since it last changed, so three tips arming in three turns
// are read one at a time. The slot's first occupant of the session waits on
// nothing, and a slot going EMPTY never waits: a tip whose arming fact stopped
// being true stands down at once, whatever the gap says.
func (b *noticeBoard) rank(cands []noticeCandidate, turn int) string {
	eligible := func(c noticeCandidate) bool { return c.armed && !b.done[c.id] && !b.retired(c.id) }
	held := b.current[slotHint]
	best := ""
	for _, c := range cands {
		if eligible(c) {
			best = c.id
			break
		}
	}
	if best == "" {
		return ""
	}
	if best != held && b.lastHintTurn >= 0 && turn-b.lastHintTurn < noticeGap {
		// Too soon for a different line. The one standing keeps standing if it
		// is still eligible, and the slot goes quiet otherwise.
		for _, c := range cands {
			if c.id == held && eligible(c) {
				return held
			}
		}
		return ""
	}
	return best
}

// rotate is HOME'S RULE: EVERY ELIGIBLE TIP HAS ITS TURN, in the table's
// order, round and round. The one standing keeps standing until the slot is
// asked to advance — or until it stops being eligible, when the next takes
// over at once so the row is never blank while there is something true to
// say. With one eligible tip the rotation is that tip; with none the row is
// empty. A retired notice, or one retired this session, is never a candidate.
//
// THE ONE EXCEPTION IS A TIP THAT HAS JUST BECOME TRUE. It jumps the ring
// whether or not the slot was asked to move: `/compact summarizes the
// conversation now` is worth saying when the window crosses half, and a ring
// of twenty tips would otherwise bring it round the best part of an hour
// later. It jumps once — at the decision that first sees it armed — and then
// takes its turn like every other row.
func (b *noticeBoard) rotate(slot noticeSlot, cands []noticeCandidate) string {
	held := b.current[slot]
	eligible := func(c noticeCandidate) bool { return c.armed && !b.done[c.id] && !b.retired(c.id) }
	advance := b.advance[slot]
	b.advance[slot] = false
	for _, c := range cands {
		if c.fresh && c.id != held && eligible(c) {
			return c.id
		}
	}
	at := -1
	for i, c := range cands {
		if c.id == held {
			at = i
		}
	}
	if at >= 0 && !advance && eligible(cands[at]) {
		return held
	}
	// Walk the ring from the one after the held one, back round to it.
	for step := 1; step <= len(cands); step++ {
		c := cands[(at+step+len(cands))%len(cands)]
		if eligible(c) {
			return c.id
		}
	}
	return ""
}

// take records that a slot now holds id. The one standing before is settled
// first — its showing counted if it stood long enough to be read — and the
// new one's standing starts now when the row is live. It reports whether the
// slot's occupant changed, and whether the ledger did.
//
// A SHOWING IS A TIP THAT STOOD [noticeReadTime] ON A ROW SOMEBODY COULD SEE.
// It is counted when the tip LEAVES — the slot changing hands, the row going
// out of sight — rather than when it arrives, because only then is it known
// how long it stood ([noticeBoard.settle]). A slot re-decided to the same tip
// is nothing at all, which is what keeps an hour of events on one tip at one
// showing; and a slot deciding while its row cannot be seen — home's while a
// conversation is in front, the conversation's before its quiet minute —
// starts no standing, because what has not been read has not been shown
// ([app.noticeLive]). Until 2026-09-22 every visible change of hands counted,
// and [noticeReadTime] says what that cost.
func (b *noticeBoard) take(slot noticeSlot, id string, live bool, now time.Time, limitOf func(string) int, turn int) (changed, wrote bool) {
	if b.current[slot] == id {
		return false, false
	}
	wrote = b.settle(slot, now, limitOf)
	b.current[slot] = id
	if slot == slotHint {
		// THE CONVERSATION'S ROW COUNTS ON ARRIVAL, ONCE PER SESSION. Its tip
		// stands on the keys row for as long as the frame is quiet, with no
		// clock over it and no cross to end it, so there is no departure to
		// measure — and an hour in the slot is one showing, not one per event.
		b.lastHintTurn = turn
		if id == "" || b.shown[id] {
			return true, wrote
		}
		b.shown[id] = true
		return true, b.count(id, limitOf(id)) || wrote
	}
	if id == "" || !live {
		return true, wrote
	}
	if slot == slotNote {
		// A NEWS LINE IS SAID, NOT STOOD: the transcript has it the moment it
		// is decided ([app.noticeShow]), so deciding it is its showing.
		return true, b.count(id, limitOf(id)) || wrote
	}
	b.since[slot] = now
	return true, wrote
}

// visible says the tip standing in a slot can be seen from now on — the row
// came into view with the tip already on it — and starts its standing unless
// one is already running. It is the other half of [noticeBoard.take], for the
// tip that was decided before the row was in front.
func (b *noticeBoard) visible(slot noticeSlot, now time.Time) {
	if b.current[slot] != "" && b.since[slot].IsZero() {
		b.since[slot] = now
	}
}

// settle ends the standing of the tip in a slot, counting a showing when it
// stood [noticeReadTime] or more, and retiring the notice when that showing
// was its last allowed. It reports whether the ledger changed. A slot with no
// standing running — nothing on it, or a row nobody could see — settles to
// nothing.
func (b *noticeBoard) settle(slot noticeSlot, now time.Time, limitOf func(string) int) bool {
	id, since := b.current[slot], b.since[slot]
	b.since[slot] = time.Time{}
	if id == "" || since.IsZero() || now.Sub(since) < noticeReadTime {
		return false
	}
	return b.count(id, limitOf(id))
}

// count records one showing of id, retiring it when that was its last
// allowed, and reports that the ledger changed.
func (b *noticeBoard) count(id string, limit int) bool {
	if b.ledger.show(id) >= limit {
		b.ledger.retire(id)
	}
	return true
}

// ── THE SURFACE'S SIDE ──────────────────────────────────────────────────────

// noticeEvent is THE ONE SEAM. Every place that proves a gesture happened says
// so here, in one line, and everything else in this file follows from the call:
// notices naming the event retire, the event is remembered for the rules that
// wait on it, and every slot is decided again.
func (a *app) noticeEvent(name string) {
	b := &a.notices
	if b.seen == nil {
		// A surface built without [newApp] — a test's bare app — still has a
		// board, and it remembers for as long as it lives. It is minted rather
		// than LOADED: a bare board has no ledger path, and going through
		// [newNoticeBoard] to reach that conclusion put a file read on the graph
		// of everything the frame can reach (framedisk_law_test.go).
		*b = bareNoticeBoard()
	}
	b.seen[name] = true
	wrote := false
	for _, n := range notices {
		if n.retire == name && !b.retired(n.id) {
			b.retire(n.id)
			wrote = true
		}
	}
	for slot := noticeSlot(0); slot < noticeSlots; slot++ {
		if a.noticeFill(slot) {
			wrote = true
		}
	}
	if wrote {
		b.save()
	}
	a.touch()
}

// noticeFill decides one slot and shows what it decided. It reports whether
// the ledger changed.
func (a *app) noticeFill(slot noticeSlot) bool {
	b := &a.notices
	if !b.enabled {
		// Off is off for every slot: a person who silenced hints did not ask to
		// be told about features either. The rows are left exactly as they are,
		// so turning the toggle back on shows what was due.
		return false
	}
	if b.armed[slot] == nil {
		b.armed[slot] = make(map[string]bool, len(notices))
	}
	cands := make([]noticeCandidate, 0, len(notices))
	for _, n := range notices {
		if !n.draws(slot) || b.done[n.id] || b.retired(n.id) {
			continue
		}
		if n.news && !b.news {
			continue
		}
		armed := n.armed(a)
		cands = append(cands, noticeCandidate{id: n.id, armed: armed, fresh: armed && !b.armed[slot][n.id]})
		b.armed[slot][n.id] = armed
	}
	id := b.pick(slot, cands, a.turn)
	now := a.now()
	changed, wrote := b.take(slot, id, a.noticeLive(slot), now, a.noticeLimit, a.turn)
	if changed {
		// A new tip is a new thing to read, so its clock starts again. THE
		// CROSS IS NOT SPENT HERE: a row somebody put away stays away until
		// that row leaves the frame ([noticeBoard.hidden]), and a tip arriving
		// behind it is a tip nobody is being shown.
		b.at[slot] = now
	}
	// A ROW IN FRONT WITH A TIP ON IT IS BEING SHOWN, whether the tip was
	// decided just now or before the row came into view.
	if a.noticeLive(slot) {
		b.visible(slot, now)
	}
	if changed && id != "" {
		a.noticeShow(slot, id)
	}
	return wrote
}

// noticeLive is whether a slot's row can be seen at all right now — which is
// what makes a change of hands a showing ([noticeBoard.take]): home's row
// while home is in front, the conversation's once its quiet minute has passed.
//
// A ROW WHOSE CROSS HAS BEEN PRESSED IS NOT LIVE. It draws nothing until home
// goes out of view ([noticeBoard.hidden]), and a tip standing behind a blank
// row is a tip nobody is reading — which is the whole of what [noticeReadTime]
// exists to tell apart.
//
// THE CONVERSATION'S ROW IS NEVER LIVE IN THIS SENSE, because it does not
// measure a standing at all: its showing is counted when the slot takes a tip,
// once per session ([noticeBoard.take]).
func (a *app) noticeLive(slot noticeSlot) bool {
	if a.notices.hidden[slot] {
		return false
	}
	switch slot {
	case slotHome:
		return a.at(pageHome)
	case slotHint:
		return false
	}
	return true
}

// noticeSettle ends the standing of a slot's tip because its row is going out
// of sight — home being left, a conversation's row hidden by a key — and
// writes the ledger when that standing was a showing ([noticeBoard.settle]).
func (a *app) noticeSettle(slot noticeSlot) {
	b := &a.notices
	if b.seen == nil {
		return
	}
	if b.settle(slot, a.now(), a.noticeLimit) {
		b.save()
	}
}

// noticeLimit is the showing limit of the notice with this id.
func (a *app) noticeLimit(id string) int {
	for _, n := range notices {
		if n.id == id {
			return n.limit()
		}
	}
	return noticeShownDefault
}

// noticeRotate moves a row on to the next tip. It is asked on every visit to
// home ([app.showPage]) and on home's beat once a tip has stood [hintEvery] at
// rest ([app.noticeHomeBeat]) — HOME'S ROW ALONE, because a conversation's
// takes the first eligible row rather than a turn. Rotating is the one thing an
// event does not do to a slot, so it is its own seam.
//
// IT DOES NOT LIFT A CROSS. The row a person put away is put away until it
// leaves the frame, and the beat that turns the ring every two minutes is
// exactly the thing that used to bring a tip back onto a home they were still
// standing on ([noticeBoard.hidden]).
func (a *app) noticeRotate(slot noticeSlot) {
	b := &a.notices
	if b.seen == nil {
		*b = bareNoticeBoard()
	}
	b.advance[slot] = true
	if a.noticeFill(slot) {
		b.save()
	}
	a.touch()
}

// noticeHomeRotate is [app.noticeRotate] for home's row: every road home.
func (a *app) noticeHomeRotate() { a.noticeRotate(slotHome) }

// noticeHomeBeat is home's clock asking whether the row is due to move
// (app.go's [homeTickMsg]): it is, once the tip standing has been up for
// [hintEvery] while home was quiet enough for it to be read. A row nobody
// could see — the box being typed into, a list up — does not age, because
// what has not been read has not been shown.
func (a *app) noticeHomeBeat() {
	b := &a.notices
	if b.current[slotHome] == "" || !a.noticeHomeQuiet() {
		return
	}
	if a.now().Sub(b.at[slotHome]) >= hintEvery {
		a.noticeHomeRotate()
	}
}

// noticeHomeHint is the line standing on home's row, while home is quiet
// enough for it to be read over an idle box, spelled for this terminal's
// keyboard (chords.go's [chordSpelling.say] turns `alt` into `opt` on a Mac).
func (a *app) noticeHomeHint() string {
	b := &a.notices
	id := b.current[slotHome]
	if id == "" || !b.enabled || b.hidden[slotHome] || !a.noticeHomeQuiet() {
		return ""
	}
	return a.chords.say(a.noticeLine(id))
}

// noticeLine is what the notice with this id says right now, or "" for an id
// the table does not hold.
func (a *app) noticeLine(id string) string {
	for _, n := range notices {
		if n.id == id {
			return n.line(a)
		}
	}
	return ""
}

// noticeHomeQuiet is whether nothing on home outranks a tip: the box is at
// rest, no list or layer has the keyboard, and no exchange is being read.
func (a *app) noticeHomeQuiet() bool {
	return a.at(pageHome) && a.home.box.empty() && !a.home.cmd.open && !a.home.comp.open && !a.home.searching() &&
		a.paneExchange() == nil && !a.targetPickShowing() && !a.composer.open && !a.hopShowing()
}

// noticeDismiss is the cross on a tip row, and what it means is ENOUGH OF
// THESE FOR NOW — not "say something else". The row goes blank and STAYS
// blank for the rest of this sitting: on home, until home is left and come
// back to; in a conversation, until the row goes out of sight under a key and
// the next quiet minute brings it back ([noticeBoard.hidden] names both, and
// they are the same law — the cross is lifted by the row going out of view).
//
// THE OWNER'S RULING, 2026-09-22: "do not show another hint until the user
// comes back to the home tab after leaving it". A cross answered with a second
// sentence in the same breath is the surface talking over somebody who has
// just asked it to stop.
//
// AND THE TIP THAT WAS PUT AWAY KEEPS ITS WHOLE ALLOWANCE. Its standing is
// thrown away rather than counted: a person who pressed the cross was telling
// the surface they did not want to read that line now, which is the opposite
// of having read it, and spending a showing on the gesture would retire a tip
// six dismissals in. The slot is asked to advance, so the row that comes back
// is a different one and this tip takes its turn again later in the ring.
func (a *app) noticeDismiss(slot noticeSlot) {
	b := &a.notices
	if b.seen == nil {
		*b = bareNoticeBoard()
	}
	b.since[slot] = time.Time{}
	b.hidden[slot] = true
	b.advance[slot] = true
	a.touch()
}

// noticeShow puts a newly chosen notice where its slot draws. The hint slots
// are read at render time ([app.noticeHint], [app.noticeHomeHint]) and need
// nothing done here; the note slot is a line in the transcript, said once, now.
func (a *app) noticeShow(slot noticeSlot, id string) {
	if slot != slotNote {
		return
	}
	if line := a.noticeLine(id); line != "" {
		a.note(line)
	}
}

// noticeHint is the conversation's tip: the lowest rung of the KEYS ROW at the
// foot (render.go's [app.footHint]), drawn whenever the frame is quiet enough
// for a tip to be read over an idle box.
//
// IT IS ON NO CLOCK AND HAS NO CROSS. For one build on 2026-09-22 it had both
// — a row of its own over the rule, a quiet minute before it appeared, a
// two-minute rotation and a cross — and the owner put it back where it was:
// the foot, decided by the events that prove what is happening, shown while
// nothing is happening. Home's row keeps the newer shape; the two boxes are
// read differently and are allowed to differ ([noticeBoard.pick]).
//
// IT DRAWS OVER NOTHING THAT IS HAPPENING. A running turn, a list, a layer, a
// box with words in it — each of those belongs to the thing being done, and
// the list here is [app.noticeQuiet].
func (a *app) noticeHint() string {
	b := &a.notices
	id := b.current[slotHint]
	if id == "" || !b.enabled || !a.noticeQuiet() {
		return ""
	}
	return a.chords.say(a.noticeLine(id))
}

// noticeQuiet is whether nothing on the frame outranks a tip.
func (a *app) noticeQuiet() bool {
	return a.input.empty() && a.state != stateWorking && a.showing() == nil &&
		!a.rew.on && !a.rewSheet.open && !a.copy.on && !a.menu.open && !a.comp.open &&
		!a.pick.open && !a.roster.open && !a.asking() && !a.roomOpen()
}

// lastAnswerRunes is how long the newest finished answer is — the fact the
// rewind hint arms on. It walks back from the end and stops at the first answer,
// so a long conversation costs no more than a short one.
func (a *app) lastAnswerRunes() int {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryAssistant {
			return len([]rune(a.entries[i].text))
		}
	}
	return 0
}

// buildStamp is the stable source identity for the news channel. It leaves the
// build time out because rebuilding unchanged source must not repeat old news.
func buildStamp() string {
	return buildinfo.Revision()
}

// showUnreadProfileKeys uses the notice ledger for a profile-scoped, set-scoped
// conversation note. The keys are the config loader's result; this layer only
// identifies and renders that result.
func (a *app) showUnreadProfileKeys(keys []string) {
	if len(keys) == 0 || !a.notices.enabled {
		return
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return
	}
	digest := sha256.Sum256(encoded)
	id := "unread-profile-config-keys-" + hex.EncodeToString(digest[:])
	if a.notices.retired(id) {
		return
	}
	a.notices.ledger.show(id)
	a.notices.ledger.retire(id)
	a.notices.save()
	a.note("config.json keys are not read: " + strings.Join(keys, ", ") + "; anything set under them is ignored and defaults apply.")
}
