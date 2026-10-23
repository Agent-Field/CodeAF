package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestWorkerRetryableResponsesAreNotCached(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, status := range []int{408, 429, 500, 503} {
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
