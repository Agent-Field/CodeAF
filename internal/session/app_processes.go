package session

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

// AppProcessOutputBytes is the disk limit for the app's process output. The
// terminal keeps its existing chunk policy; both readers keep the 64 KiB ring.
const AppProcessOutputBytes = 10 << 20
const AppProcessOutputRetention = 7 * 24 * time.Hour

// A conversation has one engine writer. This lock orders its asynchronous
// naming and exit updates so a late name cannot overwrite a settled process.
var processRecordMu sync.Mutex

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
	root, head, err := processStore{dir: dir}.open()
	if err != nil {
		return
	}
	defer root.Close()
	if _, err := (processStore{dir: dir}).put(root, &head, n, true); err != nil {
		return
	}
	liveProcessSet(dir, n.ID, a, !n.Over())
}

// ReadProcessNotices is the legacy whole-list read for a reader that does not
// hold the conversation: the newest [processLegacyListMax] records, with a
// saved running record shown stopped as this list always showed it. It writes
// nothing. processes.page is the paged, truthful read.
func ReadProcessNotices(file string) ([]JobNotice, error) {
	if file == "" {
		return []JobNotice{}, nil
	}
	rows, err := legacyListOf(processStore{dir: filepath.Dir(file)}, nil, false)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// legacyListOf pages the history into the legacy list's shape.
func legacyListOf(s processStore, live map[int]JobNotice, owner bool) ([]JobNotice, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var liveRows []ProcessPageRow
	for _, n := range live {
		liveRows = append(liveRows, ProcessPageRow{JobNotice: n, Live: true})
	}
	sort.Slice(liveRows, func(i, j int) bool { return liveRows[i].ID > liveRows[j].ID })
	var vouch func(int) (JobNotice, bool)
	if owner {
		vouch = func(id int) (JobNotice, bool) { n, ok := live[id]; return n, ok }
	}
	out := []JobNotice{}
	ask := ProcessPageAsk{Limit: ProcessPageMax}
	for len(out) < processLegacyListMax+len(live) {
		page, err := s.page(ctx, ask, ProcessPageMax, vouch, liveRows, owner)
		if err != nil {
			return nil, err
		}
		for _, row := range page.Rows {
			n := row.JobNotice
			if row.Unknown != "" {
				// THE LEGACY LIST HAS NO WORD FOR UNKNOWN. It keeps the state it
				// always showed, and no longer pairs it with an invented ending.
				n.State = JobStopped
			}
			out = append(out, n)
		}
		if page.Next == "" {
			break
		}
		ask.Cursor = page.Next
		liveRows = nil
	}
	return out, nil
}

// JobNotices joins the saved history with the live registry, as the legacy
// whole list: the newest [processLegacyListMax] records and every live job.
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
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	return legacyListOf(processStore{dir: dir}, live, !a.closedForRecords())
}

// ownLiveJobNotices is this agent's own live jobs with its owner words on.
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
	live := a.ownLiveJobNotices()
	if dir != "" {
		for id, n := range liveProcessesOf(dir) {
			if _, held := live[id]; !held {
				live[id] = n
			}
		}
	}
	rows := make([]ProcessPageRow, 0, len(live))
	for _, n := range live {
		rows = append(rows, ProcessPageRow{JobNotice: n, Live: true})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	if dir == "" {
		if ask.Cursor != "" {
			return ProcessPage{}, ErrProcessStale
		}
		return ProcessPage{Rows: rows}, nil
	}
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	owner := !a.closedForRecords()
	return processStore{dir: dir}.page(ctx, ask, limit, func(id int) (JobNotice, bool) {
		n, ok := live[id]
		return n, ok
	}, rows, owner)
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

// RerunProcess takes the retained command from this engine's record, never a
// client-supplied shell string. Only ended shell commands, kept whole, can be
// run again.
func (a *Agent) RerunProcess(id int) (JobNotice, error) {
	n, found, err := a.ProcessRecord(id)
	if err != nil {
		return JobNotice{}, err
	}
	if !found {
		return JobNotice{}, errors.New("no such process")
	}
	if n.State == JobRunning {
		if _, live := a.ownLiveJobNotices()[id]; !live {
			if _, live := liveProcessesOf(a.processConvDir())[id]; !live {
				n.State = JobStopped
			}
		}
	}
	if !n.Over() || n.Kind != JobKindCommand || strings.TrimSpace(n.Command) == "" || n.CommandCut {
		return JobNotice{}, errors.New("only ended commands can be run again")
	}
	started, err := a.jobs.start(n.Command)
	if err != nil {
		return JobNotice{}, err
	}
	return noticeOf(started.info()), nil
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
