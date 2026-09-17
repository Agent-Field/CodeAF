package plandb

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Store is the plan: one file, a mutex for this process, and an advisory file
// lock for every other one (lock.go). Its method set is the aforge-v1 port's,
// kept because it already answers the CLI's questions: the graph laws
// (validateGraphs below) are the port's own and were correct there.
//
// WHAT THE ADAPTATION TOOK OUT, deliberately, is written at the functions that
// changed: the aforge-v1 store doubled as a governance gate — validateSpec
// required a role, deliverables and acceptance on every task, and Claim refused
// a task whose effect was unresolved or that claimed no resources. The rust
// CLI has no flags for any of that, so every `plandb add` the doctrine teaches
// would have been refused by the port's own gates. The gates are gone; the
// graph laws stay.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	data state
	// locked is the advisory lock's handle while a Transaction or changeTask
	// holds it, so releaseFile can find the file holdFile took without either
	// of them guessing at the other's state.
	locked *os.File
}

// Open loads the plan at path, or creates one when the file does not exist.
//
// THE ROOT IS THE RUN, which is the reference loop's own model: `plandb init`
// makes a project and the runtime runs it by seeding a root task for the work
// it was given. A file that exists but belongs to a different run is a
// refusal, not a merge: two sessions sharing one store by accident would each
// dispatch the other's children.
func Open(path, project, rootID, rootTitle, rootDescription string) (*Store, error) {
	store := &Store{path: path, now: time.Now}
	loaded, err := loadState(path)
	if err == nil {
		if (rootID != "" && loaded.RootID != rootID) || (project != "" && loaded.Project != project) {
			return nil, errors.New("plan store belongs to a different run")
		}
		store.data = loaded
		return store, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if strings.TrimSpace(project) == "" || !validID(rootID) {
		return nil, errors.New("a new plan store needs a project and a valid root id")
	}
	// THE CREATE IS A TRANSACTION TOO. Two processes can reach this line at
	// once — the runtime seeding, a worker's CLI init-ing — and under the
	// file lock the second one re-reads and adopts the first one's file
	// rather than renaming its own over it. Adopting keeps the rule the same
	// as the load road: a store that belongs to another run is a refusal,
	// not a merge.
	if err := store.holdFile(); err != nil {
		return nil, err
	}
	defer store.releaseFile()
	store.mu.Lock()
	defer store.mu.Unlock()
	if fresh, err := loadState(path); err == nil {
		if (rootID != "" && fresh.RootID != rootID) || (project != "" && fresh.Project != project) {
			return nil, errors.New("plan store belongs to a different run")
		}
		store.data = fresh
		return store, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	now := store.now().UTC()
	root := &Task{
		TaskSpec: TaskSpec{
			ID: rootID, Title: strings.TrimSpace(rootTitle), Description: rootDescription,
			Kind: "generic", Parallel: "safe", Isolation: "shared",
		},
		Status: StatusRunning, ClaimedBy: "runtime", CreatedAt: now, UpdatedAt: now,
	}
	store.data = state{
		Version: stateVersion, Project: project, RootID: rootID,
		Tasks: map[string]*Task{rootID: root}, Order: []string{rootID}, NextID: 1,
	}
	if err := store.commitLocked(store.data); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

func (s *Store) Project() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Project
}

func (s *Store) RootID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.RootID
}

// AddMany admits a batch of specs and answers the tasks as they now stand.
// The batch is validated whole before any of it is written, so a split with a
// bad third part creates nothing — which is the property the CLI's split
// answer and the runtime's dispatch both rest on.
//
// THE ID IS THE CALLER'S. The runtime mints ids it can match to nodes; the
// CLI mints short random ones the rust CLI's shape (`t-` + four base-36
// characters) and honours `--as` names. Both roads end here.
func (s *Store) AddMany(specs []TaskSpec) ([]*Task, error) {
	// EVERY WRITE IS ONE TRANSACTION. This store is written by more than one
	// process — the CLI's add and split are separate processes — so the batch
	// is read, validated and written under the file lock (lock.go) and against
	// a fresh load, or a stale handle's write would rename over another
	// handle's task and lose it. The store-test wave proved exactly that loss
	// with a failing test before this gate went in.
	if err := s.holdFile(); err != nil {
		return nil, err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	if len(specs) == 0 {
		return nil, errors.New("tasks must not be empty")
	}
	if len(specs) > 256 {
		return nil, errors.New("a plan may contain at most 256 tasks")
	}
	next := cloneState(s.data)
	if len(next.Tasks)+len(specs) > 1024 {
		return nil, errors.New("a run may contain at most 1024 tasks")
	}
	batch := make(map[string]bool, len(specs))
	for i := range specs {
		specs[i] = normalizeSpec(specs[i], next.RootID)
		if err := validateSpec(specs[i]); err != nil {
			return nil, fmt.Errorf("task %d: %w", i, err)
		}
		if next.Tasks[specs[i].ID] != nil || batch[specs[i].ID] {
			return nil, fmt.Errorf("duplicate task id %q", specs[i].ID)
		}
		batch[specs[i].ID] = true
	}
	for _, spec := range specs {
		if next.Tasks[spec.ParentID] == nil && !batch[spec.ParentID] {
			return nil, fmt.Errorf("task %q has unknown parent %q", spec.ID, spec.ParentID)
		}
		if parent := next.Tasks[spec.ParentID]; parent != nil && terminal(parent.Status) {
			return nil, fmt.Errorf("task %q has terminal parent %q", spec.ID, spec.ParentID)
		}
		for _, dep := range spec.Dependencies {
			if next.Tasks[dep.TaskID] == nil && !batch[dep.TaskID] {
				return nil, fmt.Errorf("task %q has unknown dependency %q", spec.ID, dep.TaskID)
			}
			if dep.TaskID == spec.ID {
				return nil, fmt.Errorf("task %q depends on itself", spec.ID)
			}
		}
	}
	now := s.now().UTC()
	for _, spec := range specs {
		task := &Task{TaskSpec: spec, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
		next.Tasks[spec.ID] = task
		next.Order = append(next.Order, spec.ID)
	}
	for _, spec := range specs {
		if parent := next.Tasks[spec.ParentID]; parent != nil {
			parent.Composite = true
			parent.UpdatedAt = now
		}
	}
	if err := validateGraphs(next); err != nil {
		return nil, err
	}
	promote(&next, now)
	if err := s.commitLocked(next); err != nil {
		return nil, err
	}
	created := make([]*Task, 0, len(specs))
	for _, spec := range specs {
		created = append(created, cloneTask(s.data.Tasks[spec.ID]))
	}
	return created, nil
}

// ReadyLeaves answers the tasks a worker may start now: ready, not composite,
// nothing else running they conflict with.
func (s *Store) ReadyLeaves() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tasks []*Task
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.Status != StatusReady || task.Composite || len(executionBlockReasons(s.data, task)) > 0 {
			continue
		}
		tasks = append(tasks, cloneTask(task))
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Priority > tasks[j].Priority })
	return tasks
}

