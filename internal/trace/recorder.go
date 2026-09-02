package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// timeLayout is how every ts in the record is spelled, and it is calllog's
// layout to the millisecond so that a line of the model-call log and a line of
// the record can be read side by side without converting anything.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// CallBody is one model call, whole: what went out, what came back, and how it
// ended. It is written as a file of its own named by the call id, because a
// body is the one record a reader wants exactly one of, and because a
// megabyte-long line in the events file would make that file unreadable for
// every other purpose.
type CallBody struct {
	// CallID is the token the model-call log already mints per attempt. It is
	// what joins this file to that line and to the conversation's own transcript.
	CallID string
	Model  string
	// Request and Response are the wire bodies as bytes. They are written as
	// JSON where they are JSON and as a string otherwise, so a reader gets one
	// document to read rather than JSON quoted inside JSON.
	Request  []byte
	Response []byte
	// Error is the provider's own sentence where the call failed, unclipped:
	// the reason to keep the record is that the exact words are what is wrong.
	Error string
	// Finish is how the reply ended — "stop", "length", "tool_calls" — and
	// Reasoning is the thinking text where the endpoint returned it separately
	// from the answer.
	Finish    string
	Reasoning string
}

// ToolEvent is one tool call as it ran. Status is "ok", "failed" or "refused",
// and the third is a fact the other two cannot carry: a tool a gate refused
// never ran at all, and a record in which a refusal reads like a failure is a
// record that sends somebody debugging the tool instead of the gate.
type ToolEvent struct {
	CallID   string
	Name     string
	Args     string
	Result   string
	Started  time.Time
	Duration time.Duration
	Status   string
	// Refuser is who said no — the approval policy, the guardian, a budget —
	// and Reason is their own sentence for it.
	Refuser string
	Reason  string
}

// Decision is a choice the run made, with the reason it made it. Kind is what
// KIND of choice it was ("lane", "hedge", "effort"), Subject is what the choice
// was about, Choice is what was chosen, and Alternatives are what was not — the
// four together being what somebody reconstructing a run actually asks for.
type Decision struct {
	CallID       string
	Kind         string
	Subject      string
	Choice       string
	Reason       string
	Alternatives []string
}

// Recorder is one run's folder. EVERY METHOD IS A NO-OP ON A NIL RECEIVER, so a
// feeder site is one call and one nil check and never a branch around the call
// itself — which is the property that lets these sit on the hot path.
type Recorder struct {
	run  string
	dir  string
	max  int64
	keep int

	mutex sync.Mutex
	// events is the appended file, opened by the first record. Nothing is
	// opened before then, so a run that records nothing leaves no folder.
	events *os.File
	// bytes is what this run has written, counted rather than stat'd for the
	// reason calllog counts: a stat per record is a syscall to learn something
	// this process already knows.
	bytes int64
	// capped is set by the run reaching [MaxRunBytes], and silenced by a write
	// that failed. Both stop the record; only the first says so in the file,
	// because the second is a disk that cannot be written to.
	capped   bool
	silenced bool
	wrote    bool
}

// Folder is where this run's record is, whether or not anything is in it yet.
func (r *Recorder) Folder() string {
	if r == nil {
		return ""
	}
	return r.dir
}

