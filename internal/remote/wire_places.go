package remote

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── THE THREE PLACES THAT COULD NOT CROSS ───────────────────────────────────
//
// [MethodPlacesWorld] answered five of the surface's seven places out of one
// walk, and stopped there because the other three are not that walk: spend adds
// up an append-only LEDGER, search asks a full-text INDEX a question, and memory
// reads — and WRITES — a store of what the machine has learned. None of those is
// a listing of the projects root, so none of them could ride the world door, and
// each one drew a dim sentence in place of its rows (internal/tui3's host.go).
//
// These are their doors. They are additive to version 4, which is deliberate and
// is the whole reason they are shaped this way: an engine that predates them
// answers `engine: no such method` (server.go's fallthrough), the surface reads
// that as the reading being absent HERE, and the place says the sentence it has
// always said instead of hanging on a call nobody will answer. Nothing about the
// old wire moved, so a new surface against an old engine is exactly the program
// that shipped, and an old surface against a new engine never asks.
//
// ONE METHOD PER READING, which is the law [MethodPlacesWorld] already states:
// a single door answering everything would tie a page that wants the ledger on a
// three-second beat to a full-text search nobody typed.
const (
	// MethodPlacesLedger is the spend place's reading of THE ENGINE MACHINE'S
	// usage ledger, and it is BOUNDED BY A FLOOR because the ledger is a file
	// that grows by a line per model call, forever.
	//
	// A DOOR THAT ANSWERED THE WHOLE LEDGER WOULD CARRY A YEAR OF A BUSY
	// MACHINE'S ROWS ACROSS AN SSH PIPE TO DRAW A FORTNIGHT. So the surface says
	// how far back it is looking and the engine answers only that far back — and
	// on the beat after, the surface asks from the newest instant it already
	// holds, so the second call and every call after it carries the handful of
	// lines written since. That is [session.UsageCache]'s own tail-read law with
	// a wire in the middle of it (cmd/aforge's [hostLedger]).
	MethodPlacesLedger = "Places.Ledger" // LedgerArgs → LedgerReading

	// MethodPlacesSearch is one full-text query over every message the ENGINE
	// machine has kept ([store.Store.SearchConversations]).
	//
	// IT IS THE ONE DOOR ON THIS WIRE A KEYSTROKE MAY REACH, and it is still not
	// reached ON a keystroke: the search place arms a quiet interval and asks
	// only when the words have stopped moving (internal/tui3's place_search.go),
	// so this is exactly one call per question a person actually asked. There is
	// no cache behind it for the reason there is one behind every other reading
	// here — a query is not a beat, it is an answer somebody's enter key is
	// waiting for.
	MethodPlacesSearch  = "Places.Search"  // SearchArgs → []store.ConversationHit
	MethodPlacesArchive = "Places.Archive" // ArchiveArgs → nothing

	// ── the memory doors ────────────────────────────────────────────────────
	//
	// THE MEMORY PLACE IS THE ONLY PLACE ON THIS SURFACE THAT WRITES, and that is
	// why there are seven of these rather than one reading. A place that listed
	// the far machine's memories and edited this one's would be worse than a
	// place that crossed nothing at all: every row on it would be true and every
	// key on it would land somewhere else. So the whole seam crosses, readings
	// and writes together, and an engine that cannot answer one of them cannot
	// answer any of them — the door is the store, and the store is nil or it is
	// not (server.go's [Engine.Memory]).
	//
	// The names are the SURFACE's names for these readings and not the store's,
	// because the surface's seam is what this wire exists to fill; cmd/aforge
	// already owns that translation in one place ([v3Brain]).

	// MethodMemorySnapshot is everything remembered on the engine machine,
	// shelved and counted, in the two statements the place draws from.
	MethodMemorySnapshot = "Memory.Snapshot" // int (row cap) → store.MemoryShelves
	// MethodMemoryChanged is how many memories were learned and how many were
	// let go of since an instant — the two figures the memory tab's number is
	// made of. A zero instant answers zeros, which the store already promises.
	MethodMemoryChanged = "Memory.ChangedSince" // time.Time → MemoryChange
	// MethodMemoryList is the flat list behind `/memories` and behind the undo,
	// which has to re-read after a restore.
	MethodMemoryList = "Memory.List" // MemoryListArgs → []store.Memory
	// MethodMemoryUpdate is `e` on a line: the wording, fixed.
	MethodMemoryUpdate = "Memory.Update" // MemoryUpdateArgs → nothing
	// MethodMemoryForget is `f` on a line, and MethodMemoryRestore is the undo
	// that takes it back. Both carry the id and nothing else.
	MethodMemoryForget  = "Memory.Forget"  // string (id) → nothing
	MethodMemoryRestore = "Memory.Restore" // string (id) → nothing
	// MethodMemoryProvenance is where and when one memory was learned, asked for
	// the ONE id whose card is open rather than for every row on the page.
	MethodMemoryProvenance = "Memory.Provenance" // string (id) → MemoryOrigin
)

