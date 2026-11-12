package session

// task_branches.go answers "which of this conversation's task branches are not
// in the checkout yet", from git, a page at a time.
//
// The branch NAMES come from the conversation's own task records (tasks.json),
// so a branch somebody else made with a `task/` prefix is never this
// conversation's. Every number comes from git itself: whether the branch is
// merged is `git for-each-ref --no-merged`, and its file counts are `git diff
// --numstat` from the merge base. The task record's own Changed list is never
// read for a count, because a record can outlive the branch it describes.
//
// EVERY RECORDED NAME IS REACHABLE AND NOTHING IS UNBOUNDED. A listing used to
// read only the first two hundred names, silently, and ask git four
// uncancellable questions of each. Now the recorded names are walked in record
// order by a cursor, a batch at a time, every git call under the request's own
// context and with its output capped; a page that runs out of time says so and
// resumes where it stopped, and a page that could examine nothing is busy,
// never "none". The cursor is tied to the recorded list it indexes and to the
// HEAD commit its rows were judged against, and a cursor from any other is
// refused, never re-aimed at a nearby place.
//
// There is no merge here. Landing a branch is the person's own act, planned
// and confirmed through task_branch_land.go.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

// TaskBranchFact is one unmerged branch of a conversation's.
type TaskBranchFact struct {
	Name  string
	Task  string
	Title string
	// TitleTruncated says Title was cut to taskBranchTitleMax bytes.
	TitleTruncated bool
	Head           string
	// Base is the merge base with the checkout's HEAD, "" when there is none
	// or git could not say.
	Base string
	// Files, Added and Removed are from `git diff --numstat Base..Head`. A
	// binary file counts as a file and adds no lines. CountsKnown is false when
	// git could not say (no merge base, a failed diff, or a diff that could not
	// be read within the request's time); the counts are then zero and mean
	// nothing.
	Files       int
	Added       int
	Removed     int
	CountsKnown bool
	// Capped says the numstat was longer than the engine reads: the counts are
	// of the part read, so they are lower bounds.
	Capped bool
}

const (
	// TaskBranchPageDefault and TaskBranchPageMax bound one page's rows.
	TaskBranchPageDefault = 50
	TaskBranchPageMax     = 100
	// taskBranchBatch is how many recorded names one for-each-ref asks about,
	// and taskBranchBatchOutMax bounds what it may answer.
	taskBranchBatch       = 64
	taskBranchBatchOutMax = 64 << 10
	taskBranchTitleMax    = 512
	// taskBranchListingsMax is how many branch listings the whole process runs
	// at once. A third is busy at once; nothing waits in a queue.
	taskBranchListingsMax = 2
	// taskBranchTickEvery is how many steps of a linear pass over the records
	// run between two looks at the request's clock.
	taskBranchTickEvery = 1024
	// taskBranchDiagMax bounds the diagnostic git's no-repository answer is
	// read from.
	taskBranchDiagMax = 4096
)

// taskBranchSlots are the listings running now, process-wide. A slot is taken
// without waiting and given back only when ListTaskBranches returns, which is
// after every git it started has exited and its decode has finished - a
// cancelled request still holds its slot until then.
var taskBranchSlots = make(chan struct{}, taskBranchListingsMax)

// taskBranchTick looks at ctx once every taskBranchTickEvery steps, so a pass
// over a hundred thousand records ends with the request.
type taskBranchTick struct {
	ctx context.Context
	n   int
}

func (t *taskBranchTick) spent() bool {
	t.n++
	return t.n%taskBranchTickEvery == 0 && t.ctx.Err() != nil
}

// The most of the task records one listing reads. Variables only so a test
// can make them small.
var (
	taskBranchRecordsMax    int64 = 32 << 20
	taskBranchTombstonesMax int64 = 8 << 20
	// A diff's numstat is read record by record and never kept: these bound
	// how much of it is read.
	taskBranchNumstatMax      = 20000
	taskBranchNumstatBytesMax = 4 << 20
)

