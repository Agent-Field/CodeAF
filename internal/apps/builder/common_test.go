package builder_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestBuildPlanOffersCommonTypesAndExistingCRMContacts(t *testing.T) {
	review := make(chan struct{}, 2)
	options := builder.Options{
		Changed: func(e builder.Event) {
			if e.Line == "Plan ready to review." {
				select {
				case review <- struct{}{}:
				default:
				}
			}
		},
		Home:    t.TempDir(),
		CanLook: func() bool { return true },
		Compiler: fixtureCompiler{schema.Definition{Files: map[string][]byte{
			"screens/contacts.json": []byte(`{"screen":"contacts","title":"Contacts","body":{"id":"root","part":"Stack"}}`),
		}}},
		Catalog: func(string) (builder.Catalog, error) {
			return builder.Catalog{
				Types:    schema.TypeSet{"contact": {Owner: "crm", Common: "person"}},
				Counts:   map[string]int{"contact": 4},
				AppNames: map[string]string{"crm": "CRM"},
			}, nil
		},
		Model: func() (builder.Completer, error) {
			step := 0
			var keep, writing bool
			return script(func(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) (ai.Message, error) {
				step++
				switch step {
				case 1:
					return toolCalls(command("context", nil)), nil
				case 2:
					var state struct {
						Using []string
						Stage string
					}
					last := messages[len(messages)-1]
					if last.Role == "tool" && len(last.Content) > 0 {
						_ = json.Unmarshal([]byte(last.Content[0].Text), &state)
					}
					keep = slices.Contains(state.Using, "contact")
					writing = state.Stage == builder.Writing
					if writing {
						return toolCalls(command("markup", map[string]string{"screen": "contacts"})), nil
					}
					return toolCalls(command("plan", map[string]any{"name": "Support", "plan": json.RawMessage(`{"ask":"Track customers","data":[{"id":"customer","line":"Customer details"}],"screens":[{"id":"contacts","line":"Contact list"}]}`)})), nil
				case 3:
					if writing {
						return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I wrote the Contacts screen."}}}, nil
					}
					return toolCalls(command("add-type", map[string]any{
						"name":       "customer",
						"access":     "rw",
						"definition": map[string]any{"uses": keep, "common": "person", "fields": map[string]any{}},
					})), nil
				default:
					return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I planned Support."}}}, nil
				}
			}), nil
		},
	}
	jobs, err := builder.OpenJobs(options)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan, err := jobs.Plan(ctx, builder.PlanRequest{Ask: "Track Support customers."})
	if err != nil {
		t.Fatal(err)
	}
	outline, err := schema.ParsePlan(plan.Plan)
	if err != nil || len(outline.Data) != 0 || len(plan.Offers) != 1 ||
		plan.Offers[0].Type != "contact" || plan.Offers[0].Count != 4 ||
		plan.Offers[0].Outline == nil || plan.Offers[0].Outline.Line != "Customer details" {
		t.Fatalf("wanted one customer row with its reuse choice: plan=%s offers=%+v, %v", plan.Plan, plan.Offers, err)
	}
	<-review
	if err := jobs.Say(plan.Draft, "Use my existing contact records (4, from CRM) instead of making a new type", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-review:
	case <-ctx.Done():
		t.Fatal("the reuse choice did not finish replanning")
	}
	state, err := jobs.State(plan.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != builder.Planning || len(state.Screens) != 0 {
		t.Fatalf("accepted plan must still be waiting for Write: state=%s screens=%v", state.State, state.Screens)
	}
	if len(state.Offers) != 0 || !slices.Contains(state.Using, "customer") || !slices.Contains(state.Needs, "Uses your existing Contacts from CRM") || !slices.Contains(state.Needs, "Read and edit customer") {
		t.Fatalf("expected the card to name the CRM reuse: %+v", state)
	}
	if err := jobs.Build(plan.Draft); err != nil {
		t.Fatal(err)
	}
	state = waitState(t, jobs, plan.Draft, builder.Ready)
	if !slices.Equal(state.Screens, []string{"contacts"}) || !slices.Contains(state.Using, "customer") {
		t.Fatalf("built screens or binding missing: %+v", state)
	}
}

func TestHeadlessReuseIsProposedUntilApply(t *testing.T) {
	for _, test := range []struct {
		name           string
		desk, existing bool
	}{
		{name: "headless", existing: true},
		{name: "desk", desk: true, existing: true},
		{name: "new common type", desk: true},
	} {
		desk := test.desk
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			definitions := store.NewAt(home, func(string) (schema.TypeSet, error) {
				return schema.TypeSet{"contact": {Owner: "crm", Common: "person", Fields: map[string]schema.Field{"name": {Type: "text"}}}}, nil
			})
			var checked atomic.Int32
			var deskReady atomic.Bool
			deskReady.Store(desk)
			opts := builder.Options{
				Home: home, Store: definitions, CanLook: deskReady.Load,
				Check: func(_ context.Context, _ string, def schema.Definition, types schema.TypeSet) schema.Report {
					checked.Add(1)
					return check.Definition(def, types)
				},
				Catalog: func(app string) (builder.Catalog, error) {
					if !test.existing {
						return builder.Catalog{}, nil
					}
					return builder.Catalog{
						Types:    schema.TypeSet{"contact": {Owner: "crm", Common: "person", Fields: map[string]schema.Field{"name": {Type: "text"}}}},
						AppNames: map[string]string{"crm": "CRM"},
					}, nil
				},
				Compiler: fixtureCompiler{schema.Definition{Files: map[string][]byte{
					"screens/contacts.json": []byte(`{"screen":"contacts","title":"Contacts","body":{"id":"root","part":"Stack"}}`),
				}}},
				Model: func() (builder.Completer, error) {
					step := 0
					var planning bool
					return script(func(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) (ai.Message, error) {
						step++
						switch step {
						case 1:
							return toolCalls(command("context", nil)), nil
						case 2:
							var state struct {
								Stage string
								Using []string
							}
							last := messages[len(messages)-1]
							if err := json.Unmarshal([]byte(last.Content[0].Text), &state); err != nil {
								return ai.Message{}, err
							}
							planning = state.Stage == builder.Planning
							if planning {
								return toolCalls(command("plan", map[string]any{"name": "Contacts", "plan": json.RawMessage(`{"ask":"Reuse CRM Contacts","data":[{"id":"customer","line":"Existing contacts"}],"screens":[{"id":"contacts","line":"Contact list"}]}`)})), nil
							}
							if slices.Contains(state.Using, "contact") == desk {
								return ai.Message{}, errors.New("unexpected description of proposed reuse")
							}
							return toolCalls(command("markup", map[string]string{"screen": "contacts"})), nil
						case 3:
							return toolCalls(command("add-type", map[string]any{"name": "customer", "definition": map[string]any{"uses": !planning && (desk || !test.existing), "common": "person", "fields": map[string]any{}}})), nil
						case 4:
							if !planning {
								return toolCalls(command("check", nil)), nil
							}
							fallthrough
						default:
							if planning {
								return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I outlined Contacts."}}}, nil
							}
							last := messages[len(messages)-1]
							want := []string{`"status":"definition_only"`}
							if desk && test.existing {
								want = []string{`"status":"needs_you"`, "choose_reuse"}
							} else if !test.existing {
								want = append(want, schema.CodeUsesUnknown)
							}
							for _, text := range want {
								if !strings.Contains(last.Content[0].Text, text) {
									t.Errorf("the Builder's preliminary report did not contain %s", text)
								}
							}
							deskReady.Store(false)
							return ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Contacts are ready."}}}, nil
						}
					}), nil
				},
			}
			jobs, err := builder.OpenJobs(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer jobs.Close()
			plan, err := jobs.Plan(t.Context(), builder.PlanRequest{Ask: "Reuse CRM contacts."})
			if err != nil {
				t.Fatal(err)
			}
			const proposal = "Uses your existing Contacts from CRM"
			if test.existing && (slices.Contains(plan.Needs, proposal) == desk || len(plan.Offers) == 0 || plan.Offers[0].Type != "contact") {
				t.Fatalf("plan needs and offers = %v, %v", plan.Needs, plan.Offers)
			}
			if err := jobs.Build(plan.Draft); err != nil {
				t.Fatal(err)
			}
			if desk {
				state := waitState(t, jobs, plan.Draft, builder.NeedsYou)
				event := state.Progress[len(state.Progress)-1]
				if !test.existing {
					if checked.Load() == 0 || event.Report == nil || !slices.Contains(event.Report.Codes(), schema.CodeUsesUnknown) ||
						slices.Contains(event.Report.Codes(), "choose_reuse") {
						t.Fatalf("a missing reuse source should be a definition finding: %+v", event)
					}
					return
				}
				if checked.Load() != 0 || event.OK != nil || event.Report == nil || !slices.Contains(event.Report.Codes(), "choose_reuse") {
					t.Fatalf("expected a consent question before Check: %+v", event)
				}
				if slices.ContainsFunc(state.Transcript, func(message builder.Message) bool { return message.Text == "Contacts are ready." }) {
					t.Fatal("the model's reply from before Check was shown while consent was pending")
				}
				return
			}
			state := waitState(t, jobs, plan.Draft, builder.Ready)
			if !slices.Contains(state.Needs, proposal) || !slices.Contains(state.Using, "customer") {
				t.Fatalf("checked reuse = %v, %v", state.Needs, state.Using)
			}
			if state.Published {
				t.Fatal("the headless build published before Apply")
			}
			if _, err := definitions.Get(plan.Draft); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("unapplied app lookup = %v", err)
			}
			if _, err := jobs.Apply(plan.Draft); err != nil {
				t.Fatal(err)
			}
			live, err := definitions.Get(plan.Draft)
			if err != nil {
				t.Fatal(err)
			}
			app, err := schema.ParseApp(live.App)
			if err != nil || !app.Records["customer"].Uses {
				t.Fatalf("applied reuse = %+v, %v", app.Records["customer"], err)
			}
		})
	}
}
