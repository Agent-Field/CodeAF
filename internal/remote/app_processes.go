package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// These additive doors carry the engine's own process shelf and retained
// commands. They introduce no client-supplied command execution route.
const MethodProcessList = "Process.List"
const MethodProcessRerun = "Process.Rerun"

// Process.Page and Process.Record answer one bounded page of the history and
// one record by number, from the engine that holds the conversation, so a
// window never reads another engine's conversation off the disk while that
// engine is running it.
const MethodProcessPage = "Process.Page"
const MethodProcessRecord = "Process.Record"

// processPageDeadline bounds the engine's walk when the caller's deadline
// does not travel with the call.
const processPageDeadline = 2 * time.Second

type ProcessPageArgs struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
}

type ProcessRecordArgs struct {
	ID int `json:"id"`
}

type processRecordReply struct {
	Found  bool              `json:"found"`
	Record session.JobNotice `json:"record"`
}

// ErrProcessPageUnserved is an engine too old to page its history.
var ErrProcessPageUnserved = errors.New("this engine cannot page processes")

func (a *Agent) ProcessPage(ctx context.Context, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	// THE CALLER'S DEADLINE TRAVELS WITH THE CALL: a page asked for within two
	// seconds gives up at two, never at the wire's own longer ceiling.
	raw, err := a.c.call(ctx, MethodProcessPage, ProcessPageArgs{Limit: ask.Limit, Cursor: ask.Cursor})
	if err != nil {
		if strings.Contains(err.Error(), "no such process action") {
			return session.ProcessPage{}, ErrProcessPageUnserved
		}
		if strings.Contains(err.Error(), session.ErrProcessStale.Error()) {
			return session.ProcessPage{}, session.ErrProcessStale
		}
		return session.ProcessPage{}, err
	}
	var page session.ProcessPage
	err = json.Unmarshal(raw, &page)
	return page, err
}

func (a *Agent) ProcessRecord(ctx context.Context, id int) (session.JobNotice, bool, error) {
	raw, err := a.c.call(ctx, MethodProcessRecord, ProcessRecordArgs{ID: id})
	if err != nil {
		if strings.Contains(err.Error(), "no such process action") {
			return session.JobNotice{}, false, ErrProcessPageUnserved
		}
		return session.JobNotice{}, false, err
	}
	var reply processRecordReply
	err = json.Unmarshal(raw, &reply)
	return reply.Record, reply.Found, err
}

type ProcessRerunArgs struct {
	ID int `json:"id"`
}

func (a *Agent) JobNotices() ([]session.JobNotice, error) {
	raw, err := a.c.call(nil, MethodProcessList, nil)
	if err != nil {
		return nil, err
	}
	var rows []session.JobNotice
	err = json.Unmarshal(raw, &rows)
	return rows, err
}
func (a *Agent) RerunProcess(id int) (session.JobNotice, error) {
	raw, err := a.c.call(nil, MethodProcessRerun, ProcessRerunArgs{ID: id})
	if err != nil {
		return session.JobNotice{}, err
	}
	var row session.JobNotice
	err = json.Unmarshal(raw, &row)
	return row, err
}

// ── the stamped process actions ────────────────────────────────────────────

// Process.Actions answers what the engine holding a conversation will do with
// one run now; Process.RerunAt and Process.StopAt act on that run only after
// asking the same question again; Process.StopAll stops every job the engine
// runs for the conversation under one shared deadline. Each names its run by
// number AND start, so a window's id never reaches another run of the number.
const (
	MethodProcessActions = "Process.Actions"
	MethodProcessRerunAt = "Process.RerunAt"
	MethodProcessStopAt  = "Process.StopAt"
	MethodProcessStopAll = "Process.StopAll"
)

// ProcessActArgs names one run: its number and its start in Unix
// milliseconds, zero for a record with no start.
type ProcessActArgs struct {
	ID        int   `json:"id"`
	StartedMS int64 `json:"started_ms"`
	// Caller deadline survives a queued frame; an expired mutator is never admitted.
	DeadlineUnixNano int64 `json:"deadline_unix_nano,omitempty"`
}

