package session

// deliverable_task_git.go is what git itself can PROVE about a task worker's
// file, and nothing it would have to be trusted to guess.
//
// THREE QUESTIONS, EACH ANSWERED FROM ONE NAMED OBJECT, never from a working
// directory that somebody else may be writing:
//   - is this new file inside a real repository, untracked and not ignored
//     ([untrackedInRepo]) — the proof "created in the repository" needs;
//   - which regular-file blob does this revision hold at this path
//     ([treeBlob]) — never a link, a submodule or a directory, and a path is
//     always a path (GIT_LITERAL_PATHSPECS), never a pattern;
//   - what are that blob's bytes ([readBlob]) — streamed from the object
//     store and bounded, under the caller's context.
//
// Every call runs under a context, so a git that hangs costs its caller's
// bound and no more.

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// deliverGitCallMax bounds one git question asked on a write or a read path.
var deliverGitCallMax = 3 * time.Second

// literalPaths makes every path argument a path and never a pattern.
var literalPaths = []string{"GIT_LITERAL_PATHSPECS=1"}

// errNoBlob is a path that holds no regular file at that revision.
var errNoBlob = errors.New("no regular file at that path")

// repoTop is the top of the repository dir is inside, canonical, or "" when
// it is not inside one. It is [repositoryRoot]'s answer, the one asker of that
// question, so a scratch folder sitting inside somebody's checkout is never
// taken for a file in their repository.
func repoTop(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	root, ok := repositoryRoot(dir)
	if !ok {
		return ""
	}
	return canonicalFile(root)
}

// untrackedInRepo says git lists rel (relative to top) as an untracked file
// that is not ignored: a file new to that repository's work tree.
func untrackedInRepo(ctx context.Context, top, rel string) bool {
	if !validBranchRel(rel) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	out, err := gitContext(ctx, top, literalPaths, "ls-files", "--others", "--exclude-standard", "-z", "--", rel)
	return err == nil && out == rel+"\x00"
}

// treeEntry is one entry of a tree as ls-tree names it.
type treeEntry struct{ mode, kind, oid string }

// regular says the entry is a file: not a link, a submodule or a folder.
func (e treeEntry) regular() bool {
	return e.kind == "blob" && (e.mode == "100644" || e.mode == "100755")
}

// treeEntries is what rev holds at each of rels, in ONE git call; a rel with
// no entry is absent from the answer. Every rel must be a valid one.
func treeEntries(ctx context.Context, dir, rev string, rels []string) (map[string]treeEntry, error) {
	if strings.TrimSpace(rev) == "" || strings.HasPrefix(rev, "-") || len(rels) == 0 {
		return nil, errNoBlob
	}
	for _, rel := range rels {
		if !validBranchRel(rel) {
			return nil, errNoBlob
		}
	}
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	out, err := gitContext(ctx, dir, literalPaths, append([]string{"ls-tree", "-z", "--full-tree", rev, "--"}, rels...)...)
	if err != nil {
		return nil, err
	}
	entries := map[string]treeEntry{}
	for _, record := range strings.Split(out, "\x00") {
		head, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(head)
		if ok && len(fields) == 3 {
			entries[path] = treeEntry{mode: fields[0], kind: fields[1], oid: fields[2]}
		}
	}
	return entries, nil
}

// treeBlob is the object id of the regular file rev holds at rel. A link, a
// submodule, a directory or no entry at all is errNoBlob.
func treeBlob(ctx context.Context, dir, rev, rel string) (string, error) {
	entries, err := treeEntries(ctx, dir, rev, []string{rel})
	if err != nil {
		return "", err
	}
	entry, ok := entries[rel]
	if !ok || !entry.regular() {
		return "", errNoBlob
	}
	return entry.oid, nil
}

// blobCommand starts `git cat-file blob oid` in dir with its output on a pipe.
func blobCommand(ctx context.Context, dir, oid string) (*exec.Cmd, io.ReadCloser, error) {
	if strings.TrimSpace(dir) == "" || len(oid) < 40 || !isHex(oid) {
		return nil, nil, errNoBlob
	}
	command := exec.CommandContext(ctx, "git", "cat-file", "blob", oid)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := command.Start(); err != nil {
		return nil, nil, err
	}
	return command, out, nil
}

// readBlob reads at most max bytes of the blob oid.
func readBlob(ctx context.Context, dir, oid string, max int) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	command, out, err := blobCommand(ctx, dir, oid)
	if err != nil {
		return nil, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(out, int64(max)+1))
	_ = out.Close() // git sees a closed pipe and stops: nothing past max is read
	_ = command.Wait()
	if readErr != nil {
		return nil, false, readErr
	}
	if len(data) > max {
		return data[:max], true, nil
	}
	return data, false, nil
}
