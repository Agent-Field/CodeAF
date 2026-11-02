package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestMemoryLaterStageDrainsSkippedEvidence(t *testing.T) {
	s := newLocalSetup(t)
	s.start()
	floor := s.backend.local.host.floor
	item, err := floor.Create(factory.Item{Repo: "acme/api", Cap: 1, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var extraction atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return
		}
		result := "Cedar queues jobs."
		var payload struct {
			Messages []knowledge.Source `json:"messages"`
		}
		if len(body.Messages) > 1 {
			_ = json.Unmarshal([]byte(body.Messages[1].Content), &payload)
		}
		if len(payload.Messages) > 0 {
			if extraction.Add(1) == 1 {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					close(cancelled)
					return
				}
			}
			claims := []any{}
			for _, source := range payload.Messages {
				claims = append(claims, map[string]any{"local_id": source.ID, "text": source.Text, "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": source.ID, "quote": source.Text}}})
			}
			raw, _ := json.Marshal(map[string]any{"claims": claims})
			result = string(raw)
		} else if len(body.Messages) > 1 && strings.HasPrefix(body.Messages[1].Content, "{") {
			result = `{"subjects":[]}`
		}
		raw, _ := json.Marshal(map[string]any{"model": knowledge.Model, "choices": []any{map[string]any{"delta": map[string]any{"content": result}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
	}))
	t.Cleanup(server.Close)
	original := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		if strings.Contains(u.Host, "openrouter.ai") {
			clone.URL.Scheme = "http"
			clone.URL.Host = server.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(clone)
	})}
	t.Cleanup(func() { http.DefaultClient = original })
	jobCtx, endJob := context.WithCancel(context.Background())
	t.Cleanup(endJob)
	root, profile := t.TempDir(), filepath.Join(s.home, "handoff-profile")
	meter := &stageBudget{floor: floor, item: item}
	agents := []*session.Agent{}
	for n := 0; n < 3; n++ {
		place := session.Place{Dir: filepath.Join(root, "bucket", session.NewSessionID())}
		if err := os.MkdirAll(place.Dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := markFactoryChat(floor, item, place.Transcript()); err != nil {
			t.Fatal(err)
		}
		agent, err := session.New(session.Config{Workspace: root, Place: place, SessionFile: place.Transcript(), ProfileDir: profile, Model: knowledge.Model, APIKey: "fixture", BaseURL: server.URL, JobBudget: meter, JobContext: jobCtx, Unattended: true, DeterministicNames: true})
		if err != nil {
			t.Fatal(err)
		}
		agents = append(agents, agent)
		t.Cleanup(func() { _ = agent.Close() })
	}
	vault := agents[0].Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	submit := func(agent *session.Agent, text string) {
		t.Helper()
		events, err := agent.Submit(context.Background(), text)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			for range events {
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("stage did not end within memory cap")
		}
	}
	submit(agents[0], "Cedar queues jobs.")
	select {
	case <-entered:
	default:
		t.Fatal("first stage did not start memory")
	}
	if err := agents[0].Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
		t.Fatal("closing a finished stage cancelled job-owned compilation")
	default:
	}
	submit(agents[1], "Birch retains skipped evidence.")
	if err := agents[1].Close(); err != nil {
		t.Fatal(err)
	}
	if extraction.Load() != 1 {
		t.Fatal("later stage stacked a compiler")
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := vault.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Pending == 0 && !snapshot.NeedsUpdate {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first compile did not finish: %+v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Publication finishes only after its provider charges have been banked.
	want := agents[0].Usage().CostUSD
	reopened, err := session.New(session.Config{Workspace: root, SessionFile: agents[0].SessionPath(), ProfileDir: profile, Model: knowledge.Model, APIKey: "fixture", BaseURL: server.URL, JobBudget: meter, JobContext: jobCtx, Unattended: true, DeterministicNames: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.Usage().CostUSD; want <= .01 || math.Abs(got-want) > .000001 {
		t.Fatalf("reopened stage lost paid memory: USD=%.4f want %.4f", got, want)
	}
	submit(agents[2], "Cedar queues jobs.")
	hits, err := vault.Search("Birch", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("later finished turn lost skipped stage's searchable evidence: hits=%v error=%v", hits, err)
	}
	page, err := vault.Read(hits[0].Page.ID)
	if err != nil || len(page.Evidence) == 0 {
		t.Fatalf("skipped stage citation missing: %+v %v", page, err)
	}
	endJob()
}
