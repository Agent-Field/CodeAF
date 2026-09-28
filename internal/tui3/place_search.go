package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE SEARCH PLACE ────────────────────────────────────────────────────────
//
// Everything that has been said on this machine, found by the words you
// remember of it. It reads the full-text index the conversation store already
// keeps ([store.Store.SearchConversations]) — one query across every thread,
// with the thread's name joined in the same statement — and nothing is indexed
// behind anybody's back: this reads a record that was already being written.
//
// ── THE STORE READ IS NEVER ON THE DRAW AND NEVER ON THE KEYSTROKE ──────────
//
// SQLite can wait, and a surface that waited with it would be a surface that
// stutters while somebody types. So a keystroke does exactly two things: it puts
// a character in the composer, and it arms a QUIET INTERVAL ([searchDebounce]).
// When that interval survives to the end without another key landing, the read
// goes out as a Bubble Tea command and comes back as a message. Two generation
// guards keep the answer honest — [searchTickAccepted] stops an old interval
// starting work for words already replaced, and [searchDoneAccepted] stops a
// slower old read landing on top of newer results.
//
// ── TYPING HERE DOES NOT OFFER PLACES, AND THAT IS NOT AN OMISSION ──────────
//
// SCREEN 1g's "typing offers places too" is HOME's column: type `sta` on home
// and the standing place is offered above the conversations that match
// (homeplaces.go). The router already does that, on the one screen that is a
// switcher, and doing it again here would put two answers to `sta` on two
// screens one `tab` apart — the same offer said twice, in two rankings that
// could disagree. This place answers one question: what was said, and where.

// searchPage is the whole search place: what has been asked, what came back,
// and the world those results are joined against.
type searchPage struct {
	// ask is the query the surface is currently answering FOR, with the
	// generation every guard compares against. It is bumped on every change to
	// the box, so a stale tick or a slow read can be recognised and dropped.
	ask  searchAsk
	hits []store.ConversationHit
	// unfolded is whether the results' fold is open ([searchReading.unfolding]).
	unfolded bool
	// world is the reading the hits are joined against for a project name and
	// the transcript a door opens. It is read when this place opens and on the
	// place clock's beat, never per keystroke: a conversation absent from it is
	// still a valid result, because the message itself is durable even when its
	// project metadata is not (readSearch says so).
	world session.World
	// reading is the derived answer the body is drawn from.
	reading searchReading
	cursor  int
	// top and shown are the WINDOW the last draw put over the reading, and hover
	// the line the pointer is over (-1 for none). They are the same three fields
	// every promoted place keeps and they mean the same thing on each: the
	// window follows the cursor ([placeTop]), which mouse and keyboard both move.
	top, shown int
	hover      int
	read       time.Time
	// query is the box: what is typed on this place, drawn on the control row
	// at the head of the body ([searchControlRow]) and read by [app.searchAsked].
	// It is the place's own rather than a shared composer because typing here
	// SEARCHES, and only home's box sends anything ([place.box]).
	query editor
	// lead is how many rows of the body the last draw spent above the reading —
	// one for the control row while there is a query, none while the whisper
	// is teaching what to type — so a press resolves against the rows drawn.
	lead int
	// waiting says a read is out. It is held so that the place can tell "nothing
	// on this machine says that" from "nobody has answered yet" — two very
	// different sentences to be looking at.
	waiting bool
}

// openSearch walks into the search place.
func (a *app) openSearch() tea.Cmd {
	now := a.now()
	a.search = searchPage{world: a.readWorld(), read: now, hover: -1}
	a.rebuildSearch()
	return tea.Batch(a.armPlaceClock(), a.searchAsked())
}

// refreshSearch is the place clock's beat: the world is re-read so a
// conversation started in the next terminal has a door here, and the results
// themselves are left alone — re-running somebody's query behind their back
// every three seconds would make a still page move under them.
func (a *app) refreshSearch() {
	if !a.at(pageSearch) {
		return
	}
	a.search.world = a.readWorld()
	a.search.read = a.now()
	a.rebuildSearch()
}

