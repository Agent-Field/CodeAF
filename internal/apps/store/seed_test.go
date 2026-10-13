package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type seedRecords struct {
	svc.Records
	seed func(context.Context, string, map[string][]bridge.Record) error
}

func (r seedRecords) Seed(ctx context.Context, app string, rows map[string][]bridge.Record) error {
	return r.seed(ctx, app, rows)
}

func TestGoldenCRMSeed(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "first call", true: "explicit"}[explicit], func(t *testing.T) {
			hub := &eventLog{}
			all := &svc.Services{Hub: hub}
			s := NewWithOptions(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, all, Options{WithoutDemo: explicit})
			all.Store = s
			s.now = func() time.Time { return testTime }
			var calls int
			all.Records = seedRecords{seed: func(ctx context.Context, app string, rows map[string][]bridge.Record) error {
				calls++
				def, err := all.Store.Definition(ctx, app)
				if err != nil || def.App.ID != "crm" || def.App.Version != 1 {
					t.Fatalf("definition during seed = %+v, %v", def, err)
				}
				if len(rows["deal"]) != 6 || rows["deal"][0].ID == "" || rows["deal"][0].Fields["name"] == nil {
					t.Fatalf("seed rows = %+v", rows)
				}
				return nil
			}}
			if explicit {
				if err := s.SeedGoldenCRM(testCtx); err != nil {
					t.Fatal(err)
				}
			}
			apps, err := s.List(testCtx, owner)
			if err != nil || len(apps) != 1 || apps[0].ID != "crm" || apps[0].State != bridge.StateKept || apps[0].Version != 1 {
				t.Fatalf("seeded apps = %+v, %v", apps, err)
			}
			def, err := s.Definition(testCtx, "crm")
			if err != nil || def.App.Space != "personal" {
				t.Fatalf("seed definition = %+v, %v", def, err)
			}
			files, _ := spec.GoldenCRM()
			files, err = versionIdentity(files, "crm", 1)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetVersion(testCtx, "crm", 1)
			if err != nil || !reflect.DeepEqual(got, detail(apps[0], files)) {
				t.Fatalf("seeded files = %+v, %v", got, err)
			}
			versions, err := s.Versions(testCtx, "crm")
			if err != nil || len(versions) != 1 || versions[0].Tag != "v1" || versions[0].Request != def.Plan.Ask || versions[0].By != svc.Owner {
				t.Fatalf("seed versions = %+v, %v", versions, err)
			}
			if err := s.SeedGoldenCRM(testCtx); err != nil {
				t.Fatal(err)
			}
			if err := s.Archive(testCtx, "crm"); err != nil {
				t.Fatal(err)
			}
			reopened := New(svc.Config{Root: s.root}, all)
			if err := reopened.SeedGoldenCRM(testCtx); err != nil {
				t.Fatal(err)
			}
			archived, err := reopened.Get(testCtx, "crm")
			if err != nil || archived.State != StateArchived || calls != 1 {
				t.Fatalf("reseed = %+v, %v, calls=%d", archived, err, calls)
			}
			if len(hub.events) != 2 || hub.events[0].Topic != bridge.TopicState("crm") || hub.events[0].Data.(bridge.AppSummary) != apps[0] {
				t.Fatalf("seed events = %+v", hub.events)
			}
			for _, name := range []string{"records.db", "sample.db"} {
				if _, err := os.Stat(filepath.Join(s.appDir("crm"), name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("store wrote records file %s: %v", name, err)
				}
			}
		})
	}
}

func TestSeedLeavesNonemptyRegistryAlone(t *testing.T) {
	for _, kind := range []string{"app", "unmanaged file"} {
		t.Run(kind, func(t *testing.T) {
			s, hub := newStore(t)
			if kind == "app" {
				if _, err := s.Create(testCtx, owner, "Tasks", "Track tasks"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(s.root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(s.root, "marker"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadDir(s.root)
			if err != nil {
				t.Fatal(err)
			}
			events := len(hub.events)
			reopened := New(svc.Config{Root: s.root}, &svc.Services{Hub: hub})
			if _, err := reopened.List(testCtx, owner); err != nil {
				t.Fatal(err)
			}
			if err := reopened.SeedGoldenCRM(testCtx); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadDir(s.root)
			if err != nil || len(after) != len(before) || after[0].Name() != before[0].Name() || len(hub.events) != events {
				t.Fatalf("seed changed registry: %v, %v", after, err)
			}
		})
	}
}

func TestSeedFailureCanRetry(t *testing.T) {
	s, hub := newStore(t)
	failure := errors.New("seed failed")
	var calls int
	s.all.Records = seedRecords{seed: func(context.Context, string, map[string][]bridge.Record) error {
		calls++
		if calls == 1 {
			return failure
		}
		return nil
	}}
	if err := s.SeedGoldenCRM(testCtx); !errors.Is(err, failure) {
		t.Fatalf("seed error = %v", err)
	}
	if _, err := os.Stat(s.appDir("crm")); !errors.Is(err, os.ErrNotExist) || len(hub.events) != 0 {
		t.Fatalf("failed seed survived: %v, events=%v", err, hub.events)
	}
	if err := s.SeedGoldenCRM(testCtx); err != nil || calls != 2 || len(hub.events) != 1 {
		t.Fatalf("retry = %v, calls=%d, events=%v", err, calls, hub.events)
	}
}
