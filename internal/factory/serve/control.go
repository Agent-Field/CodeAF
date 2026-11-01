package serve

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
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
	if body.Mode == "resume_pipeline" && it.QKind == "results_not_saved" {
		if h.workers == nil {
			h.fail(w, 422, "Saved results need the original runner to reconnect.")
			return
		}
		err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if cur.QKind != "results_not_saved" || cur.Manual {
				return fmt.Errorf("the result-save block changed")
			}
			cur.ReportRetryAt = time.Now().UTC()
			return nil
		})
		if err == nil {
			_, err = h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalReconcile})
		}
		h.finish(w, err)
		return
	}
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		wasManual := cur.Manual
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
		prOpen := cur.PullRequest != nil && cur.PullRequest.State == factory.PROpen
		if wasManual && !prOpen && cur.Stream != nil {
			cur.Notes = append(cur.Notes, factory.GaveBackNote)
		}
		if words := strings.TrimSpace(body.Words); words != "" {
			cur.Notes = append(cur.Notes, words)
		}
		if prOpen {
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
			if f.Kind == "readiness" && !wasManual && strings.TrimSpace(body.Words) == "" {
				// Retry the failed final review, including the same snapshot.
				// A generic repair request would force edits instead and the
				// old reviewing cursor would suppress an unchanged-head retry.
				f.Readiness = nil
				f.Request = ""
			} else {
				f.Request = "The person resumed the pipeline. Inspect the current worktree and manual conversation changes, verify them, and maintain this PR. " + strings.TrimSpace(body.Words)
			}
			cur.State = factory.StateShipped
			factory.QueueCIFollowUp(cur, time.Now().UTC())
		} else if cur.Stream != nil {
			// GIVING BACK A TAKEN-OVER JOB CHECKS THE PERSON'S COMMITS AGAIN:
			// the run goes on from the first check or review that had already
			// passed, with every step after it, and every step reads that the
			// branch changed by hand.
			if wasManual {
				if from := factory.CheckAgainFrom(cur.Stages, cur.Stream.Phases, cur.Stream.Cur); from < cur.Stream.Cur {
					return run.StepControl(cur, cur.Stream.Phases[from].Name, true, "", time.Now().UTC())
				}
			}
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
