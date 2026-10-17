package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// TestSelfSendsTheWorkerCredentialAndSpeaksForOlderFactories: the capacity
// read carries the runner's own credential, never the person's token, and a
// factory without the route is "unsupported" rather than a lost lease.
func TestSelfSendsTheWorkerCredentialAndSpeaksForOlderFactories(t *testing.T) {
	for status, want := range map[int]error{404: ErrSelfUnsupported, 405: ErrSelfUnsupported, 401: ErrNotAttached, 200: nil} {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Authorization")
			if r.URL.Path != "/v1/runner/self" || r.Method != http.MethodGet {
				t.Errorf("%s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(status)
			if status == 200 {
				_, _ = w.Write([]byte(`{"slots":6}`))
			}
		}))
		self, err := Open(server.URL, "person-token").Worker("worker-credential").Self(context.Background())
		server.Close()
		if !errors.Is(err, want) && !(want == nil && err == nil) {
			t.Errorf("status %d: err = %v, want %v", status, err, want)
		}
		if got != "Bearer worker-credential" {
			t.Errorf("status %d: Authorization = %q", status, got)
		}
		if status == 200 && self.Slots != 6 {
			t.Errorf("slots = %d", self.Slots)
		}
	}
}

// TestAttachAttestsLiveCapacityInAHeaderNeverTheBody: the JSON a factory from
// before the field would refuse never carries it; one POST, header only when
// the worker attests.
func TestAttachAttestsLiveCapacityInAHeaderNeverTheBody(t *testing.T) {
	for _, live := range []bool{true, false} {
		posts, header, unknown := 0, "", error(nil)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			posts++
			header = r.Header.Get(api.LiveCapacityHeader)
			raw, _ := io.ReadAll(r.Body)
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			var old struct {
				Name   string `json:"name"`
				Handle string `json:"handle"`
				Slots  int    `json:"slots"`
			}
			unknown = dec.Decode(&old)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"credential":"c"}`))
		}))
		in := api.WorkerAttach{Name: "n", Handle: "h", Slots: 2, LiveCapacity: live}
		_, err := Open(server.URL, "").Worker("").Attach(context.Background(), in)
		server.Close()
		if err != nil || unknown != nil || posts != 1 {
			t.Errorf("live=%v: err=%v unknown=%v posts=%d", live, err, unknown, posts)
		}
		if want := map[bool]string{true: api.LiveCapacityHeaderValue, false: ""}[live]; header != want {
			t.Errorf("live=%v: header %q, want %q", live, header, want)
		}
		if !in.LiveCapacity == live {
			t.Errorf("caller's struct was mutated")
		}
	}
}
