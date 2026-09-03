package session

// The laws of the splice (steer.go), one test each:
//
//	(a) a steer cuts the current generation and lands at the boundary it makes,
//	    as user content of the SAME turn — the model reads the question, the
//	    partial work, then the correction
//	(b) several steers land in the order they were sent
//	(c) a steer that reaches the last generation before it seals is consumed by
//	    the boundary its cut creates
//	(d) a steer that missed every boundary of a turn that ANSWERED falls through
//	    onto the follow-up queue and asks its own question, on the same stream
//	(e) a steer that fell through onto an INTERRUPTED turn is dropped with that
//	    queue, said out loud first and written down — never silently, and never
//	    into the transcript of the turn it missed
//	(f) the same on a turn that FAULTED
//	(g) a steer sent when nothing is running is refused
//	(h) the record says which of the two happened, and a surface reads it back
//	(i) a transcript with no steers in it loads exactly as it always did
//	(j) a node's room steer is untouched by any of it

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// heldTurn is a turn parked inside its first provider call, which is the only
// moment a steer can be sent into: entered says the step has begun, release
// lets it finish.
type heldTurn struct {
	entered chan struct{}
	release chan struct{}
}

func newHeldTurn() *heldTurn {
	return &heldTurn{entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldTurn) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}
}

// mustSteer sends one steer into a running turn and fails rather than returning
// an error, because every test below is about what happens AFTER it is taken.
func mustSteer(t *testing.T, agent *Agent, words string) <-chan Event {
	t.Helper()
	stream, err := agent.Steer(words)
	if err != nil {
		t.Fatalf("Steer(%q): %v", words, err)
	}
	return stream
}

// steerEvents is the three steer kinds off one stream, in the order they
// arrived, spelled as their words so a failure says what happened rather than
// which integers it saw.
func steerEvents(collected []Event) []string {
	var out []string
	for _, event := range collected {
		switch event.Kind {
		case EventSteerAccepted:
			out = append(out, "accepted:"+event.Steer.Words)
		case EventSteerConsumed:
			out = append(out, "consumed:"+event.Steer.Words)
		case EventSteerFellThrough:
			out = append(out, "fell:"+event.Steer.Words)
		}
	}
	return out
}

// userLines is every user-role message in one request, in order — the shape
// every assertion about "what the model was actually sent" is made against.
func userLines(messages []ai.Message) []string {
	var out []string
	for _, message := range messages {
		if message.Role == "user" {
			out = append(out, messageText(message))
		}
	}
	return out
}

