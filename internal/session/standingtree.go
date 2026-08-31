package session

// WHERE YOU STAND, YOU WRITE; WHERE YOU REFER, THE WORK IS KEPT AND LANDED.
//
// places.go answers WHICH FOLDERS this conversation is about. This file answers
// what happens when the conversation's own hands — read, write, edit — are
// aimed at one of them.
//
//   - THE STANDING PLACE ([Config.Workspace]) is edited directly, exactly as it
//     always was. Somebody who opened aforge inside their project loses nothing
//     and notices nothing.
//   - A REFERRED FOLDER gets a working copy of its own, cut LAZILY ON THE FIRST
//     WRITE and never on referring: a repository gets `git worktree add` off its
//     HEAD, a plain folder gets a copy. Every later write to that folder lands
//     in the same copy, and it comes home through the landing the tasks already
//     have — the branch merged ([taskTree.comeHome]), or the files laid back by
//     name ([taskTree.landMirror]).
//
// So the safety story is one sentence: THE ONLY LIVE CHECKOUT THIS CONVERSATION
// CAN CHANGE WITHOUT A LANDING IS THE ONE IT IS STANDING IN. Referring wrongly
// costs nothing, which is what lets the accrual in places.go stay silent: no
// write reaches the person's real folder until they have seen what changed.
//
// ── READS FOLLOW WRITES, WHICH IS WHY THIS IS NOT WRITE-ONLY ──
//
// A model that writes /repo/x and then reads /repo/x MUST see its own work, or
// it is reasoning about a file that no longer says what it thinks. So once a
// folder has a working copy, a READ of a path that EXISTS IN THAT COPY is
// answered from the copy — the copy is that folder's truth for this
// conversation until it lands — and every other read is the real path,
// untouched. That shape was chosen over the two alternatives on purpose:
//
//   - Redirecting no reads would hand the model back the pre-edit bytes of its
//     own file, which is worse than not staging at all.
//   - Redirecting EVERY read under the folder would answer "no such file" for
//     everything a `git worktree` does not carry — an untracked file, a `.env`,
//     the person's uncommitted work — for a conversation that only meant to
//     look. Existence is the honest line between the two.
//
// And a write or an edit to a path that exists in the folder but not yet in the
// copy SEEDS it first ([Agent.seedStandingPath]), so an edit never fails on the
// gap a worktree's HEAD leaves.
//
// ── WHAT IS NOT REDIRECTED, SAID OUT LOUD ──
//
// `bash` is not. A shell command names its paths inside a string this program
// does not parse, and rewriting them would be guessing at somebody's quoting;
// the command runs in the standing workspace and touches whatever it names.
// `grep`, `find` and `ls` are not either: they are questions about what is
// there, and the real folder is the honest answer to those. The manual says
// both of these in the person's own words rather than leaving them to be
// discovered.
//
// ── AND FURROW ──
//
// The design's preference is a furrow universe wherever furrow watches the
// folder: byte-exact, carrying the dirty tree and the environment a worktree
// leaves behind. [internal/furrow] does not offer it yet — its seam materializes
// a universe only as the side effect of RUNNING A COMMAND in one
// ([furrow.Workspace.RunInFork]), and there is no call that simply hands back a
// universe to work in, though the binary's own `furrow fork` can. So the two
// roads below are worktree and copy, and the universe is a named seam: when
// `internal/furrow` grows a Fork door, it belongs in [Agent.cutStandingTree] as
// the first arm, with [furrow.Workspace.MergeFork] as its landing.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// StandingTree is one conversation's own working copy of one referred folder:
// where the folder is, where the copy is, and what has been written into it
// that the folder itself does not have yet.
//
// IT IS A RECORD AND NOT A CITATION, which is what makes it different from
// everything else on [Meta]. A referred place is recoverable by looking at the
// disk again; unlanded work is not recoverable from anywhere, so this is
// written down the moment it changes and read back at open — a person who
// closes the terminal with work in a copy finds it waiting.
type StandingTree struct {
	// Folder is the referred folder this copy is OF: absolute, canonical, and
	// the repository root when the folder is inside one, exactly as
	// [PlaceRef.Path] is.
	Folder string `json:"folder"`
	// Dir is the working copy itself, under the session's own trees/.
	Dir string `json:"dir"`
	// Mode is how this copy stands on the folder — [TaskModeWorktree] or
	// [TaskModeMirror] — and it is the task modes' own vocabulary rather than a
	// second one, because the landing it takes is the task landing.
	Mode TaskMode `json:"mode"`
	// Branch and Root are the worktree's half: the branch the work is on and
	// the repository it merges back into. Both are empty for a copy.
	Branch string `json:"branch,omitempty"`
	Root   string `json:"root,omitempty"`
	// Cut is when the copy was made, which is the moment everything in the
	// folder was still true.
	Cut time.Time `json:"cut,omitempty"`
	// Wrote is what THIS CONVERSATION'S OWN HANDS wrote, folder-relative and
	// slash-spelled, in the order it was written and without repeats.
	//
	// IT IS THE ONE READING OF WHAT LANDS, for [taskTree.comeHome]'s stated
	// reason: a landing that walked the directory instead would carry back
	// everything a build left in it. It is also what the chip counts, so the
	// number a person sees and the files that move are one list.
	Wrote []string `json:"wrote,omitempty"`
}

