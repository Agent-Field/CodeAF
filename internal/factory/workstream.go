package factory

import (
	"slices"
	"time"
)

// ── AN ORGANIZATION'S WORKSTREAMS ──────────────────────────────────────────
//
// An organization's workstreams live on its server (the store keeps them in
// one SQLite database beside the go-ahead policies), never in someone's app. A
// workstream has exactly ONE owner, a person or a team, who carries its budget
// and go-ahead approval, and it can be shared with several teams, the way a
// label is put on a mail rather than a mail moved into a folder.
//
// These are the shapes the store keeps and the wire answers (api aliases
// them), so a page, a decision or a source is spelled in one place.

// OrgWorkstream is one organization workstream as the server keeps it. Rev
// counts every save, so a writer that read an older one is refused rather
// than silently overwriting what someone else wrote.
type OrgWorkstream struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Stage string          `json:"stage"`
	Owner WorkstreamOwner `json:"owner"`
	// Shares are who else may reach it, and with which role ([Share]).
	Shares []Share `json:"shares"`
	// SharedWith is the teams-only share list kept before roles. The store
	// folds it into Shares as edit on every read ([OrgWorkstream.FoldShares])
	// and never writes it again.
	SharedWith []string `json:"shared_with,omitempty"`
	// Private is an item only its owner reaches: it has a person owner and
	// no shares, and the operator is not told it exists (OWNER-DECISIONS §8).
	Private bool `json:"private,omitempty"`
	// Space marks a person's own space inside the organization ("My space",
	// serve/myspace.go): one per person, private to them, holding their pages,
	// canvases and chats and no tasks. It is never listed with the
	// workstreams, never shared, moved, nested or archived, and the operator
	// sees only its totals.
	Space bool `json:"space,omitempty"`
	// Agents are the agents this workstream lists under People and rules ›
	// Agents (org_agent.go): links, never copies. Only these may be its doers.
	Agents    []WorkstreamAgentLink `json:"agents,omitempty"`
	Purpose   string                `json:"purpose"`
	Pages     []WorkstreamPage      `json:"pages"`
	Decisions []WorkstreamDecision  `json:"decisions"`
	Sources   []WorkstreamSource    `json:"sources"`
	Links     []string              `json:"links"`
	// Jobs are the factory jobs on this server that belong to the workstream,
	// by id, in the order they were added. A job belongs to at most one
	// workstream: adding it to another takes it out of the first.
	Jobs []int `json:"jobs"`
	// BudgetUSD is what the workstream may spend, nil when it carries no
	// budget. What it has spent is never kept here: spend is posted by whoever
	// ran the work, and summing the posts keeps a spend from moving the rev a
	// writer read.
	BudgetUSD *float64 `json:"budget_usd,omitempty"`
	CreatedBy string   `json:"created_by"`
	// FromLocalID is the Personal workstream an import came from, and
	// ImportedBy who moved it in: together they make a repeated import answer
	// the copy already made instead of making a second one.
	FromLocalID string `json:"from_local_id,omitempty"`
	ImportedBy  string `json:"imported_by,omitempty"`
	// FromLocalLinks are the laptop ids an import linked to, kept so a linked
	// workstream that moves in later is linked by its new id too.
	FromLocalLinks []string   `json:"from_local_links,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	Rev            int        `json:"rev"`
	// Parent, Steward and Rules are its structure (workstream_structure.go):
	// the workstream it sits inside, the person who looks after it, and how
	// it runs. Rules nil is [DefaultRules] for its steward.
	Parent  string           `json:"parent,omitempty"`
	Steward string           `json:"steward,omitempty"`
	Rules   *WorkstreamRules `json:"rules,omitempty"`
	// PeopleAdded is when each person or team ("person:<handle>",
	// "team:<id>") was added, for the line "Added Nov 2"; one added before it
	// was kept has none, and says nothing.
	PeopleAdded map[string]string `json:"people_added,omitempty"`
	// PeopleAddedBy is who added each of them (a handle), by the same keys,
	// so the person added can be told "Ann added you"; absent before it was
	// kept, and for the people a workstream started with.
	PeopleAddedBy map[string]string `json:"people_added_by,omitempty"`
}

// OrgWorkstreams are every organization workstream, by id.
type OrgWorkstreams map[string]OrgWorkstream

// WorkstreamOwner is who owns a workstream: one person (their handle) or one
// team (its id).
type WorkstreamOwner struct {
	Kind string `json:"kind" jsonschema:"enum=person,enum=team"`
	ID   string `json:"id"`
}

// WorkstreamPage is one page of a workstream's notes.
type WorkstreamPage struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Parent    string `json:"parent,omitempty"`
	BodyMD    string `json:"body_md"`
	Kind      string `json:"kind" jsonschema:"enum=overview,enum=page,enum=decisions,enum=spec,enum=runbook,enum=research"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
	// Rev counts every change to this one page, so two people changing two
	// different pages never refuse each other; a page's place in the list is
	// its order.
	Rev int `json:"rev"`
}

// WorkstreamDecision is one decision a workstream keeps.
type WorkstreamDecision struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state" jsonschema:"enum=kept,enum=proposed,enum=superseded,enum=withdrawn"`
	At    string `json:"at"`
	By    string `json:"by"`
	// ByName is the maker's name, as the list shows it.
	ByName string `json:"by_name,omitempty"`
	// From is where it came from: an answered Ask, a request that changed
	// shared work, or a rule set in People and rules.
	From *DecisionFrom `json:"from,omitempty"`
	// ReplacedBy is the decision that replaced this one.
	ReplacedBy string `json:"replaced_by,omitempty"`
	// ChangedBy and ChangedAt are who withdrew or replaced it, and when.
	ChangedBy *PersonRef `json:"changed_by,omitempty"`
	ChangedAt string     `json:"changed_at,omitempty"`
}

// WorkstreamSource is one thing a workstream reads from.
type WorkstreamSource struct {
	ID    string `json:"id"`
	Kind  string `json:"kind" jsonschema:"enum=repo,enum=url,enum=file,enum=chat"`
	Ref   string `json:"ref"`
	Title string `json:"title"`
}

// The words each enumerated field may hold.
var (
	WorkstreamStages        = []string{"shaping", "building", "ongoing", "place", "done"}
	WorkstreamPageKinds     = []string{"overview", "page", "decisions", "spec", "runbook", "research"}
	WorkstreamDecisionState = []string{"kept", "proposed", "superseded", "withdrawn"}
	WorkstreamSourceKinds   = []string{"repo", "url", "file", "chat"}
)

// FoldShares moves the old teams-only SharedWith into Shares as edit, once,
// so every reader after the store sees one share list.
func (w *OrgWorkstream) FoldShares() {
	for _, s := range TeamShares(w.SharedWith) {
		if !slices.ContainsFunc(w.Shares, func(cur Share) bool { return cur.Kind == s.Kind && cur.ID == s.ID }) {
			w.Shares = append(w.Shares, s)
		}
	}
	w.SharedWith = nil
	if w.Shares == nil {
		w.Shares = []Share{}
	}
}
