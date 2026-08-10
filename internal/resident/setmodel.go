package resident

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Changing the model under a running job used to mean cancelling it. Restart
// inheritance carried a pin forward onto fresh nodes, and nothing at all moved
// a pin on nodes that already existed — so the only way to answer "this is
// going badly, use the better one" was to throw the work away and ask again.
//
// This arm is the other answer, and it is deliberately the calm one: it moves
// the pin and touches nothing else. Steps that have not started pick up the new
// model when they are claimed; steps already in flight finish on the model they
// were handed. Wanting it immediate is two explicit acts — cancel the turn, then
// switch — and never one silent one.

// setModel applies CommandSetModel: Target names the subtree, Instruction names
// the model. The model is carried in the instruction rather than in a new
// column for the reason CommandExpedite records — replay stays a switch on the
// verb, and no command row grows a field for one kind's sake. Reasoning effort
// rides inside the same string, because the pin it writes is one slug and the
// graph has never had a second axis to put an effort on.
func (r *Reconciler) setModel(command store.Command) (commandOutcome, error) {
	model := strings.TrimSpace(command.Instruction)
	if model == "" {
		return modelRefusal("no model was named"), nil
	}
	rebinding, err := r.store.SetSubtreeWorkModel(command.Target, model, "asked mid-run")
	if err != nil {
		// A target that finished, folded, or was never there is news for the
		// person, not a fault for the log: the funnel checked it when the
		// command was written and a job is allowed to move on in between.
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
			return modelRefusal(err.Error()), nil
		}
		return commandOutcome{}, err
	}
	if len(rebinding.Nodes) == 0 {
		return modelRefusal(fmt.Sprintf("nothing left under %s is still to be done on another model", command.Target)), nil
	}
	receipt := fmt.Sprintf("switching remaining work to %s — %d %s move at their next step",
		model, len(rebinding.Nodes), plural(len(rebinding.Nodes), "step", "steps"))
	if rebinding.Running > 0 {
		receipt += fmt.Sprintf(", %d already running %s on the model they started on",
			rebinding.Running, plural(rebinding.Running, "finishes", "finish"))
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  fmt.Sprintf("repointed %d live nodes under %s to %s", len(rebinding.Nodes), rebinding.Root, model),
		receipt: receipt,
	}, nil
}

// modelRefusal keeps every way this can decline in one voice. A refusal speaks
// rather than files — receiptVoice's rule — so it is the only line the person
// sees, and it has to say what did not happen.
func modelRefusal(reason string) commandOutcome {
	return commandOutcome{
		status:  store.CommandRejected,
		result:  reason,
		receipt: "I couldn't switch models: " + reason,
	}
}
