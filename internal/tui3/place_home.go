package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE HOME PLACE ──────────────────────────────────────────────────────────
//
// THIS FILE IS HOME AS A PLACE (docs/design/home-rethink/ARCHITECTURE.md): the
// switcher it draws, the two views it can be shown in, the verbs its rows offer,
// and the card beside them. The reading is switcher.go's and is PURE; the frame
// around it is the router's and knows no place by name; nothing here switches on
// a page id, and the shared router files carry a call into this file rather than
// a home-shaped body.
//
// ── WHERE HOME'S `place` METHODS ALREADY LIVE ───────────────────────────────
//
// The interface itself is the refactor lane's, and these are the functions it
// binds to, written down here so that lane has one list rather than a search:
//
//	id       pageHome
//	open     [app.openHome]        home.go — reads the world, the bands and the
//	                               ledger once, then builds
//	close    [app.closeHome]       home.go — writes the look stamp
//	tick     [app.refreshHome]     home.go — the three-second beat
//	body     [app.homeBody]        home.go, with [app.homeSwitchCard] here
//	stops    [homeLine.stop]       home.go
//	enter    [app.homeEnter]       home.go — the doors and their refusals
//	verbs    [app.homeRowVerbs]    HERE
//	alt      [app.homeAlt]         HERE
//	window   —                     home has no time window
//	box      [homeView.box]        the one foot box: filter and message at once
//	note     —                     home says its count on the section line
//	hint     [app.homeHint]        home.go
//	changed  —                     the per-place look stamps are another lane's
//
// The state struct is [homeView] (home.go), which the refactor lane renames; it
// is not moved here in this wave because four hundred lines of doors, clock and
// typed surface still hold it.
//
// ── HOME IS A SWITCHER, NOT A DIRECTORY ─────────────────────────────────────
//
// Home used to be a tree: every project a heading, its conversations under it,
// three open and the rest folded away under a rule, with two attention strips
// standing over the whole thing. That shape answers "where is my work", and the
// person opening this screen twenty times a day is not asking that. Seconds
// between opens, ten to twenty live chats, three to five projects — so the tree
// bought five rows of scaffolding to reach twenty leaves, and the two strips
// said a second time what the list was already saying once.
//
// SO THE LIST IS ONE FLAT RANKED LIST (SCREEN 1a): what needs you, then what is
// moving, then what is quiet, with the project demoted to a tag on the row and
// the right-hand note carrying the one fact the card was really for. NEEDS-YOU
// AND MOVING ARE THE SORT ORDER NOW, which is why the strips are gone rather
// than moved: a summary standing over a list sorted the same way is the same
// reading twice.
//
// THE READING IS switcher.go's AND THIS FILE OWNS ONLY THE WIRING. [readSwitcher]
// is pure — a world, the standing bands, a look stamp and a clock in, a list of
// lines out — and everything this file does is turn those lines into lines of
// home's own column so that every door, card, digit and key that already worked
// on a conversation or a standing item goes on working on it untouched. A
// switcher row for a conversation IS a [homeSession] row; a switcher row for a
// watch IS a [homeItem] row. What it carries in addition is [homeLine.sw], the
// reading's own line, which is what paints it.
//
// AND TYPING IS UNTOUCHED. The moment there is something in the box the column
// is [homeView.buildWorld]'s drop-up again, ranked by [homeRank], with `ask
// here` and the action row against the foot — "type to search or start" is the
// promise the foot has always made and this wave does not touch it.

// The row kinds the switcher adds, declared HERE and given values far above the
// iota block in home.go for [homePlace]'s reason: that block is edited by other
// lanes in the same wave, and a constant appended to it would be a conflict over
// a line that says nothing.
const (
	// homeLedger is one line of `since you left` — a watch that fired, work that
	// landed, something memory learned. IT IS A DOOR AND THAT IS THE WHOLE POINT
	// (SCREEN 1a): discoverability on this machine is solved by events rather
	// than by inventories, so you learn the memory place exists on the day it
	// tells you it learned something, and enter on the line goes there.
	homeLedger homeRowKind = 241
	// homeSwitchFold is the one fold at the foot of the list — `▸ 15 more, quiet
	// since aug 21` — and it is a door exactly as [homeQuiet] was, with the same
	// two marks and the same gestures.
	homeSwitchFold homeRowKind = 242
	// homeSwitchHead is a line of the reading that names rather than opens: the
	// `20 chats · what wants you first` claim, the `since you left · 3h` heading,
	// and a project's name while `alt+g` is grouping. It is not a cursor stop,
	// for [homeHeading]'s reason.
	homeSwitchHead homeRowKind = 243
)

// ── the reading ─────────────────────────────────────────────────────────────

