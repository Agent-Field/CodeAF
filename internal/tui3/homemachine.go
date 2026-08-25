package tui3

// THE MACHINE'S OWN CARD: WHAT THE RIGHT COLUMN IS ABOUT WHEN THE CURSOR IS ON
// NOTHING.
//
// Every other card on this screen is about a row somebody is pointing at — a
// conversation, a standing item, a project. This one is about the machine, and
// it is what a person sees at the moment they are looking at NOTHING in
// particular: the morning glance (docs/HOME-BRIDGE.md). Its bands are the three
// questions that have no row to hang off — what is keeping an eye on things,
// what happened everywhere since you left, and what the day has come to — and
// each of them is registered from its own file like every other band
// (homebands.go).
//
// ── ONE READER, TWO SURFACES ──
//
// The pulse line at the top of the screen (pulse.go) says the same three facts
// in three words, and it MUST NOT read them again: a top line saying `4 orders`
// over a card listing three would be the screen arguing with itself. So the
// reading is taken once, here, on home's own three-second beat, and both
// surfaces draw from it ([app.machineFactsAt]).
//
// ── AND IT NEVER BLOCKS ──
//
// Everything below is either already in the view — the world, the standing
// bands, the news the phone tier's inbox reads — or a single small file, and it
// is taken at most once per [homeEvery] whoever asks first. A band is drawn on
// every frame; the disk is walked on home's clock and nowhere else (the fourth
// law in homebands.go's header).

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// machineFacts is the whole of what this machine has to say about itself, as
// the card's three bands and the pulse line all read it.
//
// EVERY FIELD IS EMPTY WHEN IT IS NOT TRUE, because the emptiness law is
// enforced by the READING and not by four separate drawing decisions: a machine
// with nothing standing has no watchlist, a day with no work has no counts, and
// each of those absences reaches the card and the pulse as the same absence.
type machineFacts struct {
	// watching is every standing order on the machine that is active and is NOT
	// waiting on a person, soonest first. One that needs somebody is an
	// attention row and belongs in `needs you` (docs/HOME-BRIDGE.md's left
	// column) — drawing it here as well would put the same thing in two zones
	// that answer two different questions.
	watching []StandingItemView
	// news is what happened across every project since you left, newest first.
	news []homePhoneNote
	// chats and tasks are the local day counted: conversations somebody spoke in
	// today, and work that ran today.
	chats int
	tasks int
	// spent is what that work cost, in dollars, and ceiling is the machine-wide
	// daily allowance it is spending against — zero for a machine that has none.
	spent   float64
	ceiling float64
	// firing is whether a pass has one of those orders in its hands at this
	// instant. It is the ambient side's own `●`, counted once here so the pulse
	// and the card cannot disagree about whether the machine is working.
	firing bool
	// hands is HOW MANY THINGS THIS MACHINE HAS IN FLIGHT RIGHT NOW, across
	// every project and every kind of thing: a task node out, a conversation
	// mid-turn, an errand answering, a standing order firing.
	//
	// IT IS THE MOVING ZONE'S OWN ARITHMETIC AND NOT A SECOND ACCOUNTING
	// ([app.machineHands] counts the rows [homeView.attentionMoving] gathers).
	// Home already knows what is running everywhere — that is the whole subject
	// of the left column's second strip — and a count derived a second way here
	// would be a top line saying `3 working` over a strip listing four, which is
	// the exact failure the one-reader law was written against.
	hands int
}

// machineCeilingNear is how much of the day's allowance has to be gone before
// the figure stops being an ordinary dim fact.
//
// FOUR FIFTHS IS WHERE A BOUND STARTS TO MATTER. [hueWarn] exists for exactly
// this shape — a bound that is about to be reached, which is not a failure and
// must not wear the failure hue — and the honest moment to raise the figure out
// of the dim is when there is still enough left to do something about it.
const machineCeilingNear = 0.8

// nearCeiling reports that the day's spend is close enough to the machine's
// allowance to be worth a colour. It is ONE PREDICATE for both surfaces: the
// pulse raises its spend segment on it and the `today` band raises the same
// figure with its allowance beside it, and two thresholds would be two answers
// to when a person should start paying attention.
func (f machineFacts) nearCeiling() bool {
	return f.ceiling > 0 && f.spent >= f.ceiling*machineCeilingNear
}

