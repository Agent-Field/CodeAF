package session

import (
	"bufio"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// ── A REQUEST MADE ON A NODE'S BEHALF LEAVES THE TRAIL ITS WORKER'S DO ───────
//
// THE MEASURED FAILURE. Task 5 of conversation de9eabcb10cc1e45, 2026-09-11:
// the reading that sized the work thought for 219 seconds, and the node's row
// said the model's id, its room said nothing had arrived, its pulse said it had
// made no requests, and its journal said nothing at all until the bill landed
// (task_calltrail.go).

// sizingStream is a reading as a real endpoint sends it: a run of thought, then
// the answer — two parts, which is what the division below asks for — then the
// usage frame.
var sizingStream = []string{
	`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"Two parts, "}}]}`,
	`{"choices":[{"index":0,"delta":{"reasoning":"one per directory, "}}]}`,
	`{"choices":[{"index":0,"delta":{"reasoning":"and neither touches the other."}}]}`,
	`{"choices":[{"index":0,"delta":{"content":"{\"parts\":[{\"title\":\"the adapters\",\"summary\":\"s\",\"brief\":\"b\",\"acceptance\":\"a\"},"}}]}`,
	`{"choices":[{"index":0,"delta":{"content":"{\"title\":\"the tests\",\"summary\":\"s\",\"brief\":\"b\",\"acceptance\":\"a\"}]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":900,"completion_tokens":60}}`,
}

// flightsIn is every flight line in one journal, in the order written.
func flightsIn(t *testing.T, path string) []journalFlight {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the node's journal: %v", err)
	}
	defer file.Close()
	var flights []journalFlight
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for scanner.Scan() {
		var entry sessionEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Type == "flight" && entry.Flight != nil {
			flights = append(flights, *entry.Flight)
		}
	}
	return flights
}

// sizingMoves is every phase move about one node until it is back at work.
func sizingMoves(t *testing.T, updates <-chan Event, id uint64) []TaskPhaseNotice {
	t.Helper()
	var seen []TaskPhaseNotice
	deadline := time.After(30 * time.Second)
	for {
		select {
		case event := <-updates:
			if event.Kind != EventTaskPhase || event.TaskPhase == nil || event.TaskPhase.ID != id {
				continue
			}
			seen = append(seen, *event.TaskPhase)
			if event.TaskPhase.Phase == TaskPhaseWorking {
				return seen
			}
		case <-deadline:
			t.Fatalf("the node never went back to work; moves so far: %+v", seen)
			return nil
		}
	}
}

