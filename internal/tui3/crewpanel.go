package tui3

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/router"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE CREW PANEL: /crew with nothing after it.
//
//	╭─ crew ─────────────────────────────────────── esc ─╮
//	│   worker    auto · usually glm-5.3-flash           │
//	│   planner   auto · usually glm-5.3-flash           │
//	│ › checker   (pin) kimi-k3                           │
//	│                                                    │
//	│   models    ‹ all › (96)                           │
//	│   cap       none                                   │
//	│                                                    │
//	│   today $1.84 · 14 tasks                           │
//	╰─ enter change · esc close · ? keys ────────────────╯
//
// It is the one place a person changes WHAT IS ALLOWED — which seat is pinned
// to which model, which models a seat may be picked from, and what the crews
// may spend in a day — and those three are the whole of what persists about a
// crew (crew.go's header, internal/config's crew.go). How hard to try ONE task
// is said in the ask and never here.
//
// FIVE ROWS, ONE VERB. Three seats, the allowed models and the cap are the only
// rows the cursor stops on, and enter is what every one of them answers:
//
//   - A SEAT opens the one list this panel has — the models a seat could sit,
//     `auto` first, the router's own suggestion marked — and enter on a row of
//     it pins that seat. Unpinning is choosing `auto`, which is the first row
//     the list opens on, so it is two presses and there is no second verb for it.
//   - THE MODELS ROW IS EDITED WHERE IT STANDS. ←/→ walk its four answers — all,
//     open, a price ceiling, custom — and each step is written as it is taken; a
//     price turns the row into its two ceilings, typed into holes on the row
//     itself; custom opens the checklist, the one other screen here.
//   - THE CAP IS TYPED WHERE IT STANDS. A digit on the row starts it, enter
//     keeps it, an emptied hole is no cap. There is no dialog to open first.
//
// EVERY CHANGE IS WRITTEN THE MOMENT IT IS MADE, through the same writers the
// `/crew` shortcuts use (internal/config's SetCrewPin, SetCrewAllowedRule,
// SetCrewCap), so a refusal is the same refusal whichever door asked. What
// makes that safe is the undo beside it: the row that changed wears a tick,
// and for a few seconds the bottom edge offers `z undo`, which puts the rows
// back exactly as they were ([config.RestoreCrewState]).
//
// PROVIDERS ARE NOT A ROW. What a crew can route through is the connections a
// person made, and a list of them here would be a list nobody can change from
// here; they surface only where they are the reason something cannot work — a
// pin whose provider is not connected, or no provider at all.
//
// ── WHAT IT IS BUILT FROM ──
//
// Nothing here is a widget of its own. The block is THE ONE FRAME (frame.go),
// the frame every object that hangs over the box is drawn in; its rows are the
// overlay row's lead and ground (palette.go's [overlayLead], [palette.cursor]);
// its two lists walk with [moveCursor] and [listTop] and filter with the
// matcher every picker shares ([fuzzyTerms], internal/fuzzy); its filter box is
// the one-line box in the input line's place, exactly where /model and /connect
// put theirs (input.go); and its holes are the question form's own
// `[ value ]` (questioninput.go's [app.questionHole]).
//
// ── ONE PANEL, EVERY DOOR ──
//
// /crew opens it over the conversation. A place cannot draw an overlay
// (pages.go's [app.closeModals]), so /crew typed on home, and the settings
// sheet's `seats` row, step off the place, open this, and esc steps back onto
// the place they came from — one panel, never an embedded copy that could say
// something different.

// crewView is which of the panel's screens is up.
type crewView uint8

const (
	// crewMain is the five rows.
	crewMain crewView = iota
	// crewPicking is the list a seat is pinned from.
	crewPicking
	// crewChecking is the custom checklist of allowed models and providers.
	crewChecking
	// crewKeys is `?`: every key and gesture the panel takes.
	crewKeys
)

// The main panel's five stops, in the order the cursor walks them. The three
// seats come first and in [crewroute.Seats] order, so a stop below crewModels
// is a seat by its index.
const (
	crewModels = 3
	crewCap    = 4
	crewStops  = 5
)

// crewListRows is how many rows either list shows at most — the same ceiling
// the model picker keeps ([pickerRows]).
const crewListRows = pickerRows

// crewUndoFor is how long the bottom edge offers `z undo` after a change, and
// how long the changed row wears its tick.
const crewUndoFor = 5 * time.Second

// crewNarrow is the width under which the seats drop their "usually" hint.
// The hint is the one fact on a seat row that is a reading rather than a
// setting, so it is the first to go, and the row never clips instead.
const crewNarrow = 70

// Default price ceilings, dollars per million tokens in and out, for the first
// time the models row is walked onto a price. They are the grammar's own
// example (crewroute's allowed.go), which admits the cheap open models the
// router's evidence priced and leaves the frontier out.
const (
	crewPriceIn  = 1.0
	crewPriceOut = 5.0
)

// The words the panel says. Each is quoted in the manual as it is spelled
// here, and the e2e suite waits for the title.
const (
	crewTitleWord      = "crew"
	crewMainKeys       = "enter change · esc close · ? keys"
	crewPickKeys       = "type to filter · enter pick · → routes · esc back"
	crewCheckKeys      = "type to filter · space or enter tick · esc back"
	crewKeysKeys       = "esc back"
	crewUndoWord       = "z undo"
	crewAutoWord       = "auto — codeaf picks per task"
	crewAnyRouteWord   = "any route · cheapest"
	crewNoProviderWord = "no providers connected — /connect adds one"
	crewNothingMatches = "nothing matches"
	crewAllowFixWord   = "enter to allow it"
	crewPickHint       = "type to filter"
)

// crewModelBases are the four answers the models row walks, in order.
var crewModelBases = []string{"all", "open", "price", "custom"}

// crewPanel is the whole of /crew. The zero value is closed.
type crewPanel struct {
	open bool

	// back is the place /crew was asked for on, which esc stands back up; and
	// backTab/backCursor are where the settings sheet was when its `seats` row
	// opened this, so esc lands on that row rather than on the sheet's first tab.
	back       page
	backTab    int
	backCursor int

	// THE READING, taken when the panel opens and again after every write this
	// panel or a shortcut makes ([crewPanel.read]). A draw never reads the disk.
	pins      map[crewroute.Seat]config.CrewPin
	usual     map[crewroute.Seat]string
	usualSeen map[crewroute.Seat]bool
	suggest   map[crewroute.Seat]string
	rule      crewroute.Allowed
	capUSD    float64
	providers []config.CrewProvider
	offers    []config.CrewOffer
	gaps      []crewroute.Gap
	log       router.CrewLog

	view   crewView
	cursor int

	// step is the models row's position when the person has walked it onto
	// `custom` and not yet ticked anything: nothing is written by landing
	// there, so the row shows the step until they open the checklist or leave.
	// -1 is "show the rule in force".
	step int
	// priceIn and priceOut are the ceilings the row offers when it is walked
	// onto a price, remembered from the rule in force or the last one typed.
	priceIn, priceOut float64
	// edit is the hole being typed into on the main rows — the cap, or one of
	// the two price ceilings — and nil when none is.
	edit *crewHole

	pick  *crewPick
	check *crewCheck

	// saved is the stop that took the last change, and savedAt when; undo is
	// the rows as they stood before it. The tick and the `z undo` offer last
	// [crewUndoFor] from savedAt.
	saved   int
	savedAt time.Time
	undo    *config.CrewState
	// refusal is the last write a writer refused, in its own words, shown under
	// the rows until the next key.
	refusal string

	// owner maps each drawn line to the stop, list row or checklist line it
	// belongs to, -1 for a line that answers to nothing; arrows are the cells of
	// the models row's ‹ and ›, so a press on either walks it.
	owner  []int
	arrows [2]hudSpan
	// arrowLine is the drawn line the arrows are on, -1 when not drawn.
	arrowLine int
}

