package session

import (
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AppProcessOutputBytes is the disk limit for the app's process output. The
// terminal keeps its existing chunk policy; both readers keep the 64 KiB ring.
const AppProcessOutputBytes = factory.DefaultKeepOutputMB << 20
const AppProcessOutputRetention = factory.DefaultKeepOutputDays * 24 * time.Hour

// A conversation has one engine writer. This lock orders its asynchronous
// naming and exit updates so a late name cannot overwrite a settled process.
var processRecordMu sync.Mutex

func processRecordPath(file string) string {
	return filepath.Join(filepath.Dir(file), "processes.json")
}

// ReadProcessNotices reads the durable shelf without opening a conversation or
// reviving its work. Output expiry never removes these ended process records.
func ReadProcessNotices(file string) ([]JobNotice, error) {
	if file == "" {
		return []JobNotice{}, nil
	}
	raw, err := os.ReadFile(processRecordPath(file))
	if errors.Is(err, os.ErrNotExist) {
		return []JobNotice{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []JobNotice
	err = json.Unmarshal(raw, &rows)
	return rows, err
}

func (a *Agent) keepProcessNotice(n JobNotice) {
	if n.Kind == JobKindTask || a.SessionPath() == "" {
		return
	}
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return
	}
	rows, err := ReadProcessNotices(a.SessionPath())
	if err != nil {
		return
	}
	found := false
	for i, old := range rows {
		if old.ID != n.ID {
			continue
		}
		if old.Over() && !n.Over() {
			return
		}
		rows[i], found = n, true
	}
	if !found {
		rows = append(rows, n)
	}
	writeProcessRecords(a.SessionPath(), rows)
}

func writeProcessRecords(file string, rows []JobNotice) {
	raw, err := json.Marshal(rows)
	if err != nil {
		return
	}
	path := processRecordPath(file)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".processes-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	_ = tmp.Chmod(0600)
	_, writeErr := tmp.Write(raw)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if writeErr == nil && syncErr == nil && closeErr == nil {
		_ = os.Rename(tmp.Name(), path)
	}
}

// JobNotices joins the saved shelf with the live registry. A saved running row
// whose process no longer exists is stopped, rather than revived by a read.
func (a *Agent) JobNotices() ([]JobNotice, error) {
	saved, err := ReadProcessNotices(a.SessionPath())
	if err != nil {
		return nil, err
	}
	live := a.liveJobNotices()
	out := make(map[int]JobNotice, len(saved)+len(live))
	for _, n := range saved {
		if n.State == JobRunning {
			n.State = JobStopped
		}
		out[n.ID] = n
	}
	for id, n := range live {
		out[id] = n
	}
	rows := make([]JobNotice, 0, len(out))
	for _, n := range out {
		rows = append(rows, n)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	return rows, nil
}

// RerunProcess takes the retained command from this engine's shelf, never a
// client-supplied shell string. Only ended shell commands can be run again.
func (a *Agent) RerunProcess(id int) (JobNotice, error) {
	rows, err := a.JobNotices()
	if err != nil {
		return JobNotice{}, err
	}
	for _, n := range rows {
		if n.ID != id {
			continue
		}
		if !n.Over() || n.Kind != JobKindCommand || strings.TrimSpace(n.Command) == "" {
			return JobNotice{}, errors.New("only ended commands can be run again")
		}
		started, err := a.jobs.start(n.Command)
		if err != nil {
			return JobNotice{}, err
		}
		return noticeOf(started.info()), nil
	}
	return JobNotice{}, errors.New("no such process")
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
	err := jobRetentionSweepBefore(filepath.Join(root, placeLogs, droppingJobs), defaultJobRetentionBudget(), now.Add(-appProcessOutputAge()))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Close fences asynchronous exit and naming announcements before its final
// shelf write. The record mutex joins writes already admitted; the closed flag
// refuses later ones, so no callback can recreate a directory after cleanup.
func (a *Agent) finishProcessRecords() {
	if a.SessionPath() == "" {
		return
	}
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	rows, err := a.JobNotices()
	if err != nil {
		return
	}
	if len(rows) == 0 {
		return
	}
	for i := range rows {
		if rows[i].State == JobRunning {
			rows[i].State = JobStopped
		}
	}
	writeProcessRecords(a.SessionPath(), rows)
}

// Terminal callers retain their existing policy; app sweeps read this Mac's saved age.
func appProcessOutputAge() time.Duration {
	if config.AppMode() {
		_, age := config.AppOutputPolicyAt(config.ProfileDir())
		return age
	}
	return AppProcessOutputRetention
}
