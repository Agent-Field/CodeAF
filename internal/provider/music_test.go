package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// musicStream builds the event stream the router actually sends: the clip
// arrives base64 in ONE delta.audio.data event, with content deltas around it
// carrying placeholder markers rather than words.
func musicStream(clip []byte, format string) string {
	audio := fmt.Sprintf(`{"choices":[{"delta":{"audio":{"data":%q,"format":%q}}}]}`,
		base64.StdEncoding.EncodeToString(clip), format)
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"<audio>"}}]}`,
		"data: " + audio,
		`data: {"choices":[{"delta":{}}],"usage":{"cost":0.08}}`,
		"data: [DONE]",
		"",
	}, "\n\n")
}

// THE THREE THINGS THE ROUTER REFUSES A MUSIC REQUEST FOR, pinned on the bytes
// that leave rather than on the Go struct — the struct is exactly what was
// wrong before, and every one of these was learned by being refused:
//
//   - modalities must ask for audio, or a chat model answers in prose
//   - stream must be true, or the request is 400 "Audio output requires
//     stream: true"
//   - max_tokens must be ABSENT, because a cap made the provider answer 5xx
//     rather than truncate
//
// And the endpoint is /chat/completions, which is the whole surprise of this
// lane: there is no music endpoint on the router at all.
func TestMusicRequestAsksForAudioOverAStreamAndSendsNoTokenCap(t *testing.T) {
	var (
		body map[string]any
		path string
	)
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		_ = json.NewDecoder(request.Body).Decode(&body)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, musicStream([]byte("a song"), "mp3"))
	}))
	media, err := NewMediaClient(Config{APIKey: "media-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	response, err := media.GenerateMusic(context.Background(), MusicRequest{
		Model: "compose/model", Prompt: "A calm solo piano loop",
	})
	if err != nil {
		t.Fatalf("GenerateMusic: %v", err)
	}

	if path != "/api/v1/chat/completions" {
		t.Fatalf("music posted to %q — there is no music endpoint; the lane is chat completions", path)
	}
	modalities, _ := body["modalities"].([]any)
	if len(modalities) != 1 || modalities[0] != "audio" {
		t.Fatalf("modalities = %+v, want [audio] — without it a chat model answers in prose", body["modalities"])
	}
	if body["stream"] != true {
		t.Fatalf("stream = %v, want true — a non-streamed request is refused with "+
			`400 "Audio output requires stream: true"`, body["stream"])
	}
	if _, capped := body["max_tokens"]; capped {
		t.Fatalf("the music request carried max_tokens (%v); a cap made the provider answer 5xx rather than truncate", body["max_tokens"])
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one user turn", body["messages"])
	}
	turn, _ := messages[0].(map[string]any)
	if turn["role"] != "user" || turn["content"] != "A calm solo piano loop" {
		t.Fatalf("message = %+v", turn)
	}

	// And what comes back is the DECODED clip, its format, and the cost.
	if string(response.Audio) != "a song" || response.Format != "mp3" {
		t.Fatalf("music response = %+v", response)
	}
	if response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost != 0.08 {
		t.Fatalf("music usage = %+v", response.Usage)
	}
}

// THE AUDIO IS NOT THE TEXT. delta.content carries placeholder markers, not
// lyrics and not a transcript, and a reader that accumulated it would hand back
// prose and drop the song. This stream says "<audio>" in content and the clip in
// delta.audio; only the clip may come out.
func TestMusicReadsTheAudioDeltaAndNeverTheContentDelta(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, musicStream([]byte("the actual clip"), "mp3"))
	}))
	media, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	response, err := media.GenerateMusic(context.Background(), MusicRequest{Model: "compose/model", Prompt: "a loop"})
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Audio) != "the actual clip" {
		t.Fatalf("music = %q, want the audio delta and not the content delta", response.Audio)
	}
}

// A clip split across several events is still one base64 document, and it is
// decoded ONCE at the end: base64 is only decodable on four-character
// boundaries, so a per-event decode would corrupt a split that did not land on
// one — silently, in a way nothing downstream could detect.
func TestMusicJoinsASplitClipBeforeDecodingIt(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("a longer piece of music"))
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		// Deliberately split off a boundary that is not a multiple of four.
		for _, part := range []string{encoded[:5], encoded[5:11], encoded[11:]} {
			_, _ = io.WriteString(writer, fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"audio\":{\"data\":%q}}}]}\n\n", part))
		}
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	media, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	response, err := media.GenerateMusic(context.Background(), MusicRequest{Model: "compose/model", Prompt: "a loop"})
	if err != nil {
		t.Fatalf("GenerateMusic: %v", err)
	}
	if string(response.Audio) != "a longer piece of music" {
		t.Fatalf("joined music = %q", response.Audio)
	}
}

// The refusals, each in its own words. A model that cannot compose is the
// interesting one: it answers the chat request perfectly well, in prose, and
// sends no audio delta at all — so "no audio" has to be a failure and never an
// empty file written to disk.
func TestMusicRefusals(t *testing.T) {
	media := func(t *testing.T, handler http.HandlerFunc) *MediaClient {
		t.Helper()
		client, err := NewMediaClient(Config{APIKey: "k", BaseURL: "https://openrouter.example/api/v1", HTTPClient: handlerClient(handler)})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}

	t.Run("a model that answers in words and sends no audio", func(t *testing.T) {
		client := media(t, func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"Here is a description of the music.\"}}]}\n\ndata: [DONE]\n\n")
		})
		_, err := client.GenerateMusic(context.Background(), MusicRequest{Model: "chat/model", Prompt: "a loop"})
		if err == nil || !strings.Contains(err.Error(), "may not be a music model") {
			t.Fatalf("no-audio error = %v", err)
		}
	})

	t.Run("an error delivered inside a 200 stream", func(t *testing.T) {
		client := media(t, func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `data: {"error":{"code":"policy","message":"content was refused"}}`+"\n\ndata: [DONE]\n\n")
		})
		_, err := client.GenerateMusic(context.Background(), MusicRequest{Model: "compose/model", Prompt: "a loop"})
		if err == nil || !strings.Contains(err.Error(), "policy: content was refused") {
			t.Fatalf("in-stream error = %v", err)
		}
	})

	t.Run("audio that will not decode", func(t *testing.T) {
		client := media(t, func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"audio\":{\"data\":\"!!!not base64!!!\"}}}]}\n\ndata: [DONE]\n\n")
		})
		_, err := client.GenerateMusic(context.Background(), MusicRequest{Model: "compose/model", Prompt: "a loop"})
		if err == nil || !strings.Contains(err.Error(), "could not be decoded") {
			t.Fatalf("undecodable audio error = %v", err)
		}
	})

	t.Run("an HTTP refusal", func(t *testing.T) {
		client := media(t, func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"message":"Audio output requires stream: true"}}`)
		})
		_, err := client.GenerateMusic(context.Background(), MusicRequest{Model: "compose/model", Prompt: "a loop"})
		if err == nil || !strings.Contains(err.Error(), "Audio output requires stream") {
			t.Fatalf("http error = %v", err)
		}
	})

	// The two arguments the request cannot be built without, refused before a
	// socket is opened.
	t.Run("a missing model or prompt never reaches the wire", func(t *testing.T) {
		reached := false
		client := media(t, func(http.ResponseWriter, *http.Request) { reached = true })
		if _, err := client.GenerateMusic(context.Background(), MusicRequest{Prompt: "a loop"}); err == nil {
			t.Fatal("a music request with no model was sent")
		}
		if _, err := client.GenerateMusic(context.Background(), MusicRequest{Model: "compose/model"}); err == nil {
			t.Fatal("a music request with no prompt was sent")
		}
		if reached {
			t.Fatal("an incomplete music request reached the provider")
		}
	})
}
