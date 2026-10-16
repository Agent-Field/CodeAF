// Package workgraph is the engine's GENERAL account of work that waits on
// other work: tasks, who owns them, which task waits on which, what state each
// is in, when that state was entered, what each changed and what it cost.
//
// WHAT IT IS NOT. Nothing here knows how a surface draws anything. There are no
// columns, no coordinates, no colours, no cards, no boards and no chain: a
// surface computes every one of those from the facts below. The package
// imports neither the session nor the app wire, so the same facts can be asked
// of any producer that can name its tasks and its links. The session's reader
// (internal/session/workgraph_source.go) and the app's door (internal/appwire's
// work.graph) are two consumers of it.
//
// THE LAWS.
//
//   - A LINK IS A STORED, ENFORCED COMMAND AND NOTHING ELSE. An edge exists
//     because a producer recorded that one task waits on another and its
//     scheduler honoured it. No edge is ever inferred from a title, a body, the
//     order tasks were made in, or how a group is shaped.
//   - DIRECTION IS ALWAYS DEPENDENT → PREREQUISITE. [GraphEdge.From] is the task
//     that waits; [GraphEdge.To] is the task it waits on.
//   - NO ID IS FABRICATED. Every edge names a real task by its [TaskRef]. A
//     target that is not among the nodes of the scope is said to be external (a
//     real task elsewhere, with its state when readable) or missing (the record
//     names a task the owner no longer has) — never silently dropped, never
//     invented.
//   - UNKNOWN IS NOT ZERO. A fact nobody recorded is absent, and what it was
//     counted against is said beside every sum ([GraphFigure]).
//   - EVERY ANSWER IS BOUNDED. A page holds at most [MaxNodes] nodes, at most
//     [MaxEdges] edges and strings cut to [MaxTitleRunes] / [MaxTextRunes]; the
//     true totals are always said, so a short page is never mistaken for a
//     whole scope.
//   - THE READ IS READ-ONLY. It takes no lock the work holds, writes nothing,
//     and answers from immutable snapshots, so a caller cannot reach into
//     another caller's answer.
package workgraph

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Contract is the version of this vocabulary. It moves only when a field
// changes meaning; a new optional field does not move it.
const Contract = 1

// The bounds of one answer. They are constants, not settings: a surface that
// needs more asks again with the cursor.
const (
	// DefaultNodes is how many nodes a page holds when the request names none.
	DefaultNodes = 200
	// MaxNodes is the most nodes one page holds, whatever was asked.
	MaxNodes = 500
	// MaxEdges is the most edges one page holds. A page ends early rather than
	// pass it, and says so through its cursor.
	MaxEdges = 4096
	// MaxEdgesPerNode is the most waits-on edges one node's row carries; the
	// node's WaitsOnTotal is the true count and WaitsOnCut says the list is cut.
	MaxEdgesPerNode = 64
	// MaxFilesPerNode is the most changed paths one node carries.
	MaxFilesPerNode = 50
	// MaxClusters is the most clusters one page holds.
	MaxClusters = 200
	// MaxMembers is the most people one answer holds.
	MaxMembers = 500
	// MaxCycles is the most cycles an answer names, each of at most
	// [MaxCycleNodes] tasks; CyclesTotal is the true count.
	MaxCycles     = 16
	MaxCycleNodes = 32
	// MaxFocusDepth and MaxFocusNodes bound a neighbourhood walk.
	MaxFocusDepth = 6
	MaxFocusNodes = MaxNodes
	// MaxTitleRunes and MaxTextRunes cut the words a node carries.
	MaxTitleRunes = 160
	MaxTextRunes  = 1000
	// MaxScopeNodes is the most tasks one scope is built from. A scope with more
	// says Coverage.Truncated and is built from the first of them in order.
	MaxScopeNodes = 200_000
)

// ScopeKind is what a request is about.
type ScopeKind string

// The three scopes.
const (
	ScopeAgent ScopeKind = "agent"
	ScopeCrew  ScopeKind = "crew"
	ScopeTask  ScopeKind = "task"
)

