package subharness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Running a sub-harness is a walk of its program, not an interpretation of it.
// This package owns the shape — what runs next, what a branch skipped, how
// many rounds a loop was allowed — and hands each node to an Exec that owns
// the work. That split is why there is no second engine here: the caller's
// Exec is the session loop, the tool call, the ask; the walk is the only thing
// a sub-harness adds.
//
// What the walk produces is the run's evidence. A Trace is a condensed dag:
// the Trail is what actually executed, in order, and Edges are the edges that
// were actually taken — so a branch that went right leaves a trace with no
// left edge in it, and the run's shape can be read back without re-deriving it
// from the program.

// Result is one node's outcome, as its executor reports it.
type Result struct {
	// Out is the node's output, condensed for the trail. The full artifact
	// belongs wherever the executor keeps artifacts; what lands here is what a
	// person reading the run needs to see.
	Out string
	// Next is a branch node's choice: the id of the one successor to take.
	// Empty means the first successor in program order. Ignored by every other
	// kind.
	Next string
	// Done is a loop.until node's condition: true when the loop may stop.
	// Ignored by every other kind.
	Done bool
}

// Exec runs one node. It is the whole interface between a sub-harness and the
// machinery that does the work.
type Exec func(context.Context, Node) (Result, error)

// Trail is one executed step, condensed. A loop that ran three rounds leaves
// three entries with the same Id and three different Steps, which is the only
// honest way to record a bounded loop in a flat list.
type Trail struct {
	Step    int           `json:"step"`
	Id      string        `json:"id"`
	Kind    string        `json:"kind"`
	Out     string        `json:"out,omitempty"`
	Err     string        `json:"err,omitempty"`
	Elapsed time.Duration `json:"elapsed"`
}

// Trace is one run, whole: who ran, what executed, which edges were taken, and
// what the run spent of its dynamism budget.
type Trace struct {
	Id      Id            `json:"id"`
	Started time.Time     `json:"started"`
	Elapsed time.Duration `json:"elapsed"`
	Trail   []Trail       `json:"trail"`
	Edges   []Edge        `json:"edges,omitempty"`
	// Spent is how much of Dyn.Cap the run used. A run that spent nothing took
	// the shape the program already had.
	Spent int `json:"spent,omitempty"`
	// Err is the failure that ended the run early, if one did. It is recorded
	// rather than only returned because a saved trace outlives the call.
	Err string `json:"err,omitempty"`
}

// Run walks a harness's program, handing each reached node to exec.
//
// The rules are the program's own. A branch takes one successor and everything
// only that branch led to is never reached. A parallel.join in `all` mode
// waits for every incoming edge and is skipped if a branch upstream means one
// will never arrive. A loop.until re-executes the same node until its executor
// says Done, bounded by both its own max_rounds and what is left of the
// harness's dynamism budget — the run stops deciding when the budget is gone,
// it does not fail.
//
// A failing exec ends the run: the failure is recorded in the trail, in the
// trace, and returned. The trace comes back either way, because a run that
// died halfway is exactly the run worth reading.
func Run(ctx context.Context, h Harness, exec Exec) (Trace, error) {
	h = h.Normalize()
	if err := Validate(h); err != nil {
		return Trace{}, err
	}
	if exec == nil {
		return Trace{}, fmt.Errorf("subharness: %s: run with no executor", h.Id.Name)
	}
	order, err := topo(h.Program)
	if err != nil {
		return Trace{}, fmt.Errorf("subharness: %s: %w", h.Id.Name, err)
	}

	started := time.Now()
	trace := Trace{Id: h.Id, Started: started}
	live := map[Edge]bool{}
	budget := h.Dyn.Cap

	fail := func(node Node, elapsed time.Duration, err error) (Trace, error) {
		trace.Trail = append(trace.Trail, Trail{
			Step: len(trace.Trail) + 1, Id: node.Id, Kind: node.Kind,
			Err: err.Error(), Elapsed: elapsed,
		})
		trace.Err = err.Error()
		trace.Elapsed = time.Since(started)
		trace.Edges = liveEdges(h.Program, live)
		return trace, err
	}

	for _, id := range order {
		node, _ := h.Program.Node(id)
		// The entry is the one node nothing leads to; every other node runs
		// only if something that ran leads to it.
		if len(h.Program.Predecessors(id)) > 0 && !enters(h.Program, node, live) {
			continue
		}

		rounds := 1
		if node.Kind == KindLoopUntil {
			rounds = node.Fields.Int("max_rounds", DefaultRounds)
			if extra := rounds - 1; extra > budget {
				rounds = budget + 1
			}
		}
		var last Result
		for round := 0; round < rounds; round++ {
			if err := ctx.Err(); err != nil {
				return fail(node, 0, err)
			}
			at := time.Now()
			result, err := exec(ctx, node)
			elapsed := time.Since(at)
			if err != nil {
				return fail(node, elapsed, fmt.Errorf("subharness: %s: node %q: %w", h.Id.Name, node.Id, err))
			}
			trace.Trail = append(trace.Trail, Trail{
				Step: len(trace.Trail) + 1, Id: node.Id, Kind: node.Kind,
				Out: result.Out, Elapsed: elapsed,
			})
			last = result
			if round > 0 {
				// A round the program did not already contain is a decision,
				// and decisions are what the dynamism budget buys.
				budget--
				trace.Spent++
			}
			if node.Kind != KindLoopUntil || result.Done {
				break
			}
		}

		successors := h.Program.Successors(id)
		if node.Kind == KindBranch && len(successors) > 0 {
			taken := last.Next
			if taken == "" {
				taken = successors[0]
			}
			if !contains(successors, taken) {
				return fail(node, 0, fmt.Errorf("subharness: %s: node %q branched to %q, which it does not lead to",
					h.Id.Name, node.Id, taken))
			}
			successors = []string{taken}
		}
		for _, next := range successors {
			live[Edge{id, next}] = true
		}
	}

	trace.Elapsed = time.Since(started)
	trace.Edges = liveEdges(h.Program, live)
	return trace, nil
}