// ReadySet is ReadyLeaves with the reasons: what can run and, for each task
// that cannot, why not. The doctrine's `list --status ready` and the
// runtime's dispatch both read it.
func (s *Store) ReadySet() ReadySet {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ReadySet{}
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.Status != StatusReady || task.Composite {
			continue
		}
		if reasons := executionBlockReasons(s.data, task); len(reasons) > 0 {
			result.Blocked = append(result.Blocked, BlockedTask{Task: cloneTask(task), Reasons: reasons})
			continue
		}
		result.Runnable = append(result.Runnable, cloneTask(task))
	}
	sort.SliceStable(result.Runnable, func(i, j int) bool {
		return result.Runnable[i].Priority > result.Runnable[j].Priority
	})
	return result
}

func (s *Store) Show(id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.data.Tasks[id]
	if task == nil {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return cloneTask(task), nil
}

// Task returns a copy of one task by exact id, for callers that already know
// the id (the runtime does; a store-born node's id is the plan id).
func (s *Store) Task(id string) *Task {
	task, err := s.Show(id)
	if err != nil {
		return nil
	}
	return task
}

// Resolve answers one task for a word the model may have written loosely:
// an exact id first, then `t-` + the word, then a unique prefix of either.
// The rust CLI fuzzy-matches ids and the doctrine leans on that; a prefix
// that fits more than one task is a question the caller must not guess at.
func (s *Store) Resolve(word string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.data.Tasks[strings.TrimPrefix(word, "t-")]; task != nil {
		return cloneTask(task), nil
	}
	var matches []*Task
	for _, id := range s.data.Order {
		if id == s.data.RootID {
			continue
		}
		if strings.HasPrefix(id, strings.TrimPrefix(word, "t-")) || strings.HasPrefix("t-"+id, word) {
			matches = append(matches, cloneTask(s.data.Tasks[id]))
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no task matches %q", word)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, task := range matches {
			ids = append(ids, "t-"+task.ID)
		}
		return nil, fmt.Errorf("%q matches several tasks: %s", word, strings.Join(ids, ", "))
	}
}

// Claim hands a ready leaf to an agent. THE AGENT NAME IS THE RUNTIME'S
// NAMING TRICK from the reference loop: the supervisor claims with the task's
// own id, so the worker that later finishes "as" the task can only be the
// worker the task was handed to. Ownership in Done and Fail is enforced
// against exactly this name.
func (s *Store) Claim(id, agent string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if task.Status != StatusReady || task.Composite {
			return fmt.Errorf("task %q is not a runnable ready leaf", id)
		}
		if strings.TrimSpace(agent) == "" {
			return errors.New("agent is required for claim")
		}
		task.Status, task.ClaimedBy, task.UpdatedAt = StatusRunning, strings.TrimSpace(agent), now
		return nil
	})
}

// Done completes a task its agent owns. The root is the runtime's, exactly as
// the aforge-v1 port had it and the reference loop enforces: a worker cannot
// finish the run, only its own task.
func (s *Store) Done(id, agent, result string, artifacts, evidence []string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if len(result) > 64<<10 {
			return errors.New("completion result exceeds 65536 bytes")
		}
		if id == next.RootID {
			return errors.New("the harness owns root completion")
		}
		// THE ONE CASE OWNERSHIP YIELDS TO: a composite parent the store itself
		// auto-completed with an empty result while its own node was still
		// working. The placeholder is bookkeeping, not a report; the node's
		// landing is what the placeholder was waiting for, and the caller that
		// fills it adopts the task as its own. Any task with words in it — a
		// worker's own done, a real ending — keeps its words and its owner.
		placeholder := task.Status == StatusDone && strings.TrimSpace(task.Result) == "" && task.ClaimedBy == ""
		if !placeholder {
			if err := requireOwner(task, agent); err != nil {
				return err
			}
			if task.Composite && !allChildrenDone(*next, id) {
				return fmt.Errorf("task %q has unfinished or failed children", id)
			}
			switch task.Status {
			case StatusClaimed, StatusRunning:
			default:
				return fmt.Errorf("task %q cannot complete from status %s", id, task.Status)
			}
		}
		if len(task.EvidenceRequirements) > 0 && len(cleanStrings(evidence)) == 0 {
			return fmt.Errorf("task %q requires completion evidence", id)
		}
		task.Status, task.Result = StatusDone, result
		task.ClaimedBy = strings.TrimSpace(agent)
		task.Artifacts = cleanStrings(artifacts)
		task.Evidence = cleanStrings(evidence)
		task.UpdatedAt, task.CompletedAt = now, now
		promote(next, now)
		return nil
	})
}

