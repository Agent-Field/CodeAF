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
	"github.com/Agent-Field/codeaf/internal/modelsource"
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
//	│   providers ✓ openrouter  ✓ codex sub  ○ my-vllm  + │
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
// SIX ROWS, ONE VERB. Three seats, the allowed models, the providers and the
// cap are the only rows the cursor stops on, and enter is what every one of
// them answers:
//
//   - A SEAT opens the one list this panel has — the models a seat could sit,
//     `auto` first, the router's own suggestion marked — and enter on a row of
//     it pins that seat. Unpinning is choosing `auto`, which is the first row
//     the list opens on, so it is two presses and there is no second verb for it.
//   - THE MODELS ROW IS EDITED WHERE IT STANDS. ←/→ walk its four answers — all,
//     open, a price ceiling, custom — and each step is written as it is taken; a
//     price turns the row into its two ceilings, typed into holes on the row
//     itself; custom opens the checklist, the one other screen here.
//   - THE PROVIDERS ROW IS TOGGLED WHERE IT STANDS. It is one chip per
//     connected provider, ←/→ walk them, and space turns the one under the
//     cursor off or on; the `+` at its end is /connect. Enter opens the one
//     screen that says more about each — how it bills, how many models it
//     serves, what it carried today.
//   - THE CAP IS TYPED WHERE IT STANDS. A digit on the row starts it, enter
//     keeps it, an emptied hole is no cap. There is no dialog to open first.
//
// EVERY CHANGE IS WRITTEN THE MOMENT IT IS MADE, through the same writers the
// `/crew` shortcuts use (internal/config's SetCrewPin, SetCrewAllowedRule,
// SetCrewProviderOn, SetCrewCap), so a refusal is the same refusal whichever door asked. What
// makes that safe is the undo beside it: the row that changed wears a tick,
// and for a few seconds the bottom edge offers `z undo`, which puts the rows
// back exactly as they were ([config.RestoreCrewState]).
//
// A PROVIDER TURNED OFF IS A ROUTE TAKEN AWAY, NOT A MODEL. The allowed models
// are the models row's rule less every model no provider that is still on
// serves, and the models row counts exactly that; the set of providers turned
// off is a row of its own beside the rule (internal/crewroute's providers.go
// argues why it is never a `-x` inside it). Connecting a provider is still
// what adds one — the row only ever lists connections — so a provider
// connected tomorrow is on the day it is connected, and the last one on
// cannot be turned off, because a crew with nothing to route through is no
// crew at all.
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
	// crewMain is the six rows.
	crewMain crewView = iota
	// crewPicking is the list a seat is pinned from.
	crewPicking
	// crewChecking is the custom checklist of allowed models.
	crewChecking
	// crewProviding is the providers row laid out, one provider a line.
	crewProviding
	// crewKeys is `?`: every key and gesture the panel takes.
	crewKeys
)

// The main panel's six stops, in the order the cursor walks them. The three
// seats come first and in [crewroute.Seats] order, so a stop below crewModels
// is a seat by its index.
const (
	crewModels    = 3
	crewProviders = 4
	crewCap       = 5
	crewStops     = 6
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
	crewChipKeys       = "enter change · space toggle · esc close · ? keys"
	crewProvKeys       = "space or enter toggle · esc back"
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
	crewProviderOff    = "provider off"
	crewFreeRoutesWord = "free routes"
	crewFreeRoutesWhy  = "rate-limited, may log prompts"
	crewConnectChip    = "+"
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
	taskUSD   float64
	free      bool
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

	// chip is the providers row's position: a provider by its index in
	// providers, or len(providers) for the `+` that opens /connect.
	chip int
	// chips are the cells of each chip on the drawn row, the `+` last, and
	// chipLine the drawn line they are on, -1 when not drawn; folded is the
	// row drawn as its count because the chips did not fit, when ←/→ have
	// nothing to walk and space opens the list instead.
	chips    []hudSpan
	chipLine int
	folded   bool
	// prov is the providers list's cursor, and provSaved the line that took
	// the last change there, which wears the tick while [crewPanel.live].
	prov      int
	provSaved int
}