// crewHole is one hole typed into on the main rows.
type crewHole struct {
	// stop is the row the hole is on; price is which ceiling — 0 in, 1 out —
	// when the stop is the models row.
	stop  int
	price int
	box   editor
}

// crewPick is the seat list.
type crewPick struct {
	seat   crewroute.Seat
	filter editor
	// rows are what the list can show: `auto`, then every offer, the router's
	// suggestion first. They are built once, when the list opens.
	rows []crewPickRow
	// hits are indexes into rows in rank order, and lines what is DRAWN — a
	// hit, or one route of the hit whose routes are unfolded under it.
	hits   []int
	lines  []crewPickLine
	cursor int
	top    int
	score  []int
	// unfold is the model whose routes are showing, "" when none is.
	unfold string
	// refuse is the line whose pin the allowed models refused, and why: the
	// next enter on it widens the rule and pins.
	refuse    int
	refuseWhy string
}

// crewPickRow is one row the seat list can show.
type crewPickRow struct {
	auto      bool
	offer     config.CrewOffer
	suggested bool
	fields    []string
}

// crewPickLine is one drawn line of the seat list: the row, and which route
// of it — -1 for the model itself, 0 for "any route", n for its nth route.
type crewPickLine struct {
	row   int
	route int
}

// crewCheck is the custom checklist.
type crewCheck struct {
	filter editor
	// lines are the providers, then the models; hits index them.
	lines  []crewCheckLine
	hits   []int
	cursor int
	top    int
}

// crewCheckLine is one checklist line: a provider, or an offer.
type crewCheckLine struct {
	provider *config.CrewProvider
	offer    *config.CrewOffer
}

func (p *crewPanel) close() { *p = crewPanel{} }

// ── the reading ─────────────────────────────────────────────────────────────

// read takes the profile's crew again: the pins, the rule and the cap, the
// providers and what they reach, the day's log, and what the router would
// pick for a seat nobody pinned. It is the ONE place the panel touches the
// disk, called on open and after every write.
func (p *crewPanel) read(dir string) {
	p.pins = config.CrewPinsAt(dir)
	p.rule = config.CrewAllowedAt(dir)
	p.capUSD = config.CrewCapAt(dir)
	p.providers = config.CrewProvidersAt(dir)
	p.offers = config.CrewOffersAt(dir)
	p.gaps = config.CrewGapsAt(dir)
	p.log = config.CrewLogAt(dir)
	p.suggest = map[crewroute.Seat]string{}
	if d, err := crewroute.Decide(crewroute.Request{Class: crewroute.Other, Candidates: config.CrewCandidatesAt(dir)}); err == nil {
		for _, seat := range crewroute.Seats {
			p.suggest[seat] = d.Seat(seat).Model
		}
	}
	p.usual, p.usualSeen = crewUsual(p.log, p.suggest)
	if p.rule.Base == crewroute.BasePrice {
		p.priceIn, p.priceOut = p.rule.MaxIn, p.rule.MaxOut
	}
	if p.priceIn == 0 && p.priceOut == 0 {
		p.priceIn, p.priceOut = crewPriceIn, crewPriceOut
	}
}

// crewUsual is what each seat USUALLY runs when nobody pinned it: the model
// the recent tasks seated there most often, newest winning a tie — and, on a
// profile with no history yet, what the router would seat now. The second
// answer says which of the two it is, because "usually" is a claim about the
// past that a fresh install cannot make.
func crewUsual(log router.CrewLog, suggest map[crewroute.Seat]string) (map[crewroute.Seat]string, map[crewroute.Seat]bool) {
	usual, seen := map[crewroute.Seat]string{}, map[crewroute.Seat]bool{}
	for _, seat := range crewroute.Seats {
		counts, best, top := map[string]int{}, "", 0
		for _, task := range log.Recent {
			if slicesHas(task.Record.Pinned, string(seat)) {
				continue
			}
			model := crewroute.ShortModel(task.Record.Seats[string(seat)])
			if model == "" {
				continue
			}
			counts[model]++
			if counts[model] > top {
				best, top = model, counts[model]
			}
		}
		if best != "" {
			usual[seat], seen[seat] = best, true
			continue
		}
		if model := crewroute.ShortModel(suggest[seat]); model != "" {
			usual[seat] = model
		}
	}
	return usual, seen
}

// slicesHas says whether a list of words holds one.
func slicesHas(words []string, word string) bool {
	for _, w := range words {
		if w == word {
			return true
		}
	}
	return false
}

// modelStep is the models row's position: the step the person walked it to,
// or the rule in force.
func (p *crewPanel) modelStep() int {
	if p.step >= 0 {
		return p.step
	}
	switch {
	case p.rule.Custom():
		return 3
	case p.rule.Base == crewroute.BasePrice:
		return 2
	case p.rule.Base == crewroute.BaseOpen:
		return 1
	}
	return 0
}

// countFor is how many reachable models one step of the models row admits —
// the dim figure beside it, so walking the row is a way of reading what each
// answer would leave.
func (p *crewPanel) countFor(step int) int {
	var rule crewroute.Allowed
	switch step {
	case 0:
		rule = crewroute.Allowed{Base: crewroute.BaseAll}
	case 1:
		rule = crewroute.Allowed{Base: crewroute.BaseOpen}
	case 2:
		rule = crewroute.Allowed{Base: crewroute.BasePrice, MaxIn: p.priceIn, MaxOut: p.priceOut}
	default:
		n := 0
		for _, offer := range p.offers {
			if offer.Allowed {
				n++
			}
		}
		return n
	}
	n := 0
	for _, offer := range p.offers {
		if rule.AdmitsModel(offer.Model) {
			n++
		}
	}
	return n
}

// live is whether the last change still wears its tick and offers its undo.
func (p *crewPanel) live(now time.Time) bool {
	return p.saved >= 0 && !p.savedAt.IsZero() && now.Sub(p.savedAt) < crewUndoFor
}

// ── the app's side: opening, writing, undoing ───────────────────────────────

// openCrew is the panel's one door. A place cannot draw an overlay, so asked
// from one it steps off onto the conversation first and remembers where it
// was, which is where esc goes back to.
func (a *app) openCrew() {
	if a.hosted() {
		a.note(a.remoteProfileWord("the crew"))
		return
	}
	back, tab, cursor := a.page, 0, 0
	if a.at(pageSettings) {
		tab, cursor = a.sheet.tab, a.sheet.cursor
	}
	if a.pageShowing() {
		a.showPage(pageNone)
	}
	a.closeLists()
	a.dismissWelcome()
	a.crewUI = crewPanel{open: true, back: back, backTab: tab, backCursor: cursor, saved: -1, step: -1}
	a.crewUI.read(a.profileDir)
	a.touch()
}

// closeCrew is esc on the main panel: the panel goes, and a place it was
// opened from stands back up where it was left.
func (a *app) closeCrew() tea.Cmd {
	back, tab, cursor := a.crewUI.back, a.crewUI.backTab, a.crewUI.backCursor
	a.crewUI.close()
	a.touch()
	if placeRegistry[back] == nil {
		return nil
	}
	cmd := a.showPage(back)
	if a.at(pageSettings) {
		a.sheet.tab = tab
		a.sheet.build()
		a.sheet.cursor = a.sheet.clampCursor(cursor)
	}
	return cmd
}

