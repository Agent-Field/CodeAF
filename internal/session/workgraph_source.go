package session

// THE SOURCE: PERSISTED TASK FACTS, READ ONCE PER CHANGE.
//
// [WorkSource] answers "what are the tasks of this conversation" from the
// files the conversation keeps, WITHOUT TAKING ANY LOCK THE WORK HOLDS and
// without asking a live agent anything: it stats and reads files. A reader of
// ten thousand tasks therefore costs the running work nothing, and a closed
// conversation answers exactly as an open one does.
//
// ── CACHED BY WHAT THE FILES ARE, NOT BY WHEN THEY WERE ASKED ABOUT ──
//
// Each conversation's projection is cached under the stamp of its sources —
// size, modification time and inode of tasks.json, the plan stores and the
// deletion list. [WorkSource.Stamp] is stat calls and nothing else, so a caller
// can ask "has anything moved" for a thousand conversations without reading a
// byte of any of them; an unchanged stamp is never reparsed (the checkpoint is
// written atomically by rename, which changes the inode, so a rewrite with the
// same size and second is still seen). The project's task index, which only the
// checks of landed work come from, is cached the same way under its own stamp
// and is NOT part of any conversation's, so a landing elsewhere in the project
// never reparses a conversation that did not change.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// Why a conversation could not be read, in the words the graph reports.
const (
	WorkIssueMissing    = "missing"
	WorkIssueUnreadable = "unreadable"
	WorkIssueTooLarge   = "too_large"
)

// workFileMax is the largest checkpoint the source will parse. Past it the
// conversation is reported too_large rather than read whole.
const workFileMax = 256 << 20

// ErrWorkOwnerMissing is the answer for a conversation folder that is not there.
var ErrWorkOwnerMissing = errors.New("the conversation's folder is not there")

// OwnerFacts is one conversation's tasks as generic facts, immutable once
// built. Issue is non-empty when part of the conversation could not be read; the
// facts that could be are still here.
type OwnerFacts struct {
	Owner string
	Stamp string
	Facts []workgraph.Fact
	Issue string
	// Externals are the other conversations this one's tasks wait on, sorted: the
	// sources a target's state is read from, so a caller can name them in its
	// revision.
	Externals []string

	byID map[string]int32
}

// Find is the fact for a task token ("12" or "t-ab"), if the conversation has it.
func (o *OwnerFacts) Find(task string) (*workgraph.Fact, bool) {
	i, ok := o.byID[task]
	if !ok {
		return nil, false
	}
	return &o.Facts[i], true
}

type ownerEntry struct {
	stamp string
	facts *OwnerFacts
}

// CheckIndex is the recorded check runs of a project's landed work, keyed by
// the conversation and task that earned them.
type CheckIndex struct {
	Stamp string
	by    map[string]*workgraph.TaskChecks
}

// Of is the recorded checks of one task, or nil when the index has none.
func (c *CheckIndex) Of(owner, task string) *workgraph.TaskChecks {
	if c == nil {
		return nil
	}
	return c.by[owner+":"+task]
}

type indexEntry struct {
	stamp string
	index *CheckIndex
}

// WorkSource reads and caches conversations' task facts. The zero value is not
// usable; use [NewWorkSource].
type WorkSource struct {
	mu     sync.Mutex
	owners map[string]*ownerEntry
	index  map[string]*indexEntry
	parses atomic.Int64
}

// NewWorkSource is an empty source.
func NewWorkSource() *WorkSource {
	return &WorkSource{owners: map[string]*ownerEntry{}, index: map[string]*indexEntry{}}
}

// Parses is how many conversation projections have been built from disk, for
// tests and diagnostics: an unchanged conversation never adds to it.
func (s *WorkSource) Parses() int64 { return s.parses.Load() }

// workSources are the files a conversation's tasks come from, in a fixed order.
func workSources(dir string) []string {
	db := filepath.Join(dir, planStoreFilename)
	files := []string{
		Place{Dir: dir}.Tasks(), filepath.Join(dir, taskDeletedFile), db, db + "-wal",
	}
	return append(files, planArchivePaths(db)...)
}

// Stamp names the sources of the conversation in dir as they are right now: stat
// calls only. The empty string is a folder that is not there.
func (s *WorkSource) Stamp(dir string) string {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	var b strings.Builder
	for _, file := range workSources(dir) {
		b.WriteString(wgStamp(file))
		b.WriteByte(';')
	}
	return b.String()
}

