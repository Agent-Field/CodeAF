package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
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
	// hover is the line of the reading the pointer is over, and -1 for none.
	// Mouse navigation moves the shared cursor to this line.
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
	// HEADER — the control that pages it back — above nothing at all. Drawing the
	// whisper in both cases swallowed the only way out of the second.
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
	// seats is THE RUN'S OWN SPEND, rolled up by seat and held here for the same
	// reason [spendPage.names] is: it is read from the plan store, which touches
	// seams, so it is read on the open and on the beat and never on the draw
	// ([app.askSpendSeats], [session.Agent.PlanSpend]). THE WINDOW ARROWS RE-ASK IT
	// and they are the one keystroke that does: the rollup arrives already summed
	// over a window, and a sum cannot be cut down to a narrower one the way the
	// ledger's own lines can.
	//
	// IT IS ASKED OFF THE UPDATE LOOP, because over a connection this read is a
	// door to another process. The lines land here through a fold, on the next
	// pass, exactly as every other door's answer does (offloop.go).
	seats []session.PlanSpendLine
	// slice is WHICH CUT OF THE LEDGER IS DRAWN ([spendSlice]) — `by topic` on
	// the way in, and `by model` a keystroke away. It lives on the page rather
	// than in the reading for [spendPage.unfolded]'s reason: a reading is an
	// immutable answer and this is a thing a person did to the page.
	slice spendSlice
}

// spendWindowDays is the window this place opens on: a fortnight, by the day.
// It is a fortnight because that is long enough to show a working rhythm and
// short enough that one cell is one day at every width the surface promises.
const spendWindowDays = 14

// openSpend walks into the spend place: one read of the ledger, and the clock
// that keeps it current.
func (a *app) openSpend() tea.Cmd {
	now := a.now()
	// THE PAGE OPENS ON `by topic`, which is the question a person walks in with:
	// `by model` answers a narrower one and is one keystroke away. The zero
	// [spendSlice] is that cut, so the field is not spelled here.
	a.spend = spendPage{cache: session.UsageCache{Path: a.usageLedger},
		win: session.LastDays(now, spendWindowDays), hover: -1,
		world: a.readWorld()}
	a.readSpendLines(now)
	// THE SEATS ARE ASKED OFF THE LOOP AND BATCHED WITH THE CLOCK: walking in
	// reads the run's seat spend once, and the clock keeps it current.
	return tea.Batch(a.askSpendSeats(a.spend.win.From), a.armPlaceClock())
}

