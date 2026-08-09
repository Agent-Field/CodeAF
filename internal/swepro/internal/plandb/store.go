// In-memory PlanDB store — port of src/plandb/store.ts.
//
// The TS original's only atomicity mechanism is the JS event loop: every
// public method is one synchronous critical section. Go gets real
// concurrency, so a single store-wide mutex serializes every public method;
// internal cross-calls use the unexported *Locked variants to keep the lock
// non-reentrant. The OBSERVABLE contract (e.g. ClaimTask returns nil unless
// status is exactly "ready") is unchanged.
//
// Fidelity notes (deliberate, do not "fix"):
//   - storeNow() packs Date.now()*4096 + per-ms counter, exactly like the TS
//     now() — stale-reaper.ts decodes this packing, and listTasks sort order
//     depends on it.
//   - genID consumes one RNG draw per character in the same order as TS, so a
//     seeded differential run stays aligned. Generated-id collisions silently
//     OVERWRITE the earlier task (JS Map.set), matching TS.
//   - All maps preserve insertion order (jscompat.OrderedMap); promoteReady is
//     a single pass in insertion order — a task promoted early in the pass can
//     satisfy a later task's dep within the SAME pass.
//   - criticalPath has NO cycle guard, like TS. A dependency cycle crashes
//     both implementations (TS: stack overflow; Go: goroutine stack fatal).
package plandb

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ── typed coalesce error ─────────────────────────────────────────────────

type CoalesceErrorCode string

const (
	CoalesceTooFew             CoalesceErrorCode = "too-few"
	CoalesceDuplicateID        CoalesceErrorCode = "duplicate-id"
	CoalesceNotFound           CoalesceErrorCode = "not-found"
	CoalesceMixedParents       CoalesceErrorCode = "mixed-parents"
	CoalesceNotMergeableStatus CoalesceErrorCode = "not-mergeable-status"
)

type CoalesceError struct {
	Code CoalesceErrorCode
	msg  string
}

func (e *CoalesceError) Error() string { return e.msg }

func newCoalesceError(code CoalesceErrorCode, msg string) *CoalesceError {
	return &CoalesceError{Code: code, msg: msg}
}

var coalesceOKStatuses = map[TaskStatus]bool{StatusPending: true, StatusReady: true}

// ── merged-description builders (exported, pure) ─────────────────────────

type CoalesceMember struct {
	ID          TaskID
	Title       string
	Description *string
}

// BuildCoalescedDescription mirrors buildCoalescedDescription — the exact
// format (header line, blank-line separators, "## <id> — <title>" with an
// em-dash U+2014, trimmed bodies) is a public contract: the scheduler builds
// the identical string to pre-estimate the merged band before committing.
func BuildCoalescedDescription(members []CoalesceMember, fileScope []string) string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	header := fmt.Sprintf("Coalesced from %d sibling tasks: %s", len(members), strings.Join(ids, ", "))
	blocks := []string{header}
	if len(fileScope) > 0 {
		blocks = append(blocks, "file_scope: "+strings.Join(fileScope, ", "))
	}
	for _, m := range members {
		body := ""
		if m.Description != nil {
			body = jscompat.Trim(*m.Description)
		}
		block := fmt.Sprintf("## %s — %s", m.ID, m.Title)
		if body != "" {
			block += "\n" + body
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

func BuildCoalescedTitle(titles []string) string {
	return strings.Join(titles, " + ")
}

// ── id + monotonic-time sources (package-global, like the TS module) ─────

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

var (
	timeMu      sync.Mutex
	monoCounter int64
	lastMs      int64
	nowMillis   = func() int64 { return time.Now().UnixMilli() }
	randFloat   = rand.Float64
)

// storeNow mirrors now(): ms*4096 + 12-bit per-ms counter, strictly
// increasing over the process lifetime.
func storeNow() int64 {
	timeMu.Lock()
	defer timeMu.Unlock()
	ms := nowMillis()
	if ms == lastMs {
		monoCounter++
	} else {
		lastMs = ms
		monoCounter = 0
	}
	return ms*4096 + (monoCounter & 0xfff)
}

func genID(prefix string, length int) string {
	out := prefix + "-"
	for i := 0; i < length; i++ {
		out += string(idAlphabet[int(math.Floor(randFloat()*float64(len(idAlphabet))))])
	}
	return out
}

// SetRandForTesting pins the id RNG (e.g. to jscompat.Mulberry32(seed)) so a
// TS↔Go differential harness generates identical ids. Returns a restore func.
func SetRandForTesting(f func() float64) func() {
	prev := randFloat
	randFloat = f
	return func() { randFloat = prev }
}

// SetClockForTesting pins the millisecond clock. Returns a restore func.
func SetClockForTesting(f func() int64) func() {
	timeMu.Lock()
	prev := nowMillis
	nowMillis = f
	lastMs = 0
	monoCounter = 0
	timeMu.Unlock()
	return func() {
		timeMu.Lock()
		nowMillis = prev
		lastMs = 0
		monoCounter = 0
		timeMu.Unlock()
	}
}

var closedStatuses = map[TaskStatus]bool{
	StatusDone: true, StatusDonePartial: true, StatusFailed: true, StatusCancelled: true,
}

// depRank mirrors DEP_RANK; an unknown/empty kind ranks 0 (so any known kind
// beats it), matching JS `DEP_RANK[b] > DEP_RANK[a]` with undefined coercion.
var depRank = map[DepKind]int{DepFeedsInto: 2, DepBlocks: 2, DepSuggests: 1}

func strongestDep(a DepKind, aSet bool, b DepKind) DepKind {
	if !aSet {
		return b
	}
	// JS: `DEP_RANK[b] > DEP_RANK[a] ? b : a`. Any comparison involving a
	// missing rank (undefined) is false, so `a` wins unless BOTH ranks exist
	// and b's is strictly greater.
	rb, hasB := depRank[b]
	ra, hasA := depRank[a]
	if hasB && hasA && rb > ra {
		return b
	}
	return a
}

// ── journal hook ─────────────────────────────────────────────────────────

// Journal mirrors PlanDBJournal: a durable write-through hook receiving FULL
// resulting rows. Implementations MUST be fail-open — a journal write error
// must never propagate into a store mutation. All methods are invoked while
// the store lock is held.
type Journal interface {
	PutTask(task *Task)
	PutProject(project *Project)
	PutContext(entry *ContextEntry)
	AddDep(from, to TaskID, kind DepKind)
	RemoveDep(from, to TaskID)
	Reset()
}

// ── the store ────────────────────────────────────────────────────────────

type PlanDB struct {
	mu            sync.Mutex
	projects      *jscompat.OrderedMap[ProjectID, *Project]
	tasks         *jscompat.OrderedMap[TaskID, *Task]
	depsFrom      *jscompat.OrderedMap[TaskID, *jscompat.OrderedMap[TaskID, DepKind]]
	depsTo        *jscompat.OrderedMap[TaskID, *jscompat.OrderedMap[TaskID, DepKind]]
	contexts      []*ContextEntry
	projectByName *jscompat.OrderedMap[string, ProjectID]
	journal       Journal
}

func NewPlanDB() *PlanDB {
	return &PlanDB{
		projects:      jscompat.NewOrderedMap[ProjectID, *Project](),
		tasks:         jscompat.NewOrderedMap[TaskID, *Task](),
		depsFrom:      jscompat.NewOrderedMap[TaskID, *jscompat.OrderedMap[TaskID, DepKind]](),
		depsTo:        jscompat.NewOrderedMap[TaskID, *jscompat.OrderedMap[TaskID, DepKind]](),
		projectByName: jscompat.NewOrderedMap[string, ProjectID](),
	}
}

// ── persistence hook ─────────────────────────────────────────────────────

func (db *PlanDB) AttachJournal(j Journal) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.journal = j
}

func (db *PlanDB) HasJournal() bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.journal != nil
}

