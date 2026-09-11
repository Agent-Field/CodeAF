package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
	// lens is which READING of the held lines is on screen (spendlens.go). Rhythm
	// is the default; `[` / `]` and `/spend models|days|year` move it.
	lens spendLens
	// group and sort are the Models / Days lens controls (`g`, `c` / `t`).
	group spendGroup
	sort  spendSort
	// priorWin, priorLens and priorDay remember the Days view a day-drill left,
	// so esc returns there with the cursor on the day you opened (spendlens.go).
	priorWin  session.UsageWindow
	priorLens spendLens
	priorDay  time.Time
	drilled   bool
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
	// unfolded is whether the subjects' fold is open. It lasts while the place
	// is up and a fresh visit starts it shut, as every fold on a place does.
	unfolded bool
	// woke is whether focus has been put at the page's centre of mass yet on
	// this visit; after that the cursor is the person's.
	woke bool
	// read is the instant the lines were read, and every figure and age on the
	// page is measured from it rather than from a fresh clock.
	read time.Time
	// held is whether the ledger holds ANY priced line at all, in any window, and
	// it is what tells the two empty pages apart — the same fact and the same
	// argument as the standing and tasks places' own ([standingPlace.held]).
	//
	// A WINDOW EMPTIED BY THE ARROWS IS NOT AN EMPTY MACHINE. Both draw no rows,
	// and the right answer to each is the opposite of the other: a machine that
	// has spent nothing wants the frame to say what arrives here
	// ([placeWhisper]), while a window paged onto a quiet fortnight wants the
	// HEADER — the control that pages it back — above a dim guide naming the
	// keys that leave ([spendQuietGuide]). Drawing the whisper in both cases
	// swallowed the only way out of the second.
	held bool
	// known is whether the ledger seam has answered. A far cache warms a beat
	// later; until then the body draws a skeleton ([spendWarmingRows]) rather
	// than the empty-machine whisper, which would lie about a bill still on the
	// wire. Local reads are known the moment they return.
	known bool
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

// spendCenterOfMass is the row focus wakes on: THE FIRST THING THE MONEY WENT
// ON, at the head of `what it was for` — the row this page exists to answer.
// It woke on the pointer line, a door to the limits editor, so the first
// `enter` on arrival left the bill for a settings tab (PLACES-AUDIT.md finding
// 16). A page with no subjects wakes where it always did.
func (a *app) spendCenterOfMass() int {
	at := a.spend.cursor
	if len(a.spend.reading.subjects) == 0 {
		return at
	}
	first := spendSubjectKey(a.spend.reading.subjects[0])
	// THE LAST ROW NAMING IT, because the loudest day above the table can name
	// the same subject and the table's own row is the one under its heading.
	for i, stop := range a.spend.stops {
		if stop.ok && !stop.rails && !stop.fold && spendSubjectKey(stop.subject) == first {
			at = i
		}
	}
	return at
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
	lines, held := []session.UsageLine(nil), false
	if a.ledger != nil {
		var known bool
		lines, held, known = a.ledger(a.spend.win.From)
		if !known {
			// KEEP THE LAST ANSWER where we had one. A warm miss after a real
			// reading must not flash the skeleton over a fortnight somebody was
			// already looking at; the first visit with no answer yet stays
			// unknown and draws [spendWarmingRows].
			return
		}
		a.spend.known = true
	} else {
		lines, _ = a.spend.cache.Read(time.Time{})
		a.spend.known = true
	}

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
	a.spend.held = held
	if a.ledger == nil {
		for _, line := range lines {
			if line.USD > 0 {
				a.spend.held = true
				break
			}
		}
	}
	a.rebuildSpend()
}

