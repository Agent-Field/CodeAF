package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the harness ─────────────────────────────────────────────────────────────

// lookSaid is what a second model wrote about the picture in the measured
// vector-db-brief t2 turn (qloop run 12): the bars in the order the chart draws
// them.
const lookSaid = "Bars (Left to Right): pgvector ~$2,100, Pinecone ~$3,100, Weaviate ~$2,600, Qdrant ~$2,300, Milvus ~$2,800."

// wrongReply is the reply that turn sent: the bars in an order the chart does
// not show.
const wrongReply = "Pinecone is now $3,100.\n\n- Cost chart bars now: pgvector $2,100, Qdrant $2,300, Weaviate $2,600, Milvus $2,800, Pinecone $3,100."

// rightReply is the reply the turn should have sent.
const rightReply = "Pinecone is now $3,100, and the cost chart shows it second from the left."

// lookTool answers the way `view_image` does when a second model looks: "seen
// by <model>: <words>". The guard reads the result, never the tool's name.
func lookTool(ran *atomic.Int64) bare.Tool {
	return bare.Tool{
		Name: "look", Description: "looks at the work",
		Schema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			ran.Add(1)
			return "seen by stub/vision: " + lookSaid, false, nil
		},
	}
}

// changeTool is a step that may change the work: a shell line, as the canvas is
// reached, is never one the approval floor can call read-only.
func changeTool(name string, ran *atomic.Int64, fails bool) bare.Tool {
	return bare.Tool{
		Name: name, Description: "changes the work",
		Schema: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			ran.Add(1)
			if fails {
				return "usage: unknown command \"node\"", true, nil
			}
			return `{"seq":136}`, false, nil
		},
	}
}

func callStep(id, name, arguments string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, name, arguments), nil
	}
}

func replyStep(text string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(text), nil }
}

// unreachable is the step after the turn should have ended.
var unreachable = replyStep("unreachable: the turn should have ended")

// isReplyCheckCall recognises the reader's request by its ask.
func isReplyCheckCall(messages []ai.Message) bool {
	return len(messages) == 1 && strings.Contains(messageText(messages[0]), replyCheckAsk)
}

// replyCheckAgent is an agent whose reply-check role has a model, answered off
// the queue by shape with whatever answer says.
func replyCheckAgent(t *testing.T, steps []step, answer string, tools ...bare.Tool) (*Agent, *scriptedCompleter, *atomic.Int64) {
	t.Helper()
	var asked atomic.Int64
	completer := &scriptedCompleter{steps: steps, aside: func(messages []ai.Message) (*ai.Response, bool) {
		if !isReplyCheckCall(messages) {
			return nil, false
		}
		asked.Add(1)
		return textResponse(answer), true
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{roles.PinKey(roles.RoleReplyCheck): "check/model"})
	})
	agent.tools = append(agent.tools, tools...)
	return agent, completer, &asked
}

func runTurnOn(t *testing.T, agent *Agent, ask string) []Event {
	t.Helper()
	events, err := agent.Submit(context.Background(), ask)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return collect(t, events)
}

func lastAssistantText(agent *Agent) string {
	return checkpointLastSaid(agent.snapshot())
}

// ── the contradicted look ───────────────────────────────────────────────────

// THE MEASURED SHAPE: the turn changed the work, looked at it after the change,
// and replied with something the look contradicts. The reader names it, the
// model is sent back once with the reader's line in hand, and the corrected
// reply is the one the turn ends on.
func TestAReplyTheLookContradictsIsSentBackOnceWithTheLine(t *testing.T) {
	var looked, changed atomic.Int64
	finding := `- "Cost chart bars now: pgvector, Qdrant, Weaviate, Milvus, Pinecone" — the look shows pgvector, Pinecone, Weaviate, Qdrant, Milvus.`
	agent, completer, asked := replyCheckAgent(t, []step{
		callStep("c1", "change", `{}`),
		callStep("l1", "look", `{"question":"List the bars left to right"}`),
		replyStep(wrongReply),
		replyStep(rightReply),
		unreachable,
	}, "Here is what I found:\n"+finding, lookTool(&looked), changeTool("change", &changed, false))
	collected := runTurnOn(t, agent, "pinecone came back with a quote, 3100/mo. update it")
	if got, want := completer.requests(), 4; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if got := asked.Load(); got != 1 {
		t.Fatalf("the reader was asked %d times; want 1", got)
	}
	sent := completer.request(3)
	lead := messageText(sent[len(sent)-1])
	if !isReplyCheckLead(lead) || !strings.Contains(lead, finding) {
		t.Fatalf("the send-back was %q; want the contradicted arm carrying the reader's line", lead)
	}
	if !saidSomething(noticeTexts(collected), replyCheckNote) {
		t.Errorf("the person was not told; notices were %q", noticeTexts(collected))
	}
	if got := lastAssistantText(agent); got != rightReply {
		t.Errorf("the turn ended on %q; want the corrected reply", got)
	}
}

