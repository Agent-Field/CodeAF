package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AppProcessOutputBytes is the disk limit for the app's process output. The
// terminal keeps its existing chunk policy; both readers keep the 64 KiB ring.
const AppProcessOutputBytes = 10 << 20
const AppProcessOutputRetention = 7 * 24 * time.Hour

// A conversation has one engine writer. This lock orders its asynchronous
// naming and exit updates so a late name cannot overwrite a settled process.
//
// IT IS A ONE-SLOT CHANNEL AND NOT A MUTEX so a reader can wait for it within
// its own deadline ([recordLock.LockCtx]) and give up honestly, instead of
// queueing behind writers for as long as they take. Every holder is bounded:
// a write folds legacy arrays for at most [processFoldWriteTime], and every
// file it reads is opened non-blocking.
var processRecordMu = make(recordLock, 1)

type recordLock chan struct{}

func (l recordLock) Lock()   { l <- struct{}{} }
func (l recordLock) Unlock() { <-l }

// LockCtx takes the lock or answers the context's error when it ends first.
func (l recordLock) LockCtx(ctx context.Context) error {
	select {
	case l <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processConvDir is the folder whose history this agent's jobs are written
// into. A conversation writes beside its own transcript, as the shelf always
// did. A task worker carries no Place of its own and writes into the
// conversation that owns its family (its droppings Place), so the
// conversation's history holds its task's processes. Anything else writes
// beside its own SessionFile.
func (a *Agent) processConvDir() string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.config.Place.Dir) == "" {
		if family := a.config.droppings.Dir; strings.TrimSpace(family) != "" {
			return family
		}
	}
	if file := a.SessionPath(); file != "" {
		return filepath.Dir(file)
	}
	return ""
}

// processOwner is who started this agent's jobs, as only this agent's own
// construction can say: a task worker knows its node id, and the conversation
// agent is the one whose transcript is its Place's transcript. Anything else
// says nothing.
func (a *Agent) processOwner() (string, uint64) {
	if a.config.taskID != 0 {
		return JobStartedByTask, a.config.taskID
	}
	if a.config.Place.Dir != "" && a.SessionPath() != "" && a.SessionPath() == a.config.Place.Transcript() {
		return JobStartedByChat, 0
	}
	return "", 0
}

func (a *Agent) closedForRecords() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

// keepProcessNotice writes one job's record. It writes ONE small file, never
// the history (process_store.go), and marks the job live for the
// conversation's own page while it runs.
func (a *Agent) keepProcessNotice(n JobNotice) {
	dir := a.processConvDir()
	if n.Kind == JobKindTask || dir == "" {
		return
	}
	n.StartedBy, n.Task = a.processOwner()
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	if a.closedForRecords() {
		return
	}
	root, head, err := processStore{dir: dir}.open(processFoldWriteTime)
	if err != nil {
		return
	}
	defer root.Close()
	if _, err := (processStore{dir: dir}).put(root, &head, n, true); err != nil {
		return
	}
	liveProcessSet(dir, n.ID, a, !n.Over())
}

// ErrProcessListPartial is the legacy whole list refusing to answer a
// history it cannot carry whole: longer than [processLegacyListMax], holding
// records that cannot be read, or not walked to its end within the list's
// deadline. The old shape has no way to say "part of it", so it says nothing
// rather than presenting a part as the whole; processes.page carries the
// rest, with its cursor and its incomplete flag.
var ErrProcessListPartial = errors.New("the process history does not fit the whole list; read it with processes.page")

// processLegacyListTime bounds one whole-list read.
const processLegacyListTime = 2 * time.Second

// ReadProcessNotices is the legacy whole-list read for a reader that does not
// hold the conversation, with a saved running record shown stopped as this
// list always showed it, or [ErrProcessListPartial]. It writes nothing.
func ReadProcessNotices(file string) ([]JobNotice, error) {
	if file == "" {
		return []JobNotice{}, nil
	}
	rows, err := legacyListOf(processStore{dir: filepath.Dir(file)}, nil, nil, false)
	if err != nil {
		return nil, err
	}
	document, _ := loadTaskCheckpoint(taskCheckpointPath(file))
	var kept []JobNotice
	for _, record := range document.Runs {
		if n, ok := jobNoticeFromRow(runRowNotice(record)); ok {
			kept = append(kept, n)
		}
	}
	rows = mergeKeptJobs(rows, kept)
	if len(rows) > processLegacyListMax {
		return nil, ErrProcessListPartial
	}
	return rows, nil
}

