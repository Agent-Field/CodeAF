package store_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestKeptDemoRecordsPersist(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := map[bool]string{false: "first launch", true: "explicit seed"}[explicit]
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			cfg := svc.Config{Root: t.TempDir()}
			all := &svc.Services{}
			s := store.NewWithOptions(cfg, all, store.Options{WithoutDemo: explicit})
			all.Store, all.Records = s, records.New(cfg, all)
			if explicit {
				if err := s.SeedGoldenCRM(ctx); err != nil {
					t.Fatal(err)
				}
			}
			app, err := s.Get(ctx, "crm")
			if err != nil || app.State != bridge.StateKept || app.Version != 1 {
				t.Fatalf("demo = %+v, %v", app, err)
			}
			scope := svc.Scope{App: "crm", Data: svc.DataReal, Caller: svc.Caller{Member: svc.Owner}}
			page, err := all.Records.Query(ctx, scope, bridge.QueryParams{Type: "deal", Query: "sort:-amount", CountBy: "stage"})
			if err != nil || page.Total != 6 || len(page.Rows) != 6 || page.Rows[0].ID != "dl_northwind" || page.Counts["Proposal"] != 2 {
				t.Fatalf("kept demo records = %+v, %v", page, err)
			}
			if _, err := all.Records.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Qualified"}}); err != nil {
				t.Fatal(err)
			}
			all.Store, all.Records = store.New(cfg, all), records.New(cfg, all)
			if err := all.Store.(*store.Service).SeedGoldenCRM(ctx); err != nil {
				t.Fatal(err)
			}
			row, err := all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
			if err != nil || row.Fields["stage"] != "Qualified" {
				t.Fatalf("restarted demo = %+v, %v", row, err)
			}
		})
	}
}
