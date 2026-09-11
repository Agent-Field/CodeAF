package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE WALL KEEPS WHAT WAS THOUGHT (issue #927) ─────────────────────────────
//
// A structuring call on a reasoning model thought for its whole four-minute
// wall, was cut, and handed its caller nothing: every token it had reasoned was
// billed and thrown away, and the command that asked for it struck twice and
// told the person the model had stopped answering. These are that call, at the
// scale a test can run: the wall is tens of milliseconds, the model is a stub
// that streams its thought through the same observer door the provider does,
// and nothing here sleeps — the only clock is the wall's own.

// thoughtWall is the wall these scenarios run under. Nothing waits it out except
// a completion that is meant to reach it. The answer ask runs under it too, and
// cannot lose a race to it: the stub answers that ask before anything reads a
// clock, and a completion that returns an answer is kept whatever its timer says.
const thoughtWall = 20 * time.Millisecond

// sent is one request the thinking model received.
type sent struct {
	messages []ai.Message
	options  int
	effort   provider.Effort
	// left is how long the completion had when it was sent — its context's own
	// deadline, read at the door.
	left time.Duration
}

// thinkingModel is the model from the incident: it thinks, out loud on the
// stream, and does not stop until its context does. When `answer` is set it is
// what the model says to a request that tells it to stop thinking — the answer
// ask — and when it is empty the model thinks through that one too.
type thinkingModel struct {
	thought, begun, answer string
	// forming, when set, is a tool call the model starts to write after its
	// thought: the name, then its arguments as far as they get.
	forming *provider.StreamEvent
	// cancel, when set, is called once the thought is on the wire: the caller
	// going away mid-thought, without anybody sleeping to arrange it.
	cancel context.CancelFunc
	// failsWith, when set, is what every thinking call ends with instead of
	// waiting for its context: a bound UNDER the wall — the dispatcher's
	// patience for one attempt, the stream guard giving up — which ends the
	// completion while the wall and the caller are both still waiting.
	failsWith error

	mu    sync.Mutex
	asked []sent
	// first is the context the first completion was sent with, kept so a test
	// can speak on its stream after the completion has ended.
	first context.Context
}

func (m *thinkingModel) Model() string { return "thinking/model" }

func (m *thinkingModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	m.mu.Lock()
	var left time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		left = time.Until(deadline)
	}
	m.asked = append(m.asked, sent{messages: messages, options: len(options), effort: provider.ReasoningEffortFrom(ctx), left: left})
	turn := len(m.asked)
	if turn == 1 {
		m.first = ctx
	}
	m.mu.Unlock()

	if turn > 1 && m.answer != "" {
		return answered(m.answer), nil
	}
	provider.EmitEvent(ctx, provider.StreamEvent{Kind: provider.StreamThinking})
	provider.EmitEvent(ctx, provider.StreamEvent{Kind: provider.StreamReasoning, Delta: m.thought})
	if m.begun != "" {
		provider.Emit(ctx, provider.StreamDelta, m.begun)
	}
	if m.forming != nil {
		provider.EmitEvent(ctx, *m.forming)
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.failsWith != nil {
		return nil, m.failsWith
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *thinkingModel) requests() []sent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sent(nil), m.asked...)
}

func answered(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}}
}

func textOf(message ai.Message) string {
	var text strings.Builder
	for _, part := range message.Content {
		text.WriteString(part.Text)
	}
	return text.String()
}

func planAsk() []ai.Message {
	return []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: "Ground the request."}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "a long issue body"}}},
	}
}