var (
	// ErrTaskBranchCursor is a cursor this engine did not issue, or one issued
	// for another recorded list or another HEAD.
	ErrTaskBranchCursor = errors.New("branch cursor is not valid for the task records and HEAD as they are now")
	// ErrTaskBranchBusy is a page that ran out of time before it could examine
	// a single recorded name.
	ErrTaskBranchBusy = errors.New("git did not answer in time; try again")
	// ErrTaskBranchRecordsTooLarge is a task checkpoint or tombstone file
	// larger than a listing reads.
	ErrTaskBranchRecordsTooLarge = errors.New("the task records are larger than a branch listing reads")
	// ErrTaskBranchRecords is task records that exist and cannot be read, or a
	// git failure before any name was examined: unknown, never "none".
	ErrTaskBranchRecords = errors.New("the task records could not be read")
	ErrTaskBranchGit     = errors.New("git could not list the task branches")
)

// validTaskBranch is a branch name this package would have cut: `task/` and a
// plain slug, nothing a ref could use to name another thing.
func validTaskBranch(name string) bool {
	rest, ok := strings.CutPrefix(name, "task/")
	if !ok || rest == "" || len(name) > 200 || strings.Contains(name, "..") {
		return false
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return !strings.HasSuffix(rest, ".lock") && !strings.HasPrefix(rest, ".") && !strings.HasSuffix(rest, ".")
}

func gitExit(err error) (code int, ran bool) {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), true
	}
	return 0, false
}

type namedBranch struct{ branch, task, title string }

// recordedTaskBranches are the `task/*` branches the conversation's own task
// records name, once each, in record order: nodes, then runs, the first
// mention winning, leaving out the excluded ids. There is no cap; the cursor
// walks them all.
func recordedTaskBranches(ctx context.Context, document taskDocument, excluded map[string]bool) ([]namedBranch, error) {
	var out []namedBranch
	seen := map[string]bool{}
	tick := taskBranchTick{ctx: ctx}
	add := func(branch string, id uint64, title string) bool {
		if validTaskBranch(branch) && !seen[branch] && !excluded[strconv.FormatUint(id, 10)] {
			seen[branch] = true
			out = append(out, namedBranch{branch, strconv.FormatUint(id, 10), title})
		}
		return !tick.spent()
	}
	for _, node := range document.Nodes {
		if !add(node.Branch, node.ID, node.Title) {
			return nil, errTaskBranchTime
		}
	}
	for _, run := range document.Runs {
		if !add(run.Branch, run.ID, run.Title) {
			return nil, errTaskBranchTime
		}
	}
	return out, nil
}

// readAtMost reads path whole when it is at most max bytes, a chunk at a time
// with ctx asked between chunks, so a slow disk ends with the request.
func readAtMost(ctx context.Context, path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var data []byte
	chunk := make([]byte, 1<<20)
	for {
		if ctx.Err() != nil {
			return nil, errTaskBranchTime
		}
		n, err := file.Read(chunk)
		data = append(data, chunk[:n]...)
		if int64(len(data)) > max {
			return nil, ErrTaskBranchRecordsTooLarge
		}
		if errors.Is(err, io.EOF) {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// ownedTaskBranches are the recorded names of the conversation's own task
// checkpoint, with its tombstoned tasks and their descendants taken out. A
// checkpoint never written names nothing; one that cannot be read, or
// tombstones that cannot be, are an error, because "these tasks have no
// branches" would be a guess.
//
// The checkpoint is decoded by [decodeTasks], the store's own reader, so a
// file the store would refuse names nothing here either. That decode is the
// one step not interrupted midway: it is bounded by taskBranchRecordsMax, and
// the clock is asked before and after it.
func ownedTaskBranches(ctx context.Context, place Place) ([]namedBranch, error) {
	content, err := readAtMost(ctx, place.Tasks(), taskBranchRecordsMax)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case errors.Is(err, ErrTaskBranchRecordsTooLarge), errors.Is(err, errTaskBranchTime):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("%w: %v", ErrTaskBranchRecords, err)
	}
	var deleted map[string]bool
	raw, err := readAtMost(ctx, place.truth(taskDeletedFile), taskBranchTombstonesMax)
	switch {
	case os.IsNotExist(err):
	case errors.Is(err, ErrTaskBranchRecordsTooLarge), errors.Is(err, errTaskBranchTime):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("%w: %v", ErrTaskBranchRecords, err)
	default:
		if err := json.Unmarshal(raw, &deleted); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTaskBranchRecords, err)
		}
	}
	if ctx.Err() != nil {
		return nil, errTaskBranchTime
	}
	document, err := decodeTasks(content)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTaskBranchRecords, err)
	}
	if ctx.Err() != nil {
		return nil, errTaskBranchTime
	}
	excluded, _, err := tombstonedTasks(ctx, document, deleted)
	if err != nil {
		return nil, err
	}
	return recordedTaskBranches(ctx, document, excluded)
}

