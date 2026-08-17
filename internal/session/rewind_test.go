package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A rewind drops the last thing the person said and the whole turn it started —
// the reply, the tool calls, the results — and leaves everything before it
// exactly as it was.
func TestRewindDropsTheLastTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("an empty directory"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "how does the planner work?"))
	collect(t, mustSubmit(t, agent, "now delete it all"))

	removed, err := agent.Rewind()
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}

	// What came back is what the surface must un-draw: the person's message,
	// the assistant turn it started, the tool row inside it, and the result.
	if len(removed) == 0 {
		t.Fatal("Rewind reported nothing removed")
	}
	if removed[0].Role != "user" || removed[0].Text != "now delete it all" {
		t.Fatalf("first removed entry = %+v, want the message that started the turn", removed[0])
	}
	sawToolRow := false
	for _, entry := range removed {
		if entry.Role == "tool" && entry.Tool == "ls" {
			sawToolRow = true
		}
	}
	if !sawToolRow {
		t.Fatalf("the dropped turn's tool call was not reported: %+v", removed)
	}

	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript after rewind = %v, want the first turn alone", got)
	}
	if countMessages(agent, "now delete it all") != 0 {
		t.Fatal("the rewound message is still in the transcript")
	}

	// And the model never hears it again: the next request carries the first
	// turn and the new question, and nothing of the turn that was taken back.
	collect(t, mustSubmit(t, agent, "never mind — explain the executor"))
	sent := completer.request(3)
	for _, message := range sent {
		if strings.Contains(messageText(message), "now delete it all") {
			t.Fatalf("the rewound turn rode the next request: %v", rolesOf(sent))
		}
	}
	if got, want := rolesOf(sent), []string{"system", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("next request = %v, want %v", got, want)
	}
}

// The journal is rewound too, and a session that rewinds and keeps working
// resumes as itself: the dropped turn is gone, and everything said AFTER the
// rewind is ordinary conversation that replays.
func TestRewindSurvivesAResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("an empty directory"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the executor runs leaves"), nil
		},
	}}
	first, workspace := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, first, "how does the planner work?"))
	collect(t, mustSubmit(t, first, "now delete it all"))
	if _, err := first.Rewind(); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	collect(t, mustSubmit(t, first, "explain the executor instead"))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := newAgent(Config{
		Workspace:   workspace,
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	second.mu.Lock()
	restored := append([]ai.Message(nil), second.messages...)
	second.mu.Unlock()

	if got, want := rolesOf(restored), []string{"system", "user", "assistant", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("resumed transcript = %v, want the rewound turn gone and the new one kept", got)
	}
	for _, message := range restored {
		if strings.Contains(messageText(message), "now delete it all") {
			t.Fatal("the rewound turn came back on resume")
		}
	}
	if messageText(restored[3]) != "explain the executor instead" {
		t.Fatalf("post-rewind message = %q", messageText(restored[3]))
	}
	if messageText(restored[4]) != "the executor runs leaves" {
		t.Fatalf("post-rewind reply = %q", messageText(restored[4]))
	}
}

// A rewind under a running turn would cut the transcript the turn is writing.
// It refuses instead, and says which door to use.
func TestRewindRefusesWhileATurnRuns(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "working on it")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "start something long")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the scripted step never started")
	}

	if _, err := agent.Rewind(); !errors.Is(err, ErrTurnInFlight) {
		t.Fatalf("Rewind mid-turn = %v, want ErrTurnInFlight", err)
	}

	agent.Interrupt()
	collect(t, events)

	// Once the turn is over the same rewind lands: the refusal was about
	// timing, not about the request.
	if _, err := agent.Rewind(); err != nil {
		t.Fatalf("Rewind after the turn ended: %v", err)
	}
	if got, want := transcriptRoles(agent), []string{"system"}; !equalStrings(got, want) {
		t.Fatalf("transcript = %v, want the interrupted turn dropped", got)
	}
}

// Nothing said, nothing to take back. The sentinel is what lets the surface say
// so instead of reporting a success that removed nothing.
func TestRewindWithNothingToRewind(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.Rewind(); !errors.Is(err, ErrNothingToRewind) {
		t.Fatalf("Rewind on a fresh session = %v, want ErrNothingToRewind", err)
	}
}