func (a *app) rebuildSearch() {
	p := &a.search
	next := readSearch(p.ask.query, p.hits, p.world, p.read)
	// A NEW QUESTION STARTS WITH ITS FOLD SHUT: the rest of an old answer is
	// not the rest of this one.
	if next.query != p.reading.query {
		p.unfolded = false
	}
	p.reading = next.unfolding(p.unfolded)
	// AND WHETHER THERE IS AN INDEX AT ALL IS A FACT ABOUT THE SURFACE, not
	// about the words: it is read here, where the reading is made, so the page
	// can tell "nothing was said" from "nothing looked" ([searchNoIndexWord]).
	// The hosted case is answered further up by [placeSearch.remote], which says
	// WHOSE index is missing and is the better sentence where it applies.
	p.reading.noIndex = a.searchStore == nil && !a.hosted()
	p.cursor = a.nearestSearchStop(p.cursor)
}

// searchAsked is what a change to the box costs: one generation, and one quiet
// interval. It is called from the key handler after the character has landed,
// so the generation it carries is the generation of the words now on screen.
func (a *app) searchAsked() tea.Cmd {
	query := strings.TrimSpace(a.search.query.String())
	if query == a.search.ask.query {
		return nil
	}
	a.search.ask = searchAsk{query: query, gen: a.search.ask.gen + 1}
	a.search.cursor = 0
	if query == "" {
		// AN EMPTY BOX IS NOT A QUERY. The results go with the words that found
		// them, the page falls back to saying what it is for, and no read goes out
		// — a search for nothing is a table scan with no question in it.
		a.search.hits, a.search.waiting = nil, false
		a.rebuildSearch()
		return nil
	}
	a.search.waiting = true
	return a.searchQuiet(a.search.ask.gen)
}

// searchQuiet is the quiet interval, ARMED THROUGH A SEAM.
//
// IT IS INJECTABLE FOR THE TESTS AND FOR NO OTHER REASON, and the reason is a
// real defect rather than a convenience. [searchDebounceEvery] is 150ms of REAL
// TIME; the suite drives this surface by running each command it produces under
// a budget of about the same length, so whether a keystroke's interval fired
// before the harness gave up was a coin toss decided by how loaded the machine
// was — and `TestTheSearchPlaceDropsAStaleIntervalAndAStaleAnswer` typed six
// letters, each arming one. A test that fails on a busy laptop and passes on an
// idle one is a test nobody reads any more.
//
// So the arming is a field: nil is the real timer, and a test puts a command
// here that answers nothing and delivers [searchTickMsg] itself, at the instant
// it means to. Nothing else in this program ever sets it — the surface arms the
// real interval, which is what the other tests in that file assert by driving
// the tick by hand exactly as the loop would.
func (a *app) searchQuiet(gen int) tea.Cmd {
	if a.searchArm != nil {
		return a.searchArm(gen)
	}
	return searchDebounce(gen)
}

// searchTick is the quiet interval arriving. It sends the read only when the
// words have not moved on since the interval was armed.
func (a *app) searchTick(msg searchTickMsg) tea.Cmd {
	if !a.at(pageSearch) || !searchTickAccepted(a.search.ask, msg) {
		return nil
	}
	if a.searchStore == nil {
		// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no index
		// behind it the place keeps saying what it is for rather than drawing an
		// empty result list under somebody's words.
		//
		// It matched conversations by NAME here for one build on 2026-09-22,
		// the way home's box does, and the owner took that back the next day:
		// a place called `search` that searches something narrower than it says
		// is worse than one that refuses.
		a.search.waiting = false
		return nil
	}
	return searchCmd(a.searchStore, a.search.ask)
}

// searchDone is the read landing. A result for words already replaced is
// dropped, which is the whole of what the generation is for.
func (a *app) searchDone(msg searchDoneMsg) {
	if !a.at(pageSearch) || !searchDoneAccepted(a.search.ask, msg) {
		return
	}
	a.search.waiting = false
	if msg.err != nil {
		// THE REFUSAL IS THE PLACE'S ONE LINE and not a note in the transcript:
		// somebody standing on this page asked this page a question.
		a.pageMsg = searchFailedWord + " · " + msg.err.Error()
		return
	}
	// AND A READ THAT LANDS CLEARS ITS OWN OLD FAILURE AND NOTHING ELSE. That
	// line is the ROUTER'S and it is shared with every refusal a place can be
	// handed: `alt+2` on a machine that has run no work leaves its sentence
	// there and puts the person back here, where this place re-arms its query on
	// the way in ([app.openSearch]) and answered it a moment later — wiping the
	// sentence off the screen before anybody could read it. A refusal that
	// flashes is a refusal that did not happen.
	if strings.HasPrefix(a.pageMsg, searchFailedWord) {
		a.pageMsg = ""
	}
	a.search.hits = msg.hits
	a.rebuildSearch()
	a.touch()
}