// machineFactsAt is that reading, taken once per [homeEvery] and answered from
// the view in between.
//
// IT IS KEPT ON THE VIEW AND DIES WITH THE SCREEN ([homeView] is zeroed when
// home closes), which is the same bargain every other cache on this surface
// makes: the numbers are worth a walk of the disk twice a minute and never
// worth one per paint.
func (a *app) machineFactsAt(now time.Time) machineFacts {
	h := &a.home
	if !h.machineAt.IsZero() && !now.IsZero() && now.Sub(h.machineAt) < homeEvery {
		return h.machine
	}
	facts := machineFacts{
		// THE NEWS IS THE PHONE TIER'S OWN READING, asked again rather than
		// derived again. [homeView.phoneNotes] already walks every project's
		// inbox and every conversation's landings and puts them in one order,
		// with its own cache on home's clock; a second derivation here would be
		// a second answer to "what is news", and the first day they disagreed
		// the phone and the desk would be showing two different machines.
		news:    h.phoneNotes(),
		ceiling: a.machineAllowance(),
	}
	facts.watching = a.machineWatching()
	for _, view := range facts.watching {
		if view.Running {
			facts.firing = true
			break
		}
	}
	facts.chats, facts.tasks, facts.spent = a.machineDay(now)
	facts.hands = a.machineHands()
	h.machine, h.machineAt = facts, now
	// AND THIS IS THE BEAT THE SPARK IS SAMPLED ON. A fresh reading happens once
	// per [homeEvery] whoever asks for it first, which is exactly the clock a
	// chart of "what has this machine been doing lately" wants — so the sample
	// is taken HERE, where the reading is taken, rather than on a clock of its
	// own that could disagree with the figure beside it (spark.go's
	// [app.sampleHands]).
	a.sampleHands(facts.hands)
	return facts
}

// handsRingSize is how many readings the `hands` spark keeps. Sixty samples on
// home's three-second beat is THREE MINUTES OF MACHINE, which is about as far
// back as "lately" reaches for somebody who has just sat down — and it is two
// samples per braille cell, so a window this long is thirty cells of chart and
// fits a card at every width one is drawn at.
const handsRingSize = 60

// sampleHands appends one reading to that ring, keeping the last
// [handsRingSize].
//
// A READING IDENTICAL TO THE ONE BEFORE IT IS STILL KEPT, which is
// [app.sampleContext]'s own rule and matters more here: a flat run is what a
// quiet machine LOOKS like, and a ring that collapsed repeats would draw a busy
// shape over a still afternoon.
func (a *app) sampleHands(hands int) {
	a.handsRing = append(a.handsRing, hands)
	if len(a.handsRing) > handsRingSize {
		a.handsRing = a.handsRing[len(a.handsRing)-handsRingSize:]
	}
}

// machineHands is how many things this machine has in flight, counted off the
// rows the moving zone gathers ([homeView.attentionMoving]).
//
// ONE ROW IS NOT ALWAYS ONE HAND. A conversation with three task nodes out is
// one row and three things being done, so it counts three. A row with no count —
// a conversation merely mid-turn, an errand answering, an order firing — is one
// hand: something IS being done there, and the machine has no finer number for
// it than "this".
//
// IT IS COUNTED OFF THE READING AND NOT OFF THE COLUMN. The list caps what it
// draws ([switcherShown]) and the pulse's figure is about the MACHINE, so a
// count taken from the rows on screen would fall the moment a ninth thing
// started — which is the opposite of what the figure means.
func (a *app) machineHands() int {
	hands := 0
	for _, row := range a.home.world.Sessions() {
		if row.Archived || row.NeedsPerson() {
			continue
		}
		if row.Tasks.Running > 1 {
			hands += row.Tasks.Running
			continue
		}
		if row.Tasks.Running == 1 || row.Live && row.Presence.State == session.PresenceWorking {
			hands++
		}
	}
	for _, views := range a.home.items {
		for _, view := range views {
			if view.Running && strings.TrimSpace(view.Item.NeedsPerson) == "" {
				hands++
			}
		}
	}
	for _, ex := range a.home.exchanges {
		if ex.working && !ex.waiting() {
			hands++
		}
	}
	return hands
}

