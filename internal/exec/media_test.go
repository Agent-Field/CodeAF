package exec

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type fakeMediaProvider struct {
	imageRequest   provider.ImageRequest
	speechRequest  provider.SpeechRequest
	imageResponse  *provider.ImageResponse
	speechResponse *provider.SpeechResponse
	imageErr       error
	speechErr      error
}

func (f *fakeMediaProvider) GenerateImage(_ context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	f.imageRequest = request
	return f.imageResponse, f.imageErr
}

func (f *fakeMediaProvider) Speak(_ context.Context, request provider.SpeechRequest) (*provider.SpeechResponse, error) {
	f.speechRequest = request
	return f.speechResponse, f.speechErr
}

type fakeModalities map[string]bool

func (f fakeModalities) Supports(modelID, direction, modality string) bool {
	return f[modelID+":"+direction+":"+modality]
}

func mediaToolbox(t *testing.T, provider MediaProvider, modalities ModalityCatalog) (*Toolbox, *Workspace) {
	t.Helper()
	space := workspace(t)
	tools := newToolboxWithMedia(space, 7, nil, nil, &MediaTools{
		Provider: provider, Catalog: modalities, ImageModel: "paint/model",
		SpeechModel: "voice/model", WorkingModel: "vision/model",
	})
	return tools, space
}

