// Workstreams v0 (the Workstreams plan, WP-0) is a proposal until reviewed.
// PROTOCOL.md, "workstream methods", is the wire index.
package appwire

import (
	"encoding/json"
	"reflect"
	"time"
)

const (
	FeatureWorkstreams     = "workstreams"
	EventWorkstreamChanged = "workstream.changed"
	CodeInOtherWorkstream  = "in_other_workstream"
	CodeVersionConflict    = "version_conflict"
)

// ErrWorkstreamsNotYet is the capability refusal for a shapes-only engine.
var ErrWorkstreamsNotYet = Errorf(CodeUnknownMethod, "workstreams: not implemented yet")

// WorkstreamMethods follows the contract index order.
var WorkstreamMethods = []string{
	"workstream.list",
	"workstream.get",
	"workstream.create",
	"workstream.update",
	"workstream.mark_done",
	"workstream.reopen",
	"workstream.plan_get",
	"workstream.plan_set",
	"page.list",
	"page.get",
	"page.save",
	"page.create",
	"page.update",
	"page.move",
	"page.delete",
	"page.history",
	"page.restore",
	"record.list",
	"record.propose",
	"record.keep",
	"record.drop",
	"record.supersede",
	"goahead.list",
	"goahead.grant",
	"goahead.revoke",
	"wake.list",
	"wake.apply",
	"wake.undo",
	"wake.not_now",
}

// Optional numbers and booleans use pointers so known zero and false survive.
// Optional times use omitzero; required lists must be initialized even when empty.
// Numeric counts, versions, positions, job numbers and waves are non-negative.
type WorkstreamWake struct {
	Morning       bool `json:"morning"`
	RelatedIssues bool `json:"related_issues"`
}

type WorkstreamRef struct {
	Kind      string  `json:"kind" enum:"collection,conversation,task,artifact,crew,process,factory_job"`
	ID        string  `json:"id"`
	SessionID *string `json:"session_id,omitempty"`
	Factory   *string `json:"factory,omitempty"`
}

type WorkstreamSource struct {
	Kind    string  `json:"kind" enum:"chat,message,factory_job,page,url"`
	ID      string  `json:"id"`
	ChatID  *string `json:"chat_id,omitempty"`
	Factory *string `json:"factory,omitempty"`
	Label   *string `json:"label,omitempty"`
}

type WorkstreamRow struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Stage        string    `json:"stage" enum:"shaping,building,done"`
	Total        int       `json:"total"`
	Merged       *int      `json:"merged,omitempty"`
	NeedsYou     *int      `json:"needs_you,omitempty"`
	SpendUSD     *float64  `json:"spend_usd,omitempty"`
	BudgetUSD    *float64  `json:"budget_usd,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	AgentID      string    `json:"agent_id"`
	OriginChatID *string   `json:"origin_chat_id,omitempty"`
}

type Workstream struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Why          string         `json:"why"`
	Stage        string         `json:"stage" enum:"shaping,building,done"`
	AgentID      string         `json:"agent_id"`
	OriginChatID *string        `json:"origin_chat_id,omitempty"`
	Autonomy     string         `json:"autonomy" enum:"propose,small,off"`
	Wake         WorkstreamWake `json:"wake"`
	BudgetUSD    *float64       `json:"budget_usd,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	StartedAt    time.Time      `json:"started_at,omitzero"`
	DoneAt       time.Time      `json:"done_at,omitzero"`
	HowItWent    *string        `json:"how_it_went,omitempty"`
	Total        int            `json:"total"`
	Merged       *int           `json:"merged,omitempty"`
	NeedsYou     *int           `json:"needs_you,omitempty"`
	SpendUSD     *float64       `json:"spend_usd,omitempty"`
}

type WorkstreamPlan struct {
	WorkstreamID string                `json:"workstream_id"`
	Version      int                   `json:"version"`
	UpdatedAt    time.Time             `json:"updated_at,omitzero"`
	Milestones   []WorkstreamMilestone `json:"milestones"`
	Waves        []WorkstreamWave      `json:"waves"`
}

