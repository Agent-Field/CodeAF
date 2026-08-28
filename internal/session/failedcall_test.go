package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE TURN THAT DIED WITH NOTHING WRITTEN DOWN ────────────────────────────
//
// SWE-Marathon run s2, 22:45 UTC. Three calls in fifteen seconds came back with
// no content and no usage — journaled as EMPTY ASSISTANT MESSAGES, which the
// remains-reader then read as a turn that had stopped short and re-opened, three
// times, at mark-reader prices. Then the turn died on:
//
//	error: after 3 retries: API error (400): Provider returned error
//
// and the benchmark cell settled with five hours of budget unspent. Nothing about
// the 400 reached the journal at all — no row, no status, no endpoint, no
// provider name, no upstream body — so the autopsy could say a turn had died and
// nothing whatever about why. These tests are the four things that changed.

// A FAILED CALL IS WRITTEN DOWN, WITH THE UPSTREAM AND ITS OWN WORDS.
func TestAFailedCallIsJournaledWithTheUpstreamAndItsWords(t *testing.T) {
	const said = "input length 97445 is over the ceiling this endpoint accepts"
	path := filepath.Join(t.TempDir(), "session.jsonl")
	refusal := refusalOf(400, "the request was turned away", "Baidu", said)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, refusal },
	}}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events, err := agent.Submit(context.Background(), "port the language server")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	rows := journaledErrors(t, path)
	if len(rows) != 1 {
		t.Fatalf("a failed call wrote %d error rows, want one per attempt: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Status != 400 {
		t.Errorf("status = %d, want the status the refusal arrived under", row.Status)
	}
	if row.Provider != "Baidu" {
		t.Errorf("provider = %q, want the upstream the router named", row.Provider)
	}
	if !strings.Contains(row.Raw, "over the ceiling") {
		t.Errorf("raw = %q, want the upstream's own body", row.Raw)
	}
	if row.Attempt != 1 {
		t.Errorf("attempt = %d, want the rung of the ladder this was", row.Attempt)
	}
	if row.Model != "test/model" {
		t.Errorf("model = %q, want the model the turn was on", row.Model)
	}
	// AND THE ONE TOKEN FIGURE A FAILED CALL HAS: what the request was carrying.
	// The provider counted nothing, so without this the row cannot say whether
	// the refused request was small or enormous.
	if row.Input <= 0 {
		t.Errorf("input = %d, want the session's own estimate of the refused request", row.Input)
	}
	// AND THE PERSON'S LINE NAMES THE UPSTREAM AND WHAT IT SAID.
	if !strings.Contains(refusal.Error(), "Baidu") || !strings.Contains(refusal.Error(), "over the ceiling") {
		t.Errorf("the person is still told nothing about the refusal: %q", refusal.Error())
	}
}

// AND A REQUEST THE ROUTER ITSELF REFUSED IS NOT RETRIED.
//
// The measured 400 said "Provider returned error", which matches the retryable
// pattern, so it was asked again at 2s, 4s and 8s — three deliveries of the same
// request into the same wall. The pattern is asked SECOND now: a 4xx that named
// no upstream is the router reading our own bytes, every endpoint alive will say
// the same thing, and the ladder stops at once.
func TestARequestTheRouterItselfRefusedIsNotRetried(t *testing.T) {
	var calls atomic.Int64
	// Word for word the sentence the measured run drew, minus the metadata: this
	// is the shape that matches `provider.?returned.?error` and used to be retried.
	refusal := refusalOf(400, "Provider returned error", "", "")
	completer := &scriptedCompleter{steps: repeatedStep(4, func(context.Context, []ai.Message) (*ai.Response, error) {
		calls.Add(1)
		return nil, refusal
	})}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })

	began := time.Now()
	events, err := agent.Submit(context.Background(), "port the language server")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if got := calls.Load(); got != 1 {
		t.Fatalf("a malformed request was sent %d times; want once — no endpoint would answer it differently", got)
	}
	// The first rung of the old ladder was a two-second wait. Not waiting it is
	// the whole of the fix, and the clock is the honest witness.
	if spent := time.Since(began); spent >= retryBaseDelay {
		t.Errorf("the turn spent %s before giving up; the ladder was walked anyway", spent)
	}
	// AND ONE ROW PER ATTEMPT MEANS ONE ROW.
	if rows := len(journaledErrors(t, path)); rows != 1 {
		t.Errorf("%d error rows for one attempt, want one", rows)
	}
}

