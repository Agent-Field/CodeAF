package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// The seam this file locks is the one every money cell on the composer's meta
// strip reads through, and the property it has to keep is not arithmetic: it is
// that "nothing has been billed" and "this cost nothing" stay two different
// answers all the way to the renderer.

func roomStore(t *testing.T, name string) *Store {
	t.Helper()
	return openTestStore(t, filepath.Join(t.TempDir(), name+".db"))
}

func say(t *testing.T, graph *Store, sessionID string, role Role, body string) Message {
	t.Helper()
	message, err := graph.PostMessage(Message{SessionID: sessionID, Role: role, Body: body})
	if err != nil {
		t.Fatalf("post %s message: %v", role, err)
	}
	return message
}

func bill(t *testing.T, graph *Store, usage NodeUsage) {
	t.Helper()
	if err := graph.RecordUsage(usage); err != nil {
		t.Fatalf("record usage for %s: %v", usage.NodeID, err)
	}
}

func TestARoomNobodyHasSpokenInHasNoSpendRatherThanZeroSpend(t *testing.T) {
	graph := roomStore(t, "silent")

	if _, found, err := graph.SessionSpend("never-opened"); err != nil || found {
		t.Fatalf("SessionSpend of an unspoken room = found %v, err %v; want absent", found, err)
	}
	if _, found, err := graph.TurnSpend("never-opened"); err != nil || found {
		t.Fatalf("TurnSpend of an unspoken room = found %v, err %v; want absent", found, err)
	}

	// A room that has been spoken in but has billed nothing is a different
	// fact: the window exists, and it is empty. The renderer draws — for both,
	// but only one of them can ever become a number.
	say(t, graph, "room", RoleUser, "hello")
	spend, found, err := graph.TurnSpend("room")
	if err != nil || !found {
		t.Fatalf("TurnSpend after a user turn = found %v, err %v; want present", found, err)
	}
	if spend.Recorded() {
		t.Fatalf("an unbilled turn reports Recorded; want absence, got %+v", spend)
	}

	// And a run that genuinely cost nothing is neither of those.
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 12, CompletionTokens: 3})
	spend, _, err = graph.TurnSpend("room")
	if err != nil {
		t.Fatal(err)
	}
	if !spend.Recorded() || spend.Cost() != 0 {
		t.Fatalf("a free run = %+v; want Recorded with zero cost", spend)
	}
}

func TestRoomSpendSeparatesWorkItCommissionedFromConversationItCannotClaim(t *testing.T) {
	graph := roomStore(t, "split")
	say(t, graph, "room", RoleUser, "build the thing")

	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "the thing", Stage: 2},
		{ID: "leaf", Parent: "job", Brief: "a part of it", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "build the thing"}); err != nil {
		t.Fatal(err)
	}
	// The head's own calls: routing, compiling, answering. All spine.
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 400, CompletionTokens: 20, Cost: 0.01})
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 9000, CompletionTokens: 300, Cost: 0.05})
	// The work it commissioned: attributed by node, exactly.
	bill(t, graph, NodeUsage{NodeID: "leaf", PromptTokens: 50000, CompletionTokens: 900, Cost: 0.40})

	spend, found, err := graph.TurnSpend("room")
	if err != nil || !found {
		t.Fatalf("TurnSpend = found %v, err %v", found, err)
	}
	if spend.Work.Runs != 1 || spend.Work.Cost != 0.40 {
		t.Fatalf("room work = %+v; want the one leaf at $0.40", spend.Work)
	}
	if spend.Spine.Runs != 2 {
		t.Fatalf("conversation runs = %d; want the two head calls", spend.Spine.Runs)
	}
	if got := spend.Cost(); got < 0.4599 || got > 0.4601 {
		t.Fatalf("window cost = %v; want 0.46", got)
	}
	if spend.Shared {
		t.Fatalf("one room talking reports Shared; want the figure claimable")
	}
	// The head's window occupancy is the largest prompt the conversation sent,
	// never the sum of them and never the leaf's, whose row is a whole tool
	// loop added up.
	if spend.SpinePromptHighWater != 9000 {
		t.Fatalf("SpinePromptHighWater = %d; want the answering call's 9000", spend.SpinePromptHighWater)
	}
}

