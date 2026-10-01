package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// A model switch takes effect at the next request boundary. The same live
// connection must observe both profile transitions through pushed facts.
func TestRemoteMemoryAvailabilityFollowsAutomaticProfileBothWays(t *testing.T) {
	t.Setenv("CODEAF_PROMPT_PROFILE", "")
	for _, initial := range []string{"test/small", "test/large"} {
		t.Run(initial, func(t *testing.T) {
			brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = brain.Close() })
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"data":[]}`)
					return
				}
				// The real session also sends non-streaming naming and memory
				// requests, so those callers need their ordinary JSON response.
				var request struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if !request.Stream {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(model.Close)
			far, err := session.New(session.Config{Workspace: t.TempDir(), Model: initial, APIKey: "fixture", BaseURL: model.URL + "/v1", Memory: brain, ContextWindowFor: func(model string) int {
				if model == "test/small" {
					return 16385
				}
				return 128000
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = far.Close() })
			loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loop.Close() })
			handle := loop.Client.Agent()
			wantInitial := initial == "test/large"
			if handle.Remembers() != wantInitial || far.Remembers() != wantInitial {
				t.Fatal("initial profile memory availability is wrong")
			}
			next := []string{"test/large", "test/small"}
			if wantInitial {
				next = []string{"test/small", "test/large"}
			}
			for _, model := range next {
				handle.SetModel(model)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				events, err := handle.Submit(ctx, "hello")
				if err != nil {
					cancel()
					t.Fatal(err)
				}
				done := false
				for !done {
					select {
					case ev, ok := <-events:
						if !ok {
							done = true
							break
						}
						if ev.Kind == session.EventError {
							cancel()
							t.Fatal(ev.Err)
						}
					case <-ctx.Done():
						cancel()
						t.Fatal("profile transition request did not finish")
					}
				}
				cancel()
				want := model == "test/large"
				if far.Remembers() != want {
					t.Fatalf("engine memory=%v for %s", far.Remembers(), model)
				}
				if handle.Remembers() != want {
					t.Fatalf("remote memory=%v for %s, want %v without a new welcome", handle.Remembers(), model, want)
				}
				if want {
					if _, err := handle.Memories(""); err != nil {
						t.Fatal(err)
					}
				} else if _, err := handle.Memories(""); err == nil {
					t.Fatal("lean engine allowed memory")
				}
			}
		})
	}
}
