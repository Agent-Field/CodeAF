package shape

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type parsedDefinitionStore struct {
	svc.Store
	def *spec.Bundle
}

func (s parsedDefinitionStore) Definition(context.Context, string) (*spec.Bundle, error) {
	return s.def, nil
}

func TestRecordsShapeHookWithParsedDefinition(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files spec.Files
		valid bool
	}{
		{name: "nil files", valid: true},
		{name: "empty files", files: spec.Files{}, valid: true},
		{name: "no app source", files: spec.Files{spec.AgentFile: []byte("instructions")}, valid: true},
		{name: "app source", files: spec.Files{spec.AppFile: []byte(`{"records":{}}`)}, valid: true},
		{name: "empty app source", files: spec.Files{spec.AppFile: nil}},
		{name: "malformed app source", files: spec.Files{spec.AppFile: []byte(`{`)}},
	} {
		for _, mode := range []svc.DataMode{svc.DataSample, svc.DataReal} {
			t.Run(tc.name+"/"+string(mode), func(t *testing.T) {
				ctx := context.Background()
				def := &spec.Bundle{Files: maps.Clone(tc.files), App: &spec.App{
					ID: "items", Records: map[string]*spec.RecordType{
						"item": {Title: "name", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText, Required: true}}},
					},
				}}
				before := maps.Clone(def.Files)
				s := records.New(svc.Config{Root: t.TempDir()}, &svc.Services{Store: parsedDefinitionStore{def: def}})
				scope := svc.Scope{App: def.App.ID, Data: mode, Caller: svc.Caller{Member: svc.Owner}}
				if !tc.valid {
					if _, err := s.Query(ctx, scope, bridge.QueryParams{Type: "item"}); err == nil {
						t.Fatal("invalid app source accepted")
					}
					return
				}
				row := bridge.Record{ID: "item_1", Fields: map[string]any{"name": "First item"}}
				if mode == svc.DataSample {
					if err := s.Seed(ctx, scope.App, map[string][]bridge.Record{"item": {row}}); err != nil {
						t.Fatal(err)
					}
				} else {
					var err error
					row, err = s.Create(ctx, scope, bridge.CreateRecordParams{Type: "item", Set: row.Fields})
					if err != nil {
						t.Fatal(err)
					}
				}
				page, err := s.Query(ctx, scope, bridge.QueryParams{Type: "item"})
				if err != nil || page.Total != 1 {
					t.Fatalf("query = %+v, %v", page, err)
				}
				got, err := s.Get(ctx, scope, bridge.GetRecordParams{Type: "item", ID: row.ID})
				if err != nil || got.Fields["name"] != "First item" {
					t.Fatalf("get = %+v, %v", got, err)
				}
				if !reflect.DeepEqual(before, def.Files) {
					t.Fatal("record access changed the store's definition files")
				}
			})
		}
	}
}