// machineWatching is every active order on the machine, soonest first.
//
// IT READS WHAT HOME ALREADY READ. [app.readStandBands] walks the store once
// per reading of the world and leaves every project's items on the view — this
// is that map flattened, filtered and put in the order the card reads it, with
// no second walk of anything.
func (a *app) machineWatching() []StandingItemView {
	var out []StandingItemView
	seen := make(map[string]bool)
	for _, views := range a.home.items {
		for _, view := range views {
			item := view.Item
			if item.Status != standing.StatusActive || strings.TrimSpace(item.NeedsPerson) != "" {
				continue
			}
			// A workspace home knows twice — once as a project with
			// conversations and once as a bare one — must not put the same order
			// on the card twice. [app.readBareBands] keys a bare workspace by
			// its own path rather than by a bucket, so the two readings land
			// under two keys and only the id can tell them apart.
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			out = append(out, view)
		}
	}
	standByNextDue(out)
	return out
}

// ── the paint ───────────────────────────────────────────────────────────────

// THE GLYPH CARRIES THE HUE AND THE WORDS STAY CALM. It is this codebase's own
// grammar for a row that has a state: one tinted cell at the head, ordinary text
// beside it ([app.homeTaskGlyph] paints a task's row exactly this way, and
// [standRowInk] is the same decision about an item's tail). A whole row in a
// status colour is a row that shouts, and a column of them is a screen nobody
// can read down.

// machineGlyphInk is how one standing order's mark is painted on this card.
//
// THE VIOLET IS NOT HERE, AND THAT IS THE RESERVATION HOLDING. [hueAsk] means
// one thing on this surface — a person is genuinely being waited on — and an
// order that needs somebody is not drawn on this band at all; it is an attention
// row ([app.machineWatching]). So the three hues this can answer are the three
// states it can actually draw.
func machineGlyphInk(pal palette, view StandingItemView) func(string) string {
	switch {
	case view.Running:
		// The working hue: something is in flight on the machine's behalf. It is
		// the same claim the status line's spinner makes ([app.keepingWord]).
		return pal.muted
	case view.News:
		// Something landed since you last looked, which is the one piece of good
		// news a `◦` row can carry.
		return pal.add
	}
	return pal.dim
}

// machineLeadInk paints a row whose first cell is a glyph: the glyph in the hue
// its state earns, the words after it in the calm ink every other row of this
// column uses.
//
// It works on the FITTED string because [bandSidesWithSeparator] clips from the
// right and never from the left — the glyph is still the first thing on the row
// at every width, and a row too narrow even for it is painted whole in the calm
// ink rather than half in a hue.
func machineLeadInk(pal palette, glyph string, tint func(string) string) func(string) string {
	return func(row string) string {
		if glyph == "" || !strings.HasPrefix(row, glyph) {
			return pal.muted(row)
		}
		return tint(glyph) + pal.muted(strings.TrimPrefix(row, glyph))
	}
}

// machineDay is the local day counted: conversations somebody spoke in, work
// that ran, and what it all cost.
//
// THE DAY IS THE PERSON'S OWN DAY and not twenty-four hours: "today" on this
// card means since midnight where they are sitting, which is what the standing
// ledger already means by it (internal/standing's ledger.go).
//
// WHAT THE MONEY IS MADE OF IS WHAT CAN BE HONESTLY DATED. A task carries the
// instant it landed and what it cost ([session.TaskIndexEntry]), and the
// standing ledger is a file per day; a conversation's own spend is a lifetime
// total on its meta.json with no day in it, so it is NOT split across days
// here. A figure invented by pretending a week of talking happened this morning
// would be worse than the one this leaves out.
func (a *app) machineDay(now time.Time) (chats, tasks int, spent float64) {
	day := machineDayStart(now)
	if day.IsZero() {
		return 0, 0, 0
	}
	for _, project := range a.home.everyProject() {
		for _, row := range project.Sessions {
			if !row.At.Before(day) {
				chats++
			}
			for _, entry := range row.Tasks.Rows {
				switch {
				case !entry.EndedAt.IsZero() && !entry.EndedAt.Before(day):
					tasks++
					spent += entry.Cost
				case row.Runs(entry):
					// WORK IN FLIGHT IS WORK THE DAY RAN. It has no landing
					// stamp yet and no bill either, so it is counted and not
					// priced — which is the emptiness law reaching one clause of
					// one figure rather than the whole line.
					tasks++
				}
			}
		}
	}
	return chats, tasks, spent + a.machineStandingSpend(day)
}

// machineStandingSpend is what everything standing spent today, out of the
// ledger the ticker writes. A surface with no way to ask answers nothing, which
// is the same absence [app.standWeek] answers with.
func (a *app) machineStandingSpend(day time.Time) float64 {
	if a.stands.Runs == nil {
		return 0
	}
	total := 0.0
	for _, spend := range a.stands.Runs(day) {
		total += spend.USD
	}
	return total
}

