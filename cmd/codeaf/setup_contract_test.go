package main

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"path/filepath"
	"testing"
)

// Unix parity 2 contract items 1/2: shared fresh default and saved choices.
// Contract 1: fresh setup supplies the CLI default; explicit and previously
// saved model choices survive another setup read.
func TestSetupContractFreshDefaultsAndSavedChoices(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	setup, err := b.Setup()
	if err != nil {
		t.Fatal(err)
	}
	if setup.DefaultModel != config.DefaultModel {
		t.Fatalf("fresh model = %q; want CLI default %q", setup.DefaultModel, config.DefaultModel)
	}
	chosen := "example/explicit-model"
	setup.DefaultModel = chosen
	if _, err := b.SetSetup(setup); err != nil {
		t.Fatal(err)
	}
	setup, err = b.Setup()
	if err != nil || setup.DefaultModel != chosen {
		t.Fatalf("saved setup = %+v, %v", setup, err)
	}
	settings, err := b.Settings()
	if err != nil || settings.DefaultModel != chosen {
		t.Fatalf("saved settings = %+v, %v", settings, err)
	}
}