// spendCenterOfMass is the row focus wakes on: THE FIRST THING THE MONEY WENT
// ON, at the head of `what it was for` — the row this page exists to answer.
// It woke on the pointer line, a door to the limits editor, so the first
// `enter` on arrival left the bill for a settings tab (PLACES-AUDIT.md finding
// 16). A page with no subjects wakes where it always did.
func (a *app) spendCenterOfMass() int {
	at := a.spend.cursor
	// AND ONLY WHERE THAT ROW IS DRAWN. `by model` draws no subject rows at all
	// ([spendSlice]), so a wake that hunted one there would leave the cursor
	// standing where the nearest stop happened to be rather than where this
	// place put it.
	if a.spend.slice != spendByTopic || len(a.spend.reading.subjects) == 0 {
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

// planSpendAgent is the slice of [session.Agent] the spend page reads a run's
// seat spending through. It is asserted rather than added to [planAgent] so that
// the tasks place's own fake, which knows nothing of a spend rollup, keeps
// answering exactly the plan seam it already answers.
type planSpendAgent interface {
	// PlanSpend is this conversation's plan spend rolled up by seat: one line
	// per role the store charged, the model that seat most spent through, and
	// the dollars and calls since a moment. Nil is the honest answer for a
	// conversation with no plan store or nothing priced in the window.
	PlanSpend(since time.Time) []session.PlanSpendLine
}

// askSpendSeats asks the run's seat spend for the window the page draws, OFF
// the update loop, and folds the lines back into the reading.
//
// IT IS A COMMAND AND NOT A READ, which is the whole of what the door crossing
// the wire changed. The plan store and its spend ledger live on the engine's
// disk, so over a connection this is a call to another process that can take the
// round trip's whole deadline; asked from Update it would freeze the window
// while the engine answered (offloop.go). What it found is folded in on the next
// pass, exactly as every other door here is.
//
// A SURFACE WITH NO PLAN SEAM ASKS NOTHING. The assertion failing is the honest
// nil the page draws as the block's heading and whisper, decided here rather
// than paid for on a round trip that has nothing to carry.
func (a *app) askSpendSeats(since time.Time) tea.Cmd {
	reader, ok := a.agent.(planSpendAgent)
	if !ok {
		a.spend.seats = nil
		a.rebuildSpend()
		return nil
	}
	return a.offLoop(func() func(here bool) tea.Cmd {
		lines := reader.PlanSpend(since)
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			a.spend.seats = lines
			a.rebuildSpend()
			return nil
		}
	})
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
			return
		}
	} else {
		lines, _ = a.spend.cache.Read(time.Time{})
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
		seated(p.seats).railed(a.machineAllowance()).lost(session.UsageDrops()).
		todayed(spendDayTotal(p.lines, p.read)).unfolding(p.unfolded).slicing(p.slice)
	// THE DOORS ARE SETTLED HERE AS WELL AS AT THE DRAW, and the two agree
	// because WHICH rows exist does not depend on the width — only what each of
	// them can fit does. Waiting for a draw would leave the cursor standing on
	// the header until the first frame, which is a real state on a window that
	// opened this place and has not painted yet.
	_, p.stops = p.reading.body(a.width, a.pal)
	// AN EMPTY WINDOW STILL HAS ITS ONE CONTROL, and it is settled here like
	// every other stop rather than by the draw ([placeSpend.body] paints the
	// matching rows). A reading with nothing priced in it answers no rows at
	// all, so without this the cut's arrows would be unbound until the first
	// frame — and on a window paged back onto a quiet fortnight that is the only
	// row there is.
	if len(p.stops) == 0 && p.held {
		p.stops = []spendStop{{}, {ok: true, slice: true}}
	}
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
	case "left", "right":
		// `←` AND `→` STEP THE CUT, AND ONLY ON THE ROW THAT DRAWS THEM. The
		// router offers this place the arrow after the verb strip has declined it
		// (placekeys.go: "the arrow keeps every meaning it already had on that
		// place"), and the heading declines the strip ([placeSpend.verbs]) — so
		// the two claims on one key never meet.
		if !a.spendStopAt(a.spend.cursor).slice {
			return nil
		}
		step := 1
		if msg.String() == "left" {
			step = -1
		}
		a.stepSpendSlice(step)
		return nil
	case "enter":
		// A ROW OPENS, AND NOTHING ELSE HAPPENS: this place has no box, and only
		// home starts things ([place.box]).
		cmd, _ := a.openSpendRow()
		return cmd
	}
	listNavigate(msg, nil, a.moveSpend, func() {}, memoryPlaceRows)
	a.touch()
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
	// AND THE POINTER LINE OPENS THE ONE EDITOR MONEY HAS. It is the only row
	// here that is not a thing money was spent on, and the only door out of this
	// place that goes somewhere a person can change something (moneydoor.go's
	// [app.openSpending]).
	if stop.rails {
		return a.openSpending(spendTodayKey), true
	}
	// THE FOLD LINE OPENS WHERE IT STANDS, and the cursor stays on it: the
	// line is still there, now saying `fewer`, so the next `enter` undoes it.
	if stop.fold {
		a.spend.unfolded = !a.spend.unfolded
		a.rebuildSpend()
		return nil, true
	}
	// AND THE CUT'S HEADING SWAPS THE CUT, on `enter` exactly as on `→`. A row
	// drawn with arrows on it whose `enter` did nothing would be the one row of
	// this page that answers the key every other row answers with silence.
	if stop.slice {
		a.stepSpendSlice(1)
		return nil, true
	}
	switch stop.subject.Kind {
	case session.SubjectTask:
		// THE RECORD CARD FOR THAT PIECE OF WORK, and not merely the page it is
		// filed on. `enter opens it in tasks` was three quarters true: it opened
		// the place and left the person to find their own row in a list of
		// everything this machine has ever run.
		if record := a.spendTaskRecord(stop.subject); record != nil {
			return a.openTaskRecord(record), true
		}
		// AND A ROW WHOSE WORK THE RECORD NO LONGER HOLDS SAYS SO AND STAYS PUT.
		// It walked to the tasks place instead — a page about everything this
		// machine has run, opened in answer to `enter` on one row of a bill — and
		// a person then had to work out for themselves that the thing they asked
		// for was not there. A door onto a thing this build cannot find is worse
		// than no door at all, and the place's own message line is where a
		// refusal goes (place_search.go's [app.openConversationRow] says it the
		// same way).
		a.pageMsg = spendGoneTaskWord
		return nil, true
	case session.SubjectStanding:
		return a.openStandingAt(stop.subject.ID), true
	case session.SubjectConversation:
		// AND A CONVERSATION IS OPENED, not merely pointed at. This arm went to
		// home — the switcher — on the argument that home is the one screen that
		// can resolve a conversation id into a window; what a person pressing
		// `enter` on a row of their own bill actually gets from that is the home
		// page, with the conversation they named nowhere on it.
		//
		// [app.openConversationRow] is the door every place that is not home
		// already uses (place_search.go), with all of its refusals: a transcript
		// this terminal is already holding is brought forward rather than
		// reopened, a folder that has since gone says so, and the conversation
		// this window was in is detached rather than closed.
		if row, ok := a.spendSessionRow(stop.subject); ok {
			return a.openConversationRow(row), true
		}
		a.pageMsg = spendGoneTalkWord
		return nil, true
	}
	return nil, false
}

