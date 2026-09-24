package plandb

import (
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ErrClosed is what every method that can refuse answers once Close has
// released the store: each write, and each read whose answer the database
// holds rather than this handle's memory. IT IS THE SEAM A WORKER THAT
// OUTLIVED ITS RUN LANDS ON — the spend row, the trajectory ending and the
// completion such a worker still owes come back as a refusal a best-effort
// writer drops, rather than as a nil handle to dereference.
var ErrClosed = errors.New("plan store is closed")

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
	// closed is set by Close under the lock. Every method asks it there before
	// it touches a handle Close may already have released — the nil `db` and
	// `rdb` below, which used to be what a late write found.
	closed bool
	// db is the SQLite handle every read-modify-write transaction runs on. One
	// connection per store (openDatabase), so the pragmas are set once and a
	// transaction never races its own store for the write lock.
	db *sql.DB
	// rdb is the handle every read runs on, a second connection in DEFERRED
	// transactions (openReadDatabase). It is what lets a read answer the last
	// committed plan — another process's write included — and run beside a
	// writer holding the write lock rather than behind it.
	rdb *sql.DB
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
	rdb, err := openReadDatabase(path)
	if err != nil {
		_ = db.Close()
		store.db = nil
		return nil, err
	}
	store.rdb = rdb
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

// TaskDir is the folder one task's record lives in beside the store: the
// trajectory the run's worker appends its steps to and the transcript and
// spill files the session seat leaves both land there, so one task's page is
// one folder a person can open. The store's own path is the one root both
// roads derive it from — the session seat reads it off the store path it was
// given, the run off the store it drives — and a second spelling of the
// layout would be two answers to where a task's record is.
func TaskDir(storeDir, id string) string {
	return path.Join(storeDir, "tasks", id)
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
// CLI mints short random ones — `t-` + six base-36 characters — and honours
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
	s.refresh()
	var tasks []*Task
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.Status != StatusReady || task.Composite || task.Waiting || pausedInLineage(s.data, task) || len(executionBlockReasons(s.data, task)) > 0 {
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
	s.refresh()
	filter := firstFilter(filters)
	result := ReadySet{}
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if !filter.admits(task.Project, task.Chat) {
			continue
		}
		if task.Status != StatusReady || task.Composite || task.Waiting || pausedInLineage(s.data, task) {
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
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
	s.refresh()
	task := s.data.Tasks[id]
	if task == nil {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return cloneTask(task), nil
}

// RoleOf answers the seat a task's shape gives it at the moment it is asked,
// never the seat it was born with: a task with children is a coordinator and
// answers plan, and a leaf answers the role it was declared with, which is
// work unless add or split was told otherwise. Reading the shape rather than a
// stored guess is what lets a leaf that splits move up to the plan seat
// without anyone configuring it, and fall back to its own role once the
// archive has taken its children away.
func (s *Store) RoleOf(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.errIfClosed(); err != nil {
		return "", err
	}
	s.refresh()
	id = strings.TrimSpace(strings.TrimPrefix(id, "t-"))
	task := s.data.Tasks[id]
	if task == nil {
		return "", fmt.Errorf("task %q not found", id)
	}
	return roleOf(task), nil
}

// roleOf is the one rule RoleOf and the CLI's renders share, as a free
// function so a caller that already holds a task can ask it without the lock.
func roleOf(task *Task) string {
	if task.Composite {
		return RolePlan
	}
	if task.Role != "" {
		return task.Role
	}
	return RoleWork
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
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
	s.refresh()
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
//
// THE OWNER IS THE PROCESS, AND IT IS OPTIONAL. Dispatch is per process, so
// the run supervisor names the process that holds the claim —
// "<hostname>:<pid>" — in an argument beside the agent, and every pass
// touches that claim's seen-at stamp. A claim made without an owner (the
// CLI's own `go`, the session graph's dispatch) has no process behind it, and
// the claim's agent stands in for one so the seen-at stamp is never left
// empty.
func (s *Store) Claim(id, agent string, owner ...string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if task.Status != StatusReady || task.Composite {
			return fmt.Errorf("task %q is not a runnable ready leaf", id)
		}
		if strings.TrimSpace(agent) == "" {
			return errors.New("agent is required for claim")
		}
		who := strings.TrimSpace(agent)
		if len(owner) > 0 && strings.TrimSpace(owner[0]) != "" {
			who = strings.TrimSpace(owner[0])
		}
		task.Status, task.ClaimedBy, task.UpdatedAt = StatusRunning, strings.TrimSpace(agent), now
		task.Owner, task.SeenAt = who, now
		return nil
	})
}

// ClaimWake restores ownership to a ready composite the supervisor is waking.
// Ordinary Claim remains leaf-only; this narrow road exists so a coordinator
// that released its claim with Wait can use Wait or Done on its next turn.
func (s *Store) ClaimWake(id, agent string, owner ...string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if task.Status != StatusReady || !task.Composite {
			return fmt.Errorf("task %q is not a ready composite", id)
		}
		if strings.TrimSpace(agent) == "" {
			return errors.New("agent is required for claim")
		}
		who := strings.TrimSpace(agent)
		if len(owner) > 0 && strings.TrimSpace(owner[0]) != "" {
			who = strings.TrimSpace(owner[0])
		}
		task.Status, task.ClaimedBy, task.UpdatedAt = StatusRunning, strings.TrimSpace(agent), now
		task.Owner, task.SeenAt = who, now
		return nil
	})
}

// AddReviewCheck seats a review check beneath its parent WHATEVER THE PARENT HAS
// WRITTEN ABOUT ITSELF MEANWHILE. A finished piece of work is reviewed when its
// worker's return reaches the run, and that can be after the task above it has
// written its own done: a worker still in its turn may finish the moment its
// children's rows read done, and the store admits that because every child it
// has IS finished. The review then has to go beneath a task that reads done,
// which [Store.AddMany] refuses for every child, and rightly.
//
// A TASK IS NOT DONE UNTIL THE REVIEWS BENEATH IT HAVE LANDED, whoever wrote its
// ending. So this one transaction adds the check and moves every done ancestor
// back to waiting on it, each keeping the result it earned. The root is
// completed again by the run once the tree is whole ([Store.CompleteRoot]); a
// task between closes the way any composite nobody is working closes, when its
// children are all terminal ([promote]). NOTHING BUT A CHECK COMES IN THIS WAY:
// every other child of a terminal task continues to be refused by AddMany.
func (s *Store) AddReviewCheck(spec TaskSpec) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	err := s.transact(func(next *state, now time.Time) error {
		spec = normalizeSpec(spec, next.RootID)
		id = spec.ID
		if err := validateSpec(spec); err != nil {
			return err
		}
		if spec.Role != RoleCheck {
			return errors.New("only a review check is seated this way: the child must have role check")
		}
		if next.Tasks[spec.ID] != nil {
			return fmt.Errorf("duplicate task id %q", spec.ID)
		}
		parent := next.Tasks[spec.ParentID]
		if parent == nil {
			return fmt.Errorf("parent %q does not exist", spec.ParentID)
		}
		check := &Task{TaskSpec: spec, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
		next.Tasks[spec.ID] = check
		next.Order = append(next.Order, spec.ID)
		for ancestor := parent; ancestor != nil; ancestor = next.Tasks[ancestor.ParentID] {
			ancestor.Composite = true
			if ancestor.Status == StatusDone {
				ancestor.Status = StatusPending
				ancestor.CompletedAt = time.Time{}
			}
			ancestor.UpdatedAt = now
		}
		promote(next, now)
		return validateGraphs(*next)
	})
	if err != nil {
		return nil, err
	}
	return cloneTask(s.data.Tasks[id]), nil
}

// refuseParked is the one statement of a law three roads keep: A PARKED TASK IS
// FINISHED ONLY BY A WORKER WOKEN FOR IT. [Store.Wait] flags the task and
// releases its claim, and [Store.Wake] is the only place the flag is cleared,
// immediately before the run launches the task's worker again. So an ending
// that arrives while the flag is set comes from a worker that has already left:
// a round it had started before it parked still ends its tool, and when that
// tool is the task's own finish the store used to admit it. On the run's own
// task that closed a run over a child nobody had reviewed, and it left the pair
// nothing legitimate produces, done and still waiting.
//
// The refusal changes nothing else. The task stays parked with its claim
// released, and it is woken the ordinary way when its wait is over. The sentence
// is for the late worker, which can act on it by doing nothing more. [Store.Done],
// [Store.Fail] and [Store.CompleteRoot] all ask here, because every one of them
// writes an ending and an ending spelled three times is a law with three versions.
func refuseParked(task *Task) error {
	if task == nil || !task.Waiting {
		return nil
	}
	return fmt.Errorf("task %q is waiting and can only be finished after it is woken", task.ID)
}

// Done completes a task its agent owns. The root is the runtime's, exactly as
// the earlier port had it: a worker cannot finish the run, only its own task.
//
// THE ONE EXCEPTION IS THE ROOT'S OWN WORKER. The run's root is handed to a
// worker like every other task ([internal/run]'s supervisor launches it), and
// on this belt that worker finishes by naming its task's id — which for the
// root is `root`. It is the same act every other worker performs on its own
// task, and refusing it would leave the root's worker with nothing to end the
// loop on. So the root may be completed by an agent whose name IS the root id,
// and by nothing else: the run's own completion stays [Store.CompleteRoot]'s,
// and a worker that is not the root's own is still refused.
func (s *Store) Done(id, agent, result string, artifacts, evidence []string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if err := refuseParked(task); err != nil {
			return err
		}
		text := strings.TrimSpace(result)
		// A REVIEW CONCLUSION CARRIES ITS BASIS WITH IT, written by the same
		// gate that judged it: a holds conclusion is refused unless every
		// declared check has a recorded zero-exit audited run ([checkVerdictBasis]),
		// and whatever the verdict is, its basis is recorded on the node so a
		// later reader sees how it was earned without reopening the trajectory.
		if task.Role == RoleCheck && isReviewConclusion(text) {
			basis, earned := s.checkVerdictBasis(task)
			if strings.HasPrefix(text, "holds:") && !earned {
				return errors.New("holds conclusion requires every declared Checks: command to have a recorded zero-exit audited run")
			}
			task.VerdictBasis = basis
		}
		if len(result) > 64<<10 {
			return errors.New("completion result exceeds 65536 bytes")
		}
		rootWorker := id == next.RootID && strings.TrimSpace(agent) == id
		if id == next.RootID && !rootWorker {
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
			// THE ROOT IS NEVER CLAIMED, so its worker cannot answer the ownership
			// check every other task's worker does. The root's own worker is named
			// instead, above, and the finish law still holds: the root cannot close
			// while a child or a hard dependency is open.
			if !rootWorker {
				if err := requireOwner(task, agent); err != nil {
					return err
				}
			}
			if ok, reason := canFinish(*next, task); !ok {
				return errors.New(reason)
			}
			if !rootWorker {
				switch task.Status {
				case StatusClaimed, StatusRunning:
				default:
					return fmt.Errorf("task %q cannot complete from status %s", id, task.Status)
				}
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

func isReviewConclusion(result string) bool {
	result = strings.TrimSpace(result)
	return strings.HasPrefix(result, "holds:") || strings.HasPrefix(result, "does not hold:")
}

// checkVerdictBasis reads HOW a review verdict was earned off the check task's
// own record, and whether a holds conclusion is EARNED by it. With nothing
// declared the verdict is a reading one and reading can always hold
// ([TestReadingCanHoldAndDoesNotHoldIsUngated]); with a declaration, every
// declared command must have a recorded zero-exit run AND must pass the same
// read-only audit law the checker's door applies, because a command merely
// present in some earlier record proves none of those facts. The recorded
// exits are exactly what the persisted basis carries, runs included, so the
// proof is on the node rather than in a file a reader would have to reopen.
func (s *Store) checkVerdictBasis(task *Task) (VerdictBasis, bool) {
	if len(task.Checks) == 0 {
		return VerdictBasis{Kind: "reading"}, true
	}
	recorded, newBuild, hasRecord := s.recordedRuns(task)
	if !newBuild {
		if !hasRecord {
			// A DECLARATION WITH NOTHING OBSERVED AT ALL cannot hold. The
			// trajectory is missing or empty, so no declared command was seen to
			// run: the purest unproven declaration, distinct from an old record,
			// which has lines and only lacks the exit marker. It is named as
			// unobserved and refused.
			return VerdictBasis{Kind: "reading", Unobserved: append([]string(nil), task.Checks...)}, false
		}
		// An old record, from before command exits were recorded: lines but no
		// marker. There is no run to judge and a reader cannot tell a refused or
		// unrun check from a passing one, so the verdict is a reading one, which
		// can hold, and it names the declared checks it never observed so no
		// reader mistakes it for a holds earned by running them.
		return VerdictBasis{Kind: "reading", Unobserved: append([]string(nil), task.Checks...)}, true
	}
	seen := make(map[string]bool, len(task.Checks))
	runs := make([]VerdictRun, 0, len(task.Checks))
	earned := true
	for _, raw := range task.Checks {
		command := strings.TrimSpace(raw)
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		if !auditableDeclaredCheck(command) {
			earned = false
		}
		exit, ran := recorded[command]
		if !ran {
			earned = false
			continue
		}
		runs = append(runs, VerdictRun{Command: command, ExitCode: exit})
		if exit != 0 {
			earned = false
		}
	}
	return VerdictBasis{Kind: "run", Runs: runs}, earned
}

// recordedRuns is the checker's own record as a map from command to exit code:
// every `step` the trajectory holds, with the cd wrapper the belt writes around
// a worker command stripped the same way the session's own reading stripped it.
// The LAST exit for a command wins, because a command re-run is a later fact
// about the same check.
func (s *Store) recordedRuns(task *Task) (map[string]int, bool, bool) {
	out := map[string]int{}
	data, err := os.ReadFile(filepath.Join(TaskDir(filepath.Dir(s.path), task.ID), "trajectory.jsonl"))
	if err != nil {
		// No trajectory file at all: nothing was observed. This is not an old
		// record, which has lines and only lacks the exit marker, but the absence
		// of any record. A new build cannot reach here, because a worker stamps
		// its opening line before any step and fails the run if that write fails,
		// so an empty record under a declaration is an unproven declaration.
		return out, false, false
	}
	// newBuild is true when this record was written by a build that records
	// command exits: a step carried an exit, or any line was stamped
	// exits_recorded. internal/run/trajectory.go stamps that on the opening line
	// (before any step) and on the ending line, so a record cut off mid-run is
	// still known to be new; keep the field name in step with that writer. A
	// record with no exit and no stamp is genuinely old, and the reader must not
	// treat its silence as a passing run.
	newBuild := false
	hasRecord := false
	for _, line := range strings.Split(string(data), "\n") {
		var step struct {
			Kind          string `json:"kind"`
			Command       string `json:"command"`
			ExitCode      *int   `json:"exit_code"`
			ExitsRecorded bool   `json:"exits_recorded"`
		}
		if json.Unmarshal([]byte(line), &step) != nil {
			continue
		}
		// Any parsed line is an observation, so the record exists; this is not the
		// empty-record case even when no line carries an exit or a marker.
		hasRecord = true
		// Any line a build stamped, opening or ending, marks the record new even
		// when no step ran or the run was cut off before its ending.
		if step.ExitsRecorded {
			newBuild = true
		}
		if step.Kind != "step" {
			continue
		}
		// AN ABSENT EXIT IS UNKNOWN, NOT ZERO. A step the recorder could not stamp
		// decodes to a nil pointer here and is not a recorded run at all.
		if step.ExitCode == nil {
			continue
		}
		newBuild = true
		command := strings.TrimSpace(step.Command)
		// The belt wraps a worker command in a cd to the ABSOLUTE root of the
		// task's own copy. Only that wrapper is stripped, so a check the checker
		// typed with its own relative cd is a different command and counts as one,
		// which is the ruling that a check runs from the root of the task's copy.
		if prefix, inner, ok := strings.Cut(command, " && "); ok {
			fields := strings.Fields(prefix)
			if len(fields) == 2 && fields[0] == "cd" && strings.HasPrefix(fields[1], "/") {
				command = strings.TrimSpace(inner)
			}
		}
		out[command] = *step.ExitCode
	}
	return out, newBuild, hasRecord
}

// auditableDeclaredCheck is the store's half of the check door's law, asked of
// a persisted declared check before a holds verdict may rest on it. It is the
// SAME three questions the proposal door asks ([declaredCheckList] in
// internal/session): one command in shape, a first word with something in it
// besides wildcards, nothing that starts with an option, and no shell
// composition, that a blanket-allow gate would still let run. A second
// reading here would be a second door, which is why every half is asked of the
// same functions the session asks: [approval.Vouchable], the blanket-allow
// policy whose critical table is the build's floor, and the one-command
// reader [approval.FirstCompositionOutsideQuotes] the door reads with too.
func auditableDeclaredCheck(command string) bool {
	if !approval.Vouchable(command) || auditAllowAll.CheckBash(command).Action != approval.ActionAllow {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 || strings.HasPrefix(fields[0], "-") || strings.Trim(fields[0], "*?[]") == "" {
		return false
	}
	if _, composed := approval.FirstCompositionOutsideQuotes(command); composed {
		return false
	}
	return true
}

// auditAllowAll is the read-only checker's own policy ([auditAllowed] in
// internal/session), restated here because the store and the door must agree
// about what a critical command is or the gate and the door drift apart.
var auditAllowAll = approval.Policy{Default: approval.ActionAllow}

// SetVerdictBasis records HOW a task's verdict was earned on the task itself.
// Done writes it by the same gate that judged the verdict; this store method
// is the seam a reader with a basis it already holds writes through, and the
// basis persists with the row.
func (s *Store) SetVerdictBasis(id string, basis VerdictBasis) (*Task, error) {
	return s.changeTask(id, func(_ *state, task *Task, _ time.Time) error {
		task.VerdictBasis = basis
		return nil
	})
}

// Fail marks a task failed by its owner, with a reason the next reader sees.
func (s *Store) Fail(id, agent, message string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if err := refuseParked(task); err != nil {
			return err
		}
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
		task.Owner, task.SeenAt = "", time.Time{}
		promote(next, now)
		return nil
	})
}

// heldStatus reports whether a task's status is one an owner holds: claimed
// or running. Every read of a live claim — the stale scan, the take-over's
// release — asks it rather than spelling the two words out.
func heldStatus(status Status) bool {
	return status == StatusClaimed || status == StatusRunning
}

// Wait parks a task whose worker cannot go on: the claim is released, the task
// stays open and NOT done, and the runtime launches it again through its wake
// road when its wait is over — once every dependency is done and every child has
// finished, or at once if one of them failed or was cancelled. THE PARK IS A
// WAIT ON SOMETHING, so a task with nothing open to wait on — no dependency
// that is not done, no child that is not terminal — is refused, and a worker
// cannot park forever on nothing. A parked task keeps a non-terminal status,
// so it counts as open for every finish law: a parent cannot complete while a
// parked child stands, and a dependent cannot complete while a parked
// dependency stands.
func (s *Store) Wait(id, agent string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		// THE ROOT IS NEVER CLAIMED, so its own worker parks it the way it
		// finishes it — by naming the root's id — and every other caller is
		// held to the claim it does not have.
		rootWorker := id == next.RootID && strings.TrimSpace(agent) == id
		if !rootWorker {
			if err := requireOwner(task, agent); err != nil {
				return err
			}
			if !heldStatus(task.Status) {
				return fmt.Errorf("task %q is not claimed or running", id)
			}
		}
		if len(openWaits(*next, task)) == 0 {
			return fmt.Errorf("task %q has nothing to wait for — no open dependency, no open child", id)
		}
		task.Waiting, task.WaitedAt = true, now
		task.ClaimedBy, task.Owner, task.SeenAt = "", "", time.Time{}
		task.Status = StatusPending
		promote(next, now)
		return nil
	})
}

// OpenWaits names what a task is still waiting on, from the store's own
// current state: every dependency whose upstream is not done, and every child
// that is not terminal. It is the wait's other half — a parked task's wait is
// OVER exactly when this answers nothing — and the runtime reads it there
// rather than re-deriving the rule (internal/run's waitMoved). Sorted, so the
// reason a refusal names is the same on every call.
func (s *Store) OpenWaits(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	task := s.data.Tasks[id]
	if task == nil {
		return nil
	}
	return openWaits(s.data, task)
}

// openWaits names what a task is waiting on: every dependency whose upstream is
// not done, and every child that is not terminal. Sorted, so the reason a
// refusal names is the same on every call.
func openWaits(value state, task *Task) []string {
	var out []string
	for _, dep := range task.Dependencies {
		upstream := value.Tasks[dep.TaskID]
		if upstream == nil || upstream.Status != StatusDone {
			out = append(out, dep.TaskID)
		}
	}
	for _, id := range value.Order {
		child := value.Tasks[id]
		if child.ParentID == task.ID && !terminal(child.Status) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Wake clears a parked task's wait flag, the other half of [Store.Wait]: the
// runtime calls it when the task's wait is over — nothing it waited on is open
// any more, or one of them failed or was cancelled — immediately before it
// launches the task's worker again. It moves NOTHING ELSE — not the
// status, and it does not promote. A leaf keeps the status [Wait] left it (Ready
// where its hard dependencies are done, so the launch's claim answers the
// ownership check, and Pending where one is still open, so the launch is
// dropped and the ordinary frontier brings the task back once it clears). A
// composite is launched without a claim and needs no status; promoting here
// would let the store auto-complete a composite the instant its last child
// landed, before the worker it is being woken for can integrate them.
func (s *Store) Wake(id string) (*Task, error) {
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if !task.Waiting {
			return nil
		}
		task.Waiting, task.WaitedAt = false, time.Time{}
		task.UpdatedAt = now
		return nil
	})
}

// TouchClaims refreshes the seen-at stamp of every task an owner holds, so a
// live process's claims never read as stale. It is the supervisor's heartbeat
// — called once per pass — and it touches the seen-at column alone: the
// UpdatedAt moment every other write moves is what a waiting reader's Changed
// reads, and a heartbeat is not work the plan did, so refreshing a live claim
// must not wake anybody. An owner holding nothing writes nothing.
func (s *Store) TouchClaims(owner string) (int, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	if !s.ownsHeld(owner) {
		return 0, nil
	}
	touched := 0
	err := s.transact(func(next *state, now time.Time) error {
		touched = 0
		for _, id := range next.Order {
			task := next.Tasks[id]
			if task.Owner != owner || !heldStatus(task.Status) {
				continue
			}
			task.SeenAt = now
			touched++
		}
		if touched == 0 {
			return errNoChange
		}
		return nil
	})
	return touched, err
}

// ownsHeld reports whether owner holds any claim in the loaded plan, so the
// heartbeat skips its write when it has nothing to refresh.
func (s *Store) ownsHeld(owner string) bool {
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.Owner == owner && heldStatus(task.Status) {
			return true
		}
	}
	return false
}

