// Package registry is the one command registry named in the chat-rebuild
// doc's 5.22 ("Discoverability: no typed-only actions"): every action lives
// on a visible object, and typing is an accelerator, never the only door. A
// single catalog of entries — id, verb phrase, description, scope predicate,
// key binding, slash alias, journal mapping — is the source every render
// surface (action strips, ctrl+k palette, slash-filtered palette, contextual
// footer, chips, empty states) reads from, so discoverability holds by
// construction instead of by six surfaces staying in sync by hand.
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

import "github.com/Agent-Field/aforge-v2/internal/store"

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
)

// ScopeAny matches every entry regardless of scope. It is the predicate a
// surface with no current focus (an empty state, the unscoped ctrl+k
// palette) queries with.
const ScopeAny = ScopeThread | ScopeNode | ScopeTalk

// Has reports whether scope and other share at least one bit — the one test
// every query function in this package runs, so an entry scoped to more than
// one surface (alt+g closes the node view and toggles the rail either way)
// is found from any of them.
func (s Scope) Has(other Scope) bool { return s&other != 0 }

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
	// Slash is the alias typed after "/" in the composer or a task's steer
	// line, without the leading slash. Empty when the entry has none.
	Slash string
	// Journal is what the action journals, when it journals anything.
	Journal Journal

	lowerVerb, lowerDescription string
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