// spendTaskRecord is the record row for a task subject, out of the world this
// place is already holding.
//
// IT MATCHES ON THE PAIR, AND THE PAIR IS (id, CONVERSATION). An id alone is not
// unique across the record — ids restart with every conversation
// ([session.TaskIndexEntry.ID]) — so two conversations each holding a task `7`
// are two different pieces of work under one name.
//
// THE CONVERSATION IS [session.SubjectSpend.Root] AND NOT ITS Session. This
// matched Session for one build and therefore matched nothing: the ledger writes
// the task node's OWN journal id there, while the index's SessionID is the
// conversation that ran it. Every task row fell through to an id-only fallback,
// and `enter` opened whichever conversation's `7` the world walked first. The
// fixtures were green because they put the conversation in Session, which no
// real ledger line does.
//
// A LEDGER LINE WITH NO ROOT IS THE ONLY PLACE THE ID STANDS ALONE. Lines
// written before that field was read carry no conversation, so there is nothing
// to disambiguate with and the first row of that id is the honest answer — but
// a row that HAS a conversation and does not match is a different piece of work,
// and opening it would be worse than opening nothing.
func (a *app) spendTaskRecord(subject session.SubjectSpend) *session.TaskIndexEntry {
	id, root := strings.TrimSpace(subject.ID), strings.TrimSpace(subject.Root)
	if id == "" {
		return nil
	}
	for _, project := range a.spend.world.Projects {
		for _, row := range project.Sessions {
			for at := range row.Tasks.Rows {
				entry := &row.Tasks.Rows[at]
				if strings.TrimSpace(entry.ID) != id {
					continue
				}
				if root == "" || strings.TrimSpace(entry.SessionID) == root {
					return entry
				}
			}
		}
	}
	return nil
}

// spendSessionRow is the world's record of a conversation subject, in the shape
// every door onto a conversation on this surface takes
// ([app.openConversationRow]).
func (a *app) spendSessionRow(subject session.SubjectSpend) (session.SessionRow, bool) {
	id := strings.TrimSpace(subject.ID)
	if id == "" {
		return session.SessionRow{}, false
	}
	for _, project := range a.spend.world.Projects {
		for _, row := range project.Sessions {
			if strings.TrimSpace(row.ID) == id {
				return row, true
			}
		}
	}
	return session.SessionRow{}, false
}

