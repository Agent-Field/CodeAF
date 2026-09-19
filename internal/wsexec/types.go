package wsexec

import "context"

// LaunchRequest is one launch-or-join intent. GrantRev is a string here
// because the frozen Wave 4 signature copies it that way; the grant row
// itself still stores Revision as an int.
type LaunchRequest struct {
	RequestKey, EquivalenceKey, GrantID, CoordinatorID, OwnerChatID, Brief string
	GrantRev, AssignmentRev                                                string
}

// SteerRevision is a delegated change to running work. PersonRequestID cites
// the original person request; model-supplied person origin is refused (A11).
type SteerRevision struct {
	WorkID, GrantID, Text, PersonRequestID string
	GrantRev                               int
}

// WorkView is the inspectable identity of one owned run/task.
type WorkView struct {
	WorkID, RequestKey, RunInstanceID, Road, State, OwnerChatID, GrantID string
	Joined                                                               bool // true when LaunchOrJoin followed existing work
}

// ResultView is an observe snapshot. Empty work is emptiness: nothing, never
// a fake 100%.
type ResultView struct {
	WorkID, RunInstanceID, State, Detail string
}

// AdmitRequest is what the existing task/run door receives. OwnerChatID is
// the owning conversation. There is no ParentID field: folder membership is
// not a plan-database parent.
type AdmitRequest struct {
	RequestKey, Brief, Road, OwnerChatID string
}

// AdmitResult is the runtime's accepted identity for one request key.
type AdmitResult struct {
	RunInstanceID, RuntimeRef, Road string
	Already                         bool // runtime already had this request key
}

// Grant is the adapter's copy of the schema row so tests can inject a fake
// Store without importing workspace internals.
type Grant struct {
	ID, Goal, CoordinatorID, ScopeKind, FolderID, SnapshotJSON string
	ActionJSON, Issuer, Origin, Actor, Status                  string
	BudgetUSD                                                  float64
	Revision, RevocationRevision                               int
	CreatedAt, UpdatedAt                                       string
}

// ExecutionBinding is launch intent plus the bound run. WorkID is the
// request key so Inspect can use BindingByRequestKey without a second index.
type ExecutionBinding struct {
	ID, RequestKey, EquivalenceKey, WorkID, RunInstanceID, Road string
	OwnerChatID, GrantID, CoordinatorID, RuntimeRef             string
	AssignmentRev, GrantRev, State, Fence, Owner                string
	LeaseUntil, CreatedAt, UpdatedAt, BoundAt, AdmittedAt       string
}

// Runtime is the existing task/run door. Session-task maps to StartTask.
// bash-run maps to the registered RunEngine / PlanDB store.
type Runtime interface {
	Admit(ctx context.Context, req AdmitRequest) (AdmitResult, error)
	Inspect(ctx context.Context, runInstanceID string) (WorkView, error)
	Steer(ctx context.Context, runInstanceID string, rev SteerRevision) error
	Pause(ctx context.Context, runInstanceID string) error
	Stop(ctx context.Context, runInstanceID string) error
	Observe(ctx context.Context, runInstanceID string) (ResultView, error)
	FindByRequestKey(ctx context.Context, requestKey string) (AdmitResult, bool, error)
}

// Store is the durable grant and binding door. Schema persists the rows;
// this package talks only to the interface so a fake in tests is enough
// until integrate. Open refuses to invent a memory fallback.
type Store interface {
	PutBinding(ctx context.Context, b ExecutionBinding) (ExecutionBinding, error)
	BindingByRequestKey(ctx context.Context, requestKey string) (ExecutionBinding, error)
	BindingByEquivalence(ctx context.Context, equivalenceKey string) (ExecutionBinding, error)
	BindRuntime(ctx context.Context, requestKey, runInstanceID, runtimeRef string) (ExecutionBinding, error)
	GetGrant(ctx context.Context, id string) (Grant, error)
}
