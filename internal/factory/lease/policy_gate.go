package lease

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// holdAtPolicy reads settings and an unlocked snapshot while this job's lock
// is held. Its own spend comes from the locked report, never the snapshot.
func (d *Desk) holdAtPolicy(it *factory.Item, now time.Time) bool {
	if it.Group == "" {
		return false
	}
	p, err := d.store.Policy(it.Group)
	reason := ""
	switch {
	case err != nil:
		reason = "its group's policy cannot be read"
	case p.Version == 0:
		return false
	case p.Stopped:
		if policyFinishingStep(*it, p) {
			return false
		}
		reason = "its group is stopped"
	case it.Cap > 0 && factory.JobLifetimeSpent(*it) >= it.Cap && !approvedWriteOnly(*it):
		// The existing job-budget gate retains its question when both caps apply.
		return false
	case it.PolicyRun == "":
		return false
	default:
		jobs, err := d.store.List()
		if err != nil {
			reason = "its group's policy cannot be read"
			break
		}
		spent := make(map[int]float64, len(jobs))
		for _, job := range jobs {
			spent[job.ID] = factory.JobLifetimeSpent(job)
		}
		spent[it.ID] = factory.JobLifetimeSpent(*it)
		var ok bool
		reason, ok = factory.ContinueDecision(p, *it, func(id int) float64 { return spent[id] }, now)
		if ok {
			return false
		}
	}
	if it.Blocked == nil || it.Blocked.By != factory.BlockPolicy || it.Blocked.Reason != reason {
		it.Blocked = &factory.JobBlock{By: factory.BlockPolicy, Reason: reason, Since: now.UTC()}
		it.Note(now, "held: "+reason)
		if d.record != nil {
			_, _ = d.record.Append(context.Background(), record.Event{Kind: record.Held, Job: it.ID, Actor: record.Actor{Kind: "factory"}, Rule: factory.BlockPolicy, Detail: reason, Cost: factory.JobLifetimeSpent(*it)})
		}
	}
	ask := factory.Asked{With: factory.AskedYou, At: now, Question: reason}
	if it.QKind == "cap" && it.Asking != nil {
		ask = *it.Asking
		ask.Question = reason
	}
	it.State, it.QKind, it.Question, it.Asking = factory.StateNeedsYou, "cap", reason, &ask
	it.PolicyRun, it.OverLimit, it.StopAfterStep = "", false, false
	return true
}

// A group stop lets an already paid step finish through Stop after this step.
// A queued job or a new step has no running phase begun before that decision.
func policyFinishingStep(it factory.Item, p factory.GroupPolicy) bool {
	if !it.StopAfterStep || it.State != factory.StateRunning || it.Stream == nil || p.UpdatedAt == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if ph.State == factory.PhaseRunning && !ph.Started.IsZero() && ph.Started.Before(*p.UpdatedAt) {
			return true
		}
	}
	return false
}
