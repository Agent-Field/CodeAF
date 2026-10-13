package imports

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type hubspotReader struct {
	door   svc.Connectors
	scope  svc.Scope
	source bridge.ImportSourceSelection
}

var hubspotColumns = []string{"Deal Name", "Company", "Amount", "Deal Stage", "Deal Owner", "Close Date", "Probability", "Create Date"}
var hubspotProperties = []string{"dealname", "company", "amount", "dealstage", "hubspot_owner_id", "closedate", "hs_deal_stage_probability", "createdate"}

func (r hubspotReader) Rows(ctx context.Context) ([]string, [][]string, error) {
	object := r.source.Object
	if object == "" {
		object = "deals"
	}
	if object != "deals" {
		return nil, nil, invalid("HubSpot imports support deals")
	}
	return readPages(ctx, r.door, r.scope, "hubspot.deals.list", map[string]any{"object": object}, "results", "properties", hubspotProperties, "after")
}
