package serve

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// A TASK AFI WORKS ON THE SERVER RUNS IN A SEAT OF ITS OWN, NEVER IN A
// PERSON'S PANE. Each person's conversation with Afi is private to them (L1
// F2), so a task somebody started cannot be answered into anybody's
// conversation: it has its own folder and its own memory of the work. WHAT IT
// SAYS AND DOES THERE IS THE TASK'S WORKING CONVERSATION, which everyone who
// can see the workstream reads, live, as task.work events
// (workstream_task_work.go; OWNER-DECISIONS 2026-11-17 §8); the task's owner
// and whoever started the run steer it there. What it did is also told on the
// task itself: its state, its results and a line in its thread, all written
// through the same door and the same routes a person's client uses.
//
// The run's rights are the rights of the person who started it, checked on
// every write as they are now, so a starter who loses edit mid-run stops the
// run's writes; the run's doer is Afi.

// errTaskBusy is the refusal a second start gets while a task's run is going.
var errTaskBusy = errors.New("this task is already being worked on")

// errTaskNoRunner is the refusal when this server cannot run Afi at all: no
// runner, or no model key from the operator.
var errTaskNoRunner = errors.New("this organization's server cannot run Afi: it has no model key")

// NoRunnerAnsweredLine is the thread's line when an answer to Afi's question
// lands on a server that cannot run Afi: the answer is kept, the task waits.
const NoRunnerAnsweredLine = "Answer kept · Afi carries on once this organization has a model key"

// taskRunKey names one task's run.
type taskRunKey struct{ ws, task string }

// taskRunDir is the folder of a task's run: the agent's own conversation for
// that task and nothing else.
func (h *Handler) taskRunDir(ws, task string) string {
	return filepath.Join(h.config.Store.Root(), agentDirName, ws, "tasks", task)
}

// runTaskTurn starts Afi on task, as starter, with words as what it is asked
// to do, and returns once the run is accepted. It is errTaskBusy while the
// task's previous run is still going and errTaskNoRunner when this server
// cannot run Afi; on either the caller leaves the task where it was. The run
// itself sets the task done or failed through ws_task_update; a run that ends
// without doing either, and was not stopped, is settled by finishTaskRun: done
// with what it wrote, or failed with the reason, so a task never says running
// with nothing at work on it.
//
// IT TOUCHES NO WORKSTREAMS DATABASE BEFORE IT RETURNS, so a task write may call
// it inside its own EditWorkstreamTasks transaction: that store has one
// connection, and a synchronous read here would wait on the transaction that
// is waiting on it. The organization's key is a file of its own. Every read of
// the task happens in the run, after the caller's transaction has ended; a
// caller whose transaction fails after a nil return calls stopTaskTurn.
func (h *Handler) runTaskTurn(ws factory.OrgWorkstream, task factory.WorkstreamTask, starter, words string) error {
	return h.startTaskRun(ws, task, starter, words, 0, false)
}

// startTaskRun is runTaskTurn for a run that has already been resumed
// `resumes` times after a restart cut it off (workstream_task_lease.go), and,
// when answered, for the run an answer to Afi's Ask started. Either one goes on
// with work an earlier run of the task did, so finishTaskRun counts the files
// that run made (agentDoor.goesOn). A run over the limits waits in the queue
// (workstream_task_queue.go) and is accepted all the same: it starts as soon
// as a place is free.
func (h *Handler) startTaskRun(ws factory.OrgWorkstream, task factory.WorkstreamTask, starter, words string, resumes int, answered bool) error {
	return h.startTaskRunTyped(ws, task, starter, words, "", resumes, answered)
}

