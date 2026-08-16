package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// What stood here was the single door between a ROUTED decision and the journal:
// one command, chosen as the terminal act of a turn that could not revisit it,
// with the optimistic reply worded before the row existed. The law at that door
// — the reply is posted on the far side of a journaled row or it is never posted
// at all — survives entirely, and it is now the law of every acting tool: a tool
// that could not journal returns an error the loop must speak to, and a tool
// that did returns what it did. What is gone is the door's monopoly.
//
// The candidate reader below survives too. A verb that knows what it wants to do
// and not what to do it to is still an ordinary thing to say, and the union of
// "live jobs the words reach" and "standing rules the words reach" is still the
// only honest answer to it. It is a READ now: the control tool hands the list
// back to the loop, which names the ids, rather than posting a question the loop
// would then talk over.

// describedTarget is one thing a verb with no target could have meant: a job on
// the board, or a standing rule. The two are matched by machinery that already
// exists and answers the same question in its own half of the world; this only
// puts the two answers in one list, because the person asking "stand down the
// stretch reminder" did not say which half they were pointing at and should not
// have to.
type describedTarget struct {
	job  store.SurgeryTarget
	rule store.Charter
}

func (candidate describedTarget) isRule() bool { return candidate.rule.ID != "" }

// describedTargetCap bounds one candidate list. It is the charter askback's cap
// for the same reason: past a handful, this is a list to search rather than a
// choice to make.
const describedTargetCap = charterAskbackCap

// describedTargets is the union of the two candidate readers the head already
// trusts: the surgery search over live work, and the charter search over
// standing rules. A rule joins the list only for the verbs a rule can actually
// answer — the mapping below is the store's own table, not a guess — so a
// candidate offered here is always a candidate that can be journaled.
func (h *Head) describedTargets(kind store.CommandKind, description string) ([]describedTarget, error) {
	lower := strings.ToLower(strings.TrimSpace(description))
	jobs, err := h.surgeryMatches(surgeryIntent{
		Kind: kind, Reference: surgeryReference(lower),
		IncludeLeaves: kind == store.CommandAmend || kind == store.CommandReprioritize ||
			kind == store.CommandRestart,
	})
	if err != nil {
		return nil, err
	}
	if len(jobs) > describedTargetCap {
		jobs = jobs[:describedTargetCap]
	}
	candidates := make([]describedTarget, 0, len(jobs)+describedTargetCap)
	for _, job := range jobs {
		candidates = append(candidates, describedTarget{job: job})
	}
	if _, ok := charterTransition(kind); !ok {
		return candidates, nil
	}
	rules, err := h.charterCandidates(charterReference(lower, ""))
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		candidates = append(candidates, describedTarget{rule: rule})
	}
	return candidates, nil
}

// charterTransition maps a verb aimed at work onto the durable transition the
// same verb means for a standing rule. Only these two exist: standing a rule
// down is retiring it, and holding it is pausing it. A verb with no entry here
// simply cannot be about a rule, and the candidate list says so by leaving
// rules out rather than by offering one it would have to refuse.
func charterTransition(kind store.CommandKind) (store.CommandKind, bool) {
	switch kind {
	case store.CommandCancel:
		return store.CommandCharterRetire, true
	case store.CommandPause:
		return store.CommandCharterPause, true
	default:
		return "", false
	}
}
