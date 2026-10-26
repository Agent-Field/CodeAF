package session

// WHAT A PERSON MAY DO WITH ONE PROCESS IS THE ENGINE'S TO SAY, AND IT SAYS IT
// FROM THE RECORD AND THE REGISTRY THAT HOLDS THE JOB — never from a row's
// words. A surface's row is drawn from a page that may be seconds old and may
// have cut the command it shows, so "Run again" and "Stop" are answered here,
// by number and start, and every verb asks the same question again at the
// moment it acts.
//
// RUN AGAIN IS CONSERVATIVE ON PURPOSE (M1). It runs the recorded command,
// whole, in the recorded folder, and only when that folder is this agent's own
// workspace now, the agent's own chat started it, and its owner saw it end. A
// record saved as running that nobody vouches for has an ending nobody saw, and
// is never rewritten to stopped to make it eligible. A task worker's command is
// the worker's, in the worker's tree, and is not run from the conversation.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Why a verb is not available. They are wire words: a surface keys its
// sentence on them and never on the error text.
const (
	ProcessActionUnsupported = "unsupported"

	ProcessStopEnded   = "ended"
	ProcessStopUnknown = "unknown"
	ProcessStopTask    = "task"
	// ProcessStopSaving is a render that has its bytes and is saving them: a
	// stop would not stop it, so none is offered.
	ProcessStopSaving = "saving"

	ProcessRerunRunning        = "running"
	ProcessRerunUnknown        = "unknown"
	ProcessRerunNotCommand     = "not_command"
	ProcessRerunNoCommand      = "no_command"
	ProcessRerunCommandCut     = "command_cut"
	ProcessRerunTaskWorker     = "task_worker"
	ProcessRerunStarterUnknown = "starter_unknown"
	ProcessRerunNoFolder       = "no_folder"
	ProcessRerunOtherFolder    = "other_folder"
	ProcessRerunFolderGone     = "folder_gone"
)

// The stop-all bounds. PERF.md states them; a change here changes it.
const (
	// ProcessStopAllMax is how many running jobs one stop-all signals.
	ProcessStopAllMax = 256
	// ProcessStopAllScan is how many of a registry's newest jobs, and of the
	// conversation's live marks, one stop-all examines to find them.
	ProcessStopAllScan = 4096
	// ProcessStopAllWait is the ONE deadline every job of a stop-all shares:
	// a TERM grace and a KILL grace, and a second to see them land.
	ProcessStopAllWait = 2*jobTermGrace + time.Second
)

// ProcessAction is one verb's availability; Reason is set only when it is not.
type ProcessAction struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// ProcessActions is what this engine will do with one job now.
type ProcessActions struct {
	Record  JobNotice     `json:"record"`
	Unknown string        `json:"unknown,omitempty"`
	Stop    ProcessAction `json:"stop"`
	Rerun   ProcessAction `json:"rerun"`
}

// ProcessActionRefused is a verb refused for one of the reasons above.
type ProcessActionRefused struct {
	Verb   string
	Reason string
}

func (e *ProcessActionRefused) Error() string {
	why := map[string]string{
		ProcessStopUnknown:         "its ending was never seen and nothing here holds it",
		ProcessStopTask:            "it is a task; stop the task",
		ProcessStopSaving:          "it has finished rendering and is being saved",
		ProcessRerunRunning:        "it is still running",
		ProcessRerunNotCommand:     "only a command can be run again",
		ProcessRerunNoCommand:      "its command was not kept",
		ProcessRerunCommandCut:     "its command was too long to keep whole",
		ProcessRerunTaskWorker:     "a task started it, in the task's own folder",
		ProcessRerunStarterUnknown: "nothing says this conversation started it",
		ProcessRerunNoFolder:       "the folder it ran in was not kept",
		ProcessRerunOtherFolder:    "it ran in another folder than this conversation's",
		ProcessRerunFolderGone:     "the folder it ran in is gone",
	}[e.Reason]
	if e.Verb == "rerun" && e.Reason == ProcessRerunUnknown {
		why = "its ending was never seen, so it is not run again"
	}
	if why == "" {
		why = e.Reason
	}
	if e.Verb == "rerun" {
		return "this process cannot be run again: " + why
	}
	return "this process cannot be stopped: " + why
}