// startTaskRunTyped is startTaskRun with typed: words a person typed for this
// run (an answer's own words, a Keep going's note), which the run's web_fetch
// may follow an address in (taskLeashWords). words itself is composed, partly
// from what the model wrote, and is never trusted that way.
func (h *Handler) startTaskRunTyped(ws factory.OrgWorkstream, task factory.WorkstreamTask, starter, words, typed string, resumes int, answered bool) error {
	if !h.serverAgentServed() {
		return errTaskNoRunner
	}
	starter = factory.NormalizeHandle(starter)
	if starter == "" {
		return errors.New("a task's run needs the person who started it")
	}
	key := taskRunKey{ws.ID, task.ID}
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("this factory is closing")
	}
	if d.tasks == nil {
		d.tasks = map[taskRunKey]context.CancelFunc{}
	}
	if _, busy := d.tasks[key]; busy || d.queuedLocked(key) >= 0 {
		d.mu.Unlock()
		return errTaskBusy
	}
	d.mu.Unlock()
	return h.admitTaskRun(pendingRun{key: key, ws: ws, task: task, starter: starter, words: words, typed: typed, resumes: resumes, answered: answered})
}

// admitTaskRun starts run now when there is room under the limits, and
// otherwise puts it in the queue.
func (h *Handler) admitTaskRun(run pendingRun) error {
	d := &h.agentDesk
	ws, key := run.ws, run.key
	d.mu.Lock()
	if d.ctx == nil || d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("this factory is closing")
	}
	if _, busy := d.tasks[key]; busy || d.queuedLocked(key) >= 0 {
		d.mu.Unlock()
		return errTaskBusy
	}
	if !d.roomLocked(ws.ID) {
		queued := d.enqueueLocked(run)
		owner := d.ownerLocked()
		d.mu.Unlock()
		h.keepQueued(queued, owner)
		return nil
	}
	err := h.launchLocked(run)
	d.mu.Unlock()
	return err
}

