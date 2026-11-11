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
	ProcessRecord(context.Context, int) (session.JobNotice, bool, error)
}

// THE CONVERSATION THE APP HOLDS IS A REMOTE AGENT, even in process, so the
// owning engine's doors must stay on it or every held page falls back to the
// whole list.
var (
	_ appProcessPager    = (*remote.Agent)(nil)
	_ appProcessRecorder = (*remote.Agent)(nil)
)

// ONE PAGE IS EVERY KIND OF PROCESS, AND EVERY ROW COUNTS. A page answers the
// agent's kept-running services first, then the processes its engine runs
// now, then its saved history, and every one of those rows counts against the
// limit and against the reply's bytes. The cursor carries which phase it is
// in: services after a service id (with the revision of the agent's service
// membership, so a set that changed under the reader is said, never skipped
// or repeated), then the engine's own cursor (live rows, then history).
// Nothing is dropped to fit: a row that does not fit begins the next page.

// appProcessRowMax bounds one row's encoding. A row longer than this — a
// service or a command with megabytes of text in it — has its long text cut
// to [appProcessFieldMax] and says so with cut, so no single row can be
// larger than a page and every page moves forward.
const (
	appProcessRowMax   = 256 << 10
	appProcessFieldMax = 4 << 10
	// appProcessPageEnvelope is what a reply keeps for itself beyond its rows.
	appProcessPageEnvelope = 64 << 10
)

// PageProcesses answers one page of an agent's process history.
func (b *appBackend) PageProcesses(ctx context.Context, ask appwire.ProcessPageAsk) (*appwire.ProcessPageResult, error) {
	limit, err := appwire.CheckProcessPageAsk(ask)
	if err != nil {
		return nil, err
	}
	a, err := b.findCrewOwner(ask.Agent)
	if err != nil {
		return nil, err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	convDir := ""
	if transcript != "" {
		convDir = session.DirOf(transcript)
	}
	owner := appProcessOwner{
		agent: ask.Agent, live: b.liveOf(a), convDir: convDir,
		read: func(ctx context.Context, sessionAsk session.ProcessPageAsk) (session.ProcessPage, error) {
			return session.ReadProcessPage(ctx, transcript, sessionAsk)
		},
	}
	out, err := b.pageOwner(ctx, owner, ask.Cursor, limit, appwire.ProcessPageReplyMax)
	if err != nil {
		return nil, appProcessPageError(err)
	}
	return out, nil
}

// appProcessOwner is where one conversation's history is read from. A held
// conversation (live) is asked through its own engine; otherwise read answers
// from its store. convDir is the folder its managed logs are under, "" when
// that folder is not one this reader may open (the last line is then left out
// rather than read unsafely).
type appProcessOwner struct {
	agent   string
	live    *appLive
	convDir string
	read    func(context.Context, session.ProcessPageAsk) (session.ProcessPage, error)
}

// pageOwner is one page of one conversation's history, under limit rows and
// budget bytes: the page the agent form answers, and the visit a crew page
// makes to each of its owners.
func (b *appBackend) pageOwner(ctx context.Context, o appProcessOwner, cursor string, limit, budget int) (*appwire.ProcessPageResult, error) {
	stopped := b.quitStoppedSet(o.agent)
	services := func(after string, n int) ([]appwire.Process, bool, string) {
		svc, err := b.servicesOf()
		if err != nil {
			// No supervisor here: the agent keeps no services, and the empty
			// membership has its own fixed revision.
			return nil, false, ""
		}
		rows, more, revision := svc.sup.Page(o.agent, after, n)
		out := make([]appwire.Process, 0, len(rows))
		for _, row := range rows {
			// NO TAIL IS READ FOR A PAGED SERVICE: its last line is absent
			// (unknown) rather than read from a path through a reader that
			// follows links; processes.output still reads it.
			out = append(out, appServiceProcessBase(row, false))
		}
		return out, more, revision
	}
	engine := func(ctx context.Context, sessionAsk session.ProcessPageAsk) (session.ProcessPage, error) {
		if o.live != nil {
			return b.heldProcessPage(ctx, o.live, sessionAsk)
		}
		return o.read(ctx, sessionAsk)
	}
	rowOf := func(row session.ProcessPageRow) appwire.Process {
		p := appProcessPageRow(o.agent, o.convDir, row)
		if stopped[p.ID] && p.State != appwire.ProcessRunning {
			p.State, p.Reason, p.ExitCode, p.UnknownReason = appwire.ProcessStopped, appQuitStoppedWord, nil, ""
		}
		return p
	}
	return appComposeProcessPage(ctx, cursor, limit, budget, services, engine, rowOf)
}

// The app's page cursor, around the engine's.
const (
	appCursorVersion = "a1"
	appCursorSep     = "\x1f"
)

type appPageCursor struct {
	services bool
	revision string // the service membership's, in the services phase
	after    string // the last service id answered
	engine   string // the engine's own cursor, in the engine phase
}

func (c appPageCursor) encode() string {
	parts := []string{appCursorVersion, "E", c.engine}
	if c.services {
		parts = []string{appCursorVersion, "S", c.revision, c.after}
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, appCursorSep)))
}

