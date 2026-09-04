package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const toolCompactResultBytes = 4000

func toolCompactOutput(round int) string {
	// THE MARKER IS AT THE END, which is where [checkpointResultTail] looks
	// and where a real tool's verdict lives. A marker at the head would be
	// the first thing the digest cut away.
	return strings.Repeat("x", toolCompactResultBytes) + fmt.Sprintf("\nROUND-%02d-UNIQUE-TAIL", round)
}

func toolCompactMessages(rounds, newestSiblings int) []ai.Message {
	messages := []ai.Message{
		textMessage("system", "SYSTEM-PROMPT-MUST-NOT-MOVE"),
		textMessage("user", "do the work"),
	}
	if newestSiblings < 1 {
		newestSiblings = 1
	}
	for round := 0; round < rounds; round++ {
		call := fmt.Sprintf("call-%d", round)
		messages = append(messages,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{
				Type: "text", Text: toolCompactOutput(round),
			}}},
		)
	}
	for extra := 1; extra < newestSiblings; extra++ {
		call := fmt.Sprintf("call-new-%d", extra)
		messages = append(messages, ai.Message{
			Role:       "tool",
			ToolCallID: call,
			Content:    []ai.ContentPart{{Type: "text", Text: toolCompactOutput(100 + extra)}},
		})
	}
	return messages
}

func toolTextsOf(messages []ai.Message) []string {
	var texts []string
	for _, message := range messages {
		if message.Role == "tool" {
			texts = append(texts, messageContentText(message))
		}
	}
	return texts
}

func toolResultPayloadBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		if message.Role == "tool" {
			total += messageBytes(message)
		}
	}
	return total
}

func TestCompactToolHistoryKeepsNewestVerbatimAndSystemUntouched(t *testing.T) {
	original := toolCompactMessages(6, 1)
	got := compactToolHistory(original, len(original), nil)

	if messageContentText(got[0]) != messageContentText(original[0]) {
		t.Fatalf("system prompt moved: %q", messageContentText(got[0]))
	}
	texts := toolTextsOf(got)
	if len(texts) != 6 {
		t.Fatalf("dropped a tool result: got %d, want 6", len(texts))
	}
	if texts[5] != toolCompactOutput(5) {
		t.Fatalf("newest result was compacted: %.80q", texts[5])
	}
	for index := 0; index < 5; index++ {
		if texts[index] == toolCompactOutput(index) {
			t.Fatalf("old result %d stayed verbatim", index)
		}
		if !strings.Contains(texts[index], fmt.Sprintf("ROUND-%02d-UNIQUE-TAIL", index)) {
			t.Fatalf("old result %d lost its tail: %.80q", index, texts[index])
		}
		if len(texts[index]) > compactViewBytes+reducedViewSlack {
			t.Fatalf("old result %d is %d bytes, want a reduced view", index, len(texts[index]))
		}
	}
	for _, message := range original {
		if message.Role != "tool" {
			continue
		}
		if !strings.Contains(messageContentText(message), strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatal("compactToolHistory mutated an input result")
		}
	}
}

func TestCompactToolHistoryKeepsAParallelNewestBatchVerbatim(t *testing.T) {
	original := toolCompactMessages(4, 3)
	got := compactToolHistory(original, len(original), nil)
	texts := toolTextsOf(got)
	if len(texts) != 6 {
		t.Fatalf("tool results = %d, want 6", len(texts))
	}
	for _, text := range texts[len(texts)-3:] {
		if !strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("a newest-batch sibling was compacted: %.80q", text)
		}
	}
	for _, text := range texts[:len(texts)-3] {
		if strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("an older result stayed verbatim: %.80q", text)
		}
	}
}

func TestCompactToolHistoryBoundsOldResultsToTheDigestBudget(t *testing.T) {
	// Enough long results that tails alone would blow the 5k-token account.
	// The second pass must shrink the far end so consumed evidence stays inside
	// the same budget the checkpoint reader already proved.
	const rounds = 80
	original := toolCompactMessages(rounds, 1)
	got := compactToolHistory(original, len(original), nil)
	texts := toolTextsOf(got)
	if texts[len(texts)-1] != toolCompactOutput(rounds-1) {
		t.Fatal("newest result was compacted to make the budget")
	}
	var old []int
	cut := newestToolBatchStart(got)
	for index, message := range got {
		if message.Role == "tool" && index < cut {
			old = append(old, index)
		}
	}
	if spent := toolResultBytes(got, old); spent > checkpointDigestBytes {
		t.Fatalf("old tool results weigh %d bytes, want at most the digest budget %d", spent, checkpointDigestBytes)
	}
}