// A compaction summary is a user-role message this package injected, not
// something anybody said. Rewinding to it would drop a whole resumed
// conversation and leave the summary that replaced its beginning.
func TestRewindSkipsTheCompactionNote(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", compactionNote("## Goal\nship the thing")),
		textMessage("user", "so where were we?"),
		textMessage("assistant", "here is where we were"))
	agent.mu.Unlock()

	if _, err := agent.Rewind(); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if got, want := transcriptRoles(agent), []string{"system", "user"}; !equalStrings(got, want) {
		t.Fatalf("transcript = %v, want the summary note kept", got)
	}

	// And the note itself is not a turn: a second rewind has nothing left.
	if _, err := agent.Rewind(); !errors.Is(err, ErrNothingToRewind) {
		t.Fatalf("second Rewind = %v, want ErrNothingToRewind", err)
	}
}

// ── the points list ─────────────────────────────────────────────────────────

// twoTurnAgent is the history every points test reads: two turns, the second of
// them a tool call and its result, which is the smallest conversation that has
// one of everything a point can be — two turn anchors, step boundaries around an
// assistant reply and a tool result, and one boundary the batch forbids.
//
//	0 system
//	1 user      "how does the planner work?"
//	2 assistant "the planner walks the graph"
//	3 user      "now delete it all"
//	4 assistant (calls ls as c1)
//	5 tool      c1's result
//	6 assistant "an empty directory"
func twoTurnAgent(t *testing.T, mutate func(*Config)) (*Agent, *scriptedCompleter, string) {
	t.Helper()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("an empty directory"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, mutate)
	// A journaled session names itself after its first turn (title.go), and
	// that one call would eat a scripted step and leave the fixture a turn
	// short. These tests are about the transcript, not the name, so the single
	// attempt is marked spent before the conversation starts.
	agent.mu.Lock()
	agent.titleTried = true
	agent.mu.Unlock()

	collect(t, mustSubmit(t, agent, "how does the planner work?"))
	collect(t, mustSubmit(t, agent, "now delete it all"))

	want := []string{"system", "user", "assistant", "user", "assistant", "tool", "assistant"}
	if got := transcriptRoles(agent); !equalStrings(got, want) {
		t.Fatalf("the fixture is not the history these tests describe: %v", got)
	}
	return agent, completer, workspace
}

func pointIndices(points []RewindPoint) []int {
	out := make([]int, len(points))
	for i, point := range points {
		out[i] = point.Index
	}
	return out
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Every place the conversation can be cut, oldest first: the two things the
// person said, the boundaries between the steps of the second turn, and NOT the
// boundary in the middle of a batch — a cut there would keep an assistant
// message asking for a result it had just dropped.
func TestRewindPointsOverAMultiTurnHistory(t *testing.T) {
	agent, _, _ := twoTurnAgent(t, nil)

	points := agent.RewindPoints()
	if got, want := pointIndices(points), []int{1, 2, 3, 4, 6}; !equalInts(got, want) {
		t.Fatalf("point indices = %v, want %v (5 is inside the batch)", got, want)
	}

	// The two turn anchors carry what was said; the step points carry nothing,
	// because there is no draft to hand back to a cut that lands mid-turn.
	wantTurn := map[int]string{1: "how does the planner work?", 3: "now delete it all"}
	for _, point := range points {
		said, isTurn := wantTurn[point.Index]
		if point.Turn != isTurn {
			t.Fatalf("point %d Turn = %v, want %v", point.Index, point.Turn, isTurn)
		}
		if point.Said != said {
			t.Fatalf("point %d Said = %q, want %q", point.Index, point.Said, said)
		}
	}

	// Entry is checked against a REAL transcript, because that is the whole
	// claim it makes: the row a surface drew for the first message the cut
	// would drop.
	transcript := agent.Transcript()
	if len(transcript) != 7 {
		t.Fatalf("transcript rows = %d, want 7 (four messages, a call row, a result, a reply)", len(transcript))
	}
	wantEntry := map[int]int{1: 0, 2: 1, 3: 2, 4: 3, 6: 6}
	for _, point := range points {
		if point.Entry != wantEntry[point.Index] {
			t.Fatalf("point %d Entry = %d, want %d", point.Index, point.Entry, wantEntry[point.Index])
		}
		if point.Entry < 0 || point.Entry >= len(transcript) {
			t.Fatalf("point %d Entry = %d, outside a %d-row transcript", point.Index, point.Entry, len(transcript))
		}
	}

	// The row Entry names is the row the cut starts on: for a turn point, the
	// person's message itself.
	for _, point := range points {
		if !point.Turn {
			continue
		}
		row := transcript[point.Entry]
		if row.Role != "user" || row.Text != point.Said {
			t.Fatalf("point %d Entry lands on %+v, want the person's own row", point.Index, row)
		}
	}
	// And for the two step points, the assistant rows they open on — the second
	// of which is the reply that came after the tool result, four rows further
	// down than its message index, because the call drew a row of its own.
	if row := transcript[3]; row.Role != "assistant" {
		t.Fatalf("the step point at message 4 lands on %+v, want the assistant reply", row)
	}
	if row := transcript[6]; row.Role != "assistant" || row.Text != "an empty directory" {
		t.Fatalf("the step point at message 6 lands on %+v, want the closing reply", row)
	}

	// What the excluded boundary would have cost: the retained assistant
	// message at 4 still holds the call the result at 5 answered.
	agent.mu.Lock()
	calls := len(agent.messages[4].ToolCalls)
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("fixture message 4 holds %d calls, want the one the batch point protects", calls)
	}
}

