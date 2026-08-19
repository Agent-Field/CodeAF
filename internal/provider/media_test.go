package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMediaClientUsesVerifiedImageAndSpeechWireShapes(t *testing.T) {
	var paths []string
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		if request.Header.Get("Authorization") != "Bearer media-key" {
			t.Errorf("auth = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		switch request.URL.Path {
		case "/api/v1/images":
			if body["model"] != "paint/model" || body["prompt"] != "a harbor" || body["output_format"] != "png" {
				t.Errorf("image body = %+v", body)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"data":[{"b64_json":"aW1hZ2U=","media_type":"image/png"}],"usage":{"cost":0.25}}`)
		case "/api/v1/audio/speech":
			if body["model"] != "voice/model" || body["input"] != "hello" || body["response_format"] != "mp3" {
				t.Errorf("speech body = %+v", body)
			}
			writer.Header().Set("X-OpenRouter-Cost", "0.03")
			_, _ = writer.Write([]byte("mp3 bytes"))
		default:
			http.NotFound(writer, request)
		}
	}))
	media, err := NewMediaClient(Config{APIKey: "media-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	image, err := media.GenerateImage(context.Background(), ImageRequest{Model: "paint/model", Prompt: "a harbor"})
	if err != nil || len(image.Data) != 1 || image.Usage == nil || image.Usage.Cost == nil || *image.Usage.Cost != 0.25 {
		t.Fatalf("image = %+v err=%v", image, err)
	}
	speech, err := media.Speak(context.Background(), SpeechRequest{Model: "voice/model", Input: "hello", Voice: "alloy"})
	if err != nil || string(speech.Audio) != "mp3 bytes" || speech.Usage == nil || speech.Usage.Cost == nil || *speech.Usage.Cost != 0.03 {
		t.Fatalf("speech = %+v err=%v", speech, err)
	}
	if strings.Join(paths, ",") != "/api/v1/images,/api/v1/audio/speech" {
		t.Fatalf("paths = %v", paths)
	}
}

// The image endpoint validates input_references as an array of OBJECTS, and it
// says so by refusing the whole render: a bare data URL comes back as
// `{"expected":"object","code":"invalid_type","path":["input_references",0]}`
// and nothing is drawn. Text-to-image never touches the field, so the break was
// invisible until somebody asked for an edit of a picture they already had.
//
// This test reads the BYTES ON THE WIRE rather than the Go struct, because the
// struct is exactly what was wrong: the shape has to be pinned where the
// endpoint reads it.
func TestImageReferencesRideAsObjectsOnTheWire(t *testing.T) {
	var body map[string]any
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"data":[{"b64_json":"aW1hZ2U=","media_type":"image/png"}]}`)
	}))
	media, err := NewMediaClient(Config{APIKey: "media-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := media.GenerateImage(context.Background(), ImageRequest{
		Model: "paint/model", Prompt: "now in colour",
		InputReferences: []ImageReference{NewImageReference("data:image/png;base64,c2tldGNo")},
	}); err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}

	references, _ := body["input_references"].([]any)
	if len(references) != 1 {
		t.Fatalf("input_references = %+v, want one entry", body["input_references"])
	}
	envelope, isObject := references[0].(map[string]any)
	if !isObject {
		t.Fatalf("input_references[0] = %T (%v), want an object — a string is refused with "+
			"\"expected object, received string\"", references[0], references[0])
	}
	if envelope["type"] != "image_url" {
		t.Fatalf("reference type = %v, want image_url", envelope["type"])
	}
	url, _ := envelope["image_url"].(map[string]any)
	if url == nil || url["url"] != "data:image/png;base64,c2tldGNo" {
		t.Fatalf("reference image_url = %+v", envelope["image_url"])
	}
	// A frame type is the VIDEO endpoint's slot alone; an image reference must
	// not carry an empty one into a request that has no notion of frames.
	if _, present := envelope["frame_type"]; present {
		t.Fatalf("image reference carried a frame_type: %+v", envelope)
	}

	// And a request with no references does not send the key at all, so a plain
	// text-to-image call still reaches models that reject an empty array.
	body = nil
	if _, err := media.GenerateImage(context.Background(), ImageRequest{Model: "paint/model", Prompt: "a harbor"}); err != nil {
		t.Fatalf("GenerateImage without references: %v", err)
	}
	if _, present := body["input_references"]; present {
		t.Fatalf("a reference-free request still sent input_references: %+v", body)
	}
}

