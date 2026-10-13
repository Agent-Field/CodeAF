package settings

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) GetBudget(ctx context.Context, scope svc.Scope) (bridge.BudgetInfo, error) {
	def, err := s.definition(ctx, scope.App)
	if err != nil {
		return bridge.BudgetInfo{}, err
	}
	s.mu.Lock()
	p, err := s.read(scope.App)
	s.mu.Unlock()
	if err != nil {
		return bridge.BudgetInfo{}, err
	}
	return s.budget(ctx, scope, def.App, p, s.clock())
}

func (s *Service) SetBudget(ctx context.Context, scope svc.Scope, params bridge.SetBudgetParams) (bridge.BudgetInfo, error) {
	def, err := s.definition(ctx, scope.App)
	if err != nil {
		return bridge.BudgetInfo{}, err
	}
	if !canEditBudget(scope.Caller, def.App) {
		return bridge.BudgetInfo{}, bridge.Errorf(bridge.CodeDenied, "Owner or Admin required")
	}
	if !validMonth(params.Month) {
		return bridge.BudgetInfo{}, bridge.Errorf(bridge.CodeInvalid, "month must be a finite positive USD amount")
	}
	info, changed, err := s.setBudget(ctx, scope, def.App, params.Month)
	if err != nil || !changed {
		return info, err
	}
	line := fmt.Sprintf("@%s set %s budget to up to $%g a month · Undo", strings.TrimPrefix(scope.Caller.Member, "@"), def.App.Name, params.Month)
	return info, s.activity(ctx, scope, line)
}

func (s *Service) setBudget(ctx context.Context, scope svc.Scope, app *spec.App, month float64) (bridge.BudgetInfo, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.read(scope.App)
	if err != nil {
		return bridge.BudgetInfo{}, false, err
	}
	unchanged := p.Month != nil && *p.Month == month
	p.Month = &month
	info, err := s.budget(ctx, scope, app, p, s.clock())
	if err != nil || unchanged {
		return info, false, err
	}
	if err := s.write(scope.App, p); err != nil {
		return bridge.BudgetInfo{}, false, err
	}
	return info, true, nil
}

func validMonth(month float64) bool {
	return month > 0 && !math.IsNaN(month) && !math.IsInf(month, 0)
}

func canEditBudget(caller svc.Caller, app *spec.App) bool {
	// An app Editor is not a workspace Admin. Until the spaces role seam
	// lands, only Personal supplies the combined Owner and Editor authority.
	return !caller.Agent && caller.Member != "" && svc.RoleOf(caller) == svc.RoleEditor && (app.Space == "" || app.Space == "personal")
}

func (s *Service) budget(ctx context.Context, scope svc.Scope, app *spec.App, p preferences, at time.Time) (bridge.BudgetInfo, error) {
	spent, err := s.spent(ctx, scope, at)
	if err != nil {
		return bridge.BudgetInfo{}, err
	}
	month := app.Budget.Month
	if p.Month != nil {
		month = *p.Month
	}
	atCap := app.Budget.AtCap
	if atCap == "" {
		atCap = "ask"
	}
	return bridge.BudgetInfo{
		Month: month, Spent: spent, Currency: "USD", AtCap: atCap,
		ResetsAt: time.Date(at.Year(), at.Month()+1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Paused:   spent >= month, CanEdit: canEditBudget(scope.Caller, app), Presets: []float64{5, 10, 20, 50},
	}, nil
}

func (s *Service) spent(ctx context.Context, scope svc.Scope, at time.Time) (float64, error) {
	if s.all.Runs == nil {
		return 0, bridge.NotBuilt("runs")
	}
	page, err := s.all.Runs.List(ctx, scope, bridge.ListRunsParams{})
	if err != nil {
		return 0, err
	}
	var spent float64
	for _, run := range page.Runs {
		started, err := time.Parse(time.RFC3339, run.StartedAt)
		// The runs engine counts an undated journal entry in the current
		// month too, so damaged dates cannot silently hide spending.
		if err != nil || started.UTC().Format("2006-01") == at.UTC().Format("2006-01") {
			spent += run.Cost.Spent
		}
	}
	return spent, nil
}

func (s *Service) Month(app string) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.read(app)
	if err != nil || p.Month == nil {
		return 0, false
	}
	return *p.Month, true
}