// Wrote reports whether anything reached the disk, which is what a door asks
// before printing the folder's path at exit.
func (r *Recorder) Wrote() bool {
	if r == nil {
		return false
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.wrote
}

// Call writes one model call's bodies. The context is taken for the shape every
// feeder site already has; the run is the recorder's own, because a recorder
// handed a context from another run would write a record under the wrong id.
func (r *Recorder) Call(ctx context.Context, body CallBody) {
	if r == nil {
		return
	}
	document := map[string]any{
		"kind":  "call",
		"run":   r.run,
		"ts":    time.Now().Format(timeLayout),
		"call":  body.CallID,
		"model": body.Model,
	}
	putRaw(document, "request", body.Request)
	putRaw(document, "response", body.Response)
	putText(document, "reasoning", body.Reasoning)
	putText(document, "finish", body.Finish)
	putText(document, "error", body.Error)
	line, err := json.Marshal(document)
	if err != nil {
		// A record that will not serialize is a bug in the builder rather than
		// a broken disk, and it must not stop the records that follow.
		return
	}
	name := body.CallID
	if name == "" {
		// A call with no id still has bodies worth keeping; it simply cannot be
		// joined to a line of the model-call log. The time is the only other
		// thing that names it.
		name = "call-" + time.Now().Format("150405.000")
	}
	r.writeFile(filepath.Join(CallsDirName, name+".json"), line)
}

// Tool appends one tool call.
func (r *Recorder) Tool(ctx context.Context, event ToolEvent) {
	if r == nil {
		return
	}
	document := map[string]any{
		"kind": "tool",
		"run":  r.run,
		"ts":   time.Now().Format(timeLayout),
		"tool": event.Name,
	}
	putText(document, "call", event.CallID)
	putText(document, "args", event.Args)
	putText(document, "result", event.Result)
	putText(document, "status", event.Status)
	putText(document, "refused_by", event.Refuser)
	putText(document, "reason", event.Reason)
	if !event.Started.IsZero() {
		document["started"] = event.Started.Format(timeLayout)
	}
	// The emptiness law applies to files as much as to screens: a duration
	// nobody measured is absent rather than a zero somebody could read as
	// instant.
	if event.Duration > 0 {
		document["ms"] = event.Duration.Milliseconds()
	}
	r.appendEvent(document)
}

// Decision appends one choice and its reason.
func (r *Recorder) Decision(ctx context.Context, decision Decision) {
	if r == nil {
		return
	}
	document := map[string]any{
		"kind": "decision",
		"run":  r.run,
		"ts":   time.Now().Format(timeLayout),
	}
	putText(document, "call", decision.CallID)
	putText(document, "decision", decision.Kind)
	putText(document, "subject", decision.Subject)
	putText(document, "choice", decision.Choice)
	putText(document, "reason", decision.Reason)
	if len(decision.Alternatives) > 0 {
		document["alternatives"] = decision.Alternatives
	}
	r.appendEvent(document)
}

// putRaw writes a wire body as JSON where it is JSON and as a string where it
// is not, after scrubbing it. Absent where there is nothing, because a request
// that was never sent has no body and an empty one would say it did.
func putRaw(document map[string]any, field string, body []byte) {
	if len(body) == 0 {
		return
	}
	clean := Scrub(body)
	if json.Valid(clean) {
		document[field] = json.RawMessage(clean)
		return
	}
	document[field] = string(clean)
}

func putText(document map[string]any, field, value string) {
	if value == "" {
		return
	}
	document[field] = string(Scrub([]byte(value)))
}

func (r *Recorder) appendEvent(document map[string]any) {
	line, err := json.Marshal(document)
	if err != nil {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.append(line)
}

// append is the one road to the events file, under the mutex its callers hold.
// ONE APPENDER WITH ONE MUTEX is the only shape in which records written from
// several goroutines cannot interleave halfway through a line.
func (r *Recorder) append(line []byte) {
	if r.silenced || r.capped {
		return
	}
	if err := r.open(); err != nil {
		r.silence(err)
		return
	}
	if r.bytes+int64(len(line))+1 > r.max {
		r.cap()
		return
	}
	written, err := r.events.Write(append(line, '\n'))
	r.bytes += int64(written)
	r.wrote = true
	if err != nil {
		r.silence(err)
	}
}

// writeFile puts one document in a file of its own inside the run's folder. It
// counts against the same ceiling the events file does, because the ceiling is
// on the RUN and a person's disk does not care which of the two files filled it.
func (r *Recorder) writeFile(name string, document []byte) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.silenced || r.capped {
		return
	}
	if err := r.open(); err != nil {
		r.silence(err)
		return
	}
	if r.bytes+int64(len(document)) > r.max {
		r.cap()
		return
	}
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		r.silence(err)
		return
	}
	if err := os.WriteFile(path, document, 0o600); err != nil {
		r.silence(err)
		return
	}
	r.bytes += int64(len(document))
	r.wrote = true
}

// cap ends the run's record with one line saying why, and stops. The line is
// written past the ceiling on purpose: a record that stopped without saying so
// is indistinguishable from a run that ended early, which is the one reading a
// person must not be left with.
func (r *Recorder) cap() {
	r.capped = true
	line, err := json.Marshal(map[string]any{
		"kind":  "capped",
		"run":   r.run,
		"ts":    time.Now().Format(timeLayout),
		"bytes": r.bytes,
		"max":   r.max,
	})
	if err != nil || r.events == nil {
		return
	}
	if _, err := r.events.Write(append(line, '\n')); err == nil {
		r.wrote = true
	}
}

// open creates the run's folder and the events file on first use, and prunes
// the folders of older runs while it is there. NOTHING IS OPENED BEFORE THE
// FIRST RECORD, so a run that records nothing — every run with the switch off,
// and a switched-on run that never reached a feeder — leaves no folder behind.
func (r *Recorder) open() error {
	if r.events != nil {
		return nil
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}
	prune(filepath.Dir(r.dir), r.keep)
	file, err := os.OpenFile(filepath.Join(r.dir, EventsFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	r.events = file
	r.bytes = 0
	if info, err := file.Stat(); err == nil {
		r.bytes = info.Size()
	}
	return nil
}

// silence stops this run's record after ONE line naming what could not be
// written. One line and not one per record: the surfaces that make model calls
// are drawing a conversation, and a disk problem is not a turn.
func (r *Recorder) silence(err error) {
	if r.events != nil {
		r.events.Close()
		r.events = nil
	}
	r.silenced = true
	fmt.Fprintf(stderr, "aforge: cannot write the debug record at %s (%v); it is off for this run\n", r.dir, err)
}
