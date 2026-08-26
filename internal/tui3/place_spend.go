package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE SPEND PLACE ─────────────────────────────────────────────────────────
//
// What this machine has cost, by the day, by the model, and by what it was for
// (SCREEN 2c). Three questions of the same rows and nothing else, and the two
// arrow axes that move between them (SCREEN 3d).
//
// THE FIGURES ARE THE BILL AND NOT AN ESTIMATE. Every model call writes one
// line to a machine-wide ledger where it was made (internal/session's
// usage_ledger.go), and this page adds those lines up. It is the only place on
// the surface that can answer "what did opus cost me this month" or "what did
// Tuesday cost", because a conversation's own spend is a lifetime scalar with
// no day and no model in it.
//
// THE LEDGER IS READ THROUGH A CACHE AND ON A CLOCK, NEVER ON A DRAW. The file
// grows by a line per call, so a reader that re-parsed it whenever it changed
// would re-parse it after every turn forever; [session.UsageCache] keeps what it
// has parsed and reads only what was appended since. The read happens when this
// place opens and on the place clock's beat (placecounts.go), and every draw
// after that is arithmetic over lines already in memory.
//
// THERE IS NO BUDGET EDITOR HERE AND THERE WILL NOT BE ONE. The allowance is a
// rail, it is drawn on the status line's money segment, and it is edited on the
// segment that shows it — the label between the arrows being the reading and the
// control at once is the same move that segment already makes. A second place to
// set it would be a second answer to what the ceiling is.

// spendPage is the whole spend place: which window it is showing, the lines it
// is showing it over, and the row the cursor is on.
type spendPage struct {
	// cache is this surface's reader over the machine-wide ledger. It holds
	// parsed lines and a file offset and belongs to THIS goroutine, which is why
	// the door hands over a path and never a cache ([Options.UsageLedger]).
	cache session.UsageCache
	lines []session.UsageLine
	// win is which stretch of time and how coarse — the two dimensions the four
	// `shift+arrow` keys move ([session.UsageWindow]).
	win session.UsageWindow
	// reading is the answer the body is drawn from: derived, immutable, and
	// rebuilt only when the lines, the window or the names actually changed.
	reading spendReading
	// cursor is a ROW OF THE BODY, and the rows it may stand on are the ones
	// that named something money was spent on.
	cursor int
	// stops is the door map the last draw wrote, so `enter` opens the thing the
	// row the person is looking at named.
	stops []spendStop
	// top and shown are the WINDOW the last draw put over the body: the first
	// line of the reading that was drawn, and how many of them fit. The window
	// follows the cursor ([placeTop]) rather than being scrolled on its own, and
	// the pair is what turns a row of the terminal back into a line of the body
	// for the pointer (pages.go's [app.placeBodyPress]).
	top, shown int
	// hover is the line of the reading the pointer is over, and -1 for none. THE
	// POINTER PREVIEWS AND THE CURSOR SELECTS: it is drawn at the same rung as
	// the cursor's own row and moves nothing.
	hover int
	// read is the instant the lines were read, and every figure and age on the
	// page is measured from it rather than from a fresh clock.
	read time.Time
	// held is whether the ledger holds ANY priced line at all, in any window, and
	// it is what tells the two empty pages apart — the same fact and the same
	// argument as the standing and tasks places' own ([standingPlace.held]).
	//
	// A WINDOW EMPTIED BY THE ARROWS IS NOT AN EMPTY MACHINE. Both draw no rows,
	// and the right answer to each is the opposite of the other: a machine that
	// has spent nothing wants the frame spent saying what this place is for
	// ([spendTeach]), while a window paged onto a quiet fortnight wants the
	// HEADER — the control that pages it back — above nothing at all. Drawing the
	// teaching in both cases swallowed the only way out of the second.
	held bool
	// world is THIS PLACE'S OWN SCAN of the projects root, taken on the way in
	// and again on the beat. It is what `what it was for` joins its ids against
	// ([app.spendNames] says why it is not home's).
	world session.World
	// names is that join, ALREADY MADE: an id against the word a person calls
	// that thing, built once where the world is read and held here.
	//
	// IT IS A FIELD BECAUSE THE JOIN TOUCHES SEAMS AND MOVING THE WINDOW MUST
	// NOT. [app.rebuildSpend] runs on every `shift+←` — at key-repeat rate,
	// which is what makes holding the arrow down a design promise — and it used
	// to build this map on the spot, walking the standing store once per
	// project each time. The lines are already in memory and so, now, is this.
	names map[string]string
}