// buildSwitch is the resting column: the errands, then the reading, as lines of
// home's own list.
//
// EVERY FACT IN IT WAS ALREADY READ. The world, the standing bands and the look
// stamp are the same three readings this screen has always been built from, on
// the same three-second beat ([homeEvery]); the two memory figures come in
// through [app.readSwitchLedger] on that same beat and never on a draw. This
// function opens nothing and stats nothing.
func (h *homeView) buildSwitch() {
	// EVERY PROJECT, INCLUDING THE ONES HOME KNOWS ONLY THROUGH A WATCH. A
	// workspace nobody has spoken in is exactly as able to need somebody as a busy
	// one ([homeView.everyProject], homestanding.go's [app.readBareBands]), and a
	// reading that walked the world alone would show a person nothing on the one
	// screen that exists to say what is true — which is the defect that reader was
	// written to close.
	world := h.world
	world.Projects = h.everyProject()
	h.reading = readSwitcher(world, h.items, switcherHere{session: h.here, project: h.bucket}, h.gone, h.seen, h.world.Read,
		switcherView{grouped: h.grouped, hideQuiet: h.hideQuiet, all: h.moreOpen}, h.ledger)
	// THE ERRANDS STAND OVER THE READING AND ARE NOT IN IT. An `ask here` errand
	// is a live conversation with the person's own question in it and no row in
	// the world at all ([homeExchange] — they are kept outside v3/projects on
	// purpose), so the ranked list cannot hold one. They go where the thing you
	// asked for a minute ago belongs: at the top, above everything the machine
	// has to say for itself.
	errands := h.switchExchanges()
	h.lines = append(h.lines, errands...)
	if len(errands) > 0 && len(h.reading.lines) > 0 {
		h.lines = append(h.lines, homeLine{kind: homeBlank})
	}
	for i := range h.reading.lines {
		h.lines = append(h.lines, h.switchLine(&h.reading.lines[i]))
	}
	// A MACHINE WITH NOTHING ON IT STILL SAYS SO WHERE ITS FIRST ROW WOULD BE
	// ([homeEmptyRow]) — an empty home is the same screen with fewer rows, never
	// a different screen. A machine whose rows CANNOT be read from here says why
	// instead, in the same slot and the same dim register ([homeView.why]).
	if len(h.lines) == 0 {
		empty := homeEmptyLines()
		if h.why != "" {
			empty = []string{h.why}
		}
		for _, part := range empty {
			h.lines = append(h.lines, homeLine{kind: homeEmptyRow, project: part})
		}
	}
}

// switchLine is one line of the reading as a line of home's column.
//
// A CONVERSATION'S ROW IS A [homeSession] ROW AND A WATCH'S IS A [homeItem] ROW,
// and that is the whole reason this wave did not have to touch a single door.
// enter, ctrl+t, ctrl+o, ctrl+y, ctrl+e, ctrl+x, the digits that answer a
// question and the card the registry draws all ask the line what KIND it is, and
// the answer is the same answer it has always been. What is new on the line is
// [homeLine.sw], which is what paints it.
func (h *homeView) switchLine(line *switcherLine) homeLine {
	out := homeLine{sw: line}
	if line.row == nil {
		if line.blank {
			out.kind = homeBlank
			return out
		}
		out.kind = homeSwitchHead
		return out
	}
	row := line.row
	switch row.kind {
	case switcherConversation:
		out.kind, out.row, out.project = homeSession, row.session, row.project
		out.dir = homeBucketOf(row.session.Transcript)
	case switcherStanding:
		out.kind, out.view, out.item, out.project = homeItem, row.item, row.item.Item, row.project
	case switcherLedger:
		// THE PLACE THE LINE IS A DOOR TO RIDES [homeLine.project], which is the
		// same field an offered place carries its word in (homeplaces.go). One
		// field, one meaning: the lowercase name of somewhere to go.
		out.kind, out.project = homeLedger, row.place
		out.view, out.item = row.item, row.item.Item
	case switcherFold:
		out.kind, out.folded, out.quiet = homeSwitchFold, !h.moreOpen, h.reading.hidden
	}
	return out
}

// switchExchanges is every errand this screen is holding, in the order the
// project blocks used to draw them in: what wants you, then what is moving, then
// what is done, older first inside each.
//
// It is [homeView.exchangeLines] with the project taken out of it, because there
// are no project blocks at rest any more — an errand belongs to the machine's
// one list, not to a heading it happened to be asked under.
func (h *homeView) switchExchanges() []homeLine {
	if len(h.exchanges) == 0 {
		return nil
	}
	mine := append([]*homeExchange(nil), h.exchanges...)
	sort.SliceStable(mine, func(i, j int) bool {
		if a, b := exchangeRank(mine[i]), exchangeRank(mine[j]); a != b {
			return a < b
		}
		return mine[i].began.Before(mine[j].began)
	})
	lines := make([]homeLine, 0, len(mine))
	for _, ex := range mine {
		lines = append(lines, homeLine{kind: homeExchangeRow, dir: ex.bucket, ex: ex})
	}
	return lines
}

// ── what the memory place has to say for itself ─────────────────────────────