// searchFrame is this place, drawn: the shared frame with this place's body in
// it, and the hit map cast back into the body lines this place answers with
// (pages.go's [app.placeDraw] and [placeLineHits]).
func (a *app) searchFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := a.placeDraw(placeSearch{}, width, height)
	return lines, placeLineHits(hits), caretX, caretY
}

func (a *app) nearestSearchStop(from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(a.search.reading.hits)+2; i++ {
		if a.search.reading.stop(i) {
			return i
		}
	}
	return 0
}

// moveSearch walks the cursor by whole conversations. The facet legend and the
// folded count are context rather than doors ([searchReading.at] holds that
// law), so nothing stops on them.
func (a *app) moveSearch(delta int) {
	var doors []int
	for i := 0; i < len(a.search.reading.hits)+2; i++ {
		if a.search.reading.stop(i) {
			doors = append(doors, i)
		}
	}
	if len(doors) == 0 {
		return
	}
	at := 0
	for i, row := range doors {
		if row <= a.search.cursor {
			at = i
		}
	}
	a.search.cursor = doors[moveCursor(at, delta, len(doors))]
}

// ── the keys ────────────────────────────────────────────────────────────────

func (a *app) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if box := a.placeBox(); box != nil && !box.empty() {
			box.reset()
			a.touch()
			return a.searchAsked()
		}
		a.leavePlace()
		return nil
	case "up", "ctrl+p":
		a.moveSearch(-1)
		a.touch()
		return nil
	case "down", "ctrl+n":
		a.moveSearch(1)
		a.touch()
		return nil
	case "enter":
		if a.search.reading.foldAt(a.search.cursor) {
			return placeSearch{}.enter(a)
		}
		if hit, ok := a.search.reading.at(a.search.cursor); ok {
			return a.openSearchHit(hit)
		}
		// NOTHING UNDER THE CURSOR OPENS NOTHING: only home starts things
		// ([place.box]).
		return nil
	}
	if box := a.placeBox(); box != nil {
		listNavigate(msg, box, a.moveSearch, func() {}, searchShown)
		a.touch()
		return a.searchAsked()
	}
	return nil
}

// openSearchHit is `enter` on a result: the conversation the matching turn was
// said in.
func (a *app) openSearchHit(hit searchHit) tea.Cmd {
	if strings.TrimSpace(hit.transcript) == "" {
		// A HIT THE WORLD SCAN COULD NOT PLACE IS STILL A TRUE HIT and stays on
		// the page — the message is durable even where its project folder is not
		// — but there is nothing to open, and a door that led nowhere would be
		// worse than the row saying only what it says.
		a.pageMsg = searchNoDoorWord
		return nil
	}
	return a.openConversationRow(hit.row())
}

// searchFailedWord leads the one sentence this place says when the index could
// not answer. It is a constant because it is spelled twice — once when the
// failure is written and once when it is taken back down — and two spellings of
// one sentence is a failure line that never clears.
const searchFailedWord = "could not search what was said"

// searchNoDoorWord is what a result with no findable conversation says. It
// names the fact rather than a fault, because it is neither: the turn is
// remembered and the folder it was held in is not.
const searchNoDoorWord = "that conversation is not on this machine any more"

// row is the world's own record of the conversation a hit came from, in the
// shape every door on this surface takes.
func (h searchHit) row() session.SessionRow {
	return session.SessionRow{ID: h.sessionID, Transcript: h.transcript, ProjectDir: h.dir, Title: h.title}
}

// ── the one door onto a conversation that is not home's own column ──────────

