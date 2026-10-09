package session

import (
	"bufio"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"os"
	"sync"
)

// Worker facts join only through the node's recorded plan identity. Auxiliary
// models can cost more than the worker and must never rename its task.
type planWorkerFact struct {
	model, journal string
	steps          int
	id             uint64
	live           plandb.LiveStep
}
type planWorkerFacts map[string]planWorkerFact

func (a *Agent) planWorkerFacts() planWorkerFacts {
	g := a.tasker()
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	facts := make(planWorkerFacts)
	for _, node := range g.nodes {
		id := node.spec.planID
		if id == "" {
			continue
		}
		old, found := facts[id]
		if found && old.id >= node.id {
			continue
		}
		fact := planWorkerFact{model: node.spec.model, journal: node.journal, id: node.id}
		if node.room != nil {
			state := node.room.recorder().state(0)
			fact.steps = state.Steps
			if !node.state.settled() && state.Call != "" && !state.Since.IsZero() {
				fact.live = plandb.LiveStep{Step: state.Steps + 1, Command: state.Call, Since: state.Since}
			}
		}
		facts[id] = fact
	}
	return facts
}

func (facts planWorkerFacts) apply(row *PlanTaskRow, dir, id string) {
	fact, ok := facts[id]
	if !ok {
		return
	}
	row.Model = fact.model
	model, recorded := "", fact.steps
	if fact.steps == 0 || fact.model == "" {
		model, recorded = planWorkerJournalFacts(fact.journal)
	}
	if fact.model == "" && model != "" {
		row.Model = model
	}
	// A dedicated run trajectory is the authority when present. Ordinary
	// session workers publish tool completions to their own session journal.
	if _, err := os.Stat(planTrajectoryPath(dir, id)); err == nil {
		return
	}
	steps := fact.steps
	if recorded > steps {
		steps = recorded
	}
	row.Steps = steps
	if row.Live.Empty() && !fact.live.Empty() {
		row.Live = fact.live
	}
}

type planWorkerJournalReading struct {
	info  os.FileInfo
	model string
	steps int
}

var planWorkerJournalCache = struct {
	sync.Mutex
	rows map[string]planWorkerJournalReading
}{rows: make(map[string]planWorkerJournalReading)}

func planWorkerJournalFacts(path string) (string, int) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0
	}
	planWorkerJournalCache.Lock()
	cached, ok := planWorkerJournalCache.rows[path]
	planWorkerJournalCache.Unlock()
	if ok && os.SameFile(cached.info, info) && cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) {
		return cached.model, cached.steps
	}

	file, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64<<10), 8<<20)
	seen := make(map[string]bool)
	model := ""
	for scan.Scan() {
		var line struct {
			Type  string       `json:"type"`
			Model string       `json:"model"`
			Took  *journalTook `json:"took"`
		}
		if json.Unmarshal(scan.Bytes(), &line) != nil {
			continue
		}
		if line.Type == "session" && model == "" {
			model = line.Model
		}
		if line.Type != "took" || line.Took == nil || line.Took.CallID == "" {
			continue
		}
		seen[line.Took.CallID] = true
	}
	if scan.Err() == nil {
		planWorkerJournalCache.Lock()
		if len(planWorkerJournalCache.rows) >= 256 {
			clear(planWorkerJournalCache.rows)
		}
		planWorkerJournalCache.rows[path] = planWorkerJournalReading{info: info, model: model, steps: len(seen)}
		planWorkerJournalCache.Unlock()
	}
	return model, len(seen)
}
