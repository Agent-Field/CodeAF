package homes

import "time"

// The input contract.
//
// The wiring fills a [State] whenever the facts move — a poll lands, a charter
// changes status, a service dies, a belief is retracted — and hands it to
// [Source.SetState] and [View.Render]. It is not consulted per keystroke and
// never per frame: everything a render needs is precomputed at that moment.
// This is internal/tui2/modelui's Catalog contract, verbatim, for the same
// reason it states — a surface that could reach a store would be a surface that
// reads one per keystroke.
//
// WHAT THIS PACKAGE NEEDS FROM ELSEWHERE. Every entry names the read that fills
// it. Reads that exist today are named with their package-qualified signature;
// reads that DO NOT EXIST are marked GAP and say precisely what is missing and
// where it would have to come from. Nothing here invents a store write.
//
//	── Notebook ──────────────────────────────────────────────────────────────
//	Beliefs        (*store.Store).Facts(limit) for the whole notebook including
//	               retracted rows, or RecentFacts(limit) for active only. The
//	               old chat reached it through (*command.Commander).Notebook(10)
//	               and (*command.Commander).SearchNotebook(terms, limit); the
//	               second is the read behind [Notebook.Query].
//	Belief.Uses    store.Fact.Uses — retrieval telemetry, rebuildable, and the
//	               field docs say nothing ranks on it. Shown as trivia, dropped
//	               first under width pressure.
//	Belief.Trust   store.CredibilityWord(store.Fact.Confidence). Confidence is a
//	               projection applied on every read path (factsWhere →
//	               applyFactCredibility), so it is always populated.
//	Belief.Evidence
//	               (*command.Commander).NotebookEvidence(seq) — the refs behind
//	               a belief, already resolved to node ids or "#seq".
//	Notebook.Total GAP. Every list read is a WINDOW (Facts(limit),
//	               RecentFacts(limit)); nothing counts the notebook. The old
//	               self page worked around it with a scan ceiling of 500 and the
//	               string "500+". The read that would settle it is a
//	               `func (s *Store) FactCount(status string) (int, error)` — one
//	               `SELECT COUNT(*) FROM facts` with the same status filter
//	               ActiveFacts already writes — in internal/store/facts.go.
//	               Until it lands, fill Total with the window length and set
//	               [Notebook.AtCeiling] so the count renders as `500+` rather
//	               than as a number that is quietly wrong.
//
//	── Self ──────────────────────────────────────────────────────────────────
//	RouteCrafts       (*command.Commander).Crafts() → []tui.CraftSummary, and
//	                  CraftDetail(name) for the drill. NOTE: those types live in
//	                  internal/tui today (self_drill.go), which the v2 surface
//	                  may not import. The wiring maps them into [Item]; the
//	                  types themselves want re-homing into internal/craft or
//	                  internal/command, which is a MOVE and not a new read.
//	RouteCompetence   (*store.Store).CompetenceMap(store.CompetenceOptions{Now:…})
//	                  → store.CompetenceMap{Scopes []ScopeCompetence}.
//	RouteBeliefs      the same read as the notebook home. The two are ONE
//	                  collection seen from two rooms, and this package models it
//	                  once: fill [Self]'s beliefs route from [Notebook].
//	RouteSkills       (*store.Store).SkillFacts("", limit).
//	RouteWatches      the same read as the standing home — see below.
//	RouteServices     the same read as the services home — see below.
//	RoutePractice     (*store.Store).SelfReceipts(since) → []store.SelfReceipt,
//	                  plus the live practice roots, which are NOT a read: the old
//	                  page scanned Snapshot()/ActiveSnapshot() for nodes whose
//	                  Parent is store.RootID and Group is store.PracticeGroup.
//	RouteDials        (*config.Settings).Groups() filtered to
//	                  config.CategoryLearning — the two rows being
//	                  config.KeyDemandShare and config.KeyProposeSkills. It is a
//	                  settings projection and not a store read at all, which is
//	                  why Dials carries no count.
//	Today.SpendUSD    (*store.Store).SelfSpendToday().
//	Today.Learned     derived: the distinct union of SelfReceipt.FactIDs and
//	                  SkillIDs over today's receipts. A receipt may name a fact
//	                  twice and the person is being told how much was learned,
//	                  not how many rows were written.
//	Today.Practiced   derived from the practice roots' StartedAt/FinishedAt.
//
//	── Standing ──────────────────────────────────────────────────────────────
//	Charters       (*store.Store).Charters(statuses…) → []store.Charter.
//	Charter.Cadence store.WatchSpec.Spoken() — the humane spelling; String() is
//	               the machine one and never reaches a person.
//	Charter.NextDue store.Charter.NextDue.
//	Charter.Greens store.Charter.GreenFirings against the probation ladder;
//	               store.Charter.Autonomy says which side of it the charter is
//	               on.
//	Charter.LastLine store.Charter.LastCheckLine — the sentence the sentinel
//	               wrote when it looked and found nothing. It is the honest
//	               status line for a charter that has never fired.
//	Charter.LastFired
//	               (*store.Store).CharterLastFired(id).
//	Charter.Today  (*store.Store).FiringsToday(id, now).
//	Charter.CostPerRun
//	               store.Charter.Rails().EstimatedCostUSD, or the measured
//	               figure from (*store.Store).MeasuredCostPerRun(). 12.9.2 is
//	               binding here: money is COMPUTED, never spoken into existence,
//	               and an unmeasured rate says it is unmeasured. Set HasCost
//	               false rather than passing a zero that renders as free.
//	Charter.Verbs  the command registry (5.22), resolved by the wiring. The
//	               canonical ids are [CharterVerbs]. NOTE that "probation" has
//	               no registry entry and no old-TUI door — store.CommandCharter\
//	               Probation is reachable only conversationally, from
//	               internal/head. 5.24 names it as one of the four affordances,
//	               so the registry entry is a GAP: internal/registry/catalog.go
//	               needs an entry whose Journal maps to that command kind.
//
//	── Services ──────────────────────────────────────────────────────────────
//	Services       (*store.Store).ActiveServices() → []store.Service. Note it
//	               excludes stopped services by design; a room for a service the
//	               user just stopped comes from
//	               (*store.Store).SearchRestartableServices(reference) or
//	               (*store.Store).Service(id).
//	Service.Health store.ServiceHealth.Suffix() — the compact rail spelling
//	               that store already owns.
//	Service.Log    NOT A STORE READ. The old chat read the tail off disk:
//	               internal/tui/services.go readServiceLogTail opens
//	               store.Service.LogPath, ReadAt's the last 32KiB, strips ANSI
//	               and keeps the last ten lines. That function is in a package
//	               this surface may not import, so the wiring owns the read; the
//	               package hands the lines over already stripped, because a log
//	               line is untrusted bytes and [View] sanitises but does not
//	               un-escape.
//	Service.Verbs  the registry again; canonical ids are [ServiceVerbs], which
//	               map to store.CommandServiceStop / ServiceRestart /
//	               ServiceAutoRestart.
//
//	── Spend ─────────────────────────────────────────────────────────────────
//	Spend.SpentUSD (*store.Store).SpendToday(), which is already the header
//	               figure.
//	Spend.LimitUSD store.DailyRail.Ceiling from (*store.Store).DailyRailToday(
//	               base), where base is config's daily budget. DailyRail also
//	               carries Unlimited and Reached, which is why [SpendState] has
//	               both flags rather than a sentinel number.
//	Spend edits    (*command.Commander).Budget([]string{…}) is the existing
//	               door: "default <amt>" writes the config default and "<amt>"
//	               raises today's rail. This package renders the editor and
//	               hands back a [SpendResult]; it never writes.
//
//	── Voice ─────────────────────────────────────────────────────────────────
//	Mic            GAP, and the honest kind: there is no dictation subsystem in
//	               this tree at all — no recogniser, no audio seam, no setting.
//	               [Mic] renders the AFFORDANCE and its four states so the place
//	               line has its slot and the one-way steer mark (5.24) exists
//	               where it is specified; [MicUnavailable] is the state a
//	               product with no recogniser is honestly in, and it is the zero
//	               value for exactly that reason.
//
//	── Everywhere ────────────────────────────────────────────────────────────
//	Visitor        5.24's multi-window rule: a window without the resident lease
//	               renders every room READ-ONLY. The lease is
//	               internal/resident's; the chat surface already carries a
//	               Residents seam. The sentence is the wiring's own words, the
//	               way modelui's Disabled is, because only the wiring knows why.
//
// If a read ever goes missing, the fix is a field here that the wiring fills,
// not a call from this package into a store.

