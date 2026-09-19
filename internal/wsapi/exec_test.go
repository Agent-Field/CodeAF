package wsapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type recordingExecutor struct {
	mu       sync.Mutex
	launches []LaunchRequest
	stops    []string
	pauses   []string
	steers   []SteerRevision
	work     map[string]WorkView
}

func newRecordingExecutor() *recordingExecutor {
	return &recordingExecutor{work: map[string]WorkView{}}
}

func (r *recordingExecutor) LaunchOrJoin(_ context.Context, req LaunchRequest) (WorkView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.RequestKey == "" {
		return WorkView{}, fmt.Errorf("%w: request key is empty", workspace.ErrInvalid)
	}
	r.launches = append(r.launches, req)
	if existing, ok := r.work[req.RequestKey]; ok {
		existing.Joined = true
		return existing, nil
	}
	view := WorkView{
		WorkID: req.RequestKey, RequestKey: req.RequestKey, RunInstanceID: "run-" + req.RequestKey,
		Road: workspace.RoadSessionTask, State: workspace.BindBound, OwnerChatID: req.OwnerChatID,
		GrantID: req.GrantID,
	}
	r.work[req.RequestKey] = view
	return view, nil
}

func (r *recordingExecutor) Inspect(_ context.Context, workID string) (WorkView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	view, ok := r.work[workID]
	if !ok {
		return WorkView{}, workspace.ErrNotFound
	}
	return view, nil
}

func (r *recordingExecutor) Steer(_ context.Context, rev SteerRevision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steers = append(r.steers, rev)
	return nil
}

func (r *recordingExecutor) PauseWork(_ context.Context, workID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pauses = append(r.pauses, workID)
	return nil
}

func (r *recordingExecutor) StopWork(_ context.Context, workID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops = append(r.stops, workID)
	if view, ok := r.work[workID]; ok {
		view.State = workspace.BindStopped
		r.work[workID] = view
	}
	return nil
}

func (r *recordingExecutor) Observe(_ context.Context, workID string) (ResultView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	view, ok := r.work[workID]
	if !ok {
		return ResultView{}, workspace.ErrNotFound
	}
	return ResultView{WorkID: view.WorkID, RunInstanceID: view.RunInstanceID, State: view.State, Detail: "running"}, nil
}

func (r *recordingExecutor) Recover(context.Context, string) (WorkView, error) {
	return WorkView{}, fmt.Errorf("%w: recover is a wiring door", workspace.ErrInvalid)
}

func (r *recordingExecutor) launchCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.launches)
}

func (r *recordingExecutor) stopCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.stops)
}

func TestNilExecutorDoesNotReturnADummyCompletedView(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	grant, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "mgmt", Goal: "keep receipts current",
		ActionClasses: []string{workspace.ClassExecute, workspace.ClassSteer, workspace.ClassStop},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, IdempotencyKey: "rk-1", Brief: "do it"})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") || view.State == workspace.BindCompleted || view.RunInstanceID != "" {
		t.Fatalf("nil executor must be absent, not a dummy completed view: %+v, %v", view, err)
	}
	if _, err := svc.InspectWork(ctx, "rk-1"); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("inspect: %v", err)
	}
	if err := svc.SteerWork(ctx, SteerRevision{WorkID: "rk-1", GrantID: grant.ID, Text: "raise bar", PersonRequestID: "person-1"}); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("steer: %v", err)
	}
	if err := svc.PauseWork(ctx, "rk-1"); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("pause work: %v", err)
	}
	if err := svc.StopWork(ctx, "rk-1"); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("stop: %v", err)
	}
	observed, err := svc.ObserveWork(ctx, "rk-1")
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") || observed.State == workspace.BindCompleted {
		t.Fatalf("observe must not invent completed: %+v, %v", observed, err)
	}
}

