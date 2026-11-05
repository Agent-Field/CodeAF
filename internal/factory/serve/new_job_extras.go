package serve

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// checkNewJobExtras runs before any issue, job or startup write.
func (h *Handler) checkNewJobExtras(w http.ResponseWriter, r *http.Request, body *api.NewJob) bool {
	if err := validateNewJobExtras(body); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	if body.Group != "" || body.Milestone != "" || body.Budget != nil || body.SplitFrom != nil {
		if !h.requireVerb(w, h.config.Store != nil) {
			return false
		}
	}
	if body.SplitFrom == nil {
		return true
	}
	parent, err := h.config.Store.Get(*body.SplitFrom)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		h.failed(w, err)
		return false
	}
	visible := err == nil && factory.IsIssueJob(parent) && h.repoVisible(r, itemRepo(parent))
	if h.authFailed(w, r) {
		return false
	}
	if !visible {
		h.fail(w, http.StatusUnprocessableEntity, "split_from must name a visible job in this factory")
		return false
	}
	return true
}

func validateNewJobExtras(body *api.NewJob) error {
	if body.SplitFrom != nil && *body.SplitFrom < 1 {
		return fmt.Errorf("split_from must be a job number of at least 1")
	}
	for field, value := range map[string]*string{"group": &body.Group, "milestone": &body.Milestone} {
		if strings.ContainsFunc(*value, unicode.IsControl) || utf8.RuneCountInString(strings.TrimSpace(*value)) > factory.MaxJobLabel {
			return fmt.Errorf("%s must be at most %d characters with no control characters", field, factory.MaxJobLabel)
		}
		*value = strings.TrimSpace(*value)
	}
	if body.Budget != nil && !slices.Contains(factory.JobBudgetSteps(), *body.Budget) {
		return fmt.Errorf("budget must be one of %v US dollars", factory.JobBudgetSteps())
	}
	return nil
}

// stampNewJobExtras leaves the default create untouched and budgets before start.
func (h *Handler) stampNewJobExtras(r *http.Request, id int, body api.NewJob) error {
	if body.Group == "" && body.Milestone == "" && body.Budget == nil && body.SplitFrom == nil {
		return nil
	}
	stamp := func() error {
		return h.config.Store.Update(id, func(it *factory.Item) error {
			it.Group, it.Milestone = body.Group, body.Milestone
			if body.SplitFrom != nil {
				it.SplitFrom = *body.SplitFrom
				it.Note(h.clock(), fmt.Sprintf("split from #%d", it.SplitFrom))
			}
			if body.Budget != nil {
				it.Cap = *body.Budget
			}
			return nil
		})
	}
	if body.Budget != nil {
		return h.recordAction(r, factory.Item{ID: id}, record.BudgetChanged, fmt.Sprintf("$%.2f", *body.Budget), stamp)
	}
	return stamp()
}
