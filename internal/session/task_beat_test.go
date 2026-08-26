package session

// THE STORE RECORDS LIVENESS AT THE CADENCE OF THE WORK.
//
// tasks.json is written at admission and at landing, so between them a working
// node and a hung one are the same two bytes on disk. These tests read the pulse
// FROM INSIDE THE RUN — a scripted step is a provider call, so a step that opens
// the sidecar is reading what the node is saying about itself at that exact
// instant — which is the only place the claim "at the cadence of the work" can
// actually be checked.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// beatWatch collects what the pulse said at each moment somebody looked.
type beatWatch struct {
	mu   sync.Mutex
	rows []taskBeatRow
	// named is what the CHECKPOINT said about the pulse while the node ran. The
	// row names the sidecar, so a reader that already has tasks.json open never
	// has to guess at a path (task_store.go's taskRecord.Beat).
	named string
}

// look opens the node's sidecar and keeps what it found, with whatever the
// checkpoint was saying about it at the same moment. A reading that finds no file
// is kept as a zero row, so a test can tell "the pulse said nothing" from "nobody
// looked".
func (w *beatWatch) look(agent *Agent, id uint64) {
	store := agent.graph().store
	row, _ := readTaskBeat(store.beatPath(id))
	named := ""
	if document, ok := loadTaskCheckpoint(store.path); ok {
		for _, record := range document.Nodes {
			if record.ID == id {
				named = record.Beat
			}
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rows = append(w.rows, row)
	if named != "" {
		w.named = named
	}
}

func (w *beatWatch) seen() []taskBeatRow {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]taskBeatRow, len(w.rows))
	copy(out, w.rows)
	return out
}

func (w *beatWatch) namedInTheCheckpoint() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.named
}

// beatSession is a session with a journal, which is what gives it a store and
// therefore a pulse at all. Without one the checkpoint has no path, the store is
// nil, and every node runs exactly as it did before this existed.
func beatSession(t *testing.T, repo string, completer Completer, mutate ...func(*Config)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.SessionFile = filepath.Join(t.TempDir(), placeTranscript)
		for _, extra := range mutate {
			extra(config)
		}
	})
	return agent
}

// A RUNNING NODE'S PULSE MOVES WITH ITS CALLS.
//
// The count and the two edges advance at every provider call boundary, which is
// the cadence of the work rather than a clock somebody chose — so an outside
// reader comparing two readings can tell a node that is calling models from one
// that has been sitting inside the same request for four minutes.
func TestARunningNodesHeartbeatAdvancesAcrossItsCalls(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	var agent *Agent
	watch := &beatWatch{}
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go with a greeting"),
			finalText("handed off"),
		},
		child: nodeLane(6, func(_, wrote bool) *ai.Response {
			watch.look(agent, 1)
			if !wrote {
				return writeResponse("call-src", "greet.go",
					"package greet\n\nfunc Greet() string { return \"hi\" }\n")
			}
			return pricedResponse("Wrote greet.go with the greeting.", 0.01)
		}),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — git status --porcelain shows no greet.go"),
		},
	}
	agent = beatSession(t, repo, completer)
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)

	rows := watch.seen()
	if len(rows) < 2 {
		t.Fatalf("the node's pulse was read %d times; the test needs at least two calls to compare", len(rows))
	}
	for index, row := range rows {
		if row.UpdatedAt.IsZero() {
			t.Fatalf("reading %d found no pulse at all while the node was working", index)
		}
		if row.Node != 1 {
			t.Fatalf("reading %d is about node %d, want the node that is running", index, row.Node)
		}
		if !row.working() {
			t.Errorf("reading %d, taken INSIDE a request, does not show one in flight: %+v", index, row)
		}
	}
	first, last := rows[0], rows[len(rows)-1]
	if last.Requests <= first.Requests {
		t.Errorf("requests went %d → %d across %d readings: the pulse is not moving with the work",
			first.Requests, last.Requests, len(rows))
	}
	if !last.RequestStarted.After(first.RequestStarted) {
		t.Errorf("the last request started at %v and the first at %v: the pulse is stale",
			last.RequestStarted, first.RequestStarted)
	}
	if !last.Started.Equal(first.Started) {
		t.Errorf("the node's own start moved from %v to %v: that is the one fixed point here",
			first.Started, last.Started)
	}

	// AND A READER HOLDING THE CHECKPOINT FOUND THE PULSE WITHOUT GUESSING.
	if named := watch.namedInTheCheckpoint(); named != graph.store.beatPath(1) {
		t.Errorf("tasks.json named the pulse as %q, want %q", named, graph.store.beatPath(1))
	}
	// AND IT DOES NOT OUTLIVE THE RUN. A landed node's liveness is its final
	// state, and a file left behind would invite a reader to conclude from it. The
	// wait is for the run's own goroutine, which takes the pulse away on its way
	// out — a node is settled a moment before the goroutine that ran it returns.
	waitFor(t, "the node's pulse to be taken away", func() bool {
		_, err := os.Stat(graph.store.beatPath(1))
		return os.IsNotExist(err)
	})
	if document, ok := loadTaskCheckpoint(graph.store.path); ok {
		for _, record := range document.Nodes {
			if record.ID == 1 && strings.TrimSpace(record.Beat) != "" {
				t.Errorf("a landed row still points at a pulse: %q", record.Beat)
			}
		}
	}
}

