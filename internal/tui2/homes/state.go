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
//	Belief.Class   the row's own read: kind `playbook` is a playbook, kind
//	               `trait` is a trait (store.TraitMeasurement decoded for
//	               [Belief.Samples]), a preference under store.TasteScopePrefix
//	               is a taste rule ((*store.Store).TasteRules, facts.go:990),
//	               everything else is plain.
//	Belief.Channel store.Fact.Channel, mapped one for one.
//
//	── Know-how ──────────────────────────────────────────────────────────────
//	Crafts         (*command.Commander).Crafts() for the list and CraftDetail
//	               (name) for the drill (internal/command/craft.go:53,79).
//	Craft.Proved   the SURVIVAL RECORD, which is a trait rather than a table:
//	               (*store.Store).Trait(resident.CraftSurvivalKey(name)) decodes
//	               a resident.CraftSurvival{For,Against,LastCost}
//	               (internal/resident/craftmind.go:314). HasRecord false is what
//	               "draft, never run" is drawn from — a workflow with no trait
//	               has not run, which is not the same fact as having lost.
//	Craft.Version  len(CraftDetail.History): the commits behind the file.
//	Skills         (*store.Store).SkillFacts("", limit) — store.Fact.Artifact is
//	               the installed path, .Uses the reach count, .Status/.StatusNote
//	               the retirement.
//
//	── Practice ──────────────────────────────────────────────────────────────
//	Questions      (*store.Store).Questions("", limit) (internal/store/practice
//	               .go:141); the lifecycle is store.Fact.Status.
//	Question.Runs  (*store.Store).QuestionPractices(seq) (practice.go:312).
//	Question.Cost  the self receipts a practice round wrote: a round carries
//	               TargetKind "fact" and TargetID the question's seq
//	               (self_receipt.go:243), so the money and the surprise delta
//	               are read from SelfReceipts without a new join.
//	Competence     (*store.Store).CompetenceMap(store.CompetenceOptions{Now:…})
//	               (competence.go:151): strongest is the best-measured
//	               CompetenceStrong scope, frontier the CompetenceFrontier one.
//	Today.SpendUSD (*store.Store).SelfSpendToday() (self_receipt.go:116).
//	Today.Practiced
//	               the practice roots' wall clock. There is no read for it —
//	               the old page scanned a whole Snapshot — so this wave adds one
//	               narrow query, (*store.Store).PracticedToday(now), in a new
//	               store file rather than making a surface walk the graph.
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
//	                  config.CategoryPractice — whatever rows that group holds,
//	                  never a list named here. It is a settings projection and
//	                  not a store read at all, which is why Dials carries no
//	                  count. (The two rows this used to name, practice_demand_pct
//	                  and propose_new_skills, were deleted with the loops that
//	                  never read them.)
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
//	Mic.State      internal/voice is SHIPPED — microphone capture and OpenRouter
//	               speech-to-text behind voice.Recorder and voice.Transcriber,
//	               constructed in cmd/aforge/chat.go and handed over by
//	               (*command.Commander).VoiceRecorder / .VoiceTranscriber. The
//	               state machine that drives them is internal/tui/voice.go's
//	               idle / starting / recording / finalizing, and [MicState] maps
//	               onto it one for one. A nil seam is [MicUnavailable].
//	Mic.Target     the SELECTED rail row's rail.ComposerMode. This is the field
//	               with no source in the old surface at all, and it is the one
//	               5.24 legislates: nothing in internal/tui/voice.go knows
//	               whether the draft it merges into is a chat or a steer line.
//	Mic.Elapsed    the wiring's own clock over the recording.
//	               NOT a gap, but worth stating: voice.Transcript.Usage.Cost is
//	               journaled by the old surface (recordVoiceUsage) and belongs
//	               in the spend reading, not on this cell.
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
	Knowhow  Knowhow
	Practice Practice

	// Self, Standing and Services are the RAIL's rooms and no longer bands on
	// the notebook page. The split is the wave's own decision (notebook-split.md
	// §1: doing versus knowing) — a standing promise and a live process are
	// things the resident is DOING, and they moved to the work page. The state
	// stays here because the four rooms of 5.24 are still rail scopes and
	// [View] still draws them; what left is the page's claim on them.
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
	// Class is which KIND OF ROW this is, as the reader meets it: a plain
	// belief, a taste rule, a measured trait, a playbook. It is a small enum
	// rather than a second string because the page words each class itself —
	// "you keep correcting: …" is the product's sentence, not the store's — and
	// because a trait is read-only (notebook-split.md §3) and read-only is a
	// decision this package must be able to make without parsing prose.
	Class BeliefClass
	// Channel is how the belief entered the notebook, for the detail page's one
	// honest sentence about provenance.
	Channel BeliefChannel
	// Status is the row's own lifecycle word where its class has one — a taste
	// rule is `forming` while it is still a candidate and `kept` once it stands.
	// Empty for every class that has no lifecycle, which is most of them.
	Status string
	// Samples is how many measurements are behind a trait. HasSamples separates
	// "measured zero times", which cannot happen, from "not counted".
	Samples    int
	HasSamples bool
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
	// LastUsed is when it was last recalled into a model's context. Zero drops
	// the cell, the way every other missing instant does.
	LastUsed time.Time
	// Evidence names the work that taught it, already resolved by the wiring
	// into something a person can read and a host can open.
	Evidence []Evidence
	// Verbs are the belief's affordances, from the command registry (5.22),
	// resolved by the wiring exactly as a charter's and a service's are. The
	// canonical ids are [BeliefVerbs].
	//
	// It is the one field the full page added, and it is a seam rather than a
	// read: the notebook always had a `retract` door (the old overlay's
	// confirmation) and an `edit` one (the head's own correction path), and
	// neither had a registry entry a surface could draw. Empty is honest — see
	// [BeliefVerbs] for what the page draws when the registry has not caught up.
	Verbs []Verb
}

