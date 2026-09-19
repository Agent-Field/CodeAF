package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"unicode/utf8"
)

const (
	JobPending   = "pending"
	JobLeased    = "leased"
	JobCompleted = "completed"
	JobDeferred  = "deferred"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
	JobOrganize  = "observe_and_organize"
	// OrganizeExistingKey is the coalesce key for the explicit Folders-place
	// survey. Automatic after-message jobs use chat id + source revision instead.
	OrganizeExistingKey = "organize_existing"
	// OrganizeThisCause marks a targeted Organize this chat job so an
	// explicit click still runs when workspace.organize is off. Automatic
	// after-message rows leave CauseID empty.
	OrganizeThisCause  = "organize_this_chat"
	GuidanceActive     = "active"
	GuidanceSuperseded = "superseded"
)

const jobColumns = "id,type,state,owner,fence,cause_id,coalesce_key,chat_id,source_rev,error,attempt,lease_until,created_at,updated_at,cursor,enqueued_at,started_at,committed_at"

// OrganizeChatKey is chat id + source revision. A second enqueue for the same
// key while the row is pending or leased is a no-op success in the store.
func OrganizeChatKey(chatID, sourceRev string) string {
	return chatID + ":" + sourceRev
}

const guidanceColumns = "id,scope_id,text,status,origin,actor,source_ref,supersedes,revision,created_at,updated_at"

// ScopeID empty means virtual Root. Root is still not a collections row.
type Guidance struct {
	ID, ScopeID, Text, Status, Origin, Actor, SourceRef, Supersedes string
	Revision                                                        int
	CreatedAt, UpdatedAt                                            string
}

type Job struct {
	ID, Type, State, Owner, Fence, CauseID, CoalesceKey, ChatID, SourceRev, Error string
	Attempt                                                                       int
	LeaseUntil, CreatedAt, UpdatedAt                                              string
	// Cursor is the survey checkpoint: the next unfiled chat id to process.
	// Empty means the start of Unfiled. Additive on the jobs row, not a table.
	Cursor string
	// EnqueuedAt / StartedAt / CommittedAt are scheduler timings. Visible
	// paint is a UI proof instant and is not stored.
	EnqueuedAt, StartedAt, CommittedAt string
}

type Observation struct {
	ID, ChatID, SourceRev, Purpose, Body, Evidence, Model, PromptVersion string
	CreatedAt                                                            string
}

type Suppression struct {
	CollectionID, Kind, RefID, SessionID, EvidenceHash, Actor, At string
}

type Proposal struct {
	ID, ChatID, SourceRev, PlanJSON, Result, IdempotencyKey string
	CreatedAt                                               string
}

func mintID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

// fenceConflict is a stale lease refusal. It is ErrConflict so callers that
// already handle a CAS miss still see one, and the sentence names the fence
// so a person can tell a worker race from a stale collection revision.
func fenceConflict() error {
	return fmt.Errorf("%w: job fence does not match", ErrConflict)
}

func validGuidanceText(text string) bool {
	return text != "" && len(text) <= 64*1024 && utf8.ValidString(text)
}

func validJobFinish(state string) bool {
	switch state {
	case JobCompleted, JobDeferred, JobFailed, JobCancelled:
		return true
	}
	return false
}

func validGuidanceStatus(status string) bool {
	return status == GuidanceActive || status == GuidanceSuperseded
}
