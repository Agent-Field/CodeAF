package delegate

// The records: one JSON object per line on the program's stdout, the types
// below read, everything else ignored (docs/design/delegate/PROTOCOL.md).
// Ignoring the rest is what makes the reader generic — a program's own records
// pass straight through — and it is also why a line that is not JSON at all is
// dropped and counted rather than failing the run: a program that printed one
// stray line has not stopped being one codeaf can run.
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
	RecordHello = "hello"
	RecordStage = "stage"
	// RecordSpend is v1's cumulative cost. It is still read until codeaf's
	// model API meters every call itself, which makes it the one source of
	// truth for money and this record redundant.
	RecordSpend    = "spend"
	RecordStep     = "step"
	RecordTerminal = "terminal"
)

// ProtocolVersion is the version `hello` carries. Both ends are this package,
// so it moves only when a record changes meaning, and a mismatch means the two
// processes are two builds.
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
)

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
	// Stage is a phase change: the live step.
	Stage(stage, status string)
	// Spend is the cumulative cost so far. The reader guarantees it never
	// goes down: a program that sends a lower figure is answered with the
	// last high one, because the bank behind this reads deltas.
	Spend(usd float64)
	// Step is one finished action: command and the observation head, both
	// already capped.
	Step(command, observation string)
	// Terminal is the result. It is told at most once; a second terminal on
	// the stream is ignored, because the contract says exactly one and the
	// first is the one the program wrote on purpose.
	Terminal(t Terminal)
}

// Reading is what a reader saw, for the record the launch keeps: the last
// stage, the high-water spend, how many steps, whether a terminal arrived, and
// how many lines were not the protocol's (dropped, not failed).
type Reading struct {
	Hello      *Hello
	LastStage  string
	LastStatus string
	SpendUSD   float64
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
			var rec struct {
				Stage  string `json:"stage"`
				Status string `json:"status"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil || rec.Stage == "" {
				reading.Ignored++
				continue
			}
			reading.LastStage, reading.LastStatus = rec.Stage, rec.Status
			if sink != nil {
				sink.Stage(rec.Stage, rec.Status)
			}
		case RecordSpend:
			var rec struct {
				CostUSD *float64 `json:"cost_usd"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil || rec.CostUSD == nil {
				reading.Ignored++
				continue
			}
			// NEVER DOWN. The bank behind the sink adds deltas, and a figure
			// that fell would be a refund nobody issued.
			if *rec.CostUSD > reading.SpendUSD {
				reading.SpendUSD = *rec.CostUSD
			}
			if sink != nil {
				sink.Spend(reading.SpendUSD)
			}
		case RecordStep:
			var rec struct {
				Command     string `json:"command"`
				Observation string `json:"observation"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil || strings.TrimSpace(rec.Command) == "" {
				reading.Ignored++
				continue
			}
			reading.Steps++
			if sink != nil {
				sink.Step(cut(oneLine(rec.Command), commandCap), cut(rec.Observation, observationCap))
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
