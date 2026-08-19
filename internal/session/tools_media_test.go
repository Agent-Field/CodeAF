package session

// The scripted media client every generation test drives, and the two helpers
// that wire it: one fake for all four verbs, because there is one contract for
// all four (media_contract.go) and a test that stubbed each endpoint separately
// could not notice a verb reaching for the wrong one — which is precisely the
// bug generate_music shipped with, composing through the speech endpoint.

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// scriptedMedia is the whole [MediaGenerator] without a socket. It records what
// it was asked for so the tests can assert the request shape, and answers with
// whatever the test handed it.
//
// hold, when set, blocks a video render until the test closes it — which is how
// "the tool returned before the render finished" is asserted as a fact rather
// than as a race the test usually wins.
type scriptedMedia struct {
	mu       sync.Mutex
	seen     []provider.ImageRequest
	spoken   []provider.SpeechRequest
	composed []provider.MusicRequest
	filmed   []provider.VideoRequest

	// the painting half
	base64    string
	mediaType string
	usage     *ai.Usage
	err       error
	empty     bool

	// the speaking half
	audio      []byte
	speechErr  error
	speechCost *ai.Usage

	// the composing half, which is a lane of its own and not the speaking one
	// under another model (media_contract.go)
	music       []byte
	musicFormat string
	musicErr    error
	musicCost   *ai.Usage

	// the filming half
	video     []byte
	videoErr  error
	videoCost *ai.Usage
	hold      chan struct{}
}

// Transcribe answers the hear lane's method with nothing: no test in this file
// listens, and a scripted zero keeps the fake on the grown interface.
func (p *scriptedMedia) Transcribe(_ context.Context, _ provider.TranscriptionRequest) (*provider.TranscriptionResponse, error) {
	return &provider.TranscriptionResponse{}, nil
}

func (p *scriptedMedia) GenerateImage(_ context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	p.mu.Lock()
	p.seen = append(p.seen, request)
	p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	if p.empty {
		return &provider.ImageResponse{}, nil
	}
	return &provider.ImageResponse{
		Data:  []provider.GeneratedImage{{Base64: p.base64, MediaType: p.mediaType}},
		Usage: p.usage,
	}, nil
}

func (p *scriptedMedia) Speak(_ context.Context, request provider.SpeechRequest) (*provider.SpeechResponse, error) {
	p.mu.Lock()
	p.spoken = append(p.spoken, request)
	p.mu.Unlock()
	if p.speechErr != nil {
		return nil, p.speechErr
	}
	return &provider.SpeechResponse{Audio: p.audio, Usage: p.speechCost}, nil
}

func (p *scriptedMedia) GenerateMusic(_ context.Context, request provider.MusicRequest) (*provider.MusicResponse, error) {
	p.mu.Lock()
	p.composed = append(p.composed, request)
	p.mu.Unlock()
	if p.musicErr != nil {
		return nil, p.musicErr
	}
	return &provider.MusicResponse{Audio: p.music, Format: p.musicFormat, Usage: p.musicCost}, nil
}

func (p *scriptedMedia) GenerateVideo(ctx context.Context, request provider.VideoRequest) (*provider.VideoResponse, error) {
	p.mu.Lock()
	p.filmed = append(p.filmed, request)
	hold := p.hold
	p.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.videoErr != nil {
		return nil, p.videoErr
	}
	return &provider.VideoResponse{Video: p.video, Usage: p.videoCost}, nil
}

func (p *scriptedMedia) request(index int) provider.ImageRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index >= len(p.seen) {
		return provider.ImageRequest{}
	}
	return p.seen[index]
}

func (p *scriptedMedia) speech(index int) provider.SpeechRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index >= len(p.spoken) {
		return provider.SpeechRequest{}
	}
	return p.spoken[index]
}

func (p *scriptedMedia) composition(index int) provider.MusicRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index >= len(p.composed) {
		return provider.MusicRequest{}
	}
	return p.composed[index]
}

func (p *scriptedMedia) speeches() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.spoken)
}

func (p *scriptedMedia) film(index int) provider.VideoRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index >= len(p.filmed) {
		return provider.VideoRequest{}
	}
	return p.filmed[index]
}

func (p *scriptedMedia) films() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.filmed)
}

// mediaModels is [Config.MediaModel] over a fixed table: exactly the modalities
// the test wired, and "" for every other one — which is the absence law's other
// half, and the reason a test can put one verb on the belt without the rest.
func mediaModels(pairs map[string]string) func(string) string {
	return func(modality string) string { return pairs[modality] }
}

// newMediaAgent wires a session with the whole media belt: one client, and a
// resolver answering for all four generation modalities.
func newMediaAgent(t *testing.T, media *scriptedMedia, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = media
		config.MediaModel = mediaModels(map[string]string{
			modalityImage:  "paint/model",
			modalitySpeech: "talk/model",
			modalityMusic:  "compose/model",
			modalityVideo:  "film/model",
		})
		if mutate != nil {
			mutate(config)
		}
	})
}

