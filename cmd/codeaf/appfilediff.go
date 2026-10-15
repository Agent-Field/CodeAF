package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// FILES.DIFF IS READ-ONLY GIT, BOUNDED IN TIME, IN OUTPUT AND IN NUMBER.
//
// Every process this file starts is a git that reads: rev-parse, ls-files,
// ls-tree, check-attr, diff-index and diff, told GIT_OPTIONAL_LOCKS=0 so not
// even the index's stat cache is written back. Nothing in the repository's own
// configuration may make one of them run another program: an external diff and
// a textconv are switched off by flag, fsmonitor by -c, and a file whose
// attributes send it through a filter is answered unavailable rather than
// diffed, because diffing a worktree file runs its clean filter. The person's
// global and system configuration are left alone otherwise.
//
// All git processes of one diff share one deadline of FileDiffSeconds. Their
// output is counted as it arrives, and the moment it passes what can be shown
// the process is killed; nothing is collected first and cut afterwards. At
// most FileDiffJobs diffs run at once, and one more is answered busy at once
// rather than queued, so repeated requests cannot pile up processes.

// appDiffSlots holds one token per diff being read.
var appDiffSlots = make(chan struct{}, appwire.FileDiffJobs)

// appGitErr is stderr kept from one git process, for the first line of a
// refusal: no more than this is held whatever git writes.
const appGitErr = 4 << 10