// GraphScope names what a request is about. For an agent it is the agent's
// own id; for a crew the crew's id; for a task the task's [TaskRef.ID], whose
// scope is that task's own sub-tasks at every depth.
type GraphScope struct {
	Kind ScopeKind `json:"kind" enum:"agent,crew,task"`
	ID   string    `json:"id"`
}

// TaskRef is a task's immutable identity: the conversation that owns it and the
// number that conversation minted for it. Two conversations both have a task 3,
// so the pair — never the number alone — names a task.
type TaskRef struct {
	// Owner is the owning conversation's id (its folder's name, stable across
	// launches). Task is that conversation's own id for the task: a canonical
	// decimal number for a task or run row the conversation minted, or the plan
	// store's own "t-<id>" token for a plan row (see [ValidPlanToken]).
	Owner string `json:"owner"`
	Task  string `json:"task"`
}

// ID is the task's id on every wire that names a task: "<owner>:<task>". It is
// collision-safe across owners because neither part may hold a colon.
func (r TaskRef) ID() string { return r.Owner + ":" + r.Task }

// The bounds a reference's parts live within. They are the engine's own limits
// on an identity, enforced before any path is built from it.
const (
	// MaxOwnerBytes bounds an owner id. A conversation's folder name is a uuid or
	// a short slug today; 128 bytes leaves room without letting an id become a
	// payload.
	MaxOwnerBytes = 128
	// MaxPlanTokenBytes bounds a plan-store task token ("t-" and the store's id).
	MaxPlanTokenBytes = 64
)

// ValidOwner reports whether s is a safe owner id: ONE path component, bounded,
// printable, with no separator, colon, NUL or control character, and neither "."
// nor ".." nor a dotfile. It is the same law the app backend applies before it
// joins a conversation id to a folder (a bare name, never a path), so a
// valid-looking reference can never walk out of the places root.
func ValidOwner(s string) bool {
	if s == "" || len(s) > MaxOwnerBytes || s[0] == '.' || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == ':' || r == utf8.RuneError {
			return false
		}
	}
	return true
}