// readSwitchLedger takes the two figures the ledger's memory line is made of —
// how many things this machine learned since the look stamp, and how many it let
// go of — once per reading of the world, where every other disk-backed fact on
// this screen is taken.
//
// ON HOME'S OWN BEAT AND NEVER ON A DRAW. There is SQLite behind that seam
// (place_memory.go's [memoryStore]), and the law every home reader is held to is
// that a draw never touches a disk (tui3.go). A window with no store, or a store
// that could not be read, answers nothing — and the ledger then draws no memory
// line at all, which is the emptiness law rather than a gap.
func (a *app) readSwitchLedger() {
	a.home.ledger = switcherLedgerInput{}
	if a.memory == nil || a.home.seen.IsZero() {
		return
	}
	learned, letGo, err := a.memory.ChangedSince(a.home.seen)
	if err != nil {
		return
	}
	a.home.ledger = switcherLedgerInput{learned: learned, letGo: letGo}
}

// ── the two views ───────────────────────────────────────────────────────────

// homeAlt is `alt+g` and `alt+q`: the two things this place can be shown
// differently as, and the only two letters home declares to the router's
// alt+<letter> class (placekeys.go's [app.placeAlt]).
//
// BOTH ARE REMEMBERED FOR THE PROCESS AND NEITHER IS A SETTING. A person who
// groups the list expects it grouped the next time they open home in this
// terminal; they do not expect to have found a preference they now own and have
// to maintain. So the two flags live on the app — which closing home does not
// clear — and nothing writes them to a disk.
func (a *app) homeAlt(letter rune) bool {
	if !a.at(pageHome) || a.home.phone || a.home.searching() {
		// A QUERY HAS NO GROUPING TO TOGGLE. While something is typed the column
		// is the drop-up of matches ([homeView.buildWorld]), and a key that
		// silently changed a list that is not on the screen would be the worst
		// kind of chord — one that does something you cannot see.
		return false
	}
	switch letter {
	case 'g':
		a.switchGrouped = !a.switchGrouped
		a.home.grouped = a.switchGrouped
	case 'q':
		a.switchQuiet = !a.switchQuiet
		a.home.hideQuiet = a.switchQuiet
	default:
		return false
	}
	a.home.build()
	return true
}

// ── the doors ───────────────────────────────────────────────────────────────

// homeLedgerEnter is enter on a `since you left` line: GO TO THE PLACE THAT OWNS
// IT.
//
// A line that says a watch fired and cannot be asked about that watch is a
// notification, and this surface does not have notifications. The word in the
// right margin IS the door, which is why the two are one field.
func (a *app) homeLedgerEnter(line homeLine) tea.Cmd {
	id, ok := parsePageWord(line.project)
	if !ok {
		return nil
	}
	return a.showPage(id)
}

// foldSwitch is enter or an arrow on the fold at the foot: show every row, or
// fold them back away. It is a DOOR and not a setting, which is why it dies with
// the screen where `alt+g` and `alt+q` do not.
//
// AND IT LEAVES THE CURSOR ON THE LINE THAT DID IT, so the gesture can be
// reversed without moving — the law every other fold on this column keeps
// (home.go's [homeView.fold], [homeView.foldItems]). [homeView.build] follows a
// conversation, an item, a project or an errand and knows nothing about a fold,
// so without this the key that opened the list would throw the hand to the top
// of it and `←` would have nothing under it to close.
func (h *homeView) foldSwitch(open bool) {
	h.moreOpen = open
	h.build()
	for at, line := range h.lines {
		if line.kind == homeSwitchFold {
			h.cursor, h.picked = at, true
			return
		}
	}
}

// ── the strip ───────────────────────────────────────────────────────────────

// homeRowVerbs is `→` on a row of home: the verbs the READING itself says this
// row has, wired to the doors home already had for them.
//
// IT IS HOME'S HALF OF THE STRIP AND IT LIVES HERE. verbstrip.go knows the
// mechanism — a letter is a verb only while the strip naming it is drawn — and
// carries one call into this file for the place it is standing in.
//
// THE READING NAMES THEM AND THIS FUNCTION ONLY WIRES THEM. A conversation that
// is not asking anything has no `y`; a row with no address has no `open folder`;
// a paused watch offers `resume it` where a running one offers `pause it`. That
// is [switcherVerbsFor]'s law, and a strip that invented a verb here could offer
// one the row has no way to perform.
func (a *app) homeRowVerbs() []verb {
	line, ok := a.home.previewLine()
	if !ok {
		return nil
	}
	if line.sw == nil || line.sw.row == nil {
		// A ROW THE TYPED SURFACE BUILT, WHICH THE READING NEVER SAW. Under a
		// query the column is [homeRank]'s drop-up and a standing item's row is
		// the one thing on it with verbs — the two actions home has been
		// ADVERTISING on such a row without binding (`homeItemActions`,
		// homestanding.go), bound to ctrl+e and ctrl+x, which the line never
		// named, and whose bare `p` and `s` typed.
		if line.kind != homeItem {
			return nil
		}
		return []verb{
			{key: 'p', word: homeItemPauseWord, do: func() tea.Cmd { return a.homeItemWrite(line, standing.StatusPaused) }},
			{key: 's', word: homeItemStopWord, do: func() tea.Cmd { return a.homeItemWrite(line, standing.StatusRetired) }},
		}
	}
	row := *line.sw.row
	var verbs []verb
	for _, v := range switcherVerbsFor(row) {
		// A VERB THAT CANNOT WORK IS ABSENT, NOT BROKEN. Two of the doors want a
		// folder — a fresh conversation rooted in it, and handing it to the
		// machine's file manager — and a row whose folder is not there any more
		// would offer two keystrokes it has already decided against. It is the
		// place that drops them and not the reading: the reading is pure and has
		// no disk, and this is what the cached stat map is for.
		if row.gone && (v.key == 't' || v.key == 'o') {
			continue
		}
		verbs = append(verbs, a.homeSwitchVerb(line, row, v))
	}
	return verbs
}