// DiffFile answers files.diff for path, relative to the agent's workspace or
// absolute, compared with base (HEAD when empty).
func (b *appBackend) DiffFile(agent, path, base string) (appwire.FileDiff, error) {
	if !appwire.ValidDiffBase(base) {
		return appwire.FileDiff{}, appwire.Errorf(appwire.CodeBadParams, "files.diff base must be a commit or ref name")
	}
	a, err := b.find(agent)
	if err != nil {
		return appwire.FileDiff{}, err
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	full := path
	if !filepath.IsAbs(full) {
		if workspace == "" {
			return appwire.FileDiff{}, appwire.Errorf(appwire.CodeBadParams, "this agent has no workspace, so %s needs a full path", path)
		}
		full = filepath.Join(workspace, full)
	}
	select {
	case appDiffSlots <- struct{}{}:
		defer func() { <-appDiffSlots }()
	default:
		return appwire.FileDiff{}, appwire.Errorf(appwire.CodeBusy, "other diffs are still being read; try again in a moment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), appwire.FileDiffSeconds*time.Second)
	defer cancel()
	return appDiff(ctx, path, filepath.Clean(full), appCanonical(workspace), base), nil
}

// appDiff is the whole answer for full (absolute, cleaned) under root, the
// canonical workspace, compared with requested when it names a commit and with
// HEAD otherwise. requested has passed appwire.ValidDiffBase.
func appDiff(ctx context.Context, path, full, root, requested string) appwire.FileDiff {
	out := appwire.FileDiff{Path: path, Resolved: full}
	resolved, err := filepath.EvalSymlinks(full)
	exists := err == nil
	if !exists {
		// Deleted, or a link to nothing: judged where it was.
		resolved = appCanonical(full)
	}
	out.Resolved = resolved
	out.Outside = !appWithin(root, resolved)
	if exists {
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			out.State, out.Reason, out.Detail = appwire.DiffUnavailable, appwire.DiffReasonGitFailed, "a folder has no diff of its own"
			return out
		}
	}
	dir := filepath.Dir(resolved)
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	g := appGit{ctx: ctx}

	top, fail := g.out(dir, 4<<10, "rev-parse", "--show-toplevel")
	if fail != nil {
		if fail.notRepo() {
			out.State, out.Folder = appwire.DiffNotGit, filepath.Dir(resolved)
			if !exists {
				out.State = appwire.DiffMissing
			}
			return out
		}
		return fail.answer(out)
	}
	top = strings.TrimSuffix(top, "\n")
	if canon, err := filepath.EvalSymlinks(top); err == nil {
		top = canon
	}
	rel, err := filepath.Rel(top, resolved)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		out.State, out.Folder = appwire.DiffNotGit, filepath.Dir(resolved)
		return out
	}
	rel = filepath.ToSlash(rel)
	out.Repo = top

	head, fail := g.out(top, 256, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if fail != nil {
		if fail.exit == 1 {
			out.State = appwire.DiffNoCommits
			return out
		}
		return fail.answer(out)
	}
	base := strings.TrimSpace(head)
	out.Base, out.Commit = appwire.DiffBaseHead, base
	if requested != "" {
		out.Requested = requested
		// Resolved to its hash here, and only the hash is handed on, so the
		// name is read as a revision exactly once, after --end-of-options.
		named, fail := g.out(top, 256, "rev-parse", "-q", "--verify", "--end-of-options", requested+"^{commit}")
		switch {
		case fail == nil && appHash(strings.TrimSpace(named)):
			base = strings.TrimSpace(named)
			out.Base, out.Commit = appwire.DiffBaseRequested, base
			_, fail := g.out(top, 256, "merge-base", "--is-ancestor", base, strings.TrimSpace(head))
			if fail != nil && fail.exit != 1 {
				return fail.answer(out)
			}
			inHistory := fail == nil
			out.InHistory = &inHistory
		case fail == nil || fail.exit == 1 || fail.exit == 128:
			out.RequestedUnavailable = appwire.DiffRequestedUnknown
		default:
			return fail.answer(out)
		}
	}

	inIndex, fail := g.out(top, 64<<10, "ls-files", "-z", "--cached", "--", rel)
	if fail != nil {
		return fail.answer(out)
	}
	inBase, fail := g.out(top, 64<<10, "ls-tree", "-z", "-l", base, "--", rel)
	if fail != nil {
		return fail.answer(out)
	}
	if inIndex == "" && inBase == "" {
		out.Base, out.Commit, out.InHistory = "", "", nil
		if !exists {
			out.State = appwire.DiffMissing
			return out
		}
		others, fail := g.out(top, 64<<10, "ls-files", "-z", "--others", "--exclude-standard", "--", rel)
		if fail != nil {
			return fail.answer(out)
		}
		if others == "" {
			out.State = appwire.DiffIgnored
			return out
		}
		return appUntracked(out, resolved)
	}

	attrs, fail := g.out(top, 4<<10, "check-attr", "-z", "filter", "--", rel)
	if fail != nil {
		return fail.answer(out)
	}
	if f := strings.Split(attrs, "\x00"); len(f) >= 3 && f[2] != "unspecified" && f[2] != "unset" {
		out.State, out.Reason, out.Detail = appwire.DiffUnavailable, appwire.DiffReasonFilter, f[2]
		return out
	}

	paths := []string{rel}
	if inBase == "" {
		// Added since the base: a staged rename names where it came from.
		renames, fail := g.out(top, 256<<10, "diff-index", "--cached", "-M", "-z", "--name-status", "--no-ext-diff", base)
		if fail == nil {
			if old := appRenamedFrom(renames, rel); old != "" {
				out.OldPath, paths = old, []string{old, rel}
			}
		} else if fail.timedOut {
			return fail.answer(out)
		}
	}
	// Bounding git's output does not bound what git reads to produce it, so
	// a side over the preview's bound is refused before anything is diffed.
	if exists {
		if info, err := os.Stat(resolved); err == nil && info.Size() > appwire.FileReadMax {
			out.State, out.Reason = appwire.DiffUnavailable, appwire.DiffReasonTooLarge
			return out
		}
	}
	if out.OldPath != "" {
		if inBase, fail = g.out(top, 64<<10, "ls-tree", "-z", "-l", base, "--", out.OldPath); fail != nil {
			return fail.answer(out)
		}
	}
	if appTreeTooLarge(inBase) {
		out.State, out.Reason = appwire.DiffUnavailable, appwire.DiffReasonTooLarge
		return out
	}
	args := append([]string{"diff", "--numstat", "-z", "--no-color", "--no-ext-diff", "--no-textconv", "-M", base, "--"}, paths...)
	numstat, fail := g.out(top, 64<<10, args...)
	if fail != nil {
		return fail.answer(out)
	}
	add, del, binary, found := appNumstat(numstat)
	switch {
	case !found:
		out.State = appwire.DiffUnchanged
		return out
	case out.OldPath != "":
		out.Change = "renamed"
	case inBase == "":
		out.Change = "added"
	case !exists || inIndex == "":
		out.Change = "deleted"
	default:
		out.Change = "modified"
	}
	out.State = appwire.DiffChanged
	if binary {
		out.Binary = true
		return out
	}
	out.Add, out.Del = &add, &del

	args = append([]string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "--unified=3", base, "--"}, paths...)
	hunks, overflow, fail := g.capped(top, appwire.FileDiffMax+16<<10, appwire.FileDiffLines+256, args...)
	if fail != nil {
		return fail.answer(out)
	}
	out.Text, out.Lines, out.Truncated = appHunks(hunks)
	out.Truncated = out.Truncated || overflow
	if !utf8.ValidString(out.Text) {
		// Text git took for text but that is not UTF-8 would be altered on the
		// way out, so it is answered as binary: facts, no body, no counts.
		out.Binary, out.Text, out.Lines, out.Truncated, out.Add, out.Del = true, "", 0, false, nil, nil
	}
	return out
}

// appTreeTooLarge says whether a `ls-tree -z -l` entry is a blob over
// FileReadMax.
func appTreeTooLarge(out string) bool {
	for _, entry := range strings.Split(out, "\x00") {
		meta, _, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) == 4 {
			if size, err := strconv.ParseInt(fields[3], 10, 64); err == nil && size > appwire.FileReadMax {
				return true
			}
		}
	}
	return false
}