// standingFilesRemembered caps the paths one copy's record carries.
//
// It is a bound on the FILE, not a limit on the work: the landing of a worktree
// commits what git sees changed regardless, and this list is what the sentence
// names and what a copy lays back. A conversation that has written two thousand
// distinct files into one folder has stopped being a conversation and become a
// task, and a meta.json that grew without a ceiling would be re-read and
// re-written on every one of those writes.
const standingFilesRemembered = 512

// StandingChange is one folder with work in it that has not landed — what the
// composer's chip draws, and nothing more. A conversation with nothing waiting
// answers an empty slice, and the surface draws nothing at all.
type StandingChange struct {
	// Folder is the absolute folder, Name is its basename — what the chip says,
	// because a person recognises `agentfield` faster than a path they would
	// have to read to the end.
	Folder string
	Name   string
	// Files is how many distinct files are waiting.
	Files int
}

// FolderLanding is what a landing did, or — before it is confirmed — what it would
// do. One shape for both, because the card that asks and the line that reports
// are looking at the same facts.
type FolderLanding struct {
	Folder string
	Name   string
	// Files is what has been written, folder-relative, in write order.
	Files []string
	// Merged is the outcome once it has happened: [mergeMerged] for a branch
	// that went home, [mergeConflicted] for one that would not and was kept,
	// [mergeInPlace] for a copy laid back by name. It is empty in a preview.
	Merged string
	// Note is the sentence that only exists when something needs explaining —
	// a conflict, files nobody wrote, a copy that could not be laid back. The
	// emptiness law: an ordinary landing says nothing here.
	Note string
}

// Kept reports that the work did NOT go into the folder and is still on its own
// branch, which is what git does with a merge it cannot settle.
//
// It is a method rather than a comparison a surface makes for itself, because
// the strings [taskTree.comeHome] answers with are this package's and a surface
// reading one of them by hand is the second copy of a fact that will drift.
func (l FolderLanding) Kept() bool { return l.Merged == mergeConflicted }

// Places whose folder the conversation may not stage into, answered once.
//
// A FOLDER THE PERSON SAID "IN PLACE" ABOUT IS EDITED DIRECTLY. That is
// [PlaceRef.Mode]'s whole job and it is never guessed — somebody said "here",
// "directly" or "in place" about that folder, and a said mode is never
// overruled (places.go).
func (ref PlaceRef) staged() bool { return TaskMode(ref.Mode) != TaskModeInPlace }

