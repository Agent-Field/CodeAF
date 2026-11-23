package factory

import "time"

// OrgCanvas is what an organization server keeps about one of its canvases
// beside the canvas itself: who owns it, who it is shared with, and whether it
// is private. The canvas — its title, frames, op log, versions and marks — is
// canvas-core's own directory, read and written only through `canvas rpc`
// (factory/canvasrpc), so nothing here repeats it.
type OrgCanvas struct {
	ID        string          `json:"id"`
	Owner     WorkstreamOwner `json:"owner"`
	Shares    []Share         `json:"shares,omitempty"`
	Private   bool            `json:"private,omitempty"`
	CreatedBy string          `json:"created_by"`
	// PolicyPrivate keeps an explicitly configured creation default inside a workstream.
	PolicyPrivate bool `json:"policy_private,omitempty"`
	// AgentCreated retains the sponsor as owner when the canvas is in a workstream.
	AgentCreated bool      `json:"agent_created,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	// UpdatedBy is the handle of whoever wrote it last.
	UpdatedBy string `json:"updated_by,omitempty"`
	// FromLocal is "<handle>/<local id>" for a canvas a person moved in from
	// their laptop, so moving the same one twice finds the first.
	FromLocal string `json:"from_local,omitempty"`
	// Workstream is the organization workstream this canvas belongs to, made
	// from inside it. A canvas in a workstream is reached the way the
	// workstream is — its owner, its shares, Private — and its own Shares
	// only add to that. Empty for a canvas that stands on its own.
	Workstream string `json:"workstream,omitempty"`
}

// OrgCanvases are an organization's canvases by id.
type OrgCanvases map[string]OrgCanvas