func TestMusicUsesSpeechEndpointWithoutVoice(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/audio/speech" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["model"] != "google/lyria-3-clip-preview" || body["input"] != "glass bells at dawn" || body["response_format"] != "mp3" {
			t.Fatalf("music body = %+v", body)
		}
		if _, present := body["voice"]; present {
			t.Fatalf("music body included voice: %+v", body)
		}
		writer.Header().Set("X-OpenRouter-Cost", "0.04")
		_, _ = writer.Write([]byte("music bytes"))
	}))
	media, err := NewMediaClient(Config{APIKey: "media-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	clip, err := media.Speak(context.Background(), SpeechRequest{
		Model: "google/lyria-3-clip-preview", Input: "glass bells at dawn", ResponseFormat: "mp3",
	})
	if err != nil || string(clip.Audio) != "music bytes" || clip.Usage == nil || clip.Usage.Cost == nil || *clip.Usage.Cost != 0.04 {
		t.Fatalf("music = %+v err=%v", clip, err)
	}
}

func TestGenerateVideoSubmitsPollsAndDownloadsWithoutSleeping(t *testing.T) {
	states := []string{"pending", "in_progress", "completed"}
	polls := 0
	var delays []time.Duration
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/videos":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["model"] != "motion/model" || body["prompt"] != "a quiet harbor" || body["duration"] != float64(8) ||
				body["resolution"] != "720p" || body["aspect_ratio"] != "16:9" {
				t.Errorf("video body = %+v", body)
			}
			frames, _ := body["frame_images"].([]any)
			if len(frames) != 2 || frames[0].(map[string]any)["frame_type"] != "first_frame" ||
				frames[1].(map[string]any)["frame_type"] != "last_frame" {
				t.Errorf("frame images = %+v", frames)
			}
			_, _ = io.WriteString(writer, `{"id":"job-1","polling_url":"/api/v1/videos/job-1","status":"pending"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/videos/job-1":
			if request.Header.Get("Authorization") != "Bearer media-key" {
				t.Errorf("poll auth = %q", request.Header.Get("Authorization"))
			}
			status := states[polls]
			polls++
			if status == "completed" {
				_, _ = io.WriteString(writer, `{"id":"job-1","status":"completed","unsigned_urls":["https://files.example/video.mp4"],"usage":{"cost":1.25}}`)
			} else {
				_, _ = io.WriteString(writer, `{"id":"job-1","status":"`+status+`"}`)
			}
		case request.Method == http.MethodGet && request.URL.Host == "files.example":
			if request.Header.Get("Authorization") != "" {
				t.Errorf("bearer credential leaked to download host")
			}
			_, _ = writer.Write([]byte("mp4 bytes"))
		default:
			http.NotFound(writer, request)
		}
	}))
	media, err := NewMediaClient(Config{APIKey: "media-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	media.videoNow = func() time.Time { return now }
	media.videoWait = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		now = now.Add(delay)
		return nil
	}
	response, err := media.GenerateVideo(context.Background(), VideoRequest{
		Model: "motion/model", Prompt: "a quiet harbor", Duration: 8, Resolution: "720p", AspectRatio: "16:9",
		FrameImages: []ImageReference{
			{Type: "image_url", ImageURL: ImageReferenceURL{URL: "data:image/png;base64,Zmlyc3Q="}, FrameType: "first_frame"},
			{Type: "image_url", ImageURL: ImageReferenceURL{URL: "data:image/png;base64,bGFzdA=="}, FrameType: "last_frame"},
		},
	})
	if err != nil || string(response.Video) != "mp4 bytes" || response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 1.25 {
		t.Fatalf("video = %+v err=%v", response, err)
	}
	if polls != 3 || len(delays) != 3 || delays[0] != 5*time.Second || delays[1] != 10*time.Second || delays[2] != 20*time.Second {
		t.Fatalf("polls = %d delays = %v", polls, delays)
	}
}

func TestGenerateVideoFailureAndTimeout(t *testing.T) {
	t.Run("failure detail", func(t *testing.T) {
		client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodPost {
				_, _ = io.WriteString(writer, `{"id":"job-fail","status":"pending"}`)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"job-fail","status":"failed","error":{"code":"policy","message":"content was refused"}}`)
		}))
		media, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
		media.videoWait = func(context.Context, time.Duration) error { return nil }
		_, err := media.GenerateVideo(context.Background(), VideoRequest{Model: "motion/model", Prompt: "prompt"})
		if err == nil || !strings.Contains(err.Error(), "policy: content was refused") {
			t.Fatalf("failure = %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		polls := 0
		client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodPost {
				_, _ = io.WriteString(writer, `{"id":"job-slow","status":"pending"}`)
				return
			}
			polls++
			_, _ = io.WriteString(writer, `{"id":"job-slow","status":"pending"}`)
		}))
		media, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
		now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
		media.videoNow = func() time.Time { return now }
		media.videoWait = func(_ context.Context, delay time.Duration) error { now = now.Add(delay); return nil }
		media.videoTimeout = 12 * time.Second
		_, err := media.GenerateVideo(context.Background(), VideoRequest{Model: "motion/model", Prompt: "prompt"})
		if !errors.Is(err, ErrVideoTimeout) || polls != 1 {
			t.Fatalf("timeout = %v polls=%d", err, polls)
		}
	})
}