// appHash says whether s is a whole SHA-1 or SHA-256 object name.
func appHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// appUntracked answers an untracked file as wholly added, from the file itself,
// within the same bounds as files.read: there is no base, so there are no
// hunks to ask git for.
func appUntracked(out appwire.FileDiff, resolved string) appwire.FileDiff {
	out.State, out.Base = appwire.DiffUntracked, appwire.DiffBaseNone
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return out
	}
	if info.Size() > appwire.FileReadMax {
		out.Reason = appwire.DiffReasonTooLarge
		return out
	}
	file, err := os.Open(resolved)
	if err != nil {
		return out
	}
	data, err := io.ReadAll(io.LimitReader(file, appwire.FileReadMax+1))
	_ = file.Close()
	if err != nil || len(data) > appwire.FileReadMax {
		out.Reason = appwire.DiffReasonTooLarge
		return out
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		out.Binary = true
		return out
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	add, del := len(lines), 0
	out.Add, out.Del = &add, &del
	var text strings.Builder
	if add > 0 {
		text.WriteString("@@ -0,0 +1," + strconv.Itoa(add) + " @@\n")
	}
	for _, line := range lines {
		text.WriteString("+")
		text.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			text.WriteString("\n\\ No newline at end of file\n")
		}
	}
	out.Text, out.Lines, out.Truncated = appHunks(text.String())
	return out
}

// appHunks keeps diff output from its first hunk, drops every file header in
// it, and cuts after at most FileDiffLines lines and FileDiffMax bytes.
func appHunks(raw string) (text string, lines int, truncated bool) {
	var kept strings.Builder
	header := true
	for raw != "" {
		line := raw
		if i := strings.IndexByte(raw, '\n'); i >= 0 {
			line, raw = raw[:i+1], raw[i+1:]
		} else {
			raw = ""
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header = true
			continue
		case strings.HasPrefix(line, "@@"):
			header = false
		case header:
			continue
		}
		if lines >= appwire.FileDiffLines || kept.Len()+len(line) > appwire.FileDiffMax {
			return kept.String(), lines, true
		}
		kept.WriteString(line)
		lines++
	}
	return kept.String(), lines, false
}

// appNumstat totals `diff --numstat -z`; a "-" count is a binary file.
func appNumstat(out string) (add, del int, binary, found bool) {
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		found = true
		if parts[0] == "-" || parts[1] == "-" {
			binary = true
		} else {
			a, _ := strconv.Atoi(parts[0])
			d, _ := strconv.Atoi(parts[1])
			add, del = add+a, del+d
		}
		if parts[2] == "" {
			// A rename: the two names follow as their own fields.
			i += 2
		}
	}
	return add, del, binary, found
}

// appRenamedFrom is the source of a rename to rel in `--name-status -z`.
func appRenamedFrom(out, rel string) string {
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		status := fields[i]
		switch {
		case strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C"):
			if i+2 < len(fields) && fields[i+2] == rel && strings.HasPrefix(status, "R") {
				return fields[i+1]
			}
			i += 2
		case status != "":
			i++
		}
	}
	return ""
}

