package voice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Spoken input reaches the same router, under the same key, as typed input — so
// it has to say it is the same app. This client wrote its own two headers by
// hand for a release and drifted the moment a third one existed: every word
// somebody spoke was attributed as an unclassified app while every word they
// typed was attributed correctly, and nothing failed, which is why it lasted.
//
// The assertion is the WHOLE SET and not only the categories header, because
// the fault was never one missing value — it was a second place that thought it
// knew the list.
func TestATranscriptionCarriesTheWholeAttributionSet(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"hello"}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		APIKey:         "key",
		BaseURL:        server.URL,
		SiteURL:        "https://agentfield.ai",
		SiteName:       "AgentField AI",
		SiteCategories: "cli-agent,programming-app",
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Transcribe(context.Background(), []byte("RIFF....WAVE"), TranscriptionOptions{Model: "whisper"}); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	for header, want := range map[string]string{
		"HTTP-Referer":            "https://agentfield.ai",
		"X-OpenRouter-Title":      "AgentField AI",
		"X-Title":                 "AgentField AI",
		"X-OpenRouter-Categories": "cli-agent,programming-app",
	} {
		if got := seen.Get(header); got != want {
			t.Errorf("%s: got %q, want %q", header, got, want)
		}
	}
}

// An operator who cleared a field asked not to be attributed by it, and an empty
// header is a claim about the app rather than the absence of one.
func TestATranscriptionSendsNothingItWasNotGiven(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Write([]byte(`{"text":"hello"}`))
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "key", BaseURL: server.URL, HTTPClient: server.Client()})
	if _, err := client.Transcribe(context.Background(), []byte("RIFF....WAVE"), TranscriptionOptions{Model: "whisper"}); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	for _, header := range []string{"HTTP-Referer", "X-OpenRouter-Title", "X-Title", "X-OpenRouter-Categories"} {
		if _, present := seen[http.CanonicalHeaderKey(header)]; present {
			t.Errorf("%s was sent empty", header)
		}
	}
}