// State is everything the four homes draw from.
//
// The zero value is meaningful and renders honestly: the group collapsed, four
// rooms with nothing in them, and each room's own empty line teaching what would
// put something there (5.22 rule 6). That is exactly what a fresh machine looks
// like, so an unconfigured product and an unwired surface show the same true
// thing.
type State struct {
	// Expanded is whether the home group is open. It is the RAIL's state, not
	// this package's — 5.24 says the group is collapsed by default so live work
	// keeps the top, and who remembers that is the model that owns the cursor.
	Expanded bool

	// Visitor is why every room here is read-only, in the wiring's own words:
	// "visitor window — only the resident may act" (5.24). Empty means the
	// window holds the lease. When set it disables every verb and is shown on
	// the affordance strip, so the affordance never lies (5.20 rule 3).
	Visitor string

	Notebook Notebook
	Self     Self
	Standing Standing
	Services Services

	// Now is the clock the age and cadence readings are taken against. A zero
	// Now drops every relative time rather than dating the frame from the Unix
	// epoch — 10.2.8: a number that has not arrived and a number that is zero
	// are different facts, and the missing one renders as nothing.
	Now time.Time
}

// Notebook is the belief room's facts.
type Notebook struct {
	// Beliefs is the window, in display order — newest first is what every
	// read behind it already returns.
	Beliefs []Belief
	// Total is how many beliefs there are. See the GAP note above: with no
	// count read, the wiring passes the window length and sets AtCeiling.
	Total int
	// AtCeiling says Total is a floor rather than a count, and renders `N+`.
	AtCeiling bool
	// Query is the filter the list was narrowed by, echoed in the empty state
	// so "nothing learned about X" names the X (5.22 rule 6).
	Query string
}