func TestALongFrozenToolHistoryCompactsToSubLinearPrompt(t *testing.T) {
	const rounds = 16
	steps := make([]step, 0, rounds+2)
	for round := 0; round < rounds; round++ {
		round := round
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			// Unique arguments and visible assistant text keep the loop
			// detector and the silent-streak watch from ending the turn
			// before the history has had a chance to grow.
			return toolResponseWithText(
				fmt.Sprintf("call-%d", round),
				"read",
				fmt.Sprintf(`{"round":%d}`, round),
				fmt.Sprintf("working round %d", round),
			), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("still done"), nil
	})
	completer := &scriptedCompleter{
		steps: steps,
		aside: func(messages []ai.Message) (*ai.Response, bool) {
			// THE TURN'S SYSTEM PROMPT IS THE LITERAL "SYSTEM". Every errand
			// beside it — title, memory, route judge, checkpoint reader — has
			// a page of its own, and answering those here keeps them from
			// stealing a scripted tool round.
			if len(messages) == 0 || messageContentText(messages[0]) != "SYSTEM" {
				return textResponse("aside"), true
			}
			return nil, false
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = bigTestWindow
		config.CompactEnabled = false
		config.TaskAudit = false
	})
	issued := 0
	for index := range agent.tools {
		if agent.tools[index].Name != "read" {
			continue
		}
		tool := staticTool("read", "")
		tool.Execute = func(context.Context, json.RawMessage) (string, bool, error) {
			text := toolCompactOutput(issued)
			issued++
			return text, false, nil
		}
		agent.tools[index] = tool
		break
	}

	events, err := agent.Submit(context.Background(), "read sixteen times")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	if completer.requests() < rounds {
		t.Fatalf("turn made %d requests, want at least %d tool rounds", completer.requests(), rounds)
	}
	events, err = agent.Submit(context.Background(), "recap that work")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	lastReq := completer.request(completer.requests() - 1)
	if messageContentText(lastReq[0]) != "SYSTEM" {
		t.Fatalf("system prompt was rewritten: %q", messageContentText(lastReq[0]))
	}
	sent := toolTextsOf(lastReq)
	if len(sent) < rounds {
		t.Fatalf("request lost tool results: got %d, want %d", len(sent), rounds)
	}
	if !strings.Contains(sent[len(sent)-1], strings.Repeat("x", toolCompactResultBytes)) {
		t.Fatalf("newest sent result was not verbatim: %.80q", sent[len(sent)-1])
	}
	oldBytes := 0
	for _, text := range sent[:len(sent)-1] {
		if strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("an old sent result stayed verbatim: %.80q", text)
		}
		oldBytes += len(text)
	}
	if oldBytes > checkpointDigestBytes {
		t.Fatalf("old sent results weigh %d bytes, want at most the digest budget %d", oldBytes, checkpointDigestBytes)
	}

	// RAW GROWTH IS LINEAR IN THE RESULT BODY. Sixteen full results would put
	// tens of kilobytes of tool payload on the next turn's request; the
	// compacted frozen view must stay a small fraction of that.
	raw := 0
	for round := 0; round < rounds; round++ {
		raw += len(toolCompactOutput(round))
	}
	got := toolResultPayloadBytes(lastReq)
	if got*2 > raw {
		t.Fatalf("last request tool payload %d bytes is not sub-linear against raw %d", got, raw)
	}

	// THE JOURNAL AND THE LIVE TRANSCRIPT KEEP THE BYTES. A view that shrank
	// the record would fail the stub.go law this pass is written on.
	live := toolTexts(agent)
	if len(live) < rounds {
		t.Fatalf("live transcript lost results: %d", len(live))
	}
	for index, text := range live {
		if !strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("live result %d was rewritten: %.80q", index, text)
		}
	}
}