// journalEntries reads a session file back as its lines.
func journalEntries(t *testing.T, path string) []sessionEntry {
	t.Helper()
	var entries []sessionEntry
	for _, line := range readLines(t, path) {
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

// ── (a) the splice lands at the boundary, as the same turn's user content ───

func TestASteerLandsAtTheNextBoundaryAsSameTurnUserContent(t *testing.T) {
	started := make(chan struct{})
	var reflectedArgs string
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "I started in the wrong place")
			close(started)
			<-ctx.Done()
			return &ai.Response{Usage: &ai.Usage{PromptTokens: 11, CompletionTokens: 4}}, ctx.Err()
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if strings.Contains(strings.Join(userLines(messages), "\n"), "count the files") {
				reflectedArgs = `{"path":"count-the-files"}`
			} else {
				reflectedArgs = `{"path":"."}`
			}
			return toolResponse("steered-ls", "ls", reflectedArgs), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("counted them too"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "list the workspace")
	<-started
	steered := mustSteer(t, agent, "count the files while you are there")

	collect(t, turn)
	fromSteer := collect(t, steered)

	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want 3 — the steer and its tool must ride the SAME turn", completer.requests())
	}
	second := completer.request(1)
	// The question, the work so far, then the correction: one turn, in order.
	if got, want := rolesOf(second), []string{"system", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v — the steer lands at the step boundary", got, want)
	}
	if got := messageText(second[len(second)-2]); got != "I started in the wrong place" {
		t.Fatalf("partial assistant = %q, want the streamed text", got)
	}
	if got, want := userLines(second), []string{"list the workspace", "count the files while you are there"}; !equalStrings(got, want) {
		t.Fatalf("user content = %v, want %v — the question, then the steer", got, want)
	}
	// M8: the request after the splice does not merely contain the correction;
	// its next tool call is chosen from the steered word and the loop continues.
	if reflectedArgs != `{"path":"count-the-files"}` {
		t.Fatalf("the next tool call ignored the steered word: %s", reflectedArgs)
	}
	if got := roleText(completer.request(2), "tool"); !strings.Contains(got, "count-the-files") {
		t.Fatalf("the subsequent tool result does not reflect the steered call: %q", got)
	}
	// And the person watching their own correction is told it landed.
	if got, want := steerEvents(fromSteer), []string{
		"accepted:count the files while you are there",
		"consumed:count the files while you are there",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	// Nothing was interrupted and nothing was thrown away: the turn ended
	// normally, on its own stream.
	if last := fromSteer[len(fromSteer)-1]; last.Kind != EventTurnDone {
		t.Fatalf("steer stream ended with %v, want EventTurnDone", last.Kind)
	}
	if last := fromSteer[len(fromSteer)-1]; last.Usage.Input < 21 || last.Usage.Output < 9 {
		t.Fatalf("turn usage = %+v, want the cut and finishing attempts both charged", last.Usage)
	}
}

func TestASteerDuringReasoningOnlyDropsTheUnfinishedSidecar(t *testing.T) {
	started := make(chan struct{})
	var carried []provider.MessageReasoning
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamThinking, "")
			provider.EmitEvent(ctx, provider.StreamEvent{
				Kind: provider.StreamReasoning, Delta: "private first half", ReasoningField: "reasoning_content",
			})
			close(started)
			<-ctx.Done()
			return &ai.Response{Usage: &ai.Usage{PromptTokens: 8, CompletionTokens: 3}}, ctx.Err()
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			carried = provider.MessageReasoningFrom(ctx)
			return textResponse("continued cleanly"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "think this through")
	<-started
	mustSteer(t, agent, "use the simpler route")
	collect(t, turn)

	second := completer.request(1)
	if got, want := rolesOf(second), []string{"system", "user", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v", got, want)
	}
	for _, sidecar := range carried {
		if strings.Contains(sidecar.Text, "private first half") {
			t.Fatalf("cut reasoning leaked into the next request: %#v", carried)
		}
	}
}

func TestASteerDropsAnIncompleteToolCallAndKeepsItsText(t *testing.T) {
	started := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "I found the file. ")
			provider.EmitEvent(ctx, provider.StreamEvent{
				Kind: provider.StreamToolCallForming, Index: 0, ID: "cut-call", Tool: "read",
				Delta: `{"path":"internal/session/steer.go`,
			})
			close(started)
			<-ctx.Done()
			return &ai.Response{Usage: &ai.Usage{PromptTokens: 7, CompletionTokens: 4}}, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("continued without the broken call"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "inspect the session")
	<-started
	mustSteer(t, agent, "use the other file")
	collect(t, turn)

	second := completer.request(1)
	if got, want := rolesOf(second), []string{"system", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v", got, want)
	}
	cut := second[len(second)-2]
	if len(cut.ToolCalls) != 0 {
		t.Fatalf("the cut assistant kept an incomplete tool call: %#v", cut.ToolCalls)
	}
	text := messageText(cut)
	if !strings.Contains(text, "I found the file.") ||
		!strings.Contains(text, "incomplete tool call dropped when you steered") {
		t.Fatalf("cut assistant text = %q, want the partial and the dropped-call account", text)
	}
}

// ── (b) several steers keep the order they were typed in ────────────────────

