package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// planLastWordsCap is the most of a worker's final message a page carries. The
// page is a summary of where a task ended up; the transcript beside it is the
// place for the whole of what the worker said.
const planLastWordsCap = 600

// planEndKind is the trajectory's ending line, the one [run.Step] writes with
// the kind "end". It is spelled here for the reason [PlanStep] mirrors the step
// line: this package cannot import the one that writes the record.
const planEndKind = "end"

// PlanTaskEnding is how a task's worker finished, off the trajectory's ending
// line: why its loop stopped and what it said it had done. It is nil for a task
// whose worker never reached an ending — one still running, one that never ran,
// and every task from a run that died mid-step.
type PlanTaskEnding struct {
	// Reason is why the worker's loop ended, in the engine's own words (a turn
	// that ended, a step cap, a wall).
	Reason string
	// Result is the worker's own account of the work.
	Result string
	// At is when it ended; zero when the record cannot say.
	At time.Time
}

// planEndLine is the ending line's decoded shape, the three fields a page reads.
type planEndLine struct {
	Kind    string    `json:"kind"`
	Reason  string    `json:"reason"`
	Result  string    `json:"result"`
	EndedAt time.Time `json:"ended_at"`
}

// planTaskEnding reads the LAST ending line of one task's trajectory, because a
// task woken again after it ended writes a new ending and the latest is the one
// that says where the task stands.
//
// The instant is the line's own clock when a program wrote one, and otherwise
// the moment the store completed the task, then the file's own modification time
// as a last resort: an ending is the last thing appended, so the file's age is
// when it was written.
func planTaskEnding(dir, id string, completed time.Time) *PlanTaskEnding {
	path := planTrajectoryPath(dir, id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var last *planEndLine
	for _, line := range strings.Split(string(data), "\n") {
		var end planEndLine
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &end) != nil || end.Kind != planEndKind {
			continue
		}
		last = &end
	}
	if last == nil {
		return nil
	}
	at := last.EndedAt
	if at.IsZero() {
		at = completed
	}
	if at.IsZero() {
		if info, err := os.Stat(path); err == nil {
			at = info.ModTime()
		}
	}
	return &PlanTaskEnding{Reason: strings.TrimSpace(last.Reason), Result: strings.TrimSpace(last.Result), At: at}
}

// planLastWords is the final thing the task's newest worker said, cut to
// [planLastWordsCap] characters, and "" when there is no transcript or the worker
// never spoke. The names are stamped to the microsecond ([workerJournalName]),
// so the newest sorts last.
func planLastWords(dir, id string) string {
	names, err := filepath.Glob(filepath.Join(plandb.TaskDir(dir, id), "*_worker.jsonl"))
	if err != nil || len(names) == 0 {
		return ""
	}
	newest := names[0]
	for _, name := range names[1:] {
		if filepath.Base(name) > filepath.Base(newest) {
			newest = name
		}
	}
	said, ok := PeekReport(newest)
	if !ok {
		return ""
	}
	return cutRunes(strings.TrimSpace(said), planLastWordsCap)
}

// cutRunes shortens text to at most limit characters, ending a cut one with an
// ellipsis so the reader can tell it was cut.
func cutRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimRight(string(runes[:limit-1]), " \t\n") + "…"
}

// planTaskChanged is the files the task's run wrote, repo-relative, off the
// newest run row that names the task: the same landing record the live task
// notice carries ([TaskNotice.Changed]) and the checkpoint keeps
// ([runRecord.Changed]), so a reopened page and the card that watched the work
// land cannot disagree. Nil when no row names the task or the row recorded none.
func (a *Agent) planTaskChanged(id string) []string {
	g := a.tasker()
	if g == nil {
		return nil
	}
	g.mu.Lock()
	rows := g.runRowsLocked()
	g.mu.Unlock()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].PlanTask == id && len(rows[i].Changed) > 0 {
			return append([]string(nil), rows[i].Changed...)
		}
	}
	return nil
}
