package main

// processes.page and processes.log for the desktop app (internal/appwire's
// processpage.go has the contract). A held conversation is asked through its
// own engine, so the engine that runs a conversation is the only one that
// says what in it is live; a conversation nobody here holds is read off its
// store, read-only, and a record saved as running is unknown rather than
// guessed at. A log is read from the path the OWNER's record names for that
// exact process, never from a path anybody sent.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appProcessLastLineBytes bounds the tail read for a page row's last line.
const appProcessLastLineBytes = 4 << 10

type appProcessPager interface {
	ProcessPage(context.Context, session.ProcessPageAsk) (session.ProcessPage, error)
}

type appProcessRecorder interface {
	ProcessRecord(int) (session.JobNotice, bool, error)
}

// THE CONVERSATION THE APP HOLDS IS A REMOTE AGENT, even in process, so the
// owning engine's doors must stay on it or every held page falls back to the
// whole list.
var (
	_ appProcessPager    = (*remote.Agent)(nil)
	_ appProcessRecorder = (*remote.Agent)(nil)
)

// PageProcesses answers one page of an agent's process history.
func (b *appBackend) PageProcesses(ctx context.Context, ask appwire.ProcessPageAsk) (*appwire.ProcessPageResult, error) {
	limit, err := appwire.CheckProcessPageAsk(ask)
	if err != nil {
		return nil, err
	}
	a, err := b.find(ask.Agent)
	if err != nil {
		return nil, err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	live := b.liveOf(a)
	sessionAsk := session.ProcessPageAsk{Limit: limit, Cursor: ask.Cursor}
	var page session.ProcessPage
	switch {
	case live != nil:
		page, err = b.heldProcessPage(ctx, live, sessionAsk)
	default:
		page, err = session.ReadProcessPage(ctx, transcript, sessionAsk)
	}
	if err != nil {
		return nil, appProcessPageError(err)
	}
	out := &appwire.ProcessPageResult{Processes: []appwire.Process{}, Next: page.Next, Incomplete: page.Incomplete, Revision: page.Revision}
	if ask.Cursor == "" {
		out.Processes = append(out.Processes, b.serviceProcesses(ask.Agent)...)
	}
	for _, row := range page.Rows {
		out.Processes = append(out.Processes, appProcessPageRow(ask.Agent, row))
	}
	out.Processes = b.quitStoppedOver(ask.Agent, out.Processes)
	appFitProcessPage(out, page)
	return out, nil
}

// heldProcessPage asks the owning engine. An engine too old to page answers
// its whole list, which is paged here so the answer keeps the page's bounds.
func (b *appBackend) heldProcessPage(ctx context.Context, live *appLive, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	if pager, ok := live.conn.Conv.(appProcessPager); ok {
		page, err := pager.ProcessPage(ctx, ask)
		if !errors.Is(err, remote.ErrProcessPageUnserved) {
			return page, err
		}
	}
	rows, err := live.conn.Conv.JobNotices()
	if err != nil {
		return session.ProcessPage{}, err
	}
	return appPageList(rows, ask)
}

// appPageList pages a whole list from an engine that cannot page. Its cursor
// names the list it was cut from by the newest number in it.
func appPageList(rows []session.JobNotice, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	kept := rows[:0:0]
	for _, row := range rows {
		if row.Kind != session.JobKindTask {
			kept = append(kept, row)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID > kept[j].ID })
	revision := "list"
	if len(kept) > 0 {
		revision = "list" + strconv.Itoa(kept[0].ID)
	}
	before := 0
	if ask.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(ask.Cursor)
		parts := strings.Split(string(raw), ":")
		if err != nil || len(parts) != 3 || parts[0] != "p1" {
			return session.ProcessPage{}, errors.New("that cursor is not one a page gave")
		}
		if parts[1] != revision {
			return session.ProcessPage{}, session.ErrProcessStale
		}
		if before, err = strconv.Atoi(parts[2]); err != nil {
			return session.ProcessPage{}, errors.New("that cursor is not one a page gave")
		}
	}
	page := session.ProcessPage{Revision: revision}
	for _, row := range kept {
		if before > 0 && row.ID >= before {
			continue
		}
		if len(page.Rows) == ask.Limit {
			page.Next = session.ProcessCursorBefore(revision, row.ID)
			break
		}
		// An old engine already folded a saved running row into stopped; what
		// it calls running it runs.
		page.Rows = append(page.Rows, session.ProcessPageRow{JobNotice: row, Live: row.State == session.JobRunning})
	}
	return page, nil
}

// appProcessPageRow is one history row in the wire's words.
func appProcessPageRow(agent string, row session.ProcessPageRow) appwire.Process {
	n := row.JobNotice
	process := appProcessBase(agent, n)
	// THE LAST LINE IS READ WITHIN A BOUND AND WITHOUT FOLLOWING A LINK; the
	// list's own tail reader opens whatever the path names.
	tail, err := session.ReadProcessLog(n.LogPath, session.ProcessLogAsk{Limit: appProcessLastLineBytes}, nil)
	switch {
	case err == nil && tail.Gone:
		process.LogGone = true
	case err == nil:
		lines := strings.Split(strings.TrimRight(tail.Text, "\n"), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if line := strings.TrimSpace(lines[i]); line != "" {
				process.LastLine = redact.Secrets(appDetail(line))
				break
			}
		}
	}
	process.Dir = n.Dir
	process.LogPath = n.LogPath
	process.OutputTruncated = n.OutputTruncated
	switch n.StartedBy {
	case session.JobStartedByChat:
		process.StartedBy = &appwire.ProcessStarter{Kind: session.JobStartedByChat}
	case session.JobStartedByTask:
		if n.Task != 0 {
			process.StartedBy = &appwire.ProcessStarter{Kind: session.JobStartedByTask, Task: appTaskID(agent, n.Task)}
		}
	}
	// AN ENDING IS SHOWN ONLY WHEN SOMEBODY SAW IT. A recorded ending wins; a
	// clean or failed exit settled by the registry carries its final elapsed;
	// a stop with no recorded ending (the close of an engine) has none.
	if !n.Ended.IsZero() {
		ended := n.Ended
		process.EndedAt = &ended
	} else if process.State == appwire.ProcessStopped {
		process.EndedAt = nil
	}
	if row.Unknown != "" {
		process.State = appwire.ProcessUnknown
		process.UnknownReason = row.Unknown
		process.EndedAt, process.ExitCode = nil, nil
	}
	return process
}