// TestAThoughtCutAtTheWallIsAskedForItsAnswer is the incident, fixed: the call
// reaches its wall with thought on the wire, and what the caller gets is the
// answer that thought reached — asked for once, on the same request, with the
// thought in front of the model and its thinking switched off.
func TestAThoughtCutAtTheWallIsAskedForItsAnswer(t *testing.T) {
	model := &thinkingModel{thought: "Two settled points: the law is structural, and it walks the tree.", answer: `{"points":["structural","walks the tree"]}`}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(thoughtWall)

	response, err := client.CompleteWithMessages(context.Background(), planAsk(), ai.WithJSONMode())
	if err != nil {
		t.Fatalf("a call whose thought reached its wall failed instead of answering: %v", err)
	}
	if response.Text() != model.answer {
		t.Fatalf("answer = %q, want the answer ask's %q", response.Text(), model.answer)
	}

	requests := model.requests()
	if len(requests) != 2 {
		t.Fatalf("the model was asked %d times, want the call and one answer ask", len(requests))
	}
	first, second := requests[0], requests[1]
	// THE SECOND ASK IS THE SAME REQUEST, CARRYING WHAT WAS THOUGHT. The caller's
	// own messages lead it unchanged — the same lineage, the same prefix — then
	// the thought as the assistant turn it was, then the ask.
	if len(second.messages) != len(first.messages)+2 {
		t.Fatalf("answer ask has %d messages, want the %d asked plus the thought and the ask", len(second.messages), len(first.messages))
	}
	for index, message := range first.messages {
		if textOf(second.messages[index]) != textOf(message) || second.messages[index].Role != message.Role {
			t.Fatalf("answer ask rewrote message %d of the caller's request", index)
		}
	}
	thought := second.messages[len(first.messages)]
	if thought.Role != "assistant" || textOf(thought) != model.thought {
		t.Fatalf("the thought travelled as %s %q, want the assistant's own %q", thought.Role, textOf(thought), model.thought)
	}
	if ask := second.messages[len(second.messages)-1]; ask.Role != "user" || textOf(ask) != answerNowPrompt {
		t.Fatalf("the last message is %s %q, want the answer ask", ask.Role, textOf(ask))
	}
	// THINKING IS OFF FOR THE ASK AND ONLY FOR THE ASK, and the request's own
	// shape still travels: it asks for a whole answer, which has one.
	if first.effort != provider.EffortNone {
		t.Fatalf("the call itself went out with effort %q; the wall changes nothing on it but the budget", first.effort)
	}
	if second.effort != provider.EffortOff {
		t.Fatalf("the answer ask went out with effort %q, want thinking switched off", second.effort)
	}
	if second.options != first.options {
		t.Fatalf("the answer ask carried %d options, the call %d; the answer is asked for in the request's own shape", second.options, first.options)
	}
}

// TestABegunAnswerTravelsWithItsThought covers a model cut after it had started
// writing: what it had begun is part of what it had worked out.
func TestABegunAnswerTravelsWithItsThought(t *testing.T) {
	model := &thinkingModel{thought: "Settled.", begun: `{"points":["struct`, answer: `{"points":["structural"]}`}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(thoughtWall)

	if _, err := client.CompleteWithMessages(context.Background(), planAsk()); err != nil {
		t.Fatal(err)
	}
	requests := model.requests()
	if got := textOf(requests[1].messages[len(planAsk())]); got != model.thought+"\n\n"+model.begun {
		t.Fatalf("the assistant turn was %q, want the thought and then the begun answer", got)
	}
}

// TestAnAnswerAskThatRunsOutOfTimeTooIsTheWall keeps the honest failure. A model
// that thinks through the answer ask as well is a call that died of time, and
// the layer above is told so in the words the watchdog already reads.
func TestAnAnswerAskThatRunsOutOfTimeTooIsTheWall(t *testing.T) {
	model := &thinkingModel{thought: "Still thinking."}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(thoughtWall)

	_, err := client.CompleteWithMessages(context.Background(), planAsk())
	if !errors.Is(err, ErrCallWall) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the call wall, legible as a deadline", err)
	}
	if got := len(model.requests()); got != 2 {
		t.Fatalf("the model was asked %d times, want the call and exactly one answer ask", got)
	}
	if !strings.Contains(err.Error(), "thought past its time") {
		t.Fatalf("the wall's error does not name its cause: %v", err)
	}
}

