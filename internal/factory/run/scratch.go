package run

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A STEP'S SCRATCH IS NEVER INSIDE ITS CHECKOUT. On the 2026-11-06 live run
// (factory-demo PR #148) the proof step needed a second copy of the repository
// to prove a prefix case, had nowhere else to put one, and made it at
// `.proof-prefix-15992/` in the checkout. Everything left in the checkout is
// the change, so the copy was measured as ten new files, tripped the
// protected-path approval as `.proof-prefix-15992/auth/token.go`, and was
// committed into the pull request.
//
// Two halves, so neither the model's obedience nor a guess about names is the
// only guard:
//
//   - every conversation step is given a scratch folder OUTSIDE its worktree
//     ([ScratchDir]), made before it opens and named in its brief, with the
//     rule that what is left in the checkout is published;
//   - a copy of the repository that is left in the checkout anyway is never
//     part of the change ([repoCopies]): the change a page shows, the paths
//     the approval judges and the commit a publish makes all leave it out,
//     and it stays on disk, untracked, for the person to look at.

// ScratchDir is the scratch folder of the job whose worktree is worktree: a
// folder of its own under the machine's temporary directory, outside the
// checkout and outside git, so nothing in it is ever measured or published,
// and never beside a person's own checkout either. It is the same folder for
// every round of the job on this machine, so a later step finds what an
// earlier one left. "" for no worktree.
func ScratchDir(worktree string) string {
	if strings.TrimSpace(worktree) == "" {
		return ""
	}
	clean := filepath.Clean(worktree)
	sum := sha256.Sum256([]byte(clean))
	return filepath.Join(os.TempDir(), "codeaf-scratch", filepath.Base(clean)+"-"+hex.EncodeToString(sum[:4]))
}

// makeScratch makes the job's scratch folder. A folder that cannot be made
// is not a reason to hold the step back: the brief still names it, and the
// step can make it itself.
func makeScratch(worktree string) {
	if dir := ScratchDir(worktree); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
}

// scratchBrief is the brief's paragraph that names the step's scratch folder.
func scratchBrief(scratch string) string {
	if scratch == "" {
		return ""
	}
	return "scratch: " + scratch + " is this job's own folder outside the checkout, for copies of the repository, experiments, logs and other throwaway files. Make none of them inside the checkout: everything left in the checkout is published with the change."
}

// repoCopies is the folders among the job's added files that are a copy of
// the repository rather than a change to it: a top-level folder that did not
// exist at the base, at least two of whose files sit at a path the repository
// tracked at the base, most of whose files do, and that is either hidden
// (`.proof-prefix-15992/`) or copies at least half of the repository. A new
// package, even one that starts as a copy of a few files, is none of these.
// Sorted; nil for none.
func repoCopies(added []string, tracked map[string]bool) []string {
	if len(tracked) == 0 {
		return nil
	}
	existed := map[string]bool{}
	for path := range tracked {
		if top, _, ok := strings.Cut(path, "/"); ok {
			existed[top] = true
		}
	}
	type tally struct{ files, mirrored int }
	dirs := map[string]*tally{}
	for _, path := range added {
		top, rest, ok := strings.Cut(path, "/")
		if !ok || existed[top] || rest == "" {
			continue
		}
		t := dirs[top]
		if t == nil {
			t = &tally{}
			dirs[top] = t
		}
		t.files++
		if tracked[rest] {
			t.mirrored++
		}
	}
	var out []string
	for top, t := range dirs {
		whole := t.mirrored*2 >= len(tracked)
		if t.mirrored >= 2 && t.mirrored*2 > t.files && (strings.HasPrefix(top, ".") || whole) {
			out = append(out, top)
		}
	}
	sort.Strings(out)
	return out
}

// inCopy says whether path lies under one of the folders copies names.
func inCopy(path string, copies []string) bool {
	for _, top := range copies {
		if strings.HasPrefix(path, top+"/") {
			return true
		}
	}
	return false
}

// trackedAt is every path the repository tracked at base, nil when it cannot
// be read (then no folder is judged a copy).
func trackedAt(git Git, dir, base string) map[string]bool {
	if strings.TrimSpace(base) == "" {
		return nil
	}
	out, err := git.Run(dir, "ls-tree", "-r", "--name-only", "-z", base)
	if err != nil {
		return nil
	}
	tracked := map[string]bool{}
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			tracked[path] = true
		}
	}
	return tracked
}

// excludeCopies is the git pathspecs that leave each copy out.
func excludeCopies(copies []string) []string {
	if len(copies) == 0 {
		return nil
	}
	out := []string{"."}
	for _, top := range copies {
		out = append(out, ":(exclude)"+top)
	}
	return out
}

// checkoutCopies is [repoCopies] over what the checkout at dir adds to base:
// files committed since it and files not yet tracked. No base, nothing judged.
func checkoutCopies(git Git, dir, base string) ([]string, error) {
	tracked := trackedAt(git, dir, base)
	if tracked == nil {
		return nil, nil
	}
	var added []string
	committed, err := git.Run(dir, "diff", "--no-renames", "--name-only", "--diff-filter=A", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := git.Run(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, list := range []string{committed, untracked} {
		for _, path := range strings.Split(list, "\x00") {
			if path != "" {
				added = append(added, path)
			}
		}
	}
	return repoCopies(added, tracked), nil
}