// launchLocked starts run now. d.mu is held, and run's task has no run.
func (h *Handler) launchLocked(r pendingRun) error {
	now := h.clock().UTC()
	tid, err := cell.NewID(now)
	if err != nil {
		return err
	}
	turn := "turn_" + tid
	ws, task, starter, key, resumes := r.ws, r.task, r.starter, r.key, r.resumes
	d := &h.agentDesk
	ctx, pause := context.WithCancelCause(d.ctx)
	cancel := func() { pause(nil) }
	d.tasks[key] = cancel
	if d.gen == nil {
		d.gen = map[taskRunKey]int{}
	}
	d.gen[key]++
	mine := d.gen[key]
	// release gives up this run's place, once; a newer run on the task
	// keeps its own.
	release := func() {
		d.mu.Lock()
		if d.gen[key] == mine {
			delete(d.tasks, key)
			delete(d.watch, key)
		}
		d.mu.Unlock()
	}
	// The run is watched for its limits (workstream_task_limits.go).
	limit := taskSpendCap(ws)
	watch := &runWatch{}
	if d.watch == nil {
		d.watch = map[taskRunKey]*runWatch{}
	}
	d.watch[key] = watch
	d.servedLocked(ws.ID)
	d.wg.Add(1)
	owner := d.ownerLocked()

	door := &agentDoor{h: h, id: ws.ID, asker: starter, wrote: &taskWrites{}, runTask: task.ID, goesOn: r.answered || resumes > 0}
	run := AgentTurn{Workstream: ws.ID, Name: ws.Name, Turn: turn, Asker: starter, Text: strings.TrimSpace(r.words), LeashWords: taskLeashWords(task, r.typed), Now: now, About: h.orgAbout(),
		Task: &AgentTask{ID: task.ID, Title: task.Title},
		Key:  h.orgModelKey(), Dir: filepath.Join(h.taskRunDir(ws.ID, task.ID), "agent"), Door: h.withLibrary(door), Library: h.spacesServed(),
		// Every line is a line of the task's Working conversation, and its
		// last step is what the running row says it is doing.
		Step:     watch.noting(h.taskWorkStep(key, turn)),
		Progress: watch.progress(limit, pause)}
	guard.Go("factory workstream task", func() {
		// Counted out last of all, after the Working conversation has closed and
		// told its watchers: Close waits for this run, and nothing of it may
		// write once Close has returned.
		defer d.wg.Done()
		// The Working conversation opens here, never in launchLocked: it takes
		// the desk's lock and publishes, and launchLocked holds that lock (a
		// queued run starts from inside it). Steers wait in its inbox until
		// the run reads them.
		run.Steers = h.openTaskWork(key, turn, starter, r.answered)
		var next *taskResume
		var failed error
		defer func() {
			// Last, once this run is gone: its place is given up, the answer
			// that came while it was ending starts the next one, and the next
			// run waiting under the limits takes what is left.
			release()
			cancel()
			if next != nil {
				h.resumeTaskRun(ws, next.task, next.starter, next.words, next.typed)
			}
			h.dispatchQueued()
		}()
		var spent float64
		// Before that: everyone reading the task sees this run end, so the
		// next run's lines open a turn of their own.
		defer func() { h.closeTaskWork(key, turn, failed, spent) }()
		defer cancel()
		// The lease is kept first and beats while the run goes, so a restart
		// that cuts the run off leaves a row the next start settles; it is
		// dropped when the run ends here (before an answer that came meanwhile
		// starts the next run, whose lease it is not), unless the server is
		// closing, which is exactly the cut the next start must find.
		lease := h.holdTaskLease(ctx, key, owner, starter, run.Text, r.typed, now, resumes, func() float64 {
			// Every beat: a workstream that has spent its budget pauses
			// the run.
			usd := watch.spent()
			if h.overBudget(ws.ID, usd) {
				pause(runPause{kind: pauseBudget, usd: usd})
			}
			return usd
		})
		defer func() {
			if d.ctx.Err() == nil {
				lease()
			}
		}()
		// The packet is read here, in the run, after the caller's write has
		// ended: runTaskTurn touches no workstreams database before it returns.
		// It comes after the lease, which keeps the plain words: a resume
		// replays them through startTaskRun, and so through a fresh packet.
		run.Text, _ = h.taskPacket(ctx, door, task.ID, run.Text)
		wall := time.AfterFunc(taskWallMost, func() { pause(runPause{kind: pauseWall, usd: watch.spent()}) })
		defer wall.Stop()
		var end AgentTurnEnd
		var err error
		if h.overBudget(ws.ID, 0) {
			// Checked before the run spends anything.
			pause(runPause{kind: pauseBudget})
		} else {
			end, err = h.config.WorkstreamAgent(ctx, run)
		}
		if ctx.Err() == nil {
			failed = err
		}
		spent = end.USD
		if end.USD > 0 {
			h.postAgentSpend(run, end.USD)
		}
		// One step under the lock: the run leaves, and whatever answer
		// queued behind it is taken, so no answer can land in between
		// and be lost.
		d.mu.Lock()
		if d.gen[key] == mine {
			delete(d.tasks, key)
		}
		if res, ok := d.resume[key]; ok {
			delete(d.resume, key)
			if ctx.Err() == nil {
				next = &res
			}
		}
		d.mu.Unlock()
		if ctx.Err() != nil {
			var p runPause
			if errors.As(context.Cause(ctx), &p) && d.ctx.Err() == nil {
				// Paused at a limit: an Ask to the owner, never a failure.
				// The place is given up first, so the answer can start the
				// next run at once.
				lease()
				release()
				h.pauseTaskRun(ws, task.ID, p)
			}
			// Otherwise stopped: whoever stopped it has already said so.
			return
		}
		if next != nil {
			// The task was answered while this run ended: it goes on, and
			// the answer's run is the one try, never a retry beside it.
			return
		}
		if err != nil && end.Transient && !r.retried {
			h.retryTaskRun(r, err)
			return
		}
		h.finishTaskRun(door, task.ID, err)
	})
	return nil
}

// taskResume is a run to start on a task once its current one has ended.
type taskResume struct {
	task                  factory.WorkstreamTask
	starter, words, typed string
}

// AN ANSWERED ASK PUTS AFI BACK TO WORK. An Afi task the server runs pauses
// on an Ask and its run ends; the answer sets it running again, and running
// means a doer is at work (live2 E1 F4: the task said Running for eight and
// ten minutes with no model call behind it). So the answer wakes Afi in the
// task's own seat, with what it was asked and what the person answered. A
// run still ending when the answer arrives starts the next one as it goes.