// ValidNumber reports whether s is a canonical positive uint64: ASCII digits,
// no sign, no leading zero, at most 20 digits and no larger than 2^64-1.
func ValidNumber(s string) bool {
	if s == "" || len(s) > 20 || s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// ValidPlanToken reports whether s is a plan-store task id as the engine spells
// it ("t-" followed by the store's own id of letters, digits, dot, dash and
// underscore). Plan rows are real tasks the app lists (AppTasks) whose identity
// is the store's, not a number the conversation minted; they keep it.
func ValidPlanToken(s string) bool {
	if len(s) < 3 || len(s) > MaxPlanTokenBytes || s[0] != 't' || s[1] != '-' {
		return false
	}
	for i := 2; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

// Valid reports whether the reference names something safe to look up: a valid
// owner and a task that is either a canonical number or a plan token.
func (r TaskRef) Valid() bool {
	return ValidOwner(r.Owner) && (ValidNumber(r.Task) || ValidPlanToken(r.Task))
}

// ParseID is the reference an id names, and false for a string that is not
// "<owner>:<number>".
func ParseID(id string) (TaskRef, bool) {
	i := strings.LastIndex(id, ":")
	if i <= 0 {
		return TaskRef{}, false
	}
	ref := TaskRef{Owner: id[:i], Task: id[i+1:]}
	return ref, ref.Valid()
}

// The views of a request.
const (
	// ViewNodes pages the tasks and, with them, the edges whose dependent is on
	// the page. It is the default.
	ViewNodes = "nodes"
	// ViewClusters pages the groupings: sub-crews and task groups, each with its
	// true member count and a count by state.
	ViewClusters = "clusters"
	// ViewPeople answers the crews and the members that own the scope's work.
	ViewPeople = "people"
	// ViewMetrics answers the scope's figures, each with what it was counted
	// against.
	ViewMetrics = "metrics"
)

// The kinds of cluster.
const (
	ClusterCrew  = "crew"
	ClusterGroup = "group"
)

// The directions a focus walk follows.
const (
	// FocusPrerequisites follows waits-on links: what the focus waits on.
	FocusPrerequisites = "prerequisites"
	// FocusDependents follows them backwards: what waits on the focus.
	FocusDependents = "dependents"
	FocusBoth       = "both"
)

// GraphRequest is work.graph's params. Every field but Scope may be left out.
type GraphRequest struct {
	Scope GraphScope `json:"scope"`
	View  string     `json:"view,omitempty" enum:"nodes,clusters,people,metrics"`
	// Cluster, with ViewNodes, restricts the page to one cluster's members: a
	// sub-crew (and every crew under it) or a task group.
	Cluster *ClusterRef `json:"cluster,omitempty"`
	// Focus, with ViewNodes, answers one task's neighbourhood instead of a page
	// of the scope: the tasks reachable from it along waits-on links, nearest
	// first, to Depth steps. It has no cursor and says Cut when it stopped
	// early.
	Focus *FocusRef `json:"focus,omitempty"`
	// Cursor is the Next of the page before; Limit is how many nodes (or
	// clusters) to answer, bounded by [MaxNodes].
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	// IfRevision is the Revision the caller already holds. When it is still
	// current the answer is only Unchanged: no page is built or sent.
	IfRevision string `json:"if_revision,omitempty"`
}

// ClusterRef names one cluster.
type ClusterRef struct {
	Kind string `json:"kind" enum:"crew,group"`
	ID   string `json:"id"`
}

// FocusRef names the task a neighbourhood is walked from.
type FocusRef struct {
	Task      string `json:"task"`
	Direction string `json:"direction,omitempty" enum:"prerequisites,dependents,both"`
	Depth     int    `json:"depth,omitempty"`
}

// GraphResult is work.graph's answer. Which of the sections is filled depends on
// the view; Scope, Revision, Totals and Coverage are always there unless the
// answer is Unchanged.
type GraphResult struct {
	Contract int    `json:"contract"`
	View     string `json:"view"`
	// Revision names the exact source facts the answer was built from. It
	// changes when, and only when, a source the scope reads changed: a task
	// moved, a link was recorded, a member joined. A caller holds it and sends
	// it back as IfRevision; the events that say something moved are the ones
	// the wire already has (task.changed, member.changed, crew events), so no
	// caller polls.
	Revision string `json:"revision"`
	// AsOf is when the engine last read the sources this answer is built from —
	// the build time of the cached snapshot, NOT the time of this request, and not
	// part of the revision. A banner may say "as of 14:32"; it must not claim the
	// content is newer than that.
	AsOf      *time.Time `json:"as_of,omitempty"`
	Unchanged bool       `json:"unchanged,omitempty"`

	Scope    GraphScope  `json:"scope"`
	About    ScopeFacts  `json:"about"`
	Totals   GraphTotals `json:"totals"`
	Coverage Coverage    `json:"coverage"`

	Nodes    []GraphNode    `json:"nodes,omitempty"`
	Edges    []GraphEdge    `json:"edges,omitempty"`
	Clusters []GraphCluster `json:"clusters,omitempty"`
	Crews    []CrewFact     `json:"crews,omitempty"`
	Members  []MemberFact   `json:"members,omitempty"`
	Metrics  *GraphMetrics  `json:"metrics,omitempty"`

	Diagnostics Diagnostics `json:"diagnostics"`
	// Next is the cursor of the following page, absent on the last. Returned is
	// how many nodes (or clusters) this page holds and Cut says a focus walk
	// stopped at a bound.
	Next     string `json:"next_cursor,omitempty"`
	Returned int    `json:"returned"`
	Cut      bool   `json:"cut,omitempty"`
}

// ScopeFacts is what the scope itself is, as the producer knew it.
type ScopeFacts struct {
	Title string `json:"title,omitempty"`
	// State is "open" or "closed" for a crew or an agent; empty when the
	// producer does not say. A closed scope is ReadOnly: nothing in it moves.
	State    string     `json:"state,omitempty" enum:"open,closed"`
	ClosedAt *time.Time `json:"closed_at,omitempty"`
	ReadOnly bool       `json:"read_only,omitempty"`
	// Root is the task a task scope is about, with its own facts.
	Root *GraphNode `json:"root,omitempty"`
	// Brief is what the scope was asked to do, with where the words came from:
	// a task's instructions, a crew's goal, an agent's first request. Absent
	// when the producer has none.
	Brief *GraphText `json:"brief,omitempty"`
}

// GraphText is words cut to a bound and the place they were read from.
type GraphText struct {
	// Source is the recorded field the text is: "task_brief" (the person's
	// edit), "admitted_brief" (what the task was admitted with), "request" (the
	// person's own message), "crew_goal" or "first_request".
	Source string `json:"source"`
	Text   string `json:"text"`
	Cut    bool   `json:"cut,omitempty"`
	// Anchor is where the person's original words are, when the record points at
	// them: the conversation and the line of its journal. Absent when the record
	// carries no pointer — no anchor is ever made up.
	Anchor *TextAnchor `json:"anchor,omitempty"`
}

// TextAnchor points at a line of a conversation's journal.
type TextAnchor struct {
	Agent string `json:"agent"`
	Line  int    `json:"line"`
}

// RouteSeat is one role of the crew a task was accepted to run on, as the record
// stored it at admission: the role, the model seated there, and whether a
// person pinned it.
type RouteSeat struct {
	Role   string `json:"role"`
	Model  string `json:"model"`
	Pinned bool   `json:"pinned,omitempty"`
	// Price is the accepted route's own published tariff at admission. Absent
	// is unknown — a record written before tariffs were stored, or a route
	// nobody prices — never a price of nothing.
	Price *RoutePrice `json:"price,omitempty"`
}

// RoutePrice is one route's published price in dollars per token.
type RoutePrice struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
	CacheRead  float64 `json:"cache_read"`
}

// GraphTotals are the scope's true sizes, whatever the page holds.
type GraphTotals struct {
	Nodes int `json:"nodes"`
	// Edges counts every waits-on link whose dependent is in the scope,
	// including those to external and missing targets.
	Edges    int `json:"edges"`
	Clusters int `json:"clusters,omitempty"`
	Crews    int `json:"crews,omitempty"`
	Members  int `json:"members,omitempty"`
	// ByState counts nodes by their engine state word.
	ByState map[string]int `json:"by_state,omitempty"`
}

// Coverage says how much of the truth the answer is built from, so a missing
// fact is never read as a zero. Every count is of nodes.
type Coverage struct {
	// Complete is true when every owner in scope was read and nothing was cut at
	// [MaxScopeNodes].
	Complete  bool `json:"complete"`
	Truncated bool `json:"truncated,omitempty"`
	// Owners is how many conversations the scope reads; Unread names those that
	// could not be read, each with a reason, at most 16.
	Owners int           `json:"owners"`
	Unread []OwnerIssue  `json:"unread,omitempty"`
	Links  LinkCoverage  `json:"links"`
	Clock  ClockCoverage `json:"clock"`
	// Excluded names the kinds of work the scope's producer does not put in this
	// graph, so their absence is a statement and not a silence.
	Excluded []string `json:"excluded,omitempty"`
}

// OwnerIssue is one conversation the scope could not read.
type OwnerIssue struct {
	Owner  string `json:"owner"`
	Reason string `json:"reason" enum:"missing,unreadable,too_large,partial"`
}

// LinkCoverage counts nodes by whether their waits-on links were recorded.
// Recorded says the node's links are on its record — zero links is then a true
// "waits on nothing". Unrecorded says the record a node came from never carried
// links (a handed-over run's row), so zero links there means unknown.
type LinkCoverage struct {
	Recorded   int `json:"recorded"`
	Unrecorded int `json:"unrecorded"`
}

// ClockCoverage counts nodes by which instants their record carries.
type ClockCoverage struct {
	Admitted int `json:"admitted"`
	Started  int `json:"started"`
	State    int `json:"state"`
	Ended    int `json:"ended"`
}

// The engine's task state words, as persisted.
const KindQuick = "quick"

const (
	StateQueued      = "queued"
	StateRunning     = "running"
	StateDone        = "done"
	StateFailed      = "failed"
	StateUnverified  = "unverified"
	StateInterrupted = "interrupted"
)

// The kinds of link and the stores one is read from.
const (
	// KindWaitsOn is the only kind: From does not start until To is done.
	KindWaitsOn = "waits_on"
	// ViaRecord is a link between two tasks of one conversation, written when
	// the later one was admitted and enforced by that conversation's frontier.
	ViaRecord = "record"
	// ViaExternal is a link to a task of another conversation, recorded by
	// WaitOnTask and enforced by the same frontier before the task starts.
	ViaExternal = "external"
)

// How an edge's target stands against the scope.
const (
	// TargetNode: the target is a node of this scope (it may be on another page).
	TargetNode = "node"
	// TargetExternal: the target is a real task outside the scope; ToState says
	// its state when it could be read.
	TargetExternal = "external"
	// TargetMissing: the record names a task its owner does not have.
	TargetMissing = "missing"
)

// GraphNode is one task.
type GraphNode struct {
	// ID is the task's wire id, Ref its identity and Seq its owner's admission
	// number (the order the owner made its tasks in).
	ID  string  `json:"id"`
	Ref TaskRef `json:"ref"`
	Seq uint64  `json:"seq"`
	// Kind is the engine's kind word: "task" for ordinary work, "quick" (a
	// one-shot lookup the app does not list as a task: kept only so a link to it
	// resolves, and not counted in the metrics), "run" for a handed-over task's
	// row, "plan" for a row of a run's own plan store (a real sub-task the app
	// lists, identified by the store's "t-<id>"), "index" for a task the project's
	// index remembers that the conversation's checkpoint no longer holds, and the
	// producer's own words for the rest (harness, subharness).
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// State is one of the engine state words (queued, running, done, failed,
	// unverified, interrupted). Paused, Stopped and Declined are the person's
	// holds and endings, which the state word alone does not carry. Ending is
	// why a failed task stopped, in the engine's word.
	State string `json:"state" enum:"queued,running,done,failed,unverified,interrupted"`
	// StateDetail is the producing store's own status word when it has finer
	// ones than State does — a plan row's "pending", "ready", "claimed",
	// "running", "done", "failed" or "cancelled" — so that queued can be told
	// apart as waiting on prerequisites ("pending") or free to start ("ready").
	// Absent where State is already the store's own word.
	StateDetail string `json:"state_detail,omitempty"`
	Paused      bool   `json:"paused,omitempty"`
	Stopped     bool   `json:"stopped,omitempty"`
	Declined    bool   `json:"declined,omitempty"`
	Ending      string `json:"ending,omitempty"`
	// Parent is the task that handed this one out (its wire id), empty at the
	// top of a family. ParentState says whether that parent is among the nodes:
	// "root" (none), "node", or "missing" (the record names a parent its owner
	// no longer has). Depth is the family depth the owner recorded, 1 for a
	// conversation's own work.
	Parent      string `json:"parent,omitempty"`
	ParentState string `json:"parent_state" enum:"root,node,missing"`
	Depth       int    `json:"depth,omitempty"`
	// Group is the adaptive run this task belongs to, when it belongs to one.
	Group *GroupRef `json:"group,omitempty"`
	// Crew is the sub-crew the task's owner sits in, inside the scope; absent
	// outside a crew scope and for an owner in no crew of it.
	Crew *CrewPlace `json:"crew,omitempty"`

	Clock        Clock      `json:"clock"`
	Instructions *GraphText `json:"instructions,omitempty"`
	Model        string     `json:"model,omitempty"`
	// Route is the crew the record accepted for the task, role by role. Absent
	// where the record keeps none (only a handed-over run's row persists one).
	Route   []RouteSeat `json:"route,omitempty"`
	Landing *Landing    `json:"landing,omitempty"`
	Files   FileSet     `json:"files"`
	Spend   *TaskSpend  `json:"spend,omitempty"`
	Checks  *TaskChecks `json:"checks,omitempty"`

	// Links says whether this task's waits-on links were recorded ("recorded")
	// or the record never carried them ("unrecorded"). WaitsOnTotal is the true
	// number of tasks it waits on and WaitedByTotal the true number of tasks in
	// the scope that wait on it; WaitsOnCut says the page's edges for it are cut
	// at [MaxEdgesPerNode]. InCycle says it is on a waits-on cycle.
	Links         string `json:"links" enum:"recorded,unrecorded"`
	WaitsOnTotal  int    `json:"waits_on_total"`
	WaitedByTotal int    `json:"waited_by_total"`
	WaitsOnCut    bool   `json:"waits_on_cut,omitempty"`
	InCycle       bool   `json:"in_cycle,omitempty"`
}

// GroupRef names the adaptive run a task belongs to and carries the run's own
// name, which is the title of the run's own row.
type GroupRef struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// CrewPlace is a task owner's place in a crew scope: the sub-crew it sits in
// and that crew's ancestry inside the scope, outermost first. Also lists the
// other crews of the scope the owner sits in, at most 8.
type CrewPlace struct {
	Crew   string   `json:"crew"`
	Path   []string `json:"path"`
	AlsoIn []string `json:"also_in,omitempty"`
}

// Clock is the instants a task's record carries. Each is absent when nothing
// recorded it — never the read time, never a guess.
//
//   - AdmittedAt: when the owner made the task.
//   - StartedAt: when it began running.
//   - StateAt: when it entered the state it is in.
//   - EndedAt: when it landed or stopped.
type Clock struct {
	AdmittedAt *time.Time `json:"admitted_at,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	StateAt    *time.Time `json:"state_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// Landing is how a finished task's work came home, as the record says.
type Landing struct {
	// Outcome is "merged", "kept" (left on its branch), "conflicted" or
	// "inplace" (written in the person's own folder).
	Outcome string `json:"outcome"`
	// At is when it landed, absent for a record that never carried the instant.
	At *time.Time `json:"at,omitempty"`
}

// FileSet is the paths a task changed, workspace-relative and slash-spelled.
// Total is the true count; Paths is at most [MaxFilesPerNode] of them. Source
// is "changed" (the landing's own list) or "wrote" (what a running task has
// written so far); Omitted counts recorded paths withheld as outside the
// workspace.
type FileSet struct {
	Paths   []string `json:"paths,omitempty"`
	Total   int      `json:"total"`
	Source  string   `json:"source,omitempty" enum:"changed,wrote"`
	Omitted int      `json:"omitted,omitempty"`
}

// TaskSpend is what a task spent, as recorded. Basis is "priced" (the record
// carries dollars), "unpriced" (it carries tokens and no price, so the dollars
// are unknown) — a task with neither carries no TaskSpend at all.
type TaskSpend struct {
	USD    float64 `json:"usd,omitempty"`
	Tokens int     `json:"tokens,omitempty"`
	Basis  string  `json:"basis" enum:"priced,unpriced"`
}

// TaskChecks is the declared checks' recorded runs for a task: how many passed
// (exit 0) and how many did not. Absent when no run was recorded.
type TaskChecks struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// GraphEdge is one waits-on link. From waits on To.
type GraphEdge struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	ToRef TaskRef `json:"to_ref"`
	Kind  string  `json:"kind" enum:"waits_on"`
	Via   string  `json:"via" enum:"record,external"`
	// Target says how To stands against the scope; ToState is the prerequisite's
	// state word when it is known, including for an external target that could
	// be read. InCycle says the edge is on a waits-on cycle.
	Target  string `json:"target" enum:"node,external,missing"`
	ToState string `json:"to_state,omitempty"`
	InCycle bool   `json:"in_cycle,omitempty"`
}

// GraphCluster is one grouping of the scope's tasks: a sub-crew or a task group.
// Total is the true member count (a crew cluster counts every crew beneath it)
// and ByState counts them by state word.
type GraphCluster struct {
	Kind   string `json:"kind" enum:"crew,group"`
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Parent string `json:"parent,omitempty"`
	Total  int    `json:"total"`
	// Direct counts the tasks that sit in this cluster itself and not in one
	// beneath it.
	Direct  int            `json:"direct"`
	ByState map[string]int `json:"by_state,omitempty"`
}

// CrewFact is one crew of a crew scope, and the scope's own crew first.
type CrewFact struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Parent   string     `json:"parent,omitempty"`
	Manager  string     `json:"manager,omitempty"`
	Hue      *float64   `json:"hue,omitempty"`
	State    string     `json:"state,omitempty" enum:"open,closed"`
	ClosedAt *time.Time `json:"closed_at,omitempty"`
	Members  int        `json:"members"`
}

