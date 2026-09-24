package router

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// A CREW'S DECISION AND ITS OUTCOME, IN THE SAME DIARY AS EVERY OTHER ROUTE.
//
// The crew router (internal/crewroute) picks a task's worker, planner and
// checker from the class of work the task is, and what it learns from —
// whether the person accepted the result or asked for it redone stronger — is
// exactly the kind of row this log exists to keep: a decision with the
// candidates it was made among, and a later row that settles it. So the crew
// writes here, under [CrewClass], rather than into a second file that would be
// a second answer to "what did the router do and how did it go".
//
// Two rows per task, joined by the call id: the DECISION, written when the
// crew is picked, and the OUTCOME, written Final when the task is accepted or
// redone. A reader takes the last row for a call id as the truth, the same
// rule every other row here keeps.

// CrewClass is the class every crew row is written under.
const CrewClass = "crew"

// The outcomes a crew row settles with.
const (
	// CrewAccepted is a task whose result was kept: it landed, or the person
	// took it.
	CrewAccepted = "accepted"
	// CrewRedone is a task the person asked to be done again by a stronger
	// crew. It is the learning signal: the class was under-served here.
	CrewRedone = "redo stronger"
	// CrewNotKept is a task that ended without a result anybody kept — it
	// failed, or was stopped. It teaches nothing about the crew's strength.
	CrewNotKept = "not kept"
)

// CrewRecord is the crew half of a row.
type CrewRecord struct {
	// TaskClass is the class the task was read as — bugfix, openended, other.
	TaskClass string `json:"task_class"`
	// Repo is the repository the task ran in, the key the learned offset is
	// kept under.
	Repo   string `json:"repo,omitempty"`
	Title  string `json:"title,omitempty"`
	Effort string `json:"effort,omitempty"`
	Steps  int    `json:"steps,omitempty"`
	// Seats is seat → the id that ran; Providers seat → the route; Kinds
	// seat → how that route bills (metered, plan, local).
	Seats     map[string]string `json:"seats"`
	Providers map[string]string `json:"providers,omitempty"`
	Kinds     map[string]string `json:"kinds,omitempty"`
	Pinned    []string          `json:"pinned,omitempty"`
	EstUSD    float64           `json:"est_usd,omitempty"`
}

// LogCrewDecision appends a crew's decision row. Best-effort by construction:
// a log that will not open costs a lesson, never a task.
func LogCrewDecision(dir, call string, record CrewRecord, candidates []string) {
	events, err := OpenEvents(dir)
	if err != nil {
		return
	}
	defer events.Close()
	events.Append(Event{Call: call, Run: record.Repo, Class: CrewClass, Shape: record.TaskClass,
		Candidates: candidates, Model: record.Seats["worker"], Crew: &record})
}

// LogCrewOutcome appends the row that settles a crew's decision.
func LogCrewOutcome(dir, call string, record CrewRecord, outcome string, costUSD float64) {
	events, err := OpenEvents(dir)
	if err != nil {
		return
	}
	defer events.Close()
	events.Append(Event{Call: call, Run: record.Repo, Class: CrewClass, Shape: record.TaskClass,
		Model: record.Seats["worker"], Crew: &record, Final: true, Outcome: outcome, Cost: costUSD})
}

// CrewTask is one task as the log remembers it.
type CrewTask struct {
	At      time.Time
	Call    string
	Record  CrewRecord
	Outcome string
	CostUSD float64
	Settled bool
}

// CrewLog is what the log says about crews.
type CrewLog struct {
	// SpentUSD is today's settled crew spend; Tasks, OnPlan and Local count
	// today's tasks, and how many of them ran their worker on a subscription
	// plan or a local model.
	SpentUSD float64
	Tasks    int
	OnPlan   int
	Local    int
	// Recent is the last few tasks, newest first.
	Recent []CrewTask
	// Offsets is the learned escalation offset per repository and class,
	// keyed repo + "\x00" + class.
	Offsets map[string]int
}

// The learning rule's two numbers. A redo raises its repository and class by
// one step, to at most crewMaxOffset; crewDecayAfter accepted tasks of that
// class there take a step back off, so a class that was under-served once is
// not overpaid for ever.
const (
	crewMaxOffset  = 3
	crewDecayAfter = 5
	// crewRecent is how many tasks the panel lists.
	crewRecent = 8
	// crewTailBytes is how much of the log is read: the crew rows are a few
	// hundred bytes among every routed turn, and a day's tasks and the offsets
	// that matter live in the recent tail, not in months of history.
	crewTailBytes = 8 << 20
)

// ReadCrewLog reads the crew rows out of the log's tail. now decides which
// day is today, in the local time zone the person lives in.
func ReadCrewLog(dir string, now time.Time) CrewLog {
	out := CrewLog{Offsets: map[string]int{}}
	path, err := statePath(dir, "router-events.jsonl")
	if err != nil {
		return out
	}
	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > crewTailBytes {
		if _, err := file.Seek(info.Size()-crewTailBytes, io.SeekStart); err != nil {
			return out
		}
	}
	tasks := map[string]*CrewTask{}
	var order []string
	needle := []byte(`"class":"` + CrewClass + `"`)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, needle) {
			continue
		}
		var row Event
		if json.Unmarshal(line, &row) != nil || row.Class != CrewClass || row.Crew == nil {
			continue
		}
		task, seen := tasks[row.Call]
		if !seen {
			task = &CrewTask{At: row.At, Call: row.Call}
			tasks[row.Call] = task
			order = append(order, row.Call)
		}
		task.Record = *row.Crew
		if row.Final {
			task.Outcome, task.CostUSD, task.Settled = row.Outcome, row.Cost, true
			task.At = row.At
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return tasks[order[i]].At.Before(tasks[order[j]].At) })
	year, month, day := now.Local().Date()
	accepted := map[string]int{}
	for _, call := range order {
		task := tasks[call]
		key := task.Record.Repo + "\x00" + task.Record.TaskClass
		switch task.Outcome {
		case CrewRedone:
			if out.Offsets[key] < crewMaxOffset {
				out.Offsets[key]++
			}
			accepted[key] = 0
		case CrewAccepted:
			accepted[key]++
			if accepted[key] >= crewDecayAfter && out.Offsets[key] > 0 {
				out.Offsets[key]--
				accepted[key] = 0
			}
		}
		if y, m, d := task.At.Local().Date(); y == year && m == month && d == day {
			out.Tasks++
			out.SpentUSD += task.CostUSD
			switch task.Record.Kinds["worker"] {
			case "plan":
				out.OnPlan++
			case "local":
				out.Local++
			}
		}
	}
	for key, offset := range out.Offsets {
		if offset == 0 {
			delete(out.Offsets, key)
		}
	}
	for i := len(order) - 1; i >= 0 && len(out.Recent) < crewRecent; i-- {
		out.Recent = append(out.Recent, *tasks[order[i]])
	}
	return out
}

// CrewCallID names one task's crew rows. It is the task's own identity where
// the caller has one — a redo must settle the SAME call — and fresh otherwise.
func CrewCallID(identity string) string {
	if identity = strings.TrimSpace(identity); identity != "" {
		return "crew:" + identity
	}
	return "crew:" + callID()
}