// tombstonedTasks is the set [filterDeletedDocument] takes out of a document
// - every tombstoned id and every node or run row whose Parent chain reaches
// one - found in ONE pass: the rows are indexed by parent once and each id is
// visited at most once, so a deep chain costs its length, never its square.
// steps is the visits made, for a test to bound.
//
// The ids are compared as the strings the tombstone file holds, exactly as
// that filter compares them. A dependency's state is not this question
// (blockDeletedPrerequisites fails a dependent, it does not remove it), so a
// dependent of a deleted task keeps its branch here as it keeps its row there.
func tombstonedTasks(ctx context.Context, document taskDocument, deleted map[string]bool) (excluded map[string]bool, steps int, err error) {
	excluded = map[string]bool{}
	tick := taskBranchTick{ctx: ctx}
	var queue []string
	for id, gone := range deleted {
		if gone && !excluded[id] {
			excluded[id] = true
			queue = append(queue, id)
		}
		if tick.spent() {
			return nil, steps, errTaskBranchTime
		}
	}
	if len(queue) == 0 {
		return excluded, 0, nil
	}
	children, err := taskChildren(&tick, document)
	if err != nil {
		return nil, steps, err
	}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, child := range children[id] {
			steps++
			if !excluded[child] {
				excluded[child] = true
				queue = append(queue, child)
			}
			if tick.spent() {
				return nil, steps, errTaskBranchTime
			}
		}
	}
	return excluded, steps, nil
}

// taskChildren indexes every node and run row by its parent's id, once.
func taskChildren(tick *taskBranchTick, document taskDocument) (map[string][]string, error) {
	children := map[string][]string{}
	add := func(id, parent uint64) bool {
		if parent != 0 {
			key := strconv.FormatUint(parent, 10)
			children[key] = append(children[key], strconv.FormatUint(id, 10))
		}
		return !tick.spent()
	}
	for _, node := range document.Nodes {
		if !add(node.ID, node.Parent) {
			return nil, errTaskBranchTime
		}
	}
	for _, run := range document.Runs {
		if !add(run.ID, run.Parent) {
			return nil, errTaskBranchTime
		}
	}
	return children, nil
}

// TaskBranchQuery is one page's request.
type TaskBranchQuery struct {
	Cursor string
	// Limit is the most rows; 0 is TaskBranchPageDefault, and it is clamped to
	// TaskBranchPageMax.
	Limit int
}

// TaskBranchPage is one page of a conversation's unmerged task branches.
type TaskBranchPage struct {
	// Repo is true when git answered that the workspace has a HEAD commit, and
	// false only when it answered that it has none (or there is no workspace).
	Repo bool
	// Head is the HEAD commit every row was judged against.
	Head string
	// Recorded is how many names the task records give: what the cursor walks.
	Recorded int
	Branches []TaskBranchFact
	// Next resumes at the first name this page did not examine; "" means every
	// recorded name was examined against Head.
	Next string
	// Incomplete says the page ended because time ran out (or git failed after
	// progress), not because it was full or the names ran out.
	Incomplete bool
}

// taskBranchCursor is a listing's place: the index of the first recorded name
// not yet examined, the generation of the recorded list that index is into,
// and the HEAD commit the listing judges against.
type taskBranchCursor struct {
	Offset     int    `json:"o"`
	Generation string `json:"g"`
	Head       string `json:"h"`
}

