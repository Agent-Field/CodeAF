package api

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── AN ORGANIZATION'S CANVASES ON THE WIRE ─────────────────────────────────
//
// An organization's canvases live on its server; the desk reads and writes
// them through the routes below, and every change goes through canvas-core
// (`canvas rpc`) on the server, so the shapes a canvas speaks — the snapshot,
// an op, an op record, a version, a mark — are canvas-core's own, passed
// through unchanged. What the server adds is who owns it and who may see it.
//
//	GET    /v1/canvases                          -> CanvasList
//	POST   /v1/canvases          CanvasNew       -> CanvasRow (201)
//	POST   /v1/canvases/import   CanvasImport    -> CanvasRow (201, or 200 when it was moved in before)
//	GET    /v1/canvases/{id}                     -> CanvasRow
//	PATCH  /v1/canvases/{id}     CanvasPatch     -> CanvasRow
//	DELETE /v1/canvases/{id}                     -> 204
//	PUT    /v1/canvases/{id}/shares CanvasSharesPut -> CanvasRow
//	GET    /v1/canvases/{id}/snapshot            -> canvas-core Snapshot
//	GET    /v1/canvases/{id}/ops?since=N         -> CanvasOps
//	POST   /v1/canvases/{id}/ops CanvasOpsPost   -> CanvasOps (the records written), 409 CanvasConflict
//	GET    /v1/canvases/{id}/versions            -> {"versions":[Version]}
//	POST   /v1/canvases/{id}/versions {"label"}  -> Version
//	POST   /v1/canvases/{id}/versions/{vid}/restore -> CanvasOps
//	GET    /v1/canvases/{id}/marks               -> {"marks":[Mark + "by"]}
//	POST   /v1/canvases/{id}/marks {frame,node,shape,text} -> Mark + "by"
//	PUT    /v1/canvases/{id}/assets/{sha}  (bytes; Content-Type) -> Asset
//	GET    /v1/canvases/{id}/assets/{sha}        -> the bytes
//
// THE ROLES: view reads; comment adds marks and takes a hand on a frame; edit
// writes ops, versions and restores; full shares, changes the owner, makes it
// private, trashes and deletes. One the caller cannot view answers 404.

// The live events a canvas sends, filtered to those who may view it.
const (
	EventCanvasChanged = "canvas.changed"
	EventCanvasRemoved = "canvas.removed"
)

// CanvasEvent is a canvas event's data: which canvas, the seq it reached and
// the handle of who wrote it (absent on a removal).
type CanvasEvent struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Seq  int64  `json:"seq,omitempty"`
	By   string `json:"by,omitempty"`
}

// CanvasRow is one canvas as a list shows it: canvas-core's summary (id,
// title, updated, outline, members, trashed, frames…) with the
// organization's fields beside it. A private canvas the operator may only
// know exists carries NameOnly and nothing of what it says.
type CanvasRow struct {
	Summary   json.RawMessage `json:"-"`
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Owner     WorkstreamOwner `json:"owner"`
	Shares    []factory.Share `json:"shares"`
	Private   bool            `json:"private"`
	MyRole    string          `json:"my_role"`
	Seq       int64           `json:"seq"`
	UpdatedBy string          `json:"updated_by,omitempty"`
	NameOnly  bool            `json:"name_only,omitempty"`
}

// MarshalJSON writes canvas-core's summary with the organization's fields
// laid over it, so a reader of either finds every field it knows.
func (r CanvasRow) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if len(r.Summary) > 0 && !r.NameOnly {
		if err := json.Unmarshal(r.Summary, &out); err != nil {
			return nil, err
		}
	}
	type plain CanvasRow
	own, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	var mine map[string]any
	if err := json.Unmarshal(own, &mine); err != nil {
		return nil, err
	}
	for k, v := range mine {
		out[k] = v
	}
	return json.Marshal(out)
}

// CanvasList is GET /v1/canvases: the canvases the caller can see, newest
// change first.
type CanvasList struct {
	Canvases []CanvasRow `json:"canvases"`
}

// CanvasNew makes a canvas. Owner left out is the caller.
type CanvasNew struct {
	Title   string           `json:"title"`
	Owner   *WorkstreamOwner `json:"owner,omitempty"`
	Shares  []factory.Share  `json:"shares,omitempty"`
	Private bool             `json:"private,omitempty"`
}

// CanvasPatch renames a canvas (edit) or moves it to or from the trash (full).
type CanvasPatch struct {
	Title   *string `json:"title,omitempty"`
	Trashed *bool   `json:"trashed,omitempty"`
}

// CanvasSharesPut sets who a canvas is shared with, and optionally its owner
// and whether it is private, in one change (full).
type CanvasSharesPut struct {
	Shares  []factory.Share  `json:"shares"`
	Owner   *WorkstreamOwner `json:"owner,omitempty"`
	Private *bool            `json:"private,omitempty"`
}

// CanvasOpsPost is a batch of canvas-core ops written as the caller. BaseSeq
// is the seq the writer has seen; a conflict answers every record after it.
type CanvasOpsPost struct {
	BaseSeq int64             `json:"base_seq"`
	Ops     []json.RawMessage `json:"ops"`
}

// CanvasOps is a run of canvas-core op records and the seq they end at.
type CanvasOps struct {
	Seq int64             `json:"seq"`
	Ops []json.RawMessage `json:"ops"`
}

// CanvasConflict is the 409 when another person's hand is on a frame the
// batch writes in or takes: who holds it, which frame, and what the writer
// has not seen yet.
type CanvasConflict struct {
	Error     string            `json:"error"`
	Reason    string            `json:"reason"`
	Frame     string            `json:"frame"`
	Holder    string            `json:"holder"`
	Seq       int64             `json:"seq"`
	Ops       []json.RawMessage `json:"ops"`
	RequestID string            `json:"request_id,omitempty"`
}

// CanvasAsset is one asset in an import: its sha256, its bytes (base64), its
// type and name.
type CanvasAsset struct {
	Sha  string `json:"sha"`
	Data []byte `json:"data"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

// CanvasImport is a Personal canvas moving into the organization: its
// canvas-core snapshot and assets, and its id on the laptop, so moving the
// same one twice answers the first.
type CanvasImport struct {
	Title       string           `json:"title,omitempty"`
	Snapshot    json.RawMessage  `json:"snapshot"`
	Assets      []CanvasAsset    `json:"assets,omitempty"`
	FromLocalID string           `json:"from_local_id"`
	Owner       *WorkstreamOwner `json:"owner,omitempty"`
	Shares      []factory.Share  `json:"shares,omitempty"`
	Private     bool             `json:"private,omitempty"`
}
