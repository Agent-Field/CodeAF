// Package store owns Aforge's durable, append-only task graph.
//
// Events are the source of truth. Nodes and edges are queryable materialized
// views updated in the same SQLite transaction as the event that changed them.
// Any process may open the database: WAL keeps readers independent, and claim
// tokens make worker ownership a compare-and-swap rather than process state.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	_ "modernc.org/sqlite"
)

const (
	// RootID is the one permanent spine root. It is created with a new store and
	// is never itself scheduled or folded.
	RootID = "root"

	// MaxDigestBytes keeps a digest small enough to route through the graph.
	// Large results belong in the content-addressed store and are referenced by
	// pointers instead. It is a bound on what a reader TAKES — a dependency
	// input, a partial quoted into a prompt, a receipt line — and every such
	// reader applies it at the read.
	MaxDigestBytes = 4 << 10

	// MaxSummaryBytes bounds what a settled node records as its own outcome.
	//
	// It is deliberately not MaxDigestBytes. A job root's summary is not a
	// digest of the deliverable, it IS the deliverable: it is what the thread
	// announces, what `aforge do` prints, and what an export carries. Bounding
	// the record at the routing bound made a guillotine out of a budget — a
	// 697-second PR review lost its approve/request-changes verdict mid-word at
	// 4,096 bytes, in the store, before any surface could have shown it. So the
	// record is bounded by what the thread can carry, and the readers that need
	// something smaller keep taking MaxDigestBytes of it.
	MaxSummaryBytes = MaxMessageBytes
)

// Status is the scheduling state of a node.
type Status string

