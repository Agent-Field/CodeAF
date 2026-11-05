package lease

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// holdAtPolicy reads settings and a bounded, non-migrating spend snapshot
// while this job's lock is held. A lease has no admitted report steps.
func (d *Desk) holdAtPolicy(it *factory.Item, now time.Time, admitted ...map[policyStep]bool) bool {
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
		if len(admitted) != 0 && !policyNewStep(*it, admitted[0]) {
			it.StopAfterStep = true
			return false
		}
		reason = "its group is stopped"
	case it.Cap > 0 && factory.JobLifetimeSpent(*it) >= it.Cap && !approvedWriteOnly(*it):
		// The existing job-budget gate retains its question when both caps apply.
		return false
	case it.PolicyRun == "":
		return false
	default:
		var ok bool
		reason, ok = factory.ContinueDecision(p, *it, d.policySpentOf(p, *it, now), now)
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