// processActReply carries a refusal as a FACT rather than an error, so its
// reason crosses the wire as the word it is and not as a sentence to parse.
type processActReply struct {
	NotFound bool                        `json:"not_found,omitempty"`
	Refused  string                      `json:"refused,omitempty"`
	Verb     string                      `json:"verb,omitempty"`
	Actions  *session.ProcessActions     `json:"actions,omitempty"`
	Record   *session.JobNotice          `json:"record,omitempty"`
	Stop     *session.ProcessStopOutcome `json:"stop,omitempty"`
	Receipt  *session.ProcessStopReceipt `json:"receipt,omitempty"`
}

// ErrProcessActionsUnserved is an engine too old to answer the stamped doors.
var ErrProcessActionsUnserved = errors.New("this engine cannot check a process's actions")

// processActor is the stamped doors, served by an engine's own agent and by a
// remote agent served onward alike.
type processActor interface {
	ProcessActions(ctx context.Context, id int, startedMS int64) (session.ProcessActions, error)
	RerunProcessAt(ctx context.Context, id int, startedMS int64) (session.JobNotice, error)
	StopProcessAt(ctx context.Context, id int, startedMS int64) (session.ProcessStopOutcome, error)
	StopConversationProcesses(ctx context.Context) (session.ProcessStopReceipt, error)
}

func (a *Agent) processAct(ctx context.Context, method string, args ProcessActArgs) (processActReply, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return processActReply{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		args.DeadlineUnixNano = deadline.UnixNano()
	}
	raw, err := a.c.call(ctx, method, args)
	if err != nil {
		if strings.Contains(err.Error(), "no such method") || strings.Contains(err.Error(), "no such process action") {
			return processActReply{}, ErrProcessActionsUnserved
		}
		return processActReply{}, err
	}
	var reply processActReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return processActReply{}, err
	}
	switch {
	case reply.NotFound:
		return processActReply{}, session.ErrNoSuchProcess
	case reply.Refused != "":
		return processActReply{}, &session.ProcessActionRefused{Verb: reply.Verb, Reason: reply.Refused}
	}
	return reply, nil
}

func (a *Agent) ProcessActions(ctx context.Context, id int, startedMS int64) (session.ProcessActions, error) {
	reply, err := a.processAct(ctx, MethodProcessActions, ProcessActArgs{ID: id, StartedMS: startedMS})
	if err != nil || reply.Actions == nil {
		return session.ProcessActions{}, orMissing(err)
	}
	return *reply.Actions, nil
}

func (a *Agent) RerunProcessAt(ctx context.Context, id int, startedMS int64) (session.JobNotice, error) {
	reply, err := a.processAct(ctx, MethodProcessRerunAt, ProcessActArgs{ID: id, StartedMS: startedMS})
	if err != nil || reply.Record == nil {
		return session.JobNotice{}, orMissing(err)
	}
	return *reply.Record, nil
}

func (a *Agent) StopProcessAt(ctx context.Context, id int, startedMS int64) (session.ProcessStopOutcome, error) {
	reply, err := a.processAct(ctx, MethodProcessStopAt, ProcessActArgs{ID: id, StartedMS: startedMS})
	if err != nil || reply.Stop == nil {
		return session.ProcessStopOutcome{}, orMissing(err)
	}
	return *reply.Stop, nil
}

func (a *Agent) StopConversationProcesses(ctx context.Context) (session.ProcessStopReceipt, error) {
	reply, err := a.processAct(ctx, MethodProcessStopAll, ProcessActArgs{})
	if err != nil || reply.Receipt == nil {
		return session.ProcessStopReceipt{}, orMissing(err)
	}
	return *reply.Receipt, nil
}

// orMissing is err, or the error of an answer that carried nothing.
func orMissing(err error) error {
	if err != nil {
		return err
	}
	return errors.New("the engine answered without the process's facts")
}

// processActDeadline bounds the engine's own work on one stamped door: a
// stop's two graces and a second to see them land, which is the stop-all's
// one shared deadline too.
const processActDeadline = session.ProcessStopAllWait