// AND IT DOES NOT LOOP. A reader that would object to every reply is asked once,
// and the second reply — still wrong — is the one the turn ends on.
func TestTheReplyCheckIsOnceATurnEvenWhenTheSecondReplyIsWrongToo(t *testing.T) {
	var looked, changed atomic.Int64
	agent, completer, asked := replyCheckAgent(t, []step{
		callStep("c1", "change", `{}`),
		callStep("l1", "look", `{}`),
		replyStep(wrongReply),
		replyStep(wrongReply + " (again)"),
		unreachable,
	}, `- "bars now" — the look shows another order.`, lookTool(&looked), changeTool("change", &changed, false))
	runTurnOn(t, agent, "update it")
	if got, want := completer.requests(), 4; got != want {
		t.Fatalf("the loop made %d requests; want %d — one send-back at most", got, want)
	}
	if got := asked.Load(); got != 1 {
		t.Fatalf("the reader was asked %d times; want exactly 1", got)
	}
	if n := strings.Count(transcriptText(agent), replyCheckLeadOpening); n != 1 {
		t.Errorf("the send-back reached the transcript %d times; want 1", n)
	}
}

// A REPLY THE MODEL STANDS BY COSTS ONE SHORT CALL AND IS NEVER WRITTEN TWICE.
func TestNoChangeAfterTheReplyCheckEndsTheTurnOnTheFirstReply(t *testing.T) {
	var looked, changed atomic.Int64
	agent, completer, _ := replyCheckAgent(t, []step{
		callStep("c1", "change", `{}`),
		callStep("l1", "look", `{}`),
		replyStep(wrongReply),
		replyStep(NoChangeReply),
		unreachable,
	}, `- "bars now" — the look shows another order.`, lookTool(&looked), changeTool("change", &changed, false))
	runTurnOn(t, agent, "update it")
	if got, want := completer.requests(), 4; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if got := lastAssistantText(agent); got != wrongReply {
		t.Errorf("the standing reply is %q; want the first reply kept", got)
	}
}

// A READER THAT FINDS NOTHING, AND A READER NOBODY CAN REACH, BOTH LET THE
// REPLY GO OUT ON THE FIRST REQUEST.
func TestAReplyTheLookBearsOutGoesOutAsWritten(t *testing.T) {
	for name, answer := range map[string]string{
		"none":             "NONE",
		"prose, not lines": "The reply looks consistent with the look.",
	} {
		t.Run(name, func(t *testing.T) {
			var looked, changed atomic.Int64
			agent, completer, asked := replyCheckAgent(t, []step{
				callStep("c1", "change", `{}`),
				callStep("l1", "look", `{}`),
				replyStep(rightReply),
				unreachable,
			}, answer, lookTool(&looked), changeTool("change", &changed, false))
			collected := runTurnOn(t, agent, "update it")
			if got, want := completer.requests(), 3; got != want {
				t.Fatalf("the loop made %d requests; want %d", got, want)
			}
			if asked.Load() != 1 {
				t.Errorf("the reader was asked %d times; want 1", asked.Load())
			}
			if saidSomething(noticeTexts(collected), replyCheckNote) {
				t.Errorf("a clean reply was sent back")
			}
		})
	}
	t.Run("no model for the role", func(t *testing.T) {
		var looked, changed atomic.Int64
		completer := &scriptedCompleter{steps: []step{
			callStep("c1", "change", `{}`), callStep("l1", "look", `{}`), replyStep(wrongReply), unreachable,
		}}
		agent := loopAgent(t, completer, lookTool(&looked), changeTool("change", &changed, false))
		runTurnOn(t, agent, "update it")
		if got, want := completer.requests(), 3; got != want {
			t.Fatalf("the loop made %d requests; want %d — the arm is absent with no model", got, want)
		}
	})
}

// A TURN THAT NEVER LOOKED IS NEVER READ: there is nothing to compare with, and
// an ordinary turn pays nothing.
func TestATurnThatNeverLookedPaysNoReader(t *testing.T) {
	var changed atomic.Int64
	agent, completer, asked := replyCheckAgent(t, []step{
		callStep("c1", "change", `{}`),
		replyStep(wrongReply),
		unreachable,
	}, `- "anything" — objection.`, changeTool("change", &changed, false))
	runTurnOn(t, agent, "update it")
	if got, want := completer.requests(), 2; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if asked.Load() != 0 {
		t.Errorf("the reader was asked %d times on a turn with no look", asked.Load())
	}
}

// ── the stale look ──────────────────────────────────────────────────────────

