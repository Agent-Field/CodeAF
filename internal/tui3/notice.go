package tui3

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
)

// THE NOTICES: telling a person one thing at the moment it becomes true.
//
// A surface learns you by what you have already done, and this file is where it
// keeps what it has told you. Two kinds of thing live here at launch:
//
//   - EARNED HINTS. One line in the legend's hint slot — `ctrl+. sees every task
//     this project has run` — that fires the first time it is relevant (a task
//     just started) and RETIRES FOR GOOD the first time the gesture it teaches is
//     used (the task page opened), or after it has been shown in a few separate
//     sessions without being acted on. A hint that stays up after you have
//     learned the key is a cheatsheet, and a cheatsheet is read once and never
//     again (render.go's [app.hintWord] says the same about static keys).
//   - NEWS. One dim transcript line, said once, the first time this binary runs
//     after its build changed — the place a shipped feature announces itself.
//     The channel exists and is empty; a wave that ships something registers a
//     row with [notice.news] set and writes nothing else.
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
// (notice_ledger.go): how many sessions each notice has been shown in, when it
// retired, and which build the news channel last saw. A missing or unreadable
// ledger is an empty one — a person is never told their hints file is corrupt,
// because the worst case is a tip they have seen before.

// noticeSlot is where a notice may draw. Exactly two exist; the type is an enum
// rather than a bool so a later wave can add one without touching the rows that
// exist — a new slot lands as one constant above [noticeSlots] and one case in
// [app.noticeShow].
type noticeSlot uint8

const (
	// slotHint is the legend's hint slot (render.go's [app.legendRight]), and a
	// notice standing in it is the LOWEST RUNG THERE IS: every state key and every
	// existing hint outranks it, so a tip is only ever drawn over an idle box.
	slotHint noticeSlot = iota
	// slotNote is one calm transcript line through [app.note]. It is reserved
	// for news: a hint belongs beside the box it is about, and a line in the
	// conversation is for something that is true once.
	slotNote
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
)

// noticeEvents is every event there is, in one list, so the table check can
// refuse a retire rule that names a word nobody fires.
var noticeEvents = []string{
	eventBoot, eventTurnEnded, eventTaskStarted, eventTaskPageOpened,
	eventMenuOpened, eventRewound, eventCopyEntered, eventModelSwitched,
	eventCompacted, eventFilesOpened, eventResumeOpened, eventCostShown,
	eventStandingOpened, eventDeliverableMade,
}

// notice is one thing the surface may tell a person, and the whole of the rule
// for when.
type notice struct {
	// id is the stable name the ledger files it under: kebab-case, and NEVER
	// REUSED once a build has shipped it, because a retired id is a promise that
	// a person has already been told this and does not want to be again.
	id   string
	slot noticeSlot
	// priority decides between two notices eligible for one slot at once;
	// higher wins, and the table's order breaks a tie.
	priority int
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
	// maxShown is how many SESSIONS the notice may be shown in before it retires
	// by itself, whether or not the gesture was ever used; zero means
	// [noticeShownDefault]. Counted per session and not per frame, because a
	// hint standing in the slot for an hour is one showing.
	maxShown int
	// news marks the what's-new channel: a row that is armed only on the first
	// launch after the binary's build changed, and shown once.
	news bool
}

// noticeShownDefault is how many sessions a hint may be shown in before it is
// taken as read. Three is one more than a coincidence: a tip seen in two
// separate sessions and never acted on is a tip about something the person
// does not want, and the fourth showing would be the surface nagging.
const noticeShownDefault = 3

// noticeGap is the fewest turns between one hint standing down and a different
// one taking the slot. It is what keeps a busy first session from reading as a
// slideshow: three hints arming in three consecutive turns are shown one at a
// time, each with room to be read.
const noticeGap = 2

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

