package wsexec

import (
	"context"
	"fmt"
	"strings"
)

// Inspect returns the binding plus live runtime state when bound.
func (a *Adapter) Inspect(ctx context.Context, workID string) (WorkView, error) {
	if err := a.require(); err != nil {
		return WorkView{}, err
	}
	held, err := a.bindingByWork(ctx, workID)
	if err != nil {
		return WorkView{}, err
	}
	view := bindingView(held, false)
	if held.RunInstanceID == "" {
		return view, nil
	}
	live, err := a.runtime.Inspect(ctx, held.RunInstanceID)
	if err != nil {
		return WorkView{}, err
	}
	if live.State != "" {
		view.State = live.State
	}
	if live.RunInstanceID != "" {
		view.RunInstanceID = live.RunInstanceID
	}
	return view, nil
}

// Steer requires an authentic grant with ClassSteer and a citation of the
// original person request. Model-supplied person origin is refused on both
// roads (A11).
func (a *Adapter) Steer(ctx context.Context, rev SteerRevision) error {
	if err := a.require(); err != nil {
		return err
	}
	if err := refuseModelPerson(rev); err != nil {
		return err
	}
	grant, err := a.authorizeLaunch(ctx, rev.GrantID)
	if err != nil {
		return err
	}
	if !grantHas(grant, ClassSteer) {
		return fmt.Errorf("%w: grant lacks steer", ErrInvalid)
	}
	if rev.GrantRev != 0 && rev.GrantRev != grant.Revision {
		return fmt.Errorf("%w: grant revision does not match", ErrConflict)
	}
	held, err := a.bindingByWork(ctx, rev.WorkID)
	if err != nil {
		return err
	}
	if held.RunInstanceID == "" {
		return fmt.Errorf("%w: work is not bound", ErrInvalid)
	}
	return a.runtime.Steer(ctx, held.RunInstanceID, rev)
}

// PauseWork pauses an existing bound run. It is not pause coordination.
func (a *Adapter) PauseWork(ctx context.Context, workID string) error {
	return a.controlWork(ctx, workID, a.runtime.Pause)
}

// StopWork stops an existing bound run. A revoked grant cannot stop.
// History remains on the binding; this is not pause coordination.
func (a *Adapter) StopWork(ctx context.Context, workID string) error {
	if err := a.require(); err != nil {
		return err
	}
	held, err := a.bindingByWork(ctx, workID)
	if err != nil {
		return err
	}
	grant, err := a.store.GetGrant(ctx, held.GrantID)
	if err != nil {
		return err
	}
	if grant.Status != GrantActive {
		return fmt.Errorf("%w: grant is %s", ErrInvalid, grant.Status)
	}
	if !grantHas(grant, ClassStop) && !grantHas(grant, ClassExecute) {
		return fmt.Errorf("%w: grant lacks stop", ErrInvalid)
	}
	if held.RunInstanceID == "" {
		return fmt.Errorf("%w: work is not bound", ErrInvalid)
	}
	return a.runtime.Stop(ctx, held.RunInstanceID)
}

// Observe returns the runtime result, or a labelled pending view when the
// reserved row has no run-instance id yet. It never fabricates completed.
func (a *Adapter) Observe(ctx context.Context, workID string) (ResultView, error) {
	if err := a.require(); err != nil {
		return ResultView{}, err
	}
	held, err := a.bindingByWork(ctx, workID)
	if err != nil {
		return ResultView{}, err
	}
	if held.RunInstanceID == "" {
		return ResultView{WorkID: held.WorkID, State: held.State, Detail: "pending"}, nil
	}
	return a.runtime.Observe(ctx, held.RunInstanceID)
}

func (a *Adapter) controlWork(ctx context.Context, workID string, fn func(context.Context, string) error) error {
	if err := a.require(); err != nil {
		return err
	}
	held, err := a.bindingByWork(ctx, workID)
	if err != nil {
		return err
	}
	if held.RunInstanceID == "" {
		return fmt.Errorf("%w: work is not bound", ErrInvalid)
	}
	return fn(ctx, held.RunInstanceID)
}

func (a *Adapter) bindingByWork(ctx context.Context, workID string) (ExecutionBinding, error) {
	if strings.TrimSpace(workID) == "" {
		return ExecutionBinding{}, fmt.Errorf("%w: work id is empty", ErrInvalid)
	}
	return a.store.BindingByRequestKey(ctx, workID)
}
