package subharness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// THE RUN IS A TRACE DAG, and it is the only durable output of an execution.
//
// A sub-harness is a shape somebody approved once and will run many times, so
// the interesting question after a run is never "what did it print" — it is
// "which way did it go, and where did the time and the refusals land". A list of
// lines cannot answer that: a branch taken, a lane that lost the race, a loop
// that needed three rounds, a gate a person declined, are all facts about the
// SHAPE the run took, and the shape is a graph.
//
// So every node that runs writes one [Trace], and every trace names the traces
// it came after ([Trace.Needs]). The result is an ordinary DAG:
//
//   - a straight run is a chain;
//   - a branch is a chain that skipped its other arms, which are simply absent;
//   - a loop is one trace per round, each carrying its round number;
//   - a split is a fan-out to the lanes and a parallel.join fanning back in —
//     the one kind that is never written in a program and always in a trace
//     (kinds.go).
//
// The file is plain JSON under harnesses/<name>/run/<ts>.json, and it is written
// ONCE, at the end. A trace streamed to disk would be a second thing to keep
// consistent under a cancel, and the run is seconds-to-minutes long: the loss
// window is a crash, and a crashed run's honest record is that it has none.

// outputCap bounds what one trace node carries of its own output. A trace is
// read by a person in a panel and by the next builder as evidence; the whole of
// a build log is neither.
const outputCap = 4 << 10

// Status is how a run ended.
type Status string

const (
	// StatusOK means the program reached its end.
	StatusOK Status = "ok"
	// StatusFailed means a node failed and nothing caught it.
	StatusFailed Status = "failed"
	// StatusDeclined means a person said no at a human.gate. It is NOT a
	// failure: the gate did exactly what it is for, and a history that filed
	// every refusal as a fault would be a history that punishes the feature.
	StatusDeclined Status = "declined"
	// StatusIntervened means a person took the run over at a gate — the
	// escalation ([GateAnswer.Intervene]). The program stopped where it stood
	// and a human continued from there.
	StatusIntervened Status = "intervened"
	// StatusCancelled means the context died: an interrupt, a closed session, a
	// deadline.
	StatusCancelled Status = "cancelled"
)

// Run is one execution of one version of one harness.
type Run struct {
	Harness string `json:"harness"`
	Version int    `json:"version"`
	// Input is what the run was started on, verbatim. It is the one thing that
	// makes a trace re-runnable by hand.
	Input string `json:"input,omitempty"`
	// Trigger says what started it — "person", or the trigger node's kind.
	Trigger  string    `json:"trigger,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	Status   Status    `json:"status"`
	// Error is why, when the status is not ok.
	Error string `json:"error,omitempty"`
	// Output is what the last node produced, capped.
	Output string `json:"output,omitempty"`
	// Nodes is the DAG, in the order the traces were opened.
	Nodes []Trace `json:"nodes"`
}

// Trace is one node's moment in a run.
type Trace struct {
	// ID is unique inside this run: the program node's id, plus the round or the
	// lane when one node runs more than once. A program id appearing twice in a
	// DAG would make every edge ambiguous.
	ID string `json:"id"`
	// Node is the program node's own id — what the card calls it.
	Node string `json:"node"`
	Kind Kind   `json:"kind"`
	// Needs is the trace ids this one ran after. It is the edge set, and it is
	// what makes this a DAG rather than a list with timestamps.
	Needs []string `json:"needs,omitempty"`
	// Round is which pass of a loop this was, 1-based; zero outside a loop.
	Round int `json:"round,omitempty"`
	// Lane is which arm of a split this ran in; empty outside one.
	Lane string `json:"lane,omitempty"`

	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	// OK is whether this node succeeded. A verify that returned false is a node
	// that RAN correctly and reported a failure, which is OK=false — the two are
	// the same fact from the program's side, which is why the condition language
	// has one word for it (predicate.go's `failed`).
	OK bool `json:"ok"`
	// Output is what the node produced, capped, and Error why it did not.
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	// Answer is a human.gate's outcome in one word — approved, declined,
	// intervened — and empty on every other kind.
	Answer string `json:"answer,omitempty"`
	// Note carries what the node itself wanted said: the condition a branch
	// matched, the rung a verify climbed, the version a call resolved to. It is
	// the difference between a trace you can read and a trace you can only
	// diff.
	Note string `json:"note,omitempty"`
}

// Elapsed is how long this node took, and zero for one still open.
func (t Trace) Elapsed() time.Duration {
	if t.Finished.IsZero() {
		return 0
	}
	return t.Finished.Sub(t.Started)
}

// Elapsed is the run's wall time.
func (r Run) Elapsed() time.Duration {
	if r.Finished.IsZero() {
		return 0
	}
	return r.Finished.Sub(r.Started)
}

// Failed reports whether this run ended in a way somebody should look at. A
// decline is not one: a person answered a question, and the answer was no.
func (r Run) Failed() bool {
	return r.Status == StatusFailed || r.Status == StatusCancelled
}

func (r Run) encode() ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(r); err != nil {
		return nil, fmt.Errorf("subharness: %w", err)
	}
	return out.Bytes(), nil
}

func decodeRun(data []byte) (Run, error) {
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return run, fmt.Errorf("subharness: %w", err)
	}
	return run, nil
}

// ── the recorder ────────────────────────────────────────────────────────────

// recorder collects traces during a run. It is locked because a parallel.split
// runs its lanes concurrently and every one of them writes here.
type recorder struct {
	mu    sync.Mutex
	nodes []Trace
	// taken counts how many traces each program id has produced, which is what
	// makes a loop's third round "check#3" instead of a second "check".
	taken map[string]int
}

func newRecorder() *recorder { return &recorder{taken: map[string]int{}} }

// open starts one trace and returns its id. needs is the edge set — the ids this
// node ran after — and is copied, because a caller's slice is its own.
func (r *recorder) open(node Node, needs []string, round int, lane string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := node.ID
	r.taken[node.ID]++
	if seen := r.taken[node.ID]; seen > 1 {
		id = fmt.Sprintf("%s#%d", node.ID, seen)
	}
	r.nodes = append(r.nodes, Trace{
		ID:      id,
		Node:    node.ID,
		Kind:    node.Kind,
		Needs:   append([]string(nil), needs...),
		Round:   round,
		Lane:    lane,
		Started: time.Now().UTC(),
	})
	return id
}

// close settles one trace. An id nobody opened is ignored rather than panicking:
// the recorder is bookkeeping, and bookkeeping must never be the thing that ends
// a run.
func (r *recorder) close(id string, ok bool, output, note, answer string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for at := range r.nodes {
		if r.nodes[at].ID != id {
			continue
		}
		r.nodes[at].Finished = time.Now().UTC()
		r.nodes[at].OK = ok
		r.nodes[at].Output = capText(output)
		r.nodes[at].Note = note
		r.nodes[at].Answer = answer
		if err != nil {
			r.nodes[at].Error = err.Error()
		}
		return
	}
}

func (r *recorder) take() []Trace {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Trace(nil), r.nodes...)
}

// capText bounds one trace field, marking the cut on a rune boundary so a JSON
// file never carries half a character.
func capText(text string) string {
	if len(text) <= outputCap {
		return text
	}
	cut := outputCap
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut] + fmt.Sprintf("… (%d more bytes)", len(text)-cut)
}
