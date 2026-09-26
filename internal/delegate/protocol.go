package delegate

// The records: one JSON object per line on the program's stdout, the types
// below read, everything else ignored (docs/design/delegate/PROTOCOL.md).
// Ignoring the rest is what makes the reader generic — a program's own records
// pass straight through — and it is also why a line that is not JSON at all is
// dropped and counted rather than failing the run: a program that printed one
// stray line has not stopped being one codeaf can run.
//
// THERE IS NO SPEND RECORD. Version 1 read a cumulative `spend` the program
// reported about itself; the model API (internal/provider/modelapi) meters
// every call the program makes as it is made, so money has one source of truth
// and it is not the program's word. A `spend` line a program still writes is
// one more line this reader does not know, ignored and counted like any other.
//
// VERSION 2 IS INTERNAL. Both ends are compiled from this package into one
// binary, so the Go types here are the specification and the number in `hello`
// guards the one case where the two ends can still differ: an engine that
// outlived a rebuild starting the NEW binary as its child.

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The record types.
const (
	// RecordHello is the first line a program writes: the protocol it speaks,
	// its name, and the stages it will move through, in order.
	RecordHello    = "hello"
	RecordStage    = "stage"
	RecordStep     = "step"
	RecordTerminal = "terminal"
)

// ProtocolVersion is the version `hello` carries. Both ends are this package,
// so it moves only when a record changes meaning, and a mismatch means the two
// processes are two builds.
//
// AN OPTIONAL FIELD ADDED TO A RECORD IS NOT A NEW MEANING. A stage's `data`
// and a step's `tool`, `step` and `exit` arrived inside version 2: a reader
// that predates them ignores them as it ignores every field it does not know,
// and a program that does not send them is read exactly as before.
const ProtocolVersion = 2

// Hello is the first record: who is speaking, in which protocol, and the
// stages it will move through, which is what lets a page draw the whole track
// before the program has reached the end of it.
type Hello struct {
	Protocol int      `json:"protocol"`
	Delegate string   `json:"delegate"`
	Stages   []string `json:"stages,omitempty"`
}

// The terminal statuses. The set is closed and it is senior-dev's, because
// senior-dev's projection of an ending onto four words was already the right one:
// the work stands, it does not, a ceiling stopped it, or the program itself
// broke.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusBudget  = "budget-exhausted"
	StatusCrashed = "crashed"
)

// Caps the reader applies so a record can never carry more than the page
// draws. A program that sends more is cut here, on a rune boundary, rather
// than trusted to have capped itself.
const (
	commandCap     = 200
	observationCap = 2048
	// labelCap bounds a step's tool name and its step id: each is one word a
	// page prints, never a payload.
	labelCap = 64
)

// StageDataCap is the most bytes a stage record's data may take, in JSON. It
// is a curated copy of what the program already knows about the phase — an
// attempt number, a count, a verdict of its own checks — for a page to say in
// words, and never the program's whole account of itself, which stays on its
// stderr. A reader drops data past it rather than cut it, because half an
// object is not an object; the program is expected to have curated to it, and
// senior-dev does (internal/seniordev/app's stage_data.go).
const StageDataCap = 1024

// StageRecord is one `stage` record: the phase the program moved to, how it
// stands in it, and the small copy of what it knows about it.
type StageRecord struct {
	Stage  string `json:"stage"`
	Status string `json:"status"`
	// Data is a JSON object of at most [StageDataCap] bytes, or nothing. It is
	// OPTIONAL AND ADDITIVE: a reader of version 2 that predates it reads the
	// record without it.
	Data json.RawMessage `json:"data,omitempty"`
}