// openConversationRow opens one conversation from a place that is not home.
//
// IT IS [app.homeOpenLine]'S LADDER WITH HOME'S COLUMN TAKEN OUT, and the order
// of the checks is the feature there and here alike. Identity comes first,
// because a transcript THIS PROCESS holds answers [session.InUse] true about
// itself — a flock rides the open file description rather than the process — so
// a conversation one keystroke away would otherwise be refused as somebody
// else's window (keeper.go's [app.holding] states the whole rule).
//
// The two differences from home's are both about where a refusal goes. Home
// says its refusals on its own message line, which belongs to home's view; a
// place that is not home says them on [app.pageMsg], which is the router's one
// line for exactly this. And there is no "home already knows this row is
// locked" shortcut here, because this place never read the flock: the open
// reports it instead, which is what the resume picker has always done.
func (a *app) openConversationRow(row session.SessionRow) tea.Cmd {
	switch {
	case a.holding(row.Transcript):
		// A conversation this terminal already has open: the one on screen, or one
		// running behind it. Either way enter goes to it rather than opening
		// anything — reopening would drop the lock, replay the journal and land
		// exactly where it started. It says nothing, for home's reason: closing
		// into the conversation somebody just confirmed IS the thing happening.
		cmd, _ := a.bringForward(row.Transcript)
		a.standDownFullscreen()
		a.closeRoom()
		a.touch()
		return cmd
	case !a.canOpen():
		a.pageMsg = resumeUnavailableWord
		return nil
	}
	where := strings.TrimSpace(row.ProjectDir)
	if where != "" && !homeFolderThere(where) {
		// ONE os.Stat, ON THE KEYSTROKE. An agent whose tool root does not exist
		// fails every bash and every relative path in a way nothing on screen
		// explains, so the door is checked before it is walked through.
		a.pageMsg = homeGoneWord + " · " + where
		return nil
	}
	// AND THE CONVERSATION THIS WINDOW WAS IN GOES ON RUNNING. It is detached
	// rather than closed and put in the keeper, exactly as home's own door leaves
	// it — which is the whole of what makes any of these places a way BETWEEN
	// conversations rather than a list of ones to open in another terminal.
	cmd, refusal := a.openBeside(where, row.Transcript)
	if refusal != "" {
		a.pageMsg = refusal
		return nil
	}
	a.standDownFullscreen()
	a.closeRoom()
	a.touch()
	return cmd
}

// ── the place ───────────────────────────────────────────────────────────────

// placeSearch is this place's handle on the registry (pages.go's [place] states
// the contract and why the handle holds no state of its own).
type placeSearch struct{ placeBase }

// box is the query ([searchPage.query]).
func (placeSearch) box(a *app) *editor { return &a.search.query }

func init() { registerPlace(placeSearch{}) }

func (placeSearch) id() page     { return pageSearch }
func (placeSearch) word() string { return "search" }

func (placeSearch) open(a *app) tea.Cmd { return a.openSearch() }

// close writes the look stamp and drops the query in flight with the world it
// was joined against.
func (placeSearch) close(a *app) {
	a.leavePage(pageSearch)
	a.search = searchPage{}
}

func (placeSearch) tick(a *app, now time.Time) (bool, tea.Cmd) {
	a.refreshSearch()
	return true, nil
}

// body is the results, or — while the box is empty — this place's heading and
// its whisper ([placeWhisper]): a search with no words in it is a place waiting
// for one, not a page that is missing.
// remote is this place over --host: the index it reads is the one this machine's
// conversations were written into, and this conversation was written on another
// (pages.go's [place.remote]).
//
// THE SENTENCE MATTERS MORE HERE THAN THE ROWS DO. A search that finds nothing
// looks exactly like a search that found nothing — so without this line a person
// would read "we never talked about that" off a place that never looked.
func (placeSearch) remote(a *app) string {
	if a.hosted() && a.searchStore == nil {
		return searchRemoteWord
	}
	return ""
}