// Belief is one row of the notebook — a projection of store.Fact, not the type
// itself, for modelui's reason: a surface that accepted the store's type would
// be one refactor away from calling the store.
type Belief struct {
	// ID is the durable identity (store.Fact.Seq, as a string). Never rendered
	// (5.14).
	ID string
	// Body is the belief, in aforge's words.
	Body string
	// Scope is what it is about: `user`, `env`, `repo:/p`. Rendered verbatim —
	// it is a key the person can use in a search, not prose to be prettied.
	Scope string
	// Kind is the fact kind's word (`preference`, `lesson`, `skill`).
	Kind string
	// Trust is store.CredibilityWord's reading — "strong", "steady",
	// "tentative". Empty when the belief has no channel and therefore no
	// credibility to report, which is a different fact from low confidence.
	Trust string
	// Learned is when it was written. Zero drops the age cell.
	Learned time.Time
	// Retired marks a quarantined belief — the head's `forget` door (12.8.11)
	// and the notebook's own retract action. It is drawn dim and struck
	// through rather than removed, because a belief that vanishes is a belief
	// nobody can tell you let go of.
	Retired bool
	// Provisional marks a candidate or superseded row: known, not yet load
	// bearing. Same dim tier, no strike.
	Provisional bool
	// Uses is retrieval telemetry. HasUses separates "never retrieved" from
	// "not counted".
	Uses    int
	HasUses bool
	// Evidence names the work that taught it, already resolved by the wiring.
	Evidence []string
}