var errAppCursor = errors.New("that cursor is not one a page gave")

func decodeAppPageCursor(cursor string) (appPageCursor, error) {
	if cursor == "" {
		return appPageCursor{services: true}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return appPageCursor{}, errAppCursor
	}
	parts := strings.Split(string(raw), appCursorSep)
	switch {
	case len(parts) == 4 && parts[0] == appCursorVersion && parts[1] == "S" && (parts[3] == "" || appServiceLocal.MatchString(parts[3])):
		return appPageCursor{services: true, revision: parts[2], after: parts[3]}, nil
	case len(parts) == 3 && parts[0] == appCursorVersion && parts[1] == "E":
		return appPageCursor{engine: parts[2]}, nil
	}
	return appPageCursor{}, errAppCursor
}

// appBoundRow keeps one row's ENCODED size under [appProcessRowMax], or says it
// cannot.
//
// THE BOUND IS ON THE BYTES THAT LEAVE, NOT ON THE TEXT GOING IN. JSON writes
// `<`, `>`, `&` and control characters as six bytes each, so a field of 4 KiB
// of shell punctuation is 24 KiB on the wire, and a row cut by raw length
// could still be far over its bound. So the row's free text is cut to a
// smaller cap on each pass — 4 KiB, 1 KiB, 256, 64, 16, nothing — and
// encoded again, until it fits. What is never cut is bounded by the code
// that makes it: the ids (the agent is at most 128 bytes on the request), the
// enumerations, the times and the numbers. A row that still did not fit with
// every free text emptied would be one this page cannot represent, and it is
// refused rather than sent over its bound.
func appBoundRow(p appwire.Process) (appwire.Process, int, bool) {
	raw, _ := json.Marshal(p)
	if len(raw) <= appProcessRowMax {
		return p, len(raw), true
	}
	for _, limit := range []int{appProcessFieldMax, 1 << 10, 256, 64, 16, 0} {
		p = appCutRowText(p, limit)
		raw, _ = json.Marshal(p)
		if len(raw) <= appProcessRowMax {
			return p, len(raw), true
		}
	}
	return p, len(raw), false
}

// appCutRowText cuts every free-text field of a row to at most limit bytes,
// at a character boundary, and marks the row cut.
func appCutRowText(p appwire.Process, limit int) appwire.Process {
	cut := func(s *string) {
		if len(*s) > limit {
			*s = strings.ToValidUTF8((*s)[:limit], "")
		}
	}
	for _, f := range []*string{&p.Title, &p.Command, &p.LastLine, &p.Reason, &p.Dir, &p.LogPath, &p.URL} {
		cut(f)
	}
	if p.Service != nil {
		svc := *p.Service
		for _, f := range []*string{&svc.Name, &svc.Dir, &svc.OpenURL, &svc.Reason, &svc.Episode, &svc.EpisodeReason, &svc.StoppedBy, &svc.StartedBy.Label, &svc.StartedBy.CallID, &svc.Health.Cmd, &svc.Health.URL, &svc.Health.Words} {
			cut(f)
		}
		if svc.Check != nil {
			check := *svc.Check
			cut(&check.Detail)
			svc.Check = &check
		}
		p.Service = &svc
	}
	p.Cut = true
	return p
}

