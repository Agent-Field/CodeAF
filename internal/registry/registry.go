// Package registry is the one command registry named in the chat-rebuild
// doc's 5.22 ("Discoverability: no typed-only actions"): every action lives
// on a visible object, and typing is an accelerator, never the only door. A
// single catalog of entries — id, verb phrase, description, scope predicate,
// key binding, slash alias, journal mapping — is the source every render
// surface (action strips, the summon palette, slash-filtered palette,
// contextual footer, chips, empty states) reads from, so discoverability holds
// by construction instead of by six surfaces staying in sync by hand.
//
// This package is pure data and query functions. It renders nothing and
// knows nothing about Bubble Tea, lipgloss, or any TUI type — the dependency
// runs one way, tui → registry, so a future web surface can read the same
// catalog without dragging a terminal renderer in behind it.
//
// Wave 1 seeds the catalog faithfully from what already exists — the slash
// table, the chat/task-page keybindings, and the head belt verbs that
// journal a real command — and adds no entry a live surface cannot already
// reach some other way. UI adoption (wiring the six surfaces to read from
// here instead of their own tables) is Waves 2–3.
package registry

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Scope is a predicate over where an entry applies, expressed as a bitmask
// rather than a function: composing two scopes is a bitwise OR, and testing
// membership is a single AND. That keeps the predicate data — a static field
// on the entry — instead of growing into a zoo of small closures, one per
// surface, that a query function would have to know how to call.
type Scope uint8

const (
	// ScopeThread is the room surface: the composer, the transcript, the
	// rail, and the header — everywhere the surface is showing while no task
	// is open for inspection.
	ScopeThread Scope = 1 << iota
	// ScopeNode is one task's activity view: its steer line and its feed.
	// Bare-letter accelerators here (c, r) are a different vocabulary than
	// ScopeThread's (v, y, Y) because the node view claims the keyboard
	// first and never falls through to the thread's own bindings.
	ScopeNode
	// ScopeTalk is reached only through the user's own words to the head —
	// the belt in internal/head/toolbelt.go — never through a key or a
	// slash alias. An entry scoped here still belongs in the one registry:
	// the capability-honesty surface (5.20 rule 3) has to be able to say
	// what the orchestrator can do, and a verb the belt carries is exactly
	// that, even with no accelerator of its own.
	ScopeTalk
	// ScopeItem is one thing the resident knows or is doing, open on its own
	// page: a belief, a way of working, a forged tool, a standing rule, a
	// service. The verbs here always act on THAT item — retire it, run it,
	// stop it — so the item is half the verb and the verb means nothing
	// without it.
	//
	// That is also why it is not in [ScopeAny]. Every other scope answers
	// "what can be done here"; this one answers "what can be done to this",
	// and an unscoped palette offering `retire` with nothing selected would be
	// a door that cannot open — the exact dishonesty 5.22 exists to remove. A
	// surface asks for ScopeItem when it has an item, and never otherwise.
	ScopeItem
)

// ScopeAny matches every entry a surface can offer with nothing in particular
// selected. It is the predicate a surface with no current focus (an empty
// state, the unscoped summon palette) queries with.
//
// It deliberately leaves [ScopeItem] out — see that scope's own doc. A caller
// that genuinely wants the whole catalog, item verbs included (a completeness
// test, a capability inventory), asks with [ScopeEvery].
const ScopeAny = ScopeThread | ScopeNode | ScopeTalk

// ScopeEvery is every scope this package defines. It is the honest spelling of
// "the whole catalog" for the handful of callers that mean it.
const ScopeEvery = ScopeAny | ScopeItem

// Has reports whether scope and other share at least one bit — the one test
// every query function in this package runs, so an entry scoped to more than
// one surface (alt+g closes the node view and toggles the rail either way)
// is found from any of them.
func (s Scope) Has(other Scope) bool { return s&other != 0 }