// TestACallersCancelIsNeverAnsweredFor keeps an interrupt an interrupt: a person
// who stopped the call mid-thought is not asked on behalf of.
func TestACallersCancelIsNeverAnsweredFor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &thinkingModel{thought: "Thinking.", answer: "{}", cancel: cancel}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(time.Minute)

	_, err := client.CompleteWithMessages(ctx, planAsk())
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCallWall) {
		t.Fatalf("error = %v, want the caller's own cancellation and nothing else", err)
	}
	if got := len(model.requests()); got != 1 {
		t.Fatalf("a cancelled call was asked %d times; the caller who left is not answered for", got)
	}
}

// TestSomebodyWatchingIsToldTheAnswerIsBeingAskedFor is the stream's half. What
// the model thought reaches the caller's own observer as it always did; the
// answer ask is announced before its reply arrives — as a replacement when an
// answer had begun on the screen, so the whole answer is not drawn after half
// of one — and nothing the first completion says after it ended is forwarded.
func TestSomebodyWatchingIsToldTheAnswerIsBeingAskedFor(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		begun string
		want  provider.StreamEventKind
	}{
		{"only thought was on the wire", "", provider.StreamNotice},
		{"an answer had begun on the screen", `{"points":`, provider.StreamReplaced},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var mu sync.Mutex
			var seen []provider.StreamEvent
			ctx := provider.WithStreamObserver(context.Background(), func(event provider.StreamEvent) {
				mu.Lock()
				defer mu.Unlock()
				seen = append(seen, event)
			})
			model := &thinkingModel{thought: "Settled.", begun: scenario.begun, answer: "{}"}
			client := Adopt(config.Config{}, model.Model(), model).WithCallWall(thoughtWall)
			if _, err := client.CompleteWithMessages(ctx, planAsk()); err != nil {
				t.Fatal(err)
			}
			// The first completion's stream, spoken on after it ended.
			provider.Emit(model.first, provider.StreamDelta, "a late word from a dead completion")

			mu.Lock()
			defer mu.Unlock()
			var reasoning, announced bool
			for _, event := range seen {
				switch {
				case event.Kind == provider.StreamReasoning && event.Delta == model.thought:
					reasoning = true
				case event.Kind == scenario.want && event.Delta == askingForTheAnswer:
					announced = true
				case event.Delta == "a late word from a dead completion":
					t.Fatal("a word the first completion said after it ended was forwarded into the answer ask's stream")
				}
			}
			if !reasoning {
				t.Fatalf("the thought never reached the caller's observer: %+v", seen)
			}
			if !announced {
				t.Fatalf("the answer ask was not announced as %v: %+v", scenario.want, seen)
			}
		})
	}
}

// TestACallerWithLessTimeThanTheWallStillHasItsAnswerAsked is the deadline the
// review found one layer up: a late round of a command whose rail has less time
// left than a whole call. The first completion is given its share of what the
// caller has — never the slot's full wall — so the wall, not the caller, is what
// cuts it, and the answer ask runs in the time that is left.
func TestACallerWithLessTimeThanTheWallStillHasItsAnswerAsked(t *testing.T) {
	const rail = 80 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), rail)
	defer cancel()
	model := &thinkingModel{thought: "Settled.", answer: `{"points":[]}`}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(time.Hour)

	response, err := client.CompleteWithMessages(ctx, planAsk())
	if err != nil {
		t.Fatalf("a call on a short rail lost its thought to the caller's deadline: %v", err)
	}
	if response.Text() != model.answer {
		t.Fatalf("answer = %q, want the answer ask's", response.Text())
	}
	requests := model.requests()
	if len(requests) != 2 {
		t.Fatalf("the model was asked %d times, want the call and its answer ask", len(requests))
	}
	// The first completion had about half the rail and never the hour; the
	// answer ask had what was left of it.
	if first := requests[0].left; first <= 0 || first > rail/completionsPerCall {
		t.Fatalf("the first completion had %s of an %s rail; it gets its share, so the answer ask still has one", first, rail)
	}
	if second := requests[1].left; second <= 0 || second > rail {
		t.Fatalf("the answer ask had %s, want what was left of the %s rail", second, rail)
	}
}

