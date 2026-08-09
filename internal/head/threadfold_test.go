package head

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// syntheticSession writes a thread with every shape the window has to survive
// a cut on: the person's turns, thread-level lines with no node behind them,
// job chatter anchored to nodes, attachments, and a numbered askback with its
// answer. It is long enough that both windows overflow several times over, so
// the cheap read is exercised on either side of a cut rather than only before
// the first one.
func syntheticSession(t *testing.T, graph *store.Store, session string) []store.Message {
	t.Helper()
	post := func(message store.Message) store.Message {
		message.SessionID = session
		written, err := graph.PostMessage(message)
		if err != nil {
			t.Fatalf("post %q: %v", message.Body, err)
		}
		return written
	}
	written := make([]store.Message, 0, 128)
	for round := 0; round < 5; round++ {
		node := fmt.Sprintf("task-%s-%d", session, round)
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
			{ID: node, Brief: "some work", Stage: 1},
		}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "some work"}); err != nil {
			t.Fatal(err)
		}
		written = append(written,
			post(store.Message{Role: store.RoleUser, Body: fmt.Sprintf("round %d: what about the pricing?", round)}),
			post(store.Message{Role: store.RoleAgent, Body: fmt.Sprintf("round %d: on it", round)}),
			post(store.Message{Role: store.RoleSystem, NodeID: node,
				Body: fmt.Sprintf("round %d: started", round)}),
			post(store.Message{Role: store.RoleAgent, NodeID: node,
				Body: fmt.Sprintf("round %d: still going", round)}),
			post(store.Message{Role: store.RoleUser, Body: fmt.Sprintf("round %d: here is the sheet", round),
				Attachments: []string{fmt.Sprintf("sha256:%02d", round), fmt.Sprintf("sha256:%02d-b", round)}}),
			post(store.Message{Role: store.RoleAgent, NodeID: node,
				Body: fmt.Sprintf("round %d: delivered", round), Attachments: []string{fmt.Sprintf("out-%d.md", round)}}),
			post(store.Message{Role: store.RoleAgent, QuestionSeq: int64(round + 1),
				Body:    fmt.Sprintf("round %d: which one?", round),
				Options: []store.QuestionOption{{Label: "the first"}, {Label: "the second"}}}),
			post(store.Message{Role: store.RoleUser, QuestionSeq: int64(round + 1), Body: "the second"}),
		)
	}
	return written
}

// The window is a left fold over an append-only journal, so folding once and
// carrying the state forward has to render exactly what folding from zero
// renders — for every bound, on both sides of every cut. This is the whole
// licence for keeping the fold at all.
func TestThreadFoldResumesByteIdentically(t *testing.T) {
	graph := openHeadStore(t)
	written := syntheticSession(t, graph, "fold")
	syntheticSession(t, graph, "other")

	warm := New(nil, graph)
	for _, message := range written {
		// A fresh head has no fold to resume, so this read is the from-zero one.
		want, err := New(nil, graph).recentThread("fold", message.Seq)
		if err != nil {
			t.Fatal(err)
		}
		got, err := warm.recentThread("fold", message.Seq)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resumed window before seq %d differs from the from-zero fold:\ngot  %v\nwant %v",
				message.Seq, bodies(got), bodies(want))
		}
		if warm.renderThread(got) != warm.renderThread(want) {
			t.Fatalf("resumed window before seq %d rendered different bytes", message.Seq)
		}
		// Asking again inside the same turn is what the router, the correction
		// reader, the redirect reader and the identity reader all do.
		again, err := warm.recentThread("fold", message.Seq)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again, want) {
			t.Fatalf("the second read of seq %d differs from the first", message.Seq)
		}
	}
}

// Two threads in one process, and a read of a bound the fold has already passed.
// Neither is the case the resume was built for, and both must still be right.
func TestThreadFoldStartsOverWhenItCannotResume(t *testing.T) {
	graph := openHeadStore(t)
	first := syntheticSession(t, graph, "fold")
	second := syntheticSession(t, graph, "other")

	warm := New(nil, graph)
	check := func(session string, seq int64) {
		t.Helper()
		want, err := New(nil, graph).recentThread(session, seq)
		if err != nil {
			t.Fatal(err)
		}
		got, err := warm.recentThread(session, seq)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s before seq %d:\ngot  %v\nwant %v", session, seq, bodies(got), bodies(want))
		}
	}
	// Alternating sessions: each read finds the other thread's fold in hand.
	for index := range first {
		check("fold", first[index].Seq)
		check("other", second[index].Seq)
	}
	// And backwards, which no resume can serve.
	check("fold", first[len(first)/2].Seq)
	check("fold", first[3].Seq)
	check("fold", first[len(first)-1].Seq)
}

// A window handed to a caller is that caller's own. It used to be, because
// every read built fresh slices; a kept fold that handed out its own working
// slice would let the next cut rewrite a thread already rendered.
func TestThreadFoldHandsOutIndependentWindows(t *testing.T) {
	graph := openHeadStore(t)
	written := syntheticSession(t, graph, "fold")
	warm := New(nil, graph)

	held, err := warm.recentThread("fold", written[len(written)-1].Seq)
	if err != nil {
		t.Fatal(err)
	}
	before := bodies(held)
	held[0].Body = "rewritten by a caller"
	for _, message := range written {
		if _, err := warm.recentThread("fold", message.Seq); err != nil {
			t.Fatal(err)
		}
	}
	again, err := warm.recentThread("fold", written[len(written)-1].Seq)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bodies(again), before) {
		t.Fatalf("a caller's edit reached the kept fold:\ngot  %v\nwant %v", bodies(again), before)
	}
}

func bodies(messages []store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, fmt.Sprintf("%d:%s", message.Seq, message.Body))
	}
	return out
}