// quitStoppedOver lays the app's own record of processes its quit stopped
// over a page's rows: that stop is a fact this app saw, so it settles a row
// the page could otherwise only call unknown.
func (b *appBackend) quitStoppedOver(agent string, list []appwire.Process) []appwire.Process {
	stopped := map[string]bool{}
	_ = b.state.read(func(f *appStateFile) {
		for _, p := range f.QuitStopped {
			if p.Agent == agent {
				stopped[p.ID] = true
			}
		}
	})
	for i := range list {
		if stopped[list[i].ID] && list[i].State != appwire.ProcessRunning {
			list[i].State, list[i].Reason, list[i].ExitCode, list[i].UnknownReason = appwire.ProcessStopped, appQuitStoppedWord, nil, ""
		}
	}
	return list
}

// appFitProcessPage keeps the encoded reply under the wire's budget. Rows cut
// here are not lost: Next continues from the first one cut.
func appFitProcessPage(out *appwire.ProcessPageResult, page session.ProcessPage) {
	raw, err := json.Marshal(out)
	if err != nil || len(raw) <= appwire.ProcessPageReplyMax {
		return
	}
	size := len(raw)
	for len(out.Processes) > 0 && size > appwire.ProcessPageReplyMax {
		last := out.Processes[len(out.Processes)-1]
		one, _ := json.Marshal(last)
		size -= len(one) + 1
		out.Processes = out.Processes[:len(out.Processes)-1]
		if _, job, _, ok := appProcessJob(last.ID); ok {
			out.Next = session.ProcessCursorBefore(page.Revision, job)
		}
	}
}

func appProcessPageError(err error) error {
	var wire *appwire.Error
	switch {
	case errors.As(err, &wire):
		return err
	case errors.Is(err, session.ErrProcessStale):
		return appwire.Errorf(appwire.CodeStaleCursor, "the process history changed; read it again from the first page")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	case strings.Contains(err.Error(), "cursor is not one a page gave"):
		return appwire.Errorf(appwire.CodeBadParams, "cursor is one a previous page gave")
	}
	return appEngineError(err)
}

// ReadProcessLog answers one window of a process's output.
func (b *appBackend) ReadProcessLog(ctx context.Context, ask appwire.ProcessLogAsk) (*appwire.ProcessLogResult, error) {
	limit, err := appwire.CheckProcessLogAsk(ask)
	if err != nil {
		return nil, err
	}
	if _, _, ok := appServiceID(ask.ID); ok {
		return nil, appwire.Errorf(appwire.CodeBadParams, "a kept-running process's output is read with processes.output")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := b.processRecord(ask.ID)
	if err != nil {
		return nil, err
	}
	read, err := session.ReadProcessLog(record.LogPath, session.ProcessLogAsk{Offset: ask.Offset, Limit: limit, Revision: ask.Revision}, redact.Secrets)
	switch {
	case errors.Is(err, session.ErrProcessLogStale):
		return nil, appwire.Errorf(appwire.CodeStaleCursor, "the output changed under that offset; read it again from the end")
	case err != nil:
		return nil, appwire.Errorf(appwire.CodeEngine, "the output could not be read")
	}
	return &appwire.ProcessLogResult{
		Text: read.Text, Offset: read.Offset, Next: read.Next, Size: read.Size, EOF: read.EOF,
		MoreBefore: read.MoreBefore, OutputTruncated: record.OutputTruncated, LogGone: read.Gone, Revision: read.Revision,
	}, nil
}

// processRecord is the record a process id names, from the engine holding
// its conversation or, when none here does, its store. The start stamp in the
// id must match, so an id never reaches another run of the same number.
func (b *appBackend) processRecord(id string) (session.JobNotice, error) {
	agent, job, stamp, ok := appProcessJob(id)
	if !ok {
		return session.JobNotice{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	a, err := b.find(agent)
	if err != nil {
		return session.JobNotice{}, err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	var record session.JobNotice
	found := false
	if live := b.liveOf(a); live != nil {
		recorder, can := live.conn.Conv.(appProcessRecorder)
		if can {
			record, found, err = recorder.ProcessRecord(job)
		}
		if !can || errors.Is(err, remote.ErrProcessPageUnserved) {
			_, row, rerr := b.processRow(id)
			if rerr != nil {
				return session.JobNotice{}, rerr
			}
			record, found, err = row, true, nil
		}
	} else {
		record, found, err = session.ReadProcessRecord(transcript, job)
	}
	if err != nil {
		return session.JobNotice{}, appEngineError(err)
	}
	if !found || (stamp != "" && appProcessStamp(record) != stamp) {
		return session.JobNotice{}, appwire.Errorf(appwire.CodeNotFound, "there is no process %s", id)
	}
	return record, nil
}