// Run loads a version pointer, runs it, and saves the trace under the
// harness's run directory. The trace is saved whether the run succeeded or
// not, and its path comes back with it.
func (s *Store) Run(ctx context.Context, name string, version int, exec Exec) (Trace, string, error) {
	h, err := s.Load(name, version)
	if err != nil {
		return Trace{}, "", err
	}
	trace, runErr := Run(ctx, h, exec)
	if trace.Id.Name == "" {
		// The run never started — a validation failure on a page that should
		// not have been savable. There is no evidence to keep.
		return trace, "", runErr
	}
	path, saveErr := s.SaveRun(trace)
	if runErr != nil {
		return trace, path, runErr
	}
	return trace, path, saveErr
}

// RunFile runs a harness page read straight off disk, without going through
// the registry. This is how a draft is tried before it is saved.
func RunFile(ctx context.Context, path string, exec Exec) (Trace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Trace{}, err
	}
	h, err := Decode(data)
	if err != nil {
		return Trace{}, fmt.Errorf("subharness: %s: %w", path, err)
	}
	return Run(ctx, h, exec)
}

// enters decides whether a node is reached. One live incoming edge is enough
// for everything except a join in `all` mode, which is a barrier by
// definition: it waits for all of its incoming edges, and a branch upstream
// that means one will never arrive is what skips it.
//
// `first` and `any` are the same thing to this walk, which runs one node at a
// time: the first lane to arrive is the only lane that has arrived. They part
// company in an executor that actually runs lanes concurrently, and the mode
// is carried through the trace so that executor can tell them apart.
func enters(p Program, node Node, live map[Edge]bool) bool {
	incoming := p.Predecessors(node.Id)
	if len(incoming) == 0 {
		return false
	}
	arrived := 0
	for _, from := range incoming {
		if live[Edge{from, node.Id}] {
			arrived++
		}
	}
	if node.Kind == KindParallelJoin && joinMode(node) == JoinAll {
		return arrived == len(incoming)
	}
	return arrived > 0
}

func joinMode(node Node) string {
	if mode := node.Fields.Get("mode"); mode != "" {
		return mode
	}
	return JoinAll
}

// liveEdges returns the taken edges in program order, which keeps a trace's
// dag comparable to the program it came from.
func liveEdges(p Program, live map[Edge]bool) []Edge {
	var taken []Edge
	for _, edge := range p.Edges {
		if live[edge] {
			taken = append(taken, edge)
		}
	}
	return taken
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func (t Trace) encode() ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(t); err != nil {
		return nil, fmt.Errorf("subharness: encode trace: %w", err)
	}
	return buffer.Bytes(), nil
}

func decodeTrace(data []byte) (Trace, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var t Trace
	if err := decoder.Decode(&t); err != nil {
		return Trace{}, fmt.Errorf("subharness: decode trace: %w", err)
	}
	return t, nil
}
