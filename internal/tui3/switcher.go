package tui3

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// ── THE SWITCHER'S READING ──────────────────────────────────────────────────
//
// HOME IS A SWITCHER, NOT A DIRECTORY, AND THIS FILE IS THE WHOLE OF WHAT IT
// READS (SCREEN 1a). Data in — a world, the standing bands, where this window is
// standing, a look stamp and a clock — rows, stops and verbs out.
//
// IT IS PURE AND IT MUST STAY PURE. Nothing here takes an *app, starts a clock,
// opens a file or asks the disk anything: the facts are gathered on home's own
// three-second beat and handed in whole (docs/design/home-rethink/ARCHITECTURE.md
// states the three layers and which may know what). That is what lets the whole
// reading be tested with fixtures, and what keeps the panels that draw it
// (homepanel_*.go) pure over it too.

// switcherVerb is one thing the strip can offer for a row: the letter, the word
// it is spelled with, and — for the two verbs that ANSWER a question — the
// option key that answer has to be sent under.
//
// THE ANSWER KEY IS CARRIED AND NEVER DERIVED. A question's options are the ones
// that session offered ("1", "3", sometimes "2"), and a strip that recomputed
// them from the letter it drew would be answering a different question than the
// one on the row (homeband_answer.go's [app.answerKey] holds the same law for
// the digits).
type switcherVerb struct {
	key    rune
	word   string
	answer string
}

type switcherLedgerInput struct {
	learned int
	letGo   int
	// made is the files conversations made since the look stamp, read on home's
	// beat and never on a draw (place_home.go's [app.readSwitchLedger]).
	made []session.Artifact
}

type switcherKind uint8

const (
	switcherConversation switcherKind = iota
	switcherStanding
	switcherLedger
)

// switcherRow holds every kind of door the router can open. Zero fields are
// deliberately meaningful: a row never fabricates an address it was not given.
type switcherRow struct {
	chatKey string
	kind    switcherKind
	session session.SessionRow
	item    StandingItemView
	place   string
	project string
	title   string
	note    string
	age     string
	at      time.Time
	needs   bool
	moving  bool
	paused  bool
	here    bool
	held    bool
	// coming is a conversation another window has been asked to let go of, and
	// it OUTRANKS [switcherRow.held] on the margin: `another window` is where it
	// is, and a person who has just pressed enter is asking whether it is on its
	// way (takeovervoice.go).
	coming bool
	// door is whether enter on this row would ASK for the conversation, which
	// is the one thing that decides if the margin may name the door
	// (takeovervoice.go's [takeoverHeldDoorWord]). It is narrower than
	// [switcherRow.held]: over --host the holder is a window on this laptop and
	// the journal is on the far machine, and a conversation with no folder of
	// its own has nowhere to leave a request — both are held, and neither has a
	// door. A row that promised a key it would then refuse is the worst thing a
	// word on a margin can do, which is the rule `here` is already written to.
	door    bool
	gone    bool
	options []session.AnswerOption
	// task is the landed piece of work a `since you left` line names, and path
	// the file one names; each is the line's door (place_home.go's
	// [app.homeLedgerEnter]). note is the one fact such a line carries besides
	// its title — a task's cost, the conversation a file was made in — and it is
	// the line's DESCRIPTION, drawn under the cursor, and not its margin: the
	// margin is when it happened, like every row of the field (owner,
	// 2026-09-15).
	task *session.TaskIndexEntry
	path string
}

// switcherReading is the whole of what the grid's panels read off the machine:
// every conversation and every standing thing that needs somebody or is firing,
// ranked, and the `since you left` lines beside them (homegrid.go's
// [homeView.gridInput]).
type switcherReading struct {
	// rows is ranked: what needs you (oldest first), then what is moving, then
	// the rest by recency ([switcherLess]).
	rows []switcherRow
	// ledger is `since you left`, newest first, and empty on a first look.
	ledger []switcherRow
	now    time.Time
}

// switcherHere is WHERE THIS WINDOW IS STANDING: `session` is the conversation
// on screen — the one row that wears `here` instead of an age.
//
// IT IS THE EXACT ANSWER AND NEVER THE BROAD ONE. A reading that only had the
// project would have to guess which of its rows was `here` (it used to, and it
// guessed the first open one).
type switcherHere struct {
	session string
	// coming is the transcript this window has asked another window to let go
	// of, and "" when it has asked for nothing. It is a fact about THIS window
	// rather than about the world, which is why it travels with the address
	// above rather than being read off any row.
	coming string
	// hosted is this window looking at ANOTHER MACHINE's home over --host, in
	// which case no row has a door out of `another window`: the window holding
	// the conversation is on this laptop and the journal is over there, so there
	// is nobody here to ask (takeover.go's header states the rule).
	hosted bool
}