// Two sent inside one step arrive together at the boundary that step ends on,
// and one sent after it arrives at the next — which is the same rule twice
// (steer.go): every steer lands at the FIRST boundary after it was sent.
func TestSteersLandInTheOrderTheyWereSent(t *testing.T) {
	first := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "looking")
			close(first)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("both folded in"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "look around")
	<-first
	mustSteer(t, agent, "one")
	mustSteer(t, agent, "two")
	collect(t, turn)

	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2", completer.requests())
	}
	// Both typed into one generation arrive at its cut boundary, in order.
	if got, want := userLines(completer.request(1)), []string{"look around", "one", "two"}; !equalStrings(got, want) {
		t.Fatalf("second request user content = %v, want %v", got, want)
	}
}

// ── (c) the last generation is still a boundary ─────────────────────────────

// A steer typed at a turn whose last request has already gone out has no
// boundary left. It must not vanish, and it must not be written into the turn
// it missed: it becomes an ordinary waiting message, and the stream the person
// is already holding carries the turn it starts.
func TestASteerCutsEvenTheLastGenerationBeforeItCanSeal(t *testing.T) {
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			// A text-only answer: this turn has no next step to steer into.
			return textResponse("here is the list"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("and here are the counts"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "list the workspace")
	held.wait(t)
	steered := mustSteer(t, agent, "count them as well")
	close(held.release)

	collect(t, turn)
	fromSteer := collect(t, steered)

	if got, want := steerEvents(fromSteer), []string{
		"accepted:count them as well",
		"consumed:count them as well",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	// One turn was cut and continued; the steer never became a second turn.
	var turns int
	for _, event := range fromSteer {
		if event.Kind == EventTurnDone {
			turns++
		}
	}
	if turns != 1 {
		t.Fatalf("EventTurnDone on the steer's stream = %d, want 1", turns)
	}
	// And the second turn is an ORDINARY question: its own turn, opening on the
	// person's words, with nothing pretending they steered the first one.
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2", completer.requests())
	}
	if got, want := userLines(completer.request(1)),
		[]string{"list the workspace", "count them as well"}; !equalStrings(got, want) {
		t.Fatalf("follow-up request user content = %v, want %v", got, want)
	}
	if got := messageText(lastMessage(agent)); got != "and here are the counts" {
		t.Fatalf("last message = %q, want the answer to the waiting message", got)
	}
}

type happyFallThroughCompleter struct {
	mu           sync.Mutex
	conversation [][]ai.Message
	judgeEntered chan struct{}
	releaseJudge chan struct{}
	judgeOnce    sync.Once
}

func (c *happyFallThroughCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	if system == routeAheadBrief {
		return textResponse(routeConfirmNo), nil
	}
	if system == routeJudgeBrief {
		wait := false
		c.judgeOnce.Do(func() {
			close(c.judgeEntered)
			wait = true
		})
		if wait {
			select {
			case <-c.releaseJudge:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return textResponse(routeConfirmNo), nil
	}
	if len(messages) > 0 && strings.Contains(messageText(messages[len(messages)-1]), "[still asked]") {
		return textResponse(checkpointNothingLeft), nil
	}
	if system != "SYSTEM" {
		return textResponse(routeConfirmNo), nil
	}
	c.mu.Lock()
	snapshot := append([]ai.Message(nil), messages...)
	c.conversation = append(c.conversation, snapshot)
	c.mu.Unlock()
	return textResponse("ordinary answer"), nil
}

func (c *happyFallThroughCompleter) requests() [][]ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]ai.Message, len(c.conversation))
	copy(out, c.conversation)
	return out
}

