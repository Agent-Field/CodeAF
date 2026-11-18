package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE RUNNER'S HALF OF AN EXTRA TASK (internal/factory's task.go says what a
// task is and where it goes).
//
// The factory's door writes the task into the job and, when a runner holds
// the job, either lets that runner go so it pulls the job again with the task
// in place, or tells it through its lease ([Runner.Task]) that a task waits
// for the round running now to end. The job's own goroutine then puts the
// task in front of the next thing it would run, at the two places where no
// round is under way: between two steps and before a step's next round.
//
// ONLY THE JOB'S OWN GOROUTINE MOVES A PHASE, AND ONLY WHERE NO ROUND IS
// RUNNING. A round writes its step by the phase's place in the run, so a
// phase put in front of a running step would have that round's results
// written onto the wrong step.

// taskFloor is what the runner keeps about extra tasks, by job. floorLoop.mu
// guards it.
type taskFloor struct {
	// queued are the tasks a lease signal brought while a round was running,
	// oldest first, until the job's goroutine places them.
	queued map[int][]factory.Phase
	// before is each task's checkout as its first round found it, by the
	// task's name; "" for a task that had already begun where this runner
	// could not see, whose change is then taken as made.
	before map[int]map[string]string
	// shape is the names of the run's phases in the order the kept results
	// are keyed by ([floorLoop.results]).
	shape map[int][]string
	// carried are the tasks, by name, whose following step has been handed
	// its conversation back.
	carried map[int]map[string]bool
	// checked are the finished tasks, by name, whose change this runner has
	// looked at ([floorLoop.settleTasks]). A run that starts afresh keeps
	// them: a task checked in an earlier drive of the job is not checked
	// again, until it begins again.
	checked map[int]map[string]bool
}

// forget drops everything kept about a job's tasks but which it has checked:
// its run starts afresh.
func (t *taskFloor) forget(id int) {
	delete(t.queued, id)
	delete(t.before, id)
	delete(t.shape, id)
	delete(t.carried, id)
}

// AddTask is the door's write of an extra task, under the job's lock: the
// rules are applied, the task goes where the job's state sends it, and the
// job's log says who asked for what. held says a runner holds the job to run
// its steps, and now that the task was asked for at once. A refusal changes
// nothing.
func AddTask(it *factory.Item, by, words string, held, now bool, at time.Time) (factory.TaskAdded, error) {
	words = strings.TrimSpace(words)
	if err := factory.TaskRefusal(*it, words); err != nil {
		return factory.TaskAdded{}, err
	}
	// The main agent's task on a stop it may decide is one more round, under
	// the same limit as its other decisions (factory's task_handoff.go).
	if factory.TaskBy(by) == factory.TaskByMainAgent {
		if err := factory.ManagerTaskRefusal(*it); err != nil {
			return factory.TaskAdded{}, err
		}
		factory.NoteManagerTask(it)
	}
	added := factory.TaskAdded{Road: factory.TaskRoad(*it, held, now), By: factory.TaskBy(by), Step: factory.FollowPRStep}
	switch added.Road {
	case factory.TaskRunsFollowNext:
		factory.HoldFollowTask(it, by, words)
	case factory.TaskRunsFollow:
		factory.QueueFollowTask(it, by, words, at)
	default:
		ph := factory.TaskPhase(*it, by, words)
		added.Step = ph.Name
		switch added.Road {
		case factory.TaskRunsLanded:
			requeueLanded(it, ph)
		case factory.TaskRunsAfter:
			s := it.Stream
			s.Phases = factory.InsertPhase(s.Phases, factory.AfterRound(s.Phases, s.Cur), ph)
		default:
			taskBefore(it, ph, at)
		}
	}
	loopSay(it, at, "said", factory.TaskLine(by, words))
	return added, nil
}

