package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// assertClickableAsk is the whole of 13.3 bug 1's producing half, stated once.
//
// A question the head asks has to be three things at the same time, and before
// this wave it was only ever two of them plus a smuggled copy of the third:
//
//   - a row a person can CLICK — the durable typed options, in order, so a reply
//     of "2" means the second one on every surface;
//   - a block a renderer can DRAW without parsing prose — the question part,
//     saying how it is spelled and whose answer it is;
//   - a line a person can READ — prose, with the choices numbered in it, and no
//     machinery anywhere in it.
//
// The last one is what the old chat reads, so it is asserted as prose AND as
// the absence of a payload: a body that still carried `{"kind":...}` would pass
// every other assertion here and still be the bug.
func assertClickableAsk(t *testing.T, message store.Message, prompt string, labels ...string) {
	t.Helper()

	if !strings.HasPrefix(message.Body, prompt) {
		t.Fatalf("the ask does not open with its question: %q", message.Body)
	}
	if strings.Contains(message.Body, `"kind":`) || strings.Contains(message.Body, "```") {
		t.Fatalf("the ask still smuggles machinery through its body: %q", message.Body)
	}
	if len(message.Options) != len(labels) {
		t.Fatalf("the ask offers %d durable options, want %d: %+v", len(message.Options), len(labels), message.Options)
	}
	for index, label := range labels {
		if message.Options[index].Label != label {
			t.Fatalf("option %d is %q, want %q", index+1, message.Options[index].Label, label)
		}
		// The number a person reads is the number their reply carries, and the
		// old chat recovers exactly this spelling.
		if row := "▸ " + string(rune('1'+index)) + ". " + label; !strings.Contains(message.Body, row) {
			t.Fatalf("the body does not carry %q as a readable numbered row: %q", row, message.Body)
		}
	}

	part, found := questionPartOf(message)
	if !found {
		t.Fatalf("the ask carries no question part, so a renderer would have to scan its prose: %+v", message.Parts)
	}
	if part.Kind != store.QuestionChoose {
		t.Fatalf("the question part is drawn as %q, want %q", part.Kind, store.QuestionChoose)
	}
	if part.Class != store.QuestionConsent {
		t.Fatalf("the question part is class %q — an ask the head minted is the person's to answer", part.Class)
	}
	if text, ok := textPartOf(message); !ok || text != prompt {
		t.Fatalf("the prompt does not ride as its own text part: %q ok=%t", text, ok)
	}
}

func questionPartOf(message store.Message) (store.QuestionPart, bool) {
	for _, part := range message.Parts {
		if part.Kind == store.PartQuestion && part.Question != nil {
			return *part.Question, true
		}
	}
	return store.QuestionPart{}, false
}

func textPartOf(message store.Message) (string, bool) {
	for _, part := range message.Parts {
		if part.Kind == store.PartText {
			return part.Text, true
		}
	}
	return "", false
}

// The ask tool's question is the commonest one in the product and was the one
// 13.3 caught on screen as raw JSON. Everything about it is now typed, and the
// part says the one thing the options cannot: that the loop is waiting on the
// person, not on itself.
func TestTheAskToolPostsATypedQuestionAndAHumaneBody(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("a1", beltToolAsk, map[string]any{
			"question": "Which report did you mean?",
			"options":  []string{"the quarterly one", "the board pack"}})}},
	}}
	user := postUser(t, graph, "asking", "redo the report")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "asking", user.Seq)
	assertClickableAsk(t, reply, "Which report did you mean?", "the quarterly one", "the board pack")

	part, _ := questionPartOf(reply)
	// Seq zero is a statement, not an omission: this ask has no lifecycle row
	// behind it, and the answer comes back to the loop that asked.
	if part.Seq != 0 {
		t.Fatalf("a conversational askback claimed durable question %d", part.Seq)
	}
	if !part.AllowFree {
		t.Fatal("the ask refuses free text, which no conversational askback does")
	}
	if !isAskQuestion(reply.Options) {
		t.Fatalf("the loop's own answer routing was lost: %+v", reply.Options)
	}
}

