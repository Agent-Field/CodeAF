package appwire

import (
	"encoding/json"
	"strings"
	"time"
)

// Wave 3: tasks, quick tasks, processes, running long and sleep
// (PROTOCOL.md). The types and the methods sit together, as setup.go's do.

// StateAsleep is an agent whose Mac is asleep: its processes were stopped and
// it resumes on wake.
const StateAsleep = "asleep"

// CodeConflict is quick.undo's refusal when a file it would restore changed
// since the quick task wrote it; the error names the files in Changed.
const CodeConflict = "conflict"

// The events of this wave.
const (
	EventQuickChanged   = "quick.changed"
	EventProcessChanged = "process.changed"
	EventProcessOutput  = "process.output"
	EventTurnLong       = "turn.long"
)

// The quick task states.
const (
	QuickRunning  = "running"
	QuickDone     = "done"
	QuickUndone   = "undone"
	QuickGrew     = "grew"
	QuickConflict = "conflict"
)

// The process states.
const (
	ProcessRunning = "running"
	ProcessEnded   = "ended"
	ProcessFailed  = "failed"
	ProcessStopped = "stopped"
)

// The verdicts of a task waiting for the person's call.
const (
	VerdictAccept      = "accept"
	VerdictNotRight    = "not_right"
	VerdictRecheck     = "recheck"
	VerdictLetItDecide = "let_it_decide"
)

// FileChange is one file a task or a quick task changed, with the lines it
// added and removed where they are known.
type FileChange struct {
	Path string `json:"path"`
	Add  int    `json:"add"`
	Del  int    `json:"del"`
}

// TaskNote is one note the person sent a task, and the step it was read at.
type TaskNote struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	At         time.Time `json:"at"`
	ReadAtStep *int      `json:"read_at_step,omitempty"`
}

// TaskPage is everything one task's page draws.
type TaskPage struct {
	Task   Task         `json:"task"`
	Result string       `json:"result,omitempty"`
	Files  []FileChange `json:"files"`
	Log    []Entry      `json:"log"`
	Notes  []TaskNote   `json:"notes"`
	Brief  string       `json:"brief"`
}

// Quick is one quick task: work done where the person is, which can be undone.
type Quick struct {
	ID       string        `json:"id"`
	Agent    string        `json:"agent"`
	Title    string        `json:"title"`
	Files    []FileChange  `json:"files"`
	State    string        `json:"state"`
	Grew     *QuickGrowth  `json:"grew,omitempty"`
	Conflict *QuickChanged `json:"conflict,omitempty"`
}

// QuickGrowth is why a quick task asked to become a full task.
type QuickGrowth struct {
	Files  int    `json:"files"`
	Reason string `json:"reason"`
}

// QuickChanged names the files that changed since a quick task wrote them.
type QuickChanged struct {
	Changed []string `json:"changed"`
}