// homeSwitchVerb is one of them, given the door it names.
func (a *app) homeSwitchVerb(line homeLine, row switcherRow, v switcherVerb) verb {
	do := func() tea.Cmd { return nil }
	switch {
	case v.answer != "":
		// 1b's ANSWER IN PLACE: the question's own option words, sent down the
		// road the digits already ride (homeband_answer.go's [app.sendAnswer]),
		// so a question answered from the strip and the same question answered
		// with `1` are one act with one record.
		do = func() tea.Cmd {
			live := a.homeTrue(row.session)
			question, ok := answerable(live, a.now())
			if !ok {
				return nil
			}
			cmd, _ := a.sendAnswer(live, question, v.answer)
			return cmd
		}
	case v.key == 'a':
		do = func() tea.Cmd { return a.homeArchiveRow(row.session) }
	case v.key == 't':
		do = func() tea.Cmd { return a.homeStart(homeWhere(line)) }
	case v.key == 'o':
		do = func() tea.Cmd { return a.homeOpenFolder(row.session) }
	case v.key == 'c':
		do = func() tea.Cmd { return a.homeCopyPath(row.session) }
	case v.key == 'p':
		do = func() tea.Cmd { return a.homeItemWrite(line, standing.StatusPaused) }
	case v.key == 'r':
		do = func() tea.Cmd { return a.homeItemWrite(line, standing.StatusActive) }
	}
	return verb{key: v.key, word: v.word, do: do}
}

// homeArchiveRow is `a put it away` — the same write `ctrl+e` makes, said once
// so the key and the strip can never mean two different things.
func (a *app) homeArchiveRow(row session.SessionRow) tea.Cmd {
	if err := session.SetArchived(row.Dir, !row.Archived); err != nil {
		a.home.say("could not put it away", "")
		return nil
	}
	if row.Archived {
		a.home.say("brought back", "")
	} else {
		a.home.say(homePutAwayWord, "")
	}
	a.refreshHome()
	return nil
}

// homeOpenFolder is `o open folder`, and homeCopyPath is `c copy path` — the
// same two doors ctrl+o and ctrl+y are.
func (a *app) homeOpenFolder(row session.SessionRow) tea.Cmd {
	path := strings.TrimSpace(row.Workspace)
	if path == "" || processOpener(path) != nil {
		a.home.say("could not open "+path, "")
		return nil
	}
	a.home.say("opened "+path, path)
	return nil
}

func (a *app) homeCopyPath(row session.SessionRow) tea.Cmd {
	path := strings.TrimSpace(row.Workspace)
	if path == "" {
		a.home.say("could not copy path", "")
		return nil
	}
	a.home.say("copied "+path, path)
	return tea.Raw(osc52(path, a.tmux))
}

// homePutAwayWord is what putting a conversation away says, in one place because
// the key and the strip both say it.
const homePutAwayWord = "put away · type its name to find it again"

// ── the card ────────────────────────────────────────────────────────────────

// ── the card at ≥160 columns (SCREEN 1d) ────────────────────────────────────
//
// SIXTEEN REGISTERED BANDS BECAME FIVE, AND EVERY ONE EITHER ASKS YOU SOMETHING
// YOU CAN ANSWER HERE OR POINTS AT A PAGE. That is the whole selection rule. The
// card exists only past [homeCardMin], where the width is genuinely spare — and
// a card drawn on spare width has to be worth more than the row it is beside, so
// it may not be a second reading of what the row already says. What went:
//
//   - `state` and `leftoff` — what a conversation is doing and the last thing it
//     said. The row's own note carries exactly that now
//     ([switcherConversationNote]), one column over, for a person who is not
//     pointing at anything.
//   - `news`, `nextup`, `watchlist`, `sinceleft`, `agents`, `today` — every one of
//     them is a thing that happened by itself, and every one of them is a line of
//     the ledger at the top of the list or a row of a place the ledger opens.
//   - `gone` and `repo` — folded into the place line, where the address they are
//     about is.
//   - `spend` and `thinking` — folded into one facts line ([app.homeCardFacts]).
//   - `keys` — the legend became the `→ verbs` hint, because the letters live on
//     the strip and only while the strip is drawn (SCREEN 3a).
//
// THE ORDER AND THE WORDING ARE THE DESIGN'S, EXACTLY (FIDELITY.md item 8): the
// title, the place line, `it is stopped on you` with the question and its answer
// keys, `work` with `▸ N more tasks` naming the tasks place, `made for you` with
// the path, the facts line, then `→ verbs`.