// Fail marks a task failed by its owner, with a reason the next reader sees.
func (s *Store) Fail(id, agent, message string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if len(message) > 32<<10 {
			return errors.New("failure reason exceeds 32768 bytes")
		}
		if id == next.RootID {
			return errors.New("the harness owns the root task")
		}
		if terminal(task.Status) {
			return fmt.Errorf("task %q is already terminal", id)
		}
		if err := requireOwner(task, agent); err != nil {
			return err
		}
		task.Status, task.Error, task.UpdatedAt, task.CompletedAt = StatusFailed, message, now, now
		promote(next, now)
		return nil
	})
}

// Release puts a claimed task back to pending, for an owner that is not going
// to finish it. Promotion runs again so a task whose blocker cleared while it
// was held comes back ready.
func (s *Store) Release(id, agent string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if id == next.RootID {
			return errors.New("the harness owns the root task")
		}
		if task.Status != StatusClaimed && task.Status != StatusRunning {
			return fmt.Errorf("task %q is not claimed or running", id)
		}
		if err := requireOwner(task, agent); err != nil {
			return err
		}
		task.Status, task.ClaimedBy, task.UpdatedAt = StatusPending, "", now
		promote(next, now)
		return nil
	})
}

// Retry reopens a failed task. The runtime uses it when a node's ending says
// the work, not the store, is worth another run.
func (s *Store) Retry(id string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if id == next.RootID {
			return errors.New("the harness owns the root task")
		}
		if task.Status != StatusFailed {
			return fmt.Errorf("task %q is not failed", id)
		}
		task.Status, task.Error, task.ClaimedBy = StatusPending, "", ""
		task.CompletedAt, task.UpdatedAt = time.Time{}, now
		promote(next, now)
		return nil
	})
}

// Cancel ends a task, its descendants, and the work that hard-depends on it.
// The cascade is the port's own law and matches the reference's `--cascade`
// default: a cancelled dependency is a cancelled dependent, because nothing
// in this store can resolve a hard edge whose upstream will never answer.
func (s *Store) Cancel(id, reason string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if id == next.RootID {
			return errors.New("the harness owns the root task")
		}
		if terminal(task.Status) {
			return fmt.Errorf("task %q is already terminal", id)
		}
		task.Status, task.Error, task.ClaimedBy = StatusCancelled, strings.TrimSpace(reason), ""
		task.UpdatedAt, task.CompletedAt = now, now
		cancelDescendants(next, id, "ancestor "+id+" was cancelled", now)
		cancelBlockedDependents(next, id, "dependency "+id+" was cancelled", now)
		promote(next, now)
		return nil
	})
}

// Amend prepends text to a task's description — the doctrine's "annotate
// future work" verb, and one of the two ways a plan learns while it runs.
func (s *Store) Amend(id, text string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if len(text) > 32<<10 {
			return errors.New("amendment exceeds 32768 bytes")
		}
		task.Description = strings.TrimSpace(text) + "\n\n" + task.Description
		task.UpdatedAt = now
		return nil
	})
}

// Revise patches a task's contract before it runs. After it starts, the
// contract is frozen: a worker mid-flight answering a spec nobody wrote is
// the failure the revision gate exists to prevent.
func (s *Store) Revise(id string, patch TaskPatch) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if task.Status != StatusPending && task.Status != StatusReady {
			return fmt.Errorf("task %q can only be revised before execution", id)
		}
		applyPatch(&task.TaskSpec, patch)
		task.TaskSpec = normalizeSpec(task.TaskSpec, next.RootID)
		if err := validateSpec(task.TaskSpec); err != nil {
			return err
		}
		task.UpdatedAt = now
		return nil
	})
}

// AddDep adds one edge between two tasks. It is the CLI's `task add-dep`, and
// the graph laws are asked of the whole result: a cross-lineage hard edge or
// a cycle refuses the edge rather than corrupting the plan.
func (s *Store) AddDep(downstream, upstream string, kind DepKind) (*Task, error) {
	if kind == "" {
		kind = DepFeedsInto
	}
	return s.changeTask(downstream, func(next *state, task *Task, now time.Time) error {
		if next.Tasks[upstream] == nil {
			return fmt.Errorf("task %q not found", upstream)
		}
		for _, dep := range task.Dependencies {
			if dep.TaskID == upstream {
				return fmt.Errorf("task %q already depends on %q", downstream, upstream)
			}
		}
		task.Dependencies = append(task.Dependencies, Dependency{TaskID: upstream, Kind: kind})
		if err := validateGraphs(*next); err != nil {
			return err
		}
		// A ready task that has just gained a hard dependency is not runnable
		// now, and "ready" must mean runnable now — so it falls back to pending
		// and promote() re-raises it when the new upstream finishes. Claimed and
		// running work stays where it is: a task mid-flight cannot be re-scoped
		// out from under its worker by a later edge.
		if task.Status == StatusReady && !depsDone(*next, task) {
			task.Status = StatusPending
		}
		task.UpdatedAt = now
		promote(next, now)
		return nil
	})
}