// Evidence is one piece of work that taught a belief.
//
// It is a NAME and a handle rather than the handle alone, which is the whole
// point of the type: the first live build drew `taught by  task-5381` and a raw
// id is banned on every surface (5.14, §19). The wiring resolves the work's own
// title; the page draws the title and keeps the handle behind it as a door, so
// the reference stays reachable instead of merely being spelled politely.
type Evidence struct {
	// Name is the work's title, in its own words. Empty is honest — the wiring
	// could not resolve one — and the page says "a past task" with its age
	// rather than falling back to the handle.
	Name string
	// Room is what a host opens to show the work. Never rendered (5.14). Empty
	// means the reference is not a room at all: an earlier note in the notebook
	// rather than a task that ran.
	Room string
	// When the work happened. Zero drops the age cell, like every other missing
	// instant here.
	When time.Time
}

// BeliefClass is which kind of row a belief is, as the reader meets it on the
// page. It is not the store's fact kind — [Belief.Kind] carries that — because
// two of these classes are lifecycles laid over ORDINARY preference and trait
// facts (store's own words for taste and traits), and the reader meets them as
// different kinds of sentence.
type BeliefClass uint8

const (
	// BeliefPlain is a belief in aforge's own words, about a scope. Its receipt
	// leads with that scope, because "what is this about" is the only thing a
	// plain belief needs said for it.
	BeliefPlain BeliefClass = iota
	// BeliefTaste is a rule the user keeps correcting toward.
	BeliefTaste
	// BeliefTrait is a MEASUREMENT about the user. Read-only, always: disputing
	// a measurement is a conversation, not a form (notebook-split.md §3).
	BeliefTrait
	// BeliefPlaybook is a scoped strategy earned from earlier work.
	BeliefPlaybook
)

// Word is the class's honest word in a receipt, or "" for a plain belief, whose
// receipt leads with its scope instead.
func (c BeliefClass) Word() string {
	switch c {
	case BeliefTaste:
		return "taste"
	case BeliefTrait:
		return "trait"
	case BeliefPlaybook:
		return "playbook"
	}
	return ""
}

// Lead is the sentence the page puts in FRONT of the body for a class whose
// body is not a statement on its own. A taste rule's body is the correction
// ("shorter commit lines") and a trait's is the measurement ("you usually
// accept first drafts"); neither reads as a belief without the words that say
// what kind of observation it is.
//
// It lives here rather than at the render site for the reason [Home.Blurb] and
// [RouteID.Explain] do: a sentence that lives at a render site is a sentence
// that gets two spellings.
func (c BeliefClass) Lead() string {
	switch c {
	case BeliefTaste:
		return "you keep correcting: "
	case BeliefTrait:
		return "measured: "
	}
	return ""
}

// ReadOnly reports a class no verb may act on. A trait is measured, not held.
func (c BeliefClass) ReadOnly() bool { return c == BeliefTrait }

// BeliefChannel is how a belief entered the notebook, in the four ways the
// store records (store.FactChannel). The page spends it as ONE sentence on the
// detail page and never as a chip on a row: how a thing was learned is
// provenance a reader asks for, not something they scan a list by.
type BeliefChannel uint8

