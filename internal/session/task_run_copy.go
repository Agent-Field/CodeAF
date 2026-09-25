package session

// A RUN'S WORKING COPY IS WRITTEN DOWN, OR IT CANNOT BE RESUMED.
//
// THE DIRECTORY IS DERIVED AND THE BRANCH IS NOT. That asymmetry is the whole
// reason this file exists, and a reader who believes both are reconstructible
// will delete it. A run's directory is a pure function of the conversation's
// folder and the run's own number ([taskOwnFolder]), so any process can work it
// out again. Its branch is minted with a random suffix at the moment the copy
// is cut ([cutTaskWorktree]), and nothing before this file wrote it anywhere.
//
// SO THE ROAD THAT MAKES A COPY MUST NEVER BE ASKED FOR ONE TWICE. It carves
// the directory it is handed, and it is documented as safe to do so precisely
// because the only thing that can be sitting at a run's path is that run's own
// wreckage. Call it again to "resume" a run and it clears out the directory
// holding every step of the work, cuts a fresh branch off a new random name,
// and hands back something that looks perfectly healthy. Nobody would see it
// until it was their work.
//
// WHAT THIS FILE GIVES INSTEAD is a record written when the run starts and a
// reader that ADOPTS what it names. The reader refuses rather than repairs.

import (
	"fmt"
	"os"
	"strings"
)

// TaskCopyRecord is where a run's work happened, in the fields a later process
// needs to find it again rather than make it again.
//
// IT IS ADDITIVE AND ITS ABSENCE IS ORDINARY. Every run row written before this
// existed carries none of it, and that is not a bug to repair — it is a run
// whose branch genuinely was never recorded and genuinely cannot be resumed.
// [runCopyTree] says so out loud rather than guessing ([errNoRunCopy]).
type TaskCopyRecord struct {
	// Dir is the directory the workers typed in and Branch is the branch their
	// work is on. Dir alone is not enough: a directory whose branch is unknown
	// can be read but not landed.
	Dir    string `json:"dir,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Root is the repository the branch merges back into.
	Root string `json:"root,omitempty"`
	// Ground is the repository or folder the work is ABOUT and Mode is how the
	// copy stands on it. For a worktree they repeat what Root and Branch say;
	// for every other mode they are the only record of it, which is the same
	// reason a node's own record carries them (task_store.go).
	Ground string   `json:"ground,omitempty"`
	Mode   TaskMode `json:"mode,omitempty"`
	// Home is the root checkout's branch when this copy was cut and HomeSha is
	// the commit that name held. A landing compares them with what is there now,
	// so that work never follows a person who moved their checkout while the run
	// was going.
	Home    string `json:"home,omitempty"`
	HomeSha string `json:"homeSha,omitempty"`
	// Rung is which rung of the ground ladder made this world and Seal is the
	// one string that names it, carried for the reason a node carries them: a
	// landing outlives the run that made the world.
	Rung GroundRung `json:"rung,omitempty"`
	Seal string     `json:"seal,omitempty"`
}

// runCopyOf writes a live run's copy down. It is taken from the tree the run is
// actually working in rather than from anything derived, because a value worked
// out a second time is a second authority over one fact.
func runCopyOf(tree taskTree) *TaskCopyRecord {
	if strings.TrimSpace(tree.dir) == "" {
		return nil
	}
	return &TaskCopyRecord{
		Dir:     tree.dir,
		Branch:  tree.branch,
		Root:    tree.root,
		Ground:  tree.ground,
		Mode:    tree.mode,
		Home:    tree.home,
		HomeSha: tree.homeSha,
		Rung:    tree.rung,
		Seal:    tree.seal,
	}
}

// errNoRunCopy is the refusal for a run whose copy was never written down, and
// it is the CORRECT ANSWER FOREVER rather than a stopgap. Such a run's branch
// was minted at random and recorded nowhere; there is no reading of the record,
// the conversation or the disk that can recover it. Anything this could do
// instead — derive the directory, carve it, cut a fresh branch — is the
// destructive road wearing a helpful face.
var errNoRunCopy = fmt.Errorf("this run's working copy was not written down when it started, so there is nothing to carry on from")

// programNotCarriedOn is the sentence a program's run is refused carrying on
// in, by the door and on its row alike.
func programNotCarriedOn(program string) string {
	return program + "'s run is never carried on: its work is left where it ended, and a new hand-off starts a new run"
}

// runCannotContinue answers WHY a run cannot be carried on, in the words a
// person reads, or the empty string when it can.
//
// IT IS THE SAME SENTENCE THE DOOR REFUSES WITH ([errNoRunCopy]) AND NOT A
// PARALLEL ONE. This is the third place those words could appear — the reader,
// the door, and now a row — and it is the one most likely to drift, because a
// row invites phrasing written for a row rather than for a person. One
// sentence, taken from one constant, cannot drift.
//
// IT READS NO DISK, because it is asked while a row is being drawn. What it can
// answer without one is the permanent half: a run whose copy was never written
// down can never be carried on, whatever is on the disk today. The other half —
// a copy that WAS written down and is no longer there — is a fact about this
// moment, so the door finds it out at the moment it matters ([runCopyTree]) and
// says so then.
//
// A PROGRAM'S RUN IS NEVER CARRIED ON, whatever its record says: the program
// did its one run alone and left its work where it ended, and the next hand-off
// is a new run ([programNotCarriedOn]).
func runCannotContinue(record *TaskCopyRecord, program string) string {
	if program = strings.TrimSpace(program); program != "" {
		return programNotCarriedOn(program)
	}
	if record == nil || strings.TrimSpace(record.Dir) == "" {
		return errNoRunCopy.Error()
	}
	return ""
}

// runCopyTree rebuilds the tree a run was working in, from the record alone.
//
// IT ADOPTS AND NEVER MAKES. Nothing here carves a directory, cuts a branch or
// derives a path, which is the one rule that keeps a resume from being a
// deletion. Every road out is either the copy that was written down or a
// refusal naming what is missing.
//
// It is [TaskNode.workingCopy] stated for a run, and it refuses in the same two
// places for the same two reasons: a copy nobody recorded, and a copy that is
// no longer on disk. A tree invented in either case would merge an empty branch
// and call it done.
func runCopyTree(record *TaskCopyRecord, place Place) (taskTree, error) {
	if record == nil || strings.TrimSpace(record.Dir) == "" {
		return taskTree{}, errNoRunCopy
	}
	dir := strings.TrimSpace(record.Dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		// THE BRANCH IS NAMED EVEN THOUGH THE FOLDER IS GONE, because it is the
		// one thing left that a person can act on: the work is on it.
		if branch := strings.TrimSpace(record.Branch); branch != "" {
			return taskTree{}, fmt.Errorf("its working copy is gone from %s, so there is nothing left to carry on in — its branch %s is still there", dir, branch)
		}
		return taskTree{}, fmt.Errorf("its working copy is gone from %s, so there is nothing left to carry on in", dir)
	}
	return taskTree{
		dir:     dir,
		root:    record.Root,
		branch:  record.Branch,
		home:    record.Home,
		homeSha: record.HomeSha,
		ground:  record.Ground,
		mode:    record.Mode,
		rung:    record.Rung,
		seal:    record.Seal,
		place:   place,
		// A SHELL WORKER'S COPY, which is what a run's always is: its workers
		// edit through bash and fill no write ledger (task_run_belt.go states it
		// where the copy is first made).
		bashBelt: true,
	}, nil
}