func (db *PlanDB) setTask(task *Task) {
	db.tasks.Set(task.ID, task)
	if db.journal != nil {
		db.journal.PutTask(task)
	}
}

func (db *PlanDB) setProject(project *Project) {
	db.projects.Set(project.ID, project)
	if db.journal != nil {
		db.journal.PutProject(project)
	}
}

// Restore mirrors _restore: raw-state load that bypasses the journal and id
// generation, rebuilding every internal index from the flat rows.
func (db *PlanDB) Restore(state Snapshot) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.projects.Clear()
	db.tasks.Clear()
	db.depsFrom.Clear()
	db.depsTo.Clear()
	db.contexts = db.contexts[:0]
	db.projectByName.Clear()
	for _, p := range state.Projects {
		db.projects.Set(p.ID, p)
		db.projectByName.Set(p.Name, p.ID)
	}
	for _, t := range state.Tasks {
		db.tasks.Set(t.ID, t)
	}
	for _, c := range state.Contexts {
		db.contexts = append(db.contexts, c)
	}
	for _, d := range state.Dependencies {
		db.linkDep(d.FromTask, d.ToTask, d.Kind)
	}
}

// ── projects ─────────────────────────────────────────────────────────────

func (db *PlanDB) Init(name string, description ...string) *Project {
	db.mu.Lock()
	defer db.mu.Unlock()
	var desc *string
	if len(description) > 0 {
		desc = &description[0]
	}
	return db.initLocked(name, desc)
}

func (db *PlanDB) initLocked(name string, description *string) *Project {
	if existing, ok := db.projectByName.Get(name); ok {
		p, _ := db.projects.Get(existing)
		return p
	}
	pid := genID("p", 4)
	project := &Project{
		ID:          pid,
		Name:        name,
		Description: description,
		Status:      "active",
		Metadata:    nil,
		CreatedAt:   storeNow(),
		UpdatedAt:   storeNow(),
	}
	db.setProject(project)
	db.projectByName.Set(name, pid)
	return project
}

func (db *PlanDB) DefaultProject() *Project {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.defaultProjectLocked()
}

func (db *PlanDB) defaultProjectLocked() *Project {
	if db.projects.Len() == 0 {
		return db.initLocked("codeaf", nil)
	}
	p, _ := db.projects.First()
	return p
}

func (db *PlanDB) ResolveProject(nameOrID string) *Project {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.resolveProjectLocked(nameOrID)
}

func (db *PlanDB) resolveProjectLocked(nameOrID string) *Project {
	if nameOrID == "" {
		return db.defaultProjectLocked()
	}
	if byID, ok := db.projects.Get(nameOrID); ok {
		return byID
	}
	if byNameID, ok := db.projectByName.Get(nameOrID); ok {
		p, _ := db.projects.Get(byNameID)
		return p
	}
	// Auto-create on reference (mirrors CLI behavior on first add).
	return db.initLocked(nameOrID, nil)
}

func (db *PlanDB) ListProjects() []*Project {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.projects.Values()
}

// ── tasks ────────────────────────────────────────────────────────────────

func (db *PlanDB) AddTask(input AddTaskInput) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.addTaskLocked(input)
}

func (db *PlanDB) addTaskLocked(input AddTaskInput) (*Task, error) {
	project := db.resolveProjectLocked(input.Project)
	tid := input.CustomID
	if tid == "" {
		tid = genID("t", 4)
	}
	if input.CustomID != "" && db.tasks.Has(input.CustomID) {
		return nil, fmt.Errorf("task id already exists: %s", input.CustomID)
	}
	description := encodePolicy(input)
	tags := composeTags(input)
	var parentID *TaskID
	if input.Parent != "" {
		parentID = &input.Parent
	}

	if parentID != nil && !db.tasks.Has(*parentID) {
		return nil, fmt.Errorf("parent task not found: %s", *parentID)
	}
	if parentID != nil {
		parent, _ := db.tasks.Get(*parentID)
		// Promote parent to composite if it has at least one child.
		if !parent.IsComposite {
			next := *parent
			next.IsComposite = true
			next.UpdatedAt = storeNow()
			db.setTask(&next)
		}
	}

	priority := 0.0
	if input.Priority != nil {
		priority = *input.Priority
	}
	task := &Task{
		ID:           tid,
		ProjectID:    project.ID,
		ParentTaskID: parentID,
		IsComposite:  false,
		Title:        input.Title,
		Description:  &description,
		Status:       StatusPending,
		Kind:         kindOrDefault(input.Kind),
		Priority:     jscompat.JSNumber(priority),
		AgentID:      nil,
		ClaimedAt:    nil,
		StartedAt:    nil,
		CompletedAt:  nil,
		Result:       nil,
		Error:        nil,
		Files:        nil,
		Metadata:     nil,
		Tags:         tags,
		CreatedAt:    storeNow(),
		UpdatedAt:    storeNow(),
	}
	db.setTask(task)

	for _, dep := range input.Deps {
		if db.tasks.Has(dep.TaskID) {
			kind := DepFeedsInto
			if dep.Kind != nil {
				kind = *dep.Kind
			}
			db.addDepLocked(dep.TaskID, tid, kind)
		}
	}

	// If task has no incomplete deps, promote to ready.
	db.recomputeStatus(tid)
	got, _ := db.tasks.Get(tid)
	return got, nil
}

func kindOrDefault(k TaskKind) TaskKind {
	if k == "" {
		return "generic"
	}
	return k
}

func (db *PlanDB) GetTask(tid TaskID) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	t, _ := db.tasks.Get(tid)
	return t
}

type ListTasksFilter struct {
	Project string
	Status  string
	Kind    string
	Tag     string
	Parent  TaskID
}

func (db *PlanDB) ListTasks(filter *ListTasksFilter) []*Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.listTasksLocked(filter)
}

