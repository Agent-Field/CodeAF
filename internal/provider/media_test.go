package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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
