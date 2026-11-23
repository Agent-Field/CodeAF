package directory_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// TestC7OldServiceRefusesTakesAcrossClients proves C7 at the HTTP door: a
// service without the promise never receives a forced take, from any client,
// and its absence is read again before each attempt (one read per refused take).
func TestC7OldServiceRefusesTakesAcrossClients(t *testing.T) {
	reads, takes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/acquire") {
			takes++
			t.Error("old service received a take")
		}
		reads++
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	for range 2 {
		client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
		_, err := client.Acquire(context.Background(), cellX, directory.AcquireOpts{Force: true, RequireConditional: true})
		if !errors.Is(err, directory.ErrConditionalTake) {
			t.Fatalf("take = %v", err)
		}
	}
	if reads != 2 || takes != 0 {
		t.Fatalf("requests: reads=%d takes=%d", reads, takes)
	}
}

func TestHTTPF2HeadGuardRequiresAtomicSupport(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "current"}[supported], func(t *testing.T) {
			takes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(directory.ConditionalTakeHeader, "1")
				if supported {
					w.Header().Set(directory.HeadTakeHeader, "1")
				}
				if strings.HasSuffix(r.URL.Path, "/acquire") {
					takes++
					var body directory.AcquireOpts
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Expect == nil || body.Expect.Head != "saved-head" {
						t.Error("F2 head guard was stripped")
					}
				}
				_ = json.NewEncoder(w).Encode(directory.CellView{})
			}))
			defer srv.Close()
			client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
			_, err := client.Acquire(t.Context(), cellX, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: devA, Fence: 1, Head: "saved-head"}})
			if supported && (err != nil || takes != 1) {
				t.Fatalf("F2 supported take failed: %v, %d requests", err, takes)
			}
			if !supported && (!errors.Is(err, directory.ErrConditionalTake) || takes != 0) {
				t.Fatalf("F2 unsupported atomic guard was weakened: %v, %d requests", err, takes)
			}
		})
	}
}

// TestOldServiceStillTakesALapsedLease pins #2072: a lapsed lease is taken on
// a service without the promise, because its own acquire still refuses a live
// holder. Waiting for the service's update must never lock a person out.
func TestOldServiceStillTakesALapsedLease(t *testing.T) {
	takes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/acquire") {
			takes++
		}
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	_, err := client.Acquire(context.Background(), cellX, directory.AcquireOpts{RequireConditional: true, Expect: &directory.LeaseExpectation{Device: devA, Fence: 1}})
	if err != nil || takes != 1 {
		t.Fatalf("lapsed take on an old service: err=%v takes=%d", err, takes)
	}
}

// TestC7OldServiceStillAllowsAnOrdinaryResume proves C7's compatibility
// boundary: a local non-forced resume cannot displace a live other holder.
func TestC7OldServiceStillAllowsAnOrdinaryResume(t *testing.T) {
	reads, resumes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
			resumes++
		} else {
			reads++
		}
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	_, err := client.Acquire(context.Background(), cellX, directory.AcquireOpts{Expect: &directory.LeaseExpectation{Device: devA, Fence: 1}})
	if err != nil || resumes != 1 || reads != 0 {
		t.Fatalf("ordinary resume: err=%v resumes=%d reads=%d", err, resumes, reads)
	}
}

// TestC7AServiceUpdatedWhileRunningMovesChatsAtTheNextAttempt proves C7's
// update boundary: a hosted service redeployed while codeaf runs is trusted at
// the next take, without a restart, and its promise is then kept.
func TestC7AServiceUpdatedWhileRunningMovesChatsAtTheNextAttempt(t *testing.T) {
	updated, reads, takes := false, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if updated {
			w.Header().Set(directory.ConditionalTakeHeader, "1")
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
			takes++
		} else {
			reads++
		}
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	if _, err := client.Acquire(context.Background(), cellX, directory.AcquireOpts{Force: true}); !errors.Is(err, directory.ErrConditionalTake) {
		t.Fatalf("take before the update = %v", err)
	}
	updated = true
	for range 2 {
		if _, err := client.Acquire(context.Background(), cellX, directory.AcquireOpts{Force: true}); err != nil {
			t.Fatalf("take after the update = %v", err)
		}
	}
	if reads != 2 || takes != 2 {
		t.Fatalf("requests: reads=%d takes=%d; want one read before each refused or first trusted take", reads, takes)
	}
}
