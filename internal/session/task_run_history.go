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

// Resolve missing recorded models once before graph recovery takes its lock.
// Roster watchers then replay ordinary notices without filesystem reads.
func (a *Agent) restoreWorkerHistoryModels(document *taskDocument) {
	a.mu.Lock()
	place, session := a.familyPlace(nil), a.sessionID()
	a.mu.Unlock()
	base := taskJournalDir(place, session)
	for i := range document.Runs {
		record := &document.Runs[i]
		if record.Model == "" && record.Kind == "" && record.ID != 0 {
			record.Model = findWorkerHistory(base, record.ID).model
		}
	}
}