// Self is the self room: the eight routes as rail rows, plus the one line that
// is always true above them.
type Self struct {
	// Routes are the wiring's rows. A route the wiring does not supply still
	// gets a row — [Self.Route] returns the honest empty one — so a partially
	// wired surface shows eight rooms with nothing in them rather than a short
	// list that looks complete.
	Routes []Route
	// Today is the day line (5.24's "arrival brief" instinct, applied to the
	// one room whose whole subject is what happened while nobody watched).
	Today Today
}

// Route is one of the eight self routes with its contents.
type Route struct {
	// ID is which route. A route outside the eight is dropped.
	ID RouteID
	// Items are the route's rows, in display order.
	Items []Item
	// Count is how many there are in total; Items may be a window of it.
	// HasCount is false for Practice and Dials, which are states rather than
	// collections — a number in front of them would be a number about nothing.
	Count    int
	HasCount bool
	// AtCeiling says Count is a floor (see [Notebook.AtCeiling]).
	AtCeiling bool
}

// Item is one row inside a self route, and one shape for all eight. The routes
// are genuinely different collections — a craft, a scope's competence, a
// belief, a skill, a charter, a service, a practice receipt, a settings dial —
// and they agree about exactly this much: a name, a line under it, a state, and
// whether it wants a human.
type Item struct {
	// ID is the row's durable identity. Never rendered.
	ID string
	// Name is the row's word.
	Name string
	// Detail is the one dim line under it — the wiring's sentence about this
	// row. Never invented here: an empty Detail draws no line, because a
	// made-up detail is worse than a missing one (5.9's rule for a status).
	Detail string
	// Note is the right-hand cell: an age, a count, a reading. It claims its
	// room before the name does, so a long name never pushes a number off the
	// row (5.21's width-stability law).
	Note string
	// Life is the row's state where it has one. A collection with no lifecycle
	// (a belief, a dial) leaves it [LifeSettled]'s neighbour [LifeQueued] —
	// see [Item.Inert].
	Life Lifecycle
	// Inert marks a row with no lifecycle at all, so [Item.Attention] draws
	// chrome rather than claiming the row is queued to do something.
	Inert bool
	// Needs marks a row waiting on a human — a proposed charter, an unsettled
	// belief. It is the amber `?` and it outranks everything (5.9).
	Needs bool
	// Path is the artifact this row points at (12.5.1): a craft's directory, a
	// skill's teaching, a service's log. Rendered with the middle ellipsis a
	// path deserves.
	Path string
}

// Today is the self room's day line: what today cost, what it taught, and how
// much of it was practice.
type Today struct {
	SpendUSD float64
	HasSpend bool
	Learned  int
	// Practiced is idle time spent on self-directed work. Zero draws nothing.
	Practiced time.Duration
}

// Standing is the charter room.
type Standing struct {
	Charters []Charter
	// TenureAt is how many consecutive green firings promote a probationary
	// charter — the store's ladder, passed in rather than hardcoded, because a
	// surface that owned the number would be a second place it lives.
	TenureAt int
}

// CharterState is a charter's standing, mirroring store.CharterStatus one for
// one so the wiring is a switch and not a judgement:
//
//	draft, proposed → CharterProposed  (the store rewrites draft on read)
//	active          → CharterActive
//	paused          → CharterPaused
//	retired         → CharterRetired
//
// store.CharterAutonomy is a SECOND axis and stays one: a charter is active AND
// on probation, and collapsing the two would hide which.
type CharterState uint8

const (
	// CharterProposed is waiting for a human to stand it up. It is the one
	// charter state that is an open question (5.9), and it draws amber.
	CharterProposed CharterState = iota
	// CharterActive is standing and checking.
	CharterActive
	// CharterPaused is standing and not checking.
	CharterPaused
	// CharterRetired is over.
	CharterRetired
)

// String names the state.
func (c CharterState) String() string {
	switch c {
	case CharterProposed:
		return "proposed"
	case CharterActive:
		return "active"
	case CharterPaused:
		return "paused"
	case CharterRetired:
		return "retired"
	}
	return "invalid"
}

