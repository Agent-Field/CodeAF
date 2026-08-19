package session

// WHAT A PERSON SEES WHEN THEY WALK INTO A DESIGN.
//
// A design node is a room like any other node's, and for a while it was the one
// room in this build with nobody talking in it: the designer's whole stream was
// observed privately, one throttled status line escaped to the chat, and the
// room a person opened to WATCH the page being written showed that line
// scrolling in place for two minutes and then a finished card.
//
// THE FIX FOR THAT WAS ONCE THE OPPOSITE MISTAKE, and these tests hold the line
// between them. The designer's reply is a JSON envelope, so a room handed the
// content deltas showed a wall of braces — and a journal handed the same text
// replayed that wall to whoever opened the design tomorrow. What the room gets
// live is the THINKING, over the replacing progress row the chat already draws
// off the same stream; what the journal gets is a MILESTONE per stage, in a
// person's words, with the card in a fence so markdown leaves its columns alone.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE ROOM HEARS THE DESIGNER THINK AND NEVER SEES IT TYPE. The reasoning is
// prose and is drawn exactly as a worker's is (internal/tui3's roomThink); the
// content is the page's own JSON, and the live account of THAT is the replacing
// progress row (harness_build.go's harnessProgress), never a text delta.
func TestADesignRoomHearsTheDesignerThinkAndNeverSeesTheJSON(t *testing.T) {
	agent, _ := buildAgent(t, streamingDesigner(), t.TempDir())
	room := newTaskRoom()
	lane := room.join()

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model",
		designSeat{id: 4, room: room, thread: designThread(t)}); err != nil {
		t.Fatalf("the design did not land: %v", err)
	}
	room.close()

	var thought strings.Builder
	turns, typed := 0, 0
	for _, event := range drainRoom(t, lane) {
		switch event.Kind {
		case EventReasoning:
			thought.WriteString(event.Text)
		case EventTextDelta:
			typed++
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
	if typed != 0 {
		t.Fatalf("%d chunks of the designer's JSON were typed into the room", typed)
	}
	if turns != 2 {
		t.Fatalf("%d calls said they were finished, not 2", turns)
	}
}

// AND THE JOURNAL KEEPS THE STORY, NOT THE TRANSPORT. A room reopened tomorrow
// is read off the node's journal (internal/tui3's readRoomJournal), so what is
// written there is what the design will be read as: the draft named and sized,
// why it is shaped that way, what it answers to, and the card in a fence.
func TestADesignsMilestonesAreKeptInTheNodesJournal(t *testing.T) {
	agent, _ := buildAgent(t, streamingDesigner(), t.TempDir())
	thread, journal := designThreadWithJournal(t)

	page, _, err := agent.designPage(context.Background(), "anything", "test/model",
		designSeat{id: 4, room: newTaskRoom(), thread: thread})
	if err != nil {
		t.Fatalf("the design did not land: %v", err)
	}

	written := journaledMilestones(t, journal)
	if len(written) != 2 {
		t.Fatalf("the journal kept %d milestones, not 2: %v", len(written), written)
	}
	draft := written[0]
	if !strings.HasPrefix(draft, "The draft is written — flake-triage · 2 steps.") {
		t.Fatalf("the draft milestone opens with %q", draft)
	}
	if !strings.Contains(draft, "Two jobs: read the failure, then check the report names it.") {
		t.Fatalf("the justification is not in the milestone: %q", draft)
	}
	if !strings.Contains(draft, "It answers to: flaky test · triage the flake · chase a flake") {
		t.Fatalf("the cues are not in the milestone: %q", draft)
	}
	// THE CARD IS FENCED OR IT IS NOT A CARD. The room renders this as markdown,
	// which folds single newlines into prose — and single newlines are the whole
	// of a card's alignment.
	if !strings.Contains(draft, "```\n"+subharness.Card(page)+"\n```") {
		t.Fatalf("the card is not fenced in the milestone: %q", draft)
	}
	// AND NONE OF THE ENVELOPE SURVIVES. `"harness"` is the key the page hangs off
	// in the designer's reply, so its presence is the wall of JSON coming back.
	if strings.Contains(draft, `"harness"`) {
		t.Fatalf("the raw envelope was journaled: %q", draft)
	}
	if written[1] != "The review read the draft and left it as written." {
		t.Fatalf("the review milestone reads %q", written[1])
	}

	// AND NONE OF IT IS IN FRONT OF THE MODEL. The thread answers questions about
	// the page that was actually written; a draft it replaced sitting in the same
	// transcript is a wrong answer waiting to be given ([Agent.journalOnly]).
	thread.mu.Lock()
	defer thread.mu.Unlock()
	for _, message := range thread.messages {
		if strings.Contains(messageContentText(message), "The draft is written") {
			t.Fatal("a design milestone reached the thread's own transcript")
		}
	}
}

// AN ATTEMPT THE LAW TURNED DOWN IS A LINE IN THE STORY. A design that took two
// tries reads as two tries afterwards — in the validator's own sentence, which is
// the most specific account of what was wrong — and never as the page it refused.
func TestARefusedAttemptIsJournaledAsASentence(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(ladderWithoutVerify), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	thread, journal := designThreadWithJournal(t)

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model",
		designSeat{id: 4, room: newTaskRoom(), thread: thread}); err != nil {
		t.Fatalf("the repaired design was refused too: %v", err)
	}

	written := journaledMilestones(t, journal)
	if len(written) != 3 {
		t.Fatalf("the journal kept %d milestones, not 3: %v", len(written), written)
	}
	refused := written[0]
	if !strings.HasPrefix(refused, "Attempt 1 was refused: ") {
		t.Fatalf("the refusal reads %q", refused)
	}
	if !strings.Contains(refused, "no verify node") {
		t.Fatalf("the refusal does not say what was wrong: %q", refused)
	}
	if strings.Contains(refused, "\n") {
		t.Fatalf("the refusal is not one paragraph: %q", refused)
	}
	if strings.Contains(refused, `"harness"`) {
		t.Fatalf("the refused page was journaled: %q", refused)
	}
}

