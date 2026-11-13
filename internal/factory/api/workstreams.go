package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ── AN ORGANIZATION'S WORKSTREAMS ON THE WIRE ──────────────────────────────
//
// An organization's workstreams live on its server, never in someone's app:
// the desk reads and writes them through the routes below, and a Personal
// workstream moving in is uploaded once (POST /v1/workstreams/import), after
// which the server copy is the only copy.
//
//	GET    /v1/workstreams                    -> WorkstreamList, visible to me, newest first
//	POST   /v1/workstreams                    WorkstreamNew -> Workstream
//	GET    /v1/workstreams/{id}               -> Workstream
//	PATCH  /v1/workstreams/{id}  If-Match:rev WorkstreamPatch -> Workstream, 412 on a stale rev
//	DELETE /v1/workstreams/{id}               -> 204, soft; ?purge=1 operator only
//	PUT    /v1/workstreams/{id}/owner         WorkstreamOwner -> Workstream
//	PUT    /v1/workstreams/{id}/shares        WorkstreamSharesPut -> WorkstreamShared
//	GET    /v1/workstreams/{id}/shares/preview?teams=a,b -> WorkstreamSharePreview
//	POST   /v1/workstreams/import             WorkstreamPackage -> Workstream
//
// Every change is published on the event stream as a [WorkstreamEvent].

// The stages a workstream can be in.
const (
	WorkstreamShaping  = "shaping"
	WorkstreamBuilding = "building"
	WorkstreamOngoing  = "ongoing"
	WorkstreamPlace    = "place"
	WorkstreamDone     = "done"
)

// The kinds of owner a workstream has: exactly one person or one team.
const (
	OwnerPerson = "person"
	OwnerTeam   = "team"
)

// The event kinds a workstream change publishes.
const (
	EventWorkstreamChanged = "workstream.changed"
	EventWorkstreamRemoved = "workstream.removed"
)

// The shapes kept on the server are spelled once, in package factory.
type (
	WorkstreamOwner    = factory.WorkstreamOwner
	WorkstreamPage     = factory.WorkstreamPage
	WorkstreamDecision = factory.WorkstreamDecision
	WorkstreamSource   = factory.WorkstreamSource
)

// WorkstreamSummary is one row of the list: enough to draw Home.
type WorkstreamSummary struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Stage      string          `json:"stage" jsonschema:"enum=shaping,enum=building,enum=ongoing,enum=place,enum=done"`
	Owner      WorkstreamOwner `json:"owner"`
	SharedWith []string        `json:"shared_with"`
	NeedsYou   int             `json:"needs_you"`
	Merged     int             `json:"merged"`
	Total      int             `json:"total"`
	UpdatedAt  string          `json:"updated_at"`
	ArchivedAt string          `json:"archived_at,omitempty"`
	Rev        int             `json:"rev"`
}

// Workstream is one workstream whole. GoAheads are the allowances of the
// go-ahead policy kept for its id ([Policy]), [] when it has none.
type Workstream struct {
	WorkstreamSummary
	Purpose   string               `json:"purpose"`
	Pages     []WorkstreamPage     `json:"pages"`
	Decisions []WorkstreamDecision `json:"decisions"`
	Sources   []WorkstreamSource   `json:"sources"`
	Links     []string             `json:"links"`
	GoAheads  []PolicyAllowance    `json:"go_aheads"`
	CreatedBy string               `json:"created_by"`
}

// WorkstreamList is GET /v1/workstreams's answer.
type WorkstreamList struct {
	Workstreams []WorkstreamSummary `json:"workstreams"`
}

// WorkstreamNew is POST /v1/workstreams's body. Owner defaults to the caller.
type WorkstreamNew struct {
	Name      string               `json:"name"`
	Purpose   string               `json:"purpose,omitempty"`
	Stage     string               `json:"stage,omitempty"`
	Owner     *WorkstreamOwner     `json:"owner,omitempty"`
	Pages     []WorkstreamPage     `json:"pages,omitempty"`
	Decisions []WorkstreamDecision `json:"decisions,omitempty"`
	Sources   []WorkstreamSource   `json:"sources,omitempty"`
	Links     []string             `json:"links,omitempty"`
}

// WorkstreamPatch is PATCH /v1/workstreams/{id}'s body: any subset, and an
// array that is sent REPLACES the one kept.
type WorkstreamPatch struct {
	Name      *string               `json:"name,omitempty"`
	Purpose   *string               `json:"purpose,omitempty"`
	Stage     *string               `json:"stage,omitempty"`
	Pages     *[]WorkstreamPage     `json:"pages,omitempty"`
	Decisions *[]WorkstreamDecision `json:"decisions,omitempty"`
	Sources   *[]WorkstreamSource   `json:"sources,omitempty"`
	Links     *[]string             `json:"links,omitempty"`
}

// WorkstreamSharesPut is PUT /v1/workstreams/{id}/shares's body: the whole
// list of teams it is shared with, and whether the workstreams it links to are
// shared with those teams too.
type WorkstreamSharesPut struct {
	Teams         []string `json:"teams"`
	IncludeLinked bool     `json:"include_linked"`
}

// WorkstreamShared is its answer: the workstream as it is now, and the ids of
// the linked workstreams that were shared beside it.
type WorkstreamShared struct {
	Workstream Workstream `json:"workstream"`
	AlsoShared []string   `json:"also_shared"`
}

// WorkstreamSharePreview is what include_linked would add.
type WorkstreamSharePreview struct {
	Linked []WorkstreamSummary `json:"linked"`
}

// WorkstreamPackage is a Personal workstream moving in, once: a workstream
// without the id, rev, owner and shares the server gives it. Its jobs stay
// where they ran.
type WorkstreamPackage struct {
	FromLocalID string               `json:"from_local_id"`
	Name        string               `json:"name"`
	Stage       string               `json:"stage,omitempty"`
	Purpose     string               `json:"purpose,omitempty"`
	Pages       []WorkstreamPage     `json:"pages,omitempty"`
	Decisions   []WorkstreamDecision `json:"decisions,omitempty"`
	Sources     []WorkstreamSource   `json:"sources,omitempty"`
	Links       []string             `json:"links,omitempty"`
	CreatedBy   string               `json:"created_by,omitempty"`
}

// WorkstreamEvent is the payload a workstream change publishes.
type WorkstreamEvent struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