const (
	// ChannelUnknown is a belief with no channel recorded, and it says nothing
	// rather than guessing.
	ChannelUnknown BeliefChannel = iota
	// ChannelStated is the user's own words.
	ChannelStated
	// ChannelInferred is aforge's reading of what it saw.
	ChannelInferred
	// ChannelDistilled is consolidation's: several observations become one line.
	ChannelDistilled
	// ChannelTrial is a question that ran until the evidence settled it.
	ChannelTrial
)

// Word is the channel as a sentence a person reads.
func (c BeliefChannel) Word() string {
	switch c {
	case ChannelStated:
		return "you said it"
	case ChannelInferred:
		return "inferred from your work"
	case ChannelDistilled:
		return "distilled from several observations"
	case ChannelTrial:
		return "settled by trials"
	}
	return ""
}

// Knowhow is the know-how section: the shapes aforge learned to repeat
// (crafts, chip-tagged `workflow`) and the tools it forged (skills, `tool`).
//
// They are ONE section and two lists rather than two sections, because the
// question a reader brings to this band is "what can you already do", and the
// difference between a workflow and an executable is an implementation detail
// the chip word carries in full.
type Knowhow struct {
	Crafts []Craft
	Skills []Skill
}

// Craft is one learned workflow — YAML in a git repository beside the brain,
// compiled into a graph subtree when it runs.
type Craft struct {
	// ID is the durable handle a verb carries. Never rendered (5.14).
	ID string
	// Name is the workflow's own name, off disk.
	Name string
	// Description is the one line the file carries about itself.
	Description string
	// Proved and Against are the survival record: runs that settled, runs that
	// failed or were cancelled. HasRecord false means it has NEVER RUN, which
	// is a different fact from having run and lost — a draft is not a failure,
	// and the row says "draft, never run" rather than "proved 0".
	Proved    int
	Against   int
	HasRecord bool
	// CostPerRun is what the last clean run actually spent. HasCost false says
	// unmeasured, and §16's MONEY rule is why it is a flag rather than a zero.
	CostPerRun float64
	HasCost    bool
	// Version is how many commits this workflow has, which is what `vN` counts.
	// Zero drops the cell rather than claiming a v0.
	Version int
	// Updated is when the newest commit landed.
	Updated time.Time
	// Dir is the craft repository on disk (12.5.1: the artifact is real and the
	// page points at it).
	Dir string
	// Ceilings are the bounds a run will actually obey.
	Ceilings CraftCeilings
	// Steps are the workflow's leaves in FILE ORDER, which is the order the
	// author wrote and the only order that reads as a procedure.
	Steps []CraftStep
	// History is the version list, NEWEST FIRST (git log's own order).
	History []CraftVersion
	// Verbs are the affordances from the registry. Canonical ids are
	// [CraftVerbs]; empty is honest — see that variable for what is drawn.
	Verbs []Verb
}

// CraftCeilings are one workflow's bounds. HasCost distinguishes an unbounded
// workflow from a free one.
type CraftCeilings struct {
	CostUSD   float64
	HasCost   bool
	WallClock time.Duration
}

// CraftStep is one leaf of a workflow as the detail page reads it.
type CraftStep struct {
	// Brief is what the step does, in the author's words.
	Brief string
	// Needs names the steps that must land first, by their BRIEFS rather than
	// their ids — the wiring resolves them, because a step id is an id (5.14).
	Needs []string
	// Model is the humane model word where the step pins one; Skill is the tool
	// it reaches for; Verify says the step checks its own work.
	Model  string
	Skill  string
	Verify bool
}

// CraftVersion is one commit in a workflow's history.
type CraftVersion struct {
	// Version is the `vN` this commit is. It is counted from the FIRST commit,
	// so v1 is where the workflow was forged and vN is what runs today.
	Version int
	// Subject is the commit's own sentence.
	Subject string
	When    time.Time
}

// Skill is one forged executable: a procedure that ran, passed, and was kept.
type Skill struct {
	// ID is the durable handle. Never rendered.
	ID string
	// Name is the tool's word.
	Name string
	// Body is what it does, in aforge's words.
	Body string
	// Path is where it is installed (12.5.1).
	Path string
	// Uses is how often work has reached for it. HasUses separates "never used"
	// from "not counted".
	Uses    int
	HasUses bool
	// Learned is when it was kept. Zero drops the age cell.
	Learned time.Time
	// Retired says it is no longer offered to work; Note is why, in the store's
	// own words. A retired skill is drawn dim and struck, exactly as a let-go
	// belief is, because a tool that vanished is a tool nobody can tell you
	// stopped using.
	Retired bool
	Note    string
	// Verbs are the affordances. Canonical ids are [SkillVerbs].
	Verbs []Verb
}