// AND A CALL THAT ANSWERED NOTHING IS A FAILED CALL, NOT AN EMPTY ANSWER.
//
// No words, no tool call, nothing the provider counted. That is not a short
// answer, it is an endpoint that did not answer — and it used to reach the
// journal as an empty assistant message, indistinguishable from a model that had
// finished, and reach the remains-reader as a turn that had stopped short.
func TestACallThatAnsweredNothingIsAFailureAndIsNotReadForWhatRemains(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var remainsAsks atomic.Int64
	// Past the ladder's first rung, so the reader is armed and the ONLY thing
	// that can be keeping it quiet is the turn having broken.
	completer := &scriptedCompleter{steps: brokenSteps(checkpointMarkAt(2), &remainsAsks,
		func(context.Context, []ai.Message) (*ai.Response, error) { return emptyResponse(), nil })}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port the language server and get the golden tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a broken turn was read %d times for what remains; the error is what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a broken turn was carried on: %q", noticeTexts(collected))
	}
	// AND IT IS ON THE FILE AS A FAILURE.
	if rows := journaledErrors(t, path); len(rows) == 0 {
		t.Error("a call that answered nothing left the journal exactly as it found it")
	}
	// AND NOT IN THE TRANSCRIPT AS SOMETHING THE MODEL SAID. An empty assistant
	// message is noise a later step re-reads, and a shape some providers reject
	// ([Agent.keepPartial] refuses to write one for the same reason).
	for _, message := range agent.snapshot() {
		if message.Role == "assistant" && strings.TrimSpace(messageText(message)) == "" && len(message.ToolCalls) == 0 {
			t.Error("a failed call was recorded as an empty assistant message")
			break
		}
	}
}

// AND A TURN WHOSE LAST CALL ERRORED IS NEVER READ FOR WHAT REMAINS EITHER.
//
// THE LAW: A TURN THAT ENDED IN AN ERROR IS NEVER READ FOR WHAT REMAINS. THE
// ERROR IS WHAT REMAINS, and the retry ladder above already owns it. Re-opening
// buys a fourth identical failure and takes the turn away from the one path that
// can report it.
func TestATurnWhoseLastCallErroredIsNotReopened(t *testing.T) {
	var remainsAsks atomic.Int64
	refusal := refusalOf(400, "the request was turned away", "Baidu", "upstream said no")
	completer := &scriptedCompleter{steps: brokenSteps(checkpointMarkAt(2), &remainsAsks,
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, refusal })}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port the language server and get the golden tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn that died on a refusal was read %d times for what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn that died on a refusal was carried on: %q", noticeTexts(collected))
	}
	// AND THE PERSON IS TOLD, in the sentence the refusal now carries.
	var told bool
	for _, event := range collected {
		if event.Kind == EventError && event.Err != nil && strings.Contains(event.Err.Error(), "Baidu") {
			told = true
		}
	}
	if !told {
		t.Error("the turn ended without naming the upstream that refused it")
	}
}