// AddNote leaves a task-scoped message. The note is public to every worker on
// the run — the CLI's notes listing prints all of them — and the author is
// recorded so a reader can tell an owner's handoff from a bystander's
// observation.
func (s *Store) AddNote(taskID, agent, body string) (Note, error) {
	// One transaction, like every writer: the file lock, a fresh load, the
	// change, the rename. See AddMany for why.
	if err := s.holdFile(); err != nil {
		return Note{}, err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	if s.data.Tasks[taskID] == nil {
		return Note{}, fmt.Errorf("task %q not found", taskID)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Note{}, errors.New("note content is required")
	}
	if len(body) > 32<<10 {
		return Note{}, errors.New("note content exceeds 32768 bytes")
	}
	next := cloneState(s.data)
	next.NextID++
	note := Note{
		ID: fmt.Sprintf("n-%08x", next.NextID), TaskID: taskID,
		Agent: strings.TrimSpace(agent), Body: body, At: s.now().UTC(),
	}
	next.Notes = append(next.Notes, note)
	if err := s.commitLocked(next); err != nil {
		return Note{}, err
	}
	return note, nil
}

// Notes answers one task's notes, newest last, with a bound so a task that
// accumulated a transcript's worth cannot become one.
func (s *Store) Notes(taskID string, limit int) []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var notes []Note
	for _, note := range s.data.Notes {
		if note.TaskID != taskID {
			continue
		}
		notes = append(notes, note)
		if len(notes) >= limit {
			break
		}
	}
	return notes
}

// AddContext records a run-wide fact. Kinds are freeform because the rust
// CLI's are — the doctrine says `--kind decision` and the store takes the
// word at face value.
func (s *Store) AddContext(taskID, kind, content string) (ContextEntry, error) {
	if err := s.holdFile(); err != nil {
		return ContextEntry{}, err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ContextEntry{}, errors.New("context content is required")
	}
	if len(content) > 32<<10 {
		return ContextEntry{}, errors.New("context content exceeds 32768 bytes")
	}
	if len(s.data.Contexts) >= 4096 {
		return ContextEntry{}, errors.New("context entry limit reached")
	}
	if taskID != "" && s.data.Tasks[taskID] == nil {
		return ContextEntry{}, fmt.Errorf("task %q not found", taskID)
	}
	if kind == "" {
		kind = "discovery"
	}
	next := cloneState(s.data)
	next.NextID++
	entry := ContextEntry{
		ID: fmt.Sprintf("c-%08x", next.NextID), TaskID: taskID, Kind: kind,
		Content: content, CreatedAt: s.now().UTC(),
	}
	next.Contexts = append(next.Contexts, entry)
	if err := s.commitLocked(next); err != nil {
		return ContextEntry{}, err
	}
	return entry, nil
}

// Contexts answers the run's context entries, newest first, bounded and
// filterable the way the CLI's `contexts --kind` filters.
func (s *Store) Contexts(taskID, kind string, limit int) []ContextEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entries := make([]ContextEntry, 0, limit)
	for i := len(s.data.Contexts) - 1; i >= 0 && len(entries) < limit; i-- {
		entry := s.data.Contexts[i]
		if taskID != "" && entry.TaskID != taskID {
			continue
		}
		if kind != "" && entry.Kind != kind {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// Prune removes one context entry by id. It is the CLI's own verb over its
// own store, and the id it names is the id AddContext answered.
func (s *Store) Prune(id string) error {
	if err := s.holdFile(); err != nil {
		return err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	for i, entry := range s.data.Contexts {
		if entry.ID != id {
			continue
		}
		next := cloneState(s.data)
		next.Contexts = append(next.Contexts[:i:i], next.Contexts[i+1:]...)
		return s.commitLocked(next)
	}
	return fmt.Errorf("context %q not found", id)
}

func (s *Store) Summary() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return summarize(s.data)
}

// Tasks answers every task in admission order, copies. The reading verbs —
// overview, status, list — render from this.
func (s *Store) Tasks() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]*Task, 0, len(s.data.Order))
	for _, id := range s.data.Order {
		tasks = append(tasks, cloneTask(s.data.Tasks[id]))
	}
	return tasks
}

// CanFinalize says whether the run is over: the root's descendants all
// terminal, and a plain-word reason naming the open ones when they are not.
func (s *Store) CanFinalize() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.data.Tasks[s.data.RootID]
	if root == nil {
		return false, "plan root is missing"
	}
	var open []string
	for _, id := range s.data.Order {
		if id == s.data.RootID {
			continue
		}
		task := s.data.Tasks[id]
		if !terminal(task.Status) {
			open = append(open, fmt.Sprintf("%s(%s)", id, task.Status))
		}
	}
	if len(open) == 0 {
		return true, ""
	}
	if len(open) > 8 {
		open = append(open[:8], fmt.Sprintf("+%d more", len(open)-8))
	}
	return false, "open plan tasks remain: " + strings.Join(open, ", ")
}

