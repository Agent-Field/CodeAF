package config

import (
	"context"
	"net/http"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
)

func TestAnEmptyCatalogLeavesEveryGenerationModelUnresolved(t *testing.T) {
	offline := &http.Client{Transport: configRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, http.ErrServerClosed
	})}
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://another-provider.example/v1", Dir: t.TempDir(), HTTPClient: offline,
	})
	configured := Config{}
	if image := configured.ResolveImageModel(models); image != "" {
		t.Errorf("empty catalog resolved image model %q", image)
	}
	if speech := configured.ResolveSpeechModel(models); speech != "" {
		t.Errorf("empty catalog resolved speech model %q", speech)
	}
	if music := configured.ResolveMusicModel(models); music != "" {
		t.Errorf("empty catalog resolved music model %q", music)
	}
	if video := configured.ResolveVideoModel(models); video != "" {
		t.Errorf("empty catalog resolved video model %q", video)
	}
}