// LedgerArgs is how far back the spend place is looking.
//
// SINCE IS A FLOOR AND NOT A WINDOW. The page's own window has two ends and
// moves under the arrow keys at key-repeat rate; this carries only the earlier
// one, so a person paging BACK widens what the surface holds by one call and a
// person paging forward within it makes none at all. A zero Since is the whole
// ledger, which is what a caller with no window yet means and what a test means.
type LedgerArgs struct {
	Since time.Time `json:"since,omitempty"`
}

// LedgerReading is the answer: the lines at or after the floor, and the one fact
// about the rest of the file that a bounded read cannot carry.
type LedgerReading struct {
	// Lines are every priced call at or after [LedgerArgs.Since], in the order
	// the ledger holds them.
	Lines []session.UsageLine `json:"lines,omitempty"`
	// Held is whether the ledger holds ANY priced line at all, at any depth —
	// the fact that tells a machine which has never spent anything from a window
	// paged onto a quiet fortnight. The spend page draws its own three sentences
	// for the first and the window header over nothing for the second
	// (internal/tui3's [spendPage.held]), and a bounded read cannot tell them
	// apart on its own: no lines since a floor is both.
	Held bool `json:"held,omitempty"`
}

// SearchArgs is one question for the far machine's index.
type SearchArgs struct {
	Terms string `json:"terms"`
	Limit int    `json:"limit,omitempty"`
}

type ArchiveArgs struct {
	Dir      string `json:"dir"`
	Archived bool   `json:"archived"`
}

// MemoryChange is [MethodMemoryChanged]'s pair of figures. It is a struct
// because the call answers two numbers and a JSON array of two ints is a shape
// nobody reading a frame with `head` could name.
type MemoryChange struct {
	Learned int `json:"learned,omitempty"`
	LetGo   int `json:"letGo,omitempty"`
}

// MemoryListArgs is the flat list, by scope and bounded.
type MemoryListArgs struct {
	Scope string `json:"scope,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// MemoryUpdateArgs is one memory's wording, fixed. Every field travels whole —
// the store's Update replaces rather than patches, and a wire that carried only
// what changed would have to know which of the four a person touched.
type MemoryUpdateArgs struct {
	ID    string   `json:"id"`
	Title string   `json:"title,omitempty"`
	Text  string   `json:"text,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

// MemoryOrigin is where and when one memory was learned: the conversation's own
// id, its title, and the instant. It is the store's three return values with
// names on them, for [MemoryChange]'s reason.
type MemoryOrigin struct {
	Session string    `json:"session,omitempty"`
	Title   string    `json:"title,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

// EngineMemory is the ENGINE MACHINE'S memory store as this wire needs it, and
// it is deliberately the same seven methods internal/tui3's MemoryStore asks
// for — one interface, satisfied by the same adapter at both ends, so that a
// method added to the place cannot land here as a method the far end silently
// does not have (cmd/aforge's [v3Brain] satisfies both by construction).
//
// NIL IS MEMORY OFF ON THAT MACHINE, and it is answered as a refusal rather than
// as an empty store — the same reading [Engine.World] and [Engine.StandingItems]
// already ask for. The surface keeps the difference, because "that machine
// remembers nothing" and "that machine is not remembering" are two sentences and
// only one of them is about a setting.
type EngineMemory interface {
	Snapshot(limit int) (store.MemoryShelves, error)
	ChangedSince(t time.Time) (learned, letGo int, err error)
	ListMemories(scope string, limit int) ([]store.Memory, error)
	UpdateMemory(id, title, text string, tags []string) error
	ForgetMemory(id string) error
	RestoreMemory(id string) error
	MemoryProvenance(id string) (sessionID, sessionTitle string, writtenAt time.Time, err error)
}