// StepRecord is one `step` record: one finished action, what was run and the
// head of what came back, and — each optional, each absent from a program that
// does not say it — the tool that ran it, the step of the program's own
// process it served, and a command's exit code.
type StepRecord struct {
	// Command is the action on one line, `<tool>: <what it was about>`.
	Command string `json:"command"`
	// Observation is the head of what came back.
	Observation string `json:"observation,omitempty"`
	// Tool is the tool's own name.
	Tool string `json:"tool,omitempty"`
	// Step is the program's own id for the part of its process the action
	// served (senior-dev's are app.Steps). It is the program's word, drawn
	// through the program's own vocabulary ([Delegate.Present]).
	Step string `json:"step,omitempty"`
	// Exit is a command's exit code, present only for an action that ran a
	// command and learned how it exited — which is why it is a pointer: a
	// command that exited 0 and an action that ran none are two facts.
	Exit *int `json:"exit,omitempty"`
	// Added and Removed are the lines an action that changed a file added and
	// removed, present only when the program counted them.
	Added   *int `json:"added,omitempty"`
	Removed *int `json:"removed,omitempty"`
}

// stageData is a record's data as a reader keeps it: a JSON object of at most
// [StageDataCap] bytes, and nothing for anything else.
func stageData(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) > StageDataCap || !strings.HasPrefix(trimmed, "{") || !json.Valid([]byte(trimmed)) {
		return nil
	}
	return json.RawMessage(trimmed)
}

// label is a step's tool name or step id as a reader keeps it: one line, cut.
func label(s string) string { return cut(oneLine(s), labelCap) }

// maxLineBytes bounds one stdout line. A program that writes a megabyte on one
// line is mirroring something it should not, and a reader without a bound is
// a way for a child to take the parent's memory.
const maxLineBytes = 4 << 20

// Terminal is the one record that is the result. Data is kept whole so the
// landing note can read the optional keys, in the protocol's spelling and in
// senior-dev's own, through the accessors below rather than by every caller
// knowing both.
type Terminal struct {
	Status  string                     `json:"status"`
	Message string                     `json:"message"`
	Data    map[string]json.RawMessage `json:"data"`
}

// CostUSD is the final total, and false when the record did not carry one.
func (t Terminal) CostUSD() (float64, bool) { return t.number("cost_usd") }

// Reason is the longer reason when there is one.
func (t Terminal) Reason() string { return t.text("reason") }

// Claim is what the program's model said it did: `claim` in the protocol,
// `submission_reason` in senior-dev's record.
func (t Terminal) Claim() string { return first(t.text("claim"), t.text("submission_reason")) }

// Observed is what the program itself verified: `observed` in the protocol.
// senior-dev spells its observation as its own inner status and a count of
// failing verification commands, which read here as one sentence so the
// landing note can keep the claim and the observation apart.
func (t Terminal) Observed() string {
	if observed := t.text("observed"); observed != "" {
		return observed
	}
	inner := t.text("status")
	if inner == "" {
		return ""
	}
	if failing, ok := t.number("verification_failing"); ok && failing > 0 {
		commands, _ := t.number("verification_commands")
		return inner + ", verification failed " + strconv.Itoa(int(failing)) + " of " + strconv.Itoa(int(commands)) + " commands"
	}
	return inner
}

// Verdict is the program's own word for how its work stood when it ended —
// senior-dev's inner status (`pass`, `pass-unverified`, `fail`) — beside the
// protocol's status word, and "" when the record carried none.
func (t Terminal) Verdict() string { return t.text("status") }

// Deliverable is the answer text of a delegate that lands text.
func (t Terminal) Deliverable() string { return t.text("deliverable") }