// taskBefore puts a task before the step the job is on and queues the job:
// nothing is running that step, or the runner that was is being let go.
//
// A STOP THE JOB STOOD AT BECOMES ONE MORE ROUND AFTER THE TASK. A step out
// of rounds gets the round "try once more" gives, a failed step runs again
// as a rerun runs it, an approval is asked again once the task has run, and a
// question a round asked goes with that round, to be asked again if the step
// still needs it.
func taskBefore(it *factory.Item, task factory.Phase, now time.Time) {
	s := it.Stream
	at := nextPhase(s.Phases)
	cur := s.Cur
	on := cur >= 0 && cur < len(s.Phases)
	switch {
	case it.Failure != nil:
		at = failedStepAgain(it, now)
	case it.State == factory.StateNeedsYou && it.QKind == factory.QKindOutOfAttempts && on:
		ph := &s.Phases[cur]
		if ph.Verification == nil || ph.Verification.State != "incomplete" {
			ph.RoundLimit = max(ph.RoundLimit, ph.Round+1)
			ph.Round = max(ph.Round, 1) + 1
		}
		setPhaseState(ph, factory.PhasePending, now)
		ph.Chat = ""
		at = cur
	case it.State == factory.StateNeedsYou && it.QKind == factory.QKindApprove && on:
		setPhaseState(&s.Phases[cur], factory.PhasePending, now)
		it.Approval = nil
		at = cur
	case it.State == factory.StateNeedsYou && on && s.Phases[cur].State == factory.PhaseWaiting:
		setPhaseState(&s.Phases[cur], factory.PhaseRunning, now)
		at = cur
	}
	if at < 0 {
		at = len(s.Phases)
	}
	// TASKS RUN IN THE ORDER THEY WERE ASKED FOR. The new one goes behind
	// those already waiting in front of the step, and those that waited
	// behind the step's round go in front of it too, ahead of the new one.
	waits := func(k int) bool {
		return k < len(s.Phases) && factory.IsTask(s.Phases[k]) && s.Phases[k].State == factory.PhasePending
	}
	first := at
	for waits(at) {
		at++
	}
	for at < len(s.Phases) && waits(at+1) {
		earlier := s.Phases[at+1]
		s.Phases = append(s.Phases[:at+1:at+1], s.Phases[at+2:]...)
		s.Phases = factory.InsertPhase(s.Phases, at, earlier)
		at++
	}
	s.Phases = factory.InsertPhase(s.Phases, at, task)
	s.Cur = first
	s.Ended = time.Time{}
	it.Question, it.QKind, it.Asking = "", "", nil
	// A job its person holds stays held: the task runs when they resume it.
	if !storedPaused(*it) {
		it.State = factory.StateQueued
	}
}

// failedStepAgain stands the job's failed step to run again, as a rerun
// does, and answers its place in the run. A failure no step of the run can
// rerun (the landing's commit) is taken down, and the run goes on from its
// first unfinished step.
func failedStepAgain(it *factory.Item, now time.Time) int {
	s := it.Stream
	if name := it.Failure.Step; phaseNamed(s.Phases, name) && stepControl(it, name, true, "", "", now) == nil {
		return s.Cur
	}
	it.Failure, it.Recovery = nil, nil
	for i := range s.Phases {
		ph := &s.Phases[i]
		if ph.Failure == nil {
			continue
		}
		ph.Failure = nil
		if ph.State == factory.PhaseFailed {
			setPhaseState(ph, factory.PhasePending, now)
		}
	}
	return nextPhase(s.Phases)
}

// requeueLanded puts one more phase after the last finished one of a landed
// job and queues it again, for a request for changes and for a task alike.
// The approval the job waited for is withdrawn with the publish it would
// have made: the phase makes the next head, and that head gets its own.
func requeueLanded(it *factory.Item, ph factory.Phase) {
	s := it.Stream
	at := 0
	for k, p := range s.Phases {
		if p.State == factory.PhaseDone || p.State == factory.PhaseSkipped {
			at = k + 1
		}
	}
	s.Phases = factory.InsertPhase(s.Phases, at, ph)
	s.Cur = at
	s.Ended = time.Time{}
	it.State = factory.StateQueued
	it.PublishStatus, it.Publishing = nil, false
	it.PublishError = ""
	// The checks stay for the new step to build on; the person's override was for the run that is over.
	it.Overrides = nil
}