type WorkstreamMilestone struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type WorkstreamWave struct {
	N           int                  `json:"n"`
	Title       *string              `json:"title,omitempty"`
	MilestoneID *string              `json:"milestone_id,omitempty"`
	AutoStart   bool                 `json:"auto_start"`
	Items       []WorkstreamPlanItem `json:"items"`
}

type WorkstreamPlanItem struct {
	Kind      string            `json:"kind" enum:"job,draft"`
	Factory   *string           `json:"factory,omitempty"`
	Job       *int              `json:"job,omitempty"`
	DraftID   *string           `json:"draft_id,omitempty"`
	Title     *string           `json:"title,omitempty"`
	Repo      *string           `json:"repo,omitempty"`
	Text      *string           `json:"text,omitempty"`
	Size      *string           `json:"size,omitempty" enum:"S,M,L"`
	BudgetUSD *float64          `json:"budget_usd,omitempty"`
	Source    *WorkstreamSource `json:"source,omitempty"`
}

type WorkstreamPageNode struct {
	ID           string            `json:"id"`
	WorkstreamID string            `json:"workstream_id"`
	ParentID     *string           `json:"parent_id,omitempty"`
	Title        string            `json:"title"`
	Owner        string            `json:"owner" enum:"you,agent"`
	ReadByJobs   bool              `json:"read_by_jobs"`
	Position     int               `json:"position"`
	Version      int               `json:"version"`
	UpdatedAt    time.Time         `json:"updated_at"`
	UpdatedBy    string            `json:"updated_by" enum:"you,agent"`
	UpdatedFrom  *WorkstreamSource `json:"updated_from,omitempty"`
	DeletedAt    time.Time         `json:"deleted_at,omitzero"`
}

type WorkstreamPage struct {
	ID           string            `json:"id"`
	WorkstreamID string            `json:"workstream_id"`
	ParentID     *string           `json:"parent_id,omitempty"`
	Title        string            `json:"title"`
	Owner        string            `json:"owner" enum:"you,agent"`
	ReadByJobs   bool              `json:"read_by_jobs"`
	Position     int               `json:"position"`
	Version      int               `json:"version"`
	UpdatedAt    time.Time         `json:"updated_at"`
	UpdatedBy    string            `json:"updated_by" enum:"you,agent"`
	UpdatedFrom  *WorkstreamSource `json:"updated_from,omitempty"`
	DeletedAt    time.Time         `json:"deleted_at,omitzero"`
	Doc          json.RawMessage   `json:"doc"`
	Markdown     *string           `json:"markdown,omitempty"`
}

type WorkstreamPageVersion struct {
	Version int               `json:"version"`
	SavedAt time.Time         `json:"saved_at"`
	SavedBy string            `json:"saved_by" enum:"you,agent"`
	From    *WorkstreamSource `json:"from,omitempty"`
	Title   string            `json:"title"`
	Bytes   int               `json:"bytes"`
}

type WorkstreamRecord struct {
	ID           string                     `json:"id"`
	WorkstreamID string                     `json:"workstream_id"`
	Kind         string                     `json:"kind" enum:"decision,question,goahead"`
	Text         string                     `json:"text"`
	State        string                     `json:"state" enum:"proposed,kept,dropped,superseded"`
	By           string                     `json:"by" enum:"you,agent"`
	Source       *WorkstreamSource          `json:"source,omitempty"`
	CreatedAt    time.Time                  `json:"created_at"`
	DecidedAt    time.Time                  `json:"decided_at,omitzero"`
	Supersedes   *string                    `json:"supersedes,omitempty"`
	SupersededBy *string                    `json:"superseded_by,omitempty"`
	GoAhead      *WorkstreamGoAheadProposal `json:"goahead,omitempty"`
	GoAheadID    *string                    `json:"goahead_id,omitempty"`
}

type WorkstreamGoAheadProposal struct {
	Scope       string    `json:"scope" enum:"job,wave,milestone"`
	Factory     *string   `json:"factory,omitempty"`
	Job         *int      `json:"job,omitempty"`
	Wave        *int      `json:"wave,omitempty"`
	MilestoneID *string   `json:"milestone_id,omitempty"`
	CapUSD      float64   `json:"cap_usd"`
	EndsAt      time.Time `json:"ends_at"`
}