// M12: a steer arriving after the last model boundary on a successful turn is
// reported as fall-through, journaled unconsumed, and asked once as a new turn.
func TestASteerAfterTheLastBoundaryFallsThroughIntoOneFreshTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &happyFallThroughCompleter{
		judgeEntered: make(chan struct{}),
		releaseJudge: make(chan struct{}),
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierLow): routeScreenModel,
		})
	})
	turn := mustSubmit(t, agent, "explain how parser state flows across every package")
	select {
	case <-completer.judgeEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("the successful turn never passed its last model boundary")
	}
	steered := mustSteer(t, agent, "fix the parser instead")
	close(completer.releaseJudge)
	collect(t, turn)
	fromSteer := collect(t, steered)

	if got, want := steerEvents(fromSteer), []string{
		"accepted:fix the parser instead",
		"fell:fix the parser instead",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	requests := completer.requests()
	if len(requests) != 2 {
		t.Fatalf("ordinary requests = %d, want the original and one fresh turn", len(requests))
	}
	got := userLines(requests[1])
	var copies int
	for _, line := range got {
		if line == "fix the parser instead" {
			copies++
		}
	}
	if len(got) == 0 || got[len(got)-1] != "fix the parser instead" || copies != 1 {
		t.Fatalf("the fresh turn's user words are %q", got)
	}
	var turns int
	for _, event := range fromSteer {
		if event.Kind == EventTurnDone {
			turns++
		}
	}
	if turns != 2 {
		t.Fatalf("the steer's stream carried %d completed turns, want the missed turn and its own", turns)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !recordsAFallThrough(t, path, "fix the parser instead") {
		t.Fatal("the successful fall-through is not journaled as unconsumed")
	}
}

// ── (e) fall-through onto a turn somebody stopped ───────────────────────────

// An interrupted turn drops its follow-up queue, because a drain must never
// resurrect a turn somebody stopped ([Agent.nextFollowUpLocked]). A steer that
// falls through onto one is an ordinary waiting message from that instant, and
// goes with the rest of the queue — but it is SAID on the stream and WRITTEN
// DOWN first, and it is never put into the transcript of the turn it missed.
func TestASteerFallsThroughWhenTheTurnIsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	turn := mustSubmit(t, agent, "start the long thing")
	held.wait(t)
	steered := mustSteer(t, agent, "actually stop at the parser")
	close(held.release)
	agent.Interrupt()

	collect(t, turn)
	fromSteer := collect(t, steered)

	if got, want := steerEvents(fromSteer), []string{
		"accepted:actually stop at the parser",
		"fell:actually stop at the parser",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	// Not in the transcript of the turn it missed: the model never read it, and
	// a record that held it would say the model had.
	agent.mu.Lock()
	said := userLines(agent.messages)
	agent.mu.Unlock()
	for _, line := range said {
		if line == "actually stop at the parser" {
			t.Fatalf("the steer was written into the interrupted turn: %v", said)
		}
	}
	// A stop stops everything said to that turn, and no turn ran on it.
	if completer.requests() != 1 {
		t.Fatalf("requests = %d, want 1 — a drain must not restart a stopped turn", completer.requests())
	}
	// But the record says it was sent and that it steered nothing.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !recordsAFallThrough(t, path, "actually stop at the parser") {
		t.Fatal("the journal holds no record of the steer that fell through")
	}
}

// recordsAFallThrough reports whether the file holds the honest line for one
// steer: sent, aimed at a turn, never carried by a request of it.
func recordsAFallThrough(t *testing.T, path, words string) bool {
	t.Helper()
	for _, entry := range journalEntries(t, path) {
		if entry.Type != "steer" || entry.Content != words {
			continue
		}
		if entry.Steer == nil || entry.Steer.Consumed {
			t.Fatalf("fall-through line does not say it fell through: %+v", entry.Steer)
		}
		if entry.Steer.At == "" {
			t.Fatal("fall-through line does not say when the person sent it")
		}
		return true
	}
	return false
}

// ── (f) fall-through on a turn that faulted ─────────────────────────────────

func TestASteerCutsARequestBeforeItsFaultCanEndTheTurn(t *testing.T) {
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			return nil, errors.New("insufficient_quota")
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "start it")
	held.wait(t)
	steered := mustSteer(t, agent, "and skip the tests")
	close(held.release)

	collected := collect(t, turn)
	var faulted bool
	for _, event := range collected {
		if event.Kind == EventError {
			faulted = true
		}
	}
	if faulted {
		t.Fatalf("turn events = %v, did not want the cut request's late fault", kinds(collected))
	}
	if got, want := steerEvents(collect(t, steered)), []string{
		"accepted:and skip the tests",
		"consumed:and skip the tests",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2 — the steer continues the turn", completer.requests())
	}
}