// landedWithTask queues a job again that landed with a task still waiting:
// the task was asked for while the run's last round was ending, too late for
// that runner to place it. It runs before anything is published.
func landedWithTask(it *factory.Item) {
	i, ok := factory.WaitingTask(*it)
	if !ok || it.Stream == nil {
		return
	}
	s := it.Stream
	task := s.Phases[i]
	s.Phases = append(s.Phases[:i:i], s.Phases[i+1:]...)
	// A landing gate the landing stood waiting waits again after the task.
	if g := factory.LandingGate(*it); g >= 0 && s.Phases[g].State == factory.PhaseWaiting {
		s.Phases[g].State = factory.PhasePending
	}
	requeueLanded(it, task)
}

// Task is the runner's door for a task the factory added while this runner
// was in the middle of a round. The task is kept until that round ends; the
// job's goroutine then puts it before the step's next round or the next
// step. A job nothing is running here keeps no task: the factory's own copy
// holds it, and the pull that takes the job next carries it.
func (r *Runner) Task(id int, name, words, by string) error {
	name, words = strings.TrimSpace(name), strings.TrimSpace(words)
	if name == "" || words == "" {
		return nil
	}
	lp := r.loop()
	lp.mu.Lock()
	defer lp.mu.Unlock()
	c := lp.ctls[id]
	if c == nil || c.finishing || c.reverify || c.ctx.Err() != nil {
		return nil
	}
	if slices.ContainsFunc(lp.tasks.queued[id], func(ph factory.Phase) bool { return ph.Name == name }) {
		return nil
	}
	if lp.tasks.queued == nil {
		lp.tasks.queued = map[int][]factory.Phase{}
	}
	lp.tasks.queued[id] = append(lp.tasks.queued[id], factory.NamedTaskPhase(name, by, words))
	return nil
}

// SetBoundary binds a leased job's way to hear its factory between two
// rounds: hear carries out whatever the factory has waiting for the job (a
// task among it) before it returns. Nil removes the binding.
//
// A TASK MUST NOT MISS THE ROUND IT WAITS FOR. What the factory has for a
// runner rides the runner's heartbeat, every two seconds; a task sent in a
// round's last two seconds would otherwise find that round over and the next
// already begun, and wait for a whole round more.
func (r *Runner) SetBoundary(id int, hear func(context.Context)) {
	if hear == nil {
		r.boundaries.Delete(id)
		return
	}
	r.boundaries.Store(id, hear)
}

// boundaryWait bounds one hearing of the factory between rounds. A factory
// that does not answer holds no round back: the task is then heard at the
// next heartbeat and runs at the boundary after it.
const boundaryWait = 5 * time.Second

// hearFactory hears the factory out before the job's goroutine looks for
// tasks to place.
func (lp *floorLoop) hearFactory(c *loopCtl) {
	hear, ok := lp.r.boundaries.Load(c.id)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, boundaryWait)
	defer cancel()
	hear.(func(context.Context))(ctx)
}

// tasksBetween runs between two steps of the job's run: a task that just
// finished has its change looked at, and the tasks asked for meanwhile go
// before the next step.
func (lp *floorLoop) tasksBetween(c *loopCtl) {
	lp.settleTasks(c)
	lp.hearFactory(c)
	lp.placeTasks(c, -1, 0, 0)
}