// notices is the table, in priority order for reading. Text is chosen to agree
// with the manual page that answers each hint (internal/manual/chat's
// hints-and-tips.md), so the tip and the page say the same words.
var notices = []notice{
	{
		id: "compact-at-half", slot: slotHint, priority: 90,
		armed: func(a *app) bool {
			pct, ok := a.ctxPercent()
			return ok && pct >= contextHintPct
		},
		text:   "/compact summarizes the conversation now",
		retire: eventCompacted,
	},
	{
		id: "cost-after-spend", slot: slotHint, priority: 85,
		armed:  func(a *app) bool { return a.cost >= costHintUSD },
		text:   "/cost says what this conversation has spent",
		retire: eventCostShown,
	},
	{
		id: "task-page-after-first-task", slot: slotHint, priority: 80,
		armed:  func(a *app) bool { return a.notices.seen[eventTaskStarted] },
		text:   "ctrl+. sees every task this project has run",
		retire: eventTaskPageOpened,
	},
	{
		id: "rewind-after-long-answer", slot: slotHint, priority: 70,
		armed:  func(a *app) bool { return a.lastAnswerRunes() >= longAnswerRunes },
		text:   "esc esc takes back the last message",
		retire: eventRewound,
	},
	{
		id: "files-after-first-deliverable", slot: slotHint, priority: 60,
		armed:  func(a *app) bool { return a.notices.seen[eventDeliverableMade] },
		text:   "/files finds everything made for you",
		retire: eventFilesOpened,
	},
	// A TIP MAY NOT TEACH A KEY THE REST STATE ALREADY NAMES (ISSUE-126). There
	// was a `menu-after-first-turn` tip here reading `/ shows every command`,
	// armed from the end of the first turn and retired only when somebody pressed
	// `/`. It occupied slotHint, which outranks the rest state below it — so from
	// a person's very first answer until they happened to open the menu, the slot
	// said one key, and the three the rest state names (`space space home`,
	// `tab last`, `ctrl+k switch`) were never advertised at all. On a fresh home
	// that is every frame there is.
	//
	// It was also a downgrade on its own terms: the rest state ends in
	// `/ commands` (render.go's [microcopy]), so the tip displaced a line naming
	// four doors with a longer sentence about one of them. The owner's standing
	// law is that a key which only acts once you have learned what it is for is
	// invisible — a tip has to teach something the frame is NOT already saying.
	{
		// The welcome box already walked this directory for its recent column
		// (welcome.go), so the fact is at hand for nothing; a fresh directory
		// with no earlier conversation has an empty list and the hint stays down.
		id: "resume-when-earlier-exists", slot: slotHint, priority: 40,
		armed:  func(a *app) bool { return len(a.welcome.recent) > 0 },
		text:   "/resume opens an earlier conversation",
		retire: eventResumeOpened,
	},
	{
		id: "standing-after-several-sessions", slot: slotHint, priority: 10,
		armed:  func(a *app) bool { return len(a.welcome.recent) >= 3 },
		text:   "/standing keeps something always true",
		retire: eventStandingOpened,
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
	// enabled is the Display tab's "hints" row. Off silences both slots.
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
	// shown is every notice counted as shown this session, so an hour in the
	// slot is one showing and not one per event.
	shown map[string]bool
	// lastHintTurn is the turn the hint slot last changed hands on, or -1 when
	// it never has; [noticeGap] is measured from it.
	lastHintTurn int
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

// tipsLive reports whether the earned-tips machinery still has anything to
// say to this person: any notice in the table that is neither retired for good
// nor done for this session. It is the gate the legend's idle slot decays
// behind (render.go's [app.legendRight]) — the generic key advertising is
// shown while a tip would still be, and quiet once the person has earned the
// quiet.
func (b *noticeBoard) tipsLive() bool {
	for _, n := range notices {
		if !b.retired(n.id) && !b.done[n.id] {
			return true
		}
	}
	return false
}

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
	id       string
	priority int
	armed    bool
	limit    int
}

// pick decides what a slot should hold, given the candidates for it and the
// turn the surface is on. It returns "" for nothing, and it changes nothing on
// the board — [noticeBoard.take] records the decision.
//
// The rules, in the order they are applied:
//
//   - A retired notice, or one retired this session, is never a candidate.
//   - Among the armed ones the highest priority wins, table order breaking a
//     tie. The one already standing is preferred over an equal.
//   - THE HINT SLOT CHANGES HANDS SLOWLY. A different id may take it only once
//     [noticeGap] turns have passed since it last changed, so three hints arming
//     in three turns are read one at a time. The slot's first occupant of the
//     session waits on nothing. A slot going EMPTY never waits: a hint whose
//     arming fact stopped being true stands down at once.
func (b *noticeBoard) pick(slot noticeSlot, cands []noticeCandidate, turn int) string {
	held := b.current[slot]
	best, found := noticeCandidate{}, false
	for _, c := range cands {
		if !c.armed || b.done[c.id] || b.retired(c.id) {
			continue
		}
		if !found || c.priority > best.priority || (c.priority == best.priority && c.id == held) {
			best, found = c, true
		}
	}
	if !found {
		return ""
	}
	if slot == slotHint && best.id != held && b.lastHintTurn >= 0 && turn-b.lastHintTurn < noticeGap {
		// Too soon for a different line. The one standing keeps standing if it
		// is still eligible, and the slot goes quiet otherwise.
		for _, c := range cands {
			if c.id == held && c.armed && !b.done[c.id] && !b.retired(c.id) {
				return held
			}
		}
		return ""
	}
	return best.id
}

// take records that a slot now holds id — counting the showing once per
// session, retiring the notice when this showing was its last allowed, and
// noting the turn so the gap can be measured. It reports whether the slot's
// occupant changed, and whether the ledger did.
func (b *noticeBoard) take(slot noticeSlot, id string, limit int, turn int) (changed, wrote bool) {
	if b.current[slot] == id {
		return false, false
	}
	b.current[slot] = id
	if id == "" {
		return true, false
	}
	if slot == slotHint {
		b.lastHintTurn = turn
	}
	if b.shown[id] {
		return true, false
	}
	b.shown[id] = true
	count := b.ledger.show(id)
	if count >= limit {
		// The last allowed showing is still a showing: the line stays up for
		// this session and the ledger closes the book on it for the next.
		b.ledger.retire(id)
	}
	return true, true
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
		// board, and it remembers for as long as it lives.
		*b = newNoticeBoard("", "", true)
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
		// Off is off for both slots: a person who silenced hints did not ask to
		// be told about features either. The rows are left exactly as they are,
		// so turning the toggle back on shows what was due.
		return false
	}
	cands := make([]noticeCandidate, 0, len(notices))
	limits := make(map[string]int, len(notices))
	for _, n := range notices {
		if n.slot != slot || b.done[n.id] || b.retired(n.id) {
			continue
		}
		if n.news && !b.news {
			continue
		}
		cands = append(cands, noticeCandidate{id: n.id, priority: n.priority, armed: n.armed(a)})
		limits[n.id] = n.limit()
	}
	id := b.pick(slot, cands, a.turn)
	changed, wrote := b.take(slot, id, limits[id], a.turn)
	if changed && id != "" {
		a.noticeShow(slot, id)
	}
	return wrote
}