// Surface is the registry's second binding axis. [Scope] says WHERE an entry
// applies; Surface says HOW the surface holding the keyboard there is even able
// to SPELL an accelerator.
//
// They are different questions, and "toggle receipts" is the proof. The action
// applies in the room — ScopeThread — on both surfaces that have ever drawn a
// room. The v1 window routes it from a bare "v", because there the keyboard is
// not always the composer's. The v2 chat surface cannot: it is composer-first
// by design (5.15), a focused composer owns every printable key it is handed,
// and so it binds ctrl+r instead. One Key string cannot be true of both, and a
// registry that is the single source of truth (5.22) may not be false about
// either — a footer built from a row that names an unbindable key teaches a
// keystroke that does nothing, which is worse than teaching none.
//
// The alternatives were both worse. Re-keying the row moves the lie rather than
// removing it. A second entry for the same action puts the verb in the palette
// twice and makes the catalog drift from itself by construction, which is the
// exact failure mode one registry exists to prevent.
type Surface uint8

const (
	// SurfaceDefault is the surface an entry's [Entry.Key] is written for: one
	// where the keyboard is not permanently the composer's, so a bare letter
	// can be an accelerator. Zero value, so every query that does not ask
	// about surfaces keeps the answer it always gave.
	SurfaceDefault Surface = iota
	// SurfaceComposerFirst is a surface where a focused composer holds every
	// printable key, so only chords are bindable. A bare-letter entry has no
	// accelerator here — and saying so is the honest answer, not a gap: the
	// action still exists, still belongs in the palette and the `?` overlay,
	// and still reaches the user through the visible object it lives on
	// (5.22's law is that typing is the accelerator, never the only door).
	SurfaceComposerFirst
)

// String names the surface.
func (s Surface) String() string {
	switch s {
	case SurfaceDefault:
		return "default"
	case SurfaceComposerFirst:
		return "composer-first"
	}
	return "invalid"
}

// Journal names the durable record an entry's action leaves, when it leaves
// one. Most entries here are pure surface — open a picker, scroll, toggle a
// view — and carry a zero Journal; that is a legitimate, common value, not a
// gap the seeding missed. Kind and Tool are never both set: a verb either
// journals through the ordinary command path (Kind, resolved against
// store.CommandKind) or is reached only through the head belt (Tool, the
// belt's own tool name) — never both, because the belt's control tool itself
// journals a Kind, and that is the entry the seeded catalog uses.
type Journal struct {
	// Kind is the store command kind the action journals as. Empty when the
	// entry never journals (view toggles, local settings, reads).
	Kind store.CommandKind
	// Tool is the head belt tool name the entry resolves to, for the
	// entries reachable only through ScopeTalk. Empty for everything else.
	Tool string
}

// Empty reports whether the entry leaves no durable record at all.
func (j Journal) Empty() bool { return j.Kind == "" && j.Tool == "" }

// Entry is one row of the registry: everything a render surface needs to
// show the action and everything a query needs to find it. The lowercase
// fields are precomputed once at package init so a fuzzy match over the
// whole catalog never lowercases the same string twice per keystroke.
type Entry struct {
	// ID is unique across the whole catalog, checked by TestEntriesHaveUniqueIDs.
	ID string
	// Verb is the short verb phrase a strip or chip renders: "cancel",
	// "restart", "open self".
	Verb string
	// Description is the one-line sentence the palette and the `?` surface
	// show beside Verb.
	Description string
	// Scope is where the entry applies — see Scope's doc.
	Scope Scope
	// Key is the canonical live key binding, in Bubble Tea's own chord
	// spelling ("c", "alt+g", "ctrl+j"). Empty when the entry has none.
	// Where today's surface accepts more than one spelling for the same
	// chord (ctrl+t and alt+g both toggle the task list), Key names the one
	// the help screen leads with; the synonym is not a second registration.
	Key string
	// ChordKey is this entry's accelerator on a [SurfaceComposerFirst]
	// surface, where Key's bare letter cannot be bound at all. It is recorded
	// only when a surface really binds the chord — the registry names doors
	// that exist, never doors it would like to exist — so most rows leave it
	// empty, and a row whose Key is already a chord never needs one.
	//
	// Read it through [Entry.KeyOn] or [Entry.On] rather than directly: those
	// answer the question a render surface actually has, which is "what, if
	// anything, do I tell the user to press here."
	ChordKey string
	// Slash is the alias typed after "/" in the composer or a task's steer
	// line, without the leading slash. Empty when the entry has none.
	Slash string
	// Journal is what the action journals, when it journals anything.
	Journal Journal
	// Confirm is the question a destructive verb asks before it fires, in the
	// product's own voice and in one line. EMPTY IS THE COMMON CASE and means
	// the verb fires at once: pausing a rule, restarting a service and running
	// a way of working are all reversible by doing the opposite, and asking
	// about them would be ceremony.
	//
	// It is a sentence rather than a boolean because the only useful part of a
	// confirmation is what it says is about to be lost, and that is per-verb:
	// "retire this rule?" and "stop this service?" are not the same warning
	// with a different noun in it. A surface draws it however it draws
	// questions; this package renders nothing.
	//
	// Confirm and [Entry.Steer] are never both set. A verb that needs the
	// user's own words is answered by them typing, and a confirmation on top of
	// that would be asking twice about one sentence.
	Confirm string
	// Steer is the composer seed for a verb that carries an argument the
	// resident has to interpret — a new cadence, a corrected belief, the reason
	// a version is going back. Per the one-mouth law those verbs do not fire on
	// a click: they put the user's cursor in the composer with the sentence
	// half written, and the head reads what they finish.
	//
	// It carries exactly one %s, which is the item's own name as the user knows
	// it. Read it through [Entry.SteerFor] rather than formatting it by hand.
	Steer string

	lowerVerb, lowerDescription string
}