// taskTurn runs before each round of phase i, round being the round about to
// start and limit its loop's. It says true when it put a task in front of the
// step, which then waits: the caller goes back to the run's first unfinished
// phase, the task.
func (lp *floorLoop) taskTurn(c *loopCtl, i, round, limit int) bool {
	lp.hearFactory(c)
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil || i >= len(it.Stream.Phases) {
		return false
	}
	phases := it.Stream.Phases
	ph := phases[i]
	lp.mu.Lock()
	lp.noteShape(c.id, phases)
	waiting := len(lp.tasks.queued[c.id])
	cut := c.carry != ""
	_, seen := lp.tasks.before[c.id][ph.Name]
	lp.mu.Unlock()
	if factory.IsTask(ph) {
		if !seen {
			lp.taskBegins(c, it, ph)
		}
		// A task asked for while another runs goes after it, in order.
		return false
	}
	// A STEP A TASK CUT IN FRONT OF CARRIES ON IN ITS OWN CONVERSATION, as a
	// step a restart interrupted does ([Runner.revive]): its round was under
	// way, and what it had read and decided is in that chat.
	if !cut && c.revived == 0 && i > 0 && ph.State == factory.PhaseRunning && ph.Round == round && strings.TrimSpace(ph.Chat) != "" {
		if prev := phases[i-1]; factory.IsTask(prev) && prev.State == factory.PhaseDone {
			lp.mu.Lock()
			first := !lp.tasks.carried[c.id][prev.Name]
			if first {
				if lp.tasks.carried == nil {
					lp.tasks.carried = map[int]map[string]bool{}
				}
				if lp.tasks.carried[c.id] == nil {
					lp.tasks.carried[c.id] = map[string]bool{}
				}
				lp.tasks.carried[c.id][prev.Name] = true
				c.carry = strings.TrimSpace(ph.Chat)
			}
			lp.mu.Unlock()
			if first {
				c.revived = i + 1
				return false
			}
		}
	}
	// A round a pause or a restart cut short goes on first; the task runs
	// when that round ends.
	if waiting == 0 || cut || c.revived == i+1 {
		return false
	}
	return lp.placeTasks(c, i, round, limit)
}

// taskBegins keeps the checkout as a task's first round finds it, so its end
// can tell whether the task changed anything ([floorLoop.settleTasks]).
func (lp *floorLoop) taskBegins(c *loopCtl, it factory.Item, ph factory.Phase) {
	key := ""
	if ph.State == factory.PhasePending && ph.Round == 0 {
		key = lp.checkoutNow(it)
	}
	lp.mu.Lock()
	if lp.tasks.before == nil {
		lp.tasks.before = map[int]map[string]string{}
	}
	if lp.tasks.before[c.id] == nil {
		lp.tasks.before[c.id] = map[string]string{}
	}
	lp.tasks.before[c.id][ph.Name] = key
	delete(lp.tasks.checked[c.id], ph.Name)
	lp.mu.Unlock()
}