type WorkstreamGoAhead struct {
	ID           string    `json:"id"`
	WorkstreamID string    `json:"workstream_id"`
	Scope        string    `json:"scope" enum:"job,wave,milestone"`
	Factory      *string   `json:"factory,omitempty"`
	Job          *int      `json:"job,omitempty"`
	Wave         *int      `json:"wave,omitempty"`
	MilestoneID  *string   `json:"milestone_id,omitempty"`
	CapUSD       float64   `json:"cap_usd"`
	EndsAt       time.Time `json:"ends_at"`
	State        string    `json:"state" enum:"active,ended"`
	EndReason    *string   `json:"end_reason,omitempty" enum:"expired,revoked,workstream_done"`
	SpentUSD     *float64  `json:"spent_usd,omitempty"`
	GrantedAt    time.Time `json:"granted_at"`
	EndedAt      time.Time `json:"ended_at,omitzero"`
}

type WorkstreamChanged struct {
	ID    string   `json:"id"`
	Parts []string `json:"parts,omitempty" enum:"workstream,plan,pages,records,goaheads,jobs,wakes"`
}

type WorkstreamTaken struct {
	Factory        string `json:"factory"`
	Job            int    `json:"job"`
	WorkstreamID   string `json:"workstream_id"`
	WorkstreamName string `json:"workstream_name"`
}

type WorkstreamVersionConflict struct {
	ID        string    `json:"id"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by" enum:"you,agent"`
}

type WorkstreamPatch struct {
	Name      *string         `json:"name,omitempty"`
	Why       *string         `json:"why,omitempty"`
	Autonomy  *string         `json:"autonomy,omitempty" enum:"propose,small,off"`
	Wake      *WorkstreamWake `json:"wake,omitempty"`
	BudgetUSD *float64        `json:"budget_usd,omitempty"`
}

type WorkstreamListParams struct{}

type WorkstreamListResult struct {
	Workstreams []WorkstreamRow `json:"workstreams"`
}

type WorkstreamGetParams struct {
	ID string `json:"id"`
}

type WorkstreamGetResult struct {
	Workstream Workstream `json:"workstream"`
}

type WorkstreamCreateParams struct {
	Name         string          `json:"name"`
	Why          *string         `json:"why,omitempty"`
	OriginChatID *string         `json:"origin_chat_id,omitempty"`
	Refs         []WorkstreamRef `json:"refs,omitempty"`
	Model        *string         `json:"model,omitempty"`
	ClientID     *string         `json:"client_id,omitempty"`
}

type WorkstreamCreateResult struct {
	Workstream Workstream `json:"workstream"`
}

type WorkstreamUpdateParams struct {
	ID    string          `json:"id"`
	Patch WorkstreamPatch `json:"patch"`
}

type WorkstreamUpdateResult struct {
	Workstream Workstream `json:"workstream"`
}

type WorkstreamMarkDoneParams struct {
	ID           string  `json:"id"`
	HowItWent    *string `json:"how_it_went,omitempty"`
	ArchiveChats *bool   `json:"archive_chats,omitempty"`
}

type WorkstreamMarkDoneResult struct {
	Workstream Workstream `json:"workstream"`
}

type WorkstreamReopenParams struct {
	ID string `json:"id"`
}

type WorkstreamReopenResult struct {
	Workstream Workstream `json:"workstream"`
}

type WorkstreamPlanGetParams struct {
	ID string `json:"id"`
}

type WorkstreamPlanGetResult struct {
	Plan WorkstreamPlan `json:"plan"`
}

type WorkstreamPlanSetParams struct {
	ID          string                `json:"id"`
	BaseVersion int                   `json:"base_version"`
	Milestones  []WorkstreamMilestone `json:"milestones"`
	Waves       []WorkstreamWave      `json:"waves"`
}

type WorkstreamPlanSetResult struct {
	Plan WorkstreamPlan `json:"plan"`
}

