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
	imageCalls     int
	speechCalls    int
	videoCalls     int
	imageRequest   provider.ImageRequest
	speechRequest  provider.SpeechRequest
	videoRequest   provider.VideoRequest
	imageResponse  *provider.ImageResponse
	speechResponse *provider.SpeechResponse
	videoResponse  *provider.VideoResponse
	imageErr       error
	speechErr      error
	videoErr       error
}

func (f *fakeMediaProvider) GenerateImage(_ context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	f.imageCalls++
	f.imageRequest = request
	return f.imageResponse, f.imageErr
}

func (f *fakeMediaProvider) Speak(_ context.Context, request provider.SpeechRequest) (*provider.SpeechResponse, error) {
	f.speechCalls++
	f.speechRequest = request
	return f.speechResponse, f.speechErr
}

func (f *fakeMediaProvider) GenerateVideo(_ context.Context, request provider.VideoRequest) (*provider.VideoResponse, error) {
	f.videoCalls++
	f.videoRequest = request
	return f.videoResponse, f.videoErr
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
		SpeechModel: "voice/model", MusicModel: "music/model", VideoModel: "motion/model",
		VideoPrice: 0.5, WorkingModel: "vision/model",
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
	if len(definitions) != 9 {
		t.Fatalf("definitions = %d, want four base + five media", len(definitions))
	}
	want := map[string]bool{"generate_image": false, "generate_music": false, "generate_video": false, "speak": false, "view_image": false}
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
	tools.media.BeforeSpend = func(context.Context, float64) error { return errors.New("rail") }
	result := tools.Execute(context.Background(), "generate_image", `{"prompt":"harbor"}`)
	if !result.IsError || !strings.Contains(result.Content, "paused at the daily budget") || result.Usage != (Usage{}) {
		t.Fatalf("gated result = %+v", result)
	}
	music := tools.Execute(context.Background(), "generate_music", `{"prompt":"harbor song"}`)
	video := tools.Execute(context.Background(), "generate_video", `{"prompt":"harbor motion"}`)
	if !music.IsError || !video.IsError || fake.imageCalls != 0 || fake.speechCalls != 0 || fake.videoCalls != 0 {
		t.Fatalf("generation escaped gate: image=%d speech=%d video=%d music=%+v video_result=%+v",
			fake.imageCalls, fake.speechCalls, fake.videoCalls, music, video)
	}
}

func TestGenerateMusicWritesMP3WithoutVoiceAndRecordsHeaderUsage(t *testing.T) {
	cost := 0.04
	fake := &fakeMediaProvider{speechResponse: &provider.SpeechResponse{Audio: []byte("music"), Usage: &ai.Usage{Cost: &cost}}}
	tools, space := mediaToolbox(t, fake, fakeModalities{})
	result := tools.Execute(context.Background(), "generate_music", `{"prompt":"Glass Bells at Dawn","format":"mp3"}`)
	if result.IsError || result.Usage.Calls != 1 || result.Usage.Cost != cost {
		t.Fatalf("music result = %+v", result)
	}
	if fake.speechRequest.Model != "music/model" || fake.speechRequest.Voice != "" || fake.speechRequest.ResponseFormat != "mp3" {
		t.Fatalf("music request = %+v", fake.speechRequest)
	}
	if _, ok := space.Locate(filepath.Join("media", "glass-bells-at-dawn.mp3")); !ok {
		t.Fatal("music file was not written")
	}

	fake.speechResponse = nil
	fake.speechErr = errors.New("provider detail")
	failed := tools.Execute(context.Background(), "generate_music", `{"prompt":"failure"}`)
	if !failed.IsError || failed.Usage != (Usage{}) || !strings.Contains(failed.Content, "music generation failed") || strings.Contains(failed.Content, "provider detail") {
		t.Fatalf("failed music = %+v", failed)
	}
}

func TestGenerateVideoMapsReferencesWritesMP4AndGatesKnownPrice(t *testing.T) {
	cost := 1.25
	fake := &fakeMediaProvider{videoResponse: &provider.VideoResponse{Video: []byte("video"), Usage: &ai.Usage{Cost: &cost}}}
	tools, space := mediaToolbox(t, fake, fakeModalities{})
	for _, name := range []string{"first.png", "last.jpg", "style.webp", "palette.gif"} {
		if err := os.WriteFile(filepath.Join(space.Root(), name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var estimated float64
	tools.media.BeforeSpend = func(_ context.Context, amount float64) error { estimated = amount; return nil }
	result := tools.Execute(context.Background(), "generate_video", `{
		"prompt":"Lanterns Across the Harbor","duration":8,"resolution":"720p","aspect_ratio":"16:9",
		"reference_paths":["first.png","last.jpg","style.webp","palette.gif"]}`)
	if result.IsError || result.Usage.Calls != 1 || result.Usage.Cost != cost || estimated != 0.5 {
		t.Fatalf("video result = %+v estimate=%v", result, estimated)
	}
	request := fake.videoRequest
	if request.Duration != 8 || request.Resolution != "720p" || request.AspectRatio != "16:9" ||
		len(request.FrameImages) != 2 || len(request.InputReferences) != 2 ||
		request.FrameImages[0].FrameType != "first_frame" || request.FrameImages[1].FrameType != "last_frame" ||
		request.InputReferences[0].FrameType != "" {
		t.Fatalf("video request = %+v", request)
	}
	if !strings.HasPrefix(request.FrameImages[0].ImageURL.URL, "data:image/png;base64,") ||
		!strings.HasPrefix(request.FrameImages[1].ImageURL.URL, "data:image/jpeg;base64,") {
		t.Fatalf("video refs = %+v", request.FrameImages)
	}
	if _, ok := space.Locate(filepath.Join("media", "lanterns-across-the-harbor.mp4")); !ok {
		t.Fatal("video file was not written")
	}
}

func TestGenerateVideoFailureAndTimeoutAreCalmAndCostNothing(t *testing.T) {
	fake := &fakeMediaProvider{videoErr: errors.New("video job failed: policy refused this prompt")}
	tools, _ := mediaToolbox(t, fake, fakeModalities{})
	failed := tools.Execute(context.Background(), "generate_video", `{"prompt":"failure"}`)
	if !failed.IsError || failed.Usage != (Usage{}) || !strings.Contains(failed.Content, "policy refused this prompt") || strings.Contains(failed.Content, "\n") {
		t.Fatalf("failed video = %+v", failed)
	}

	fake.videoErr = provider.ErrVideoTimeout
	timedOut := tools.Execute(context.Background(), "generate_video", `{"prompt":"slow"}`)
	if !timedOut.IsError || timedOut.Usage != (Usage{}) || !strings.Contains(timedOut.Content, "timed out") || strings.Contains(timedOut.Content, "\n") {
		t.Fatalf("timed-out video = %+v", timedOut)
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
