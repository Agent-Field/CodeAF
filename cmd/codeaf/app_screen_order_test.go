package main

import (
	"os"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestAppOpensTheFirstPlannedScreen(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	defs := store.New()
	draft, err := defs.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/schema/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	meta, err := defs.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}

	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	wire := wireOver(t, backend)
	want := []string{"deals", "companies", "contacts"}

	var kept store.Definition
	wire.must("apps.get", map[string]string{"app": meta.ID}, &kept)
	if !slices.Equal(kept.Screens, want) {
		t.Errorf("kept app screens = %v, want %v", kept.Screens, want)
	}

	var editing builder.PlanResult
	wire.must("apps.change", map[string]string{"app": meta.ID}, &editing)
	var preview builder.Preview
	wire.must("apps.preview", builder.PreviewRequest{Draft: editing.Draft}, &preview)
	if !slices.Equal(preview.Screens, want) {
		t.Errorf("preview screens = %v, want %v", preview.Screens, want)
	}
}