// appComposeProcessPage is one page across the three phases, under limit rows
// and budget bytes. Its sources are given, so the rule is proved without a
// supervisor, an engine or a four-megabyte fixture.
func appComposeProcessPage(ctx context.Context, cursor string, limit, budget int,
	services func(after string, n int) ([]appwire.Process, bool, string),
	engine func(context.Context, session.ProcessPageAsk) (session.ProcessPage, error),
	rowOf func(session.ProcessPageRow) appwire.Process,
) (*appwire.ProcessPageResult, error) {
	cur, err := decodeAppPageCursor(cursor)
	if err != nil {
		return nil, err
	}
	out := &appwire.ProcessPageResult{Processes: []appwire.Process{}}
	used := appProcessPageEnvelope
	var unrepresentable error
	admit := func(p appwire.Process) bool {
		p, n, ok := appBoundRow(p)
		if !ok {
			unrepresentable = appwire.Errorf(appwire.CodeTooLarge, "process %s cannot be carried in one page row", p.ID)
			return false
		}
		if len(out.Processes) == limit || used+n+1 > budget {
			return false
		}
		used += n + 1
		out.Processes = append(out.Processes, p)
		return true
	}
	if cur.services {
		rows, more, revision := services(cur.after, limit)
		if cursor != "" && cur.revision != revision {
			return nil, session.ErrProcessStale
		}
		last := cur.after
		for _, p := range rows {
			if !admit(p) {
				if unrepresentable != nil {
					return nil, unrepresentable
				}
				out.Next = appPageCursor{services: true, revision: revision, after: last}.encode()
				return out, nil
			}
			_, local, _ := appServiceID(p.ID)
			last = local
		}
		if more {
			out.Next = appPageCursor{services: true, revision: revision, after: last}.encode()
			return out, nil
		}
		cur = appPageCursor{}
	}
	remaining := limit - len(out.Processes)
	if remaining == 0 {
		out.Next = cur.encode()
		return out, nil
	}
	page, err := engine(ctx, session.ProcessPageAsk{Limit: remaining, Cursor: cur.engine})
	if err != nil {
		return nil, err
	}
	out.Incomplete, out.Revision = page.Incomplete, page.Revision
	for _, row := range page.Rows {
		if !admit(rowOf(row)) {
			if unrepresentable != nil {
				return nil, unrepresentable
			}
			out.Next = appPageCursor{engine: appEngineCursorAt(page.Revision, row)}.encode()
			return out, nil
		}
	}
	if page.Next != "" {
		out.Next = appPageCursor{engine: page.Next}.encode()
	}
	return out, nil
}

// appEngineCursorAt is the engine cursor that begins with row, in its phase:
// a live row continues the live rows, any other the saved history below it.
func appEngineCursorAt(revision string, row session.ProcessPageRow) string {
	if row.Live {
		return session.ProcessCursorLive(revision, row.ID)
	}
	return session.ProcessCursorBefore(revision, row.ID)
}

// heldProcessPage asks the owning engine, and ONLY it. An engine that cannot
// page its history is refused as unknown_method: the alternative — fetching
// its whole legacy list to page here — is a read no bound or deadline covers,
// which is exactly what this method exists not to do. processes.list still
// lists such an engine's processes, as it always did, for a surface that
// chooses it.
func (b *appBackend) heldProcessPage(ctx context.Context, live *appLive, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	pager, ok := live.conn.Conv.(appProcessPager)
	if !ok {
		return session.ProcessPage{}, appProcessUnpaged()
	}
	page, err := pager.ProcessPage(ctx, ask)
	if errors.Is(err, remote.ErrProcessPageUnserved) {
		return session.ProcessPage{}, appProcessUnpaged()
	}
	return page, err
}

// appProcessUnpaged is the refusal for an engine that cannot page.
func appProcessUnpaged() error {
	return appwire.Errorf(appwire.CodeUnknownMethod, "this conversation's engine cannot page its processes; processes.list still lists them")
}

// appProcessPageRow is one history row in the wire's words.
func appProcessPageRow(agent, convDir string, row session.ProcessPageRow) appwire.Process {
	n := row.JobNotice
	process := appProcessBase(agent, n)
	// THE LAST LINE IS READ WITHIN A BOUND AND WITHOUT FOLLOWING A LINK; the
	// list's own tail reader opens whatever the path names.
	tail, err := session.ReadProcessLog(convDir, n, session.ProcessLogAsk{Limit: appProcessLastLineBytes, Final: n.Over() && row.Unknown == ""}, nil)
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

// quitStoppedSet is the ids of the agent's processes this app's quit
// stopped: a stop this app saw, so it settles a row the page could otherwise
// only call unknown.
func (b *appBackend) quitStoppedSet(agent string) map[string]bool {
	stopped := map[string]bool{}
	_ = b.state.read(func(f *appStateFile) {
		for _, p := range f.QuitStopped {
			if p.Agent == agent {
				stopped[p.ID] = true
			}
		}
	})
	return stopped
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
	case errors.Is(err, errAppCursor), strings.Contains(err.Error(), "cursor is not one a page gave"):
		return appwire.Errorf(appwire.CodeBadParams, "cursor is one a previous page gave")
	}
	return appEngineError(err)
}

