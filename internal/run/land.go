package run

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Landing is what a run's landing answers: the branch the working copy's work
// was committed on, the paths that commit carried, and — when the landing
// refused — the sentence saying why. A landing either names a branch or says
// what stopped it, so the two are never both empty and never both set.
type Landing struct {
	Branch  string
	Changed []string
	Refused string
}

// Land commits a run's working copy onto its branch and writes the answer as a
// note on the root task, so the run's own page carries where its work went.
//
// THE WORK IS THE COPY'S OWN ([session.LandRunTree]), because a run's workers
// edit through bash and leave no ledger: the tree's own status is the record.
// The commit message is the root task's title — the run's own name for the
// thing the person asked for.
//
// A REFUSAL IS AN ANSWER, NOT A FAULT. Nothing to land is the ordinary ending
// of a run that only read, and it is written on the root the same way a landing
// is. An error is the run having no working copy to land in at all, and then
// there is no note to write, because there is nothing about this run to say.
func Land(ctx context.Context, store *plandb.Store, workspace, rootID string) (Landing, error) {
	if err := ctx.Err(); err != nil {
		return Landing{}, err
	}
	root := store.Task(rootID)
	if root == nil {
		return Landing{}, fmt.Errorf("land a run: no task %s in the store", rootID)
	}
	// THE LANDING IS SIGNED WITH THE BARE `Assisted-by` LINE. A run's store
	// records no model on its root, and a line naming a guessed one would be a
	// provenance claim nobody made.
	branch, changed, refusal, err := session.LandRunTree(workspace, root.Title, "")
	if err != nil {
		return Landing{}, err
	}
	landing := Landing{Branch: branch, Changed: changed, Refused: refusal}
	// THE NOTE'S AUTHOR IS THE RUN'S OWN NAME, the store's naming trick at this
	// door ([Supervisor.pass] claims a task under its own id): the landing is
	// the run's hand, and the row it leaves reads as the run's.
	if _, err := store.AddNote(rootID, rootID, landingNote(landing)); err != nil {
		return Landing{}, fmt.Errorf("record the run's landing: %w", err)
	}
	return landing, nil
}

// landingNote is the one line a run's page carries about its landing: where the
// work went and how much of it, or the sentence that says why it did not go.
func landingNote(landing Landing) string {
	if landing.Refused != "" {
		return landing.Refused
	}
	files := "files"
	if len(landing.Changed) == 1 {
		files = "file"
	}
	return fmt.Sprintf("landed on %s: %d %s", landing.Branch, len(landing.Changed), files)
}

// LandingNote is [landingNote] as a door outside this package reads it: the one
// sentence a landing answers with, so a headless door can carry the branch the
// landing named out to its caller in the same words the run's own page holds.
func LandingNote(landing Landing) string { return landingNote(landing) }
