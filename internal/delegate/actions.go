package delegate

// The action log: every stage, step and ending a program reported, one line
// each, stamped with the moment codeaf received it, kept in the task's own
// record folder beside the conversation log. The run's worker (internal/run)
// and the shell verb (cmd/codeaf) write it as the records arrive; the task page
// reads it (internal/session) and draws the program's work as the actions it
// took, each under the step of the program's own process it served.
//
// THE TIME IS CODEAF'S. A program's records carry no clock of their own that
// codeaf trusts, and the page merges this log with the conversation log, whose
// every time is codeaf's too; stamping on receipt is what makes the two one
// timeline.
//
// THE WORDS ARE THE PROGRAM'S. A line keeps the record as the program wrote it
// — its stage and status, its step id, its data — and the program's own
// vocabulary ([Delegate.Present]) turns a line into what a person reads, at the
// moment the page is read. So a program that learns to say a thing better says
// it better about every run it has made.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ActionsFile is the log's name inside a task's record folder.
const ActionsFile = "delegate-actions.jsonl"

// The kinds of line the log holds: a stage record, a step record, and the
// terminal record's status and message.
const (
	ActionStage = "stage"
	ActionStep  = "step"
	ActionEnd   = "end"
)

// Action is one line of the log.
type Action struct {
	// At is when codeaf received the record.
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	// Stage, Status and Data are a stage's; Status is the ending's too.
	Stage  string          `json:"stage,omitempty"`
	Status string          `json:"status,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	// Tool, Step, Command, Observation and Exit are a step's.
	Tool        string `json:"tool,omitempty"`
	Step        string `json:"step,omitempty"`
	Command     string `json:"command,omitempty"`
	Observation string `json:"observation,omitempty"`
	Exit        *int   `json:"exit,omitempty"`
	// Added and Removed are a step's lines added and removed, when the program
	// counted them.
	Added   *int `json:"added,omitempty"`
	Removed *int `json:"removed,omitempty"`
	// Message is the ending's one sentence.
	Message string `json:"message,omitempty"`
}

// StageAction is a stage record as a line of the log, received at at.
func StageAction(at time.Time, record StageRecord) Action {
	return Action{At: at, Kind: ActionStage, Stage: record.Stage, Status: record.Status, Data: record.Data}
}

// StepAction is a step record as a line of the log, received at at.
func StepAction(at time.Time, record StepRecord) Action {
	return Action{
		At: at, Kind: ActionStep, Tool: record.Tool, Step: record.Step,
		Command: record.Command, Observation: record.Observation, Exit: record.Exit,
		Added: record.Added, Removed: record.Removed,
	}
}

// EndAction is the terminal record as the log's last line: its status and its
// sentence. The rest of the record is the result's, which the run keeps whole.
func EndAction(at time.Time, t Terminal) Action {
	return Action{At: at, Kind: ActionEnd, Status: t.Status, Message: t.Message}
}

// capped is the line as it is written: every text held to the reader's own
// caps, the observation and the message to a turn's, and data that is not an
// object under [StageDataCap] left off.
func (a Action) capped() Action {
	a.Command = cut(oneLine(a.Command), commandCap)
	a.Observation = cut(a.Observation, turnTextCap)
	a.Message = cut(a.Message, turnTextCap)
	a.Tool, a.Step = label(a.Tool), label(a.Step)
	a.Stage, a.Status = label(a.Stage), label(a.Status)
	a.Data = stageData(a.Data)
	return a
}

// AppendAction writes one line to the log in dir, capped, in one write, making
// the folder when it is not there.
func AppendAction(dir string, action Action) error {
	line, err := json.Marshal(action.capped())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, ActionsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// ReadActions reads the log in dir in the order it was written, and the last n
// lines of it (n <= 0 for all). A log that is not there is no actions and no
// error — a run from before the log existed, or one whose program has said
// nothing yet — and a line that does not parse is skipped, because a log cut
// mid-write is still a log.
func ReadActions(dir string, n int) ([]Action, error) {
	file, err := os.Open(filepath.Join(dir, ActionsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var actions []Action
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var action Action
		if json.Unmarshal([]byte(line), &action) != nil || action.Kind == "" {
			continue
		}
		actions = append(actions, action)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if n > 0 && len(actions) > n {
		actions = actions[len(actions)-n:]
	}
	return actions, nil
}

// Shown is one action as a person reads it on its program's page: the step it
// belongs under, the words, and how it came out. A program's own vocabulary
// makes it ([Delegate.Present]); a program with none is read plainly
// ([Delegate.Reader]).
type Shown struct {
	At time.Time `json:"at"`
	// Step is the word for the part of the program's process the action
	// served, printed once at the head of each run of actions in it. Empty is
	// an action inside whatever step is under way.
	Step string `json:"step,omitempty"`
	// Text is the action in words: `read internal/auth/middleware.go`.
	Text string `json:"text"`
	// Outcome is how it came out, in a word or two: `passes`, `fails · exit 2`,
	// `4 files`. Empty when there is nothing to say.
	Outcome string `json:"outcome,omitempty"`
	// Detail is the whole of the step as the log kept it — the command or
	// argument, and what came back — which the page opens under the action's
	// one line when it is clicked. Empty for a line with nothing more to show.
	Detail string `json:"detail,omitempty"`
	// Lines says the action changed a file and counted how: Added and Removed
	// are its lines added and removed, drawn as `+N,-M` in the diff's own
	// colours beside the action. False for every other action.
	Lines   bool `json:"lines,omitempty"`
	Added   int  `json:"added,omitempty"`
	Removed int  `json:"removed,omitempty"`
	// Steer marks the program steering its own model — a nudge, a last turn, a
	// retry after a dropped call, a correction — rather than working through it.
	Steer bool `json:"steer,omitempty"`
	// Memory marks the action that says the program compacted its memory, and
	// Model names the model an action says it moved to, with Reason why. The
	// page reads both beside the conversation log, which says the same two
	// things from the model's side, so one compaction or one switch is drawn
	// once.
	Memory bool   `json:"memory,omitempty"`
	Model  string `json:"model,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ActionReader reads a program's action log for its page: each line, in the
// order the log holds them, as the words a person reads ([Shown]), and false
// for a line the page leaves out. A reader may remember the lines before — a
// program can only tell its model's second nudge from a retry of the first by
// what came earlier — so one reader reads one log, from its first line.
type ActionReader func(Action) (Shown, bool)

// Reader is a fresh reader of this program's action log: through the program's
// own vocabulary when it has one ([Delegate.Present]), and plainly otherwise —
// a stage as its name and status, a step as its command with its step id as
// the step's word and its exit as the outcome, the ending as its sentence. A
// line with no words is left out, and every line shown keeps the moment codeaf
// received it.
func (d Delegate) Reader() ActionReader {
	read := ActionReader(plainShown)
	if d.Present != nil {
		if own := d.Present(); own != nil {
			read = own
		}
	}
	return func(action Action) (Shown, bool) {
		shown, ok := read(action)
		if !ok || strings.TrimSpace(shown.Text) == "" {
			return Shown{}, false
		}
		shown.At = action.At
		return shown, true
	}
}

// plainShown is a program's action read with no vocabulary of its own.
func plainShown(action Action) (Shown, bool) {
	switch action.Kind {
	case ActionStage:
		text := action.Stage
		if action.Status != "" {
			text += " · " + action.Status
		}
		return Shown{Text: text}, true
	case ActionStep:
		return Shown{Step: action.Step, Text: action.Command, Outcome: ExitWord(action.Exit)}, true
	case ActionEnd:
		return Shown{Text: action.Message}, true
	}
	return Shown{}, false
}

// ExitWord is how a command came out, in the words every program's page uses:
// `passes` for an exit of 0, `fails · exit N` for any other, and nothing for an
// action that ran no command or learned no exit.
func ExitWord(exit *int) string {
	if exit == nil {
		return ""
	}
	if *exit == 0 {
		return "passes"
	}
	return "fails · exit " + strconv.Itoa(*exit)
}
