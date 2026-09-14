package tui3

import (
	"os"
	"path/filepath"
	"testing"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
)

func TestAPickerCacheForOneServiceIsNeverServedToAnother(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	const base = "https://shared.example/v1"
	if err := WriteModelCacheFor("deepseek", base, []Model{{ID: "deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	if got := CachedModelsFor("deepseek", base); len(got) != 1 || got[0].ID != "deepseek-chat" {
		t.Fatalf("own cache = %+v", got)
	}
	if got := CachedModelsFor("z-ai", base); len(got) != 0 {
		t.Fatalf("z-ai received deepseek cache: %+v", got)
	}

	wrong, err := os.ReadFile(ModelCachePathFor("deepseek", base))
	if err != nil {
		t.Fatal(err)
	}
	wrongPath := ModelCachePathFor("z-ai", base)
	if err := os.MkdirAll(filepath.Dir(wrongPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrongPath, wrong, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CachedModelsFor("z-ai", base); len(got) != 0 {
		t.Fatalf("copied cache with wrong ownership stamp was served: %+v", got)
	}
}

func TestTheDefaultPickerCacheKeepsItsLegacyPathAndShape(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	legacy := []byte(`{"models":[{"id":"legacy/model"}]}`)
	if err := os.MkdirAll(filepath.Dir(ModelCachePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ModelCachePath(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if ModelCachePathFor("", modelcatalog.DefaultBaseURL) != ModelCachePath() {
		t.Fatal("the default pair moved from its legacy cache path")
	}
	if got := CachedModels(); len(got) != 1 || got[0].ID != "legacy/model" {
		t.Fatalf("legacy cache = %+v", got)
	}
	if got := CachedModelsFor("deepseek", modelcatalog.DefaultBaseURL); len(got) != 0 {
		t.Fatalf("legacy default cache reached another service: %+v", got)
	}
}
