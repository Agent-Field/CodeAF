package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func errandAsk(purpose string) ErrandAsk {
	return ErrandAsk{
		Purpose:  purpose,
		System:   "Write one status line as JSON.",
		Messages: []ErrandMessage{{Role: "user", Text: "Two checks passed."}},
		JSON:     true,
	}
}

// An errand is the caller's own prompt on the agent's metered door: one call,
// no tools, a bounded output, the caller's system text first, charged to the
// agent's job budget once, and never a turn.
func TestAnErrandIsOneMeteredToolLessCallWithTheCallersPrompt(t *testing.T) {
	budget := &liveBudgetStub{cap: 1}
	c := &replyCompleter{answer: func(_ context.Context, msgs []ai.Message, req ai.Request) (*ai.Response, error) {
		if len(req.Tools) != 0 || req.MaxTokens == nil || *req.MaxTokens != 300 {
			t.Errorf("errand used tools or the wrong output bound: tools=%d max=%v", len(req.Tools), req.MaxTokens)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Errorf("json errand did not ask for a JSON object: %+v", req.ResponseFormat)
		}
		if len(msgs) != 2 || msgs[0].Role != "system" || messageContentText(msgs[0]) != "Write one status line as JSON." ||
			msgs[1].Role != "user" || messageContentText(msgs[1]) != "Two checks passed." {
			t.Errorf("caller's prompt was not sent as given: %+v", msgs)
		}
		return textResponse(` {"line":"Checks pass."} `), nil
	}}
	a, _ := newTestAgent(t, pricedAt(c, 0.02), func(cfg *Config) { cfg.OneModel = true; cfg.JobBudget = budget })
	ask := errandAsk("status_line")
	ask.MaxTokens = 300
	ask.Cache = "k1"
	got, err := a.RunErrand(t.Context(), ask)
	if err != nil || got.Text != `{"line":"Checks pass."}` || got.Reason != "" {
		t.Fatalf("answer=%+v err=%v", got, err)
	}
	if got.Model != "test/model" || got.Role != "title" || got.Usage.In != 10 || got.Usage.Out != 5 || got.Cached {
		t.Fatalf("answer did not say who answered and what it cost: %+v", got)
	}
	again, err := a.RunErrand(t.Context(), ask)
	if err != nil || !again.Cached || again.Text != got.Text || c.calls.Load() != 1 {
		t.Fatalf("a cached errand bought another request: %+v calls=%d", again, c.calls.Load())
	}
	budget.mu.Lock()
	spent := budget.spent
	budget.mu.Unlock()
	if spent != 0.02 {
		t.Fatalf("errand cost was not charged exactly once: %v", spent)
	}
	if a.turnSeq != 0 {
		t.Fatal("errand started a turn")
	}
}