// Charter is one standing promise.
type Charter struct {
	// ID is the charter id — the target a verb's command carries. Never
	// rendered.
	ID string
	// Invariant is the promise in words, and it is the card's name.
	Invariant string
	// Cadence is store.WatchSpec.Spoken() — "every weekday at 9am", not a cron
	// string. 5.13: chrome speaks the product's language.
	Cadence string
	State   CharterState
	// Probation says the charter is still earning tenure; Greens is how far
	// along it is, against [Standing.TenureAt].
	Probation bool
	Greens    int
	// LastLine is what the sentinel wrote the last time it looked and found
	// nothing — the honest status for a charter that has never fired.
	LastLine string
	// LastFired and NextDue drive the two time cells. Zero drops each.
	LastFired time.Time
	NextDue   time.Time
	// Today is how many times it has fired since local midnight.
	Today int
	// CostPerRun is dollars per firing. HasCost false says UNMEASURED, and the
	// row says so in words (12.9.2) rather than rendering $0.00, which reads as
	// free.
	CostPerRun float64
	HasCost    bool
	// Verbs are the affordances, from the registry (5.22). An empty slice
	// draws no strip; this package never substitutes verbs of its own.
	Verbs []Verb
}

// Services is the service room.
type Services struct {
	Services []Service
}

// Service is one process aforge keeps alive.
type Service struct {
	// ID is the command target. Never rendered.
	ID string
	// Name is the service's word.
	Name string
	// Command is what is being kept alive. It is a command line and renders
	// verbatim, tail-truncated — unlike a path, its head is the information.
	Command string
	// Health is store.ServiceHealth.Suffix() — ":8080", "GET /healthz".
	Health string
	// Life is the run state: running → [LifeWorking], failed → [LifeFailed],
	// stopped → [LifeCancelled], resting → [LifePaused].
	Life Lifecycle
	// AutoRestart says the supervisor brings it back; Restarts is how often it
	// already has. A high restart count under a running service is the honest
	// way to show a flapping process without inventing a health verdict.
	AutoRestart bool
	Restarts    int
	// Since is when the current incarnation came up. Zero drops the cell.
	Since time.Time
	// LogPath is where the tail comes from (12.5.1: the artifact is on disk and
	// the room points at it).
	LogPath string
	// Log is the tail, OLDEST FIRST, already ANSI-stripped by the wiring.
	Log []string
	// Dropped is how many lines the tail left behind. It is not decoration:
	// 12.5.2 says a thing that was cut renders visibly cut, and a ten-line
	// window onto a ten-thousand-line log that does not say so is the same lie
	// of omission at a smaller scale.
	Dropped int
	// Verbs are the affordances, from the registry.
	Verbs []Verb
}

// Verb is one affordance on a focused charter or service, resolved by the
// wiring from the command registry (5.22). This package draws it and never
// invents one: a second list of verbs beside the registry is exactly the drift
// the registry exists to forbid.
type Verb struct {
	// ID is the registry entry id, carried back when the verb is chosen.
	ID string
	// Label is the verb phrase — registry.Entry.Verb.
	Label string
	// Key is the accelerator the registry assigned, drawn dim before the label
	// the way the action strip of 5.22 rule 1 draws it. Empty draws no key.
	Key string
	// Disabled is why this verb cannot be used here, in the wiring's words. The
	// verb still renders, spends its row on the reason, and refuses (5.20 rule
	// 3). [State.Visitor] is the wiring's cue to set it on every verb.
	Disabled string
}

// CharterVerbs are the four affordances 5.24 names for a focused charter, in
// draw order. They are IDS TO RESOLVE, not entries: the wiring looks each one
// up in the registry and drops what the registry does not carry, so a verb can
// never appear here without a key, a description and a journal mapping behind
// it.
//
// "probation" is in the list and is currently a GAP — see state.go's read map.
var CharterVerbs = []string{"pause", "probation", "cadence", "retire"}

// ServiceVerbs are the three 5.24 names for a service room, in draw order.
var ServiceVerbs = []string{"stop", "restart", "auto-restart"}
