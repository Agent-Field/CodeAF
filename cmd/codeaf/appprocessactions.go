package main

// processes.actions, processes.stop_all and processes.log_target for the
// desktop app (internal/appwire's processactions.go), and the stamped doors
// processes.rerun and processes.stop now act through.
//
// THE ENGINE HOLDING THE CONVERSATION SAYS WHAT MAY BE DONE. This file asks it
// by number and start, and adds only what this process knows itself: whether a
// managed log is on this machine's disk, and a service's state from the
// supervisor this app runs. A conversation not open here is answered from its
// saved record and offered nothing that would need an owner to vouch for it.

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appProcessActor is the stamped doors of the engine holding a conversation.
type appProcessActor interface {
	ProcessActions(ctx context.Context, id int, startedMS int64) (session.ProcessActions, error)
	RerunProcessAt(ctx context.Context, id int, startedMS int64) (session.JobNotice, error)
	StopProcessAt(ctx context.Context, id int, startedMS int64) (session.ProcessStopOutcome, error)
	StopConversationProcesses(ctx context.Context) (session.ProcessStopReceipt, error)
}

// appProcessStopWait bounds one stamped stop: the engine's own two graces and
// a second for the answer to cross back.
const appProcessStopWait = session.ProcessStopAllWait + time.Second

// appProcessStampMS is a process id's start stamp as Unix milliseconds, zero
// for an id written without one.
func appProcessStampMS(stamp string) (int64, bool) {
	if stamp == "" {
		return 0, true
	}
	ms, err := strconv.ParseInt(stamp, 36, 64)
	return ms, err == nil && ms > 0
}

// appProcessRun is a job id's agent, number and start, or bad_params.
func appProcessRun(id string) (string, int, int64, error) {
	agent, job, stamp, ok := appProcessJob(id)
	ms, good := appProcessStampMS(stamp)
	if !ok || !good {
		return "", 0, 0, appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	return agent, job, ms, nil
}

// appProcessActError is a stamped door's error in the wire's words.
func appProcessActError(err error) error {
	var refused *session.ProcessActionRefused
	switch {
	case errors.Is(err, session.ErrNoSuchProcess):
		return appwire.Errorf(appwire.CodeNotFound, "there is no such process")
	case errors.As(err, &refused):
		return appwire.Errorf(appwire.CodeConflict, "%s", refused.Error())
	case errors.Is(err, remote.ErrProcessActionsUnserved):
		return appwire.Errorf(appwire.CodeUnknownMethod, "this conversation's engine is older and cannot check this; reopen the conversation")
	}
	return appEngineError(err)
}

// ── processes.actions ───────────────────────────────────────────────────────

// ProcessActions is what may be done with one process now.
func (b *appBackend) ProcessActions(ctx context.Context, id string) (*appwire.ProcessActionsResult, error) {
	if _, _, ok := appServiceID(id); ok {
		return b.serviceActions(id)
	}
	agent, job, ms, err := appProcessRun(id)
	if err != nil {
		return nil, err
	}
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return nil, err
	}
	res := &appwire.ProcessActionsResult{ID: id, Kind: appwire.ProcessKindJob}
	var record session.JobNotice
	unknown := ""
	answered := false
	live := b.liveOf(a)
	if live != nil {
		if actor, can := live.conn.Conv.(appProcessActor); can {
			facts, err := actor.ProcessActions(ctx, job, ms)
			switch {
			case errors.Is(err, remote.ErrProcessActionsUnserved):
			case err != nil:
				return nil, appProcessActError(err)
			default:
				record, unknown, answered = facts.Record, facts.Unknown, true
				res.Stop = appwire.ProcessAction{Available: facts.Stop.Available, Reason: facts.Stop.Reason}
				res.Rerun = appwire.ProcessAction{Available: facts.Rerun.Available, Reason: facts.Rerun.Reason}
			}
		}
	}
	convDir := ""
	if !answered {
		record, convDir, err = b.processRecord(ctx, id, false)
		if err != nil {
			return nil, err
		}
		switch {
		case live != nil:
			// AN ENGINE TOO OLD TO ANSWER IS ASKED NOTHING: its verbs are
			// unsupported, never inferred from the record.
			res.Stop = appwire.ProcessAction{Reason: appwire.ProcessActionUnsupported}
			res.Rerun = appwire.ProcessAction{Reason: appwire.ProcessActionUnsupported}
		default:
			// NOBODY HERE HOLDS IT. A record saved as running is unknown, and
			// nothing is offered that an owner would have to vouch for.
			if record.State == session.JobRunning {
				unknown = appwire.ProcessUnknownNotHeld
				res.Stop = appwire.ProcessAction{Reason: appwire.ProcessActionNotHeld}
			} else {
				res.Stop = appwire.ProcessAction{Reason: session.ProcessStopEnded}
			}
			res.Rerun = appwire.ProcessAction{Reason: appwire.ProcessActionNotHeld}
		}
	} else {
		var transcript string
		b.locked(func() { transcript = a.transcript })
		if transcript != "" {
			convDir = session.DirOf(transcript)
		}
	}
	res.State = appProcessBase(agent, record).State
	if unknown != "" {
		res.State, res.UnknownReason = appwire.ProcessUnknown, unknown
	}
	res.Log = appJobLogAction(convDir, record)
	return res, nil
}

