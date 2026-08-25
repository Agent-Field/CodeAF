package session

// THE PROMPT CACHE IS A PROPERTY OF THE BYTES, AND THIS IS WHERE IT IS PINNED.
//
// Every endpoint this build talks to caches on the exact LEADING BYTES of a
// request — automatically on DeepSeek, OpenAI and most of what OpenRouter
// fronts; behind explicit markers on the Anthropic family (internal/provider's
// caching.go). Nothing in the body switches it on. The only two levers a client
// holds are byte stability and replica affinity, and the affinity key is worth
// nothing if the bytes moved: one changed character anywhere in the prefix
// re-prices every token after it at the uncached rate, which on a working model
// is about five times the cached one.
//
// A turn of this loop makes one request per tool round, and by the tenth round
// the transcript is most of what is being sent. So the property that has to hold
// is narrow and absolute: request N's whole message list must reappear,
// unchanged and in order, at the head of request N+1's, and the tool block in
// front of it must not move at all. internal/exec pins the same law for a leaf
// (prefixcache_test.go there); this is the conversation's half of it.
//
// The three things that CAN legally rewrite history are all outside a turn — a
// compaction fold, a rewind, the stubbing pass at a completed turn's end — and
// the last of those is asserted here too, because it is the one that runs on its
// own and the one whose price is invisible from the transcript.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/reflex"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// beltCompleter is the scripted completer plus the half of a request that the
// scripted one throws away: the tool definitions. They ride in front of the
// whole transcript on this wire, so a schema block that shifts costs more than
// any message can.
type beltCompleter struct {
	scriptedCompleter
	tools [][]ai.ToolDefinition
}

func (b *beltCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	b.mu.Lock()
	b.tools = append(b.tools, request.Tools)
	b.mu.Unlock()
	return b.scriptedCompleter.CompleteWithMessages(ctx, messages, options...)
}

// wireMessages is the comparison the provider actually makes: the bytes of each
// message rather than the Go value. Two transcripts that differ only in a field
// the encoder drops are the same prefix; two that agree in Go and encode
// differently are not. Unmarked messages go to the wire through exactly this
// marshaller (internal/provider's encodeMessages), so this is the real thing.
func wireMessages(t *testing.T, messages []ai.Message) []string {
	t.Helper()
	encoded := make([]string, len(messages))
	for index, message := range messages {
		raw, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		encoded[index] = string(raw)
	}
	return encoded
}

// wireTools is the same for the schema block.
func wireTools(t *testing.T, tools []ai.ToolDefinition) string {
	t.Helper()
	raw, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// assertPrefix is the whole law in one place: everything `earlier` sent must
// still be there, byte for byte, in the same order, at the head of `later`.
func assertPrefix(t *testing.T, what string, earlier, later []string) {
	t.Helper()
	if len(later) < len(earlier) {
		t.Fatalf("%s: the list shrank from %d to %d — a request that drops what the last one sent is a cold prefix",
			what, len(earlier), len(later))
	}
	for index := range earlier {
		if earlier[index] == later[index] {
			continue
		}
		t.Fatalf("%s: entry %d was rewritten, so every token after it is re-billed uncached\nwas:  %.400s\nnow:  %.400s",
			what, index, earlier[index], later[index])
	}
}

// ── within one turn ─────────────────────────────────────────────────────────

// TestConsecutiveRequestsOfOneTurnSendAByteStablePrefix is the regression the
// whole discipline rests on. The turn below runs three tool rounds and one of
// the results is heavy enough to be stub-eligible, which is the shape that would
// catch a stubbing pass — or anything else that rewrites in place — being moved
// inside the loop.
func TestConsecutiveRequestsOfOneTurnSendAByteStablePrefix(t *testing.T) {
	completer := &beltCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fat", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-2", "fat", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-3", "thin", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = append(agent.tools,
		staticTool("fat", heavyOutput("BIGREAD")),
		staticTool("thin", "a short answer"))

	events, err := agent.Submit(context.Background(), "read the two big things")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if got := completer.requests(); got != 4 {
		t.Fatalf("the turn made %d requests, want the scripted 4", got)
	}
	for index := 1; index < completer.requests(); index++ {
		assertPrefix(t,
			fmt.Sprintf("request %d against request %d", index, index-1),
			wireMessages(t, completer.request(index-1)),
			wireMessages(t, completer.request(index)))
	}
}