// Process is one background process an agent started.
type Process struct {
	ID        string     `json:"id"`
	Agent     string     `json:"agent"`
	Title     string     `json:"title"`
	Command   string     `json:"command"`
	State     string     `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	LastLine  string     `json:"last_line,omitempty"`
	// Reason is why a stopped process stopped, when something other than a
	// person stopped it: "stopped by quit".
	Reason string `json:"reason,omitempty"`
}

// The data of this wave's events.
type (
	QuickChangedEvent struct {
		Quick Quick `json:"quick"`
	}
	ProcessChanged struct {
		Process Process `json:"process"`
	}
	ProcessOutput struct {
		ID    string   `json:"id"`
		Lines []string `json:"lines"`
	}
	TurnLong struct {
		Agent    string `json:"agent"`
		Turn     uint64 `json:"turn"`
		ElapsedS int    `json:"elapsed_s"`
		Steps    int    `json:"steps"`
	}
)

// Worker is the engine's half of this wave. A [Engine] that is also a Worker
// serves these methods; one that is not refuses them as unknown.
type Worker interface {
	GetTask(id string) (TaskPage, error)
	// ActOnTask is every verb on a task: start, hold, decline, typing, note,
	// stop, and decide with a verdict.
	ActOnTask(id, action, verdict, text string) error
	UndoQuick(id string) error
	PromoteQuick(id string) (Task, error)
	ListProcesses(agent string) ([]Process, error)
	ProcessOutput(id string, from int) ([]string, int, error)
	FollowProcess(id string, follow bool) error
	StopProcess(id string) error
	RerunProcess(id string) (Process, error)
	MoveToTask(agent string, turn uint64) (Task, error)
	Sleep() error
	Wake() error
}

func init() {
	for name, m := range workMethods {
		methods[name] = m
	}
}

// worker is the engine as a Worker, or a refusal saying the method is not
// served by this engine.
func worker(e Engine, name string) (Worker, error) {
	w, ok := e.(Worker)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return w, nil
}

// taskVerb is one card verb that takes only an id.
func taskVerb(action string) method {
	return func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "tasks."+action)
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, w.ActOnTask(id, action, "", "")
	}
}

var workMethods = map[string]method{
	"tasks.get": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "tasks.get")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		page, err := w.GetTask(id)
		if err != nil {
			return nil, err
		}
		page.Files, page.Log, page.Notes = nonNil(page.Files), nonNil(page.Log), nonNil(page.Notes)
		return struct {
			Page TaskPage `json:"page"`
		}{page}, nil
	},
	"tasks.start":   taskVerb("start"),
	"tasks.hold":    taskVerb("hold"),
	"tasks.decline": taskVerb("decline"),
	"tasks.typing":  taskVerb("typing"),
	"tasks.stop":    taskVerb("stop"),
	"tasks.note": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "tasks.note")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			ClientID string `json:"client_id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "tasks.note needs an id and some text")
		}
		// An engine that keeps notes as entries answers the note's entry.
		if t, ok := e.(Talker); ok {
			entry, err := t.NoteTask(p.ID, p.Text, p.ClientID)
			if err != nil {
				return nil, err
			}
			return struct {
				Entry Entry `json:"entry"`
			}{entry}, nil
		}
		return struct{}{}, w.ActOnTask(p.ID, "note", "", p.Text)
	},
	"tasks.decide": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "tasks.decide")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID      string `json:"id"`
			Verdict string `json:"verdict"`
			Text    string `json:"text"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "tasks.decide needs an id")
		}
		switch p.Verdict {
		case VerdictAccept, VerdictNotRight, VerdictRecheck, VerdictLetItDecide:
		default:
			return nil, Errorf(CodeBadParams, "verdict is accept, not_right, recheck or let_it_decide")
		}
		if p.Verdict == VerdictNotRight && strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "not_right needs the words to tell it")
		}
		return struct{}{}, w.ActOnTask(p.ID, "decide", p.Verdict, p.Text)
	},
	"quick.undo": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "quick.undo")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, w.UndoQuick(id)
	},
	"quick.promote": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "quick.promote")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		task, err := w.PromoteQuick(id)
		if err != nil {
			return nil, err
		}
		return taskResult{task}, nil
	},
	"processes.list": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "processes.list")
		if err != nil {
			return nil, err
		}
		agent, err := decodeID(raw, "agent")
		if err != nil {
			return nil, err
		}
		list, err := w.ListProcesses(agent)
		if err != nil {
			return nil, err
		}
		return struct {
			Processes []Process `json:"processes"`
		}{nonNil(list)}, nil
	},
	"processes.output": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "processes.output")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID       string `json:"id"`
			FromLine int    `json:"from_line"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || p.FromLine < 0 {
			return nil, Errorf(CodeBadParams, "processes.output needs an id and a from_line of zero or more")
		}
		lines, next, err := w.ProcessOutput(p.ID, p.FromLine)
		if err != nil {
			return nil, err
		}
		return struct {
			Lines    []string `json:"lines"`
			NextLine int      `json:"next_line"`
		}{nonNil(lines), next}, nil
	},
	"processes.follow":   processVerb(func(w Worker, id string) error { return w.FollowProcess(id, true) }),
	"processes.unfollow": processVerb(func(w Worker, id string) error { return w.FollowProcess(id, false) }),
	"processes.stop":     processVerb(func(w Worker, id string) error { return w.StopProcess(id) }),
	"processes.rerun": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "processes.rerun")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		process, err := w.RerunProcess(id)
		if err != nil {
			return nil, err
		}
		return struct {
			Process Process `json:"process"`
		}{process}, nil
	},
	"agents.move_to_task": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "agents.move_to_task")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID   string `json:"id"`
			Turn uint64 `json:"turn"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "agents.move_to_task needs an id")
		}
		task, err := w.MoveToTask(p.ID, p.Turn)
		if err != nil {
			return nil, err
		}
		return taskResult{task}, nil
	},
	"power.sleep": func(e Engine, _ json.RawMessage) (any, error) {
		w, err := worker(e, "power.sleep")
		if err != nil {
			return nil, err
		}
		return struct{}{}, w.Sleep()
	},
	"power.wake": func(e Engine, _ json.RawMessage) (any, error) {
		w, err := worker(e, "power.wake")
		if err != nil {
			return nil, err
		}
		return struct{}{}, w.Wake()
	},
}

// processVerb is one process verb that takes only an id.
func processVerb(act func(Worker, string) error) method {
	return func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "processes")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, act(w, id)
	}
}

type taskResult struct {
	Task Task `json:"task"`
}