// appJobLogAction is whether a job's managed log can be opened here now.
func appJobLogAction(convDir string, record session.JobNotice) appwire.ProcessAction {
	file, err := session.ProcessLogTarget(convDir, record)
	switch {
	case err != nil:
		return appwire.ProcessAction{Reason: appwire.ProcessLogUnmanaged}
	case file.NoLog:
		return appwire.ProcessAction{Reason: appwire.ProcessLogNoLog}
	case file.Gone:
		return appwire.ProcessAction{Reason: appwire.ProcessLogGone}
	}
	return appwire.ProcessAction{Available: true}
}

// serviceActions is processes.actions for a kept-running service, from the
// supervisor this app runs.
func (b *appBackend) serviceActions(id string) (*appwire.ProcessActionsResult, error) {
	svc, agent, local, err := b.serviceRow(id)
	if err != nil {
		return nil, err
	}
	p, err := b.GetService(id)
	if err != nil {
		return nil, err
	}
	res := &appwire.ProcessActionsResult{ID: id, Kind: appwire.ProcessKindService, State: p.State}
	removed := p.Service != nil && p.Service.Removed
	switch {
	case p.State == appwire.ProcessRunning:
		res.Stop.Available = true
		res.Rerun.Reason = session.ProcessRerunRunning
	case removed:
		res.Stop.Reason = session.ProcessStopEnded
		res.Rerun.Reason = appwire.ProcessRerunRemoved
	default:
		res.Stop.Reason = session.ProcessStopEnded
		res.Rerun.Available = true
	}
	file, err := appServiceLogFile(svc, agent, local)
	switch {
	case errors.Is(err, service.ErrNotFound):
		res.Log.Reason = appwire.ProcessLogNoLog
	case err != nil:
		res.Log.Reason = appwire.ProcessLogUnmanaged
	case file.Gone:
		res.Log.Reason = appwire.ProcessLogGone
	default:
		res.Log.Available = true
	}
	return res, nil
}

// appServiceLogFile is a service's log, opened below the supervisor's own
// folder the way a job's is below its conversation's ([session.ManagedLogFile]).
func appServiceLogFile(svc *appServices, agent, local string) (session.ProcessLogFile, error) {
	path, err := svc.sup.OutputPath(agent, local)
	if err != nil {
		return session.ProcessLogFile{}, err
	}
	logs := filepath.Dir(path)
	if filepath.Base(logs) != "logs" {
		return session.ProcessLogFile{}, errors.New("a service's log is not in its supervisor's logs folder")
	}
	return session.ManagedLogFile(filepath.Dir(logs), filepath.Base(logs), filepath.Base(path))
}

// ── processes.log_target ────────────────────────────────────────────────────