// The card's three section words. Each is quoted in the manual exactly as it is
// spelled here, and each names what is UNDER it rather than what kind of band it
// is — `made for you` and not `deliverables`, because the card is read by a
// person and not by the registry.
const (
	homeCardStoppedWord = "it is stopped on you"
	homeCardWorkWord    = "work"
	homeCardMadeWord    = "made for you"
	// homeCardTalkWord is the third thing that can be done with a question a
	// person is looking at, and 1d draws it on the end of the answer keys:
	// `y yes · n no · enter open and talk`.
	//
	// IT IS A KEY THE CARD MAY NAME, unlike the letters. A letter is a verb only
	// while the strip naming it is on screen, which is why the card says what can
	// be done and never which letter does it ([app.homeCardVerbs]); `enter` is
	// bound on this row whatever is typed, so naming it promises nothing the
	// composer is about to eat. And it is the honest third option: the chips
	// answer the question from here, and this opens the conversation that asked
	// it — which is what somebody who needs the rest of the card has to do.
	homeCardTalkWord = "enter open and talk"
)

// homeCardTasks is how many pieces of work the card shows before the rest fold.
// Three, because the fold's whole point is to name the place that holds the
// rest, and a card is not that place.
const homeCardTasks = 3

// homeSwitchCard is the card beside the switcher: the title, the place, the five
// bands, and the hint that names the strip.
//
// IT IS ASSEMBLED BY [homeBands] LIKE EVERY OTHER CARD ON THIS SCREEN, so a
// short frame drops whole bands from the bottom and the title never goes at all.
func (a *app) homeSwitchCard(line homeLine, width, room int, pal palette) []string {
	row := line.row
	ctx := bandContext{
		subject: bandSubject{kind: bandKindSession, row: row, project: line.project,
			dir: strings.TrimSpace(row.ProjectDir), world: a.home.world},
		width: width, now: a.home.world.Read, pal: pal,
	}
	bands := [][]string{{pal.bold(pal.ink(fit(homeName(row), width)))}}
	for _, band := range [][]string{
		a.homeCardPlace(row, width, pal),
		a.homeCardAnswer(ctx),
		a.homeCardWork(ctx),
		a.homeCardMade(ctx),
		a.homeCardFacts(ctx),
		a.homeCardVerbs(width, pal),
	} {
		if len(band) > 0 {
			bands = append(bands, band)
		}
	}
	return homeBands(bands, room)
}

// homeCardPlace is the card's second line: WHERE this conversation is, where its
// repository stands, and whether it is the one this window is holding.
//
//	~/aforge-v2 · master, 1 file dirty · here
//
// THE REPOSITORY IS ON THE PLACE LINE RATHER THAN IN A BAND OF ITS OWN, because
// a branch and a dirty count are facts ABOUT that address and a band between the
// address and them would be saying the same address twice. The reading itself is
// still homeband_repo.go's — one bounded `git status` per workspace per
// [homeRepoTTL], taken when a card arrives and never on a draw.
//
// AND THE REPOSITORY'S OWN CLAUSES ARE JOINED WITH COMMAS. The line is three
// things — the address, the state of the repository there, and the door word —
// separated by ` · `; inside the middle one, `master, 1 file dirty` is one
// clause about one repository, and a second `·` there would read as a fourth
// thing (SCREEN 1d spells it exactly this way).
//
// AND A FOLDER THAT IS NOT THERE ANY MORE SAYS SO HERE, in place of the branch
// it cannot have: the refusal belongs against the address it is about
// ([homeGoneWord] is what the door says too).
func (a *app) homeCardPlace(row session.SessionRow, width int, pal palette) []string {
	where := strings.TrimSpace(row.Workspace)
	if where == "" {
		where = strings.TrimSpace(row.ProjectDir)
	}
	if where == "" {
		return nil
	}
	parts := []string{where}
	switch {
	case a.homeGone(where):
		parts = append(parts, homeGoneWord)
	default:
		if repo := a.home.repos[where]; repo.line != "" {
			parts = append(parts, strings.Join(strings.Split(repo.line, " · "), ", "))
		}
	}
	switch {
	case a.homeMark(row) == markHere:
		parts = append(parts, homeHereWord)
	case row.Open || row.Live:
		parts = append(parts, a.homeHolding(row))
	}
	// THE WHOLE LINE IS A DOOR (pathlink.go), and the anchor covers all of it
	// rather than the path half: the address and what is true about it are one
	// reading, and a link that stopped at the first clause would be a target a
	// narrow card had already cut off.
	return []string{pal.dim(a.pathLink(where, fitLeft(strings.Join(parts, " · "), width)))}
}