// crewRefreshed is the tail every crew write shares, from this panel or from
// a shortcut: an open panel re-reads, an open settings sheet rebuilds.
func (a *app) crewRefreshed() {
	if a.crewUI.open {
		a.crewUI.read(a.profileDir)
	}
	a.refreshSettings()
}

// crewWrite is every change the panel makes: the rows as they stood are kept
// for the undo, the write goes through the writer it names, and a refusal is
// shown in the writer's own words and changes nothing. It answers the tick
// that takes the tick and the undo offer back down.
func (a *app) crewWrite(stop int, write func(dir string) error) tea.Cmd {
	p := &a.crewUI
	before := config.CrewStateAt(a.profileDir)
	if err := write(a.profileDir); err != nil {
		p.refusal = err.Error()
		a.touch()
		return nil
	}
	p.refusal = ""
	p.undo = &before
	p.saved, p.savedAt = stop, a.now()
	a.crewRefreshed()
	a.touch()
	return surfaceTick(crewUndoFor, func(time.Time) tea.Msg { return crewUndoMsg{} })
}

// crewUndoMsg is the undo window closing: one repaint, so the tick and the
// offer come down.
type crewUndoMsg struct{}

// crewUndo is `z`: the rows put back as they stood before the last change.
func (a *app) crewUndo() {
	p := &a.crewUI
	if p.undo == nil || !p.live(a.now()) {
		return
	}
	if err := config.RestoreCrewState(a.profileDir, *p.undo); err != nil {
		p.refusal = err.Error()
		return
	}
	p.undo, p.saved, p.step = nil, -1, -1
	a.crewRefreshed()
}

// ── keys ────────────────────────────────────────────────────────────────────

// crewKey routes one keypress while the panel owns the keyboard.
func (a *app) crewKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.crewUI
	defer a.touch()
	switch p.view {
	case crewPicking:
		return a.crewPickKey(msg)
	case crewChecking:
		return a.crewCheckKey(msg)
	case crewKeys:
		if k := msg.String(); k == "esc" || k == "?" || k == "q" {
			p.view = crewMain
		}
		return nil
	}
	if p.edit != nil {
		return a.crewHoleKey(msg)
	}
	key := msg.String()
	// A REFUSAL IS READ ONCE. It stays until the next key, which is the moment
	// the person has read it and moved on.
	p.refusal = ""
	switch key {
	case "esc":
		return a.closeCrew()
	case "?":
		p.view = crewKeys
	case "up", "k", "ctrl+p":
		a.crewMove(-1)
	case "down", "j", "ctrl+n":
		a.crewMove(1)
	case "home":
		a.crewMove(-crewStops)
	case "end":
		a.crewMove(crewStops)
	case "z":
		a.crewUndo()
	case "left", "h":
		if p.cursor == crewModels {
			return a.crewStepModels(-1)
		}
	case "right", "l":
		if p.cursor == crewModels {
			return a.crewStepModels(1)
		}
		if p.cursor < crewModels {
			a.crewOpenPick(crewroute.Seats[p.cursor])
		}
	case "enter", " ", "space":
		return a.crewEnter()
	case "backspace", "delete":
		if p.cursor == crewCap {
			p.edit = &crewHole{stop: crewCap}
			p.edit.box.setText(crewDollarsWord(p.capUSD))
			p.edit.box.deleteBackward()
		}
	default:
		if text := msg.Key().Text; p.cursor == crewCap && crewNumeric(text) {
			// A DIGIT ON THE CAP ROW IS THE CAP BEING TYPED. The hole opens on
			// the digit rather than on the old figure, because a person typing a
			// number is saying a new one.
			p.edit = &crewHole{stop: crewCap}
			p.edit.box.insert(text)
		}
	}
	return nil
}

// crewMove walks the five stops, clamping at both ends. Leaving the models
// row lets go of a step nothing was written for.
func (a *app) crewMove(delta int) {
	p := &a.crewUI
	p.cursor = moveCursor(p.cursor, delta, crewStops)
	if p.cursor != crewModels {
		p.step = -1
	}
}

// crewEnter is enter on a main row — the one verb.
func (a *app) crewEnter() tea.Cmd {
	p := &a.crewUI
	switch {
	case p.cursor < crewModels:
		a.crewOpenPick(crewroute.Seats[p.cursor])
	case p.cursor == crewModels:
		switch p.modelStep() {
		case 2:
			a.crewEditPrice(0)
		case 3:
			a.crewOpenCheck()
		default:
			return a.crewStepModels(1)
		}
	case p.cursor == crewCap:
		p.edit = &crewHole{stop: crewCap}
		if p.capUSD > 0 {
			p.edit.box.setText(crewDollarsWord(p.capUSD))
		}
	}
	return nil
}

// crewStepModels walks the models row one answer, and writes it: `all`,
// `open` and a price are rules and are written as they are stepped onto;
// `custom` is a list nobody has ticked yet, so landing on it writes nothing
// and enter opens the checklist.
func (a *app) crewStepModels(delta int) tea.Cmd {
	p := &a.crewUI
	next := p.modelStep() + delta
	if next < 0 || next >= len(crewModelBases) {
		return nil
	}
	if next == 3 {
		p.step = 3
		if p.rule.Custom() {
			p.step = -1
		}
		return nil
	}
	p.step = -1
	var rule crewroute.Allowed
	switch next {
	case 0:
		rule = p.rule.Rebased(crewroute.BaseAll, 0, 0)
	case 1:
		rule = p.rule.Rebased(crewroute.BaseOpen, 0, 0)
	case 2:
		rule = p.rule.Rebased(crewroute.BasePrice, p.priceIn, p.priceOut)
	}
	return a.crewWrite(crewModels, func(dir string) error { return config.SetCrewAllowedRule(dir, rule) })
}

// crewEditPrice opens one of the two ceiling holes on the models row.
func (a *app) crewEditPrice(which int) {
	p := &a.crewUI
	p.edit = &crewHole{stop: crewModels, price: which}
	value := p.priceIn
	if which == 1 {
		value = p.priceOut
	}
	p.edit.box.setText(strconv.FormatFloat(value, 'f', -1, 64))
}

// crewHoleKey is a key while a hole on the main rows is being typed into.
//
// ENTER KEEPS, ESC LEAVES IT AS IT WAS. On the price row enter on the first
// ceiling moves to the second — tab does too — and enter on the second writes
// both, because a price rule is one rule with two numbers in it.
func (a *app) crewHoleKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.crewUI
	hole := p.edit
	switch key := msg.String(); key {
	case "esc":
		p.edit = nil
		return nil
	case "tab":
		if hole.stop == crewModels {
			a.crewKeepPriceHole()
			a.crewEditPrice(1 - hole.price)
		}
		return nil
	case "enter":
		if hole.stop == crewCap {
			p.edit = nil
			raw := strings.TrimSpace(hole.box.String())
			if raw == "" {
				raw = "none"
			}
			return a.crewWrite(crewCap, func(dir string) error { return config.SetCrewCap(dir, raw) })
		}
		if !a.crewKeepPriceHole() {
			return nil
		}
		if hole.price == 0 {
			a.crewEditPrice(1)
			return nil
		}
		p.edit = nil
		rule := p.rule.Rebased(crewroute.BasePrice, p.priceIn, p.priceOut)
		return a.crewWrite(crewModels, func(dir string) error { return config.SetCrewAllowedRule(dir, rule) })
	case "backspace":
		hole.box.deleteBackward()
	case "ctrl+u":
		hole.box.killToStart()
	case "ctrl+k":
		hole.box.killToEnd()
	default:
		if text := msg.Key().Text; crewNumeric(text) {
			hole.box.insert(text)
		}
	}
	return nil
}

