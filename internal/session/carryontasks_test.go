package session

// A TURN THAT ENDED WITH WORK OF ITS OWN STILL RUNNING IS WAITING, NOT STOPPED
// SHORT.
//
// THE MEASURED FAILURE these cases are written from: 2026-09-10, 16:19, a live
// drive on deepseek-v4.1-flash. The chat started two quick tasks in one batch
// and ended its turn saying they would land on their own. The first landed at
// 19:22, its note woke a turn, and the chat answered it — and then the carry-on
// road read that answer three times over, at 19:51, 20:05 and 20:11, each time
// told by the reader that the second task had not come back and each time
// answered by the model with "still running, no gap to fix". Three model calls
// and three `tasks` polls of the node that was about to report, which is the
// whole of [checkpointCarryOnCap] spent on a turn that was correctly waiting.
//
// The gate is turnhandoff.go's [Agent.turnIsWaitingOnItsOwnTasks] and the cases
// drive [Agent.Submit] and the real landing road, because what the failure cost
// was whole turns.

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fixture ─────────────────────────────────────────────────────────────

const (
	// pieceAsk is the person's own words, and it is what each piece is admitted
	// with — so the landing's reply tag carries it and the woken turn owes it
	// (wakecause.go). It is long enough not to be a trivial ask, which is what
	// [Agent.checkpoints] would otherwise decline on.
	pieceAsk = "walk the taxonomy across every package and write me the cross-package table of what each one owns"
	// piecesSaid is what the woken turn says when one piece has landed and
	// another has not: exactly the shape the drive produced.
	piecesSaid = "the first piece is in; the other one is still running and I'll write the table when it lands"
	// pieceGap is what the reader answers about that turn, truthfully — the
	// table really has not been written — and it is the answer this road must
	// stop buying while the work is out.
	pieceGap = "the second task never returned and the cross-package table has not been written"
)

// waitingOnPiecesSteps answers every ask this road makes: the sidecar's three,
// and everything else with one line and no tool call, which is a turn that has
// stopped. It counts the turns it answered so a case can say whether a turn was
// re-opened at all.
//
// There are no working rounds in it because none are needed: a WOKEN turn
// outranks the price gate ([Agent.checkpointReopen]), so the reader is armed on
// the first ending — which is what made the measured turn readable and is the
// exact condition this gate has to hold under.
func waitingOnPiecesSteps(said string, replies *atomic.Int64, remains func() string) []step {
	steps := make([]step, 60)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if answer, handled := checkpointSidecar(messages, remains); handled {
				return answer, nil
			}
			replies.Add(1)
			return textResponse(said), nil
		}
	}
	return steps
}

// livePiece admits one piece of this conversation's own work and leaves it
// running.
//
// IT GOES THROUGH THE ONE ADMISSION DOOR ([TaskGraph.admit]) rather than being
// appended to the graph, because the fact under test is stamped there:
// `admitBy` is whose request handed the work out (turnhandoff.go), and a node
// planted around that door would prove nothing about a node a conversation
// started. The stubbed runner never lands it, so it stays out for as long as
// the case needs.
func livePiece(t *testing.T, agent *Agent, title string, quick bool) *TaskNode {
	t.Helper()
	graph := agent.graph()
	id := graph.reserve()
	spec := taskSpec{
		title:       title,
		request:     pieceAsk,
		brief:       "walk " + title + " and say what it owns",
		deliverable: "one paragraph naming what it owns",
		acceptance:  "the paragraph names every type in it",
	}
	if quick {
		spec.quick = newQuickTaskSpec("walk "+title+" and say what it owns", nil, nil)
	}
	graph.admit(id, spec)
	node := graph.node(id)
	if node == nil {
		t.Fatalf("piece %d was not admitted", id)
	}
	return node
}

// land settles one piece the way its runner would, which is what wakes the
// conversation with the news.
func land(node *TaskNode, report string) {
	node.finish(report, nil, "", "")
	node.graph.complete(node, TaskDone)
}

// carryOnRow is the row the carry-on road wrote when it stood down, if it wrote
// one (checkpoint.go's [Agent.journalCarryOnAwaited]).
func carryOnRow(t *testing.T, path string) (journalCeiling, bool) {
	t.Helper()
	for _, entry := range journaledEntries(t, path, "ceiling") {
		if entry.Ceiling != nil && entry.Ceiling.Seam == checkpointSeamCarry {
			return *entry.Ceiling, true
		}
	}
	return journalCeiling{}, false
}

// ── the defect itself ───────────────────────────────────────────────────────