// TestTheToolBlockIsByteIdenticalAcrossOneTurn: the schemas are encoded ahead of
// the transcript, so a belt that grows, shrinks or reorders mid-turn invalidates
// EVERYTHING — not merely the messages behind the change, but every message,
// because they all sit behind the block. Nothing in a turn arms a family today
// and this is what says so out loud.
func TestTheToolBlockIsByteIdenticalAcrossOneTurn(t *testing.T) {
	completer := &beltCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "thin", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-2", "thin", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = append(agent.tools, staticTool("thin", "a short answer"))

	events, err := agent.Submit(context.Background(), "look twice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	completer.mu.Lock()
	sent := append([][]ai.ToolDefinition(nil), completer.tools...)
	completer.mu.Unlock()
	if len(sent) < 2 {
		t.Fatalf("the turn made %d requests, want several to compare", len(sent))
	}
	first := wireTools(t, sent[0])
	if strings.TrimSpace(first) == "" || first == "null" {
		t.Fatal("no tool block reached the wire; this test would pass on nothing")
	}
	for index := 1; index < len(sent); index++ {
		if got := wireTools(t, sent[index]); got != first {
			t.Fatalf("request %d sent a different tool block, which re-prices the whole transcript behind it\nwas:  %.400s\nnow:  %.400s",
				index, first, got)
		}
	}
}

// ── across a turn boundary ──────────────────────────────────────────────────

// TestASecondTurnKeepsTheFirstTurnsPrefix. Between two turns the loop refreshes
// message[0] — the clock, the standing orders and the memory block live in it
// (memory.go's refreshSystemLocked) — and a refresh that CHANGED anything there
// re-prices the entire conversation, because message[0] is in front of every
// message there is. That is why only what holds for the life of a conversation
// is allowed in there; the two blocks that move with the work were taken out of
// it and ride at the tail instead (agent.go's landVolatileLocked).
//
// A conversation where none of those moved must therefore send the first turn's
// bytes again unchanged. That is the property; the cost of breaking it is stated
// where the blocks are assembled.
func TestASecondTurnKeepsTheFirstTurnsPrefix(t *testing.T) {
	completer := &beltCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("first"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("second"), nil
		},
	}}}
	agent, _ := newTestAgent(t, completer, nil)

	for _, said := range []string{"one", "two"} {
		events, err := agent.Submit(context.Background(), said)
		if err != nil {
			t.Fatalf("Submit %q: %v", said, err)
		}
		collect(t, events)
	}
	if got := completer.requests(); got != 2 {
		t.Fatalf("the session made %d requests, want 2", got)
	}
	assertPrefix(t, "the second turn against the first",
		wireMessages(t, completer.request(0)),
		wireMessages(t, completer.request(1)))
}

// ── the blocks that move with the work ──────────────────────────────────────

// TestTheVolatileNoteHoldsItsPlaceAcrossOneTurn is the guard the state card and
// the other windows' block are worth having, and the one that says what shape
// they may take.
//
// They are context that MOVES: the card is rewritten every time a post-turn
// delta lands, and the other windows' block is re-read at the start of every
// turn. Both used to be rendered into message[0], where a change re-priced every
// message of the conversation behind them. They ride at the tail now, and the
// tail is the one place a growing transcript will hold still — but only if the
// note is a REAL MESSAGE that stays where it was said. A block stitched onto the
// end of each request instead would sit at a different index every round, which
// is this test failing.
func TestTheVolatileNoteHoldsItsPlaceAcrossOneTurn(t *testing.T) {
	completer := &beltCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "thin", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-2", "thin", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = append(agent.tools, staticTool("thin", "a short answer"))
	agent.mergeStateCard(reflex.StateDelta{
		Goal: "ship the parser",
		Next: []string{"write the replay"},
	})

	events, err := agent.Submit(context.Background(), "look twice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if got := completer.requests(); got != 3 {
		t.Fatalf("the turn made %d requests, want the scripted 3", got)
	}
	carried := false
	for _, message := range completer.request(0) {
		if strings.Contains(messageContentText(message), "goal: ship the parser") {
			carried = true
		}
	}
	if !carried {
		t.Fatal("no request carried the card; this test would pass on nothing")
	}
	for index := 1; index < completer.requests(); index++ {
		assertPrefix(t,
			fmt.Sprintf("request %d against request %d", index, index-1),
			wireMessages(t, completer.request(index-1)),
			wireMessages(t, completer.request(index)))
	}
}