// Practice is what aforge did with idle time: the questions it is drilling, how
// competent it has measured itself to be, and what the day cost.
type Practice struct {
	Questions  []Question
	Competence Competence
	// Today is the day receipt — spend, time practiced, things learned. It is
	// the same [Today] the self room draws and it lives here because the
	// practice band is where the design puts it (notebook-split.md §2).
	Today Today
}

// QuestionLife is a knowledge gap's lifecycle, mirroring the store's four
// question statuses one for one so the wiring is a switch and not a judgement.
type QuestionLife uint8

const (
	// QuestionAsked is open and not yet drilled.
	QuestionAsked QuestionLife = iota
	// QuestionPracticing is being drilled right now.
	QuestionPracticing
	// QuestionResolved is answered.
	QuestionResolved
	// QuestionRetired is one aforge stopped asking.
	QuestionRetired
)

// Word is the lifecycle in the product's language.
func (q QuestionLife) Word() string {
	switch q {
	case QuestionAsked:
		return "open"
	case QuestionPracticing:
		return "practicing"
	case QuestionResolved:
		return "resolved"
	case QuestionRetired:
		return "retired"
	}
	return ""
}

// life maps a question's lifecycle onto the rail's state vocabulary, so a
// question means the same thing in the glyph column as everything else does.
//
// Retired is [LifeCancelled] and not [LifeSettled] for the reason a retired
// charter is: it did not succeed, it stopped.
func (q QuestionLife) life() Lifecycle {
	switch q {
	case QuestionPracticing:
		return LifeWorking
	case QuestionResolved:
		return LifeSettled
	case QuestionRetired:
		return LifeCancelled
	}
	return LifeQueued
}

// Question is one durable knowledge gap and what drilling it has cost.
type Question struct {
	// ID is the durable handle. Never rendered.
	ID string
	// Body is the gap, in one line, as it was written.
	Body string
	// Scope is what it is about, rendered verbatim like a belief's.
	Scope string
	Life  QuestionLife
	// Runs is how many practice rounds it has had; Cost is what they came to.
	// HasCost false leaves the money off entirely (§16's MONEY rule).
	Runs    int
	CostUSD float64
	HasCost bool
	// Asked is when the gap was written down. Zero drops the age.
	Asked time.Time
	// Note is the store's own sentence about how it ended, where there is one.
	Note string
	// Attempts are the practice rounds, OLDEST FIRST, for the detail page.
	Attempts []Attempt
}

// Attempt is one practice round: what it cost, and how much less surprising the
// world became after it.
type Attempt struct {
	CostUSD float64
	HasCost bool
	// Delta is the surprise the round removed. HasDelta false means the round
	// has not landed an outcome yet, which is a different fact from a round
	// that taught nothing.
	Delta    float64
	HasDelta bool
	When     time.Time
}

// Competence is the one line aforge is allowed to say about how good it is: the
// scope it is strongest in and the one at its frontier. Both are measured
// (store.CompetenceMap) and either may be absent, which renders as absence.
type Competence struct {
	Strongest string
	Frontier  string
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

// CraftVerbs and SkillVerbs are the notebook's other two affordance sets, in
// draw order, spelled with the registry ids notebook-split.md §4 fixes across
// all three lanes of this wave. Like every other list here they are IDS TO
// RESOLVE: the wiring looks each one up and fills [Craft.Verbs] / [Skill.Verbs],
// and where it has not caught up the page draws the word with the id behind it
// and no key (see [BeliefVerbs] for why a word without a key is honest and an
// invented key is not).
var (
	CraftVerbs = []string{"craft.run", "craft.revert", "craft.retire"}
	SkillVerbs = []string{"skill.retire"}
)

// VerbWord is the word a registry id is drawn as: the last dotted segment,
// which is the verb the id was named for. `craft.revert` draws "revert".
//
// It exists because 5.14 forbids rendering an id and §4 fixes the ids — so a
// fallback strip that has no registry entry to read a label from still has to
// put a WORD on the screen, and the only word it is entitled to is the one the
// id already spells. It is not a translation table and must never grow into
// one: a verb whose word differs from its id's tail has a registry entry, and
// the entry's own label wins.
func VerbWord(id string) string {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			return id[i+1:]
		}
	}
	return id
}

// fallbackVerbs turns a list of registry ids into a drawable strip for a row
// the wiring has not filled. Every verb carries the reason it cannot be used
// where there is one, so a visitor window disables the notebook the same way it
// disables everything else.
func fallbackVerbs(ids []string, filled []Verb, visitor string) []Verb {
	if len(filled) > 0 {
		return filled
	}
	out := make([]Verb, 0, len(ids))
	for _, id := range ids {
		out = append(out, Verb{ID: id, Label: VerbWord(id), Disabled: visitor})
	}
	return out
}
