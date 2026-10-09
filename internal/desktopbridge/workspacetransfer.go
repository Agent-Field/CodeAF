package desktopbridge

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

// POST /workspaces/{source}/transfer publishes a move between two places as one
// step: both complete next documents, each with the revision it was made from,
// or neither.
//
//	{intent, writer, destination, sourceRevision, destinationRevision,
//	 sourceWorkspace, destinationWorkspace}
//	→ 200 {intent, source: Record, destination: Record, already}
//	→ 409 {error, code:"conflict", source: Record, destination: Record}
//	→ 409 {error, code:"intent_changed"}
//
// A refused (409 conflict) transfer reserves nothing, so the renderer may rebase
// onto the records it carries and retry under the same intent. After a commit,
// the same bytes are acknowledged with already=true and no new revision. The
// route sits behind the same token and native-origin checks as every
// /workspaces route, and nothing here opens a conversation or calls a model.
// The record's movedTo is the durable relocation acknowledgment
// (workspacestore/pair.go).

type workspaceTransfer struct {
	Intent               string          `json:"intent"`
	Writer               string          `json:"writer"`
	Destination          string          `json:"destination"`
	SourceRevision       uint64          `json:"sourceRevision"`
	DestinationRevision  uint64          `json:"destinationRevision"`
	SourceWorkspace      json.RawMessage `json:"sourceWorkspace"`
	DestinationWorkspace json.RawMessage `json:"destinationWorkspace"`
}

func (b *Bridge) transferWorkspace(w http.ResponseWriter, r *http.Request, store *workspacestore.Store, source string) {
	if r.Method != http.MethodPost {
		fail(w, 405, "POST required")
		return
	}
	if !workspacestore.ValidKey(source) {
		failWorkspace(w, http.StatusNotFound, "unknown_key", "there is no such tab set")
		return
	}
	var body workspaceTransfer
	if !decodeMax(w, r, &body, 2*workspacestore.MaxDocumentBytes+16<<10) {
		return
	}
	res, err := store.PutPair(workspacestore.PairRequest{
		Intent: body.Intent, Writer: body.Writer, Source: source, Destination: body.Destination,
		SourceRevision: body.SourceRevision, DestinationRevision: body.DestinationRevision,
		SourceWorkspace: body.SourceWorkspace, DestinationWorkspace: body.DestinationWorkspace,
	})
	var conflict *workspacestore.PairConflictError
	if errors.As(err, &conflict) {
		writeStatus(w, http.StatusConflict, map[string]any{
			"error": "these tabs changed in another window", "code": "conflict",
			"source": conflict.Source, "destination": conflict.Destination,
		})
		return
	}
	if err != nil {
		workspaceError(w, err)
		return
	}
	write(w, map[string]any{"intent": res.Intent, "source": res.Source, "destination": res.Destination, "already": res.Already})
}