// StaleClaims answers the tasks a process holds without touching them: a
// claimed or running task whose owner was last seen longer ago than the
// window, so a take-over knows which claims a dead process left behind. THE
// ROOT IS NOT ONE OF THEM — it is the run itself, claimed by the runtime and
// never a process's to take over — and a task that is not currently held is
// not stale either, however long ago it was last touched.
func (s *Store) StaleClaims(olderThan time.Duration) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	cutoff := s.now().UTC().Add(-olderThan)
	var stale []Task
	for _, id := range s.data.Order {
		task := s.data.Tasks[id]
		if task.ID == s.data.RootID || !heldStatus(task.Status) {
			continue
		}
		if task.SeenAt.Before(cutoff) {
			stale = append(stale, *cloneTask(task))
		}
	}
	return stale
}

// Pause holds a task, and by inheritance everything under it, out of the
// ready frontier without changing a single status: the flag is what readiness
// reads, not a rung of the ladder. It is the runtime's and a person's hold,
// never a worker's — the bare lifecycle verb stays refused to workers — and
// the root, which is the run itself, is nobody's to pause.
func (s *Store) Pause(id string) (*Task, error) {
	return s.hold(id, true)
}

// Resume releases a hold Pause set. Resuming a task that was not paused
// changes nothing and reports no error, so a caller may call it without
// asking first.
func (s *Store) Resume(id string) (*Task, error) {
	return s.hold(id, false)
}