// THE WHOLE TRAIL, THROUGH THE REAL TRANSPORT. The reviewer here is the shipped
// provider client reading a scripted stream, so every moment below came from the
// one place internal/provider counts a request's life — not from a fake of it.
func TestASizingRequestLeavesItsTrailOnTheJournalThePulseAndTheRow(t *testing.T) {
	server := newReasoningServer(t, sizingStream)
	client, err := provider.NewClient(provider.Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	nest := newDivideNestOn(t, wideBrief, 0, client, nil)
	beatAt := filepath.Join(t.TempDir(), "1"+taskBeatSuffix)
	nest.graph.mu.Lock()
	nest.parent.beat = newTaskBeat(beatAt, nest.parent.id, "the whole job", time.Now())
	nest.graph.mu.Unlock()
	updates := nest.session.TaskUpdates()

	if answer := nest.divide(t, divideArgs(wideEvidence, 2)); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the division the reading admitted", answer)
	}

	// THE ROW: the request rides the phase, under the ladder's own sentence, and
	// says it was thinking before it was writing.
	moves := sizingMoves(t, updates, nest.parent.id)
	var thinking, writing *TaskPhaseNotice
	for i := range moves {
		move := &moves[i]
		if move.Call == nil {
			continue
		}
		if move.Phase != TaskPhaseSizing || move.Text != "asking test/model" {
			t.Fatalf("a live request rode %q under %q, want the sizing phase and its sentence", move.Phase, move.Text)
		}
		if move.Call.Model != "test/model" || move.Call.Started.IsZero() {
			t.Fatalf("the live request names %q at %v, want the model and when it went out", move.Call.Model, move.Call.Started)
		}
		switch move.Call.Phase {
		case provider.CallThinking:
			if thinking == nil {
				thinking = move
			}
		case provider.CallWriting:
			writing = move
		case provider.CallEnded:
			t.Fatal("an ended request was drawn as live")
		}
	}
	if thinking == nil || thinking.Call.Reasoning == 0 || thinking.Call.FirstToken.IsZero() {
		t.Fatalf("the row never drew the model thinking, with its first token and its count: %+v", moves)
	}
	if writing == nil || writing.Call.Tokens == 0 || writing.Call.Reasoning < thinking.Call.Reasoning {
		t.Fatalf("the row never drew the answer arriving on top of the thought: %+v", moves)
	}
	// AND THE REQUEST IS OFF THE ROW BEFORE THE NODE LEAVES THE PHASE, so a
	// surface copying each notice whole never draws a request under `working`.
	if last := moves[len(moves)-2]; last.Call != nil {
		t.Fatalf("the last sizing notice still carried a live request: %+v", last)
	}
	if back := moves[len(moves)-1]; back.Call != nil || back.Text != "" {
		t.Fatalf("the node went back to work carrying %+v", back)
	}

	// THE JOURNAL: both ends, the second carrying the shape of the wait.
	flights := flightsIn(t, nest.journal)
	if len(flights) != 2 {
		t.Fatalf("the node's journal holds %d flight lines, want the request's two ends: %+v", len(flights), flights)
	}
	out, back := flights[0], flights[1]
	if out.Phase != string(provider.CallStarted) || out.Role != string(roles.RoleDivision) || out.Model != "test/model" {
		t.Fatalf("the request went out as %+v, want the division's reading of test/model", out)
	}
	if back.Phase != string(provider.CallEnded) || back.End != string(provider.CallEndAnswered) {
		t.Fatalf("the request came back as %+v, want it answered", back)
	}
	if back.Reasoning == 0 || back.Output == 0 {
		t.Fatalf("the request's end carries %d thought and %d answer, want both counted", back.Reasoning, back.Output)
	}

	// THE PULSE: one request, out and back.
	row, ok := readTaskBeat(beatAt)
	if !ok {
		t.Fatal("the node's pulse was never written")
	}
	if row.Requests != 1 || row.working() {
		t.Fatalf("the pulse says %d requests, in flight %v; want the one reading, answered", row.Requests, row.working())
	}
}

// trailFor is a trail over one node of a divide nest, with nothing behind it
// but the moments a test hands it.
func trailFor(t *testing.T) (*divideNest, *callTrail, <-chan Event) {
	t.Helper()
	nest := newDivideNest(t, wideBrief, 0)
	updates := nest.session.TaskUpdates()
	trail := nest.node.trailCalls(nest.parent, TaskPhaseNotice{Phase: TaskPhaseRepairing, Round: 1, Rounds: 2}, roles.RoleDivision)
	return nest, trail, updates
}

// callsAbout is every phase move a finished trail sent about one node. The
// trail has joined its goroutine by the time [callTrail.end] returns, so a
// marker sent after it arrives after everything the trail said — the lane keeps
// order — and reading up to it is reading all of it, with no clock asked to
// guess when the silence began.
func callsAbout(t *testing.T, nest *divideNest, updates <-chan Event) []TaskPhaseNotice {
	t.Helper()
	nest.parent.phaseTeller(nest.node).emitTaskPhase(TaskPhaseNotice{ID: nest.parent.id, Phase: TaskPhaseWorking})
	moves := sizingMoves(t, updates, nest.parent.id)
	return moves[:len(moves)-1]
}