type WorkstreamPageListParams struct {
	WorkstreamID   string `json:"workstream_id"`
	IncludeDeleted *bool  `json:"include_deleted,omitempty"`
}

type WorkstreamPageListResult struct {
	Pages   []WorkstreamPageNode `json:"pages"`
	Deleted []WorkstreamPageNode `json:"deleted,omitzero"`
}

type WorkstreamPageGetParams struct {
	ID       string `json:"id"`
	Markdown *bool  `json:"markdown,omitempty"`
	Version  *int   `json:"version,omitempty"`
}

type WorkstreamPageGetResult struct {
	Page WorkstreamPage `json:"page"`
}

type WorkstreamPageSaveParams struct {
	ID          string            `json:"id"`
	Doc         json.RawMessage   `json:"doc"`
	BaseVersion int               `json:"base_version"`
	Source      *WorkstreamSource `json:"source,omitempty"`
}

type WorkstreamPageSaveResult struct {
	Page WorkstreamPage `json:"page"`
}

type WorkstreamPageCreateParams struct {
	WorkstreamID string            `json:"workstream_id"`
	ParentID     *string           `json:"parent_id,omitempty"`
	AfterID      *string           `json:"after_id,omitempty"`
	Title        string            `json:"title"`
	Owner        *string           `json:"owner,omitempty" enum:"you,agent"`
	ReadByJobs   *bool             `json:"read_by_jobs,omitempty"`
	Doc          json.RawMessage   `json:"doc,omitempty"`
	Source       *WorkstreamSource `json:"source,omitempty"`
}

type WorkstreamPageCreateResult struct {
	Page WorkstreamPage `json:"page"`
}

type WorkstreamPageUpdateParams struct {
	ID         string  `json:"id"`
	Title      *string `json:"title,omitempty"`
	Owner      *string `json:"owner,omitempty" enum:"you,agent"`
	ReadByJobs *bool   `json:"read_by_jobs,omitempty"`
}

type WorkstreamPageUpdateResult struct {
	Page WorkstreamPageNode `json:"page"`
}

type WorkstreamPageMoveParams struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id,omitempty"`
	AfterID  *string `json:"after_id,omitempty"`
}

type WorkstreamPageMoveResult struct {
	Page WorkstreamPageNode `json:"page"`
}

type WorkstreamPageDeleteParams struct {
	ID string `json:"id"`
}

type WorkstreamPageDeleteResult struct {
	Deleted []string `json:"deleted"`
}

type WorkstreamPageHistoryParams struct {
	ID    string `json:"id"`
	Limit *int   `json:"limit,omitempty"`
}

type WorkstreamPageHistoryResult struct {
	Versions []WorkstreamPageVersion `json:"versions"`
}

type WorkstreamPageRestoreParams struct {
	ID          string `json:"id"`
	Version     *int   `json:"version,omitempty"`
	BaseVersion *int   `json:"base_version,omitempty"`
}

type WorkstreamPageRestoreResult struct {
	Page WorkstreamPage `json:"page"`
}

type WorkstreamRecordListParams struct {
	WorkstreamID    string  `json:"workstream_id"`
	Kind            *string `json:"kind,omitempty" enum:"decision,question,goahead"`
	State           *string `json:"state,omitempty" enum:"proposed,kept,dropped,superseded"`
	IncludeGoAheads *bool   `json:"include_goaheads,omitempty"`
}

type WorkstreamRecordListResult struct {
	Records []WorkstreamRecord `json:"records"`
}

type WorkstreamRecordProposeParams struct {
	WorkstreamID string                     `json:"workstream_id"`
	Kind         string                     `json:"kind" enum:"decision,question,goahead"`
	Text         string                     `json:"text"`
	Source       *WorkstreamSource          `json:"source,omitempty"`
	Kept         *bool                      `json:"kept,omitempty"`
	GoAhead      *WorkstreamGoAheadProposal `json:"goahead,omitempty"`
}

type WorkstreamRecordProposeResult struct {
	Record WorkstreamRecord `json:"record"`
}

type WorkstreamRecordKeepParams struct {
	ID string `json:"id"`
}