// hold is the one road both Pause and Resume take: refuse the root, set the
// flag to the wanted value, and leave an already-settled task alone.
func (s *Store) hold(id string, paused bool) (*Task, error) {
	id = strings.TrimSpace(strings.TrimPrefix(id, "t-"))
	return s.changeTask(id, func(next *state, task *Task, now time.Time) error {
		if task.ID == next.RootID {
			return errors.New("the harness owns the root task")
		}
		if task.Paused == paused {
			return errNoChange
		}
		task.Paused, task.UpdatedAt = paused, now
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
		task.Owner, task.SeenAt = "", time.Time{}
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
		task.Owner, task.SeenAt = "", time.Time{}
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
		if task.Status != StatusPending && task.Status != StatusReady && !questionOnlyPatch(patch) {
			return fmt.Errorf("task %q can only be revised before execution", id)
		}
		applyPatch(&task.TaskSpec, patch)
		task.TaskSpec = normalizeSpec(task.TaskSpec, next.RootID)
		if task.ID == next.RootID {
			task.ParentID = ""
		}
		if err := validateSpec(task.TaskSpec); err != nil {
			return err
		}
		task.UpdatedAt = now
		return nil
	})
}

func questionOnlyPatch(patch TaskPatch) bool {
	return patch.Question != nil && patch.Title == nil && patch.Description == nil && patch.Kind == nil &&
		patch.Priority == nil && patch.Capabilities == nil && patch.Resources == nil && patch.Effect == nil &&
		patch.Parallel == nil && patch.Isolation == nil && patch.Role == nil && patch.ContextInputs == nil &&
		patch.Deliverables == nil && patch.EvidenceRequirements == nil && patch.Agent == nil &&
		patch.Acceptance == nil && patch.Checks == nil
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

// A NOTE'S AUTHOR IS ONE OF TWO HANDS: a worker leaving a handoff for the
// next worker, or the person steering the run. The store records which, and
// nothing else about the note changes with it.
const (
	NoteFromWorker = "worker"
	NoteFromPerson = "person"
)

// AddNote leaves a task-scoped message. The note is public to every worker on
// the run — the CLI's notes listing prints all of them — and the author is
// recorded so a reader can tell an owner's handoff from a bystander's
// observation.
func (s *Store) AddNote(taskID, agent, body string) (Note, error) {
	return s.addNote(taskID, agent, body, NoteFromWorker)
}

// AddPersonNote leaves a note in the person's own voice. It is AddNote with
// the author taken to be the person and no agent name; the CLI prints it with
// a `person:` prefix, and it is the same store row, because a note's home is
// the task either way.
func (s *Store) AddPersonNote(taskID, body string) (Note, error) {
	return s.addNote(taskID, "", body, NoteFromPerson)
}

// addNote is the one road both note writers take: validate, mint an id, and
// stamp the change on the task the note hangs on.
func (s *Store) addNote(taskID, agent, body, from string) (Note, error) {
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
			Agent: strings.TrimSpace(agent), Body: text, At: now, From: from,
			Project: next.Tasks[taskID].Project, Chat: next.Tasks[taskID].Chat,
		}
		next.Notes = append(next.Notes, note)
		// A note is a change to the task it hangs on, so it moves the task's
		// own updated_at too — the field a waiting worker's wake reads beside
		// the note's timestamp.
		next.Tasks[taskID].UpdatedAt = now
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
	s.refresh()
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

// Changed answers the ids of the tasks that moved since a moment: the task
// row itself, a note left on it, or a context entry scoped to it. It is what
// a waiting worker's wake reads for its one early return — the child or
// dependency that ended failed or cancelled since the park ([internal/run]'s
// waitMoved), not the whole wake: a move that is not a landing is not a reason
// to come back. One read over the three places a task's state lives, and it
// names which tasks to look at again, never what changed about them. The ids
// are the store's bare spelling, in admission order, and a task created after
// the moment counts as changed because its own row is newer than the moment.
func (s *Store) Changed(since time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	moved := map[string]bool{}
	for _, note := range s.data.Notes {
		if note.At.After(since) {
			moved[note.TaskID] = true
		}
	}
	for _, entry := range s.data.Contexts {
		if entry.TaskID != "" && entry.CreatedAt.After(since) {
			moved[entry.TaskID] = true
		}
	}
	var ids []string
	for _, id := range s.data.Order {
		if moved[id] || s.data.Tasks[id].UpdatedAt.After(since) {
			ids = append(ids, id)
		}
	}
	return ids
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
		// A task-scoped entry is a change to that task, so it moves the
		// task's updated_at; a run-wide entry has no task to move.
		if taskID != "" {
			next.Tasks[taskID].UpdatedAt = now
		}
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
	s.refresh()
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
	s.refresh()
	result := summarize(s.data)
	if byProject, byChat, err := s.spendTotals(); err == nil {
		if len(byProject) > 0 {
			result.ProjectSpend = byProject
		}
		if len(byChat) > 0 {
			result.ChatSpend = byChat
		}
	}
	return result
}

// Tasks answers every task in admission order, copies, narrowed to the tags a
// filter names. The reading verbs — overview, status, list — render from this.
func (s *Store) Tasks(filters ...Filter) []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
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
	s.refresh()
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
		if err := refuseParked(root); err != nil {
			return err
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

// StopRoot ends the run on a person's word. Only the runtime calls it, the way
// only the runtime calls [Store.CompleteRoot], and it is the one ending of the
// run's own task that does not wait for the work under it: the run's task and
// every task still open are cancelled in one transaction, and every task that
// had already ended keeps the ending it has.
//
// EVERY OPEN TASK IS ENDED, NOT ONLY THE ONES A CASCADE WOULD REACH. Every task
// in a store is the run's, so the walk is over the store and not down the tree:
// a cascade that follows cancelled parents stops at a parent that ended earlier
// and would leave the open work under it to be offered to the next worker.
//
// A RUN LEFT OPEN IS A RUN THE NEXT HAND-OFF ADOPTS, which is why a stop has to
// be written here and cannot only be a context somebody cut: a store whose run
// task is still open is picked up again by the next run over it, stopped work
// included. Two presses are one stop, and a run that ended by itself is left as
// it ended.
func (s *Store) StopRoot(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transact(func(next *state, now time.Time) error {
		root := next.Tasks[next.RootID]
		if root == nil || terminal(root.Status) {
			return errNoChange
		}
		reason = strings.TrimSpace(reason)
		for _, task := range next.Tasks {
			if terminal(task.Status) {
				continue
			}
			task.Status, task.Error, task.ClaimedBy = StatusCancelled, reason, ""
			task.Owner, task.SeenAt = "", time.Time{}
			task.UpdatedAt, task.CompletedAt = now, now
		}
		promote(next, now)
		return nil
	})
}

// FailRoot ends the run because the run's own task failed: its worker came
// home with an error and nothing of the run is still working. Only the runtime
// calls it, the way only the runtime calls [Store.CompleteRoot] and
// [Store.StopRoot]. The run's task is failed with the reason, and every other
// task still open is cancelled with it, in one transaction; a task that had
// already ended keeps its ending. No result is written: a result is what a
// finished run delivers, and a failed worker's account is not one.
//
// A FAILED RUN WAS LEFT OPEN, AND AN OPEN RUN READS AS RUNNING. Nothing wrote
// the ending of a run whose own worker failed, so its store said `running` for
// ever: the task's page drew `running` and offered `stop it` over a program
// that had ended forty minutes earlier, and the next hand-off would have
// adopted the dead run's store as live work ([Store.StopRoot] says why an open
// run is adopted). A run that already ended is left as it ended.
func (s *Store) FailRoot(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transact(func(next *state, now time.Time) error {
		root := next.Tasks[next.RootID]
		if root == nil || terminal(root.Status) {
			return errNoChange
		}
		reason = strings.TrimSpace(reason)
		for _, task := range next.Tasks {
			if terminal(task.Status) || task.ID == root.ID {
				continue
			}
			task.Status, task.Error, task.ClaimedBy = StatusCancelled, reason, ""
			task.Owner, task.SeenAt = "", time.Time{}
			task.UpdatedAt, task.CompletedAt = now, now
		}
		root.Status, root.Error, root.ClaimedBy = StatusFailed, reason, ""
		root.Owner, root.SeenAt = "", time.Time{}
		root.UpdatedAt, root.CompletedAt = now, now
		promote(next, now)
		return nil
	})
}

// Archive moves whole finished subtrees out of the live plan and into the
// archive: a task and every task under it, when each one has been terminal —
// done, cancelled or failed — for longer than the window. The moved tasks
// leave Tasks, ReadySet and Search whole, so nothing that reads the plan sees
// them again, and Archived reads the rows back whole. Two things are kept
// honest: a subtree a still-live task depends on stays in the plan, because
// an edge to a task that is gone would break the next open, and every
// surviving parent's composite flag is recomputed, because a parent whose
// last child left is no longer composite. The root is never archived: it is
// the run.
func (s *Store) Archive(olderThan time.Duration) ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.beginWrite()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	fresh, err := loadState(tx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	doomed, moved := archiveSelection(fresh, now.Add(-olderThan))
	if len(moved) == 0 {
		s.data = fresh
		return nil, nil
	}
	for _, task := range moved {
		task.ArchivedAt = now
	}
	if err := insertArchived(tx, moved, now); err != nil {
		return nil, err
	}
	for id := range doomed {
		delete(fresh.Tasks, id)
	}
	fresh.Order = keepIDs(fresh.Order, doomed)
	fresh.Notes = keepNotes(fresh.Notes, doomed)
	fresh.Contexts = keepContexts(fresh.Contexts, doomed)
	recomputeComposite(&fresh)
	if err := saveState(tx, fresh); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.data = fresh
	return moved, nil
}

// Archived lists the tasks the archive holds, in admission order, each
// carrying the moment it was archived. It is the read behind the CLI's
// `list --archived`.
func (s *Store) Archived() ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
	return loadArchived(s.rdb)
}

// archiveSelection chooses the maximal finished subtrees older than the
// cutoff. A task is a candidate when it is terminal, its whole subtree is
// terminal, and every task in it has been terminal since before the cutoff;
// a candidate whose parent is itself a candidate is not a root, because the
// parent's move already carries it. Each root's subtree is then kept only when
// no still-live task depends on anything in it.
func archiveSelection(value state, cutoff time.Time) (map[string]bool, []*Task) {
	children := map[string][]string{}
	for _, id := range value.Order {
		children[value.Tasks[id].ParentID] = append(children[value.Tasks[id].ParentID], id)
	}
	allTerminal := map[string]bool{}
	latest := map[string]time.Time{}
	var walk func(id string)
	walk = func(id string) {
		if _, seen := allTerminal[id]; seen {
			return
		}
		task := value.Tasks[id]
		allTerminal[id] = terminal(task.Status)
		// A terminal task's moment is when it completed; the fallback is the
		// last write that touched it, which is what an older store carries.
		latest[id] = task.CompletedAt
		if latest[id].IsZero() {
			latest[id] = task.UpdatedAt
		}
		for _, child := range children[id] {
			walk(child)
			if !allTerminal[child] {
				allTerminal[id] = false
			}
			if latest[child].After(latest[id]) {
				latest[id] = latest[child]
			}
		}
	}
	for _, id := range value.Order {
		walk(id)
	}
	eligible := func(id string) bool {
		return id != value.RootID && terminal(value.Tasks[id].Status) && allTerminal[id] && latest[id].Before(cutoff)
	}
	doomed := map[string]bool{}
	for _, id := range value.Order {
		if !eligible(id) {
			continue
		}
		if parent := value.Tasks[id].ParentID; parent != "" && eligible(parent) {
			continue
		}
		set := map[string]bool{}
		markSubtree(children, id, set)
		if referencedFromOutside(value, set) {
			continue
		}
		for member := range set {
			doomed[member] = true
		}
	}
	if len(doomed) == 0 {
		return nil, nil
	}
	var moved []*Task
	for _, id := range value.Order {
		if doomed[id] {
			moved = append(moved, cloneTask(value.Tasks[id]))
		}
	}
	return doomed, moved
}

// markSubtree collects a task and every task under it.
func markSubtree(children map[string][]string, id string, into map[string]bool) {
	if into[id] {
		return
	}
	into[id] = true
	for _, child := range children[id] {
		markSubtree(children, child, into)
	}
}

// referencedFromOutside reports whether any task outside the set depends on a
// task inside it. Such a set may not be archived: its edges would dangle.
func referencedFromOutside(value state, set map[string]bool) bool {
	for id, task := range value.Tasks {
		if set[id] {
			continue
		}
		for _, dep := range task.Dependencies {
			if set[dep.TaskID] {
				return true
			}
		}
	}
	return false
}

// recomputeComposite restores the one invariant the archive can break: a
// task's composite flag is exactly whether it still has a child in the plan.
// A parent whose last child left the plan stops being composite.
func recomputeComposite(value *state) {
	hasChild := map[string]bool{}
	for _, id := range value.Order {
		if parent := value.Tasks[id].ParentID; parent != "" {
			hasChild[parent] = true
		}
	}
	for _, id := range value.Order {
		value.Tasks[id].Composite = hasChild[id]
	}
}

func keepIDs(order []string, drop map[string]bool) []string {
	kept := order[:0]
	for _, id := range order {
		if !drop[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

func keepNotes(notes []Note, drop map[string]bool) []Note {
	kept := notes[:0]
	for _, note := range notes {
		if !drop[note.TaskID] {
			kept = append(kept, note)
		}
	}
	return kept
}

func keepContexts(entries []ContextEntry, drop map[string]bool) []ContextEntry {
	kept := entries[:0]
	for _, entry := range entries {
		if entry.TaskID == "" || !drop[entry.TaskID] {
			kept = append(kept, entry)
		}
	}
	return kept
}

// Search answers the tasks, notes and context entries whose words match the
// query, best first. The ranking is simple term overlap — the CLI contract is
// "ranked results", and what ranks them is the store's own choice so long as
// the same query answers the same order.
func (s *Store) Search(query string, limit int, filters ...Filter) []SearchResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
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
	s.refresh()
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
	s.refresh()
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

// NextID mints one short id the store has never used: `t-` + six base-36
// characters, drawn from crypto/rand. Collision is retried, not mapped
// around — six characters is over two billion spellings and a plan is bounded
// far below that.
func (s *Store) NextID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	return s.nextIDLocked()
}

func (s *Store) nextIDLocked() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	for {
		id := make([]byte, 6)
		for i := range id {
			id[i] = alphabet[randomIndex(len(alphabet))]
		}
		if s.data.Tasks[string(id)] == nil {
			return string(id)
		}
	}
}

// randomIndex draws one index into an n-symbol alphabet from crypto/rand,
// throwing away the byte values that would lean the draw toward the first
// symbols. An id is public, so the draw must not be predictable from one run
// to the next, which is why it is not math/rand.
func randomIndex(n int) int {
	limit := 256 - 256%n
	var b [1]byte
	for {
		// crypto/rand.Read never fails on a supported platform; the store's
		// ids are drawn from it and not from math/rand so two runs cannot be
		// predicted from each other.
		_, _ = cryptorand.Read(b[:])
		if int(b[0]) < limit {
			return int(b[0]) % n
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
			if task.Status != StatusReady || task.Composite || pausedInLineage(*next, task) || len(executionBlockReasons(*next, task)) > 0 {
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
		pick.Owner, pick.SeenAt = strings.TrimSpace(agent), now
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
	// A RELEASED STORE REFUSES HERE, once, for every writer there is: each
	// write ends up through this one transaction opener, so a late spend row,
	// a late completion or a late failure all answer ErrClosed rather than
	// dereferencing the nil handle Close left behind.
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
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

// errIfClosed is the refusal every method makes on a store Close has already
// released, and nil while the store is open. The caller holds the store's
// lock, which is the one moment `closed` and the two database handles are in
// a known state together: this is why the check is here rather than in each
// writer, and why a handle released while a worker still held it answers a
// word instead of dying.
func (s *Store) errIfClosed() error {
	if s.closed {
		return ErrClosed
	}
	return nil
}

// refresh adopts the last committed plan from the database into the handle's
// memory, through the read handle in a DEFERRED transaction. Every read calls
// it before answering, so a handle's answer is the plan the database holds and
// not the plan this handle last wrote itself — which is what makes two
// processes on one file agree, and is the reason Changed on one handle names a
// task the other just moved. WAL lets the snapshot run while a writer holds
// the write lock, so a read never waits behind an open write; and because the
// read is one transaction, it answers a whole committed plan and never a plan
// half-written. A refresh that cannot read keeps the memory it has: the store
// would rather answer its last good plan than fail a read it cannot report,
// and a read on a closed store is exactly that — the plan the handle last
// held, answered rather than reached for.
func (s *Store) refresh() {
	if s.closed || s.rdb == nil {
		return
	}
	tx, err := s.rdb.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	fresh, err := loadState(tx)
	if err != nil {
		return
	}
	s.data = fresh
}

// Close releases the store's database handles and marks the handle closed
// against every later call. A store opened for one pass — the runtime opens
// one per pulse — must be closed when the pass is done, or every pass would
// leave a connection and a file descriptor behind.
//
// A CLOSED STORE REFUSES; IT NEVER PANICS. Close is what a caller does the
// moment a run is over, and the run's own workers are the last writers to
// reach for the handle: every method that carries an error — each write,
// through the one transaction opener, and each read the database answers
// rather than this handle's memory (Show, RoleOf, Resolve, Archived) —
// answers ErrClosed afterwards, and the reads that carry none (Tasks, Notes,
// the summaries and rollups) answer the plan the handle last held instead of
// reaching for a handle that is gone. Closing a store twice is not an error:
// a caller that closes in a defer and again on the ending road says the same
// thing both times.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	err := error(nil)
	if s.rdb != nil {
		err = s.rdb.Close()
		s.rdb = nil
	}
	if s.db != nil {
		if closeErr := s.db.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		s.db = nil
	}
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
	if spec.Role == "" {
		spec.Role = RoleWork
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
	// An empty role is not a fifth seat: normalizeSpec defaults it to work
	// before validation, and a row a store made before the seat was read
	// carries the empty word, so it is let through and answered as work.
	if spec.Role != "" && !validRole(spec.Role) {
		return roleRefusal()
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

// roleWords names the four seats in the order every refusal lists them.
var roleWords = []string{RolePlan, RoleWork, RoleCheck, RoleProbe}

// validRole reports whether word is one of the four the store may carry as a
// seat. The empty word is not one of them: the default is applied before
// validation, so an empty word is a row made before the seat was read.
func validRole(word string) bool {
	return oneOf(word, roleWords...)
}

// roleRefusal is the one sentence that names the four seats, so the store's
// own validation and the CLI's own flag refuse with the same words.
func roleRefusal() error {
	return fmt.Errorf("role must be one of %s", strings.Join(roleWords, ", "))
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
		//
		// A COMPOSITE A WORKER IS HOLDING IS NOT THE STORE'S TO CLOSE. A parent
		// whose own worker is claimed or running has a result still to be written
		// — its children's landings wake it, and it reports them — so the store
		// leaves it open and the run writes its ending (internal/run's supervisor).
		// A composite nobody is working — a parent that never had a worker of its
		// own — still auto-completes here, which is what lets a store-only plan
		// finish without a seat for every coordinator.
		for _, id := range value.Order {
			task := value.Tasks[id]
			if id == value.RootID || !task.Composite || task.Waiting || terminal(task.Status) || heldStatus(task.Status) || !allChildrenTerminal(*value, id) {
				continue
			}
			task.Status = StatusDone
			for _, child := range value.Tasks {
				if child.ParentID == id && child.Status != StatusDone {
					// FAILED IS FOR A CHILD THAT DID NOT LAND DONE, AND THE REASON
					// NAMES IT. A composite whose children all landed done is never
					// closed failed; when one did not, the next reader — the
					// coordinator's own re-plan — is told which child and how it
					// ended.
					task.Status = StatusFailed
					if strings.TrimSpace(task.Error) == "" {
						task.Error = fmt.Sprintf("child %q %s", child.ID, child.Status)
					}
					break
				}
			}
			task.UpdatedAt, task.CompletedAt, changed = now, now, true
		}
	}
}

// pausedInLineage reports whether the task or any ancestor of it is paused.
// Pause is inherited down the containment tree without touching a single
// status, so every readiness read consults it beside the status ladder: a
// paused subtree leaves the frontier whole while the tasks in it keep the
// status they had. The walk is the same parent chain the ready ladder uses,
// and it ends at the root, whose parent is the empty string.
func pausedInLineage(value state, task *Task) bool {
	for current := task; current != nil; current = value.Tasks[current.ParentID] {
		if current.Paused {
			return true
		}
	}
	return false
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

// CanFinish reports whether a task may be completed now, and when it may
// not, the reason naming the first task in the way: a child that is not
// terminal, or a hard dependency that is not done. `suggests` does not block
// — it is advice, not a gate — and the child scan walks the plan in the order
// it is carried, so the reason a refusal names is the same one on every call.
// Done refuses with this same reason.
func (s *Store) CanFinish(id string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	id = strings.TrimSpace(strings.TrimPrefix(id, "t-"))
	task := s.data.Tasks[id]
	if task == nil {
		return false, fmt.Sprintf("task %q not found", id)
	}
	return canFinish(s.data, task)
}

// canFinish is the law CanFinish reads, as a free function so Done can ask it
// against the transaction's own fresh state rather than the locked copy.
func canFinish(value state, task *Task) (bool, string) {
	for _, id := range value.Order {
		child := value.Tasks[id]
		if child.ParentID != task.ID {
			continue
		}
		if !terminal(child.Status) {
			return false, fmt.Sprintf("task %q has a child %q that has not finished", task.ID, id)
		}
	}
	for _, dep := range task.Dependencies {
		if dep.Kind == DepSuggests {
			continue
		}
		upstream := value.Tasks[dep.TaskID]
		if upstream == nil || upstream.Status != StatusDone {
			return false, fmt.Sprintf("task %q has an unfinished dependency %q", task.ID, dep.TaskID)
		}
	}
	return true, ""
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
	if patch.Question != nil {
		spec.Question = *patch.Question
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
	if patch.Checks != nil {
		spec.Checks = append([]string(nil), (*patch.Checks)...)
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

// AddSpend records one charge against a task: the model that spent it, the
// role it played, the dollars and the token counts. The ledger is append-only
// — the summary reads it back and nothing here rewrites it — so the row lives
// outside the whole-plan rewrite every other writer performs, and its own
// transaction is all it needs. Nothing in the runtime calls this yet; the
// wiring that spends is a later change.
func (s *Store) AddSpend(taskID, model, role string, usd float64, inTokens, outTokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO spend (task_id, model, role, usd, in_tokens, out_tokens, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(strings.TrimPrefix(taskID, "t-")), strings.TrimSpace(model), strings.TrimSpace(role),
		usd, inTokens, outTokens, formatTime(s.now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// spendTotals reads the ledger grouped by the tags of the task each charge
// was made against: one map keyed by project and one keyed by chat, each
// carrying the dollars and the call count under that tag. A charge whose task
// the store does not know is left out — it has no tag to be counted under.
func (s *Store) spendTotals() (map[string]SpendTotal, map[string]SpendTotal, error) {
	if err := s.errIfClosed(); err != nil {
		return nil, nil, err
	}
	byProject := map[string]SpendTotal{}
	byChat := map[string]SpendTotal{}
	rows, err := s.rdb.Query(`SELECT t.project, t.chat, SUM(s.usd), COUNT(*)
		FROM spend s JOIN tasks t ON t.id = s.task_id
		GROUP BY t.project, t.chat`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var project, chat string
		var total SpendTotal
		if err := rows.Scan(&project, &chat, &total.USD, &total.Calls); err != nil {
			return nil, nil, err
		}
		byProject[project] = addSpend(byProject[project], total)
		byChat[chat] = addSpend(byChat[chat], total)
	}
	return byProject, byChat, rows.Err()
}

// addSpend folds one group's totals into a tag's running total.
func addSpend(into, add SpendTotal) SpendTotal {
	return SpendTotal{USD: into.USD + add.USD, Calls: into.Calls + add.Calls}
}

// SpendSummary answers the ledger grouped by role and by model: the dollars
// spent and the calls that spent them under each seat and each model. The
// ledger is append-only and read here whole — one query, two groupings — so a
// seat's cost is a row drawn from every run, and a charge whose role or model
// the run never named still appears under the empty word rather than being
// dropped.
func (s *Store) SpendSummary() SpendSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := SpendSummary{ByRole: map[string]SpendTotal{}, ByModel: map[string]SpendTotal{}}
	if s.closed {
		return result
	}
	rows, err := s.rdb.Query(`SELECT role, model, SUM(usd), COUNT(*) FROM spend GROUP BY role, model`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var role, model string
		var total SpendTotal
		if err := rows.Scan(&role, &model, &total.USD, &total.Calls); err != nil {
			return result
		}
		result.ByRole[role] = addSpend(result.ByRole[role], total)
		result.ByModel[model] = addSpend(result.ByModel[model], total)
	}
	return result
}

// SpendLine is one row of a spend rollup: the key the ledger was grouped
// under — a chat, a project, a seat, a model, or the task a charge was made
// against — and the dollars, tokens and calls the rows under that key carry.
//
// MODEL AND ROLE MIRROR THE KEY ON THE MODEL AND SEAT AXES, so a reader asking
// for a model's or a seat's spend can read the word by name; on the entity
// axes — chat, project, task — a group spans several models and seats, so both
// stay empty rather than naming one of many.
type SpendLine struct {
	Key   string  `json:"key"`
	Model string  `json:"model,omitempty"`
	Role  string  `json:"role,omitempty"`
	USD   float64 `json:"usd"`
	In    int     `json:"in"`
	Out   int     `json:"out"`
	Calls int     `json:"calls"`
}

// spendAxes names the rollups SpendBy accepts: the two tags a store carries
// on every row, the two attribution columns the ledger writes, and the task a
// charge was made against.
var spendAxes = []string{"chat", "project", "seat", "model", "task"}

// spendAxisKey answers the column one axis groups on, and whether it needs the
// tasks join: chat and project read the tags of the task a charge names, while
// seat, model and task read the ledger's own columns.
func spendAxisKey(axis string) (string, bool) {
	switch axis {
	case "chat":
		return "t.chat", true
	case "project":
		return "t.project", true
	case "seat":
		return "s.role", false
	case "model":
		return "s.model", false
	case "task":
		return "s.task_id", false
	}
	return "", false
}

// SpendBy answers the ledger rolled up under one axis, heaviest key first,
// over the charges written since a moment — the zero time means the whole
// ledger. Each line is the exact sum of the rows under its key, and a key with
// no rows under it is absent, never a zero line.
//
// chat and project read the tags of the task each charge names, so a charge
// whose task the store does not hold has no tag to be counted under and is left
// out, exactly as spendTotals does. seat, model and task read the ledger's own
// columns, so a charge the store cannot place still answers under the word it
// carries. An axis that is not one of spendAxes answers nothing.
func (s *Store) SpendBy(axis string, since time.Time) []SpendLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	keyExpr, join := spendAxisKey(axis)
	if keyExpr == "" {
		return nil
	}
	// THE WINDOW IS APPLIED IN GO, not in the query: the ledger stores `at` as
	// RFC3339Nano, whose fractional digits are variable, so a text comparison
	// against a bound would misorder a whole second against its own fraction.
	// The ledger is small by design, so reading its rows and cutting them at
	// the parsed moment is both exact and cheap.
	query := `SELECT ` + keyExpr + `, s.usd, s.in_tokens, s.out_tokens, s.at FROM spend s`
	if join {
		query += ` JOIN tasks t ON t.id = s.task_id`
	}
	rows, err := s.rdb.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	grouped := map[string]*SpendLine{}
	for rows.Next() {
		var key, at string
		var usd float64
		var in, out int
		if err := rows.Scan(&key, &usd, &in, &out, &at); err != nil {
			return nil
		}
		moment, err := parseTime(at)
		if err != nil {
			return nil
		}
		if !since.IsZero() && moment.Before(since) {
			continue
		}
		line := grouped[key]
		if line == nil {
			line = &SpendLine{Key: key}
			grouped[key] = line
		}
		line.USD += usd
		line.In += in
		line.Out += out
		line.Calls++
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	lines := make([]SpendLine, 0, len(grouped))
	for _, line := range grouped {
		switch axis {
		case "model":
			line.Model = line.Key
		case "seat":
			line.Role = line.Key
		}
		lines = append(lines, *line)
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].USD != lines[j].USD {
			return lines[i].USD > lines[j].USD
		}
		return lines[i].Key < lines[j].Key
	})
	return lines
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