// ErrNoSuchProcess is a number with no record here, or whose start is
// another run's.
var ErrNoSuchProcess = errors.New("no such process")

// processHold is one job as this engine knows it now.
type processHold struct {
	n       JobNotice
	owner   *Agent
	jobs    *jobRegistry
	target  *job
	unknown string
}

// processHoldOf is job id from the registry holding it — this agent's own, or
// a task worker's marked live in the conversation's folder ([Agent.jobHolder])
// — and otherwise from its saved record, unknown when it was saved running.
func (a *Agent) processHoldOf(id int) (processHold, bool, error) {
	if id <= 0 {
		return processHold{}, false, nil
	}
	if owner, jobs, target := a.jobHolder(id); target != nil {
		n := noticeOf(target.info())
		n.StartedBy, n.Task = owner.processOwner()
		return processHold{n: n, owner: owner, jobs: jobs, target: target}, true, nil
	}
	dir := a.processConvDir()
	if dir == "" {
		return processHold{}, false, nil
	}
	n, found, err := processStore{dir: dir}.readProcessRecord(id)
	if err != nil || !found {
		return processHold{}, found, err
	}
	hold := processHold{n: n}
	if n.State == JobRunning {
		hold.unknown = ProcessUnknownOwnerLost
		if a.closedForRecords() {
			hold.unknown = ProcessUnknownNotHeld
		}
	}
	return hold, true, nil
}

// sameStart says a record is the run a stamped id names: startedMS is the
// start in Unix milliseconds, and zero names a record with no start.
func sameStart(n JobNotice, startedMS int64) bool {
	if startedMS == 0 {
		return n.Started.IsZero()
	}
	return !n.Started.IsZero() && n.Started.UnixMilli() == startedMS
}

// actionsOf is the two verbs' availability for one hold.
func (a *Agent) actionsOf(h processHold) ProcessActions {
	out := ProcessActions{Record: h.n, Unknown: h.unknown}
	running := h.target != nil && h.target.running()
	switch {
	case running && h.n.Kind == JobKindTask:
		out.Stop.Reason = ProcessStopTask
	case running && h.target.saving():
		out.Stop.Reason = ProcessStopSaving
	case running:
		out.Stop.Available = true
	case h.unknown != "":
		out.Stop.Reason = ProcessStopUnknown
	default:
		out.Stop.Reason = ProcessStopEnded
	}
	out.Rerun.Reason = a.rerunBlocked(h, running)
	out.Rerun.Available = out.Rerun.Reason == ""
	return out
}

// rerunBlocked is why h may not be run again, or "" when it may.
func (a *Agent) rerunBlocked(h processHold, running bool) string {
	n := h.n
	switch {
	case running || (h.unknown == "" && !n.Over()):
		return ProcessRerunRunning
	case h.unknown != "":
		return ProcessRerunUnknown
	case n.Kind != JobKindCommand:
		return ProcessRerunNotCommand
	case strings.TrimSpace(n.Command) == "":
		return ProcessRerunNoCommand
	case n.CommandCut:
		return ProcessRerunCommandCut
	case (h.owner != nil && h.owner != a) || n.StartedBy == JobStartedByTask:
		return ProcessRerunTaskWorker
	case n.StartedBy != JobStartedByChat:
		return ProcessRerunStarterUnknown
	case strings.TrimSpace(n.Dir) == "":
		return ProcessRerunNoFolder
	}
	a.mu.Lock()
	jobs := a.jobs
	a.mu.Unlock()
	if jobs == nil {
		return ProcessRerunOtherFolder
	}
	jobs.mu.Lock()
	workspace := jobs.workspace
	jobs.mu.Unlock()
	if filepath.Clean(n.Dir) != filepath.Clean(workspace) {
		return ProcessRerunOtherFolder
	}
	if info, err := os.Stat(n.Dir); err != nil || !info.IsDir() {
		return ProcessRerunFolderGone
	}
	return ""
}