// crewKeepPriceHole reads the price hole being typed into back into the
// ceiling it is for, and refuses — out loud, under the rows — a figure that is
// not a number of dollars.
func (a *app) crewKeepPriceHole() bool {
	p := &a.crewUI
	raw := strings.TrimPrefix(strings.TrimSpace(p.edit.box.String()), "$")
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		p.refusal = "a ceiling is dollars per million tokens — a number, like 1 or 0.5"
		return false
	}
	if p.edit.price == 0 {
		p.priceIn = value
	} else {
		p.priceOut = value
	}
	return true
}

// crewNumeric is whether a typed key belongs in a dollar hole.
func crewNumeric(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if (r < '0' || r > '9') && r != '.' && r != '$' {
			return false
		}
	}
	return true
}

// crewDollarsWord is a cap as the hole holds it: the plain figure.
func crewDollarsWord(usd float64) string {
	if usd <= 0 {
		return ""
	}
	return strconv.FormatFloat(usd, 'f', -1, 64)
}

// ── the seat list ───────────────────────────────────────────────────────────

// crewOpenPick opens the list one seat is pinned from.
//
// IT OPENS ON `auto`, NOT ON THE PIN. The pin is marked where it stands, and the
// cursor on the first row is what makes unpinning two presses — the second
// verb this panel refuses to have.
func (a *app) crewOpenPick(seat crewroute.Seat) {
	p := &a.crewUI
	pick := &crewPick{seat: seat, refuse: -1}
	pick.rows = append(pick.rows, crewPickRow{auto: true, fields: []string{"auto", "codeaf picks per task"}})
	suggested := crewroute.Lineage(p.suggest[seat])
	var rest []crewPickRow
	for _, offer := range p.offers {
		row := crewPickRow{offer: offer, fields: []string{offer.Model.ID, crewroute.ShortModel(offer.Model.ID)}}
		for _, r := range offer.Routes {
			row.fields = append(row.fields, r.Provider)
		}
		if suggested != "" && crewroute.Lineage(offer.Model.ID) == suggested {
			row.suggested = true
			pick.rows = append(pick.rows, row)
			continue
		}
		rest = append(rest, row)
	}
	// ALLOWED FIRST, THEN BY NAME. A model the rule leaves out is still a row
	// — the list says why rather than hiding it — but it is not what a person
	// reading down for a model to pin is looking for first.
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].offer.Allowed != rest[j].offer.Allowed {
			return rest[i].offer.Allowed
		}
		return rest[i].offer.Model.ID < rest[j].offer.Model.ID
	})
	pick.rows = append(pick.rows, rest...)
	pick.score = make([]int, len(pick.rows))
	pick.rank()
	p.pick, p.view = pick, crewPicking
}

// rank narrows the list to the filter — every term must match, scored by the
// matcher every picker shares — and lays the drawn lines out again.
func (k *crewPick) rank() {
	query := strings.TrimSpace(k.filter.String())
	ft := fuzzyTerms(strings.Fields(strings.ToLower(query)))
	k.hits = k.hits[:0]
	for i, row := range k.rows {
		if len(ft) == 0 {
			k.hits = append(k.hits, i)
			continue
		}
		total, hit := fuzzy.ScoreFields(row.fields, ft)
		if !hit {
			continue
		}
		k.score[i] = total
		k.hits = append(k.hits, i)
	}
	if len(ft) > 0 {
		sort.SliceStable(k.hits, func(a, b int) bool { return k.score[k.hits[a]] > k.score[k.hits[b]] })
	}
	k.cursor, k.top, k.refuse = 0, 0, -1
	k.relist()
}

// relist lays out the drawn lines: every hit, and the routes of the unfolded
// one under it.
func (k *crewPick) relist() {
	k.lines = k.lines[:0]
	for _, at := range k.hits {
		k.lines = append(k.lines, crewPickLine{row: at, route: -1})
		row := k.rows[at]
		if row.auto || row.offer.Model.ID != k.unfold {
			continue
		}
		k.lines = append(k.lines, crewPickLine{row: at, route: 0})
		for n := range row.offer.Routes {
			k.lines = append(k.lines, crewPickLine{row: at, route: n + 1})
		}
	}
	if k.cursor >= len(k.lines) {
		k.cursor = max(0, len(k.lines)-1)
	}
}

func (k *crewPick) move(delta int) {
	k.cursor = moveCursor(k.cursor, delta, len(k.lines))
	k.refuse = -1
	k.top = listTop(k.cursor, k.top, len(k.lines), crewListRows)
}

// crewPickKey is a key on the seat list.
func (a *app) crewPickKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.crewUI
	k := p.pick
	switch msg.String() {
	case "esc":
		// BACK ONE LEVEL: a refusal first, then a folded route list, then the
		// list itself — the key never takes two steps at once.
		switch {
		case k.refuse >= 0:
			k.refuse = -1
		case k.unfold != "":
			a.crewFold(k)
		default:
			p.pick, p.view = nil, crewMain
		}
		return nil
	case "enter":
		return a.crewPickEnter()
	case "right":
		if at := k.cursor; at < len(k.lines) {
			line := k.lines[at]
			if row := k.rows[line.row]; !row.auto && line.route < 0 && len(row.offer.Routes) > 0 {
				k.unfold = row.offer.Model.ID
				k.relist()
				return nil
			}
		}
	case "left":
		if k.unfold != "" {
			a.crewFold(k)
			return nil
		}
	}
	listNavigate(msg, &k.filter, k.move, k.rank, crewListRows)
	return nil
}

// crewFold closes the route list and puts the cursor back on its model.
func (a *app) crewFold(k *crewPick) {
	model := k.unfold
	k.unfold = ""
	k.refuse = -1
	k.relist()
	for i, line := range k.lines {
		if row := k.rows[line.row]; !row.auto && row.offer.Model.ID == model {
			k.cursor = i
		}
	}
	k.top = listTop(k.cursor, k.top, len(k.lines), crewListRows)
}