// homeCardAnswer is the band that ACTS: what this conversation is stopped on,
// the question in its own words, and the keys that answer it from here.
//
//	it is stopped on you
//	Add a --report-only mode so the report can be
//	regenerated without re-running the sweep?
//	1 do it   2 leave it
//
// THE LEAD LINE IS WHY THE CARD IS WORTH ITS CELLS. A card that drew the answer
// keys alone would be asking a person to answer a question it had not asked;
// the row's note says `asks: …` cut to one line, and this is the place the whole
// of it fits.
func (a *app) homeCardAnswer(ctx bandContext) []string {
	keys := drawAnswerBand(a, ctx)
	if len(keys) == 0 {
		return nil
	}
	question, ok := answerable(a.homeTrue(ctx.subject.row), ctx.now)
	if !ok {
		return keys
	}
	rows := []string{ctx.pal.dim(fit(homeCardStoppedWord, ctx.width))}
	for _, said := range wrap(switcherFirstLine(question.Text), ctx.width) {
		rows = append(rows, ctx.pal.ink(said))
	}
	return append(rows, homeCardTalkTail(keys, ctx.width, ctx.pal)...)
}

// homeCardTalkTail puts `enter open and talk` on the end of the answer keys, on
// their own row when the card is too narrow to carry both.
//
// THE CLAUSE IS DIM AND THE CHIPS ARE NOT, because they are two different
// offers: the chips ANSWER the question from here and wear the one hue this
// screen paints "waiting on you" in, and this is the way out to the conversation
// that asked it. A third amber chip would read as a third answer.
func homeCardTalkTail(keys []string, width int, pal palette) []string {
	if len(keys) == 0 {
		return keys
	}
	last := len(keys) - 1
	tail := pal.dim(answerChipGap + homeCardTalkWord)
	if ansi.StringWidth(keys[last])+ansi.StringWidth(answerChipGap+homeCardTalkWord) <= width {
		keys[last] += tail
		return keys
	}
	return append(keys, pal.dim(fit(homeCardTalkWord, width)))
}

// homeCardWork is what this conversation had run and what it came to, and the
// door onto the rest of it.
//
//	work
//	✓ toy-scale validation of decomposition          $1.63
//	▸ 3 more tasks                                   tasks
//
// THE FOLD NAMES THE PLACE THAT HOLDS THE REST, in the right margin every row of
// this surface says where it goes in. Anything that grows says `▸ N more` and
// names its page (SCREEN 1d); a card is not the tasks place and must not pretend
// to be one.
func (a *app) homeCardWork(ctx bandContext) []string {
	row, pal := ctx.subject.row, ctx.pal
	var drawn [][]string
	for _, entry := range row.Tasks.Rows {
		label := strings.TrimSpace(entry.Label)
		if label == "" {
			label = strings.TrimSpace(entry.Title)
		}
		cost := ""
		if entry.Cost > 0 {
			cost = dollars(entry.Cost)
		}
		lead := a.homeTaskGlyph(entry, row) + " "
		body := bandSides(ctx.width-2, 0, 8, label, cost, pal.muted, placeMoneyInk(pal))
		if len(body) == 0 {
			continue
		}
		body[0] = lead + body[0]
		// AND A TASK THAT IS NOT DONE SAYS WHY, UNDER ITS OWN NAME. The design's
		// example (SCREEN 1d) is a card of landed work, where the mark and the
		// figure are the whole row; a run that failed, gave up or was cut off has
		// something a person has to read, and a card that drew it as one more
		// tick with a price on it would be the screen calling every outcome the
		// same outcome.
		if homeTaskWord(entry, row) != doneWord {
			body = append(body, homeWorkUnder(entry, row, ctx.width, pal)...)
		}
		drawn = append(drawn, body)
	}
	if len(drawn) == 0 {
		return nil
	}
	// AND THE HEADING CARRIES THE ONE CAPTION THIS BAND HAS EVER HAD. Work that
	// landed since home was last closed is NEWS — the delta the look stamp buys
	// (home.go's [homeView.seen]) — and it is said once over the whole band rather
	// than on each row, out at the right margin where every line of this surface
	// says the thing that is true of what is under it. A first look, with no stamp
	// to measure from, captions nothing.
	head := pal.dim(fit(homeCardWorkWord, ctx.width))
	if a.homeFresh(row) > 0 {
		head = switcherSides(ctx.width, homeCardWorkWord, homeFreshWord, pal.dim, pal.dim)
	}
	rows := []string{head}
	shown := drawn
	if len(drawn) > homeCardTasks {
		shown = drawn[:homeCardTasks]
	}
	for _, group := range shown {
		rows = append(rows, group...)
	}
	if more := len(drawn) - len(shown); more > 0 {
		rows = append(rows, switcherSides(ctx.width,
			foldLine(more, "")+" "+switcherPlural(more, "task", "tasks"),
			pageTasks.word(), pal.dim, pal.dim))
	}
	return rows
}

// homeCardMade is the files this conversation left behind, under the words a
// person would use for them.
func (a *app) homeCardMade(ctx bandContext) []string {
	rows := a.drawHomeBandNamed("deliverables", ctx)
	if len(rows) == 0 {
		return nil
	}
	return append([]string{ctx.pal.dim(fit(homeCardMadeWord, ctx.width))}, rows...)
}

