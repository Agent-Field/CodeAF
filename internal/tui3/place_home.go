package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
// through [app.memoryChangedSince] on that same beat and never on a draw. This
// function opens nothing and stats nothing.
func (h *homeView) buildSwitch() {
	h.reading = readSwitcher(h.world, h.items, switcherHere{session: h.here, project: h.bucket}, h.seen, h.world.Read,
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
	// a different screen.
	if len(h.lines) == 0 {
		for _, part := range homeEmptyLines() {
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

// memoryChangedSince is the two figures the `since you left` ledger's memory
// line is made of: how many things this machine learned since the look stamp,
// and how many it let go of.
//
// IT ANSWERS ZERO AND ZERO, AND THAT IS THE CORRECT EMPTY STATE RATHER THAN A
// GAP. The records exist — `store.MemoryChangedSince` is exactly this pair — but
// the seam that hands v3's surface a memory store is another lane's to wire, and
// under the emptiness law a figure this surface cannot honestly compute is a
// line it does not draw ([readSwitcher] omits the memory row entirely for a zero
// pair). The lane that wires it calls it HERE and on home's own three-second
// beat, never on a draw: this is SQLite behind the seam, and the law every home
// reader is held to is that a draw never touches a disk (tui3.go).
func (a *app) memoryChangedSince(time.Time) (learned, letGo int) { return 0, 0 }

// readSwitchLedger takes those two figures once per reading of the world, where
// every other disk-backed fact on this screen is taken.
func (a *app) readSwitchLedger() {
	learned, letGo := a.memoryChangedSince(a.home.seen)
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
	if !a.home.open || a.home.phone || a.home.searching() {
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
func (h *homeView) foldSwitch(open bool) {
	h.moreOpen = open
	h.build()
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

// homeCardBands is which of the registry's bands the switcher's card draws, in
// the order it draws them (SCREEN 1d).
//
// SIXTEEN REGISTERED BANDS BECAME FIVE, AND EVERY ONE EITHER ASKS YOU SOMETHING
// YOU CAN ANSWER HERE OR POINTS AT A PAGE. That is the whole selection rule. The
// card exists only past [homeCardMin], where the width is genuinely spare — and
// a card drawn on spare width has to be worth more than the row it is beside, so
// it may not be a second reading of what the row already says. What went:
//
//   - `state` and `leftoff` — what a conversation is doing and the last thing it
//     said. The row's own note carries exactly that now ([switcherConversationNote]),
//     one column over, for a person who is not pointing at anything.
//   - `news`, `nextup`, `watchlist`, `sinceleft`, `agents`, `today` — every one of
//     them is a thing that happened by itself, and every one of them is a line of
//     the ledger at the top of the list or a row of a place the ledger opens.
//   - `gone` — folded into the place line, where the address it is about is.
//   - `spend` and `thinking` — folded into one facts line ([app.homeCardFacts]).
//   - `keys` — the legend became the `→ verbs` hint, because the letters live on
//     the strip and only while the strip is drawn (SCREEN 3a).
//
// The five that stayed are the five that act: a question you can answer without
// opening anything, the work and what it came to, the files it made, what it
// cost, and where the repository stands.
var homeCardBands = []string{"answer", "work", "deliverables"}

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
	if place := a.homeCardPlace(row, width, pal); place != nil {
		bands = append(bands, place)
	}
	for _, name := range homeCardBands {
		if rows := a.drawHomeBandNamed(name, ctx); len(rows) > 0 {
			bands = append(bands, rows)
		}
	}
	if facts := a.homeCardFacts(ctx); facts != nil {
		bands = append(bands, facts)
	}
	if hint := a.homeCardVerbs(width, pal); hint != nil {
		bands = append(bands, hint)
	}
	return homeBands(bands, room)
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

// homeCardPlace is the card's second line: WHERE this conversation is, where its
// repository stands, and whether it is the one this window is holding.
//
//	~/aforge-v2 · master · 1 file dirty · here
//
// THE REPOSITORY IS ON THE PLACE LINE RATHER THAN IN A BAND OF ITS OWN, because
// a branch and a dirty count are facts ABOUT that address and a band between the
// address and them would be saying the same address twice. The reading itself is
// still homeband_repo.go's — one bounded `git status` per workspace per
// [homeRepoTTL], taken when a card arrives and never on a draw.
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
			parts = append(parts, repo.line)
		}
	}
	if held := a.homeHolding(row); held != "" {
		parts = append(parts, held)
	} else if a.homeMark(row) == markHere {
		parts = append(parts, homeHereWord)
	}
	// THE WHOLE LINE IS A DOOR (pathlink.go), and the anchor covers all of it
	// rather than the path half: the address and what is true about it are one
	// reading, and a link that stopped at the first clause would be a target a
	// narrow card had already cut off.
	return []string{pal.dim(a.pathLink(where, fitLeft(strings.Join(parts, " · "), width)))}
}

// homeCardFacts is `spend` and `thinking` on ONE line — what this cost, and the
// rung it thinks at.
//
//	spent $1.63 · 3.6M tokens · last active 3h
//
// TWO BANDS BECAME ONE LINE because they are one sentence: both are arithmetic
// about the thing on the card, and a blank row between "what it cost" and "how
// hard it thinks" was a paragraph break inside a clause.
//
// A CONVERSATION HAS NO RUNG THIS SCREEN CAN HONESTLY STATE, so it says none.
// The only rungs home can read are the install's default and a standing item's
// own (homeband_thinking.go's [app.bandRung]); a conversation's is its own
// sticky setting, belongs to the window that session is open in, and is
// deliberately untouchable from here (home.go's ctrl+v). Printing the machine's
// default on a chat's card would be advertising a fact about the install as a
// fact about the chat.
func (a *app) homeCardFacts(ctx bandContext) []string {
	clauses := []string{}
	if facts := homeFacts(ctx.subject.row, ctx.now); facts != "" {
		clauses = append(clauses, strings.Split(facts, " · ")...)
	}
	if rung, _ := a.bandRung(ctx.subject); effortClause(rung) != "" {
		clauses = append(clauses, effortClause(rung))
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
