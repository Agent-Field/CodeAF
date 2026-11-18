package factory

import (
	"strconv"
	"strings"
	"time"
)

// AN EXTRA TASK IS A STEP NOBODY PLANNED. The job's main agent, or its person,
// sends a delegate into the job's checkout with plain instructions, whatever
// step the job is on. It is a phase of the run only, never one of the job's
// stages, so the nine-step limit and the rule that nothing follows proof do
// not stand in its way, and it is named task (task2, task3, …).
//
// Where it goes depends on what the job is doing ([TaskRoad]):
//
//   - nothing is running: before the step the job is on;
//   - a round is running: right after that round, before the step's next
//     round or the next step, unless it was asked for now;
//   - the job has landed: after its last finished step, and the approval it
//     waited for is withdrawn, as Request changes withdraws it;
//   - its pull request is open: it is the next Follow PR round's request, so
//     its commits go through the approval every repair push goes through.
//
// A TASK ON A JOB THAT STOPPED IS THE WORD FOR ONE MORE ROUND. A step out of
// rounds, a failed step and a Follow PR that reached its limit each run again
// after the task, as they do after "try once more", a rerun and an answer.

// Who asked for a task.
const (
	TaskByMainAgent = "main_agent"
	TaskByYou       = "you"
)

// TaskRounds is how many rounds a task gets, as a shipped step that runs
// until done gets: one more than the first, then it stops as any step out of
// rounds stops.
const TaskRounds = 2

// taskName is the first task's name; a job's later ones are numbered.
const taskName = "task"

// TaskAsk is what an extra task's phase carries: the instructions, whole, and
// who gave them. A phase's note changes as it runs (a shortfall, a question),
// so the instructions are kept here.
type TaskAsk struct {
	Words string
	By    string
}

// Where a task goes ([TaskRoad]).
const (
	// TaskRunsBefore is before the step the job is on: nothing is running it.
	TaskRunsBefore = "before"
	// TaskRunsAfter is right after the round running now.
	TaskRunsAfter = "after"
	// TaskRunsLanded is after the last finished step of a landed job.
	TaskRunsLanded = "landed"
	// TaskRunsFollow is the next Follow PR round, with the task as its request.
	TaskRunsFollow = "follow"
	// TaskRunsFollowNext is the Follow PR round after the one running now.
	TaskRunsFollowNext = "follow_next"
)

// TaskAdded is what the door answers: the step the task runs as and the road
// it took.
type TaskAdded struct {
	Step string
	Road string
	By   string
}

// TaskRefused is why a task cannot be added now, in words a person reads.
type TaskRefused string

func (e TaskRefused) Error() string { return string(e) }

// The refusals. They are said to the person and to the main agent as they are.
const (
	taskNoWords     TaskRefused = "Say what the task is."
	taskFinished    TaskRefused = "This job is finished, so it takes no more tasks."
	taskStopped     TaskRefused = "You stopped this job. Start it again first."
	taskNotStarted  TaskRefused = "Start the job first. A task needs a job that is under way, with its checkout made."
	taskElsewhere   TaskRefused = "This job runs on AgentField and has no checkout here for a task."
	taskPutAway     TaskRefused = "A decision on this job was put away. "
	taskTakenOver   TaskRefused = "You have taken this job over. Give it back with Resume pipeline first."
	taskBudget      TaskRefused = "Job budget reached. Raise the budget before sending a task."
	taskNoBudget    TaskRefused = "Choose a budget for this job before sending a task."
	taskPublishing  TaskRefused = "A push is in flight. Send the task after it lands."
	taskMerging     TaskRefused = "A merge is in flight. Send the task after it settles."
	taskPlanWaits   TaskRefused = "The plan is waiting for approval. Approve it or send it back first. A task runs once the plan is approved."
	taskSpendStop   TaskRefused = "This job is stopped after its last step, as you asked. Answer that first."
	taskNotSaved    TaskRefused = "The last step's results were not saved. Run the step again or resume the pipeline first."
	taskRunApproval TaskRefused = "An AgentField run is waiting for its approval. Answer that first."
)