// MemberFact is one conversation in a crew of the scope, with what an org chart
// needs: where it sits, who it answers to and what it leads, and how its work
// stands.
type MemberFact struct {
	Agent     string `json:"agent"`
	Handle    string `json:"handle,omitempty"`
	Title     string `json:"title,omitempty"`
	Crew      string `json:"crew"`
	Manager   bool   `json:"manager,omitempty"`
	ReportsTo string `json:"reports_to,omitempty"`
	// Leads is the sub-crew this member manages.
	Leads    string     `json:"leads,omitempty"`
	JoinedAt *time.Time `json:"joined_at,omitempty"`
	// Tasks counts this member's tasks in the scope by state word; Total is the
	// true count.
	Tasks MemberTasks `json:"tasks"`
}

// MemberTasks counts one member's tasks.
type MemberTasks struct {
	Total   int            `json:"total"`
	ByState map[string]int `json:"by_state,omitempty"`
}

// GraphFigure is one number with what it was counted against. Value is the sum
// of what is known; Counted is how many of Of tasks contributed. Known is
// Counted > 0 and Partial is Counted < Of, so an unknown never reads as zero and
// a sum over some of the work never reads as the whole.
type GraphFigure struct {
	Value   float64 `json:"value"`
	Known   bool    `json:"known"`
	Partial bool    `json:"partial,omitempty"`
	Counted int     `json:"counted"`
	Of      int     `json:"of"`
}

