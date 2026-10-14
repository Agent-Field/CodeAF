package records

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestFixtureBootstrapAndPersistedAppIsolation(t *testing.T) {
	for _, test := range []struct {
		name      string
		persisted bool
		modified  bool
		mode      svc.DataMode
		want      int
	}{
		{name: "fixture real", mode: svc.DataReal, want: 62},
		{name: "fixture sample", mode: svc.DataSample, want: 62},
		{name: "persisted real", persisted: true, mode: svc.DataReal},
		{name: "persisted sample", persisted: true, mode: svc.DataSample},
		{name: "other definition", modified: true, mode: svc.DataReal},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			files, _ := spec.GoldenCRM()
			if test.modified {
				files[spec.AgentFile] = append(files[spec.AgentFile], '\n')
			}
			def, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			cfg := svc.Config{Root: t.TempDir()}
			if test.persisted {
				if err := os.MkdirAll(filepath.Join(cfg.Root, "crm", "definition"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			hub := &captureHub{}
			all := &svc.Services{Store: definitionStore{def: def}, Hub: hub}
			s := New(cfg, all)
			scope := svc.Scope{App: "crm", Data: test.mode, Caller: svc.Caller{Member: svc.Owner}}
			page, err := s.Query(ctx, scope, bridge.QueryParams{Type: "deal"})
			if err != nil || page.Total != test.want || len(hub.events) != 0 {
				t.Fatalf("bootstrap: %+v %v events=%d", page, err, len(hub.events))
			}
			if test.mode == svc.DataSample {
				if _, err := os.Stat(filepath.Join(cfg.Root, "crm", "records.db")); !os.IsNotExist(err) {
					t.Fatal("sample query touched real data")
				}
			}
			if test.want == 0 {
				return
			}
			ids := make([]string, 0, len(page.Rows))
			for _, row := range page.Rows {
				ids = append(ids, row.ID)
			}
			if _, err := s.Delete(ctx, scope, bridge.DeleteRecordParams{Type: "deal", IDs: ids}); err != nil {
				t.Fatal(err)
			}
			restarted := New(cfg, all)
			page, err = restarted.Query(ctx, scope, bridge.QueryParams{Type: "deal"})
			if err != nil || page.Total != 0 {
				t.Fatalf("deleted fixture records returned after restart: %+v %v", page, err)
			}
		})
	}
}