// standingFolder is the referred folder one path is aimed at, or nothing.
//
// THE STANDING WORKSPACE ALWAYS WINS. A referred folder that contains the
// workspace — somebody standing in a subdirectory of a project they also
// referred to — must not turn the conversation's own directory into a staged
// one, because that is the one place the design promises stays direct.
//
// THE DEEPEST REFERRED FOLDER WINS AFTER THAT, so a person who refers to a
// project and then to one library inside it gets the library's own copy for
// paths under it rather than the project's.
func (a *Agent) standingFolder(path string) (PlaceRef, bool) {
	workspace := canonicalPath(strings.TrimSpace(a.config.Workspace))
	if workspace != "" && under(path, workspace) {
		return PlaceRef{}, false
	}
	var best PlaceRef
	for _, place := range a.referredPlaces() {
		if !under(path, place.Path) || !place.staged() {
			continue
		}
		if len(place.Path) > len(best.Path) {
			best = place
		}
	}
	return best, best.Path != ""
}

// under reports whether a path is the directory itself or something inside it.
// Both sides are canonical by the time they reach here, so this is arithmetic
// on strings and never touches the disk.
func under(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// StandingTrees is a copy of what this conversation holds, for a surface and
// for a test. Nothing changes when it is read.
func (a *Agent) StandingTrees() []StandingTree {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.trees) == 0 {
		return nil
	}
	out := make([]StandingTree, len(a.trees))
	copy(out, a.trees)
	return out
}