// ProcessLogTarget is the facts a native opener needs about one process's
// managed log, opened and identified just now. It grants nothing.
func (b *appBackend) ProcessLogTarget(ctx context.Context, id string) (*appwire.ProcessLogTargetResult, error) {
	if _, _, ok := appServiceID(id); ok {
		svc, agent, local, err := b.serviceRow(id)
		if err != nil {
			return nil, err
		}
		file, err := appServiceLogFile(svc, agent, local)
		if err != nil {
			return nil, appServiceLogError(err)
		}
		return appLogTarget(id, appwire.ProcessKindService, file, service.LogCapBytes), nil
	}
	if _, _, _, err := appProcessRun(id); err != nil {
		return nil, err
	}
	record, convDir, err := b.processRecord(ctx, id, false)
	if err != nil {
		return nil, err
	}
	file, err := session.ProcessLogTarget(convDir, record)
	switch {
	case session.ErrProcessLogUnmanaged(err):
		return nil, appwire.Errorf(appwire.CodeEngine, "this process's output is not in its conversation's logs folder")
	case err != nil:
		return nil, appwire.Errorf(appwire.CodeEngine, "this process's output could not be opened safely")
	case file.NoLog:
		return nil, appwire.Errorf(appwire.CodeNotFound, "this process kept no output")
	}
	return appLogTarget(id, appwire.ProcessKindJob, file, session.AppProcessOutputBytes), nil
}

func appServiceLogError(err error) error {
	if errors.Is(err, service.ErrNotFound) {
		return appwire.Errorf(appwire.CodeNotFound, "there is no such process")
	}
	return appwire.Errorf(appwire.CodeEngine, "this process's output could not be opened safely")
}

func appLogTarget(id, kind string, file session.ProcessLogFile, capBytes int64) *appwire.ProcessLogTargetResult {
	res := &appwire.ProcessLogTargetResult{ID: id, Kind: kind, Local: true, CapBytes: capBytes}
	if file.Gone {
		res.Gone = true
		return res
	}
	res.Path, res.Size, res.Revision = file.Path, file.Size, file.Revision
	return res
}

// ── processes.stop_all ──────────────────────────────────────────────────────

// StopAllProcesses stops one agent's running jobs and kept-running services at
// once, under ctx — the method's one deadline from the moment it was asked —
// and joins both before it answers.
func (b *appBackend) StopAllProcesses(ctx context.Context, ask appwire.ProcessStopAllAsk) (*appwire.ProcessStopAllResult, error) {
	if err := appwire.CheckProcessStopAllAsk(ask); err != nil {
		return nil, err
	}
	agent := ask.Agent
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return nil, err
	}
	admitted := false
	b.locked(func() {
		if b.stoppingAll == nil {
			b.stoppingAll = map[string]bool{}
		}
		if !b.stoppingAll[agent] {
			b.stoppingAll[agent], admitted = true, true
		}
	})
	if !admitted {
		return nil, appwire.Errorf(appwire.CodeBusy, "this agent's processes are already being stopped")
	}
	defer b.locked(func() { delete(b.stoppingAll, agent) })
	// A stop never opens an agent to manufacture an empty registry. A missing
	// held owner cannot prove its saved jobs ended; local kept services still
	// have their own supervisor and are stopped independently below.
	live := b.liveOf(a)
	res := &appwire.ProcessStopAllResult{
		Agent: agent, Scope: appwire.ProcessScopeAgent,
		NotStopped: []string{}, ServicesNotStopped: []string{}, Unsupported: []string{},
		JobsDeadlineMS: int(session.ProcessStopAllWait / time.Millisecond),
	}
	if deadline, ok := ctx.Deadline(); ok {
		res.ServicesDeadlineMS = max(0, int(time.Until(deadline)/time.Millisecond))
	}
	var (
		wg       sync.WaitGroup
		jobs     session.ProcessStopReceipt
		jobsErr  error
		services appStopServices
	)
	if live != nil {
		wg.Add(1)
		guard.Go("stop-all-jobs", func() {
			defer wg.Done()
			actor, can := live.conn.Conv.(appProcessActor)
			if !can {
				jobsErr = remote.ErrProcessActionsUnserved
				return
			}
			jobs, jobsErr = actor.StopConversationProcesses(ctx)
		})
	}
	wg.Add(1)
	guard.Go("stop-all-services", func() {
		defer wg.Done()
		services = b.stopOwnedServices(ctx, agent)
	})
	wg.Wait()

	switch {
	case live == nil:
		res.RemainingUnknown = true
	case errors.Is(jobsErr, remote.ErrProcessActionsUnserved):
		res.Unsupported = append(res.Unsupported, appwire.ProcessStopAllJobs)
	case jobsErr != nil:
		// THE ENGINE DID NOT ANSWER IN TIME, OR FAILED: what it stopped is not
		// known here, and the receipt says so rather than counting nothing.
		res.RemainingUnknown = true
	default:
		res.Eligible, res.Stopped, res.AlreadyEnded = jobs.Eligible, jobs.Stopped, jobs.AlreadyEnded
		res.Unenumerated, res.RemainingUnknown = jobs.Unenumerated, jobs.RemainingUnknown
		for _, n := range jobs.NotStopped {
			if len(res.NotStopped) == appwire.ProcessStopAllIDs {
				res.RemainingUnknown = true
				break
			}
			res.NotStopped = append(res.NotStopped, appProcessID(agent, n))
		}
	}
	if services.unsupported {
		res.Unsupported = append(res.Unsupported, appwire.ProcessStopAllServices)
	}
	res.ServicesEligible, res.ServicesStopped, res.ServicesNotStopped = services.eligible, services.stopped, services.notStopped
	if services.cut {
		res.RemainingUnknown = true
	}
	res.Partial = res.Unenumerated > 0 || res.RemainingUnknown || len(res.Unsupported) > 0
	return res, nil
}

