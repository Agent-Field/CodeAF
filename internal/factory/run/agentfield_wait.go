package run

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// SyncAgentFieldWait makes the job's Needs you agree with its AgentField
// run's pause for a person ([factory.AgentFieldWait]): a run that waits for
// approval puts the job in Needs you with the reasoner's question, and a run
// no longer waiting takes that question down and the job back to running.
//
// THE PAUSE IS THE CONTROL PLANE'S, AND THIS IS ONLY ITS SHADOW. The control
// plane holds the one approval state; whoever answers it first — this
// factory's own door, hax, the control plane's UI — wins, and the next read of
// the run says so. So a question answered somewhere else clears here by
// itself, and a re-plan after "Ask for changes" is a new pause whose new body
// is what the person reads. It answers whether it changed the job.
func SyncAgentFieldWait(it *factory.Item, now time.Time) bool {
	w := factory.AgentFieldWaiting(*it)
	switch {
	case w != nil:
		q := factory.AgentFieldQuestion(*w)
		if it.State == factory.StateNeedsYou && it.QKind == factory.QKindAgentField && it.Question == q {
			return false
		}
		if it.State != factory.StateRunning && it.State != factory.StateNeedsYou {
			return false
		}
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = q, factory.QKindAgentField
		it.Asking = nil
		ensureStream(it, now)
		loopSay(it, now, "ask", q)
		return true
	case it.QKind == factory.QKindAgentField:
		it.Question, it.QKind = "", ""
		it.Asking = nil
		if it.State == factory.StateNeedsYou {
			it.State = factory.StateRunning
		}
		ensureStream(it, now)
		loopSay(it, now, "said", "the AgentField run's approval was answered")
		return true
	}
	return false
}