// resumeTaskRun starts Afi again on t, as starter, telling it words; when t's
// run is still going it waits for that run to end. A task that is no longer
// running by then (done, stopped, asked again) is left as it is.
func (h *Handler) resumeTaskRun(ws factory.OrgWorkstream, t factory.WorkstreamTask, starter, words, typed string) {
	d := &h.agentDesk
	key := taskRunKey{ws.ID, t.ID}
	d.mu.Lock()
	if _, busy := d.tasks[key]; busy {
		if d.resume == nil {
			d.resume = map[taskRunKey]taskResume{}
		}
		d.resume[key] = taskResume{task: t, starter: starter, words: words, typed: typed}
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	door := &agentDoor{h: h, id: ws.ID, asker: factory.NormalizeHandle(starter)}
	cur, err := door.task(context.Background(), t.ID)
	if err != nil || cur.State != factory.TaskRunning || cur.DoneBy.Kind != factory.TaskByAfi {
		return
	}
	// A server with no runner was never running Afi here (the same answer a
	// Start gets), so only an unexpected refusal is worth a line in the log.
	if err := h.startTaskRunTyped(ws, cur, starter, words, typed, 0, true); err != nil && !errors.Is(err, errTaskBusy) && !errors.Is(err, errTaskNoRunner) {
		log.Printf("factory: workstream %s: task %s was answered but Afi could not go on: %v", ws.ID, t.ID, err)
	}
}

// AN ANSWER KEPT WHILE NOTHING COULD RUN AFI IS CARRIED ON ONCE SOMETHING CAN
// (review fix, 2026-11-25). The answer route parks Afi's task Up next with
// NoRunnerAnsweredLine on a server without a model key; when the operator sets
// one, every task parked that way is set running again and resumed with the
// answer it was given, the same words a normal answered resume tells Afi.

// taskResumedWithKeyLine is the thread's line when a parked answer is taken up.
const taskResumedWithKeyLine = "Afi carries on with the answer, now that this organization has a model key"

// resumeParkedAnswers starts Afi again on every task an answer parked while
// this server could not run Afi. It is called once a model key is set.
func (h *Handler) resumeParkedAnswers() {
	st := h.config.Store
	if st == nil || !h.serverAgentServed() {
		return
	}
	wss, err := st.Workstreams()
	if err != nil {
		return
	}
	for _, ws := range wss {
		tasks, err := st.WorkstreamTasks(ws.ID)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if t.State != factory.TaskUpNext || t.DoneBy.Kind != factory.TaskByAfi || h.taskTurnRunning(ws.ID, t.ID) {
				continue
			}
			if _, job := factory.IsJobTask(t.ID); job {
				continue
			}
			events, err := st.WorkstreamTaskEvents(ws.ID, t.ID)
			if err != nil {
				continue
			}
			answered, ok := parkedAnswer(events)
			if !ok {
				continue
			}
			h.resumeParkedAnswer(ws, t, answered)
		}
	}
}

// parkedAnswer is the answer a task's thread says was kept while nothing could
// run Afi: the task's last state line is NoRunnerAnsweredLine, and the answer
// is the last one given before it.
func parkedAnswer(events []factory.WorkstreamTaskEvent) (factory.WorkstreamTaskEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != factory.TaskEventState {
			continue
		}
		if e.Text != NoRunnerAnsweredLine {
			return factory.WorkstreamTaskEvent{}, false
		}
		for j := i - 1; j >= 0; j-- {
			if events[j].Kind == factory.TaskEventAnswered {
				return events[j], true
			}
		}
		return factory.WorkstreamTaskEvent{}, false
	}
	return factory.WorkstreamTaskEvent{}, false
}