func TestLiveCompactToolHistoryKeepsNewestReadable(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("OPENROUTER_API_KEY missing")
	}
	model := strings.TrimSpace(os.Getenv("AFORGE_LIVE_MODEL"))
	if model == "" {
		model = "deepseek/deepseek-v4-flash-0731"
	}
	client, err := provider.NewClient(provider.Config{
		APIKey:  key,
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   model,
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := toolCompactMessages(5, 1)
	history := compactToolHistory(messages, len(messages), nil)
	history = append(history, textMessage("user",
		"Reply with only the last line of the newest tool result, nothing else."))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	response, err := client.CompleteWithMessages(ctx, history,
		ai.WithModel(model), ai.WithMaxTokens(64))
	if err != nil {
		t.Fatalf("live compacted history was refused: %v", err)
	}
	if response == nil || len(response.Choices) == 0 {
		t.Fatal("live call returned no choice")
	}
	got := messageContentText(response.Choices[0].Message)
	if !strings.Contains(got, "ROUND-04-UNIQUE-TAIL") {
		t.Fatalf("live model did not see the newest verbatim tail: %q", got)
	}
}

// reducedViewSlack is what a reduced view spends on top of the head and tail it
// keeps: one header line naming the tool, the size and the pointer, and the
// elision note between the halves. Two hundred bytes covers a long store ref or
// a long journal path and is still a rounding error against the kilobytes the
// view replaces.
const reducedViewSlack = 200

// headedOutput is a result whose FIRST line is the interesting one — the shape
// of every failing build, every missing file, every stack trace — and whose last
// line is the verdict.
func headedOutput(round int) string {
	return fmt.Sprintf("HEAD-%02d-ERROR: no such file\n", round) +
		strings.Repeat("filler line that nobody needs to read\n", 200) +
		fmt.Sprintf("ROUND-%02d-UNIQUE-TAIL", round)
}

func headedMessages(rounds int) []ai.Message {
	messages := []ai.Message{
		textMessage("system", "SYSTEM-PROMPT-MUST-NOT-MOVE"),
		textMessage("user", "do the work"),
	}
	for round := 0; round < rounds; round++ {
		call := fmt.Sprintf("call-%d", round)
		messages = append(messages,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "bash", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{
				Type: "text", Text: headedOutput(round),
			}}},
		)
	}
	return messages
}

// A REDUCED RESULT KEEPS BOTH ENDS AND SAYS WHAT IT CUT. The tail alone showed
// the model that something had finished and never that it had started by saying
// `no such file`, and it named nowhere to read the rest.
func TestAReducedResultKeepsBothEndsAndNamesItsSource(t *testing.T) {
	original := headedMessages(4)
	source := func(message ai.Message) string { return "store:" + message.ToolCallID }
	got := compactToolHistory(original, len(original), source)
	texts := toolTextsOf(got)

	for index, text := range texts[:len(texts)-1] {
		if !strings.Contains(text, fmt.Sprintf("HEAD-%02d-ERROR: no such file", index)) {
			t.Fatalf("result %d lost the head that said what went wrong: %.120q", index, text)
		}
		if !strings.Contains(text, fmt.Sprintf("ROUND-%02d-UNIQUE-TAIL", index)) {
			t.Fatalf("result %d lost its verdict: %.120q", index, text)
		}
		if !strings.Contains(text, "full: store:"+fmt.Sprintf("call-%d", index)) {
			t.Fatalf("result %d named no source to read it back: %.200q", index, text)
		}
		if !strings.Contains(text, fmt.Sprintf("%d bytes ·", len(headedOutput(index)))) {
			t.Fatalf("result %d did not say its true size: %.200q", index, text)
		}
		if len(text) > compactViewBytes+reducedViewSlack {
			t.Fatalf("result %d is %d bytes, want at most %d", index, len(text), compactViewBytes+reducedViewSlack)
		}
	}
}

// THE ELIDED COUNT IS THE COUNT. A model decides whether to spend a call on the
// rest from this number, so head + elided + tail has to be the whole result.
func TestAReducedViewCountsTheBytesItCutExactly(t *testing.T) {
	text := headedOutput(7)
	view := reducedResultView("bash", text, "store:1")
	body := view[strings.Index(view, "\n")+1:]
	head, rest, ok := strings.Cut(body, "\n…[")
	if !ok {
		t.Fatalf("no elision note in the view: %.200q", view)
	}
	note, tail, ok := strings.Cut(rest, " bytes elided]…\n")
	if !ok {
		t.Fatalf("malformed elision note: %.200q", rest)
	}
	elided := 0
	if _, err := fmt.Sscanf(note, "%d", &elided); err != nil {
		t.Fatalf("elision note %q is not a count: %v", note, err)
	}
	if got := len(head) + elided + len(tail); got != len(strings.TrimSpace(text)) {
		t.Fatalf("head %d + elided %d + tail %d = %d, want the whole %d-byte result",
			len(head), elided, len(tail), got, len(strings.TrimSpace(text)))
	}
}

