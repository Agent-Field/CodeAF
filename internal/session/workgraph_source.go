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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// Why a conversation could not be read, in the words the graph reports.
const (
	WorkIssueMissing    = "missing"
	WorkIssueUnreadable = "unreadable"
	WorkIssueTooLarge   = "too_large"
	// WorkIssuePartial: part of the conversation is here and part was left out at
	// a bound; the facts that are here are true and the count is not the whole.
	WorkIssuePartial = "partial"
)

// WorkLimits are the bounds the source reads and remembers within. The zero
// value of a field takes its default; they are fields for the tests and for a
// process that wants other bounds, not settings a person sees.
type WorkLimits struct {
	// CacheBytes and CacheOwners bound the conversations remembered: an estimate
	// of their bytes and their count. A projection over half of CacheBytes is
	// served and not retained.
	CacheBytes  int64
	CacheOwners int
	// IndexBytes and IndexFiles bound the remembered check indexes the same way.
	IndexBytes int64
	IndexFiles int
	// FileMax is the largest checkpoint, and PlanMax the most bytes of plan store
	// (database and its journal) read for one conversation. Past either the
	// source does not read the file at all and says too_large.
	FileMax int64
	PlanMax int64
	// IndexMax is the most bytes of a check index read: its NEWEST bytes.
	IndexMax int64
	// Flights bounds the distinct conversation reads in flight at once, and
	// IndexFlights the check-index reads; past either a new read is refused with
	// [ErrWorkBusy]. RawBytes bounds the WEIGHT of the reads decoding at once
	// (their files' bytes, taken at the decode's own inflation): a read waits
	// for room, cancellably, and one heavier than the whole runs alone.
	Flights      int
	IndexFlights int
	RawBytes     int64
	// OwnerBytes is the most estimated bytes of facts one conversation yields.
	OwnerBytes int64
	// Facts is the most facts one conversation yields; Waits the most stored
	// waits one task yields. Past either the rest is left out and the
	// conversation says partial.
	Facts int
	Waits int
}

// THE AGGREGATE BUDGET. Everything the work graph holds on to or has in flight
// is bounded, in bytes, and the bounds add up to 128 MiB so that it is a number
// a laptop can be promised:
//
//	RETAINED
//	  conversations remembered      16 MiB   (this file)
//	  check indexes remembered       4 MiB   (this file)
//	  snapshots remembered          16 MiB   (cmd/codeaf, workgraph.Cache)
//	IN FLIGHT, each admitted by a gate that REFUSES what does not fit it
//	  files being read and decoded  28 MiB   (this file, at decode inflation)
//	  snapshots being built         64 MiB   (cmd/codeaf, at workgraph.BuildWeight)
//
// There is no "runs alone" exemption anywhere: a request whose files, facts or
// build would not fit a gate is refused with workgraph.TooLarge BEFORE the
// memory is spent. About ten thousand ordinary tasks fit (a 10 000-task build
// allocates ~42 MiB and is weighed at ~58 MiB); a scope much past that, or ten
// thousand tasks that each carry the longest text a node may, is asked to be
// narrowed. A snapshot too big to KEEP (over its 16 MiB share) is still built
// within the gate and served whole, and not remembered.
const (
	// WorkSnapshotBytes is the share the app's snapshot cache is built with,
	// written here so the whole budget is added up in one place. WorkBuildBytes
	// is the build gate.
	WorkSnapshotBytes   = 16 << 20
	WorkBuildBytes      = 64 << 20
	defaultCacheBytes   = 16 << 20
	defaultCacheOwners  = 4096
	defaultIndexBytes   = 4 << 20
	defaultIndexFiles   = 64
	defaultFileMax      = 7 << 20
	defaultPlanMax      = 7 << 20
	defaultIndexMax     = 4 << 20
	defaultFacts        = 20_000
	defaultOwnerBytes   = 8 << 20
	defaultWaits        = 1024
	defaultFlights      = 8
	defaultIndexFlights = 4
	defaultRawBytes     = 28 << 20
	// rawInflation is how many times its file's bytes a decode holds at peak:
	// the raw bytes and the decoded structs; the projected facts are bounded by
	// their own quota.
	rawInflation = 2
)