// crewPickEnter is enter on the seat list: auto unpins, a model pins, a route
// pins that model to that provider — and a pin the allowed models refuse is
// answered on its own row, with the one key that lets it in.
func (a *app) crewPickEnter() tea.Cmd {
	p := &a.crewUI
	k := p.pick
	if k.cursor >= len(k.lines) {
		return nil
	}
	line := k.lines[k.cursor]
	row := k.rows[line.row]
	seat := k.seat
	stop := crewSeatStop(seat)
	done := func(cmd tea.Cmd) tea.Cmd {
		if p.refusal == "" {
			p.pick, p.view, p.cursor = nil, crewMain, stop
		}
		return cmd
	}
	if row.auto {
		return done(a.crewWrite(stop, func(dir string) error { return config.ClearCrewPin(dir, seat) }))
	}
	raw := row.offer.Model.ID
	provider := ""
	if line.route > 0 && line.route-1 < len(row.offer.Routes) {
		provider = row.offer.Routes[line.route-1].Provider
		raw += "@" + provider
	}
	if k.refuse == k.cursor {
		// THE SECOND ENTER IS THE FIX: the rule is widened by exactly this model
		// — and this provider, where it was the provider the rule left out — and
		// the pin is written in the same breath, so the undo takes both back.
		rule := p.rule
		if !rule.AdmitsModel(row.offer.Model) {
			rule, _ = rule.Toggled(row.offer.Model, true)
		}
		if provider != "" && !rule.AdmitsRoute(provider) {
			rule = rule.RouteToggled(provider, true)
		}
		return done(a.crewWrite(stop, func(dir string) error {
			if err := config.SetCrewAllowedRule(dir, rule); err != nil {
				return err
			}
			return config.SetCrewPin(dir, seat, raw)
		}))
	}
	pin, _, err := config.ParseCrewPin(raw)
	if err == nil {
		err = config.CrewPinAllowed(a.profileDir, pin)
	}
	if err != nil && crewWidenable(p.rule, row.offer, provider) {
		k.refuse, k.refuseWhy = k.cursor, crewRefusalWord(p.rule, row.offer, provider)
		return nil
	}
	return done(a.crewWrite(stop, func(dir string) error { return config.SetCrewPin(dir, seat, raw) }))
}

// crewWidenable is whether the allowed rule is what refused this pin — the one
// refusal the panel can fix on the spot. A provider that is not connected is
// not the rule's to fix, and its refusal is shown as the writer words it.
func crewWidenable(rule crewroute.Allowed, offer config.CrewOffer, provider string) bool {
	if !rule.AdmitsModel(offer.Model) {
		return true
	}
	return provider != "" && !rule.AdmitsRoute(provider)
}

// crewRefusalWord is the refusal under a row the allowed models leave out.
func crewRefusalWord(rule crewroute.Allowed, offer config.CrewOffer, provider string) string {
	name := crewroute.ShortModel(offer.Model.ID)
	if rule.AdmitsModel(offer.Model) && provider != "" {
		return provider + " is a provider your allowed models leave out (" + rule.String() + ") — " + crewAllowFixWord
	}
	return name + " is not in your allowed models (" + rule.String() + ") — " + crewAllowFixWord
}

// crewSeatStop is a seat's row on the main panel.
func crewSeatStop(seat crewroute.Seat) int {
	for i, s := range crewroute.Seats {
		if s == seat {
			return i
		}
	}
	return 0
}

// ── the custom checklist ────────────────────────────────────────────────────

// crewOpenCheck opens the checklist: every connected provider, then every
// model they reach, each ticked where the rule admits it.
func (a *app) crewOpenCheck() {
	p := &a.crewUI
	check := &crewCheck{}
	for i := range p.providers {
		check.lines = append(check.lines, crewCheckLine{provider: &p.providers[i]})
	}
	for i := range p.offers {
		check.lines = append(check.lines, crewCheckLine{offer: &p.offers[i]})
	}
	check.rank()
	p.check, p.view, p.step = check, crewChecking, -1
}

// rank narrows the checklist to the filter, in its own order: a checklist is
// read down, and one that reordered itself under a tick would move the row a
// person had just pressed.
func (c *crewCheck) rank() {
	ft := fuzzyTerms(strings.Fields(strings.ToLower(c.filter.String())))
	c.hits = c.hits[:0]
	for i, line := range c.lines {
		if len(ft) > 0 {
			var fields []string
			if line.provider != nil {
				fields = []string{line.provider.ID, line.provider.Name}
			} else {
				fields = []string{line.offer.Model.ID, crewroute.ShortModel(line.offer.Model.ID)}
			}
			if _, hit := fuzzy.ScoreFields(fields, ft); !hit {
				continue
			}
		}
		c.hits = append(c.hits, i)
	}
	c.cursor, c.top = 0, 0
}

func (c *crewCheck) move(delta int) {
	c.cursor = moveCursor(c.cursor, delta, len(c.hits))
	c.top = listTop(c.cursor, c.top, len(c.hits), crewListRows)
}

// ticked is whether one checklist line is admitted by the rule in force.
func (p *crewPanel) ticked(line crewCheckLine) bool {
	if line.provider != nil {
		return p.rule.AdmitsRoute(line.provider.ID)
	}
	return p.rule.AdmitsModel(line.offer.Model)
}

// crewCheckKey is a key on the checklist. Space and enter tick; every other
// printable key is the filter.
func (a *app) crewCheckKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.crewUI
	c := p.check
	p.refusal = ""
	switch msg.String() {
	case "esc":
		p.check, p.view = nil, crewMain
		return nil
	case "enter", " ", "space":
		return a.crewTick()
	}
	listNavigate(msg, &c.filter, c.move, c.rank, crewListRows)
	return nil
}

// crewTick flips the line under the cursor, in the shortest rule that says it
// ([crewroute.Allowed.Toggled]).
func (a *app) crewTick() tea.Cmd {
	p := &a.crewUI
	c := p.check
	if c.cursor >= len(c.hits) {
		return nil
	}
	line := c.lines[c.hits[c.cursor]]
	admit := !p.ticked(line)
	var rule crewroute.Allowed
	if line.provider != nil {
		rule = p.rule.RouteToggled(line.provider.ID, admit)
	} else {
		var ok bool
		if rule, ok = p.rule.Toggled(line.offer.Model, admit); !ok {
			p.refusal = "a list keeps one model at least — tick another before this one"
			return nil
		}
	}
	return a.crewWrite(crewModels, func(dir string) error { return config.SetCrewAllowedRule(dir, rule) })
}

// ── drawing ─────────────────────────────────────────────────────────────────

// crewHeight is how many lines the panel wants: its rows and the frame's two
// edges. The overlay's own clamp ([app.overlayHeight]) keeps the status line
// and the box on a short terminal, and the draw windows the rows into what
// is left.
func (a *app) crewHeight(width int) int {
	if !a.crewUI.open {
		return 0
	}
	rows, _ := a.crewRows(width)
	return len(rows) + 2
}

// crewDraw is the panel's block, exactly n lines.
func (a *app) crewDraw(width, n, hover int) []string {
	p := &a.crewUI
	if n <= 0 || !p.open {
		return nil
	}
	// The pointer's line counts from the block's first line, which is the top
	// edge; the rows count from the line under it.
	rowHover := -1
	if hover > 0 {
		rowHover = hover - 1
	}
	rows, owner := a.crewRowsHover(width, rowHover)
	room := n - 2
	if room < 1 {
		// A FRAME WITH NO ROOM FOR A ROW DRAWS THE ROWS BARE: two edges and
		// nothing between them say less than one row alone.
		room = n
		rows, owner = crewWindow(rows, owner, p.focusLine(owner), room)
		p.owner = owner
		p.arrowLine = crewArrowLine(owner, p)
		return crewPad(rows, n)
	}
	rows, owner = crewWindow(rows, owner, p.focusLine(owner), room)
	title, keys := a.crewEdges()
	aside := a.pal.dim("esc")
	// THE UNDO IS OFFERED WHERE `z` MEANS IT: on the five rows. In a list the
	// letter is a filter's, and in a hole it is nothing, so an offer drawn there
	// would be a key that does something else.
	keysAside := ""
	if p.view == crewMain && p.edit == nil && p.live(a.now()) && p.undo != nil {
		keysAside = a.pal.dim(crewUndoWord)
	}
	lines, _ := framed{title: title, aside: aside, keys: a.pal.dim(keys), keysAside: keysAside}.draw(a.pal, width, rows)
	p.owner = append(append([]int{-1}, owner...), -1)
	p.arrowLine = -1
	for i, at := range p.owner {
		if p.view == crewMain && at == crewModels {
			p.arrowLine = i
		}
	}
	return crewPad(lines, n)
}