func encodeTaskBranchCursor(body taskBranchCursor) string {
	raw, _ := json.Marshal(body)
	return "b1." + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeTaskBranchCursor(cursor string) (taskBranchCursor, error) {
	var body taskBranchCursor
	text, ok := strings.CutPrefix(cursor, "b1.")
	if !ok || len(text) > 512 {
		return body, ErrTaskBranchCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || json.Unmarshal(raw, &body) != nil || body.Offset < 0 || len(body.Generation) != 32 || !isHex(body.Generation) {
		return taskBranchCursor{}, ErrTaskBranchCursor
	}
	return body, nil
}

// taskBranchGeneration names one recorded list exactly: the conversation's
// place, the workspace, and every (branch, task) in order. Titles are left
// out because an index into the list does not move when a title does.
func taskBranchGeneration(ctx context.Context, place Place, workspace string, names []namedBranch) (string, error) {
	sum := sha256.New()
	tick := taskBranchTick{ctx: ctx}
	fmt.Fprintf(sum, "%d:%s%d:%s", len(place.Dir), place.Dir, len(workspace), workspace)
	for _, named := range names {
		fmt.Fprintf(sum, "%d:%s%d:%s", len(named.branch), named.branch, len(named.task), named.task)
		if tick.spent() {
			return "", errTaskBranchTime
		}
	}
	return hex.EncodeToString(sum.Sum(nil)[:16]), nil
}

// ListTaskBranches is one page of the conversation's `task/*` branches that
// still exist in workspace and are NOT reachable from its HEAD, in record
// order. ctx bounds the whole page - the HEAD check, reading the task records,
// and every git call - with one bounded exception, [ownedTaskBranches]'s
// decode.
func ListTaskBranches(ctx context.Context, place Place, workspace string, q TaskBranchQuery) (TaskBranchPage, error) {
	select {
	case taskBranchSlots <- struct{}{}:
	default:
		return TaskBranchPage{}, ErrTaskBranchBusy // two are running: busy now, never queued
	}
	defer func() { <-taskBranchSlots }()
	head, err := workspaceHead(ctx, place, workspace)
	if err != nil || head == "" {
		return TaskBranchPage{}, err
	}
	names, err := ownedTaskBranches(ctx, place)
	if err == nil {
		var generation string
		if generation, err = taskBranchGeneration(ctx, place, workspace, names); err == nil {
			return pageTaskBranches(ctx, workspace, head, generation, names, q)
		}
	}
	if errors.Is(err, errTaskBranchTime) {
		return TaskBranchPage{}, ErrTaskBranchBusy
	}
	return TaskBranchPage{}, err
}

// pageTaskBranches is one page of names, resumed at q's cursor.
func pageTaskBranches(ctx context.Context, workspace, head, generation string, names []namedBranch, q TaskBranchQuery) (TaskBranchPage, error) {
	start, err := cursorStart(q.Cursor, generation, head, len(names))
	if err != nil {
		return TaskBranchPage{}, err
	}
	page := TaskBranchPage{Repo: true, Head: head, Recorded: len(names), Branches: []TaskBranchFact{}}
	limit := q.Limit
	if limit <= 0 {
		limit = TaskBranchPageDefault
	}
	at, stopped, err := scanTaskBranches(ctx, workspace, head, names, start, min(limit, TaskBranchPageMax), &page)
	switch {
	case err != nil:
		return TaskBranchPage{}, err
	case at == start && at < len(names):
		return TaskBranchPage{}, ErrTaskBranchBusy
	case at < len(names):
		page.Next = encodeTaskBranchCursor(taskBranchCursor{Offset: at, Generation: generation, Head: head})
	}
	page.Incomplete = stopped
	return page, nil
}

// workspaceHead is the HEAD commit of workspace, "" only when it is KNOWN
// that there is none: no folder there, git's own "not a git repository", no
// work tree, or a HEAD that names a branch with no commit yet. Every other
// failure - a git that exits oddly, a refused safe.directory, a folder that
// cannot be looked at, a HEAD that names nothing - is ErrTaskBranchGit, and
// time running out is busy. Neither is ever "not a repository".
func workspaceHead(ctx context.Context, place Place, workspace string) (string, error) {
	if place.Dir == "" || strings.TrimSpace(workspace) == "" {
		return "", nil
	}
	switch info, err := os.Stat(workspace); {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return "", nil // nothing is there to be a checkout
	case err != nil:
		return "", fmt.Errorf("%w: the workspace could not be looked at: %v", ErrTaskBranchGit, err)
	case !info.IsDir():
		return "", nil
	}
	out, _, err := runTaskBranchGitDiag(ctx, workspace, 256, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	head := strings.TrimSpace(string(out))
	switch {
	case errors.Is(err, errTaskBranchTime):
		return "", ErrTaskBranchBusy
	case err == nil && len(head) >= 40 && isHex(head):
		return head, nil
	case err == nil:
		return "", fmt.Errorf("%w: HEAD answered something that is not a commit id", ErrTaskBranchGit)
	}
	if _, ran := gitExit(err); !ran {
		return "", fmt.Errorf("%w: %v", ErrTaskBranchGit, err) // git never ran: nothing it said
	}
	if proof := noHeadProof(ctx, workspace); proof != nil {
		return "", proof
	}
	return "", nil
}

// noHeadProof is nil when git says, in its own exact terms, that workspace
// has no HEAD commit to judge against, and the error to answer otherwise.
func noHeadProof(ctx context.Context, workspace string) error {
	out, diag, err := runTaskBranchGitDiag(ctx, workspace, 64, "rev-parse", "--is-inside-work-tree")
	code, ran := gitExit(err)
	switch {
	case errors.Is(err, errTaskBranchTime):
		return ErrTaskBranchBusy
	case err == nil && string(out) == "false\n":
		return unbornHead(ctx, workspace)
	case err == nil && string(out) == "true\n":
		return unbornHead(ctx, workspace)
	case ran && code == 128 && notARepository(string(diag)):
		return nil
	}
	return fmt.Errorf("%w: git could not say whether the workspace is a checkout: %v", ErrTaskBranchGit, err)
}

// unbornHead is nil when HEAD names a branch that has no commit yet - a
// repository nothing was committed to - and ErrTaskBranchGit when HEAD is
// detached at nothing or names a branch that exists and is still no commit.
func unbornHead(ctx context.Context, workspace string) error {
	out, _, err := runTaskBranchGitDiag(ctx, workspace, 512, "symbolic-ref", "--quiet", "HEAD")
	ref := strings.TrimSpace(string(out))
	switch {
	case errors.Is(err, errTaskBranchTime):
		return ErrTaskBranchBusy
	case err != nil || !strings.HasPrefix(ref, "refs/heads/") || strings.ContainsAny(ref, "\n "):
		return fmt.Errorf("%w: HEAD names no commit", ErrTaskBranchGit)
	}
	listed, _, err := runTaskBranchGitDiag(ctx, workspace, taskBranchBatchOutMax, "for-each-ref", "--format=%(refname)", ref)
	switch {
	case errors.Is(err, errTaskBranchTime):
		return ErrTaskBranchBusy
	case err != nil:
		return fmt.Errorf("%w: %v", ErrTaskBranchGit, err)
	}
	for _, line := range strings.Split(string(listed), "\n") {
		if line == ref {
			return fmt.Errorf("%w: HEAD's branch is not a commit", ErrTaskBranchGit)
		}
	}
	return nil
}

// notARepository is git's exact diagnostic, under LC_ALL=C, for a folder no
// repository contains - the plain one and the one that stopped at a
// filesystem boundary - and nothing else. Any other words are not proof.
func notARepository(diag string) bool {
	text := strings.TrimSuffix(diag, "\n")
	if text == "fatal: not a git repository (or any of the parent directories): .git" {
		return true
	}
	first, second, ok := strings.Cut(text, "\n")
	return ok && strings.HasPrefix(first, "fatal: not a git repository (or any parent up to mount point ") &&
		strings.HasSuffix(first, ")") && second == "Stopping at filesystem boundary (GIT_DISCOVERY_ACROSS_FILESYSTEM not set)."
}

// cursorStart is the index a cursor resumes at, refused unless it was issued
// for this very recorded list and this HEAD.
func cursorStart(cursor, generation, head string, recorded int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	held, err := decodeTaskBranchCursor(cursor)
	if err != nil || held.Generation != generation || held.Head != head || held.Offset >= recorded {
		return 0, ErrTaskBranchCursor
	}
	return held.Offset, nil
}

// scanTaskBranches examines names from start, a batch at a time, until the
// page holds limit rows, the names run out, or time does. It answers the
// index of the first name it did not examine, and whether it stopped early.
// An error is a git failure before anything was examined; one after progress
// ends the page there instead, so the next page asks again.
func scanTaskBranches(ctx context.Context, workspace, head string, names []namedBranch, start, limit int, page *TaskBranchPage) (at int, stopped bool, err error) {
	at = start
	for at < len(names) && len(page.Branches) < limit {
		if ctx.Err() != nil {
			return at, true, nil
		}
		batch := names[at:min(at+taskBranchBatch, len(names))]
		tips, err := unmergedTips(ctx, workspace, head, batch)
		switch {
		case errors.Is(err, errTaskBranchTime):
			return at, true, nil
		case err != nil && at == start:
			return at, false, fmt.Errorf("%w: %v", ErrTaskBranchGit, err)
		case err != nil:
			return at, true, nil
		}
		for _, named := range batch {
			tip, unmerged := tips[named.branch]
			if !unmerged {
				at++
				continue
			}
			fact, timely := branchFact(ctx, workspace, head, tip, named)
			if !timely && at > start {
				return at, true, nil // asked again, in full, by the next page
			}
			page.Branches = append(page.Branches, fact)
			at++
			if !timely {
				return at, true, nil
			}
			if len(page.Branches) >= limit {
				return at, false, nil
			}
		}
	}
	return at, false, nil
}

// unmergedTips is, of batch, the branches that exist and HEAD does not reach,
// with their tips, in one for-each-ref.
func unmergedTips(ctx context.Context, workspace, head string, batch []namedBranch) (map[string]string, error) {
	args := []string{"for-each-ref", "--format=%(objectname) %(refname)", "--no-merged=" + head}
	want := make(map[string]bool, len(batch))
	for _, named := range batch {
		ref := "refs/heads/" + named.branch
		want[ref] = true
		args = append(args, ref)
	}
	out, err := runTaskBranchGit(ctx, workspace, taskBranchBatchOutMax, args...)
	if err != nil {
		return nil, err
	}
	tips := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		oid, ref, ok := strings.Cut(line, " ")
		if ok && want[ref] && len(oid) >= 40 && isHex(oid) {
			tips[strings.TrimPrefix(ref, "refs/heads/")] = oid
		}
	}
	return tips, nil
}

// branchFact is one unmerged branch's row. timely is false when the request's
// time ran out before its base or counts were had; the row then carries what
// was known, and its counts are absent rather than zero.
func branchFact(ctx context.Context, workspace, head, tip string, named namedBranch) (fact TaskBranchFact, timely bool) {
	fact = TaskBranchFact{Name: named.branch, Task: named.task, Title: named.title, Head: tip}
	if len(fact.Title) > taskBranchTitleMax {
		cut := taskBranchTitleMax
		for cut > 0 && !utf8.RuneStart(fact.Title[cut]) {
			cut--
		}
		fact.Title, fact.TitleTruncated = fact.Title[:cut], true
	}
	out, err := runTaskBranchGit(ctx, workspace, 256, "merge-base", head, tip)
	if errors.Is(err, errTaskBranchTime) {
		return fact, false
	}
	base := strings.TrimSpace(string(out))
	if err != nil || len(base) < 40 || !isHex(base) {
		return fact, true // no merge base (exit 1), or git could not say
	}
	fact.Base = base
	files, added, removed, capped, err := countBranchDiff(ctx, workspace, base, tip)
	if errors.Is(err, errTaskBranchTime) {
		return fact, false
	}
	if err == nil {
		fact.Files, fact.Added, fact.Removed, fact.Capped, fact.CountsKnown = files, added, removed, capped, true
	}
	return fact, true
}

// errTaskBranchTime is a git call that did not answer before its time ran out.
var errTaskBranchTime = errors.New("git did not answer in time")

// errTaskBranchLong is a git call that said more than is read.
var errTaskBranchLong = errors.New("git said more than a branch listing reads")

// taskBranchCommand is the one door this file's git goes through. A variable
// only so a test can put a git that stalls behind it.
var taskBranchCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	// The words of git's diagnostics are read in one place (notARepository),
	// so they are asked for in one language.
	command.Env = append(command.Env, "LC_ALL=C")
	command.Env = append(command.Env, literalPaths...)
	return command
}

// runTaskBranchGit runs one git command under ctx and at most
// deliverGitCallMax, and answers its output when it exited cleanly within max
// bytes. A call that ran out of time is errTaskBranchTime, whatever else
// happened to it.
func runTaskBranchGit(ctx context.Context, dir string, max int, args ...string) ([]byte, error) {
	out, _, err := runTaskBranchGitDiag(ctx, dir, max, args...)
	return out, err
}

// cappedText keeps the first max bytes written to it and drops the rest.
type cappedText struct {
	data []byte
	max  int
}

func (c *cappedText) Write(p []byte) (int, error) {
	c.data = append(c.data, p[:min(len(p), max(c.max-len(c.data), 0))]...)
	return len(p), nil
}

// runTaskBranchGitDiag is [runTaskBranchGit] that also answers the first
// taskBranchDiagMax bytes git wrote to its error stream.
func runTaskBranchGitDiag(ctx context.Context, dir string, max int, args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	command := taskBranchCommand(ctx, dir, args...)
	diag := &cappedText{max: taskBranchDiagMax}
	command.Stderr = diag
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := command.Start(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, errTaskBranchTime // the time was spent before git could start
		}
		return nil, nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(out, int64(max)+1))
	_ = out.Close() // past max git sees a closed pipe and stops
	waitErr := command.Wait()
	switch {
	case ctx.Err() != nil:
		return nil, nil, errTaskBranchTime
	case len(data) > max:
		return nil, nil, errTaskBranchLong
	case readErr != nil:
		return nil, nil, readErr
	case waitErr != nil:
		return data, diag.data, waitErr
	}
	return data, diag.data, nil
}