func (l WorkLimits) withDefaults() WorkLimits {
	set := func(v *int64, d int64) {
		if *v <= 0 {
			*v = d
		}
	}
	setN := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	set(&l.CacheBytes, defaultCacheBytes)
	setN(&l.CacheOwners, defaultCacheOwners)
	set(&l.IndexBytes, defaultIndexBytes)
	setN(&l.IndexFiles, defaultIndexFiles)
	set(&l.FileMax, defaultFileMax)
	set(&l.PlanMax, defaultPlanMax)
	set(&l.IndexMax, defaultIndexMax)
	set(&l.RawBytes, defaultRawBytes)
	set(&l.OwnerBytes, defaultOwnerBytes)
	setN(&l.Flights, defaultFlights)
	setN(&l.IndexFlights, defaultIndexFlights)
	setN(&l.Facts, defaultFacts)
	setN(&l.Waits, defaultWaits)
	return l
}

// ErrWorkOwnerMissing is the answer for a conversation folder that is not there.
var ErrWorkOwnerMissing = errors.New("the conversation's folder is not there")

// OwnerFacts is one conversation's tasks as generic facts, immutable once
// built, so any number of readers may hold it at once. Issue is non-empty when
// part of the conversation could not be read; the facts that could be are still
// here.
type OwnerFacts struct {
	Owner string
	Stamp string
	Facts []workgraph.Fact
	Issue string
	// Bytes is the estimate the cache budgets this projection at, and Weight what
	// building its facts into a snapshot allocates at peak ([workgraph.BuildWeight]).
	// Both are accumulated as the facts are added, against a quota.
	Bytes  int64
	Weight int64
	// Externals are the other conversations this one's tasks wait on, sorted: the
	// sources a target's state is read from, so a caller can name them in its
	// revision.
	Externals []string

	byID map[string]int32

	// hidden and sources are the document's dismissed set and each node's recorded
	// source by number, held only while one conversation is projected so a plan row
	// can be given its run's block; both are dropped before the projection is kept.
	hidden map[string]bool
	links  map[string]*TaskSource
}

// Find is the fact for a task token ("12" or "t-ab"), if the conversation has it.
func (o *OwnerFacts) Find(task string) (*workgraph.Fact, bool) {
	i, ok := o.byID[task]
	if !ok {
		return nil, false
	}
	return &o.Facts[i], true
}

// CheckIndex is the recorded check runs of a project's landed work, keyed by
// the conversation and task that earned them. Partial says the index file was
// larger than the bound and only its newest bytes were read: a task with no
// entry then has NO RECORDED CHECKS KNOWN, not none.
type CheckIndex struct {
	Stamp   string
	Partial bool
	Bytes   int64
	by      map[string]*workgraph.TaskChecks
}

// Of is the recorded checks of one task, or nil when the index has none.
func (c *CheckIndex) Of(owner, task string) *workgraph.TaskChecks {
	if c == nil {
		return nil
	}
	return c.by[owner+":"+task]
}

// WorkSource reads and caches conversations' task facts within [WorkLimits].
// The zero value is not usable; use [NewWorkSource].
type WorkSource struct {
	limits WorkLimits
	owners *byteLRU
	index  *byteLRU
	raw    *workgraph.Gate
	parses atomic.Int64
}

// NewWorkSource is an empty source with the default bounds.
func NewWorkSource() *WorkSource { return NewWorkSourceLimits(WorkLimits{}) }