// spendWindowDays is the window this place opens on: a fortnight, by the day.
// It is a fortnight because that is long enough to show a working rhythm and
// short enough that one cell is one day at every width the surface promises.
const spendWindowDays = 14

// openSpend walks into the spend place: one read of the ledger, and the clock
// that keeps it current.
func (a *app) openSpend() tea.Cmd {
	now := a.now()
	a.spend = spendPage{cache: session.UsageCache{Path: a.usageLedger},
		win: session.LastDays(now, spendWindowDays), hover: -1,
		world: a.readWorld()}
	a.readSpendLines(now)
	return a.armPlaceClock()
}

// spendCrewNow is WHO IS BOUND TO WHAT RIGHT NOW: the crew as the settings
// registry reads it, turned around so a model id answers with its slot's word,
// plus the slots nothing is bound to.
//
// IT IS THE BINDING AND NOT AN ATTRIBUTION (FIDELITY item 7). A model's rows in
// the ledger say what each call named ITSELF; this says what a person has told
// this machine that model is for, which is the only version of the fact they can
// act on from the chip the table sends them to.
//
// THE READ IS IN MEMORY. [config.Settings.ModelSlotBindings] asks each role slot
// through the seams this surface wired when the registry was built
// (settings.go's [app.registry]) — the conversation's own model, and whatever
// the door answers for the rest — so this costs no disk and may run on the beat.
func (a *app) spendCrewNow() spendCrew {
	crew := spendCrew{role: map[string]string{}}
	bound := a.registry().ModelSlotBindings()
	for _, slot := range config.ModelSlots() {
		if slot.Role == "" {
			continue
		}
		model := strings.TrimSpace(bound[slot.Slot])
		if model == "" {
			// AN EMPTY READING IS NOT THE SAME AS AN EMPTY BINDING, and only one
			// of the two may be drawn. This surface holds a client for ONE of the
			// five slots — the conversation it is sitting in — and answers every
			// other with the sentence [app.slotRefusal] says: "that model is
			// chosen where its session is opened". So a slot this window cannot
			// ask about is UNKNOWN, the emptiness law renders unknown as nothing,
			// and the row is left off. The moment a door wires the role seam
			// ([config.SettingsOptions.RoleModel]) the slot answers here and the
			// `planning · unbound · follows execution` row the design draws
			// appears with it.
			if a.answersForSlot(slot) {
				crew.unbound = append(crew.unbound, slot)
			}
			continue
		}
		// TWO SLOTS ON ONE MODEL SAY BOTH, in the ladder's order, because the
		// same model answering the conversation and the work is the ordinary
		// arrangement and a row that named only the first would be telling
		// somebody the other slot is somewhere else.
		key := spendModelKey(model)
		if was := crew.role[key]; was != "" {
			crew.role[key] = was + " · " + slot.Label
			continue
		}
		crew.role[key] = slot.Label
	}
	return crew
}

// answersForSlot is whether this window can say anything at all about one model
// slot — which is the same question [app.slotRefusal] answers from the writing
// end, asked here so the spend page draws a slot's absence only where the
// absence is a fact rather than a silence.
//
// THE CONVERSATION IS THE ONE IT HOLDS. The registry's reader for the other four
// answers nothing on this surface (settings.go's [app.registry] says so in as
// many words), and a page that turned that silence into `unbound` would be
// telling somebody nothing runs their work.
func (a *app) answersForSlot(slot config.ModelSlot) bool {
	return slot.Slot == talkSlot
}

// refreshSpend is the place clock's beat on this page: the cache reads only
// what has been appended since it last looked.
func (a *app) refreshSpend() {
	if !a.at(pageSpend) {
		return
	}
	a.readSpendLines(a.now())
}

// readSpendLines reads the ledger and rebuilds the reading over it.
//
// THE READ'S ERROR IS DROPPED AND ITS LINES ARE KEPT, which is the shape the
// cache is written for: a torn last line or an unreadable tail answers
// everything it could parse BESIDE the error rather than instead of it, and a
// page that threw away a fortnight of true figures because one row was half
// written would be the worse of the two wrong answers.
func (a *app) readSpendLines(now time.Time) {
	lines, _ := a.spend.cache.Read(time.Time{})
	a.spend.lines, a.spend.read = lines, now
	// AND THE WORLD WITH THE LINES, on the same beat and for the same reason the
	// bands and the world are read together on home: a ledger line minted by work
	// that started ten seconds ago has a title only in a scan taken after it.
	a.spend.world = a.readWorld()
	// AND THE JOIN IS MADE HERE, WITH THE WORLD IT IS MADE FROM. This is the one
	// moment the seams behind it may be touched — the open and the beat — so
	// that every keystroke after it, the window arrows included, is arithmetic
	// over what these two lines left behind.
	a.spend.names = a.spendNames(a.spend.world)
	a.spend.held = false
	for _, line := range lines {
		if line.USD > 0 {
			a.spend.held = true
			break
		}
	}
	a.rebuildSpend()
}