// CompleteRoot ends the run. Only the runtime calls it, and only when
// nothing is open; the word it writes is the run's own account of itself.
// One transaction, like every writer: the file lock, a fresh load, the
// change, the rename. See AddMany for why.
func (s *Store) CompleteRoot(result string) error {
	if err := s.holdFile(); err != nil {
		return err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	root := s.data.Tasks[s.data.RootID]
	if root == nil || terminal(root.Status) {
		return nil
	}
	if hasOpenDescendants(s.data, root.ID) {
		return errors.New("root has open descendants")
	}
	next := cloneState(s.data)
	root = next.Tasks[next.RootID]
	now := s.now().UTC()
	root.Status = StatusDone
	for _, task := range next.Tasks {
		if task.ID != root.ID && task.Status != StatusDone {
			root.Status = StatusFailed
			break
		}
	}
	root.Result, root.UpdatedAt, root.CompletedAt = result, now, now
	return s.commitLocked(next)
}

// Search answers the tasks, notes and context entries whose words match the
// query, best first. The ranking is simple term overlap rather than the rust
// CLI's BM25 — the CLI contract is "ranked results", and what ranks them is
// the store's own choice so long as the same query answers the same order.
func (s *Store) Search(query string, limit int) []SearchResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var results []SearchResult
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		score := scoreText(terms, task.Title) * 4
		score += scoreText(terms, task.Description)
		if score > 0 {
			results = append(results, SearchResult{Kind: "task", ID: task.ID, Score: score,
				Title: task.Title, Detail: firstLine(task.Description)})
		}
	}
	for _, note := range s.data.Notes {
		if score := scoreText(terms, note.Body); score > 0 {
			results = append(results, SearchResult{Kind: "note", ID: note.ID, Score: score,
				TaskID: note.TaskID, Detail: firstLine(note.Body)})
		}
	}
	for _, entry := range s.data.Contexts {
		if score := scoreText(terms, entry.Content); score > 0 {
			results = append(results, SearchResult{Kind: "context", ID: entry.ID, Score: score,
				Detail: firstLine(entry.Content)})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Kind < results[j].Kind
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// SearchResult is one ranked answer: what kind of thing it is, where it
// lives, the line that matched, and the score that ranked it.
type SearchResult struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	TaskID string `json:"task_id,omitempty"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Score  int    `json:"-"`
}

// CriticalPath answers the longest chain of hard dependencies between
// unfinished tasks, upstream first. Empty is the honest answer for a plan
// with no such chain — one task, or all parallel — and the doctrine teaches
// it as "prioritize this", which a missing answer must never fake.
func (s *Store) CriticalPath() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	children := map[string][]string{}
	hasUpstream := map[string]bool{}
	for _, task := range s.data.Tasks {
		for _, dep := range task.Dependencies {
			if dep.Kind == DepSuggests {
				continue
			}
			children[dep.TaskID] = append(children[dep.TaskID], task.ID)
			hasUpstream[task.ID] = true
		}
	}
	var best []string
	var walk func(id string, seen map[string]bool, chain []string)
	walk = func(id string, seen map[string]bool, chain []string) {
		if len(chain) > len(best) {
			best = append([]string(nil), chain...)
		}
		for _, next := range children[id] {
			if seen[next] {
				continue
			}
			seen[next] = true
			walk(next, seen, append(chain, next))
			delete(seen, next)
		}
	}
	// The walk seeds from every task with no hard upstream, never from the
	// root: the lineage rule refuses a hard dependency on the root by design,
	// so children[root] is always empty and a root-seeded walk never leaves
	// the starting line. validateGraphs has already proved there is no cycle,
	// so the seen-set walk always ends.
	for _, id := range s.data.Order {
		if hasUpstream[id] {
			continue
		}
		seen := map[string]bool{id: true}
		walk(id, seen, []string{id})
	}
	var path []*Task
	for _, id := range best {
		path = append(path, cloneTask(s.data.Tasks[id]))
	}
	return path
}

// Bottlenecks answers the unfinished tasks whose completion unlocks the most
// downstream work, most first, bounded by the caller's limit.
func (s *Store) Bottlenecks(limit int) []BlockedCount {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	var counts []BlockedCount
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.ID == s.data.RootID || terminal(task.Status) {
			continue
		}
		downstream := reachable(s.data, task.ID, false)
		counts = append(counts, BlockedCount{Task: cloneTask(task), Downstream: len(downstream)})
	}
	sort.SliceStable(counts, func(i, j int) bool {
		if counts[i].Downstream != counts[j].Downstream {
			return counts[i].Downstream > counts[j].Downstream
		}
		return counts[i].Task.ID < counts[j].Task.ID
	})
	if len(counts) > limit {
		counts = counts[:limit]
	}
	return counts
}

// BlockedCount is one bottleneck row: the task and how much it holds up.
type BlockedCount struct {
	Task       *Task `json:"task"`
	Downstream int   `json:"downstream"`
}

// NextID mints one short id the store has never used, in the rust CLI's
// shape: `t-` + four base-36 characters. Collision is retried, not mapped
// around — four characters is 1.6 million spellings and a plan is bounded
// far below that.
func (s *Store) NextID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextIDLocked()
}

func (s *Store) nextIDLocked() string {
	alphabet := "0123456789abcdefghijklmnopqrstuvwxyz"
	for {
		id := make([]byte, 4)
		for i := range id {
			id[i] = alphabet[rand.Intn(len(alphabet))]
		}
		if s.data.Tasks[string(id)] == nil {
			return string(id)
		}
	}
}

// ClaimNext claims the highest-priority ready leaf for an agent, or answers
// nil when nothing is ready. It is the `go` verb's whole body, and it is one
// transaction rather than a read plus a claim because two agents asking at
// once must not both be handed the same task — the read and the write happen
// under the same hold of both locks, which is the only shape that guarantees
// it.
func (s *Store) ClaimNext(agent string) (*Task, error) {
	if strings.TrimSpace(agent) == "" {
		return nil, errors.New("agent is required for claim")
	}
	if err := s.holdFile(); err != nil {
		return nil, err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	var best *Task
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.Status != StatusReady || task.Composite || len(executionBlockReasons(s.data, task)) > 0 {
			continue
		}
		if best == nil || task.Priority > best.Priority {
			best = task
		}
	}
	if best == nil {
		return nil, nil
	}
	next := cloneState(s.data)
	task := next.Tasks[best.ID]
	now := s.now().UTC()
	task.Status, task.ClaimedBy, task.UpdatedAt = StatusRunning, strings.TrimSpace(agent), now
	if err := s.commitLocked(next); err != nil {
		return nil, err
	}
	return cloneTask(s.data.Tasks[best.ID]), nil
}

func (s *Store) holdFile() error {
	f, err := lockFile(s.path)
	if err != nil {
		return err
	}
	if err := lockExclusive(f); err != nil {
		_ = f.Close()
		return err
	}
	s.mu.Lock()
	s.locked = f
	s.mu.Unlock()
	return nil
}

func (s *Store) releaseFile() {
	s.mu.Lock()
	f := s.locked
	s.locked = nil
	s.mu.Unlock()
	if f == nil {
		return
	}
	_ = unlockExclusive(f)
	_ = f.Close()
}

func (s *Store) changeTask(id string, change func(*state, *Task, time.Time) error) (*Task, error) {
	if err := s.holdFile(); err != nil {
		return nil, err
	}
	defer s.releaseFile()
	s.mu.Lock()
	defer s.mu.Unlock()
	if fresh, err := loadState(s.path); err == nil {
		s.data = fresh
	}
	if s.data.Tasks[id] == nil {
		return nil, fmt.Errorf("task %q not found", id)
	}
	next := cloneState(s.data)
	task := next.Tasks[id]
	if err := change(&next, task, s.now().UTC()); err != nil {
		return nil, err
	}
	if err := s.commitLocked(next); err != nil {
		return nil, err
	}
	return cloneTask(s.data.Tasks[id]), nil
}

// commitLocked persists the next state and adopts it. Called with the mutex
// held AND the file lock held — the file lock is what orders the other
// processes, and this function is the only writer on the rename road.
func (s *Store) commitLocked(next state) error {
	if err := saveState(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func normalizeSpec(spec TaskSpec, rootID string) TaskSpec {
	spec.ID = strings.TrimSpace(strings.TrimPrefix(spec.ID, "t-"))
	spec.Title = strings.TrimSpace(spec.Title)
	spec.ParentID = strings.TrimSpace(strings.TrimPrefix(spec.ParentID, "t-"))
	if spec.ParentID == "" {
		spec.ParentID = rootID
	}
	if spec.Kind == "" {
		spec.Kind = "generic"
	}
	if spec.Effect == "" {
		spec.Effect = EffectObserve
	}
	if spec.Parallel == "" {
		spec.Parallel = "safe"
	}
	if spec.Isolation == "" {
		spec.Isolation = "shared"
	}
	spec.Capabilities = cleanStrings(spec.Capabilities)
	spec.Resources = cleanResourceClaims(spec.Resources)
	spec.ContextInputs = cleanStrings(spec.ContextInputs)
	spec.Deliverables = cleanStrings(spec.Deliverables)
	spec.EvidenceRequirements = cleanStrings(spec.EvidenceRequirements)
	for i := range spec.Dependencies {
		spec.Dependencies[i].TaskID = strings.TrimSpace(strings.TrimPrefix(spec.Dependencies[i].TaskID, "t-"))
		if spec.Dependencies[i].Kind == "" {
			spec.Dependencies[i].Kind = DepFeedsInto
		}
	}
	return spec
}

// validateSpec is what the CLI itself can see, no more. The aforge-v1 gate
// demanded a role, deliverables and acceptance on every task; the rust
// `plandb add "t" --description "d"` creates a task with none of them, so the
// gate would have refused the reference's own doctrine sentence. The graph
// laws are asked separately, in validateGraphs, and they stay whole.
func validateSpec(spec TaskSpec) error {
	if !validID(spec.ID) {
		return fmt.Errorf("invalid id %q", spec.ID)
	}
	if spec.Title == "" || len(spec.Title) > 240 {
		return errors.New("title is required and must be at most 240 characters")
	}
	if len(spec.Description) > 32<<10 {
		return errors.New("description must not exceed 32768 bytes")
	}
	if spec.Priority < -1000 || spec.Priority > 1000 {
		return fmt.Errorf("priority %d is outside -1000..1000", spec.Priority)
	}
	if !oneOf(string(spec.Effect), string(EffectObserve), string(EffectReversibleWrite), string(EffectExternalAction), string(EffectIrreversible), string(EffectMixed)) {
		return fmt.Errorf("invalid effect %q", spec.Effect)
	}
	if !oneOf(spec.Parallel, "safe", "serial") {
		return fmt.Errorf("invalid parallel policy %q", spec.Parallel)
	}
	if !oneOf(spec.Isolation, "shared", "snapshot", "exclusive") {
		return fmt.Errorf("invalid isolation policy %q", spec.Isolation)
	}
	for _, resource := range spec.Resources {
		if resource.URI == "" || !oneOf(resource.Mode, "read", "write", "exclusive") {
			return fmt.Errorf("invalid resource claim %#v", resource)
		}
	}
	for _, dep := range spec.Dependencies {
		if !validID(dep.TaskID) || !oneOf(string(dep.Kind), string(DepFeedsInto), string(DepBlocks), string(DepSuggests)) {
			return errors.New("invalid dependency")
		}
	}
	return nil
}

func validateGraphs(value state) error {
	if err := detectCycle(value, func(task *Task) []string {
		var out []string
		for _, dep := range task.Dependencies {
			if dep.Kind != DepSuggests {
				out = append(out, dep.TaskID)
			}
		}
		return out
	}); err != nil {
		return fmt.Errorf("dependency graph: %w", err)
	}
	if err := detectCycle(value, func(task *Task) []string {
		if task.ParentID == "" {
			return nil
		}
		return []string{task.ParentID}
	}); err != nil {
		return fmt.Errorf("containment graph: %w", err)
	}
	// THE LINEAGE RULE IS A WRITTEN DIVERGENCE. The rust CLI's concept blurb
	// says dependencies cross containment boundaries freely; this store refuses
	// a HARD edge across a lineage because its readiness walks the parent
	// chain and a cross-lineage hard edge makes promotion and readiness two
	// different words for the same question. `suggests` crosses freely, and
	// the doctrine's split grammar (deps_on names siblings) never needs the
	// hard form across lineages.
	for _, task := range value.Tasks {
		for _, dep := range task.Dependencies {
			if dep.Kind == DepSuggests {
				continue
			}
			if ancestorOf(value, task.ID, dep.TaskID) || ancestorOf(value, dep.TaskID, task.ID) {
				return fmt.Errorf("task %q has hard dependency %q across its containment lineage", task.ID, dep.TaskID)
			}
		}
	}
	return nil
}

func detectCycle(value state, edges func(*Task) []string) error {
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("cycle includes %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, next := range edges(value.Tasks[id]) {
			if err := visit(next); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range value.Tasks {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func promote(value *state, now time.Time) {
	changed := true
	for changed {
		changed = false
		for _, id := range value.Order {
			task := value.Tasks[id]
			if task.Status == StatusPending && depsDone(*value, task) {
				task.Status, task.UpdatedAt, changed = StatusReady, now, true
			}
		}
		// A COMPOSITE TASK AUTO-COMPLETES when its children are all terminal
		// and all done — the reference's own promise, and the half the
		// doctrine's parents rely on to finish without a worker ever touching
		// them.
		for _, id := range value.Order {
			task := value.Tasks[id]
			if id == value.RootID || !task.Composite || terminal(task.Status) || !allChildrenTerminal(*value, id) {
				continue
			}
			task.Status = StatusDone
			for _, child := range value.Tasks {
				if child.ParentID == id && child.Status != StatusDone {
					task.Status = StatusFailed
					break
				}
			}
			task.UpdatedAt, task.CompletedAt, changed = now, now, true
		}
	}
}

// depsDone is the readiness rule: every hard dependency of the task AND of
// each of its ancestors is done. The ancestor half is what makes the
// containment graph part of scheduling — a child cannot run out from under
// an unfinished parent's coordination.
func depsDone(value state, task *Task) bool {
	current := task
	for current != nil {
		for _, dep := range current.Dependencies {
			if dep.Kind == DepSuggests {
				continue
			}
			upstream := value.Tasks[dep.TaskID]
			if upstream == nil || upstream.Status != StatusDone {
				return false
			}
		}
		current = value.Tasks[current.ParentID]
	}
	return true
}

func allChildrenDone(value state, id string) bool {
	hasChildren := false
	for _, child := range value.Tasks {
		if child.ParentID != id {
			continue
		}
		hasChildren = true
		if child.Status != StatusDone {
			return false
		}
	}
	return hasChildren
}

func allChildrenTerminal(value state, id string) bool {
	hasChildren := false
	for _, child := range value.Tasks {
		if child.ParentID != id {
			continue
		}
		hasChildren = true
		if !terminal(child.Status) {
			return false
		}
	}
	return hasChildren
}

func hasOpenDescendants(value state, rootID string) bool {
	for _, task := range value.Tasks {
		if task.ID == rootID {
			continue
		}
		if !terminal(task.Status) {
			return true
		}
	}
	return false
}

// executionBlockReasons answers why a ready task still cannot start. The
// aforge-v1 port asked an eligibility ladder here (unresolved effects,
// unclaimed resources); the CLI has no verbs for any of that, so the ladder
// went with it and what remains is conflict with running work — the one
// reason this store can still state in the CLI's own words.
func executionBlockReasons(value state, task *Task) []string {
	var reasons []string
	for _, other := range value.Tasks {
		if other.ID == task.ID || other.ID == value.RootID || (other.Status != StatusClaimed && other.Status != StatusRunning) {
			continue
		}
		if executionConflict(task, other) {
			reasons = append(reasons, "conflicts with active task "+other.ID)
		}
	}
	return reasons
}

func executionConflict(left, right *Task) bool {
	if left.Parallel == "serial" && right.Parallel == "serial" {
		return true
	}
	if left.Isolation == "exclusive" || right.Isolation == "exclusive" {
		return true
	}
	for _, a := range left.Resources {
		for _, b := range right.Resources {
			if a.Mode == "read" && b.Mode == "read" {
				continue
			}
			if resourceOverlap(a.URI, b.URI) {
				return true
			}
		}
	}
	return false
}

func resourceOverlap(left, right string) bool {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if left == "" || right == "" {
		return true
	}
	if left == right || left == "*" || right == "*" {
		return true
	}
	if matched, _ := path.Match(left, right); matched {
		return true
	}
	if matched, _ := path.Match(right, left); matched {
		return true
	}
	leftPrefix := strings.TrimSuffix(strings.TrimSuffix(left, "/**"), "/*")
	rightPrefix := strings.TrimSuffix(strings.TrimSuffix(right, "/**"), "/*")
	return strings.HasPrefix(leftPrefix+"/", rightPrefix+"/") || strings.HasPrefix(rightPrefix+"/", leftPrefix+"/")
}

func requireOwner(task *Task, agent string) error {
	agent = strings.TrimSpace(agent)
	if task.ClaimedBy == "" {
		return fmt.Errorf("task %q is not claimed", task.ID)
	}
	if agent == "" || agent != task.ClaimedBy {
		return fmt.Errorf("task %q is owned by %q", task.ID, task.ClaimedBy)
	}
	return nil
}

func applyPatch(spec *TaskSpec, patch TaskPatch) {
	if patch.Title != nil {
		spec.Title = *patch.Title
	}
	if patch.Description != nil {
		spec.Description = *patch.Description
	}
	if patch.Kind != nil {
		spec.Kind = *patch.Kind
	}
	if patch.Priority != nil {
		spec.Priority = *patch.Priority
	}
	if patch.Capabilities != nil {
		spec.Capabilities = append([]string(nil), (*patch.Capabilities)...)
	}
	if patch.Resources != nil {
		spec.Resources = append([]ResourceClaim(nil), (*patch.Resources)...)
	}
	if patch.Effect != nil {
		spec.Effect = *patch.Effect
	}
	if patch.Parallel != nil {
		spec.Parallel = *patch.Parallel
	}
	if patch.Isolation != nil {
		spec.Isolation = *patch.Isolation
	}
	if patch.Role != nil {
		spec.Role = *patch.Role
	}
	if patch.ContextInputs != nil {
		spec.ContextInputs = append([]string(nil), (*patch.ContextInputs)...)
	}
	if patch.Deliverables != nil {
		spec.Deliverables = append([]string(nil), (*patch.Deliverables)...)
	}
	if patch.EvidenceRequirements != nil {
		spec.EvidenceRequirements = append([]string(nil), (*patch.EvidenceRequirements)...)
	}
	if patch.Agent != nil {
		spec.Agent = *patch.Agent
	}
	if patch.Acceptance != nil {
		spec.Acceptance = *patch.Acceptance
	}
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func cleanResourceClaims(values []ResourceClaim) []ResourceClaim {
	result := make([]ResourceClaim, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value.URI = strings.TrimSpace(value.URI)
		value.Mode = strings.TrimSpace(value.Mode)
		key := value.Mode + "\x00" + value.URI
		if value.URI != "" && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}

func ancestorOf(value state, ancestor, descendant string) bool {
	for current := value.Tasks[descendant]; current != nil && current.ParentID != ""; current = value.Tasks[current.ParentID] {
		if current.ParentID == ancestor {
			return true
		}
	}
	return false
}

func cancelBlockedDependents(value *state, failedID, reason string, now time.Time) {
	changed := true
	for changed {
		changed = false
		for _, task := range value.Tasks {
			if terminal(task.Status) || task.ID == value.RootID {
				continue
			}
			for _, dep := range task.Dependencies {
				if dep.Kind == DepSuggests {
					continue
				}
				upstream := value.Tasks[dep.TaskID]
				if dep.TaskID == failedID || (upstream != nil && upstream.Status == StatusCancelled) {
					task.Status, task.Error, task.ClaimedBy = StatusCancelled, reason, ""
					task.UpdatedAt, task.CompletedAt, changed = now, now, true
					break
				}
			}
		}
	}
}

func cancelDescendants(value *state, parentID, reason string, now time.Time) {
	changed := true
	parents := map[string]bool{parentID: true}
	for changed {
		changed = false
		for _, task := range value.Tasks {
			if !parents[task.ParentID] || terminal(task.Status) {
				continue
			}
			task.Status, task.Error, task.ClaimedBy = StatusCancelled, reason, ""
			task.UpdatedAt, task.CompletedAt = now, now
			parents[task.ID], changed = true, true
		}
	}
}

func summarize(value state) Summary {
	result := Summary{Project: value.Project, RootID: value.RootID, Total: len(value.Tasks)}
	for _, task := range value.Tasks {
		switch task.Status {
		case StatusPending:
			result.Pending++
		case StatusReady:
			result.Ready++
		case StatusClaimed, StatusRunning:
			result.Running++
		case StatusDone:
			result.Done++
		case StatusFailed:
			result.Failed++
		case StatusCancelled:
			result.Cancelled++
		}
	}
	return result
}

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	copyTask := *task
	copyTask.Dependencies = append([]Dependency(nil), task.Dependencies...)
	copyTask.Capabilities = append([]string(nil), task.Capabilities...)
	copyTask.Resources = append([]ResourceClaim(nil), task.Resources...)
	copyTask.ContextInputs = append([]string(nil), task.ContextInputs...)
	copyTask.Deliverables = append([]string(nil), task.Deliverables...)
	copyTask.EvidenceRequirements = append([]string(nil), task.EvidenceRequirements...)
	copyTask.Artifacts = append([]string(nil), task.Artifacts...)
	copyTask.Evidence = append([]string(nil), task.Evidence...)
	return &copyTask
}

func terminal(status Status) bool {
	return status == StatusDone || status == StatusFailed || status == StatusCancelled
}

func validID(id string) bool {
	return idPattern.MatchString(strings.TrimSpace(strings.TrimPrefix(id, "t-")))
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

// reachable answers the tasks that (hard-)depend on id, directly or through
// others. blocked=true follows the dependents of a cancelled task; false
// follows what becomes ready when it completes.
func reachable(value state, id string, _ bool) []string {
	var out []string
	seen := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, task := range value.Tasks {
			if seen[task.ID] {
				continue
			}
			for _, dep := range task.Dependencies {
				if dep.Kind == DepSuggests || dep.TaskID != current {
					continue
				}
				seen[task.ID] = true
				out = append(out, task.ID)
				queue = append(queue, task.ID)
				break
			}
		}
	}
	return out
}

// searchTerms splits a query into lowercase words worth matching. Punctuation
// is not worth matching and a stopword is not either.
func searchTerms(query string) []string {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '_' || r == '-')
	})
	stops := map[string]bool{"the": true, "a": true, "an": true, "of": true, "and": true, "to": true, "in": true, "for": true, "on": true, "is": true, "it": true}
	var terms []string
	seen := map[string]bool{}
	for _, word := range words {
		if len(word) < 2 || stops[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	return terms
}

func scoreText(terms []string, text string) int {
	if text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	score := 0
	for _, term := range terms {
		count := strings.Count(lower, term)
		if count > 0 {
			score += count
		}
	}
	return score
}

func firstLine(text string) string {
	if line := strings.SplitN(text, "\n", 2)[0]; strings.TrimSpace(line) != "" {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(text)
}
