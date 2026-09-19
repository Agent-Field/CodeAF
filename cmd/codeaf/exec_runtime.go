package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsexec"
)

// workspaceExecStore copies grant and binding rows onto the adapter's types
// so wsexec never imports workspace internals. Errors map so join-by-key
// sees wsexec.ErrNotFound rather than the collections sentence.
type workspaceExecStore struct{ jobs *workspace.Store }

func (s *workspaceExecStore) PutBinding(ctx context.Context, b wsexec.ExecutionBinding) (wsexec.ExecutionBinding, error) {
	got, err := s.jobs.PutBinding(ctx, workspaceBindingOf(b))
	return execBindingOf(got), execStoreError(err)
}

func (s *workspaceExecStore) BindingByRequestKey(ctx context.Context, requestKey string) (wsexec.ExecutionBinding, error) {
	got, err := s.jobs.BindingByRequestKey(ctx, requestKey)
	return execBindingOf(got), execStoreError(err)
}

func (s *workspaceExecStore) BindingByEquivalence(ctx context.Context, equivalenceKey string) (wsexec.ExecutionBinding, error) {
	got, err := s.jobs.BindingByEquivalence(ctx, equivalenceKey)
	return execBindingOf(got), execStoreError(err)
}

func (s *workspaceExecStore) BindRuntime(ctx context.Context, requestKey, runInstanceID, runtimeRef string) (wsexec.ExecutionBinding, error) {
	got, err := s.jobs.BindRuntime(ctx, requestKey, runInstanceID, runtimeRef)
	return execBindingOf(got), execStoreError(err)
}

func (s *workspaceExecStore) RecordJoiner(ctx context.Context, requestKey, chatID string) (wsexec.ExecutionBinding, error) {
	got, err := s.jobs.RecordJoiner(ctx, requestKey, chatID)
	return execBindingOf(got), execStoreError(err)
}

func (s *workspaceExecStore) GetGrant(ctx context.Context, id string) (wsexec.Grant, error) {
	got, err := s.jobs.GetGrant(ctx, id)
	return execGrantOf(got), execStoreError(err)
}

func execStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workspace.ErrNotFound):
		return wsexec.ErrNotFound
	case errors.Is(err, workspace.ErrConflict):
		return fmt.Errorf("%w: binding", wsexec.ErrConflict)
	default:
		return err
	}
}

func execGrantOf(g workspace.Grant) wsexec.Grant {
	return wsexec.Grant{
		ID: g.ID, Goal: g.Goal, CoordinatorID: g.CoordinatorID, ScopeKind: g.ScopeKind, FolderID: g.FolderID,
		SnapshotJSON: g.SnapshotJSON, ActionJSON: g.ActionJSON, Issuer: g.Issuer, Origin: g.Origin, Actor: g.Actor,
		Status: g.Status, BudgetUSD: g.BudgetUSD, Revision: g.Revision, RevocationRevision: g.RevocationRevision,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
}

func execBindingOf(b workspace.ExecutionBinding) wsexec.ExecutionBinding {
	return wsexec.ExecutionBinding{
		ID: b.ID, RequestKey: b.RequestKey, EquivalenceKey: b.EquivalenceKey, WorkID: b.WorkID,
		RunInstanceID: b.RunInstanceID, Road: b.Road, OwnerChatID: b.OwnerChatID, GrantID: b.GrantID,
		CoordinatorID: b.CoordinatorID, RuntimeRef: b.RuntimeRef, AssignmentRev: b.AssignmentRev, GrantRev: b.GrantRev,
		State: b.State, Fence: b.Fence, Owner: b.Owner, LeaseUntil: b.LeaseUntil,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, BoundAt: b.BoundAt, AdmittedAt: b.AdmittedAt,
		JoinerJSON: b.JoinerJSON,
	}
}

func workspaceBindingOf(b wsexec.ExecutionBinding) workspace.ExecutionBinding {
	return workspace.ExecutionBinding{
		ID: b.ID, RequestKey: b.RequestKey, EquivalenceKey: b.EquivalenceKey, WorkID: b.WorkID,
		RunInstanceID: b.RunInstanceID, Road: b.Road, OwnerChatID: b.OwnerChatID, GrantID: b.GrantID,
		CoordinatorID: b.CoordinatorID, RuntimeRef: b.RuntimeRef, AssignmentRev: b.AssignmentRev, GrantRev: b.GrantRev,
		State: b.State, Fence: b.Fence, Owner: b.Owner, LeaseUntil: b.LeaseUntil,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, BoundAt: b.BoundAt, AdmittedAt: b.AdmittedAt,
		JoinerJSON: b.JoinerJSON,
	}
}

// liveExecRuntime maps Admit onto the owning conversation's StartTask door.
// A missing live agent is labelled absence, never a fabricated completed run.
type liveExecRuntime struct {
	mu     sync.Mutex
	agents map[string]*session.Agent
	byRun  map[string]execLive
}

func (r *liveExecRuntime) bind(chatID string, agent *session.Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[chatID] = agent
}

func (r *liveExecRuntime) agent(chatID string) *session.Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agents[strings.TrimSpace(chatID)]
}