// UnlandedChanges is what the composer draws: one row per folder with work
// waiting in it, newest cut first. THE EMPTINESS LAW LIVES HERE — a folder with
// a copy but nothing written into it is not news and answers nothing, so the
// chip appears exactly when there is something to land.
func (a *Agent) UnlandedChanges() []StandingChange {
	var out []StandingChange
	for _, tree := range a.StandingTrees() {
		if len(tree.Wrote) == 0 {
			continue
		}
		out = append(out, StandingChange{
			Folder: tree.Folder,
			Name:   filepath.Base(tree.Folder),
			Files:  len(tree.Wrote),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// standingTreeFor is the copy this conversation holds of one folder, and
// whether it holds one at all.
func (a *Agent) standingTreeFor(folder string) (StandingTree, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, tree := range a.trees {
		if tree.Folder == folder {
			return tree, true
		}
	}
	return StandingTree{}, false
}

// standingAim is the whole redirect, asked once per tool call: where does this
// path really go, and which copy is it in.
//
// writing decides whether a copy may be CUT. A write or an edit cuts one; a
// read never does — reading a folder is looking at the disk, which the model
// could always do, and a copy made by a look would be a folder copied by
// curiosity. A read is only redirected into a copy that already exists, and
// only for a file that is actually in it (see this file's header).
func (a *Agent) standingAim(raw string, writing bool) (aimed string, tree StandingTree, ok bool) {
	if strings.TrimSpace(raw) == "" {
		return "", StandingTree{}, false
	}
	path := canonicalPath(resolveAgainst(raw, a.config.Workspace))
	place, found := a.standingFolder(path)
	if !found {
		return "", StandingTree{}, false
	}
	tree, held := a.standingTreeFor(place.Path)
	if !held {
		if !writing {
			return "", StandingTree{}, false
		}
		var err error
		if tree, err = a.cutStandingTree(place); err != nil {
			// A COPY THAT COULD NOT BE MADE IS NOT A WRITE THAT IS REFUSED. The
			// tool goes on to the real path, which is what it would have done
			// yesterday, and the failure is not turned into a wall between the
			// person and their own folder. Nothing here is load-bearing for
			// safety that the person did not already have.
			return "", StandingTree{}, false
		}
	}
	relative, err := filepath.Rel(place.Path, path)
	if err != nil {
		return "", StandingTree{}, false
	}
	aimed = filepath.Join(tree.Dir, relative)
	if !writing {
		// The existence rule. A file the copy holds is this conversation's own
		// version of it; anything else is the folder's, read where it lives.
		if _, err := os.Lstat(aimed); err != nil {
			return "", StandingTree{}, false
		}
		return aimed, tree, true
	}
	a.seedStandingPath(path, aimed)
	return aimed, tree, true
}

// resolveAgainst is the tools' own reading of a path — absolute as it stands, a
// relative name against the conversation's directory — spelled here rather than
// borrowed from bare so that this file does not depend on the belt to answer a
// question about a string.
func resolveAgainst(raw, workspace string) string {
	path := strings.TrimSpace(raw)
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(filepath.Separator)))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(strings.TrimSpace(workspace), path)
	}
	return filepath.Clean(path)
}

// seedStandingPath puts the folder's own version of a file into the copy before
// the copy is written over.
//
// IT EXISTS FOR THE GAP A WORKTREE LEAVES. A branch carries HEAD and nothing
// else — the person's uncommitted work and every untracked file stay in their
// checkout — so an `edit` aimed at one of those would land on a path that is
// not there and fail on a file the person can see. Seeding makes the copy what
// the folder is for that one file, which is what "this copy is the folder's
// truth for this conversation" has to mean to be usable.
//
// A file already in the copy is left exactly alone: it is this conversation's
// own newer version, and overwriting it with the folder's would be undoing the
// last edit on the way into the next one.
func (a *Agent) seedStandingPath(from, to string) {
	if _, err := os.Lstat(to); err == nil {
		return
	}
	info, err := os.Lstat(from)
	if err != nil || info.IsDir() {
		return
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return
	}
	_ = copyPath(from, to)
}

// cutStandingTree makes the working copy, once, on the first write.
//
// The two roads are the task modes' own and are chosen the same way
// [prepareTaskTreeOn] chooses them: a repository with something to branch from
// gets a worktree off its HEAD, and everything else gets a copy of the folder.
// There is no third road today; the universe furrow would give is the seam this
// file's header names.
func (a *Agent) cutStandingTree(place PlaceRef) (StandingTree, error) {
	// ONE CUT PER FOLDER, WHATEVER ARRIVES AT ONCE. Two tool calls in one turn
	// can both find no copy and both start cutting, and the loser would leave a
	// registered worktree nothing holds a record of. The agent's own lock cannot
	// be held here — this runs git and copies files — so the cut has a lock of
	// its own, and the record is read again under it.
	a.treeCut.Lock()
	defer a.treeCut.Unlock()
	if tree, held := a.standingTreeFor(place.Path); held {
		return tree, nil
	}
	trees := a.config.Place.Trees()
	if strings.TrimSpace(trees) == "" {
		// A conversation with no folder of its own — a headless run, a test —
		// has nowhere to put a working copy, so it has none and writes where it
		// always did. Absent, not broken.
		return StandingTree{}, errors.New("this conversation has no folder to keep a working copy in")
	}
	name := slugify(filepath.Base(place.Path))
	dir := canonicalPath(filepath.Join(trees, "folder-"+name+"-"+shortID()))
	tree := StandingTree{Folder: place.Path, Dir: dir, Cut: time.Now()}

	if root, ok := repositoryRoot(place.Path); ok && hasCommit(root) {
		branch := "chat/" + name + "-" + shortID()
		cut, err := cutWorktreeAt(a.config.Place, root, dir, branch, 0o700)
		if err != nil {
			return StandingTree{}, err
		}
		tree.Dir, tree.Mode, tree.Branch, tree.Root = cut.dir, TaskModeWorktree, branch, root
	} else {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return StandingTree{}, err
		}
		if problem := copyFolderInto(place.Path, dir); problem != "" {
			_ = os.RemoveAll(dir)
			return StandingTree{}, errors.New(problem)
		}
		tree.Mode = TaskModeMirror
	}
	a.mu.Lock()
	a.trees = append(append([]StandingTree{}, a.trees...), tree)
	a.mu.Unlock()
	a.stampTrees()
	return tree, nil
}

