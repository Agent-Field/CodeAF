package builder_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type script func(context.Context, []ai.Message, []ai.ToolDefinition) (ai.Message, error)

func (s script) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, opt := range opts {
		_ = opt(&request)
	}
	msg, err := s(ctx, messages, request.Tools)
	return &ai.Response{Choices: []ai.Choice{{Message: msg}}}, err
}

type fixtureCompiler struct{ def schema.Definition }

func (f fixtureCompiler) Compile(_ context.Context, _ string, _ schema.Definition, _ schema.TypeSet, raw json.RawMessage) (builder.CompiledChange, error) {
	var params struct{ Screen string }
	if err := json.Unmarshal(raw, &params); err != nil {
		return builder.CompiledChange{}, err
	}
	return builder.CompiledChange{ScreenID: params.Screen, Screen: f.def.Files["screens/"+params.Screen+".json"]}, nil
}

func scriptedApp(t *testing.T, home string) builder.Options {
	t.Helper()
	def, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	app, err := schema.ParseApp(def.Files["app.json"])
	if err != nil {
		t.Fatal(err)
	}
	return builder.Options{
		Home: home, Compiler: fixtureCompiler{def},
		Model: func() (builder.Completer, error) {
			cursor := 0
			sentContext := false
			var stage string
			return script(func(_ context.Context, messages []ai.Message, tools []ai.ToolDefinition) (ai.Message, error) {
				if len(tools) != 1 || tools[0].Function.Name != "bash" {
					return ai.Message{}, fmt.Errorf("unexpected command slot: %+v", tools)
				}
				last := messages[len(messages)-1]
				if !sentContext {
					sentContext = true
					return toolCalls(command("context", nil)), nil
				}
				if stage == "" && last.Role == "tool" && len(last.Content) > 0 {
					var state struct{ Stage string }
					_ = json.Unmarshal([]byte(last.Content[0].Text), &state)
					stage = state.Stage
				}
				if stage == "" {
					return ai.Message{}, errors.New("the scoped command did not supply its phase")
				}
				calls := []ai.ToolCall{
					command("plan", map[string]any{"name": app.Name, "kit": app.Kit, "plan": json.RawMessage(def.Files["plan.json"])}),
				}
				for _, name := range slices.Sorted(maps.Keys(app.Records)) {
					calls = append(calls, command("add-type", map[string]any{"name": name, "definition": app.Records[name]}))
				}
				calls = append(calls,
					command("configure", map[string]any{
						"capabilities": app.Capabilities, "triggers": app.Triggers, "commands": app.Commands,
						"budget": app.Budget, "agent": app.Agent,
					}),
				)
				if stage != "planning" {
					for _, path := range def.ScreenFiles() {
						calls = append(calls, command("recipe", map[string]any{"screen": schema.FileID(path)}))
					}
					for _, path := range def.RunFiles() {
						calls = append(calls, command("set-run", map[string]any{"id": schema.FileID(path), "text": string(def.Files[path])}))
					}
					calls = append(calls,
						command("set-agent", map[string]any{"text": string(def.Files["agent.md"])}),
						command("set-sample", map[string]any{"text": string(def.Files["sample.json"])}),
					)
				}
				if cursor < len(calls) {
					call := calls[cursor]
					cursor++
					call.ID = fmt.Sprintf("app-%02d", cursor)
					return toolCalls(call), nil
				}
				return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I set up your CRM."}}}, nil
			}), nil
		},
	}
}

func toolCalls(calls ...ai.ToolCall) ai.Message {
	return ai.Message{Role: "assistant", ToolCalls: calls}
}

func command(verb string, input any) ai.ToolCall {
	body := ""
	if input != nil {
		raw, _ := json.Marshal(input)
		body = " " + string(raw)
	}
	return rawCommand(verb, "codeaf app "+verb+body)
}

func rawCommand(id, command string) ai.ToolCall {
	args, _ := json.Marshal(map[string]string{"command": command})
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}}
}

