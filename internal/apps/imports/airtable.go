package imports

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type airtableReader struct {
	door   svc.Connectors
	scope  svc.Scope
	source bridge.ImportSourceSelection
}

func (r airtableReader) Rows(ctx context.Context) ([]string, [][]string, error) {
	return readPages(ctx, r.door, r.scope, "airtable.records.list", map[string]any{"base": r.source.Base, "table": r.source.Table}, "records", "fields", nil, "offset")
}
