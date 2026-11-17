package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func transientReplayHandler(t *testing.T, durable bool) (*Handler, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	h := &Handler{mux: http.NewServeMux(), config: Config{Now: func() time.Time { return clock }}}
	if durable {
		var err error
		h.config.Store, err = store.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
	}
	return h, &clock
}

func transientKeyedRequest(path string) *http.Request {
	r := httptest.NewRequest("POST", path+"?mode=test", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer reader")
	r.Header.Set("Cookie", "session=reader")
	r.Header.Set("Idempotency-Key", "same-attempt")
	return r
}

// servedKeyed sends r as ServeHTTP does: a route that markPreMutation lists
// starts with the phase marker on the writer, which only its handler clears.
func servedKeyed(h *Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	if markPreMutation(r) {
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
	}
	h.idempotent(w, r)
	return w
}

func seedTransientReplay(t *testing.T, h *Handler, path string, status int) {
	t.Helper()
	r := transientKeyedRequest(path)
	sum := sha256.Sum256([]byte(r.Header.Get("Authorization") + "\x00" + r.Header.Get("Cookie") + "\x00" + r.URL.RequestURI() + "\x00" + r.Header.Get("Idempotency-Key")))
	id := hex.EncodeToString(sum[:])
	row := replay{At: h.clock(), Status: status, Body: []byte("old transient answer")}
	if h.config.Store == nil {
		h.replays.rows = map[string]replay{id: row}
		return
	}
	dir := filepath.Join(h.config.Store.Root(), "idempotency")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func storeName(durable bool) string { return map[bool]string{false: "/memory", true: "/disk"}[durable] }

// C1, C2, C4: An answer that wrote nothing reaches its caller once, then the
// same key runs again and replays its success. On a job create that is any
// answer still carrying the phase marker, a refusal as much as a 5xx; on a
// worker's route it is a 5xx, 408 or 429, and an older build's row of one is
// bypassed, as before.
func TestAKeyedServerErrorRunsAgainOnRetry(t *testing.T) {
	for _, route := range []struct {
		path     string
		statuses []int
		seeded   []bool
	}{
		{"/v1/jobs", []int{500, 503, 429, 408, 422, 409}, []bool{false}},
		{"/v1/runners/pull", []int{500, 503, 429, 408}, []bool{false, true}},
	} {
		for _, durable := range []bool{false, true} {
			for _, seeded := range route.seeded {
				for _, status := range route.statuses {
					name := route.path + "/" + strconv.Itoa(status) + storeName(durable) + map[bool]string{false: "/fresh", true: "/old-row"}[seeded]
					t.Run(name, func(t *testing.T) {
						h, _ := transientReplayHandler(t, durable)
						if seeded {
							seedTransientReplay(t, h, route.path, status)
						}
						calls := 0
						h.mux.HandleFunc(route.path, func(w http.ResponseWriter, _ *http.Request) {
							calls++
							code, body := 200, "accepted"
							if !seeded && calls == 1 {
								code, body = status, "retry this answer"
							} else {
								clearPhase(w)
							}
							w.Header().Set("X-Answer", body)
							w.WriteHeader(code)
							_, _ = w.Write([]byte(body))
						})
						for attempt := range 3 {
							w := servedKeyed(h, transientKeyedRequest(route.path))
							wantStatus, wantBody, wantCalls, wantPhase := 200, "accepted", 2, ""
							if seeded {
								wantCalls = 1
								if durable {
									// A fresh handler must read the replacement from disk.
									h = &Handler{mux: h.mux, config: h.config}
								}
							} else if attempt == 0 {
								wantStatus, wantBody, wantCalls = status, "retry this answer", 1
								if route.path == "/v1/jobs" {
									wantPhase = api.PhasePreMutation
								}
							}
							if w.Code != wantStatus || w.Body.String() != wantBody || w.Header().Get("X-Answer") != wantBody || calls != wantCalls || w.Header().Get(api.PhaseHeader) != wantPhase {
								t.Fatalf("attempt %d = %d %q, header=%q phase=%q calls=%d; want %d %q phase=%q calls=%d", attempt, w.Code, w.Body.String(), w.Header().Get("X-Answer"), w.Header().Get(api.PhaseHeader), calls, wantStatus, wantBody, wantPhase, wantCalls)
							}
						}
					})
				}
			}
		}
	}
}

// A keyed answer given after the handler's first change is kept, whatever its
// status, so a resend can never make that change twice. The replay never
// carries the phase marker. A create row an older build kept is replayed for
// the same reason: nothing in it says the create wrote nothing.
func TestAKeyedAnswerAfterAWriteIsReplayed(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, seeded := range []bool{false, true} {
			for _, status := range []int{500, 502, 503, 429, 408, 409} {
				name := strconv.Itoa(status) + storeName(durable) + map[bool]string{false: "/fresh", true: "/old-row"}[seeded]
				t.Run(name, func(t *testing.T) {
					h, _ := transientReplayHandler(t, durable)
					wantBody := "the job exists; its start failed"
					if seeded {
						seedTransientReplay(t, h, "/v1/jobs", status)
						wantBody = "old transient answer"
					}
					calls := 0
					h.mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, _ *http.Request) {
						calls++
						clearPhase(w)
						if calls > 1 {
							w.WriteHeader(201)
							_, _ = w.Write([]byte("a second job"))
							return
						}
						w.WriteHeader(status)
						_, _ = w.Write([]byte("the job exists; its start failed"))
					})
					wantCalls := 1
					if seeded {
						wantCalls = 0
					}
					for attempt := range 3 {
						w := servedKeyed(h, transientKeyedRequest("/v1/jobs"))
						if w.Code != status || w.Body.String() != wantBody || calls != wantCalls || w.Header().Get(api.PhaseHeader) != "" {
							t.Fatalf("attempt %d = %d %q phase=%q calls=%d; want %d %q replayed, calls=%d", attempt, w.Code, w.Body.String(), w.Header().Get(api.PhaseHeader), calls, status, wantBody, wantCalls)
						}
						if durable {
							h = &Handler{mux: h.mux, config: h.config}
						}
					}
				})
			}
		}
	}
}

