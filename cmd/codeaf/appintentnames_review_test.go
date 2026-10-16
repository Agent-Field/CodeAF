package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A real in-process engine and wire client, with the local HTTP title response
// withheld until AFTER turn.ended. A turn-only title listener cannot pass this.
func TestAppIntentTitleAfterTurnEndsReachesNativeProjection(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	arrived, release := make(chan struct{}), make(chan struct{})
	var started, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	original := model.server.Config.Handler
	model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request recordedRequest
		_ = json.Unmarshal(raw, &request)
		words := request.texts()
		if len(words) > 0 && words[0] == "You name conversations." {
			started.Do(func() { close(arrived) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		original.ServeHTTP(w, r)
	})
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Text: "repair the tokenizer"})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 10*time.Second, appTurnDone)
	select {
	case <-arrived:
	case <-time.After(time.Second):
		t.Fatal("title request never arrived")
	}
	unblock.Do(func() { close(release) })
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && event == appwire.EventAgentChanged && c.Agent.ID == agent.ID && c.Agent.Title == "done"
	})
	if _, err := backend.Rename(agent.ID, "My tokenizer work"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Rename(agent.ID, "My final tokenizer name"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer fresh.Close()
	agents, err := fresh.ListAgents(workspace)
	if err != nil || len(agents) != 1 || agents[0].Title != "My final tokenizer name" {
		t.Fatalf("reopened agents=%+v error=%v", agents, err)
	}
	if got := len(namerRequests(model)); got != 1 {
		t.Fatalf("title requests=%d", got)
	}
}