// TestACardThatMovesBetweenTurnsCostsOnlyTheTail is the finding itself, pinned.
// The card moved between the two turns below — which is what a card DOES, on
// most turns of a working session — and the conversation it moved in must be
// re-sent byte for byte, message[0] included. Everything the second turn pays
// for beyond that is what it appended.
func TestACardThatMovesBetweenTurnsCostsOnlyTheTail(t *testing.T) {
	completer := &beltCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("first"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("second"), nil
		},
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.mergeStateCard(reflex.StateDelta{Goal: "ship the parser"})

	events, err := agent.Submit(context.Background(), "one")
	if err != nil {
		t.Fatalf("Submit one: %v", err)
	}
	collect(t, events)

	// The post-turn pass, in the one respect this test cares about: the card now
	// says something else.
	agent.mergeStateCard(reflex.StateDelta{
		Done: []string{"the replay drops the pre-cut transcript"},
		Next: []string{"the fold marker's id range"},
	})

	events, err = agent.Submit(context.Background(), "two")
	if err != nil {
		t.Fatalf("Submit two: %v", err)
	}
	collect(t, events)

	first := wireMessages(t, completer.request(0))
	second := wireMessages(t, completer.request(1))
	assertPrefix(t, "the turn after the card moved", first, second)

	// And the new state did reach the model, at the back, so the prefix above is
	// stable because the note moved to the tail and not because nothing happened.
	tail := second[len(first):]
	if len(tail) == 0 {
		t.Fatal("the second turn appended nothing at all")
	}
	if !strings.Contains(strings.Join(tail, "\n"), "the fold marker's id range") {
		t.Fatalf("the new card never reached the request:\n%s", strings.Join(tail, "\n"))
	}
}

// TestASessionWithNothingToSayLandsNoNote. The emptiness law, at the one place
// it costs money: a conversation with no card and no other window on the project
// sends no note, no heading and no empty tags.
func TestASessionWithNothingToSayLandsNoNote(t *testing.T) {
	completer := &beltCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "say something")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	for _, message := range completer.request(0) {
		if strings.Contains(messageContentText(message), volatileNoteOpening) {
			t.Fatalf("a session with nothing to say sent a note:\n%s", messageContentText(message))
		}
	}
}

// TestTheVolatileNoteIsOnTheMeter. The note is transcript like everything else
// in the transcript, so the two places that ask how big this conversation has
// got — the compaction threshold and the oversize guard — see it without being
// told about it. A note the meter could not see would be context that grew for
// free right up until the provider refused the request.
func TestTheVolatileNoteIsOnTheMeter(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	agent.mu.Lock()
	before := agent.estimateTokensLocked()
	agent.mu.Unlock()

	agent.mergeStateCard(reflex.StateDelta{Goal: strings.Repeat("ship the parser ", 200)})
	note := volatileNote(agent)
	if note == "" {
		t.Fatal("no note landed; this test would pass on nothing")
	}

	agent.mu.Lock()
	after := agent.estimateTokensLocked()
	agent.mu.Unlock()
	if want := before + len(note)/bytesPerToken; after < want {
		t.Fatalf("the meter reads %d tokens after a %d-byte note, want at least %d", after, len(note), want)
	}
}

// ── the append law on the belt ──────────────────────────────────────────────