// C3: A keyed answer that is kept replays for 24 hours, in memory and across a
// store-backed restart, then expires. Each request goes through the marker
// ServeHTTP sets (servedKeyed), so this is what the route really does: a job
// create's success, whose handler cleared the marker before making the job,
// and a refusal of a job verb that is not marked (markPreMutation), such as
// approve, which keeps every keyed answer as before. A job create's own
// refusal is marked and is not kept (TestAKeyedServerErrorRunsAgainOnRetry,
// and on the real handler TestAKeyedCreateThatWroteNothingRunsAgain).
func TestAKeptKeyedAnswerReplaysForADay(t *testing.T) {
	for _, route := range []struct {
		name, path string
		status     int
		write      bool // the handler clears the marker, as before a write
	}{
		{"a job create's success", "/v1/jobs", http.StatusCreated, true},
		{"an unmarked verb's refusal", "/v1/jobs/7/approve", http.StatusUnprocessableEntity, false},
		{"an unmarked verb's conflict", "/v1/jobs/7/approve", http.StatusConflict, false},
	} {
		for _, durable := range []bool{false, true} {
			t.Run(route.name+storeName(durable), func(t *testing.T) {
				h, clock := transientReplayHandler(t, durable)
				calls := 0
				h.mux.HandleFunc(route.path, func(w http.ResponseWriter, _ *http.Request) {
					calls++
					if route.write {
						clearPhase(w)
					}
					w.WriteHeader(route.status)
					_, _ = w.Write([]byte(strconv.Itoa(calls)))
				})
				for attempt := range 3 {
					w := servedKeyed(h, transientKeyedRequest(route.path))
					wantCalls := 1
					if attempt == 2 {
						wantCalls = 2
					}
					if w.Code != route.status || w.Body.String() != strconv.Itoa(wantCalls) || calls != wantCalls || w.Header().Get(api.PhaseHeader) != "" {
						t.Fatalf("attempt %d = %d %q phase=%q calls=%d, want status=%d calls=%d", attempt, w.Code, w.Body.String(), w.Header().Get(api.PhaseHeader), calls, route.status, wantCalls)
					}
					if attempt == 0 {
						*clock = clock.Add(24*time.Hour - time.Nanosecond)
						if durable {
							h = &Handler{mux: h.mux, config: h.config}
						}
					} else {
						*clock = clock.Add(time.Nanosecond)
					}
				}
			})
		}
	}
}
