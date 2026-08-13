package store

import (
	"strings"
	"testing"
	"time"
)

func postThreadMessage(t *testing.T, graph *Store, message Message) Message {
	t.Helper()
	posted, err := graph.PostMessage(message)
	if err != nil {
		t.Fatalf("post %q: %v", message.Body, err)
	}
	return posted
}

// spliceThreadJob admits one job root under the spine, stamped with the room
// that asked for it — the same shape a commissioned task lands in.
func spliceThreadJob(t *testing.T, graph *Store, session, id, title string) {
	t.Helper()
	err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: id, Title: title, Brief: title, Stage: 1},
		{ID: id + "-step", Parent: id, Title: title + " step", Brief: "a step", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: session, Intent: title})
	if err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
}

func arcFor(arcs []ThreadArc, sessionID string) (ThreadArc, bool) {
	for _, arc := range arcs {
		if arc.SessionID == sessionID {
			return arc, true
		}
	}
	return ThreadArc{}, false
}

// A thread is alive when one of exactly three things is true of its end. All
// three are asserted in one store because the query has to tell them apart, not
// merely notice that something is unresolved — the surface draws a different
// row for each, and the head says a different sentence about each.
func TestOpenThreadsFlagsTheThreeUnresolvedShapes(t *testing.T) {
	graph := openThreadStore(t)

	// The head asked and nothing came back.
	postThreadMessage(t, graph, Message{SessionID: "asking", Role: RoleUser, Body: "what should the discount be?"})
	postThreadMessage(t, graph, Message{SessionID: "asking", Role: RoleAgent,
		Body: "Ten percent, or fifteen?", QuestionSeq: 0,
		Options: []QuestionOption{{Label: "ten", Value: "a"}, {Label: "fifteen", Value: "b"}}})

	// The person spoke and nothing answered.
	postThreadMessage(t, graph, Message{SessionID: "waiting", Role: RoleUser, Body: "start on the deploy failures"})
	postThreadMessage(t, graph, Message{SessionID: "waiting", Role: RoleAgent, Body: "On it."})
	postThreadMessage(t, graph, Message{SessionID: "waiting", Role: RoleUser, Body: "and check staging too"})

	// Work landed and nobody has been in the room since.
	spliceThreadJob(t, graph, "landed", "invoices", "Invoice audit")
	postThreadMessage(t, graph, Message{SessionID: "landed", Role: RoleUser, Body: "audit the invoices"})
	postThreadMessage(t, graph, Message{SessionID: "landed", Role: RoleAgent, Body: "Put that in hand."})
	postThreadMessage(t, graph, Message{SessionID: "landed", Role: RoleSystem, NodeID: "invoices",
		Body: "Invoice audit finished — three duplicates."})

	// And one quiet thread, settled at both ends, which must sink.
	postThreadMessage(t, graph, Message{SessionID: "quiet", Role: RoleUser, Body: "what is the capital of France"})
	postThreadMessage(t, graph, Message{SessionID: "quiet", Role: RoleAgent, Body: "Paris."})

	arcs, err := graph.OpenThreads(10)
	if err != nil {
		t.Fatal(err)
	}
	if _, alive := arcFor(arcs, "quiet"); alive {
		t.Fatalf("a settled conversation is still being called alive: %+v", arcs)
	}

	asking, ok := arcFor(arcs, "asking")
	if !ok || asking.Open != ThreadOpenQuestion {
		t.Fatalf("an unanswered question did not read as one: %+v", asking)
	}
	if !strings.Contains(asking.Left, "Ten percent") {
		t.Fatalf("the left-at line is not the question that is hanging: %q", asking.Left)
	}

	waiting, ok := arcFor(arcs, "waiting")
	if !ok || waiting.Open != ThreadOpenUnanswered {
		t.Fatalf("a message with no reply after it did not read as one: %+v", waiting)
	}
	if !strings.Contains(waiting.Left, "staging") {
		t.Fatalf("the left-at line is not their last words: %q", waiting.Left)
	}

	landed, ok := arcFor(arcs, "landed")
	if !ok || landed.Open != ThreadOpenDelivery {
		t.Fatalf("an unseen delivery did not read as one: %+v", landed)
	}
	if !landed.UnseenDelivery {
		t.Fatal("the one ornament the product allows was not raised on the delivery that earned it")
	}
}