// SteerFor is this entry's composer seed for one named item, or "" when the
// verb is not a steering verb at all. A surface tests the empty answer to
// decide which of the two doors it is drawing — the seed, or the direct fire —
// so it never has to keep its own list of which verbs carry an argument.
func (e Entry) SteerFor(name string) string {
	if e.Steer == "" {
		return ""
	}
	name = strings.TrimSpace(name)
	if name == "" {
		// A seed with a hole in it is worse than no seed: the person would have
		// to delete the sentence before writing their own.
		return ""
	}
	return fmt.Sprintf(e.Steer, name)
}

// Destructive reports that this verb asks before it acts. It is the one-word
// form of "does this row carry a [Entry.Confirm]", named so a caller reads the
// question it is actually asking.
func (e Entry) Destructive() bool { return e.Confirm != "" }

// KeyOn is the accelerator this entry actually has on surface, which is the
// only form of the question a render surface can honestly ask. It returns "" —
// no accelerator here — rather than a key the surface cannot bind.
//
// On a [SurfaceComposerFirst] surface a recorded [Entry.ChordKey] wins, and a
// bare-letter Key resolves to nothing at all, because the composer will consume
// that letter as text. A chord Key ("ctrl+j", "alt+g") is bindable on every
// surface and is returned unchanged.
func (e Entry) KeyOn(surface Surface) string {
	if surface != SurfaceComposerFirst {
		return e.Key
	}
	if e.ChordKey != "" {
		return e.ChordKey
	}
	if barePrintable(e.Key) {
		return ""
	}
	return e.Key
}

// On projects the entry onto a surface: the same id, verb, description, scope
// and journal, with Key resolved by [Entry.KeyOn]. It reports false when the
// entry has no accelerator on that surface, so a caller building a key-shaped
// strip (the contextual footer of 5.22 rule 4) can drop the row with one test
// and never has to correct a key by hand — a surface that hand-corrects the
// registry's keys is a second source of truth wearing the first one's clothes.
func (e Entry) On(surface Surface) (Entry, bool) {
	key := e.KeyOn(surface)
	if key == "" {
		return Entry{}, false
	}
	e.Key, e.ChordKey = key, ""
	return e, true
}

// barePrintable reports whether key is a single printable character rather
// than a chord or a named key. Bubble Tea spells every named key as a word
// ("tab", "esc", "enter") and every chord with a "+", so one rune is exactly
// the case a composer swallows as text.
func barePrintable(key string) bool {
	return utf8.RuneCountInString(key) == 1
}