// switcherGone is which project folders were NOT on the disk when the world was
// last read, keyed by the path a row answers for ([homeWhere]).
//
// IT IS A READING AND NOT A SYSCALL. One os.Stat per project per reading, taken
// where the world is taken (home.go's [homeView.readGone]), handed in here whole
// — because this file may not touch a disk and because the door itself stats
// again on the keystroke anyway. A path this map has never heard of is not gone.
type switcherGone map[string]bool

// readSwitcher uses the same attention rules as homeattention.go: NeedsPerson
// outranks everything; moving is Tasks.Running or a fresh PresenceWorking
// conversation, and a standing item moves only while view.Running. An item's
// own NeedsPerson likewise outranks its running marker.
func readSwitcher(world session.World, items map[string][]StandingItemView, fired []StandingItemView, here switcherHere, gone switcherGone, seen time.Time, now time.Time, ledger switcherLedgerInput) switcherReading {
	r := switcherReading{now: now}
	var all []switcherRow
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			if row.Archived {
				continue
			}
			needs := row.NeedsPerson()
			moving := !needs && (row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking)
			// EXACTLY THE ONE CONVERSATION THIS WINDOW IS HOLDING. A broader test
			// would put `here` on a row somebody would then press enter on and go
			// nowhere, which is the worst thing a word on a door can do.
			atHere := here.session != "" && ((row.Dir != "" && filepath.Clean(row.Dir) == filepath.Clean(here.session)) ||
				(row.ID != "" && row.ID == here.session))
			options := []session.AnswerOption(nil)
			if row.NeedsPerson() && strings.TrimSpace(row.Presence.Question.Text) != "" {
				options = append(options, row.Presence.Question.Options...)
			}
			coming := !atHere && here.coming != "" && strings.TrimSpace(row.Transcript) == here.coming
			held := !atHere && row.Open && strings.TrimSpace(row.Transcript) != ""
			all = append(all, switcherRow{
				kind: switcherConversation, session: row, project: project.Name,
				title: homeName(row), note: switcherConversationNote(row, seen), age: sinceAt(row.At, now),
				at: switcherSortAt(row), needs: needs, here: atHere,
				// AND THE TWO FACTS THAT DECIDE WHETHER ENTER CAN WORK AT ALL. A row
				// another window is holding and a row whose folder is not there any
				// more both refuse when they are pressed, and a list that said so only
				// on a card would be saying it only past a hundred and sixty columns —
				// which is exactly the trap the short spellings were written for.
				// THE LOCK AND NOT THE HEARTBEAT. `Open` is a window holding this
				// journal, which is exactly what the door refuses on; `Live` is a
				// conversation saying what it is doing, which the note already
				// carries and which would put `another window` on the margin of
				// every row that is asking anything.
				held:   held,
				door:   held && !here.hosted && strings.TrimSpace(row.Dir) != "",
				coming: coming,
				// AND A CONVERSATION ON ITS WAY HERE IS MOVING, whatever else is
				// or is not running in it. The one spinner belongs to the thing
				// that started most recently (homespinner.go), and nothing on
				// this machine started more recently than the keystroke that
				// asked for this row.
				moving:  moving || coming,
				gone:    gone[switcherWhere(row, project)],
				options: options,
			})
		}
		for _, view := range items[project.Dir] {
			if strings.TrimSpace(view.Item.NeedsPerson) == "" && !view.Running {
				continue
			}
			needs := strings.TrimSpace(view.Item.NeedsPerson) != ""
			all = append(all, switcherRow{
				kind: switcherStanding, item: view, project: project.Name,
				title: strings.TrimSpace(view.Item.Words), note: switcherStandingNote(view),
				age: sinceAt(switcherItemAt(view), now), at: switcherItemAt(view),
				needs: needs, moving: !needs && view.Running, paused: view.Item.Status == standing.StatusPaused,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return switcherLess(all[i], all[j]) })
	r.rows = all
	r.addLedger(items, fired, world, seen, ledger)
	return r
}

// switcherWhere is the project a row belongs to, in the words [homeView.gone] is
// keyed by: the project's own path, and its name when nothing recorded one. It
// is [homeWhere] read off a row rather than off a drawn line, so the two can
// never disagree about which folder a row is about.
func switcherWhere(row session.SessionRow, project session.Project) string {
	if path := strings.TrimSpace(row.ProjectDir); path != "" {
		return path
	}
	return project.Name
}

