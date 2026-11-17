package main

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The admission of tasks.program_report. A report is up to 2 MiB read off the
// disk and about 2.8 MiB of text on the wire, so the process reads at most
// appProgramReportReads at a time. A slot is taken BEFORE anything is opened and held
// until the answer is built and this call returns: there is no queue, no parked
// request and no goroutine, so a third request is refused busy at once, and a
// read that cannot finish inside appProgramReportTime is busy too, never an empty or a
// partial report.
const appProgramReportReads = 2

// appProgramReportTime is a variable for a test alone.
var appProgramReportTime = 3 * time.Second

// appProgramReportReader is a conversation that serves its programs' reports
// (internal/session's AppProgramReport). A far conversation does not.
type appProgramReportReader interface {
	AppProgramReport(ctx context.Context, id, launch, name string) (session.AppProgramReport, bool, error)
}

// ProgramReport answers one report of a task's program (tasks.program_report).
func (b *appBackend) ProgramReport(id, launch, name string) (appwire.ProgramReportResult, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.ProgramReportResult{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	if !delegate.ValidLaunch(launch) || !delegate.ValidReportName(name) {
		return appwire.ProgramReportResult{}, appwire.Errorf(appwire.CodeBadParams, "tasks.program_report needs a launch id and a bare report name")
	}
	ctx, release, err := b.reserveProgramReport()
	if err != nil {
		return appwire.ProgramReportResult{}, err
	}
	defer release()
	_, live, endLive, err := b.borrowLive(agent)
	if err != nil {
		return appwire.ProgramReportResult{}, err
	}
	defer endLive()
	reader, ok := live.conn.Conv.(appProgramReportReader)
	if !ok {
		return appwire.ProgramReportResult{}, appwire.Errorf(appwire.CodeUnknownMethod, "this engine does not serve tasks.program_report")
	}
	report, found, err := reader.AppProgramReport(ctx, local, launch, name)
	if err != nil {
		return appwire.ProgramReportResult{}, appProgramReportError(ctx, err)
	}
	if !found {
		return appwire.ProgramReportResult{}, appwire.Errorf(appwire.CodeNotFound, "there is no program's task %s", id)
	}
	// THE BYTES ARE CHECKED AGAIN HERE, NOT ONLY WHERE THEY WERE READ: a
	// conversation that is not this process's is held to the same bound and the
	// same shape before they are encoded.
	if len(report.Data) > appwire.ProgramReportMax {
		return appwire.ProgramReportResult{}, appwire.Errorf(appwire.CodeTooLarge, "the report is over 2 MiB")
	}
	return appwire.ProgramReportResult{ID: id, Program: report.Program, Launch: report.Launch, Name: report.Name,
		StartedAt: report.StartedAt, EndedAt: report.EndedAt, Bytes: len(report.Data), SHA256: report.SHA256,
		Data: base64.StdEncoding.EncodeToString(report.Data)}, nil
}

// reserveProgramReport takes one of the appProgramReportReads slots and answers the context
// the read runs under (its deadline, and Close cancelling it) and the release
// that gives the slot back. A full process, or one closing, is refused here.
func (b *appBackend) reserveProgramReport() (context.Context, func(), error) {
	var parent context.Context
	var err error
	b.locked(func() {
		switch {
		case b.closing:
			err = appwire.Errorf(appwire.CodeClosed, "the app is closing")
		case b.reportReads >= appProgramReportReads:
			err = appwire.Errorf(appwire.CodeBusy, "two reports are being read; ask again in a moment")
		default:
			if b.reportCtx == nil {
				b.reportCtx, b.reportCancel = context.WithCancel(context.Background())
			}
			b.reportReads++
			parent = b.reportCtx
		}
	})
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(parent, appProgramReportTime)
	return ctx, func() {
		cancel()
		b.locked(func() { b.reportReads-- })
	}, nil
}

// appProgramReportError is a refusal to serve a report in the wire's words.
func appProgramReportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return appwire.Errorf(appwire.CodeBusy, "the report could not be read in time; ask again")
	case errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled:
		return appwire.Errorf(appwire.CodeClosed, "the app is closing")
	case errors.Is(err, delegate.ErrReportUndeclared):
		return appwire.Errorf(appwire.CodeBadParams, "%v", err)
	case errors.Is(err, delegate.ErrReportMoved), errors.Is(err, delegate.ErrReportChanged), errors.Is(err, delegate.ErrReportNotReceipted):
		return appwire.Errorf(appwire.CodeConflict, "%v", err)
	case errors.Is(err, delegate.ErrReportNotFinal), errors.Is(err, delegate.ErrReportNotIssued), errors.Is(err, delegate.ErrReportMissing),
		errors.Is(err, delegate.ErrReportNoRecord):
		return appwire.Errorf(appwire.CodeNotFound, "%v", err)
	case errors.Is(err, delegate.ErrReportTooLarge):
		return appwire.Errorf(appwire.CodeTooLarge, "%v", err)
	case errors.Is(err, delegate.ErrReportUnsafe), errors.Is(err, delegate.ErrReportUnreadable), errors.Is(err, delegate.ErrReportUnsupported):
		return appwire.Errorf(appwire.CodeEngine, "%v", err)
	}
	return appEngineError(err)
}

var _ appwire.ProgramReporter = (*appBackend)(nil)
