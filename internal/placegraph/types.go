// Package placegraph is the canonical store of the PLACE GRAPH: the named
// groupings a person files chats under (design: Places 6e, "Places: the model and
// rules"), the memberships that tie a chat to zero, one or many of them, and the
// reversible receipts that let ⌘Z take a structural action back.
//
// THE WORD "PLACE" HAS TWO MEANINGS IN THIS REPOSITORY. internal/session's Place
// is a session's on-disk folder layout ("referred folders"). Nothing here touches
// it, imports it, or shares a field with it. A placegraph.Place is the
// person-facing grouping, and the two never meet.
//
// WHAT LIVES HERE AND WHAT DOES NOT. This package owns structure: ids, names, the
// parent edges, tint, archive state, the pinned order, memberships, and the plain
// context and policy a place carries. It does NOT resolve a chat's context for a
// turn, rank sources, call a model or watch a session; those are later layers that
// read a Snapshot and render what it holds.
package placegraph

import (
	"encoding/json"
	"errors"
	"time"
)

// SchemaVersion is the on-disk format. A file written by a NEWER version is never
// rewritten (ErrUnsupportedVersion): losing a newer build's data to an older
// build's save is worse than refusing to start.
const SchemaVersion = 1

// The two virtual places. Neither is ever stored as a Place row.
const (
	// RootID is "All places": the parent of every top-level place. A Place whose
	// Parents is empty is a child of the root. Passing RootID as a parent is
	// accepted and normalised away, so callers may say "under the root" in words.
	RootID = "root"
	// NowID is the unplaced bucket: chats in no place. It has no Home, a graphite
	// tint, and no membership rows; a chat is "in Now" exactly when it is in no
	// active place (Snapshot.Unplaced).
	NowID = "now"
)

// Tint is one of the six fixed hues (design 9d). The empty Tint means "no choice
// made here": a child inherits from its first parent, a top-level place shows
// graphite.
type Tint string

const (
	TintTide     Tint = "tide"
	TintIris     Tint = "iris"
	TintRose     Tint = "rose"
	TintSand     Tint = "sand"
	TintSage     Tint = "sage"
	TintGraphite Tint = "graphite"
)

// Tints lists the palette in design order. PickableTints is the same without
// graphite, which is Now's colour and the "nothing chosen" colour, and which the
// tint menu does not offer (design 8f).
var (
	Tints         = []Tint{TintTide, TintIris, TintRose, TintSand, TintSage, TintGraphite}
	PickableTints = []Tint{TintTide, TintIris, TintRose, TintSand, TintSage}
)

// Valid reports whether t is one of the six hues.
func (t Tint) Valid() bool {
	for _, k := range Tints {
		if t == k {
			return true
		}
	}
	return false
}

// AddedBy says who filed a chat: the person, or the AI offering an existing place.
type AddedBy string

const (
	AddedByYou AddedBy = "you"
	AddedByAI  AddedBy = "ai"
)

// Valid reports whether a is one of the two spellings.
func (a AddedBy) Valid() bool { return a == AddedByYou || a == AddedByAI }

// SourceKind says what a context source points at (design 6f: repo or folder,
// file, URL; a chat reference is what dropping a tab resolves to).
type SourceKind string

const (
	SourceFolder SourceKind = "folder"
	SourceRepo   SourceKind = "repo"
	SourceFile   SourceKind = "file"
	SourceURL    SourceKind = "url"
	SourceChat   SourceKind = "chat"
)

// Valid reports whether k is a known kind.
func (k SourceKind) Valid() bool {
	switch k {
	case SourceFolder, SourceRepo, SourceFile, SourceURL, SourceChat:
		return true
	}
	return false
}

// Source is one thing a place makes available to its chats. The store keeps the
// reference and a label; it never opens, reads or validates the target.
type Source struct {
	ID      string     `json:"id"`
	Kind    SourceKind `json:"kind"`
	Ref     string     `json:"ref"`
	Label   string     `json:"label,omitempty"`
	AddedBy AddedBy    `json:"addedBy"`
	At      time.Time  `json:"at,omitzero"`
}

// Context is what a place says to its chats: plain prose instructions (design:
// "instructions are plain prose on the home") and sources. Sources add up across
// places and never conflict.
type Context struct {
	Instructions string   `json:"instructions,omitempty"`
	Sources      []Source `json:"sources,omitempty"`
}