// TestAHalfSentCallTravelsAsWordsAndIsVoided keeps what a model had begun to
// call. It is answer being written, so it is kept and asked from; it is not an
// instruction, so it travels as words; and a surface that drew it forming is
// told to throw it away before the answer ask's reply arrives.
func TestAHalfSentCallTravelsAsWordsAndIsVoided(t *testing.T) {
	var mu sync.Mutex
	var kinds []provider.StreamEventKind
	ctx := provider.WithStreamObserver(context.Background(), func(event provider.StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, event.Kind)
	})
	model := &thinkingModel{
		forming: &provider.StreamEvent{Kind: provider.StreamToolCallForming, Index: 0, Tool: "write", Delta: `{"path":"notes.md","con`},
		answer:  "done",
	}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(thoughtWall)
	if _, err := client.CompleteWithMessages(ctx, planAsk()); err != nil {
		t.Fatalf("a completion cut while calling a tool kept nothing: %v", err)
	}
	requests := model.requests()
	if len(requests) != 2 {
		t.Fatalf("the model was asked %d times, want the call and its answer ask", len(requests))
	}
	if got := textOf(requests[1].messages[len(planAsk())]); got != `I had begun calling write with: {"path":"notes.md","con` {
		t.Fatalf("the begun call travelled as %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if kinds[len(kinds)-1] != provider.StreamReplaced {
		t.Fatalf("a surface holding a half-drawn call was not told to void it: %v", kinds)
	}
}

// ── the wall reaches the wire ────────────────────────────────────────────────

// believedLedger believes one thing about one machine and nothing else.
type believedLedger struct{ belief lanes.Belief }

func (l believedLedger) Note(lanes.Sighting)           {}
func (l believedLedger) NoteOutcome(lanes.Outcome)     {}
func (l believedLedger) Prime(lanes.Row, float64)      {}
func (l believedLedger) Beliefs(string) []lanes.Belief { return nil }
func (l believedLedger) Belief(id lanes.ID) (lanes.Belief, bool) {
	return l.belief, id == l.belief.ID
}

