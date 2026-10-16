package appwire

import (
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
	// ProcessUnknown is answered ONLY by processes.page (processpage.go): a
	// record saved as running that no live owner vouches for. It carries no
	// exit code and no ending, and UnknownReason says why it is unknown.
	ProcessUnknown = "unknown"
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

// TaskNote is one note on a task's page, who said it, and the step it was read
// at.
type TaskNote struct {
	ID   string    `json:"id"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
	// From is WHO SAID IT: [NoteFromYou] or [NoteFromTask], carried from the
	// engine's own record of each note's origin and never read back out of its
	// words.
	//
	// AN ABSENT ORIGIN IS UNKNOWN AND NOT YOURS. It is omitempty so a note from
	// a record that never labelled one serialises exactly as it did before this
	// field existed; an app that reads the absence as the person's would be
	// making the claim this field was added to stop. A surface with no origin
	// draws the note without attributing it.
	From       string `json:"from,omitempty" enum:"you,task"`
	ReadAtStep *int   `json:"read_at_step,omitempty"`
}

// Who a task note is from.
const (
	// NoteFromYou is the person at a keyboard: a note typed into the task.
	NoteFromYou = "you"
	// NoteFromTask is the work's own line, or another agent's coordination with
	// it — anything the person did not type.
	NoteFromTask = "task"
)

// TaskPage is everything one task's page draws.
type TaskPage struct {
	Task   Task         `json:"task"`
	Result string       `json:"result,omitempty"`
	Files  []FileChange `json:"files"`
	Log    []Entry      `json:"log"`
	Notes  []TaskNote   `json:"notes"`
	Brief  string       `json:"brief"`
	// Program is the specialist the task was handed to, present only for such
	// a task (program.go). Absent is not "no program was run": an engine that
	// predates the field leaves it out too.
	Program *TaskProgram `json:"program,omitempty"`
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
	// Service is set on a kept-running process (services.go); an ordinary
	// process has none.
	Service *Service `json:"service,omitempty"`
	// URL is the first loopback address the process printed, as an origin
	// (`http://localhost:3000`): recorded once from its actual output, valid
	// port, never guessed from the command. Absent when it printed none.
	URL string `json:"url,omitempty"`
	// The history facts below are answered by processes.page and absent
	// elsewhere unless stated. Dir is the folder the process was started in;
	// LogPath is for showing and copying ONLY and is never accepted back
	// (processes.log takes the id); LogGone says the record outlived its
	// output; OutputTruncated says older output was discarded by the cap.
	Dir             string          `json:"dir,omitempty"`
	LogPath         string          `json:"log_path,omitempty"`
	LogGone         bool            `json:"log_gone,omitempty"`
	OutputTruncated bool            `json:"output_truncated,omitempty"`
	StartedBy       *ProcessStarter `json:"started_by,omitempty"`
	UnknownReason   string          `json:"unknown_reason,omitempty"`
}

// ProcessStarter is who started a process, present only when the agent that
// started it said so: Kind "chat" for the conversation itself, "task" with the
// task's id for a task worker.
type ProcessStarter struct {
	Kind string `json:"kind"`
	Task string `json:"task,omitempty"`
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
	MoveToTask(agent string, turn uint64) (MoveResult, error)
	Sleep() error
	Wake() error
}

// TaskModeler is the optional half of a Worker that can move one proposed task
// to another model: tasks.model. An engine without it refuses the method as
// unknown, and no other verb changes a task's model.
type TaskModeler interface {
	// SetTaskModel names the one proposed task, the model and, optionally, the
	// state and version of the card the person saw; a stale card is refused.
	SetTaskModel(id, model, state string, version *uint64) error
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
	return typed(func(e Engine, p IDParams) (*Empty, error) {
		w, err := worker(e, "tasks."+action)
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(w.ActOnTask(id, action, "", ""))
	})
}

var workMethods = map[string]method{
	"tasks.get": typed(func(e Engine, p IDParams) (*PageResult, error) {
		w, err := worker(e, "tasks.get")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		page, err := w.GetTask(id)
		if err != nil {
			return nil, err
		}
		page.Files, page.Log, page.Notes = nonNil(page.Files), nonNil(page.Log), nonNil(page.Notes)
		return &PageResult{page}, nil
	}),
	"tasks.start":   taskVerb("start"),
	"tasks.hold":    taskVerb("hold"),
	"tasks.decline": taskVerb("decline"),
	"tasks.typing":  taskVerb("typing"),
	"tasks.stop":    taskVerb("stop"),
	// DISMISS HIDES A SETTLED FAILURE FROM THE DEFAULT VIEW AND RESTORE UNDOES IT.
	// Neither retries, cancels, declines or deletes anything; the engine refuses a
	// task that is not a settled failure or that something live waits on.
	"tasks.dismiss": taskVerb("dismiss"),
	"tasks.restore": taskVerb("restore"),
	"tasks.note": typed(func(e Engine, p struct {
		ID       string `json:"id"`
		Text     string `json:"text"`
		ClientID string `json:"client_id"`
	}) (*TaskNoteResult, error) {
		w, err := worker(e, "tasks.note")
		if err != nil {
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
			return &TaskNoteResult{&entry}, nil
		}
		return &TaskNoteResult{}, w.ActOnTask(p.ID, "note", "", p.Text)
	}),
	"tasks.decide": typed(func(e Engine, p struct {
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
		Text    string `json:"text"`
	}) (*Empty, error) {
		w, err := worker(e, "tasks.decide")
		if err != nil {
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
		return done(w.ActOnTask(p.ID, "decide", p.Verdict, p.Text))
	}),
	"tasks.model": typed(func(e Engine, p struct {
		ID      string  `json:"id"`
		Model   string  `json:"model"`
		State   string  `json:"state"`
		Version *uint64 `json:"version"`
	}) (*Empty, error) {
		m, ok := e.(TaskModeler)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve tasks.model")
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Model) == "" {
			return nil, Errorf(CodeBadParams, "tasks.model needs an id and a model")
		}
		return done(m.SetTaskModel(p.ID, p.Model, p.State, p.Version))
	}),
	"quick.undo": typed(func(e Engine, p IDParams) (*Empty, error) {
		w, err := worker(e, "quick.undo")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(w.UndoQuick(id))
	}),
	"quick.promote": typed(func(e Engine, p IDParams) (*taskResult, error) {
		w, err := worker(e, "quick.promote")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		task, err := w.PromoteQuick(id)
		if err != nil {
			return nil, err
		}
		return &taskResult{task}, nil
	}),
	"processes.list": typed(func(e Engine, p AgentParams) (*ProcessesResult, error) {
		w, err := worker(e, "processes.list")
		if err != nil {
			return nil, err
		}
		agent, err := required("agent", p.Agent)
		if err != nil {
			return nil, err
		}
		list, err := w.ListProcesses(agent)
		if err != nil {
			return nil, err
		}
		return &ProcessesResult{nonNil(list)}, nil
	}),
	"processes.output": typed(func(e Engine, p struct {
		ID       string `json:"id"`
		FromLine int    `json:"from_line"`
	}) (*LinesNextLineResult, error) {
		w, err := worker(e, "processes.output")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || p.FromLine < 0 {
			return nil, Errorf(CodeBadParams, "processes.output needs an id and a from_line of zero or more")
		}
		lines, next, err := w.ProcessOutput(p.ID, p.FromLine)
		if err != nil {
			return nil, err
		}
		return &LinesNextLineResult{nonNil(lines), next}, nil
	}),
	"processes.follow":   processVerb(func(w Worker, id string) error { return w.FollowProcess(id, true) }),
	"processes.unfollow": processVerb(func(w Worker, id string) error { return w.FollowProcess(id, false) }),
	"processes.stop":     processVerb(func(w Worker, id string) error { return w.StopProcess(id) }),
	"processes.rerun": typed(func(e Engine, p IDParams) (*ProcessResult, error) {
		w, err := worker(e, "processes.rerun")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		process, err := w.RerunProcess(id)
		if err != nil {
			return nil, err
		}
		return &ProcessResult{process}, nil
	}),
	"agents.move_to_task": typed(func(e Engine, p struct {
		ID   string `json:"id"`
		Turn uint64 `json:"turn"`
	}) (*MoveResult, error) {
		w, err := worker(e, "agents.move_to_task")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "agents.move_to_task needs an id")
		}
		moved, err := w.MoveToTask(p.ID, p.Turn)
		if err != nil {
			return nil, err
		}
		return &moved, nil
	}),
	"power.sleep": typed(func(e Engine, _ struct{}) (*Empty, error) {
		w, err := worker(e, "power.sleep")
		if err != nil {
			return nil, err
		}
		return done(w.Sleep())
	}),
	"power.wake": typed(func(e Engine, _ struct{}) (*Empty, error) {
		w, err := worker(e, "power.wake")
		if err != nil {
			return nil, err
		}
		return done(w.Wake())
	}),
}

// processVerb is one process verb that takes only an id.
func processVerb(act func(Worker, string) error) method {
	return typed(func(e Engine, p IDParams) (*Empty, error) {
		w, err := worker(e, "processes")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(act(w, id))
	})
}

// MoveResult is agents.move_to_task's answer, given at once. Task is the task
// the reply became when it had already moved; Moving says the move is taken
// and the reply moves when the step in flight ends, its task arriving as
// task.changed and one notice saying it moved.
type MoveResult struct {
	Task   *Task `json:"task,omitempty"`
	Moving bool  `json:"moving,omitempty"`
}

// TaskNoteResult is tasks.note's answer: the note's entry, from an engine that
// keeps notes as entries.
type TaskNoteResult struct {
	Entry *Entry `json:"entry,omitempty"`
}

type taskResult struct {
	Task Task `json:"task"`
}

// The answers of this file's methods.
type (
	PageResult struct {
		Page TaskPage `json:"page"`
	}
	ProcessesResult struct {
		Processes []Process `json:"processes"`
	}
	ProcessResult struct {
		Process Process `json:"process"`
	}
)

// The answers of this file's methods.
type (
	LinesNextLineResult struct {
		Lines    []string `json:"lines"`
		NextLine int      `json:"next_line"`
	}
)