func TestATurnCostsWhatWasBilledAfterTheMessageThatAskedForIt(t *testing.T) {
	graph := roomStore(t, "window")
	say(t, graph, "room", RoleUser, "first ask")
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 1000, Cost: 0.10})
	say(t, graph, "room", RoleAgent, "first answer")
	say(t, graph, "room", RoleUser, "second ask")
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 2000, Cost: 0.02})

	turn, found, err := graph.TurnSpend("room")
	if err != nil || !found {
		t.Fatalf("TurnSpend = found %v, err %v", found, err)
	}
	if turn.Spine.Runs != 1 || turn.Cost() != 0.02 || turn.SpinePromptHighWater != 2000 {
		t.Fatalf("newest turn = %+v; want only the second ask's run", turn)
	}

	session, found, err := graph.SessionSpend("room")
	if err != nil || !found {
		t.Fatalf("SessionSpend = found %v, err %v", found, err)
	}
	if session.Spine.Runs != 2 {
		t.Fatalf("room lifetime = %+v; want both turns", session.Spine)
	}
	if got := session.Cost(); got < 0.1199 || got > 0.1201 {
		t.Fatalf("room lifetime cost = %v; want 0.12", got)
	}
	// An agent line is not a turn boundary: the turn opens at what the user
	// asked, which is the only boundary the journal draws for one.
	if turn.SinceSeq <= session.SinceSeq {
		t.Fatalf("turn window opens at %d, room window at %d; want the turn later",
			turn.SinceSeq, session.SinceSeq)
	}
}

func TestAnotherRoomInTheWindowMakesConversationCostACeilingAndTheContextUnsayable(t *testing.T) {
	graph := roomStore(t, "shared")
	say(t, graph, "room", RoleUser, "mine")
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 3000, Cost: 0.03})

	before, _, err := graph.TurnSpend("room")
	if err != nil {
		t.Fatal(err)
	}
	if before.Shared || before.SpinePromptHighWater != 3000 {
		t.Fatalf("alone in the window = %+v; want claimable with a context figure", before)
	}

	// A second room speaks. Its head calls bill the spine under no name, so
	// this room can no longer tell which of the spine rows are its own.
	say(t, graph, "other", RoleUser, "theirs")

	after, _, err := graph.TurnSpend("room")
	if err != nil {
		t.Fatal(err)
	}
	if !after.Shared {
		t.Fatalf("two rooms live = %+v; want Shared", after)
	}
	if after.SpinePromptHighWater != 0 {
		t.Fatalf("SpinePromptHighWater = %d with two rooms live; want silence, not a borrowed context",
			after.SpinePromptHighWater)
	}
	// Work stays exact through all of it: it is attributed by node, not by
	// window, so nothing another room does can move it.
	if after.Work.Runs != 0 {
		t.Fatalf("room work = %+v; want none", after.Work)
	}
}