func switcherSortAt(row session.SessionRow) time.Time {
	if row.NeedsPerson() {
		return attentionWaitedSince(row)
	}
	if row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking {
		return attentionMovingSince(row)
	}
	return row.At
}

func switcherItemAt(view StandingItemView) time.Time {
	if strings.TrimSpace(view.Item.NeedsPerson) != "" {
		return view.Item.Updated
	}
	if view.Running {
		return view.Mark.Since
	}
	return view.Item.LastFired
}

func switcherLess(a, b switcherRow) bool {
	ra, rb := switcherRank(a), switcherRank(b)
	if ra != rb {
		return ra > rb
	}
	if ra == 3 { // A longer wait belongs first.
		return attentionOlder(a.at, b.at)
	}
	if ra == 2 { // More live work is the useful tie-break before recency.
		ba, bb := switcherBusy(a), switcherBusy(b)
		if ba != bb {
			return ba > bb
		}
	}
	return a.at.After(b.at)
}

func switcherRank(row switcherRow) int {
	if row.needs {
		return 3
	}
	if row.moving {
		return 2
	}
	return 1
}

func switcherBusy(row switcherRow) int {
	if row.kind == switcherConversation && row.session.Tasks.Running > 0 {
		return row.session.Tasks.Running
	}
	if row.moving {
		return 1
	}
	return 0
}

func switcherConversationNote(row session.SessionRow, seen time.Time) string {
	if row.NeedsPerson() {
		line := switcherFirstLine(row.Presence.Question.Text)
		if line == "" {
			line = switcherFirstLine(row.Reason())
		}
		if line == "" {
			return ""
		}
		// THE GATE'S OWN SENTENCE IS THE WHOLE NOTE, and this row adds not one
		// word to it. The line a consent question carries is written once, by the
		// lane that knows the tool, EXPRESSLY so that another window can say what
		// this session is stopped on (session's consent.go: it is "the one line
		// another window may answer this from") — and it is already a predicate
		// about the conversation named beside it: `needs your ok to run bash`.
		// This row used to prefix `wants to ` onto it, from the days when the
		// engine handed over a bare action, and what a person actually read on
		// home was `consentws wants to needs your ok to run bash`. One sentence,
		// written in one place, repeated here exactly.
		if row.Presence.Question.Kind == session.QuestionConsent {
			return line
		}
		return switcherAsksWord + line
	}
	if row.Tasks.Running > 0 {
		note := fmt.Sprintf("%d %s running", row.Tasks.Running, switcherPlural(row.Tasks.Running, "task", "tasks"))
		for _, entry := range row.Tasks.Rows {
			if !row.Runs(entry) {
				continue
			}
			// THE PHASE OUTRANKS THE ACTIVITY WHEREVER BOTH ARE KNOWN, for
			// taskphase.go's reason: through a check and a repair round the
			// activity is the worker's last call sitting there finished, and the
			// phase is what is actually happening. It is also the only one of the
			// two that crosses a window at all (session's [session.PresenceTask.Phase]).
			if phase := taskPhaseWords(row.Phase(entry), 0, 0); phase != "" {
				return note + " · " + phase
			}
			if strings.TrimSpace(entry.Activity) != "" {
				return note + " · " + switcherFirstLine(entry.Activity)
			}
		}
		return note
	}
	// A FIRST LOOK HAS NO ORIGIN TO MEASURE FROM, so nothing is news. The stamp is
	// zero until home has been closed once (home.go's [homeView.seen] states the
	// law); without this guard every task a conversation ever ran would be work
	// that landed "while you were away", on the one screen whose whole job is to
	// say what changed.
	if seen.IsZero() {
		return ""
	}
	files := 0
	saved := false
	for _, entry := range row.Tasks.Rows {
		if !entry.EndedAt.After(seen) {
			continue
		}
		files += entry.FilesChanged
		if session.TaskKindWord(entry.Kind) == "saved shape" {
			saved = true
		}
	}
	if files > 0 {
		return fmt.Sprintf("%d files made", files)
	}
	if saved {
		return "ran a saved shape"
	}
	return ""
}