// countBranchDiff is `git diff --numstat` of base..tip, read one record at a
// time and kept as three numbers. Past taskBranchNumstatMax files or
// taskBranchNumstatBytesMax bytes it stops reading, stops git, and says capped.
func countBranchDiff(ctx context.Context, workspace, base, tip string) (files, added, removed int, capped bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, deliverGitCallMax)
	defer cancel()
	command := taskBranchCommand(ctx, workspace, "diff", "--numstat", "-z", "--no-renames", base, tip, "--")
	out, err := command.StdoutPipe()
	if err != nil {
		return 0, 0, 0, false, err
	}
	if err := command.Start(); err != nil {
		if ctx.Err() != nil {
			return 0, 0, 0, false, errTaskBranchTime
		}
		return 0, 0, 0, false, err
	}
	tally, readErr := tallyNumstat(bufio.NewReaderSize(out, 64<<10))
	_ = out.Close()
	if tally.capped {
		cancel() // stopped on purpose: git is told to stop, and that is no failure
	}
	waitErr := command.Wait()
	switch {
	case tally.capped:
		return tally.files, tally.added, tally.removed, true, nil
	case ctx.Err() != nil:
		return 0, 0, 0, false, errTaskBranchTime
	case readErr != nil:
		return 0, 0, 0, false, readErr
	case waitErr != nil:
		return 0, 0, 0, false, waitErr
	}
	return tally.files, tally.added, tally.removed, false, nil
}