// ProcessActions is what this engine will do with job id, whose start is
// startedMS ([sameStart]), now. It reads; it changes nothing.
func (a *Agent) ProcessActions(ctx context.Context, id int, startedMS int64) (ProcessActions, error) {
	if err := ctx.Err(); err != nil {
		return ProcessActions{}, err
	}
	h, found, err := a.processHoldOf(id)
	if err != nil {
		return ProcessActions{}, err
	}
	if !found || !sameStart(h.n, startedMS) {
		return ProcessActions{}, ErrNoSuchProcess
	}
	facts := a.actionsOf(h)
	if err := ctx.Err(); err != nil {
		return ProcessActions{}, err
	}
	return facts, nil
}

// RerunProcess takes the retained command from this engine's record, never a
// client-supplied shell string, and runs it again under the M1 policy above.
// It is the old unstamped door; [Agent.RerunProcessAt] is the stamped one.
func (a *Agent) RerunProcess(id int) (JobNotice, error) {
	h, found, err := a.processHoldOf(id)
	if err != nil {
		return JobNotice{}, err
	}
	if !found {
		return JobNotice{}, ErrNoSuchProcess
	}
	return a.rerun(h)
}

// RerunProcessAt is [Agent.RerunProcess] for the run a stamped id names.
func (a *Agent) RerunProcessAt(ctx context.Context, id int, startedMS int64) (JobNotice, error) {
	if err := ctx.Err(); err != nil {
		return JobNotice{}, err
	}
	h, found, err := a.processHoldOf(id)
	if err != nil {
		return JobNotice{}, err
	}
	if !found || !sameStart(h.n, startedMS) {
		return JobNotice{}, ErrNoSuchProcess
	}
	return a.rerunUnder(ctx, h)
}

func (a *Agent) rerun(h processHold) (JobNotice, error) {
	return a.rerunUnder(context.Background(), h)
}

func (a *Agent) rerunUnder(ctx context.Context, h processHold) (JobNotice, error) {
	if err := a.CheckWork(); err != nil {
		return JobNotice{}, err
	}
	if err := ctx.Err(); err != nil {
		return JobNotice{}, err
	}
	facts := a.actionsOf(h)
	if !facts.Rerun.Available {
		return JobNotice{}, &ProcessActionRefused{Verb: "rerun", Reason: facts.Rerun.Reason}
	}
	a.mu.Lock()
	jobs := a.jobs
	a.mu.Unlock()
	started, err := jobs.startInUnder(ctx, h.n.Command, h.n.Dir)
	if err != nil {
		return JobNotice{}, err
	}
	return noticeOf(started.info()), nil
}

// ProcessStopOutcome is what one stamped stop proved.
type ProcessStopOutcome struct {
	Line string `json:"line"`
	// Ended says the job had already ended; nothing was signalled.
	Ended bool `json:"ended,omitempty"`
	// Unproven says the job was signalled and had not ended when ctx or the
	// graces ran out.
	Unproven bool `json:"unproven,omitempty"`
	// Refused says the stop did not happen: a render committed to saving its
	// file between the facts and the kill, and keeps running until it lands.
	Refused bool `json:"refused,omitempty"`
}

// StopProcessAt is a person's stop of the run a stamped id names, through the
// registry that holds it, under ctx: the person-stop path of [Agent.cancelJob]
// with its graces ending when ctx does.
func (a *Agent) StopProcessAt(ctx context.Context, id int, startedMS int64) (ProcessStopOutcome, error) {
	h, found, err := a.processHoldOf(id)
	if err != nil {
		return ProcessStopOutcome{}, err
	}
	if !found || !sameStart(h.n, startedMS) {
		return ProcessStopOutcome{}, ErrNoSuchProcess
	}
	facts := a.actionsOf(h)
	switch {
	case facts.Stop.Available:
	case facts.Stop.Reason == ProcessStopEnded:
		return ProcessStopOutcome{Line: jobStopName(id, h.target) + " has already finished; there is nothing to stop", Ended: true}, nil
	default:
		return ProcessStopOutcome{}, &ProcessActionRefused{Verb: "stop", Reason: facts.Stop.Reason}
	}
	line, outcome := h.owner.stopJob(ctx, h.jobs, h.target)
	return ProcessStopOutcome{Line: line, Ended: outcome == jobStopEnded, Unproven: outcome == jobStopUnproven, Refused: outcome == jobStopRefused}, nil
}