// AND THE STRUCTURAL RULE ITSELF, stated once and read directly: what counts as
// a turn that broke rather than a turn that stopped short.
func TestWhatCountsAsATurnThatBroke(t *testing.T) {
	if !turnBroke(nil) {
		t.Error("nothing came back at all and it was read as an answer")
	}
	if !turnBroke(emptyResponse()) {
		t.Error("an empty step was read as a turn that stopped short")
	}
	if !turnBroke(finishedResponse("", "error")) {
		t.Error("a provider that said it stopped on an error was read as a turn that stopped short")
	}
	// AND A MISSING FINISH REASON IS NOT BREAKAGE. Plenty of endpoints simply do
	// not send one, and reading their silence as a fault would switch the whole
	// carry-on off against them.
	if turnBroke(textResponse("I've finished the parser, next I'll wire the handlers")) {
		t.Error("an ordinary answer from an endpoint that sends no finish reason was read as broken")
	}
	if turnBroke(finishedResponse("done", "stop")) {
		t.Error("a clean stop was read as broken")
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

// brokenSteps works a turn for `rounds` tool calls and then answers with
// whatever `ending` gives — an empty response, or an error. The remains-reader's
// ask is counted rather than answered, because the whole assertion is that it is
// never asked at all.
// ── THE THIRTY SILENT MINUTES ───────────────────────────────────────────────
//
// SWE-Marathon run s10, 11:15:55Z to 11:45:54Z: thirty minutes in which no row
// of any kind was written anywhere. A failed call writes one now (above), tool
// execution was excluded — the last bash returned in four tenths of a second —
// and what was left was a streamed reply nothing bounded. The wall bounds it
// (internal/provider's streamguard.go), and this is the row it leaves behind.
//
// THE ROW HAS TO SAY HOW FAR IT GOT. "The turn stopped" was the whole of the
// last autopsy; a cut that says who was serving, for how long, and with how much
// answer delivered is the difference between a fact and another guess.
func TestAWallCutIsJournaledWithTheEndpointAndHowFarItGot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	cut := &provider.StreamCut{
		Reason:   provider.CutOverrun,
		Waited:   15 * time.Minute,
		Provider: "gusher",
		Ran:      18*time.Minute + 30*time.Second,
		Tokens:   4210,
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, cut },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return finishedResponse("done on the second ask", "stop"), nil
		},
	}}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events, err := agent.Submit(context.Background(), "port the language server")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	rows := journaledErrors(t, path)
	if len(rows) != 1 {
		t.Fatalf("a wall cut wrote %d error rows, want one: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Provider != "gusher" {
		t.Errorf("provider = %q, want the endpoint the stream named", row.Provider)
	}
	if row.Endpoint != "gusher" {
		t.Errorf("endpoint = %q, want the cut to name who was serving when nothing else did", row.Endpoint)
	}
	if row.Output != 4210 {
		t.Errorf("output = %d, want how much answer had arrived before the cut", row.Output)
	}
	if row.DurationMS != (18*time.Minute + 30*time.Second).Milliseconds() {
		t.Errorf("durationMs = %d, want how long the request actually ran", row.DurationMS)
	}
	if !strings.Contains(row.Message, "without finishing") {
		t.Errorf("message = %q, want the cut's own sentence", row.Message)
	}
}

// AND A REFUSAL STILL LEAVES BOTH FIGURES EMPTY. The emptiness law: a zero
// output on a row would read as "the endpoint produced nothing", and only a cut
// can say that honestly — a 400 never opened a stream to produce anything in.
func TestARefusedCallWritesNoDistanceFigures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, refusalOf(400, "the request was turned away", "Baidu", "over the ceiling")
		},
	}}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	events, err := agent.Submit(context.Background(), "port the language server")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	rows := journaledErrors(t, path)
	if len(rows) != 1 {
		t.Fatalf("wrote %d error rows, want one: %+v", len(rows), rows)
	}
	if rows[0].Output != 0 || rows[0].DurationMS != 0 {
		t.Fatalf("a refusal carried distance figures: output=%d durationMs=%d",
			rows[0].Output, rows[0].DurationMS)
	}
}

func brokenSteps(rounds int, asks *atomic.Int64, ending step) []step {
	var done atomic.Int64
	return repeatedStep(rounds+40, func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		if askedForSketch(messages) {
			return textResponse(checkpointChainSketch), nil
		}
		if askedForHandoff(messages) {
			return textResponse("a draft of what is left"), nil
		}
		if askedToWriteHandoff(messages) {
			return textResponse("a brief somebody could work from"), nil
		}
		if askedForRemains(messages) {
			asks.Add(1)
			return textResponse(checkpointNothingLeft), nil
		}
		if call := done.Add(1); call <= int64(rounds) {
			return toolResponse(fmt.Sprintf("call-%d", call), "ls", fmt.Sprintf(`{"path":"./%d"}`, call)), nil
		}
		return ending(ctx, messages)
	})
}

func repeatedStep(count int, one step) []step {
	steps := make([]step, count)
	for index := range steps {
		steps[index] = one
	}
	return steps
}

// emptyResponse is the shape the measured failure produced three times: a
// well-formed response carrying nothing at all.
func emptyResponse() *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant"}}}}
}

// finishedResponse is one answer with the finish reason the provider sent.
func finishedResponse(text, reason string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{
			Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
			FinishReason: reason,
		}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

func journaledErrors(t *testing.T, path string) []journalError {
	t.Helper()
	var rows []journalError
	for _, entry := range journaledEntries(t, path, "error") {
		if entry.Error != nil {
			rows = append(rows, *entry.Error)
		}
	}
	return rows
}

// refusalOf is one provider refusal built by hand, the way the wire delivers it:
// the status, the router's sentence, the upstream it named, and what that
// upstream itself said.
func refusalOf(status int, message, upstream, raw string) *provider.APIError {
	return &provider.APIError{Status: status, Message: message, Provider: upstream, Raw: raw}
}