// ── (g) a steer with nothing to steer is refused ────────────────────────────

// The caller had a plain send available and did not use it, so the honest
// answer is that there was nothing running — not a turn started behind their
// back under a different name.
func TestSteeringAnIdleSessionIsRefused(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)

	stream, err := agent.Steer("go left instead")
	if !errors.Is(err, ErrNothingToSteer) {
		t.Fatalf("Steer while idle = %v, want ErrNothingToSteer", err)
	}
	if stream != nil {
		t.Fatal("a refused steer must hand back no stream to wait on")
	}
	if completer.requests() != 0 {
		t.Fatalf("requests = %d, want 0 — a refusal sends nothing", completer.requests())
	}
	// And an empty one is refused before the question is even asked.
	if _, err := agent.Steer("   "); err == nil {
		t.Fatal("an empty steer must be refused")
	}
	// A steer AFTER the turn it was meant for has ended is the same refusal, and
	// this is the case a surface actually hits: the person typed while the answer
	// was still drawing and pressed enter a beat late.
	collect(t, mustSubmit(t, agent, "ask something"))
	if _, err := agent.Steer("too late"); !errors.Is(err, ErrNothingToSteer) {
		t.Fatalf("Steer after the turn = %v, want ErrNothingToSteer", err)
	}
}

// ── (h) the record says whether the model read it ───────────────────────────

// A steer is an ordinary user message in the transcript, because that is what
// the model has to read it as. The journal keeps the one bit that says it did
// not open the turn it sits in, plus the landing clause a surface draws beneath
// the person's line.
func TestTheRecordSaysASteerWasPartOfTheTurnItSteered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	turn := mustSubmit(t, agent, "list the workspace")
	held.wait(t)
	sent := time.Now()
	mustSteer(t, agent, "and count them")
	close(held.release)
	collect(t, turn)

	// The live surface receives an ordinary user message carrying a steer mark.
	entries := agent.Transcript()
	var trunk, elbow *DisplayEntry
	for index := range entries {
		switch entries[index].Text {
		case "list the workspace":
			trunk = &entries[index]
		case "and count them":
			elbow = &entries[index]
		}
	}
	if trunk == nil || elbow == nil {
		t.Fatalf("transcript is missing the question or the steer: %+v", entries)
	}
	if trunk.Steer != nil {
		t.Fatal("the message that OPENED the turn must not be marked a steer")
	}
	if elbow.Steer == nil {
		t.Fatal("the steer mark was lost, so replay would treat it as a new question")
	}
	if !elbow.Steer.Consumed {
		t.Fatal("the steer landed, and the record must say so")
	}
	if elbow.Steer.Landing != "stopped the reply here" {
		t.Fatalf("landing = %q, want the provider-cut account", elbow.Steer.Landing)
	}
	if elbow.Steer.At.Before(sent.Add(-time.Second)) || elbow.Steer.At.After(time.Now()) {
		t.Fatalf("steer instant = %v, want the moment it was sent", elbow.Steer.At)
	}

	// And the same, read back off the file after the process is gone.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var found bool
	for _, entry := range journalEntries(t, path) {
		if entry.Type == "message" && entry.Content == "and count them" {
			found = true
			if entry.Steer == nil || !entry.Steer.Consumed || entry.Steer.At == "" ||
				entry.Steer.Landing != "stopped the reply here" {
				t.Fatalf("journal line for the steer = %+v", entry.Steer)
			}
		}
		if entry.Type == "message" && entry.Content == "list the workspace" && entry.Steer != nil {
			t.Fatal("the turn's opening question is marked as a steer in the file")
		}
	}
	if !found {
		t.Fatal("the steer is not in the journal at all")
	}

	// A resume reads the mark back, so the user line and landing clause survive.
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	steers := 0
	for _, message := range replayed.messages {
		if messageText(message) == "and count them" {
			mark, marked := replayed.steers[noteKey(message)]
			if !marked || mark.Landing != "stopped the reply here" {
				t.Fatal("the replay lost the steer's mark")
			}
			steers++
		}
	}
	if steers != 1 {
		t.Fatalf("replayed steers = %d, want 1", steers)
	}
}

