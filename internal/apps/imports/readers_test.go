package imports

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type connectorCall struct {
	scope svc.Scope
	tool  string
	args  map[string]any
}
type fakeConnectors struct {
	svc.Connectors
	reply any
	err   error
	calls []connectorCall
}

func (f *fakeConnectors) Call(_ context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	f.calls = append(f.calls, connectorCall{scope, tool, args})
	return f.reply, f.err
}
func hubspotTable() any {
	return map[string]any{"columns": hubspotColumns, "rows": [][]string{
		{"Annual renewal", "Globex", "48000", "appointmentscheduled", "mona@acme.com", "2026-11-30", "0.6", "2026-03-02"},
		{"Team plan", "Northwind", "14400", "qualifiedtobuy", "raj@acme.com", "2026-11-30", "0.4", "2026-03-03"},
		{"Security add-on", "Wayne Ent.", "7200", "appointmentscheduled", "outside@elsewhere.com", "2026-11-30", "0.6", "2026-03-04"},
		{"Expansion", "Hooli", "22500", "qualifiedtobuy", "jm@acme.com", "2026-11-30", "0.4", "2026-03-05"},
		{"Starter plan", "Soylent", "3600", "closedwon", "mona@acme.com", "2026-11-30", "1", "2026-03-06"},
		{"Annual support", "Globex", "5000", "closedwon", "raj@acme.com", "2026-11-30", "1", "2026-03-07"},
	}}
}

type fakeGrants struct {
	svc.Grants
	accounts bridge.AccountsResult
	err      error
	caller   svc.Caller
}

func (g *fakeGrants) Accounts(_ context.Context, caller svc.Caller) (bridge.AccountsResult, error) {
	g.caller = caller
	return g.accounts, g.err
}
func TestSourcesUseCallersConnections(t *testing.T) {
	for _, test := range []struct {
		name        string
		connections []bridge.Connection
		kinds       []string
	}{
		{"no accounts", nil, []string{"csv"}},
		{"HubSpot", []bridge.Connection{{Service: "hubspot", Status: "connected"}}, []string{"csv", "hubspot"}},
		{"both and duplicate", []bridge.Connection{{Service: "hubspot", Status: "connected"}, {Service: "airtable", Status: "connected"}, {Service: "hubspot", Status: "connected"}}, []string{"csv", "airtable", "hubspot"}},
		{"expired", []bridge.Connection{{Service: "airtable", Status: "expired"}}, []string{"csv"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			grants := &fakeGrants{accounts: bridge.AccountsResult{Connections: test.connections}}
			f.all.Connect.Grants = grants
			result, err := f.service.Sources(context.Background(), f.scope, bridge.ImportSourcesParams{Type: "deal"})
			if err != nil {
				t.Fatal(err)
			}
			kinds := []string{}
			for _, source := range result.Sources {
				kinds = append(kinds, source.Kind)
				if !source.Connected {
					t.Fatal("listed an unavailable source")
				}
			}
			if !reflect.DeepEqual(kinds, test.kinds) || grants.caller != f.scope.Caller {
				t.Fatalf("sources = %+v caller = %+v", result, grants.caller)
			}
		})
	}
}

func TestConnectorReadersUseDoorAndPreserveScope(t *testing.T) {
	for _, test := range []struct {
		name, tool string
		source     bridge.ImportSourceSelection
		reply      any
		want       any
	}{
		{"HubSpot table", "hubspot.deals.list", bridge.ImportSourceSelection{Kind: "hubspot"}, hubspotTable(), "Annual renewal"},
		{"HubSpot properties", "hubspot.deals.list", bridge.ImportSourceSelection{Kind: "hubspot", Object: "deals"}, map[string]any{"results": []any{map[string]any{"properties": map[string]any{"dealname": "Cedar", "amount": "1200", "createdate": "2026-03-02"}}}}, "Cedar"},
		{"Airtable", "airtable.records.list", bridge.ImportSourceSelection{Kind: "airtable", Base: "app123", Table: "Deals"}, map[string]any{"records": []any{map[string]any{"fields": map[string]any{"Deal Name": "Airtable renewal", "Amount": 3600}}}}, "Airtable renewal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, mode := range []svc.DataMode{svc.DataSample, svc.DataReal} {
				f := setup(t)
				f.scope.Data = mode
				door := &fakeConnectors{reply: test.reply}
				f.all.Connectors = door
				p := preview(t, f, bridge.ImportPreviewParams{Type: "deal", Source: test.source})
				if p.RowsShown[0].Fields["name"] != test.want || len(door.calls) != 1 || door.calls[0].tool != test.tool || door.calls[0].scope != f.scope {
					t.Fatalf("preview = %+v calls = %+v", p, door.calls)
				}
				if test.source.Kind == "airtable" && (door.calls[0].args["base"] != "app123" || door.calls[0].args["table"] != "Deals") {
					t.Fatal(door.calls)
				}
			}
		})
	}
}

func TestInvalidSourcesAndConnectorErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		source bridge.ImportSourceSelection
		reply  any
		err    error
		code   bridge.ErrorCode
	}{
		{"base64", bridge.ImportSourceSelection{Kind: "csv", File: "%%%"}, nil, nil, bridge.CodeInvalid},
		{"unknown", bridge.ImportSourceSelection{Kind: "unknown"}, nil, nil, bridge.CodeInvalid},
		{"HubSpot object", bridge.ImportSourceSelection{Kind: "hubspot", Object: "contacts"}, nil, nil, bridge.CodeInvalid},
		{"missing rows", bridge.ImportSourceSelection{Kind: "hubspot"}, map[string]any{"ok": true}, nil, bridge.CodeInvalid},
		{"expired", bridge.ImportSourceSelection{Kind: "hubspot"}, nil, reason(bridge.CodeConflict, bridge.ReasonExpired, "reconnect"), bridge.CodeConflict},
		{"row width", bridge.ImportSourceSelection{Kind: "airtable"}, map[string]any{"columns": []string{"Name", "Amount"}, "rows": [][]string{{"A"}}}, nil, bridge.CodeInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			f.all.Connectors = &fakeConnectors{reply: test.reply, err: test.err}
			_, err := f.service.Preview(context.Background(), f.scope, bridge.ImportPreviewParams{Type: "deal", Source: test.source})
			requireCode(t, err, test.code, "")
		})
	}
	t.Run("CSV BOM", func(t *testing.T) {
		reader := csvReader{file: base64.StdEncoding.EncodeToString([]byte("\ufeffDeal Name\nA\n"))}
		columns, rows, err := reader.Rows(context.Background())
		if err != nil || columns[0] != "Deal Name" || rows[0][0] != "A" {
			t.Fatalf("%v %v %v", columns, rows, err)
		}
	})
}

type pagedDoor struct {
	svc.Connectors
	calls   []map[string]any
	replies []any
}

func (d *pagedDoor) Call(_ context.Context, _ svc.Scope, _ string, args map[string]any) (any, error) {
	copy := map[string]any{}
	for key, value := range args {
		copy[key] = value
	}
	d.calls = append(d.calls, copy)
	return d.replies[min(len(d.calls)-1, len(d.replies)-1)], nil
}
func TestReadersFollowPaginationAndRejectCycles(t *testing.T) {
	for _, kind := range []string{"airtable", "hubspot"} {
		t.Run(kind, func(t *testing.T) {
			first := map[string]any{"columns": []string{"Deal Name"}, "rows": [][]string{{"A"}}}
			second := map[string]any{"columns": []string{"Deal Name", "Amount"}, "rows": [][]string{{"B", "42"}}}
			cursor := "offset"
			if kind == "airtable" {
				first["offset"] = "next"
			} else {
				cursor = "after"
				first["paging"] = map[string]any{"next": map[string]any{"after": "next"}}
			}
			f := setup(t)
			door := &pagedDoor{replies: []any{first, second}}
			f.all.Connectors = door
			p := preview(t, f, bridge.ImportPreviewParams{Type: "deal", Source: bridge.ImportSourceSelection{Kind: kind}})
			if p.Total != 2 || p.RowsShown[1].Fields["value"] != float64(42) || len(door.calls) != 2 || door.calls[1][cursor] != "next" {
				t.Fatalf("preview = %+v calls = %+v", p, door.calls)
			}
			door.calls = nil
			door.replies = []any{first}
			_, err := f.service.Preview(context.Background(), f.scope, bridge.ImportPreviewParams{Type: "deal", Source: bridge.ImportSourceSelection{Kind: kind}})
			requireCode(t, err, bridge.CodeInvalid, "")
		})
	}
}