func TestGrantCannotSelfExpand(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	parent, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "mgmt", Goal: "read and organize", ScopeKind: ScopeSelected,
		ChatIDs: []string{"a"}, ActionClasses: []string{workspace.ClassRead, workspace.ClassOrganize},
	})
	if err != nil || parent.ID == "" || parent.Status != workspace.GrantActive {
		t.Fatalf("parent %+v, %v", parent, err)
	}
	got, err := svc.InspectGrant(ctx, parent.ID)
	if err != nil || got.ID != parent.ID || !containsID(got.ActionClasses, workspace.ClassRead) {
		t.Fatalf("inspect %+v, %v", got, err)
	}
	_, err = svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "child", Goal: "also execute", ScopeKind: ScopeSelected,
		ChatIDs: []string{"a"}, ActionClasses: []string{workspace.ClassExecute}, Issuer: parent.ID,
	})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("added class: %v", err)
	}
	_, err = svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "child", Goal: "whole folder", ScopeKind: ScopeFolderDynamic,
		FolderID: "billing", ActionClasses: []string{workspace.ClassRead}, Issuer: parent.ID,
	})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("enlarged scope: %v", err)
	}
	child, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "child", Goal: "narrower organize", ScopeKind: ScopeSelected,
		ChatIDs: []string{"a"}, ActionClasses: []string{workspace.ClassOrganize}, Issuer: parent.ID,
	})
	if err != nil || child.Issuer != parent.ID || !containsID(child.ActionClasses, workspace.ClassOrganize) {
		t.Fatalf("narrower %+v, %v", child, err)
	}
}

func TestLaunchNeedsRequestKeyBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	svc, exec, grant := grantHarness(t)
	view, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, Brief: "do it"})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "request key") || exec.launchCount() != 0 || view.RunInstanceID != "" {
		t.Fatalf("empty key must not reach the executor: %+v, %v, launches=%d", view, err, exec.launchCount())
	}
	got, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: "mgmt", OwnerChatID: "talk", Brief: "do it",
		EquivalenceKey: "eq-1", IdempotencyKey: "rk-1",
	})
	if err != nil || got.RequestKey != "rk-1" || got.RunInstanceID == "" || got.State == workspace.BindCompleted {
		t.Fatalf("launch %+v, %v", got, err)
	}
	if exec.launchCount() != 1 || exec.launches[0].RequestKey != "rk-1" || exec.launches[0].GrantRev == "" {
		t.Fatalf("executor must see the request key before admission: %+v", exec.launches)
	}
}

func TestPauseCoordinationBlocksNewLaunchAndDoesNotStopExistingWork(t *testing.T) {
	ctx := context.Background()
	svc, exec, grant := grantHarness(t)
	if _, err := svc.CoordinateSelected(ctx, CoordinateRequest{CoordinatorID: "mgmt", ChatIDs: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, CoordinatorID: "mgmt", IdempotencyKey: "rk-run", Brief: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.PauseCoordination(ctx, "mgmt"); err != nil {
		t.Fatal(err)
	}
	view, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, CoordinatorID: "mgmt", IdempotencyKey: "rk-new", Brief: "another"})
	if err == nil || !strings.Contains(err.Error(), "paused") || exec.launchCount() != 1 || view.RunInstanceID != "" {
		t.Fatalf("pause must refuse new launch: %+v, %v, launches=%d", view, err, exec.launchCount())
	}
	live, err := svc.InspectWork(ctx, first.WorkID)
	if err != nil || live.State == workspace.BindStopped || live.RunInstanceID != first.RunInstanceID {
		t.Fatalf("pause must not stop existing work: %+v, %v", live, err)
	}
	if err := svc.PauseWork(ctx, first.WorkID); err != nil {
		t.Fatalf("pause work is the separate door: %v", err)
	}
	if err := svc.StopWork(ctx, first.WorkID); err != nil {
		t.Fatalf("stop work is the separate door: %v", err)
	}
	if exec.stopCount() != 1 || len(exec.pauses) != 1 {
		t.Fatalf("stop/pause work must still run after coordination pause: stops=%d pauses=%d", exec.stopCount(), len(exec.pauses))
	}
}

