package api

import "time"

// ── AN ORGANIZATION'S LIVE DOCUMENTS (the wire of /v1/docs) ────────────────
//
// A live document is a Yjs document two people type into at once: a
// workstream page (`ws/<id>/page/<pid>`) or the text of a canvas frame
// (`canvas/<id>/frame/<fid>`). THE SERVER NEVER PARSES ONE. Updates, the
// snapshot and awareness states travel as base64 of the bytes the desk's Yjs
// made; the server stamps seq, by and at, keeps the log, and relays it.
//
//	GET  /v1/docs/{doc}?since=<seq>    DocState
//	POST /v1/docs/{doc}/updates        DocUpdatePost → DocUpdated
//	PUT  /v1/docs/{doc}/snapshot       DocSnapshotPut → DocSnapshotKept
//	POST /v1/docs/{doc}/awareness      DocAwarenessPost → 204
//	GET  /v1/docs/{doc}/events?since=  text/event-stream of the Event* kinds
//
// {doc} is written with its slashes, so /v1/docs/ws/ws_1/page/p_2/updates.
// Seeing the parent item (view) is what reads and follows a document and
// sends awareness; changing it (edit) is what sends updates and snapshots.
// A document whose parent you cannot see answers 404, the same as none.

// The event kinds a document's stream carries.
const (
	// EventDocUpdate is one stamped update: [DocUpdateEvent]. Its SSE id is
	// its seq, so a stream reopened with Last-Event-ID replays the rest.
	EventDocUpdate = "doc.update"
	// EventDocSnapshot opens a replay that starts behind the log: the
	// snapshot to load before the updates after it ([DocSnapshotEvent]).
	EventDocSnapshot = "doc.snapshot"
	// EventDocAwareness is one client's caret and name ([DocAwarenessEvent]);
	// a null state is a client that left or lapsed.
	EventDocAwareness = "doc.awareness"
	// EventDocRemoved ends the stream: the document is gone (its page was
	// deleted).
	EventDocRemoved = "doc.removed"
	// EventDocLagged ends the stream: this reader fell behind. Open it again
	// with since = the last seq applied; nothing was lost.
	EventDocLagged = "doc.lagged"
)

// The refusal reasons a document route adds to the usual ones.
const (
	DocReasonSnapshotNeeded = "snapshot_needed"
	DocReasonSeeded         = "seeded"
	DocReasonReset          = "reset"
	DocReasonTooLarge       = "too_large"
	DocReasonSlowDown       = "slow_down"
)

// DocUpdate is one stamped update. Update is base64.
type DocUpdate struct {
	Seq    int64     `json:"seq"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Update string    `json:"update"`
}

// DocSnapshot is the latest client-made state and the seq it covers.
type DocSnapshot struct {
	Seq   int64     `json:"seq"`
	By    string    `json:"by"`
	At    time.Time `json:"at"`
	State string    `json:"state"`
}

// DocAware is one client's awareness state. State is base64 of the
// y-protocols awareness update; null is a client that left.
type DocAware struct {
	By     string    `json:"by"`
	Client uint64    `json:"client"`
	State  *string   `json:"state"`
	At     time.Time `json:"at"`
}

// DocState is GET /v1/docs/{doc}: load Snapshot when it is set, then apply
// Updates in order; Seq is the newest stamped, the since to follow with.
type DocState struct {
	Doc       string       `json:"doc"`
	Seq       int64        `json:"seq"`
	Snapshot  *DocSnapshot `json:"snapshot"`
	Updates   []DocUpdate  `json:"updates"`
	Awareness []DocAware   `json:"awareness"`
	// CanEdit is whether this reader may send updates.
	CanEdit bool `json:"can_edit"`
	// Compact asks an editor to post a snapshot: the log is past the soft
	// line.
	Compact bool `json:"compact"`
}

// DocUpdatePost sends one update. BaseSeq is the newest seq the sender has
// applied; Seed accepts the update only into an empty document, so two desks
// that open a page for the first time cannot both put its text in.
type DocUpdatePost struct {
	BaseSeq int64  `json:"base_seq"`
	Update  string `json:"update"`
	Seed    bool   `json:"seed,omitempty"`
}

// DocUpdated is what the server stamped.
type DocUpdated struct {
	Doc     string    `json:"doc"`
	Seq     int64     `json:"seq"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
	Compact bool      `json:"compact"`
}

// DocSnapshotPut is a client-made Y.encodeStateAsUpdate covering Seq.
type DocSnapshotPut struct {
	Seq   int64  `json:"seq"`
	State string `json:"state"`
}

// DocSnapshotKept answers a snapshot. Kept is false when one at or past Seq
// was already kept, which is not an error: two desks compacted at once.
type DocSnapshotKept struct {
	Doc  string `json:"doc"`
	Seq  int64  `json:"seq"`
	Kept bool   `json:"kept"`
}

// DocAwarenessPost sends this client's awareness state; a null State says
// it left.
type DocAwarenessPost struct {
	Client uint64  `json:"client"`
	State  *string `json:"state"`
}

// DocUpdateEvent is the data of a doc.update event.
type DocUpdateEvent struct {
	Doc string `json:"doc"`
	DocUpdate
}

// DocSnapshotEvent is the data of a doc.snapshot event.
type DocSnapshotEvent struct {
	Doc string `json:"doc"`
	DocSnapshot
}

// DocAwarenessEvent is the data of a doc.awareness event.
type DocAwarenessEvent struct {
	Doc string `json:"doc"`
	DocAware
}

// DocEndEvent is the data of doc.removed and doc.lagged; Seq is the last
// update this stream sent.
type DocEndEvent struct {
	Doc string `json:"doc"`
	Seq int64  `json:"seq"`
}