// machineDayStart is midnight, locally.
func machineDayStart(now time.Time) time.Time {
	if now.IsZero() {
		return time.Time{}
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// machineAllowance is the machine-wide daily allowance everything on it spends
// against — the person's own daily budget row, which is the rail a firing is
// held to as well (cmd/aforge's v3StandingDailyRail).
//
// IT IS ONE SETTING READ IN ONE PLACE. The ceiling is drawn on the `today` band
// and nowhere else on this screen — never on the pulse, which says what has been
// spent and never a fraction (docs/HOME-BRIDGE.md).
func (a *app) machineAllowance() float64 {
	rail, err := config.DailyBudgetUSDAt(a.profileDir)
	if err != nil || rail <= 0 {
		return 0
	}
	return rail
}

// ── the card ────────────────────────────────────────────────────────────────

// machineSubject is the machine as the band registry sees it.
func (a *app) machineSubject() bandSubject {
	return bandSubject{kind: bandKindMachine, world: a.home.world}
}

// machineCard is the right column at rest: the registered machine bands and
// nothing else.
//
// IT HAS NO TITLE LINE, and that is the emptiness law rather than an omission.
// Every other card leads with what the row is called because the pane has to say
// which of a hundred rows it is about; this one is about the machine a person is
// sitting at, whose name is already the first word of the line above
// (pulse.go). A heading here would be a row spent saying "this screen".
func (a *app) machineCard(width, room int, pal palette) []string {
	a.home.machineDoors = a.home.machineDoors[:0]
	return homeBands(a.drawHomeBands(bandContext{
		subject: a.machineSubject(), width: width, now: a.home.world.Read, pal: pal,
	}), room)
}

// ── every news row is a door ────────────────────────────────────────────────

// machineDoor is one news row drawn this frame and the thing it names.
//
// It is recorded at draw time for [app.bandFoldAt]'s reason and read back the
// same way: the card is assembled band by band and drops whole bands on a short
// frame, so a row number computed against it would be a second answer to where
// things ended up. The record is thrown away and rewritten on every paint of
// the card ([app.machineCard]).
type machineDoor struct {
	text string
	note homePhoneNote
}

// noteMachineDoor records one.
func (a *app) noteMachineDoor(text string, note homePhoneNote) {
	a.home.machineDoors = append(a.home.machineDoors, machineDoor{
		text: strings.TrimSpace(text), note: note,
	})
}

// machinePress is a press on the machine's card, and the only thing on it a
// pointer can act on beyond a fold line is a news row.
//
// EVERY NEWS ROW IS A DOOR (docs/HOME-BRIDGE.md), and the door is the thing the
// row NAMES: the conversation the news landed in, opened through the same road
// a row on the left column opens through ([app.homeOpenLine] — one answer to
// whether another project may be opened, not two). News left in a project's own
// inbox has no conversation behind it (homeband_news.go says why that inbox
// exists), so its door is the project, and it is opened by putting the cursor on
// it in the list — which is where every other project road on this screen ends.
func (a *app) machinePress(rowText string) (tea.Cmd, bool) {
	if !a.home.resting() {
		return nil, false
	}
	drawn := strings.TrimSpace(rowText)
	if drawn == "" {
		return nil, false
	}
	for _, door := range a.home.machineDoors {
		if door.text == "" || !strings.Contains(drawn, door.text) {
			continue
		}
		if door.note.hasRow {
			return a.homeOpenLine(homeLine{
				kind: homeSession, project: door.note.project,
				dir: door.note.dir, row: door.note.row,
			}), true
		}
		a.home.pointPlace(door.note.dir)
		return nil, true
	}
	return nil, false
}

// pointPlace puts the cursor on the first row of a project, whether that
// project is drawn open or folded to one line, and leaves it where it is when
// nothing on the column belongs to that place.
func (h *homeView) pointPlace(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	for at, line := range h.lines {
		if line.dir == dir && line.stop() {
			h.cursor, h.picked = at, true
			return
		}
	}
}

// machineNewsPlace is where one piece of news happened, as its row says it: the
// project's name, and its directory for a place nothing ever named.
func machineNewsPlace(note homePhoneNote) string {
	if name := strings.TrimSpace(note.project); name != "" {
		return name
	}
	return strings.TrimSpace(note.dir)
}