type numstatTally struct {
	files, added, removed int
	capped                bool
}

// tallyNumstat counts `--numstat -z` records ("<added>\t<removed>\t<path>",
// a binary file "-\t-") until the end or a bound. A record past either bound
// is one more than is read: capped, and only then, so a diff of exactly the
// bound is not. A record longer than the reader's buffer is counted once.
func tallyNumstat(reader *bufio.Reader) (tally numstatTally, err error) {
	read, partial := 0, false
	for {
		record, readErr := reader.ReadSlice(0)
		read += len(record)
		if len(record) > 0 && !partial {
			if tally.files >= taskBranchNumstatMax || read > taskBranchNumstatBytesMax {
				tally.capped = true
				return tally, nil
			}
			tally.count(record)
		}
		partial = errors.Is(readErr, bufio.ErrBufferFull)
		switch {
		case partial:
		case errors.Is(readErr, io.EOF):
			return tally, nil
		case readErr != nil:
			return tally, readErr
		}
	}
}

func (t *numstatTally) count(record []byte) {
	fields := strings.SplitN(string(record[:min(len(record), 64)]), "\t", 3)
	if len(fields) != 3 {
		return
	}
	t.files++
	if n, err := strconv.Atoi(fields[0]); err == nil {
		t.added += n
	}
	if n, err := strconv.Atoi(fields[1]); err == nil {
		t.removed += n
	}
}

