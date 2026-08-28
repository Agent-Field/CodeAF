package reflex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// thinker is a Completer that behaves the way a reasoning model behaves: told
// it may think, it spends the whole ceiling deliberating and answers with
// nothing; told not to, it answers the question.
//
// It is the fake this whole file exists for. The bug it reproduces was invisible
// to every other fake here because they all answer whatever the script says
// regardless of what the request asked for, and the request was the problem.
type thinker struct {
	answer string
	// efforts is the reasoning knob each call carried, in order.
	efforts []provider.Effort
	// budgets is the max-tokens ceiling each call carried, in order.
	budgets []int
}

func (t *thinker) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := ai.Request{Messages: messages}
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	effort := provider.ReasoningEffortFrom(ctx)
	t.efforts = append(t.efforts, effort)
	budget := 0
	if request.MaxTokens != nil {
		budget = *request.MaxTokens
	}
	t.budgets = append(t.budgets, budget)
	if effort != provider.EffortOff {
		// The whole ceiling went on the thinking pass. This is exactly what the
		// usage journal recorded against nex-agi/nex-n2-mini: input around 640,
		// output at the cap, and a reply with no text in it.
		return reply(""), nil
	}
	return reply(t.answer), nil
}

// TestAReflexCallTellsAReasoningModelNotToThink is the fix for aux calls that
// billed for nothing.
//
// A reflex call is a sort, not a thought. Left free to deliberate, a reasoning
// model on the reflex tier spends the entire 200-token ceiling doing it and
// returns an empty answer — several times a turn, every turn, billed in full.
// The request now carries provider.EffortOff, so the ceiling buys the answer.
func TestAReflexCallTellsAReasoningModelNotToThink(t *testing.T) {
	model := &thinker{answer: `{"inject":["m3"]}`}

	result, err := Route(context.Background(), model, "what did we decide about the tests?", index)
	if err != nil {
		t.Fatalf("Route against a reasoning model: %v", err)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "m3" {
		t.Fatalf("Route returned %#v, want the one routed line", result)
	}
	if len(model.efforts) != 1 {
		t.Fatalf("Route made %d calls, want one — a call that answered needs no repair", len(model.efforts))
	}
	if model.efforts[0] != provider.EffortOff {
		t.Fatalf("the reflex asked for effort %q, want %q — a reasoning model burns the whole ceiling otherwise",
			model.efforts[0], provider.EffortOff)
	}
	if model.budgets[0] != answerTokens {
		t.Fatalf("ceiling was %d, want the ordinary %d", model.budgets[0], answerTokens)
	}
}

// TestAnUncappedEmptyReflexAnswerIsNotRepaired pins the boundary around the
// budget recovery.
//
// The repair retry works by putting the model's own bad answer in front of it
// and asking again. There is no such answer when the reply was empty, so the
// repair is a second full-price call asking the identical question of a model
// that gave no evidence more room would help. Only finish=length at the ceiling
// gets that retry; an ordinary blank remains one call.
func TestAnUncappedEmptyReflexAnswerIsNotRepaired(t *testing.T) {
	silent := &fake{replies: []string{"", ""}}

	_, err := Route(context.Background(), silent, "what did we decide?", index)
	if !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("Route error = %v, want %v", err, ErrReflexFailed)
	}
	if len(silent.calls) != 1 {
		t.Fatalf("an empty answer cost %d calls, want exactly one", len(silent.calls))
	}

	// A reply that is wrong but PRESENT is still worth one repair: that is the
	// case the retry was written for and this must not have taken it away.
	fenced := &fake{replies: []string{"here you go:", `{"inject":[]}`}}
	if _, err := Route(context.Background(), fenced, "what did we decide?", index); err != nil {
		t.Fatalf("Route after a repairable answer: %v", err)
	}
	if len(fenced.calls) != 2 {
		t.Fatalf("a repairable answer cost %d calls, want two", len(fenced.calls))
	}
}

