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
func serveAppProcesses(agent WrappedAgent, call Frame) ([]byte, error) {
	switch call.Method {
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