// ── (i) a conversation with no steers in it is untouched ────────────────────

// The mark is new; the file is not. A transcript written before steering
// existed carries no steer line and no steer field, and it must replay into
// exactly the messages and exactly the display entries it always did.
func TestATranscriptWithNoSteersLoadsExactlyAsItAlwaysDid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, "/w", "m", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	journal.appendMessage(textMessage("user", "what does this do"))
	journal.appendMessage(textMessage("assistant", "it reads the file"))
	journal.appendNote(textMessage("user", "task 3 finished"))
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got, want := rolesOf(replayed.messages), []string{"user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("replayed roles = %v, want %v", got, want)
	}
	if len(replayed.steers) != 0 {
		t.Fatalf("replayed steers = %v, want none in a file that has none", replayed.steers)
	}

	reopened, restored, err := openSessionFile(path, "/w", "m", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	entries := shapeEntries(restored.messages, reopened)
	if got, want := len(entries), 3; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}
	for _, entry := range entries {
		if entry.Steer != nil {
			t.Fatalf("an old line came back marked as a steer: %+v", entry)
		}
	}
	// And the marks that were already there still work: the session's own line
	// is still an aside and not the person's words.
	if entries[2].Role != "aside" {
		t.Fatalf("note role = %q, want aside", entries[2].Role)
	}
}

// ── (j) the room's steer is a different act and is untouched ────────────────

// [Agent.SteerTask] puts a line into a running NODE — another agent, its own
// transcript — and it shares this package's steering lane with the splice
// without sharing its marks. A line steered at a node is not a splice into
// anybody's turn, and nothing about it may start to behave like one.
func TestNodeSteeringIsNotATurnSplice(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	if !agent.enqueueSteeredLine("the config lives under etc/", false) {
		t.Fatal("the node's own steering lane refused a line")
	}
	agent.mu.Lock()
	queued := append([]userMessage(nil), agent.steering...)
	agent.mu.Unlock()
	if len(queued) != 1 {
		t.Fatalf("queued = %d, want the one steered line", len(queued))
	}
	if !queued[0].steered {
		t.Fatal("a line steered at a node lost its own mark")
	}
	if queued[0].steer != nil {
		t.Fatal("a line steered at a node was marked as a turn splice")
	}
	// AND IT CARRIES THE RECORD'S OWN MARK, which is the other half of the same
	// distinction (#252): the person corrected running work, so the file says so
	// — with the promise this door actually makes, which is delivery.
	crossed := queued[0].crossed
	if crossed == nil {
		t.Fatal("a line steered at a node carries nothing for the record to keep")
	}
	if !crossed.Consumed || crossed.Landing != steerDeliveredWord || crossed.At.IsZero() {
		t.Fatalf("the delivered line's mark = %+v, want it delivered, at a known instant", *crossed)
	}
	// It drains as it always did: into the transcript, as the person's words,
	// and with no lift out of the queue.
	agent.mu.Lock()
	agent.liftSteersLocked(nil)
	landed, owed := agent.drainSteeringLocked(nil)
	agent.mu.Unlock()
	if landed != 1 || !owed {
		t.Fatalf("drain = (%d, %v), want the line landed and an answer owed", landed, owed)
	}
	if got := messageText(lastMessage(agent)); got != "the config lives under etc/" {
		t.Fatalf("transcript tail = %q, want the steered line", got)
	}
	if strings.TrimSpace(messageText(lastMessage(agent))) == "" {
		t.Fatal("the steered line reached the transcript empty")
	}
}

