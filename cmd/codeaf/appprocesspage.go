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
	"path/filepath"
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
	a, err := b.find(ask.Agent)
	if err != nil {
		return nil, err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	live := b.liveOf(a)
	convDir := ""
	if transcript != "" {
		convDir = filepath.Dir(transcript)
	}
	stopped := b.quitStoppedSet(ask.Agent)
	services := func(after string, n int) ([]appwire.Process, bool, string) {
		svc, err := b.servicesOf()
		if err != nil {
			// No supervisor here: the agent keeps no services, and the empty
			// membership has its own fixed revision.
			return nil, false, ""
		}
		rows, more, revision := svc.sup.Page(ask.Agent, after, n)
		out := make([]appwire.Process, 0, len(rows))
		for _, row := range rows {
			out = append(out, appServiceProcess(row, false))
		}
		return out, more, revision
	}
	engine := func(ctx context.Context, sessionAsk session.ProcessPageAsk) (session.ProcessPage, error) {
		if live != nil {
			return b.heldProcessPage(ctx, live, sessionAsk)
		}
		return session.ReadProcessPage(ctx, transcript, sessionAsk)
	}
	rowOf := func(row session.ProcessPageRow) appwire.Process {
		p := appProcessPageRow(ask.Agent, convDir, row)
		if stopped[p.ID] && p.State != appwire.ProcessRunning {
			p.State, p.Reason, p.ExitCode, p.UnknownReason = appwire.ProcessStopped, appQuitStoppedWord, nil, ""
		}
		return p
	}
	out, err := appComposeProcessPage(ctx, ask.Cursor, limit, appwire.ProcessPageReplyMax, services, engine, rowOf)
	if err != nil {
		return nil, appProcessPageError(err)
	}
	return out, nil
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

// appBoundRow keeps one row under [appProcessRowMax].
func appBoundRow(p appwire.Process) (appwire.Process, int) {
	raw, _ := json.Marshal(p)
	if len(raw) <= appProcessRowMax {
		return p, len(raw)
	}
	cut := func(s *string) {
		if len(*s) > appProcessFieldMax {
			*s = strings.ToValidUTF8((*s)[:appProcessFieldMax], "")
		}
	}
	for _, f := range []*string{&p.Title, &p.Command, &p.LastLine, &p.Reason, &p.Dir, &p.LogPath, &p.URL} {
		cut(f)
	}
	if p.Service != nil {
		svc := *p.Service
		for _, f := range []*string{&svc.Name, &svc.Dir, &svc.OpenURL, &svc.Reason, &svc.EpisodeReason, &svc.StartedBy.Label, &svc.Health.Cmd, &svc.Health.URL, &svc.Health.Words} {
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
	raw, _ = json.Marshal(p)
	return p, len(raw)
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
	admit := func(p appwire.Process) bool {
		p, n := appBoundRow(p)
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
	if row.Live && !strings.HasPrefix(revision, appListRevision) {
		return session.ProcessCursorLive(revision, row.ID)
	}
	return session.ProcessCursorBefore(revision, row.ID)
}

// appListRevision prefixes the revision of a page cut from an old engine's
// whole list, whose cursor knows only numbers.
const appListRevision = "list"

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
	revision := appListRevision
	if len(kept) > 0 {
		revision = appListRevision + strconv.Itoa(kept[0].ID)
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
		// AN OLD ENGINE'S LIST HAS NO LIVE PHASE. Its running rows keep their
		// state, and are not marked live: this list's cursor knows only
		// numbers, and a live mark would make a cut continue in a phase it
		// cannot parse.
		page.Rows = append(page.Rows, session.ProcessPageRow{JobNotice: row})
	}
	return page, nil
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
	record, convDir, err := b.processRecord(ctx, ask.ID)
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
func (b *appBackend) processRecord(ctx context.Context, id string) (session.JobNotice, string, error) {
	agent, job, stamp, ok := appProcessJob(id)
	if !ok {
		return session.JobNotice{}, "", appwire.Errorf(appwire.CodeBadParams, "%s is not a process id", id)
	}
	a, err := b.find(agent)
	if err != nil {
		return session.JobNotice{}, "", err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	convDir := ""
	if transcript != "" {
		convDir = filepath.Dir(transcript)
	}
	var record session.JobNotice
	found := false
	if live := b.liveOf(a); live != nil {
		recorder, can := live.conn.Conv.(appProcessRecorder)
		if can {
			record, found, err = recorder.ProcessRecord(ctx, job)
		}
		if !can || errors.Is(err, remote.ErrProcessPageUnserved) {
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