func (db *PlanDB) listTasksLocked(filter *ListTasksFilter) []*Task {
	var project *Project
	if filter != nil && filter.Project != "" {
		project = db.resolveProjectLocked(filter.Project)
	}
	out := []*Task{}
	for _, task := range db.tasks.Values() {
		if project != nil && task.ProjectID != project.ID {
			continue
		}
		if filter != nil && filter.Status != "" && string(task.Status) != filter.Status {
			continue
		}
		if filter != nil && filter.Kind != "" && string(task.Kind) != filter.Kind {
			continue
		}
		if filter != nil && filter.Tag != "" && !contains(task.Tags, filter.Tag) {
			continue
		}
		if filter != nil && filter.Parent != "" && (task.ParentTaskID == nil || *task.ParentTaskID != filter.Parent) {
			continue
		}
		out = append(out, task)
	}
	// Stable order: created_at ascending.
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ClaimTask mirrors claimTask: claim is gated on status='ready'; returns nil
// otherwise. The read-check-write is atomic under the store lock (the TS
// version relies on event-loop serialization for the same guarantee).
func (db *PlanDB) ClaimTask(tid TaskID, agent string) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok || task.Status != StatusReady {
		return nil
	}
	claimedAt := storeNow()
	next := *task
	next.Status = StatusClaimed
	next.AgentID = &agent
	next.ClaimedAt = &claimedAt
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	return &next
}

func (db *PlanDB) StartTask(tid TaskID) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok || task.Status != StatusClaimed {
		return nil
	}
	startedAt := storeNow()
	next := *task
	next.Status = StatusRunning
	next.StartedAt = &startedAt
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	return &next
}

type DoneOpts struct {
	Result any
	Files  []string
	Agent  *string
}

func (db *PlanDB) DoneTask(tid TaskID, opts DoneOpts) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil, nil
	}
	// Refuse to close while open descendants exist.
	if db.hasOpenDescendants(tid) {
		return nil, fmt.Errorf("Cannot mark task done while descendant tasks are still open.")
	}
	completedAt := storeNow()
	next := *task
	next.Status = StatusDone
	next.CompletedAt = &completedAt
	next.Result = resultToRaw(opts.Result)
	next.Files = opts.Files
	if opts.Agent != nil {
		next.AgentID = opts.Agent
	}
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	// After any close-status change, re-evaluate every pending task — a
	// single done can unblock tasks deep in the graph through the ancestor
	// chain, not just direct downstream.
	db.promoteReady()
	// Propagate done state up to a composite parent when all children are done.
	if task.ParentTaskID != nil {
		db.maybeCompleteComposite(*task.ParentTaskID, 0)
	}
	return &next, nil
}

func (db *PlanDB) FailTask(tid TaskID, errMsg string, agent *string) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil
	}
	completedAt := storeNow()
	next := *task
	next.Status = StatusFailed
	next.Error = &errMsg
	next.CompletedAt = &completedAt
	if agent != nil {
		next.AgentID = agent
	}
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	return &next
}

// DonePartialTask mirrors donePartialTask: close as done_partial — work
// accepted with known gaps. Downstream feeds_into deps see it as complete.
func (db *PlanDB) DonePartialTask(tid TaskID, opts DoneOpts) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil, nil
	}
	if db.hasOpenDescendants(tid) {
		return nil, fmt.Errorf("Cannot mark task done_partial while descendant tasks are still open.")
	}
	completedAt := storeNow()
	next := *task
	next.Status = StatusDonePartial
	next.CompletedAt = &completedAt
	next.Result = resultToRaw(opts.Result)
	next.Files = opts.Files
	if opts.Agent != nil {
		next.AgentID = opts.Agent
	}
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	db.promoteReady()
	if task.ParentTaskID != nil {
		db.maybeCompleteComposite(*task.ParentTaskID, 0)
	}
	return &next, nil
}

// ReleaseTask mirrors releaseTask: claimed|running → pending, then
// recomputeStatus promotes to ready iff deps allow. Clears execution stamps,
// PRESERVES result/error history.
func (db *PlanDB) ReleaseTask(tid TaskID) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil
	}
	if task.Status != StatusClaimed && task.Status != StatusRunning {
		return nil
	}
	next := *task
	next.Status = StatusPending
	next.AgentID = nil
	next.ClaimedAt = nil
	next.StartedAt = nil
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	// Promote back to ready when deps allow; otherwise it stays pending.
	db.recomputeStatus(tid)
	got, _ := db.tasks.Get(tid)
	return got
}

// ReopenToPending mirrors reopenToPending: ready → pending, ONLY when the
// task now has an unmet dependency. Returns (nil, nil) for unknown ids and an
// error for illegal states — this asymmetry is the TS null-vs-throw contract.
func (db *PlanDB) ReopenToPending(tid TaskID) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil, nil
	}
	if task.Status != StatusReady {
		return nil, fmt.Errorf("task %s must be ready to reopen to pending (status=%s)", tid, task.Status)
	}
	if db.effectiveUnmetDeps(tid) == 0 {
		return nil, fmt.Errorf("task %s has all dependencies met; refusing to reopen a genuinely-ready task to pending", tid)
	}
	next := *task
	next.Status = StatusPending
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	return &next, nil
}

func (db *PlanDB) CancelTask(tid TaskID) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.cancelTaskLocked(tid)
}

func (db *PlanDB) cancelTaskLocked(tid TaskID) *Task {
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil
	}
	next := *task
	next.Status = StatusCancelled
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	// Cancellation unblocks dependents (deliberate divergence, see
	// effectiveUnmetDeps) and counts as "not blocking" the composite parent's
	// auto-completion.
	db.promoteReady()
	if task.ParentTaskID != nil {
		db.maybeCompleteComposite(*task.ParentTaskID, 0)
	}
	return &next
}

// UpdateFields mirrors the Partial<Pick<Task, ...>> argument of updateTask:
// nil pointer = field not provided (TS undefined, stripped before the spread).
type UpdateFields struct {
	Title       *string
	Description *string
	Kind        *TaskKind
	Priority    *float64
}

func (db *PlanDB) UpdateTask(tid TaskID, fields UpdateFields) *Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.updateTaskLocked(tid, fields)
}

func (db *PlanDB) updateTaskLocked(tid TaskID, fields UpdateFields) *Task {
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil
	}
	next := *task
	if fields.Title != nil {
		next.Title = *fields.Title
	}
	if fields.Description != nil {
		next.Description = fields.Description
	}
	if fields.Kind != nil {
		next.Kind = *fields.Kind
	}
	if fields.Priority != nil {
		next.Priority = jscompat.JSNumber(*fields.Priority)
	}
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	return &next
}