// TestArmingAFamilyOnlyAppendsToTheToolBlock pins connect.go's stated law at the
// one place it can be broken cheaply. Arming costs exactly one invalidation, at
// the back of the block, once per family — and it costs that much ONLY while
// every definition already there keeps its position and its bytes.
func TestArmingAFamilyOnlyAppendsToTheToolBlock(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	held := make([]string, 0, len(agent.beltDefinitions()))
	for _, definition := range agent.beltDefinitions() {
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, string(raw))
	}

	arriving := []bare.Tool{{
		Name:        "prefix_probe",
		Description: "a family arming mid-session",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
	}}
	names, err := agent.armFamily(arriving)
	if err != nil {
		t.Fatalf("armFamily: %v", err)
	}
	if len(names) != 1 || names[0] != "prefix_probe" {
		t.Fatalf("armFamily reported %v, want the one tool it was handed", names)
	}

	grown := make([]string, 0, len(agent.beltDefinitions()))
	for _, definition := range agent.beltDefinitions() {
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		grown = append(grown, string(raw))
	}
	if len(grown) != len(held)+1 {
		t.Fatalf("the belt went from %d definitions to %d, want exactly one appended", len(held), len(grown))
	}
	assertPrefix(t, "the belt after arming", held, grown)

	// And arming the same family again is free: nothing arrives, so nothing at
	// all moves.
	if names, err := agent.armFamily(arriving); err != nil || len(names) != 0 {
		t.Fatalf("re-arming reported %v (err %v), want nothing new", names, err)
	}
}

// ── the stubbing pass, which is allowed to rewrite and has to earn it ───────

// TestStubbingLeavesTheCachedPrefixAloneForATrivialReclaim. The pass replaces an
// old heavy result with a pointer to itself, which is a rewrite in the MIDDLE of
// the transcript: every byte behind it goes cold on the next request. That is
// worth paying when the result is a two-hundred-kilobyte read and is not worth
// paying to save one line, so the pass weighs the two (stubPrefixShare) and
// declines quietly when the arithmetic says no.
func TestStubbingLeavesTheCachedPrefixAloneForATrivialReclaim(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	// One barely-eligible result, then a tail far heavier than anything
	// stubbing it could give back.
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", "turn one"),
		ai.Message{Role: "tool", ToolCallID: "call-1",
			Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("m", stubMinBytes+200)}}},
	)
	for turn := 2; turn <= stubKeepTurns+1; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("turn %d", turn)),
			textMessage("assistant", strings.Repeat("t", 30_000)))
	}
	before := wireMessages(t, agent.messages)
	agent.mu.Unlock()

	agent.stubOldOutputs()

	agent.mu.Lock()
	after := wireMessages(t, agent.messages)
	agent.mu.Unlock()
	assertPrefix(t, "the transcript after a pass that should have declined", before, after)
}

// TestStubbingStillRewritesWhenTheReclaimIsWorthIt is the other half, and the
// one that keeps the gate above from being a way to turn the feature off. The
// same shape with a genuinely heavy result must still be replaced.
func TestStubbingStillRewritesWhenTheReclaimIsWorthIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	heavy := strings.Repeat("h", 200_000)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", "turn one"),
		ai.Message{Role: "tool", ToolCallID: "call-1",
			Content: []ai.ContentPart{{Type: "text", Text: heavy}}},
	)
	for turn := 2; turn <= stubKeepTurns+1; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("turn %d", turn)),
			textMessage("assistant", strings.Repeat("t", 30_000)))
	}
	agent.mu.Unlock()

	agent.stubOldOutputs()

	texts := toolTexts(agent)
	if len(texts) != 1 {
		t.Fatalf("tool messages: got %d, want 1", len(texts))
	}
	if !strings.HasPrefix(texts[0], stubMarker) {
		t.Fatalf("a 200KB result was left verbatim: %.80q", texts[0])
	}
}

// AN OLD VOLATILE NOTE FOLDS WITH THE HISTORY. The person's own words survive
// every fold; the session's note about the card does not, because the next
// landing supersedes it and a long session would otherwise carry every state
// the card ever had, uncompactable.
func TestAnOldVolatileNoteFoldsWithTheHistory(t *testing.T) {
	if !isVolatileNote(volatileNoteOpening + "anything") {
		t.Fatal("the opening no longer marks a note")
	}
	person := textMessage("user", "please look at the reconciler")
	note := textMessage("user", volatileNoteOpening+"<state>old</state>")
	if isVolatileNote(messageContentText(person)) {
		t.Fatal("a person's message read as the session's note")
	}
	if !isVolatileNote(messageContentText(note)) {
		t.Fatal("the note was not recognized through messageContentText")
	}
}