// NewWorkSourceLimits is an empty source with the given bounds.
func NewWorkSourceLimits(l WorkLimits) *WorkSource {
	l = l.withDefaults()
	return &WorkSource{
		limits: l, raw: workgraph.NewGate(l.RawBytes),
		owners: newByteLRU(l.CacheBytes, l.CacheOwners, l.Flights), index: newByteLRU(l.IndexBytes, l.IndexFiles, l.IndexFlights),
	}
}

// Parses is how many conversation projections have been built from disk, for
// tests and diagnostics: an unchanged, still-remembered conversation never adds
// to it.
func (s *WorkSource) Parses() int64 { return s.parses.Load() }

// Held is what the source remembers right now: conversations and their
// estimated bytes, and check indexes and theirs.
func (s *WorkSource) Held() (owners int, ownerBytes int64, indexes int, indexBytes int64) {
	owners, ownerBytes = s.owners.held()
	indexes, indexBytes = s.index.held()
	return owners, ownerBytes, indexes, indexBytes
}

// Evictions is how many conversations were dropped for the budget, and how many
// were served without being retained for being over it.
func (s *WorkSource) Evictions() (evicted, unretained int64) {
	return s.owners.evicted.Load(), s.owners.unretained.Load()
}

// Coalesced is how many reads joined one already running.
func (s *WorkSource) Coalesced() int64 { return s.owners.coalesced.Load() + s.index.coalesced.Load() }

// plainFile reports whether path is a regular file that is not a link: the
// source never follows a link out of a conversation's folder.
func plainFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// plainDir is the same for the folder itself.
func plainDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// workSources are the files a conversation's tasks come from, in a fixed order.
func workSources(dir string) []string {
	db := filepath.Join(dir, planStoreFilename)
	files := []string{
		Place{Dir: dir}.Tasks(), filepath.Join(dir, taskDeletedFile), db, db + "-wal",
	}
	return append(files, planArchivePaths(db)...)
}