// A cut at a step boundary keeps the instruction and drops only the last thing
// the model did with it. The tail it leaves is one a provider will take: every
// call still in the transcript still has its answer.
func TestRewindAtAStepBoundaryLeavesAValidTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _, _ := twoTurnAgent(t, func(config *Config) { config.SessionFile = path })

	removed, err := agent.RewindAt(6)
	if err != nil {
		t.Fatalf("RewindAt(6): %v", err)
	}
	if len(removed) != 1 || removed[0].Role != "assistant" || removed[0].Text != "an empty directory" {
		t.Fatalf("removed = %+v, want the closing reply alone", removed)
	}

	want := []string{"system", "user", "assistant", "user", "assistant", "tool"}
	if got := transcriptRoles(agent); !equalStrings(got, want) {
		t.Fatalf("transcript after the cut = %v, want %v", got, want)
	}
	assertNoDanglingCalls(t, agent)

	// The journal says one message left, which is the count a replay applies.
	if got := lastRewindMarker(t, path); got != 1 {
		t.Fatalf("journaled dropped = %d, want 1", got)
	}

	// And the retained tail is a place the conversation can carry on from: the
	// next request opens with the tool result and nothing dangles in it.
	if _, err := agent.RewindAt(4); err != nil {
		t.Fatalf("RewindAt(4) on the retained tail: %v", err)
	}
	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("transcript after the second cut = %v, want %v", got, want)
	}
	assertNoDanglingCalls(t, agent)
	// Two markers now, and each counts against the transcript as it stood when
	// it was written: the closing reply was already gone, so this cut drops the
	// call and its result and nothing else.
	if got := lastRewindMarker(t, path); got != 2 {
		t.Fatalf("journaled dropped = %d, want 2 (the call and its result)", got)
	}
}

// assertNoDanglingCalls is the law the points list exists to keep: no message
// left in the transcript asks for a result that is not also in it.
func assertNoDanglingCalls(t *testing.T, a *Agent) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	answered := map[string]bool{}
	for _, message := range a.messages {
		if message.Role == "tool" && message.ToolCallID != "" {
			answered[message.ToolCallID] = true
		}
	}
	for index, message := range a.messages {
		for _, call := range message.ToolCalls {
			if !answered[call.ID] {
				t.Fatalf("message %d still calls %q (%s) with no result behind it", index, call.ID, call.Function.Name)
			}
		}
	}
}

