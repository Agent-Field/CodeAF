package delegate

// The conversation log: one record per model call a program makes through its
// model API, kept in the task's own record folder beside the trajectory. The
// API writes it (internal/provider) and the task page reads it
// (internal/session), and neither may import the other, so the record and the
// one door each side uses live here.
//
// THIS IS WHAT MAKES A PROGRAM'S WORK VISIBLE. To the program the API is an
// ordinary model backend; to codeaf the program is a very particular person
// asking it things. Every exchange is therefore a turn — what the program sent
// that it had not sent before, and what the model answered — and the page
// draws the turns as the conversation they are.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConversationFile is the log's name inside a task's record folder.
const ConversationFile = "delegate-conversation.jsonl"

// ProgramFile names, inside a task's record folder, which program the run
// handed its task to and the stages it said it would move through (its
// `hello`). The worker writes it when the hello arrives; the task page reads
// it to say whose conversation it is drawing, after the run as well as during.
const ProgramFile = "delegate-program.json"

// ProgramRecord is ProgramFile's content.
type ProgramRecord struct {
	Name   string   `json:"name"`
	Stages []string `json:"stages,omitempty"`
}

// WriteProgram writes the record, whole, making the folder when it is not
// there.
func WriteProgram(dir string, record ProgramRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp := filepath.Join(dir, ProgramFile+".tmp")
	if err := os.WriteFile(temp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(dir, ProgramFile))
}

// ReadProgram reads the record; ok is false for a run that handed its task to
// no program, or whose program has not said hello yet.
func ReadProgram(dir string) (ProgramRecord, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ProgramFile))
	if err != nil {
		return ProgramRecord{}, false
	}
	var record ProgramRecord
	if json.Unmarshal(data, &record) != nil || record.Name == "" {
		return ProgramRecord{}, false
	}
	return record, true
}

// MainThread is the thread a call belongs to when the program gave it no
// other: its one long conversation.
const MainThread = "main"

// Turn is one model call a program made through its model API. A call is
// written twice under one Seq — when it starts, with no Ended, and when it
// ends — and a reader keeps the later, which is how the page shows a call in
// flight without a second file.
type Turn struct {
	Seq int `json:"seq"`
	// Thread tells conversations apart when a program holds more than one at
	// once (a summary of its own history, a helper agent): the call's cache key
	// or its own id, MainThread when it gave none.
	Thread  string    `json:"thread,omitempty"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
	// Model is the model the program asked for; Served is the one that
	// answered, when codeaf's router answered with another.
	Model  string `json:"model"`
	Served string `json:"served,omitempty"`
	// Sent is what the program sent that the thread's previous call did not:
	// its brief first, then its tools' results and its own words. Restarted is
	// true when the program rewrote its history instead of adding to it (a
	// compaction), so Sent is then everything it sent.
	Sent      []Said `json:"sent,omitempty"`
	Restarted bool   `json:"restarted,omitempty"`
	// Reply is the model's text, and Calls the tools it asked the program to run.
	Reply string    `json:"reply,omitempty"`
	Calls []ToolUse `json:"calls,omitempty"`
	// The call's size and price, as the funnel metered them.
	TokensIn  int     `json:"tokens_in,omitempty"`
	TokensOut int     `json:"tokens_out,omitempty"`
	Cached    int     `json:"cached,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
	// Refused is codeaf's own refusal — the ceiling, a run that has ended — set
	// when the call never reached a model. Failed is the model's side failing.
	Refused string `json:"refused,omitempty"`
	Failed  string `json:"failed,omitempty"`
}

// InFlight answers whether the call has not come back yet.
func (t Turn) InFlight() bool { return t.Ended.IsZero() && t.Refused == "" && t.Failed == "" }

// Said is one message a program sent: whose it is and its words. A tool's
// result carries the tool it answers.
type Said struct {
	// Role is "system", "user" or "tool", as the program sent it.
	Role string `json:"role"`
	Tool string `json:"tool,omitempty"`
	Text string `json:"text"`
}

// ToolUse is one tool a model asked the program to run, with its arguments on
// one line.
type ToolUse struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

// The caps a turn is written with. A page draws the first lines of these; the
// whole of a message is the program's own record, never codeaf's.
const (
	turnTextCap  = 2048
	turnSaidMax  = 12
	turnCallsMax = 16
	turnArgsCap  = 200
)

// capped is the turn as it is written: every text cut on a rune boundary, the
// newest messages kept when there are too many, the tool calls bounded.
func (t Turn) capped() Turn {
	t.Reply = cut(t.Reply, turnTextCap)
	if len(t.Sent) > turnSaidMax {
		t.Sent = t.Sent[len(t.Sent)-turnSaidMax:]
	}
	sent := make([]Said, len(t.Sent))
	for i, said := range t.Sent {
		said.Text = cut(said.Text, turnTextCap)
		sent[i] = said
	}
	t.Sent = sent
	if len(t.Calls) > turnCallsMax {
		t.Calls = t.Calls[:turnCallsMax]
	}
	calls := make([]ToolUse, len(t.Calls))
	for i, call := range t.Calls {
		call.Args = cut(oneLine(call.Args), turnArgsCap)
		calls[i] = call
	}
	t.Calls = calls
	if t.Thread == "" {
		t.Thread = MainThread
	}
	return t
}

// AppendTurn writes one turn to the log in dir, capped, in one write, making
// the folder when it is not there.
func AppendTurn(dir string, turn Turn) error {
	line, err := json.Marshal(turn.capped())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, ConversationFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// ReadTurns reads the log in dir: every call once, in the order they started,
// each as its latest record says, and the last n of them (n <= 0 for all). A
// log that is not there is no turns and no error, because a run that has not
// called a model yet has said nothing; a line that does not parse is skipped,
// because a log cut mid-write is still a log.
func ReadTurns(dir string, n int) ([]Turn, error) {
	file, err := os.Open(filepath.Join(dir, ConversationFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	latest := map[int]int{}
	var turns []Turn
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var turn Turn
		if json.Unmarshal([]byte(line), &turn) != nil {
			continue
		}
		if at, seen := latest[turn.Seq]; seen {
			turns[at] = turn
			continue
		}
		latest[turn.Seq] = len(turns)
		turns = append(turns, turn)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if n > 0 && len(turns) > n {
		turns = turns[len(turns)-n:]
	}
	return turns, nil
}
