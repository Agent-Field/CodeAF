package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestPersonalAppDefinitionsUseEngineHomeOverWire(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	definitions := store.New()
	draft, err := definitions.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/store/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	meta, err := definitions.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "apps", meta.ID, "current", "app.json")); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	wire := wireOver(t, backend)

	var got store.Definition
	wire.must("apps.get", map[string]string{"app": meta.ID}, &got)
	if got.Meta.ID != meta.ID || got.Meta.Version != 1 || len(got.Screens) == 0 {
		t.Fatalf("app from the engine home: %+v", got)
	}
	var kit struct {
		Kit   *int              `json:"kit"`
		Parts []json.RawMessage `json:"parts"`
	}
	wire.must("apps.kit", map[string]string{}, &kit)
	if kit.Kit == nil || *kit.Kit != 2 || len(kit.Parts) != 63 {
		t.Fatalf("embedded kit version = %v, parts = %d, want kit 2 with 63 parts", kit.Kit, len(kit.Parts))
	}
}
