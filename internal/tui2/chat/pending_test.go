package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE SKELETON IS BORN FROM THE COMMAND ROW, NOT FROM A MESSAGE (user-reported
// 2026-08-11: "there is a gap between when it says it's going to start and
// then I see the card"). The command is journaled the instant the head hands
// work over; compile and plan run for seconds to minutes after that, and the
// receipt — the first MESSAGE this task ever posts — arrives only when they
// are all done. A conversation that waits for the receipt renders that whole
// interval as silence, so the poll reads the session's pending splices and
// draws the card's skeleton from them directly.

// pendingBackend is a boardBackend that also answers [commandReader], which is
// the optional read the skeleton is drawn from.
type pendingBackend struct {
	boardBackend
	pending []store.Command
}

func (b *pendingBackend) PendingCommands(int) ([]store.Command, error) {
	return append([]store.Command(nil), b.pending...), nil
}

func pendingApp(t *testing.T) (*App, *pendingBackend) {
	t.Helper()
	backend := &pendingBackend{boardBackend: *board()}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}

func TestAPendingSpliceDrawsTheSkeletonBeforeAnyMessage(t *testing.T) {
	app, backend := pendingApp(t)
	before := app.transcript.Len()
	backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Build the panel site from scratch"}}
	backend.journal++
	poll(t, app)

	if got := app.transcript.Len(); got != before+1 {
		t.Fatalf("the pending splice drew %d blocks, want one skeleton", got-before)
	}
	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok || !block.provisional || block.job != "task-900" {
		t.Fatalf("the skeleton is not a provisional card for its task: %+v", block)
	}
	rows := blockRows(t, app, app.transcript.Len()-1, 100)
	for _, want := range []string{"Build the panel site", creatingWord} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the skeleton is missing %q:\n%s", want, rows)
		}
	}

	// The same pending row on the next poll is the same skeleton, not a twin.
	backend.journal++
	poll(t, app)
	if got := app.transcript.Len(); got != before+1 {
		t.Fatalf("a second poll minted a twin: %d blocks", got-before)
	}
}

func TestTheReceiptCoalescesIntoTheSkeleton(t *testing.T) {
	app, backend := pendingApp(t)
	backend.pending = []store.Command{{Seq: 900, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Build the panel site from scratch"}}
	backend.journal++
	poll(t, app)
	at := app.transcript.Len() - 1

	// Compile and plan land: the task exists, the command settles, and the
	// receipt posts — all in one journal move, exactly as the resident does it.
	backend.pending = nil
	backend.nodes = append(backend.nodes, store.Node{ID: "task-900",
		Title: "panel site", Status: store.Running, CreatedSeq: 20})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 900, Body: "Here's my reading: a panel site built from scratch."})
	poll(t, app)

	if got := app.transcript.Len(); got != at+1 {
		t.Fatalf("the receipt landed beside the skeleton: %d blocks after %d", got, at+1)
	}
	block, ok := app.transcript.Block(at).(*messageBlock)
	if !ok || block.provisional {
		t.Fatalf("the receipt did not take the skeleton's place: %+v", block)
	}
	rows := blockRows(t, app, at, 100)
	if !strings.Contains(rows, "panel site") {
		t.Fatalf("the named card is missing its name:\n%s", rows)
	}
}

// A SPLICE CAN MINT EITHER SPELLING. A request the craft shelf answers
// decisively compiles to `craft-<seq>`, not `task-<seq>` — and a skeleton that
// only ever looked for the first buried a LIVE craft job as "didn't start"
// while its workers ran (user-reported, 2026-08-11).
func TestACraftSpliceNamesTheSkeletonInsteadOfBuryingIt(t *testing.T) {
	app, backend := pendingApp(t)
	backend.pending = []store.Command{{Seq: 902, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Ten more chapters, same audience"}}
	backend.journal++
	poll(t, app)
	at := app.transcript.Len() - 1

	// The craft shelf answered: the command settles and the job exists under
	// the OTHER prefix.
	backend.pending = nil
	backend.nodes = append(backend.nodes, store.Node{ID: "craft-902",
		Title: "smut sequel", Status: store.Running, CreatedSeq: 30})
	backend.journal++
	poll(t, app)

	block, ok := app.transcript.Block(at).(*messageBlock)
	if !ok {
		t.Fatalf("the skeleton is gone: %T", app.transcript.Block(at))
	}
	if block.phase == unstartedWord {
		t.Fatalf("a live craft job was buried as %q", unstartedWord)
	}
	if block.job != "craft-902" {
		t.Fatalf("the skeleton did not adopt the craft spelling: %q", block.job)
	}

	// And the receipt coalesces into it rather than standing beside it.
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 902, Body: "Here's my reading: ten more chapters."})
	poll(t, app)
	if got := app.transcript.Len(); got != at+1 {
		t.Fatalf("the receipt landed beside the adopted skeleton: %d blocks after %d", got, at+1)
	}
}

func TestASettledCommandWithNoTaskBuriesItsSkeleton(t *testing.T) {
	app, backend := pendingApp(t)
	backend.pending = []store.Command{{Seq: 901, SessionID: testSession,
		Kind: store.CommandSplice, Instruction: "Do the impossible"}}
	backend.journal++
	poll(t, app)
	at := app.transcript.Len() - 1

	// The command is rejected: it leaves pending, no task is ever minted, and
	// the refusal speaks for itself as an agent line.
	backend.pending = nil
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		CommandSeq: 901, Body: "I couldn't apply that request: no."})
	poll(t, app)

	block, ok := app.transcript.Block(at).(*messageBlock)
	if !ok {
		t.Fatalf("the skeleton is gone: %T", app.transcript.Block(at))
	}
	if block.provisional || block.breathing || block.working {
		t.Fatalf("a buried skeleton is still breathing: %+v", block)
	}
	if block.phase != unstartedWord {
		t.Fatalf("the skeleton is not wearing its ending: %q", block.phase)
	}
}
