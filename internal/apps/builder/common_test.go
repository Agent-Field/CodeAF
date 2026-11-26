package builder_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestBuildPlanOffersCommonTypesAndExistingCRMContacts(t *testing.T) {
	options := builder.Options{
		Home:    t.TempDir(),
		CanLook: func() bool { return true },
		Catalog: func(string) (builder.Catalog, error) {
			return builder.Catalog{
				Types:    schema.TypeSet{"contact": {Owner: "crm", Common: "person"}},
				Counts:   map[string]int{"contact": 4},
				AppNames: map[string]string{"crm": "CRM"},
			}, nil
		},
		Model: func() (builder.Completer, error) {
			step := 0
			var keep bool
			return script(func(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) (ai.Message, error) {
				step++
				switch step {
				case 1:
					return toolCalls(command("context", nil)), nil
				case 2:
					var state struct{ Using []string }
					last := messages[len(messages)-1]
					if last.Role == "tool" && len(last.Content) > 0 {
						_ = json.Unmarshal([]byte(last.Content[0].Text), &state)
					}
					keep = slices.Contains(state.Using, "contact")
					return toolCalls(command("plan", map[string]any{"name": "Support", "plan": map[string]any{"ask": "Track customers"}})), nil
				case 3:
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
	if len(plan.Offers) != 2 || plan.Offers[0].Type != "contact" || plan.Offers[0].Count != 4 ||
		plan.Offers[1].Type != "person" || plan.Offers[1].App != "" {
		t.Fatalf("wanted existing contact and library Person offers: %+v", plan.Offers)
	}
	_, err = jobs.Plan(ctx, builder.PlanRequest{Draft: plan.Draft, Request: "Use my existing Contacts."})
	if err != nil {
		t.Fatal(err)
	}
	state, err := jobs.State(plan.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(state.Using, "customer") || !slices.Contains(state.Needs, "Uses your existing Contacts from CRM") || !slices.Contains(state.Needs, "Read and edit customer") {
		t.Fatalf("expected the card to name the CRM reuse: %+v", state)
	}
}

func TestHeadlessReuseIsProposedUntilApply(t *testing.T) {
	for _, desk := range []bool{false, true} {
		name := "headless"
		if desk {
			name = "desk"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			definitions := store.NewAt(home, func(string) (schema.TypeSet, error) {
				return schema.TypeSet{"contact": {Owner: "crm", Common: "person", Fields: map[string]schema.Field{"name": {Type: "text"}}}}, nil
			})
			var deskReady atomic.Bool
			deskReady.Store(desk)
			opts := builder.Options{
				Home: home, Store: definitions, CanLook: deskReady.Load,
				Catalog: func(app string) (builder.Catalog, error) {
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
							return toolCalls(command("add-type", map[string]any{"name": "customer", "definition": map[string]any{"uses": !planning && desk, "common": "person", "fields": map[string]any{}}})), nil
						default:
							if !planning {
								deskReady.Store(false)
							}
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
			if slices.Contains(plan.Needs, proposal) == desk || len(plan.Offers) == 0 || plan.Offers[0].Type != "contact" {
				t.Fatalf("plan needs and offers = %v, %v", plan.Needs, plan.Offers)
			}
			if err := jobs.Build(plan.Draft); err != nil {
				t.Fatal(err)
			}
			if desk {
				waitState(t, jobs, plan.Draft, builder.NeedsYou)
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