// AmendTask mirrors amendTask: only pending/ready tasks; `\n---\n` separator.
func (db *PlanDB) AmendTask(tid TaskID, content string, position string) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil, nil
	}
	if task.Status != StatusPending && task.Status != StatusReady {
		return nil, fmt.Errorf("task %s must be pending or ready to amend (status=%s)", tid, task.Status)
	}
	existing := ""
	if task.Description != nil {
		existing = *task.Description
	}
	sep := ""
	if existing != "" {
		sep = "\n---\n"
	}
	var description string
	if position == "prepend" {
		description = content + sep + existing
	} else {
		description = existing + sep + content
	}
	return db.updateTaskLocked(tid, UpdateFields{Description: &description}), nil
}

// InsertTask mirrors insertTask: insert between after (upstream) and before
// (downstream), rewiring the after->before edge through the new task.
func (db *PlanDB) InsertTask(afterID TaskID, beforeID TaskID, input AddTaskInput) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	after, ok := db.tasks.Get(afterID)
	if !ok {
		return nil, fmt.Errorf("upstream task not found: %s", afterID)
	}
	if beforeID != "" && !db.tasks.Has(beforeID) {
		return nil, fmt.Errorf("downstream task not found: %s", beforeID)
	}

	in := input
	if in.Project == "" {
		if p, ok := db.projects.Get(after.ProjectID); ok {
			in.Project = p.Name
		}
	}
	if in.Parent == "" && after.ParentTaskID != nil {
		in.Parent = *after.ParentTaskID
	}
	if in.Deps == nil {
		k := DepFeedsInto
		in.Deps = []DepSpec{{TaskID: afterID, Kind: &k}}
	}
	created, err := db.addTaskLocked(in)
	if err != nil {
		return nil, err
	}
	if beforeID != "" {
		db.removeDepLocked(afterID, beforeID)
		db.addDepLocked(created.ID, beforeID, DepFeedsInto)
		db.recomputeStatus(beforeID)
	}
	return created, nil
}

// SplitTask mirrors splitTask: comma-separated titles for parallel split,
// 'A > B > C' for sequential.
func (db *PlanDB) SplitTask(parentID TaskID, into string) ([]*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	parent, ok := db.tasks.Get(parentID)
	if !ok {
		return nil, fmt.Errorf("task not found: %s", parentID)
	}
	sequential := strings.Contains(into, ">")
	sep := ","
	if sequential {
		sep = ">"
	}
	var titles []string
	for _, s := range strings.Split(into, sep) {
		t := jscompat.Trim(s)
		if t != "" {
			titles = append(titles, t)
		}
	}
	if len(titles) == 0 {
		return nil, fmt.Errorf("split spec is empty: %s", into)
	}

	projectName := ""
	if p, ok := db.projects.Get(parent.ProjectID); ok {
		projectName = p.Name
	}
	created := []*Task{}
	prev := ""
	for _, title := range titles {
		var deps []DepSpec
		if sequential && prev != "" {
			k := DepFeedsInto
			deps = []DepSpec{{TaskID: prev, Kind: &k}}
		}
		child, err := db.addTaskLocked(AddTaskInput{
			Title:   title,
			Parent:  parentID,
			Project: projectName,
			Kind:    parent.Kind,
			Deps:    deps,
		})
		if err != nil {
			return nil, err
		}
		created = append(created, child)
		prev = child.ID
	}
	return created, nil
}

type SplitSpec struct {
	Title       string
	Description string
	Tags        []string
}