// TaskRefusal says why a task with these words cannot be added to the job
// now, or nil. The door applies it under the job's lock and the job's offers
// read it ([OffersOf]), so what is offered is what the door takes.
func TaskRefusal(it Item, words string) error {
	if strings.TrimSpace(words) == "" {
		return taskNoWords
	}
	if err := RefuseDropped(it); err != nil {
		return TaskRefused(err.Error())
	}
	if JobState(it) == "done" {
		return taskFinished
	}
	if StoppedByPerson(it) {
		return taskStopped
	}
	if err := RequireIssueJob(it); err != nil {
		return TaskRefused(err.Error())
	}
	if JobState(it) == "backlog" || !taskStarted(it) {
		return taskNotStarted
	}
	if RunsElsewhere(it) {
		return taskElsewhere
	}
	if RecoveryPaused(it) {
		return taskPutAway + TaskRefused(DecisionPauseDetail(it))
	}
	if it.Manual {
		return taskTakenOver
	}
	open := it.PullRequest != nil && it.PullRequest.State == PROpen
	if it.Cap > 0 && JobLifetimeSpent(it) >= it.Cap {
		return taskBudget
	}
	// A Follow PR round is never queued for a job with no budget at all
	// ([QueueCIFollowUp]).
	if open && it.Cap <= 0 {
		return taskNoBudget
	}
	if _, what := actInFlight(it); it.Publishing || what == "approving" {
		return taskPublishing
	}
	if m := it.Merging; m != nil && m.State != MergeFailed {
		return taskMerging
	}
	if it.State == StateNeedsYou {
		switch {
		// A TASK NEVER APPROVES A PLAN BY THE BACK DOOR. Before the plan is
		// approved there is nothing agreed to work on, and a delegate sent in
		// now would do the work the approval is there to hold.
		case it.QKind == QKindApprove && NeedsKind(it) == NeedsApprovePlan:
			return taskPlanWaits
		case it.QKind == QKindSpendStopped:
			return taskSpendStop
		case it.QKind == "results_not_saved":
			return taskNotSaved
		case it.QKind == QKindAgentField:
			return taskRunApproval
		}
	}
	return nil
}

// taskStarted says the job has a run that began: a step of it has run a
// round, so its checkout is there.
func taskStarted(it Item) bool {
	if it.Stream == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if ph.State != PhasePending || ph.Round > 0 {
			return true
		}
	}
	return false
}

// TaskRoad says where a task goes on the job as it stands. held says a runner
// holds the job to run its steps, and now that the task was asked for at once.
func TaskRoad(it Item, held, now bool) string {
	if pr := it.PullRequest; pr != nil && pr.State == PROpen {
		// A runner that holds the job is in a Follow PR round, or handing one
		// in: the task waits for it unless it was asked for now.
		if held && !now {
			return TaskRunsFollowNext
		}
		return TaskRunsFollow
	}
	if it.State == StateLanded {
		return TaskRunsLanded
	}
	if held && !now && roundRunning(it) {
		return TaskRunsAfter
	}
	return TaskRunsBefore
}

// roundRunning says the step a held job is on is in the middle of a round:
// it is running, or it waits on a question it asked from inside the round.
func roundRunning(it Item) bool {
	s := it.Stream
	if s == nil || s.Cur < 0 || s.Cur >= len(s.Phases) {
		return false
	}
	ph := s.Phases[s.Cur]
	switch it.State {
	case StateRunning, StateQueued:
		return ph.State == PhaseRunning
	case StateNeedsYou:
		return it.QKind == QKindStep && it.Asking != nil && ph.Kind != StageGate && ph.Round > 0
	}
	return false
}

// IsTask says the phase is an extra task's.
func IsTask(ph Phase) bool { return ph.Task != nil }

// TaskPhase is the phase of a new task on the job, named so no step or phase
// of the job shares its name.
func TaskPhase(it Item, by, words string) Phase {
	name := taskName
	taken := func(name string) bool {
		if StageIndex(it.Stages, name) >= 0 {
			return true
		}
		if it.Stream != nil {
			for _, ph := range it.Stream.Phases {
				if ph.Name == name {
					return true
				}
			}
		}
		return false
	}
	for k := 2; taken(name); k++ {
		name = taskName + strconv.Itoa(k)
	}
	return NamedTaskPhase(name, by, words)
}