// crewHole is one hole typed into on the main rows.
type crewHole struct {
	// stop is the row the hole is on; price is which ceiling — 0 in, 1 out —
	// when the stop is the models row, and which limit — 0 daily, 1 per task —
	// when it is the cap row.
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
	// tier is each hit's match tier ([crewMatchTier]), which outranks score.
	tier []int
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
//
// IT IS MODELS ONLY. A provider is taken away on the providers row, where the
// verdict is a set beside the rule; a `-provider` inside the rule would also
// read as a vendor, and would be dropped the next time the models row was
// walked onto a new base.
type crewCheck struct {
	filter editor
	// lines are the models; hits index them.
	lines  []crewCheckLine
	hits   []int
	cursor int
	top    int
}

// crewCheckLine is one checklist line: an offer.
type crewCheckLine struct {
	offer *config.CrewOffer
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
	p.taskUSD = config.CrewTaskCapAt(dir)
	p.free = config.CrewFreeRoutesAt(dir)
	p.providers = config.CrewProvidersAt(dir)
	p.chip = min(p.chip, len(p.providers))
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
	crewReachableUsual(p.usual, p.usualSeen, p.suggest, p.offers)
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

// crewReachableUsual keeps a seat's hint only where it names a model the seat
// could be given TODAY.
//
// "USUALLY" AND "LIKELY" ARE CLAIMS ABOUT NOW, not only about the past: a model
// the recent tasks ran whose route has since been quarantined, whose provider
// was turned off, which the allowed rule no longer admits, or which reached
// the seat only through a free pool now switched off, gives way to what the
// router would pick now; and where that is not reachable either, the hint is
// empty, which the seat reads as "nothing allowed can sit this seat". An offer
// is reachable when the rule admits it on a provider that is on — the offers
// themselves already left out unhealthy and switched-off free routes
// ([config.CrewOffersAt]).
func crewReachableUsual(usual map[crewroute.Seat]string, seen map[crewroute.Seat]bool, suggest map[crewroute.Seat]string, offers []config.CrewOffer) {
	reachable := map[string]bool{}
	for _, offer := range offers {
		if offer.Allowed && offer.Served {
			reachable[crewroute.ShortModel(offer.Model.ID)] = true
		}
	}
	for _, seat := range crewroute.Seats {
		if model := usual[seat]; model != "" && reachable[model] {
			continue
		}
		usual[seat], seen[seat] = "", false
		if model := crewroute.ShortModel(suggest[seat]); reachable[model] {
			usual[seat] = model
		}
	}
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
// answer would leave. A model only a provider turned off serves is not
// counted on any step: it is not one a seat can be picked from.
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
		if offer.Served && rule.AdmitsModel(offer.Model) {
			n++
		}
	}
	return n
}

// providersOn is how many connected providers are on.
func (p *crewPanel) providersOn() int {
	n := 0
	for _, provider := range p.providers {
		if provider.On {
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
	a.crewUI = crewPanel{open: true, back: back, backTab: tab, backCursor: cursor, saved: -1, step: -1, chipLine: -1, provSaved: -1}
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
	case crewProviding:
		return a.crewProvKey(msg)
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
		if p.cursor == crewProviders && !p.folded {
			p.chip = max(0, p.chip-1)
		}
	case "right", "l":
		if p.cursor == crewModels {
			return a.crewStepModels(1)
		}
		if p.cursor == crewProviders && !p.folded {
			p.chip = min(len(p.providers), p.chip+1)
		}
		if p.cursor < crewModels {
			a.crewOpenPick(crewroute.Seats[p.cursor])
		}
	case " ", "space":
		if p.cursor == crewProviders {
			return a.crewSpaceChip()
		}
		return a.crewEnter()
	case "enter":
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

// crewMove walks the six stops, clamping at both ends. Leaving the models
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
	case p.cursor == crewProviders:
		if !p.folded && p.chip == len(p.providers) {
			return a.crewConnect()
		}
		a.crewOpenProv()
	case p.cursor == crewCap:
		p.edit = &crewHole{stop: crewCap}
		if p.capUSD > 0 {
			p.edit.box.setText(crewDollarsWord(p.capUSD))
		}
	}
	return nil
}

// ── the providers row ───────────────────────────────────────────────────────

// crewSpaceChip is space on the providers row: the chip under the cursor
// turned off or on, or /connect on the `+`. A row folded to its count has no
// chip under the cursor, so space there opens the list, where every provider
// is a line of its own.
func (a *app) crewSpaceChip() tea.Cmd {
	p := &a.crewUI
	switch {
	case p.folded:
		a.crewOpenProv()
		return nil
	case p.chip >= len(p.providers):
		return a.crewConnect()
	}
	return a.crewToggleProvider(p.chip)
}

// crewToggleProvider turns one provider off or on, through the writer the
// shortcut uses — so the last provider on is refused here in the writer's own
// words ([config.ErrCrewLastProvider]), under the rows, and nothing changes.
func (a *app) crewToggleProvider(at int) tea.Cmd {
	p := &a.crewUI
	if at < 0 || at >= len(p.providers) {
		return nil
	}
	provider := p.providers[at]
	cmd := a.crewWrite(crewProviders, func(dir string) error { return config.SetCrewProviderOn(dir, provider.ID, !provider.On) })
	if p.refusal == "" && p.view == crewProviding {
		p.provSaved = at
	}
	return cmd
}

// crewConnect is the `+`: the panel goes and /connect opens in its place —
// the one door a provider is added through, never a second copy of it here.
// A provider connected there is on the next time this panel is read, and
// backing out of /connect comes back here, to the providers row
// ([app.dismissConnect]), because that is where the person asked from.
func (a *app) crewConnect() tea.Cmd {
	p := &a.crewUI
	back := &crewReturn{back: p.back, tab: p.backTab, cursor: p.backCursor}
	p.close()
	a.openConnect()
	if a.connPanel.open {
		a.connPanel.crew = back
	}
	a.touch()
	return nil
}

// crewReturn is where the crew panel stood when its `+` handed over to
// /connect: the place it was opened from, which its own esc goes back to.
type crewReturn struct {
	back        page
	tab, cursor int
}

// dismissConnect is backing out of /connect — esc on the list, or a press off
// it. Opened from the crew panel's `+`, it stands the crew panel back up on
// the providers row, cursor on the `+`; opened any other way it just closes.
func (a *app) dismissConnect() {
	back := a.connPanel.crew
	a.connPanel.close()
	if back == nil {
		return
	}
	a.crewUI = crewPanel{open: true, back: back.back, backTab: back.tab, backCursor: back.cursor,
		saved: -1, step: -1, chipLine: -1, provSaved: -1, cursor: crewProviders}
	a.crewUI.read(a.profileDir)
	a.crewUI.chip = len(a.crewUI.providers)
}

// crewOpenProv opens the providers list on the provider the row's cursor was
// on.
func (a *app) crewOpenProv() {
	p := &a.crewUI
	p.view, p.prov, p.provSaved = crewProviding, min(p.chip, max(0, len(p.providers)-1)), -1
}

// crewProvLines is how many lines the providers list has: a provider a line
// and the free-routes switch last, which is the list's and never the row's —
// a free pool is a way a provider can be reached, and the list is where how
// each one is reached is said.
func (p *crewPanel) crewProvLines() int {
	if len(p.providers) == 0 {
		return 0
	}
	return len(p.providers) + 1
}

// crewProvKey is a key on the providers list. There is no filter: a
// person has a handful of connections, and every letter is free to mean
// what it means on the rows — `z` is the undo here too.
func (a *app) crewProvKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.crewUI
	p.refusal = ""
	switch msg.String() {
	case "esc", "q":
		p.view, p.cursor = crewMain, crewProviders
		p.chip = min(p.prov, max(0, len(p.providers)-1))
	case "up", "k", "ctrl+p":
		p.prov = moveCursor(p.prov, -1, p.crewProvLines())
	case "down", "j", "ctrl+n":
		p.prov = moveCursor(p.prov, 1, p.crewProvLines())
	case "home":
		p.prov = 0
	case "end":
		p.prov = max(0, p.crewProvLines()-1)
	case "z":
		a.crewUndo()
	case "enter", " ", "space":
		return a.crewProvToggle()
	}
	return nil
}

// crewProvToggle is space or enter on a line of the providers list: a
// provider turned off or on, or the free routes.
func (a *app) crewProvToggle() tea.Cmd {
	p := &a.crewUI
	if p.prov == len(p.providers) && len(p.providers) > 0 {
		free := !p.free
		cmd := a.crewWrite(crewProviders, func(dir string) error { return config.SetCrewFreeRoutes(dir, free) })
		if p.refusal == "" {
			p.provSaved = p.prov
		}
		return cmd
	}
	return a.crewToggleProvider(p.prov)
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
		if hole.stop == crewCap {
			a.crewEditCap(1 - hole.price)
		}
		return nil
	case "enter":
		if hole.stop == crewCap {
			p.edit = nil
			raw := strings.TrimSpace(hole.box.String())
			if hole.price == 1 {
				// AN EMPTIED PER-TASK HOLE IS THE DEFAULT: a task always has a limit.
				if raw == "" {
					raw = strconv.FormatFloat(config.CrewTaskCapDefault, 'f', -1, 64)
				}
				return a.crewWrite(crewCap, func(dir string) error { return config.SetCrewTaskCap(dir, raw) })
			}
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

// crewEditCap opens one of the cap row's two holes: 0 the daily cap, 1 the
// per-task limit, holding the figure in force.
func (a *app) crewEditCap(which int) {
	p := &a.crewUI
	p.edit = &crewHole{stop: crewCap, price: which}
	if which == 1 {
		p.edit.box.setText(crewDollarsWord(p.taskUSD))
		return
	}
	p.edit.box.setText(crewDollarsWord(p.capUSD))
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
//
// A MODEL NAME IS TYPED FROM ITS FRONT. The shared matcher takes any
// subsequence, which is right for a command palette and wrong for a list of
// model ids: `kim` finds kimi-k3, and also gro-k-IM-agine and K-rea-2-medIum,
// which nobody typing three letters of a name meant. So the matcher decides
// only WHETHER a row matches, and how strongly within its tier; the tier
// decides the order first ([crewMatchTier]) — a prefix of the id or the name,
// then a prefix of a word inside it, then anywhere inside it, then a
// subsequence — and a subsequence-only row is shown only when nothing matched
// better, because then it is the best the list has.
func (k *crewPick) rank() {
	query := strings.TrimSpace(k.filter.String())
	words := strings.Fields(strings.ToLower(query))
	ft := fuzzyTerms(words)
	k.hits = k.hits[:0]
	if len(k.tier) != len(k.rows) {
		k.tier = make([]int, len(k.rows))
	}
	strong := false
	for i, row := range k.rows {
		if len(ft) == 0 {
			k.hits = append(k.hits, i)
			continue
		}
		total, hit := fuzzy.ScoreFields(row.fields, ft)
		if !hit {
			continue
		}
		k.score[i], k.tier[i] = total, crewMatchTier(row.fields, words)
		strong = strong || k.tier[i] < crewTierSubsequence
		k.hits = append(k.hits, i)
	}
	if len(ft) > 0 {
		if strong {
			kept := k.hits[:0]
			for _, at := range k.hits {
				if k.tier[at] < crewTierSubsequence {
					kept = append(kept, at)
				}
			}
			k.hits = kept
		}
		sort.SliceStable(k.hits, func(a, b int) bool {
			ta, tb := k.tier[k.hits[a]], k.tier[k.hits[b]]
			if ta != tb {
				return ta < tb
			}
			return k.score[k.hits[a]] > k.score[k.hits[b]]
		})
	}
	k.cursor, k.top, k.refuse = 0, 0, -1
	k.relist()
}

// The seat list's match tiers, strongest first.
const (
	// crewTierPrefix is a field the word begins: `kim` of kimi-k3, `deep` of
	// deepseek/deepseek-v4-flash.
	crewTierPrefix = iota
	// crewTierWord is a word inside a field the word begins, after a `/`, a
	// `-`, a `.` or a space: `flash` of glm-5.3-flash.
	crewTierWord
	// crewTierInside is the word anywhere inside a field.
	crewTierInside
	// crewTierSubsequence is the word's letters in order, with gaps — the
	// matcher's own verdict and nothing stronger.
	crewTierSubsequence
)

// crewMatchTier is how strongly a row matches every typed word: the weakest
// of the words' own tiers, each word's the strongest over the row's fields.
func crewMatchTier(fields, words []string) int {
	tier := crewTierPrefix
	for _, word := range words {
		best := crewTierSubsequence
		for _, field := range fields {
			field = strings.ToLower(field)
			switch {
			case strings.HasPrefix(field, word):
				best = crewTierPrefix
			case crewWordPrefix(field, word):
				best = min(best, crewTierWord)
			case strings.Contains(field, word):
				best = min(best, crewTierInside)
			}
		}
		tier = max(tier, best)
	}
	return tier
}

// crewWordPrefix is whether a word begins some word inside a field.
func crewWordPrefix(field, word string) bool {
	for i := 1; i < len(field); i++ {
		switch field[i-1] {
		case '/', '-', '.', ' ', '_', ':':
			if strings.HasPrefix(field[i:], word) {
				return true
			}
		}
	}
	return false
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
		route := row.offer.Routes[line.route-1]
		provider = route.Provider
		if route.Kind == crewroute.Free && !crewroute.IsFree(raw) {
			// THE FREE POOL IS THE CHOICE: the pin names it, or it would run on
			// the paid route of the same provider.
			raw += ":free"
		}
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

// crewOpenCheck opens the checklist: every model a connected provider
// reaches, each ticked where the rule admits it.
func (a *app) crewOpenCheck() {
	p := &a.crewUI
	check := &crewCheck{}
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
			fields := []string{line.offer.Model.ID, crewroute.ShortModel(line.offer.Model.ID)}
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
	rule, ok := p.rule.Toggled(line.offer.Model, !p.ticked(line))
	if !ok {
		p.refusal = "a list keeps one model at least — tick another before this one"
		return nil
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
		p.arrowLine = crewArrowLine(owner, p, crewModels)
		p.chipLine = crewArrowLine(owner, p, crewProviders)
		return crewPad(rows, n)
	}
	rows, owner = crewWindow(rows, owner, p.focusLine(owner), room)
	title, keys := a.crewEdges()
	aside := a.pal.dim("esc")
	// THE UNDO IS OFFERED WHERE `z` MEANS IT: on the six rows and the providers
	// list, which has no filter. In a filtered list the letter is the filter's,
	// and in a hole it is nothing, so an offer drawn there would be a key that
	// does something else.
	keysAside := ""
	if (p.view == crewMain || p.view == crewProviding) && p.edit == nil && p.live(a.now()) && p.undo != nil {
		keysAside = a.pal.dim(crewUndoWord)
	}
	lines, _ := framed{title: title, aside: aside, keys: a.pal.dim(keys), keysAside: keysAside}.draw(a.pal, width, rows)
	p.owner = append(append([]int{-1}, owner...), -1)
	p.arrowLine = crewArrowLine(p.owner, p, crewModels)
	p.chipLine = crewArrowLine(p.owner, p, crewProviders)
	return crewPad(lines, n)
}

// crewArrowLine is the drawn line of one main row — the models row, whose
// arrows a press walks, or the providers row, whose chips a press toggles —
// and -1 when the main rows are not up.
func crewArrowLine(owner []int, p *crewPanel, stop int) int {
	if p.view != crewMain {
		return -1
	}
	for i, at := range owner {
		if at == stop {
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
	case crewProviding:
		want = p.prov
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
	case crewProviding:
		return a.pal.muted(crewTitleWord + " · providers · " + strconv.Itoa(p.providersOn()) + " of " + strconv.Itoa(len(p.providers)) + " on"), crewProvKeys
	}
	if p.cursor == crewProviders && p.edit == nil {
		// SPACE IS SAID WHERE IT MEANS SOMETHING OF ITS OWN. On every other row it
		// is enter, which the edge already says.
		return title, crewChipKeys
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
	case crewProviding:
		return a.crewProvRows(inner, hover)
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
			value += "  " + a.pal.add(a.crewMark(tokens.GSettled))
		}
		add(a.crewRowLine(a.crewLabel(string(seat), p.cursor == i)+value, p.cursor == i, hover == len(rows), false, width), i)
	}
	add("", -1)
	models := a.crewModelsValue()
	if p.live(now) && p.saved == crewModels {
		models += "  " + a.pal.add(a.crewMark(tokens.GSettled))
	}
	modelsLine := a.crewRowLine(a.crewLabel("models", p.cursor == crewModels)+models, p.cursor == crewModels, hover == len(rows), false, width)
	p.arrows = crewArrowSpans(modelsLine)
	add(modelsLine, crewModels)
	add(a.crewProvidersLine(width, hover == len(rows)), crewProviders)
	capValue := a.crewCapValue()
	if p.live(now) && p.saved == crewCap {
		capValue += "  " + a.pal.add(a.crewMark(tokens.GSettled))
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
		for _, line := range crewSaid(a.crewMark(tokens.GFailed)+" "+p.refusal, width, a.pal.bad) {
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
	value := a.crewMark(tokens.GPinned) + " " + a.pal.data(crewroute.ShortModel(pin.Model))
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
//
// A PIN WHOSE ONLY PROVIDER IS TURNED OFF says so rather than `unavailable`:
// the provider is still connected, and the fix is a space on the providers
// row, not a trip to /connect.
func (p *crewPanel) pinTrouble(pin config.CrewPin) string {
	if pin.Provider != "" {
		for _, provider := range p.providers {
			if provider.ID == pin.Provider {
				if !provider.On {
					return crewProviderOff
				}
				return ""
			}
		}
		return "unavailable · " + pin.Provider + " is not connected"
	}
	// A pin written with a connection's own prefix is reached by that
	// connection whether or not the catalog knows the model.
	for _, provider := range p.providers {
		if provider.ID != modelsource.DefaultID && strings.HasPrefix(strings.ToLower(pin.Model), strings.ToLower(provider.Written)+"/") {
			if !provider.On {
				return crewProviderOff
			}
			return ""
		}
	}
	// A PINNED FREE POOL RUNS whatever the free-routes switch says — a pin is
	// the person's own choice, and the switch governs only what the router
	// picks — so it is reachable wherever the default service is on.
	if crewroute.IsFree(strings.TrimPrefix(pin.Model, "openrouter/")) {
		for _, provider := range p.providers {
			if provider.ID == modelsource.DefaultID {
				if !provider.On {
					return crewProviderOff
				}
				return ""
			}
		}
	}
	lineage := crewroute.Lineage(strings.TrimPrefix(pin.Model, "openrouter/"))
	for _, offer := range p.offers {
		if crewroute.Lineage(offer.Model.ID) == lineage {
			if !offer.Served {
				return crewProviderOff
			}
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

// crewCapValue is the cap row: the per-task limit and the daily cap (none,
// or the figure), either one the hole being typed. Tab in a hole moves to the
// other.
func (a *app) crewCapValue() string {
	p := &a.crewUI
	hole := func(text string) string { return a.pal.bold(a.pal.ink("$[ " + text + " ]")) }
	task := a.pal.ink(config.CrewTaskMoney(p.taskUSD))
	daily := a.pal.ink("none")
	if p.capUSD > 0 {
		daily = a.pal.ink(crewroute.Money(p.capUSD))
	}
	if p.edit != nil && p.edit.stop == crewCap {
		if p.edit.price == 1 {
			return a.pal.dim("per task ") + hole(p.edit.box.String()) + a.pal.dim(" · daily ") + daily +
				a.pal.dim(" · empty is "+config.CrewTaskMoney(config.CrewTaskCapDefault)+" · tab daily")
		}
		return a.pal.dim("per task ") + task + a.pal.dim(" · daily ") + hole(p.edit.box.String()) +
			a.pal.dim(" · empty is none · tab per task")
	}
	return a.pal.dim("per task ") + task + a.pal.dim(" · daily ") + daily
}

// crewChipRoom is the cells a providers row keeps free past its chips, for
// the tick a toggle leaves — so the row that just changed never folds or
// unfolds under the tick it wears.
const crewChipRoom = 3

// crewChipWords is one provider as its chip says it: the name, and the kind
// hint where how it bills is not a key — `sub` for a subscription, `local`
// for a model on this machine when local is asked for. A custom endpoint
// is the prefix the person wrote for it, which is the name they gave it —
// its id and its catalog name are the same for every custom endpoint.
func crewChipWords(provider config.CrewProvider, local bool) (string, string) {
	name := provider.ID
	switch {
	case modelsource.IsCustomID(provider.ID):
		if provider.Written != "" {
			name = provider.Written
		}
	case provider.Kind == crewroute.Plan:
		return name, "sub"
	case provider.Kind == crewroute.Local && local:
		return name, "local"
	}
	return name, ""
}

// crewProvidersLine is the providers row, drawn, with the cells of every chip
// found on it for the pointer.
//
// THE CHIPS FOLD RATHER THAN CLIP. A row that does not fit drops its `local`
// hints first — the one word on it a person can do without — and then says
// only how many are on, `3 of 4 on`, with enter the way to the list that has
// room for every one. A chip cut in half would be a toggle nobody could read.
func (a *app) crewProvidersLine(width int, hovered bool) string {
	p := &a.crewUI
	selected := p.cursor == crewProviders
	room := width - ansi.StringWidth(overlayLead(false, false, a.pal)) - ansi.StringWidth(ansi.Strip(a.crewLabel("providers", false))) - crewChipRoom
	var words []string
	value := ""
	p.folded = false
	for _, local := range []bool{true, false} {
		value, words = a.crewChips(local, selected)
		if ansi.StringWidth(ansi.Strip(value)) <= room {
			break
		}
		value, words = "", nil
	}
	if value == "" {
		p.folded = true
		value = a.pal.ink(strconv.Itoa(p.providersOn())+" of "+strconv.Itoa(len(p.providers))) + a.pal.dim(" on")
	}
	if p.live(a.now()) && p.saved == crewProviders {
		value += "  " + a.pal.add(a.crewMark(tokens.GSettled))
	}
	line := a.crewRowLine(a.crewLabel("providers", selected)+value, selected, hovered, false, width)
	p.chips = crewChipSpans(line, words)
	return line
}

// crewChips is the chips, painted — on in ink behind a tick, off dim behind an
// empty circle, the cursor's in bold when the row has the cursor — and the
// plain words of each, in order, the `+` last.
func (a *app) crewChips(local, selected bool) (string, []string) {
	p := &a.crewUI
	var parts, words []string
	for i, provider := range p.providers {
		name, hint := crewChipWords(provider, local)
		mark, ink := a.crewMark(tokens.GSettled), a.pal.ink
		if !provider.On {
			mark, ink = a.crewMark(tokens.GQueued), a.pal.dim
		}
		plainWord := mark + " " + name
		text := ink(mark) + " " + ink(name)
		if selected && p.chip == i {
			text = a.pal.bold(ink(mark + " " + name))
		}
		if hint != "" {
			plainWord += " " + hint
			text += a.pal.dim(" " + hint)
		}
		parts, words = append(parts, text), append(words, plainWord)
	}
	plus := a.pal.dim(crewConnectChip)
	if selected && p.chip == len(p.providers) {
		plus = a.pal.bold(a.pal.ink(crewConnectChip))
	}
	parts, words = append(parts, plus), append(words, crewConnectChip)
	return strings.Join(parts, "  "), words
}

// crewChipSpans finds each chip's cells on the drawn row, in order, so a
// press on one toggles it — the models row's arrows found the same way
// ([crewArrowSpans]).
func crewChipSpans(line string, words []string) []hudSpan {
	if len(words) == 0 {
		return nil
	}
	plainLine := ansi.Strip(line)
	from := strings.Index(plainLine, "providers")
	if from < 0 {
		return nil
	}
	from += len("providers")
	spans := make([]hudSpan, 0, len(words))
	for _, word := range words {
		at := strings.Index(plainLine[from:], word)
		if at < 0 {
			return nil
		}
		start := ansi.StringWidth(plainLine[:from+at])
		spans = append(spans, hudSpan{from: start, to: start + ansi.StringWidth(word)})
		from += at + len(word)
	}
	return spans
}

// crewKindWord is how a provider bills, as the providers list says it.
func crewKindWord(provider config.CrewProvider) string {
	switch {
	case modelsource.IsCustomID(provider.ID):
		return "custom endpoint"
	case provider.Kind == crewroute.Plan:
		return "subscription"
	case provider.Kind == crewroute.Local:
		return "local"
	}
	return "api key"
}

// crewServedWord is how many of the models a seat could be pinned to one
// provider serves. A custom endpoint serves models the catalog does not
// describe, which a pin that names it reaches and nothing else does.
func (p *crewPanel) crewServedWord(provider config.CrewProvider) string {
	n := 0
	for _, offer := range p.offers {
		for _, r := range offer.Routes {
			if r.Provider == provider.ID {
				n++
				break
			}
		}
	}
	switch {
	case n == 0 && modelsource.IsCustomID(provider.ID):
		return "pins only"
	case n == 1:
		return "1 model"
	}
	return strconv.Itoa(n) + " models"
}

// crewDayWord is what one provider carried today, as the providers list says it.
func (p *crewPanel) crewDayWord(provider config.CrewProvider) string {
	if usd := p.log.ProviderUSD[provider.ID]; usd > 0 {
		return "today " + crewroute.Money(usd)
	}
	return "nothing today"
}

// crewProvRows is the providers list: one provider a line — on or off, its
// name, how it bills, how many models it serves and what it carried today,
// the day's share laid on it by the seats it carried (internal/router's
// crew.go). Columns line up, because a list of four is read down.
func (a *app) crewProvRows(width, hover int) ([]string, []int) {
	p := &a.crewUI
	if len(p.providers) == 0 {
		return []string{a.pal.dim(fit("  "+crewNoProviderWord, width))}, []int{-1}
	}
	nameW, kindW, servedW, dayW := 0, 0, 0, 0
	for _, provider := range p.providers {
		name, _ := crewChipWords(provider, false)
		nameW = max(nameW, ansi.StringWidth(name))
		kindW = max(kindW, ansi.StringWidth(crewKindWord(provider)))
		servedW = max(servedW, ansi.StringWidth(p.crewServedWord(provider)))
		dayW = max(dayW, ansi.StringWidth(p.crewDayWord(provider)))
	}
	// THE STATE WORD NEVER GOES. A line is the mark, the name and `on` or `off`
	// first; the facts between them give way when the frame is narrow, the day's
	// spend first, then how many models, then how it bills — the order a person
	// deciding whether to switch a provider off needs them least.
	pad := func(word string, w int) string { return word + strings.Repeat(" ", max(0, w-ansi.StringWidth(word))) }
	room := width - ansi.StringWidth(overlayLead(false, false, a.pal)) - crewChipRoom
	fixed := 2 + nameW + 2 + 3
	facts := 0
	for _, n := range []int{3, 2, 1} {
		w := fixed + kindW + 2
		if n >= 2 {
			w += servedW
		}
		if n >= 3 {
			w += 3 + dayW
		}
		if w <= room {
			facts = n
			break
		}
	}
	var rows []string
	var owner []int
	now := a.now()
	for i, provider := range p.providers {
		name, _ := crewChipWords(provider, false)
		mark, ink, state := a.crewMark(tokens.GSettled), a.pal.ink, "on"
		if !provider.On {
			mark, ink, state = a.crewMark(tokens.GQueued), a.pal.dim, "off"
		}
		var fact string
		switch facts {
		case 3:
			fact = pad(crewKindWord(provider), kindW) + "  " + pad(p.crewServedWord(provider), servedW) + " · " + pad(p.crewDayWord(provider), dayW)
		case 2:
			fact = pad(crewKindWord(provider), kindW) + "  " + pad(p.crewServedWord(provider), servedW)
		case 1:
			fact = pad(crewKindWord(provider), kindW)
		}
		text := ink(mark) + " " + ink(pad(name, nameW)) + "  "
		if fact != "" {
			text += a.pal.dim(fact) + "  "
		}
		text += ink(state)
		if p.live(now) && p.saved == crewProviders && p.provSaved == i {
			text += "  " + a.pal.add(a.crewMark(tokens.GSettled))
		}
		rows = append(rows, a.crewRowLine(text, i == p.prov, hover == len(rows), false, width))
		owner = append(owner, i)
	}
	// THE FREE ROUTES, apart from the providers and after them: a switch about
	// every provider at once, and off unless somebody turned it on, because a
	// free pool may keep what it is sent.
	rows, owner = append(rows, ""), append(owner, -1)
	at := len(p.providers)
	state, ink := "off", a.pal.dim
	if p.free {
		state, ink = "on", a.pal.ink
	}
	text := a.pal.ink(crewFreeRoutesWord) + "  " + ink(state) + a.pal.dim(" · "+crewFreeRoutesWhy)
	if p.live(now) && p.saved == crewProviders && p.provSaved == at {
		text += "  " + a.pal.add(a.crewMark(tokens.GSettled))
	}
	rows = append(rows, a.crewRowLine(text, p.prov == at, hover == len(rows), false, width))
	owner = append(owner, at)
	if p.refusal != "" {
		for _, said := range crewSaid(a.crewMark(tokens.GFailed)+" "+p.refusal, width, a.pal.bad) {
			rows = append(rows, said)
			owner = append(owner, -1)
		}
	}
	return rows, owner
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
		day += " · " + strconv.Itoa(log.Tasks) + " task"
		if log.Tasks != 1 {
			day += "s"
		}
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
	// A SEAT NOTHING ALLOWED CAN SIT ALREADY SAYS SO ON ITS ROW, and that is the
	// larger trouble: a warning that the checker will be weak beside a seat that
	// cannot be sat at all is the smaller half of the same sentence.
	for _, seat := range crewroute.Seats {
		if _, pinned := p.pins[seat]; !pinned && p.usual[seat] == "" {
			return out
		}
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
			for _, said := range crewSaid(a.crewMark(tokens.GFailed)+" "+k.refuseWhy, width, a.pal.warn) {
				rows = append(rows, said)
				owner = append(owner, -1)
			}
		}
	}
	if p.refusal != "" {
		for _, said := range crewSaid(a.crewMark(tokens.GFailed)+" "+p.refusal, width, a.pal.bad) {
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
		lead = a.pal.warn(a.crewMark(tokens.GRecommended)) + " "
	}
	ink := a.pal.ink
	if !offer.Allowed {
		ink = a.pal.dim
	}
	text := lead + ink(name)
	if current {
		text += " " + a.crewMark(tokens.GPinned)
	}
	facts := []string{crewPerM(offer.Model)}
	if len(offer.Routes) > 0 {
		provider := offer.Routes[0].Provider
		if extra := len(offer.Routes) - 1; extra > 0 {
			provider += " +" + strconv.Itoa(extra)
		}
		facts = append(facts, provider)
	}
	switch {
	case !offer.Served:
		facts = append(facts, crewProviderOff)
	case !offer.Allowed:
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
			mark = a.pal.ink(a.crewMark(tokens.GSettled)) + " "
		}
		text := mark + a.pal.ink(line.offer.Model.ID) + a.pal.dim("  "+crewPerM(line.offer.Model))
		if !line.offer.Served {
			text += a.pal.dim(" · " + crewProviderOff)
		}
		rows = append(rows, a.crewRowLine(text, at == c.cursor, hover == len(rows), false, width))
		owner = append(owner, at)
	}
	if p.refusal != "" {
		rows = append(rows, a.pal.bad(fit("  "+a.crewMark(tokens.GFailed)+" "+p.refusal, width)))
		owner = append(owner, -1)
	}
	return rows, owner
}

// crewKeyLines is `?`: every key the panel takes, and the pointer's share.
var crewKeyLines = [][2]string{
	{"↑↓  j k", "move"},
	{"enter", "change the row · pick · tick"},
	{"←→", "walk the models row · the providers · a model's routes"},
	{"space", "turn the provider under the cursor off or on"},
	{"0-9", "type the daily cap, or a price ceiling"},
	{"tab", "the cap row's other limit · the other price ceiling"},
	{"type", "filter a list"},
	{"z", "undo the last change, for a few seconds"},
	{"esc", "back one level · close"},
	{"click", "a row is enter · ‹ › walk the models row · a provider toggles"},
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
		if at == crewProviders && mark.index == p.chipLine && !p.folded {
			// A PRESS ON A CHIP IS SPACE ON IT; a press on the row's name or its
			// empty end is enter on the row, the list, as on every other row.
			for i, span := range p.chips {
				if span.holds(x) {
					p.chip = i
					return a.crewSpaceChip()
				}
			}
		}
		return a.crewEnter()
	case crewPicking:
		p.pick.cursor = at
		return a.crewPickEnter()
	case crewChecking:
		p.check.cursor = at
		return a.crewTick()
	case crewProviding:
		p.prov = at
		return a.crewProvToggle()
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
	case crewProviding:
		p.prov = moveCursor(p.prov, delta, p.crewProvLines())
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
	p.provSaved = -1
	p.cursor, p.undo = stop, &before
	p.saved, p.savedAt = stop, a.now()
	a.touch()
	return surfaceTick(crewUndoFor, func(time.Time) tea.Msg { return crewUndoMsg{} })
}