type WorkstreamRecordKeepResult struct {
	Record WorkstreamRecord `json:"record"`
}

type WorkstreamRecordDropParams struct {
	ID string `json:"id"`
}

type WorkstreamRecordDropResult struct {
	Record WorkstreamRecord `json:"record"`
}

type WorkstreamRecordSupersedeParams struct {
	ID     string            `json:"id"`
	Text   string            `json:"text"`
	Kind   *string           `json:"kind,omitempty" enum:"decision,question"`
	Source *WorkstreamSource `json:"source,omitempty"`
}

type WorkstreamRecordSupersedeResult struct {
	Record     WorkstreamRecord `json:"record"`
	Superseded WorkstreamRecord `json:"superseded"`
}

type WorkstreamGoAheadListParams struct {
	WorkstreamID string `json:"workstream_id"`
	Ended        *bool  `json:"ended,omitempty"`
}

type WorkstreamGoAheadListResult struct {
	GoAheads []WorkstreamGoAhead `json:"goaheads"`
}

type WorkstreamGoAheadGrantParams struct {
	WorkstreamID string    `json:"workstream_id"`
	Scope        string    `json:"scope" enum:"job,wave,milestone"`
	Factory      *string   `json:"factory,omitempty"`
	Job          *int      `json:"job,omitempty"`
	Wave         *int      `json:"wave,omitempty"`
	MilestoneID  *string   `json:"milestone_id,omitempty"`
	CapUSD       float64   `json:"cap_usd"`
	EndsAt       time.Time `json:"ends_at"`
}

type WorkstreamGoAheadGrantResult struct {
	GoAhead WorkstreamGoAhead `json:"goahead"`
}

type WorkstreamGoAheadRevokeParams struct {
	ID string `json:"id"`
}

type WorkstreamGoAheadRevokeResult struct {
	GoAhead WorkstreamGoAhead `json:"goahead"`
}

type WorkstreamWakeEvent struct {
	Kind    string  `json:"kind" enum:"morning,finished,merged,failed,needs_you"`
	Factory *string `json:"factory,omitempty"`
	Job     *int    `json:"job,omitempty"`
	Label   *string `json:"label,omitempty"`
}

type WorkstreamWakeItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind" enum:"move,draft,page,decision,question"`
	State     string    `json:"state" enum:"applied,undone,proposed,dismissed"`
	Text      string    `json:"text"`
	Why       *string   `json:"why,omitempty"`
	Factory   *string   `json:"factory,omitempty"`
	Job       *int      `json:"job,omitempty"`
	DraftID   *string   `json:"draft_id,omitempty"`
	Wave      *int      `json:"wave,omitempty"`
	PageID    *string   `json:"page_id,omitempty"`
	RecordID  *string   `json:"record_id,omitempty"`
	BudgetUSD *float64  `json:"budget_usd,omitempty"`
	Note      *string   `json:"note,omitempty"`
	AppliedAt time.Time `json:"applied_at,omitzero"`
	UndoneAt  time.Time `json:"undone_at,omitzero"`
}

type WorkstreamWakeRun struct {
	ID           string               `json:"id"`
	WorkstreamID string               `json:"workstream_id"`
	Event        WorkstreamWakeEvent  `json:"event"`
	State        string               `json:"state" enum:"running,settled,failed"`
	StartedAt    time.Time            `json:"started_at"`
	SettledAt    time.Time            `json:"settled_at,omitzero"`
	CostUSD      *float64             `json:"cost_usd,omitempty"`
	Summary      *string              `json:"summary,omitempty"`
	Error        *string              `json:"error,omitempty"`
	DismissedAt  time.Time            `json:"dismissed_at,omitzero"`
	Items        []WorkstreamWakeItem `json:"items"`
}