// stepSpendSlice walks the ring of cuts and keeps the cursor on the control.
//
// THE CURSOR STAYS ON THE HEADING. A cut swapped under a cursor that then went
// hunting for the nearest stop would leave the person one keystroke from the
// control they had just used and no way of knowing where it went; the heading is
// the same row in both cuts, so the cursor has nowhere to go.
func (a *app) stepSpendSlice(by int) {
	a.spend.slice = a.spend.slice.step(by)
	a.rebuildSpend()
	for at, stop := range a.spend.stops {
		if stop.slice {
			a.spend.cursor = at
			break
		}
	}
	a.touch()
}

// spendWindowKey is [app.placeWindow]'s spend arm: the four drawn arrow chords,
// and nothing else. It answers whether the window actually moved, so a key that
// changed nothing draws nothing — and, where it moved, the command that asks the
// run's seat spend again over the window now drawn (off the update loop).
func (a *app) spendWindowKey(key string) (bool, tea.Cmd) {
	if !a.at(pageSpend) {
		return false, nil
	}
	// AND A KEY IS BOUND ONLY WHERE THE HALF OF THE CONTROL NAMING IT IS DRAWN.
	// One predicate answers the paint and the keys on every windowed place
	// (placeprose.go's [placeWindowFits]): a frame too narrow for the arrows has
	// no window at all, and one with room for the arrows but not for
	// `shift+↑ coarser` beside them has no zoom.
	width, _ := a.size()
	arrows, grain := placeWindowFits(width, a.spend.reading.headWords(width), a.spend.win)
	if !arrows {
		return false, nil
	}
	if (key == "shift+up" || key == "shift+down") && !grain {
		return false, nil
	}
	next := a.spend.reading.step(a.spend.win, key)
	if next == a.spend.win {
		return false, nil
	}
	a.spend.win = next
	// THE LEDGER'S LINES ARE ALREADY IN MEMORY, so moving the window is arithmetic
	// over them and never a re-read of the ledger. A fortnight back is the same
	// cache answered a different question, which is what lets a person hold the
	// arrow down.
	//
	// THE SEAT ROLLUP IS THE ONE FIGURE THAT CANNOT BE RE-CUT FROM WHAT A READ
	// LEFT BEHIND, because it arrives already summed over the window it was asked
	// for: the lines behind it stay in the store. Keeping the old sum under the
	// new window would draw a fortnight's seat dollars beside a month's every
	// other figure, so the rollup is asked again over the window the page now
	// draws. It costs a conversation with no plan nothing at all — that is the
	// seam's own nil — and one read-only pass over a small ledger for one that
	// has a plan, which is the same pass the beat was already making.
	a.rebuildSpend()
	return true, a.askSpendSeats(next.From)
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

func (placeSpend) tick(a *app, now time.Time) (bool, tea.Cmd) {
	a.refreshSpend()
	return true, a.askSpendSeats(a.spend.win.From)
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
	if a.spend.reading.empty() {
		if !a.spend.held {
			return placeWhisperRows(pageSpend, width, room, a.pal)
		}
		// THE HEADER STAYS, because it is the only thing on this frame naming the
		// window the four arrow keys move ([spendPage.held] holds the argument).
		//
		// AND SO DOES THE CUT'S CONTROL, for exactly that reason and no other.
		// It lived only on the heading over a table's rows, so a window paged
		// back onto a quiet fortnight drew no heading, no arrows and no foot —
		// and `by model` then had no way back to `by topic` except paging the
		// window forward again. A control a person can be stranded away from is
		// a control they cannot rely on; this frame has room for it, and the cut
		// is a fact about the page rather than about the rows.
		rows := make([]placeRow, 0, room)
		rows = append(rows, placeRow{text: a.spend.reading.windowHeaderRow(width, a.pal), hit: -1})
		cut := len(rows)
		on := cut == a.spend.cursor
		text := placeLead + a.spend.reading.sliceHeading(width-len(placeLead), on, a.pal)
		if on {
			text = placeBand(text, width, a.pal)
		}
		rows = append(rows, placeRow{text: text, hit: cut})
		for len(rows) < room {
			rows = append(rows, placeRow{text: "", hit: -1})
		}
		a.spend.top, a.spend.shown = 0, len(rows)
		return rows
	}
	lit := func(i int) bool { return i == a.spend.cursor && a.spendStopAt(i).ok }
	body, stops := a.spend.reading.paint(width, a.pal, lit)
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

func (placeSpend) window(a *app, key string) (bool, tea.Cmd) { return a.spendWindowKey(key) }

// verbs is what `→` opens over the row under the cursor, and on this place it is
// one letter: `b`, the limits.
//
// THIS IS WHERE THE DESIGN'S `b` IS REAL. A bare letter may be a verb only while
// the strip naming it is on the screen (verbstrip.go's first law) — every
// printable key belongs to the composer otherwise, which is the product and not
// a compromise — so `b` is drawn before it works, and it works on every row of
// this place because every row of this place is about money.
func (placeSpend) verbs(a *app) []verb {
	// AND IT STANDS DOWN ON THE CUT'S HEADING, whose `→` is the step between
	// cuts ([app.stepSpendSlice]). Two claims on one key are settled by the row:
	// this one has arrows drawn on it and the strip is not what they mean.
	if stop := a.spendStopAt(a.spend.cursor); !stop.ok || stop.fold || stop.slice {
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
	// spendGoneTaskWord and spendGoneTalkWord are what a row says when the thing
	// money was spent on is no longer in the record. THE LEDGER OUTLIVES WHAT IT
	// IS ABOUT: a line stays on the bill for as long as the window covers it,
	// while the work it names can be forgotten, and the row is still a true
	// reading of what was spent. So the refusal names the fact rather than a
	// fault, in the words the search place already refuses in.
	spendGoneTaskWord = "that piece of work is not on this machine any more"
	spendGoneTalkWord = "that conversation is not on this machine any more"
	// spendSliceWord introduces the OTHER cut on the foot, while the cursor is
	// on the heading that swaps them.
	spendSliceWord = "←→ "
)

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
	stop := a.spendStopAt(a.spend.cursor)
	switch {
	case stop.fold:
		parts = append(parts, foldEnterWord(a.spend.unfolded))
	case stop.slice:
		// THE FOOT NAMES THE CUT THE ARROWS LEAD TO, not the one already on the
		// frame. A control with two positions has one useful thing to say about
		// itself, and it is where the key goes.
		parts = append(parts, spendSliceWord+a.spend.slice.step(1).word())
	case stop.rails || stop.subject.Kind != "":
		// AND `enter` IS NAMED ONLY ON A ROW IT OPENS SOMETHING FROM. The rows of
		// `by model` are stops so that a long table scrolls under the cursor
		// ([spendReading.paint]), and they open nothing — a model is not a thing
		// money was spent on — so a foot promising a door there would be this
		// surface advertising a key that does nothing.
		parts = append(parts, spendEnterWord)
	}
	// AND THE LIMITS ARE OFFERED ON EVERY ROW THAT HAS THEM, which is every row
	// of this place that is not the fold or the cut's own control: every row here
	// is about money ([placeSpend.verbs]).
	for _, v := range (placeSpend{}).verbs(a) {
		parts = append(parts, spendVerbLead+v.word)
	}
	width, _ := a.size()
	if arrows, _ := placeWindowFits(width, a.spend.reading.headWords(width), a.spend.win); arrows {
		parts = append(parts, spendWindowWord)
	}
	if len(parts) == 0 {
		// A PAGE WITH NOTHING ON IT STILL HAS A WAY OUT, and that is all it has.
		// [placeTailed] adds `tab next place`, so this is `esc close` rather than
		// a foot naming three keys over an empty ledger.
		return mapCloseWords
	}
	return strings.Join(parts, railSep) + railSep + mapCloseWords
}

func (placeSpend) press(a *app, y int) (tea.Cmd, bool) {
	if at, ok := placeBodyLine(y, a.spend.top, a.spend.shown); ok && a.spendStopAt(at).ok {
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
	return placeHoverMoved(&a.spend.hover, &a.spend.cursor, next, next, a)
}

func (placeSpend) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.moveSpend(delta)
	a.touch()
	return nil, true
}

// key is this place's own reading of a key the router did not take
// (pages.go's [place] states the split).
func (placeSpend) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.spendKey(msg) }