func (placeSearch) body(a *app, width, room int) []placeRow {
	lit := func(i int) bool { return i == a.search.cursor }
	body := a.search.reading.paint(width, a.pal, lit)
	rows := make([]placeRow, 0, room)
	// THE QUERY IS THE FIRST ROW OF THE BODY, over the results it found, the
	// way the tasks filter stands over the list it narrows ([tasksControlRow]):
	// the foot draws no box on this place ([place.box]), so the letters a
	// person types have to be where their eye already is. It is not a stop,
	// and it is not drawn while there is nothing typed: the whisper under it
	// already says what to type, and a page that said it twice would be the
	// tasks page's old title row.
	a.search.lead = 0
	if query := strings.TrimSpace(a.search.query.String()); query != "" {
		rows = append(rows, placeRow{text: searchControlRow(query, width, a.pal), hit: -1})
		a.search.lead = 1
		room = max(room-1, 0)
	}
	// THE WINDOW FOLLOWS THE CURSOR, which is what makes `↓` past the last
	// visible result scroll rather than walking the selection off the screen.
	a.search.top = placeTop(a.search.top, a.search.cursor, len(body), room)
	for i := a.search.top; i < len(body); i++ {
		if len(rows) >= room {
			break
		}
		text := body[i]
		if a.search.reading.stop(i) && lit(i) {
			text = placeBand(text, width, a.pal)
		}
		rows = append(rows, placeRow{text: text, hit: i})
	}
	a.search.shown = len(rows) - a.search.lead
	for len(rows) < room+a.search.lead {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

// searchControlRow is the query as the body's head row: the search mark, then
// the words typed, in ink.
func searchControlRow(query string, width int, pal palette) string {
	mark := pal.glyph(tokens.GFilter)
	room := max(width-len(placeLead)-ansi.StringWidth(mark)-1, 1)
	return placeLead + pal.dim(mark) + " " + pal.ink(fit(query, room))
}

// stops is every row of the reading that is a conversation. The facet legend and
// the folded count are context rather than doors ([searchReading.at] holds that
// law), so nothing stops on them.
func (placeSearch) stops(a *app) []int {
	var doors []int
	for i := 0; i < len(a.search.reading.hits)+2; i++ {
		if a.search.reading.stop(i) {
			doors = append(doors, i)
		}
	}
	return doors
}

// cursorAt is the row of the reading the cursor is on (pages.go's
// [place.cursorAt]).
func (placeSearch) cursorAt(a *app) int { return a.search.cursor }

func (placeSearch) enter(a *app) tea.Cmd {
	// THE FOLD LINE OPENS WHERE IT STANDS and the cursor stays on it, now
	// reading `fewer`, so the next `enter` undoes it.
	if a.search.reading.foldAt(a.search.cursor) {
		a.search.unfolded = !a.search.unfolded
		a.rebuildSearch()
		a.touch()
		return nil
	}
	if hit, ok := a.search.reading.at(a.search.cursor); ok {
		return a.openSearchHit(hit)
	}
	return nil
}

func (placeSearch) press(a *app, y int) (tea.Cmd, bool) {
	// The control row, while there is one, is the body's first row and answers
	// to nothing ([searchPage.lead]).
	if at, ok := placeBodyLine(y-a.search.lead, a.search.top, a.search.shown); ok {
		if a.search.reading.stop(at) {
			a.search.cursor = at
			a.touch()
			return placeSearch{}.enter(a), true
		}
	}
	return nil, true
}

func (placeSearch) hover(a *app, y int) bool {
	next := -1
	if at, ok := placeBodyLine(y-a.search.lead, a.search.top, a.search.shown); ok {
		if a.search.reading.stop(at) {
			next = at
		}
	}
	return placeHoverMoved(&a.search.hover, &a.search.cursor, next, next, a)
}

func (placeSearch) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.moveSearch(delta)
	a.touch()
	return nil, true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeSearch) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.searchKey(msg) }

// The two feet this place has, and they are two because `enter` means two
// different things here — which is exactly the fault this place used to have
// while it had no foot at all.
//
// THE ROUTER'S DEFAULT SAID `enter talk about it` OVER BOTH OF THEM, six rows
// under this place's own body saying `enter opens the conversation at the
// matching turn.` — and the manual sided with the body (places.md's search
// section). With results up, `enter` opens the hit ([placeSearch.enter]); with
// an empty box `placeTalk` returns nil and the key does nothing at all. Neither
// state was the one the foot named.
const (
	// searchHitHint is the foot standing on a result: what enter opens, how to
	// move between them, and that the box is still a search box.
	searchHitHint = "enter opens it at that turn · ↑↓ pick · type to search · esc clear the words"
	// searchAskHint is the foot with nothing to stand on — the teaching page and
	// a search that found nothing. NOTHING IS NAMED THAT IS NOT BOUND, so
	// `enter` and `↑↓` are simply absent rather than promised over an empty
	// body (place_standing.go's hint holds the same argument at more length).
	searchAskHint = "type to search · esc clear the words"
)

// hint is WHAT THE ROW UNDER THE CURSOR CAN BE ASKED FOR (pages.go's
// [place.hint]) — and on the teaching page and the no-hit line there is no row,
// so the foot says only the two things that are true there.
func (placeSearch) about() string { return "search every conversation" }

func (placeSearch) hint(a *app) string {
	if a.search.reading.foldAt(a.search.cursor) {
		return foldEnterWord(a.search.unfolded) + " · ↑↓ pick · type to search · esc clear the words"
	}
	if _, ok := a.search.reading.at(a.search.cursor); ok {
		return searchHitHint
	}
	return searchAskHint
}