// The dot is about ATTENTION, not about the message: a delivery in a room
// somebody has since had open is read, and the room goes quiet.
func TestOpenThreadsSinksADeliveryTheReaderHasSeen(t *testing.T) {
	graph := openThreadStore(t)
	spliceThreadJob(t, graph, "seen", "invoices", "Invoice audit")
	postThreadMessage(t, graph, Message{SessionID: "seen", Role: RoleUser, Body: "audit the invoices"})
	postThreadMessage(t, graph, Message{SessionID: "seen", Role: RoleAgent, Body: "Put that in hand."})
	postThreadMessage(t, graph, Message{SessionID: "seen", Role: RoleSystem, NodeID: "invoices",
		Body: "Invoice audit finished."})

	arcs, err := graph.OpenThreads(10)
	if err != nil {
		t.Fatal(err)
	}
	if arc, ok := arcFor(arcs, "seen"); !ok || !arc.UnseenDelivery {
		t.Fatalf("the delivery was not unseen before anybody looked: %+v", arc)
	}

	if _, err := graph.TouchSeen("tui", "seen", SeenAttached); err != nil {
		t.Fatal(err)
	}
	arcs, err = graph.OpenThreads(10)
	if err != nil {
		t.Fatal(err)
	}
	if arc, ok := arcFor(arcs, "seen"); ok {
		t.Fatalf("a delivery somebody has since read is still being called alive: %+v", arc)
	}
}

// The nudge may only name a thread that has genuinely been parked. A thread
// waiting since an hour ago is an ordinary working conversation and saying
// anything about it would be nagging.
func TestParkedThreadsExcludesAFreshlyOpenOne(t *testing.T) {
	graph := openThreadStore(t)
	postThreadMessage(t, graph, Message{SessionID: "fresh", Role: RoleUser, Body: "look at the pricing page"})

	now := time.Now()
	parked, err := graph.ParkedThreads(now, 18*time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(parked) != 0 {
		t.Fatalf("a conversation open for seconds was called parked: %+v", parked)
	}
	// The same thread, judged from a day later, is exactly what the nudge is for.
	parked, err = graph.ParkedThreads(now.Add(30*time.Hour), 18*time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := arcFor(parked, "fresh"); !ok {
		t.Fatalf("a thread parked for thirty hours was never named: %+v", parked)
	}
}

// SessionNodes answers "what did this conversation set going", and answers it
// with JOB ROOTS: a splice stamps every node it admits with the commissioning
// room, so the unfiltered read would describe three jobs as forty steps.
func TestSessionNodesReturnsJobRootsOfThatRoomOnly(t *testing.T) {
	graph := openThreadStore(t)
	spliceThreadJob(t, graph, "pricing", "audit", "Pricing audit")
	spliceThreadJob(t, graph, "pricing", "scan", "Competitor scan")
	spliceThreadJob(t, graph, "deploys", "deploy", "Deploy triage")

	nodes, err := graph.SessionNodes("pricing", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("job roots of one room = %d, want 2: %+v", len(nodes), nodes)
	}
	// Newest first, because the recent ones are what a returning reader is
	// asking about.
	if nodes[0].ID != "scan" || nodes[1].ID != "audit" {
		t.Fatalf("job roots came back in the wrong order: %s, %s", nodes[0].ID, nodes[1].ID)
	}
	for _, node := range nodes {
		if node.Provenance.SessionID != "pricing" {
			t.Fatalf("another room's work leaked into this one: %+v", node)
		}
	}
}

// The room-switch part is a wire form between the engine and the surface, so
// what it survives is the whole of what it is worth: a write, a read, and a
// rebuild of the projection from the journal alone.
func TestRoomSwitchPartSurvivesTheJournal(t *testing.T) {
	graph := openThreadStore(t)
	posted := postThreadMessage(t, graph, Message{
		SessionID: "old", Role: RoleSystem, Body: "continuing in a new thread",
		Parts: []MessagePart{RoomSwitchRef("new-room")},
	})
	if len(posted.Parts) != 1 {
		t.Fatalf("the part was dropped on the way in: %+v", posted.Parts)
	}
	if target, ok := RoomSwitchTarget(posted.Parts[0]); !ok || target != "new-room" {
		t.Fatalf("the switch does not name the room it points at: %q ok=%t", target, ok)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	messages, err := graph.Messages("old", 0, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages after rebuild = %+v err=%v", messages, err)
	}
	if target, ok := RoomSwitchTarget(messages[0].Parts[0]); !ok || target != "new-room" {
		t.Fatalf("the switch did not survive a rebuild: %q ok=%t", target, ok)
	}
}

// A switch with no room to switch to is the one way this part can be wrong, and
// it is refused at the write rather than surfaced as a row that moves a window
// nowhere.
func TestRoomSwitchPartRefusesAnEmptyRoom(t *testing.T) {
	graph := openThreadStore(t)
	_, err := graph.PostMessage(Message{
		SessionID: "old", Role: RoleSystem, Body: "continuing in a new thread",
		Parts: []MessagePart{{Kind: PartRoomSwitch, Text: "  "}},
	})
	if err == nil {
		t.Fatal("a room-switch naming no room was accepted")
	}
}

// Two rooms opened in the same instant must not be the same room.
func TestNewSessionIDIsUnique(t *testing.T) {
	seen := make(map[string]bool, 64)
	for index := 0; index < 64; index++ {
		id := NewSessionID()
		if strings.TrimSpace(id) == "" {
			t.Fatal("a room was minted with no name")
		}
		if seen[id] {
			t.Fatalf("two rooms were minted with the same name: %q", id)
		}
		seen[id] = true
	}
}
