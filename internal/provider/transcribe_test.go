package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The wire shape is internal/voice's, and this pins it: JSON to
// /audio/transcriptions, the bytes base64 inside input_audio, and a BARE format
// word beside them.
func TestTranscribeUsesTheVerifiedAudioWireShape(t *testing.T) {
	var seen string
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = request.URL.Path
		if request.Header.Get("Authorization") != "Bearer media-key" {
			t.Errorf("auth = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		audio, _ := body["input_audio"].(map[string]any)
		if body["model"] != "ears/model" || audio == nil {
			t.Fatalf("transcription body = %+v", body)
		}
		if audio["format"] != "mp3" || audio["data"] != "aGVsbG8=" {
			t.Errorf("audio envelope = %+v", audio)
		}
		if body["language"] != "en" {
			t.Errorf("language = %v", body["language"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"text":"  hello there  ","usage":{"cost":0.02,"seconds":3}}`)
	}))
	media, err := NewMediaClient(Config{APIKey: "media-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}

	response, err := media.Transcribe(context.Background(), TranscriptionRequest{
		Model: "ears/model", Data: []byte("hello"), MIME: "audio/mpeg", Language: "en",
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if seen != "/api/v1/audio/transcriptions" {
		t.Fatalf("path = %q", seen)
	}
	if response.Text != "hello there" {
		t.Fatalf("text = %q (it should arrive trimmed)", response.Text)
	}
	if response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 0.02 {
		t.Fatalf("usage = %+v", response.Usage)
	}
}

// A deployment that carries no usage object still bills, and the cost header is
// where it says so — the same fallback Speak uses.
func TestTranscribeReadsCostFromTheHeader(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-OpenRouter-Cost", "0.004")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"text":"words"}`)
	}))
	media, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})

	response, err := media.Transcribe(context.Background(), TranscriptionRequest{
		Model: "ears/model", Data: []byte("x"), Filename: "memo.wav",
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 0.004 {
		t.Fatalf("usage = %+v", response.Usage)
	}
}

// The format word is refused HERE rather than sent, because a 400 from the
// endpoint carries nothing that points back at the caller's file.
func TestTranscribeRefusesWhatItCannotName(t *testing.T) {
	media, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1"})

	_, err := media.Transcribe(context.Background(), TranscriptionRequest{
		Model: "ears/model", Data: []byte("x"), Filename: "notes.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "mp3, wav, m4a, ogg, flac or webm") {
		t.Fatalf("err = %v", err)
	}

	if _, err := media.Transcribe(context.Background(), TranscriptionRequest{Model: "ears/model"}); err == nil ||
		!strings.Contains(err.Error(), "audio is empty") {
		t.Fatalf("empty audio err = %v", err)
	}
	if _, err := media.Transcribe(context.Background(), TranscriptionRequest{Data: []byte("x"), MIME: "audio/mpeg"}); err == nil ||
		!strings.Contains(err.Error(), "model is required") {
		t.Fatalf("missing model err = %v", err)
	}
}

// Two claims about one file, and the media type wins: a caller that sniffed the
// bytes put its answer there, and a name is only what somebody typed.
func TestTranscriptionFormatPrefersTheMediaType(t *testing.T) {
	for _, testCase := range []struct {
		mediaType, filename, want string
	}{
		{"audio/mpeg", "clip.wav", "mp3"},
		{"audio/wav; codecs=1", "clip.bin", "wav"},
		{"", "voice.m4a", "m4a"},
		{"", "song.FLAC", "flac"},
		{"application/octet-stream", "notes.txt", ""},
	} {
		if got := transcriptionFormat(testCase.mediaType, testCase.filename); got != testCase.want {
			t.Errorf("format(%q, %q) = %q, want %q", testCase.mediaType, testCase.filename, got, testCase.want)
		}
	}
}
