package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestSetTintPersistsAndPublishes(t *testing.T) {
	for _, tint := range spec.Tints {
		t.Run(tint, func(t *testing.T) {
			s, hub := newStore(t)
			before := kept(t, s)
			hub.events = nil
			got, err := s.SetTint(testCtx, owner, bridge.SetTintParams{App: before.ID, Tint: tint})
			if err != nil {
				t.Fatal(err)
			}
			if got.Tint != tint || got.ID != before.ID || got.State != bridge.StateKept || got.Version != before.Version+1 {
				t.Fatalf("summary=%+v", got)
			}
			if len(hub.events) != 1 || hub.events[0].Topic != bridge.TopicState(before.ID) || !reflect.DeepEqual(hub.events[0].Data, got) {
				t.Fatalf("events=%+v", hub.events)
			}
			data, err := os.ReadFile(filepath.Join(s.definitionDir(before.ID), spec.AppFile))
			if err != nil {
				t.Fatal(err)
			}
			var app spec.App
			if err := json.Unmarshal(data, &app); err != nil || app.Tint != tint {
				t.Fatalf("app.json tint=%q error=%v", app.Tint, err)
			}
			reopened := NewWithOptions(svc.Config{Root: s.root}, nil, Options{WithoutDemo: true})
			detail, err := reopened.Get(testCtx, before.ID)
			if err != nil || !reflect.DeepEqual(detail.AppSummary, got) {
				t.Fatalf("reopened=%+v error=%v", detail, err)
			}
			def, err := reopened.Definition(testCtx, before.ID)
			if err != nil || def.App.Tint != tint {
				t.Fatalf("definition=%+v error=%v", def, err)
			}
			apps, err := reopened.List(testCtx, owner)
			if err != nil || len(apps) != 1 || !reflect.DeepEqual(apps[0], got) {
				t.Fatalf("list=%+v error=%v", apps, err)
			}
			old, err := reopened.GetVersion(testCtx, before.ID, before.Version)
			if err != nil || old.Tint != before.Tint {
				t.Fatalf("old version=%+v error=%v", old, err)
			}
		})
	}
}

func TestSetTintRejectsWithoutChangingApp(t *testing.T) {
	for _, tt := range []struct {
		name, app, tint string
		caller          svc.Caller
		preview         bool
		code            bridge.ErrorCode
	}{
		{"invalid", "crm", "violet", owner, false, bridge.CodeInvalid},
		{"empty", "crm", "", owner, false, bridge.CodeInvalid},
		{"case", "crm", "Iris", owner, false, bridge.CodeInvalid},
		{"non-owner", "crm", "rose", svc.Caller{Member: "raj", Role: svc.RoleEditor}, false, bridge.CodeDenied},
		{"agent", "crm", "rose", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor, Agent: true}, false, bridge.CodeDenied},
		{"unknown", "missing", "rose", owner, false, bridge.CodeNotFound},
		{"preview", "crm", "rose", owner, true, bridge.CodeConflict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, hub := newStore(t)
			if tt.preview {
				preview(t, s)
			} else {
				kept(t, s)
			}
			before, err := s.Get(testCtx, "crm")
			if err != nil {
				t.Fatal(err)
			}
			hub.events = nil
			_, err = s.SetTint(testCtx, tt.caller, bridge.SetTintParams{App: tt.app, Tint: tt.tint})
			expectCode(t, err, tt.code)
			if tt.code == bridge.CodeInvalid {
				var wire *bridge.Error
				if !errors.As(err, &wire) || wire.Message != "tint must be one of iris, sage, sand, tide, rose, graphite" {
					t.Fatal(err)
				}
			}
			after, err := s.Get(testCtx, "crm")
			if err != nil || !reflect.DeepEqual(after, before) || len(hub.events) != 0 {
				t.Fatalf("app changed after rejection: %+v events=%+v error=%v", after, hub.events, err)
			}
		})
	}
}
