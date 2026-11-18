package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── a turn that stops on its own interim update ─────────────────────────────

// A TURN WHOSE LAST REPLY OPENS WITH THE INTERIM MARKER AND CALLS NOTHING IS
// SENT BACK TO WORK. This is the measured shape from the connected-canvas eval
// (run 5, board-deck): the model set up five empty slide frames, wrote
// "[update] … Next I write each slide's HTML in one pass", and the turn sealed
// with every slide blank. The marker is the protocol's own word for "not the
// end", so the loop reads it and carries on without spending a reader, on a
// typed turn the end-of-turn reader never looks at.
func TestATurnThatStopsOnItsOwnUpdateIsSentBackToWork(t *testing.T) {
	recorder := &blockRecorder{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "read", `{"path":"notes.md"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("[update] The canvas already holds the facts I need and the five slide frames are in place. Next I write each slide's HTML in one pass."), nil
		},
		// The nudge lands and the model does the work it announced.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "read", `{"path":"slides.md"}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "The five slides are written.")
			return textResponse("The five slides are written."), nil
		},
	}}
	agent, _ := newTestAgent(t, recorder, nil)

	events, err := agent.Submit(context.Background(), "5 slide deck for the board meeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := recorder.requests(); got != 4 {
		t.Fatalf("the loop made %d requests; want 4 — a call, the update, the call it announced, the answer", got)
	}
	transcript := transcriptText(agent)
	if n := strings.Count(transcript, checkpointUpdateNudgeLead); n != 1 {
		t.Errorf("the turn was sent back %d times; want once:\n%s", n, transcript)
	}
	// THE CONTINUATION REACHES THE MODEL, as the newest message of the very
	// next request, right after the update it answers.
	recorder.mu.Lock()
	next := recorder.sent[2]
	recorder.mu.Unlock()
	if last := next[len(next)-1]; last.Role != "user" || messageText(last) != checkpointUpdateNudgeLead {
		t.Errorf("the request after the update ended on %s %q; want the continuation", last.Role, messageText(last))
	}
	if before := next[len(next)-2]; before.Role != "assistant" || !strings.HasPrefix(messageText(before), UserUpdatePrefix) {
		t.Errorf("the continuation followed %s %q; want the update it answers", before.Role, messageText(before))
	}
	// AND THE ANSWER THE WORK ENDED ON IS THE ONE THE PERSON IS SHOWN.
	if streamed := streamedText(collected); !strings.HasSuffix(strings.TrimSpace(streamed), "The five slides are written.") {
		t.Errorf("the turn did not stream its final answer; streamed %q", streamed)
	}
	if !strings.HasSuffix(strings.TrimSpace(transcript), "assistant: The five slides are written.") {
		t.Errorf("the turn did not end on its final answer:\n%s", transcript)
	}
	if !saidSomething(noticeTexts(collected), checkpointUpdateNudgeNote) {
		t.Errorf("nobody told the person why the turn went on; notices were %q", noticeTexts(collected))
	}
	// THE PERSON NEVER SEES THE CONTINUATION AS THEIR OWN WORDS. It is the
	// harness speaking, recorded in the user lane only because that is the one
	// lane the model can be told something in.
	if !isContinuationContext(checkpointUpdateNudgeLead) {
		t.Error("the update nudge would be drawn as a line the person typed")
	}
	// THE APP LEAVES THE LINE OUT, as it does every line that sends a turn back
	// (cmd/codeaf/app.go's pump asks [IsCarryOnNotice]); the terminal draws it
	// as one dim notice.
	if !IsCarryOnNotice(checkpointUpdateNudgeNote) {
		t.Error("the update nudge's notice is not one of the lines that send a turn back")
	}
}

// streamedText is every word the turn streamed to the person, in order.
func streamedText(events []Event) string {
	var text strings.Builder
	for _, event := range events {
		if event.Kind == EventTextDelta {
			text.WriteString(event.Text)
		}
	}
	return text.String()
}

// AND IT IS BOUNDED, AT TWO. A model that answers every nudge with another
// promise is sent back twice and then the turn ends, rather than paying a
// request for every sentence of it. THE NUMBERS ARE WRITTEN OUT and not read
// off [checkpointUpdateNudgeCap], so a change to the cap fails here and is
// made on purpose, with the manual's "at most twice" beside it.
func TestTheUpdateNudgeIsBoundedATurn(t *testing.T) {
	var steps []step
	for range 5 {
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("[update] Next I write the slides."), nil
		})
	}
	recorder := &blockRecorder{steps: steps}
	agent, _ := newTestAgent(t, recorder, nil)

	events, err := agent.Submit(context.Background(), "5 slide deck for the board meeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if got := recorder.requests(); got != 3 {
		t.Fatalf("the loop made %d requests; want 3 — the update, two nudges' answers, and then the turn ends", got)
	}
	if n := strings.Count(strings.Join(noticeTexts(collected), "\n"), checkpointUpdateNudgeNote); n != 2 {
		t.Errorf("the person was told %d times; want 2", n)
	}
	if n := strings.Count(transcriptText(agent), checkpointUpdateNudgeLead); n != 2 {
		t.Errorf("the model was sent back %d times; want 2", n)
	}
}