// ReadProcessLog answers one window of a process's output, from the file its
// conversation's managed logs folder holds for that exact job. An ended
// process's last unterminated line is its last line; a running one's is left
// until it is finished.
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
	record, convDir, err := b.processRecord(ctx, ask.ID, false)
	if err != nil {
		return nil, err
	}
	read, err := session.ReadProcessLog(convDir, record, session.ProcessLogAsk{Offset: ask.Offset, Limit: limit, Revision: ask.Revision, Final: record.Over()}, redact.Secrets)
	switch {
	case errors.Is(err, session.ErrProcessLogStale):
		return nil, appwire.Errorf(appwire.CodeStaleCursor, "the output changed under that offset; read it again from the end")
	case session.ErrProcessLogUnmanaged(err):
		return nil, appwire.Errorf(appwire.CodeEngine, "this process's output is not in its conversation's logs folder")
	case err != nil:
		return nil, appwire.Errorf(appwire.CodeEngine, "the output could not be read")
	}
	return &appwire.ProcessLogResult{
		Text: read.Text, Offset: read.Offset, Next: read.Next, Size: read.Size, EOF: read.EOF, Partial: read.Partial,
		MoreBefore: read.MoreBefore, OutputTruncated: record.OutputTruncated, LogGone: read.Gone, Revision: read.Revision,
	}, nil
}

// processRecord is the record a process id names, from the engine holding
// its conversation or, when none here does, its store, and the conversation's
// folder its log must be under. The start stamp in the id must match, so an id
// never reaches another run of the same number.
func (b *appBackend) processRecord(ctx context.Context, id string, legacyList bool) (session.JobNotice, string, error) {
	agent, job, stamp, ok := appProcessJob(id)
	if !ok {
		return session.JobNotice{}, "", appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return session.JobNotice{}, "", err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	convDir := ""
	if transcript != "" {
		convDir = session.DirOf(transcript)
	}
	var record session.JobNotice
	found := false
	if live := b.liveOf(a); live != nil {
		recorder, can := live.conn.Conv.(appProcessRecorder)
		if can {
			record, found, err = recorder.ProcessRecord(ctx, job)
		}
		if !can || errors.Is(err, remote.ErrProcessPageUnserved) {
			// ONLY THE LEGACY VERBS may find a process in an old engine's
			// whole list (stop, rerun, output, follow did so before this
			// wave). processes.log is a bounded method and refuses instead.
			if !legacyList {
				return session.JobNotice{}, "", appProcessUnpaged()
			}
			_, row, rerr := b.processRowFromList(id)
			if rerr != nil {
				return session.JobNotice{}, "", rerr
			}
			record, found, err = row, true, nil
		}
	} else {
		record, found, err = session.ReadProcessRecord(transcript, job)
	}
	if err != nil {
		return session.JobNotice{}, "", appEngineError(err)
	}
	if !found || (stamp != "" && appProcessStamp(record) != stamp) {
		return session.JobNotice{}, "", appwire.Errorf(appwire.CodeNotFound, "there is no process %s", id)
	}
	return record, convDir, nil
}

// appRunningPages bounds how many pages a sleep or a quit reads to find the
// running processes.
const appRunningPages = 20

// runningJobs is the processes the held conversation's own engine runs now,
// for a sleep or a quit to stop. It reads the page's live rows, which lead the
// first pages, and stops at the first saved row — never the whole history.
// An engine too old to page answers from its list.
func (b *appBackend) runningJobs(live *appLive) ([]session.JobNotice, error) {
	if pager, ok := live.conn.Conv.(appProcessPager); ok {
		var out []session.JobNotice
		ask := session.ProcessPageAsk{Limit: session.ProcessPageMax}
		for pages := 0; pages < appRunningPages; pages++ {
			ctx, cancel := context.WithTimeout(context.Background(), appwire.ProcessReadTimeout)
			page, err := pager.ProcessPage(ctx, ask)
			cancel()
			if errors.Is(err, remote.ErrProcessPageUnserved) {
				break
			}
			if err != nil {
				return nil, err
			}
			for _, row := range page.Rows {
				if !row.Live {
					return out, nil
				}
				if row.State == session.JobRunning && row.Kind != session.JobKindTask {
					out = append(out, row.JobNotice)
				}
			}
			if page.Next == "" {
				return out, nil
			}
			ask.Cursor = page.Next
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	rows, err := live.conn.Conv.JobNotices()
	if err != nil {
		return nil, err
	}
	var out []session.JobNotice
	for _, row := range rows {
		if row.State == session.JobRunning && row.Kind != session.JobKindTask {
			out = append(out, row)
		}
	}
	return out, nil
}