// SplitTaskWithSpecs mirrors splitTaskWithSpecs (the W1 concurrency-cut path).
func (db *PlanDB) SplitTaskWithSpecs(parentID TaskID, specs []SplitSpec) ([]*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	parent, ok := db.tasks.Get(parentID)
	if !ok {
		return nil, fmt.Errorf("task not found: %s", parentID)
	}
	if parent.Status != StatusPending && parent.Status != StatusReady {
		return nil, fmt.Errorf("task %s must be pending or ready to split (status=%s)", parentID, parent.Status)
	}
	bad := false
	for _, spec := range specs {
		if jscompat.Trim(spec.Title) == "" || jscompat.Trim(spec.Description) == "" {
			bad = true
		}
	}
	if len(specs) < 2 || bad {
		return nil, fmt.Errorf("concurrency split requires at least two non-empty task specs")
	}
	projectName := ""
	if p, ok := db.projects.Get(parent.ProjectID); ok {
		projectName = p.Name
	}
	out := []*Task{}
	for _, spec := range specs {
		title := jscompat.Trim(spec.Title)
		desc := spec.Description
		child, err := db.addTaskLocked(AddTaskInput{
			Title:       title,
			Description: &desc,
			Tags:        spec.Tags,
			Parent:      parentID,
			Project:     projectName,
			Kind:        parent.Kind,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, nil
}

// CoalesceTasks mirrors coalesceTasks — the inverse of split. See the TS
// original for the full precondition/effect contract; every step below is a
// literal transliteration, including iteration order and the order of id/time
// draws (which a seeded differential run observes).
func (db *PlanDB) CoalesceTasks(taskIDs []TaskID, opts CoalesceTasksOpts) (*Task, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// ── validate ──
	if len(taskIDs) < 2 {
		return nil, newCoalesceError(CoalesceTooFew, fmt.Sprintf("coalesce requires ≥2 tasks, got %d", len(taskIDs)))
	}
	seen := map[TaskID]bool{}
	for _, tid := range taskIDs {
		if seen[tid] {
			return nil, newCoalesceError(CoalesceDuplicateID, "duplicate task id in coalesce group: "+tid)
		}
		seen[tid] = true
	}
	members := []*Task{}
	for _, tid := range taskIDs {
		t, ok := db.tasks.Get(tid)
		if !ok {
			return nil, newCoalesceError(CoalesceNotFound, "task not found: "+tid)
		}
		members = append(members, t)
	}
	parentID := members[0].ParentTaskID
	for _, m := range members {
		if !ptrEq(m.ParentTaskID, parentID) {
			return nil, newCoalesceError(CoalesceMixedParents, fmt.Sprintf(
				"coalesce group spans multiple parents (%s → %s, expected %s)",
				m.ID, ptrOrNull(m.ParentTaskID), ptrOrNull(parentID)))
		}
		if !coalesceOKStatuses[m.Status] {
			return nil, newCoalesceError(CoalesceNotMergeableStatus, fmt.Sprintf(
				"task %s is %s; only pending/ready tasks can be coalesced", m.ID, m.Status))
		}
	}

	groupIDs := map[TaskID]bool{}
	for _, tid := range taskIDs {
		groupIDs[tid] = true
	}

	// ── build the merged task ──
	cm := make([]CoalesceMember, len(members))
	for i, m := range members {
		cm[i] = CoalesceMember{ID: m.ID, Title: m.Title, Description: m.Description}
	}
	mergedDescription := BuildCoalescedDescription(cm, opts.FileScope)
	tagSet := newOrderedSet()
	for _, m := range members {
		for _, tag := range m.Tags {
			if strings.HasPrefix(tag, "scope:") {
				continue // stale once combined
			}
			tagSet.add(tag)
		}
	}
	if opts.ScopeTagFor != nil {
		if scopeTag := opts.ScopeTagFor(mergedDescription); scopeTag != "" {
			tagSet.add(scopeTag)
		}
	}

	// External incoming deps: union across members of edges whose upstream is
	// outside the group; strongest kind wins.
	externalDeps := jscompat.NewOrderedMap[TaskID, DepKind]()
	for _, m := range members {
		if incoming, ok := db.depsTo.Get(m.ID); ok {
			for _, e := range incoming.Entries() {
				if groupIDs[e.Key] {
					continue // internal edge — absorbed
				}
				existing, has := externalDeps.Get(e.Key)
				externalDeps.Set(e.Key, strongestDep(existing, has, e.Val))
			}
		}
	}

	project, _ := db.projects.Get(members[0].ProjectID)
	titles := make([]string, len(members))
	for i, m := range members {
		titles[i] = m.Title
	}
	var deps []DepSpec
	for _, e := range externalDeps.Entries() {
		k := e.Val
		deps = append(deps, DepSpec{TaskID: e.Key, Kind: &k})
	}
	projectName := ""
	if project != nil {
		projectName = project.Name
	}
	parentStr := ""
	if parentID != nil {
		parentStr = *parentID
	}
	merged, err := db.addTaskLocked(AddTaskInput{
		Title:       BuildCoalescedTitle(titles),
		Description: &mergedDescription,
		Kind:        members[0].Kind,
		Parent:      parentStr,
		Project:     projectName,
		Tags:        tagSet.values(),
		Deps:        deps,
	})
	if err != nil {
		return nil, err
	}

	// Rewire dependents: every external task that depended on ANY member now
	// depends on the merged task instead.
	for _, m := range members {
		if outgoing, ok := db.depsFrom.Get(m.ID); ok {
			for _, e := range outgoing.Entries() {
				db.removeDepLocked(m.ID, e.Key)
				if groupIDs[e.Key] || e.Key == merged.ID {
					continue
				}
				db.addDepLocked(merged.ID, e.Key, e.Val)
				db.recomputeStatus(e.Key)
			}
		}
	}

	// ── cancel members with a coalesced context entry ──
	for _, m := range members {
		db.addContextLocked(fmt.Sprintf("Coalesced into %s (%s).", merged.ID, merged.Title), contextOpts{
			project: projectName,
			taskID:  m.ID,
			kind:    "coalesced",
		})
		db.cancelTaskLocked(m.ID)
	}

	got, _ := db.tasks.Get(merged.ID)
	return got, nil
}

func ptrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func ptrOrNull(p *string) string {
	if p == nil {
		return "null"
	}
	return *p
}

type orderedSet struct {
	seen map[string]bool
	vals []string
}

func newOrderedSet() *orderedSet { return &orderedSet{seen: map[string]bool{}} }

func (s *orderedSet) add(v string) {
	if !s.seen[v] {
		s.seen[v] = true
		s.vals = append(s.vals, v)
	}
}

func (s *orderedSet) values() []string {
	out := make([]string, len(s.vals))
	copy(out, s.vals)
	return out
}

// ── deps ─────────────────────────────────────────────────────────────────

func (db *PlanDB) AddDep(from, to TaskID, kind DepKind) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.addDepLocked(from, to, kind)
}

func (db *PlanDB) addDepLocked(from, to TaskID, kind DepKind) {
	db.linkDep(from, to, kind)
	if db.journal != nil {
		db.journal.AddDep(from, to, kind)
	}
}

// linkDep is the index-only edge insert (no journal write) — shared by
// addDep and Restore.
func (db *PlanDB) linkDep(from, to TaskID, kind DepKind) {
	row, ok := db.depsFrom.Get(from)
	if !ok {
		row = jscompat.NewOrderedMap[TaskID, DepKind]()
		db.depsFrom.Set(from, row)
	}
	row.Set(to, kind)
	reverse, ok := db.depsTo.Get(to)
	if !ok {
		reverse = jscompat.NewOrderedMap[TaskID, DepKind]()
		db.depsTo.Set(to, reverse)
	}
	reverse.Set(from, kind)
}

func (db *PlanDB) RemoveDep(from, to TaskID) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.removeDepLocked(from, to)
}

func (db *PlanDB) removeDepLocked(from, to TaskID) {
	if row, ok := db.depsFrom.Get(from); ok {
		row.Delete(to)
	}
	if reverse, ok := db.depsTo.Get(to); ok {
		reverse.Delete(from)
	}
	if db.journal != nil {
		db.journal.RemoveDep(from, to)
	}
}

// AllDependencies mirrors allDependencies: flattened edges sorted (from, to)
// so snapshot output is index-order independent.
func (db *PlanDB) AllDependencies() []Dependency {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.allDependenciesLocked()
}

func (db *PlanDB) allDependenciesLocked() []Dependency {
	out := []Dependency{}
	for _, fe := range db.depsFrom.Entries() {
		for _, te := range fe.Val.Entries() {
			out = append(out, Dependency{FromTask: fe.Key, ToTask: te.Key, Kind: te.Val})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FromTask == out[j].FromTask {
			return out[i].ToTask < out[j].ToTask
		}
		return out[i].FromTask < out[j].FromTask
	})
	return out
}

func (db *PlanDB) DepsOfTask(tid TaskID) []Dependency {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.depsOfTaskLocked(tid)
}

func (db *PlanDB) depsOfTaskLocked(tid TaskID) []Dependency {
	incoming, ok := db.depsTo.Get(tid)
	if !ok {
		return []Dependency{}
	}
	out := []Dependency{}
	for _, e := range incoming.Entries() {
		out = append(out, Dependency{FromTask: e.Key, ToTask: tid, Kind: e.Val})
	}
	return out
}

func (db *PlanDB) DependentsOfTask(tid TaskID) []Dependency {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.dependentsOfTaskLocked(tid)
}

func (db *PlanDB) dependentsOfTaskLocked(tid TaskID) []Dependency {
	outgoing, ok := db.depsFrom.Get(tid)
	if !ok {
		return []Dependency{}
	}
	out := []Dependency{}
	for _, e := range outgoing.Entries() {
		out = append(out, Dependency{FromTask: tid, ToTask: e.Key, Kind: e.Val})
	}
	return out
}

// ── contexts ─────────────────────────────────────────────────────────────

type contextOpts struct {
	project string
	taskID  TaskID
	kind    string
}

type AddContextOpts struct {
	Project string
	TaskID  TaskID
	Kind    string
}

func (db *PlanDB) AddContext(content string, opts AddContextOpts) *ContextEntry {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.addContextLocked(content, contextOpts{project: opts.Project, taskID: opts.TaskID, kind: opts.Kind})
}

func (db *PlanDB) addContextLocked(content string, opts contextOpts) *ContextEntry {
	project := db.resolveProjectLocked(opts.project)
	var taskID *TaskID
	if opts.taskID != "" {
		t := opts.taskID
		taskID = &t
	}
	kind := opts.kind
	if kind == "" {
		kind = "note"
	}
	entry := &ContextEntry{
		ID:        genID("ctx", 6),
		ProjectID: project.ID,
		TaskID:    taskID,
		Kind:      kind,
		Content:   content,
		CreatedAt: storeNow(),
	}
	db.contexts = append(db.contexts, entry)
	if db.journal != nil {
		db.journal.PutContext(entry)
	}
	return entry
}

type ListContextsFilter struct {
	Project string
	Kind    string
	TaskID  TaskID
	Limit   float64 // JS number semantics: 0/NaN are falsy → no limit
}

func (db *PlanDB) ListContexts(filter *ListContextsFilter) []*ContextEntry {
	db.mu.Lock()
	defer db.mu.Unlock()
	var project *Project
	if filter != nil && filter.Project != "" {
		project = db.resolveProjectLocked(filter.Project)
	}
	out := []*ContextEntry{}
	for _, c := range db.contexts {
		if project != nil && c.ProjectID != project.ID {
			continue
		}
		if filter != nil && filter.Kind != "" && c.Kind != filter.Kind {
			continue
		}
		if filter != nil && filter.TaskID != "" && (c.TaskID == nil || *c.TaskID != filter.TaskID) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if filter != nil && jscompat.Truthy(filter.Limit) {
		out = jscompat.SliceTo(out, filter.Limit)
	}
	return out
}

// Search mirrors search: substring match over task title/description then
// context content; tasks first, then contexts. Elements are *Task or
// *ContextEntry (the TS return is the same union).
func (db *PlanDB) Search(query string, project string, limit float64) []any {
	db.mu.Lock()
	defer db.mu.Unlock()
	needle := strings.ToLower(query)
	var proj *Project
	if project != "" {
		proj = db.resolveProjectLocked(project)
	}
	hits := []any{}
	for _, task := range db.tasks.Values() {
		if proj != nil && task.ProjectID != proj.ID {
			continue
		}
		desc := ""
		if task.Description != nil {
			desc = *task.Description
		}
		if strings.Contains(strings.ToLower(task.Title), needle) || strings.Contains(strings.ToLower(desc), needle) {
			hits = append(hits, task)
		}
	}
	for _, c := range db.contexts {
		if proj != nil && c.ProjectID != proj.ID {
			continue
		}
		if strings.Contains(strings.ToLower(c.Content), needle) {
			hits = append(hits, c)
		}
	}
	if jscompat.Truthy(limit) {
		hits = jscompat.SliceTo(hits, limit)
	}
	return hits
}

// ── analytics ────────────────────────────────────────────────────────────

func (db *PlanDB) Status(project string) Status {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.statusLocked(project)
}

func (db *PlanDB) statusLocked(project string) Status {
	var tasks []*Task
	if project != "" {
		tasks = db.listTasksLocked(&ListTasksFilter{Project: project})
	} else {
		tasks = db.tasks.Values()
	}
	s := Status{Total: len(tasks)}
	for _, t := range tasks {
		switch t.Status {
		case StatusDone, StatusDonePartial:
			s.Done++
		case StatusPending:
			s.Pending++
		case StatusReady:
			s.Ready++
		case StatusRunning:
			s.Running++
		case StatusClaimed:
			s.Claimed++
		case StatusFailed:
			s.Failed++
		case StatusCancelled:
			s.Cancelled++
		}
	}
	return s
}

func (db *PlanDB) WhatUnlocks(tid TaskID) []*Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	out := []*Task{}
	downstream, ok := db.depsFrom.Get(tid)
	if !ok {
		return out
	}
	for _, did := range downstream.Keys() {
		t, ok := db.tasks.Get(did)
		if !ok {
			continue
		}
		deps := db.depsOfTaskLocked(did)
		// Task is unlocked by this one only if all OTHER deps are already done.
		otherDepsOpen := false
		for _, d := range deps {
			if d.FromTask == tid {
				continue
			}
			up, _ := db.tasks.Get(d.FromTask)
			status := TaskStatus("")
			if up != nil {
				status = up.Status
			}
			if !closedStatuses[status] {
				otherDepsOpen = true
			}
		}
		if !otherDepsOpen {
			out = append(out, t)
		}
	}
	return out
}

// Ahead mirrors ahead: layered readiness lookahead. depth follows JS number
// loop semantics (`for (let i = 0; i < depth; i++)`).
func (db *PlanDB) Ahead(depth float64, project string) [][]*Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	var projTasks []*Task
	if project != "" {
		projTasks = db.listTasksLocked(&ListTasksFilter{Project: project})
	} else {
		projTasks = db.tasks.Values()
	}
	closed := map[TaskID]bool{}
	for _, t := range projTasks {
		if closedStatuses[t.Status] {
			closed[t.ID] = true
		}
	}
	layers := [][]*Task{}
	inLayers := func(id TaskID) bool {
		for _, l := range layers {
			for _, lt := range l {
				if lt.ID == id {
					return true
				}
			}
		}
		return false
	}
	for i := 0.0; i < depth; i++ {
		layer := []*Task{}
		for _, t := range projTasks {
			if closed[t.ID] {
				continue
			}
			if inLayers(t.ID) {
				continue
			}
			ok := true
			for _, d := range db.depsOfTaskLocked(t.ID) {
				if d.Kind != DepFeedsInto && d.Kind != DepBlocks {
					continue
				}
				if !closed[d.FromTask] && !inLayers(d.FromTask) {
					ok = false
					break
				}
			}
			if ok {
				layer = append(layer, t)
			}
		}
		if len(layer) == 0 {
			break
		}
		layers = append(layers, layer)
	}
	return layers
}

