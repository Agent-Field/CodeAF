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
//	PUT    /v1/workstreams/{id}/shares        WorkstreamSharesPut -> WorkstreamShared, 412 on a stale rev
//	GET    /v1/workstreams/{id}/shares/preview?shares=team:a:edit,person:b:view (or ?teams=a,b) -> WorkstreamSharePreview
//	POST   /v1/workstreams/import             WorkstreamPackage -> Workstream
//
// Its parts (pages one at a time, versions, plan, wakes, suggestions, jobs,
// unarchive, spend) have their own routes: workstream_parts.go.
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
	Share              = factory.Share
)

// WorkstreamSummary is one row of the list: enough to draw Home.
//
// Shares are who else reaches it and with which role; SharedWith is the team
// ids among them, kept for clients that read only teams. Private is a
// workstream only its owner reaches. MyRole is the caller's role on it: view,
// comment, edit or full, and "" on the operator's NAME-ONLY row for someone
// else's private workstream, which carries its id, name, owner, private and
// updated_at and nothing else (its counts are zero and its content is 404).
type WorkstreamSummary struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Stage      string          `json:"stage" jsonschema:"enum=shaping,enum=building,enum=ongoing,enum=place,enum=done"`
	Owner      WorkstreamOwner `json:"owner"`
	SharedWith []string        `json:"shared_with"`
	Shares     []Share         `json:"shares"`
	Private    bool            `json:"private"`
	MyRole     string          `json:"my_role"`
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
	// Jobs are the factory jobs on this server that belong to it.
	Jobs []int `json:"jobs"`
	// BudgetUSD is what it may spend (absent: no budget); SpendUSD what has
	// been posted against it.
	BudgetUSD *float64 `json:"budget_usd,omitempty"`
	SpendUSD  float64  `json:"spend_usd"`
	CreatedBy string   `json:"created_by"`
}

// WorkstreamList is GET /v1/workstreams's answer.
type WorkstreamList struct {
	Workstreams []WorkstreamSummary `json:"workstreams"`
}

// WorkstreamNew is POST /v1/workstreams's body. Owner defaults to the caller.
// Private needs a person owner.
type WorkstreamNew struct {
	Name      string               `json:"name"`
	Private   bool                 `json:"private,omitempty"`
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
	// BudgetUSD sets what it may spend; a negative number clears it.
	BudgetUSD *float64 `json:"budget_usd,omitempty"`
}

// WorkstreamSharesPut is PUT /v1/workstreams/{id}/shares's body, and it lands
// in ONE save:
//
//   - Shares, when sent, is the whole list of people and teams it is shared
//     with, each with a role. Teams is the older body: the whole list of teams,
//     each as edit, person shares kept. Sending neither keeps the shares.
//   - Owner, when sent, gives it to that person or team first.
//   - Private, when sent, sets whether only its owner reaches it: true needs a
//     person owner, and takes it from everyone it was shared with (a non-empty
//     Shares beside it is refused). A non-empty share list on a private
//     workstream makes it not private.
//   - Rev, when sent, must be the rev the caller read, or 412.
//   - IncludeLinked shares the workstreams it links to with the same people
//     and teams too, added to and never taken from.
type WorkstreamSharesPut struct {
	Shares        *[]Share         `json:"shares,omitempty"`
	Teams         []string         `json:"teams"`
	Owner         *WorkstreamOwner `json:"owner,omitempty"`
	Private       *bool            `json:"private,omitempty"`
	Rev           *int             `json:"rev,omitempty"`
	IncludeLinked bool             `json:"include_linked"`
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
// where they ran. Links may name other Personal workstreams by the ids they had
// on the laptop: one the same person already moved in is linked by its new id.
// CreatedBy is ignored: the copy is the caller's.
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

// WorkstreamEvent is the payload a workstream change publishes. Part names
// which part changed (Part* constants), PartID which one, By who changed it and
// Rev the part's rev after; all four are absent on an older server's event.
type WorkstreamEvent struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Part   string `json:"part,omitempty"`
	PartID string `json:"part_id,omitempty"`
	// By is the handle of the person who made the change, stamped by the
	// server from the sign-in.
	By  string `json:"by,omitempty"`
	Rev int    `json:"rev,omitempty"`
	// Deleted is true when the part named was removed (a page).
	Deleted bool `json:"deleted,omitempty"`
}