// usageSince is THE DOOR ONTO THE MACHINE'S SPENDING for a reader that is not
// standing on this page — the pulse at the top of every place
// ([app.machineSpentToday]) — and it goes through the same two answers
// [app.readSpendLines] goes through, in the same order.
//
// THE SEAM COMES FIRST BECAUSE THE LEDGER MAY NOT BE ON THIS DISK. Over a
// connection the money belongs to the far machine and arrives through a cache the
// link keeps warm (tui3.go's [Options.Ledger]); a reader that opened
// [app.usageLedger] there would be drawing THIS laptop's bill on a screen about
// somebody else's machine, and PERF.md's law that a frame over a connection asks
// the far machine nothing is why it is that cache and never the wire.
//
// THE BOOL IS "IS THIS AN ANSWER" AND NOT "IS THERE ANY MONEY". A far machine
// that has not replied yet, and a ledger this process cannot open, have both said
// NOTHING — and the emptiness law draws an unknown as an absent segment rather
// than as a zero. A machine that has genuinely spent nothing answers no lines and
// true.
func (a *app) usageSince(from time.Time) ([]session.UsageLine, bool) {
	if a.ledger != nil {
		lines, _, known := a.ledger(from)
		return lines, known
	}
	lines, err := session.ReadUsage(a.usageLedger, from)
	if err != nil {
		return nil, false
	}
	return lines, true
}

