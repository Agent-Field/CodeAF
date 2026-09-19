package wsapi

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Organizer output is this type, not a map and not SQL. no-action is first-class.
const (
	PlanNoAction     = "no-action"
	PlanAdd          = "add"
	PlanRemove       = "remove"
	PlanMove         = "move"
	PlanCreateFolder = "create-folder"

	ScoreBM25      = "bm25"
	ScoreEmbed     = "embed"
	ScoreExpansion = "expansion"

	delayedDetail = "discovery delayed"
)

// EvidenceRef cites a passage the organizer actually read.
type EvidenceRef struct {
	SourceRef, PassageHash, Quote string
}

// Action is one typed membership edit inside an ActionPlan.
type Action struct {
	Kind                       string
	CollectionID, FromID, ToID string
	Ref                        workspace.Ref
	FolderName, Purpose        string
	ParentIDs                  []string
	ExpectedRevision           int // collection; zero = no check (forbidden on organizer apply)
	ExpectedFrom, ExpectedTo   int
	ExpectedRootRevision       int
	ExpectedGuidanceRevision   int
	Evidence                   []EvidenceRef
	Reason                     string
	IdempotencyKey             string
}

// ActionPlan is the organizer's only write shape. Kind==PlanNoAction ⇒ Actions empty.
type ActionPlan struct {
	Kind, ChatID, SourceRev, Model, PromptVersion string
	Actions                                       []Action
	Evidence                                      []EvidenceRef
	Degraded                                      bool
}

// InstructRequest is person-origin standing guidance for a folder or Root.
type InstructRequest struct {
	ScopeID, Text string
	Provenance    workspace.Provenance // person origin only
}

// GuidanceItem is one applicable instruction as the main turn loads it.
type GuidanceItem struct {
	ScopeID, Name, Text, Origin, Actor, SourceRef string
	Revision                                      int
}

// GuidanceRev is one scope's revision for the guidance checkpoint.
type GuidanceRev struct {
	ScopeID  string
	Revision int
}

// GuidanceSnapshot is the checkpoint the main turn re-reads before mutations.
type GuidanceSnapshot struct {
	RootRevision int
	Guidance     []GuidanceRev
}

// EffectiveGuidance is parents + ancestors + Root, loaded directly, not ranked.
type EffectiveGuidance struct {
	Items    []GuidanceItem // deduped by ScopeID; Root once
	Snapshot GuidanceSnapshot
	Conflict bool // incompatible instructions; do not silently pick recency/path
}

// SearchQuery is the hybrid evidence lookup the organizer and tools share.
type SearchQuery struct {
	Query, SessionID, ConversationID string
	Limit                            int
}

// SearchHit is one cited passage. ScoreKind is bm25, embed, or expansion.
type SearchHit struct {
	Ref, SessionID, Passage, ScoreKind string
	Degraded                           bool
}

// ApplyResult is what ApplyActionPlan wrote. Applied is empty for no-action.
type ApplyResult struct {
	PlanID  string
	Applied []workspace.MembershipEvent
}

// IndexView is software counters, never a fake 100%.
type IndexView struct {
	Passages, Vectors int
	Delayed, Degraded bool
	Detail            string
}

// Discoverer is the injected discovery index, the same pattern as Inventory.
// This package never imports wsdiscover: wiring binds the real store later.
type Discoverer interface {
	SearchLexical(ctx context.Context, query string, limit int) ([]SearchHit, error)
	SearchEmbed(ctx context.Context, query string, limit int) ([]SearchHit, error)
	IndexProgress(ctx context.Context) (IndexView, error)
}
