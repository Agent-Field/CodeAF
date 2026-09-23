package session

import "errors"

// LandRunTree commits a run's own working copy onto the branch it is checked
// out on, and it is the landing half of the run engine ([internal/run]) stated
// here, where the commit road lives.
//
// THE RUN'S WORK IS WHAT ITS TREE SAYS. Every worker of a run edits through
// bash ([NewBeltWorker]), and a shell worker fills no write ledger, so the
// working copy's own git status is the record of what the run made — the same
// road a bash-belt node's landing takes ([commitTaskWork] on its belt arm),
// reached by name rather than from inside a task tree.
//
// IT ANSWERS FOUR THINGS AND ONLY THE FIRST THREE ARE THE LANDING. The branch
// is the one the copy stands on once the commit has landed. The paths are what
// the commit carried, read off the index it built rather than off any list
// handed in. The refusal is the sentence a person reads when the landing had
// nothing to do or the tree would not take the work, and it is empty on every
// other road. The error is non-nil only when there was no working copy a
// landing could be made in at all — a directory that is not a repository, a
// copy standing on no branch — because that is a fault about the place rather
// than a landing outcome, and there is no branch or sentence to answer with.
//
// THE SWITCH IS THE BELT'S, read here for the reason [NewBeltWorker] reads it:
// the belt road stages the tree's own status minus what the harness itself
// writes, which is not what the default landing does, so a landing that ran
// with CODEAF_TASK_BELT naming the node belt would change a landing for one nobody
// composed. With the flag off this refuses and touches nothing.
//
// THE COMMIT IS SIGNED, ALWAYS, with the same two trailer lines every commit
// codeaf writes carries. model is the one the `Assisted-by` line names, and
// empty leaves that line bare — the honest answer for a door that does not know
// which model the run's workers were on, or for a person who turned the name
// off.
func LandRunTree(dir, title, model string) (branch string, changed []string, refusal string, err error) {
	if !bashBeltAsked() {
		return "", nil, "", errors.New("the bash belt is off: CODEAF_TASK_BELT names the node belt")
	}
	sign := gitSignature{named: model != "", model: model}
	saved, problem, why := commitTaskWork(dir, title, nil, sign, true)
	if problem != "" {
		if why == refusedByTheTree {
			// The place itself would not take the write, and asking again would
			// get the same answer: that is the fault arm, never a landing.
			return "", nil, "", errors.New(problem)
		}
		return "", nil, problem, nil
	}
	if len(saved) == 0 {
		return "", nil, runNothingToLand, nil
	}
	if branch = currentBranch(dir); branch == "" {
		return "", nil, "", errors.New("the run's working copy stands on no branch, so there is nothing to land")
	}
	return branch, saved, "", nil
}

// runNothingToLand is the refusal a run's landing answers when the working copy
// holds no change the run made: the branch would carry what it always carried.
const runNothingToLand = "nothing to land: the run's working copy holds no change"