type WorkstreamWakeListParams struct {
	WorkstreamID string `json:"workstream_id"`
	Limit        *int   `json:"limit,omitempty"`
}
type WorkstreamWakeListResult struct {
	Wakes []WorkstreamWakeRun `json:"wakes"`
}
type WorkstreamWakeApplyParams struct {
	ID     string  `json:"id"`
	ItemID *string `json:"item_id,omitempty"`
}
type WorkstreamWakeApplyResult struct {
	Wake WorkstreamWakeRun `json:"wake"`
}
type WorkstreamWakeUndoParams struct {
	ID     string  `json:"id"`
	ItemID *string `json:"item_id,omitempty"`
}
type WorkstreamWakeUndoResult struct {
	Wake WorkstreamWakeRun `json:"wake"`
}
type WorkstreamWakeNotNowParams struct {
	ID string `json:"id"`
}
type WorkstreamWakeNotNowResult struct {
	Wake WorkstreamWakeRun `json:"wake"`
}

// Workstreamer is the optional engine half of the workstreams contract.
// A stub implements the shapes but returns ErrWorkstreamsNotYet and serves false.
type Workstreamer interface {
	ServesWorkstreams() bool
	WorkstreamList(WorkstreamListParams) (WorkstreamListResult, error)
	WorkstreamGet(WorkstreamGetParams) (WorkstreamGetResult, error)
	WorkstreamCreate(WorkstreamCreateParams) (WorkstreamCreateResult, error)
	WorkstreamUpdate(WorkstreamUpdateParams) (WorkstreamUpdateResult, error)
	WorkstreamMarkDone(WorkstreamMarkDoneParams) (WorkstreamMarkDoneResult, error)
	WorkstreamReopen(WorkstreamReopenParams) (WorkstreamReopenResult, error)
	WorkstreamPlanGet(WorkstreamPlanGetParams) (WorkstreamPlanGetResult, error)
	WorkstreamPlanSet(WorkstreamPlanSetParams) (WorkstreamPlanSetResult, error)
	WorkstreamPageList(WorkstreamPageListParams) (WorkstreamPageListResult, error)
	WorkstreamPageGet(WorkstreamPageGetParams) (WorkstreamPageGetResult, error)
	WorkstreamPageSave(WorkstreamPageSaveParams) (WorkstreamPageSaveResult, error)
	WorkstreamPageCreate(WorkstreamPageCreateParams) (WorkstreamPageCreateResult, error)
	WorkstreamPageUpdate(WorkstreamPageUpdateParams) (WorkstreamPageUpdateResult, error)
	WorkstreamPageMove(WorkstreamPageMoveParams) (WorkstreamPageMoveResult, error)
	WorkstreamPageDelete(WorkstreamPageDeleteParams) (WorkstreamPageDeleteResult, error)
	WorkstreamPageHistory(WorkstreamPageHistoryParams) (WorkstreamPageHistoryResult, error)
	WorkstreamPageRestore(WorkstreamPageRestoreParams) (WorkstreamPageRestoreResult, error)
	WorkstreamRecordList(WorkstreamRecordListParams) (WorkstreamRecordListResult, error)
	WorkstreamRecordPropose(WorkstreamRecordProposeParams) (WorkstreamRecordProposeResult, error)
	WorkstreamRecordKeep(WorkstreamRecordKeepParams) (WorkstreamRecordKeepResult, error)
	WorkstreamRecordDrop(WorkstreamRecordDropParams) (WorkstreamRecordDropResult, error)
	WorkstreamRecordSupersede(WorkstreamRecordSupersedeParams) (WorkstreamRecordSupersedeResult, error)
	WorkstreamGoAheadList(WorkstreamGoAheadListParams) (WorkstreamGoAheadListResult, error)
	WorkstreamGoAheadGrant(WorkstreamGoAheadGrantParams) (WorkstreamGoAheadGrantResult, error)
	WorkstreamGoAheadRevoke(WorkstreamGoAheadRevokeParams) (WorkstreamGoAheadRevokeResult, error)
	WorkstreamWakeList(WorkstreamWakeListParams) (WorkstreamWakeListResult, error)
	WorkstreamWakeApply(WorkstreamWakeApplyParams) (WorkstreamWakeApplyResult, error)
	WorkstreamWakeUndo(WorkstreamWakeUndoParams) (WorkstreamWakeUndoResult, error)
	WorkstreamWakeNotNow(WorkstreamWakeNotNowParams) (WorkstreamWakeNotNowResult, error)
}