// TestAWalledPlanningCallCarriesTheBudgetItsWallImplies is the whole chain on a
// real request body: a slot walled at four minutes, a real adapter under it, a
// model that thinks at max when nothing is sent, and a machine believed at 210
// tokens a second. What the endpoint receives says how long the model may think,
// and it is the wall's own arithmetic.
func TestAWalledPlanningCallCarriesTheBudgetItsWallImplies(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	const model, lane = "z-ai/glm-5.3", "Friendli"
	lanes.Default().SetLedger(believedLedger{belief: lanes.Belief{
		ID:   lanes.ID{Model: model, Lane: lane},
		Rate: lanes.Posterior{X: math.Log(210), P: 0.01},
	}})
	t.Cleanup(func() { lanes.Default().SetLedger(nil) })

	var mu sync.Mutex
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"z-ai/glm-5.3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	defer server.Close()
	adapter, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: server.URL, Model: model,
		ReasoningProfile: func(string) (provider.ReasoningProfile, bool) {
			return provider.ReasoningProfile{Mandatory: true, Default: "max"}, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := Adopt(config.Config{}, model, adapter).WithCallWall(DefaultCallWall)

	ctx := provider.WithLaneChoice(context.Background(), lanes.Choice{Order: []string{lane}})
	if _, err := client.CompleteWithMessages(ctx, planAsk()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("the endpoint saw %d requests, want the one call — an honest call is one call", len(bodies))
	}
	reasoning, _ := bodies[0]["reasoning"].(map[string]any)
	budget, _ := reasoning["max_tokens"].(float64)
	// share(max) × 210 tok/s × 240 s: the wall, spent at this machine's pace,
	// with the answer's share of it left over.
	if budget != 47880 {
		t.Fatalf("a walled planning call carried reasoning %#v, want a budget of 47,880 tokens", reasoning)
	}
}

// TestTheBudgetFollowsTheTimeTheCallReallyHas is the same chain with a caller
// whose deadline is shorter than the wall: the model is told the thinking that
// fits the time it really has — its share of the caller's minute — and not the
// slot's four minutes.
func TestTheBudgetFollowsTheTimeTheCallReallyHas(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	const model, lane = "z-ai/glm-5.3", "Friendli"
	lanes.Default().SetLedger(believedLedger{belief: lanes.Belief{
		ID:   lanes.ID{Model: model, Lane: lane},
		Rate: lanes.Posterior{X: math.Log(210), P: 0.01},
	}})
	t.Cleanup(func() { lanes.Default().SetLedger(nil) })
	budget := make(chan float64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body struct {
			Reasoning struct {
				MaxTokens float64 `json:"max_tokens"`
			} `json:"reasoning"`
		}
		_ = json.Unmarshal(raw, &body)
		budget <- body.Reasoning.MaxTokens
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"z-ai/glm-5.3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	defer server.Close()
	adapter, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: server.URL, Model: model,
		ReasoningProfile: func(string) (provider.ReasoningProfile, bool) {
			return provider.ReasoningProfile{Mandatory: true, Default: "max"}, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := Adopt(config.Config{}, model, adapter).WithCallWall(DefaultCallWall)

	const rail = time.Minute
	ctx, cancel := context.WithTimeout(provider.WithLaneChoice(context.Background(), lanes.Choice{Order: []string{lane}}), rail)
	defer cancel()
	if _, err := client.CompleteWithMessages(ctx, planAsk()); err != nil {
		t.Fatal(err)
	}
	// share(max) × 210 tok/s × the half of the minute the first completion is
	// given; the few microseconds spent getting here cost at most a token or two.
	want := 0.95 * 210 * (rail / completionsPerCall).Seconds()
	if got := <-budget; got > want || want-got > 5 {
		t.Fatalf("a call with %s of rail was told a budget of %.0f tokens, want the %.0f its share of that rail holds",
			rail, got, want)
	}
}

// ── the clocks under the wall ───────────────────────────────────────────────
//
// The wall is not the only timer over a completion. The dispatcher gives each
// attempt its own patience and the stream guard bounds a stream that goes
// quiet, and issue #927's second run (deepseek-v4.1-flash, 2026-09-11) lost its
// size, bind and contract passes to one of those at 51 seconds — a quarter of
// the way to the four-minute wall, with the thought as lost as if the wall had
// taken it. So what the salvage asks is whether TIME ended the completion, not
// which timer did.

// TestAThoughtCutByAClockUnderTheWallIsAskedForToo is that run, at the scale of
// a test: a wall nowhere near firing, a completion ended by a deadline below it,
// and the thought asked about rather than thrown away.
func TestAThoughtCutByAClockUnderTheWallIsAskedForToo(t *testing.T) {
	model := &thinkingModel{
		thought:   "Stage 1 is the note; stage 2 is the migration.",
		answer:    `{"sizes":[]}`,
		failsWith: fmt.Errorf("decode stream: %w", context.DeadlineExceeded),
	}
	// An hour of wall, so nothing here can be the wall's own doing.
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(time.Hour)

	response, err := client.CompleteWithMessages(context.Background(), planAsk())
	if err != nil {
		t.Fatalf("a completion cut by a clock under the wall lost its thought: %v", err)
	}
	if response.Text() != model.answer {
		t.Fatalf("answer = %q, want the answer ask's", response.Text())
	}
	requests := model.requests()
	if len(requests) != 2 {
		t.Fatalf("the model was asked %d times, want the call and its answer ask", len(requests))
	}
	if got := textOf(requests[1].messages[len(planAsk())]); !strings.Contains(got, model.thought) {
		t.Fatalf("the answer ask did not carry the thought: %q", got)
	}
}

// TestAStreamTheGuardGaveUpOnKeepsItsThought is the other clock under the wall,
// through the guard's own error type.
func TestAStreamTheGuardGaveUpOnKeepsItsThought(t *testing.T) {
	model := &thinkingModel{
		thought:   "Binding stage 2 to stage 1.",
		answer:    `{"bindings":[]}`,
		failsWith: &provider.StreamCut{Reason: provider.CutStalled, Waited: 45 * time.Second},
	}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(time.Hour)

	response, err := client.CompleteWithMessages(context.Background(), planAsk())
	if err != nil {
		t.Fatalf("a stream the guard gave up on lost its thought: %v", err)
	}
	if response.Text() != model.answer {
		t.Fatalf("answer = %q, want the answer ask's", response.Text())
	}
	if got := len(model.requests()); got != 2 {
		t.Fatalf("the model was asked %d times, want the call and its answer ask", got)
	}
}

// TestAClockUnderTheWallKeepsItsOwnAccount is the honesty half of the same
// change: when the answer ask fails too, the caller is told what really stopped
// the call. Only the wall's own deadline is the wall's to name.
func TestAClockUnderTheWallKeepsItsOwnAccount(t *testing.T) {
	patience := fmt.Errorf("decode stream: %w", context.DeadlineExceeded)
	model := &thinkingModel{thought: "Thinking.", failsWith: patience}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(time.Hour)

	_, err := client.CompleteWithMessages(context.Background(), planAsk())
	if errors.Is(err, ErrCallWall) {
		t.Fatalf("a call cut 4 minutes short of its wall was blamed on the wall: %v", err)
	}
	if !errors.Is(err, patience) {
		t.Fatalf("error = %v, want the account the clock that cut it gave", err)
	}
	if !strings.Contains(err.Error(), "asked for the answer it had reached") {
		t.Fatalf("the error does not say the answer was asked for: %v", err)
	}
}

// TestNothingKeptUnderAnotherClockIsNotReworded is the same rule for a
// completion that wrote nothing: there is nothing to ask about, and the error
// travels exactly as it came.
func TestNothingKeptUnderAnotherClockIsNotReworded(t *testing.T) {
	patience := fmt.Errorf("decode stream: %w", context.DeadlineExceeded)
	model := &thinkingModel{failsWith: patience}
	client := Adopt(config.Config{}, model.Model(), model).WithCallWall(time.Hour)

	_, err := client.CompleteWithMessages(context.Background(), planAsk())
	if err.Error() != patience.Error() {
		t.Fatalf("error = %v, want %v exactly", err, patience)
	}
	if got := len(model.requests()); got != 1 {
		t.Fatalf("a completion that wrote nothing was asked %d times, want once", got)
	}
}

// TestTheCauseIsSaidInWordsAPersonCanAct mirrors the receipt law one layer down:
// a planning fault a person reads names the pass it happened in and says what
// happened in the vocabulary the receipt uses, never the machinery's.
func TestTheCauseIsSaidInWordsAPersonCanAct(t *testing.T) {
	for _, said := range []struct {
		err  error
		want string
	}{
		// The sentence issue #927's second run put in front of a person.
		{fmt.Errorf("size stage 1: %w", context.DeadlineExceeded), "size stage 1: " + RanOutOfTime},
		{fmt.Errorf("contract Synthesis: %w", context.DeadlineExceeded), "contract Synthesis: " + RanOutOfTime},
		// The wall's own error already names the cause, and is not made to
		// say it twice.
		{fmt.Errorf("%w after 4m0s", ErrCallWall), RanOutOfTime + " after 4m0s"},
		// Anything that did not die of time is not touched.
		{errors.New("the model refused this request"), "the model refused this request"},
		{nil, ""},
	} {
		if got := CauseInWords(said.err); got != said.want {
			t.Fatalf("CauseInWords(%v) = %q, want %q", said.err, got, said.want)
		}
	}
}