// appStopServices is what stopping one agent's services did.
type appStopServices struct {
	eligible, stopped int
	notStopped        []string
	unsupported, cut  bool
}

// stopOwnedServices stops every service agent owns — never another owner's —
// and waits under ctx for each to be proven gone, naming those that were not.
func (b *appBackend) stopOwnedServices(ctx context.Context, agent string) appStopServices {
	out := appStopServices{notStopped: []string{}}
	svc, err := b.servicesOf()
	if err != nil {
		out.unsupported = true
		return out
	}
	n, err := svc.sup.StopOwnerWait(ctx, agent, service.StartedByPerson)
	if errors.Is(err, service.ErrClosed) {
		out.unsupported = true
		return out
	}
	out.eligible = n
	for _, row := range svc.sup.List(agent) {
		if !row.Running && !row.Stopping {
			continue
		}
		if len(out.notStopped) == appwire.ProcessStopAllIDs {
			out.cut = true
			break
		}
		out.notStopped = append(out.notStopped, row.Owner+":"+row.ID)
	}
	out.stopped = max(n-len(out.notStopped), 0)
	return out
}

// ── the stamped doors behind processes.rerun and processes.stop ────────────

// stampedRerun runs an ended process again through the engine's stamped door,
// or refuses: an engine too old to check is never asked through the old one.
func (b *appBackend) stampedRerun(agent string, live *appLive, id string, job int) (appwire.Process, error) {
	_, _, ms, err := appProcessRun(id)
	if err != nil {
		return appwire.Process{}, err
	}
	actor, can := live.conn.Conv.(appProcessActor)
	if !can {
		return appwire.Process{}, appProcessActError(remote.ErrProcessActionsUnserved)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appwire.ProcessReadTimeout)
	defer cancel()
	again, err := actor.RerunProcessAt(ctx, job, ms)
	if err != nil {
		return appwire.Process{}, appProcessActError(err)
	}
	return appProcessOf(agent, again), nil
}

// stampedStop stops a running process through the engine's stamped door, and
// says false when the engine is too old to have one.
func (b *appBackend) stampedStop(live *appLive, id string, job int) (bool, error) {
	_, _, ms, err := appProcessRun(id)
	if err != nil {
		return true, err
	}
	actor, can := live.conn.Conv.(appProcessActor)
	if !can {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), appProcessStopWait)
	defer cancel()
	out, err := actor.StopProcessAt(ctx, job, ms)
	switch {
	case errors.Is(err, remote.ErrProcessActionsUnserved):
		return false, nil
	case err != nil:
		return true, appProcessActError(err)
	case out.Refused:
		return true, appwire.Errorf(appwire.CodeConflict, "%s", out.Line)
	case out.Unproven:
		return true, appwire.Errorf(appwire.CodeEngine, "%s", out.Line)
	}
	return true, nil
}
