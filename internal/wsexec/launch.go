package wsexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// LaunchOrJoin follows equivalent in-flight work or starts one owned run.
// Two discussions of the same issue may share one binding. Critique-only
// (grant lacks ClassExecute) is not a launch and is not blocked (A10).
func (a *Adapter) LaunchOrJoin(ctx context.Context, req LaunchRequest) (WorkView, error) {
	if err := a.require(); err != nil {
		return WorkView{}, err
	}
	if err := validateLaunch(req); err != nil {
		return WorkView{}, err
	}
	grant, err := a.authorizeLaunch(ctx, req.GrantID)
	if err != nil {
		return WorkView{}, err
	}
	if !grantHas(grant, ClassExecute) {
		return WorkView{GrantID: grant.ID}, nil
	}
	if view, ok, err := a.joinExisting(ctx, req); err != nil || ok {
		return view, err
	}
	return a.admitNew(ctx, req)
}

func (a *Adapter) joinExisting(ctx context.Context, req LaunchRequest) (WorkView, bool, error) {
	if view, ok, err := a.joinBy(ctx, req.EquivalenceKey, a.store.BindingByEquivalence); err != nil || ok {
		return view, ok, err
	}
	return a.joinBy(ctx, req.RequestKey, a.store.BindingByRequestKey)
}

func (a *Adapter) joinBy(ctx context.Context, key string, lookup func(context.Context, string) (ExecutionBinding, error)) (WorkView, bool, error) {
	if strings.TrimSpace(key) == "" {
		return WorkView{}, false, nil
	}
	got, err := lookup(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return WorkView{}, false, nil
	}
	if err != nil {
		return WorkView{}, false, err
	}
	if !joinable(got.State) {
		return WorkView{}, false, nil
	}
	return bindingView(got, true), true, nil
}

func (a *Adapter) admitNew(ctx context.Context, req LaunchRequest) (WorkView, error) {
	stored, err := a.store.PutBinding(ctx, reservedBinding(req))
	if err != nil {
		return WorkView{}, err
	}
	if stored.RunInstanceID != "" || !needsAdmit(stored) {
		return bindingView(stored, true), nil
	}
	found, ok, err := a.runtime.FindByRequestKey(ctx, stored.RequestKey)
	if err != nil {
		return WorkView{}, err
	}
	if ok {
		return a.bindFound(ctx, stored.RequestKey, found, true)
	}
	admitted, err := a.runtime.Admit(ctx, AdmitRequest{
		RequestKey:  stored.RequestKey,
		Brief:       req.Brief,
		Road:        stored.Road,
		OwnerChatID: req.OwnerChatID,
	})
	if err != nil {
		return WorkView{}, err
	}
	return a.bindFound(ctx, stored.RequestKey, admitted, admitted.Already)
}

func needsAdmit(b ExecutionBinding) bool {
	return (b.State == BindReserved || b.State == "") && b.RunInstanceID == ""
}

func reservedBinding(req LaunchRequest) ExecutionBinding {
	return ExecutionBinding{
		RequestKey:     req.RequestKey,
		EquivalenceKey: req.EquivalenceKey,
		WorkID:         req.RequestKey,
		GrantID:        req.GrantID,
		CoordinatorID:  req.CoordinatorID,
		OwnerChatID:    req.OwnerChatID,
		AssignmentRev:  req.AssignmentRev,
		GrantRev:       req.GrantRev,
		State:          BindReserved,
		Road:           currentRoad(),
		Owner:          req.CoordinatorID,
	}
}

func (a *Adapter) bindFound(ctx context.Context, requestKey string, found AdmitResult, joined bool) (WorkView, error) {
	bound, err := a.store.BindRuntime(ctx, requestKey, found.RunInstanceID, found.RuntimeRef)
	if err != nil {
		return WorkView{}, err
	}
	view := bindingView(bound, joined)
	if found.Road != "" {
		view.Road = found.Road
	}
	return view, nil
}

// Recover finds a request-key execution the runtime already accepted and
// binds it. It must not call Admit when the runtime already has that key (A14).
func (a *Adapter) Recover(ctx context.Context, requestKey string) (WorkView, error) {
	if err := a.require(); err != nil {
		return WorkView{}, err
	}
	if strings.TrimSpace(requestKey) == "" {
		return WorkView{}, fmt.Errorf("%w: request key is empty", ErrInvalid)
	}
	held, err := a.store.BindingByRequestKey(ctx, requestKey)
	if err != nil {
		return WorkView{}, err
	}
	if held.RunInstanceID != "" {
		return bindingView(held, false), nil
	}
	found, ok, err := a.runtime.FindByRequestKey(ctx, requestKey)
	if err != nil {
		return WorkView{}, err
	}
	if !ok {
		return bindingView(held, false), nil
	}
	return a.bindFound(ctx, requestKey, found, false)
}
