package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestVideoPollingCredentialOrigin(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint("foreign=", foreign), func(t *testing.T) {
			auth := "missing"
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth = r.Header.Get("Authorization")
				w.WriteHeader(401)
				fmt.Fprint(w, `{"error":{"message":"polling refused"}}`)
			}))
			defer other.Close()
			var base *httptest.Server
			base = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "POST" {
					poll := base.URL + "/poll"
					if foreign {
						poll = other.URL + "/poll"
					}
					fmt.Fprintf(w, `{"id":"job","status":"pending","polling_url":%q}`, poll)
					return
				}
				auth = r.Header.Get("Authorization")
				fmt.Fprint(w, `{"id":"job","status":"failed","error":"mock job failed"}`)
			}))
			defer base.Close()
			client, err := NewMediaClient(Config{APIKey: "mock-media-key", BaseURL: base.URL})
			if err != nil {
				t.Fatal(err)
			}
			client.videoWait = func(context.Context, time.Duration) error { return nil }
			_, err = client.GenerateVideo(context.Background(), VideoRequest{Model: "mock/video", Prompt: "a mock"})
			if err == nil {
				t.Fatal("expected job refusal")
			}
			want := "Bearer mock-media-key"
			if foreign {
				want = ""
				if !strings.Contains(err.Error(), strings.TrimPrefix(other.URL, "http://")) {
					t.Fatalf("missing polling host: %v", err)
				}
			}
			if auth != want {
				t.Fatalf("poll auth=%q want=%q", auth, want)
			}
		})
	}
}
func TestCustomToolLoopUsesGuardedCallerTransport(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, `{}`) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+"/steal?key=mock-query-secret")
		w.WriteHeader(307)
	}))
	defer source.Close()
	caller := &http.Client{Timeout: time.Second}
	client, err := NewClient(Config{APIKey: "mock-provider-key", BaseURL: source.URL, Model: "mock/model", HTTPClient: caller})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.ExecuteToolCallLoop(context.Background(), userMessages("hello"), nil, ai.ToolCallConfig{MaxTurns: 1}, func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) { return nil, nil })
	if err == nil || !strings.Contains(err.Error(), strings.TrimPrefix(target.URL, "http://")) || strings.Contains(err.Error(), "mock-query-secret") {
		t.Fatalf("unsafe loop result: %v", err)
	}
	if hits != 0 {
		t.Fatal("SDK/custom loop escaped credential boundary")
	}
	if caller.CheckRedirect != nil {
		t.Fatal("caller mutated")
	}
}
