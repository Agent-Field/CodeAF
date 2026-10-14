package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// previewCRM builds the golden CRM as an unkept preview whose sample records
// are seeded the way the Builder seeds them.
func previewCRM(t *testing.T) (*store.Service, *svc.Services, string, map[string][]bridge.Record) {
	t.Helper()
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	s := store.NewWithOptions(cfg, all, store.Options{WithoutDemo: true})
	all.Store, all.Records = s, records.New(cfg, all)
	app, err := s.Create(ctx, svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, "CRM", "Track our sales")
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
	return s, all, app.ID, rows
}

func scopeOf(app string, mode svc.DataMode) svc.Scope {
	return svc.Scope{App: app, Data: mode, Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}}
}

func TestKeepCarriesSampleRecordsIntoRealRecords(t *testing.T) {
	ctx := context.Background()
	s, all, app, rows := previewCRM(t)
	edit := bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Qualified"}}
	if _, err := all.Records.Update(ctx, scopeOf(app, svc.DataSample), edit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Keep(ctx, app); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []svc.DataMode{svc.DataSample, svc.DataReal} {
		t.Run(string(mode), func(t *testing.T) {
			for typ, want := range rows {
				page, err := all.Records.Query(ctx, scopeOf(app, mode), bridge.QueryParams{Type: typ})
				if err != nil || page.Total != len(want) {
					t.Fatalf("%s after Keep = %d, %v; want %d", typ, page.Total, err, len(want))
				}
			}
			row, err := all.Records.Get(ctx, scopeOf(app, mode), bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
			if err != nil || row.Fields["stage"] != "Qualified" {
				t.Fatalf("preview edit after Keep = %+v, %v", row, err)
			}
		})
	}
	real := scopeOf(app, svc.DataReal)
	if _, err := all.Records.Update(ctx, real, bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Won"}}); err != nil {
		t.Fatal(err)
	}
	row, err := all.Records.Get(ctx, scopeOf(app, svc.DataSample), bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
	if err != nil || row.Fields["stage"] != "Qualified" {
		t.Fatalf("real write reached sample = %+v, %v", row, err)
	}
}

func TestLaterKeepsNeverReplaceRealRecords(t *testing.T) {
	cases := []struct {
		name  string
		later func(context.Context, *store.Service, string) error
	}{
		{"second keep", func(ctx context.Context, s *store.Service, app string) error {
			if _, err := s.Keep(ctx, app); err == nil {
				return errors.New("a kept app was kept again")
			}
			return nil
		}},
		{"new version", func(ctx context.Context, s *store.Service, app string) error {
			_, err := s.Restore(ctx, app, 1)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, all, app, _ := previewCRM(t)
			if _, err := s.Keep(ctx, app); err != nil {
				t.Fatal(err)
			}
			real := scopeOf(app, svc.DataReal)
			if _, err := all.Records.Delete(ctx, real, bridge.DeleteRecordParams{Type: "deal", ID: "dl_hooli"}); err != nil {
				t.Fatal(err)
			}
			if _, err := all.Records.Update(ctx, real, bridge.UpdateRecordParams{Type: "deal", ID: "dl_globex", Set: map[string]any{"stage": "Won"}}); err != nil {
				t.Fatal(err)
			}
			if err := tc.later(ctx, s, app); err != nil {
				t.Fatal(err)
			}
			if _, err := all.Records.Get(ctx, real, bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"}); err == nil {
				t.Fatal("a deleted real record came back")
			}
			row, err := all.Records.Get(ctx, real, bridge.GetRecordParams{Type: "deal", ID: "dl_globex"})
			if err != nil || row.Fields["stage"] != "Won" {
				t.Fatalf("real edit = %+v, %v", row, err)
			}
		})
	}
}

func TestKeepLeavesExistingRealRecordsAlone(t *testing.T) {
	ctx := context.Background()
	s, all, app, _ := previewCRM(t)
	real := scopeOf(app, svc.DataReal)
	if _, err := all.Records.Create(ctx, real, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "Real Co"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Keep(ctx, app); err != nil {
		t.Fatal(err)
	}
	for typ, want := range map[string]int{"company": 1, "deal": 0} {
		page, err := all.Records.Query(ctx, real, bridge.QueryParams{Type: typ})
		if err != nil || page.Total != want {
			t.Fatalf("real %s after Keep = %d, %v; want %d", typ, page.Total, err, want)
		}
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
			if err != nil || page.Total != 62 || len(page.Rows) != 62 || page.Rows[0].ID != "dl_01_globex" || page.Counts["Proposal"] != 5 {
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
			if _, err := all.Records.Update(ctx, scope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_05_umbrella", Set: map[string]any{"stage": "Talking"}}); err != nil {
				t.Fatal(err)
			}
			sampleScope := scope
			sampleScope.Data = svc.DataSample
			row, err := all.Records.Get(ctx, sampleScope, bridge.GetRecordParams{Type: "deal", ID: "dl_05_umbrella"})
			if err != nil || row.Fields["stage"] != "Lead" {
				t.Fatalf("real write changed sample = %+v, %v", row, err)
			}
			if _, err := all.Records.Update(ctx, sampleScope, bridge.UpdateRecordParams{Type: "deal", ID: "dl_05_umbrella", Set: map[string]any{"stage": "Won"}}); err != nil {
				t.Fatal(err)
			}
			all.Store, all.Records = store.New(cfg, all), records.New(cfg, all)
			if err := all.Store.(*store.Service).SeedGoldenCRM(ctx); err != nil {
				t.Fatal(err)
			}
			row, err = all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: "deal", ID: "dl_05_umbrella"})
			if err != nil || row.Fields["stage"] != "Talking" {
				t.Fatalf("restarted demo = %+v, %v", row, err)
			}
			row, err = all.Records.Get(ctx, sampleScope, bridge.GetRecordParams{Type: "deal", ID: "dl_05_umbrella"})
			if err != nil || row.Fields["stage"] != "Won" {
				t.Fatalf("restarted sample = %+v, %v", row, err)
			}
		})
	}
}
