package serve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestThreadReturnsMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	const body = `{"type":"session","version":1,"id":"n","cwd":"/tmp","timestamp":"2026-10-11T12:00:00Z"}
{"type":"message","role":"user","content":"ship the proof","timestamp":"2026-10-11T12:00:01.5Z"}
{"type":"message","role":"assistant","content":"Proof is ready.","timestamp":"2026-10-11T12:01:00Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	seam := factory.FixtureSeam(now)
	seam.Load = func() (factory.Snapshot, error) {
		s := factory.Fixture(now)
		s.Items[0].Talk = path
		return s, nil
	}
	seam.Talk = func(_ context.Context, id int) (string, error) {
		if id != 1 {
			t.Fatalf("Talk id = %d", id)
		}
		return path, nil
	}
	s := httptest.NewServer(serve.New(seam, config))
	defer s.Close()

	var got struct {
		Messages []api.Message `json:"messages"`
	}
	getJSON(t, s.URL+"/v1/jobs/1/thread", &got)
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	user, manager := got.Messages[0], got.Messages[1]
	if user.From != "user" || user.Text != "ship the proof" || !user.At.Equal(time.Date(2026, 10, 11, 12, 0, 1, int(500*time.Millisecond), time.UTC)) {
		t.Fatalf("user message = %+v", user)
	}
	if manager.From != "manager" || manager.Text != "Proof is ready." || !manager.At.Equal(time.Date(2026, 10, 11, 12, 1, 0, 0, time.UTC)) {
		t.Fatalf("manager message = %+v", manager)
	}
	raw, err := json.Marshal(got.Messages[0])
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"at", "from", "text"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("message missing %s: %s", key, raw)
		}
	}
}

func TestThreadIsEmptyWhenNone(t *testing.T) {
	seam := factory.FixtureSeam(now)
	seam.Talk = func(context.Context, int) (string, error) {
		t.Fatal("a job with no transcript was asked to make one")
		return "", nil
	}
	h := serve.New(seam, config)
	r := httptest.NewRequest(http.MethodGet, "/v1/jobs/1/thread", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	checkJSON(t, w, http.StatusOK)
	var got struct {
		Messages []api.Message `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages == nil || len(got.Messages) != 0 {
		t.Fatalf("messages = %#v, want an empty list", got.Messages)
	}

	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	seam.Load = func() (factory.Snapshot, error) {
		s := factory.Fixture(now)
		s.Items[0].Talk = missing
		return s, nil
	}
	seam.Talk = func(context.Context, int) (string, error) { return missing, nil }
	w = httptest.NewRecorder()
	serve.New(seam, config).ServeHTTP(w, r)
	checkJSON(t, w, http.StatusOK)
	got = struct {
		Messages []api.Message `json:"messages"`
	}{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Messages == nil || len(got.Messages) != 0 {
		t.Fatalf("missing file messages = %#v, want an empty list", got.Messages)
	}
}
