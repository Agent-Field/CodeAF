package session

// WHAT A PAGE WITH NO LANE CAN READ OFF A NODE'S JOURNAL, AND WHAT A PAGE WITH
// NO LANE IS TOLD ABOUT A NODE'S PRICE.
//
// A node running on another machine reaches a surface as two things only: a
// bounded tail of its journal, read four times a second, and its row on the task
// lane. The live token column on its page is read out of the first
// ([RequestBooks]); the price on its row moves on the second, and used to move
// only when the node's state did ([childRun.tellSpend]).

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// nodeJournal writes a node's journal THROUGH THE SESSION'S OWN WRITER — the
// same doors a running node's requests go through — and hands back its bytes.
func nodeJournal(t *testing.T, write func(*sessionFile)) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.jsonl")
	journal, _, err := openSessionFile(path, "/lab", "vendor/worker", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	write(journal)
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return data
}

// ONE LINE PER REQUEST, AND THE NEWEST IS WHAT WAS LAST SENT. An errand's call
// is written into the same file with its role on it, and it is not the work's:
// counting it would move a node's figures for a title nobody on its page wrote.
func TestTheJournalsRequestLinesSayWhatEachRequestSentAndGotBack(t *testing.T) {
	data := nodeJournal(t, func(f *sessionFile) {
		f.appendMessage(ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "fix the loader"}}})
		f.appendCall(journalCall{Model: "vendor/worker", Input: 12000, CacheRead: 11000, Output: 300})
		f.appendMessage(ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "reading it"}}})
		f.appendCall(journalCall{Model: "cheap/namer", Role: "title", Input: 500, Output: 20})
		f.appendCall(journalCall{Model: "vendor/worker", Input: 15400, Output: 120})
		f.appendUsage(Usage{Input: 27400, Output: 420, Calls: 2}, "vendor/worker", false, "")
	})
	books := ReadTranscriptBytes(data).Requests
	if books.Latest != 15400 {
		t.Fatalf("the newest request weighed %d, want 15400 — what it sent, not a sum", books.Latest)
	}
	want := []RequestLine{{Input: 12000, Output: 300}, {Input: 15400, Output: 120}}
	if len(books.Recent) != len(want) || books.Recent[0] != want[0] || books.Recent[1] != want[1] {
		t.Fatalf("the requests read back as %+v, want %+v (the errand is not the work's)", books.Recent, want)
	}
}

// A TAIL OPENS MID-FILE, and the newest request is still in it: the last line
// written is the last line of any tail.
func TestATailStillKnowsTheNewestRequest(t *testing.T) {
	data := nodeJournal(t, func(f *sessionFile) {
		for i := 0; i < 40; i++ {
			f.appendMessage(ai.Message{Role: "tool", ToolCallID: "c", Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("x", 400)}}})
			f.appendCall(journalCall{Model: "vendor/worker", Input: 1000 * (i + 1), Output: 10})
		}
	})
	tail := data[len(data)/2:]
	books := ReadTranscriptBytes(tail).Requests
	if books.Latest != 40000 {
		t.Fatalf("the tail's newest request weighed %d, want 40000", books.Latest)
	}
	if len(books.Recent) == 0 || len(books.Recent) >= 40 {
		t.Fatalf("a tail read %d requests — it should hold some and not all", len(books.Recent))
	}
}

// THE OTHER WAY OF COUNTING A CACHE. A provider that reports cached prompt
// tokens BESIDE the prompt rather than inside it writes a cached share larger
// than the prompt, which only that dialect can; the request's weight is then
// the two together.
func TestACachedShareLargerThanThePromptIsAddedBack(t *testing.T) {
	data := nodeJournal(t, func(f *sessionFile) {
		f.appendCall(journalCall{Model: "vendor/worker", Input: 900, CacheRead: 42000, Output: 50})
	})
	if got := ReadTranscriptBytes(data).Requests.Latest; got != 42900 {
		t.Fatalf("the request weighed %d, want the prompt and its cache together (42900)", got)
	}
}

// A NODE'S PRICE CROSSES AT EACH STEP THAT MOVED IT, and only then. Before
// this, the row went out at state changes alone, and a hosted window drew the
// price from the moment the node started for as long as it ran.
func TestANodesPriceIsPublishedAtEachStepThatMovedIt(t *testing.T) {
	home, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	child := spentChild(t, 0.25)
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, home: home}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "Add the greeting"},
		state: TaskRunning, done: make(chan struct{})}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()
	t.Cleanup(func() { close(node.done) })

	updates, stop := home.WatchTaskUpdates()
	t.Cleanup(stop)
	room := node.openRoom()
	room.speaking(child)
	run := &childRun{node: node, room: room, child: child, log: io.Discard}

	run.observe(Event{Kind: EventTurnDone})
	// A second end that moved no money says nothing.
	run.observe(Event{Kind: EventTurnDone})
	// The sentinel is published on the same lane after both, so everything the
	// two observations sent is in front of it.
	home.emitTaskUpdate(TaskNotice{ID: 99, State: TaskRunning})

	priced := 0
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-updates:
			if !ok {
				t.Fatal("the task lane closed")
			}
			if ev.Kind != EventTaskUpdate || ev.Task == nil {
				continue
			}
			if ev.Task.ID == 99 {
				if priced != 1 {
					t.Fatalf("the node's price went out %d times for one step that moved it, want once", priced)
				}
				return
			}
			if ev.Task.ID == 1 {
				if ev.Task.CostUSD != 0.25 {
					t.Fatalf("the row carried %v, want the worker's live 0.25", ev.Task.CostUSD)
				}
				priced++
			}
		case <-deadline:
			t.Fatal("the sentinel never arrived on the task lane")
		}
	}
}