// noticeShow puts a newly chosen notice where its slot draws. The hint slot is
// read at render time ([app.noticeHint]) and needs nothing done here; the note
// slot is a line in the transcript, said once, now.
func (a *app) noticeShow(slot noticeSlot, id string) {
	if slot != slotNote {
		return
	}
	for _, n := range notices {
		if n.id == id {
			a.note(n.line(a))
			return
		}
	}
}

// noticeHint is the hint slot's lowest rung: the line standing in [slotHint],
// while the frame is quiet enough for a tip to be read over an idle box.
//
// noticeTipsLive is the legend's decay gate: whether the earned-tips
// machinery still has anything to say to this person (notice.go's
// [noticeBoard.tipsLive]).
func (a *app) noticeTipsLive() bool { return a.notices.tipsLive() }

// IT DRAWS OVER NOTHING THAT IS HAPPENING. Every state with keys of its own has
// already answered in [app.hintWord] by the time this is asked, and the list
// here is the handful of states that answer "" there on purpose — the rewind
// bar prints its own keys, a fullscreen page has no legend — plus the one this
// slot adds: a box with words in it belongs to the sentence being written.
func (a *app) noticeHint() string {
	b := &a.notices
	id := b.current[slotHint]
	if id == "" || !b.enabled || !a.noticeQuiet() {
		return ""
	}
	for _, n := range notices {
		if n.id == id {
			return n.line(a)
		}
	}
	return ""
}

// noticeQuiet is whether nothing on the frame outranks a tip.
func (a *app) noticeQuiet() bool {
	return a.input.empty() && a.state != stateWorking &&
		!a.rew.on && !a.rewSheet.open && !a.at(pageSettings) && !a.at(pageTasks) && !a.at(pageHome) &&
		!a.copy.on && !a.menu.open && !a.comp.open && !a.pick.open && !a.roster.open &&
		!a.asking() && !a.roomOpen()
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