func TestOneRoomsJobsNeverAppearInAnothersBill(t *testing.T) {
	graph := roomStore(t, "attribution")
	say(t, graph, "room-a", RoleUser, "a")
	say(t, graph, "room-b", RoleUser, "b")
	for _, job := range []struct{ id, room string }{{"job-a", "room-a"}, {"job-b", "room-b"}} {
		if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
			{ID: job.id, Brief: "work for " + job.room, Stage: 1},
		}}, Provenance{Origin: OriginUser, SessionID: job.room, Intent: "work"}); err != nil {
			t.Fatal(err)
		}
	}
	bill(t, graph, NodeUsage{NodeID: "job-a", Cost: 1.50})
	bill(t, graph, NodeUsage{NodeID: "job-b", Cost: 0.25})

	spendA, _, err := graph.SessionSpend("room-a")
	if err != nil {
		t.Fatal(err)
	}
	if spendA.Work.Runs != 1 || spendA.Work.Cost != 1.50 {
		t.Fatalf("room-a work = %+v; want its own job alone", spendA.Work)
	}
	spendB, _, err := graph.SessionSpend("room-b")
	if err != nil {
		t.Fatal(err)
	}
	if spendB.Work.Runs != 1 || spendB.Work.Cost != 0.25 {
		t.Fatalf("room-b work = %+v; want its own job alone", spendB.Work)
	}

	// A repair, a revision or a retry spliced under the job inherits the room,
	// so the room keeps paying for its own work as the graph grows under it.
	if err := graph.Splice("job-a", Subtree{Nodes: []NodeSpec{
		{ID: "repair-a", Brief: "the part that failed, again", Stage: 1},
	}}, Provenance{Origin: OriginSelf, SessionID: "room-a", Intent: "repair"}); err != nil {
		t.Fatal(err)
	}
	bill(t, graph, NodeUsage{NodeID: "repair-a", Cost: 0.10})
	spendA, _, err = graph.SessionSpend("room-a")
	if err != nil {
		t.Fatal(err)
	}
	if spendA.Work.Runs != 2 || spendA.Work.Cost < 1.5999 || spendA.Work.Cost > 1.6001 {
		t.Fatalf("room-a work after a repair = %+v; want both runs at $1.60", spendA.Work)
	}
}

// TestRoomSpendReadsOnlyTheTailOfTheJournal is 12.1.6's perf ledger applied to
// a read the composer polls: stated as a plan rather than a stopwatch, because
// the failure it guards against is a planner choice and not a slow machine.
// The subtree query needed a hand-written CROSS JOIN to keep SQLite from
// reading every run this machine has ever recorded; this one gets the same
// guarantee from the shape — a range on usage's own rowid outside, nodes by
// primary key inside — and the test exists so a later "simplification" of
// either cannot quietly turn a per-keystroke read into a full scan.
func TestRoomSpendReadsOnlyTheTailOfTheJournal(t *testing.T) {
	graph := roomStore(t, "plans")
	explain := func(query string, args ...any) string {
		t.Helper()
		rows, err := graph.db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatalf("explain: %v", err)
			}
			lines = append(lines, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("explain: %v", err)
		}
		return strings.Join(lines, "\n")
	}

	window := explain(roomSpendQuery, "room", "room", "room", "room", "room", "room", int64(1))
	if !strings.Contains(window, "SEARCH usage USING INTEGER PRIMARY KEY") {
		t.Fatalf("the window read is not a rowid range over usage:\n%s", window)
	}
	if strings.Contains(window, "SCAN usage") || strings.Contains(window, "SCAN nodes") {
		t.Fatalf("the window read scans a whole table:\n%s", window)
	}
	if !strings.Contains(window, "SEARCH nodes USING INDEX") {
		t.Fatalf("attribution does not enter nodes by index:\n%s", window)
	}

	turn := explain(`SELECT seq, ts FROM messages WHERE session_id = ? AND role = ?
		ORDER BY seq DESC LIMIT 1`, "room", string(RoleUser))
	if !strings.Contains(turn, "SEARCH messages USING INDEX messages_session_role_seq") {
		t.Fatalf("opening a turn window is not an index seek:\n%s", turn)
	}
}

func TestRoomSpendSurvivesRebuild(t *testing.T) {
	graph := roomStore(t, "rebuild")
	say(t, graph, "room", RoleUser, "go")
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "work", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "go"}); err != nil {
		t.Fatal(err)
	}
	bill(t, graph, NodeUsage{NodeID: "job", Cost: 0.20})
	bill(t, graph, NodeUsage{NodeID: RootID, PromptTokens: 700, Cost: 0.01})

	before, _, err := graph.SessionSpend("room")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, _, err := graph.SessionSpend("room")
	if err != nil {
		t.Fatal(err)
	}
	if before.Work != after.Work || before.Spine != after.Spine || before.SpinePromptHighWater != after.SpinePromptHighWater {
		t.Fatalf("rebuild changed the bill: %+v then %+v", before, after)
	}
}
