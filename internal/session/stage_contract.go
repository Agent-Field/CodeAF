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
// stopped, or has stopped without calling it and every way the runner has of
// getting a report (its reminders, a turn that must report, a report it makes
// itself) has come back empty.
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
//   - Report hands the round's result back. The runner keeps the last one, so
//     a stage that reported early and went on working reports again; a report
//     it refuses is refused with a sentence the model reads, in the same turn.
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

// StagePRBaseSyncer is available only in Follow PR rounds where the runner
// can incorporate the linked PR's base into that job's checkout.
type StagePRBaseSyncer interface {
	SyncPRBase(context.Context) (string, error)
	CanSyncPRBase() bool
}

// mayStage says whether this conversation can act as a factory stage.
// Ordinary chats and read-only tasks cannot report a stage result.
func (c Config) mayStage() bool { return !c.readingTask() && c.Stage != nil }