// Owner is the facts of the conversation owner, whose folder is dir. The same
// stamp answers from memory; a changed one is read once, by whoever asks first,
// and the others get that answer. A cancelled ctx stops the read between its
// files and caches nothing.
func (s *WorkSource) Owner(ctx context.Context, owner, dir string) (*OwnerFacts, error) {
	if !workgraph.ValidOwner(owner) {
		return nil, ErrWorkOwnerMissing
	}
	stamp := s.Stamp(dir)
	if stamp == "" {
		return nil, ErrWorkOwnerMissing
	}
	s.mu.Lock()
	if e := s.owners[dir]; e != nil && e.stamp == stamp && e.facts.Owner == owner {
		facts := e.facts
		s.mu.Unlock()
		return facts, nil
	}
	s.mu.Unlock()
	// A READ MUST NOT BE CACHED UNDER A STAMP THE READ ITSELF MOVED. Opening a
	// WAL database to look at it can create its side files, so the stamp taken
	// before the read is not the one the files settle at; the read is made again
	// under the new one, and a result is kept only when the files held still
	// across it. A conversation that is being written to as it is read is served
	// from what was read and not remembered.
	var facts *OwnerFacts
	for pass := 0; pass < 2; pass++ {
		var err error
		if facts, err = readOwner(ctx, owner, dir, stamp); err != nil {
			return nil, err
		}
		s.parses.Add(1)
		if after := s.Stamp(dir); after == stamp {
			s.mu.Lock()
			s.owners[dir] = &ownerEntry{stamp: stamp, facts: facts}
			s.mu.Unlock()
			break
		} else {
			stamp = after
		}
	}
	return facts, nil
}

// Forget drops what is held for the conversation in dir.
func (s *WorkSource) Forget(dir string) {
	s.mu.Lock()
	delete(s.owners, dir)
	s.mu.Unlock()
}