// BranchFileOnUnmergedBranch says rel is a regular file at the tip of branch,
// an unmerged task branch of workspace.
func BranchFileOnUnmergedBranch(workspace, branch, rel string) bool {
	_, err := branchBlob(context.Background(), workspace, branch, rel)
	return err == nil
}

// branchBlob is the object id of the regular file rel at the tip of branch,
// an unmerged task branch. A link, a submodule or a folder at rel is not a file
// here, and rel is a path, never a pattern ([treeBlob]).
func branchBlob(ctx context.Context, workspace, branch, rel string) (string, error) {
	oid, verdict := branchBlobIn(ctx, nil, workspace, branch, rel)
	if verdict != branchUnmerged {
		return "", os.ErrNotExist
	}
	return oid, nil
}

// branchBlobIn is [branchBlob] through a request's cache, answering why when
// there is no blob: branchSettled is git's own "not on an unmerged branch",
// branchUnknown is that git could not say in time. The tip is pinned once and
// the entry is read FROM THAT TIP, so the two answers are of one commit.
func branchBlobIn(ctx context.Context, c *DeliverSourceCache, workspace, branch, rel string) (string, branchVerdict) {
	if !validTaskBranch(branch) || !validBranchRel(rel) {
		return "", branchSettled
	}
	tip := c.branchTipOf(ctx, workspace, branch)
	if tip.verdict != branchUnmerged {
		return "", tip.verdict
	}
	entry, present, known := c.entryAt(ctx, workspace, tip.tip, rel)
	switch {
	case !known:
		return "", branchUnknown
	case !present || !entry.regular():
		return "", branchSettled
	}
	return entry.oid, branchUnmerged
}

