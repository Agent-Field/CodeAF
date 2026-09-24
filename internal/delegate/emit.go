package delegate

// The writing half of the records, for the program's side of the pipe. The
// reader (protocol.go) is the parent's; this is what a program running as
// codeaf's child calls through its [Host], so the two halves are one package
// and cannot disagree about a field's spelling.

import (
	"encoding/json"
	"io"
	"sync"
)

// Ending is a program's result as it writes it: the terminal record, in
// fields rather than a map, so a program cannot misspell the one record the
// whole protocol exists for.
type Ending struct {
	// Status is StatusPass, StatusFail, StatusBudget or StatusCrashed.
	Status string
	// Message is one sentence saying why.
	Message string
	// CostUSD is the program's own reading of what it spent, zero for none.
	// codeaf's model API meters every call itself; this figure is kept for the
	// record and never trusted over that one.
	CostUSD float64
	// Reason is the longer reason, when there is one.
	Reason string
	// Claim is what the program's model said it did, and Observed is what the
	// program itself verified. They are two witnesses and stay two fields.
	Claim    string
	Observed string
	// Deliverable is the answer text of a program that lands text.
	Deliverable string
	// Extra is any other data the program wants on the record. It never
	// overrides a field above.
	Extra map[string]any
}

// record is the Ending on the wire.
func (e Ending) record() map[string]any {
	data := map[string]any{}
	for key, value := range e.Extra {
		data[key] = value
	}
	set := func(key, value string) {
		if value != "" {
			data[key] = value
		}
	}
	if e.CostUSD > 0 {
		data["cost_usd"] = e.CostUSD
	}
	set("reason", e.Reason)
	set("claim", e.Claim)
	set("observed", e.Observed)
	set("deliverable", e.Deliverable)
	return map[string]any{"type": RecordTerminal, "status": e.Status, "message": e.Message, "data": data}
}

// Emitter writes a program's records on its stdout: one JSON object per line,
// each written whole under one lock, so two goroutines of the program can
// never interleave half a line of each.
//
// THE TERMINAL IS WRITTEN AT MOST ONCE. A second is dropped here rather than
// sent for the reader to drop, so a program's own "and one more for luck" on
// its way out cannot become the record a person reads.
type Emitter struct {
	mu    sync.Mutex
	w     io.Writer
	ended bool
	err   error
}

// NewEmitter writes to w, which for a running program is its stdout.
func NewEmitter(w io.Writer) *Emitter { return &Emitter{w: w} }

// Hello writes the first record.
func (e *Emitter) Hello(name string, stages []string) error {
	return e.write(map[string]any{"type": RecordHello, "protocol": ProtocolVersion, "delegate": name, "stages": stages})
}

// Stage writes a phase change, with its data when it is an object the reader
// will keep ([StageDataCap]) and without it otherwise, so the record a program
// writes is the record that arrives.
func (e *Emitter) Stage(stage StageRecord) error {
	record := map[string]any{"type": RecordStage, "stage": stage.Stage, "status": stage.Status}
	if data := stageData(stage.Data); data != nil {
		record["data"] = data
	}
	return e.write(record)
}

// Step writes one finished action, capped the way the reader caps it, so what
// the program meant to say is what arrives. The optional fields are written
// only when they say something.
func (e *Emitter) Step(step StepRecord) error {
	record := map[string]any{"type": RecordStep, "command": cut(oneLine(step.Command), commandCap)}
	if step.Observation != "" {
		record["observation"] = cut(step.Observation, observationCap)
	}
	if tool := label(step.Tool); tool != "" {
		record["tool"] = tool
	}
	if id := label(step.Step); id != "" {
		record["step"] = id
	}
	if step.Exit != nil {
		record["exit"] = *step.Exit
	}
	return e.write(record)
}

// Terminal writes the result, once.
func (e *Emitter) Terminal(end Ending) error {
	e.mu.Lock()
	if e.ended {
		e.mu.Unlock()
		return nil
	}
	e.ended = true
	e.mu.Unlock()
	return e.write(end.record())
}

// Ended answers whether the terminal has been written.
func (e *Emitter) Ended() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ended
}

// Err is the first write that failed, if any. A program whose stdout is gone
// has nobody left to tell; the error is kept so its ending can say so.
func (e *Emitter) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

func (e *Emitter) write(record map[string]any) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.w.Write(line); err != nil {
		if e.err == nil {
			e.err = err
		}
		return err
	}
	return nil
}
