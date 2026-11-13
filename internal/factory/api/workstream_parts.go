package api

import "encoding/json"

// ── A WORKSTREAM'S PARTS ON THE WIRE ───────────────────────────────────────
//
// Each part of an organization workstream has its own route, so two people
// changing two different parts never refuse each other. A page's BODY is not
// here: it is a live document (/v1/docs/ws/<id>/page/<pid>), and these routes
// keep a page's place, title and kind, and its saved versions.
//
//	POST   /v1/workstreams/{id}/pages                    PageNew -> Workstream (201)
//	PATCH  /v1/workstreams/{id}/pages/{pid}  If-Match:page rev  PagePatch -> Workstream, 412 stale
//	DELETE /v1/workstreams/{id}/pages/{pid}              -> Workstream; its pages move up to its parent
//	GET    /v1/workstreams/{id}/pages/{pid}/versions     -> PageVersions, newest first, without words
//	POST   /v1/workstreams/{id}/pages/{pid}/versions     PageVersionNew -> PageVersion (201; 200 when unchanged)
//	GET    /v1/workstreams/{id}/pages/{pid}/versions/{v} -> PageVersion with body_md and state
//	POST   /v1/workstreams/{id}/pages/{pid}/restore      PageRestore -> PageVersion, the new version
//	GET    /v1/workstreams/{id}/plan                     -> WorkstreamPlan ({"plan":null,"rev":0} when none)
//	PUT    /v1/workstreams/{id}/plan     If-Match:plan rev (0 for the first)  {"plan":{...}} -> WorkstreamPlan
//	GET    /v1/workstreams/{id}/wakes                    -> {"wakes":[...]}
//	POST   /v1/workstreams/{id}/wakes                    {...} -> the wake (201)
//	PATCH  /v1/workstreams/{id}/wakes/{wid}  If-Match:rev  {...} -> the wake; a shallow merge, null removes a key
//	GET    /v1/workstreams/{id}/suggestions[?page=pid]   -> {"suggestions":[...]}
//	POST   /v1/workstreams/{id}/suggestions              {...} -> the suggestion (201)
//	PATCH  /v1/workstreams/{id}/suggestions/{sid}  If-Match:rev  {...} -> the suggestion
//	PUT    /v1/workstreams/{id}/jobs/{job}               -> Workstream; the job leaves any other workstream
//	DELETE /v1/workstreams/{id}/jobs/{job}               -> Workstream
//	POST   /v1/workstreams/{id}/unarchive                -> Workstream
//	POST   /v1/workstreams/{id}/spend  Idempotency-Key   WorkstreamSpendNew -> WorkstreamSpend
//
// A wake and a suggestion are the desk's own objects: the server keeps them as
// sent and stamps id, rev, by, at, updated_by and updated_at over whatever the
// body said. Every author is the server's: a by in a body is ignored.
//
// Every change publishes workstream.changed with part, part_id, by and rev.

// The parts a workstream.changed event names.
const (
	PartWorkstream  = "workstream"
	PartPage        = "page"
	PartVersions    = "versions"
	PartPlan        = "plan"
	PartWakes       = "wakes"
	PartSuggestions = "suggestions"
	PartJobs        = "jobs"
	PartSpend       = "spend"
)

// PageNew is a page to add. Index is its place among all the pages, the end
// when left out.
type PageNew struct {
	Title  string `json:"title"`
	Kind   string `json:"kind,omitempty"`
	Parent string `json:"parent,omitempty"`
	Index  *int   `json:"index,omitempty"`
}

// PagePatch is any subset of a page's place and name. Parent "" is the top.
type PagePatch struct {
	Title  *string `json:"title,omitempty"`
	Kind   *string `json:"kind,omitempty"`
	Parent *string `json:"parent,omitempty"`
	Index  *int    `json:"index,omitempty"`
}

// PageVersion is one saved version of a page. BodyMD and State come only with
// one version read whole; State is the live document's encoded state.
type PageVersion struct {
	Version      int    `json:"version"`
	At           string `json:"at"`
	By           string `json:"by"`
	Title        string `json:"title"`
	Reason       string `json:"reason" jsonschema:"enum=idle,enum=manual,enum=restore"`
	RestoredFrom int    `json:"restored_from,omitempty"`
	Bytes        int    `json:"bytes"`
	BodyMD       string `json:"body_md,omitempty"`
	State        []byte `json:"state,omitempty"`
}

// PageVersions is a page's versions, newest first.
type PageVersions struct {
	Versions []PageVersion `json:"versions"`
}

// PageVersionNew is a version a client saves: the page's words as markdown,
// the live document's state, and why ("idle" after thirty seconds without a
// change, "manual" when a person asked).
type PageVersionNew struct {
	BodyMD string `json:"body_md"`
	State  []byte `json:"state,omitempty"`
	Reason string `json:"reason"`
}

// PageRestore names the version to bring back.
type PageRestore struct {
	Version int `json:"version"`
}

// WorkstreamPlan is a workstream's plan, the desk's object, with its rev.
type WorkstreamPlan struct {
	Plan      json.RawMessage `json:"plan"`
	Rev       int             `json:"rev"`
	UpdatedAt string          `json:"updated_at,omitempty"`
	UpdatedBy string          `json:"updated_by,omitempty"`
}

// WorkstreamPlanPut is PUT /v1/workstreams/{id}/plan's body.
type WorkstreamPlanPut struct {
	Plan json.RawMessage `json:"plan"`
}

// WorkstreamWakes and WorkstreamSuggestions are the lists.
type WorkstreamWakes struct {
	Wakes []json.RawMessage `json:"wakes"`
}

type WorkstreamSuggestions struct {
	Suggestions []json.RawMessage `json:"suggestions"`
}

// WorkstreamSpendNew is one spend posted against a workstream by whoever ran
// the work. The poster is the caller; Job is a factory job on this server and
// Chat the driver's own conversation id.
type WorkstreamSpendNew struct {
	USD  float64 `json:"usd"`
	Job  *int    `json:"job,omitempty"`
	Chat string  `json:"chat,omitempty"`
}

// WorkstreamSpend is what the workstream has spent and may spend.
type WorkstreamSpend struct {
	SpendUSD  float64  `json:"spend_usd"`
	BudgetUSD *float64 `json:"budget_usd"`
}
