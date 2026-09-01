package session

import (
	"os"
	"path/filepath"
)

// task_lay.go is how a ledger arrives over a folder: ALL OF IT, OR NONE OF IT,
// and every file whole.
//
// The lay used to walk the ledger and copy each path where it went, returning at
// the first one it could not place — so a folder family whose second file had
// nowhere to go left the person holding the first half of a deliverable, under a
// row that said done (#256). Two rules fix it and they are the ordinary ones for
// writing over somebody's own files:
//
//   - NOTHING IS IN PLACE UNTIL EVERYTHING CAN BE. The whole ledger is copied
//     beside its targets first; only when every one of them is staged does
//     anything move. A refusal takes its own leavings away and the folder is as
//     it was found.
//   - EACH FILE ARRIVES WHOLE. What is staged is renamed into place rather than
//     written over the top, so a lay stopped by a kill or a full disk leaves the
//     person's own file or the finished new one — never a truncated half of
//     either.
//
// It serves the audit's restores too ([layWork] is the one door), where the tree
// being written into is a throwaway copy and the guarantee costs a rename.

// laidWork is one ledger staged beside its targets and not yet in place.
type laidWork struct {
	// staged pairs the temporary each path was copied to with where it goes.
	staged [][2]string
	// token is what this lay's temporaries are named after, minted once so two
	// landings into one folder cannot rename each other's staged files into
	// place, and so a name a person happens to have used is never the one taken.
	token string
	// made are the directories this lay created, deepest first, so abandoning it
	// can take them away in the order it found them missing.
	made []string
	// gone are the targets to take away: paths the node wrote and then deleted,
	// which the folder must not keep either.
	gone []string
}

// layingSuffix opens the name a staged path waits under beside its target, with
// the lay's own token after it. It is the harness's own word so that a lay
// interrupted by a kill leaves something a person can recognise as machinery
// rather than as their work.
const layingSuffix = ".aforge-laying-"

// heldSuffix opens the name the person's own file waits under while the ledger
// goes in, and it is a different word from [layingSuffix] on purpose: one of
// them is the work arriving and the other is what was already there.
const heldSuffix = ".aforge-held-"

// stageLay copies the whole ledger beside where it is going and reports the
// first path that could not get there.
//
// The paths are read with [normalizeScopePath] — the same one reading of a path
// against a tree that the write scope's door and guard use (fork.go) — so a
// record that names a file absolutely and one that names it relatively land in
// the same place.
func stageLay(from, to string, wrote []string) (laidWork, string) {
	lay := laidWork{token: shortID()}
	for _, raw := range wrote {
		relative, err := normalizeScopePath(from, raw)
		if err != nil {
			// A path outside the working copy is not part of what ships, exactly as
			// it is not part of what is staged ([stageableWork] drops the same ones).
			continue
		}
		source := filepath.Join(from, filepath.FromSlash(relative))
		target := filepath.Join(to, filepath.FromSlash(relative))
		if _, err := os.Lstat(source); err != nil {
			if !os.IsNotExist(err) {
				// A source that is there and cannot be read is not a deletion, and
				// treating it as one would take the person's own file away over a
				// permission somebody changed.
				return lay, "the work could not be laid into a clean copy: " + err.Error()
			}
			// Written and then removed: what it goes over must not keep it either.
			lay.gone = append(lay.gone, target)
			continue
		}
		made, err := makeParents(filepath.Dir(target))
		lay.made = append(lay.made, made...)
		if err != nil {
			return lay, "the work could not be laid into a clean copy: " + err.Error()
		}
		staged := target + layingSuffix + lay.token
		if err := copyPath(source, staged); err != nil {
			lay.staged = append(lay.staged, [2]string{staged, target})
			return lay, "the work could not be laid into a clean copy: " + err.Error()
		}
		lay.staged = append(lay.staged, [2]string{staged, target})
	}
	return lay, ""
}

// commit puts the whole ledger in place, or puts the folder back as it was.
//
// WHAT WAS THERE IS HELD, NOT DESTROYED. Each target the ledger writes over is
// kept beside itself for the length of the commit — a hard link where the
// filesystem gives one, so nothing of the person's is copied — and only when
// every path has arrived are those held copies let go. A path that will not go
// undoes every one before it, and the folder is as it was found.
//
// A REGULAR FILE IS STILL REPLACED IN ONE STEP. The hold is a second name for
// what is already there, so the rename that puts the new file in is the same
// atomic replace it was: a kill mid-commit leaves the person's own file or the
// finished new one, never half of either, and the folder can still be rolled
// back afterwards by whoever finds the leavings.
func (l laidWork) commit() string {
	var done []layStep
	for _, pair := range l.staged {
		step, err := l.place(pair[0], pair[1])
		done = append(done, step)
		if err != nil {
			undoLay(done)
			return layProblem(err)
		}
	}
	for _, target := range l.gone {
		// A path the node wrote and then deleted is part of the ledger too, so one
		// that will not go is the same news as one that will not arrive.
		step, err := l.take(target)
		done = append(done, step)
		if err != nil {
			undoLay(done)
			return layProblem(err)
		}
	}
	for _, step := range done {
		step.settle()
	}
	return ""
}

