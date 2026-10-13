package connect

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const waitLifetime = 7 * 24 * time.Hour

type WaitError struct {
	Wait  bridge.ConnectorWait
	cause error
}

func (e *WaitError) Error() string                       { return e.cause.Error() }
func (e *WaitError) Unwrap() error                       { return e.cause }
func (e *WaitError) ConnectorWait() bridge.ConnectorWait { return e.Wait }

// WaitingRuns resumes the exact journaled step, preserving its send approval.
type WaitingRuns interface {
	ConnectorChanged(context.Context, bridge.ConnectorWait, bool) error
}

func connectionError(reason string) *bridge.Error {
	return &bridge.Error{Code: bridge.CodeConflict, Message: reason, Details: map[string]any{"reason": reason}}
}
func (s *Service) waitError(ctx context.Context, scope svc.Scope, service string, err error) error {
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != bridge.CodeConflict {
		return err
	}
	reason, _ := typed.Details["reason"].(string)
	if reason != bridge.ReasonNeedsAllow && reason != bridge.ReasonExpired {
		return err
	}
	member := scope.Caller.Member
	if owner, ok := typed.Details["member"].(string); ok && owner != "" {
		member = owner
	}
	at := s.now().UTC()
	call := invocation(ctx)
	wait := bridge.ConnectorWait{App: scope.App, RunID: call.RunID, Step: call.Step, Service: service, Member: member, Reason: reason, Since: at.Format(time.RFC3339Nano), StopsAt: at.Add(waitLifetime).Format(time.RFC3339Nano)}
	wait, added, saveErr := s.waits.put(ctx, wait)
	if saveErr != nil {
		return bridge.Errorf(bridge.CodeInternal, "could not save connection wait")
	}
	if added {
		s.publish(bridge.TopicConnectWaits(member), wait)
		if reason == bridge.ReasonNeedsAllow {
			s.publishAllow(ctx, scope, service)
		}
	}
	return &WaitError{Wait: wait, cause: err}
}

func (s *Service) Waits(ctx context.Context, caller svc.Caller) (bridge.ConnectWaitsResult, error) {
	waits, err := s.waits.list(ctx)
	if err != nil {
		return bridge.ConnectWaitsResult{}, err
	}
	result := bridge.ConnectWaitsResult{Waits: []bridge.ConnectorWait{}}
	for _, wait := range waits {
		if wait.Member == caller.Member {
			result.Waits = append(result.Waits, wait)
		}
	}
	return result, nil
}

func (s *Service) Resume(service, member string) error {
	s.resumeMu.Lock()
	defer s.resumeMu.Unlock()
	ctx := s.cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	waits, err := s.waits.list(ctx)
	if err != nil {
		return err
	}
	var result error
	for _, wait := range waits {
		if wait.Service == service && wait.Member == member {
			if err := s.finishWait(ctx, wait, s.due(wait)); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}

func (s *Service) due(wait bridge.ConnectorWait) bool {
	until, err := time.Parse(time.RFC3339Nano, wait.StopsAt)
	return err == nil && !s.now().Before(until)
}

func (s *Service) finishWait(ctx context.Context, wait bridge.ConnectorWait, stop bool) error {
	if wait.RunID != "" {
		if s.all == nil {
			return bridge.Errorf(bridge.CodeInternal, "runs cannot resume connection waits")
		}
		hook, ok := s.all.Runs.(WaitingRuns)
		if !ok {
			return bridge.Errorf(bridge.CodeInternal, "runs cannot resume connection waits")
		}
		// Keep the original deadline if the adapter discovers the token is still expired.
		if err := hook.ConnectorChanged(ctx, wait, stop); err != nil {
			return err
		}
		if err := s.waits.remove(ctx, wait); err != nil {
			return err
		}
	} else if err := s.waits.remove(ctx, wait); err != nil {
		return err
	}
	s.publish(bridge.TopicConnectWaits(wait.Member), wait)
	return nil
}

// Sweep replays persisted deadlines and detects grants renewed while this server was offline.
func (s *Service) Sweep(ctx context.Context) error {
	s.resumeMu.Lock()
	defer s.resumeMu.Unlock()
	waits, err := s.waits.list(ctx)
	if err != nil {
		return err
	}
	var result error
	for _, wait := range waits {
		stop := s.due(wait)
		if !stop {
			if s.all == nil || s.all.Connect.Grants == nil {
				continue
			}
			scope := svc.Scope{App: wait.App, Caller: svc.Caller{Member: wait.Member, Role: svc.RoleUser}, Data: svc.DataReal}
			if _, err := s.all.Connect.Grants.Resolve(ctx, scope, wait.Service); err != nil {
				continue
			}
		}
		if err := s.finishWait(ctx, wait, stop); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
func (s *Service) watch(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer s.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Sweep(ctx)
		}
	}
}
func (s *Service) Close() error { return s.waits.close() }

type allowSheets interface {
	AllowSheet(context.Context, svc.Scope, bridge.AllowSheetParams) (bridge.AllowSheet, error)
}

func (s *Service) publishAllow(ctx context.Context, scope svc.Scope, service string) {
	if sheets, ok := s.all.Connect.Grants.(allowSheets); ok {
		sheet, err := sheets.AllowSheet(ctx, scope, bridge.AllowSheetParams{Service: service})
		if err == nil {
			s.publish(bridge.TopicConnectAllow(scope.Caller.Member), sheet)
		}
	}
}