// workstreamCall preserves the params/result types used by the schema generator.
// Unsupported engines retain their capability refusal before parameter checks.
func workstreamCall[P interface{ Check() error }, R any](call func(Workstreamer, P) (R, error)) method {
	m := typed(func(e Engine, p P) (*R, error) {
		w, ok := e.(Workstreamer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve workstreams")
		}
		if !w.ServesWorkstreams() {
			return nil, ErrWorkstreamsNotYet
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		result, err := call(w, p)
		return &result, err
	})
	delegate := m.call
	m.call = func(e Engine, raw json.RawMessage) (any, error) {
		if w, ok := e.(Workstreamer); ok && w.ServesWorkstreams() {
			if err := workstreamRequired(raw, m.params); err != nil {
				return nil, err
			}
		}
		return delegate(e, raw)
	}
	return m
}

func init() {
	methods["workstream.list"] = workstreamCall(Workstreamer.WorkstreamList)
	methods["workstream.get"] = workstreamCall(Workstreamer.WorkstreamGet)
	methods["workstream.create"] = workstreamCall(Workstreamer.WorkstreamCreate)
	methods["workstream.update"] = workstreamCall(Workstreamer.WorkstreamUpdate)
	methods["workstream.mark_done"] = workstreamCall(Workstreamer.WorkstreamMarkDone)
	methods["workstream.reopen"] = workstreamCall(Workstreamer.WorkstreamReopen)
	methods["workstream.plan_get"] = workstreamCall(Workstreamer.WorkstreamPlanGet)
	methods["workstream.plan_set"] = workstreamCall(Workstreamer.WorkstreamPlanSet)
	methods["page.list"] = workstreamCall(Workstreamer.WorkstreamPageList)
	methods["page.get"] = workstreamCall(Workstreamer.WorkstreamPageGet)
	methods["page.save"] = workstreamCall(Workstreamer.WorkstreamPageSave)
	methods["page.create"] = workstreamCall(Workstreamer.WorkstreamPageCreate)
	methods["page.update"] = workstreamCall(Workstreamer.WorkstreamPageUpdate)
	methods["page.move"] = workstreamCall(Workstreamer.WorkstreamPageMove)
	methods["page.delete"] = workstreamCall(Workstreamer.WorkstreamPageDelete)
	methods["page.history"] = workstreamCall(Workstreamer.WorkstreamPageHistory)
	methods["page.restore"] = workstreamCall(Workstreamer.WorkstreamPageRestore)
	methods["record.list"] = workstreamCall(Workstreamer.WorkstreamRecordList)
	methods["record.propose"] = workstreamCall(Workstreamer.WorkstreamRecordPropose)
	methods["record.keep"] = workstreamCall(Workstreamer.WorkstreamRecordKeep)
	methods["record.drop"] = workstreamCall(Workstreamer.WorkstreamRecordDrop)
	methods["record.supersede"] = workstreamCall(Workstreamer.WorkstreamRecordSupersede)
	methods["goahead.list"] = workstreamCall(Workstreamer.WorkstreamGoAheadList)
	methods["goahead.grant"] = workstreamCall(Workstreamer.WorkstreamGoAheadGrant)
	methods["goahead.revoke"] = workstreamCall(Workstreamer.WorkstreamGoAheadRevoke)
	methods["wake.list"] = workstreamCall(Workstreamer.WorkstreamWakeList)
	methods["wake.apply"] = workstreamCall(Workstreamer.WorkstreamWakeApply)
	methods["wake.undo"] = workstreamCall(Workstreamer.WorkstreamWakeUndo)
	methods["wake.not_now"] = workstreamCall(Workstreamer.WorkstreamWakeNotNow)
	eventShapes[EventWorkstreamChanged] = reflect.TypeFor[WorkstreamChanged]()
}

func withWorkstreamsFeature(hello Hello, e Engine) Hello {
	if w, ok := e.(Workstreamer); ok && w.ServesWorkstreams() {
		return withFeature(hello, FeatureWorkstreams)
	}
	return hello
}