// rebuildSpend is the pure half: the window applied to the held lines, then the
// titles joined onto the ids the ledger carries.
func (a *app) rebuildSpend() {
	p := &a.spend
	p.reading = readSpend(p.lines, p.win, p.read).naming(p.names).crewed(a.spendCrewNow())
	// THE DOORS ARE SETTLED HERE AS WELL AS AT THE DRAW, and the two agree
	// because WHICH rows exist does not depend on the width — only what each of
	// them can fit does. Waiting for a draw would leave the cursor standing on
	// the header until the first frame, which is a real state on a window that
	// opened this place and has not painted yet.
	_, p.stops = p.reading.body(a.width, a.pal)
	p.cursor = a.nearestSpendStop(p.cursor)
}

// spendNames is the join the ledger cannot make: an id against the word a
// person calls that thing.
//
// THE LEDGER HOLDS IDS AND NOTHING ELSE and says so in its own header — no
// title for a task, none for a standing item, only the ids a page joins against
// the records it is already reading. So this walks the world's task index by
// (session, id) and the standing seam's items by id, and anything neither knows
// keeps the id: a row headed by an id is a poorer row than one headed by a
// title, and a far better one than a blank.
//
// IT IS BUILT FROM A WORLD THIS PLACE READ ITSELF, ON THE WAY IN. It used to read
// home's cached world — which is nil the moment home is left, and leaving home is
// exactly how a person gets here (`tab`, `alt+5`, the tab bar). Every row of
// `what it was for` then wore a raw id: `1`, `aaaa000000000002`, `release`. The
// scan is one walk of the places root on `open` and on the place clock's beat,
// which is what every other place pays for its own reading.
//
// AND THE PROMISES ARE ASKED OF EVERY PROJECT, not of this window's. A firing
// costs money in the workspace it fires in, so a page asking only about the
// project the window happens to be in cannot name a promise in any other one.
//
// IT IS ASKED ONCE, AND ONLY WHERE THE WORLD IS READ ([app.readSpendLines]).
// The orders came through [StandingSeam.Items], which is the store's List
// filtered to one workspace — so asking it per project walked the standing root
// once per project and parsed every document on the machine each time, to build
// one map. [StandingSeam.All] is the same answer for one read. A surface with no
// way to ask it at all — a connection, whose door answers by workspace — names
// no promise rather than fanning out into N reads, and those rows keep their
// ids, which the header above says is the poorer row and not the wrong one.
func (a *app) spendNames(world session.World) map[string]string {
	names := map[string]string{}
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			if title := strings.TrimSpace(row.Title); title != "" {
				names[session.SubjectConversation+"\x00"+row.ID] = title
			}
			for _, entry := range row.Tasks.Rows {
				if title := strings.TrimSpace(entry.Name); title != "" {
					names[session.SubjectTask+"\x00"+entry.ID] = title
				}
			}
		}
	}
	if a.stands.All == nil {
		return names
	}
	for _, item := range a.stands.All() {
		if title := strings.TrimSpace(item.Title()); title != "" {
			names[session.SubjectStanding+"\x00"+item.ID] = title
		}
	}
	return names
}

// spendFrame is this place, drawn: the shared frame with this place's body in
// it, and the hit map cast back into the body lines this place answers with
// (pages.go's [app.placeDraw] and [placeLineHits]).
func (a *app) spendFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := a.placeDraw(placeSpend{}, width, height)
	return lines, placeLineHits(hits), caretX, caretY
}

func (a *app) spendStopAt(i int) spendStop {
	if i < 0 || i >= len(a.spend.stops) {
		return spendStop{}
	}
	return a.spend.stops[i]
}

// nearestSpendStop is the first row at or after `from` that names something,
// and the last one before it when there is none. A page with no doors on it
// answers zero, and nothing is then drawn as chosen.
func (a *app) nearestSpendStop(from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(a.spend.stops); i++ {
		if a.spend.stops[i].ok {
			return i
		}
	}
	// THE WALK BACK STARTS INSIDE THE NEW PAGE AND NOT WHERE THE CURSOR WAS. A
	// window moved onto a quieter fortnight redraws with fewer rows — or none —
	// and a cursor left standing past the end would index a slice that has since
	// got shorter.
	for i := min(from, len(a.spend.stops)) - 1; i >= 0; i-- {
		if a.spend.stops[i].ok {
			return i
		}
	}
	return 0
}