// legacyListOf walks the whole history into the legacy list's shape, or
// answers [ErrProcessListPartial]. It stops on the deadline and on a page that
// moved nowhere, so it can never spin.
func legacyListOf(s processStore, running []ProcessPageRow, vouch func(int) (JobNotice, bool), owner bool) ([]JobNotice, error) {
	ctx, cancel := context.WithTimeout(context.Background(), processLegacyListTime)
	defer cancel()
	out := []JobNotice{}
	ask := ProcessPageAsk{Limit: ProcessPageMax}
	for {
		page, err := s.page(ctx, ask, ProcessPageMax, vouch, running, owner)
		if err != nil {
			return nil, err
		}
		if page.Incomplete {
			return nil, ErrProcessListPartial
		}
		for _, row := range page.Rows {
			n := row.JobNotice
			if row.Unknown != "" {
				// THE LEGACY LIST HAS NO WORD FOR UNKNOWN. It keeps the state it
				// always showed, and no longer pairs it with an invented ending.
				n.State, n.Ended = JobStopped, time.Time{}
			}
			out = append(out, n)
		}
		switch {
		case len(out) > processLegacyListMax+len(running):
			return nil, ErrProcessListPartial
		case page.Next == "":
			return out, nil
		case ctx.Err() != nil, len(page.Rows) == 0 && page.Next == ask.Cursor:
			return nil, ErrProcessListPartial
		}
		ask.Cursor = page.Next
	}
}