// TestABoundReasoningMandatoryModelGetsRoomToThink is the honest answer to the
// model that refuses the disable.
//
// provider.ReasoningMandatory is learned from a 400, never guessed, and when it
// is true the disable cannot travel. Asking such a model a question inside a
// 200-token ceiling is asking for the empty answer again, so the ceiling makes
// room for the thinking pass instead.
func TestABoundReasoningMandatoryModelGetsRoomToThink(t *testing.T) {
	const stubborn = "test-vendor/always-thinks"
	// The refusal is seeded the way a previous process would have left it: on
	// disk, read back by the loader. Nothing in this package may reach into the
	// adapter's memo directly, and a test that could would be pinning a shape
	// production does not have.
	dir := t.TempDir()
	quirks := `{"reasoning_mandatory":{"` + stubborn + `":"2026-08-19T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "model-quirks.json"), []byte(quirks), 0o600); err != nil {
		t.Fatalf("seed the refusal: %v", err)
	}
	provider.LoadQuirks(dir)
	if !provider.ReasoningMandatory(stubborn) {
		t.Fatal("the seeded refusal did not load; the rest of this test would prove nothing")
	}

	model := &thinker{answer: `{"inject":[]}`}
	if _, err := Route(context.Background(), Bind(model, stubborn), "anything at all?", index); err != nil {
		t.Fatalf("Route on a reasoning-mandatory model: %v", err)
	}
	if got := model.budgets[0]; got != thinkingAnswerTokens {
		t.Fatalf("ceiling for a reasoning-mandatory model was %d, want %d", got, thinkingAnswerTokens)
	}

	// A model nobody has been refused by keeps the ordinary ceiling.
	plain := &thinker{answer: `{"inject":[]}`}
	if _, err := Route(context.Background(), Bind(plain, "test-vendor/ordinary"), "anything at all?", index); err != nil {
		t.Fatalf("Route on an ordinary bound model: %v", err)
	}
	if got := plain.budgets[0]; got != answerTokens {
		t.Fatalf("ceiling for an ordinary model was %d, want %d", got, answerTokens)
	}
}

func cappedEmpty(tokens int) *ai.Response {
	response := reply(" \n")
	response.Choices[0].FinishReason = "length"
	response.Usage = &ai.Usage{CompletionTokens: tokens}
	return response
}

func TestAnEmptyLengthCappedAnswerRetriesOnceWithRoom(t *testing.T) {
	const model = "test-vendor/silent-disable"
	quirksAt := filepath.Join(t.TempDir(), "quirks")
	provider.LoadQuirks(quirksAt)
	client := &fake{responses: []*ai.Response{
		cappedEmpty(answerTokens),
		reply(`{"inject":["m3"],"cmd":null}`),
		reply(`{"inject":[],"cmd":null}`),
	}}
	session := &Session{}
	bound := session.Bind(client, model, "test-vendor/low", nil)

	result, err := Route(context.Background(), bound, "what did we decide about tests?", index)
	if err != nil {
		t.Fatalf("Route after the larger retry: %v", err)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "m3" {
		t.Fatalf("Route returned %+v, want the retry's answer", result)
	}
	if len(client.calls) != 2 {
		t.Fatalf("the capped answer made %d calls, want one retry", len(client.calls))
	}
	if got := *client.calls[0].MaxTokens; got != answerTokens {
		t.Fatalf("first ceiling = %d, want %d", got, answerTokens)
	}
	if got := *client.calls[1].MaxTokens; got != thinkingAnswerTokens {
		t.Fatalf("retry ceiling = %d, want %d", got, thinkingAnswerTokens)
	}
	if !provider.ReasoningDisableIgnored(model) {
		t.Fatal("the answer at the larger ceiling did not teach the model quirk")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, readErr := os.ReadFile(filepath.Join(quirksAt, "model-quirks.json"))
		if readErr == nil && strings.Contains(string(raw), model) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the learned quirk was not persisted: %v, %s", readErr, raw)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := Route(context.Background(), bound, "what else did we decide?", index); err != nil {
		t.Fatalf("later Route: %v", err)
	}
	if got := *client.calls[2].MaxTokens; got != thinkingAnswerTokens {
		t.Fatalf("later ceiling = %d, want the learned %d", got, thinkingAnswerTokens)
	}
}

func TestTwoEmptyLengthCappedAnswersFallBackOnceAndSaySoOnce(t *testing.T) {
	const primary = "test-vendor/never-answers"
	const low = "test-vendor/low"
	client := &fake{responses: []*ai.Response{
		cappedEmpty(answerTokens),
		cappedEmpty(thinkingAnswerTokens),
		reply(`{"inject":["m7"],"cmd":null}`),
		reply(`{"inject":[],"cmd":null}`),
	}}
	var notices []string
	session := &Session{}
	bound := session.Bind(client, primary, low, func(text string) {
		notices = append(notices, text)
	})

	result, err := Route(context.Background(), bound, "what themes do I like?", index)
	if err != nil {
		t.Fatalf("Route after fallback: %v", err)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "m7" {
		t.Fatalf("Route returned %+v, want the low tier's answer", result)
	}
	if len(client.calls) != 3 {
		t.Fatalf("fallback used %d calls, want small, larger, then low", len(client.calls))
	}
	for call, want := range []string{primary, primary, low} {
		if got := client.calls[call].Model; got != want {
			t.Fatalf("call %d model = %q, want %q", call+1, got, want)
		}
	}
	if len(notices) != 1 || notices[0] !=
		"the reflex model answers nothing at its budget; using "+low+" for this session" {
		t.Fatalf("notices = %q, want the one fallback line", notices)
	}

	if _, err := Route(context.Background(), bound, "what themes should I use?", index); err != nil {
		t.Fatalf("later Route on fallback: %v", err)
	}
	if len(client.calls) != 4 || client.calls[3].Model != low {
		t.Fatalf("later calls = %+v, want the low tier directly", client.calls)
	}
	if len(notices) != 1 {
		t.Fatalf("fallback was announced %d times, want once", len(notices))
	}
}

func TestANormalAnswerDoesNotBuyTheThinkingRetry(t *testing.T) {
	client := &fake{responses: []*ai.Response{reply(`{"inject":[],"cmd":null}`)}}
	if _, err := Route(context.Background(), client, "what did we decide?", index); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("a normal answer cost %d calls, want one", len(client.calls))
	}
}