// THE REVIEW SAYS WHETHER IT CHANGED ANYTHING, AND WHAT IT NOTICED EITHER WAY.
// The pass label each finding carries is machinery talking about itself, so it
// is dropped; the finding is what a person reads.
func TestTheReviewMilestoneReadsAsProse(t *testing.T) {
	stood := harnessReviewNote(harnessRevision{
		Findings: []harnessFinding{{Pass: "shape", Text: "the two steps are in the right order"}},
	})
	if want := "The review read the draft and left it as written.\n\n- the two steps are in the right order"; stood != want {
		t.Fatalf("a review that changed nothing reads %q", stood)
	}
	if strings.Contains(stood, "shape") {
		t.Fatalf("the pass label reached the person: %q", stood)
	}
	one := harnessReviewNote(harnessRevision{
		Findings: []harnessFinding{{Text: "the verify says nothing about what it checks"}},
		Ops:      []subharness.Op{{Op: subharness.OpSetField, Node: "check", Field: "check", Text: "the report names the failing test"}},
	})
	if want := "The review read the draft and changed 1 thing:\n\n- the verify says nothing about what it checks"; one != want {
		t.Fatalf("a review that changed one thing reads %q", one)
	}
	two := harnessReviewNote(harnessRevision{Ops: []subharness.Op{{Op: subharness.OpSetField}, {Op: subharness.OpSetField}}})
	if want := "The review read the draft and changed 2 things:"; two != want {
		t.Fatalf("a review that changed two things reads %q", two)
	}
}

// WHAT THE ROOM SHOWS OF A FINISHED PAGE IS THE CARD, FENCED, AND NOTHING ELSE.
// The page's own JSON used to be fenced underneath it — labelled `yaml`, over
// bytes subharness.Encode writes as JSON — which put the model's working copy in
// the middle of a person's reading.
func TestTheRoomsCopyOfThePageIsTheFencedCardAlone(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	page, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err != nil {
		t.Fatalf("the design did not land: %v", err)
	}

	recorded := harnessPageThread(page)
	if !strings.Contains(recorded, "The page is written.\n\n```\n"+subharness.Card(page)+"\n```") {
		t.Fatalf("the card is not fenced: %q", recorded)
	}
	if strings.Contains(recorded, "```yaml") || strings.Contains(recorded, "```json") {
		t.Fatalf("the page's own text is still in the room: %q", recorded)
	}
	if strings.Contains(recorded, `"nodes"`) {
		t.Fatalf("the page's JSON is still in the room: %q", recorded)
	}
	if !strings.HasSuffix(recorded, "Nothing is saved yet — the card is up, and it is saved only if it is approved.") {
		t.Fatalf("the closing sentence moved: %q", recorded)
	}
}

// AND THE MODEL STILL HAS THE PAGE, one message along and out of sight. The
// surfaces draw a transcript by role and skip "system" (internal/tui3's
// readRoomJournalTail, agent.go's shapeEntries), which is the whole mechanism:
// the thread reasons off the real page, and the room never draws it.
func TestTheThreadReasonsFromAPageTheRoomNeverDraws(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, harnessPhaseAsking)

	child := node.openRoom().speaker()
	if child == nil {
		t.Fatal("nobody is in the design's room")
	}
	// THE MODEL'S SIDE: the page is in the context it answers from.
	child.mu.Lock()
	var told strings.Builder
	for _, message := range child.messages {
		if message.Role == "system" {
			told.WriteString(messageContentText(message))
		}
	}
	child.mu.Unlock()
	if !strings.Contains(told.String(), `"flake-triage"`) {
		t.Fatalf("the thread cannot see the page it is meant to answer from: %q", told.String())
	}
	if !strings.Contains(told.String(), "Your job in this thread") {
		t.Fatalf("the thread was never told what it is for: %q", told.String())
	}

	// THE PERSON'S SIDE: nothing of it is drawn. Transcript() is the same shaping
	// every surface uses, and the instructions the thread was given are gone from
	// it too — a person walking in reads their own sentence and the card.
	shown := threadText(t, node)
	if strings.Contains(shown, `"nodes"`) || strings.Contains(shown, "Your job in this thread") {
		t.Fatalf("the room drew what only the model was meant to read: %q", shown)
	}
	if !strings.Contains(shown, "Design a reusable sub-harness for this:") {
		t.Fatalf("the person's own sentence is not the opening: %q", shown)
	}

	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)
}

// journaledMilestones is every assistant line one design wrote to a node's
// journal, in order.
func journaledMilestones(t *testing.T, journal string) []string {
	t.Helper()
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
	return written
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