// resumeParkedAnswer sets seen running and starts Afi on it with the answer,
// as whoever answered, in one write: a start Afi cannot take keeps nothing,
// and the task stays Up next with its line.
func (h *Handler) resumeParkedAnswer(ws factory.OrgWorkstream, seen factory.WorkstreamTask, answered factory.WorkstreamTaskEvent) {
	starter := ""
	if answered.By.Kind == factory.TaskByPerson {
		starter = answered.By.ID
	}
	if starter == "" {
		starter = seen.CreatedBy.ID
		if seen.Owner.Kind == factory.TaskOwnerPerson && seen.Owner.ID != "" {
			starter = seen.Owner.ID
		}
	}
	var resumed factory.WorkstreamTask
	err := h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		t, ok := keptTask(tw, ws.ID, seen.ID)
		if !ok || t.State != factory.TaskUpNext || t.Rev != seen.Rev {
			return errSettledElsewhere
		}
		stamp := h.clock().UTC().Format(time.RFC3339Nano)
		t.State, t.StateReason = factory.TaskRunning, ""
		t.Rev++
		t.UpdatedAt = stamp
		if err := keepTask(tw, t, seen.Rev); err != nil {
			return err
		}
		if err := tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: t.ID, At: stamp, By: afiBy,
			Kind: factory.TaskEventState, Text: taskResumedWithKeyLine}); err != nil {
			return err
		}
		resumed = t
		words := answeredWords(t, factory.TaskAsk{Title: answered.Question}, answered.Answer, answered.By.Name)
		return h.startTaskRunTyped(ws, t, starter, words, answered.Typed, 0, true)
	})
	if err != nil {
		if !errors.Is(err, errSettledElsewhere) && !wakeRefused(err) {
			h.stopTaskTurn(ws.ID, seen.ID)
			log.Printf("factory: workstream %s: task %s kept an answer but Afi could not go on with it: %v", ws.ID, seen.ID, err)
		}
		return
	}
	h.publishTasks(ws, afiBy.Name, resumed)
}

// taskLeashWords are the words of a task's run that a person wrote, the only
// words its web_fetch may follow an address in (session's tools_web_leash.go):
// the task's title and request when a person last wrote them, and typed, what
// a person typed for this run. Never what the model wrote: a task Afi made or
// retitled, an Ask's title or an option's label could carry an address a page
// told it to fetch, with the workstream's data in it.
func taskLeashWords(t factory.WorkstreamTask, typed string) []string {
	var out []string
	if t.TitleBy == factory.TaskByPerson && strings.TrimSpace(t.Title) != "" {
		out = append(out, t.Title)
	}
	if t.RequestBy == factory.TaskByPerson && strings.TrimSpace(t.Request) != "" {
		out = append(out, t.Request)
	}
	if typed = strings.TrimSpace(typed); typed != "" {
		out = append(out, typed)
	}
	return out
}

// answeredWords is what Afi is told when an answer puts it back to work.
func answeredWords(t factory.WorkstreamTask, ask factory.TaskAsk, answer, who string) string {
	return taskWords(t) + "\n\n" + firstNonEmpty(who, "The person") + " answered your question “" + strings.TrimSpace(ask.Title) +
		"”: " + strings.TrimSpace(answer) + "\n\nCarry on with the task in light of that answer."
}

// stopTaskTurn stops task's run, if one is going. The task's own Stop says
// so on the task; this only ends the work.
func (h *Handler) stopTaskTurn(ws, task string) bool {
	d := &h.agentDesk
	d.mu.Lock()
	key := taskRunKey{ws, task}
	cancel, ok := d.tasks[key]
	if ok {
		cancel()
	}
	// A run waiting in the queue leaves it.
	unqueued := d.dequeueLocked(key)
	owner := d.ownerLocked()
	d.mu.Unlock()
	if unqueued {
		h.dropQueued(key, owner)
	}
	return ok || unqueued
}

// taskTurnRunning is whether task has a run going now, or waiting in the
// queue for its place.
func (h *Handler) taskTurnRunning(ws, task string) bool {
	d := &h.agentDesk
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.tasks[taskRunKey{ws, task}]
	return ok || d.queuedLocked(taskRunKey{ws, task}) >= 0
}

