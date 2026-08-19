package session

// WHAT A PERSON SEES WHEN THEY WALK INTO A DESIGN.
//
// A design node is a room like any other node's, and for a while it was the one
// room in this build with nobody talking in it: the designer's whole stream was
// observed privately, one throttled status line escaped to the chat, and the
// room a person opened to WATCH the page being written showed that line
// scrolling in place for two minutes and then a finished card. These are the
// tests that keep the discussion in it — live, and on disk afterwards.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE ROOM HEARS THE DESIGNER THINK AND DRAFT. Both kinds go in — the reasoning
// and the reply — because they are the two halves of what the room already draws
// for a worker (internal/tui3's roomThink and roomSay), and a design that
// streamed only one of them would be a room that has been made half a place.
func TestADesignRoomHearsTheDesignerThinkAndDraft(t *testing.T) {
	agent, _ := buildAgent(t, streamingDesigner(), t.TempDir())
	room := newTaskRoom()
	lane := room.join()

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model",
		designSeat{id: 4, room: room, thread: designThread(t)}); err != nil {
		t.Fatalf("the design did not land: %v", err)
	}
	room.close()

	var thought, said strings.Builder
	turns := 0
	for _, event := range drainRoom(t, lane) {
		switch event.Kind {
		case EventReasoning:
			thought.WriteString(event.Text)
		case EventTextDelta:
			said.WriteString(event.Text)
		case EventTurnDone:
			turns++
		}
	}
	if !strings.Contains(thought.String(), designThinking) {
		t.Fatalf("the designer's reasoning never reached the room: %q", thought.String())
	}
	if !strings.Contains(thought.String(), reviewThinking) {
		t.Fatalf("the review pass thought in private: %q", thought.String())
	}
	// BOTH REPLIES, WHOLE. The chat gets a name and a step count off this same
	// stream; the room gets the page being typed.
	if got := said.String(); got != designReply+reviewReply {
		t.Fatalf("the room was handed %q", got)
	}
	if turns != 2 {
		t.Fatalf("%d calls said they were finished, not 2", turns)
	}
}

// AND THE HISTORY MATCHES THE LIVE LANE. A room reopened tomorrow is read off
// the node's journal (internal/tui3's readRoomJournal), so a design whose
// discussion existed only on the live lane would be a design nobody could ever
// read twice.
func TestADesignsRepliesAreKeptInTheNodesJournal(t *testing.T) {
	agent, _ := buildAgent(t, streamingDesigner(), t.TempDir())
	thread, journal := designThreadWithJournal(t)

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model",
		designSeat{id: 4, room: newTaskRoom(), thread: thread}); err != nil {
		t.Fatalf("the design did not land: %v", err)
	}

	replayed, err := replaySessionFile(journal)
	if err != nil {
		t.Fatalf("the node's journal did not read back: %v", err)
	}
	var written []string
	for _, message := range replayed.messages {
		if message.Role == "assistant" {
			written = append(written, messageContentText(message))
		}
	}
	if len(written) != 2 {
		t.Fatalf("the journal kept %d of the designer's replies, not 2: %v", len(written), written)
	}
	if written[0] != designReply || written[1] != reviewReply {
		t.Fatalf("the journal kept something other than what was said: %q", written)
	}

	// AND NONE OF IT IS IN FRONT OF THE MODEL. The thread answers questions about
	// the page that was actually written; a draft it replaced sitting in the same
	// transcript is a wrong answer waiting to be given ([Agent.journalOnly]).
	thread.mu.Lock()
	defer thread.mu.Unlock()
	for _, message := range thread.messages {
		if strings.Contains(messageContentText(message), designReply) {
			t.Fatal("a design draft reached the thread's own transcript")
		}
	}
}

// A FINISHED REPLY IS HANDED OVER ONCE. The room keeps the step it is in the
// middle of for whoever walks in next (task_room.go's taskCatchup), and a design
// call that ended without saying so would leave its whole page in there — drawn
// a second time, under the copy the journal already gave the same person.
func TestADesignsFinishedReplyIsNotCaughtUpTwice(t *testing.T) {
	agent, _ := buildAgent(t, streamingDesigner(), t.TempDir())
	room := newTaskRoom()

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model",
		designSeat{id: 4, room: room, thread: designThread(t)}); err != nil {
		t.Fatalf("the design did not land: %v", err)
	}
	if caught := takeRoom(t, room.join(), 0); len(caught) != 0 {
		t.Fatalf("a reply already on disk was replayed to the next watcher: %v", kinds(caught))
	}
}

// A DESIGN WITH NO NODE STREAMS NOWHERE AND FAILS AT NOTHING. Every caller
// without a room hands a zero seat, and the whole ladder underneath it is tested
// that way (harness_build_law_test.go) — so the seat's own methods have to be
// the ones that check.
func TestADesignWithNobodyWatchingStillLands(t *testing.T) {
	agent, _ := buildAgent(t, streamingDesigner(), t.TempDir())

	page, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err != nil {
		t.Fatalf("a design nobody was watching failed: %v", err)
	}
	if page.Id.Name != "flake-triage" {
		t.Fatalf("the page that came back is %q", page.Id.Name)
	}
}

// ── harness ─────────────────────────────────────────────────────────────────

// What the two designer turns of these tests reason out loud, so an assertion
// can name the thought it is looking for.
const (
	designThinking = "two jobs: read the failure, then check the report"
	reviewThinking = "the draft holds up"
)

// streamingDesigner answers a whole design the way a provider does: the reply
// arrives on the stream, chunk by chunk, before it is returned whole.
func streamingDesigner() *scriptedCompleter {
	return &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			return streamed(ctx, designThinking, designReply), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			return streamed(ctx, reviewThinking, reviewReply), nil
		},
	}}
}

// streamed emits one reply the way an adapter does — the reasoning first, then
// the text in pieces — and answers with the assembled response beside it.
func streamed(ctx context.Context, thought, text string) *ai.Response {
	provider.Emit(ctx, provider.StreamReasoning, thought)
	for at := 0; at < len(text); at += 64 {
		end := at + 64
		if end > len(text) {
			end = len(text)
		}
		provider.Emit(ctx, provider.StreamDelta, text[at:end])
	}
	return textResponse(text)
}

// designThread is the child a design node stands in its room, with a journal of
// its own to keep the discussion in.
func designThread(t *testing.T) *Agent {
	t.Helper()
	thread, _ := designThreadWithJournal(t)
	return thread
}

func designThreadWithJournal(t *testing.T) (*Agent, string) {
	t.Helper()
	journal := writeableJournal(t)
	thread, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	return thread, journal
}
