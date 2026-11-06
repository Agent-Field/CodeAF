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

func transientKeyedRequest() *http.Request {
	r := httptest.NewRequest("POST", "/v1/jobs?mode=test", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer reader")
	r.Header.Set("Cookie", "session=reader")
	r.Header.Set("Idempotency-Key", "same-attempt")
	return r
}

func seedTransientReplay(t *testing.T, h *Handler, status int) {
	t.Helper()
	r := transientKeyedRequest()
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

// C1, C2, C4: A transient answer reaches its caller once, then the same key
// runs again and replays its success. Older memory and disk rows are bypassed.
func TestAKeyedServerErrorRunsAgainOnRetry(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, seeded := range []bool{false, true} {
			for _, status := range []int{500, 503, 429} {
				name := strconv.Itoa(status) + map[bool]string{false: "/memory", true: "/disk"}[durable] + map[bool]string{false: "/fresh", true: "/old-row"}[seeded]
				t.Run(name, func(t *testing.T) {
					h, _ := transientReplayHandler(t, durable)
					if seeded {
						seedTransientReplay(t, h, status)
					}
					calls := 0
					h.mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, _ *http.Request) {
						calls++
						code, body := 200, "accepted"
						if !seeded && calls == 1 {
							code, body = status, "retry this answer"
						}
						w.Header().Set("X-Answer", body)
						w.WriteHeader(code)
						_, _ = w.Write([]byte(body))
					})
					for attempt := range 3 {
						w := httptest.NewRecorder()
						h.idempotent(w, transientKeyedRequest())
						wantStatus, wantBody, wantCalls := 200, "accepted", 2
						if seeded {
							wantCalls = 1
							if durable {
								// A fresh handler must read the replacement from disk.
								h = &Handler{mux: h.mux, config: h.config}
							}
						} else if attempt == 0 {
							wantStatus, wantBody, wantCalls = status, "retry this answer", 1
						}
						if w.Code != wantStatus || w.Body.String() != wantBody || w.Header().Get("X-Answer") != wantBody || calls != wantCalls {
							t.Fatalf("attempt %d = %d %q, header=%q calls=%d; want %d %q calls=%d", attempt, w.Code, w.Body.String(), w.Header().Get("X-Answer"), calls, wantStatus, wantBody, wantCalls)
						}
					}
				})
			}
		}
	}
}

// C3: Successful and definitive refusal answers still replay for 24 hours
// in memory and across a store-backed restart, then expire as before.
func TestKeyedSuccessAndRefusalsStillReplay(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, status := range []int{200, 422} {
			t.Run(strconv.Itoa(status)+map[bool]string{false: "/memory", true: "/disk"}[durable], func(t *testing.T) {
				h, clock := transientReplayHandler(t, durable)
				calls := 0
				h.mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, _ *http.Request) {
					calls++
					w.WriteHeader(status)
					_, _ = w.Write([]byte(strconv.Itoa(calls)))
				})
				for attempt := range 3 {
					w := httptest.NewRecorder()
					h.idempotent(w, transientKeyedRequest())
					wantCalls := 1
					if attempt == 2 {
						wantCalls = 2
					}
					if w.Code != status || w.Body.String() != strconv.Itoa(wantCalls) || calls != wantCalls {
						t.Fatalf("attempt %d = %d %q calls=%d, want status=%d calls=%d", attempt, w.Code, w.Body.String(), calls, status, wantCalls)
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
