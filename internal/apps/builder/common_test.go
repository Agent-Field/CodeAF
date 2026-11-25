package builder_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestBuildPlanOffersCommonTypesAndExistingCRMContacts(t *testing.T) {
	options := builder.Options{
		Home: t.TempDir(),
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
	if !slices.Contains(state.Using, "customer") || !slices.Contains(state.Needs, "Uses your existing Contacts from CRM") {
		t.Fatalf("expected the card to name the CRM reuse: %+v", state)
	}
}
