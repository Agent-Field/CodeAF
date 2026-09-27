package session

// plandb_work.go is the READING OF A RUN'S WORK: what a run's working copy
// holds that its ground did not, for the task page's `work` tab.
//
// A RUN'S TASK HAS NO JOURNAL, AND IT HAS A WORKING COPY. The task room reads a
// node's changes off the node's own record; a run's parts are rows of a plan
// store and every one of them types in the run's one copy. So the question
// "what has this task changed" is asked of the copy the run's row wrote down
// when the run started ([TaskNotice.Copy], [runCopyOf]) and answered as git
// answers it: the difference between the commit the copy was cut from and the
// files as they stand now, committed or not.
//
// IT IS THE RUN'S WORK, NOT ONE PART'S. A part and the run's own task share the
// copy, so every page of one run reads the same difference; the page says so
// rather than pretending a part's share can be told apart.
//
// NOTHING HERE WRITES. The copy is read with plumbing commands only, so opening
// a page never moves an index or a branch under a worker that is typing in it.

import (
	"os"
	"path/filepath"
	"strings"
)

// PlanTaskWork is what a run's working copy holds that its ground did not: the
// patch, the files git has never seen, and where the copy is.
//
// ITS ZERO VALUE IS "NOTHING KNOWN", and a surface draws it as the absence it
// is. An empty Patch on a copy that was read is a run that has changed
// nothing yet, which [PlanTaskWork.Read] tells apart from a copy nobody could
// read.
type PlanTaskWork struct {
	// Dir is the run's copy, the directory its workers type in.
	Dir string
	// Patch is the unified difference between the commit the copy was cut
	// from and the files in it now, the harness's own files left out
	// ([harnessWrote]). It is capped at [planWorkPatchCap] bytes, and Cut says
	// the cap was reached.
	Patch string
	Cut   bool
	// Added is every file in the copy git has never been told about, relative
	// to the copy, the harness's own left out. A new file a worker wrote and
	// did not stage is work too, and a patch alone would not show it.
	Added []string
	// Read says the copy was found and git answered for it. False is a run
	// whose copy was never written down, or is no longer on disk, or is not a
	// repository: a page has nothing to show and says so.
	Read bool
	// NoDoor says the engine that was asked has no working-copy read at all.
	// It is set by a client whose engine answered "no such method", and it
	// does not travel on the wire: a current engine never sets it, and a page
	// draws the absence sentence rather than an empty difference.
	NoDoor bool `json:"-"`
}

// planWorkPatchCap is the most of a patch a page is handed. A run that rewrote
// a generated file can produce megabytes of difference, and the page is read,
// not archived: past the cap the page says the rest is in the copy.
const planWorkPatchCap = 256 << 10

// PlanWorkAgent is the optional door onto a run's working copy. It is its own
// interface rather than a method on [PlanAgent] because only an engine that
// holds the copy on its own disk can answer it: a surface asserts it, and a
// page on an engine without it draws the tab's honest absence.
type PlanWorkAgent interface {
	PlanTaskWork(id string) (PlanTaskWork, bool)
}

// PlanTaskWork answers the working copy of the run one store task belongs to.
// False is a task this conversation's plan does not hold.
func (a *Agent) PlanTaskWork(id string) (PlanTaskWork, bool) {
	root, ok := a.planTaskRoot(id)
	if !ok {
		return PlanTaskWork{}, false
	}
	copied := a.planRunCopy(root)
	if copied == nil {
		return PlanTaskWork{}, true
	}
	return readPlanWork(copied), true
}

// planTaskRoot is the store root of the run a task belongs to, spelled the
// way a run's row names its store task ([planStoreID]).
func (a *Agent) planTaskRoot(id string) (string, bool) {
	stores, plan, closeStores := a.openPlanReadHandles()
	defer closeStores()
	for _, store := range stores {
		if task := store.Task(planTaskID(id)); task != nil && task.Chat == plan.chat {
			return planStoreID(store.RootID()), true
		}
	}
	return "", false
}

