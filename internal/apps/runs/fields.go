package runs

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const aiFieldPrefix = "_ai_field_"
const aiFieldStep = "_fill_ai_fields"

func aiFieldRun(app App, typ string) (Definition, error) {
	record, ok := app.Records[typ]
	if !ok || record.Uses || app.Budget == nil || app.Budget.PerRun <= 0 || !slices.Contains(app.Capabilities.AI, "sort") {
		return Definition{}, ErrNotFound
	}
	properties := map[string]any{}
	var fields []string
	var prompt strings.Builder
	prompt.WriteString("Fill the declared fields using the source record.\n")
	for _, name := range slices.Sorted(maps.Keys(record.Fields)) {
		field := record.Fields[name]
		if !field.AIOnChange || field.AI == "" {
			continue
		}
		contract := schema.AIFieldOutput(field)
		if contract == nil {
			return Definition{}, errors.New("an AI field type is unavailable")
		}
		properties[name] = contract
		prompt.WriteString(name + ": " + field.AI + "\n")
		fields = append(fields, name)
	}
	if len(fields) == 0 {
		return Definition{}, ErrNotFound
	}
	return Definition{
		Run: aiFieldPrefix + typ, Title: "Fill AI fields",
		Budget: &schema.RunBudget{PerRun: app.Budget.PerRun},
		Steps: []Step{{ID: aiFieldStep, AI: &schema.AIStep{
			Prompt: prompt.String(), Output: map[string]any{
				"type": "object", "properties": properties, "required": fields, "additionalProperties": false,
			},
		}}},
	}, nil
}

func (e *Engine) fillAIFields(ctx context.Context, c *checkpoint, output any) (any, error) {
	if e.cfg.Records == nil {
		return nil, errors.New("record writes are unavailable")
	}
	_, vars := c.scope(e.cfg.Now())
	values, _ := output.(map[string]any)
	proposal, err := makeProposal("$record", vars, values)
	if err != nil {
		return nil, err
	}
	call := c.Action.Call
	call.Name = "records.update"
	result, err := e.cfg.Records(ctx, RecordCall{
		Call: call, Type: proposal.Type, IDs: []string{proposal.Record}, Set: proposal.Set, aiRevision: proposal.Revision,
	})
	if refused, ok := data.AsError(err); ok && (refused.Code == data.CodeStale || refused.Code == data.CodeNotFound) {
		// A newer event owns the next computation. This result was based on
		// an older record and must not overwrite it.
		return nil, nil
	}
	return result.Output, err
}
