package session

// The stage contract: how a conversation that IS one stage of a factory item
// hands its round back to the runner. It is the factory contract's shape
// (factory_contract.go) with a different door, and it raises no card.
//
// A STAGE CONVERSATION IS STILL A CONVERSATION. It reads, edits, runs and hands
// parts out to tasks exactly as any other does; what is different about it is
// that somebody is waiting on its answer in code. The runner
// (internal/factory/run's chat executor) opens it with the stage's brief, and
// the round is over when the conversation has called `stage_result` and
// stopped, or has stopped without calling it, which the runner records as a
// stage that did not report.
//
// THE MODEL FILLS THE RESULT AND CODE DECIDES WHAT IT MEANS. Whether `until
// clean` was met is [factory.Met] asked of what was reported, never the
// model's own word; and a plan's proposed edit reaches the item only through
// [factory.Adapt], under the recipe's bounds. Neither tool posts anywhere,
// changes the item or asks the person anything.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// StageDoor is what one stage conversation reports through. The runner sets
// it on the conversation it opens for a stage round, and on nothing else.
//
//   - Report hands the round's result back. The runner keeps the first one and
//     refuses a second with a sentence the model reads.
//   - Edit hands the runner a proposal to change the item's stages. Nothing is
//     applied here: the runner applies it, within the recipe's bounds, once
//     the round is over.
//
// The interface is spelled identically in internal/factory/run, so the one
// value the runner builds is this door without either package importing the
// other.
type StageDoor interface {
	Report(ctx context.Context, result factory.StageResult) error
	Edit(ctx context.Context, edit factory.PlanEdit) error
}

// mayStage says whether `stage_result` and `plan_edit` belong on this belt:
// this conversation is a factory stage. It is the belt's predicate, asked of
// one field, and A CONVERSATION THAT IS NOT A STAGE NEVER HAS EITHER VERB: a
// model told it can report a stage would report one to nobody.
func (c Config) mayStage() bool { return c.Stage != nil }
