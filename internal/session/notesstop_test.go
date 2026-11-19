package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a turn that stops on the note the harness asked for ─────────────────────

// The stalled reply, as deepseek-v4.1-flash wrote it on the connected-canvas
// qloop (baseline, pipeline-dashboard t1): the very next reply after the
// `[silent]` note, unmarked, naming its own next step, with no tool call.
const stalledNote = "Where I am: read the canvas skill and the Pipeline sheet (40 deals), then built a Summary tab " +
	"on the same sheet with live formulas. Next: make two charts from the Summary, then assemble the dashboard doc page."

// silentStretch is the script up to the first rung of the silent ladder: as
// many quiet tool-using replies as it takes for the `[silent]` note to land
// before the next request.
func silentStretch() []step {
	steps := make([]step, 0, silentThreshold(0))
	for index := 0; index < silentThreshold(0); index++ {
		steps = append(steps, silentCall(index))
	}
	return steps
}

// A TURN WHOSE REPLY TO THE HARNESS'S OWN REQUEST FOR A NOTE IS ONLY THAT NOTE
// IS SENT BACK TO WORK. This replays the measured shape: six quiet steps, the
// `[silent]` note, the note alone in reply — and where the turn used to seal
// on it with the dashboard never built, it now continues and finishes.
func TestATurnThatStopsOnTheNoteItWasAskedForIsSentBackToWork(t *testing.T) {
	steps := silentStretch()
	quiet := len(steps)
	steps = append(steps,
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			return textResponse(stalledNote), nil
		},
		spokenCall(quiet, "[update] Making the two charts now."),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The pipeline dashboard is on the canvas: two charts and the totals."), nil
		},
	)
	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "can u make a dashboard for the pipeline sheet")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got, want := completer.requests(), quiet+3; got != want {
		t.Fatalf("the loop made %d requests; want %d — the quiet steps, the note, the call it announced, the answer", got, want)
	}
	// THE FIXTURE IS THE MEASURED SHAPE: the note was answering the `[silent]`
	// note, which was the newest message of the request that produced it.
	asked := completer.request(quiet)
	if last := asked[len(asked)-1]; last.Role != "user" || !strings.HasPrefix(messageText(last), silentNoteOpening) {
		t.Fatalf("the stalled note answered %s %q; want the silent ladder's note", last.Role, messageText(last))
	}
	// THE CONTINUATION REACHES THE MODEL right after the note it answers.
	next := completer.request(quiet + 1)
	if last := next[len(next)-1]; last.Role != "user" || messageText(last) != checkpointNotesNudgeLead {
		t.Errorf("the request after the note ended on %s %q; want the continuation", last.Role, messageText(last))
	}
	if before := next[len(next)-2]; before.Role != "assistant" || messageText(before) != stalledNote {
		t.Errorf("the continuation followed %s %q; want the note it answers", before.Role, messageText(before))
	}
	transcript := transcriptText(agent)
	if n := strings.Count(transcript, checkpointNotesNudgeLead); n != 1 {
		t.Errorf("the turn was sent back %d times; want once:\n%s", n, transcript)
	}
	if got := int(ran.Load()); got != quiet+1 {
		t.Errorf("the tool ran %d times; want %d — the work the note announced was never done", got, quiet+1)
	}
	if !strings.HasSuffix(strings.TrimSpace(transcript), "assistant: The pipeline dashboard is on the canvas: two charts and the totals.") {
		t.Errorf("the turn did not end on its final answer:\n%s", transcript)
	}
	if !saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
		t.Errorf("nobody told the person why the turn went on; notices were %q", noticeTexts(collected))
	}
	if !isContinuationContext(checkpointNotesNudgeLead) {
		t.Error("the notes nudge would be drawn as a line the person typed")
	}
	if !IsCarryOnNotice(checkpointNotesNudgeNote) {
		t.Error("the notes nudge's notice is not one of the lines that send a turn back")
	}
}

// THE SAME SHAPE ONE RUNG LATER: the write-your-notes rule held a batch, its
// `[held]` demand stood in for the batch's results, and the model answered it
// with the note alone (run 2, cafe-site t1, minus the marker).
func TestANoteWrittenInAnswerToAHeldBatchIsSentBackToWork(t *testing.T) {
	rung := enforcedRungAt()
	steps := make([]step, 0, rung+4)
	for index := 0; index <= rung; index++ {
		steps = append(steps, silentCall(index))
	}
	steps = append(steps,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Where things stand: the Home and Visit pages exist. Next I'm stripping the duplicate sections."), nil
		},
		spokenCall(rung+1, "[update] Stripping the duplicates."),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The cafe site is done: Home and Visit, no duplicates."), nil
		},
	)
	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "can we get a simple website for the cafe")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if len(heldToolResults(agent)) != 1 {
		t.Fatalf("the fixture never reached the held rung: %v", heldToolResults(agent))
	}
	if got, want := completer.requests(), rung+4; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if !saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
		t.Errorf("the note answering the held batch was not sent back; notices were %q", noticeTexts(collected))
	}
	if !strings.HasSuffix(strings.TrimSpace(transcriptText(agent)), "assistant: The cafe site is done: Home and Visit, no duplicates.") {
		t.Errorf("the turn did not end on its final answer:\n%s", transcriptText(agent))
	}
}

