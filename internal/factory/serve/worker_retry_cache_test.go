package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestWorkerRetryableResponsesAreNotCached(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, status := range []int{408, 422, 429, 500, 503} {
			t.Run(http.StatusText(status)+map[bool]string{true: "-disk", false: "-memory"}[durable], func(t *testing.T) {
				h := &Handler{mux: http.NewServeMux()}
				if durable {
					var err error
					h.config.Store, err = store.Open(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
				}
				calls := 0
				h.mux.HandleFunc("/v1/workers/leases/test/report", func(w http.ResponseWriter, _ *http.Request) {
					calls++
					if calls == 1 {
						w.WriteHeader(status)
					} else {
						w.WriteHeader(200)
					}
				})
				for _, want := range []int{status, 200, 200} {
					r := httptest.NewRequest("POST", "/v1/workers/leases/test/report", strings.NewReader(`{}`))
					r.Header.Set("Idempotency-Key", "same")
					w := httptest.NewRecorder()
					h.idempotent(w, r)
					if w.Code != want {
						t.Fatalf("status=%d want=%d", w.Code, want)
					}
				}
				if calls != 2 {
					t.Fatalf("handler called %d times", calls)
				}
			})
		}
	}
}

func TestWorkerReportRevalidatesLegacyCachedRefusalAndDeduplicatesAcceptance(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, namespace := range []string{"workers", "runners"} {
			t.Run(namespace+map[bool]string{true: "-disk", false: "-memory"}[durable], func(t *testing.T) {
				st, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				it, err := st.Create(factory.Item{Title: "retain completed results", State: factory.StateRunning, Stream: &factory.Stream{}})
				if err != nil {
					t.Fatal(err)
				}
				h := &Handler{mux: http.NewServeMux()}
				if durable {
					h.config.Store = st
				}
				path := "/v1/" + namespace + "/leases/test/report"
				request := func(body string) *http.Request {
					r := httptest.NewRequest("POST", path, strings.NewReader(body))
					r.Header.Set("Authorization", "Bearer worker")
					r.Header.Set("Idempotency-Key", "same-completed-report")
					return r
				}
				// Seed exactly the refusal an older server persisted. The upgraded
				// handler must bypass it, while replacing it with a replayable success.
				r := request(`{"cost":0.25}`)
				sum := sha256.Sum256([]byte(r.Header.Get("Authorization") + "\x00" + r.Header.Get("Cookie") + "\x00" + r.URL.RequestURI() + "\x00" + r.Header.Get("Idempotency-Key")))
				id := hex.EncodeToString(sum[:])
				refusal := replay{At: h.clock(), Status: http.StatusUnprocessableEntity, Body: []byte(`{"error":"follow-up identity changed"}`)}
				if durable {
					dir := filepath.Join(st.Root(), "idempotency")
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					raw, err := json.Marshal(refusal)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					h.replays.rows = map[string]replay{id: refusal}
				}
				calls := 0
				h.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body struct{ Cost float64 }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(422)
						return
					}
					if err := st.Update(it.ID, func(it *factory.Item) error { it.Stream.Spent += body.Cost; return nil }); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(200)
				})
				for _, body := range []string{`{"cost":0.25}`, `{"cost":0.25}`, `invalid replay must not run`} {
					w := httptest.NewRecorder()
					h.idempotent(w, request(body))
					if w.Code != 200 {
						t.Fatalf("retained report remained refused: %d %s", w.Code, w.Body)
					}
				}
				got, err := st.Get(it.ID)
				if err != nil || calls != 1 || got.Stream.Spent != .25 {
					t.Fatalf("accepted report repeated work/cost: calls=%d spent=%+v err=%v", calls, got.Stream, err)
				}
			})
		}
	}
}

func TestReportRefusalRevalidationPreservesOtherActionResponses(t *testing.T) {
	for _, path := range []string{"/v1/workers/leases/test/done", "/v1/runners/leases/test/done", "/v1/jobs/1/answer"} {
		t.Run(path, func(t *testing.T) {
			h := &Handler{mux: http.NewServeMux()}
			calls := 0
			h.mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(422) })
			for range 2 {
				r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
				r.Header.Set("Idempotency-Key", "same-action")
				w := httptest.NewRecorder()
				h.idempotent(w, r)
				if w.Code != 422 {
					t.Fatalf("action response changed: %d", w.Code)
				}
			}
			if calls != 1 {
				t.Fatalf("non-report action revalidated %d times", calls)
			}
		})
	}
}