func (t Terminal) text(key string) string {
	raw, ok := t.Data[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func (t Terminal) number(key string) (float64, bool) {
	raw, ok := t.Data[key]
	if !ok {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, true
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// KnownStatus answers whether a terminal status is one of the four.
func KnownStatus(status string) bool {
	switch status {
	case StatusPass, StatusFail, StatusBudget, StatusCrashed:
		return true
	}
	return false
}

// Sink is what a reader tells as the stream arrives. Every method is called on
// the reader's goroutine, in stream order, and none may block on the program:
// a sink that waits on the child is a deadlock with a pipe in the middle.
type Sink interface {
	// Hello is the program's first record, told once.
	Hello(h Hello)
	// Stage is a phase change: the live step, and one line of the program's
	// action log. Its data is already held to [StageDataCap].
	Stage(record StageRecord)
	// Step is one finished action: command and the observation head, both
	// already capped, and the tool, step and exit the program said.
	Step(record StepRecord)
	// Terminal is the result. It is told at most once; a second terminal on
	// the stream is ignored, because the contract says exactly one and the
	// first is the one the program wrote on purpose.
	Terminal(t Terminal)
}

// Reading is what a reader saw, for the record the launch keeps: the last
// stage, how many steps, whether a terminal arrived, and how many lines were
// not the protocol's (dropped, not failed). What the run spent is not here:
// the model API metered it call by call, and a reading of the program's
// stdout is not where money is learned.
type Reading struct {
	Hello      *Hello
	LastStage  string
	LastStatus string
	Steps      int
	Terminal   *Terminal
	Ignored    int
}

// Read consumes r to its end, telling sink each record, and answers what it
// saw. It returns when the stream closes, which for a pipe is when the program
// exits or closes stdout; an error is only a read failure on the stream itself.
func Read(r io.Reader, sink Sink) (Reading, error) {
	var reading Reading
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &head) != nil {
			reading.Ignored++
			continue
		}
		switch head.Type {
		case RecordHello:
			// ONE HELLO. A second is ignored for the reason a second terminal
			// is: the first is the one the program wrote on purpose.
			if reading.Hello != nil {
				reading.Ignored++
				continue
			}
			var rec Hello
			if json.Unmarshal([]byte(line), &rec) != nil {
				reading.Ignored++
				continue
			}
			reading.Hello = &rec
			if sink != nil {
				sink.Hello(rec)
			}
		case RecordStage:
			// THE OPTIONAL FIELDS ARE READ FORGIVINGLY. A stage whose data is
			// not an object, or is past the cap, is still the stage: the data
			// is left off, never the record.
			var rec struct {
				Stage  string          `json:"stage"`
				Status string          `json:"status"`
				Data   json.RawMessage `json:"data"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil || rec.Stage == "" {
				reading.Ignored++
				continue
			}
			reading.LastStage, reading.LastStatus = rec.Stage, rec.Status
			if sink != nil {
				sink.Stage(StageRecord{Stage: rec.Stage, Status: rec.Status, Data: stageData(rec.Data)})
			}
		case RecordStep:
			// And so are a step's: a tool, a step id or an exit of another
			// shape than this reader's is left off, because a program that
			// spelled an optional field its own way has still finished the
			// action it is reporting.
			var rec struct {
				Command     string          `json:"command"`
				Observation string          `json:"observation"`
				Tool        json.RawMessage `json:"tool"`
				Step        json.RawMessage `json:"step"`
				Exit        json.RawMessage `json:"exit"`
				Added       json.RawMessage `json:"added"`
				Removed     json.RawMessage `json:"removed"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil || strings.TrimSpace(rec.Command) == "" {
				reading.Ignored++
				continue
			}
			reading.Steps++
			if sink != nil {
				sink.Step(StepRecord{
					Command:     cut(oneLine(rec.Command), commandCap),
					Observation: cut(rec.Observation, observationCap),
					Tool:        label(rawText(rec.Tool)),
					Step:        label(rawText(rec.Step)),
					Exit:        rawWhole(rec.Exit),
					Added:       rawWhole(rec.Added),
					Removed:     rawWhole(rec.Removed),
				})
			}
		case RecordTerminal:
			if reading.Terminal != nil {
				reading.Ignored++
				continue
			}
			var rec Terminal
			if json.Unmarshal([]byte(line), &rec) != nil || rec.Status == "" {
				reading.Ignored++
				continue
			}
			reading.Terminal = &rec
			if sink != nil {
				sink.Terminal(rec)
			}
		default:
			reading.Ignored++
		}
	}
	return reading, scanner.Err()
}

// rawText is an optional field read as a string, and nothing when it is
// absent or of another shape.
func rawText(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// rawWhole is an optional field read as a whole number, and nil when it is
// absent or of another shape.
func rawWhole(raw json.RawMessage) *int {
	var n int
	if len(raw) == 0 || json.Unmarshal(raw, &n) != nil {
		return nil
	}
	return &n
}

// oneLine folds a command onto one line, because it is drawn in a row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// cut caps text at n bytes on a rune boundary, so a record never opens a
// character it does not close.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
