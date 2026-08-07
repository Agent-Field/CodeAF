package exec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type execRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn execRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type visionWireCapture struct {
	calls    int
	bodies   []map[string]any
	status   int
	response string
}

func (c *visionWireCapture) client(t *testing.T) *provider.Client {
	t.Helper()
	httpClient := &http.Client{Transport: execRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload, _ := io.ReadAll(request.Body)
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Errorf("decode vision request: %v", err)
		}
		c.calls++
		c.bodies = append(c.bodies, body)
		status := c.status
		if status == 0 {
			status = http.StatusOK
		}
		response := c.response
		if response == "" {
			response = `{"model":"vendor/vision-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"AgentField is centered in a blue card."}}],"usage":{"prompt_tokens":19,"completion_tokens":8,"cost":0.125}}`
		}
		return &http.Response{
			StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(response)), Request: request,
		}, nil
	})}
	client, err := provider.NewClient(provider.Config{
		APIKey: "test-key", BaseURL: "https://provider.example/v1", Model: "base/model", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

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
	if len(definitions) != 10 {
		t.Fatalf("definitions = %d, want five base + five media", len(definitions))
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
	for _, definition := range definitions {
		if definition.Function.Name != "view_image" {
			continue
		}
		encoded, _ := json.Marshal(definition)
		if !strings.Contains(definition.Function.Description, "vision model looks") || !strings.Contains(string(encoded), `"question"`) {
			t.Fatalf("view_image definition = %s", encoded)
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

func TestViewImageNativePathIsUnchangedAndCarriesTargetedQuestion(t *testing.T) {
	fake := &fakeMediaProvider{}
	modalities := fakeModalities{"vision/model:input:image": true}
	tools, space := mediaToolbox(t, fake, modalities)
	if err := os.WriteFile(filepath.Join(space.Root(), "look.png"), []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := tools.Execute(context.Background(), "view_image", `{"path":"look.png"}`)
	if loaded.IsError || len(loaded.Followup) != 2 || loaded.Followup[1].ImageURL == nil ||
		!strings.HasPrefix(loaded.Followup[1].ImageURL.URL, "data:image/png;base64,") ||
		loaded.Content != "⌾ look.png\nImage loaded for the next turn." || loaded.Followup[0].Text != "Image from look.png:" ||
		loaded.Usage != (Usage{}) {
		t.Fatalf("loaded = %+v", loaded)
	}
	targeted := tools.Execute(context.Background(), "view_image", `{"path":"look.png","question":"Does the text read exactly AgentField?"}`)
	if targeted.IsError || !strings.Contains(targeted.Followup[0].Text, "Question: Does the text read exactly AgentField?") {
		t.Fatalf("targeted native view = %+v", targeted)
	}

	tools.media.WorkingModel = "text/model"
	refused := tools.Execute(context.Background(), "view_image", `{"path":"look.png"}`)
	if !refused.IsError || refused.Content != "text/model can't see images — continue with file metadata or use a vision model; no vision model available" || len(refused.Followup) != 0 {
		t.Fatalf("refused = %+v", refused)
	}
	tools.media.WorkingModel = "vision/model"
	unsupported := tools.Execute(context.Background(), "view_image", `{"path":"look.svg"}`)
	if !unsupported.IsError || !strings.Contains(unsupported.Content, "only png, jpeg, webp, and gif") {
		t.Fatalf("unsupported image = %+v", unsupported)
	}
}

func TestViewImageProxySendsBase64QuestionAndReturnsAttributedUsage(t *testing.T) {
	wire := &visionWireCapture{}
	tools, space := mediaToolbox(t, &fakeMediaProvider{}, fakeModalities{})
	tools.media.WorkingModel = "text/model"
	tools.media.VisionModel = "vendor/vision-model"
	tools.media.VisionClient = wire.client(t)
	if err := os.WriteFile(filepath.Join(space.Root(), "look.png"), []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	gateBeforeCall := false
	tools.media.BeforeSpend = func(_ context.Context, amount float64) error {
		gateBeforeCall = wire.calls == 0 && amount == 0
		return nil
	}
	result := tools.Execute(context.Background(), "view_image", `{"path":"look.png","question":"Does the text read exactly AgentField?"}`)
	if result.IsError || result.Content != "seen by vision-model: AgentField is centered in a blue card." ||
		result.Usage.Calls != 1 || result.Usage.PromptTokens != 19 || result.Usage.CompletionTokens != 8 || result.Usage.Cost != 0.125 {
		t.Fatalf("proxy result = %+v", result)
	}
	if wire.calls != 1 || !gateBeforeCall || len(wire.bodies) != 1 {
		t.Fatalf("proxy calls=%d gate_before=%t", wire.calls, gateBeforeCall)
	}
	body := wire.bodies[0]
	if body["model"] != "vendor/vision-model" {
		t.Fatalf("proxy model = %#v", body["model"])
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("proxy messages = %#v", body["messages"])
	}
	message, _ := messages[0].(map[string]any)
	parts, _ := message["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("proxy content = %#v", message["content"])
	}
	textPart, _ := parts[0].(map[string]any)
	imagePart, _ := parts[1].(map[string]any)
	imageURL, _ := imagePart["image_url"].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "Does the text read exactly AgentField?" ||
		imagePart["type"] != "image_url" || !strings.HasPrefix(fmt.Sprint(imageURL["url"]), "data:image/png;base64,") {
		t.Fatalf("proxy content parts = %#v", parts)
	}
}

func TestViewImageProxyUsesDefaultQuestion(t *testing.T) {
	wire := &visionWireCapture{}
	tools, space := mediaToolbox(t, &fakeMediaProvider{}, fakeModalities{})
	tools.media.WorkingModel = "text/model"
	tools.media.VisionModel = "vendor/vision-model"
	tools.media.VisionClient = wire.client(t)
	if err := os.WriteFile(filepath.Join(space.Root(), "look.jpg"), []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := tools.Execute(context.Background(), "view_image", `{"path":"look.jpg"}`)
	if result.IsError || wire.calls != 1 {
		t.Fatalf("default proxy result = %+v calls=%d", result, wire.calls)
	}
	messages, _ := wire.bodies[0]["messages"].([]any)
	message, _ := messages[0].(map[string]any)
	parts, _ := message["content"].([]any)
	textPart, _ := parts[0].(map[string]any)
	if textPart["text"] != defaultImageQuestion {
		t.Fatalf("default question = %#v", textPart["text"])
	}
}

func TestViewImageProxySpendGateAndFailureRecordNoUsage(t *testing.T) {
	for _, test := range []struct {
		name string
		gate bool
	}{
		{name: "gate", gate: true},
		{name: "provider failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := &visionWireCapture{}
			if !test.gate {
				wire.status = http.StatusBadRequest
				wire.response = `{"error":{"message":"upstream failed"}}`
			}
			tools, space := mediaToolbox(t, &fakeMediaProvider{}, fakeModalities{})
			tools.media.WorkingModel = "text/model"
			tools.media.VisionModel = "vendor/vision-model"
			tools.media.VisionClient = wire.client(t)
			if err := os.WriteFile(filepath.Join(space.Root(), "look.webp"), []byte("pixels"), 0o644); err != nil {
				t.Fatal(err)
			}
			if test.gate {
				tools.media.BeforeSpend = func(context.Context, float64) error { return errors.New("rail") }
			}
			result := tools.Execute(context.Background(), "view_image", `{"path":"look.webp"}`)
			if !result.IsError || result.Usage != (Usage{}) {
				t.Fatalf("failed proxy = %+v", result)
			}
			wantCalls := 1
			if test.gate {
				wantCalls = 0
				if !strings.Contains(result.Content, "paused at the daily budget") {
					t.Fatalf("gated proxy = %+v", result)
				}
			}
			if wire.calls != wantCalls {
				t.Fatalf("provider calls = %d, want %d", wire.calls, wantCalls)
			}
		})
	}
}

func TestViewImageProxyRailsRefuseBeforeSpendOrCompletion(t *testing.T) {
	wire := &visionWireCapture{}
	tools, space := mediaToolbox(t, &fakeMediaProvider{}, fakeModalities{})
	tools.media.WorkingModel = "text/model"
	tools.media.VisionModel = "vendor/vision-model"
	tools.media.VisionClient = wire.client(t)
	spendCalls := 0
	tools.media.BeforeSpend = func(context.Context, float64) error { spendCalls++; return nil }
	if err := os.WriteFile(filepath.Join(space.Root(), "look.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(space.Root(), "huge.png")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxImageInputBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := tools.Execute(context.Background(), "view_image", `{"path":"look.svg"}`)
	huge := tools.Execute(context.Background(), "view_image", `{"path":"huge.png"}`)
	if !wrong.IsError || !strings.Contains(wrong.Content, "only png, jpeg, webp, and gif") ||
		!huge.IsError || !strings.Contains(huge.Content, "over the 10 MB image limit") || wire.calls != 0 || spendCalls != 0 {
		t.Fatalf("wrong=%+v huge=%+v provider_calls=%d spend_calls=%d", wrong, huge, wire.calls, spendCalls)
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

// The model argument has exactly three spellings — absent, "best", and a name
// — and every one of them still crosses the same rail and records the same
// usage. The resolved model is named in the artifact receipt, because "which
// model made this" is the first question about a file you keep.
func TestMediaModelArgumentReadsAllThreeSpellingsAndKeepsTheRail(t *testing.T) {
	audio := func() *fakeMediaProvider {
		cost := 0.09
		return &fakeMediaProvider{
			imageResponse:  &provider.ImageResponse{Data: []provider.GeneratedImage{{Base64: base64.StdEncoding.EncodeToString([]byte("png")), MediaType: "image/png"}}},
			speechResponse: &provider.SpeechResponse{Audio: []byte("sound"), Usage: &ai.Usage{Cost: &cost}},
			videoResponse:  &provider.VideoResponse{Video: []byte("video"), Usage: &ai.Usage{Cost: &cost}},
		}
	}
	for _, test := range []struct {
		name      string
		tool      string
		arguments string
		modality  string
		word      string
		want      string
	}{
		{name: "image slot default", tool: "generate_image", arguments: `{"prompt":"harbor"}`, want: "paint/model"},
		{name: "image best", tool: "generate_image", arguments: `{"prompt":"harbor","model":"best"}`,
			modality: "image", word: "best", want: "studio/grand"},
		{name: "image name", tool: "generate_image", arguments: `{"prompt":"harbor","model":"krea"}`,
			modality: "image", word: "krea", want: "studio/grand"},
		{name: "speech best", tool: "speak", arguments: `{"text":"a calm morning","model":"best"}`,
			modality: "speech", word: "best", want: "studio/grand"},
		{name: "music name", tool: "generate_music", arguments: `{"prompt":"glass bells","model":"lyria"}`,
			modality: "music", word: "lyria", want: "studio/grand"},
		{name: "video best", tool: "generate_video", arguments: `{"prompt":"lanterns","model":"best"}`,
			modality: "video", word: "best", want: "studio/grand"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := audio()
			tools, _ := mediaToolbox(t, fake, fakeModalities{})
			var sawModality, sawWord string
			tools.media.ResolveModel = func(modality, word string) (string, error) {
				sawModality, sawWord = modality, word
				return "studio/grand", nil
			}
			gated := 0
			tools.media.BeforeSpend = func(context.Context, float64) error { gated++; return nil }
			result := tools.Execute(context.Background(), test.tool, test.arguments)
			if result.IsError {
				t.Fatal(result.Content)
			}
			if sawModality != test.modality || sawWord != test.word {
				t.Fatalf("resolver saw (%q, %q), want (%q, %q)", sawModality, sawWord, test.modality, test.word)
			}
			served := fake.imageRequest.Model
			switch test.tool {
			case "speak", "generate_music":
				served = fake.speechRequest.Model
			case "generate_video":
				served = fake.videoRequest.Model
			}
			if served != test.want {
				t.Fatalf("served model = %q, want %q", served, test.want)
			}
			if !strings.Contains(result.Content, test.want) {
				t.Fatalf("artifact receipt = %q, want the resolved model named", result.Content)
			}
			if gated != 1 || result.Usage.Calls != 1 {
				t.Fatalf("rail crossings = %d usage = %+v", gated, result.Usage)
			}
		})
	}
}

func TestMediaWrongModalityNameIsRefusedCalmlyAndCostsNothing(t *testing.T) {
	fake := &fakeMediaProvider{}
	tools, _ := mediaToolbox(t, fake, fakeModalities{})
	tools.media.ResolveModel = func(modality, word string) (string, error) {
		return "", fmt.Errorf("hexgrad/kokoro-82m makes speech, not %s", modality)
	}
	spent := 0
	tools.media.BeforeSpend = func(context.Context, float64) error { spent++; return nil }
	result := tools.Execute(context.Background(), "generate_image", `{"prompt":"harbor","model":"kokoro"}`)
	if !result.IsError || !strings.Contains(result.Content, "makes speech, not image") {
		t.Fatalf("refusal = %+v", result)
	}
	if fake.imageCalls != 0 || spent != 0 || result.Usage != (Usage{}) {
		t.Fatalf("refusal cost something: calls=%d gate=%d usage=%+v", fake.imageCalls, spent, result.Usage)
	}
}

func TestMediaModelArgumentIsInertWithoutACatalog(t *testing.T) {
	fake := &fakeMediaProvider{imageResponse: &provider.ImageResponse{
		Data: []provider.GeneratedImage{{Base64: base64.StdEncoding.EncodeToString([]byte("png")), MediaType: "image/png"}},
	}}
	tools, _ := mediaToolbox(t, fake, fakeModalities{})
	result := tools.Execute(context.Background(), "generate_image", `{"prompt":"harbor","model":"best"}`)
	if result.IsError || fake.imageRequest.Model != "paint/model" {
		t.Fatalf("embedder without a catalog = %+v request=%+v", result, fake.imageRequest)
	}
}