func serveProcessAct(agent WrappedAgent, call Frame) ([]byte, error) {
	door, ok := agent.(processActor)
	if !ok {
		return nil, errors.New("no such process action")
	}
	args, aerr := arg[ProcessActArgs](call)
	if aerr != nil {
		return nil, aerr
	}
	deadline := time.Now().Add(processActDeadline)
	if args.DeadlineUnixNano != 0 {
		caller := time.Unix(0, args.DeadlineUnixNano)
		if caller.Before(deadline) {
			deadline = caller
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var reply processActReply
	var err error
	if call.Method == MethodProcessStopAll {
		receipt, serr := door.StopConversationProcesses(ctx)
		reply.Receipt, err = &receipt, serr
	} else {
		switch call.Method {
		case MethodProcessActions:
			facts, serr := door.ProcessActions(ctx, args.ID, args.StartedMS)
			reply.Actions, err = &facts, serr
		case MethodProcessRerunAt:
			record, serr := door.RerunProcessAt(ctx, args.ID, args.StartedMS)
			reply.Record, err = &record, serr
		case MethodProcessStopAt:
			stop, serr := door.StopProcessAt(ctx, args.ID, args.StartedMS)
			reply.Stop, err = &stop, serr
		default:
			return nil, errors.New("no such process action")
		}
	}
	var refused *session.ProcessActionRefused
	switch {
	case errors.Is(err, session.ErrNoSuchProcess):
		reply = processActReply{NotFound: true}
	case errors.As(err, &refused):
		reply = processActReply{Refused: refused.Reason, Verb: refused.Verb}
	case err != nil:
		return nil, err
	}
	return json.Marshal(reply)
}

func serveAppProcesses(agent WrappedAgent, call Frame) ([]byte, error) {
	switch call.Method {
	case MethodProcessActions, MethodProcessRerunAt, MethodProcessStopAt, MethodProcessStopAll:
		return serveProcessAct(agent, call)
	case MethodProcessList:
		door, ok := agent.(interface {
			JobNotices() ([]session.JobNotice, error)
		})
		if !ok {
			return nil, errors.New("this engine cannot list processes")
		}
		rows, err := door.JobNotices()
		if err != nil {
			return nil, err
		}
		return json.Marshal(rows)
	case MethodProcessRerun:
		args, err := arg[ProcessRerunArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			RerunProcess(int) (session.JobNotice, error)
		})
		if !ok {
			return nil, errors.New("this engine cannot rerun processes")
		}
		row, err := door.RerunProcess(args.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(row)
	case MethodProcessPage:
		args, err := arg[ProcessPageArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			ProcessPage(context.Context, session.ProcessPageAsk) (session.ProcessPage, error)
		})
		if !ok {
			return nil, errors.New("no such process action")
		}
		ctx, cancel := context.WithTimeout(context.Background(), processPageDeadline)
		defer cancel()
		page, err := door.ProcessPage(ctx, session.ProcessPageAsk{Limit: args.Limit, Cursor: args.Cursor})
		if err != nil {
			return nil, err
		}
		return json.Marshal(page)
	case MethodProcessRecord:
		args, err := arg[ProcessRecordArgs](call)
		if err != nil {
			return nil, err
		}
		// AN ENGINE'S AGENT ANSWERS BY NUMBER; A REMOTE AGENT SERVED ONWARD (a
		// relay) answers by number within a context. Both are the same door.
		var record session.JobNotice
		var found bool
		switch door := agent.(type) {
		case interface {
			ProcessRecord(int) (session.JobNotice, bool, error)
		}:
			record, found, err = door.ProcessRecord(args.ID)
		case interface {
			ProcessRecord(context.Context, int) (session.JobNotice, bool, error)
		}:
			ctx, cancel := context.WithTimeout(context.Background(), processPageDeadline)
			record, found, err = door.ProcessRecord(ctx, args.ID)
			cancel()
		default:
			return nil, errors.New("no such process action")
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(processRecordReply{Found: found, Record: record})
	}
	return nil, errors.New("no such process action")
}
