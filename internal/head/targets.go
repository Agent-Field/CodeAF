package head

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/store"
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
// and not what to do it to is still an ordinary thing to say, and the live jobs
// the words reach are the only honest answer to it. It is a READ now: the
// control tool hands the list back to the loop, which names the ids, rather than
// posting a question the loop would then talk over. Standing rules used to join
// the list beside the jobs; they went with the v1 scheduler.

// describedTarget is one thing a verb with no target could have meant: a job on
// the board.
type describedTarget struct {
	job store.SurgeryTarget
}

// describedTargetCap bounds one candidate list: past a handful, this is a list
// to search rather than a choice to make.
const describedTargetCap = 4

// describedTargets is the surgery search over live work, the candidate reader
// the head already trusts, capped to a choice a person can make.
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
	candidates := make([]describedTarget, 0, len(jobs))
	for _, job := range jobs {
		candidates = append(candidates, describedTarget{job: job})
	}
	return candidates, nil
}