// A TURN WOKEN BY ONE PIECE LANDING IS NOT RE-OPENED WHILE ANOTHER IS STILL OUT.
//
// Nothing is read, nothing is carried on, and the turn ends where the model
// ended it — so there is no round in which a model with nothing to do polls the
// task that is about to report.
func TestATurnIsNotCarriedOnWhileItsOwnQuickTaskIsStillOut(t *testing.T) {
	var replies, remainsAsks atomic.Int64
	completer := &scriptedCompleter{steps: waitingOnPiecesSteps(piecesSaid, &replies, func() string {
		remainsAsks.Add(1)
		return pieceGap
	})}
	journalPath := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, completer, func(config *Config) {
		config.SessionFile = journalPath
	})
	stubbedGraph(agent, func(node *TaskNode) {})

	first := livePiece(t, agent, "the reader taxonomy", true)
	livePiece(t, agent, "the writer taxonomy", true)
	land(first, "the reader taxonomy owns four types and here they are")

	waitFor(t, "the landing to be answered", func() bool {
		return strings.Contains(transcriptText(agent), piecesSaid)
	})
	waitForQuiet(t, agent)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn waiting on its own quick task was read %d times for what remains", got)
	}
	if got := replies.Load(); got != 1 {
		t.Errorf("the woken turn made %d model calls, want the one that answered the landing", got)
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Errorf("a continuation was written into a turn waiting on a quick task it started:\n%s",
			transcriptText(agent))
	}
	// AND THE DECISION IS IN THE FILE. A road that ends a turn without reading
	// it and leaves no trace is how two completely different runs came to write
	// identical journals (#567).
	row, wrote := carryOnRow(t, journalPath)
	if !wrote {
		t.Fatalf("the carry-on road wrote no row for the ending it took:\n%s", readJournalText(t, journalPath))
	}
	if row.Decision != checkpointCeilingAwaiting {
		t.Errorf("the row decided %q, want %q", row.Decision, checkpointCeilingAwaiting)
	}
	if !strings.Contains(row.Reason, "awaiting task") {
		t.Errorf("the row does not name the piece it stood down for: %q", row.Reason)
	}
}

// AND AN ORDINARY TASK COUNTS EXACTLY AS A QUICK ONE DOES.
//
// The gate is about work this conversation started and is owed the ending of,
// and the kind of node it is has nothing to do with that. It is the same case
// with `quick` taken off the spec, because a gate that read one kind and not
// the other would leave half the defect standing.
func TestAnOrdinaryTaskStillOutStopsTheCarryOnLikeAQuickOne(t *testing.T) {
	var replies, remainsAsks atomic.Int64
	completer := &scriptedCompleter{steps: waitingOnPiecesSteps(piecesSaid, &replies, func() string {
		remainsAsks.Add(1)
		return pieceGap
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	first := livePiece(t, agent, "the reader taxonomy", false)
	second := livePiece(t, agent, "the writer taxonomy", false)
	if second.kind == TaskKindQuick {
		t.Fatal("the ordinary piece was admitted as a quick node")
	}
	land(first, "the reader taxonomy owns four types and here they are")

	waitFor(t, "the landing to be answered", func() bool {
		return strings.Contains(transcriptText(agent), piecesSaid)
	})
	waitForQuiet(t, agent)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn waiting on its own task was read %d times for what remains", got)
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Errorf("a continuation was written into a turn waiting on a task it started:\n%s",
			transcriptText(agent))
	}
}

// AND THE TURN IS CARRIED ON AGAIN THE MOMENT NOTHING OF ITS OWN IS OUT.
//
// This is the other half of the rule and the reason it is safe: the gate is a
// wait, not an exemption. With the last piece landed the reader is paid exactly
// as it was before any of this existed, and the gap it names re-opens the turn.
func TestTheWokenTurnIsCarriedOnOnceTheLastPieceHasLanded(t *testing.T) {
	const stopped = "the piece is in; I have not written the table yet"

	var replies, remainsAsks atomic.Int64
	completer := &scriptedCompleter{steps: waitingOnPiecesSteps(stopped, &replies, func() string {
		if remainsAsks.Add(1) == 1 {
			return pieceGap
		}
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	only := livePiece(t, agent, "the reader taxonomy", true)
	land(only, "the reader taxonomy owns four types and here they are")

	waitFor(t, "the turn to be re-opened on what the reader said is left", func() bool {
		return strings.Contains(transcriptText(agent), checkpointCarryOnLead+pieceGap)
	})
	waitForQuiet(t, agent)

	if got := remainsAsks.Load(); got < 1 {
		t.Fatalf("the ask was read %d times; want the reading that re-opened the turn", got)
	}
	if got := replies.Load(); got < 2 {
		t.Errorf("the woken turn made %d model calls, want the answer and the round the carry-on bought", got)
	}
}