// finishTaskRun settles a task that still says running once its run has
// ended without setting it done or failed, so a task never says running with
// nothing at work on it.
//
// A RUN THAT ENDED CLEANLY LEAVING THE TASK WAITING ON UNFINISHED WORK IS UP
// NEXT, NOT FAILED: Afi said what the task waits on, and Up next is where a
// wait shows (factory.WaitAgain), so it starts again once that work is done.
// The run an answer started set the release notes to wait on the candidate run
// and the security review and ended, and the task read Failed (mac-studio,
// 2026-11-17).
//
// A RUN THAT ENDED CLEANLY HAVING WRITTEN ITS WORK IS DONE, NOT FAILED: the
// files it made or rewrote become the task's results, beside any it already
// had. Afi often writes the page and says so in the thread without the last
// ws_task_update, and calling that a failure put finished work under Failed
// (E4 F8). A run that goes on with an earlier run's work (after an answer or a
// restart) counts the files the task made as written: the asking run wrote
// the page, and the run after the answer often only replies. A run that wrote
// nothing, or ended on an error, is failed with why in plain words.
func (h *Handler) finishTaskRun(door *agentDoor, task string, err error) {
	ctx := context.Background()
	t, terr := door.task(ctx, task)
	if terr != nil || t.State != factory.TaskRunning {
		return
	}
	if err == nil && len(t.WaitingOn) > 0 {
		if all, lerr := door.tasks(ctx); lerr == nil && len(factory.UnfinishedWaits(t, all)) > 0 {
			args, _ := json.Marshal(map[string]any{"state": factory.TaskUpNext})
			if _, perr := door.call(ctx, h.patchWorkstreamTask, http.MethodPatch, "/v1/workstreams/"+door.id+"/tasks/"+url.PathEscape(task), map[string]string{"tid": task}, json.RawMessage(args), t.Rev); perr == nil {
				return
			}
			if t, terr = door.task(ctx, task); terr != nil || t.State != factory.TaskRunning {
				return
			}
		}
	}
	wrote := door.wrote.list()
	if err == nil && len(wrote) == 0 && door.goesOn {
		wrote = door.madeByTask(ctx, task)
	}
	if err == nil && len(wrote) > 0 {
		results := []map[string]string{}
		for _, r := range t.Results {
			switch {
			case r.FileID != "":
				results = append(results, map[string]string{"file": r.FileID})
			case r.URL != "":
				results = append(results, map[string]string{"url": r.URL, "label": r.Label})
			}
		}
		for _, id := range wrote {
			if !slices.ContainsFunc(t.Results, func(r factory.TaskResult) bool { return r.FileID == id }) {
				results = append(results, map[string]string{"file": id})
			}
		}
		patch := map[string]any{"task": task, "rev": t.Rev, "state": factory.TaskDone, "results": results}
		if strings.TrimSpace(t.Handoff) == "" {
			// Done carries a hand-off note for the next task; a run that ended
			// without one gets the plainest true note there is.
			patch["handoff"] = "Made " + door.fileNames(ctx, wrote) + ". Afi left no note of its own on what to carry forward."
		}
		args, _ := json.Marshal(patch)
		if _, derr := door.Work(ctx, "ws_task_update", args); derr == nil {
			return
		}
		// A result the store refused (a file deleted since) still must not
		// leave the task running: fall through to failed, with why.
		t, terr = door.task(ctx, task)
		if terr != nil || t.State != factory.TaskRunning {
			return
		}
	}
	why := "Afi stopped before saying the task was done"
	if err != nil {
		why = "Afi could not finish: " + cutRunes(err.Error(), 300)
	}
	// An error is Failed: written straight to the task, never through the
	// tool, which turns an agent's own "failed" into an Ask to the owner.
	p := map[string]any{"state": factory.TaskFailed, "state_reason": why}
	_, _ = door.call(ctx, h.patchWorkstreamTask, http.MethodPatch, "/v1/workstreams/"+door.id+"/tasks/"+url.PathEscape(task), map[string]string{"tid": task}, p, t.Rev)
}