// A RESCUE IS NOT A SECOND ROW, A REQUEST STILL OUT AT THE END IS WRITTEN DOWN
// AS LEFT, AND NOTHING IS SAID AFTER THE END.
//
// These are the three shapes the wire cannot stage on demand: a hedge arm racing
// beside a slow request, the loser whose ending has not arrived when the reading
// is over, and the provider reporting on a request the reading already left.
func TestATrailDrawsTheArmNearestAnAnswerAndLeavesNoRequestOpen(t *testing.T) {
	nest, trail, updates := trailFor(t)
	began := time.Now()
	trail.say("asking a/slow")
	trail.heard(provider.CallProgress{Model: "a/slow", Attempt: 0, Started: began, Phase: provider.CallStarted})
	trail.heard(provider.CallProgress{Model: "a/slow", Attempt: 0, Started: began, FirstToken: began, Reasoning: 5, Phase: provider.CallThinking})
	trail.heard(provider.CallProgress{Model: "a/slow", Attempt: 1, Started: began, Phase: provider.CallStarted})
	trail.heard(provider.CallProgress{Model: "a/slow", Attempt: 1, Started: began, FirstToken: began, Served: "fast", Tokens: 40, Phase: provider.CallWriting})
	trail.end()
	// The provider speaks after the reading is over — the loser's own ending
	// arriving late — and it is dropped at the door.
	trail.heard(provider.CallProgress{Model: "a/slow", Attempt: 0, Started: began, Phase: provider.CallEnded, End: provider.CallEndCancelled})

	moves := callsAbout(t, nest, updates)
	if len(moves) == 0 {
		t.Fatal("the trail said nothing")
	}
	var nearest *TaskCall
	for _, move := range moves {
		if move.Phase != TaskPhaseRepairing || move.Round != 1 || move.Rounds != 2 {
			t.Fatalf("a notice lost the phase it was built on: %+v", move)
		}
		if move.Text != "asking a/slow" {
			t.Fatalf("a notice dropped the sentence under the phase: %+v", move)
		}
		if move.Call != nil {
			nearest = move.Call
		}
	}
	if nearest == nil || nearest.Served != "fast" || nearest.Tokens != 40 {
		t.Fatalf("the row drew %+v, want the rescue that had written the most", nearest)
	}
	if last := moves[len(moves)-1]; last.Call != nil {
		t.Fatalf("the trail ended with a live request still on the row: %+v", last.Call)
	}

	flights := flightsIn(t, nest.journal)
	ends := map[int]journalFlight{}
	starts := 0
	for _, flight := range flights {
		switch flight.Phase {
		case string(provider.CallStarted):
			starts++
		case string(provider.CallEnded):
			ends[flight.Attempt] = flight
		}
	}
	if starts != 2 || len(ends) != 2 {
		t.Fatalf("two requests went out and the journal holds %d starts and %d ends: %+v", starts, len(ends), flights)
	}
	for attempt, end := range ends {
		if end.End != string(provider.CallEndCancelled) {
			t.Fatalf("arm %d was left by the reading and is written down as %q", attempt, end.End)
		}
	}
}

// THE WATCHER DOES NO WORK. It is called from the read loop between two deltas
// of somebody's answer (internal/provider's callprogress.go), so it must return
// while the journal it will eventually write to is held by somebody else.
func TestATrailsWatcherReturnsWhileTheJournalIsHeld(t *testing.T) {
	nest, trail, _ := trailFor(t)
	file := nest.node.file
	file.mu.Lock()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		began := time.Now()
		trail.heard(provider.CallProgress{Model: "a/model", Started: began, Phase: provider.CallStarted})
		for tokens := 1; tokens <= 1000; tokens++ {
			trail.heard(provider.CallProgress{Model: "a/model", Started: began, FirstToken: began, Reasoning: tokens, Phase: provider.CallThinking})
		}
		trail.heard(provider.CallProgress{Model: "a/model", Started: began, Reasoning: 1000, Phase: provider.CallEnded, End: provider.CallEndAnswered})
	}()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		file.mu.Unlock()
		t.Fatal("the watcher waited on the journal")
	}
	file.mu.Unlock()
	trail.end()
	if flights := flightsIn(t, nest.journal); len(flights) != 2 {
		t.Fatalf("the journal holds %d flight lines once it was free, want the request's two ends", len(flights))
	}
}

// ONE DOOR. A request made on a node's behalf is watched through the node's
// call trail and nowhere else in this package, so the pulse, the journal and the
// row can never be three mechanisms that each learned a different subset of the
// requests.
func TestOnlyTheCallTrailWatchesARequestRun(t *testing.T) {
	set := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") || name == "task_calltrail.go" {
			continue
		}
		file, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "WithCallProgress" {
				return true
			}
			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "provider" {
				t.Errorf("%s watches a request run itself; attach the node's call trail instead (task_calltrail.go)", set.Position(selector.Pos()))
			}
			return true
		})
	}
}