// A SESSION THAT CAN NAME NOWHERE SAYS SO. A pointer at a store this session
// never had, or a journal it is not writing, costs the model a call and returns
// nothing — the one failure stub.go's law forbids.
func TestAReducedResultWithNoStoreOrJournalIsHonestAboutIt(t *testing.T) {
	original := headedMessages(3)
	got := compactToolHistory(original, len(original), nil)
	for index, text := range toolTextsOf(got) {
		if index == 2 {
			continue
		}
		if !strings.Contains(text, "full: "+compactNoSource) {
			t.Fatalf("result %d invented a source: %.200q", index, text)
		}
		if strings.Contains(text, ".jsonl") || strings.Contains(text, "store:") {
			t.Fatalf("result %d named a place nothing confirmed: %.200q", index, text)
		}
	}
}

// EVERY CALL KEEPS ITS RESULT, in order and by id. A shortened result is a
// saving; a missing one is a 400 from the provider on this request and on every
// request after it.
func TestCompactToolHistoryKeepsEveryCallPairedWithItsResult(t *testing.T) {
	original := toolCompactMessages(40, 2)
	got := compactToolHistory(original, len(original), nil)
	if len(got) != len(original) {
		t.Fatalf("snapshot has %d messages, want the same %d", len(got), len(original))
	}
	for index := range original {
		if got[index].Role != original[index].Role {
			t.Fatalf("message %d changed role: %q → %q", index, original[index].Role, got[index].Role)
		}
		if got[index].ToolCallID != original[index].ToolCallID {
			t.Fatalf("message %d changed its call id: %q → %q", index, original[index].ToolCallID, got[index].ToolCallID)
		}
		if len(got[index].ToolCalls) != len(original[index].ToolCalls) {
			t.Fatalf("message %d changed its calls: %d → %d", index,
				len(original[index].ToolCalls), len(got[index].ToolCalls))
		}
		for call := range original[index].ToolCalls {
			if got[index].ToolCalls[call].ID != original[index].ToolCalls[call].ID {
				t.Fatalf("message %d rewrote a call id", index)
			}
		}
	}
}

// THE SAME FROZEN PREFIX PRODUCES THE SAME BYTES, on the second request of a
// round and on the tenth. A view that reduced its own reduction would send a
// different prefix every time and pay for a cold cache on every request.
func TestCompactToolHistoryRepeatsItselfExactly(t *testing.T) {
	original := headedMessages(60)
	source := func(message ai.Message) string { return "store:" + message.ToolCallID }
	first := compactToolHistory(original, len(original), source)
	second := compactToolHistory(original, len(original), source)
	for index := range first {
		if messageContentText(first[index]) != messageContentText(second[index]) {
			t.Fatalf("message %d differs between two passes over the same frozen prefix", index)
		}
	}
	// And a pass over an already-reduced view leaves it alone: the marker is
	// what makes a reduction final.
	again := compactToolHistory(first, len(first), source)
	for index := range first {
		if messageContentText(first[index]) != messageContentText(again[index]) {
			t.Fatalf("message %d was reduced a second time: %.120q", index, messageContentText(again[index]))
		}
	}
}

// THE RUNNING TOTAL IS THE RECOMPUTED TOTAL. The budget walk carries its own sum
// instead of re-adding every old result on every iteration; if the two ever
// disagree the pass either stops early and blows the budget or keeps going and
// shrinks evidence it did not have to.
func TestTheBudgetWalkCarriesTheSameTotalItWouldRecompute(t *testing.T) {
	for _, rounds := range []int{4, 40, 400} {
		original := toolCompactMessages(rounds, 1)
		got := compactToolHistory(original, len(original), nil)
		var old []int
		cut := newestToolBatchStart(got)
		for index, message := range got {
			if message.Role == "tool" && index < cut {
				old = append(old, index)
			}
		}
		spent := toolResultBytes(got, old)
		// Under the budget, or every old result already at its one-line floor:
		// four hundred calls cannot fit the account however hard they are cut,
		// and a walk that stopped early would leave views the budget cannot pay
		// for. Either way the carried total has to agree with the recomputed one.
		if spent > checkpointDigestBytes {
			for _, index := range old {
				text := strings.TrimSpace(messageContentText(got[index]))
				if !strings.HasPrefix(text, stubMarker) {
					t.Fatalf("%d rounds: over budget at %d bytes with result %d not reduced to a line: %.120q",
						rounds, spent, index, text)
				}
			}
			continue
		}
		// Nothing shrank that did not have to: the walk stops the moment the
		// carried total is inside the budget, so the newest of the old results
		// still holds its full view.
		if rounds > 4 {
			last := messageContentText(got[old[len(old)-1]])
			if !strings.HasPrefix(strings.TrimSpace(last), compactReducedMarker) {
				t.Fatalf("%d rounds: the newest old result was over-reduced: %.120q", rounds, last)
			}
		}
	}
}