// THE TURN LOOKED, THEN CHANGED THE WORK, THEN DESCRIBED IT: it is sent back to
// look again, with no reader spent, and only once.
func TestAReplyAfterAChangeSinceTheLastLookIsSentBackToLookAgain(t *testing.T) {
	var looked, changed atomic.Int64
	agent, completer, asked := replyCheckAgent(t, []step{
		callStep("l1", "look", `{}`),
		callStep("c1", "change", `{}`),
		replyStep(wrongReply),
		callStep("l2", "look", `{}`),
		replyStep(rightReply),
		unreachable,
	}, `- "bars now" — objection.`, lookTool(&looked), changeTool("change", &changed, false))
	collected := runTurnOn(t, agent, "update it")
	if got, want := completer.requests(), 5; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if sent := completer.request(3); messageText(sent[len(sent)-1]) != replyStaleLookLead {
		t.Fatalf("the send-back was %q; want the stale look", messageText(sent[len(sent)-1]))
	}
	if asked.Load() != 0 {
		t.Errorf("the reader was asked %d times; the stale arm spends none and the guard is spent", asked.Load())
	}
	if !saidSomething(noticeTexts(collected), replyStaleLookNote) {
		t.Errorf("the person was not told; notices were %q", noticeTexts(collected))
	}
	if looked.Load() != 2 {
		t.Errorf("looked %d times; want the second look the send-back asked for", looked.Load())
	}
}

func TestLookedAtTheWorkReadsTheTurnNotTheSession(t *testing.T) {
	look := func(id string) []ai.Message {
		return []ai.Message{
			{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: id, Function: ai.ToolCallFunction{Name: "view_image", Arguments: `{"question":"q"}`}}}},
			{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: "seen by m: " + lookSaid}}},
		}
	}
	change := []ai.Message{
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "b", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"canvas patch cv n --json '{}'"}`}}}},
		{Role: "tool", ToolCallID: "b", Content: []ai.ContentPart{{Type: "text", Text: `{"seq":3}`}}},
	}
	read := []ai.Message{
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "r", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a"}`}}}},
		{Role: "tool", ToolCallID: "r", Content: []ai.ContentPart{{Type: "text", Text: "text"}}},
	}
	person := textMessage("user", "update it")
	cat := func(parts ...[]ai.Message) []ai.Message {
		var out []ai.Message
		for _, part := range parts {
			out = append(out, part...)
		}
		return out
	}
	for name, row := range map[string]struct {
		messages []ai.Message
		looks    int
		stale    bool
	}{
		"change then look":                     {cat([]ai.Message{person}, change, look("v1")), 1, false},
		"look then change":                     {cat([]ai.Message{person}, look("v1"), change), 1, true},
		"look then a read":                     {cat([]ai.Message{person}, look("v1"), read), 1, false},
		"yesterday's look is not this turn's":  {cat(look("v0"), []ai.Message{person}, change), 0, false},
		"a send-back does not open a new turn": {cat([]ai.Message{person}, look("v1"), change, []ai.Message{textMessage("user", replyStaleLookLead)}), 1, true},
	} {
		t.Run(name, func(t *testing.T) {
			looks, stale := lookedAtTheWork(row.messages)
			if len(looks) != row.looks || stale != row.stale {
				t.Errorf("lookedAtTheWork = %d looks, stale %v; want %d, %v", len(looks), stale, row.looks, row.stale)
			}
		})
	}
}

func TestReplyCheckPageIsBoundedAndKeepsTheNewestLook(t *testing.T) {
	old := turnLook{seen: strings.Repeat("old ", 3000)}
	newest := turnLook{question: "bars?", seen: lookSaid}
	page := replyCheckPage(strings.Repeat("r", 10000), []turnLook{old, old, old, newest})
	if !strings.Contains(page, lookSaid) {
		t.Fatalf("the newest look was dropped")
	}
	if limit := replyCheckLookBytes + replyCheckReplyBytes + 200; len(page) > limit {
		t.Errorf("the page is %d bytes; want at most %d", len(page), limit)
	}
	if replyCheckPage(wrongReply, []turnLook{{}}) != "" {
		t.Errorf("a look with no words gave the reader a page")
	}
}

func TestReplyCheckFindingsReadOnlyTheAskedShape(t *testing.T) {
	for answer, want := range map[string]int{
		"NONE":                      0,
		"none.":                     0,
		"":                          0,
		"It all matches.":           0,
		"- one\n- two":              2,
		"Found:\n- one\nnot a line": 1,
		strings.Repeat("- x\n", 9):  replyCheckFindings,
	} {
		if got := len(replyCheckFindingsOf(answer)); got != want {
			t.Errorf("replyCheckFindingsOf(%q) gave %d lines; want %d", answer, got, want)
		}
	}
}

// AND THE HARNESS'S OWN WORDS NEVER REACH THE PERSON AS THEIRS.
func TestTheReplyCheckLeadsAreContinuationContext(t *testing.T) {
	for _, lead := range []string{replyStaleLookLead, replyCheckLead([]string{"- x"}), failureCheckLead([]string{"- bash: usage"})} {
		if !isContinuationContext(lead) {
			t.Errorf("%q is not continuation context", lead)
		}
	}
	for _, banned := range []string{"verified", "auditor", "verdict", "refuted"} {
		for _, said := range []string{replyStaleLookNote, replyCheckNote, failureCheckNote} {
			if strings.Contains(strings.ToLower(said), banned) {
				t.Errorf("%q says %q", said, banned)
			}
		}
	}
}