// layStep is one target this commit has touched and everything needed to put it
// back: what was there (aside), and what this lay put over it (staged, empty for
// a path the ledger takes away).
type layStep struct {
	target string
	aside  string
	staged string
	// placed says the staged path actually reached the target. Without it an undo
	// could not tell what this lay put there from what was there all along, and it
	// would take the person's own file away under the name of a copy.
	placed bool
}

// settle lets go of what was held once the whole ledger is in.
func (s layStep) settle() {
	if s.aside != "" {
		_ = os.RemoveAll(s.aside)
	}
}

// place puts one staged path where it goes, holding whatever was there first.
func (l laidWork) place(staged, target string) (layStep, error) {
	step := layStep{target: target, staged: staged}
	aside, err := l.hold(target)
	step.aside = aside
	if err != nil {
		return step, err
	}
	// The hold is a second NAME for a regular file and a MOVE for anything else,
	// so a directory standing where a file goes is out of the way by now and the
	// rename has nothing left to refuse.
	if err := os.Rename(staged, target); err != nil {
		return step, err
	}
	step.placed = true
	return step, nil
}

// take moves aside a path the ledger says is gone, so that a later refusal can
// still put it back.
func (l laidWork) take(target string) (layStep, error) {
	step := layStep{target: target}
	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			return step, nil
		}
		return step, err
	}
	aside := target + heldSuffix + l.token
	if err := os.Rename(target, aside); err != nil {
		return step, err
	}
	step.aside = aside
	return step, nil
}

// hold keeps what is standing at a target for the length of the commit, and
// answers where it is being kept.
//
// A HARD LINK COSTS NOTHING AND A MOVE COSTS THE REPLACE. Linking leaves the
// person's file exactly where it is, so the rename over it is still one atomic
// step; a directory, a symlink and a filesystem with no links to give fall back
// to moving it aside, which is the same guarantee one syscall further apart.
func (l laidWork) hold(target string) (string, error) {
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	aside := target + heldSuffix + l.token
	if info.Mode().IsRegular() {
		if err := os.Link(target, aside); err == nil {
			return aside, nil
		}
	}
	if err := os.Rename(target, aside); err != nil {
		return "", err
	}
	return aside, nil
}

// undoLay puts back everything a refused commit had already moved, newest first.
func undoLay(done []layStep) {
	for at := len(done) - 1; at >= 0; at-- {
		step := done[at]
		if step.placed {
			// What this lay put there goes back to waiting under its own name, and
			// only that: a target it never reached still holds the person's own file.
			if err := os.Rename(step.target, step.staged); err != nil {
				_ = os.RemoveAll(step.target)
			}
		}
		if step.aside == "" {
			continue
		}
		if _, err := os.Lstat(step.target); err == nil {
			// THE HOLD WAS A SECOND NAME AND THE FILE NEVER LEFT, which is what a
			// hard link buys ([laidWork.hold]) — and renaming one name of a file over
			// another name of the same file does nothing at all, so the link has to
			// be dropped rather than moved back.
			_ = os.RemoveAll(step.aside)
			continue
		}
		_ = os.Rename(step.aside, step.target)
	}
}

// layProblem is the one sentence this file answers with, so that a refusal reads
// the same wherever it came from.
func layProblem(err error) string {
	return "the work could not be laid into a clean copy: " + err.Error()
}

// abandon takes back everything a refused lay put down, so the folder it was
// aimed at is as it was found.
func (l laidWork) abandon() {
	for _, pair := range l.staged {
		_ = os.RemoveAll(pair[0])
	}
	// Deepest first, which is the order they were found missing in: a directory
	// that already held something of the person's is not empty and stays.
	for _, dir := range l.made {
		_ = os.Remove(dir)
	}
}

// makeParents makes the directories one target needs and answers the ones it
// actually had to make, deepest first, so that a lay nobody goes on with can
// take its own directories away again.
func makeParents(dir string) ([]string, error) {
	var missing []string
	for probe := dir; ; {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		missing = append(missing, probe)
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return missing, err
	}
	return missing, nil
}