// moveSpend walks the cursor by whole DOORS rather than by rows, so ↓ never
// lands on a sparkline or on a section heading nothing can be done to.
func (a *app) moveSpend(delta int) {
	var doors []int
	for i, stop := range a.spend.stops {
		if stop.ok {
			doors = append(doors, i)
		}
	}
	if len(doors) == 0 {
		return
	}
	at := 0
	for i, row := range doors {
		if row <= a.spend.cursor {
			at = i
		}
	}
	a.spend.cursor = doors[moveCursor(at, delta, len(doors))]
}

// ── the keys ────────────────────────────────────────────────────────────────

// spendKey is every key on this place. The router is read first and claims the
// four `shift+arrow` chords through [app.placeWindow]; what is left here is the
// cursor, the door and the way out.
func (a *app) spendKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		// ONE LAYER AT A TIME: a box with something in it is cleared first, and
		// the second esc leaves.
		if box := a.placeBox(); box != nil && !box.empty() {
			box.reset()
			a.touch()
			return nil
		}
		a.leavePlace()
		return nil
	case "up", "ctrl+p":
		a.moveSpend(-1)
		a.touch()
		return nil
	case "down", "ctrl+n":
		a.moveSpend(1)
		a.touch()
		return nil
	case "enter":
		if cmd, opened := a.openSpendRow(); opened {
			return cmd
		}
		// NO ROW UNDER THE CURSOR MEANS THE COMPOSER'S OWN ROAD: enter is what the
		// hint line says it is — talk about it, in a conversation.
		return a.placeTalk()
	}
	if box := a.placeBox(); box != nil {
		listNavigate(msg, box, a.moveSpend, func() {}, memoryPlaceRows)
		a.touch()
	}
	return nil
}

// openSpendRow is `enter` on a row of "what it was for": it opens THE THING THE
// MONEY WAS SPENT ON, which is the only door this page has and the only one it
// should have.
//
// A task goes to the task page, where its own record card is; a standing item
// goes to the standing place, where the promise it was made under lives; a
// conversation goes to home, which is the switcher and the one screen that can
// resolve a conversation id into an open window. Anything else opens nothing
// and says nothing, because a door onto a thing this build cannot find is worse
// than no door at all.
func (a *app) openSpendRow() (tea.Cmd, bool) {
	stop := a.spendStopAt(a.spend.cursor)
	if !stop.ok {
		return nil, false
	}
	switch stop.subject.Kind {
	case session.SubjectTask:
		return a.showPage(pageTasks), true
	case session.SubjectStanding:
		return a.showPage(pageStanding), true
	case session.SubjectConversation:
		return a.showPage(pageHome), true
	}
	return nil, false
}

// spendWindowKey is [app.placeWindow]'s spend arm: the four drawn arrow chords,
// and nothing else. It answers whether the window actually moved, so a key that
// changed nothing draws nothing.
func (a *app) spendWindowKey(key string) bool {
	if !a.at(pageSpend) {
		return false
	}
	// AND A KEY IS BOUND ONLY WHERE THE HALF OF THE CONTROL NAMING IT IS DRAWN.
	// One predicate answers the paint and the keys on every windowed place
	// (placeprose.go's [placeWindowFits]): a frame too narrow for the arrows has
	// no window at all, and one with room for the arrows but not for
	// `shift+↑ coarser` beside them has no zoom.
	width, _ := a.size()
	arrows, grain := placeWindowFits(width, spendHeadWords(a.spend.reading.totals), a.spend.win)
	if !arrows {
		return false
	}
	if (key == "shift+up" || key == "shift+down") && !grain {
		return false
	}
	next := a.spend.reading.step(a.spend.win, key)
	if next == a.spend.win {
		return false
	}
	a.spend.win = next
	// THE LINES ARE ALREADY IN MEMORY, so moving the window is arithmetic and
	// never a read. A fortnight back is the same cache answered a different
	// question, which is what lets a person hold the arrow down.
	a.rebuildSpend()
	return true
}

// ── the place ───────────────────────────────────────────────────────────────

// placeSpend is this place's handle on the registry: the frame asks it for a
// body, a window and a row's door, and it reads [app.spend] for all three
// (pages.go's [place] states the contract and why the handle holds no state).
type placeSpend struct{ placeBase }

func init() { registerPlace(placeSpend{}) }

func (placeSpend) id() page     { return pageSpend }
func (placeSpend) word() string { return "spend" }

func (placeSpend) open(a *app) tea.Cmd { return a.openSpend() }