// THE FAR END STILL SAYS WHERE ITS BYTES ARE. The one-line account a
// budget-blown history falls back to used to be a first line and a size, with
// nothing to follow.
func TestTheOneLineFallbackStillNamesASource(t *testing.T) {
	original := toolCompactMessages(200, 1)
	source := func(message ai.Message) string { return "store:" + message.ToolCallID }
	got := compactToolHistory(original, len(original), source)
	lines := 0
	for index, message := range got {
		if message.Role != "tool" || index >= newestToolBatchStart(got) {
			continue
		}
		text := strings.TrimSpace(messageContentText(message))
		if !strings.HasPrefix(text, stubMarker) {
			continue
		}
		lines++
		if !strings.Contains(text, "full: store:"+message.ToolCallID) {
			t.Fatalf("one-line account %d points nowhere: %q", index, text)
		}
	}
	if lines == 0 {
		t.Fatal("no result fell back to a one-line account, so the fallback went untested")
	}
}

// BenchmarkCompactToolHistory is evidence rather than a gate: the walk is linear
// in the call count by construction, and this is what the constant looks like.
func BenchmarkCompactToolHistory(b *testing.B) {
	for _, rounds := range []int{100, 400, 1600} {
		messages := toolCompactMessages(rounds, 1)
		b.Run(fmt.Sprintf("rounds=%d", rounds), func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				compactToolHistory(messages, len(messages), nil)
			}
		})
	}
}

// THE POINTER IN A LIVE REQUEST OPENS SOMETHING. Every earlier assertion about
// sources is about a string; this one takes the string the model was actually
// sent, opens the file it names, looks for the id it names, and finds the whole
// result on that line.
func TestAReducedResultSentToTheProviderCanBeReadBackFromTheJournal(t *testing.T) {
	const rounds = 6
	steps := make([]step, 0, rounds+2)
	for round := 0; round < rounds; round++ {
		round := round
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText(
				fmt.Sprintf("call-%d", round),
				"read",
				fmt.Sprintf(`{"round":%d}`, round),
				fmt.Sprintf("working round %d", round),
			), nil
		})
	}
	steps = append(steps,
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("still done"), nil },
	)
	completer := &scriptedCompleter{
		steps: steps,
		aside: func(messages []ai.Message) (*ai.Response, bool) {
			if len(messages) == 0 || messageContentText(messages[0]) != "SYSTEM" {
				return textResponse("aside"), true
			}
			return nil, false
		},
	}
	journal := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = bigTestWindow
		config.CompactEnabled = false
		config.TaskAudit = false
		config.SessionFile = journal
	})
	issued := 0
	for index := range agent.tools {
		if agent.tools[index].Name != "read" {
			continue
		}
		tool := staticTool("read", "")
		tool.Execute = func(context.Context, json.RawMessage) (string, bool, error) {
			text := headedOutput(issued)
			issued++
			return text, false, nil
		}
		agent.tools[index] = tool
		break
	}

	events, err := agent.Submit(context.Background(), "read six times")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	// THE FROZEN REGION IS WHAT EXISTED BEFORE THIS TURN'S FIRST REQUEST, so the
	// six results become reducible only once a second turn asks about them.
	events, err = agent.Submit(context.Background(), "recap that work")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	// Every request the turn made is looked at, because the last one recorded
	// belongs to whichever errand ran after the answer.
	var reduced ai.Message
	for request := 0; request < completer.requests() && reduced.ToolCallID == ""; request++ {
		for _, message := range completer.request(request) {
			if message.Role != "tool" {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(messageContentText(message)), compactReducedMarker) {
				reduced = message
				break
			}
		}
	}
	if reduced.ToolCallID == "" {
		t.Fatal("no reduced result reached the provider, so the pointer went untested")
	}
	text := messageContentText(reduced)
	if !strings.Contains(text, "full: grep "+reduced.ToolCallID+" in "+journal) {
		t.Fatalf("reduced result named no journal to grep: %.240q", text)
	}
	content, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("the pointer names a journal that cannot be read: %v", err)
	}
	found := false
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.Contains(line, reduced.ToolCallID) {
			continue
		}
		var entry struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if strings.Contains(entry.Content, "HEAD-") && strings.Contains(entry.Content, "UNIQUE-TAIL") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("grepping %s for %s found no whole result: the pointer is dead", journal, reduced.ToolCallID)
	}
}