func TestPlanRestartsWithTranscriptAndCheckpoint(t *testing.T) {
	home := t.TempDir()
	options := scriptedApp(t, home)
	jobs, err := builder.OpenJobs(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan, err := jobs.Plan(ctx, builder.PlanRequest{Ask: "Track my deals."})
	if err != nil {
		t.Fatal(err)
	}
	jobs.Close()
	jobs, err = builder.OpenJobs(options)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	state, err := jobs.State(plan.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Transcript) != 2 || len(state.Checkpoints) != 1 || state.Transcript[1].Checkpoint != state.Checkpoints[0].ID || !strings.Contains(string(state.Plan), "deal") {
		t.Fatalf("reopened state: %+v", state)
	}
	if state.Published || state.App != "" {
		t.Fatal("an agent published its own draft")
	}
}

func TestBuildChecksThreeRoundsAndRestoresEarlierScreen(t *testing.T) {
	opts := scriptedApp(t, t.TempDir())
	checked := 0
	opts.Check = func(_ context.Context, _ string, def schema.Definition, types schema.TypeSet) schema.Report {
		checked++
		if checked < 3 {
			return schema.Report{Problems: []schema.Problem{{Code: "try_again", Message: "Try again."}}}
		}
		return schema.Validate(def, types)
	}
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan, err := jobs.Plan(ctx, builder.PlanRequest{Ask: "Make a CRM."})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Build(plan.Draft); err != nil {
		t.Fatal(err)
	}
	state := waitState(t, jobs, plan.Draft, builder.Ready)
	if len(state.Checkpoints) != 4 || len(state.Screens) == 0 || state.Published {
		t.Fatalf("expected a plan and three writing turns, got %+v", state)
	}
	preview, err := jobs.Preview(builder.PreviewRequest{Draft: plan.Draft})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := schema.Load("../schema/testdata/crm")
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := jobs.Screen(preview.Scope, "deals"); err != nil || string(raw) != string(cr.Files["screens/deals.json"]) {
		t.Fatalf("preview should serve the compiler's exact screen: %v", err)
	}
	earlier, err := jobs.Preview(builder.PreviewRequest{Draft: plan.Draft, Checkpoint: state.Checkpoints[1].ID})
	if err != nil || earlier.Scope == preview.Scope {
		t.Fatalf("earlier step should have a separate preview: %+v, %v", earlier, err)
	}
	meta, err := jobs.Apply(plan.Draft)
	if err != nil || meta.Version != 1 {
		t.Fatalf("first Apply: %+v, %v", meta, err)
	}
	draft, err := jobs.Change(meta.ID, "")
	if err != nil || draft != plan.Draft {
		t.Fatalf("Edit app should reopen its previous job: %q, %v", draft, err)
	}
	if err := jobs.Restore(draft, state.Checkpoints[0].ID); err != nil {
		t.Fatal(err)
	}
	state, err = jobs.State(draft)
	if err != nil || len(state.Checkpoints) != 5 || len(state.Screens) != 0 || state.Published {
		t.Fatalf("restore should append a step and reopen the earlier plan: %+v, %v", state, err)
	}
	if _, err := jobs.Apply(draft); err == nil {
		t.Fatal("an unchecked restored plan was published")
	}
}

func TestInterruptedJobResumesAfterEngineOpens(t *testing.T) {
	home := t.TempDir()
	start := make(chan struct{})
	opts := scriptedApp(t, home)
	reopened := opts
	opts.Model = func() (builder.Completer, error) {
		return script(func(ctx context.Context, _ []ai.Message, _ []ai.ToolDefinition) (ai.Message, error) {
			close(start)
			<-ctx.Done()
			return ai.Message{}, ctx.Err()
		}), nil
	}
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() {
		_, err := jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Make a CRM."})
		wait <- err
	}()
	select {
	case <-start:
	case <-time.After(5 * time.Second):
		t.Fatal("plan never reached the model")
	}
	waiting, err := jobs.List()
	if err != nil || len(waiting) != 1 {
		t.Fatalf("job missing while planning: %+v, %v", waiting, err)
	}
	jobs.Close()
	if err := <-wait; !errors.Is(err, context.Canceled) {
		t.Fatalf("first engine should cancel its caller: %v", err)
	}
	again, err := builder.OpenJobs(reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	state := waitState(t, again, waiting[0].Draft, builder.Ready)
	if len(state.Transcript) != 2 || len(state.Checkpoints) != 1 {
		t.Fatalf("resuming should complete the same turn once: %+v", state)
	}
}

func waitState(t *testing.T, jobs *builder.Jobs, draft, state string) builder.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		got, err := jobs.State(draft)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == state {
			return got
		}
		if got.State == builder.Failed || got.State == builder.NeedsYou {
			t.Fatalf("wanted %s, got %s: %s, progress %+v", state, got.State, got.Problem, got.Progress)
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatalf("waiting for %s, got %+v", state, got)
		}
	}
}