func (r *liveExecRuntime) Admit(ctx context.Context, req wsexec.AdmitRequest) (wsexec.AdmitResult, error) {
	agent := r.agent(req.OwnerChatID)
	if agent == nil {
		return wsexec.AdmitResult{}, wsexec.ErrAbsent
	}
	id, _, already, err := agent.AdmitTask(ctx, req.Brief, req.RequestKey)
	if err != nil {
		return wsexec.AdmitResult{}, err
	}
	runID := strconv.FormatUint(id, 10)
	r.rememberRun(runID, execLive{agent: agent, chatID: req.OwnerChatID, requestKey: req.RequestKey, road: req.Road, id: id})
	return wsexec.AdmitResult{RunInstanceID: runID, RuntimeRef: "session:" + runID, Road: req.Road, Already: already}, nil
}

func (r *liveExecRuntime) rememberRun(runID string, live execLive) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byRun[runID] = live
}

func (r *liveExecRuntime) FindByRequestKey(_ context.Context, requestKey string) (wsexec.AdmitResult, bool, error) {
	requestKey = strings.TrimSpace(requestKey)
	for _, agent := range r.agentsSnapshot() {
		id, _, ok := agent.TaskByRequestKey(requestKey)
		if !ok {
			continue
		}
		runID := strconv.FormatUint(id, 10)
		return wsexec.AdmitResult{RunInstanceID: runID, RuntimeRef: "session:" + runID}, true, nil
	}
	return wsexec.AdmitResult{}, false, nil
}

func (r *liveExecRuntime) agentsSnapshot() []*session.Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	agents := make([]*session.Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}
	return agents
}

func (r *liveExecRuntime) Inspect(_ context.Context, runInstanceID string) (wsexec.WorkView, error) {
	live, err := r.lookup(runInstanceID)
	if err != nil {
		return wsexec.WorkView{}, err
	}
	view, err := live.agent.InspectAdmitted(runInstanceID)
	if err != nil {
		view, err = live.agent.InspectAdmitted(live.requestKey)
	}
	if err != nil {
		return wsexec.WorkView{}, err
	}
	return wsexec.WorkView{
		WorkID: view.WorkID, RequestKey: view.RequestKey, RunInstanceID: view.RunInstanceID,
		Road: view.Road, State: view.State, OwnerChatID: live.chatID,
	}, nil
}

func (r *liveExecRuntime) Steer(_ context.Context, runInstanceID string, rev wsexec.SteerRevision) error {
	live, err := r.lookup(runInstanceID)
	if err != nil {
		return err
	}
	grant := session.ExecGrant{ID: rev.GrantID, Status: "active", Classes: []string{"steer"}}
	if err := live.agent.SteerDelegated(runInstanceID, rev.Text, rev.PersonRequestID, grant); err == nil {
		return nil
	}
	return live.agent.SteerDelegated(live.requestKey, rev.Text, rev.PersonRequestID, grant)
}

func (r *liveExecRuntime) Pause(_ context.Context, runInstanceID string) error {
	return r.control(runInstanceID, (*session.Agent).PauseAdmitted)
}

func (r *liveExecRuntime) Stop(_ context.Context, runInstanceID string) error {
	return r.control(runInstanceID, (*session.Agent).StopAdmitted)
}

func (r *liveExecRuntime) Observe(_ context.Context, runInstanceID string) (wsexec.ResultView, error) {
	live, err := r.lookup(runInstanceID)
	if err != nil {
		return wsexec.ResultView{}, err
	}
	got, err := live.agent.ObserveAdmitted(runInstanceID)
	if err != nil {
		got, err = live.agent.ObserveAdmitted(live.requestKey)
	}
	if err != nil {
		return wsexec.ResultView{}, err
	}
	return wsexec.ResultView{WorkID: got.WorkID, RunInstanceID: got.RunInstanceID, State: got.State, Detail: got.Detail}, nil
}

func (r *liveExecRuntime) control(runInstanceID string, fn func(*session.Agent, string) error) error {
	live, err := r.lookup(runInstanceID)
	if err != nil {
		return err
	}
	if err := fn(live.agent, runInstanceID); err == nil {
		return nil
	}
	return fn(live.agent, live.requestKey)
}

func (r *liveExecRuntime) lookup(runInstanceID string) (execLive, error) {
	runInstanceID = strings.TrimSpace(runInstanceID)
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := r.byRun[runInstanceID]
	if ok && got.agent != nil {
		return got, nil
	}
	for _, live := range r.byRun {
		if live.requestKey == runInstanceID && live.agent != nil {
			return live, nil
		}
	}
	return execLive{}, wsexec.ErrNotFound
}