func TestGenerateImageWritesReadableNamesReferencesAndUsage(t *testing.T) {
	cost := 0.42
	fake := &fakeMediaProvider{imageResponse: &provider.ImageResponse{
		Data:  []provider.GeneratedImage{{Base64: base64.StdEncoding.EncodeToString([]byte("png")), MediaType: "image/png"}},
		Usage: &ai.Usage{PromptTokens: 12, Cost: &cost},
	}}
	tools, space := mediaToolbox(t, fake, fakeModalities{})
	reference := filepath.Join(space.Root(), "reference.png")
	if err := os.WriteFile(reference, []byte("reference"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := tools.Execute(context.Background(), "generate_image", `{"prompt":"Sunset over Harbor!","n":1,"size":"16:9","reference_paths":["reference.png"]}`)
	if result.IsError {
		t.Fatal(result.Content)
	}
	if result.Usage.Calls != 1 || result.Usage.Cost != cost || result.Usage.PromptTokens != 12 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	want := filepath.Join("media", "sunset-over-harbor-1.png")
	if _, ok := space.Locate(want); !ok || !strings.Contains(result.Content, filepath.ToSlash(want)) {
		t.Fatalf("generated image missing: %q", result.Content)
	}
	if fake.imageRequest.AspectRatio != "16:9" || len(fake.imageRequest.InputReferences) != 1 ||
		!strings.HasPrefix(fake.imageRequest.InputReferences[0], "data:image/png;base64,") {
		t.Fatalf("image request = %+v", fake.imageRequest)
	}
	if got := space.Artifacts(7); len(got) != 1 || got[0] != want {
		t.Fatalf("artifacts = %v", got)
	}
}

func TestMediaToolsAreRegisteredAndHonorTheSpendGate(t *testing.T) {
	fake := &fakeMediaProvider{imageResponse: &provider.ImageResponse{
		Data: []provider.GeneratedImage{{Base64: base64.StdEncoding.EncodeToString([]byte("png")), MediaType: "image/png"}},
	}}
	tools, _ := mediaToolbox(t, fake, fakeModalities{})
	definitions := tools.Definitions()
	if len(definitions) != 7 {
		t.Fatalf("definitions = %d, want four base + three media", len(definitions))
	}
	want := map[string]bool{"generate_image": false, "speak": false, "view_image": false}
	for _, definition := range definitions {
		if _, ok := want[definition.Function.Name]; ok {
			want[definition.Function.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s was not registered", name)
		}
	}
	tools.media.BeforeSpend = func(context.Context) error { return errors.New("rail") }
	result := tools.Execute(context.Background(), "generate_image", `{"prompt":"harbor"}`)
	if !result.IsError || !strings.Contains(result.Content, "paused at the daily budget") || result.Usage != (Usage{}) {
		t.Fatalf("gated result = %+v", result)
	}
}

func TestGenerateImageFailureIsCalmAndRecordsNoUsage(t *testing.T) {
	fake := &fakeMediaProvider{imageErr: errors.New("provider exploded with secrets")}
	tools, _ := mediaToolbox(t, fake, fakeModalities{})
	result := tools.Execute(context.Background(), "generate_image", `{"prompt":"harbor"}`)
	if !result.IsError || !strings.Contains(result.Content, "image generation failed") || strings.Contains(result.Content, "secrets") {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage != (Usage{}) {
		t.Fatalf("failed generation recorded usage: %+v", result.Usage)
	}
}

func TestSpeakWritesMP3AndOnlySuccessfulCallRecordsUsage(t *testing.T) {
	cost := 0.07
	fake := &fakeMediaProvider{speechResponse: &provider.SpeechResponse{Audio: []byte("audio"), Usage: &ai.Usage{Cost: &cost}}}
	tools, space := mediaToolbox(t, fake, fakeModalities{})
	result := tools.Execute(context.Background(), "speak", `{"text":"A calm morning","voice":"nova"}`)
	if result.IsError || result.Usage.Calls != 1 || result.Usage.Cost != cost {
		t.Fatalf("result = %+v", result)
	}
	if fake.speechRequest.Voice != "nova" || fake.speechRequest.ResponseFormat != "mp3" {
		t.Fatalf("speech request = %+v", fake.speechRequest)
	}
	if _, ok := space.Locate(filepath.Join("media", "a-calm-morning.mp3")); !ok {
		t.Fatal("speech file was not written")
	}

	fake.speechErr = errors.New("nope")
	fake.speechResponse = nil
	failed := tools.Execute(context.Background(), "speak", `{"text":"failure"}`)
	if !failed.IsError || failed.Usage != (Usage{}) || !strings.Contains(failed.Content, "speech synthesis failed") {
		t.Fatalf("failed speech = %+v", failed)
	}
}

func TestViewImageCapabilityGateBothWays(t *testing.T) {
	fake := &fakeMediaProvider{}
	modalities := fakeModalities{"vision/model:input:image": true}
	tools, space := mediaToolbox(t, fake, modalities)
	if err := os.WriteFile(filepath.Join(space.Root(), "look.png"), []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := tools.Execute(context.Background(), "view_image", `{"path":"look.png"}`)
	if loaded.IsError || len(loaded.Followup) != 2 || loaded.Followup[1].ImageURL == nil ||
		!strings.HasPrefix(loaded.Followup[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("loaded = %+v", loaded)
	}

	tools.media.WorkingModel = "text/model"
	refused := tools.Execute(context.Background(), "view_image", `{"path":"look.png"}`)
	if !refused.IsError || !strings.Contains(refused.Content, "text/model can't see images") || len(refused.Followup) != 0 {
		t.Fatalf("refused = %+v", refused)
	}
	tools.media.WorkingModel = "vision/model"
	unsupported := tools.Execute(context.Background(), "view_image", `{"path":"look.svg"}`)
	if !unsupported.IsError || !strings.Contains(unsupported.Content, "only png, jpeg, webp, and gif") {
		t.Fatalf("unsupported image = %+v", unsupported)
	}
}

func TestMediaSlugDerivesReadableBoundedNames(t *testing.T) {
	if got := MediaSlug("  Sunset, over the Harbor!! "); got != "sunset-over-the-harbor" {
		t.Fatalf("slug = %q", got)
	}
	if got := MediaSlug(strings.Repeat("Long prompt ", 20)); len(got) > 56 || strings.HasSuffix(got, "-") {
		t.Fatalf("long slug = %q (%d)", got, len(got))
	}
}
