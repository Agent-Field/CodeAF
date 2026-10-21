package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"net/http"
	"strings"
	"time"
)

// Taking over revokes the automatic worker before opening an ordinary chat.
// Resuming reconciles the worktree through the pipeline, never publishes it blindly.
func (h *Handler) controlJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Mode   string      `json:"mode"`
		Words  string      `json:"words,omitempty"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "control", body.Expect) || !h.owns(w, r, it) {
		return
	}
	if body.Mode == factory.AutonomyAuto || body.Mode == factory.AutonomyManual {
		if strings.TrimSpace(body.Words) != "" {
			h.fail(w, 422, "Auto/Manual mode changes do not accept instructions")
			return
		}
		h.setAutonomy(w, r, it, body.Mode)
		return
	}
	if factory.JobState(it) == api.StateDone || it.Publishing {
		h.fail(w, 409, "Finished or publishing jobs cannot change control.")
		return
	}
	if body.Mode != "take_over" && body.Mode != "resume_pipeline" {
		h.fail(w, 422, "mode must be take_over or resume_pipeline")
		return
	}
	if body.Mode == "take_over" && factory.UnderWay(it) {
		if h.seam.Stop == nil {
			h.fail(w, 422, "This runner cannot stop automatic work.")
			return
		}
		if err := h.seam.Stop(it.ID); h.failed(w, err) {
			return
		}
	}
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.Manual = body.Mode == "take_over"
		cur.StopUndo = nil
		cur.Approval = nil
		if cur.Manual {
			if cur.PullRequest != nil && cur.FollowUp != nil {
				factory.EnsureFollowPR(cur)
			}
			cur.Question, cur.QKind = "Automation paused. Continue in the working chat; Resume pipeline checks your changes and continues.", "manual"
			cur.State = factory.StateNeedsYou
			return nil
		}
		cur.Question, cur.QKind = "", ""
		cur.Failure = nil
		cur.Asking = nil
		if words := strings.TrimSpace(body.Words); words != "" {
			cur.Notes = append(cur.Notes, words)
		}
		if cur.PullRequest != nil && cur.PullRequest.State == factory.PROpen {
			if cur.FollowUp == nil {
				factory.WatchPublishedPR(cur)
			}
			f := cur.FollowUp
			if h.config.Record != nil {
				if consent, e := h.config.Record.Trailer(r.Context(), cur.ID); e == nil {
					f.PublishConsent = consent
					f.AutoPush = true
				}
			}
			f.State = "watching"
			f.EventKey = ""
			f.Attempt = 0
			f.Request = "The person resumed the pipeline. Inspect the current worktree and manual conversation changes, verify them, and maintain this PR. " + strings.TrimSpace(body.Words)
			cur.State = factory.StateShipped
			factory.QueueCIFollowUp(cur, time.Now().UTC())
		} else if cur.Stream != nil {
			cur.Stream.Paused = false
			cur.Stream.Ended = time.Time{}
			cur.State = factory.StateQueued
			i := cur.Stream.Cur
			if i >= 0 && i < len(cur.Stream.Phases) {
				cur.Stream.Phases[i].State = factory.PhaseRunning
				cur.Stream.Phases[i].Result = nil
				cur.Stream.Phases[i].Failure = nil
			}
		}
		return nil
	})
	h.finish(w, err)
}