// NamedTaskPhase is a task's phase under a name already chosen for it: the
// runner that is told of a task makes the phase the factory made.
func NamedTaskPhase(name, by, words string) Phase {
	words = strings.TrimSpace(words)
	return Phase{Name: name, Kind: StageChat, State: PhasePending, Note: firstLine(words), Task: &TaskAsk{Words: words, By: TaskBy(by)}}
}

// TaskBy reads who asked for a task: the main agent, or the person for any
// other word.
func TaskBy(by string) string {
	if strings.TrimSpace(by) == TaskByMainAgent {
		return TaskByMainAgent
	}
	return TaskByYou
}

// TaskStage is the stage a task's phase runs as: its instructions as the
// ask, until its delegate reports it done.
func TaskStage(ph Phase) Stage {
	s := Stage{Name: ph.Name, Kind: StageChat, Ask: ph.Note, Until: UntilDone, Max: TaskRounds, On: true}
	if t := ph.Task; t != nil {
		s.Ask = t.Words
		s.By = ByYou
		if t.By == TaskByMainAgent {
			s.By = ByManager
		}
	}
	return s
}

// PhaseStage is the stage behind a phase of the job's run: the job's stage of
// that name, a task's own, or, for any other phase no stage names (a request
// for changes' prove), one made from the phase. index is the stage's place in
// the job's stages, -1 for a phase that is not one of them.
func PhaseStage(it Item, ph Phase) (st Stage, index int) {
	if IsTask(ph) {
		return TaskStage(ph), -1
	}
	if j := StageIndex(it.Stages, ph.Name); j >= 0 {
		return it.Stages[j], j
	}
	return Stage{Name: ph.Name, Kind: ph.Kind, Ask: ph.Note, Until: UntilDone, On: true}, -1
}

// TaskLine is the job log's line for a task that was added.
func TaskLine(by, words string) string {
	who := "you"
	if TaskBy(by) == TaskByMainAgent {
		who = "the main agent"
	}
	return "Task from " + who + ": " + firstLine(words)
}

// WaitingTask is the first task of the job's run that has not finished, and
// whether there is one.
func WaitingTask(it Item) (int, bool) {
	if it.Stream == nil {
		return -1, false
	}
	for i, ph := range it.Stream.Phases {
		if IsTask(ph) && ph.State != PhaseDone && ph.State != PhaseSkipped && ph.State != PhaseFailed {
			return i, true
		}
	}
	return -1, false
}

// InsertPhase is phases with ph at index at.
func InsertPhase(phases []Phase, at int, ph Phase) []Phase {
	at = min(max(at, 0), len(phases))
	out := make([]Phase, 0, len(phases)+1)
	out = append(out, phases[:at]...)
	out = append(out, ph)
	return append(out, phases[at:]...)
}

// AfterRound is where a task goes when the step at cur is in the middle of a
// round: right after it, and after any task already waiting there, so tasks
// run in the order they were asked for.
func AfterRound(phases []Phase, cur int) int {
	at := cur + 1
	for at < len(phases) && IsTask(phases[at]) && phases[at].State == PhasePending {
		at++
	}
	return at
}

// KeepTasks puts the tasks that wait in a run's old tail back into the tail
// as it was laid again from the job's steps. A STEP EDIT NEVER DROPS A TASK
// SOMEBODY ASKED FOR: each goes before the first step that followed it and is
// still there, or last when none of them is.
func KeepTasks(old, laid []Phase) []Phase {
	out := append([]Phase(nil), laid...)
	for i, ph := range old {
		if !IsTask(ph) {
			continue
		}
		at := len(out)
		for _, after := range old[i+1:] {
			if IsTask(after) {
				continue
			}
			if k := phaseAt(out, after.Name); k >= 0 {
				at = k
				break
			}
		}
		out = InsertPhase(out, at, ph)
	}
	return out
}

func phaseAt(phases []Phase, name string) int {
	for i, ph := range phases {
		if ph.Name == name {
			return i
		}
	}
	return -1
}

// FollowTaskRequest is a task as a Follow PR round reads it, in the place a
// person's own request goes.
func FollowTaskRequest(by, words string) string {
	who := "the person"
	if TaskBy(by) == TaskByMainAgent {
		who = "the job's main agent"
	}
	return "An extra task from " + who + ": " + strings.TrimSpace(words) + " " + TaskRules
}

