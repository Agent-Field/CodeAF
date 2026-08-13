package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// The skeleton's stage line.
//
// The interval between the head handing work over and the card arriving is
// seconds to minutes, and it used to render as one sentence — `creating task…` —
// for the whole of it. The planner narrates itself into the journal the whole
// time; the skeleton now reads it.

// stagedBackend is a pendingBackend whose node trail carries the planner's own
// progress rows, exactly as cmd/aforge's poster writes them: node-anchored,
// command-keyed, structured beside a readable body.
type stagedBackend struct{ pendingBackend }

// stage journals one phase under the task the pending command will mint.
func (b *stagedBackend) stage(commandSeq int64, phase string, done, total int) {
	if b.node == nil {
		b.node = map[string][]store.Message{}
	}
	node := commandTaskID(commandSeq)
	b.seq++
	b.journal++
	line := phase
	if total > 0 {
		line += " · " + itoa(done) + " of " + itoa(total)
	}
	b.node[node] = append(b.node[node], store.Message{
		Seq: b.seq, Role: store.RoleSystem, NodeID: node, CommandSeq: commandSeq,
		Body:     line,
		Progress: &store.MessageProgress{Phase: phase, Done: done, Total: total},
	})
}

func stagedApp(t *testing.T) (*App, *stagedBackend) {
	t.Helper()
	backend := &stagedBackend{pendingBackend{boardBackend: *board()}}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}

// THE SKELETON SAYS WHERE THE LONG THING HAS GOT TO, and it replaces the bare
// pulse rather than standing beside it.
func TestTheSkeletonNarratesTheStageItIsAt(t *testing.T) {
	app, backend := stagedApp(t)
	backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Build the panel site from scratch"}}
	backend.journal++
	poll(t, app)

	at := app.transcript.Len() - 1
	if rows := ansi.Strip(blockRows(t, app, at, 100)); !strings.Contains(rows, creatingWord) {
		t.Fatalf("a command the planner has said nothing about lost its pulse:\n%s", rows)
	}

	backend.stage(900, "reading the ask", 0, 0)
	poll(t, app)
	rows := ansi.Strip(blockRows(t, app, at, 100))
	if !strings.Contains(rows, "reading the ask") {
		t.Fatalf("the skeleton is not narrating its stage:\n%s", rows)
	}
	if strings.Contains(rows, creatingWord) {
		t.Fatalf("the stage line stands beside the pulse instead of replacing it:\n%s", rows)
	}

	// A counted phase carries its count, in the product's own separator.
	backend.stage(900, "setting working standards", 2, 3)
	poll(t, app)
	rows = ansi.Strip(blockRows(t, app, at, 100))
	if !strings.Contains(rows, "setting working standards · 2 of 3") {
		t.Fatalf("the counted stage did not reach the card:\n%s", rows)
	}
	if strings.Contains(rows, "reading the ask") {
		t.Fatalf("the card is showing two stages at once:\n%s", rows)
	}
	// The title is still the head's reading. A stage is a status, not a name.
	if !strings.Contains(rows, "Build the panel site") {
		t.Fatalf("the stage line took the card's title:\n%s", rows)
	}
	// And the breathe stays: the words say where it is, the dot says it is alive.
	block, _ := app.transcript.Block(at).(*messageBlock)
	if block == nil || !block.breathing {
		t.Fatalf("the card stopped breathing while it was still working: %+v", block)
	}
}

// WHEN THE COMMAND RESOLVES THE STAGE LINE GOES. The card becomes the card, and
// its live half is derived from the graph like every other cell on it.
func TestTheStageLineLeavesWhenTheCardIsNamed(t *testing.T) {
	app, backend := stagedApp(t)
	backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Build the panel site from scratch"}}
	backend.journal++
	backend.stage(900, "setting working standards", 2, 3)
	poll(t, app)
	at := app.transcript.Len() - 1
	if rows := ansi.Strip(blockRows(t, app, at, 100)); !strings.Contains(rows, "2 of 3") {
		t.Fatalf("the in-flight card is not narrating:\n%s", rows)
	}

	backend.pending = nil
	backend.nodes = append(backend.nodes, store.Node{ID: "task-900",
		Title: "panel site", Status: store.Running, CreatedSeq: 20})
	backend.journal++
	poll(t, app)

	rows := ansi.Strip(blockRows(t, app, at, 100))
	if strings.Contains(rows, "setting working standards") {
		t.Fatalf("the stage line outlived the skeleton it belonged to:\n%s", rows)
	}
	if !strings.Contains(rows, "panel site") {
		t.Fatalf("the named card never arrived:\n%s", rows)
	}
}

// A STAGE BELONGS TO ITS COMMAND. A node can carry progress from a later
// replan under a different command, and a skeleton showing that would be
// narrating somebody else's work.
func TestAStageFromAnotherCommandIsNotThisCardsStage(t *testing.T) {
	app, backend := stagedApp(t)
	backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Build the panel site"}}
	backend.journal++
	// The trail under task-900 also carries a row keyed to a different command.
	backend.stage(900, "reading the ask", 0, 0)
	if b := backend.node[commandTaskID(900)]; len(b) > 0 {
		b[len(b)-1].CommandSeq = 999
		b[len(b)-1].Progress.Phase = "somebody else's phase"
	}
	poll(t, app)

	rows := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 100))
	if strings.Contains(rows, "somebody else") {
		t.Fatalf("a stage from another command reached this card:\n%s", rows)
	}
	if !strings.Contains(rows, creatingWord) {
		t.Fatalf("the card lost its honest pulse instead:\n%s", rows)
	}
}

// A BACKEND THAT CANNOT ANSWER MAKES NO CLAIM. There is no invented stage; the
// skeleton keeps the pulse it has always had (10.2.8).
func TestASurfaceWithNoNodeTrailKeepsThePulse(t *testing.T) {
	app, backend := pendingApp(t)
	backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Build the panel site"}}
	backend.journal++
	poll(t, app)
	rows := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 100))
	if !strings.Contains(rows, creatingWord) {
		t.Fatalf("a surface with nothing to read invented a stage:\n%s", rows)
	}
}

// The line itself: the planner's words, and the count when there is one.
func TestAStageLineIsThePhaseAndItsCount(t *testing.T) {
	cases := []struct {
		progress store.MessageProgress
		want     string
	}{
		{store.MessageProgress{Phase: "reading the ask"}, "reading the ask"},
		{store.MessageProgress{Phase: "setting working standards", Done: 2, Total: 3},
			"setting working standards · 2 of 3"},
		// A generated title belongs to the subtree twigs, not to this row.
		{store.MessageProgress{Phase: "designing the approach", Done: 1, Total: 4,
			Latest: "Outline the fix"}, "designing the approach · 1 of 4"},
		{store.MessageProgress{}, ""},
	}
	for _, testCase := range cases {
		if got := stageLine(testCase.progress); got != testCase.want {
			t.Errorf("stageLine(%+v) = %q, want %q", testCase.progress, got, testCase.want)
		}
	}
}