// validBranchRel is a relative path git can name with nothing to resolve: no
// empty, "." or ".." component, no leading slash or dash, no NUL.
func validBranchRel(rel string) bool {
	if rel == "" || len(rel) > 4096 || strings.HasPrefix(rel, "-") || strings.ContainsRune(rel, 0) {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// ReadBranchBlob reads at most max bytes of rel as it is at the tip of an
// unmerged task branch, with the blob's object id as its version. The bytes
// come from git's object store, never from a working directory, and they are
// the bytes OF THAT VERSION: the id is resolved once and the read is of it.
func ReadBranchBlob(workspace, branch, rel string, max int) (data []byte, truncated bool, version string, err error) {
	ctx := context.Background()
	oid, err := branchBlob(ctx, workspace, branch, rel)
	if err != nil {
		return nil, false, "", err
	}
	data, truncated, err = readBlob(ctx, workspace, oid, max)
	if err != nil {
		return nil, false, "", err
	}
	return data, truncated, "git:" + oid, nil
}

// BranchBlobVersion is the version a read of a branch-only file would answer:
// the blob's object id, or "" when the file is not on an unmerged task branch.
func BranchBlobVersion(workspace, branch, rel string) string {
	oid, err := branchBlob(context.Background(), workspace, branch, rel)
	if err != nil {
		return ""
	}
	return "git:" + oid
}