// Policy is the part of a place that CAN conflict between places (design 6e:
// "default model, permissions"). An empty field means "this place has no opinion".
type Policy struct {
	Model       string `json:"model,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// Place is one node of the graph.
type Place struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Parents are ordered. The FIRST parent decides an inherited tint; a place
	// with no parents is a child of the root.
	Parents []string `json:"parents"`
	// Tint is an explicit choice. Empty inherits (EffectiveTint).
	Tint     Tint    `json:"tint,omitempty"`
	Archived bool    `json:"archived,omitempty"`
	Context  Context `json:"context"`
	Policy   Policy  `json:"policy"`
	// Manager is RESERVED ("not designed yet"). It is opaque JSON that is kept
	// verbatim through every load, save, merge and undo, and interpreted by nobody.
	Manager      json.RawMessage `json:"manager,omitempty"`
	CreatedAt    time.Time       `json:"createdAt,omitzero"`
	ArchivedAt   time.Time       `json:"archivedAt,omitzero"`
	LastOpenedAt time.Time       `json:"lastOpenedAt,omitzero"`
}

// Membership files one chat under one place. The chat id is whatever canonical id
// the caller uses for a conversation; this package never checks that it exists.
type Membership struct {
	ChatID  string    `json:"chatId"`
	PlaceID string    `json:"placeId"`
	AddedBy AddedBy   `json:"addedBy"`
	At      time.Time `json:"at,omitzero"`
}

// State is the persisted document and, copied, the Snapshot a caller reads.
type State struct {
	Version int `json:"version"`
	// Revision counts structural commits. It starts at 0 for a store that has
	// never written and rises by one per commit. TouchOpened does not move it,
	// so going to a place never invalidates an undo.
	Revision    uint64       `json:"revision"`
	Places      []Place      `json:"places"`
	Memberships []Membership `json:"memberships"`
	// Pinned is the rail's Pinned section, in the person's order (⌃1–9).
	Pinned []string `json:"pinned"`
	// Open is the rail's Open section, newest first (rail.go). It is soft state,
	// like LastOpenedAt: it never bumps Revision, and validation drops rows whose
	// place is gone rather than refusing the document.
	Open []OpenRow `json:"open,omitempty"`
}

// Bounds. Design scale is 20–200 places and depth 2–3; these ceilings are for
// refusing a runaway caller or a damaged file, not for ordinary use. Changing a
// cap changes docs/PLACES-STORE.md in the same commit.
const (
	MaxPlaces          = 2000
	MaxMemberships     = 50000
	MaxParents         = 16
	MaxPinned          = 50
	MaxChatPlaces      = 64
	MaxNameRunes       = 120
	MaxIDBytes         = 64
	MaxChatIDBytes     = 256
	MaxInstructions    = 64 * 1024
	MaxSources         = 200
	MaxSourceRefBytes  = 2048
	MaxSourceLabel     = 200
	MaxPolicyFieldRune = 200
	MaxManagerBytes    = 4 * 1024
	MaxFileBytes       = 32 << 20
	// MaxUndo is the design's "up to 20 steps" (Interactions: Undo).
	MaxUndo = 20
	// ContextAncestorLevels is "plus their ancestors, up to 2 levels" (6e).
	ContextAncestorLevels = 2
)

// Errors. Callers match with errors.Is.
var (
	ErrNotFound           = errors.New("placegraph: place not found")
	ErrReservedID         = errors.New("placegraph: root and now are virtual places")
	ErrInvalid            = errors.New("placegraph: invalid value")
	ErrTooLarge           = errors.New("placegraph: over a size bound")
	ErrCycle              = errors.New("placegraph: that would make a place its own ancestor")
	ErrNameTaken          = errors.New("placegraph: a sibling already has that name")
	ErrArchived           = errors.New("placegraph: place is archived")
	ErrNotArchived        = errors.New("placegraph: place is not archived")
	ErrRevisionConflict   = errors.New("placegraph: the graph changed since that receipt")
	ErrNoReceipt          = errors.New("placegraph: no such undo receipt")
	ErrLocked             = errors.New("placegraph: timed out waiting for the store lock")
	ErrUnsupportedVersion = errors.New("placegraph: file written by a newer version")
)

// Action names what a Receipt undoes.
type Action string

const (
	ActionCreate     Action = "place.create"
	ActionRename     Action = "place.rename"
	ActionTint       Action = "place.tint"
	ActionReparent   Action = "place.reparent"
	ActionArchive    Action = "place.archive"
	ActionRestore    Action = "place.restore"
	ActionDelete     Action = "place.delete"
	ActionMerge      Action = "place.merge"
	ActionContext    Action = "place.context"
	ActionPolicy     Action = "place.policy"
	ActionPin        Action = "place.pin"
	ActionUnpin      Action = "place.unpin"
	ActionReorder    Action = "place.reorder"
	ActionFile       Action = "chat.file"
	ActionUnfile     Action = "chat.unfile"
	ActionMove       Action = "chat.move"
	ActionForgetChat Action = "chat.forget"
)

// Receipt is the handle for taking one commit back. A zero Receipt (ID == "")
// means the call changed nothing, wrote nothing and left the revision alone, so
// there is nothing to undo.
type Receipt struct {
	ID             string    `json:"id"`
	Action         Action    `json:"action"`
	Subject        string    `json:"subject,omitempty"`
	BeforeRevision uint64    `json:"beforeRevision"`
	AfterRevision  uint64    `json:"afterRevision"`
	At             time.Time `json:"at"`
}

// Noop reports whether the call that returned r changed nothing.
func (r Receipt) Noop() bool { return r.ID == "" }

// Recovery describes what Open found wrong with the file on disk and what was
// done about it. It is the honest record: the damaged bytes are never discarded,
// they are moved aside.
type Recovery struct {
	// Kind is "quarantined" (the file was unusable and was moved to MovedTo; the
	// store started empty) or "repaired" (the file parsed; dangling references
	// were dropped and listed in Repairs; a copy of the original is at MovedTo).
	Kind    string    `json:"kind"`
	Reason  string    `json:"reason"`
	MovedTo string    `json:"movedTo"`
	Repairs []string  `json:"repairs,omitempty"`
	At      time.Time `json:"at"`
}