const (
	Pending   Status = "pending"
	Claimed   Status = "claimed"
	Running   Status = "running"
	Done      Status = "done"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// Origin says who introduced a subtree onto the spine.
type Origin string

const (
	OriginUser    Origin = "user"
	OriginTrigger Origin = "trigger"
	OriginSelf    Origin = "self"
)

// EdgeKind says how one node bears on another. FeedsInto and Blocks are hard
// scheduling dependencies; Suggests routes a soft hint and never delays work.
type EdgeKind string

const (
	FeedsInto EdgeKind = "feeds_into"
	Blocks    EdgeKind = "blocks"
	Suggests  EdgeKind = "suggests"
)

// EventKind names state transitions in the append-only journal.
type EventKind string

const (
	EventSpineCreated   EventKind = "spine_created"
	EventSpineRepaired  EventKind = "spine_repaired"
	EventSubtreeSpliced EventKind = "subtree_spliced"
	EventNodeClaimed    EventKind = "node_claimed"
	EventNodeStarted    EventKind = "node_started"
	EventNodeCompleted  EventKind = "node_completed"
	EventNodeFailed     EventKind = "node_failed"
	EventNodeReleased   EventKind = "node_released"
	EventSubtreeFolded  EventKind = "subtree_folded"
	EventEdgeAdded      EventKind = "edge_added"
	EventEdgeRemoved    EventKind = "edge_removed"
	EventNodeAmended    EventKind = "node_amended"
	EventNodeReparented EventKind = "node_reparented"
	EventNodeCancelled  EventKind = "node_cancelled"
	// Surgery controls are separate from the public status enum. Holds keep a
	// pending node visibly pending while making it unschedulable; cancel
	// requests let the live claim owner release cooperatively at a turn
	// boundary before the ordinary cancelled transition lands.
	EventNodeCancelRequested EventKind = "node_cancel_requested"
	EventNodeHeld            EventKind = "node_held"
	EventNodeResumed         EventKind = "node_resumed"
	EventNodePriorityChanged EventKind = "node_priority_changed"

	// Thread events: the conversation and its asynchronous mutation requests
	// live in the same journal as the graph they act on.
	EventMessagePosted         EventKind = "message_posted"
	EventCommandRequested      EventKind = "command_requested"
	EventCommandResolved       EventKind = "command_resolved"
	EventSeenTouched           EventKind = "seen_touched"
	EventAgentQuestionQueued   EventKind = "agent_question_queued"
	EventAgentQuestionSurfaced EventKind = "agent_question_surfaced"
	EventAgentQuestionResolved EventKind = "agent_question_resolved"

	// Standing-watch policy is global to this brain file. Offered is the
	// durable never-ask-twice gate; enabled and declined are the user's final
	// decision; pass is one completed headless wake observation.
	EventStandingWatchOffered  EventKind = "standing_watch_offered"
	EventStandingWatchEnabled  EventKind = "standing_watch_enabled"
	EventStandingWatchDeclined EventKind = "standing_watch_declined"
	// EventStandingWatchStoodDown is the reverse gear. Enabled and declined
	// used to be terminal by construction, which made an unattended-presence
	// consent one the product accepted and structurally refused to give back —
	// while the timer repaired itself against the user's own hands every five
	// minutes. Standing down is a fifth state rather than a rewrite of the
	// fourth because the journal is append-only and the fact that the user once
	// said yes is part of the history.
	EventStandingWatchStoodDown EventKind = "standing_watch_stood_down"
	EventStandingWatchPass      EventKind = "standing_watch_pass"

	// Usage and surprise are journaled separately because a planned leaf's
	// prediction becomes known when the complete plan lands, after its spend.
	EventUsageRecorded    EventKind = "usage_recorded"
	EventSurpriseRecorded EventKind = "surprise_recorded"
	// EventSelfReceipt is the cost-and-learning receipt produced when one
	// self-originated splice settles.
	EventSelfReceipt EventKind = "self_receipt"
	// EventSelfInquiryRetired records the deterministic two-strike policy
	// decision that stops an inquiry line which is not earning learning rent.
	EventSelfInquiryRetired EventKind = "self_inquiry_retired"

	// EventRailRaised records the user's decision to extend today's dollar
	// ceiling. The journal is the policy record; no process-local flag resumes
	// work.
	EventRailRaised EventKind = "rail_raised"

	// Overrun deferrals preserve a landed partial whose repair could not be
	// admitted at the rail. Resumption is a separate event so a rebuild can
	// recover exactly the continuations that still need to be spliced.
	EventOverrunDeferred EventKind = "overrun_deferred"
	EventOverrunResumed  EventKind = "overrun_resumed"

	// EventDeliveryGate is the final judge's evidence about one delivered job.
	EventDeliveryGate EventKind = "delivery_gate"

	// EventFactLearned is one durable fact distilled from finished work.
	EventFactLearned EventKind = "fact_learned"
	// EventFactActivated records execution promoting a skill candidate.
	EventFactActivated EventKind = "fact_activated"
	// EventFactSuperseded retires one fact in favour of a newer one.
	EventFactSuperseded EventKind = "fact_superseded"
	// EventFactInjected attributes a batch of notebook facts to one node's
	// context.
	EventFactInjected EventKind = "fact_injected"
	// EventFactQuarantined removes a suspect fact from retrieval without
	// deleting it.
	EventFactQuarantined EventKind = "fact_quarantined"
	// EventFactRestored returns a quarantined fact to active retrieval.
	EventFactRestored EventKind = "fact_restored"
	// EventScopeAliased shelves one emergent scope under another while keeping
	// the old name valid as a retrieval cue.
	EventScopeAliased EventKind = "scope_aliased"

	// EventRetrospectiveCheckpointed records how much settled top-level work
	// the periodic retrospective has already considered.
	EventRetrospectiveCheckpointed EventKind = "retrospective_checkpointed"
	// EventResidentWatermarked records how far one resident lane has already
	// got. The settle lane's cursor used to live only in the reconciler's
	// memory, which made every restart step over whatever landed while nothing
	// was ticking; a lane watermark is the same durable answer the
	// retrospective already had.
	EventResidentWatermarked EventKind = "resident_watermarked"
	// EventAssumedWithDefault records a VOI-gated skipped ask for later correction matching.
	EventAssumedWithDefault EventKind = "assumed_with_default"
	// EventParameterChanged is the sole bounded self-tuning mutation surface.
	EventParameterChanged EventKind = "parameter_changed"

	// Charter events keep standing intent and every watch decision in the same
	// append-only policy record as the work a firing creates.
	EventCharterCreated          EventKind = "charter_created"
	EventCharterRevised          EventKind = "charter_revised"
	EventCharterStatusChanged    EventKind = "charter_status_changed"
	EventCharterWatchAdvanced    EventKind = "charter_watch_advanced"
	EventCharterWoken            EventKind = "charter_woken"
	EventSentinelChecked         EventKind = "sentinel_checked"
	EventCharterFired            EventKind = "charter_fired"
	EventCharterFiringBlocked    EventKind = "charter_firing_blocked"
	EventCharterFiringDeferred   EventKind = "charter_firing_deferred"
	EventCharterProposalDeclined EventKind = "charter_proposal_declined"
	EventCharterFiringProposed   EventKind = "charter_firing_proposed"
	EventCharterFiringDeclined   EventKind = "charter_firing_declined"
	EventCharterFiringReviewed   EventKind = "charter_firing_reviewed"
	EventCharterPromoted         EventKind = "charter_promoted"
	EventCharterDemoted          EventKind = "charter_demoted"

	// Service events are the durable ownership record for processes promoted
	// out of a leaf's background-job registry.
	EventServicePromoted  EventKind = "service_promoted"
	EventServiceAdopted   EventKind = "service_adopted"
	EventServiceStopped   EventKind = "service_stopped"
	EventServiceFailed    EventKind = "service_failed"
	EventServiceRestarted EventKind = "service_restarted"
	EventServiceRested    EventKind = "service_rested"
)

var (
	ErrNotFound    = errors.New("node not found")
	ErrClaimLost   = errors.New("claim is stale or no longer owned")
	ErrNotReady    = errors.New("node is not ready")
	ErrInvalid     = errors.New("invalid graph mutation")
	ErrOpenChild   = errors.New("node has an open child")
	ErrOpenSubtree = errors.New("subtree is not complete")
	// ErrFactVetoed is the store refusing to re-derive a belief the user threw
	// away. It is an error rather than a quiet return because the quiet return
	// was a lie the callers believed: recordFact handed back the quarantined row
	// with a nil error and no event, so the reconciler announced a learning
	// moment for a write that never happened and pointed a supersession at a
	// dead row. The refusal is still not a failure — the returned Fact is the
	// standing retraction — but a caller now has to look at it to miss it.
	ErrFactVetoed = errors.New("fact was retracted by the user and may not be re-derived")
)

// Provenance is stamped onto every node admitted by one splice. Intent is
// deliberately stored verbatim: later planning and folding may interpret it,
// but the store never rewrites what was asked.
type Provenance struct {
	Origin    Origin `json:"origin"`
	SessionID string `json:"session_id,omitempty"`
	Intent    string `json:"intent"`
	// CharterID points work back to the standing responsibility whose firing or
	// self-maintenance inquiry admitted it. It is empty for ordinary user work.
	CharterID string `json:"charter_id,omitempty"`
	// Attachments are user-supplied image paths kept separate from visible
	// intent text so every leaf can receive them as multimodal content.
	Attachments []string `json:"attachments,omitempty"`
	// TrialOf is the fact sequence of the unsettled pair this subtree tests.
	// Zero means the splice is ordinary work.
	TrialOf int64 `json:"trial_of,omitempty"`
	// RetryOf links a freshly spliced retry to the failed/cancelled node it
	// supersedes. The predecessor stays immutable and fully inspectable.
	RetryOf string `json:"retry_of,omitempty"`
	// ServiceIntent records the compiler's deterministic recognition that the
	// user asked for a running thing. It is consent provenance, not a display
	// hint, and therefore travels through the splice event and Rebuild.
	ServiceIntent bool `json:"service_intent,omitempty"`
	// WorkModel is the model the user named for this job in their own words
	// ("with the better model", "use gemini"). Empty means the surface's
	// current work model serves, as always. It is provenance rather than
	// configuration: the leaf that ran is inseparable from the model asked for.
	WorkModel string `json:"work_model,omitempty"`
	// PlanModel is the model that actually structured this job, recorded only
	// when it was not the model the job's work runs on. Empty — which is nearly
	// every job — means the plan slot followed the work slot, the default the
	// whole product is built around, and a surface that shows it says nothing.
	// It is provenance for the same reason WorkModel is: a graph's shape is
	// inseparable from the model that drew it, and a slot moved an hour later
	// must not be able to rewrite the answer to "who planned this".
	PlanModel string `json:"plan_model,omitempty"`
	// Craft names the learned workflow this subtree compiled from, as
	// "name@commit". Empty is ordinary planned work. Every node of a craft run
	// carries it: survival is measured per workflow version, so the version a
	// leaf actually ran under must be as durable as the leaf itself.
	Craft string `json:"craft,omitempty"`
	// Subharness names the worker chosen for this whole subtree — the compiler's
	// judgement that the essence of this job is what one specialist is for.
	// Empty is the generalist and is nearly every job. It is provenance for the
	// same reason WorkModel is: the leaf that ran is inseparable from what ran
	// it, so the choice is made once, at splice, and survives a restart rather
	// than being re-decided by whatever the process happens to have registered
	// when the leaf finally starts.
	Subharness string `json:"subharness,omitempty"`
}

// Need is one incoming edge named by a node specification.
type Need struct {
	NodeID string   `json:"node_id"`
	Kind   EdgeKind `json:"kind"`
}

// NodeSpec is one node to admit. Exactly one node in a Subtree has an empty
// Parent; Splice attaches that node to the parent argument. Every other Parent
// names another node in the same subtree.
type NodeSpec struct {
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Brief  string `json:"brief"`
	Stage  int    `json:"stage"`
	Needs  []Need `json:"needs,omitempty"`

	// Title is a few-word display name for surfaces that cannot afford the
	// brief; empty is valid and means "derive from the brief".
	Title string `json:"title,omitempty"`

	// Group names the planning container this node expanded out of. It is
	// provenance for display — execution reads only Parent and Needs.
	Group string `json:"group,omitempty"`

	// Subharness names the worker this one node was sized for, when the sizing
	// pass judged it atomic for a specialist rather than for the generalist.
	// Empty inherits the splice's own choice, which is empty for nearly every
	// job — one node of a subtree may be a coding job while its siblings are
	// not, and the graph is where that difference lives.
	Subharness string `json:"subharness,omitempty"`
}

// Subtree is the atomic unit of admission.
type Subtree struct {
	Nodes []NodeSpec `json:"nodes"`
}

// Node is the durable scheduling view of one graph node.
type Node struct {
	ID     string
	Parent string
	Brief  string
	Title  string
	Group  string
	// Subharness is the settled answer to "what runs this leaf": the node's own
	// choice where it made one, the splice's otherwise. It is resolved once, at
	// admission, so every dispatch path reads one field and cannot disagree
	// with another about which worker a node was promised.
	Subharness string
	Stage      int
	Status     Status
	Owner      string
	ClaimToken uint64
	Attempt    uint64
	Summary    string
	Error      string
	// Held and CancelRequested are journal-derived scheduling controls. They
	// intentionally do not add presentation-only statuses to the graph.
	Held            bool
	CancelRequested bool
	Priority        int

	Provenance   Provenance
	CreatedSeq   int64
	CreatedOrder int
	UpdatedSeq   int64
	StartedAt    time.Time
	FinishedAt   time.Time

	// Folded marks historical nodes replaced in the active view. FoldRoot is
	// the compact representative that remains visible in place of its subtree.
	Folded       bool
	FoldRoot     bool
	FoldDigest   string
	FoldPointers []string
}

// Edge points from an input to the node that consumes or is constrained by it.
type Edge struct {
	From         string
	To           string
	Kind         EdgeKind
	CreatedSeq   int64
	CreatedOrder int
}

// Event is one immutable journal entry.
type Event struct {
	Seq     int64
	Time    time.Time
	NodeID  string
	Kind    EventKind
	Payload json.RawMessage
}

// Claim is the complete authority a worker needs to mutate one claimed node.
// Both owner and token must continue to match; release and reassignment make an
// older Claim permanently unusable.
type Claim struct {
	ID    string
	Owner string
	Token uint64
}

// Snapshot is a deterministic copy of the full materialized views, including
// folded historical nodes. It is useful for inspection and rebuild checks.
type Snapshot struct {
	Nodes []Node
	Edges []Edge
}

// Store is one handle onto the shared SQLite graph.
type Store struct {
	db    *sql.DB
	blobs *cas.Store
}

const schema = `
CREATE TABLE IF NOT EXISTS events (
    seq       INTEGER PRIMARY KEY AUTOINCREMENT,
    ts        TEXT NOT NULL,
    node_id   TEXT NOT NULL,
    kind      TEXT NOT NULL,
    payload   JSON NOT NULL CHECK (json_valid(payload))
);

CREATE TABLE IF NOT EXISTS nodes (
    id             TEXT PRIMARY KEY,
    parent_id      TEXT REFERENCES nodes(id),
    brief          TEXT NOT NULL,
    stage          INTEGER NOT NULL CHECK (stage >= 0),
    status         TEXT NOT NULL CHECK (status IN ('pending', 'claimed', 'running', 'done', 'failed', 'cancelled')),
    owner          TEXT NOT NULL DEFAULT '',
    claim_token    INTEGER NOT NULL DEFAULT 0 CHECK (claim_token >= 0),
    attempt        INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    summary        TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    origin         TEXT NOT NULL CHECK (origin IN ('user', 'trigger', 'self')),
    session_id     TEXT,
    intent         TEXT NOT NULL,
    charter_id     TEXT NOT NULL DEFAULT '',
    trial_of       INTEGER NOT NULL DEFAULT 0 CHECK (trial_of >= 0),
	retry_of       TEXT NOT NULL DEFAULT '',
	service_intent INTEGER NOT NULL DEFAULT 0 CHECK (service_intent IN (0, 1)),
	work_model     TEXT NOT NULL DEFAULT '',
	plan_model     TEXT NOT NULL DEFAULT '',
	craft          TEXT NOT NULL DEFAULT '',
	subharness     TEXT NOT NULL DEFAULT '',
	splice_subharness TEXT NOT NULL DEFAULT '',
    attachments    JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments)),
    created_seq    INTEGER NOT NULL REFERENCES events(seq),
    created_order  INTEGER NOT NULL CHECK (created_order >= 0),
    updated_seq    INTEGER NOT NULL REFERENCES events(seq),
    started_at     TEXT,
    finished_at    TEXT,
    folded         INTEGER NOT NULL DEFAULT 0 CHECK (folded IN (0, 1)),
    fold_root      INTEGER NOT NULL DEFAULT 0 CHECK (fold_root IN (0, 1)),
    fold_digest    TEXT NOT NULL DEFAULT '',
    fold_pointers  JSON NOT NULL DEFAULT '[]' CHECK (json_valid(fold_pointers)),
    title          TEXT NOT NULL DEFAULT '',
    grp            TEXT NOT NULL DEFAULT '',
	held           INTEGER NOT NULL DEFAULT 0 CHECK (held IN (0, 1)),
	cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
	priority       INTEGER NOT NULL DEFAULT 0,
    CHECK (fold_root = 0 OR folded = 1)
);

CREATE TABLE IF NOT EXISTS edges (
    from_id      TEXT NOT NULL REFERENCES nodes(id),
    to_id        TEXT NOT NULL REFERENCES nodes(id),
    kind         TEXT NOT NULL CHECK (kind IN ('feeds_into', 'blocks', 'suggests')),
    created_seq  INTEGER NOT NULL REFERENCES events(seq),
    created_order INTEGER NOT NULL CHECK (created_order >= 0),
    PRIMARY KEY (from_id, to_id, kind)
);

CREATE UNIQUE INDEX IF NOT EXISTS nodes_one_spine_root
    ON nodes ((1)) WHERE parent_id IS NULL;
CREATE INDEX IF NOT EXISTS nodes_parent ON nodes (parent_id);
CREATE INDEX IF NOT EXISTS nodes_ready ON nodes (status, folded, created_seq, created_order);
CREATE INDEX IF NOT EXISTS edges_to_kind ON edges (to_id, kind);
CREATE INDEX IF NOT EXISTS events_node_seq ON events (node_id, seq);
CREATE INDEX IF NOT EXISTS events_kind_ts ON events (kind, ts);

CREATE TRIGGER IF NOT EXISTS events_no_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TRIGGER IF NOT EXISTS events_no_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;
`

type spinePayload struct {
	ID         string     `json:"id"`
	Brief      string     `json:"brief"`
	Provenance Provenance `json:"provenance"`
}

// Open opens or creates the store at path. WAL is persistent database state;
// busy_timeout and foreign keys are connection-local and therefore live in the
// DSN so every pooled connection receives them.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("open store: %w: empty path", ErrInvalid)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	u := url.URL{Scheme: "file", Path: absolute}
	query := u.Query()
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	closeOnError := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return closeOnError(fmt.Errorf("open store: %w", err))
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&journalMode); err != nil {
		return closeOnError(fmt.Errorf("enable WAL: %w", err))
	}
	if !strings.EqualFold(journalMode, "wal") {
		return closeOnError(fmt.Errorf("enable WAL: SQLite selected %q", journalMode))
	}
	if _, err := db.Exec(schema); err != nil {
		return closeOnError(fmt.Errorf("initialize store schema: %w", err))
	}
	if _, err := db.Exec(threadSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize thread schema: %w", err))
	}
	if err := migrateThreadSchema(db); err != nil {
		return closeOnError(fmt.Errorf("migrate thread schema: %w", err))
	}
	// After the thread migration, never before it: the backfill reads the
	// message view, and the view's shape is what that migration settles.
	if err := migrateMessagesFTS(db); err != nil {
		return closeOnError(fmt.Errorf("migrate conversation index: %w", err))
	}
	if _, err := db.Exec(agentQuestionSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize agent question schema: %w", err))
	}
	if err := migrateAgentQuestionSchema(db); err != nil {
		return closeOnError(fmt.Errorf("migrate agent question schema: %w", err))
	}
	if _, err := db.Exec(usageSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize usage schema: %w", err))
	}
	if err := migrateUsageSchema(db); err != nil {
		return closeOnError(fmt.Errorf("migrate usage schema: %w", err))
	}
	if _, err := db.Exec(surpriseSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize surprise schema: %w", err))
	}
	if _, err := db.Exec(selfReceiptSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize self receipt schema: %w", err))
	}
	if _, err := db.Exec(charterSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize charter schema: %w", err))
	}
	if _, err := db.Exec(serviceSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize service schema: %w", err))
	}
	if _, err := db.Exec(factsSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize facts schema: %w", err))
	}
	if _, err := db.Exec(scopeAliasesSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize scope aliases schema: %w", err))
	}
	if _, err := db.Exec(retrospectiveSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize retrospective schema: %w", err))
	}
	if _, err := db.Exec(residentWatermarkSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize resident watermark schema: %w", err))
	}
	if _, err := db.Exec(metaParameterSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize meta parameter schema: %w", err))
	}
	if err := migrateFactsSchema(db); err != nil {
		return closeOnError(fmt.Errorf("migrate facts schema: %w", err))
	}
	if err := migrateNodesSchema(db); err != nil {
		return closeOnError(fmt.Errorf("migrate nodes schema: %w", err))
	}
	if err := migrateGraphFTS(db); err != nil {
		return closeOnError(fmt.Errorf("migrate graph index: %w", err))
	}

	blobs, err := cas.New(filepath.Join(filepath.Dir(absolute), "cas"))
	if err != nil {
		return closeOnError(fmt.Errorf("open content store: %w", err))
	}
	store := &Store{db: db, blobs: blobs}
	if err := store.ensureSpine(); err != nil {
		return closeOnError(err)
	}
	return store, nil
}

