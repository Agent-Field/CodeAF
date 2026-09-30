package session

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// TaskCopyCutter puts a task's working copy back on a machine it did not run on,
// by the same road a task is given its copy: the ground ladder ([carveGround]).
// It exists so that a carried copy is a fork where a started task would have got
// a fork and a linked worktree where it would have got one, with no second place
// that knows how a copy is made. The zero value is the one to use.
type TaskCopyCutter struct{}

// ladders is the ladder a copy is cut down, by whether it was a linked worktree.
// A worktree's branch lives in the project's own repository, so a worktree must
// come back as one whatever this machine could fork; anything else takes the
// whole ladder, exactly as a task being started does.
var ladders = map[bool][]groundRung{true: {snapshotRung{}}, false: groundLadder}

// Cut makes dest a copy of project on a new branch whose tip is the commit At.
// The commit is handed to the ladder as the frozen world, which is what a rung
// already opens a copy at instead of sealing the project as it stands now: what
// the copy held is At, and the project's present files are not it.
func (TaskCopyCutter) Cut(project, dest string, spec taskcopy.Spec) error {
	root, ok := repositoryRoot(project)
	if !ok {
		return fmt.Errorf("%s is not a repository to cut a task copy from", project)
	}
	order := worktreeOrder(Place{}, root, canonicalPath(dest), 0o700, spec.Branch, "restored task copy", spec.At)
	_, err := carveGroundDown(context.Background(), ladders[spec.Linked], order)
	return err
}