// drawHomeBandNamed draws the one registered band with this name, and nothing
// at all when nothing is registered under it.
//
// IT ASKS THE REGISTRY RATHER THAN THE FUNCTION, so a band stays one file that
// says what it is about and this list stays a list of names. A name nothing
// answers to draws nothing, which is what makes deleting a band a one-file
// change rather than a two-file one.
func (a *app) drawHomeBandNamed(name string, ctx bandContext) []string {
	for _, band := range homeBandsFor(ctx.subject.kind) {
		if band.name == name {
			return band.draw(a, ctx)
		}
	}
	return nil
}

// homeCardFacts is `spend` and `thinking` on ONE line — what this cost, and the
// rung work started here would think at.
//
//	spent $1.63 · 3.6M tokens · thinking high
//
// TWO BANDS BECAME ONE LINE because they are one sentence: both are arithmetic
// about the thing on the card, and a blank row between "what it cost" and "how
// hard it thinks" was a paragraph break inside a clause.
//
// THE RUNG IS THE INSTALL'S AND THE CARD DOES NOT OFFER TO MOVE IT. A
// conversation's own rung is its own sticky setting and belongs to the window
// that session is open in, which is why ctrl+v deliberately does nothing here
// (home.go) and why the legend never named a key for it. What the line states is
// the rung a task started from this card would think at, which is the fact a
// person reading a bill wants beside it.
func (a *app) homeCardFacts(ctx bandContext) []string {
	clauses := []string{}
	if facts := homeFacts(ctx.subject.row, ctx.now); facts != "" {
		clauses = append(clauses, strings.Split(facts, " · ")...)
	}
	if dir, ok := a.effortProfile(); ok {
		if clause := effortClause(config.DefaultEffortAt(dir)); clause != "" {
			clauses = append(clauses, clause)
		}
	}
	if len(clauses) == 0 {
		return nil
	}
	return bandClauses(ctx.width, 0, ctx.pal.dim, clauses...)
}

// homeCardVerbs is the card's last line: `→ verbs: new chat here, put it away`.
//
// IT NAMES THE KEY AND THE WORDS AND NEVER THE LETTERS. A letter is a verb only
// while the strip naming it is on screen (SCREEN 3a), and a card that printed
// `t new chat here` would be advertising a keystroke the composer is about to
// eat. So the card says what can be done and the strip says what to press.
func (a *app) homeCardVerbs(width int, pal palette) []string {
	verbs := a.homeRowVerbs()
	if len(verbs) == 0 {
		return nil
	}
	words := make([]string, 0, len(verbs))
	for _, v := range verbs {
		words = append(words, v.word)
	}
	return bandClauses(width, 0, pal.dim, homeVerbsWord+": "+strings.Join(words, ", "))
}

// ── the place ───────────────────────────────────────────────────────────────

// placeHome is this place's handle on the registry (pages.go's [place] states
// the contract and why the handle holds no state of its own). Home's state is
// [homeView], which is home.go's, because home is the oldest surface here and
// the one every other place borrowed its laws from.
type placeHome struct{ placeBase }

func init() { registerPlace(placeHome{}) }

func (placeHome) id() page      { return pageHome }
func (placeHome) word() string  { return "home" }
func (placeHome) counted() bool { return true }

func (placeHome) open(a *app) tea.Cmd { return a.raiseHome() }
func (placeHome) close(a *app)        { a.dropHome() }

// tick is home's own three-second beat, and home arms and reads it itself
// ([app.refreshHome], home.go's [homeEvery]) — the clock every other place
// borrowed. There is nothing for the router's beat to do here.

// body is home's own column, and the pane map beside it: two facts per row, so
// the hit is a [homeMark] rather than a line number.
func (placeHome) body(a *app, width, room int) []placeRow {
	left, right := homeColumns(width)
	a.homeWindow(room)
	body := a.homeBody(left, right, room, a.pal)
	rows := make([]placeRow, 0, len(body))
	for _, drawn := range body {
		rows = append(rows, placeRow{
			text: drawn.text,
			hit:  homeMark{line: drawn.hit, pane: drawn.pane},
		})
	}
	return rows
}

// ownFrame is home's own, for two reasons and not one. Below sixty columns this
// screen is an inbox and a sheet rather than a list (homephone.go), and at every
// width home unpacks a SECOND hit map — the pane each row shares with an errand
// drawn beside it — which has to be taken off the frame AFTER the clamp that
// cuts rows, so that both maps are cut the same way.
func (placeHome) ownFrame(a *app, width, height int) ([]string, []placeHit, int, int, bool) {
	lines, hits, caretX, caretY := a.homeFrame(width, height)
	marks := make([]placeHit, len(hits))
	for i, at := range hits {
		marks[i] = at
	}
	return lines, marks, caretX, caretY, true
}

