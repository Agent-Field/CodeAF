package session

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type localWorkerHistory struct{ journal, model string }

// Plan workers have their own numeric task directory. Never search another
// task, accept helper logs, or reuse an absolute path from a moved checkpoint.
func findWorkerHistory(base string, id uint64) localWorkerHistory {
	relative := strconv.FormatUint(id, 10)
	directory, err := openTaskHistoryPath(base, relative, true)
	if err != nil {
		return localWorkerHistory{}
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return localWorkerHistory{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		stamp, worker := strings.CutSuffix(name, "_worker.jsonl")
		if !worker || !entry.Type().IsRegular() {
			continue
		}
		if _, err := time.Parse("20060102-150405.000000", stamp); err != nil {
			continue
		}
		header, ok := readWorkerHeader(base, filepath.Join(relative, name))
		if ok {
			return localWorkerHistory{journal: filepath.Join(base, relative, name), model: header.Model}
		}
	}
	worker := filepath.Join(relative, "worker.jsonl")
	if header, ok := readWorkerHeader(base, worker); ok {
		return localWorkerHistory{journal: filepath.Join(base, worker), model: header.Model}
	}
	return localWorkerHistory{}
}

// Only the first bounded header line is needed to identify a worker and the
// model its journal recorded; index rebuild never replays whole transcripts.
func readWorkerHeader(base, relative string) (sessionHeader, bool) {
	file, err := openTaskHistoryPath(base, relative, false)
	if err != nil {
		return sessionHeader{}, false
	}
	defer file.Close()
	const limit = 64 << 10
	line, err := bufio.NewReader(io.LimitReader(file, limit)).ReadString('\n')
	if err != nil && err != io.EOF || len(line) >= limit {
		return sessionHeader{}, false
	}
	var header sessionHeader
	if json.Unmarshal([]byte(line), &header) != nil || header.Type != "session" || header.Version != sessionFileVersion || header.ID == "" {
		return sessionHeader{}, false
	}
	return header, true
}

func withLocalWorkerHistory(entry TaskIndexEntry, place Place) TaskIndexEntry {
	id, err := strconv.ParseUint(entry.ID, 10, 64)
	if err != nil || id == 0 {
		return entry
	}
	history := findWorkerHistory(taskJournalDir(place, entry.SessionID), id)
	entry.TranscriptURI = taskURI(history.journal)
	if entry.Model == "" {
		entry.Model = history.model
	}
	return entry
}

// A plan page prefers its usage ledger. Older runs without usage rows may
// still name their worker model in the locally retained journal header.
func (a *Agent) planWorkerRecordedModel(taskID string) string {
	id, err := strconv.ParseUint(taskID, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	a.mu.Lock()
	place, session := a.familyPlace(nil), a.sessionID()
	a.mu.Unlock()
	return findWorkerHistory(taskJournalDir(place, session), id).model
}

// Resolve retained worker identities before graph recovery takes its lock.
// Roster watchers then replay ordinary notices without filesystem reads.
func (a *Agent) restoreWorkerHistory(document *taskDocument) {
	a.mu.Lock()
	place, session := a.familyPlace(nil), a.sessionID()
	a.mu.Unlock()
	base := taskJournalDir(place, session)
	for i := range document.Runs {
		record := &document.Runs[i]
		if record.Kind != "" || record.ID == 0 || record.Model != "" && record.PlanTask != "" {
			continue
		}
		history := findWorkerHistory(base, record.ID)
		if record.Model == "" {
			record.Model = history.model
		}
		if record.State == TaskDone && record.PlanTask == "" && history.journal != "" {
			// A saved worker without a plan still has a stable, read-only task page.
			record.PlanTask = planStoreID("history-" + strconv.FormatUint(record.ID, 10))
		}
	}
}

// Historical rows fill only gaps in the plan stores. A sender with its store
// still reads the original page; a receiver can read settled work immediately.
func (a *Agent) historicalPlanRows(rows []PlanTaskRow) []PlanTaskRow {
	known := map[string]bool{}
	for _, row := range rows {
		known[planTaskID(row.ID)] = true
	}
	for _, notice := range a.historicalPlanNotices() {
		if !known[planTaskID(notice.PlanTask)] {
			rows = append(rows, a.historicalPlanRow(notice))
			known[planTaskID(notice.PlanTask)] = true
		}
	}
	return rows
}

func (a *Agent) historicalPlanNotices() []TaskNotice {
	g := a.tasker()
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var notices []TaskNotice
	for _, notice := range g.runRowsLocked() {
		if notice.PlanTask != "" && (notice.State == TaskDone || notice.State == TaskFailed || notice.State == TaskUnverified) {
			notices = append(notices, notice)
		}
	}
	return notices
}

func (a *Agent) historicalPlanRow(notice TaskNotice) PlanTaskRow {
	a.mu.Lock()
	owner := a.sessionID()
	a.mu.Unlock()
	return PlanTaskRow{ID: notice.PlanTask, Title: notice.Title, Status: string(notice.State),
		HistoricalRun: notice.ID, HistoricalSession: owner,
		Stopped: notice.Stopped, Archived: true, Started: notice.StartedAt, Ended: notice.EndedAt,
		Model: notice.Model, USD: notice.CostUSD, Note: notice.Report}
}

func (a *Agent) historicalPlanPage(id string) (PlanTaskPage, bool) {
	for _, notice := range a.historicalPlanNotices() {
		if planTaskID(notice.PlanTask) != planTaskID(id) {
			continue
		}
		page := PlanTaskPage{Row: a.historicalPlanRow(notice), Historical: true, Result: notice.Result}
		if notice.PendingRun != nil {
			page.Description = notice.PendingRun.Brief
		}
		a.mu.Lock()
		base := taskJournalDir(a.familyPlace(nil), a.sessionID())
		a.mu.Unlock()
		relative := filepath.Join(strconv.FormatUint(notice.ID, 10), planTrajectoryFile)
		page.Steps = planStepDisplayFactsForPage(decodePlanTrajectory(readHistoricalFile(base, relative)), a.planDisplayRunCopy())
		page.Row.Steps = len(page.Steps)
		history := findWorkerHistory(base, notice.ID)
		if history.journal != "" {
			relative, err := filepath.Rel(base, history.journal)
			if err == nil {
				page.Worker = ReadTranscriptBytes(readHistoricalFile(base, relative)).Entries
			}
		}
		return page, true
	}
	return PlanTaskPage{}, false
}

func readHistoricalFile(base, relative string) []byte {
	file, err := openTaskHistoryPath(base, relative, false)
	if err != nil {
		return nil
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	return data
}