// Close releases this process's connections. The database remains immediately
// resumable by any other handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSpine() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("initialize spine: %w", err)
	}
	defer tx.Rollback()

	var eventCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		return fmt.Errorf("initialize spine: %w", err)
	}
	if eventCount == 0 {
		payload := spinePayload{
			ID:    RootID,
			Brief: "Permanent Aforge spine",
			Provenance: Provenance{
				Origin: OriginSelf,
				Intent: "permanent spine root",
			},
		}
		seq, at, err := appendEvent(tx, RootID, EventSpineCreated, payload)
		if err != nil {
			return fmt.Errorf("initialize spine: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO nodes (
			    id, parent_id, brief, stage, status, origin, session_id,
			    intent, created_seq, created_order, updated_seq, started_at
			) VALUES (?, NULL, ?, 0, ?, ?, NULL, ?, ?, 0, ?, ?)`,
			RootID, payload.Brief, Running, payload.Provenance.Origin,
			payload.Provenance.Intent, seq, seq, formatTime(at)); err != nil {
			return fmt.Errorf("initialize spine view: %w", err)
		}
		if err := refreshGraphFTS(tx, RootID); err != nil {
			return fmt.Errorf("initialize spine index: %w", err)
		}
	} else {
		var roots, spine int
		if err := tx.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE id = ?) FROM nodes WHERE parent_id IS NULL`, RootID).Scan(&roots, &spine); err != nil {
			return fmt.Errorf("validate spine: %w", err)
		}
		if roots != 1 || spine != 1 {
			return fmt.Errorf("validate spine: materialized view has %d roots (%d permanent); run Rebuild", roots, spine)
		}
		// Self-healing: the root is Running by construction, forever. A store
		// where it is anything else was corrupted (a release made it pending,
		// a runner then "completed" it — every splice fails on a closed root).
		// Repair through the journal so Rebuild reproduces the healed state.
		var status Status
		if err := tx.QueryRow(`SELECT status FROM nodes WHERE id = ?`, RootID).Scan(&status); err != nil {
			return fmt.Errorf("validate spine: %w", err)
		}
		if status != Running {
			seq, at, err := appendEvent(tx, RootID, EventSpineRepaired,
				spineRepairPayload{Was: status})
			if err != nil {
				return fmt.Errorf("repair spine: %w", err)
			}
			if err := applySpineRepair(tx, seq, at); err != nil {
				return fmt.Errorf("repair spine: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("initialize spine: %w", err)
	}
	return nil
}

type spineRepairPayload struct {
	Was Status `json:"was"`
}

// applySpineRepair restores the root's structural state: Running, unowned,
// unheld, never folded, never cancel-requested. Shared by open-time repair
// and Rebuild replay so both produce the identical healed view.
func applySpineRepair(tx *sql.Tx, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		UPDATE nodes
		SET status = ?, owner = '', held = 0, cancel_requested = 0, folded = 0,
		    error = '', updated_seq = ?, started_at = ?, finished_at = NULL
		WHERE id = ?`,
		Running, seq, formatTime(at), RootID)
	return err
}

func appendEvent(tx *sql.Tx, nodeID string, kind EventKind, payload any) (int64, time.Time, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("encode %s event: %w", kind, err)
	}
	at := time.Now().UTC()
	result, err := tx.Exec(`INSERT INTO events (ts, node_id, kind, payload) VALUES (?, ?, ?, ?)`,
		formatTime(at), nodeID, kind, string(encoded))
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("append %s event: %w", kind, err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("read %s sequence: %w", kind, err)
	}
	return seq, at, nil
}

// journalTime is RFC 3339 with a fixed-width nanosecond field. The width is
// the whole point: timestamps are compared as text by every day-boundary query
// in this package, and RFC3339Nano drops trailing zeros — so an event landing
// exactly on a second spells itself "...T07:00:00Z" while its neighbour a
// millisecond later spells itself "...T07:00:00.001Z". Byte-wise '.' sorts
// before 'Z', which puts the later event before the earlier one and hands a
// one-second window of every day to the wrong side of local midnight.
const journalTime = "2006-01-02T15:04:05.000000000Z07:00"

// formatTime is the sole writer of every timestamp column in the store.
//
// Rows journaled before the width was fixed remain readable and remain
// correctly attributed: parseTime's layout accepts any number of fractional
// digits, and the only comparisons that cross the two spellings are against a
// whole-second bound. There an old row written exactly on the bound spells
// "...:00Z" and sorts after the new bound's "...:00.000000000Z" — which is the
// right answer at both ends, because the start bound is inclusive of that
// instant either way and the end bound excludes it either way.
func formatTime(value time.Time) string {
	return value.UTC().Format(journalTime)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func terminal(status Status) bool {
	return status == Done || status == Failed || status == Cancelled
}

func validOrigin(origin Origin) bool {
	return origin == OriginUser || origin == OriginTrigger || origin == OriginSelf
}

func validEdgeKind(kind EdgeKind) bool {
	return kind == FeedsInto || kind == Blocks || kind == Suggests
}