func switcherStandingNote(view StandingItemView) string {
	if need := switcherFirstLine(view.Item.NeedsPerson); need != "" {
		// WHICH OF THE TWO THINGS NeedsPerson CARRIES HAS ONE ANSWER, and
		// [standing.IsPermissionLine] is it, because the spellings of a refusal
		// live in standing, in that one predicate, and no surface keeps its own
		// list of them. The refusal is said bare because it is already a whole
		// sentence about the item, `stopped: it needed your ok to run bash`, so
		// the ask word would put `asks: stopped` on a row where nobody asked
		// anything, the same reading switcherConversationNote takes above for
		// the consent gate's own sentence. The word is for the other tenant, a
		// QUESTION the firing put to the person in its own words.
		if standing.IsPermissionLine(need) {
			return need
		}
		return switcherAsksWord + need
	}
	if view.Running && strings.TrimSpace(view.Mark.What) != "" {
		return switcherFirstLine(view.Mark.What)
	}
	return ""
}

// switcherAsksWord is what a row's note says in front of the question it is
// stopped on. The grid's needs panel draws the question under its row and takes
// the word back off, because there the panel's own heading already says it.
const switcherAsksWord = "asks: "

func switcherFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if at := strings.IndexByte(s, '\n'); at >= 0 {
		s = s[:at]
	}
	return strings.TrimSpace(s)
}

func switcherPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// addLedger builds the `since you left` block: what happened on its own while
// nobody was looking.
//
// IT WALKS WHAT STANDS AND WHAT WENT, and it has to walk both. items is every
// project's live band, which is the right answer for a watch that fired at six
// and is still watching; fired is what the same reading found RETIRED with a
// firing on it (homestanding.go's [app.standItems]). A one-off — the commonest
// standing thing there is, `remind me in 1 minute` — retires in the pass that
// fires it, so a block built off the bands alone was silent about exactly the
// case it exists for: the reminder went off with the terminal shut, and the
// screen a person came back to said nothing had happened.
func (r *switcherReading) addLedger(items map[string][]StandingItemView, fired []StandingItemView, world session.World, seen time.Time, input switcherLedgerInput) {
	// AND THE WHOLE BLOCK IS ABOUT A STRETCH OF TIME THAT MAY NOT EXIST YET. With
	// no look stamp there is no "since", so there is nothing to say — the same
	// first-look law [switcherConversationNote] keeps one function up.
	if seen.IsZero() {
		return
	}
	var events []switcherRow
	// ONE LINE PER ITEM, HOWEVER MANY PROJECTS HOLD IT. The bands are keyed by
	// project directory and a machine-wide watch is in every one of them, so a
	// walk that did not remember what it had seen would say the same thing four
	// times (homestanding.go's [app.readStandBands] keys them, and this is the
	// reading's own half of that fact). The retired half is deduped against the
	// same map, because an item that stood in two places went in two places.
	said := make(map[string]bool)
	add := func(view StandingItemView) {
		if !view.Item.LastFired.After(seen) || said[view.Item.ID] {
			return
		}
		said[view.Item.ID] = true
		// THE ITEM'S OWN LAST-LOOK SENTENCE AND NEVER A SECOND ONE WRITTEN HERE.
		// `fired 3 minutes ago · … — it told you` is [standing.LastLookLine], the
		// one place that sentence is composed, and a retired one-off is told the
		// same way a live watch is: what it did, when, and what came of it. The
		// row does not say the thing has stood down, because the block is a list
		// of what HAPPENED and not a roll-call of what still stands.
		line := standing.LastLookLine(view.Item, r.now)
		if line == "" {
			line = switcherFirstLine(view.Item.LastCheckLine)
		}
		if line != "" {
			events = append(events, switcherRow{kind: switcherLedger, item: view, title: line, place: "standing", at: view.Item.LastFired})
		}
	}
	for _, views := range items {
		for _, view := range views {
			add(view)
		}
	}
	for _, view := range fired {
		add(view)
	}
	events = append(events, ledgerLanded(world, seen)...)
	events = append(events, ledgerMade(world, input.made)...)
	if input.learned > 0 || input.letGo > 0 {
		parts := []string{}
		if input.learned > 0 {
			parts = append(parts, fmt.Sprintf("learned %d %s", input.learned, switcherPlural(input.learned, "thing", "things")))
		}
		if input.letGo > 0 {
			parts = append(parts, fmt.Sprintf("let go of %d", input.letGo))
		}
		events = append(events, switcherRow{kind: switcherLedger, title: strings.Join(parts, ", "), place: "memory", at: r.now})
	}
	if len(events) == 0 {
		return
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.After(events[j].at) })
	r.ledger = events
}

// switcherResumeWord is the undoing of a pause, and it is spelled here because
// nothing else on this surface offers it: an item's card and the standing place
// both pause and stop, and only a row that is ALREADY paused has a resume.
const switcherResumeWord = "resume it"