func TestASteerAdoptsAnOldBashAndItsExitArrivesLater(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("long-bash", "bash", `{"command":"sleep 4; echo steer-job-finished"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("I changed course while it finishes"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the background build finished too"), nil
		},
	}}
	// The adopted bash becomes a job, and a job is named — an errand on its own
	// goroutine (jobname.go). This test indexes into the requests it scripted, so
	// the namer must be answered off the queue or every index after the adoption
	// is one out. See [answerTheNamerOffTheQueue].
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "build and inspect")
	waitFor(t, "foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	time.Sleep(steerBashAge + 100*time.Millisecond)
	steered := mustSteer(t, agent, "inspect the parser while that runs")
	collect(t, turn)
	events := collect(t, steered)

	second := completer.request(1)
	if got := roleText(second, "tool"); !strings.Contains(got, "still running as job 1") ||
		!strings.Contains(got, "output via jobs output 1") {
		t.Fatalf("adopted tool result = %q", got)
	}
	if got, want := userLines(second), []string{"build and inspect", "inspect the parser while that runs"}; !equalStrings(got, want) {
		t.Fatalf("second request users = %v, want %v", got, want)
	}
	if got := steerLanding(events); got != "kept bash running as job 1" {
		t.Fatalf("steer landing = %q", got)
	}
	waitFor(t, "adopted job exit note", func() bool { return notesContain(agent, "steer-job-finished") })
	waitFor(t, "owed exit request", func() bool { return completer.requests() >= 3 })
	if got := strings.Join(userLines(completer.request(2)), "\n"); !strings.Contains(got, "job 1 exited 0") {
		t.Fatalf("later request has no owed exit note: %q", got)
	}
}

func TestASteerWaitsForAYoungBash(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("young-bash", "bash", `{"command":"sleep 0.5; echo young-finished"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "run the quick check")
	waitFor(t, "young foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	agent.mu.Lock()
	generation := agent.generation
	agent.mu.Unlock()
	if generation != nil {
		t.Fatal("a young bash still had a model generation to cut")
	}
	began := time.Now()
	steered := mustSteer(t, agent, "then read the result")
	collect(t, turn)
	events := collect(t, steered)
	if elapsed := time.Since(began); elapsed < 200*time.Millisecond {
		t.Fatalf("young bash landed in %s, want the batch to finish first", elapsed)
	}
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("young bash became a job: %q", list)
	}
	if got := steerLanding(events); got != "waiting for the running step" {
		t.Fatalf("steer landing = %q", got)
	}
}

func TestAStopSteerKillsAnOldBash(t *testing.T) {
	t.Parallel()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("stopped-bash", "bash", `{"command":"sleep 30"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("stopped"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	turn := mustSubmit(t, agent, "start the server")
	waitFor(t, "old foreground bash to start", func() bool { return len(agent.inFlightBash.snapshot()) == 1 })
	time.Sleep(steerBashAge + 100*time.Millisecond)
	steered := mustSteer(t, agent, "kill it")
	collect(t, turn)
	events := collect(t, steered)
	if got := roleText(completer.request(1), "tool"); !strings.Contains(got, "stopped by the person: kill it") {
		t.Fatalf("stopped tool result = %q", got)
	}
	if got := steerLanding(events); got != "stopped the running command" {
		t.Fatalf("steer landing = %q", got)
	}
	waitFor(t, "adopted job to settle killed", func() bool {
		job := agent.jobs.find(1)
		return job != nil && !job.running()
	})
	if notesContain(agent, "job 1 exited") {
		t.Fatal("a person-requested stop produced an owed exit note")
	}
}

func roleText(messages []ai.Message, role string) string {
	for _, message := range messages {
		if message.Role == role {
			return messageText(message)
		}
	}
	return ""
}

func steerLanding(events []Event) string {
	for _, event := range events {
		if event.Kind == EventSteerAccepted && event.Steer != nil {
			return event.Steer.Landing
		}
	}
	return ""
}