// A WOKEN TURN THAT STOPS ON AN UPDATE IS SENT BACK TOO. Nobody typed it — a
// job's failure woke it — and nobody is sitting in front of it to carry on
// from the update, which is the turn this matters for most.
func TestAWokenTurnThatStopsOnItsOwnUpdateIsSentBack(t *testing.T) {
	a, client, clock, wakes, _ := jobWakeFixture(t)
	client.steps = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("[update] The build failed on one import. Next I fix it and run the build again."), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The import is fixed and the build is green."), nil
		},
	}
	one := settlementJob(t, a.jobs)
	a.jobs.settleExit(one, 7)
	clock.advance(jobWakeDelay)
	var collected []Event
	select {
	case stream, ok := <-wakes:
		if !ok {
			t.Fatal("wake lane closed")
		}
		collected = collect(t, stream)
	case <-time.After(10 * time.Second):
		t.Fatal("the job's ending never woke a turn")
	}
	if got := conversationRequests(client); got != 2 {
		t.Fatalf("the woken turn made %d requests; want 2 — the update and the answer after the nudge", got)
	}
	if last := client.request(1); messageText(last[len(last)-1]) != checkpointUpdateNudgeLead {
		t.Errorf("the request after the update ended on %q; want the continuation", messageText(last[len(last)-1]))
	}
	if !saidSomething(noticeTexts(collected), checkpointUpdateNudgeNote) {
		t.Errorf("the woken turn was not sent back; notices were %q", noticeTexts(collected))
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// AN UPDATE THAT IS WAITING ON WORK ALREADY OUT IS NOT SENT BACK. "I've
// started the render and will report when it lands" is the marker used
// truthfully: the job's exit wakes the conversation by itself, and a nudge
// would only buy a poll of it. This is the waiting the end-of-turn reader
// already honours ([Agent.turnEndsWaiting]), asked BEFORE the update is read
// as a stop.
func TestAnUpdateWaitingOnRunningWorkIsNotSentBack(t *testing.T) {
	recorder := &blockRecorder{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("[update] I've started the render and will report when it lands."), nil
		},
	}}
	agent, _ := newTestAgent(t, recorder, nil)
	settlementJob(t, agent.jobs)
	if !agent.turnIsWaitingOnItsOwnWork() {
		t.Fatal("the fixture's job is not running")
	}

	events, err := agent.Submit(context.Background(), "render the board deck")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if got := recorder.requests(); got != 1 {
		t.Fatalf("the loop made %d requests; want 1 — a turn waiting on its own job ends", got)
	}
	if saidSomething(noticeTexts(collected), checkpointUpdateNudgeNote) {
		t.Error("an update waiting on running work was sent back to poll it")
	}
}

// AND ONLY THE MARKER, ONLY WHERE IT IS A MARKER. An ordinary final answer, an
// answer that merely mentions the marker, and an update that ends by asking the
// person something all end the turn exactly as before: the first two are
// answers, and the last is waiting on somebody else.
func TestOnlyAnUnaskedLeadingUpdateIsSentBack(t *testing.T) {
	for name, reply := range map[string]string{
		"a final answer":            "The five slides are written.",
		"a mention of the marker":   "Done. Interim notes start with " + UserUpdatePrefix + " and this is not one.",
		"an update asking a person": "[update] The frames are in place. Should the deck use the navy or the cream theme?",
		// The question mark is not one character: full-width (U+FF1F) and
		// Arabic (U+061F) end a question as surely as the ASCII one.
		"an update asking in Chinese": "[update] 幻灯片已建好。你想用哪个主题？",
		"an update asking in Arabic":  "[update] الشرائح جاهزة. أي سمة تريد؟",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &blockRecorder{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse(reply), nil
				},
			}}
			agent, _ := newTestAgent(t, recorder, nil)
			events, err := agent.Submit(context.Background(), "5 slide deck for the board meeting")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collected := collect(t, events)
			if got := recorder.requests(); got != 1 {
				t.Fatalf("the loop made %d requests; want 1 — this reply ends the turn", got)
			}
			if saidSomething(noticeTexts(collected), checkpointUpdateNudgeNote) {
				t.Error("a reply that is not an unasked interim update was sent back")
			}
		})
	}
}