// ProcessStopReceipt is what one stop-all of a conversation's jobs did.
type ProcessStopReceipt struct {
	// Eligible is the running jobs the bounded snapshot attempted to stop;
	// a committed render stays in NotStopped when its kill is refused.
	Eligible     int `json:"eligible"`
	Stopped      int `json:"stopped"`
	AlreadyEnded int `json:"already_ended"`
	// NotStopped is each job whose end was not seen in time, and each render
	// whose kill refused because it was already saving ([jobStopRefused]): a
	// stop-all that left one running never counts it stopped.
	NotStopped []JobNotice `json:"not_stopped"`
	// Unenumerated is the running jobs found beyond [ProcessStopAllMax], left
	// running and not signalled.
	Unenumerated int `json:"unenumerated"`
	// RemainingUnknown says a scan reached its bound before the oldest entry,
	// so running jobs may exist that were never seen.
	RemainingUnknown bool `json:"remaining_unknown,omitempty"`
}

type stopTarget struct {
	owner *Agent
	jobs  *jobRegistry
	job   *job
}

// StopConversationProcesses stops every job running for this conversation now
// — this agent's and its task workers' — and nothing saved: ONE SNAPSHOT, ONE
// SHARED DEADLINE ([ProcessStopAllWait]), every stop at once through the
// person-stop path, and every one of them joined before it answers.
func (a *Agent) StopConversationProcesses(ctx context.Context) (ProcessStopReceipt, error) {
	var receipt ProcessStopReceipt
	byID := map[int]stopTarget{}
	a.mu.Lock()
	own := a.jobs
	a.mu.Unlock()
	if own != nil {
		running, more := own.runningNewest(ProcessStopAllScan)
		receipt.RemainingUnknown = more
		for _, one := range running {
			if one.kind != jobKindTask {
				byID[one.id] = stopTarget{owner: a, jobs: own, job: one}
			}
		}
	}
	if by, _ := a.processOwner(); by == JobStartedByChat {
		if dir := a.processConvDir(); dir != "" {
			marked, total := liveProcessOwnersOf(dir, ProcessStopAllScan)
			if total > len(marked) {
				receipt.RemainingUnknown = true
			}
			for id, owner := range marked {
				if _, held := byID[id]; held || owner == nil || owner == a || owner.processConvDir() != dir {
					continue
				}
				owner.mu.Lock()
				theirs := owner.jobs
				owner.mu.Unlock()
				if theirs == nil {
					continue
				}
				if one := theirs.find(id); one != nil && one.kind != jobKindTask && one.running() {
					byID[id] = stopTarget{owner: owner, jobs: theirs, job: one}
				}
			}
		}
	}
	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	if len(ids) > ProcessStopAllMax {
		receipt.Unenumerated = len(ids) - ProcessStopAllMax
		ids = ids[:ProcessStopAllMax]
	}
	receipt.Eligible = len(ids)
	receipt.NotStopped = []JobNotice{}
	if len(ids) == 0 {
		return receipt, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ProcessStopAllWait)
	defer cancel()
	outcomes := make([]jobStopOutcome, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		t := byID[id]
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, outcomes[i] = t.owner.stopJob(ctx, t.jobs, t.job)
		}()
	}
	wg.Wait()
	for i, id := range ids {
		switch outcomes[i] {
		case jobStopStopped:
			receipt.Stopped++
		case jobStopEnded:
			receipt.AlreadyEnded++
		default:
			t := byID[id]
			n := noticeOf(t.job.info())
			n.StartedBy, n.Task = t.owner.processOwner()
			receipt.NotStopped = append(receipt.NotStopped, n)
		}
	}
	return receipt, nil
}