// A REPLY THAT REALLY WAS THE FINAL ANSWER COSTS ONE SHORT CALL AND KEEPS ITS
// PLACE. The shape is read and not the words, so a finished answer written
// right after the note was asked for is asked once; the model answers with
// [NoChangeReply], the turn ends, and nothing is sent back again.
func TestAFinalAnswerAfterTheNoteRequestEndsOnTheToken(t *testing.T) {
	steps := silentStretch()
	quiet := len(steps)
	final := "Rota built: the week of 23 to 29 November is filled as far as the hours allow."
	steps = append(steps,
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(final), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(NoChangeReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("unreachable: the turn should have ended on the token"), nil
		},
	)
	var ran atomic.Int64
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, runCounter("look", &ran))

	events, err := agent.Submit(context.Background(), "do the rota for next week")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if got, want := completer.requests(), quiet+2; got != want {
		t.Fatalf("the loop made %d requests; want %d — the answer, one continuation, the token", got, want)
	}
	if n := strings.Count(strings.Join(noticeTexts(collected), "\n"), checkpointNotesNudgeNote); n != 1 {
		t.Errorf("the person was told %d times; want once", n)
	}
	if !strings.Contains(transcriptText(agent), "assistant: "+final) {
		t.Errorf("the finished answer is gone from the record:\n%s", transcriptText(agent))
	}
}

// AND NOTHING IS SPENT WHERE THE SHAPE IS NOT THERE. An ordinary final answer
// and a question to the person end the turn on the first reply when no note was
// asked for; a question to the person ends it even right after the request
// for a note, because it is waiting on somebody else.
func TestOnlyANoteAnsweringTheRequestForOneIsSentBack(t *testing.T) {
	t.Run("a final answer", func(t *testing.T) {
		completer := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("c1", "look", `{"path":"sheet"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("The dashboard is on the canvas. Next time new numbers arrive, it updates by itself."), nil
			},
		}}
		var ran atomic.Int64
		agent := loopAgent(t, completer, runCounter("look", &ran))
		events, err := agent.Submit(context.Background(), "can u make a dashboard for the pipeline sheet")
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		collected := collect(t, events)
		if got := completer.requests(); got != 2 {
			t.Fatalf("the loop made %d requests; want 2 — this reply ends the turn", got)
		}
		if saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
			t.Error("an ordinary final answer was sent back")
		}
	})
	for name, reply := range map[string]string{
		"a question to the person":            "The Pipeline sheet has two Amount columns. Which one should the charts use?",
		"a question to the person in Chinese": "表里有两列金额。图表用哪一列？",
	} {
		t.Run(name+" after the note request", func(t *testing.T) {
			steps := silentStretch()
			quiet := len(steps)
			steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse(reply), nil
			})
			var ran atomic.Int64
			completer := &scriptedCompleter{steps: steps}
			agent := loopAgent(t, completer, runCounter("look", &ran))
			events, err := agent.Submit(context.Background(), "can u make a dashboard for the pipeline sheet")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collected := collect(t, events)
			if got, want := completer.requests(), quiet+1; got != want {
				t.Fatalf("the loop made %d requests; want %d — a question ends the turn", got, want)
			}
			if saidSomething(noticeTexts(collected), checkpointNotesNudgeNote) {
				t.Error("a question to the person was sent back")
			}
		})
	}
}

// answeredNotesAdvice reads only the reply that answers the request itself.
// Each row is the tail of a transcript, newest last.
func TestAnsweredNotesAdviceReadsOnlyTheReplyToTheRequest(t *testing.T) {
	silent := textMessage("user", nudgeNote(nudge{silent: true, count: 6, silentRung: 1}))
	held := ai.Message{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: writeNotesRule{}.demand()}}}
	result := ai.Message{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: "Summary tab written"}}}
	note := textMessage("assistant", stalledNote)
	person := textMessage("user", "can u make a dashboard for the pipeline sheet")
	for name, row := range map[string]struct {
		messages []ai.Message
		want     bool
	}{
		"after the silent note":         {[]ai.Message{person, silent, note}, true},
		"after the held demand":         {[]ai.Message{person, held, note}, true},
		"after a real tool result":      {[]ai.Message{person, result, note}, false},
		"after the person":              {[]ai.Message{silent, person, note}, false},
		"after the notes nudge itself":  {[]ai.Message{silent, note, textMessage("user", checkpointNotesNudgeLead), note}, false},
		"the token after the silent":    {[]ai.Message{person, silent, textMessage("assistant", NoChangeReply)}, false},
		"an empty reply after the note": {[]ai.Message{person, silent, textMessage("assistant", "  ")}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := answeredNotesAdvice(row.messages); got != row.want {
				t.Errorf("answeredNotesAdvice = %v; want %v", got, row.want)
			}
		})
	}
}

// THE REQUEST FOR A NOTE ASKS FOR IT BESIDE THE NEXT CALL. The old wording —
// "before your next tool call, write a short visible note" and "write that
// note now, as an ordinary visible message" — was obeyed to the letter with a
// note and no call, which is the stop this file is about.
func TestTheRequestForANoteAsksForItBesideTheNextCall(t *testing.T) {
	for name, text := range map[string]string{
		"the silent note":  nudgeNote(nudge{silent: true, count: 6, silentRung: 1}),
		"the held demand":  writeNotesRule{}.demand(),
		"the last warning": nudgeNote(nudge{silent: true, count: 12, silentRung: silentEnforceRung}),
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(text, "tool call") || !strings.Contains(text, "ends your turn") {
				t.Errorf("the request does not say the note goes with a tool call: %q", text)
			}
			if strings.Contains(text, "Before your next tool call") || strings.Contains(text, "as an ordinary visible message") {
				t.Errorf("the request still asks for the note on its own: %q", text)
			}
		})
	}
}