// GraphMetrics are the scope's figures, every one an engine fact and none an
// estimate: counted from recorded state, never forecast and never priced from
// today's catalogue.
type GraphMetrics struct {
	// Tasks is every task in the scope; Subtasks the ones handed out by another
	// task; Members the conversations of a crew scope (absent for the others).
	Tasks    GraphFigure  `json:"tasks"`
	Subtasks GraphFigure  `json:"subtasks"`
	Members  *GraphFigure `json:"members,omitempty"`
	// LandedTasks counts tasks whose work came home (merged or in place).
	// LandedFiles counts the distinct paths those tasks changed; it is partial
	// when a landed task's record carries no path list.
	LandedTasks GraphFigure `json:"landed_tasks"`
	LandedFiles GraphFigure `json:"landed_files"`
	// ChecksPassed counts declared checks with a recorded exit of 0, over the
	// declared checks that have a recorded run.
	ChecksPassed GraphFigure `json:"checks_passed"`
	// Spend is the dollars the recorded bills add up to, over the tasks that
	// carry a bill. Unpriced counts tasks with tokens and no price.
	Spend    GraphFigure `json:"spend"`
	Unpriced int         `json:"unpriced,omitempty"`
	// From and To bound the work in time: the earliest recorded admission or
	// start and the latest recorded end. Absent when no task recorded one.
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
}

// Diagnostics are what the build found wrong with the facts, bounded.
type Diagnostics struct {
	// Cycles are waits-on cycles, at most [MaxCycles], each at most
	// [MaxCycleNodes] tasks; CyclesTotal is the true count. A cycle is reported
	// and left in place: the engine refuses to write one, so one here means a
	// record that was not written by it.
	Cycles      []Cycle `json:"cycles,omitempty"`
	CyclesTotal int     `json:"cycles_total,omitempty"`
	// MissingParents counts tasks whose recorded parent is not in their owner's
	// graph; ParentCycles counts tasks on a family loop.
	MissingParents int `json:"missing_parents,omitempty"`
	ParentCycles   int `json:"parent_cycles,omitempty"`
	// MissingTargets and ExternalTargets count edges by how their target stands.
	MissingTargets  int `json:"missing_targets,omitempty"`
	ExternalTargets int `json:"external_targets,omitempty"`
}

// Cycle is the tasks of one waits-on cycle.
type Cycle struct {
	Tasks []string `json:"tasks"`
	Total int      `json:"total"`
}