// crewArrowLine is the drawn line of the models row, for a bare block.
func crewArrowLine(owner []int, p *crewPanel) int {
	if p.view != crewMain {
		return -1
	}
	for i, at := range owner {
		if at == crewModels {
			return i
		}
	}
	return -1
}

// crewPad makes a block exactly n lines, which is what the frame reserved.
func crewPad(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

// focusLine is the row the window must keep in view: the cursor's.
func (p *crewPanel) focusLine(owner []int) int {
	want := p.cursor
	switch p.view {
	case crewPicking:
		want = p.pick.cursor
	case crewChecking:
		want = p.check.cursor
	case crewKeys:
		return 0
	}
	for i, at := range owner {
		if at == want {
			return i
		}
	}
	return 0
}

// crewWindow cuts rows to room lines, keeping the focused one in view.
func crewWindow(rows []string, owner []int, focus, room int) ([]string, []int) {
	if len(rows) <= room {
		return rows, owner
	}
	top := listTop(focus, 0, len(rows), room)
	return rows[top : top+room], owner[top : top+room]
}

// crewEdges is what the frame's two edges say for the view that is up.
func (a *app) crewEdges() (string, string) {
	p := &a.crewUI
	title := a.pal.muted(crewTitleWord)
	switch p.view {
	case crewPicking:
		return a.pal.muted(crewTitleWord + " · " + string(p.pick.seat)), crewPickKeys
	case crewChecking:
		n := p.countFor(3)
		return a.pal.muted(crewTitleWord + " · allowed models · " + strconv.Itoa(n) + " of " + strconv.Itoa(len(p.offers))), crewCheckKeys
	case crewKeys:
		return a.pal.muted(crewTitleWord + " · keys"), crewKeysKeys
	}
	return title, crewMainKeys
}

// crewRows is the view's rows inside the frame, and the owner of each.
func (a *app) crewRows(width int) ([]string, []int) { return a.crewRowsHover(width, -1) }

func (a *app) crewRowsHover(width, hover int) ([]string, []int) {
	inner := frameInner(width)
	switch a.crewUI.view {
	case crewPicking:
		return a.crewPickRows(inner, hover)
	case crewChecking:
		return a.crewCheckRows(inner, hover)
	case crewKeys:
		return a.crewKeyRows(inner)
	}
	return a.crewMainRows(inner, hover)
}

// crewRowLine is one row: the overlay row's lead and ground over text the caller
// painted, set to the width and never past it. marked is THE ONE THIS SEAT IS
// ON — the pin, or auto — and it takes the ground ladder's selected step the
// way the model in use does on /model's list ([overlayRowCore] argues the
// ladder), which outranks the cursor's on the row they share.
func (a *app) crewRowLine(text string, selected, hovered, marked bool, width int) string {
	line := overlayLead(selected, hovered, a.pal) + text
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	switch {
	case marked:
		return a.pal.selected(line, width)
	case selected, hovered:
		return a.pal.cursor(line, width)
	}
	return line
}

// crewSaid is a sentence under the rows — a warning, a refusal — WRAPPED and
// never cut: the half of a refusal past the edge is usually the half that
// says what to do about it.
func crewSaid(text string, width int, paint func(string) string) []string {
	var out []string
	for i, line := range wrap(text, max(1, width-4)) {
		lead := "  "
		if i > 0 {
			lead = "    "
		}
		out = append(out, paint(lead+line))
	}
	return out
}

// crewLabel is a main row's name, set in its column.
func (a *app) crewLabel(word string, selected bool) string {
	cell := word + strings.Repeat(" ", max(1, 10-len(word)))
	if selected {
		return a.pal.bold(a.pal.ink(cell))
	}
	return a.pal.muted(cell)
}

// crewMainRows is the five rows, the day under them, and whatever cannot work.
func (a *app) crewMainRows(width, hover int) ([]string, []int) {
	p := &a.crewUI
	var rows []string
	var owner []int
	add := func(line string, at int) {
		rows = append(rows, line)
		owner = append(owner, at)
	}
	now := a.now()
	for i, seat := range crewroute.Seats {
		value := a.crewSeatValue(seat, width)
		if p.live(now) && p.saved == i {
			value += "  " + a.pal.add(a.icon(tokens.GSettled))
		}
		add(a.crewRowLine(a.crewLabel(string(seat), p.cursor == i)+value, p.cursor == i, hover == len(rows), false, width), i)
	}
	add("", -1)
	models := a.crewModelsValue()
	if p.live(now) && p.saved == crewModels {
		models += "  " + a.pal.add(a.icon(tokens.GSettled))
	}
	modelsLine := a.crewRowLine(a.crewLabel("models", p.cursor == crewModels)+models, p.cursor == crewModels, hover == len(rows), false, width)
	p.arrows = crewArrowSpans(modelsLine)
	add(modelsLine, crewModels)
	capValue := a.crewCapValue()
	if p.live(now) && p.saved == crewCap {
		capValue += "  " + a.pal.add(a.icon(tokens.GSettled))
	}
	add(a.crewRowLine(a.crewLabel("cap", p.cursor == crewCap)+capValue, p.cursor == crewCap, hover == len(rows), false, width), crewCap)
	if day := a.crewTodayWord(); day != "" {
		add("", -1)
		add(a.pal.dim(fit("  "+day, width)), -1)
	}
	for _, warning := range a.crewWarnings() {
		for _, line := range crewSaid(warning, width, a.pal.warn) {
			add(line, -1)
		}
	}
	if p.refusal != "" {
		for _, line := range crewSaid(a.icon(tokens.GFailed)+" "+p.refusal, width, a.pal.bad) {
			add(line, -1)
		}
	}
	return rows, owner
}

// crewSeatValue is a seat's value: `auto` with what it usually runs, or the
// pin glyph and the model with its route when one was pinned — and the word
// `unavailable` where the pin cannot run on anything connected.
func (a *app) crewSeatValue(seat crewroute.Seat, width int) string {
	p := &a.crewUI
	pin, pinned := p.pins[seat]
	if !pinned {
		usual := p.usual[seat]
		if len(p.providers) == 0 {
			// NOTHING CONNECTED IS SAID ONCE, under the rows, with the command
			// that fixes it — not three times, once per seat, as though each of
			// them had its own problem.
			return a.pal.ink("auto")
		}
		if usual == "" {
			return a.pal.ink("auto") + a.pal.warn(" · nothing allowed can sit this seat")
		}
		if width < crewNarrow {
			return a.pal.ink("auto")
		}
		word := " · likely "
		if p.usualSeen[seat] {
			word = " · usually "
		}
		return a.pal.ink("auto") + a.pal.dim(word+usual)
	}
	value := a.icon(tokens.GPinned) + " " + a.pal.data(crewroute.ShortModel(pin.Model))
	if pin.Provider != "" {
		value += a.pal.dim(" @" + pin.Provider)
	}
	if why := p.pinTrouble(pin); why != "" {
		value += "  " + a.pal.warn(why)
	}
	return value
}

// pinTrouble is why a pin cannot run on what is connected, or "": its
// provider is not connected, or no connected provider reaches its model.
func (p *crewPanel) pinTrouble(pin config.CrewPin) string {
	if pin.Provider != "" {
		for _, provider := range p.providers {
			if provider.ID == pin.Provider {
				return ""
			}
		}
		return "unavailable · " + pin.Provider + " is not connected"
	}
	lineage := crewroute.Lineage(strings.TrimPrefix(pin.Model, "openrouter/"))
	for _, offer := range p.offers {
		if crewroute.Lineage(offer.Model.ID) == lineage {
			return ""
		}
	}
	// A pin written with a connection's own prefix is reached by that
	// connection whether or not the catalog knows the model.
	for _, provider := range p.providers {
		if strings.HasPrefix(strings.ToLower(pin.Model), strings.ToLower(provider.Written)+"/") {
			return ""
		}
	}
	return "unavailable"
}

// crewModelsValue is the models row: the answer between its two arrows, and
// what it admits. On a price it is the two ceilings as holes; on custom, the
// rule it stands for.
func (a *app) crewModelsValue() string {
	p := &a.crewUI
	step := p.modelStep()
	value := a.pal.dim("‹ ") + a.pal.ink(crewModelBases[step]) + a.pal.dim(" ›")
	switch step {
	case 2:
		value += "  " + a.pal.dim("≤ $") + a.crewHole(0, p.priceIn) + a.pal.dim(" in / $") + a.crewHole(1, p.priceOut) + a.pal.dim(" out")
	case 3:
		if p.rule.Custom() {
			value += "  " + a.pal.dim(p.rule.String())
		} else {
			value += "  " + a.pal.dim("enter to pick")
		}
	}
	if step != 3 || p.rule.Custom() {
		value += a.pal.dim(" (" + strconv.Itoa(p.countFor(step)) + ")")
	}
	return value
}

// crewHole is one price ceiling: `[ 1 ]`, painted as the question form paints
// a hole, with the one being typed into in bold ink.
func (a *app) crewHole(which int, value float64) string {
	p := &a.crewUI
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if p.edit != nil && p.edit.stop == crewModels && p.edit.price == which {
		return a.pal.bold(a.pal.ink("[ " + p.edit.box.String() + " ]"))
	}
	return a.pal.dim("[ ") + a.pal.ink(text) + a.pal.dim(" ]")
}

// crewCapValue is the cap row: none, the figure, or the hole being typed.
func (a *app) crewCapValue() string {
	p := &a.crewUI
	if p.edit != nil && p.edit.stop == crewCap {
		return a.pal.bold(a.pal.ink("$[ "+p.edit.box.String()+" ]")) + a.pal.dim(" a day · empty is none")
	}
	if p.capUSD <= 0 {
		return a.pal.ink("none")
	}
	return a.pal.ink(crewroute.Money(p.capUSD)) + a.pal.dim(" a day")
}

// crewTodayWord is the one dim line about the day: what the crews spent and
// how many tasks they ran. A day with nothing in it says nothing.
func (a *app) crewTodayWord() string {
	log := a.crewUI.log
	if log.Tasks == 0 && log.SpentUSD <= 0 {
		return ""
	}
	day := "today " + crewroute.Money(log.SpentUSD)
	if log.Tasks > 0 {
		day += " · " + strconv.Itoa(log.Tasks) + " tasks"
		if log.OnPlan > 0 {
			day += ", " + strconv.Itoa(log.OnPlan) + " on a plan"
		}
		if log.Local > 0 {
			day += ", " + strconv.Itoa(log.Local) + " local"
		}
	}
	if capUSD := a.crewUI.capUSD; capUSD > 0 && log.SpentUSD >= capUSD {
		day += " · at the cap"
	}
	return day
}

// crewWarnings is what cannot work, one line each: no provider at all, a
// crew that rides one model, and the allowed models leaving a class of work
// without the seat that class needs.
func (a *app) crewWarnings() []string {
	p := &a.crewUI
	var out []string
	if a.oneModel {
		out = append(out, crewOneModelWord)
	}
	if len(p.providers) == 0 {
		out = append(out, crewNoProviderWord)
		return out
	}
	for _, gap := range p.gaps {
		out = append(out, gap.Line)
	}
	return out
}

// crewArrowSpans finds the models row's two arrows in the drawn line, by
// cell, so a press on either walks the row.
func crewArrowSpans(line string) [2]hudSpan {
	plainLine := ansi.Strip(line)
	var spans [2]hudSpan
	cells := 0
	for _, r := range plainLine {
		w := ansi.StringWidth(string(r))
		switch r {
		case '‹':
			spans[0] = hudSpan{from: cells, to: cells + w}
		case '›':
			if cells > 2 {
				spans[1] = hudSpan{from: cells, to: cells + w}
			}
		}
		cells += w
	}
	return spans
}

// crewPickRows is the seat list's rows.
func (a *app) crewPickRows(width, hover int) ([]string, []int) {
	p := &a.crewUI
	k := p.pick
	var rows []string
	var owner []int
	if len(k.lines) == 0 {
		return []string{a.pal.dim(fit("  "+crewNothingMatches, width))}, []int{-1}
	}
	pin, pinned := p.pins[k.seat]
	k.top = listTop(k.cursor, k.top, len(k.lines), crewListRows)
	for at := k.top; at < len(k.lines) && at < k.top+crewListRows; at++ {
		line := k.lines[at]
		row := k.rows[line.row]
		selected, hovered := at == k.cursor, hover == len(rows)
		var text string
		marked := false
		switch {
		case row.auto:
			text = "  " + a.pal.ink(crewAutoWord)
			marked = !pinned
		case line.route < 0:
			current := pinned && crewroute.Lineage(pin.Model) == crewroute.Lineage(row.offer.Model.ID)
			text = a.crewOfferText(row, current, width)
			marked = current && k.unfold != row.offer.Model.ID
		default:
			// A ROUTE HANGS UNDER ITS MODEL, two cells further in than the name
			// it belongs to — the fold /model's list draws ([laneIndent]).
			text = strings.Repeat(" ", 4) + a.crewRouteText(row.offer, line.route, pin, pinned)
			marked = crewRouteMarked(row.offer, line.route, pin, pinned)
		}
		rows = append(rows, a.crewRowLine(text, selected, hovered, marked, width))
		owner = append(owner, at)
		if k.refuse == at {
			for _, said := range crewSaid(a.icon(tokens.GFailed)+" "+k.refuseWhy, width, a.pal.warn) {
				rows = append(rows, said)
				owner = append(owner, -1)
			}
		}
	}
	if p.refusal != "" {
		for _, said := range crewSaid(a.icon(tokens.GFailed)+" "+p.refusal, width, a.pal.bad) {
			rows = append(rows, said)
			owner = append(owner, -1)
		}
	}
	return rows, owner
}

// crewRouteMarked is whether one route line is the pin this seat is on.
func crewRouteMarked(offer config.CrewOffer, route int, pin config.CrewPin, pinned bool) bool {
	if !pinned || crewroute.Lineage(pin.Model) != crewroute.Lineage(offer.Model.ID) {
		return false
	}
	if route == 0 {
		return pin.Provider == ""
	}
	return route-1 < len(offer.Routes) && offer.Routes[route-1].Provider == pin.Provider
}

// crewOfferText is one model on the seat list: the suggestion's star, the
// name, the price in and out per million, and one provider — `+n` for the
// rest, which → lays out.
func (a *app) crewOfferText(row crewPickRow, current bool, width int) string {
	offer := row.offer
	name := offer.Model.ID
	if width < crewNarrow {
		name = crewroute.ShortModel(name)
	}
	lead := "  "
	if row.suggested {
		lead = a.pal.warn(a.icon(tokens.GRecommended)) + " "
	}
	ink := a.pal.ink
	if !offer.Allowed {
		ink = a.pal.dim
	}
	text := lead + ink(name)
	if current {
		text += " " + a.icon(tokens.GPinned)
	}
	facts := []string{crewPerM(offer.Model)}
	if len(offer.Routes) > 0 {
		provider := offer.Routes[0].Provider
		if extra := len(offer.Routes) - 1; extra > 0 {
			provider += " +" + strconv.Itoa(extra)
		}
		facts = append(facts, provider)
	}
	if !offer.Allowed {
		facts = append(facts, "not allowed")
	}
	if row.suggested {
		facts = append(facts, "suggested")
	}
	return text + "  " + a.pal.dim(strings.Join(facts, " · "))
}

// crewRouteText is one route under an unfolded model: "any route" first, then
// each provider with how it bills.
func (a *app) crewRouteText(offer config.CrewOffer, route int, pin config.CrewPin, pinned bool) string {
	if route == 0 {
		return a.pal.ink(crewAnyRouteWord)
	}
	r := offer.Routes[route-1]
	return a.pal.ink(r.Provider) + a.pal.dim(" · "+string(r.Kind))
}

// crewPerM is a model's price as the list shows it: dollars per million
// tokens in and out, cents at most.
func crewPerM(m crewroute.Model) string {
	return "$" + crewCents(m.PromptPrice*1e6) + "/$" + crewCents(m.CompletionPrice*1e6)
}

// crewCents spells a price to the cent: whole dollars bare, anything else to
// two places, so a column of prices reads as money ($0.50, not $0.5).
func crewCents(usd float64) string {
	cents := math.Round(usd * 100)
	if math.Mod(cents, 100) == 0 {
		return strconv.FormatFloat(cents/100, 'f', 0, 64)
	}
	return strconv.FormatFloat(cents/100, 'f', 2, 64)
}

// crewCheckRows is the checklist's rows: a tick where the rule admits the
// line, nothing where it does not.
func (a *app) crewCheckRows(width, hover int) ([]string, []int) {
	p := &a.crewUI
	c := p.check
	if len(c.hits) == 0 {
		return []string{a.pal.dim(fit("  "+crewNothingMatches, width))}, []int{-1}
	}
	var rows []string
	var owner []int
	c.top = listTop(c.cursor, c.top, len(c.hits), crewListRows)
	for at := c.top; at < len(c.hits) && at < c.top+crewListRows; at++ {
		line := c.lines[c.hits[at]]
		mark := "  "
		if p.ticked(line) {
			mark = a.pal.ink(a.icon(tokens.GSettled)) + " "
		}
		var text string
		if line.provider != nil {
			text = mark + a.pal.ink(line.provider.ID) + a.pal.dim("  whole provider · "+string(line.provider.Kind))
		} else {
			text = mark + a.pal.ink(line.offer.Model.ID) + a.pal.dim("  "+crewPerM(line.offer.Model))
		}
		rows = append(rows, a.crewRowLine(text, at == c.cursor, hover == len(rows), false, width))
		owner = append(owner, at)
	}
	if p.refusal != "" {
		rows = append(rows, a.pal.bad(fit("  "+a.icon(tokens.GFailed)+" "+p.refusal, width)))
		owner = append(owner, -1)
	}
	return rows, owner
}

// crewKeyLines is `?`: every key the panel takes, and the pointer's share.
var crewKeyLines = [][2]string{
	{"↑↓  j k", "move"},
	{"enter", "change the row · pick · tick"},
	{"←→", "walk the models row · a model's routes"},
	{"0-9", "type the cap, or a price ceiling"},
	{"type", "filter a list"},
	{"z", "undo the last change, for a few seconds"},
	{"esc", "back one level · close"},
	{"click", "a row is enter · ‹ › walk the models row"},
	{"wheel", "scrolls a list"},
}

// crewKeyRows draws the key list.
func (a *app) crewKeyRows(width int) ([]string, []int) {
	var rows []string
	var owner []int
	for _, pair := range crewKeyLines {
		cell := pair[0] + strings.Repeat(" ", max(1, 10-ansi.StringWidth(pair[0])))
		rows = append(rows, fit("  "+a.pal.ink(cell)+a.pal.dim(pair[1]), width))
		owner = append(owner, -1)
	}
	return rows, owner
}

// ── the pointer ─────────────────────────────────────────────────────────────

// crewPress is a press on the panel. A PRESS ON A ROW IS ENTER ON IT — the one
// click grammar every list here keeps — and a press on one of the models row's
// arrows walks it. A press anywhere off the panel closes it, which is what
// pressing outside a modal list means everywhere on this surface.
func (a *app) crewPress(x, y int) tea.Cmd {
	p := &a.crewUI
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		if p.view == crewMain && p.edit == nil {
			return a.closeCrew()
		}
		return nil
	}
	at := -1
	if mark.index >= 0 && mark.index < len(p.owner) {
		at = p.owner[mark.index]
	}
	defer a.touch()
	// A HOLE BEING TYPED INTO IS NOT A LIST: a press elsewhere is swallowed and
	// esc is the way out, which is the way out of every box here.
	if p.edit != nil || at < 0 {
		return nil
	}
	x -= 1 // the frame's side cell
	switch p.view {
	case crewMain:
		p.cursor = at
		if at == crewModels && mark.index == p.arrowLine {
			switch {
			case p.arrows[0].holds(x):
				return a.crewStepModels(-1)
			case p.arrows[1].holds(x):
				return a.crewStepModels(1)
			}
		}
		return a.crewEnter()
	case crewPicking:
		p.pick.cursor = at
		return a.crewPickEnter()
	case crewChecking:
		p.check.cursor = at
		return a.crewTick()
	}
	return nil
}