// TaskRules is the one thing a task's delegate is told that a pipeline step's
// is not.
const TaskRules = "Do exactly what the task asks, in this checkout, and report what you changed. Do not push, open or edit a pull request, or post anything yourself: the factory publishes after its own checks and approval."

// QueueFollowTask makes a task the request of the job's next Follow PR
// round, on the road Resume pipeline and an answer to Follow PR's question
// take ([QueueCIFollowUp]). A task is its asker's word for one more round, so
// a repair count at its limit gives this one, and an approval the job waited
// for is withdrawn: the round makes the next head, which gets its own.
func QueueFollowTask(it *Item, by, words string, now time.Time) {
	if it.FollowUp == nil {
		WatchPublishedPR(it)
	}
	if it.FollowUp == nil {
		it.FollowUp = &PRFollowUp{Kind: "pr", Limit: DefaultPRRepairAttempts}
	}
	f := *it.FollowUp
	request := joinRequests(f.Next, FollowTaskRequest(by, words))
	f.Next = ""
	startFollowTask(it, f, request, now)
}

// HoldFollowTask keeps a task for the Follow PR round after the one running
// now ([StartHeldFollowTask]).
func HoldFollowTask(it *Item, by, words string) {
	if it.FollowUp == nil {
		it.FollowUp = &PRFollowUp{Kind: "pr", Limit: DefaultPRRepairAttempts}
	}
	f := *it.FollowUp
	f.Next = joinRequests(f.Next, FollowTaskRequest(by, words))
	it.FollowUp = &f
}

// StartHeldFollowTask starts the Follow PR round of the tasks that were asked
// for while a round ran, once that round is over and nothing else is in
// flight. The caller has seen that no runner holds the job. It says whether
// it changed the job.
func StartHeldFollowTask(it *Item, now time.Time) bool {
	f := it.FollowUp
	if f == nil || strings.TrimSpace(f.Next) == "" {
		return false
	}
	if pr := it.PullRequest; pr == nil || pr.State != PROpen {
		return false
	}
	// The same rules that would refuse the task now hold it back: it starts
	// when the push has landed, the budget is raised, the job is given back.
	if TaskRefusal(*it, f.Next) != nil {
		return false
	}
	next := *f
	request := next.Next
	next.Next = ""
	startFollowTask(it, next, request, now)
	return true
}

// startFollowTask queues the Follow PR round that carries request.
func startFollowTask(it *Item, f PRFollowUp, request string, now time.Time) {
	// A ROUND THAT IS QUEUED AND HAS NOT BEGUN TAKES THE TASK WITH IT. Queueing
	// it again would count a second repair for the one round.
	if f.State == "queued" && it.State == StateQueued {
		f.Request = joinRequests(f.Request, request)
		it.FollowUp = &f
		return
	}
	// A round this task cuts short keeps its own request ahead of the task's,
	// and its repair is not counted twice.
	if f.State == "queued" || f.State == "running" {
		request = joinRequests(f.Request, request)
		f.Attempt = max(f.Attempt-1, 0)
	}
	f.Kind, f.Readiness = "pr", nil
	f.Request = request
	f.State, f.BlockedBy, f.EventKey = "watching", "", ""
	if f.Limit > 0 && f.Attempt >= f.Limit {
		f.Attempt = f.Limit - 1
	}
	it.FollowUp = &f
	it.Question, it.QKind, it.Asking = "", "", nil
	it.Failure = nil
	// A publish that failed is over once a new round is queued: the round
	// makes the next head, and that head gets an approval of its own.
	it.PublishError, it.PublishStatus, it.PublishAnyway = "", nil, nil
	it.Approval = nil
	it.State = StateShipped
	// A pull request not read yet queues nothing here; the request stays on
	// the follow-up and the next read of GitHub queues the round.
	QueueCIFollowUp(it, now)
}

// joinRequests is two requests of one Follow PR round, the earlier first.
func joinRequests(first, second string) string {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	switch {
	case first == "" || first == second:
		return second
	case second == "":
		return first
	}
	return first + "\n" + second
}
