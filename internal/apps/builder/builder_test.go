package builder_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
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
				for _, message := range messages {
					if message.Role == "system" && len(message.Content) > 0 && len(message.Content[0].Text) > 8<<10 {
						t.Error("the scoped system prompt must fit without the full kit, manual or worked examples")
					}
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
					message := toolCalls(call)
					if cursor%4 == 0 {
						message.Content = []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("Saved %d CRM definition changes.", cursor-1)}}
					}
					return message, nil
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
	if len(state.Checkpoints) != 2 || len(state.Screens) == 0 || state.Published {
		t.Fatalf("expected a plan and one checked build, got %+v", state)
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
	if err != nil || earlier.Scope != preview.Scope {
		t.Fatalf("the latest checked step should reuse its preview: %v", err)
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
	if err != nil || len(state.Checkpoints) != 3 || len(state.Screens) != 0 || state.Published {
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

func TestPlanPersistsAndReportsTheModelFailure(t *testing.T) {
	home := t.TempDir()
	opts := scriptedApp(t, home)
	opts.Model = func() (builder.Completer, error) {
		return script(func(context.Context, []ai.Message, []ai.ToolDefinition) (ai.Message, error) {
			return ai.Message{}, errors.New("model could not fit the conversation in its context window; sk-or-v1-fakekeyexample")
		}), nil
	}
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Make a CRM."})
	if err == nil || !strings.Contains(err.Error(), "context window") || strings.Contains(err.Error(), "fakekeyexample") {
		t.Errorf("plan error = %v", err)
	}
	summaries, err := jobs.List()
	if err != nil || len(summaries) != 1 {
		t.Fatalf("jobs = %+v, %v", summaries, err)
	}
	jobs.Close()
	reopened, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err := reopened.State(summaries[0].Draft)
	if err != nil || state.State != builder.Failed || !strings.Contains(state.Problem, "context window") ||
		strings.Contains(state.Problem, "fakekeyexample") || !slices.ContainsFunc(state.Progress, func(event builder.Event) bool {
		return strings.Contains(event.Line, "context window")
	}) {
		t.Fatalf("persisted state = %+v, %v", state, err)
	}
}

func TestPlanNamesTheCallLimitWhenTheTurnKeepsWorking(t *testing.T) {
	opts := scriptedApp(t, t.TempDir())
	opts.Model = func() (builder.Completer, error) {
		step := 0
		return script(func(context.Context, []ai.Message, []ai.ToolDefinition) (ai.Message, error) {
			step++
			update := fmt.Sprintf("Outline revision %d", step)
			call := command("plan", map[string]any{"name": "CRM", "plan": map[string]string{"ask": update}})
			call.ID = fmt.Sprintf("step-%d", step)
			message := toolCalls(call)
			message.Content = []ai.ContentPart{{Type: "text", Text: update}}
			return message, nil
		}), nil
	}
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	_, err = jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Make a CRM."})
	if err == nil || !strings.Contains(err.Error(), "20-call limit") {
		t.Fatalf("plan error = %v", err)
	}
}

func TestHeadlessApplyPublishesAndRecordsSkippedLook(t *testing.T) {
	jobs, draft := buildWithDesk(t, func(context.Context, string, string, check.LookViewport) ([]string, error) {
		return nil, check.ErrDeskUnavailable
	})
	state := waitState(t, jobs, draft, builder.Ready)
	const notice = "look: skipped (desk not running)"
	if len(state.Checkpoints) != 2 || !strings.Contains(state.Checkpoints[1].Summary, notice) {
		t.Fatalf("the change card must show the skipped Look: %+v", state.Checkpoints)
	}
	if !slices.ContainsFunc(state.Progress, func(event builder.Event) bool {
		return event.OK != nil && *event.OK && event.Report != nil && event.Report.Look == "skipped (desk not running)"
	}) {
		t.Fatalf("the passing Check must record the skipped Look: %+v", state.Progress)
	}
	if meta, err := jobs.Apply(draft); err != nil || meta.Version != 1 {
		t.Fatalf("headless Apply = %+v, %v", meta, err)
	}
}

func TestDeskLayoutFindingBlocksCheckpointAndApply(t *testing.T) {
	jobs, draft := buildWithDesk(t, func(_ context.Context, _, screen string, view check.LookViewport) ([]string, error) {
		if screen == "deals" && view.Theme == "light" && view.Width == 800 {
			return []string{"[deal-list] value truncates"}, nil
		}
		return nil, nil
	})
	state := waitState(t, jobs, draft, builder.NeedsYou)
	if len(state.Checkpoints) != 1 || !strings.Contains(state.Problem, "light at 800 px: [deal-list] value truncates") {
		t.Fatalf("the layout finding must block the writing checkpoint: %+v", state)
	}
	if _, err := jobs.Apply(draft); err == nil {
		t.Fatal("Apply accepted a draft with a measured layout problem")
	}
}

func buildWithDesk(t *testing.T, look func(context.Context, string, string, check.LookViewport) ([]string, error)) (*builder.Jobs, string) {
	t.Helper()
	opts := scriptedApp(t, t.TempDir())
	opts.Check = (check.Pipeline{
		Seed:   func(string, []byte, map[string][]map[string]any) error { return nil },
		DryRun: func(context.Context, string, string, schema.Definition) error { return nil },
		Look:   look,
	}).Run
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(jobs.Close)
	plan, err := jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Make a CRM."})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Build(plan.Draft); err != nil {
		t.Fatal(err)
	}
	return jobs, plan.Draft
}

func TestStartingTheBuilderLeavesAnEmptyReadableAppShelf(t *testing.T) {
	home := t.TempDir()
	opts := scriptedApp(t, home)
	opts.Store = store.NewAt(home, nil)
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	if live, err := opts.Store.List(); err != nil || len(live) != 0 {
		t.Fatalf("the app shelf after startup = %v, %v", live, err)
	}
}

func TestConflictingApplyLeavesTheDraftDiscardableAfterRestart(t *testing.T) {
	home := t.TempDir()
	opts := scriptedApp(t, home)
	opts.Store = store.NewAt(home, nil)
	jobs, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	plan, err := jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Make a CRM."})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Build(plan.Draft); err != nil {
		t.Fatal(err)
	}
	waitState(t, jobs, plan.Draft, builder.Ready)
	first, err := jobs.Apply(plan.Draft)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := jobs.Change(first.ID, "Tidy the CRM.")
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, jobs, edit, builder.Ready)
	other, err := opts.Store.NewDraft(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other.Dir(), "agent.md"), []byte("A different published edit."), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := opts.Store.Promote(other, "Change the agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Apply(edit); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale Apply = %v", err)
	}
	jobs.Close()
	reopened, err := builder.OpenJobs(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err := reopened.State(edit)
	if err != nil || state.State != builder.NeedsYou || !strings.Contains(state.Problem, "saved app changed") {
		t.Fatalf("reopened conflict = %s, %q, %v", state.State, state.Problem, err)
	}
	if err := reopened.Discard(edit); err != nil {
		t.Fatalf("discard after the failed publication: %v", err)
	}
	live, err := opts.Store.Get(first.ID)
	if err != nil || live.Meta.Version != second.Version {
		t.Fatalf("discard changed the other publication: version %d, %v", live.Meta.Version, err)
	}
}