// stops is every line of home's column the cursor may rest on: the walk
// [homeView.move] takes, said as a list ([homeLine.stop] is the one rule).
func (placeHome) stops(a *app) []int {
	out := make([]int, 0, len(a.home.lines))
	for i, line := range a.home.lines {
		if line.stop() {
			out = append(out, i)
		}
	}
	return out
}

// cursorRow is which drawn row home's cursor landed on, found through the hit
// map the body just wrote. Home's rows carry two facts each — the list line and
// the errand pane sharing it — so the line is unpacked from [homeMark] rather
// than read as a bare index (pages.go's [place.cursorRow] says why the rows are
// handed in).
func (placeHome) cursorRow(a *app, rows []placeRow) int {
	for i, row := range rows {
		if mark, ok := row.hit.(homeMark); ok && mark.line == a.home.cursor {
			return i
		}
	}
	return -1
}

func (placeHome) enter(a *app) tea.Cmd { return a.homeEnter() }

func (placeHome) verbs(a *app) []verb { return a.homeRowVerbs() }

// rowID names the row home's cursor is standing on (pages.go's [place.rowID]).
//
// IT IS THE ROW'S OWN IDENTITY AND NEVER ITS POSITION. Home rebuilds its lines
// on every three-second beat and re-ranks them under every letter typed into
// the query, so the conversation at line nine is a different conversation a
// moment later — which is the whole reason [homeView.pointAt] finds a row by
// what it IS rather than by where it was. The name is the kind and whichever
// handle that kind of row carries, so two rows of one list cannot answer alike.
func (placeHome) rowID(a *app) string {
	line, ok := a.home.previewLine()
	if !ok {
		return ""
	}
	parts := []string{strconv.Itoa(int(line.kind)), line.project, line.dir, line.row.Transcript, line.item.ID}
	if line.ex != nil {
		parts = append(parts, line.ex.id)
	}
	if line.task != nil {
		parts = append(parts, line.task.SessionID, line.task.ID)
	}
	return strings.Join(parts, "\x00")
}

// alt is `alt+g` and `alt+q`: the two views home can actually be shown in
// ([app.homeAlt] holds the argument for why there are only two).
func (placeHome) alt(a *app, letter rune) bool { return a.homeAlt(letter) }

// box is home's own one foot box — new message AND live query at once, no mode —
// or a focused errand's line, because THE FOOT BELONGS TO WHOEVER HOLDS THE
// KEYBOARD (homeexchange.go).
func (placeHome) box(a *app) *editor {
	if ex := a.paneExchange(); ex != nil && ex.focused {
		return &ex.box
	}
	return &a.home.box
}

// hint is HOME'S WHOLE LINE, the router's own keys included. At rest that line is
// the design's sentence word for word and names four keys exactly (SCREEN 1a,
// home.go's [app.homeHint]); the router's tail appended here would make it five.
func (placeHome) hint(a *app) string { return a.homeHint() }

// changed is ZERO AND THAT IS THE DESIGN. Home is where the "since you left"
// ledger is DRAWN, in sentences that say what happened and open the place it
// happened in — so a digit on its tab would be the same news said twice, once
// uselessly.
func (placeHome) changed(a *app, since time.Time) int { return 0 }

// press, hover and wheel are home's own, because home resolves the pointer
// against two maps and a column boundary rather than against a body line
// (homemouse.go). The router hands the gesture straight over.
func (placeHome) press(a *app, y int) bool     { return false }
func (placeHome) hover(a *app, y int) bool     { return false }
func (placeHome) wheel(a *app, delta int) bool { return false }

// key is home's whole grammar, which is the oldest on this surface and the one
// every other place borrowed from (home.go's [app.homeKey]). The router is read
// before it, exactly as it is before every other place's.
func (placeHome) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.homeKey(msg) }

// owns is the two layers of home that take the WHOLE keyboard, `tab` included,
// and it is read before the router claims a single chord (pages.go's
// [place.owns] holds the argument).
//
// The phone tier's sheet over the inbox is the first (homesheet.go). The second
// is a FOCUSED ERRAND: while it holds the keyboard, `tab` hands it back to the
// list and `esc` clears a half-typed follow-up before it does, which is the two-
// zone law homeexchange.go states in full — and a `tab` the router took first
// would walk the person out of home mid-sentence.
//
// THE KEYBOARD IS SETTLED BEFORE THE KEY IS READ. An exchange holds it only
// while the cursor is on that exchange's row, so walking away can never leave
// the arrows moving a pane nobody is looking at ([app.settleExchangeFocus]).
func (placeHome) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, took := a.homeSheetKeyFirst(msg); took {
		return cmd, true
	}
	a.settleExchangeFocus()
	ex := a.paneExchange()
	if ex == nil || !ex.focused {
		return nil, false
	}
	a.home.say("", "")
	cmd := a.exchangeKey(ex, msg)
	// AND THE SWEEP RUNS AFTER THE KEY, for [app.homeKey]'s reason: what a key
	// does is move the cursor, and "have they moved off it" is a question only
	// answerable once they have.
	a.sweepExchanges()
	a.touch()
	return cmd, true
}