// AND IT SAYS WHICH OF THE NODE'S THREE LIVES THIS IS.
//
// A node under check and a node being repaired are both RUNNING — nothing landed,
// nothing was undone — so an outside reader watching the state alone sees one
// unbroken stretch across a worker, a check and a repair round. The phase is what
// tells them apart, and it goes back to the work's own word when each of them
// ends.
func TestTheHeartbeatSaysWhichPhaseTheNodeIsIn(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	var agent *Agent
	working, checking, repairing := &beatWatch{}, &beatWatch{}, &beatWatch{}
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go and a test for it, checked with `go test ./...`"),
			finalText("handed off"),
		},
		child: nodeLane(8, func(repairs, wrote bool) *ai.Response {
			if repairs {
				repairing.look(agent, 1)
			} else {
				working.look(agent, 1)
			}
			switch {
			case repairs && !wrote:
				return writeResponse("call-test", "greet_test.go",
					"package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() != \"hi\" {\n\t\tt.Fatal(\"no greeting\")\n\t}\n}\n")
			case repairs:
				return pricedResponse("Added greet_test.go, which covers the greeting.", 0.02)
			case !wrote:
				return writeResponse("call-src", "greet.go",
					"package greet\n\nfunc Greet() string { return \"hi\" }\n")
			default:
				return pricedResponse("Wrote greet.go with the greeting.", 0.01)
			}
		}),
		audit: []step{
			checkingStep(&agent, checking, bashCall("call-verify", "go test ./...")),
			checkingStep(&agent, checking, verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok",
				"REFUTED — go test ./... reports no test files: the acceptance asks for a test and there is none")),
			checkingStep(&agent, checking, bashCall("call-verify-again", "go test ./...")),
			checkingStep(&agent, checking, verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok · the greeting test runs",
				"REFUTED — go test ./... still reports no test files")),
		},
	}
	agent = beatSession(t, repo, completer, func(config *Config) { config.TaskRepairRounds = 1 })
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)

	for _, lane := range []struct {
		what  string
		watch *beatWatch
		phase string
	}{
		{"the node's own worker", working, taskBeatWorking},
		{"the check", checking, taskBeatChecking},
		{"the repair round", repairing, taskBeatRepairing},
	} {
		rows := lane.watch.seen()
		if len(rows) == 0 {
			t.Fatalf("%s never ran, so the phase it should show was never observed", lane.what)
		}
		for index, row := range rows {
			if row.Phase != lane.phase {
				t.Errorf("%s, reading %d: the pulse said %q, want %q", lane.what, index, row.Phase, lane.phase)
			}
		}
	}
}

// checkingStep wraps one of the checker's scripted answers so that the pulse is
// read at the moment the check is speaking, which is the only moment its phase is
// in force.
func checkingStep(agent **Agent, watch *beatWatch, inner step) step {
	return func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		watch.look(*agent, 1)
		return inner(ctx, messages)
	}
}