// crewWheel walks whichever list is up, a row a notch — the one wheel an
// overlay here takes, because it is the one with lists longer than its frame.
func (a *app) crewWheel(delta int) {
	p := &a.crewUI
	switch p.view {
	case crewPicking:
		p.pick.move(delta)
	case crewChecking:
		p.check.move(delta)
	case crewMain:
		if p.edit == nil {
			a.crewMove(delta)
		}
	}
	a.touch()
}

// crewBox is the one-line box under the panel while a list is up — its
// filter, in the place every overlay's filter stands — and nil otherwise.
func (a *app) crewBox() *editor {
	switch p := &a.crewUI; {
	case !p.open:
		return nil
	case p.view == crewPicking:
		return &p.pick.filter
	case p.view == crewChecking:
		return &p.check.filter
	}
	return nil
}

// crewNow is the reading a shortcut hands the panel after it wrote: the panel
// opens — or, open already, re-reads — with the tick on the row the shortcut
// changed.
func (a *app) crewNow(stop int, before config.CrewState) tea.Cmd {
	if a.hosted() {
		return nil
	}
	if !a.crewUI.open {
		a.openCrew()
	} else {
		a.crewUI.read(a.profileDir)
	}
	p := &a.crewUI
	if p.view != crewMain {
		p.view, p.pick, p.check = crewMain, nil, nil
	}
	p.cursor, p.undo = stop, &before
	p.saved, p.savedAt = stop, a.now()
	a.touch()
	return surfaceTick(crewUndoFor, func(time.Time) tea.Msg { return crewUndoMsg{} })
}
