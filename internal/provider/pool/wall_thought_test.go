package pool

import (
	"context"
	"encoding/json"
	"errors"
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
// a completion that is meant to reach it.
const thoughtWall = 20 * time.Millisecond

// sent is one request the thinking model received.
type sent struct {
	messages []ai.Message
	options  int
	effort   provider.Effort
}

// thinkingModel is the model from the incident: it thinks, out loud on the
// stream, and does not stop until its context does. When `answer` is set it is
// what the model says to a request that tells it to stop thinking — the answer
// ask — and when it is empty the model thinks through that one too.
type thinkingModel struct {
	thought, begun, answer string
	// cancel, when set, is called once the thought is on the wire: the caller
	// going away mid-thought, without anybody sleeping to arrange it.
	cancel context.CancelFunc

	mu    sync.Mutex
	asked []sent
	// first is the context the first completion was sent with, kept so a test
	// can speak on its stream after the completion has ended.
	first context.Context
}

func (m *thinkingModel) Model() string { return "thinking/model" }

func (m *thinkingModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	m.mu.Lock()
	m.asked = append(m.asked, sent{messages: messages, options: len(options), effort: provider.ReasoningEffortFrom(ctx)})
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
	if m.cancel != nil {
		m.cancel()
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
