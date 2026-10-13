package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// presentJob runs after solo permissions and presence have narrowed the job.
// A button appears only when its engine door can handle the current facts.
func (h *Handler) presentJob(r *http.Request, j *api.Job) {
	api.PresentJob(j)
	active := j.State == api.StateRunning || j.State == api.StateQueued || j.State == api.StateNeedsYou
	owner := j.Can.Stop || j.Can.Run || j.Can.RunAgain || j.Can.CancelJob
	if h.mode() == api.FactorySolo {
		owner = true
	}
	step, stoppable, rerunnable := false, false, false
	if j.Step != nil {
		for _, s := range j.Steps {
			if s.Name == j.Step.Name && s.InRun {
				step = true
				stoppable = s.State == api.StepRunning || s.State == api.StepWaiting
				rerunnable = s.State != api.StepPending
				break
			}
		}
	}
	mutable := j.State != api.StateDone && j.Publishing == nil
	j.Controls.StopStep = owner && active && stoppable && mutable && h.workers != nil && h.workers.desk != nil
	j.Controls.RerunStep = owner && step && rerunnable && mutable && h.workers != nil && h.workers.desk != nil
	j.Controls.EditSteps = owner && mutable && h.seam.Edit != nil
	j.Controls.ChangeAssignee = j.Can.ChangeAssignee && h.config.Store != nil
	local := j.Desk != nil && j.Desk.Origin != string(factory.OriginForge)
	session, _ := h.credential(r)
	hasToken := session.githubToken != ""
	if !hasToken && h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		hasToken = h.config.GitHubToken(r.Context()) != ""
	}
	j.Controls.Sync = owner && ((local && hasToken && h.config.Store != nil) || (!local && h.seam.Refresh != nil))
}