// planRunCopy is the copy a run wrote down, read from the live run while it is
// going and from the run's own row once it is not.
func (a *Agent) planRunCopy(root string) *TaskCopyRecord {
	a.beltMu.Lock()
	if run := a.beltRun; run != nil && planStoreID(run.root) == root {
		copied := runCopyOf(run.tree)
		a.beltMu.Unlock()
		if copied != nil {
			return copied
		}
	} else {
		a.beltMu.Unlock()
	}
	g := a.graph()
	if g == nil {
		return nil
	}
	g.mu.Lock()
	rows := g.runRowsLocked()
	g.mu.Unlock()
	// THE NEWEST ROW NAMING THE RUN WINS, because every publish replaces the
	// last and only a later one can know more ([Agent.publishRunRow]).
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].PlanTask == root && rows[i].Copy != nil {
			return rows[i].Copy
		}
	}
	return nil
}

// readPlanWork asks git what the copy holds against the commit it was cut
// from. A copy that is gone from disk is read off its branch instead, which is
// where a stopped or landed run's work still is.
//
// A COPY THAT WAS GIVEN BACK IS STILL A FOLDER ON DISK. An ended run leaves its
// copy behind as a plain folder with the note that it was released, and git no
// longer answers for it, so only a folder git still answers for is read as the
// live copy. Every other copy, gone or given back, is read off its branch.
func readPlanWork(copied *TaskCopyRecord) PlanTaskWork {
	work := PlanTaskWork{Dir: strings.TrimSpace(copied.Dir)}
	base := strings.TrimSpace(copied.HomeSha)
	live := false
	if info, err := os.Stat(work.Dir); err == nil && info.IsDir() {
		_, live = runTreeRoot(work.Dir)
	}
	if live {
		if base == "" {
			base = "HEAD"
		}
		patch, err := git(work.Dir, planWorkDiffArgs(base, "--", ".")...)
		if err != nil {
			return work
		}
		work.Patch, work.Cut = planWorkPatch(patch)
		if out, err := git(work.Dir, "ls-files", "--others", "--exclude-standard", "-z", "--", "."); err == nil {
			// An untracked build cache is left out as the landing leaves it
			// out ([buildCache]), so the tab never previews a file that will
			// not come home.
			for _, name := range gitNULPaths(out) {
				if !harnessWrote(filepath.ToSlash(name)) && !buildCache(filepath.ToSlash(name)) {
					work.Added = append(work.Added, name)
				}
			}
		}
		work.Read = true
		return work
	}
	root, branch := strings.TrimSpace(copied.Root), strings.TrimSpace(copied.Branch)
	if root == "" || branch == "" || base == "" {
		return work
	}
	patch, err := git(root, planWorkDiffArgs(base, "refs/heads/"+branch, "--")...)
	if err != nil {
		return work
	}
	work.Patch, work.Cut = planWorkPatch(patch)
	work.Read = true
	return work
}

// planWorkDiffArgs is the one spelling of the difference the work tab reads,
// on the live copy and off a given-back copy's branch alike, with the caller's
// revisions and pathspec after it.
//
// THE HEADER'S SHAPE IS PINNED HERE, NOT LEFT TO THE PERSON'S GIT CONFIG, because
// [PatchSectionPath] reads the file's name off it. `diff.noprefix` would write
// `diff --git P P` and `diff.mnemonicPrefix` `diff --git i/P w/P`, so the two
// prefixes are said outright. `core.quotePath=false` lets an accent arrive as
// itself rather than as octal escapes; a name with a quote or a newline is still
// quoted, and the reader unquotes it.
func planWorkDiffArgs(rest ...string) []string {
	return append([]string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}, rest...)
}

// planWorkPatch drops every file section the harness wrote and caps what is
// left. The sections are cut on git's own header line, so a file the harness
// owns is left out whole and never half.
func planWorkPatch(patch string) (string, bool) {
	var kept strings.Builder
	for _, section := range PatchSections(patch) {
		if harnessWrote(PatchSectionPath(section)) {
			continue
		}
		kept.WriteString(section)
	}
	out := kept.String()
	if len(out) <= planWorkPatchCap {
		return out, false
	}
	cut := out[:planWorkPatchCap]
	if at := strings.LastIndexByte(cut, '\n'); at > 0 {
		cut = cut[:at+1]
	}
	return cut, true
}
