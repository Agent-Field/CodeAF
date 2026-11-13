package api

import "time"

// ── WHO IS HERE (POST /v1/presence) ─────────────────────────────────────────
//
// A desk with an organization item open beats every [PresenceBeat]; a person
// not heard from for [PresenceTTL] has left. The answer, and every
// `presence` event, is the whole list of who is on that item now.

// The presence clock. ONE SOURCE OF TRUTH: the desk reads these words from
// the protocol page, and the server enforces exactly these values.
const (
	PresenceBeat = 10 * time.Second
	PresenceTTL  = 30 * time.Second
)

// EventPresence is the event kind sent when who is on an item changes, with
// [Presence] as its data. It reaches only those who may view the item.
const EventPresence = "presence"

// The item kinds presence knows. A canvas server registers "canvas".
const (
	PresenceWorkstream = "workstream"
	PresenceCanvas     = "canvas"
)

// PresenceItem names one item in the organization.
type PresenceItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PresenceHere is one person on an item: the part they are on (a page id, a
// frame id, or "" for the item itself) and whether they are editing it.
type PresenceHere struct {
	Handle  string `json:"handle"`
	Part    string `json:"part,omitempty"`
	Editing bool   `json:"editing,omitempty"`
}

// Presence is POST /v1/presence's answer and the presence event's data.
type Presence struct {
	Item PresenceItem   `json:"item"`
	Here []PresenceHere `json:"here"`
}