// Stamp names the sources of the conversation in dir as they are right now: stat
// calls only. The empty string is a folder that is not there, or is a link.
func (s *WorkSource) Stamp(dir string) string {
	if !plainDir(dir) {
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
// stamp answers from memory; a changed one is read once, by whoever asks first
// (the others wait for that read), outside every lock. A cancelled ctx stops
// the wait, and the read itself when nobody is waiting.
func (s *WorkSource) Owner(ctx context.Context, owner, dir string) (*OwnerFacts, error) {
	if !workgraph.ValidOwner(owner) {
		return nil, ErrWorkOwnerMissing
	}
	stamp := s.Stamp(dir)
	if stamp == "" {
		return nil, ErrWorkOwnerMissing
	}
	if v, ok := s.owners.get(dir, stamp); ok && v.(*OwnerFacts).Owner == owner {
		return v.(*OwnerFacts), nil
	}
	v, err := s.owners.do(ctx, dir+"\x00"+owner+"\x00"+stamp, func(rctx context.Context) (any, error) {
		held, err := s.raw.Acquire(rctx, s.rawWeight(dir))
		if err != nil {
			return nil, err
		}
		defer s.raw.Release(held)
		return s.readStable(rctx, owner, dir, stamp)
	})
	if err != nil {
		return nil, err
	}
	return v.(*OwnerFacts), nil
}

// rawWeight is what decoding the conversation's files holds at peak, from their
// sizes alone: the checkpoint and the plan stores with their journals, each at
// the inflation of its decode and never more than the bound the read itself
// refuses at.
func (s *WorkSource) rawWeight(dir string) int64 {
	tasks := min(wgSize(Place{Dir: dir}.Tasks()), s.limits.FileMax)
	db := filepath.Join(dir, planStoreFilename)
	var plan int64
	for _, path := range append(planArchivePaths(db), db) {
		plan += wgSize(path) + wgSize(path+"-wal")
	}
	return (tasks + min(plan, s.limits.PlanMax)) * rawInflation
}

// RawWeight is what reading the conversation in dir would hold at peak, so a
// caller can ask for room before it asks for the conversation.
func (s *WorkSource) RawWeight(dir string) int64 { return s.rawWeight(dir) }

// readStable reads a conversation and keeps it only if its files held still.
//
// A READ MUST NOT BE CACHED UNDER A STAMP THE READ ITSELF MOVED. Opening a WAL
// database to look at it can create its side files, so the stamp taken before
// the read is not the one the files settle at; the read is made again under the
// new one, and a result is kept only when the files held still across it. A
// conversation that is being written to as it is read is served from what was
// read and not remembered.
func (s *WorkSource) readStable(ctx context.Context, owner, dir, stamp string) (*OwnerFacts, error) {
	var facts *OwnerFacts
	for pass := 0; pass < 2; pass++ {
		var err error
		if facts, err = s.readOwner(ctx, owner, dir, stamp); err != nil {
			return nil, err
		}
		s.parses.Add(1)
		after := s.Stamp(dir)
		if after == stamp {
			s.owners.put(dir, stamp, facts, facts.Bytes)
			return facts, nil
		}
		stamp = after
	}
	return facts, nil
}

// Forget drops what is held for the conversation in dir.
func (s *WorkSource) Forget(dir string) { s.owners.forget(dir) }

// loopCheck is how many items a loop handles between looks at its context.
const loopCheck = 1024

// readOwner builds one conversation's projection from its files.
func (s *WorkSource) readOwner(ctx context.Context, owner, dir, stamp string) (*OwnerFacts, error) {
	out := &OwnerFacts{Owner: owner, Stamp: stamp}
	doc, issue := s.readTaskDocument(dir)
	out.Issue = issue
	if err := s.projectDocument(ctx, out, doc); err != nil {
		return nil, err
	}
	plan := s.readPlanFacts(ctx, owner, dir)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan.issue != "" {
		out.Issue = firstIssue(out.Issue, plan.issue)
	} else {
		// A PLAN ROW BELONGS TO THE BLOCK OF THE RUN ITS STORE IS ROOTED AT, read from
		// that run's own record by its exact id; a store whose root names no record
		// of the document has no known source. The hidden set is keyed by the row's
		// own wire token.
		parents := make(map[string]string, len(plan.facts))
		for _, fact := range plan.facts {
			parents[fact.Node.Ref.Task] = planParentToken(fact.Node)
		}
		for i := range plan.facts {
			node := &plan.facts[i].Node
			node.Dismissed = out.hidden[node.Ref.Task] && node.State == string(TaskFailed) && !node.Stopped
			node.Source = wgSource(planRowSource(node.Ref.Task,
				func(id string) string { return parents[id] },
				func(token string) (*TaskSource, bool) { source, ok := out.links[token]; return source, ok }))
		}
		out.add(s.limits, plan.facts)
	}
	out.hidden, out.links = nil, nil
	return out, s.seal(ctx, out)
}

// planParentToken is the plan token of a plan fact's parent, or "".
func planParentToken(n workgraph.GraphNode) string {
	_, parent, _ := strings.Cut(n.Parent, ":")
	return parent
}

// projectDocument projects the checkpoint's nodes and run rows, up to the
// per-conversation bound.
func (s *WorkSource) projectDocument(ctx context.Context, out *OwnerFacts, doc taskDocument) error {
	titles := wgRunTitles(doc.Runs)
	// THE HIDDEN SET IS THE DOCUMENT'S OWN, read with the rest of it, so the graph
	// and the app's list cannot disagree about what a restart brings back.
	hidden := make(map[string]bool, len(doc.Dismissed))
	for _, id := range doc.Dismissed {
		hidden[id] = true
	}
	out.hidden = hidden
	out.links = make(map[string]*TaskSource)
	for i, r := range doc.Nodes {
		if i%loopCheck == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		fact := nodeFact(out.Owner, r)
		fact.Node.Dismissed = hidden[strconv.FormatUint(r.ID, 10)] && r.State == TaskFailed && !r.App.Declined && r.Ending != TaskEndingStopped
		if r.PlanID != "" {
			out.links[planStoreID(planTaskID(r.PlanID))] = r.Source
		}
		out.add(s.limits, []workgraph.Fact{fact})
	}
	for i, r := range doc.Runs {
		if i%loopCheck == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		fact := runFact(out.Owner, r, titles)
		fact.Node.Dismissed = hidden[strconv.FormatUint(r.ID, 10)] && r.State == TaskFailed && !r.Stopped && r.Ending != TaskEndingStopped
		if r.PlanTask != "" {
			out.links[planStoreID(planTaskID(r.PlanTask))] = r.Source
		}
		out.add(s.limits, []workgraph.Fact{fact})
	}
	return nil
}

// add appends facts within the bounds, bounding each fact's waits too. What does
// not fit is left out and the conversation says partial: nothing is cut silently.
func (o *OwnerFacts) add(l WorkLimits, facts []workgraph.Fact) {
	for _, f := range facts {
		if len(o.Facts) >= l.Facts {
			o.Issue = firstIssue(o.Issue, WorkIssuePartial)
			return
		}
		if len(f.Waits) > l.Waits {
			f.Waits = f.Waits[:l.Waits]
			o.Issue = firstIssue(o.Issue, WorkIssuePartial)
		}
		// A BYTE QUOTA, NOT ONLY A COUNT: a thousand facts of the longest text are
		// more than ten thousand short ones. The fact that would pass the quota is
		// left out and the conversation says partial; what is kept is never cut.
		size := workgraph.FactBytes(&f)
		if o.Bytes+size > l.OwnerBytes {
			o.Issue = firstIssue(o.Issue, WorkIssuePartial)
			return
		}
		o.Bytes += size
		o.Weight += workgraph.BuildWeight(&f)
		o.Facts = append(o.Facts, f)
	}
}

// seal indexes the facts, names the conversations they wait on and prices the
// projection for the cache.
func (s *WorkSource) seal(ctx context.Context, out *OwnerFacts) error {
	out.byID = make(map[string]int32, len(out.Facts))
	others := map[string]bool{}
	for i := range out.Facts {
		if i%loopCheck == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		f := &out.Facts[i]
		out.byID[f.Node.Ref.Task] = int32(i)
		for _, w := range f.Waits {
			if w.On.Owner != out.Owner {
				others[w.On.Owner] = true
			}
		}
	}
	for other := range others {
		out.Externals = append(out.Externals, other)
	}
	sort.Strings(out.Externals)
	out.Bytes += int64(len(out.byID)) * 48
	return nil
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
// parse is an issue and no tasks, the way the engine drops it whole. A link, or
// a file over the bound, is never opened: it is unreadable, or too_large, from
// its stat alone.
func (s *WorkSource) readTaskDocument(dir string) (taskDocument, string) {
	file := Place{Dir: dir}.Tasks()
	info, err := os.Lstat(file)
	switch {
	case os.IsNotExist(err):
		return taskDocument{}, ""
	case err != nil, !info.Mode().IsRegular():
		return taskDocument{}, WorkIssueUnreadable
	case info.Size() > s.limits.FileMax:
		return taskDocument{}, WorkIssueTooLarge
	}
	content, err := readBounded(file, s.limits.FileMax)
	if err != nil {
		return taskDocument{}, WorkIssueUnreadable
	}
	doc, err := decodeTasks(content)
	if err != nil {
		return taskDocument{}, WorkIssueUnreadable
	}
	deleted := filepath.Join(dir, taskDeletedFile)
	if _, err := os.Lstat(deleted); err == nil && !plainFile(deleted) {
		return taskDocument{}, WorkIssueUnreadable
	}
	return filterDeletedDocument(doc, taskDeletions(filepath.Join(dir, placeTranscript))), ""
}

// readBounded reads a file and never more than limit bytes of it: a file that
// grew between the stat and the read is refused, not read whole.
func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("the file grew past its bound")
	}
	return content, nil
}

// planRead is what the conversation's plan stores gave.
type planRead struct {
	facts []workgraph.Fact
	issue string
}

// readPlanFacts reads every plan store of the conversation, ended runs first,
// each through a read-only connection and only the rows this conversation's
// own tag names. A store that is a link, is over the byte bound together with
// its journal, or will not open is an issue, and the conversation then says so
// rather than showing a plan with a hole in it.
func (s *WorkSource) readPlanFacts(ctx context.Context, owner, dir string) planRead {
	db := filepath.Join(dir, planStoreFilename)
	var out planRead
	var bytes int64
	for _, path := range append(planArchivePaths(db), db) {
		if ctx.Err() != nil {
			return out
		}
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		bytes += info.Size()
		if wal, err := os.Lstat(path + "-wal"); err == nil {
			bytes += wal.Size()
		}
		switch {
		case !info.Mode().IsRegular():
			return planRead{issue: WorkIssueUnreadable}
		case bytes > s.limits.PlanMax:
			return planRead{issue: WorkIssueTooLarge}
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

// Checks is the project's check index at path, parsed once per change and
// remembered within the budget. A missing index is an empty one; a link is not
// followed.
func (s *WorkSource) Checks(ctx context.Context, path string) (*CheckIndex, error) {
	stamp := wgStamp(path)
	if v, ok := s.index.get(path, stamp); ok {
		return v.(*CheckIndex), nil
	}
	v, err := s.index.do(ctx, path+"\x00"+stamp, func(rctx context.Context) (any, error) {
		held, err := s.raw.Acquire(rctx, min(s.limits.IndexMax, max(1, wgSize(path))))
		if err != nil {
			return nil, err
		}
		defer s.raw.Release(held)
		index, err := readCheckIndex(rctx, path, stamp, s.limits.IndexMax)
		if err != nil {
			return nil, err
		}
		s.index.put(path, stamp, index, index.Bytes)
		return index, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*CheckIndex), nil
}

// wgSize is a plain file's size, and zero for anything else.
func wgSize(path string) int64 {
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		return info.Size()
	}
	return 0
}

// readCheckIndex streams the NEWEST `limit` bytes of the index and keeps only
// the rows whose verdict rested on running the declared checks, so its memory
// is the checks and not the file. Older bytes are not read and the index says
// partial.
func readCheckIndex(ctx context.Context, path, stamp string, limit int64) (*CheckIndex, error) {
	out := &CheckIndex{Stamp: stamp, by: map[string]*workgraph.TaskChecks{}}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("the index is not a plain file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	skipFirst := false
	if info.Size() > limit {
		out.Partial, skipFirst = true, true
		if _, err := file.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return nil, err
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(file, limit))
	scanner.Buffer(make([]byte, 0, 4<<10), 256<<10)
	for n := 0; scanner.Scan(); n++ {
		if n%loopCheck == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if skipFirst {
			// the first line of a tail read is a fragment
			skipFirst = false
			continue
		}
		wgIndexRow(out, scanner.Bytes())
	}
	return out, nil
}

// wgIndexRow records one index line's check runs, if its verdict rested on any.
func wgIndexRow(out *CheckIndex, line []byte) {
	var row struct {
		ID           string               `json:"id"`
		SessionID    string               `json:"sessionId"`
		VerdictBasis *plandb.VerdictBasis `json:"verdictBasis"`
	}
	if json.Unmarshal(line, &row) != nil || row.SessionID == "" || !workgraph.ValidOwner(row.SessionID) {
		return
	}
	if checks := wgChecks(row.VerdictBasis); checks != nil {
		key := row.SessionID + ":" + row.ID
		out.by[key] = checks
		out.Bytes += int64(len(key)) + 96
	}
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
