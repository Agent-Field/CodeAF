package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestTranscriberUsesOpenRouterJSONAudioContract(t *testing.T) {
	var captured *http.Request
	var body map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		data, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(data, &body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"text":" hello world ","usage":{"seconds":2.5,"cost":0.004}}`)),
		}, nil
	})}
	client, err := NewClient(ClientConfig{
		APIKey: "secret", BaseURL: "https://openrouter.test/api/v1/", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	temperature := 0.1
	audio := []byte("raw wav bytes")
	result, err := client.Transcribe(context.Background(), audio, TranscriptionOptions{
		Model: "qwen/asr", Language: "en", Temperature: &temperature,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello world" || result.Usage.Cost != 0.004 {
		t.Fatalf("result = %+v", result)
	}
	if captured.Method != http.MethodPost || captured.URL.String() != "https://openrouter.test/api/v1/audio/transcriptions" {
		t.Fatalf("request = %s %s", captured.Method, captured.URL)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("authorization = %q", got)
	}
	input, _ := body["input_audio"].(map[string]any)
	if got := input["data"]; got != base64.StdEncoding.EncodeToString(audio) {
		t.Fatalf("audio data = %q", got)
	}
	if got := input["format"]; got != "wav" {
		t.Fatalf("audio format = %q", got)
	}
	if strings.HasPrefix(input["data"].(string), "data:") {
		t.Fatal("audio was encoded as a data URI")
	}
	if body["language"] != "en" || body["temperature"] != 0.1 {
		t.Fatalf("optional fields = %#v", body)
	}
}
