package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RunTreeSnapshot is what a working copy held before a run touched it: the
// commit it stood on and, for every path git already saw as changed, what
// that path held. A door that runs IN PLACE — `codeaf do`, which edits the
// directory it was handed and makes no commit of its own — takes one before the run and
// asks it afterwards which paths the RUN changed, so the files it names are
// the run's and never the person's own edits that were sitting there first.
//
// THE PERSON'S WORK IS NOT THE RUN'S WORK. A copy's `git status` after a run is
// the run's changes AND whatever the person had not committed yet, and the
// landing that staged the whole of it committed somebody's half-finished edit
// and an untracked secrets file as `task: <title>` on their own branch. The
// snapshot is how the two are told apart without any ledger: a path the run
// did not touch holds exactly what it held before.
//
// A directory that is not inside a git work tree has no snapshot to take, and
// [RunTreeSnapshot.Changed] answers nothing for it: there is no status to read
// the run's work off, and a walk of an arbitrary folder is not this door's
// business.
type RunTreeSnapshot struct {
	dir  string
	root string
	head string
	// unborn means the repository existed but its branch had no commit when
	// the run started; an unknown base in a non-repository is different.
	unborn bool
	// held is every path git saw as changed before the run, by absolute path,
	// and what it held then: a digest of its bytes, or empty when it was gone.
	held map[string]string
}

// SignRunCommits signs commits made since this snapshot in an in-place run.
// A snapshot on an unborn branch can sign the run's first commits; a snapshot
// outside git has no branch whose new history belongs to the run.
func (s RunTreeSnapshot) SignRunCommits(model string) (int, error) {
	sign := gitSignature{named: model != "", model: model}
	if s.unborn {
		return signUnbornRunCommits(s.root, sign)
	}
	return signRunCommits(s.root, s.head, sign)
}

// SnapshotRunTree reads dir's working copy as it stands now.
func SnapshotRunTree(dir string) RunTreeSnapshot {
	snapshot := RunTreeSnapshot{dir: dir}
	root, ok := runTreeRoot(dir)
	if !ok {
		return snapshot
	}
	snapshot.root = root
	snapshot.head = runTreeHead(root)
	snapshot.unborn = snapshot.head == ""
	snapshot.held = make(map[string]string)
	for _, path := range runTreeStatus(root) {
		snapshot.held[path] = runTreeDigest(path)
	}
	return snapshot
}

// Changed answers the paths the run changed since the snapshot was taken, as
// absolute paths, sorted: a path git sees as changed now that was clean before,
// a path that was already changed and now holds something else, a path that
// was changed before and is clean now (the run put it back), and every path a
// commit the run made itself carried. The harness's own files are never in it
// ([harnessWrote]).
func (s RunTreeSnapshot) Changed() []string {
	if s.root == "" {
		return nil
	}
	seen := make(map[string]bool)
	var changed []string
	add := func(path string) {
		if seen[path] || s.harnessOwns(path) {
			return
		}
		seen[path] = true
		changed = append(changed, path)
	}
	now := runTreeStatus(s.root)
	still := make(map[string]bool, len(now))
	for _, path := range now {
		still[path] = true
		before, was := s.held[path]
		if !was || before != runTreeDigest(path) {
			add(path)
		}
	}
	for path := range s.held {
		if !still[path] {
			add(path)
		}
	}
	// A RUN THAT COMMITTED ITS OWN WORK moved HEAD, and what it committed is
	// clean in the status above. Those paths are the run's too.
	if head := runTreeHead(s.root); head != "" && head != s.head && (s.head != "" || s.unborn) {
		if s.unborn {
			// The unborn branch has no base for git diff; its first commits
			// still belong in an in-place run's file envelope.
			if paths, err := runTouchedPaths(s.root, "", head); err == nil {
				for _, name := range paths {
					add(filepath.Join(s.root, filepath.FromSlash(name)))
				}
			}
		} else if out, err := git(s.root, "diff", "--name-only", "-z", s.head, head); err == nil {
			for _, name := range strings.Split(out, "\x00") {
				if name = strings.TrimSpace(name); name != "" {
					add(filepath.Join(s.root, filepath.FromSlash(name)))
				}
			}
		}
	}
	sort.Strings(changed)
	return changed
}

// harnessOwns says whether an absolute path is one the harness itself wrote
// under the directory the run was handed, read relative to that directory
// because the harness's own folder sits there and not at the repository's top.
// A path outside the directory is the run's: a worker that edited above the
// folder it was handed still edited it.
func (s RunTreeSnapshot) harnessOwns(path string) bool {
	base := canonicalPath(s.dir)
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return harnessWrote(filepath.ToSlash(relative))
}

// harnessWrote is THE ONE ANSWER to which paths inside a working copy are the
// harness's own rather than the work: its folder of droppings, its plan store
// under the name every road agrees on ([planStoreFilename], and the files the
// store's engine keeps beside it), and the two shims it arms (`plandb` and
// `codeaf`, plandb_plan.go's armShim). It is read by the belt
// landing ([beltTreeWork]) and by an in-place run's account of what it changed
// ([RunTreeSnapshot.Changed]), so the two cannot disagree about it.
//
// A PATH IS THE HARNESS'S ONLY BECAUSE THE HARNESS WROTE IT THERE, never
// because of what a file of that kind is usually called. A suffix such as
// `.lock` is the name a project's own lockfile carries — the one every package
// manager keeps beside the manifest — and a rule that dropped every such file
// dropped the run's dependency changes from every landing while the run said
// it had made them.
func harnessWrote(path string) bool {
	switch {
	case isTaskDropping(path):
		return true
	case path == planStoreFilename,
		strings.HasPrefix(path, planStoreFilename+"."),
		strings.HasPrefix(path, planStoreFilename+"-"):
		return true
	case path == "bin/"+planShimFilename, path == "bin/"+codeafShimFilename:
		return true
	}
	return false
}

// runTreeRoot is the canonical top of the work tree dir sits in, and false
// when dir is not inside one. It asks through [repositoryRoot], the one asker
// of that question, so a scratch folder that happens to sit inside somebody
// else's checkout reads as no repository rather than as theirs.
func runTreeRoot(dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	return repositoryRoot(dir)
}

// runTreeHead is the commit the copy stands on, and empty on a repository with
// no commit yet.
func runTreeHead(root string) string {
	out, err := git(root, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// runTreeStatus is every path git sees as changed in the copy — modified,
// added, deleted and untracked alike — as absolute paths. It reads the NUL
// form so a name with a space, a quote or a newline in it is itself; a rename
// carries both names and both are answered, because both moved.
func runTreeStatus(root string) []string {
	out, err := git(root, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil
	}
	var paths []string
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		code, name := entry[:2], entry[3:]
		paths = append(paths, filepath.Join(root, filepath.FromSlash(name)))
		if code[0] == 'R' || code[0] == 'C' {
			// The next field is the name it came from.
			if i+1 < len(fields) && fields[i+1] != "" {
				paths = append(paths, filepath.Join(root, filepath.FromSlash(fields[i+1])))
			}
			i++
		}
	}
	return paths
}

// runTreeDigest is what one path holds: a digest of a file's bytes, the target
// of a link, a word for a directory, and empty for a path that is not there.
func runTreeDigest(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "link"
		}
		return "link:" + target
	case info.IsDir():
		return "dir"
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "unreadable"
	}
	sum := sha256.Sum256(contents)
	return info.Mode().Perm().String() + ":" + hex.EncodeToString(sum[:])
}