// writeReference puts a real picture in the workspace for a reference argument
// to point at, and answers the path the model would say.
func writeReference(t *testing.T, workspace, name string, data []byte) string {
	t.Helper()
	full := filepath.Join(workspace, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("make the reference directory: %v", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("write the reference: %v", err)
	}
	return name
}

// dataURLBytes decodes one of the data URLs the belt builds, so a test can
// assert the reference carried the file's own bytes rather than something that
// merely looks like base64.
func dataURLBytes(t *testing.T, url string) []byte {
	t.Helper()
	_, encoded, found := strings.Cut(url, ";base64,")
	if !found {
		t.Fatalf("reference %q is not a base64 data URL", url)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("reference is not decodable: %v", err)
	}
	return decoded
}

// referenceURL is the same reading, one envelope out: it insists on the
// image_url wrapper every generation endpoint validates — a bare data URL is
// refused with "expected object, received string" — and hands back the URL
// inside it.
func referenceURL(t *testing.T, reference provider.ImageReference) string {
	t.Helper()
	if reference.Type != "image_url" {
		t.Fatalf("reference type = %q, want image_url", reference.Type)
	}
	if reference.ImageURL.URL == "" {
		t.Fatal("reference carried an empty image_url.url")
	}
	return reference.ImageURL.URL
}

// THE MANUAL LAW, for the verbs a bare session does not carry.
//
// internal/session/manual_test.go walks the belt of an agent with nothing wired,
// so it never sees a conditional tool: generate_image, speak, generate_music and
// generate_video are all absent there by the absence law, and a media verb could
// land with no page and pass the gate. This is the same gate over a belt that
// HAS them.
func TestTheManualMentionsEveryMediaToolOnTheBelt(t *testing.T) {
	agent, _ := newMediaAgent(t, &scriptedMedia{}, nil)
	carried := 0
	for _, tool := range agent.tools {
		switch tool.Name {
		case "generate_image", "speak", "generate_music", "generate_video":
			carried++
		default:
			continue
		}
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
	}
	if carried != 4 {
		t.Fatalf("a fully wired media belt carries %d of the four generation verbs", carried)
	}
}

// ── the composing verb ──────────────────────────────────────────────────────

// MUSIC IS ITS OWN LANE, pinned at the session's own belt.
//
// The temptation is to spell music as speak with a different model in it — the
// two make audio, the two land an mp3 — and that is exactly what the resident's
// leaf tool did until this wave. There is no music behind /audio/speech, so
// every call it made failed. What this holds is that generate_music reaches
// [MediaGenerator.GenerateMusic] with the MUSIC slot's model, sends the brief as
// the prompt, and never touches the speaking endpoint.
func TestGenerateMusicComposesOnItsOwnLaneAndNeverThroughSpeak(t *testing.T) {
	cost := 0.08
	media := &scriptedMedia{music: []byte("a song's worth of bytes"), musicFormat: "mp3", musicCost: &ai.Usage{Cost: &cost}}
	agent, workspace := newMediaAgent(t, media, nil)

	result, isError := runTool(t, agent, "generate_music", `{"prompt":"A calm solo piano loop, 90bpm"}`)
	if isError {
		t.Fatalf("generate_music = %q", result)
	}
	if got := media.composition(0); got.Model != "compose/model" || got.Prompt != "A calm solo piano loop, 90bpm" {
		t.Fatalf("music request = %+v", got)
	}
	if media.speeches() != 0 {
		t.Fatalf("a composition brief was sent to the speaking endpoint (%d calls)", media.speeches())
	}
	// The file is real, and the sentence names it.
	if !strings.Contains(result, ".mp3") || !strings.Contains(result, "composed by compose/model") {
		t.Fatalf("music result = %q", result)
	}
	written := filepath.Join(workspace, filepath.FromSlash(strings.Fields(result)[0]))
	data, readErr := os.ReadFile(written)
	if readErr != nil || string(data) != "a song's worth of bytes" {
		t.Fatalf("music file at %s = %q err=%v", written, data, readErr)
	}
}

// THE ABSENCE LAW, per modality. A machine with a speech model and no music
// model speaks and does not compose — and the reverse — because the two verbs
// ask the resolver different words (media_contract.go).
func TestTheComposingAndSpeakingVerbsAreAbsentIndependently(t *testing.T) {
	for _, want := range []struct {
		name    string
		wired   map[string]string
		carries string
		lacks   string
	}{
		{"a speech model and no music model", map[string]string{modalitySpeech: "talk/model"}, "speak", "generate_music"},
		{"a music model and no speech model", map[string]string{modalityMusic: "compose/model"}, "generate_music", "speak"},
	} {
		t.Run(want.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.Media = &scriptedMedia{}
				config.MediaModel = mediaModels(want.wired)
			})
			if !hasTool(agent, want.carries) {
				t.Fatalf("%s is not on the belt, so the model does not have the verb", want.carries)
			}
			if hasTool(agent, want.lacks) {
				t.Fatalf("%s is on the belt with no model behind it — absent-not-broken", want.lacks)
			}
		})
	}
}

// And with no client at all, neither exists however many models are named.
func TestNoMediaClientLeavesTheComposingVerbOff(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.MediaModel = mediaModels(map[string]string{modalityMusic: "compose/model"})
	})
	if hasTool(agent, "generate_music") {
		t.Fatal("generate_music is on the belt with no media client behind it")
	}
}
