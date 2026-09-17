package plandb

import (
	"database/sql"
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

// Store is the plan: one SQLite database, a mutex for this process, and a
// transaction per write that every other process serializes on. Its method set
// is the earlier port's, kept because it already answers the CLI's questions:
// the graph laws (validateGraphs below) are the port's own and were correct
// there.
//
// WHAT THE ADAPTATION TOOK OUT, deliberately, is written at the functions that
// changed: the earlier store doubled as a governance gate — validateSpec
// required a role, deliverables and acceptance on every task, and Claim refused
// a task whose effect was unresolved or that claimed no resources. The CLI has
// no flags for any of that, so every `plandb add` the doctrine teaches would
// have been refused by the port's own gates. The gates are gone; the
// graph laws stay.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	data state
	// db is the SQLite handle every read-modify-write transaction runs on. One
	// connection per store (openDatabase), so the pragmas are set once and a
	// transaction never races its own store for the write lock.
	db *sql.DB
}

// Open loads the plan at path, or creates one when the database does not exist.
//
// THE ROOT IS THE RUN: `plandb init` makes a project and the runtime runs it
// by seeding a root task for the work it was given. A store that exists but
// belongs to a different run is a refusal, not a merge: two sessions sharing
// one store by accident would each dispatch the other's children.
//
// The optional chat names the conversation the run was seeded in; it tags the
// root task, and every task, note and context entry made under the root
// inherits it. The parameter is optional so every call site that names no chat
// — a worker's own reading open, a reopen — keeps its argument list.
func Open(path, project, rootID, rootTitle, rootDescription string, chat ...string) (*Store, error) {
	store := &Store{path: path, now: time.Now}
	tag := ""
	if len(chat) > 0 {
		tag = strings.TrimSpace(chat[0])
	}
	_, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		// The symlink refusal the file writer carried is kept: a store reached
		// through a symlink is a store two paths disagree about.
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("plan store path must not be a symlink")
		}
	case os.IsNotExist(statErr):
		// A store that is not there yet has nothing to adopt, so it needs a
		// project and a root. The refusal happens before the file is made, so a
		// refused open leaves no store behind.
		if strings.TrimSpace(project) == "" || !validID(rootID) {
			return nil, errors.New("a new plan store needs a project and a valid root id")
		}
	default:
		return nil, statErr
	}
	db, err := openDatabase(path)
	if err != nil {
		return nil, err
	}
	store.db = db
	loaded, err := store.loadOrCreate(project, rootID, rootTitle, rootDescription, tag)
	if err != nil {
		_ = db.Close()
		store.db = nil
		return nil, err
	}
	store.data = loaded
	return store, nil
}

