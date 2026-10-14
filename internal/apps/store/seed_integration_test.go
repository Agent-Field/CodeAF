package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestOrdinaryKeepLeavesRealRecordsEmpty(t *testing.T) {
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	s := store.NewWithOptions(cfg, all, store.Options{WithoutDemo: true})
	all.Store, all.Records = s, records.New(cfg, all)
	caller := svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
	app, err := s.Create(ctx, caller, "CRM", "Track our sales")
	if err != nil {
		t.Fatal(err)
	}
	files, sample := spec.GoldenCRM()
	if err := s.WriteFiles(ctx, app.ID, files); err != nil {
		t.Fatal(err)
	}
	var rows map[string][]bridge.Record
	if err := json.Unmarshal(sample, &rows); err != nil {
		t.Fatal(err)
	}
	if err := all.Records.Seed(ctx, app.ID, rows); err != nil {
		t.Fatal(err)
	}
	for _, state := range []bridge.AppState{bridge.StateBuilding, bridge.StatePreview} {
		if err := s.SetState(ctx, app.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Keep(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []svc.DataMode{svc.DataSample, svc.DataReal} {
		t.Run(string(mode), func(t *testing.T) {
			scope := svc.Scope{App: app.ID, Data: mode, Caller: caller}
			page, err := all.Records.Query(ctx, scope, bridge.QueryParams{Type: "deal"})
			want := 0
			if mode == svc.DataSample {
				want = len(rows["deal"])
			}
			if err != nil || page.Total != want {
				t.Fatalf("records after Keep = %+v, %v; want %d", page, err, want)
			}
		})
	}
}

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
			if err != nil || page.Total != 10 || len(page.Rows) != 10 || page.Rows[0].ID != "dl_northwind" || page.Counts["Proposal"] != 2 {
				t.Fatalf("kept demo records = %+v, %v", page, err)
			}
			_, sample := spec.GoldenCRM()
			var rows map[string][]bridge.Record
			if err := json.Unmarshal(sample, &rows); err != nil {
				t.Fatal(err)
			}
			for typ, records := range rows {
				for _, record := range records {
					row, err := all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: typ, ID: record.ID})
					if err != nil || row.ID != record.ID {
						t.Fatalf("demo %s %s = %+v, %v", typ, record.ID, row, err)
					}
				}
			}
			if _, err := all.Records.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Qualified"}}); err != nil {
				t.Fatal(err)
			}
			sampleScope := scope
			sampleScope.Data = svc.DataSample
			row, err := all.Records.Get(ctx, sampleScope, bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
			if err != nil || row.Fields["stage"] != "Lead" {
				t.Fatalf("real write changed sample = %+v, %v", row, err)
			}
			if _, err := all.Records.Update(ctx, sampleScope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Won"}}); err != nil {
				t.Fatal(err)
			}
			all.Store, all.Records = store.New(cfg, all), records.New(cfg, all)
			if err := all.Store.(*store.Service).SeedGoldenCRM(ctx); err != nil {
				t.Fatal(err)
			}
			row, err = all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
			if err != nil || row.Fields["stage"] != "Qualified" {
				t.Fatalf("restarted demo = %+v, %v", row, err)
			}
			row, err = all.Records.Get(ctx, sampleScope, bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
			if err != nil || row.Fields["stage"] != "Won" {
				t.Fatalf("restarted sample = %+v, %v", row, err)
			}
		})
	}
}