// rebuildSpend is the pure half: the window applied to the held lines, then the
// titles joined onto the ids the ledger carries.
func (a *app) rebuildSpend() {
	p := &a.spend
	p.reading = readSpend(p.lines, p.win, p.read).naming(p.names).crewed(a.spendCrewNow()).
		railed(a.machineAllowance()).lost(session.UsageDrops()).
		todayed(spendDayTotal(p.lines, p.read)).unfolding(p.unfolded)
	// THE DOORS ARE SETTLED HERE AS WELL AS AT THE DRAW, and the two agree
	// because WHICH rows exist does not depend on the width — only what each of
	// them can fit does. Waiting for a draw would leave the cursor standing on
	// the header until the first frame, which is a real state on a window that
	// opened this place and has not painted yet.
	_, p.stops = p.reading.paintLens(p.lens, p.group, p.sort, a.width, a.pal, nil)
	p.cursor = a.nearestSpendStop(p.cursor)
	// FOCUS WAKES ONCE, on the first reading that has anything to wake on —
	// which is not always the one taken on the way in: a far machine's ledger
	// answers a beat later ([app.spendCenterOfMass]).
	if !p.woke && len(p.reading.subjects) > 0 {
		p.cursor = a.spendCenterOfMass()
		p.woke = true
	}
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

// focusSpendDay puts the cursor on the Days-lens row for day, or leaves it
// alone when that day is not on the page. Esc after a drill uses this so the
// person lands back on the day they opened rather than on the loudest day.
func (a *app) focusSpendDay(day time.Time) {
	if day.IsZero() {
		return
	}
	for i, stop := range a.spend.stops {
		if stop.ok && !stop.day.At.IsZero() && sameSpendBucket(stop.day.At, day, session.GrainDay) {
			a.spend.cursor = i
			return
		}
	}
}

// ── the keys ────────────────────────────────────────────────────────────────

// spendKey is every key on this place. The router is read first and claims the
// four `shift+arrow` chords through [app.placeWindow]; what is left here is the
// cursor, the door and the way out.
func (a *app) spendKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		// ONE LAYER AT A TIME: a day drill unwinds first, then a box with
		// something in it is cleared, and the next esc leaves.
		if a.spend.drilled {
			a.spend.win = a.spend.priorWin
			a.spend.lens = a.spend.priorLens
			day := a.spend.priorDay
			a.spend.drilled = false
			a.spend.priorDay = time.Time{}
			a.spend.woke = true // keep the person's cursor; do not re-centre
			a.rebuildSpend()
			if !day.IsZero() {
				a.focusSpendDay(day)
			}
			a.touch()
			return nil
		}
		if box := a.placeBox(); box != nil && !box.empty() {
			box.reset()
			a.touch()
			return nil
		}
		a.leavePlace()
		return nil
	case "]":
		if box := a.placeBox(); box == nil || box.empty() {
			a.cycleSpendLens(1)
			return nil
		}
	case "[":
		if box := a.placeBox(); box == nil || box.empty() {
			a.cycleSpendLens(-1)
			return nil
		}
	case "g":
		if a.spend.lens == spendLensModels {
			a.spend.group = a.spend.group.next()
			a.spend.woke = false
			a.rebuildSpend()
			a.touch()
			return nil
		}
	case "c":
		if a.spend.lens == spendLensModels || a.spend.lens == spendLensDays {
			a.spend.sort = spendSortCost
			a.spend.woke = false
			a.rebuildSpend()
			a.touch()
			return nil
		}
	case "t":
		if a.spend.lens == spendLensModels || a.spend.lens == spendLensDays {
			a.spend.sort = spendSortTokens
			a.spend.woke = false
			a.rebuildSpend()
			a.touch()
			return nil
		}
	case "s":
		// Cycle every Models column (head-cell order). Days stay on cost↔tokens.
		if a.spend.lens == spendLensModels {
			a.spend.sort = a.spend.sort.next()
			a.spend.woke = false
			a.rebuildSpend()
			a.touch()
			return nil
		}
		if a.spend.lens == spendLensDays {
			a.spend.sort = a.spend.sort.nextDay()
			a.spend.woke = false
			a.rebuildSpend()
			a.touch()
			return nil
		}
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

// cycleSpendLens is `[` / `]`: one step along the lens cycle, and a re-read when
// the Year lens needs a longer floor than the fortnight the page opened on.
func (a *app) cycleSpendLens(dir int) {
	if dir < 0 {
		a.setSpendLens(a.spend.lens.prev())
		return
	}
	a.setSpendLens(a.spend.lens.next())
}

// setSpendLens lands on one lens and rebuilds. Year may need lines older than
// the current window's From; Rhythm/Models/Days keep the window the arrows set.
func (a *app) setSpendLens(lens spendLens) {
	a.spend.lens = lens
	a.spend.woke = false
	if lens == spendLensYear {
		year := yearWindow(a.now())
		if a.spend.win.From.After(year.From) {
			// Keep the fortnight on the head; only the read floor moves so the
			// heatmap has a year of lines to draw.
			a.readSpendFrom(year.From)
			return
		}
	}
	a.rebuildSpend()
	a.touch()
}

// readSpendFrom re-reads the ledger from a floor without moving the window.
func (a *app) readSpendFrom(from time.Time) {
	now := a.now()
	lines, held := []session.UsageLine(nil), false
	if a.ledger != nil {
		var known bool
		lines, held, known = a.ledger(from)
		if !known {
			a.rebuildSpend()
			a.touch()
			return
		}
		a.spend.known = true
	} else {
		lines, _ = a.spend.cache.Read(from)
		a.spend.known = true
	}
	a.spend.lines, a.spend.read = lines, now
	a.spend.world = a.readWorld()
	a.spend.names = a.spendNames(a.spend.world)
	a.spend.held = held
	if a.ledger == nil {
		for _, line := range lines {
			if line.USD > 0 {
				a.spend.held = true
				break
			}
		}
	}
	a.rebuildSpend()
	a.touch()
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
	if !stop.ok || stop.lensBar {
		return nil, false
	}
	// AND THE POINTER LINE OPENS THE ONE EDITOR MONEY HAS. It is the only row
	// here that is not a thing money was spent on, and the only door out of this
	// place that goes somewhere a person can change something (moneydoor.go's
	// [app.openSpending]).
	if stop.rails {
		return a.openSpending(spendTodayKey), true
	}
	// THE MODELS SORT HEAD CYCLES THE ACTIVE COLUMN. Enter on the head row is
	// the keyboard twin of pressing a head cell (DESIGN.md §3).
	if stop.sortHead {
		a.spend.sort = a.spend.sort.next()
		a.spend.woke = false
		a.rebuildSpend()
		return nil, true
	}
	// THE FOLD LINE OPENS WHERE IT STANDS, and the cursor stays on it: the
	// line is still there, now saying `fewer`, so the next `enter` undoes it.
	if stop.fold {
		a.spend.unfolded = !a.spend.unfolded
		a.rebuildSpend()
		return nil, true
	}
	// A DAYS-LENS ROW DRILLS INTO THAT DAY: the window becomes the day, the lens
	// returns to Rhythm, and the models/subjects under it are that day's bill.
	// Quiet days still drill — a day with nothing priced is still a named window
	// (emptiness draws no figure; the door stays) so enter never lands on a blank
	// refuse (docs/design/spend-lenses/DESIGN.md §2).
	if !stop.day.At.IsZero() {
		a.spend.priorWin = a.spend.win
		a.spend.priorLens = a.spend.lens
		a.spend.priorDay = stop.day.At
		a.spend.drilled = true
		day := session.UsageWindow{From: stop.day.At, To: stop.day.At, Grain: session.GrainDay}
		a.spend.win = day.Normalized()
		a.spend.lens = spendLensRhythm
		a.spend.woke = false
		a.rebuildSpend()
		return nil, true
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
	arrows, grain := placeWindowFits(width, a.spend.reading.headWords(width), a.spend.win)
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

// body is the ledger, or — on a machine that has spent nothing at all — the
// place's heading and its whisper (placeprose.go's [placeWhisper]).
//
// IT ASKS THE TOTAL rather than drawing the body to see whether it is empty,
// because drawing it twice a frame to answer one question is the kind of waste a
// still page does not notice until it is on a clock.
// remote is this place over --host: the ledger it adds up is the file every
// window on THIS machine appends a model call to, and the calls this session
// makes are billed on the other one (pages.go's [place.remote]).
func (placeSpend) remote(a *app) string {
	if a.hosted() && a.ledger == nil {
		return spendRemoteWord
	}
	return ""
}

func (placeSpend) body(a *app, width, room int) []placeRow {
	// A LEDGER STILL ON THE WIRE is a skeleton, not the empty-machine whisper:
	// whispering "priced as it runs" over a bill that has not arrived yet is a
	// lie about an unknown, and the emptiness law draws unknown as absent —
	// here the honest absent is the warming line, never `$0.00`.
	if !a.spend.known {
		return spendWarmingRows(width, room, a.pal)
	}
	if a.spend.reading.empty() && a.spend.lens != spendLensYear {
		if !a.spend.held {
			return placeWhisperRows(pageSpend, width, room, a.pal)
		}
		// THE HEADER STAYS, because it is the only thing on this frame naming the
		// window the four arrow keys move ([spendPage.held] holds the argument).
		// Under it sits the quiet guide — how to leave — not blank air.
		return spendQuietWindowRows(a, width, room)
	}
	if a.spend.lens == spendLensYear && !a.spend.held && a.spend.reading.empty() {
		return placeWhisperRows(pageSpend, width, room, a.pal)
	}
	lit := func(i int) bool { return (i == a.spend.cursor || i == a.spend.hover) && a.spendStopAt(i).ok }
	body, stops := a.spend.reading.paintLens(a.spend.lens, a.spend.group, a.spend.sort, width, a.pal, lit)
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
		if lit(i) {
			text = placeBand(text, width, a.pal)
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

// rowID names the row the cursor is standing on, so a verb strip opened over it
// closes the moment the cursor walks away (verbstrip.go's [app.holdStrip]).
func (placeSpend) rowID(a *app) string { return "spend/" + itoa(a.spend.cursor) }

func (placeSpend) enter(a *app) tea.Cmd {
	cmd, _ := a.openSpendRow()
	return cmd
}

func (placeSpend) window(a *app, key string) bool { return a.spendWindowKey(key) }

// verbs is what `→` opens over the row under the cursor, and on this place it is
// one letter: `b`, the limits.
//
// THIS IS WHERE THE DESIGN'S `b` IS REAL. A bare letter may be a verb only while
// the strip naming it is on the screen (verbstrip.go's first law) — every
// printable key belongs to the composer otherwise, which is the product and not
// a compromise — so `b` is drawn before it works, and it works on every row of
// this place because every row of this place is about money.
func (placeSpend) verbs(a *app) []verb {
	if stop := a.spendStopAt(a.spend.cursor); !stop.ok || stop.fold {
		return nil
	}
	return []verb{{key: 'b', word: "the limits", do: func() tea.Cmd {
		return a.openSpending(spendTodayKey)
	}}}
}

// The clauses this place's foot is built from. They are constants because the
// manual quotes them and because two of them are the two keys a person standing
// on a row of this page would actually press.
//
// THE ROUTER'S DEFAULT USED TO SAY THIS PLACE'S FOOT FOR IT, and it named none
// of the four key classes here: `enter` on a row, `→` with one verb on it,
// the `▸ 14 more` fold and the shift-arrow window. What it said instead was
// `enter talk about it · alt+enter send it off as a task`, so the one key that
// WAS written down meant something else (pages.go says why [placeBase] no
// longer carries a `hint` at all).
const (
	spendEnterWord  = "enter opens what spent it"
	spendVerbLead   = "→ "
	spendWindowWord = "shift+←→ move the days"
	// spendLensWord is the FOOT's cycle clause. The chip strip under the rails
	// already names every lens; the foot keeps the keys and the NEXT lens so a
	// person who prefers the keyboard still finds `] models` from rhythm.
	spendLensWord = "[ ] lenses"
)

// spendLensHint is the foot clause for the lens cycle: `[ ] lenses · ] models`
// on rhythm, and the same shape for every other lens. The head already names
// the ACTIVE lens; the foot names the door out.
func spendLensHint(lens spendLens) string {
	next := lens.next().word()
	if next == "" {
		return spendLensWord
	}
	return spendLensWord + " · ] " + next
}

// hint is the foot, assembled from the clauses that are TRUE of the row under
// the cursor and of the window this frame is drawing.
//
// The shift arrows are named only where they are BOUND: the window control
// draws its own arrows on the head row and stands down on a frame too narrow to
// hold them ([app.spendWindowKey] asks [placeWindowFits] the same question), and
// a foot promising them under a head that is not drawing them would be this
// surface advertising a key that does nothing.
func (placeSpend) hint(a *app) string {
	var parts []string
	// A WARMING LEDGER HAS NO KEYS TO CYCLE. The foot used to name `[ ] lenses`
	// over a skeleton with nothing behind it.
	if !a.spend.known {
		return "esc"
	}
	// AN EMPTY MACHINE HAS NO LENS TO CYCLE. The foot used to name `[ ] lenses`
	// over a whisper with nothing behind it, which advertised a key that moved
	// nothing a person could see.
	if a.spend.held || !a.spend.reading.empty() {
		// THE ACTIVE LENS IS ON THE HEAD. The foot names the cycle keys and the
		// next lens, so a person on rhythm (today's arrival) is told `] models`
		// rather than guessing what `[ ] lenses` opens.
		parts = append(parts, spendLensHint(a.spend.lens))
		if a.spend.lens == spendLensModels {
			parts = append(parts, spendGroupKeyWord, spendSortHeadWord)
		}
		if a.spend.lens == spendLensDays {
			parts = append(parts, spendSortKeyWord)
			if stop := a.spendStopAt(a.spend.cursor); !stop.day.At.IsZero() {
				parts = append(parts, "enter opens that day")
			}
		}
	}
	if stop := a.spendStopAt(a.spend.cursor); stop.fold {
		parts = append(parts, foldEnterWord(a.spend.unfolded))
	} else if stop.sortHead {
		parts = append(parts, "enter cycles the sort")
	} else if stop.ok && stop.day.At.IsZero() {
		parts = append(parts, spendEnterWord)
		for _, v := range (placeSpend{}).verbs(a) {
			parts = append(parts, spendVerbLead+v.word)
		}
	}
	width, _ := a.size()
	if arrows, _ := placeWindowFits(width, a.spend.reading.headWords(width), a.spend.win); arrows {
		parts = append(parts, spendWindowWord)
	}
	if len(parts) == 0 {
		return "esc"
	}
	return strings.Join(parts, railSep) + railSep + "esc"
}

func (placeSpend) press(a *app, y int) (tea.Cmd, bool) {
	at, ok := placeBodyLine(y, a.spend.top, a.spend.shown)
	if !ok {
		return nil, true
	}
	stop := a.spendStopAt(at)
	// A PRESS ON A LENS CHIP SWITCHES THE READING. The strip is chrome, not a
	// cursor door — clickX is the only way to know which word was under the
	// pointer (settings.go's [sheetPress] uses the same column for its tabs).
	if stop.lensBar {
		if lens, hit := spendLensAtColumn(a.clickX); hit {
			a.setSpendLens(lens)
		}
		return nil, true
	}
	if stop.ok {
		a.spend.cursor = at
		a.touch()
		return placeSpend{}.enter(a), true
	}
	return nil, true
}

func (placeSpend) hover(a *app, y int) bool {
	next := -1
	if at, ok := placeBodyLine(y, a.spend.top, a.spend.shown); ok && a.spendStopAt(at).ok {
		next = at
	}
	return placeHoverMoved(&a.spend.hover, next, a)
}

func (placeSpend) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.moveSpend(delta)
	a.touch()
	return nil, true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeSpend) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.spendKey(msg) }

// spendWarmingRows is the skeleton while the ledger seam has not answered.
//
// HEADING + HONEST STATUS + GHOST STRUCTURE. The ghosts are floor spark cells
// and dim bullets with no labels and no dollars — presence of the page's shape
// without inventing a bill. Never `$0.00`, never `0 tok`.
func spendWarmingRows(width, room int, pal palette) []placeRow {
	rows := make([]placeRow, 0, room)
	if width >= len(placeLead)+1 {
		rows = append(rows, placeRow{
			text: placeLead + placeHeading(fit(pageSpend.word(), width-len(placeLead)), pal),
			hit:  -1,
		})
	}
	for _, words := range homeWhisperLines(spendWarmingWord, width-len(placeLead)) {
		if width < len(placeWhisperLead)+1 {
			break
		}
		rows = append(rows, placeRow{text: placeWhisperLead + pal.dim(words), hit: -1})
	}
	if len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	inner := width - len(placeLead)
	if inner >= spendWindowDays && len(rows) < room {
		ghost := strings.Repeat(tokens.Sparkline(0), spendWindowDays)
		rows = append(rows, placeRow{text: placeLead + pal.dim(fit(ghost, inner)), hit: -1})
	}
	bullet := tokens.GlyphProseBullet
	for i := 0; i < 3 && len(rows) < room; i++ {
		rows = append(rows, placeRow{text: placeLead + pal.dim(bullet), hit: -1})
	}
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

// spendQuietWindowRows is a held ledger paged onto a stretch that spent
// nothing: rails pointer, lens chips, window head with the control, and the
// quiet guide — how to leave — rather than blank air under the head. The chips
// stay so a quiet stretch still offers models / days / year by eye.
func spendQuietWindowRows(a *app, width, room int) []placeRow {
	rows := make([]placeRow, 0, room)
	inner := width - len(placeLead)
	r := a.spend.reading
	rails := placeLead + r.railsRowIn(inner, a.pal.dim)
	rows = append(rows, placeRow{text: rails, hit: 0})
	rows = append(rows, placeRow{text: spendLensBar(width, a.spend.lens, a.pal), hit: 1})
	rows = append(rows, placeRow{text: r.windowHeaderRowFor(a.spend.lens, width, a.pal), hit: -1})
	if inner > 0 {
		rows = append(rows, placeRow{text: "", hit: -1})
		rows = append(rows, placeRow{text: placeLead + a.pal.dim(fit(spendQuietGuide, inner)), hit: -1})
	}
	a.spend.stops = make([]spendStop, len(rows))
	if len(a.spend.stops) > 0 {
		a.spend.stops[0] = spendStop{ok: true, rails: true}
	}
	if len(a.spend.stops) > 1 {
		a.spend.stops[1] = spendStop{lensBar: true}
	}
	a.spend.top, a.spend.shown = 0, len(rows)
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}