// placeTasks puts the tasks a signal brought into the run, before phase i,
// or before the run's first unfinished phase when i is -1. A step that had
// begun waits again at the round it was about to start, its loop as it was.
// It says whether it placed any.
func (lp *floorLoop) placeTasks(c *loopCtl, i, round, limit int) bool {
	lp.mu.Lock()
	queued := lp.tasks.queued[c.id]
	delete(lp.tasks.queued, c.id)
	lp.mu.Unlock()
	if len(queued) == 0 {
		return false
	}
	now := lp.r.now()
	at, placed := -1, 0
	err := lp.write(c, func(it *factory.Item) error {
		s := it.Stream
		// The factory's copy may have reached this runner first, on the pull
		// that began this drive: a task the run already holds is not placed
		// twice, and with none to place nothing moves.
		var fresh []factory.Phase
		for _, task := range queued {
			if !phaseNamed(s.Phases, task.Name) {
				fresh = append(fresh, task)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		at = i
		if at < 0 {
			if at = nextPhase(s.Phases); at < 0 {
				at = len(s.Phases)
			}
		}
		if at > len(s.Phases) {
			return errors.New("the step a task goes before is gone")
		}
		if i >= 0 {
			if ph := &s.Phases[i]; ph.State != factory.PhasePending || ph.Round > 0 {
				setPhaseState(ph, factory.PhasePending, now)
				ph.Round, ph.RoundLimit = round, limit
			}
		}
		for _, task := range fresh {
			s.Phases = factory.InsertPhase(s.Phases, at+placed, task)
			placed++
		}
		s.Cur = at
		return nil
	})
	if err != nil || placed == 0 {
		return false
	}
	if cur, e := lp.r.opts.Store.Get(c.id); e == nil && cur.Stream != nil {
		lp.mu.Lock()
		lp.rekey(c.id, cur.Stream.Phases)
		lp.mu.Unlock()
	}
	if c.phase >= at {
		c.phase += placed
	}
	return true
}

// settleTasks looks at each task that finished since its checkout was kept:
// a task that changed files sends the checks that had passed round again
// ([floorLoop.afterTask]), and one that changed nothing leaves the job where
// it was.
//
// A TASK THAT FINISHED WHERE THIS RUNNER DID NOT SEE IT BEGIN IS TAKEN AS
// CHANGED. Its checkout as it began is kept only in memory, and only for the
// drive that began it: a restart, or a job let go and pulled again, between
// the task's end and this look loses it. A finished task this runner has not
// checked, with nothing but tasks finished after it, is checked again rather
// than not at all ([checkMissed]).
func (lp *floorLoop) settleTasks(c *loopCtl) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil {
		return
	}
	phases := it.Stream.Phases
	for i, ph := range phases {
		if !factory.IsTask(ph) || ph.State != factory.PhaseDone {
			continue
		}
		lp.mu.Lock()
		before, ok := lp.tasks.before[c.id][ph.Name]
		delete(lp.tasks.before[c.id], ph.Name)
		checked := lp.tasks.checked[c.id][ph.Name]
		lp.mu.Unlock()
		if !ok && (checked || !checkMissed(phases, i)) {
			continue
		}
		// A checkout that could not be read, then or now, is taken as changed:
		// checks run again for nothing rather than not at all.
		after := lp.checkoutNow(it)
		lp.afterTask(c, ph.Name, !ok || before == "" || after == "" || after != before)
		lp.mu.Lock()
		if lp.tasks.checked == nil {
			lp.tasks.checked = map[int]map[string]bool{}
		}
		if lp.tasks.checked[c.id] == nil {
			lp.tasks.checked[c.id] = map[string]bool{}
		}
		lp.tasks.checked[c.id][ph.Name] = true
		lp.mu.Unlock()
	}
}

// checkMissed says the finished task at i may have had no check: nothing but
// tasks has finished after it, and the run goes on after it. Once its check
// has run, the steps it sent round again come before it, or a step after it
// has finished.
func checkMissed(phases []factory.Phase, i int) bool {
	next := nextPhase(phases)
	if next < 0 {
		next = len(phases)
	}
	if next <= i {
		return false
	}
	for k := i + 1; k < next; k++ {
		if !factory.IsTask(phases[k]) {
			return false
		}
	}
	return true
}

// afterTask is what follows a finished task. THE CHECKS THAT HAD PASSED RUN
// AGAIN WHEN THE TASK CHANGED FILES, from the first one the change could
// undo ([factory.CheckAgainFrom]) with every step between it and the task, as
// they do when a person gives back a job they worked on by hand. A step the
// task cut in front of is told, in its own conversation, that the task ran.
func (lp *floorLoop) afterTask(c *loopCtl, name string, changed bool) {
	now := lp.r.now()
	line := ""
	_ = lp.write(c, func(it *factory.Item) error {
		s := it.Stream
		i := slices.IndexFunc(s.Phases, func(ph factory.Phase) bool { return ph.Name == name })
		if i < 0 {
			return nil
		}
		if i+1 < len(s.Phases) {
			if next := &s.Phases[i+1]; next.State == factory.PhaseRunning && next.Round > 0 && strings.TrimSpace(next.Chat) != "" {
				next.Carry = append(next.Carry, taskCarry(s.Phases[i], changed))
			}
		}
		if !changed {
			return nil
		}
		from := factory.CheckAgainFrom(it.Stages, s.Phases, i)
		if from >= i {
			return nil
		}
		for k := from; k < i; k++ {
			p := s.Phases[k]
			if factory.IsTask(p) {
				continue
			}
			again := factory.Phase{Name: p.Name, Kind: p.Kind, Note: p.Note, State: factory.PhasePending, RoundCosts: p.RoundCosts, Result: p.Result, Rounds: p.Rounds}
			if p.Round > 0 {
				again.Round = p.Round + 1
			}
			s.Phases[k] = again
		}
		s.Cur = from
		it.ResetChecks()
		line = name + " changed files. Checking again from " + s.Phases[from].Name + "."
		loopSay(it, now, "said", line)
		return nil
	})
	if line != "" {
		lp.tell(c.id, line)
	}
}

// taskCarry is what a step is told, in the conversation it carries on, about
// the task that ran while it waited.
func taskCarry(task factory.Phase, changed bool) string {
	words := "While this step waited, an extra task ran in this checkout"
	if t := task.Task; t != nil {
		words += ": " + oneLine(t.Words)
	}
	words = strings.TrimRight(words, ". ") + "."
	if r := task.Result; r != nil && strings.TrimSpace(r.Output) != "" {
		words += " It reported: " + firstLine(r.Output)
	}
	if changed {
		words += " It changed files: read what changed before you go on."
	}
	return words
}

// noteShape keeps the order of the run's phases the kept results are keyed
// by. Callers hold floorLoop.mu.
func (lp *floorLoop) noteShape(id int, phases []factory.Phase) {
	if lp.tasks.shape == nil {
		lp.tasks.shape = map[int][]string{}
	}
	names := make([]string, len(phases))
	for i, ph := range phases {
		names[i] = ph.Name
	}
	lp.tasks.shape[id] = names
}

// rekey moves each kept result to its step's new place after a task was put
// into the run: the results are kept by the phase's place, and a task put in
// front of a step moves every place after it. Callers hold floorLoop.mu.
func (lp *floorLoop) rekey(id int, phases []factory.Phase) {
	old := lp.tasks.shape[id]
	lp.noteShape(id, phases)
	results := lp.results[id]
	if len(results) == 0 || len(old) == 0 {
		return
	}
	now := lp.tasks.shape[id]
	if slices.Equal(old, now) {
		return
	}
	moved := map[int]factory.StageResult{}
	for i, res := range results {
		if i < 0 || i >= len(old) {
			continue
		}
		if k := slices.Index(now, old[i]); k >= 0 {
			moved[k] = res
		}
	}
	lp.results[id] = moved
}

// noCheckout is the key of a job this runner gives no folder: there is
// nothing a task could have changed.
const noCheckout = "no checkout"

// checkoutNow is the key of the job's checkout as it stands, [noCheckout]
// for a job with none, and "" when it could not be read.
func (lp *floorLoop) checkoutNow(it factory.Item) string {
	dir, err := lp.jobDir(it)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(dir) == "" {
		return noCheckout
	}
	key, err := checkoutKey(dir)
	if err != nil {
		return ""
	}
	return key
}

// checkoutKey names the checkout's content: its commit, what differs from it,
// and every file git does not track. Two keys that match are the same files.
func checkoutKey(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("the job has no checkout")
	}
	git := changeGit{}
	h := sha256.New()
	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"status", "--porcelain=v1", "-z", "--untracked-files=all"},
		{"diff", "--no-ext-diff", "--no-color", "HEAD", "--"},
	} {
		out, err := git.Run(dir, args...)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(h, out)
		_, _ = h.Write([]byte{0})
	}
	untracked, err := git.Run(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	for _, path := range strings.Split(untracked, "\x00") {
		if path == "" {
			continue
		}
		full := filepath.Join(dir, path)
		info, err := os.Lstat(full)
		if err != nil {
			return "", err
		}
		var content []byte
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return "", err
			}
			content = []byte(target)
		} else if info.Mode().IsRegular() {
			if content, err = os.ReadFile(full); err != nil {
				return "", err
			}
		}
		sum := sha256.Sum256(content)
		_, _ = io.WriteString(h, path)
		_, _ = h.Write(sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// taskBrief is the paragraph a task's delegate reads that no pipeline step's
// does: what a task is, the instructions whole, and that publishing is not
// its to do. "" for every other step.
func taskBrief(job Job) string {
	if job.Item.Stream == nil {
		return ""
	}
	for _, ph := range job.Item.Stream.Phases {
		if ph.Name != job.Stage.Name || ph.Task == nil {
			continue
		}
		who := "the person"
		if ph.Task.By == factory.TaskByMainAgent {
			who = "the job's main agent"
		}
		return "This is an extra task from " + who + ", not one of the pipeline's steps. " + factory.TaskRules + "\nThe task, whole:\n" + ph.Task.Words
	}
	return ""
}