// switcherMarginWord is a row's right margin: THE ONE THING THAT DECIDES WHAT
// ENTER WILL DO, and an age when nothing does. A folder that is gone outranks a
// window holding the row, which outranks this window's own — worst news first,
// because that is the order a person needs them in. The grid's panels say it
// from here too (homepanel_recent.go).
func switcherMarginWord(row switcherRow) string {
	switch {
	case row.gone:
		return homeGoneShort
	case row.coming:
		return takeoverComingWord
	case row.held:
		return homeHeldShort
	case row.here:
		return homeHereWord
	}
	return row.age
}

func switcherSides(width int, left, right string, leftInk, rightInk func(string) string) string {
	if right == "" {
		return leftInk(fit(left, width))
	}
	if ansi.StringWidth(right) >= width {
		return rightInk(fit(right, width))
	}
	room := width - ansi.StringWidth(right) - 1
	l, lw := fitWidth(left, room)
	return leftInk(l) + strings.Repeat(" ", max(1, width-lw-ansi.StringWidth(right))) + rightInk(right)
}

// switcherVerbsFor is the same answer taken from a row rather than from its
// position, which is what the surface holding these rows as lines of its own
// column needs (place_home.go).
func switcherVerbsFor(row switcherRow) []switcherVerb {
	if row.kind == switcherStanding {
		verbs := switcherQuestionVerbs(row.options)
		if row.paused {
			return append(verbs, switcherVerb{key: 'r', word: switcherResumeWord})
		}
		// ONE SPELLING FOR ONE VERB. `pause` is what an item's own card and the
		// standing place both call this act (homestanding.go's [homeItemActions],
		// place_standing.go's [placeStanding.verbs]), and a strip that said it a second
		// way would be two words for one thing on one screen.
		return append(verbs, switcherVerb{key: 'p', word: homeItemPauseWord})
	}
	if row.kind != switcherConversation {
		return nil
	}
	verbs := switcherQuestionVerbs(row.options)
	word := "close"
	if row.session.Archived {
		word = "reopen"
	}
	verbs = append(verbs, switcherVerb{key: 'x', word: word}, switcherVerb{key: 'c', word: "copy name"})
	if strings.TrimSpace(row.session.Workspace) != "" || strings.TrimSpace(row.session.ProjectDir) != "" {
		verbs = append(verbs, switcherVerb{key: 'n', word: "new in project"}, switcherVerb{key: 'o', word: "open folder"})
	}
	return verbs
}

// switcherQuestionVerbs is 1b's answer-in-place: the question's OWN option
// words, on the question's OWN keys, carrying the option key the answer has to
// be sent under.
//
// THE KEYS COME OFF THE OPTIONS AND ARE NEVER POSITIONAL. This strip used to
// draw `y` on whatever answer happened to be first and `n` on whatever happened
// to be second, and that is a lie the moment a lane orders its answers any
// other way: [session.AnswerOptions] gives the consent lane `1 allow once ·
// 2 always · 3 deny`, so the strip offered `y always` — a widening approval on
// the key a person presses for yes. The option carries the key the ANSWER is
// sent under ([session.AnswerOption.Key]) and there is no second opinion about
// it; drawing anything else is drawing a key that means something other than
// what it does.
//
// TWO, AND NEVER THE WHOLE LIST. A strip is one row of the frame and a question
// with five options would push the list down by two more; the digits still
// answer every one of them, on the row, because they are drawn there
// (homeband_answer.go).
func switcherQuestionVerbs(options []session.AnswerOption) []switcherVerb {
	var verbs []switcherVerb
	for _, option := range options {
		if len(verbs) >= switcherQuestionVerbCap {
			break
		}
		key := strings.TrimSpace(option.Key)
		word := strings.TrimSpace(option.Label)
		if key == "" || word == "" || utf8.RuneCountInString(key) != 1 {
			// A KEY THIS STRIP CANNOT DRAW IS AN ANSWER IT DOES NOT OFFER. The
			// strip's own grammar is one rune per verb ([switcherVerb.key]), and
			// an answer whose key is a word — a chord, a name — is still
			// answerable by opening the question; it is only this one row that
			// has no cell for it.
			continue
		}
		rune, _ := utf8.DecodeRuneInString(key)
		verbs = append(verbs, switcherVerb{key: rune, word: word, answer: option.Key})
	}
	return verbs
}

// switcherQuestionVerbCap is how many of a question's answers reach the strip.
const switcherQuestionVerbCap = 2