// copyFolderInto copies a plain folder into the conversation's own copy of it.
//
// ON APFS IT IS A CLONE AND COSTS NOTHING. `cp -c` is clonefile(2):
// copy-on-write, instant, and not one extra block until a file actually
// changes — which is the whole reason a plain folder is affordable to stage at
// all, and Darwin is the platform this program lives on. Anywhere the clone is
// refused — another filesystem, another operating system — the answer is
// [mirrorGround], the same copier a mirrored task already gets, with the same
// refusal for a folder too big to be worth copying.
func copyFolderInto(folder, dir string) string {
	if runtime.GOOS == "darwin" {
		if err := exec.Command("cp", "-Rc", folder+string(filepath.Separator)+".", dir).Run(); err == nil {
			return ""
		}
		// A half-finished clone would make the copy look like the folder while
		// holding some of it, so what it left is cleared before the fallback
		// walks the same ground.
		if entries, err := os.ReadDir(dir); err == nil {
			for _, entry := range entries {
				_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
			}
		}
	}
	return mirrorGround(folder, dir)
}

// noteStandingWrite records one path the conversation wrote into a copy, and it
// is the only writer of [StandingTree.Wrote].
//
// The path is stored FOLDER-RELATIVE, which is the spelling the landing reads
// ([layWork] normalizes against the copy, [commitTaskWork] stages against it)
// and the spelling a sentence can say out loud. A repeat is not appended twice:
// the count on the chip is files, not writes.
func (a *Agent) noteStandingWrite(tree StandingTree, aimed string) {
	relative, err := normalizeScopePath(tree.Dir, aimed)
	if err != nil {
		return
	}
	changed := false
	a.mu.Lock()
	kept := make([]StandingTree, len(a.trees))
	copy(kept, a.trees)
	for index := range kept {
		if kept[index].Folder != tree.Folder {
			continue
		}
		held := false
		for _, path := range kept[index].Wrote {
			if path == relative {
				held = true
				break
			}
		}
		if !held && len(kept[index].Wrote) < standingFilesRemembered {
			wrote := make([]string, len(kept[index].Wrote), len(kept[index].Wrote)+1)
			copy(wrote, kept[index].Wrote)
			kept[index].Wrote = append(wrote, relative)
			changed = true
		}
		break
	}
	if changed {
		a.trees = kept
	}
	a.mu.Unlock()
	if changed {
		a.stampTrees()
	}
}

// LandingFor is what a landing WOULD do — the folder, its name and every file
// waiting — without doing any of it. It is what the card shows before the
// person says yes.
func (a *Agent) LandingFor(folder string) (FolderLanding, bool) {
	tree, ok := a.standingTreeFor(a.standingName(folder))
	if !ok || len(tree.Wrote) == 0 {
		return FolderLanding{}, false
	}
	files := make([]string, len(tree.Wrote))
	copy(files, tree.Wrote)
	return FolderLanding{Folder: tree.Folder, Name: filepath.Base(tree.Folder), Files: files}, true
}

// standingName reads whatever a surface was given — a full path, or the
// basename a chip drew — as one of the folders this conversation holds a copy
// of. A name nothing matches comes back as it arrived, so the caller's own
// refusal is the one the person reads.
func (a *Agent) standingName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		// The ordinary case: exactly one folder is waiting, and the person who
		// typed /land with nothing after it meant that one.
		if waiting := a.UnlandedChanges(); len(waiting) == 1 {
			return waiting[0].Folder
		}
		return ""
	}
	folder := canonicalPath(resolveAgainst(name, a.config.Workspace))
	for _, tree := range a.StandingTrees() {
		if tree.Folder == folder || filepath.Base(tree.Folder) == name {
			return tree.Folder
		}
	}
	return folder
}