// The consent gate's question is the one whose body may NOT go humane, and the
// reason is 11.1 rather than taste: the numbered spelling cannot say which
// option stands if the person says nothing, and on this question the answer to
// that is "keep it". So the payload stays for the chat that reads only bodies,
// and the parts carry the same facts for the chat that reads blocks — with the
// gate's own semantics, not an askback's, in them.
func TestTheConsentGateKeepsItsDefaultAndSaysSoInTypes(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	head := New(nil, graph)
	user := postUser(t, graph, "gate", "cancel the research")

	node, found, err := graph.Node("research")
	if err != nil || !found {
		t.Fatalf("read node: found=%t err=%v", found, err)
	}
	// A cascade past the gate's own threshold: the question is owed, and what it
	// offers is what the person is consenting to.
	impact := store.SurgeryImpact{OpenNodes: SurgeryCascadeGateNodes + 1, Nodes: SurgeryCascadeGateNodes + 1}
	asked, err := head.askSurgeryConfirm(user.SessionID, node, store.CommandCancel,
		"research", "cancel it", impact)
	if err != nil || !asked {
		t.Fatalf("the gate did not ask: asked=%t err=%v", asked, err)
	}

	message := askedQuestion(t, graph, "gate", user.Seq)
	part, ok := questionPartOf(message)
	if !ok {
		t.Fatalf("the consent question carries no part: %+v", message.Parts)
	}
	if part.Kind != store.QuestionConfirm {
		t.Fatalf("the gate is drawn as %q, want %q — the inline strip is a different shape from a list", part.Kind, store.QuestionConfirm)
	}
	if part.Default != "2" {
		t.Fatalf("the part lost the default that keeps the work: %q", part.Default)
	}
	if part.AllowFree {
		t.Fatal("the part offers free text on a gate that refuses it")
	}
	if part.Class != store.QuestionConsent || part.Category != store.QuestionCategorySurgeryConfirm {
		t.Fatalf("the gate's own axis was dropped: class=%q category=%q", part.Class, part.Category)
	}
	if part.Seq == 0 {
		t.Fatal("a consent question with no durable row behind it cannot be answered later")
	}
	if part.NodeID != "research" {
		t.Fatalf("the part does not say what the gate is about: %q", part.NodeID)
	}
	// And the body is untouched, because the chat that has never heard of parts
	// reads the body and only the body.
	if !strings.Contains(message.Body, `"default":"2"`) {
		t.Fatalf("the existing chat lost the gate's default: %q", message.Body)
	}
	if text, ok := textPartOf(message); !ok || strings.Contains(text, "{") {
		t.Fatalf("the prompt part carries machinery: %q ok=%t", text, ok)
	}
}

// The prompt a text part carries is recovered from the body the durable row
// holds, so it has to survive both spellings this package writes and nothing
// else. A decoder that guessed would put a fence on screen the first time a
// producer wrote one differently.
func TestTheQuestionPromptSurvivesBothBodySpellings(t *testing.T) {
	options := []store.QuestionOption{{Label: "yes, cancel it"}, {Label: "keep it running"}}
	allowFree := false
	payload := store.QuestionMessageBody("Cancel Line scans? 3 steps", options,
		store.QuestionConfig{Kind: store.QuestionConfirm, Default: "2", AllowFree: &allowFree})
	humane := store.HumaneQuestionBody("Cancel Line scans? 3 steps", options)

	for name, body := range map[string]string{"payload": payload, "humane": humane} {
		if prompt := store.QuestionPrompt(body); prompt != "Cancel Line scans? 3 steps" {
			t.Fatalf("%s body: prompt read back as %q", name, prompt)
		}
	}
	if strings.Contains(humane, "```") || strings.Contains(humane, `"kind"`) {
		t.Fatalf("the humane body is not humane: %q", humane)
	}
	if !strings.Contains(humane, "▸ 1. yes, cancel it") || !strings.Contains(humane, "▸ 2. keep it running") {
		t.Fatalf("the humane body dropped the numbered rows the old chat parses: %q", humane)
	}
}