// liveRowsOf is the running jobs among notices, newest first, as live rows.
func liveRowsOf(live map[int]JobNotice) []ProcessPageRow {
	rows := make([]ProcessPageRow, 0, len(live))
	for _, n := range live {
		if n.State == JobRunning {
			rows = append(rows, ProcessPageRow{JobNotice: n, Live: true})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	return rows
}

// conversationLive is every job this engine holds for the conversation — its
// own and its task workers' — settled ones included, for vouching.
func (a *Agent) conversationLive(dir string) map[int]JobNotice {
	live := a.ownLiveJobNotices()
	if dir != "" {
		for id, n := range liveProcessesOf(dir) {
			if _, held := live[id]; !held {
				live[id] = n
			}
		}
	}
	return live
}

// JobNotices joins the saved history with the live registry, as the legacy
// whole list, or answers [ErrProcessListPartial].
func (a *Agent) JobNotices() ([]JobNotice, error) {
	dir := a.processConvDir()
	live := a.ownLiveJobNotices()
	if dir == "" {
		rows := make([]JobNotice, 0, len(live))
		for _, n := range live {
			rows = append(rows, n)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
		return rows, nil
	}
	owner := !a.closedForRecords()
	var vouch func(int) (JobNotice, bool)
	if owner {
		vouch = func(id int) (JobNotice, bool) { n, ok := live[id]; return n, ok }
	}
	rows, err := legacyListOf(processStore{dir: dir}, liveRowsOf(live), vouch, owner)
	if err != nil {
		return nil, err
	}
	graph := a.graph()
	graph.mu.Lock()
	var kept []JobNotice
	for _, row := range graph.runRowsLocked() {
		if n, ok := jobNoticeFromRow(row); ok {
			kept = append(kept, n)
		}
	}
	graph.mu.Unlock()
	rows = mergeKeptJobs(rows, kept)
	if len(rows) > processLegacyListMax+len(liveRowsOf(live)) {
		return nil, ErrProcessListPartial
	}
	return rows, nil
}

// ownLiveJobNotices is this agent's own registry's jobs with its owner words on.
func (a *Agent) ownLiveJobNotices() map[int]JobNotice {
	live := a.liveJobNotices()
	by, task := a.processOwner()
	for id, n := range live {
		n.StartedBy, n.Task = by, task
		live[id] = n
	}
	return live
}

// ProcessPage is one page of this conversation's history, answered by the
// engine that holds it. Live rows are every job this engine runs for the
// conversation — its own and its task workers' — and a saved running record
// no live job vouches for is unknown, never stopped.
func (a *Agent) ProcessPage(ctx context.Context, ask ProcessPageAsk) (ProcessPage, error) {
	limit, err := CheckProcessPageAsk(ask)
	if err != nil {
		return ProcessPage{}, err
	}
	dir := a.processConvDir()
	live := a.conversationLive(dir)
	rows := liveRowsOf(live)
	if dir == "" {
		return livePageOnly(ask, limit, rows)
	}
	return processStore{dir: dir}.page(ctx, ask, limit, func(id int) (JobNotice, bool) {
		n, ok := live[id]
		return n, ok
	}, rows, !a.closedForRecords())
}

// livePageOnly pages the live rows of an agent with no history to keep.
func livePageOnly(ask ProcessPageAsk, limit int, rows []ProcessPageRow) (ProcessPage, error) {
	const epoch = "live"
	before := 0
	if ask.Cursor != "" {
		cur, err := decodeProcessCursor(ask.Cursor)
		if err != nil {
			return ProcessPage{}, err
		}
		if cur.epoch != epoch || !cur.live {
			return ProcessPage{}, ErrProcessStale
		}
		before = cur.before
	}
	page := ProcessPage{Revision: epoch}
	budget := newPageBudget()
	for _, row := range rows {
		if before > 0 && row.ID >= before {
			continue
		}
		if len(page.Rows) == limit || !budget.admit(row) {
			page.Next = encodeProcessPosition(epoch, "L"+strconv.Itoa(row.ID+1))
			break
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

// ProcessRecord is one job of this conversation by number: the live job when
// this engine runs it, and otherwise its record.
func (a *Agent) ProcessRecord(id int) (JobNotice, bool, error) {
	dir := a.processConvDir()
	if n, ok := a.ownLiveJobNotices()[id]; ok {
		return n, true, nil
	}
	if dir == "" {
		return JobNotice{}, false, nil
	}
	if n, ok := liveProcessesOf(dir)[id]; ok {
		return n, true, nil
	}
	return processStore{dir: dir}.readProcessRecord(id)
}

// SweepProcessOutput expires only completed managed logs. Writer flocks keep
// active output safe, and the shelf and durable id history are never swept.
func SweepProcessOutput(file string, now time.Time) error {
	if file == "" {
		return nil
	}
	root := jobRetentionAnchor(filepath.Dir(file))
	for _, dir := range []string{filepath.Join(root, placeLogs), filepath.Join(root, placeLogs, droppingJobs)} {
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errRetentionUnsafe
		}
	}
	err := jobRetentionSweepBefore(filepath.Join(root, placeLogs, droppingJobs), defaultJobRetentionBudget(), now.Add(-AppProcessOutputRetention))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Close fences asynchronous exit and naming announcements before its final
// record writes. The record mutex joins writes already admitted; the closed
// flag refuses later ones, so no callback can recreate a directory after
// cleanup. Only the jobs THIS agent marked live are touched — one small file
// each — and a job still running at close is stopped by the close, with no
// exit code and no ending nobody saw.
func (a *Agent) finishProcessRecords() {
	dir := a.processConvDir()
	if dir == "" {
		return
	}
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	ids := liveProcessesOfAgent(dir, a)
	if len(ids) == 0 {
		return
	}
	live := a.ownLiveJobNotices()
	root, err := processStore{dir: dir}.openRoot(false)
	if err != nil {
		for _, id := range ids {
			liveProcessSet(dir, id, a, false)
		}
		return
	}
	defer root.Close()
	head, found, err := readHead(root)
	for _, id := range ids {
		liveProcessSet(dir, id, a, false)
		if err != nil || !found {
			continue
		}
		// The registry's own settled notice wins: the close refused the
		// announcement of a job it ended, and its ending is still a fact.
		n, ok, rerr := readRecord(root, id)
		if current, held := live[id]; held {
			n, ok, rerr = current, true, nil
		} else if rerr != nil || !ok || n.Over() {
			continue
		}
		if !ok || rerr != nil {
			continue
		}
		if !n.Over() {
			n.State, n.Ended = JobStopped, time.Time{}
		}
		_, _ = processStore{dir: dir}.put(root, &head, n, true)
	}
}

// reconcileProcessRecords carries known job endings into their process files.
// The caller holds the journal claim. Unfinished checkpoints and reused job
// numbers cannot turn an unknown process into an invented ending.
func reconcileProcessRecords(file string, document taskDocument) error {
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	s := processStore{dir: filepath.Dir(file)}
	root, err := s.openRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	head, found, err := readHead(root)
	if err != nil || !found {
		return err
	}
	for _, n := range knownJobEndings(document) {
		old, found, err := readRecord(root, n.ID)
		if err != nil {
			return err
		}
		if !found || old.Over() || !sameJobRun(old, n) {
			continue
		}
		// The process record owns its command, name, kind and workspace. Only
		// the ending comes from the checkpoint that observed this same run.
		old.State, old.Ended = n.State, n.Ended
		if n.Elapsed > 0 {
			old.Elapsed = n.Elapsed
		}
		old.ExitCode, old.ExitCodeUnknown = n.ExitCode, n.ExitCodeUnknown
		if _, err := s.put(root, &head, old, true); err != nil {
			return err
		}
	}
	return nil
}

// knownJobEndings is each settled, unparked job run of a checkpoint, by job
// number: the endings a checkpoint observed for itself.
func knownJobEndings(document taskDocument) map[int]JobNotice {
	ended := map[int]JobNotice{}
	for _, record := range document.Runs {
		if record.Kind != TaskKindJob || !record.State.settled() || record.Park != nil {
			continue
		}
		if n, ok := jobNoticeFromRow(runRowNotice(record)); ok {
			ended[n.ID] = n
		}
	}
	return ended
}

// sameJobRun says a process record and a checkpoint ending describe the same
// run, so a reused job number never borrows another run's ending.
func sameJobRun(record, ending JobNotice) bool {
	return !record.Started.IsZero() && !ending.Started.IsZero() && record.Started.Equal(ending.Started) && record.ToolCallID == ending.ToolCallID
}

// mergeKeptJobs fills gaps left by engines that kept jobs only in the roster.
// The process shelf owns any job it already knows, including a reused number.
func mergeKeptJobs(rows, kept []JobNotice) []JobNotice {
	seen := make(map[int]bool, len(rows))
	for _, n := range rows {
		seen[n.ID] = true
	}
	for _, n := range kept {
		if !seen[n.ID] {
			rows = append(rows, n)
			seen[n.ID] = true
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	return rows
}