// Land brings one folder's work home, through the roads the tasks already use:
// a branch is committed and merged ([taskTree.comeHome]), a copy is laid back
// over the folder by name ([taskTree.landMirror]). There is no third landing
// and no copier of its own here — a second one would be a second answer to
// "what does it mean for work to arrive".
//
// IT LANDS THE FOLDER WHOLE. Picking files out of a landing is a refinement
// this does not have, and the manual says so rather than letting somebody find
// out by looking for the key.
//
// AND THE COPY GOES WITH IT, whatever the outcome: a merged branch has nothing
// left to hold, and a conflicted one keeps its branch — which is where the work
// actually is — while the record here is dropped, because a copy this
// conversation would go on writing into after its branch was kept would be work
// piling up behind a landing that already failed.
func (a *Agent) Land(folder string) (FolderLanding, error) {
	name := a.standingName(folder)
	if name == "" {
		return FolderLanding{}, errors.New("nothing is waiting to go into a folder")
	}
	tree, ok := a.standingTreeFor(name)
	if !ok {
		return FolderLanding{}, fmt.Errorf("nothing is waiting for %s", filepath.Base(name))
	}
	if len(tree.Wrote) == 0 {
		return FolderLanding{}, fmt.Errorf("nothing has been changed in %s", filepath.Base(tree.Folder))
	}
	landing := FolderLanding{Folder: tree.Folder, Name: filepath.Base(tree.Folder), Files: append([]string{}, tree.Wrote...)}
	// THE TASK TREE IS BUILT HERE AND HELD NOWHERE, because it is the argument
	// the landing takes rather than a second record of the copy: this file's
	// StandingTree is the record, and taskTree is the shape [taskTree.comeHome]
	// reads. Keeping one of each would be the two drifting.
	work := taskTree{
		dir:    tree.Dir,
		root:   tree.Root,
		branch: tree.Branch,
		place:  a.config.Place,
		ground: tree.Folder,
		mode:   tree.Mode,
	}
	if tree.Mode != TaskModeWorktree {
		// A copy has no branch to merge, and [taskTree.comeHome] reads that from
		// merge rather than from the mode for every road but the mirror's.
		work.merge = mergeInPlace
	}
	merged, note := work.comeHome("changes from this conversation", tree.Wrote)
	landing.Merged, landing.Note = merged, note
	if tree.Mode == TaskModeMirror {
		// The copy is not removed by the landing that laid it back, so it is
		// removed here — the record has gone and a directory nothing points at
		// is litter under the person's own session folder.
		_ = os.RemoveAll(tree.Dir)
		_ = os.Remove(filepath.Dir(tree.Dir))
	}
	a.dropStandingTree(tree.Folder)
	return landing, nil
}

// dropStandingTree forgets one copy and writes the record without it.
func (a *Agent) dropStandingTree(folder string) {
	a.mu.Lock()
	kept := make([]StandingTree, 0, len(a.trees))
	for _, tree := range a.trees {
		if tree.Folder != folder {
			kept = append(kept, tree)
		}
	}
	changed := len(kept) != len(a.trees)
	if changed {
		a.trees = kept
	}
	a.mu.Unlock()
	if changed {
		a.stampTrees()
	}
}

// loadStandingTrees reads a session folder's unlanded work at open, so that a
// conversation closed with changes in a copy comes back still holding them.
//
// A RECORD WHOSE COPY IS GONE IS DROPPED. Everything else on meta.json is a
// citation and survives the thing it names disappearing; this one names a
// directory that IS the work, and a record pointing at nothing would put a chip
// on the composer for changes that cannot be landed.
func loadStandingTrees(dir string) []StandingTree {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return nil
	}
	var out []StandingTree
	for _, tree := range meta.Trees {
		if strings.TrimSpace(tree.Folder) == "" || strings.TrimSpace(tree.Dir) == "" {
			continue
		}
		if info, err := os.Stat(tree.Dir); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, tree)
	}
	return out
}