// loadOrCreate is the open's one transaction: it loads the plan if there is
// one and adopts it, or seeds a new one under the root. TWO PROCESSES CAN
// REACH THE SECOND ROAD AT ONCE — the runtime seeding, a worker's CLI init-ing
// — and BEGIN IMMEDIATE serializes them: the second re-reads and adopts the
// first one's store rather than writing its own over it. Adopting keeps the
// rule the load road states: a store that belongs to another run is a refusal,
// not a merge.
func (s *Store) loadOrCreate(project, rootID, rootTitle, rootDescription, chat string) (state, error) {
	tx, err := s.beginWrite()
	if err != nil {
		return state{}, err
	}
	defer tx.Rollback()
	loaded, err := loadState(tx)
	switch {
	case err == nil:
		if (rootID != "" && loaded.RootID != rootID) || (project != "" && loaded.Project != project) {
			return state{}, errors.New("plan store belongs to a different run")
		}
		return loaded, nil
	case !errors.Is(err, errNoStore):
		return state{}, err
	}
	if strings.TrimSpace(project) == "" || !validID(rootID) {
		return state{}, errors.New("a new plan store needs a project and a valid root id")
	}
	now := s.now().UTC()
	root := &Task{
		TaskSpec: TaskSpec{
			ID: rootID, Title: strings.TrimSpace(rootTitle), Description: rootDescription,
			Kind: "generic", Parallel: "safe", Isolation: "shared",
		},
		Status: StatusRunning, ClaimedBy: "runtime", CreatedAt: now, UpdatedAt: now,
		Project: project, Chat: chat,
	}
	fresh := state{
		Version: stateVersion, Project: project, RootID: rootID,
		Tasks: map[string]*Task{rootID: root}, Order: []string{rootID}, NextID: 1,
	}
	if err := saveState(tx, fresh); err != nil {
		return state{}, err
	}
	if err := tx.Commit(); err != nil {
		return state{}, err
	}
	return fresh, nil
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
// CLI mints short random ones — `t-` + four base-36 characters — and honours
// `--as` names. Both roads end here.
func (s *Store) AddMany(specs []TaskSpec) ([]*Task, error) {
	if len(specs) == 0 {
		return nil, errors.New("tasks must not be empty")
	}
	if len(specs) > 256 {
		return nil, errors.New("a plan may contain at most 256 tasks")
	}
	// EVERY WRITE IS ONE TRANSACTION. This store is written by more than one
	// process — the CLI's add and split are separate processes — so the batch
	// is read, validated and written inside one BEGIN IMMEDIATE transaction
	// against a fresh load, or a stale handle's write would overwrite another
	// handle's task and lose it. The store-test wave proved exactly that loss
	// with a failing test before the earlier gate went in.
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.transact(func(next *state, now time.Time) error {
		if len(next.Tasks)+len(specs) > 1024 {
			return errors.New("a run may contain at most 1024 tasks")
		}
		batch := make(map[string]bool, len(specs))
		for i := range specs {
			specs[i] = normalizeSpec(specs[i], next.RootID)
			if err := validateSpec(specs[i]); err != nil {
				return fmt.Errorf("task %d: %w", i, err)
			}
			if next.Tasks[specs[i].ID] != nil || batch[specs[i].ID] {
				return fmt.Errorf("duplicate task id %q", specs[i].ID)
			}
			batch[specs[i].ID] = true
		}
		for _, spec := range specs {
			if next.Tasks[spec.ParentID] == nil && !batch[spec.ParentID] {
				return fmt.Errorf("task %q has unknown parent %q", spec.ID, spec.ParentID)
			}
			if parent := next.Tasks[spec.ParentID]; parent != nil && terminal(parent.Status) {
				return fmt.Errorf("task %q has terminal parent %q", spec.ID, spec.ParentID)
			}
			for _, dep := range spec.Dependencies {
				if next.Tasks[dep.TaskID] == nil && !batch[dep.TaskID] {
					return fmt.Errorf("task %q has unknown dependency %q", spec.ID, dep.TaskID)
				}
				if dep.TaskID == spec.ID {
					return fmt.Errorf("task %q depends on itself", spec.ID)
				}
			}
		}
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
		if err := validateGraphs(*next); err != nil {
			return err
		}
		// TAGS ARE INHERITED FROM THE PARENT: a child's project and chat are
		// its parent task's, so a subtree carries the run it grew from. A
		// parent inside this same batch resolves through its own parent the
		// same way; the walk ends at a stored task, because the containment
		// graph has just been proved acyclic.
		born := make(map[string]bool, len(specs))
		for _, spec := range specs {
			born[spec.ID] = true
		}
		for _, spec := range specs {
			parent := spec.ParentID
			for born[parent] {
				parent = next.Tasks[parent].ParentID
			}
			if stored := next.Tasks[parent]; stored != nil {
				task := next.Tasks[spec.ID]
				task.Project, task.Chat = stored.Project, stored.Chat
			}
		}
		promote(next, now)
		return nil
	})
	if err != nil {
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
func (s *Store) ReadySet(filters ...Filter) ReadySet {
	s.mu.Lock()
	defer s.mu.Unlock()
	filter := firstFilter(filters)
	result := ReadySet{}
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if !filter.admits(task.Project, task.Chat) {
			continue
		}
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
// Ids fuzzy-match because the doctrine leans on that; a prefix that fits
// more than one task is a question the caller must not guess at.
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
// NAMING TRICK: the supervisor claims with the task's own id, so the worker
// that later finishes "as" the task can only be the worker the task was
// handed to. Ownership in Done and Fail is enforced against exactly this
// name.
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
// the earlier port had it: a worker cannot finish the run, only its own task.
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
// The cascade is the port's own law and runs unconditionally: a cancelled
// dependency is a cancelled dependent, because nothing in this store can
// resolve a hard edge whose upstream will never answer.
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
// the graph laws are asked of the whole result: a hard edge between a task and
// its own ancestor or descendant, or an edge that closes a cycle, refuses the
// edge rather than corrupting the plan. A hard edge between two branches of
// the containment tree is allowed.
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
		// now, and the same is true for every descendant whose ancestor gained
		// one. promote() owns that demotion — readiness is its law, both halves
		// of it — so a new edge only has to state itself and then promote.
		// Claimed and running work stays where it is: a task mid-flight cannot
		// be re-scoped out from under its worker by a later edge.
		task.UpdatedAt = now
		promote(next, now)
		return nil
	})
}

// RemoveDep removes one hard edge between two tasks — the insert verb's
// rewire, which replaces a direct edge with a path through a new task. It is
// a graph law like AddDep: the whole plan is asked after the edge is gone,
// and readiness is recomputed, because lifting an edge can make the downstream
// task runnable. Removing an edge that is not there is not an error: a rewire
// asked twice is the same plan.
func (s *Store) RemoveDep(downstream, upstream string) (*Task, error) {
	return s.changeTask(downstream, func(next *state, task *Task, now time.Time) error {
		kept := make([]Dependency, 0, len(task.Dependencies))
		for _, dep := range task.Dependencies {
			if dep.TaskID == upstream {
				continue
			}
			kept = append(kept, dep)
		}
		task.Dependencies = kept
		if err := validateGraphs(*next); err != nil {
			return err
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
	// One transaction, like every writer: the database's write lock, a fresh
	// load, the change, the commit. See AddMany for why.
	s.mu.Lock()
	defer s.mu.Unlock()
	var note Note
	err := s.transact(func(next *state, now time.Time) error {
		if next.Tasks[taskID] == nil {
			return fmt.Errorf("task %q not found", taskID)
		}
		text := strings.TrimSpace(body)
		if text == "" {
			return errors.New("note content is required")
		}
		if len(text) > 32<<10 {
			return errors.New("note content exceeds 32768 bytes")
		}
		next.NextID++
		note = Note{
			ID: fmt.Sprintf("n-%08x", next.NextID), TaskID: taskID,
			Agent: strings.TrimSpace(agent), Body: text, At: now,
			Project: next.Tasks[taskID].Project, Chat: next.Tasks[taskID].Chat,
		}
		next.Notes = append(next.Notes, note)
		return nil
	})
	if err != nil {
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

// AddContext records a run-wide fact. Kinds are freeform — the doctrine says
// `--kind decision` and the store takes the word at face value.
func (s *Store) AddContext(taskID, kind, content string) (ContextEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var entry ContextEntry
	err := s.transact(func(next *state, now time.Time) error {
		text := strings.TrimSpace(content)
		if text == "" {
			return errors.New("context content is required")
		}
		if len(text) > 32<<10 {
			return errors.New("context content exceeds 32768 bytes")
		}
		if len(next.Contexts) >= 4096 {
			return errors.New("context entry limit reached")
		}
		if taskID != "" && next.Tasks[taskID] == nil {
			return fmt.Errorf("task %q not found", taskID)
		}
		if kind == "" {
			kind = "discovery"
		}
		// A context entry carries the tags of the task it is scoped to, and
		// the run's own tags when it is scoped to no task: it is the run's
		// context, so it answers to the run's root.
		project, chat := "", ""
		if taskID != "" {
			project, chat = next.Tasks[taskID].Project, next.Tasks[taskID].Chat
		} else if root := next.Tasks[next.RootID]; root != nil {
			project, chat = root.Project, root.Chat
		}
		next.NextID++
		entry = ContextEntry{
			ID: fmt.Sprintf("c-%08x", next.NextID), TaskID: taskID, Kind: kind,
			Content: text, CreatedAt: now, Project: project, Chat: chat,
		}
		next.Contexts = append(next.Contexts, entry)
		return nil
	})
	if err != nil {
		return ContextEntry{}, err
	}
	return entry, nil
}

// Contexts answers the run's context entries, newest first, bounded and
// filterable the way the CLI's `contexts --kind` filters.
func (s *Store) Contexts(taskID, kind string, limit int, filters ...Filter) []ContextEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	filter := firstFilter(filters)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entries := make([]ContextEntry, 0, limit)
	for i := len(s.data.Contexts) - 1; i >= 0 && len(entries) < limit; i-- {
		entry := s.data.Contexts[i]
		if !filter.admits(entry.Project, entry.Chat) {
			continue
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transact(func(next *state, now time.Time) error {
		for i, entry := range next.Contexts {
			if entry.ID != id {
				continue
			}
			next.Contexts = append(next.Contexts[:i:i], next.Contexts[i+1:]...)
			return nil
		}
		return fmt.Errorf("context %q not found", id)
	})
}

func (s *Store) Summary() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return summarize(s.data)
}

// Tasks answers every task in admission order, copies, narrowed to the tags a
// filter names. The reading verbs — overview, status, list — render from this.
func (s *Store) Tasks(filters ...Filter) []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	filter := firstFilter(filters)
	tasks := make([]*Task, 0, len(s.data.Order))
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if !filter.admits(task.Project, task.Chat) {
			continue
		}
		tasks = append(tasks, cloneTask(task))
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
// One transaction, like every writer: the database's write lock, a fresh
// load, the change, the commit. See AddMany for why.
func (s *Store) CompleteRoot(result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transact(func(next *state, now time.Time) error {
		root := next.Tasks[next.RootID]
		if root == nil || terminal(root.Status) {
			return errNoChange
		}
		if hasOpenDescendants(*next, root.ID) {
			return errors.New("root has open descendants")
		}
		root.Status = StatusDone
		for _, task := range next.Tasks {
			if task.ID != root.ID && task.Status != StatusDone {
				root.Status = StatusFailed
				break
			}
		}
		root.Result, root.UpdatedAt, root.CompletedAt = result, now, now
		return nil
	})
}

// Search answers the tasks, notes and context entries whose words match the
// query, best first. The ranking is simple term overlap — the CLI contract is
// "ranked results", and what ranks them is the store's own choice so long as
// the same query answers the same order.
func (s *Store) Search(query string, limit int, filters ...Filter) []SearchResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	filter := firstFilter(filters)
	var results []SearchResult
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if !filter.admits(task.Project, task.Chat) {
			continue
		}
		score := scoreText(terms, task.Title) * 4
		score += scoreText(terms, task.Description)
		if score > 0 {
			results = append(results, SearchResult{Kind: "task", ID: task.ID, Score: score,
				Title: task.Title, Detail: firstLine(task.Description)})
		}
	}
	for _, note := range s.data.Notes {
		if !filter.admits(note.Project, note.Chat) {
			continue
		}
		if score := scoreText(terms, note.Body); score > 0 {
			results = append(results, SearchResult{Kind: "note", ID: note.ID, Score: score,
				TaskID: note.TaskID, Detail: firstLine(note.Body)})
		}
	}
	for _, entry := range s.data.Contexts {
		if !filter.admits(entry.Project, entry.Chat) {
			continue
		}
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

// Bottlenecks answers the unfinished tasks that hold up the most work right
// now, most first, bounded by the caller's limit. The count is the tasks that
// hard-depend on it DIRECTLY — the work one completion unblocks at once — not
// the transitive reach: a task three removes downstream is not waiting on this
// one, it is waiting on the task in between. A task nothing hard-depends on
// holds up nothing and is left out, and equal counts order by descending id.
func (s *Store) Bottlenecks(limit int) []BlockedCount {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	direct := map[string]int{}
	for _, task := range s.data.Tasks {
		for _, dep := range task.Dependencies {
			if dep.Kind == DepSuggests {
				continue
			}
			direct[dep.TaskID]++
		}
	}
	var counts []BlockedCount
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.ID == s.data.RootID || terminal(task.Status) || direct[task.ID] == 0 {
			continue
		}
		counts = append(counts, BlockedCount{Task: cloneTask(task), Downstream: direct[task.ID]})
	}
	sort.SliceStable(counts, func(i, j int) bool {
		if counts[i].Downstream != counts[j].Downstream {
			return counts[i].Downstream > counts[j].Downstream
		}
		return counts[i].Task.ID > counts[j].Task.ID
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

// NextID mints one short id the store has never used: `t-` + four base-36
// characters. Collision is retried, not mapped around — four characters is
// 1.6 million spellings and a plan is bounded far below that.
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
// inside the same transaction on the database, which is the only shape that
// guarantees it.
func (s *Store) ClaimNext(agent string) (*Task, error) {
	if strings.TrimSpace(agent) == "" {
		return nil, errors.New("agent is required for claim")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	best := ""
	err := s.transact(func(next *state, now time.Time) error {
		var pick *Task
		for _, id := range next.Order {
			task := next.Tasks[id]
			if task.Status != StatusReady || task.Composite || len(executionBlockReasons(*next, task)) > 0 {
				continue
			}
			if pick == nil || task.Priority > pick.Priority {
				pick = task
			}
		}
		if pick == nil {
			return errNoChange
		}
		best = pick.ID
		pick.Status, pick.ClaimedBy, pick.UpdatedAt = StatusRunning, strings.TrimSpace(agent), now
		return nil
	})
	if err != nil {
		return nil, err
	}
	if best == "" {
		return nil, nil
	}
	return cloneTask(s.data.Tasks[best]), nil
}

// writeLockWait bounds how long a writer will keep asking for the database's
// write lock before giving up and reporting the refusal.
const writeLockWait = 60 * time.Second

// beginWrite opens the store's one write transaction with BEGIN IMMEDIATE, so
// the database's write lock is taken at the start and everything the
// transaction reads afterwards is the plan that lock protects. The busy
// timeout set at open makes a writer wait a few seconds for the one ahead of
// it, but under many writers that fixed wait starves the unluckiest of them,
// so a refusal is asked again with a short backoff — each attempt re-enters
// the queue — until a generous bound.
func (s *Store) beginWrite() (*sql.Tx, error) {
	deadline := time.Now().Add(writeLockWait)
	for {
		tx, err := s.db.Begin()
		if err == nil {
			return tx, nil
		}
		if !isBusy(err) || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(time.Duration(rand.Intn(20)+1) * time.Millisecond)
	}
}

// transact runs one read-modify-write transaction on the store's database.
// BEGIN IMMEDIATE takes the database's write lock the moment the transaction
// opens, so two processes serialize on the database itself and no sidecar file
// is needed; the busy timeout set at open makes the second wait for the first
// rather than failing at once. The whole state is read inside the transaction
// and written back inside it, so a handle's stale memory can never erase
// another writer's task — the property the advisory lock used to buy.
//
// The store's memory adopts the fresh read even when the change is refused, so
// a refused write still leaves the handle knowing what the database holds; a
// change may return errNoChange to say it decided to write nothing at all.
func (s *Store) transact(change func(*state, time.Time) error) error {
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	fresh, err := loadState(tx)
	if err != nil {
		return err
	}
	s.data = fresh
	next := cloneState(fresh)
	if err := change(&next, s.now().UTC()); err != nil {
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	if err := saveState(tx, next); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.data = next
	return nil
}

// Close releases the store's database handle. A store opened for one pass —
// the runtime opens one per pulse — must be closed when the pass is done, or
// every pass would leave a connection and a file descriptor behind.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) changeTask(id string, change func(*state, *Task, time.Time) error) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.transact(func(next *state, now time.Time) error {
		task := next.Tasks[id]
		if task == nil {
			return fmt.Errorf("task %q not found", id)
		}
		return change(next, task, now)
	})
	if err != nil {
		return nil, err
	}
	return cloneTask(s.data.Tasks[id]), nil
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

// validateSpec is what the CLI itself can see, no more. The earlier gate
// demanded a role, deliverables and acceptance on every task; `plandb add
// "t" --description "d"` creates a task with none of them, so the gate would
// have refused the doctrine's own sentence. The graph laws are asked
// separately, in validateGraphs, and they stay whole.
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
	// THE LINEAGE RULE IS A WRITTEN DIVERGENCE, AND IT IS NARROW. A hard
	// (non-`suggests`) edge may join two tasks in different branches of the
	// containment tree: the readiness walk climbs the parent chain, so a
	// cross-branch edge gates the frontier like any other and promotion and
	// readiness still agree. The one hard edge refused is between a task and
	// its own ancestor or descendant, because that edge would have a task wait
	// on the lineage that schedules it. `suggests` crosses freely, and cycle
	// detection above runs over both graphs, so a cross-branch edge that would
	// close a loop is refused too.
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

// promote brings the ready frontier up to the truth of the graph after any
// change. It is the ONE definition of who is ready, in both directions: a
// pending task whose dependencies are all done becomes ready, and — the half
// the ancestor rule needs — a ready task that no longer satisfies that rule
// falls back to pending. Readiness and promotion cannot disagree, because
// both are asked of depsDone here: `ready` always means "this task's own hard
// dependencies and every ancestor's hard dependencies are done", and never
// the memory of a moment when that was last true.
func promote(value *state, now time.Time) {
	changed := true
	for changed {
		changed = false
		for _, id := range value.Order {
			task := value.Tasks[id]
			if task.Status == StatusReady && !depsDone(*value, task) {
				task.Status, task.UpdatedAt, changed = StatusPending, now, true
			}
		}
		for _, id := range value.Order {
			task := value.Tasks[id]
			if task.Status == StatusPending && depsDone(*value, task) {
				task.Status, task.UpdatedAt, changed = StatusReady, now, true
			}
		}
		// A COMPOSITE TASK AUTO-COMPLETES when its children are all terminal
		// and all done — the half the doctrine's parents rely on to finish
		// without a worker ever touching them.
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
// the earlier port asked an eligibility ladder here (unresolved effects,
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