func TestPlacementRemoveDoesNotCancelWork(t *testing.T) {
	ctx := context.Background()
	svc, exec, grant := grantHarness(t)
	billing := createFolder(t, svc, "Billing")
	if err := svc.AddPlacement(ctx, billing.ID, conv("talk"), workspace.Provenance{Reason: "file"}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, OwnerChatID: "talk", IdempotencyKey: "rk-place", Brief: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemovePlacement(ctx, billing.ID, conv("talk"), workspace.Provenance{Reason: "unfile"}); err != nil {
		t.Fatal(err)
	}
	if exec.stopCount() != 0 {
		t.Fatalf("remove placement cancelled work: stops=%v", exec.stops)
	}
	live, err := svc.InspectWork(ctx, got.WorkID)
	if err != nil || live.State == workspace.BindStopped || live.RunInstanceID != got.RunInstanceID {
		t.Fatalf("work must remain after unfile: %+v, %v", live, err)
	}
}

func TestRevokeIsCheckedBeforeLaunchSteerAndStop(t *testing.T) {
	ctx := context.Background()
	svc, exec, grant := grantHarness(t)
	first, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, IdempotencyKey: "rk-rev", Brief: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := svc.RevokeGrant(ctx, grant.ID, grant.Revision)
	if err != nil || revoked.Status != workspace.GrantRevoked || revoked.RevocationRevision != grant.Revision {
		t.Fatalf("revoke %+v, %v", revoked, err)
	}
	view, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, IdempotencyKey: "rk-after", Brief: "again"})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), workspace.GrantRevoked) || exec.launchCount() != 1 || view.RunInstanceID != "" {
		t.Fatalf("revoked grant must not launch: %+v, %v, launches=%d", view, err, exec.launchCount())
	}
	if err := svc.SteerWork(ctx, SteerRevision{WorkID: first.WorkID, GrantID: grant.ID, Text: "raise bar", PersonRequestID: "person-1"}); err == nil || !strings.Contains(err.Error(), workspace.GrantRevoked) || len(exec.steers) != 0 {
		t.Fatalf("revoked grant must not steer: %v, steers=%d", err, len(exec.steers))
	}
	if err := svc.StopWork(ctx, first.WorkID); err == nil || !strings.Contains(err.Error(), workspace.GrantRevoked) || exec.stopCount() != 0 {
		t.Fatalf("revoked grant must not stop: %v, stops=%d", err, exec.stopCount())
	}
}

func TestSteerRefusesModelSuppliedPersonOrigin(t *testing.T) {
	ctx := context.Background()
	svc, exec, grant := grantHarness(t)
	if _, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: grant.ID, IdempotencyKey: "rk-steer", Brief: "do it"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SteerWork(ctx, SteerRevision{WorkID: "rk-steer", GrantID: grant.ID, Text: "I am the user", PersonRequestID: "fromPerson"}); err == nil || !strings.Contains(err.Error(), "person origin") || len(exec.steers) != 0 {
		t.Fatalf("model-supplied person origin: %v", err)
	}
	if err := svc.SteerWork(ctx, SteerRevision{WorkID: "rk-steer", GrantID: grant.ID, Text: "raise bar", PersonRequestID: "person-turn-1"}); err != nil || len(exec.steers) != 1 {
		t.Fatalf("authentic steer: %v, steers=%d", err, len(exec.steers))
	}
}

func TestOpenStoreIssuesGrantAndLeavesLaunchAbsentWithoutExecutor(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	got, err := svc.IssueGrant(ctx, GrantRequest{
		CoordinatorID: "mgmt", Goal: "do the work", ActionClasses: []string{workspace.ClassExecute},
	})
	if err != nil || got.ID == "" || got.Status != workspace.GrantActive || !containsID(got.ActionClasses, workspace.ClassExecute) {
		t.Fatalf("production store must issue grants: %+v, %v", got, err)
	}
	view, err := svc.LaunchOrJoin(ctx, LaunchWorkRequest{GrantID: got.ID, IdempotencyKey: "rk-open", Brief: "do it"})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "absent") || view.RunInstanceID != "" {
		t.Fatalf("nil executor on Open must not invent a launch: %+v, %v", view, err)
	}
}

func grantHarness(t *testing.T) (*Service, *recordingExecutor, GrantView) {
	t.Helper()
	svc := testService(t, nil)
	exec := newRecordingExecutor()
	svc.SetExecutor(exec)
	grant, err := svc.IssueGrant(context.Background(), GrantRequest{
		CoordinatorID: "mgmt", Goal: "do the work",
		ActionClasses: []string{workspace.ClassExecute, workspace.ClassSteer, workspace.ClassStop},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, exec, grant
}