func TestAJSONErrandAnswersOnlyOneObject(t *testing.T) {
	for raw, want := range map[string]string{
		`{"a":1}`:                      `{"a":1}`,
		"```json\n{\"a\":1}\n```":      `{"a":1}`,
		`[1,2]`:                        "",
		`{"a":1} {"b":2}`:              "",
		`Sure: {"a":1}`:                "",
		`{"a":`:                        "",
		"  \n{\"nested\":{\"b\":2}}\n": `{"nested":{"b":2}}`,
	} {
		if got := oneJSONObject(raw); got != want {
			t.Errorf("oneJSONObject(%q) = %q, want %q", raw, got, want)
		}
	}
	c := &replyCompleter{answer: func(context.Context, []ai.Message, ai.Request) (*ai.Response, error) {
		return textResponse("not json"), nil
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	got, err := a.RunErrand(t.Context(), errandAsk("status_line"))
	if err != nil || got.Text != "" || got.Reason != ErrandInvalidJSON || got.Model == "" {
		t.Fatalf("unparseable JSON answer: %+v %v", got, err)
	}
}

func TestAJSONSchemaErrandAsksForThatSchema(t *testing.T) {
	c := &replyCompleter{answer: func(_ context.Context, _ []ai.Message, req ai.Request) (*ai.Response, error) {
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.JSONSchema == nil ||
			string(req.ResponseFormat.JSONSchema.Schema) != `{"type":"object"}` {
			t.Errorf("schema was not sent: %+v", req.ResponseFormat)
		}
		return textResponse(`{"ok":true}`), nil
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	ask := errandAsk("status_line")
	ask.JSON, ask.JSONSchema = false, []byte(`{"type":"object"}`)
	if got, err := a.RunErrand(t.Context(), ask); err != nil || got.Text != `{"ok":true}` {
		t.Fatalf("%+v %v", got, err)
	}
}

// One errand per (agent, purpose) is in flight; a second of the same purpose
// answers busy at once and never queues, while another purpose still runs.
func TestASecondErrandOfOnePurposeIsBusyAtOnce(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	c := &replyCompleter{answer: func(ctx context.Context, msgs []ai.Message, _ ai.Request) (*ai.Response, error) {
		if messageContentText(msgs[0]) == "hold" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return textResponse(`{"ok":true}`), nil
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	held := errandAsk("brief")
	held.System = "hold"
	done := make(chan ErrandAnswer, 1)
	go func() { got, _ := a.RunErrand(context.Background(), held); done <- got }()
	<-started
	if got, err := a.RunErrand(t.Context(), errandAsk("brief")); err != nil || got.Reason != ErrandBusy || got.Text != "" {
		t.Fatalf("second errand of one purpose: %+v %v", got, err)
	}
	if got, err := a.RunErrand(t.Context(), errandAsk("summary")); err != nil || got.Text != `{"ok":true}` {
		t.Fatalf("another purpose was held: %+v %v", got, err)
	}
	close(release)
	if got := <-done; got.Text != `{"ok":true}` {
		t.Fatalf("held errand: %+v", got)
	}
}

func TestAnErrandRidesAnExplicitModelOrRole(t *testing.T) {
	var asked []string
	c := &replyCompleter{answer: func(_ context.Context, _ []ai.Message, req ai.Request) (*ai.Response, error) {
		asked = append(asked, req.Model)
		return textResponse(`{"ok":true}`), nil
	}}
	pins := map[string]string{"roles.caption": "vendor/caption-model"}
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.RolesSource = func(key string) (string, bool) { v, ok := pins[key]; return v, ok }
	})
	ask := errandAsk("pp")
	ask.Model = "vendor/explicit"
	got, err := a.RunErrand(t.Context(), ask)
	if err != nil || got.Model != "vendor/explicit" {
		t.Fatalf("explicit model: %+v %v", got, err)
	}
	ask = errandAsk("qq")
	ask.Role = "caption"
	got, err = a.RunErrand(t.Context(), ask)
	if err != nil || got.Model != "vendor/caption-model" || got.Role != "caption" {
		t.Fatalf("role: %+v %v", got, err)
	}
	if len(asked) != 2 || asked[0] != "vendor/explicit" || asked[1] != "vendor/caption-model" {
		t.Fatalf("models asked: %v", asked)
	}
	ask = errandAsk("rr")
	ask.Role = "no-such-role"
	if _, err := a.RunErrand(t.Context(), ask); !errors.Is(err, ErrErrandRoute) {
		t.Fatalf("unknown role: %v", err)
	}
}

// One-model mode wins: an explicit model other than the conversation's own is
// refused with a clear error and no request.
func TestOneModelModeRefusesAnotherExplicitModel(t *testing.T) {
	c := &replyCompleter{answer: func(context.Context, []ai.Message, ai.Request) (*ai.Response, error) {
		return textResponse(`{"ok":true}`), nil
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	ask := errandAsk("pp")
	ask.Model = "vendor/other"
	_, err := a.RunErrand(t.Context(), ask)
	if !errors.Is(err, ErrErrandRoute) || !strings.Contains(err.Error(), "one-model") || c.calls.Load() != 0 {
		t.Fatalf("one-model refusal: %v calls=%d", err, c.calls.Load())
	}
	ask.Model = "test/model"
	if got, err := a.RunErrand(t.Context(), ask); err != nil || got.Text == "" {
		t.Fatalf("the conversation's own model: %+v %v", got, err)
	}
}

func TestAnErrandStopsWithTheBudgetAndTheConversation(t *testing.T) {
	budget := &liveBudgetStub{cap: 1}
	started := make(chan struct{}, 1)
	c := &replyCompleter{answer: func(ctx context.Context, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true; cfg.JobBudget = budget })
	done := make(chan ErrandAnswer, 1)
	go func() { got, _ := a.RunErrand(context.Background(), errandAsk("pp")); done <- got }()
	<-started
	budget.Spend(1)
	select {
	case got := <-done:
		if got.Reason != ErrandOverBudget {
			t.Fatalf("budget stop: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("budget did not cancel the errand")
	}
	if got, _ := a.RunErrand(t.Context(), errandAsk("pp")); got.Reason != ErrandOverBudget || c.calls.Load() != 1 {
		t.Fatalf("an exhausted job bought another errand: %+v", got)
	}

	b, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	go func() { got, _ := b.RunErrand(context.Background(), errandAsk("pp")); done <- got }()
	<-started
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.Text != "" || got.Reason == "" {
			t.Fatalf("closed errand: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("errand outlived the conversation")
	}
	if got, _ := b.RunErrand(t.Context(), errandAsk("pp")); got.Reason != ErrandClosed {
		t.Fatalf("errand on a closed conversation: %+v", got)
	}
}

func TestAnErrandAnswersTheDeadlineAsAReason(t *testing.T) {
	c := &replyCompleter{answer: func(ctx context.Context, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	ask := errandAsk("pp")
	ask.Deadline = ErrandMinDeadline
	got, err := a.RunErrand(t.Context(), ask)
	if err != nil || got.Reason != ErrandDeadline {
		t.Fatalf("deadline: %+v %v", got, err)
	}
}

func TestAnErrandAskIsBoundedAndClamped(t *testing.T) {
	big := strings.Repeat("x", ErrandMaxInputBytes)
	for name, mutate := range map[string]func(*ErrandAsk){
		"purpose":      func(a *ErrandAsk) { a.Purpose = "Bad Purpose" },
		"no messages":  func(a *ErrandAsk) { a.Messages = nil },
		"message role": func(a *ErrandAsk) { a.Messages[0].Role = "system" },
		"empty text":   func(a *ErrandAsk) { a.Messages[0].Text = " " },
		"last role":    func(a *ErrandAsk) { a.Messages = append(a.Messages, ErrandMessage{Role: "assistant", Text: "x"}) },
		"too big":      func(a *ErrandAsk) { a.System = big },
		"bad utf8":     func(a *ErrandAsk) { a.System = "\xff" },
		"effort":       func(a *ErrandAsk) { a.Effort = "furious" },
		"schema":       func(a *ErrandAsk) { a.JSONSchema = []byte(`[1]`) },
		"cache":        func(a *ErrandAsk) { a.Cache = strings.Repeat("k", 200) },
		"too many": func(a *ErrandAsk) {
			for range ErrandMaxMessages {
				a.Messages = append(a.Messages, ErrandMessage{Role: "user", Text: "x"})
			}
		},
	} {
		ask := errandAsk("pp")
		mutate(&ask)
		if _, err := ask.Normalize(); !errors.Is(err, ErrErrandAsk) {
			t.Errorf("%s: accepted %v", name, err)
		}
	}
	ask := errandAsk("pp")
	ask.MaxTokens, ask.Deadline = 1<<20, time.Hour
	got, err := ask.Normalize()
	if err != nil || got.MaxTokens != ErrandMaxTokens || got.Deadline != ErrandMaxDeadline || got.Role != "title" {
		t.Fatalf("clamp: %+v %v", got, err)
	}
	ask.MaxTokens, ask.Deadline = 0, 0
	if got, _ := ask.Normalize(); got.MaxTokens != ErrandDefaultTokens || got.Deadline != ErrandDefaultDeadline {
		t.Fatalf("defaults: %+v", got)
	}
}

// THE PAGE AI's ROLES ARE ERRAND ROLES. An errand that names insight_notes is
// answered by its own pin, else the insight pin, else the engine's default for
// insight, else the low tier, and says the role it ran as.
func TestAnInsightErrandClimbsItsFamilyLadder(t *testing.T) {
	var asked []string
	c := &replyCompleter{answer: func(_ context.Context, _ []ai.Message, req ai.Request) (*ai.Response, error) {
		asked = append(asked, req.Model)
		return textResponse(`{"ok":true}`), nil
	}}
	values := map[string]string{}
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.RolesSource = func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	})
	for i, step := range []struct {
		key, value, want string
	}{
		{"tiers.low", "vendor/low", "vendor/low"},
		{"defaults.insight", "vendor/seeded", "vendor/seeded"},
		{"roles.insight", "vendor/family", "vendor/family"},
		{"roles.insight_notes", "vendor/own", "vendor/own"},
	} {
		values[step.key] = step.value
		ask := errandAsk("repo_notes")
		ask.Role = "insight_notes"
		ask.Cache = ""
		got, err := a.RunErrand(t.Context(), ask)
		if err != nil || got.Model != step.want || got.Role != "insight_notes" || asked[i] != step.want {
			t.Fatalf("with %s set: %+v %v, asked %v", step.key, got, err, asked)
		}
	}
	routes := map[string]string{}
	for _, route := range ErrandRoutesFrom(roles.Source(func(key string) (string, bool) { v, ok := values[key]; return v, ok }), "floor") {
		routes[route.Role] = route.Model
	}
	for _, role := range []string{"insight_pipeline", "insight_views", "insight_factory_read", "insight_factory_scout"} {
		if routes[role] != "vendor/family" {
			t.Errorf("errand route for %s = %q, want the family pin", role, routes[role])
		}
	}
	if routes["insight"] != "vendor/family" || routes["insight_ask"] != "vendor/family" || routes["insight_themes"] != "vendor/family" || routes["insight_notes"] != "vendor/own" {
		t.Fatalf("errand routes for the page AI: %v", routes)
	}
}
