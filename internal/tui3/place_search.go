package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
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
	open bool
	// ask is the query the surface is currently answering FOR, with the
	// generation every guard compares against. It is bumped on every change to
	// the box, so a stale tick or a slow read can be recognised and dropped.
	ask  searchAsk
	hits []store.ConversationHit
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
	// window follows the cursor ([placeTop]), and the pointer previews where the
	// cursor selects.
	top, shown int
	hover      int
	read       time.Time
	// waiting says a read is out. It is held so that the place can tell "nothing
	// on this machine says that" from "nobody has answered yet" — two very
	// different sentences to be looking at.
	waiting bool
}

// openSearch walks into the search place.
func (a *app) openSearch() tea.Cmd {
	now := a.now()
	a.search = searchPage{open: true, world: a.readWorld(), read: now, hover: -1}
	a.rebuildSearch()
	return tea.Batch(a.armPlaceClock(), a.searchAsked())
}

// refreshSearch is the place clock's beat: the world is re-read so a
// conversation started in the next terminal has a door here, and the results
// themselves are left alone — re-running somebody's query behind their back
// every three seconds would make a still page move under them.
func (a *app) refreshSearch() {
	if !a.search.open {
		return
	}
	a.search.world = a.readWorld()
	a.search.read = a.now()
	a.rebuildSearch()
}

func (a *app) rebuildSearch() {
	p := &a.search
	p.reading = readSearch(p.ask.query, p.hits, p.world, p.read)
	p.cursor = a.nearestSearchStop(p.cursor)
}

// searchAsked is what a change to the box costs: one generation, and one quiet
// interval. It is called from the key handler after the character has landed,
// so the generation it carries is the generation of the words now on screen.
func (a *app) searchAsked() tea.Cmd {
	query := strings.TrimSpace(a.compose.String())
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
	return searchDebounce(a.search.ask.gen)
}

// searchTick is the quiet interval arriving. It sends the read only when the
// words have not moved on since the interval was armed.
func (a *app) searchTick(msg searchTickMsg) tea.Cmd {
	if !a.search.open || !searchTickAccepted(a.search.ask, msg) {
		return nil
	}
	if a.searchStore == nil {
		// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no index
		// behind it the place keeps saying what it is for rather than drawing an
		// empty result list under somebody's words.
		a.search.waiting = false
		return nil
	}
	return searchCmd(a.searchStore, a.search.ask)
}

// searchDone is the read landing. A result for words already replaced is
// dropped, which is the whole of what the generation is for.
func (a *app) searchDone(msg searchDoneMsg) {
	if !a.search.open || !searchDoneAccepted(a.search.ask, msg) {
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

// ── the frame ───────────────────────────────────────────────────────────────

func (a *app) searchFrame(width, height int) ([]string, []int, int, int) {
	return placeFrame(a, width, height, -1, func(width, room int) []placeRow[int] {
		body := a.search.reading.rows(width, a.pal)
		// THE WINDOW FOLLOWS THE CURSOR, which is what makes `↓` past the last
		// visible result scroll rather than walking the selection off the screen.
		a.search.top = placeTop(a.search.top, a.search.cursor, len(body), room)
		rows := make([]placeRow[int], 0, room)
		for i := a.search.top; i < len(body); i++ {
			if len(rows) >= room {
				break
			}
			text := body[i]
			if _, ok := a.search.reading.at(i); ok && (i == a.search.cursor || i == a.search.hover) {
				text = a.pal.selected(text, width)
			}
			rows = append(rows, placeRow[int]{text: text, hit: i})
		}
		a.search.shown = len(rows)
		for len(rows) < room {
			rows = append(rows, placeRow[int]{text: "", hit: -1})
		}
		return rows
	})
}

func (a *app) nearestSearchStop(from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(a.search.reading.hits)+2; i++ {
		if _, ok := a.search.reading.at(i); ok {
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
		if _, ok := a.search.reading.at(i); ok {
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
	if cmd, took := a.placeKey(msg); took {
		return cmd
	}
	switch msg.String() {
	case "esc":
		if box := a.placeBox(); box != nil && !box.empty() {
			box.reset()
			a.touch()
			return a.searchAsked()
		}
		a.leavePage(pageSearch)
		a.search = searchPage{}
		a.teach.close()
		a.touch()
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
		if hit, ok := a.search.reading.at(a.search.cursor); ok {
			return a.openSearchHit(hit)
		}
		return a.placeTalk()
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
	if word, room := a.roomForAnother(); !room {
		a.pageMsg = word
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
	a.touch()
	return cmd
}
