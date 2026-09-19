package workspace

import (
	"encoding/json"
	"fmt"
	"sort"
)

const (
	GrantActive     = "active"
	GrantRevoked    = "revoked"
	GrantSuperseded = "superseded"
	ClassRead       = "read"
	ClassDiscuss    = "discuss"
	ClassOrganize   = "organize"
	ClassExecute    = "execute"
	ClassSteer      = "steer"
	ClassStop       = "stop"
	BindReserved    = "reserved"
	BindAdmitted    = "admitted"
	BindBound       = "bound"
	BindPaused      = "paused"
	BindStopped     = "stopped"
	BindCompleted   = "completed"
	BindFailed      = "failed"
	RoadSessionTask = "session-task"
	RoadBashRun     = "bash-run"
)

const grantColumns = "id,goal,coordinator_id,scope_kind,folder_id,snapshot_json,action_json,issuer,origin,actor,status,budget_usd,revision,revocation_revision,created_at,updated_at"
const bindingColumns = "id,request_key,equivalence_key,work_id,run_instance_id,road,owner_chat_id,grant_id,coordinator_id,runtime_ref,assignment_rev,grant_rev,state,fence,owner,lease_until,created_at,updated_at,bound_at,admitted_at"

// Grant is a software-minted delegation. ActionJSON is a canonical JSON array
// of action-class strings. Empty SnapshotJSON with ScopeFolderDynamic resolves
// descendants at read time.
type Grant struct {
	ID, Goal, CoordinatorID, ScopeKind, FolderID, SnapshotJSON string
	ActionJSON, Issuer, Origin, Actor, Status                  string
	BudgetUSD                                                  float64
	Revision, RevocationRevision                               int
	CreatedAt, UpdatedAt                                       string
}

// ExecutionBinding is launch intent plus the later runtime identity. The
// reserved row is written before admission and keyed by RequestKey.
type ExecutionBinding struct {
	ID, RequestKey, EquivalenceKey, WorkID, RunInstanceID, Road string
	OwnerChatID, GrantID, CoordinatorID, RuntimeRef             string
	AssignmentRev, GrantRev, State, Fence, Owner                string
	LeaseUntil, CreatedAt, UpdatedAt, BoundAt, AdmittedAt       string
}

func validGrantStatus(status string) bool {
	return status == GrantActive || status == GrantRevoked || status == GrantSuperseded
}

func validActionClass(class string) bool {
	switch class {
	case ClassRead, ClassDiscuss, ClassOrganize, ClassExecute, ClassSteer, ClassStop:
		return true
	}
	return false
}

func validBindState(state string) bool {
	switch state {
	case BindReserved, BindAdmitted, BindBound, BindPaused, BindStopped, BindCompleted, BindFailed:
		return true
	}
	return false
}

func validRoad(road string) bool {
	return road == "" || road == RoadSessionTask || road == RoadBashRun
}

func liveBindState(state string) bool {
	return state == BindReserved || state == BindAdmitted || state == BindBound || state == BindPaused
}

func grantExpandError() error {
	return fmt.Errorf("%w: grant cannot expand", ErrInvalid)
}

func bindingConflict() error {
	return fmt.Errorf("%w: binding already has a run-instance id", ErrConflict)
}

func canonicalActions(raw string) (string, error) {
	canon, items, err := canonicalStringList(raw)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if !validActionClass(item) {
			return "", fmt.Errorf("%w: unknown action class %q", ErrInvalid, item)
		}
	}
	return canon, nil
}

func canonicalSnapshot(raw string) (string, error) {
	canon, _, err := canonicalStringList(raw)
	return canon, err
}

func canonicalStringList(raw string) (string, []string, error) {
	if raw == "" {
		return "[]", nil, nil
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return "", nil, fmt.Errorf("%w: grant list is not a JSON array", ErrInvalid)
	}
	sort.Strings(items)
	uniq := make([]string, 0, len(items))
	prev := ""
	for i, item := range items {
		if !validOptionalText(item, 4096) {
			return "", nil, fmt.Errorf("%w: grant field is too long", ErrInvalid)
		}
		if i > 0 && item == prev {
			continue
		}
		uniq = append(uniq, item)
		prev = item
	}
	out, err := json.Marshal(uniq)
	if err != nil {
		return "", nil, err
	}
	return string(out), uniq, nil
}

func jsonStringSet(raw string) map[string]bool {
	var items []string
	if raw == "" || json.Unmarshal([]byte(raw), &items) != nil {
		return map[string]bool{}
	}
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

func grantExpands(base, next Grant) bool {
	return actionsExpand(base.ActionJSON, next.ActionJSON) || scopeExpands(base, next)
}

func actionsExpand(baseJSON, nextJSON string) bool {
	base := jsonStringSet(baseJSON)
	for class := range jsonStringSet(nextJSON) {
		if !base[class] {
			return true
		}
	}
	return false
}

func scopeExpands(base, next Grant) bool {
	if base.ScopeKind == ScopeSelected && next.ScopeKind == ScopeFolderDynamic {
		return true
	}
	if base.FolderID != "" && (next.FolderID == "" || next.FolderID != base.FolderID) {
		return true
	}
	return snapshotExpands(base, next)
}

func snapshotExpands(base, next Grant) bool {
	if base.ScopeKind == ScopeFolderDynamic && base.SnapshotJSON == "[]" {
		return false
	}
	if next.ScopeKind == ScopeFolderDynamic && next.SnapshotJSON == "[]" && (base.ScopeKind != ScopeFolderDynamic || base.SnapshotJSON != "[]") {
		return true
	}
	baseSet := jsonStringSet(base.SnapshotJSON)
	for id := range jsonStringSet(next.SnapshotJSON) {
		if !baseSet[id] {
			return true
		}
	}
	return false
}