// readOwner builds one conversation's projection from its files.
func readOwner(ctx context.Context, owner, dir, stamp string) (*OwnerFacts, error) {
	out := &OwnerFacts{Owner: owner, Stamp: stamp}
	doc, issue := readTaskDocument(dir)
	out.Issue = issue
	titles := wgRunTitles(doc.Runs)
	for _, r := range doc.Nodes {
		out.Facts = append(out.Facts, nodeFact(owner, r))
	}
	for _, r := range doc.Runs {
		out.Facts = append(out.Facts, runFact(owner, r, titles))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan := readPlanFacts(ctx, owner, dir); plan.issue != "" {
		out.Issue = firstIssue(out.Issue, plan.issue)
	} else {
		out.Facts = append(out.Facts, plan.facts...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out.byID = make(map[string]int32, len(out.Facts))
	others := map[string]bool{}
	for i := range out.Facts {
		out.byID[out.Facts[i].Node.Ref.Task] = int32(i)
		for _, w := range out.Facts[i].Waits {
			if w.On.Owner != owner {
				others[w.On.Owner] = true
			}
		}
	}
	for other := range others {
		out.Externals = append(out.Externals, other)
	}
	sort.Strings(out.Externals)
	return out, nil
}

// FileStamp names a file's present state (size, modification instant, inode), "-"
// when it is not there: the identity a caller caches a read of the file under.
func FileStamp(path string) string { return wgStamp(path) }

func firstIssue(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// readTaskDocument is the conversation's tasks.json exactly as the engine
// itself would load it (validated, with the person's deletions applied). A
// folder with no checkpoint has no tasks and no issue; a checkpoint that will not
// parse is an issue and no tasks, the way the engine drops it whole.
func readTaskDocument(dir string) (taskDocument, string) {
	file := Place{Dir: dir}.Tasks()
	info, err := os.Stat(file)
	switch {
	case os.IsNotExist(err):
		return taskDocument{}, ""
	case err != nil:
		return taskDocument{}, WorkIssueUnreadable
	case info.Size() > workFileMax:
		return taskDocument{}, WorkIssueTooLarge
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return taskDocument{}, WorkIssueUnreadable
	}
	doc, err := decodeTasks(content)
	if err != nil {
		return taskDocument{}, WorkIssueUnreadable
	}
	return filterDeletedDocument(doc, taskDeletions(filepath.Join(dir, placeTranscript))), ""
}

// planRead is what the conversation's plan stores gave.
type planRead struct {
	facts []workgraph.Fact
	issue string
}

// readPlanFacts reads every plan store of the conversation, ended runs first,
// each through a read-only connection and only the rows this conversation's
// own tag names. A store that will not open is an issue, and the conversation
// then says so rather than showing a plan with a hole in it.
func readPlanFacts(ctx context.Context, owner, dir string) planRead {
	db := filepath.Join(dir, planStoreFilename)
	var out planRead
	paths := append(planArchivePaths(db), db)
	for _, path := range paths {
		if ctx.Err() != nil {
			return out
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		facts, err := planStoreFacts(owner, path)
		if err != nil {
			return planRead{issue: WorkIssueUnreadable}
		}
		out.facts = append(out.facts, facts...)
	}
	return out
}

// planStoreFacts is one store's rows for the owner.
func planStoreFacts(owner, path string) ([]workgraph.Fact, error) {
	read, err := plandb.ReadTasks(path)
	if err != nil {
		return nil, err
	}
	spend, usage := planSpendByTask(path), planUsageByTask(path)
	var group *workgraph.GroupRef
	for _, t := range read.Tasks {
		if t.ID == read.RootID {
			group = &workgraph.GroupRef{ID: owner + ":" + wgPlanToken(t.ID), Title: wgTitle(t.Title)}
		}
	}
	var out []workgraph.Fact
	for _, t := range read.Tasks {
		if t.Chat == owner {
			out = append(out, planFact(owner, t, group, spend, usage))
		}
	}
	return out, nil
}

// Checks is the project's check index at path, parsed once per change. A missing
// index is an empty one.
func (s *WorkSource) Checks(ctx context.Context, path string) (*CheckIndex, error) {
	stamp := wgStamp(path)
	s.mu.Lock()
	if e := s.index[path]; e != nil && e.stamp == stamp {
		index := e.index
		s.mu.Unlock()
		return index, nil
	}
	s.mu.Unlock()
	index, err := readCheckIndex(ctx, path, stamp)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.index[path] = &indexEntry{stamp: stamp, index: index}
	s.mu.Unlock()
	return index, nil
}

// readCheckIndex streams the index and keeps only the rows whose verdict rested
// on running the declared checks, so its memory is the checks and not the file.
func readCheckIndex(ctx context.Context, path, stamp string) (*CheckIndex, error) {
	out := &CheckIndex{Stamp: stamp, by: map[string]*workgraph.TaskChecks{}}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4<<10), 256<<10)
	for n := 0; scanner.Scan(); n++ {
		if n%1024 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var row struct {
			ID           string               `json:"id"`
			SessionID    string               `json:"sessionId"`
			VerdictBasis *plandb.VerdictBasis `json:"verdictBasis"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.SessionID == "" {
			continue
		}
		if checks := wgChecks(row.VerdictBasis); checks != nil {
			out.by[row.SessionID+":"+row.ID] = checks
		}
	}
	return out, nil
}

// ExternalReader is the [ExternalTasks] reader a process supplies to its
// conversations, over this source: locate names the folder of a conversation id
// (and false for one it cannot find). It answers from the same cached
// projections the graph uses, so asking is a stat and, at most, one parse of a
// conversation that moved.
func (s *WorkSource) ExternalReader(locate func(owner string) (string, bool)) ExternalTasks {
	return func(ctx context.Context, r TaskRef) (ExternalTask, error) {
		dir, ok := locate(r.Session)
		if !ok {
			return ExternalTask{}, fmt.Errorf("no conversation %s", r.Session)
		}
		facts, err := s.Owner(ctx, r.Session, dir)
		if err != nil {
			return ExternalTask{}, err
		}
		fact, ok := facts.Find(strconv.FormatUint(r.ID, 10))
		if !ok {
			if facts.Issue != "" {
				return ExternalTask{}, fmt.Errorf("conversation %s is %s", r.Session, facts.Issue)
			}
			return ExternalTask{}, nil
		}
		return ExternalTask{Exists: true, State: TaskState(fact.Node.State), Waits: waitsOf(r.Session, fact)}, nil
	}
}

// waitsOf is a fact's stored waits as task references, for the loop check.
func waitsOf(session string, fact *workgraph.Fact) []TaskRef {
	var out []TaskRef
	for _, w := range fact.Waits {
		id, err := strconv.ParseUint(w.On.Task, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, TaskRef{Session: w.On.Owner, ID: id})
	}
	return out
}
