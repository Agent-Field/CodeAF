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

// commit puts every staged path in place, and it is the half that is left with
// almost nothing to go wrong: each rename is within a directory this lay has
// already written a whole file into, so the questions of room, permission and
// parentage are all answered by the time it runs.
//
// WHAT IS LEFT IT REPORTS RATHER THAN HIDES. A rename that fails here is the one
// case a person can be handed a folder with part of the ledger in it, and the
// answer is the same as for every other refusal: the outcome says the work could
// not be saved and the node asks for their look, with the copy it came from still
// holding everything ([taskTree.landMirror]). Undoing the renames that already
// happened would mean holding a second copy of the person's own files, which is
// the cost of covering a window one rename wide.
func (l laidWork) commit() string {
	for _, pair := range l.staged {
		staged, target := pair[0], pair[1]
		// A RENAME REPLACES A FILE AND REFUSES A DIRECTORY, so the one shape it
		// cannot do on its own is cleared first. Everything else — the ordinary
		// file over an ordinary file — is one atomic step. A clearing that fails
		// needs no answer of its own: the rename under it is then the one that
		// cannot happen, and it is already reported.
		if directoryAt(target) || directoryAt(staged) {
			_ = os.RemoveAll(target)
		}
		if err := os.Rename(staged, target); err != nil {
			return "the work could not be laid into a clean copy: " + err.Error()
		}
	}
	for _, target := range l.gone {
		// A path the node wrote and then deleted is part of the ledger too, so one
		// that will not go is the same news as one that will not arrive.
		if err := os.RemoveAll(target); err != nil {
			return "the work could not be laid into a clean copy: " + err.Error()
		}
	}
	return ""
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

// directoryAt says whether something is sitting at a path and is a directory. A
// symlink is not followed: the link is what would be replaced.
func directoryAt(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}
