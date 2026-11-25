package session

import (
	"strings"
	"sync/atomic"
	"testing"
)

// failedThenQuestion is the measured sales-dashboard t3 shape (qloop run 12)
// with the reply door: steps that failed, nothing written, and a reply that
// ends by asking the person.
const failedThenQuestion = "Both charts already plot net. Which one did you mean?"

// THE MEASURED SHAPE: a failed step, nothing written, a question to the person.
// The model is handed the failed step once and told to redo it or say plainly
// what failed; the reply it writes is the one the turn ends on.
func TestAQuestionAfterFailedStepsIsSentBackOnceWithTheFailures(t *testing.T) {
	var broke atomic.Int64
	final := "Two lookups failed (unknown command) and nothing on the page changed. Both charts already plot net — which one did you mean?"
	agent, completer, _ := replyCheckAgent(t, []step{
		callStep("f1", "probe", `{}`),
		replyStep(failedThenQuestion),
		replyStep(final),
		unreachable,
	}, "NONE", changeTool("probe", &broke, true))
	collected := runTurnOn(t, agent, "the chart is wrong, use net")
	if got, want := completer.requests(), 3; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	sent := completer.request(2)
	lead := messageText(sent[len(sent)-1])
	if !isFailureCheckLead(lead) || !strings.Contains(lead, "- probe: usage: unknown command") {
		t.Fatalf("the send-back was %q; want the failure check naming the failed step", lead)
	}
	if !saidSomething(noticeTexts(collected), failureCheckNote) {
		t.Errorf("the person was not told; notices were %q", noticeTexts(collected))
	}
	if got := lastAssistantText(agent); got != final {
		t.Errorf("the turn ended on %q; want the reply that says what failed", got)
	}
}

// AND IT DOES NOT LOOP: the second question ends the turn, and [NoChangeReply]
// keeps the first.
func TestTheFailureCheckIsOnceATurn(t *testing.T) {
	for name, second := range map[string]string{
		"asked again":  failedThenQuestion + " (again)",
		"stands by it": NoChangeReply,
	} {
		t.Run(name, func(t *testing.T) {
			var broke atomic.Int64
			agent, completer, _ := replyCheckAgent(t, []step{
				callStep("f1", "probe", `{}`),
				replyStep(failedThenQuestion),
				replyStep(second),
				unreachable,
			}, "NONE", changeTool("probe", &broke, true))
			runTurnOn(t, agent, "the chart is wrong, use net")
			if got, want := completer.requests(), 3; got != want {
				t.Fatalf("the loop made %d requests; want %d — one send-back at most", got, want)
			}
			if n := strings.Count(transcriptText(agent), failureCheckLeadOpening); n != 1 {
				t.Errorf("the send-back reached the transcript %d times; want 1", n)
			}
			if second == NoChangeReply && lastAssistantText(agent) != failedThenQuestion {
				t.Errorf("the standing reply is %q; want the first", lastAssistantText(agent))
			}
		})
	}
}

// NOTHING IS SPENT WHERE THE SHAPE IS NOT THERE: no failure, or no question.
func TestTheFailureCheckNeedsAFailureAndAQuestion(t *testing.T) {
	for name, row := range map[string]struct {
		fails bool
		reply string
	}{
		"a question with nothing failed": {false, failedThenQuestion},
		"a failure, then an answer":      {true, "Switched the KPI card to net."},
	} {
		t.Run(name, func(t *testing.T) {
			var ran atomic.Int64
			agent, completer, _ := replyCheckAgent(t, []step{
				callStep("f1", "probe", `{}`),
				replyStep(row.reply),
				unreachable,
			}, "NONE", changeTool("probe", &ran, row.fails))
			collected := runTurnOn(t, agent, "the chart is wrong, use net")
			if got, want := completer.requests(), 2; got != want {
				t.Fatalf("the loop made %d requests; want %d", got, want)
			}
			if saidSomething(noticeTexts(collected), failureCheckNote) {
				t.Errorf("the turn was sent back")
			}
		})
	}
}

// THE CARD DOOR: an `ask` after failed steps is held once before anything is
// drawn, the held answer is not a refusal the ask nudge would argue with, and
// the turn's next ask is drawn.
func TestAnAskAfterFailedStepsIsHeldOnceAndNotDrawn(t *testing.T) {
	var broke atomic.Int64
	acted := "Switched the Mar gross card to net; the bars already plotted net."
	ask := `{"head":"Both bar charts already read net columns — which chart is the wrong one?","kind":"choice",` +
		`"reason":"Both bar charts already plot net, and guessing wrong would rewrite work you did not ask for.","stakes":"reversible","form":"card",` +
		`"options":[{"key":"1","label":"The Mar gross sales KPI card","body":"The only graphic that plots gross."},{"key":"2","label":"The region bar chart","body":"It already plots net."}],` +
		`"pick":{"key":"1","reason":"It is the only graphic whose numbers are gross.","confidence":"unsure","wouldChange":"Any chart you point at."}}`
	agent, completer, _ := replyCheckAgent(t, []step{
		callStep("f1", "probe", `{}`),
		callStep("a1", "ask", ask),
		replyStep(acted),
		unreachable,
	}, "NONE", changeTool("probe", &broke, true))
	agent.config.Interactive = true
	agent.tools = append(agent.tools, agent.askTool())
	collected := runTurnOn(t, agent, "the chart is wrong, use net")
	if got, want := completer.requests(), 3; got != want {
		t.Fatalf("the loop made %d requests; want %d\n%s", got, want, transcriptText(agent))
	}
	sent := completer.request(2)
	held := messageText(sent[len(sent)-1])
	if !strings.HasPrefix(held, askHeldAfterFailuresLead) || !strings.Contains(held, "- probe: usage") {
		t.Fatalf("ask answered %q; want the hold naming the failed step", held)
	}
	if !strings.HasSuffix(held, askHeldAfterFailuresClosing) {
		t.Errorf("the hold does not end on the instruction to ask in the person's words:\n%s", held)
	}
	if _, refused := askRefusedAndNotRetried(agent.snapshot()); refused {
		t.Errorf("the hold reads as a refusal, so the ask nudge would send the model round to re-ask")
	}
	for _, event := range collected {
		if event.Kind == EventQuestion {
			t.Fatalf("a card was drawn: %+v", event)
		}
	}
	if agent.holdAskAfterFailures() != "" {
		t.Errorf("a second ask in the same turn would be held again")
	}
}

// THE PERSON GETS THE OUTCOME, NOT THE MACHINERY (qloop run31, hiring t2). Told
// to "say plainly which steps failed", the model put a tool's name and its
// error string on the card. Both doors now ask for what changed, what did not,
// what is not done yet and what is needed — the honesty is kept — and forbid
// naming a tool or a command or quoting an error.
func TestTheFailureCheckAsksForTheOutcomeInThePersonsWords(t *testing.T) {
	for door, closing := range map[string]string{
		"reply": failureCheckLeadClosing,
		"ask":   askHeldAfterFailuresClosing,
	} {
		for _, want := range []string{
			"in the person's own words",
			"what you changed", "what you did not change", "what is not done yet", "what you need from them",
			"Never name a tool or a command", "never quote an error",
		} {
			if !strings.Contains(closing, want) {
				t.Errorf("the %s door's instruction misses %q:\n%s", door, want, closing)
			}
		}
		if strings.Contains(closing, "which steps failed") {
			t.Errorf("the %s door still asks the model to name the failed steps to the person:\n%s", door, closing)
		}
	}
}