// close writes the look stamp and drops the parsed ledger. A place left holding
// its lines behind a closed frame would go on being re-read on the clock while
// somebody stands somewhere else entirely.
func (placeSpend) close(a *app) {
	a.leavePage(pageSpend)
	a.spend = spendPage{}
}

func (placeSpend) tick(a *app, now time.Time) bool {
	a.refreshSpend()
	return true
}

// spendTeach is what this place says on a machine that has spent nothing.
//
// AN ALMOST-EMPTY PAGE IS THE BEST TEACHER ON THE MACHINE (SCREEN 1f). Nobody
// arrives at spend by accident — you walk into it from the tab bar, from
// `alt+5`, or by typing the word — and that arrival is the one moment a person
// is asking "what is this". So the place answers, in three sentences of dim
// prose in the body's own column, and says nothing else at all.
//
// IT IS NOT A PLACEHOLDER AND IT MUST NOT PRETEND TO BE ONE. There is no
// "coming soon", no greyed-out table with headings over it — a capability that
// cannot work is absent rather than broken (CLAUDE.md), and a page that draws
// the furniture of a feature it does not have looks like a bug rather than like
// a plan. Every sentence here is true today.
const spendTeach = "What this machine has cost, by the day, by the model, and by what it was for. " +
	"Every model call writes a line, so the figures here are the bill and not an estimate. " +
	"There is nothing to set here — the allowance is edited on the status line that shows it."

// body is the ledger, or — on a machine that has spent nothing inside the window
// it is showing — the three sentences saying what this place is for.
//
// IT ASKS THE TOTAL rather than drawing the body to see whether it is empty,
// because drawing it twice a frame to answer one question is the kind of waste a
// still page does not notice until it is on a clock.
func (placeSpend) body(a *app, width, room int) []placeRow {
	if a.spend.reading.totals.USD <= 0 {
		if !a.spend.held {
			return placeTeachRows(placeTeachProse(spendTeach, width, a.pal), room)
		}
		// THE HEADER STAYS, because it is the only thing on this frame naming the
		// window the four arrow keys move ([spendPage.held] holds the argument).
		rows := make([]placeRow, 0, room)
		rows = append(rows, placeRow{text: a.spend.reading.windowHeaderRow(width, a.pal), hit: -1})
		for len(rows) < room {
			rows = append(rows, placeRow{text: "", hit: -1})
		}
		a.spend.stops, a.spend.top, a.spend.shown = nil, 0, 0
		return rows
	}
	body, stops := a.spend.reading.body(width, a.pal)
	a.spend.stops = stops
	// THE WINDOW FOLLOWS THE CURSOR. A body cut at the room and never moved
	// loses the cursor off the bottom of the screen the moment the ledger is
	// longer than the terminal, which is the one thing a list may never do.
	a.spend.top = placeTop(a.spend.top, a.spend.cursor, len(body), room)
	rows := make([]placeRow, 0, room)
	for i := a.spend.top; i < len(body); i++ {
		if len(rows) >= room {
			break
		}
		text := body[i]
		if (i == a.spend.cursor || i == a.spend.hover) && a.spendStopAt(i).ok {
			text = a.pal.selected(text, width)
		}
		rows = append(rows, placeRow{text: text, hit: i})
	}
	a.spend.shown = len(rows)
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

// stops is every row of the body that names something money was spent on.
func (placeSpend) stops(a *app) []int {
	var doors []int
	for i, stop := range a.spend.stops {
		if stop.ok {
			doors = append(doors, i)
		}
	}
	return doors
}

// cursorAt is the row of the ledger the cursor is on (pages.go's
// [place.cursorAt]).
func (placeSpend) cursorAt(a *app) int { return a.spend.cursor }

func (placeSpend) enter(a *app) tea.Cmd {
	cmd, _ := a.openSpendRow()
	return cmd
}

func (placeSpend) window(a *app, key string) bool { return a.spendWindowKey(key) }

func (placeSpend) press(a *app, y int) bool {
	if at, ok := placeBodyLine(y, a.spend.top, a.spend.shown); ok && a.spendStopAt(at).ok {
		a.spend.cursor = at
		a.touch()
	}
	return true
}

func (placeSpend) hover(a *app, y int) bool {
	next := -1
	if at, ok := placeBodyLine(y, a.spend.top, a.spend.shown); ok && a.spendStopAt(at).ok {
		next = at
	}
	return placeHoverMoved(&a.spend.hover, next, a)
}

func (placeSpend) wheel(a *app, delta int) bool {
	a.moveSpend(delta)
	a.touch()
	return true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeSpend) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.spendKey(msg) }