// entries is the seeded catalog, built once at package init from
// catalog.go's authored rows. Static package data: nothing here allocates on
// a query path, because there is nothing left for a query to build — every
// field, including the lowercase copies fuzzy matching reads, is already
// sitting in the slice.
var entries = buildEntries()

func buildEntries() []Entry {
	rows := seedRows()
	built := make([]Entry, len(rows))
	for index, row := range rows {
		row.lowerVerb = toLower(row.Verb)
		row.lowerDescription = toLower(row.Description)
		built[index] = row
	}
	return built
}

// All returns the whole catalog. Callers that only read (never mutate) may
// keep the slice; nothing in this package appends to it after init.
func All() []Entry { return entries }

// Len is the catalog size, for callers that only want the count (a
// capability-honesty line, a test) without walking the slice.
func Len() int { return len(entries) }

// ByID finds the one entry with this id. O(entries) worst case, no
// allocation — a linear scan over static data, which is cheap enough at this
// catalog's size that an index would cost more to keep correct than it saves.
func ByID(id string) (Entry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

// BySlash finds the entry whose Slash alias matches, case-insensitively —
// the composer lowercases nothing before matching today, so neither does
// this. Empty input never matches: an empty alias is not "no entry", it is
// every entry with no alias at all, and a caller asking BySlash("") almost
// always meant "nothing was typed yet."
func BySlash(alias string) (Entry, bool) {
	if alias == "" {
		return Entry{}, false
	}
	needle := toLower(alias)
	for _, entry := range entries {
		if entry.Slash != "" && toLower(entry.Slash) == needle {
			return entry, true
		}
	}
	return Entry{}, false
}

// ByKey finds the live entry bound to key within scope. Two entries may
// share a Key across disjoint scopes (c means nothing in ScopeThread, cancel
// in ScopeNode) — ByKey resolves that the same way the surface does, by
// asking for the scope it is currently in.
func ByKey(scope Scope, key string) (Entry, bool) {
	if key == "" {
		return Entry{}, false
	}
	for _, entry := range entries {
		if entry.Key == key && entry.Scope.Has(scope) {
			return entry, true
		}
	}
	return Entry{}, false
}

// ByKeyOn is [ByKey] asked from a surface: it finds the entry that key
// actually triggers in scope on that surface, matching against [Entry.KeyOn]
// rather than the raw Key field. A composer-first surface routing ctrl+r finds
// the receipts row; the same surface can never resolve "v", which is correct,
// because "v" there is a letter the user typed into a draft.
//
// The returned entry is already projected onto the surface, so its Key is the
// chord the caller matched and not the one the catalog was seeded with.
func ByKeyOn(scope Scope, surface Surface, key string) (Entry, bool) {
	if key == "" {
		return Entry{}, false
	}
	for _, entry := range entries {
		if !entry.Scope.Has(scope) {
			continue
		}
		if bound, ok := entry.On(surface); ok && bound.Key == key {
			return bound, true
		}
	}
	return Entry{}, false
}

// AppendScope appends every entry whose scope overlaps scope onto dst and
// returns the extended slice — the append-into-caller-buffer idiom the rest
// of the tree already uses for per-frame lists (see internal/tui/node.go's
// appendTraceBlocks), so a render surface that keeps its own buffer across
// frames pays no allocation once it has grown to size.
func AppendScope(dst []Entry, scope Scope) []Entry {
	for _, entry := range entries {
		if entry.Scope.Has(scope) {
			dst = append(dst, entry)
		}
	}
	return dst
}

// ForScope is AppendScope against a fresh slice, for a caller that has no
// buffer of its own to reuse (a one-off query, a test).
func ForScope(scope Scope) []Entry { return AppendScope(nil, scope) }

// toLower is ASCII-only on purpose: every id, verb, description, and alias
// seeded in catalog.go is plain ASCII, so a full-generality strings.ToLower
// would spend its Unicode table lookups on bytes that never need one. It
// still passes non-ASCII bytes through unchanged rather than mangling them,
// so a future entry with an accented word degrades to a case-sensitive match
// instead of a wrong one.
func toLower(s string) string {
	out := make([]byte, len(s))
	for index := 0; index < len(s); index++ {
		b := s[index]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		out[index] = b
	}
	return string(out)
}