// lastRewindMarker is the dropped count on the newest rewind line in the file.
func lastRewindMarker(t *testing.T, path string) int {
	t.Helper()
	dropped := -1
	for _, line := range readLines(t, path) {
		var entry struct {
			Type    string `json:"type"`
			Dropped int    `json:"dropped"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "rewind" {
			dropped = entry.Dropped
		}
	}
	if dropped < 0 {
		t.Fatal("no rewind marker in the session file")
	}
	return dropped
}

// A mid-turn cut survives a resume exactly as a turn cut does: the marker is a
// count of messages dropped from the tail, and the tail it dropped ended inside
// a turn. The reopened session is the same conversation, row for row.
func TestRewindAtSurvivesAResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	first, _, workspace := twoTurnAgent(t, func(config *Config) { config.SessionFile = path })

	if _, err := first.RewindAt(6); err != nil {
		t.Fatalf("RewindAt(6): %v", err)
	}
	before := first.Transcript()
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := newAgent(Config{
		Workspace:   workspace,
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if got, want := transcriptRoles(second), []string{"system", "user", "assistant", "user", "assistant", "tool"}; !equalStrings(got, want) {
		t.Fatalf("resumed transcript = %v, want the mid-turn tail", got)
	}
	assertNoDanglingCalls(t, second)

	after := second.Transcript()
	if len(after) != len(before) {
		t.Fatalf("resumed rows = %d, want the %d the cut left", len(after), len(before))
	}
	for index := range before {
		if after[index].Role != before[index].Role || after[index].Text != before[index].Text || after[index].Tool != before[index].Tool {
			t.Fatalf("row %d resumed as %+v, want %+v", index, after[index], before[index])
		}
	}
	for _, message := range messagesOf(second) {
		if strings.Contains(messageText(message), "an empty directory") {
			t.Fatal("the cut reply came back on resume")
		}
	}

	// The points list is the same one on the far side of the resume, which is
	// what lets a surface draw the picker over a session it did not run.
	if got, want := pointIndices(second.RewindPoints()), []int{1, 2, 3, 4}; !equalInts(got, want) {
		t.Fatalf("resumed points = %v, want %v", got, want)
	}
}

// A cut is destructive, so an index that is not a point is refused rather than
// rounded — and the refusal says what the caller should have asked for. A turn
// in flight is refused for the reason every cut is.
func TestRewindAtRefusals(t *testing.T) {
	t.Run("an index inside a batch names the nearest point", func(t *testing.T) {
		agent, _, _ := twoTurnAgent(t, nil)
		_, err := agent.RewindAt(5)
		if err == nil {
			t.Fatal("RewindAt(5) succeeded; 5 is inside the batch")
		}
		if !strings.Contains(err.Error(), "nearest is 6") {
			t.Fatalf("RewindAt(5) = %v, want the nearest point named (6, the smaller cut)", err)
		}
		if got, want := len(agent.Transcript()), 7; got != want {
			t.Fatalf("a refused cut removed rows: %d, want %d", got, want)
		}
	})

	t.Run("the system message is never a cut", func(t *testing.T) {
		agent, _, _ := twoTurnAgent(t, nil)
		if _, err := agent.RewindAt(0); err == nil {
			t.Fatal("RewindAt(0) succeeded; index 0 is the system message")
		}
	})

	t.Run("past the end", func(t *testing.T) {
		agent, _, _ := twoTurnAgent(t, nil)
		_, err := agent.RewindAt(99)
		if err == nil {
			t.Fatal("RewindAt(99) succeeded")
		}
		if !strings.Contains(err.Error(), "nearest is 6") {
			t.Fatalf("RewindAt(99) = %v, want the last point named", err)
		}
	})

	t.Run("nothing to rewind", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
		if _, err := agent.RewindAt(1); !errors.Is(err, ErrNothingToRewind) {
			t.Fatalf("RewindAt on a fresh session = %v, want ErrNothingToRewind", err)
		}
	})

	t.Run("while a turn runs", func(t *testing.T) {
		streaming := make(chan struct{})
		completer := &scriptedCompleter{steps: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				provider.Emit(ctx, provider.StreamDelta, "working on it")
				close(streaming)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}}
		agent, _ := newTestAgent(t, completer, nil)
		events := mustSubmit(t, agent, "start something long")
		select {
		case <-streaming:
		case <-time.After(10 * time.Second):
			t.Fatal("the scripted step never started")
		}
		if _, err := agent.RewindAt(1); !errors.Is(err, ErrTurnInFlight) {
			t.Fatalf("RewindAt mid-turn = %v, want ErrTurnInFlight", err)
		}
		agent.Interrupt()
		collect(t, events)
		if _, err := agent.RewindAt(1); err != nil {
			t.Fatalf("RewindAt after the turn ended: %v", err)
		}
	})
}

// A compaction note is not a place anybody can rewind TO: it is context this
// package handed the model, and cutting there would drop a resumed conversation
// while keeping the summary that replaced its beginning. It never appears in the
// list — while the boundary just PAST it does, because keeping the summary and
// dropping what came after it is an ordinary cut.
func TestRewindPointsSkipTheCompactionNote(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", compactionNote("## Goal\nship the thing")),
		textMessage("user", "so where were we?"),
		textMessage("assistant", "here is where we were"))
	agent.mu.Unlock()

	points := agent.RewindPoints()
	if got, want := pointIndices(points), []int{2, 3}; !equalInts(got, want) {
		t.Fatalf("point indices = %v, want %v (1 is the summary note)", got, want)
	}
	for _, point := range points {
		if isCompactionNote(point.Said) {
			t.Fatalf("point %d offers the summary as something that was said: %q", point.Index, point.Said)
		}
	}
	if !points[0].Turn || points[0].Said != "so where were we?" {
		t.Fatalf("first point = %+v, want the person's question", points[0])
	}

	// And the cut the list forbids is the cut RewindAt forbids.
	if _, err := agent.RewindAt(1); err == nil {
		t.Fatal("RewindAt(1) succeeded; the summary note is not a point")
	}
}