// appGit runs read-only git under one deadline.
type appGit struct{ ctx context.Context }

// appGitFail is why one git process did not answer.
type appGitFail struct {
	exit     int
	timedOut bool
	missing  bool
	stderr   string
}

func (f *appGitFail) notRepo() bool {
	return f.exit == 128 && (strings.Contains(f.stderr, "not a git repository") || strings.Contains(f.stderr, "must be run in a work tree"))
}

// answer is the diff unavailable for this failure.
func (f *appGitFail) answer(out appwire.FileDiff) appwire.FileDiff {
	out.State = appwire.DiffUnavailable
	switch {
	case f.timedOut:
		out.Reason = appwire.DiffReasonTimeout
	case f.missing:
		out.Reason = appwire.DiffReasonNoGit
	case strings.Contains(f.stderr, "dubious ownership"):
		out.Reason = appwire.DiffReasonUnsafe
	default:
		out.Reason = appwire.DiffReasonGitFailed
		out.Detail, _, _ = strings.Cut(strings.TrimSpace(f.stderr), "\n")
	}
	return out
}

// out is one git's whole stdout, which must fit in limit bytes.
func (g appGit) out(dir string, limit int, args ...string) (string, *appGitFail) {
	text, overflow, fail := g.capped(dir, limit, 0, args...)
	if fail == nil && overflow {
		return "", &appGitFail{exit: -1, stderr: "git answered more than this preview reads"}
	}
	return text, fail
}

// capped runs git in dir and keeps at most limit bytes and, when maxLines is
// above zero, maxLines lines of stdout. Past either the process is killed and
// overflow is true; what was kept is still answered.
func (g appGit) capped(dir string, limit, maxLines int, args ...string) (string, bool, *appGitFail) {
	if err := g.ctx.Err(); err != nil {
		return "", false, &appGitFail{exit: -1, timedOut: true}
	}
	ctx, kill := context.WithCancel(g.ctx)
	defer kill()
	full := append([]string{
		"--no-pager", "--literal-pathspecs", "--no-optional-locks",
		"-c", "core.fsmonitor=false",
		"-c", "diff.noprefix=false",
		"-C", dir,
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = appGitEnv()
	cmd.Stdin = nil
	cmd.WaitDelay = 250 * time.Millisecond
	stdout := &appCap{limit: limit, lines: maxLines, kill: kill}
	stderr := &appCap{limit: appGitErr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	overflow, text := stdout.held()
	if overflow {
		return text, true, nil
	}
	if g.ctx.Err() != nil {
		return "", false, &appGitFail{exit: -1, timedOut: true}
	}
	if err != nil {
		fail := &appGitFail{exit: -1, stderr: stderr.String()}
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			fail.exit = exitErr.ExitCode()
		case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
			fail.missing = true
		}
		return "", false, fail
	}
	return text, false, nil
}

// appGitEnv is this process's environment without what would point git at
// another repository or another diff program, and with prompts and optional
// locks off. Configuration locations are the person's own and stay.
func appGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_OBJECT_DIRECTORY": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_COMMON_DIR": true, "GIT_NAMESPACE": true,
		"GIT_EXTERNAL_DIFF": true, "GIT_DIFF_OPTS": true, "GIT_PAGER": true, "PAGER": true,
	}
	env := []string{}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
}

// appCap holds at most limit bytes (and lines, when set). Past that it keeps
// nothing more, calls kill once, and still accepts the writes so the copy
// out of the pipe never stalls.
type appCap struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
	lines int
	seen  int
	over  bool
	kill  func()
}

func (c *appCap) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.over {
		return len(p), nil
	}
	keep := p
	if room := c.limit - c.buf.Len(); len(keep) > room {
		keep, c.over = keep[:max(room, 0)], true
	}
	if c.lines > 0 {
		for i, ch := range keep {
			if ch == '\n' {
				c.seen++
				if c.seen >= c.lines {
					keep, c.over = keep[:i+1], true
					break
				}
			}
		}
	}
	c.buf.Write(keep)
	if c.over && c.kill != nil {
		c.kill()
	}
	return len(p), nil
}

// held is what was kept, and whether more was refused.
func (c *appCap) held() (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.over, c.buf.String()
}

func (c *appCap) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

var _ appwire.FileDiffer = (*appBackend)(nil)