// CriticalPath mirrors criticalPath, including its quirks: the memo is shared
// across all roots (results can depend on outer iteration order), the dfs
// recurses through the GLOBAL depsFrom but filters project-scoped, and there
// is NO cycle guard.
func (db *PlanDB) CriticalPath(project string) []*Task {
	db.mu.Lock()
	defer db.mu.Unlock()
	var projTasks []*Task
	if project != "" {
		projTasks = db.listTasksLocked(&ListTasksFilter{Project: project})
	} else {
		projTasks = db.tasks.Values()
	}
	byID := map[TaskID]*Task{}
	for _, t := range projTasks {
		byID[t.ID] = t
	}
	type res struct {
		depth int
		path  []TaskID
	}
	memo := map[TaskID]res{}
	var dfs func(tid TaskID) res
	dfs = func(tid TaskID) res {
		if cached, ok := memo[tid]; ok {
			return cached
		}
		downstream, ok := db.depsFrom.Get(tid)
		if !ok || downstream.Len() == 0 {
			r := res{depth: 1, path: []TaskID{tid}}
			memo[tid] = r
			return r
		}
		best := res{depth: 1, path: []TaskID{tid}}
		for _, next := range downstream.Keys() {
			if _, in := byID[next]; !in {
				continue
			}
			r := dfs(next)
			if r.depth+1 > best.depth {
				best = res{depth: r.depth + 1, path: append([]TaskID{tid}, r.path...)}
			}
		}
		memo[tid] = best
		return best
	}
	best := res{depth: 0, path: []TaskID{}}
	for _, t := range projTasks {
		r := dfs(t.ID)
		if r.depth > best.depth {
			best = r
		}
	}
	out := []*Task{}
	for _, id := range best.path {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

type Bottleneck struct {
	Task       *Task             `json:"task"`
	Downstream jscompat.JSNumber `json:"downstream"`
}

func (db *PlanDB) Bottlenecks(project string) []Bottleneck {
	db.mu.Lock()
	defer db.mu.Unlock()
	var projTasks []*Task
	if project != "" {
		projTasks = db.listTasksLocked(&ListTasksFilter{Project: project})
	} else {
		projTasks = db.tasks.Values()
	}
	out := []Bottleneck{}
	for _, t := range projTasks {
		count := db.transitiveDownstreamCount(t.ID)
		if count > 0 {
			out = append(out, Bottleneck{Task: t, Downstream: jscompat.JSNumber(count)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Downstream > out[j].Downstream })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

type ProjectDag struct {
	Project      *Project     `json:"project"`
	Tasks        []*Task      `json:"tasks"`
	Dependencies []Dependency `json:"dependencies"`
}

func (db *PlanDB) ProjectDag(project string) ProjectDag {
	db.mu.Lock()
	defer db.mu.Unlock()
	p := db.resolveProjectLocked(project)
	tasks := db.listTasksLocked(&ListTasksFilter{Project: p.Name})
	deps := []Dependency{}
	for _, t := range tasks {
		deps = append(deps, db.dependentsOfTaskLocked(t.ID)...)
	}
	return ProjectDag{Project: p, Tasks: tasks, Dependencies: deps}
}

type Overview struct {
	Project *Project `json:"project"`
	Status  Status   `json:"status"`
	Recent  []*Task  `json:"recent"`
}

func (db *PlanDB) Overview(project string) Overview {
	db.mu.Lock()
	defer db.mu.Unlock()
	p := db.resolveProjectLocked(project)
	all := db.listTasksLocked(&ListTasksFilter{Project: p.Name})
	recent := all
	if len(all) > 10 {
		recent = all[len(all)-10:]
	}
	return Overview{Project: p, Status: db.statusLocked(p.Name), Recent: recent}
}

// SnapshotState mirrors snapshot().
func (db *PlanDB) SnapshotState() Snapshot {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.snapshotLocked()
}

func (db *PlanDB) snapshotLocked() Snapshot {
	contexts := make([]*ContextEntry, len(db.contexts))
	copy(contexts, db.contexts)
	return Snapshot{
		Projects:     db.projects.Values(),
		Tasks:        db.tasks.Values(),
		Contexts:     contexts,
		Dependencies: db.allDependenciesLocked(),
	}
}

func (db *PlanDB) Reset() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.projects.Clear()
	db.tasks.Clear()
	db.depsFrom.Clear()
	db.depsTo.Clear()
	db.contexts = db.contexts[:0]
	db.projectByName.Clear()
	if db.journal != nil {
		db.journal.Reset()
	}
}

// ── internals ────────────────────────────────────────────────────────────

func (db *PlanDB) hasOpenDescendants(tid TaskID) bool {
	stack := []TaskID{}
	for _, child := range db.tasks.Values() {
		if child.ParentTaskID != nil && *child.ParentTaskID == tid {
			stack = append(stack, child.ID)
		}
	}
	for len(stack) > 0 {
		cid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		child, ok := db.tasks.Get(cid)
		if !ok {
			continue
		}
		if !closedStatuses[child.Status] {
			return true
		}
		for _, grand := range db.tasks.Values() {
			if grand.ParentTaskID != nil && *grand.ParentTaskID == cid {
				stack = append(stack, grand.ID)
			}
		}
	}
	return false
}

// recomputeStatus mirrors recomputeStatus: pending → ready iff the task's OWN
// deps and every ANCESTOR's deps are all met.
func (db *PlanDB) recomputeStatus(tid TaskID) {
	task, ok := db.tasks.Get(tid)
	if !ok || task.Status != StatusPending {
		return
	}
	if db.effectiveUnmetDeps(tid) == 0 {
		next := *task
		next.Status = StatusReady
		next.UpdatedAt = storeNow()
		db.setTask(&next)
	}
}

// effectiveUnmetDeps mirrors the `effective_unmet` CTE walk: count unmet
// hard deps along the ancestor chain; suggests edges are ignored.
func (db *PlanDB) effectiveUnmetDeps(tid TaskID) int {
	unmet := 0
	seenAncestors := map[TaskID]bool{}
	current := tid
	hasCurrent := true
	depth := 0
	for hasCurrent && !seenAncestors[current] && depth < 32 {
		seenAncestors[current] = true
		if incoming, ok := db.depsTo.Get(current); ok {
			for _, e := range incoming.Entries() {
				if e.Val != DepFeedsInto && e.Val != DepBlocks {
					continue
				}
				status := TaskStatus("")
				if up, ok := db.tasks.Get(e.Key); ok {
					status = up.Status
				}
				// Deliberate divergence from the TS CTE (BUGS-KEPT.md): a
				// cancelled upstream also satisfies the edge. The replanner's
				// documented no-op-task strategy is cancel-and-amend, and TS
				// only survives cancel-never-unblocks because its quiet exit
				// slides into audit over an open graph — a hole this port
				// closed. Without this, a cancelled no-op leaf deadlocks every
				// dependent forever.
				if status != StatusDone && status != StatusDonePartial &&
					status != StatusCancelled {
					unmet++
				}
			}
		}
		task, ok := db.tasks.Get(current)
		if ok && task.ParentTaskID != nil {
			current = *task.ParentTaskID
		} else {
			hasCurrent = false
		}
		depth++
	}
	return unmet
}

// FailedUpstreamBlockers returns the terminally failed upstream ids that are
// the ONLY thing keeping tid pending, in insertion order. It returns nil when
// tid is not pending, when it has no failed upstream, or when some other
// dependency is still open — a still-open upstream means waiting is meaningful
// and the task must keep waiting.
//
// StatusFailed deliberately does NOT satisfy a hard edge in effectiveUnmetDeps:
// unlike a cancelled no-op, a failed upstream's work genuinely did not happen,
// so a dependent promoted blindly would build on a missing prerequisite. But a
// failed task is terminal — nothing ever moves it to done — so every dependent
// is stranded pending forever, the root scheduler dispatches no work, and the
// run dies holding whatever the failed leaf had produced. This reports that
// state so the caller can break the deadlock deliberately, at the point where
// the only alternative is a certain stall.
func (db *PlanDB) FailedUpstreamBlockers(tid TaskID) []TaskID {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.failedUpstreamBlockersLocked(tid)
}

func (db *PlanDB) failedUpstreamBlockersLocked(tid TaskID) []TaskID {
	task, ok := db.tasks.Get(tid)
	if !ok || task.Status != StatusPending {
		return nil
	}
	failed := []TaskID{}
	seenFailed := map[TaskID]bool{}
	seenAncestors := map[TaskID]bool{}
	current := tid
	hasCurrent := true
	depth := 0
	// Walk the same ancestor chain effectiveUnmetDeps does, partitioning the
	// unmet hard edges into "terminally failed" and "still open".
	for hasCurrent && !seenAncestors[current] && depth < 32 {
		seenAncestors[current] = true
		if incoming, ok := db.depsTo.Get(current); ok {
			for _, e := range incoming.Entries() {
				if e.Val != DepFeedsInto && e.Val != DepBlocks {
					continue
				}
				status := TaskStatus("")
				if up, ok := db.tasks.Get(e.Key); ok {
					status = up.Status
				}
				if status == StatusDone || status == StatusDonePartial ||
					status == StatusCancelled {
					continue
				}
				if status != StatusFailed {
					// A dependency that can still complete on its own.
					return nil
				}
				if !seenFailed[e.Key] {
					seenFailed[e.Key] = true
					failed = append(failed, e.Key)
				}
			}
		}
		ancestor, ok := db.tasks.Get(current)
		if ok && ancestor.ParentTaskID != nil {
			current = *ancestor.ParentTaskID
		} else {
			hasCurrent = false
		}
		depth++
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

// ReleaseFailedUpstreamBlock promotes a task that FailedUpstreamBlockers
// reports as deadlocked to ready, returning the bypassed failed upstream ids.
// It returns nil (and changes nothing) when the task is not in that state.
func (db *PlanDB) ReleaseFailedUpstreamBlock(tid TaskID) []TaskID {
	db.mu.Lock()
	defer db.mu.Unlock()
	blockers := db.failedUpstreamBlockersLocked(tid)
	if len(blockers) == 0 {
		return nil
	}
	task, ok := db.tasks.Get(tid)
	if !ok {
		return nil
	}
	next := *task
	next.Status = StatusReady
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	return blockers
}

// promoteReady mirrors promoteReady: a SINGLE pass over every task in
// insertion order — a task promoted early in the pass can satisfy a later
// task's dep within the same pass. No fixpoint loop.
func (db *PlanDB) promoteReady() {
	for _, task := range db.tasks.Values() {
		if current, ok := db.tasks.Get(task.ID); ok && current.Status == StatusPending {
			db.recomputeStatus(task.ID)
		}
	}
}

func (db *PlanDB) transitiveDownstreamCount(tid TaskID) int {
	seen := map[TaskID]bool{}
	stack := []TaskID{}
	if row, ok := db.depsFrom.Get(tid); ok {
		stack = append(stack, row.Keys()...)
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		if row, ok := db.depsFrom.Get(n); ok {
			stack = append(stack, row.Keys()...)
		}
	}
	return len(seen)
}

// maybeCompleteComposite mirrors COMPLETE_COMPOSITE_IF_CHILDREN_DONE:
// only fires while the parent is pending/ready; cancelled and done_partial
// children count as complete; recursion capped at depth 32.
func (db *PlanDB) maybeCompleteComposite(parentID TaskID, depth int) {
	if depth >= 32 {
		return
	}
	parent, ok := db.tasks.Get(parentID)
	if !ok || !parent.IsComposite {
		return
	}
	if parent.Status != StatusPending && parent.Status != StatusReady {
		return
	}
	children := []*Task{}
	for _, t := range db.tasks.Values() {
		if t.ParentTaskID != nil && *t.ParentTaskID == parentID {
			children = append(children, t)
		}
	}
	if len(children) == 0 {
		return
	}
	for _, c := range children {
		if c.Status != StatusDone && c.Status != StatusDonePartial && c.Status != StatusCancelled {
			return
		}
	}
	completedAt := storeNow()
	next := *parent
	next.Status = StatusDone
	next.CompletedAt = &completedAt
	next.UpdatedAt = storeNow()
	db.setTask(&next)
	db.promoteReady()
	if parent.ParentTaskID != nil {
		db.maybeCompleteComposite(*parent.ParentTaskID, depth+1)
	}
}

func encodePolicy(input AddTaskInput) string {
	desc := ""
	if input.Description != nil {
		desc = *input.Description
	}
	has := func(k string) bool {
		re := regexp.MustCompile(`(?im)^` + k + `:`)
		return re.MatchString(desc)
	}
	lines := []string{}
	if input.Access != "" && !has("access") {
		lines = append(lines, "access: "+input.Access)
	}
	if input.Parallel != "" && !has("parallel") {
		lines = append(lines, "parallel: "+input.Parallel)
	}
	if input.Worktree != "" && !has("worktree") {
		lines = append(lines, "worktree: "+input.Worktree)
	}
	if input.TaskRole != "" && !has("task_role") {
		lines = append(lines, "task_role: "+input.TaskRole)
	}
	if len(input.Outputs) > 0 && !has("outputs") {
		lines = append(lines, "outputs: "+strings.Join(input.Outputs, ","))
	}
	if len(lines) == 0 {
		return desc
	}
	joined := strings.Join(lines, "\n")
	if desc == "" {
		return joined
	}
	return joined + "\n" + desc
}

func composeTags(input AddTaskInput) []string {
	tags := newOrderedSet()
	for _, t := range input.Tags {
		tags.add(t)
	}
	if input.Access != "" {
		tags.add("access:" + input.Access)
	}
	if input.Parallel != "" {
		tags.add("parallel:" + input.Parallel)
	}
	if input.Worktree == "required" {
		tags.add("needs:worktree")
	}
	if input.TaskRole != "" {
		tags.add("role:" + input.TaskRole)
	}
	for _, out := range input.Outputs {
		tags.add("output:" + out)
	}
	return tags.values()
}

func resultToRaw(v any) []byte {
	if v == nil {
		return nil
	}
	if raw, ok := v.([]byte); ok {
		return raw
	}
	b, err := jscompat.Stringify(v)
	if err != nil {
		return nil
	}
	return b
}

// ── process-global singleton ─────────────────────────────────────────────

var (
	globalMu sync.Mutex
	globalDB *PlanDB
)

// GetPlanDB mirrors getPlanDB(): the process-global singleton.
func GetPlanDB() *PlanDB {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalDB == nil {
		globalDB = NewPlanDB()
	}
	return globalDB
}

// ResetPlanDBForTesting mirrors _resetPlanDBForTesting — test-only.
func ResetPlanDBForTesting() {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalDB = nil
}
